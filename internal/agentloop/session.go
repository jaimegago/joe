package agentloop

import (
	"context"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/observability"
)

// Session holds the conversation history for an agentic interaction
type Session struct {
	Messages []llm.Message
	metrics  *observability.Metrics

	// Token usage tracking
	TotalInputTokens  int
	TotalOutputTokens int
	TotalTokens       int

	// Per-run token tracking (reset at start of each Run)
	RunInputTokens  int
	RunOutputTokens int
	RunTokens       int
	RunLLMCalls     int

	// MaxMessages limits conversation history size to prevent unbounded growth
	// When 0, no limit is applied. Recommended: 100-200 for typical conversations.
	// Applied as a SECONDARY backstop after the token budget (a cheap guard
	// against pathological many-tiny-messages cases); the token budget is
	// primary.
	MaxMessages int

	// TokenBudget is the per-turn input token budget the history is pruned
	// to. When 0 (the default for a session constructed without a budget),
	// token-based pruning is disabled and only the MaxMessages count
	// backstop applies — preserving the prior behaviour for callers that
	// never set a budget. buildTaskRun computes it from the active model's
	// context window, the reserved output, and the prompt/tool overhead.
	TokenBudget int

	// historyTrimmed / messagesDropped record whether this turn's pruning
	// dropped any messages and how many. Surfaced on the final SSE event so
	// the UI can tell the user earlier messages are no longer in context.
	// They accumulate over the session's lifetime, which is per-task (a
	// fresh session is built per request in buildTaskRun), so they describe
	// exactly this turn.
	historyTrimmed  bool
	messagesDropped int

	// toolResultsTruncated / userMessageTruncated record this turn's
	// per-message ingestion truncation: how many tool-result messages were
	// shortened to fit their cap, and whether the incoming user message was.
	// Surfaced on the final SSE event (tool_results_truncated /
	// user_message_truncated). Like historyTrimmed they describe this turn —
	// the session is built per-task in buildTaskRun.
	toolResultsTruncated int
	userMessageTruncated bool

	// stopReason records why a run that reported terminal status "completed"
	// nonetheless did not end on a naturally tool-call-free answer — set to
	// StopReasonMaxIterations by Run's forced-synthesis path (Session:
	// loop-budget-exhaustion, decision B). Empty for a normally-completed run.
	// Like the fields above it describes this turn (a fresh session is built
	// per task in buildTaskRun); the API layer reads it via StopReason() to
	// stamp the final event and persist the assistant message's marker.
	stopReason string

	// actionsTaken counts the tool calls this SESSION has executed, across
	// every Run on it. It is the "zero actions" the terminal-turn gate keys
	// on, and it is deliberately not reset by ResetRunStats: the gate's rule
	// is stated over the session, not the turn, so a follow-up question in a
	// session that has already looked is not gated.
	//
	// A call that ERRORED still counts. The gate's claim is "you have not
	// looked yet", and a model whose tool call was denied, timed out, or came
	// back empty has looked — it holds evidence about the environment, which
	// is exactly what the zero-action case says it cannot have.
	actionsTaken int

	// terminalTurnKind / turnKindDeclared record the kind of the turn that
	// ended this run and whether the model actually declared it, rather than
	// falling back to the TurnKindAnswer default. Two fields because the
	// vocabulary is closed at three values (see turnkind.go): a turn always
	// carries one of them, and whether it was declared is a separate fact.
	// terminalTurnKind is empty only for a run that never reached a terminal
	// turn at all — a context cancellation, a token-ceiling termination, an
	// LLM error. Those return no words to the operator, so they are not
	// terminal turns and carry no kind.
	terminalTurnKind TurnKind
	turnKindDeclared bool

	// terminalConclusion is the diagnostic conclusion the model declared on
	// the turn that ended this run — the cause it committed to and the signals
	// it ruled out (see conclusion.go). Zero-valued when the model declared
	// none, which DiagnosticConclusion.Declared reports; an undeclared
	// conclusion is an absence and is never defaulted to a claim, because
	// unlike a turn kind there is no neutral value a conclusion could take.
	terminalConclusion DiagnosticConclusion

	// zeroActionQuestionGate records the gate's outcome for this session:
	// empty when it never fired, ZeroActionQuestionGateHeld when it fired and
	// the model did not go on to return another zero-action question, and
	// ZeroActionQuestionGateNotHeld when the re-entered turn was again one and
	// was returned as it stood. The bound is one firing per session, tracked
	// by the same field being non-empty.
	zeroActionQuestionGate string
}

// NewSession creates a new session with empty conversation history
func NewSession(metrics *observability.Metrics) *Session {
	metrics = observability.EnsureMetrics(metrics)
	metrics.RecordSessionStart()
	return &Session{
		Messages: make([]llm.Message, 0),
		metrics:  metrics,
	}
}

// Close releases session tracking metrics.
func (s *Session) Close() {
	s.metrics.RecordSessionEnd()
}

