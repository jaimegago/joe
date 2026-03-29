package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/safety"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/internal/tools"
	"github.com/jaimegago/joe/internal/uid"
	"github.com/jaimegago/joe/internal/useragent"
)

// taskHandler handles POST /api/v1/tasks — full agentic loop over HTTP.
type taskHandler struct {
	server *Server
}

// --- Request / Response types ---

type taskRequest struct {
	Message   string      `json:"message"`
	SessionID string      `json:"session_id,omitempty"`
	Config    *taskConfig `json:"config,omitempty"`
}

type taskConfig struct {
	MaxIterations *int   `json:"max_iterations,omitempty"`
	SafetyTier    string `json:"safety_tier,omitempty"`
	Timeout       string `json:"timeout,omitempty"`
}

type taskResponse struct {
	TaskID      string         `json:"task_id"`
	SessionID   string         `json:"session_id"`
	Status      string         `json:"status"`
	Iterations  int            `json:"iterations"`
	Steps       []taskStep     `json:"steps"`
	FinalAnswer string         `json:"final_answer"`
	ToolsUsed   []string       `json:"tools_used"`
	TotalTokens taskTokenUsage `json:"total_tokens"`
	DurationMs  int            `json:"duration_ms"`
	Error       string         `json:"error,omitempty"`
}

type taskStep struct {
	StepNumber  int              `json:"step_number"`
	LLMRequest  taskLLMRequest   `json:"llm_request"`
	LLMResponse taskLLMResponse  `json:"llm_response"`
	ToolResults []taskToolResult `json:"tool_results,omitempty"`
}

type taskLLMRequest struct {
	MessageCount   int      `json:"message_count"`
	ToolsAvailable []string `json:"tools_available"`
}

type taskLLMResponse struct {
	Content   string         `json:"content"`
	ToolCalls []taskToolCall `json:"tool_calls,omitempty"`
	Usage     taskTokenUsage `json:"usage"`
}

type taskToolCall struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type taskToolResult struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Result     any    `json:"result"`
	Error      string `json:"error,omitempty"`
	DurationMs int    `json:"duration_ms"`
}

type taskTokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// --- Handler ---

