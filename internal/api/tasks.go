package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jaimegago/joe/internal/agentctx"
	"github.com/jaimegago/joe/internal/agentloop"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/captaingate"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/llmusage"
	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/prompts"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/safety"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/skills"
	"github.com/jaimegago/joe/internal/tools"
	"github.com/jaimegago/joe/internal/uid"
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
	AllowedZones      []string          `json:"allowed_zones,omitempty"`      // restricts agent to components in these zones
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
	// HistoryTrimmed / MessagesDropped report whether this turn's history
	// pruning dropped any earlier messages (token budget or count backstop)
	// and how many. Additive, optional fields — omitted when nothing was
	// dropped — so the event shape is unchanged for the common no-trim case.
	// The streaming chat UI renders an unobtrusive notice when
	// history_trimmed is true.
	HistoryTrimmed  bool `json:"history_trimmed,omitempty"`
	MessagesDropped int  `json:"messages_dropped,omitempty"`
	// ToolResultsTruncated / UserMessageTruncated report this turn's
	// per-message ingestion truncation: how many oversized tool results were
	// shortened in place, and whether the incoming user message was shortened
	// to fit its share of the context budget. Additive optional fields
	// alongside HistoryTrimmed — omitted in the common no-truncation case.
	// The truncated tool results carry a visible marker inside the rendered
	// result, so only user_message_truncated drives a dedicated UI notice.
	ToolResultsTruncated int  `json:"tool_results_truncated,omitempty"`
	UserMessageTruncated bool `json:"user_message_truncated,omitempty"`
	// ContextWindowTokens is the active provider/model's total context-window
	// capacity in tokens — the denominator for the chat UI's context-utilization
	// badge ("input X of window Y"). It is read from the SAME capabilities
	// registry (llmusage.LookupCapabilities → ModelCapabilities.ContextWindowTokens)
	// and the SAME resolved capacity the per-turn input-token budget is computed
	// against (ComputeInputTokenBudget, see buildTaskRun), so the figure the UI
	// renders is the window history is actually pruned to fit — not a second,
	// separately-derived number. Additive, optional (omitempty), consistent with
	// the other history-pruning fields above. In practice never zero: the registry
	// returns the conservative non-zero default (D-0015(d)) for an unknown model,
	// and the UI renders against that default rather than hiding the figure.
	ContextWindowTokens int `json:"context_window_tokens,omitempty"`
	// StopReason marks a COMPLETED turn that did not end on a naturally
	// tool-call-free answer (Session: loop-budget-exhaustion, decision B).
	// Currently the only value is "max_iterations": the loop exhausted its
	// iteration budget and the answer came from a forced-synthesis call over the
	// evidence already gathered. Additive/optional (omitempty) — absent on a
	// normally-completed turn — and generic so the token-ceiling and overflow
	// paths can adopt sibling values later. The UI renders a truncation notice
	// (distinct from the destructive failure banner) when it is set; the
	// max_iterations_reached STATUS is reserved for the synthesis-FAILURE path.
	StopReason string `json:"stop_reason,omitempty"`
	// ErrorCode is the turn-level write-failure classification: the first
	// per-tool denial code observed across this turn's steps (Item 8). It lets
	// the chat UI surface a specific "why the write failed" message even though
	// a denied tool call does NOT terminate the agentic loop (the LLM receives
	// the tool error and the turn still completes). Empty when no write was
	// denied. See classifyWriteFailure / firstWriteFailureCode.
	ErrorCode string `json:"error_code,omitempty"`
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
	ID     string `json:"id"`
	Name   string `json:"name"`
	Result any    `json:"result"`
	Error  string `json:"error,omitempty"`
	// ErrorCode is the stable write-failure classification for a denied tool
	// call (Item 8): "zone_denial" (RBAC) or "incident_mode" (captain gate).
	// Empty for a success or an unclassified failure. Lets the UI render a
	// specific message instead of the raw error string.
	ErrorCode  string `json:"error_code,omitempty"`
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

	// Owner-scope a continued session before doing any work (§11 Phase 1).
	if !h.sessionAccessAllowed(r.Context(), req.SessionID, string(rbac.PrincipalFromContext(r.Context()))) {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "session not found")
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

	// Resolve max iterations (shared with the streaming task endpoint).
	maxIterations, err := resolveMaxIterations(req.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, err.Error())
		return
	}

	// Build the agent + session (shared with the streaming task endpoint).
	observer := &agentloop.SliceObserver{}
	prepared := h.buildTaskRun(r.Context(), req, maxIterations, observer)
	defer prepared.session.Close()

	// Run with timeout
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	taskID := uid.New()
	// Stream G phase G2: thread the prepared session id and the freshly
	// minted task id into context BEFORE the agentic loop runs so the
	// usage recorder can read them when persisting each per-call row.
	ctx = agentctx.WithSessionID(ctx, prepared.sessionID)
	ctx = agentctx.WithTaskID(ctx, taskID)
	start := time.Now()
	answer, runErr := prepared.agent.Run(ctx, prepared.session, req.Message)
	duration := time.Since(start)

	status, errMsg := taskStatus(ctx, runErr)
	if errors.Is(runErr, llm.ErrContextOverflow) {
		h.writeContextOverflowAudit(ctx, prepared)
	}

	// Persist the raw (un-redacted) conversation, then build the redacted
	// response — matching prior behavior where the store keeps the raw answer.
	h.persistTaskMessages(r.Context(), prepared.sessionID, req.Message, answer, prepared.session.StopReason(), start)
	resp := finalizeTaskResponse(taskID, prepared.sessionID, status, errMsg, answer, observer.Steps, prepared.session, prepared.caps.ContextWindowTokens, duration)

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
	sessionID string
	// caps + model describe the active model the run was built for. They are
	// retained so a terminal context-overflow can be audited with the model and
	// its effective context window (writeContextOverflowAudit) without
	// re-resolving the catalogue after the loop has returned.
	caps  llmusage.ModelCapabilities
	model string
}

