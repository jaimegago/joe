package agentloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jaimegago/joe/internal/agentctx"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/prompts"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/tools"
)

// AdapterFactory creates a new LLM adapter for the given provider and model.
// Used by SwitchModel to hot-swap the underlying LLM without restarting.
type AdapterFactory func(ctx context.Context, provider, model string) (llm.LLMAdapter, error)

// AgentOption configures optional Agent settings.
type AgentOption func(*Agent)

// WithAdapterFactory sets the adapter factory for hot-swapping models.
func WithAdapterFactory(f AdapterFactory) AgentOption {
	return func(a *Agent) { a.adapterFactory = f }
}

// WithCurrentModelName sets the display name of the active model.
func WithCurrentModelName(name string) AgentOption {
	return func(a *Agent) { a.currentModel = name }
}

// WithSessionLimits overrides the SessionLimits provider used by the
// loop's session token ceiling check (Stream G phase G3a). When omitted,
// NewAgent installs StaticSessionLimits — the safe default — so callers
// that never supply a provider still get the hardcoded backstop. Passing
// nil through this option is treated as "use the default" and resets the
// provider to StaticSessionLimits at the end of construction.
func WithSessionLimits(p SessionLimits) AgentOption {
	return func(a *Agent) { a.sessionLimits = p }
}

// WithMaxOutputTokens sets the explicit per-request output cap the loop
// stamps on every ChatRequest.MaxTokens it builds (Stream G context pass).
// buildTaskRun resolves it from the active model's capabilities table entry
// so the agentic path never relies on a provider's implicit default (the
// Claude adapter defaulted to 4096; the Gemini adapter set no limit at all).
// A value <= 0 leaves MaxTokens unset, preserving prior behaviour for
// callers that don't supply it.
func WithMaxOutputTokens(n int) AgentOption {
	return func(a *Agent) { a.maxOutputTokens = n }
}

// WithToolErrorClassifier installs a function that maps a per-tool execution
// error to a stable, machine-readable code recorded on the step's
// ToolResultRecord (Item 8 / differentiated write-failure feedback). The
// classifier runs on the TYPED error — before it is stringified for the wire
// — so it can use errors.As / errors.Is to distinguish, e.g., a captain-gate
// incident-mode refusal from an RBAC zone denial. Keeping the classifier
// injected (rather than importing the gate/RBAC error types here) preserves
// the loop's generality: agentloop stays unaware of captaingate / access. A
// nil classifier (the default) records no code.
func WithToolErrorClassifier(fn func(error) string) AgentOption {
	return func(a *Agent) { a.toolErrorClassifier = fn }
}

// WithAuditRepo wires the append-only audit.Repository used by the loop
// when a terminal limit fires (Stream G phase G3a: runaway termination
// writes one KindLLMLimitTriggered row). When omitted (or nil), the
// audit write is skipped — termination still happens — following the
// same nil-tolerant posture the captaingate package already uses for
// its refusal-audit path. Production wiring in api/tasks.go passes
// services.Audit explicitly.
func WithAuditRepo(r audit.Repository) AgentOption {
	return func(a *Agent) { a.auditRepo = r }
}

// BatchExecutor is the minimal surface the loop calls on its executor.
// Phase G change (docs/reference/joe-identity-design.md §0 bug #2 / §2.10): the
// loop's executor used to be a concrete *tools.Executor, but the
// captain-session gate fix requires wrapping it so the §C gate runs
// upstream of the per-tool call. Both *tools.Executor and the Phase G
// shared *captaingate.Wrapper satisfy this interface, so the loop is
// agnostic to whether the gate is installed. api/tasks.go's
// buildTaskRun composes the gate around the executor; tests that don't
// care about the gate pass *tools.Executor directly.
type BatchExecutor interface {
	ExecuteBatch(ctx context.Context, calls []tools.ToolCallRequest) ([]tools.ToolCallResult, error)
	ResultsToMessages(results []tools.ToolCallResult) []llm.Message
}