// AddMessage adds a message to the conversation history.
// If MaxMessages is set and exceeded, older messages are pruned to keep
// the most recent MaxMessages entries (sliding window).
func (s *Session) AddMessage(ctx context.Context, message llm.Message) {
	s.Messages = append(s.Messages, message)
	s.metrics.RecordSessionMessage(ctx, message.Role)
	s.pruneMessages()
}

// AddMessages adds multiple messages to the conversation history.
// If MaxMessages is set and exceeded, older messages are pruned to keep
// the most recent MaxMessages entries (sliding window).
func (s *Session) AddMessages(ctx context.Context, messages []llm.Message) {
	s.Messages = append(s.Messages, messages...)
	for _, msg := range messages {
		s.metrics.RecordSessionMessage(ctx, msg.Role)
	}
	s.pruneMessages()
}

// pruneMessages trims the conversation history. The token budget is applied
// first (primary), then the MaxMessages count cap (secondary backstop). Both
// drop from the OLDEST end and respect two hard correctness constraints: the
// most recent genuine user message is never dropped (even if it alone exceeds
// the budget), and a tool-result message is never left without its preceding
// tool-call message (call/result pairs are dropped together) — the Gemini
// adapter renders a tool result as a FunctionResponse paired with the call,
// so an orphaned result would be malformed.
func (s *Session) pruneMessages() {
	before := len(s.Messages)
	s.pruneToTokenBudget()
	s.pruneToCountBackstop()
	if dropped := before - len(s.Messages); dropped > 0 {
		s.historyTrimmed = true
		s.messagesDropped += dropped
	}
}

// pruneToTokenBudget drops oldest messages until the estimated token total
// is within TokenBudget, stopping short of the most recent genuine user
// message and never leaving a leading orphaned tool-result. Disabled when
// TokenBudget <= 0.
func (s *Session) pruneToTokenBudget() {
	if s.TokenBudget <= 0 {
		return
	}
	n := len(s.Messages)
	per := make([]int, n)
	total := 0
	for i := range s.Messages {
		per[i] = EstimateMessageTokens(s.Messages[i])
		total += per[i]
	}
	if total <= s.TokenBudget {
		return
	}
	protect := s.lastUserMessageIndex()
	cutoff := 0
	for total > s.TokenBudget {
		if cutoff >= n || (protect >= 0 && cutoff >= protect) {
			break
		}
		total -= per[cutoff]
		cutoff++
		// Dropping a tool-call message must also drop its result(s): if the
		// new boundary is a tool-result, advance past the whole run so the
		// kept slice never begins with an orphaned result.
		for cutoff < n && (protect < 0 || cutoff < protect) && s.Messages[cutoff].ToolResultID != "" {
			total -= per[cutoff]
			cutoff++
		}
	}
	if cutoff > 0 {
		s.Messages = s.Messages[cutoff:]
	}
}

// pruneToCountBackstop applies the MaxMessages count cap after the token
// budget. It honours the same pair-integrity and most-recent-user-message
// guards, so the kept slice may slightly exceed MaxMessages rather than
// orphan a tool result — correctness over an exact count.
func (s *Session) pruneToCountBackstop() {
	if s.MaxMessages <= 0 || len(s.Messages) <= s.MaxMessages {
		return
	}
	cutoff := len(s.Messages) - s.MaxMessages
	protect := s.lastUserMessageIndex()
	if protect >= 0 && cutoff > protect {
		cutoff = protect
	}
	cutoff = s.skipLeadingOrphanResults(cutoff, protect)
	if cutoff > 0 {
		s.Messages = s.Messages[cutoff:]
	}
}

// skipLeadingOrphanResults advances cutoff past a run of tool-result messages
// so the kept slice (s.Messages[cutoff:]) never begins with a tool-result
// whose tool-call parent was dropped. It never advances past protect (the
// most recent genuine user message) when one exists.
func (s *Session) skipLeadingOrphanResults(cutoff, protect int) int {
	limit := len(s.Messages)
	if protect >= 0 {
		limit = protect
	}
	for cutoff < limit && s.Messages[cutoff].ToolResultID != "" {
		cutoff++
	}
	return cutoff
}

// lastUserMessageIndex returns the index of the most recent GENUINE user
// message — Role "user" with no ToolResultID. Tool-result messages also
// carry Role "user" (see tools.ResultToMessage), so the ToolResultID guard
// is what distinguishes a real user turn from a tool result. Returns -1 when
// there is no genuine user message.
func (s *Session) lastUserMessageIndex() int {
	for i := len(s.Messages) - 1; i >= 0; i-- {
		if s.Messages[i].Role == "user" && s.Messages[i].ToolResultID == "" {
			return i
		}
	}
	return -1
}

// HistoryTrimmed reports whether pruning dropped any messages this turn.
func (s *Session) HistoryTrimmed() bool { return s.historyTrimmed }

// MessagesDropped reports how many messages pruning dropped this turn.
func (s *Session) MessagesDropped() int { return s.messagesDropped }