// errMaxIterationsTooLow is returned by resolveMaxIterations when a request
// supplies a max_iterations override below 1. Its message is surfaced verbatim
// as the 400 body, matching the pre-consolidation text exactly.
var errMaxIterationsTooLow = errors.New("max_iterations must be >= 1")

// resolveMaxIterations resolves the per-request agentic iteration cap, shared by
// the /tasks and /tasks/stream handlers (Session: loop-budget-exhaustion,
// decision E — one place instead of the two byte-identical copies). It returns
// DefaultMaxIterations when no override is present, the override when it is
// >= 1, and errMaxIterationsTooLow when a present override is below 1 (a client
// error the caller renders as a 400). Per-request override behaviour is
// otherwise unchanged.
func resolveMaxIterations(cfg *taskConfig) (int, error) {
	if cfg == nil || cfg.MaxIterations == nil {
		return agentloop.DefaultMaxIterations, nil
	}
	if *cfg.MaxIterations < 1 {
		return 0, errMaxIterationsTooLow
	}
	return *cfg.MaxIterations, nil
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
	// Ensure an agent_sessions row exists for this turn, owned by the caller
	// (DESIGN-CHAT-SESSIONS.md §11 Phase 1). chat_messages FK to agent_sessions,
	// so the parent row must exist before persistTaskMessages runs. An existing
	// session is left untouched (its owner is not overwritten); cross-owner reuse
	// is refused upstream by the handler's session-access check.
	if h.server.services.SessionModel != nil {
		if existing, _ := h.server.services.SessionModel.GetSession(ctx, sessionID); existing == nil {
			_, _ = h.server.services.SessionModel.CreateSession(ctx, sessionmodel.AgentSession{
				ID:               sessionID,
				Type:             sessionmodel.SessionTypeDefault,
				CreatorPrincipal: string(rbac.PrincipalFromContext(ctx)),
			})
		}
	}

	// Identity Phase E (docs/reference/joe-identity-design.md §3): the loop's tool
	// registry is wired to the in-process accessor-backed client. There is
	// no loopback HTTP self-call and no re-authentication as svc:server;
	// every tool's adapter/graph access reaches the accessor with the real
	// caller principal already carried in the Go context (the one
	// auth.EdgeAuth set via rbac.WithPrincipal at the edge).
	registry := tools.NewCoreRegistry(h.server.inproc, safetyPolicy, h.server.services.WebSearch)
	// D-0018 / D-0022: inject the boot-resolved write floor so the user-task
	// executor denies managed-system Mutates (publish_doc_update_*,
	// github_comment, …) whenever the floor is up
	// (observation or safe mode). This closes the floor-coverage hole D-0022
	// recorded: the floor was injected only on the Core Agent executor
	// (internal/coreagent/agent.go), so a user-task Mutate slipped through the
	// floor here. The floor is a read-only boot-sealed value; the zero value
	// (down) is inert, so this is a no-op when no floor is configured.
	execOpts := []tools.ExecutorOption{
		tools.WithPolicy(safetyPolicy),
		tools.WithWriteFloor(h.server.services.WriteFloor),
	}
	if zoneScope.allowedComponentIDs != nil {
		execOpts = append(execOpts, tools.WithAllowedComponents(zoneScope.allowedComponentIDs))
	}
	if req.Config != nil && len(req.Config.AllowedNamespaces) > 0 {
		execOpts = append(execOpts, tools.WithAllowedNamespaces(req.Config.AllowedNamespaces))
	}
	if zoneScope.zoneNamesStr != "" {
		execOpts = append(execOpts, tools.WithScopeZoneNames(zoneScope.zoneNamesStr))
	}
	if zoneScope.sourceZoneMap != nil {
		execOpts = append(execOpts, tools.WithComponentZoneMap(zoneScope.sourceZoneMap))
	}
	if zoneScope.namespaceZoneMap != nil {
		execOpts = append(execOpts, tools.WithNamespaceZoneMap(zoneScope.namespaceZoneMap))
	}
	executor := tools.NewExecutor(registry, h.server.services.Metrics, execOpts...)

	// Identity Phase G (docs/reference/joe-identity-design.md §0 bug #2, §2.10, §5
	// Invariant 6, D-0010): wrap the loop's tool executor with the
	// SHARED §C captain-session gate. The gate runs UPSTREAM of every
	// per-tool invocation in this loop — i.e. before tools.Executor's
	// safety check AND before the accessor's RBAC check inside the
	// inproc client. In incident regime, a mutating tool call from a
	// non-captain session is refused (no infra call, no accessor call);
	// the gate stays DENY-ONLY, never widens authority. Identical
	// wrapper is installed around the Core Agent's executor in
	// cmd/joe/server.go, so the §C logic exists in EXACTLY ONE
	// place. The wrapper is a drop-in for *tools.Executor (it
	// implements ExecuteBatch + ResultsToMessages); when SessionModel
	// is nil (auth-disabled local/dev), we install no wrapper and the
	// loop behaves exactly as pre-Phase-G.
	var loopExec agentloop.BatchExecutor = executor
	if h.server.services.SessionModel != nil {
		// WithFloor makes the denial precedence floor > incident (D-0022 /
		// D-0019 decision 9) hold by construction on the user-task path too: the
		// wrapper checks the SAME boot-sealed floor the inner executor now
		// carries (above) BEFORE its §C incident gate, so a floored Mutate
		// surfaces the floor reason (observation / safe_mode) rather than an
		// incident-mode redirect. Previously inert here (the executor carried no
		// floor, so the gate would have fired first); now mirrors the Core Agent
		// site in cmd/joe/server.go.
		loopExec = captaingate.New(executor, h.server.services.SessionModel, h.server.services.Audit, captaingate.WithFloor(h.server.services.WriteFloor))
	}

	// Build graph context for system prompt
	systemPrompt := prompts.TaskSystem
	// D-0019: when the boot-resolved write floor is up, tell the model its
	// posture (observation / safe mode) so it declines managed-system writes
	// proactively with articulation, rather than only after the floor denies the
	// tool call at execution. This changes neither the tool surface (no pruning)
	// nor enforcement (the floor still denies every Mutate). Full mode injects
	// nothing. Reads the same boot-sealed value the executor and floor wrapper use.
	if posture := prompts.PostureSection(h.server.services.WriteFloor.Reason()); posture != "" {
		systemPrompt += "\n\n" + posture
	}
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

	// Stream G phase G3a → G4: the production task loop reads its
	// session token ceiling through the storage-backed provider
	// hung off services.SessionLimitsProvider. The provider is
	// constructed once at startup in cmd/joe/server.go and shared
	// across tasks — per-task construction would either need its
	// own repository reference or a settings handle threaded
	// through, neither of which the check site needs. When the
	// services container has no provider wired (test harnesses),
	// fall back to the static backstop so the ceiling is still
	// enforced. services.Audit is threaded through so a ceiling
	// termination writes its KindLLMLimitTriggered row to the same
	// append-only sink the accessor and captaingate use.
	var sessionLimits agentloop.SessionLimits
	if h.server.services.SessionLimitsProvider != nil {
		sessionLimits = h.server.services.SessionLimitsProvider
	} else {
		sessionLimits = agentloop.NewStaticSessionLimits()
	}
	// Resolve the active model's capabilities (context window + default max
	// output) from the compile-time table. The active catalogue key is the
	// live SwappableAdapter's current key (it tracks runtime model swaps);
	// fall back to the configured Current key, then to the conservative
	// default for an unknown / unconfigured model. The capabilities drive
	// both the explicit per-request output cap (set on the agent below) and
	// the token budget the session prunes history to.
	caps := llmusage.LookupCapabilities("", "")
	activeModel := ""
	if cfg := h.server.services.Config; cfg != nil {
		activeKey := cfg.LLM.Current
		if sw, ok := h.server.services.LLM.(*llm.SwappableAdapter); ok {
			if cur := sw.Current(); cur != "" {
				activeKey = cur
			}
		}
		activeModel = activeKey
		if mc, ok := cfg.LLM.Available[activeKey]; ok {
			caps = llmusage.LookupCapabilities(mc.Provider, mc.Model)
		}
	}

	agent := agentloop.NewAgent(
		h.server.services.LLM,
		loopExec,
		registry,
		systemPrompt,
		agentloop.WithObserver(observer),
		agentloop.WithSessionLimits(sessionLimits),
		agentloop.WithAuditRepo(h.server.services.Audit),
		// Stream G context pass: set an explicit output cap on every request
		// the loop builds, so the agentic path never relies on a provider's
		// implicit default (Claude defaulted to 4096; Gemini set none).
		agentloop.WithMaxOutputTokens(caps.MaxOutputTokens),
		// Item 8: classify a denied tool call into a stable write-failure code
		// (incident_mode / zone_denial) so the chat UI can show a specific
		// message instead of the raw error string.
		agentloop.WithToolErrorClassifier(classifyWriteFailure),
	)
	agent.SetMaxIterations(maxIterations)

	metrics := observability.EnsureMetrics(h.server.services.Metrics)
	session := agentloop.NewSession(metrics)

	// Stream G context pass: compute this turn's input token budget —
	// floor(window * fraction) - reserved output - fixed prompt/tool overhead
	// — and prune history to it. The fraction is read live from the
	// storage-backed provider (re-read per request, so an operator change
	// takes effect on the next message without a restart); absent a provider
	// (test harnesses) it falls back to the static backstop. A pathologically
	// small computed budget is clamped to a positive floor so token pruning
	// still engages (the most-recent user message is always kept regardless).
	fraction := agentloop.DefaultContextBudgetFraction
	if h.server.services.ContextBudgetProvider != nil {
		fraction = h.server.services.ContextBudgetProvider.BudgetFraction()
	}
	overhead := agentloop.EstimateOverheadTokens(systemPrompt, registry.ToDefinitions())
	budget := agentloop.ComputeInputTokenBudget(caps.ContextWindowTokens, caps.MaxOutputTokens, overhead, fraction)
	if budget < 1 {
		budget = 1
	}
	session.TokenBudget = budget
	// Count cap stays as the secondary backstop, applied after the token
	// budget (cheap guard against pathological many-tiny-messages cases).
	session.MaxMessages = agentloop.DefaultMaxMessages

	return &preparedTask{agent: agent, session: session, sessionID: sessionID, caps: caps, model: activeModel}
}