// Agent runs the agentic loop: LLM → tool calls → LLM → ...
type Agent struct {
	mu             sync.RWMutex // protects llm and currentModel
	llm            llm.LLMAdapter
	executor       BatchExecutor
	registry       *tools.Registry
	systemPrompt   llm.SystemPrompt
	maxIterations  int
	adapterFactory AdapterFactory // optional, for hot-swap
	currentModel   string         // display name of active model
	observer       RunObserver    // optional, for step-by-step observation

	// Stream G phase G3a fields.
	//
	// sessionLimits is the provider for the session-lifetime token ceiling
	// enforced after each Chat call. NewAgent installs StaticSessionLimits
	// when no WithSessionLimits option is supplied, so callers that
	// haven't been updated still get the safe default.
	//
	// auditRepo is the append-only audit sink the loop writes one
	// KindLLMLimitTriggered row to when a terminal limit fires. nil is
	// tolerated — termination still occurs — matching captaingate's
	// best-effort posture for refusal audit when no repo is wired.
	sessionLimits SessionLimits
	auditRepo     audit.Repository

	// maxOutputTokens is the explicit per-request output cap stamped on
	// ChatRequest.MaxTokens. Zero leaves MaxTokens unset (provider default).
	maxOutputTokens int

	// toolErrorClassifier maps a typed per-tool error to a stable code
	// (e.g. "incident_mode", "zone_denial") recorded on ToolResultRecord.
	// nil (default) records no code. Installed via WithToolErrorClassifier
	// by the api layer, which owns the gate/RBAC error vocabulary.
	toolErrorClassifier func(error) string
}

// NewAgent creates a new agent. Options are applied after defaults.
func NewAgent(llmAdapter llm.LLMAdapter, executor BatchExecutor, registry *tools.Registry, systemPrompt llm.SystemPrompt, opts ...AgentOption) *Agent {
	a := &Agent{
		llm:           llmAdapter,
		executor:      executor,
		registry:      registry,
		systemPrompt:  systemPrompt,
		maxIterations: DefaultMaxIterations,
		sessionLimits: NewStaticSessionLimits(),
	}
	for _, opt := range opts {
		opt(a)
	}
	// A caller that passed WithSessionLimits(nil) explicitly should
	// still get the safe default — the session token ceiling is a
	// safety backstop and we never want it silently disabled by an
	// accidentally-nil option argument.
	if a.sessionLimits == nil {
		a.sessionLimits = NewStaticSessionLimits()
	}
	return a
}

// SwitchModel hot-swaps the LLM adapter to a different provider/model.
// Requires an AdapterFactory to have been set via WithAdapterFactory.
func (a *Agent) SwitchModel(ctx context.Context, provider, model, displayName string) error {
	if a.adapterFactory == nil {
		return fmt.Errorf("no adapter factory configured; cannot switch models")
	}
	newAdapter, err := a.adapterFactory(ctx, provider, model)
	if err != nil {
		return fmt.Errorf("failed to create adapter for %s/%s: %w", provider, model, err)
	}
	a.mu.Lock()
	a.llm = newAdapter
	a.currentModel = displayName
	a.mu.Unlock()
	return nil
}

// SetMaxIterations overrides the default max iterations for the agentic loop.
func (a *Agent) SetMaxIterations(n int) {
	a.maxIterations = n
}

// CurrentModelName returns the display name of the active model.
func (a *Agent) CurrentModelName() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.currentModel
}

