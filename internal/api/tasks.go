package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jaimegago/joe/internal/agentloop"
	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/prompts"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/safety"
	"github.com/jaimegago/joe/internal/skills"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/internal/tools"
	"github.com/jaimegago/joe/internal/uid"
)

// taskHandler handles POST /api/v1/tasks — full agentic loop over HTTP.
type taskHandler struct {
	server   *Server
	inflight *inflightTasks // tracks streamed turns awaiting delegated tool results
}

// --- Request / Response types ---

type taskRequest struct {
	Message   string      `json:"message"`
	SessionID string      `json:"session_id,omitempty"`
	Config    *taskConfig `json:"config,omitempty"`
	// ClientTools are local tools the caller (the thin CLI) can execute on its
	// own machine. They are registered as delegating stubs so the LLM can call
	// them; execution is sent back over the stream. Ignored by /tasks.
	ClientTools []clientToolDef `json:"client_tools,omitempty"`
}

// clientToolDef advertises a local tool the streaming client can service.
type clientToolDef struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Parameters  llm.ParameterSchema `json:"parameters"`
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
	maxIterations := agentloop.DefaultMaxIterations
	if req.Config != nil && req.Config.MaxIterations != nil {
		if *req.Config.MaxIterations < 1 {
			writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "max_iterations must be >= 1")
			return
		}
		maxIterations = *req.Config.MaxIterations
	}

	// Build the agent + session (shared with the streaming task endpoint).
	observer := &agentloop.SliceObserver{}
	prepared := h.buildTaskRun(r.Context(), req, maxIterations, observer)
	defer prepared.session.Close()

	// Run with timeout
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	taskID := uid.New()
	start := time.Now()
	answer, runErr := prepared.agent.Run(ctx, prepared.session, req.Message)
	duration := time.Since(start)

	status, errMsg := taskStatus(ctx, runErr)

	// Persist the raw (un-redacted) conversation, then build the redacted
	// response — matching prior behavior where the store keeps the raw answer.
	h.persistTaskMessages(r.Context(), prepared.sessionID, req.Message, answer, start)
	resp := finalizeTaskResponse(taskID, prepared.sessionID, status, errMsg, answer, observer.Steps, prepared.session, duration)

	slog.Info("task completed",
		"task_id", taskID,
		"session_id", prepared.sessionID,
		"status", status,
		"iterations", resp.Iterations,
		"duration_ms", resp.DurationMs,
	)

	slog.Info("task response",
		"task_id", taskID,
		"response", resp.FinalAnswer,
	)

	writeJSON(w, http.StatusOK, resp)
}

// preparedTask bundles the constructed agent and session for one task run. It
// is shared by the non-streaming /tasks handler and the streaming
// /tasks/stream handler so both build the runtime identically.
type preparedTask struct {
	agent     *agentloop.Agent
	session   *agentloop.Session
	registry  *tools.Registry
	sessionID string
}

// buildTaskRun constructs the core-tool agent + session for a task request.
// The observer receives per-iteration step notifications. Construction is
// infallible: all request-level validation (message, timeout, max_iterations)
// is the caller's responsibility before invoking this.
func (h *taskHandler) buildTaskRun(ctx context.Context, req taskRequest, maxIterations int, observer agentloop.RunObserver) *preparedTask {
	safetyPolicy := h.resolveSafetyPolicy(req.Config)
	zoneScope := h.resolveZoneScope(ctx, req.Config)

	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = uid.New()
	}
	if h.server.services.Store != nil {
		sess, _ := h.server.services.Store.Sessions.Get(ctx, sessionID)
		if sess == nil {
			_ = h.server.services.Store.Sessions.Create(ctx, &store.Session{
				ID:        sessionID,
				StartedAt: time.Now().UTC(),
			})
		}
	}

	// Loopback client so core tools reach joe-core's own HTTP API.
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
	systemPrompt := prompts.TaskSystem
	if zoneScope.scopeDesc != "" {
		systemPrompt += "\n\n" + zoneScope.scopeDesc
	}
	if h.server.accessor.GraphAvailable() {
		if summary, err := h.server.accessor.GraphSummary(ctx, rbac.PrincipalFromContext(ctx)); err == nil {
			systemPrompt += fmt.Sprintf(
				"\n\nCurrent infrastructure context:\nInfrastructure graph: %d nodes, %d edges. Node types: %v",
				summary.NodeCount, summary.EdgeCount, summary.NodesByType,
			)
		}
	}
	if section := renderSkillsForQuery(h.server.services.Skills.Snapshot(), req.Message); section != "" {
		systemPrompt += "\n\n" + section
	}

	agent := agentloop.NewAgent(
		h.server.services.LLM,
		executor,
		registry,
		systemPrompt,
		agentloop.WithObserver(observer),
	)
	agent.SetMaxIterations(maxIterations)

	metrics := observability.EnsureMetrics(h.server.services.Metrics)
	session := agentloop.NewSession(metrics)
	session.MaxMessages = agentloop.DefaultMaxMessages

	return &preparedTask{agent: agent, session: session, registry: registry, sessionID: sessionID}
}

// taskStatus maps an agent run error to the wire status + error message.
func taskStatus(ctx context.Context, runErr error) (status, errMsg string) {
	status = "completed"
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
	return status, errMsg
}