func (h *taskHandler) handleTask(w http.ResponseWriter, r *http.Request) {
	if h.server.services.LLM == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "LLM not available")
		return
	}

	var req taskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "invalid request body")
		return
	}
	if req.Message == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "message is required")
		return
	}

	// Parse timeout
	timeout := 5 * time.Minute
	if req.Config != nil && req.Config.Timeout != "" {
		parsed, err := time.ParseDuration(req.Config.Timeout)
		if err != nil {
			writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, fmt.Sprintf("invalid timeout: %s", err))
			return
		}
		timeout = parsed
	}

	// Resolve max iterations
	maxIterations := useragent.DefaultMaxIterations
	if req.Config != nil && req.Config.MaxIterations != nil {
		if *req.Config.MaxIterations < 1 {
			writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "max_iterations must be >= 1")
			return
		}
		maxIterations = *req.Config.MaxIterations
	}

	// Resolve safety policy
	safetyPolicy := h.resolveSafetyPolicy(req.Config)

	// Session setup
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = uid.New()
	}
	if h.server.services.Store != nil {
		sess, _ := h.server.services.Store.Sessions.Get(r.Context(), sessionID)
		if sess == nil {
			_ = h.server.services.Store.Sessions.Create(r.Context(), &store.Session{
				ID:        sessionID,
				StartedAt: time.Now().UTC(),
			})
		}
	}

	// Build the agent with a loopback client
	addr := h.server.services.Config.Server.Address
	scheme := "http"
	if h.server.services.Config.Server.TLSConfigured() {
		scheme = "https"
	}
	loopbackURL := fmt.Sprintf("%s://%s", scheme, addr)

	var clientOpts []client.ClientOption
	if h.server.services.Config.Server.APIKey != "" {
		clientOpts = append(clientOpts, client.WithAPIKey(h.server.services.Config.Server.APIKey))
	}
	if scheme == "https" {
		clientOpts = append(clientOpts, client.WithTLS())
	}
	loopbackClient := client.New(loopbackURL, clientOpts...)

	registry := tools.NewCoreRegistry(loopbackClient, safetyPolicy)
	executor := tools.NewExecutor(registry, h.server.services.Metrics,
		tools.WithPolicy(safetyPolicy),
	)

	// Build graph context for system prompt
	systemPrompt := taskSystemPrompt
	if h.server.services.Graph != nil {
		if summary, err := h.server.services.Graph.Summary(r.Context()); err == nil {
			systemPrompt += fmt.Sprintf(
				"\n\nCurrent infrastructure context:\nInfrastructure graph: %d nodes, %d edges. Node types: %v",
				summary.NodeCount, summary.EdgeCount, summary.NodesByType,
			)
		}
	}

	// Create agent with observer
	observer := &useragent.SliceObserver{}
	agent := useragent.NewAgent(
		h.server.services.LLM,
		executor,
		registry,
		systemPrompt,
		useragent.WithObserver(observer),
	)
	agent.SetMaxIterations(maxIterations)

	// Create session
	metrics := observability.EnsureMetrics(h.server.services.Metrics)
	session := useragent.NewSession(metrics)
	session.MaxMessages = useragent.DefaultMaxMessages
	defer session.Close()

	// Run with timeout
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	taskID := uid.New()
	start := time.Now()
	answer, runErr := agent.Run(ctx, session, req.Message)
	duration := time.Since(start)

	// Determine status
	status := "completed"
	var errMsg string
	if runErr != nil {
		switch {
		case ctx.Err() == context.DeadlineExceeded:
			status = "timeout"
			errMsg = "task timed out"
		case isMaxIterationsError(runErr):
			status = "max_iterations_reached"
			errMsg = runErr.Error()
		default:
			status = "error"
			errMsg = runErr.Error()
		}
	}

	// Build steps from observer
	steps := make([]taskStep, len(observer.Steps))
	toolsUsedSet := map[string]struct{}{}
	for i, s := range observer.Steps {
		step := taskStep{
			StepNumber: s.StepNumber,
			LLMRequest: taskLLMRequest{
				MessageCount:   s.LLMRequest.MessageCount,
				ToolsAvailable: s.LLMRequest.ToolsAvailable,
			},
			LLMResponse: taskLLMResponse{
				Content: s.LLMResponse.Content,
				Usage: taskTokenUsage{
					InputTokens:  s.LLMResponse.Usage.InputTokens,
					OutputTokens: s.LLMResponse.Usage.OutputTokens,
				},
			},
		}

		// Map tool calls
		if len(s.LLMResponse.ToolCalls) > 0 {
			step.LLMResponse.ToolCalls = make([]taskToolCall, len(s.LLMResponse.ToolCalls))
			for j, tc := range s.LLMResponse.ToolCalls {
				step.LLMResponse.ToolCalls[j] = taskToolCall{
					ID:   tc.ID,
					Name: tc.Name,
					Args: tc.Args,
				}
			}
		}

		// Map tool results
		if len(s.ToolResults) > 0 {
			step.ToolResults = make([]taskToolResult, len(s.ToolResults))
			for j, tr := range s.ToolResults {
				step.ToolResults[j] = taskToolResult{
					ID:         tr.ID,
					Name:       tr.Name,
					Result:     tr.Result,
					Error:      tr.Error,
					DurationMs: tr.DurationMs,
				}
				toolsUsedSet[tr.Name] = struct{}{}
			}
		}

		steps[i] = step
	}

	toolsUsed := make([]string, 0, len(toolsUsedSet))
	for name := range toolsUsedSet {
		toolsUsed = append(toolsUsed, name)
	}

	// Persist messages to store
	if h.server.services.Store != nil {
		_ = h.server.services.Store.Sessions.AddMessage(r.Context(), &store.SessionMessage{
			SessionID: sessionID,
			Role:      "user",
			Content:   req.Message,
			CreatedAt: start,
		})
		if answer != "" {
			_ = h.server.services.Store.Sessions.AddMessage(r.Context(), &store.SessionMessage{
				SessionID: sessionID,
				Role:      "assistant",
				Content:   answer,
				CreatedAt: time.Now().UTC(),
			})
		}
	}

	resp := taskResponse{
		TaskID:      taskID,
		SessionID:   sessionID,
		Status:      status,
		Iterations:  len(observer.Steps),
		Steps:       steps,
		FinalAnswer: answer,
		ToolsUsed:   toolsUsed,
		TotalTokens: taskTokenUsage{
			InputTokens:  session.TotalInputTokens,
			OutputTokens: session.TotalOutputTokens,
		},
		DurationMs: int(duration.Milliseconds()),
		Error:      errMsg,
	}

	slog.Info("task completed",
		"task_id", taskID,
		"session_id", sessionID,
		"status", status,
		"iterations", len(observer.Steps),
		"duration_ms", resp.DurationMs,
	)

	writeJSON(w, http.StatusOK, resp)
}

// resolveSafetyPolicy builds a SafetyPolicy based on the request's safety_tier override.
func (h *taskHandler) resolveSafetyPolicy(cfg *taskConfig) *safety.SafetyPolicy {
	if cfg == nil || cfg.SafetyTier == "" {
		return safety.DefaultPolicy()
	}

	policy := safety.DefaultPolicy()
	switch cfg.SafetyTier {
	case "observe":
		// Most restrictive: disable all T2 and T3
		policy.Record = safety.RecordPolicy{}
		policy.Act = safety.ActPolicy{}
	case "record":
		// Allow T2, disable T3
		policy.Act = safety.ActPolicy{}
	case "act":
		// Permissive — keep defaults (T2 enabled, T3 per default policy)
	}
	return policy
}

func isMaxIterationsError(err error) bool {
	if err == nil {
		return false
	}
	// Agent.Run returns: "max iterations (%d) reached without final response"
	return len(err.Error()) > 15 && err.Error()[:15] == "max iterations "
}

const taskSystemPrompt = `You are Joe, an AI-powered infrastructure copilot running as a task executor on joe-core. You have access to tools that query the infrastructure graph, Kubernetes clusters, cloud providers, observability platforms, and more.

Execute the user's request step by step. Use the available tools to gather information, investigate issues, and provide actionable answers. Be thorough but concise.`

func (s *Server) registerTaskRoutes(mux *http.ServeMux, prefix string) {
	h := &taskHandler{server: s}
	mux.HandleFunc(fmt.Sprintf("POST %s/tasks", prefix), h.handleTask)
}