// taskStatus maps an agent run error to the wire status + error message.
// Classification is by typed sentinels via errors.Is — never by string
// match — so a reworded error message cannot silently mis-bucket a
// terminal condition. The G3 enforcement phase extends this switch with
// cases for agentloop.ErrSessionTokenCeiling (G3a, here) and
// llmusage.ErrCostLimitExceeded (G3b, later); the structure is shaped
// to accept new errors.Is cases without further refactor.
func taskStatus(ctx context.Context, runErr error) (status, errMsg string) {
	status = "completed"
	if runErr != nil {
		switch {
		case ctx.Err() == context.DeadlineExceeded:
			status = "timeout"
			errMsg = "task timed out"
		case errors.Is(runErr, agentloop.ErrMaxIterations):
			status = "max_iterations_reached"
			errMsg = runErr.Error()
		case errors.Is(runErr, agentloop.ErrSessionTokenCeiling):
			// Stream G phase G3a: the agentic loop terminated this
			// session because its lifetime token total crossed the
			// configured ceiling. Distinct from max_iterations_reached
			// (loop cap, no token check), distinct from timeout
			// (wall-clock), distinct from the generic error bucket
			// (anything not classified above).
			status = "runaway_terminated"
			errMsg = runErr.Error()
		case errors.Is(runErr, llmusage.ErrCostLimitExceeded):
			// Stream G phase G3b: the recorder's pre-call cost-window
			// gate refused this call because accumulated spend over the
			// hourly, daily, or monthly window had reached its limit.
			// No tokens were consumed; no usage row was written. Distinct
			// from runaway_terminated (session lifetime tokens), distinct
			// from max_iterations_reached (loop cap), distinct from
			// timeout (wall-clock), distinct from the generic error
			// bucket.
			status = "cost_limit_exceeded"
			errMsg = runErr.Error()
		case errors.Is(runErr, llm.ErrContextOverflow):
			// Context pass: the provider rejected the request because the
			// prompt/input exceeded the model's context window (an adapter
			// classified the rejection into llm.ErrContextOverflow). Distinct
			// from the generic error bucket. Detection-and-reporting only — no
			// retry, no budget adjustment. The wire error is a friendly
			// message, never the raw provider text.
			status = "context_overflow"
			errMsg = "The conversation or a tool output was too large for the model's context window."
		default:
			status = "error"
			errMsg = runErr.Error()
		}
	}
	return status, errMsg
}