// Run executes the agentic loop for a user message
// The loop:
// 1. Adds user message to session history
// 2. Calls LLM with system prompt, tools, and conversation history
// 3. If LLM returns tool calls, executes them and loops back to step 2
// 4. If LLM returns no tool calls, returns the final response
func (a *Agent) Run(ctx context.Context, session *Session, userMessage string) (response string, err error) {
	start := time.Now()
	defer func() { session.metrics.RecordAgentRun(ctx, time.Since(start), err) }()

	// Reset per-run token tracking
	session.ResetRunStats()

	// Add user message to history. Per-message ingestion truncation (context
	// pass): bound the incoming user message to its share of the turn's token
	// budget before it enters history. The message is never rejected — only
	// shortened with the explicit marker — and the turn proceeds. No-op when
	// the session has no token budget.
	session.AddMessage(ctx, llm.Message{
		Role:    "user",
		Content: session.truncateUserMessage(userMessage),
	})

	// Get tool definitions for the LLM
	toolDefs := a.registry.ToDefinitions()

	// Agentic loop
	for i := 0; i < a.maxIterations; i++ {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		// Build request with current conversation history
		req := llm.ChatRequest{
			System:    a.systemPrompt,
			Messages:  session.Messages,
			Tools:     toolDefs,
			MaxTokens: a.maxOutputTokens,
		}

		// Capture tool names for observer
		var toolNames []string
		if a.observer != nil {
			toolNames = make([]string, len(toolDefs))
			for j, td := range toolDefs {
				toolNames[j] = td.Name
			}
		}

		// Call LLM (under read lock so SwitchModel can't swap mid-call)
		a.mu.RLock()
		resp, err := a.llm.Chat(ctx, req)
		a.mu.RUnlock()
		if err != nil {
			return "", fmt.Errorf("llm chat failed: %w", err)
		}

		// Track token usage
		session.AddTokenUsage(ctx, resp.Usage)

		// Stream G phase G3a — session-lifetime token ceiling backstop.
		// The just-returned call's tokens are already in the running
		// total (the AddTokenUsage above), so the check sees the same
		// number any observer of session state would. When the total is
		// at or above the ceiling we terminate the loop — wrapping
		// ErrSessionTokenCeiling with a descriptive message — BEFORE the
		// branch that decides whether to iterate on tool calls. A
		// ceiling of zero or below disables the check, which the static
		// provider never does; the storage-backed provider in a later
		// phase can return zero when an operator clears the limit.
		if ceiling := a.sessionLimits.SessionTokenCeiling(); ceiling > 0 && session.TotalTokens >= ceiling {
			a.writeRunawayAudit(ctx, session.TotalTokens, ceiling)
			return "", fmt.Errorf("session token ceiling (%d tokens) exceeded at total %d: %w",
				ceiling, session.TotalTokens, ErrSessionTokenCeiling)
		}

		// The terminal turn's declared kind, resolved on the branch below and
		// read by the gate and the completion path after it.
		var turnKind TurnKind
		var kindDeclared bool

		// A response with no tool calls is the loop's completion signal — but it
		// is NOT self-evidently a final answer. A model can narrate the tool call
		// it is about to make and then omit the call itself (reproduced on
		// gemini-2.5-flash at roughly one turn in six). Taken at face value that
		// narration ends the turn as a success, which surfaces to the user as Joe
		// announcing a step and then silently stopping — no answer, no error.
		//
		// Probe once to disambiguate: a model that meant to act gets a chance to
		// act, and the loop carries on with the narration preserved as the
		// assistant's message. A model that is genuinely done falls through to the
		// completion path below with its answer untouched.
		if len(resp.ToolCalls) == 0 {
			// Separate the declared kind from the prose BEFORE anything else
			// reads the content: the marker is joe's plumbing, and it must not
			// reach the probe's replay, the session history, the observer, or
			// the operator. Parsing here rather than at the return means there
			// is exactly one place the marker is stripped.
			turnKind, kindDeclared, resp.Content = SplitTurnKind(resp.Content)

			if recovered := a.probeUnfulfilledToolIntent(ctx, session, resp.Content); len(recovered) > 0 {
				resp.ToolCalls = recovered
			}
		}

		// The zero-action question gate: joe does not return a `question`
		// terminal turn on a session in which it has taken no actions
		// (joe-pm threads/terminal-turn-kind.md). The model has not looked
		// yet, so it cannot know that the operator's answer is what it needs.
		// The loop re-enters with the question preserved and one instruction
		// appended, rather than returning.
		//
		// Bounded at one firing per session, unconditionally. An unbounded
		// re-entry gate is a hang, and a hang is a worse failure than the
		// question it was preventing — so a second zero-action question is
		// returned as it stands, and the gate records that it did not hold.
		//
		// This supplements the prompt clause that asks the model not to stop
		// early; it does not replace it. The clause is a ceiling on behaviour
		// and this is a floor under it, and the floor is here because the
		// ceiling is advisory: the same investigation scored 1.0 on
		// 2026-08-22 and 0 on 2026-08-26 against an unchanged prompt.
		if len(resp.ToolCalls) == 0 &&
			turnKind == TurnKindQuestion &&
			session.actionsTaken == 0 &&
			session.zeroActionQuestionGate == "" {

			session.zeroActionQuestionGate = ZeroActionQuestionGateHeld

			// The model's own question stays in history — the re-entered turn
			// must see what it asked, or it will simply ask it again.
			if resp.Content != "" {
				session.AddMessage(ctx, llm.Message{
					Role:    "assistant",
					Content: resp.Content,
				})
			}
			session.AddMessage(ctx, llm.Message{
				Role:    "user",
				Content: prompts.ZeroActionQuestionReentry,
			})

			// The iteration is real — an LLM call was made and paid for — so
			// it is reported. Silence here would make the run's step count
			// disagree with its token spend, and would hide the gated question
			// from anyone reading the steps to see what joe did.
			if a.observer != nil {
				a.observer.OnStep(StepRecord{
					StepNumber: i + 1,
					LLMRequest: LLMRequestSummary{
						MessageCount:   len(session.Messages),
						ToolsAvailable: toolNames,
					},
					LLMResponse: LLMResponseSummary{
						Content: resp.Content,
						Usage:   resp.Usage,
					},
				})
			}
			continue
		}

		// If no tool calls, we have the final response
		if len(resp.ToolCalls) == 0 {
			session.terminalTurnKind = turnKind
			session.turnKindDeclared = kindDeclared

			// The gate fired earlier in this session and the model has come
			// back with the same shape of turn. It is returned rather than
			// looped, and the failure is recorded rather than swallowed.
			if session.zeroActionQuestionGate == ZeroActionQuestionGateHeld &&
				turnKind == TurnKindQuestion &&
				session.actionsTaken == 0 {
				session.zeroActionQuestionGate = ZeroActionQuestionGateNotHeld
			}

			// Add assistant's final response to history
			if resp.Content != "" {
				session.AddMessage(ctx, llm.Message{
					Role:    "assistant",
					Content: resp.Content,
				})
			}

			// Notify observer of final step (no tool execution)
			if a.observer != nil {
				a.observer.OnStep(StepRecord{
					StepNumber: i + 1,
					LLMRequest: LLMRequestSummary{
						MessageCount:   len(session.Messages),
						ToolsAvailable: toolNames,
					},
					LLMResponse: LLMResponseSummary{
						Content: resp.Content,
						Usage:   resp.Usage,
					},
				})
			}

			return resp.Content, nil
		}

		// Add assistant's response (with tool calls) to history
		// The tool calls must be preserved so the LLM sees them on the next iteration
		session.AddMessage(ctx, llm.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// Execute tool calls
		toolCallRequests := make([]tools.ToolCallRequest, len(resp.ToolCalls))
		for i, tc := range resp.ToolCalls {
			toolCallRequests[i] = tools.ToolCallRequest{
				ID:   tc.ID,
				Name: tc.Name,
				Args: tc.Args,
			}
		}

		toolStart := time.Now()
		results, err := a.executor.ExecuteBatch(ctx, toolCallRequests)
		if err != nil && !errors.Is(err, tools.ErrAllToolsFailed) {
			// Only return fatal errors, not tool execution failures
			// Tool failures are added to conversation for LLM to handle
			return "", fmt.Errorf("tool execution failed: %w", err)
		}
		toolDuration := time.Since(toolStart)

		// Every executed call counts as an action, errored ones included: the
		// zero-action gate's claim is that the model has not looked, and a
		// model holding a denial or a timeout has looked.
		session.actionsTaken += len(results)

		// Notify observer of step with tool results
		if a.observer != nil {
			toolResultRecords := make([]ToolResultRecord, len(results))
			for j, r := range results {
				rec := ToolResultRecord{
					ID:         r.ID,
					Name:       r.Name,
					Result:     r.Result,
					DurationMs: int(toolDuration.Milliseconds()) / max(len(results), 1),
				}
				if r.Error != nil {
					rec.Error = r.Error.Error()
					// Classify the TYPED error before it is lost to the string
					// above, so the wire can carry a stable tool-failure code.
					// The vocabulary belongs to whoever injected the classifier;
					// the loop stays unaware of it. Note this runs on EVERY tool
					// error, read or write — the classifier decides what is a
					// denial, not the caller and not the action class.
					if a.toolErrorClassifier != nil {
						rec.ErrorCode = a.toolErrorClassifier(r.Error)
					}
				}
				toolResultRecords[j] = rec
			}
			a.observer.OnStep(StepRecord{
				StepNumber: i + 1,
				LLMRequest: LLMRequestSummary{
					MessageCount:   len(session.Messages),
					ToolsAvailable: toolNames,
				},
				LLMResponse: LLMResponseSummary{
					Content:   resp.Content,
					ToolCalls: resp.ToolCalls,
					Usage:     resp.Usage,
				},
				ToolResults: toolResultRecords,
			})
		}

		// Convert tool results to messages and add to history
		// This includes error messages for failed tools, which the LLM can respond to
		resultMessages := a.executor.ResultsToMessages(results)
		// Per-message ingestion truncation (context pass): bound each
		// oversized tool result to its share of the turn's token budget
		// before it enters history. Truncation happens only here at
		// ingestion; messages already in history are never re-truncated on
		// later iterations.
		session.truncateResultMessages(resultMessages)
		session.AddMessages(ctx, resultMessages)
	}

	// The iteration cap was reached without the model producing a
	// tool-call-free final answer. Before surfacing ErrMaxIterations, make
	// exactly one forced-synthesis Chat call — no tools offered — asking the
	// model to answer now from the evidence already gathered (Session:
	// loop-budget-exhaustion, decision A). Exactly one audit row is written for
	// the cap hit whether or not synthesis succeeds (decision D). A successful
	// synthesis turns the run into a success carrying that answer, marked with
	// stop_reason max_iterations (decision B); an errored or empty-content
	// synthesis falls through to the existing ErrMaxIterations return, unchanged
	// from prior behaviour.
	answer, synthesized := a.synthesizeFinalAnswer(ctx, session)
	a.writeMaxIterationsAudit(ctx, a.maxIterations, synthesized)
	if synthesized {
		session.stopReason = StopReasonMaxIterations
		// A synthesised answer is still a turn that returns words to the
		// operator, so it carries a kind like any other. The synthesis prompt
		// directs the model to answer from the evidence in hand, so the
		// TurnKindAnswer default is the right one when it declares nothing —
		// but a declaration is honoured and its marker stripped, because a
		// model that has learned to emit the line will emit it here too.
		var kind TurnKind
		kind, session.turnKindDeclared, answer = SplitTurnKind(answer)
		session.terminalTurnKind = kind
		return answer, nil
	}

	// Wrap ErrMaxIterations so downstream code can errors.Is the typed
	// sentinel while log readers still see the existing descriptive text.
	return "", fmt.Errorf("max iterations (%d) reached without final response: %w", a.maxIterations, ErrMaxIterations)
}

// probeUnfulfilledToolIntent resolves the ambiguity in a response that carries
// no tool calls: is it a final answer, or did the model narrate a tool call it
// forgot to emit? (Session: silent-tool-intent-dead-end.) The loop's completion
// signal is "no tool calls", so an unfulfilled intent is otherwise indistinguishable
// from a finished turn — it ends the run as a success carrying the narration as
// the answer, which is what the user sees as Joe stopping mid-investigation with
// no message and no error.
//
// The probe replays the turn with the model's own text appended and a single
// user-role instruction (prompts.UnfulfilledToolIntentProbe) asking it to make
// the call it described, or to reply DONE if it is genuinely finished. Tools ARE
// offered — that is the whole point, the model must be able to act.
//
// Only TOOL CALLS are harvested. The probe's text is discarded, never returned
// and never written to history, so a genuine final answer reaches the user
// exactly as the model first wrote it — the probe cannot reword, shorten, or
// degrade a good answer, and the worst case for a finished turn is one cheap
// DONE round-trip. The probe message itself is built on a LOCAL copy of the
// history and is never persisted, so the conversation never carries a synthetic
// user turn the user did not send; on recovery the caller writes back a single
// coherent assistant message (the narration + the recovered calls), which is the
// turn the model meant to produce in the first place.
//
// Errors are non-fatal: a failed probe returns no tool calls and the caller
// treats the response as final, which is exactly the pre-probe behaviour.
func (a *Agent) probeUnfulfilledToolIntent(ctx context.Context, session *Session, content string) []llm.ToolCall {
	select {
	case <-ctx.Done():
		return nil
	default:
	}

	messages := make([]llm.Message, len(session.Messages), len(session.Messages)+2)
	copy(messages, session.Messages)
	// An empty-content response has no assistant turn to replay — providers
	// reject a contentless assistant message — but it is still a silent stop
	// worth probing, so only the probe instruction is appended.
	if content != "" {
		messages = append(messages, llm.Message{Role: "assistant", Content: content})
	}
	messages = append(messages, llm.Message{
		Role:    "user",
		Content: prompts.UnfulfilledToolIntentProbe,
	})

	a.mu.RLock()
	resp, err := a.llm.Chat(ctx, llm.ChatRequest{
		System:    a.systemPrompt,
		Messages:  messages,
		Tools:     a.registry.ToDefinitions(),
		MaxTokens: a.maxOutputTokens,
	})
	a.mu.RUnlock()
	if err != nil {
		return nil
	}

	// Real spend, even when the probe confirms the turn was already finished.
	session.AddTokenUsage(ctx, resp.Usage)

	return resp.ToolCalls
}

// synthesizeFinalAnswer makes one forced-synthesis Chat call after the
// iteration cap is reached (Session: loop-budget-exhaustion, decision A). The
// accumulated session messages are reused verbatim with a single appended
// user-role instruction (prompts.MaxIterationsSynthesis) directing the model to
// answer now from the evidence in hand — stating what it verified and what
// remains unverified because the step budget was reached — and NO tools are
// offered, so the model cannot spend another tool round. Content extraction
// mirrors the normal no-tool-call completion branch exactly: the raw
// resp.Content string. A non-empty string is a successful synthesis; an errored
// call or empty content is a failure the caller turns back into
// ErrMaxIterations (decision A + the bound requirement that empty content is
// explicitly a failure).
//
// The call is deliberately OBSERVER-SILENT (no OnStep): the run's reported
// iteration count is len(observer steps), so emitting a step here would inflate
// it past the cap and change the shape of an already-exhausted run. Token usage
// IS recorded (it is real spend and must appear in the turn's totals), but no
// session token-ceiling check is applied — the loop has already exited and the
// synthesis call is a bounded, tool-less single shot. The request is built from
// a LOCAL copy of the message slice and nothing is written back into session
// history, so the pruning/truncation counters the final event reports stay
// exactly what the loop left them — the synthesis attempt has no observable
// side effect beyond token usage and (on success) the returned answer.
func (a *Agent) synthesizeFinalAnswer(ctx context.Context, session *Session) (string, bool) {
	// Honour cancellation: a caller whose context is already done gets no
	// synthesis attempt (mirrors the loop's per-iteration cancellation check).
	select {
	case <-ctx.Done():
		return "", false
	default:
	}

	messages := make([]llm.Message, len(session.Messages), len(session.Messages)+1)
	copy(messages, session.Messages)
	messages = append(messages, llm.Message{
		Role:    "user",
		Content: prompts.MaxIterationsSynthesis,
	})

	req := llm.ChatRequest{
		System:   a.systemPrompt,
		Messages: messages,
		// No Tools: the synthesis turn must answer from evidence in hand.
		MaxTokens: a.maxOutputTokens,
	}

	a.mu.RLock()
	resp, err := a.llm.Chat(ctx, req)
	a.mu.RUnlock()
	if err != nil {
		return "", false
	}
	// Real spend — record it in the turn's totals even though the loop has
	// already decided to exit.
	session.AddTokenUsage(ctx, resp.Usage)
	if resp.Content == "" {
		return "", false
	}
	return resp.Content, true
}

// writeMaxIterationsAudit records one KindLLMLimitTriggered audit row for an
// iteration-cap hit (Session: loop-budget-exhaustion, decision D). It mirrors
// writeRunawayAudit exactly: same sink (a.auditRepo, injected via
// WithAuditRepo), same nil-tolerance (no repo wired → skip silently, the cap
// has already been decided and the row is forensic not gating), same
// fail-open-but-loud posture via audit.FailurePosture(FailOpen). It is called
// once per cap hit whether or not synthesis succeeded; the blob's "synthesized"
// flag records which, alongside the iteration count that was reached.
func (a *Agent) writeMaxIterationsAudit(ctx context.Context, iterations int, synthesized bool) {
	if a.auditRepo == nil {
		return
	}
	blob, _ := json.Marshal(map[string]any{
		"session_id":  agentctx.SessionID(ctx),
		"task_id":     agentctx.TaskID(ctx),
		"iterations":  iterations,
		"synthesized": synthesized,
	})
	err := a.auditRepo.Insert(ctx, audit.Event{
		Principal: string(rbac.PrincipalFromContext(ctx)),
		Action:    audit.ActionLLMMaxIterationsReached,
		Decision:  audit.DecisionDeny,
		Reason:    "max_iterations_exhausted",
		Kind:      audit.KindLLMLimitTriggered,
		Context:   string(blob),
	})
	// Fail-open-but-loud: the cap decision has already been made; an audit-write
	// failure must not mask the outcome the caller returns (a synthesized answer
	// or the ErrMaxIterations sentinel). Same posture writeRunawayAudit uses.
	_ = audit.FailurePosture(ctx, audit.ActionLLMMaxIterationsReached, err, "agentloop:max_iterations", audit.FailOpen)
}

// writeRunawayAudit records one KindLLMLimitTriggered audit row for a
// session-token-ceiling termination. Best-effort: a nil repository
// (no audit wired in tests / dev) is tolerated and skips the write
// silently — termination has already been decided by the time we get
// here, so the audit row is forensic, not gating. On a real repository
// failure we route through audit.FailurePosture (fail-open-but-loud) so the
// loud failure log lands in operational logs, but we
// still return; the loop is already exiting with the ceiling sentinel
// and the caller should see that error, not an internal audit error
// that masks the real reason.
func (a *Agent) writeRunawayAudit(ctx context.Context, total, ceiling int) {
	if a.auditRepo == nil {
		return
	}
	blob, _ := json.Marshal(map[string]any{
		"session_id":            agentctx.SessionID(ctx),
		"task_id":               agentctx.TaskID(ctx),
		"session_token_total":   total,
		"session_token_ceiling": ceiling,
	})
	err := a.auditRepo.Insert(ctx, audit.Event{
		Principal: string(rbac.PrincipalFromContext(ctx)),
		Action:    audit.ActionLLMRunawayTerminated,
		Decision:  audit.DecisionDeny,
		Reason:    "session_token_ceiling_exceeded",
		Kind:      audit.KindLLMLimitTriggered,
		Context:   string(blob),
	})
	// Fail-open-but-loud: pass audit.FailOpen so the loud log names the real
	// outcome (the termination PROCEEDED) rather than claiming a fail-closed
	// abort, and discard the returned error. The loop is already exiting via
	// the ceiling sentinel; surfacing an audit error here would replace the
	// typed terminal error the classifier wants to see.
	_ = audit.FailurePosture(ctx, audit.ActionLLMRunawayTerminated, err, "agentloop:runaway", audit.FailOpen)
}