// ToolResultsTruncated reports how many tool-result messages were shortened
// at ingestion this turn to fit their per-message cap.
func (s *Session) ToolResultsTruncated() int { return s.toolResultsTruncated }

// UserMessageTruncated reports whether the incoming user message was shortened
// at ingestion this turn to fit its per-message cap.
func (s *Session) UserMessageTruncated() bool { return s.userMessageTruncated }

// StopReason reports why this turn completed via a non-natural stop (currently
// only StopReasonMaxIterations, set by Run's forced-synthesis path). Empty for
// a normally-completed run.
func (s *Session) StopReason() string { return s.stopReason }

// ActionsTaken reports how many tool calls this session has executed, across
// every Run on it, successful or not.
func (s *Session) ActionsTaken() int { return s.actionsTaken }

// TerminalTurnKind reports the declared kind of the turn that ended this run —
// one of TurnKindAnswer, TurnKindQuestion, TurnKindRefusal. Empty only when the
// run returned no words to the operator at all (cancellation, token ceiling, an
// LLM error), which is not a terminal turn.
func (s *Session) TerminalTurnKind() TurnKind { return s.terminalTurnKind }

// TurnKindDeclared reports whether the model actually emitted the kind marker
// for the terminal turn, as against TerminalTurnKind having fallen back to the
// TurnKindAnswer default. A consumer that needs to know the model said so —
// rather than that joe assumed so — reads this alongside the kind.
func (s *Session) TurnKindDeclared() bool { return s.turnKindDeclared }

// TerminalConclusion reports the diagnostic conclusion the model declared on
// the turn that ended this run. Its Declared method says whether the model
// declared one at all — a consumer needs that separately, because an empty
// Discarded list means "discarded nothing" under a declaration and "declared
// nothing" without one, and the list alone cannot tell those apart.
func (s *Session) TerminalConclusion() DiagnosticConclusion { return s.terminalConclusion }

// ZeroActionQuestionGate reports the gate's outcome for this session:
// ZeroActionQuestionGateHeld, ZeroActionQuestionGateNotHeld, or empty when it
// never fired.
func (s *Session) ZeroActionQuestionGate() string { return s.zeroActionQuestionGate }

// truncationLimit returns the per-message token cap for the given budget
// fraction: max(fraction * TokenBudget, minTruncationTokenFloor). It returns 0
// when the session has no positive token budget — mirroring
// pruneToTokenBudget's gate — so callers leave content untouched for sessions
// that never set a budget (legacy / test callers), preserving prior behaviour.
func (s *Session) truncationLimit(fraction float64) int {
	if s.TokenBudget <= 0 {
		return 0
	}
	limit := int(float64(s.TokenBudget) * fraction)
	if limit < minTruncationTokenFloor {
		limit = minTruncationTokenFloor
	}
	return limit
}

// truncateUserMessage bounds the incoming user message to
// userMessageBudgetFraction of the turn's token budget before it enters
// history, recording whether it was shortened. The message is never rejected;
// it is only truncated with the marker so the turn can proceed. No-op when the
// session has no token budget.
func (s *Session) truncateUserMessage(content string) string {
	limit := s.truncationLimit(userMessageBudgetFraction)
	if limit <= 0 {
		return content
	}
	out, did := TruncateContent(content, limit)
	if did {
		s.userMessageTruncated = true
	}
	return out
}

// truncateResultMessages bounds each tool-result message's content to
// toolResultBudgetFraction of the turn's token budget, in place, counting how
// many were shortened. Only tool-result messages (ToolResultID set) are
// touched. Applied at ingestion, before the messages are appended; messages
// already in history are never re-truncated.
func (s *Session) truncateResultMessages(msgs []llm.Message) {
	limit := s.truncationLimit(toolResultBudgetFraction)
	if limit <= 0 {
		return
	}
	for i := range msgs {
		if msgs[i].ToolResultID == "" {
			continue
		}
		out, did := TruncateContent(msgs[i].Content, limit)
		if did {
			msgs[i].Content = out
			s.toolResultsTruncated++
		}
	}
}

// Clear clears the conversation history
func (s *Session) Clear() {
	s.Messages = make([]llm.Message, 0)
}

// ResetRunStats resets per-run token tracking (called at start of each Run)
func (s *Session) ResetRunStats() {
	s.RunInputTokens = 0
	s.RunOutputTokens = 0
	s.RunTokens = 0
	s.RunLLMCalls = 0
}

// AddTokenUsage adds token usage from an LLM response
func (s *Session) AddTokenUsage(ctx context.Context, usage llm.TokenUsage) {
	// Update per-run stats
	s.RunInputTokens += usage.InputTokens
	s.RunOutputTokens += usage.OutputTokens
	s.RunTokens += usage.TotalTokens
	s.RunLLMCalls++

	// Update total session stats
	s.TotalInputTokens += usage.InputTokens
	s.TotalOutputTokens += usage.OutputTokens
	s.TotalTokens += usage.TotalTokens
	s.metrics.RecordSessionTokens(ctx, usage.TotalTokens)
}