// writeContextOverflowAudit records one KindLLMLimitTriggered audit row for a
// turn refused because its prompt exceeded the model's context window
// (llm.ErrContextOverflow → "context_overflow" terminal status). It mirrors the
// runaway-ceiling writer (internal/agentloop/agent.go writeRunawayAudit): same
// kind, decision "deny", real caller principal, typed context blob.
//
// Best-effort and FAIL-OPEN, matching writeRunawayAudit: the turn has ALREADY
// failed by the time we get here, so an audit-write failure must not compound
// the already-failed turn — we route through audit.FailurePosture with FailOpen
// (loud log, proceed) and discard the error. A nil repository (tests/dev with no
// audit wired) is tolerated and skips the write silently. This closes the parity
// gap flagged in DECISIONS.md D-0015(f).
func (h *taskHandler) writeContextOverflowAudit(ctx context.Context, p *preparedTask) {
	repo := h.server.services.Audit
	if repo == nil {
		return
	}
	// The estimated input total is the same chars/4 heuristic the loop prunes
	// with, summed over the messages still in the session (the Chat call that
	// overflowed left them in place). It is a forensic estimate, not provider
	// accounting — labelled as such in the blob key.
	blob, _ := json.Marshal(map[string]any{
		"session_id":             agentctx.SessionID(ctx),
		"task_id":                agentctx.TaskID(ctx),
		"model":                  p.model,
		"estimated_input_tokens": agentloop.EstimateMessagesTokens(p.session.Messages),
		"context_window_tokens":  p.caps.ContextWindowTokens,
	})
	err := repo.Insert(ctx, audit.Event{
		Principal: string(rbac.PrincipalFromContext(ctx)),
		Action:    audit.ActionLLMContextOverflow,
		Decision:  audit.DecisionDeny,
		Reason:    "context_window_exceeded",
		Kind:      audit.KindLLMLimitTriggered,
		Context:   string(blob),
	})
	// Fail-open-but-loud: pass audit.FailOpen so the loud log names the real
	// outcome (the turn already failed and we PROCEED) and discard the error —
	// surfacing it here would mask the typed terminal error the classifier
	// already returned. Same posture writeRunawayAudit uses.
	_ = audit.FailurePosture(ctx, audit.ActionLLMContextOverflow, err, "api:context_overflow", audit.FailOpen)
}

