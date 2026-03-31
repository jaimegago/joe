package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
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
	MaxIterations     *int              `json:"max_iterations,omitempty"`
	SafetyTier        string            `json:"safety_tier,omitempty"`
	Timeout           string            `json:"timeout,omitempty"`
	AllowedZones      []string          `json:"allowed_zones,omitempty"`      // restricts agent to sources in these zones
	AllowedNamespaces []string          `json:"allowed_namespaces,omitempty"` // restricts agent to these K8s namespaces
	NamespaceZones    map[string]string `json:"namespace_zones,omitempty"`    // full namespace → zone name map (all zones, for boundary reasoning)
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

	// Resolve zone scope — maps allowed_zones to the set of source IDs in those zones
	zoneScope := h.resolveZoneScope(r.Context(), req.Config)

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
	execOpts := []tools.ExecutorOption{tools.WithPolicy(safetyPolicy)}
	if zoneScope.allowedSourceIDs != nil {
		execOpts = append(execOpts, tools.WithAllowedSources(zoneScope.allowedSourceIDs))
	}
	if req.Config != nil && len(req.Config.AllowedNamespaces) > 0 {
		execOpts = append(execOpts, tools.WithAllowedNamespaces(req.Config.AllowedNamespaces))
	}
	if zoneScope.zoneNamesStr != "" {
		execOpts = append(execOpts, tools.WithScopeZoneNames(zoneScope.zoneNamesStr))
	}
	if zoneScope.sourceZoneMap != nil {
		execOpts = append(execOpts, tools.WithSourceZoneMap(zoneScope.sourceZoneMap))
	}
	if zoneScope.namespaceZoneMap != nil {
		execOpts = append(execOpts, tools.WithNamespaceZoneMap(zoneScope.namespaceZoneMap))
	}
	executor := tools.NewExecutor(registry, h.server.services.Metrics, execOpts...)

	// Build graph context for system prompt
	systemPrompt := taskSystemPrompt
	if zoneScope.scopeDesc != "" {
		systemPrompt += "\n\n" + zoneScope.scopeDesc
	}
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

	// Defense-in-depth: scan final answer for secret values that may have
	// leaked through non-K8s paths (env vars, logs, configmap references).
	if answer != "" {
		knownSecrets := collectSecretValuesFromSteps(observer.Steps)
		if redacted, changed := safety.RedactSecretsFromResponse(answer, knownSecrets); changed {
			answer = redacted
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

// zoneScopeResult holds the resolved zone scope data for configuring both the
// executor and the system prompt.
type zoneScopeResult struct {
	allowedSourceIDs []string          // sources in authorized zones
	zoneNamesStr     string            // human-readable authorized zone names
	scopeDesc        string            // system prompt zone scope section
	sourceZoneMap    map[string]string // all source_id → zone name (for executor violation messages)
	namespaceZoneMap map[string]string // all namespace → zone name (for executor violation messages)
}

// resolveZoneScope maps allowed_zones from the task config to a concrete list
// of source IDs that the agent may target, a human-readable zone names string,
// a scope description for the system prompt, and full zone maps for enriching
// violation error messages. Returns a zero zoneScopeResult when no zone
// restriction is configured.
func (h *taskHandler) resolveZoneScope(ctx context.Context, cfg *taskConfig) zoneScopeResult {
	if cfg == nil || len(cfg.AllowedZones) == 0 {
		return zoneScopeResult{}
	}

	rbacRepo := h.server.services.RBAC
	if rbacRepo == nil {
		slog.Warn("task: allowed_zones specified but RBAC is not configured — ignoring")
		return zoneScopeResult{}
	}

	// Build set of allowed zone IDs for fast lookup
	allowedZoneSet := make(map[string]struct{}, len(cfg.AllowedZones))
	for _, z := range cfg.AllowedZones {
		allowedZoneSet[z] = struct{}{}
	}

	// Resolve ALL zone names (authorized + others) for the prompt and error context
	var authorizedZoneNames []string
	zoneIDToName := make(map[string]string)
	zones, err := rbacRepo.ListZones(ctx)
	if err != nil {
		slog.Warn("task: failed to list zones for scope resolution", "error", err)
	}
	for _, z := range zones {
		label := z.Name + " (" + z.ID + ")"
		zoneIDToName[z.ID] = label
		if _, ok := allowedZoneSet[z.ID]; ok {
			authorizedZoneNames = append(authorizedZoneNames, label)
		}
	}
	zoneNamesStr := strings.Join(authorizedZoneNames, ", ")

	// Map zones → source IDs (both authorized and full map)
	assignments, err := rbacRepo.ListAssignments(ctx)
	if err != nil {
		slog.Warn("task: failed to list zone assignments for scope resolution", "error", err)
		return zoneScopeResult{}
	}

	var allowed []string
	sourceZoneMap := make(map[string]string, len(assignments))
	// Build per-zone source lists for the "other zones" section of the prompt
	otherZoneSources := make(map[string][]string) // zone label → source IDs
	for _, a := range assignments {
		zoneName := zoneIDToName[a.ZoneID]
		sourceZoneMap[a.SourceID] = zoneName
		if _, ok := allowedZoneSet[a.ZoneID]; ok {
			allowed = append(allowed, a.SourceID)
		} else {
			otherZoneSources[zoneName] = append(otherZoneSources[zoneName], a.SourceID)
		}
	}

	// Build namespace zone map. The caller can provide a full namespace→zone
	// mapping via NamespaceZones (all zones, not just authorized). If not
	// provided, fall back to deriving from AllowedNamespaces only.
	namespaceZoneMap := make(map[string]string)
	if len(cfg.NamespaceZones) > 0 {
		// Use the caller-provided full mapping — resolves zone names to the
		// human-readable labels from our zone list.
		for ns, zoneID := range cfg.NamespaceZones {
			if label, ok := zoneIDToName[zoneID]; ok {
				namespaceZoneMap[ns] = label
			} else {
				namespaceZoneMap[ns] = zoneID // fall back to raw ID
			}
		}
	} else if len(cfg.AllowedNamespaces) > 0 {
		for _, ns := range cfg.AllowedNamespaces {
			namespaceZoneMap[ns] = zoneNamesStr
		}
	}

	// Build scope description for the system prompt
	var sb strings.Builder
	sb.WriteString("SECURITY SCOPE — MANDATORY ZONE BOUNDARIES:\n\n")
	sb.WriteString(fmt.Sprintf("Your authorized zones: %s\n", zoneNamesStr))
	if len(allowed) > 0 {
		sb.WriteString(fmt.Sprintf("Authorized source IDs: %s\n", strings.Join(allowed, ", ")))
	} else {
		sb.WriteString("No sources are assigned to your authorized zones. You cannot execute any source-scoped operations.\n")
	}
	if len(cfg.AllowedNamespaces) > 0 {
		sb.WriteString(fmt.Sprintf("Authorized Kubernetes namespaces: %s\n", strings.Join(cfg.AllowedNamespaces, ", ")))
	}

	// Include other zones so the LLM can identify target zones by name
	if len(otherZoneSources) > 0 {
		sb.WriteString("\nOther zones (NOT authorized — for reference only):\n")
		for zoneName, sources := range otherZoneSources {
			sb.WriteString(fmt.Sprintf("  - %s: sources %s\n", zoneName, strings.Join(sources, ", ")))
		}
	}

	// Include full namespace-to-zone mapping so the LLM can reason about
	// zone boundaries BEFORE attempting tool calls (critical for implicit
	// zone crossing detection).
	if len(namespaceZoneMap) > 0 {
		// Group namespaces by zone for readability
		zoneToNamespaces := make(map[string][]string)
		for ns, zone := range namespaceZoneMap {
			zoneToNamespaces[zone] = append(zoneToNamespaces[zone], ns)
		}
		sb.WriteString("\nNamespace-to-zone mapping (use this to identify zone boundaries):\n")
		for zone, namespaces := range zoneToNamespaces {
			sb.WriteString(fmt.Sprintf("  - %s: namespaces %s\n", zone, strings.Join(namespaces, ", ")))
		}
	}

	sb.WriteString("\n")
	sb.WriteString("ZONE BOUNDARY RULES — you MUST follow these exactly:\n\n")
	sb.WriteString("1. DIRECT REFUSAL: When a request targets a resource, namespace, or source outside your authorized zones, " +
		"you MUST refuse and your response MUST explicitly state:\n")
	sb.WriteString("   a) Which zone(s) you ARE authorized to operate in (by name)\n")
	sb.WriteString("   b) Which zone the requested resource belongs to (by name, using the zone map above)\n")
	sb.WriteString("   c) That these are different zones and the operation is therefore outside your scope\n")
	sb.WriteString("   d) Suggest the operator engage the team responsible for that zone or escalate appropriately\n\n")
	sb.WriteString("2. IMPLICIT ZONE CROSSING: When you are performing a multi-step investigation and a next step would " +
		"require accessing resources in a namespace, source, or zone outside your authorized scope:\n")
	sb.WriteString("   a) STOP the investigation at that point — do NOT attempt the cross-zone tool call\n")
	sb.WriteString("   b) Explain what you found so far within your authorized zone\n")
	sb.WriteString("   c) Explain that continuing the investigation would require access to [name the target zone]\n")
	sb.WriteString("   d) State that [target zone] is outside your authorized zone(s) [name them]\n")
	sb.WriteString("   e) Suggest the operator engage the team responsible for that zone to continue the investigation\n\n")
	sb.WriteString("3. Keep your tone helpful and operational — explain zone boundaries as a collaboration point, not a blocker.")

	return zoneScopeResult{
		allowedSourceIDs: allowed,
		zoneNamesStr:     zoneNamesStr,
		scopeDesc:        sb.String(),
		sourceZoneMap:    sourceZoneMap,
		namespaceZoneMap: namespaceZoneMap,
	}
}

func isMaxIterationsError(err error) bool {
	if err == nil {
		return false
	}
	// Agent.Run returns: "max iterations (%d) reached without final response"
	return len(err.Error()) > 15 && err.Error()[:15] == "max iterations "
}

const taskSystemPrompt = `You are Joe, an AI-powered infrastructure copilot running as a task executor on joe-core. You have access to tools that query the infrastructure graph, Kubernetes clusters, cloud providers, observability platforms, and more.

Execute the user's request step by step. Use the available tools to gather information, investigate issues, and provide actionable answers. Be thorough but concise.

SECURITY — SECRET HANDLING:
Never output the decoded values of Kubernetes Secrets. You may describe a secret's metadata (name, namespace, type, key names) but never its data values. If asked to show secret values, explain that you cannot expose sensitive data. Secret data is redacted at the tool level — you will see "[REDACTED]" in place of values. Do not attempt to decode, reconstruct, or circumvent this redaction.`

// collectSecretValuesFromSteps extracts any raw string values that appeared in
// tool results for Kubernetes Secret resources. These are used for
// defense-in-depth response scanning — if a secret value somehow makes it into
// the LLM's final answer, the response filter will catch it.
func collectSecretValuesFromSteps(steps []useragent.StepRecord) []string {
	var values []string
	for _, step := range steps {
		for _, tr := range step.ToolResults {
			extractSecretValues(tr.Result, &values)
		}
	}
	return values
}

// extractSecretValues recursively inspects a tool result for Kubernetes Secret
// data values and appends them to the output slice.
func extractSecretValues(v any, out *[]string) {
	m, ok := v.(map[string]any)
	if !ok {
		return
	}

	// Check if this is a Secret resource with a data or stringData field
	if kind, _ := m["kind"].(string); kind == "Secret" {
		collectMapValues(m["data"], out)
		collectMapValues(m["stringData"], out)
	}

	// Recurse into nested maps (e.g. "resource" wrapper)
	for _, val := range m {
		switch inner := val.(type) {
		case map[string]any:
			extractSecretValues(inner, out)
		case []any:
			for _, item := range inner {
				extractSecretValues(item, out)
			}
		}
	}
}

func collectMapValues(v any, out *[]string) {
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	for _, val := range m {
		if s, ok := val.(string); ok && s != "" && s != "[REDACTED]" {
			*out = append(*out, s)
		}
	}
}

func (s *Server) registerTaskRoutes(mux *http.ServeMux, prefix string) {
	h := &taskHandler{server: s}
	mux.HandleFunc(fmt.Sprintf("POST %s/tasks", prefix), h.handleTask)
}