// persistTaskMessages writes the user message and (if non-empty) the raw
// assistant answer to the session store. The answer is persisted un-redacted;
// response redaction happens separately in finalizeTaskResponse.
func (h *taskHandler) persistTaskMessages(ctx context.Context, sessionID, userMsg, answer string, start time.Time) {
	if h.server.services.Store == nil {
		return
	}
	_ = h.server.services.Store.Sessions.AddMessage(ctx, &store.SessionMessage{
		SessionID: sessionID,
		Role:      "user",
		Content:   userMsg,
		CreatedAt: start,
	})
	if answer != "" {
		_ = h.server.services.Store.Sessions.AddMessage(ctx, &store.SessionMessage{
			SessionID: sessionID,
			Role:      "assistant",
			Content:   answer,
			CreatedAt: time.Now().UTC(),
		})
	}
}

// taskStepFromRecord maps an agent loop StepRecord to the wire taskStep shape.
func taskStepFromRecord(s agentloop.StepRecord) taskStep {
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
	if len(s.LLMResponse.ToolCalls) > 0 {
		step.LLMResponse.ToolCalls = make([]taskToolCall, len(s.LLMResponse.ToolCalls))
		for j, tc := range s.LLMResponse.ToolCalls {
			step.LLMResponse.ToolCalls[j] = taskToolCall{ID: tc.ID, Name: tc.Name, Args: tc.Args}
		}
	}
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
		}
	}
	return step
}

// finalizeTaskResponse builds the wire response from the collected steps,
// deriving tools-used and applying defense-in-depth secret redaction to the
// final answer (the redaction operates on the response copy only).
func finalizeTaskResponse(taskID, sessionID, status, errMsg, answer string, steps []agentloop.StepRecord, session *agentloop.Session, duration time.Duration) taskResponse {
	outSteps := make([]taskStep, len(steps))
	toolsUsedSet := map[string]struct{}{}
	for i, s := range steps {
		outSteps[i] = taskStepFromRecord(s)
		for _, tr := range outSteps[i].ToolResults {
			toolsUsedSet[tr.Name] = struct{}{}
		}
	}
	toolsUsed := make([]string, 0, len(toolsUsedSet))
	for name := range toolsUsedSet {
		toolsUsed = append(toolsUsed, name)
	}

	// Defense-in-depth: scan final answer for secret values that may have
	// leaked through non-K8s paths (env vars, logs, configmap references).
	if answer != "" {
		knownSecrets := collectSecretValuesFromSteps(steps)
		if redacted, changed := safety.RedactSecretsFromResponse(answer, knownSecrets); changed {
			answer = redacted
		}
	}

	return taskResponse{
		TaskID:      taskID,
		SessionID:   sessionID,
		Status:      status,
		Iterations:  len(steps),
		Steps:       outSteps,
		FinalAnswer: answer,
		ToolsUsed:   toolsUsed,
		TotalTokens: taskTokenUsage{
			InputTokens:  session.TotalInputTokens,
			OutputTokens: session.TotalOutputTokens,
		},
		DurationMs: int(duration.Milliseconds()),
		Error:      errMsg,
	}
}

// seedHistory loads prior user/assistant turns for sessionID from the store
// into the in-memory session, giving the streaming endpoint multi-turn
// continuity (the non-streaming /tasks endpoint does not seed, preserving its
// single-turn contract).
func (h *taskHandler) seedHistory(ctx context.Context, session *agentloop.Session, sessionID string) {
	if h.server.services.Store == nil {
		return
	}
	msgs, err := h.server.services.Store.Sessions.GetMessages(ctx, sessionID)
	if err != nil || len(msgs) == 0 {
		return
	}
	seeded := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == "user" || m.Role == "assistant" {
			seeded = append(seeded, llm.Message{Role: m.Role, Content: m.Content})
		}
	}
	session.AddMessages(ctx, seeded)
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
	scopeDesc := prompts.BuildZoneScopePrompt(prompts.ZoneScopeParams{
		ZoneNamesStr:      zoneNamesStr,
		AllowedSourceIDs:  allowed,
		AllowedNamespaces: cfg.AllowedNamespaces,
		OtherZoneSources:  otherZoneSources,
		NamespaceZoneMap:  namespaceZoneMap,
	})

	return zoneScopeResult{
		allowedSourceIDs: allowed,
		zoneNamesStr:     zoneNamesStr,
		scopeDesc:        scopeDesc,
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

// collectSecretValuesFromSteps extracts any raw string values that appeared in
// tool results for Kubernetes Secret resources. These are used for
// defense-in-depth response scanning — if a secret value somehow makes it into
// the LLM's final answer, the response filter will catch it.
func collectSecretValuesFromSteps(steps []agentloop.StepRecord) []string {
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
	h := &taskHandler{server: s, inflight: newInflightTasks()}
	mux.HandleFunc(fmt.Sprintf("POST %s/tasks", prefix), h.handleTask)
	// Phase 2: streamed agentic turn for the thin CLI (SSE). The non-streaming
	// /tasks endpoint above is unchanged so oasisctl keeps working.
	mux.HandleFunc(fmt.Sprintf("POST %s/tasks/stream", prefix), h.handleTaskStream)
	// Phase 2: callback for delegated local-tool results (CLI → joe-core).
	mux.HandleFunc(fmt.Sprintf("POST %s/tasks/stream/{taskID}/tool-results", prefix), h.handleToolResult)
}

// renderSkillsForQuery routes the user query through the skill registry and
// renders matched skills as a system-prompt addition. Returns "" when the
// router is unconfigured or nothing matched, so callers can append
// unconditionally. Selected skill names are logged for auditability.
func renderSkillsForQuery(router *skills.Router, query string) string {
	if router == nil {
		return ""
	}
	matched := router.Match(query)
	if len(matched) == 0 {
		return ""
	}
	names := make([]string, len(matched))
	for i, s := range matched {
		names[i] = s.Name
	}
	slog.Info("skills activated", "skills", names)
	return skills.RenderPromptSection(matched)
}