// persistTaskMessages writes the user message and (if non-empty) the raw
// assistant answer to the session store. The answer is persisted un-redacted;
// response redaction happens separately in finalizeTaskResponse.
// The writes are detached from the request's cancellation (same pattern as
// generateTitleAsync): a client that disconnects mid-stream cancels r.Context(),
// and losing the turn transcript because the viewer closed a tab would be a
// silent data loss. Principal/session context values are preserved.
func (h *taskHandler) persistTaskMessages(ctx context.Context, sessionID, userMsg, answer, stopReason string, start time.Time) {
	if h.server.services.SessionModel == nil {
		return
	}
	ctx = context.WithoutCancel(ctx)
	if _, err := h.server.services.SessionModel.AddChatMessage(ctx, sessionmodel.ChatMessage{
		ID:        uid.New(),
		SessionID: sessionID,
		Role:      "user",
		Content:   userMsg,
		CreatedAt: start,
	}); err != nil {
		slog.Warn("persist task transcript: user message write failed", "session_id", sessionID, "error", err)
	}
	// Title a freshly-started session from its opening message (heuristic now,
	// async LLM upgrade after). A no-op once the session already has a title.
	h.maybeAutoTitle(ctx, sessionID, userMsg)
	if answer != "" {
		if _, err := h.server.services.SessionModel.AddChatMessage(ctx, sessionmodel.ChatMessage{
			ID:         uid.New(),
			SessionID:  sessionID,
			Role:       "assistant",
			Content:    answer,
			StopReason: stopReason,
			CreatedAt:  time.Now().UTC(),
		}); err != nil {
			slog.Warn("persist task transcript: assistant message write failed", "session_id", sessionID, "error", err)
		}
	}
}

// sessionAccessAllowed reports whether the caller may use sessionID for a task
// turn. A not-yet-existing session is allowed — buildTaskRun creates it owned by
// the caller. An existing session owned by a different principal is refused:
// this closes the cross-user leak on the task path, where seedHistory would
// otherwise load another user's prior messages into the model context
// (DESIGN-CHAT-SESSIONS.md §10 — send/continue is owner-only). A store error
// fails CLOSED — we cannot prove ownership, so we must not risk continuing
// another user's session. When SessionModel is unwired (auth-disabled/dev
// harnesses) there is no persistence and no owner to honor, so the check passes.
func (h *taskHandler) sessionAccessAllowed(ctx context.Context, sessionID, principal string) bool {
	if h.server.services.SessionModel == nil || sessionID == "" {
		return true
	}
	sess, err := h.server.services.SessionModel.GetSession(ctx, sessionID)
	if err != nil {
		return false
	}
	if sess == nil {
		// Not-yet-existing session: allowed, buildTaskRun creates it caller-owned.
		return true
	}
	return sess.CreatorPrincipal == principal
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
				ErrorCode:  tr.ErrorCode,
				DurationMs: tr.DurationMs,
			}
		}
	}
	return step
}

// finalizeTaskResponse builds the wire response from the collected steps,
// deriving tools-used and applying defense-in-depth secret redaction to the
// final answer (the redaction operates on the response copy only).
func finalizeTaskResponse(taskID, sessionID, status, errMsg, answer string, steps []agentloop.StepRecord, session *agentloop.Session, contextWindowTokens int, duration time.Duration) taskResponse {
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
		ContextWindowTokens:  contextWindowTokens,
		DurationMs:           int(duration.Milliseconds()),
		Error:                errMsg,
		HistoryTrimmed:       session.HistoryTrimmed(),
		MessagesDropped:      session.MessagesDropped(),
		ToolResultsTruncated: session.ToolResultsTruncated(),
		UserMessageTruncated: session.UserMessageTruncated(),
		StopReason:           session.StopReason(),
		ErrorCode:            firstWriteFailureCode(outSteps),
	}
}

// firstWriteFailureCode returns the first non-empty per-tool write-failure
// code across the turn's steps, or "" if no tool call was denied. A denied
// write does not terminate the agentic loop — the LLM receives the tool error
// and the turn still completes — so this turn-level summary is how the chat UI
// learns a write was refused and why, without scanning every step itself.
func firstWriteFailureCode(steps []taskStep) string {
	for _, s := range steps {
		for _, tr := range s.ToolResults {
			if tr.ErrorCode != "" {
				return tr.ErrorCode
			}
		}
	}
	return ""
}

// seedHistory loads prior user/assistant turns for sessionID from the store
// into the in-memory session, giving the streaming endpoint multi-turn
// continuity (the non-streaming /tasks endpoint does not seed, preserving its
// single-turn contract).
func (h *taskHandler) seedHistory(ctx context.Context, session *agentloop.Session, sessionID string) {
	if h.server.services.SessionModel == nil {
		return
	}
	msgs, err := h.server.services.SessionModel.ListChatMessages(ctx, sessionID)
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
	allowedComponentIDs []string          // components in authorized zones
	zoneNamesStr        string            // human-readable authorized zone names
	scopeDesc           string            // system prompt zone scope section
	sourceZoneMap       map[string]string // all component_id → zone name (for executor violation messages)
	namespaceZoneMap    map[string]string // all namespace → zone name (for executor violation messages)
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
	otherZoneComponents := make(map[string][]string) // zone label → source IDs
	for _, a := range assignments {
		zoneName := zoneIDToName[a.ZoneID]
		sourceZoneMap[a.ComponentID] = zoneName
		if _, ok := allowedZoneSet[a.ZoneID]; ok {
			allowed = append(allowed, a.ComponentID)
		} else {
			otherZoneComponents[zoneName] = append(otherZoneComponents[zoneName], a.ComponentID)
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
		ZoneNamesStr:        zoneNamesStr,
		AllowedComponentIDs: allowed,
		AllowedNamespaces:   cfg.AllowedNamespaces,
		OtherZoneComponents: otherZoneComponents,
		NamespaceZoneMap:    namespaceZoneMap,
	})

	return zoneScopeResult{
		allowedComponentIDs: allowed,
		zoneNamesStr:        zoneNamesStr,
		scopeDesc:           scopeDesc,
		sourceZoneMap:       sourceZoneMap,
		namespaceZoneMap:    namespaceZoneMap,
	}
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
	h := &taskHandler{server: s}
	mux.HandleFunc(fmt.Sprintf("POST %s/tasks", prefix), h.handleTask)
	// Phase 2: streamed agentic turn for the thin CLI (SSE). The non-streaming
	// /tasks endpoint above is unchanged so oasisctl keeps working.
	mux.HandleFunc(fmt.Sprintf("POST %s/tasks/stream", prefix), h.handleTaskStream)
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
