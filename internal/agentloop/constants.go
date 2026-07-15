package agentloop

const (
	// DefaultMaxIterations prevents infinite agentic loops. Raised from 10 to 20
	// (Session: loop-budget-exhaustion, decision E) to give multi-step
	// investigations more room before the cap fires; per-request override via
	// taskConfig.MaxIterations is unchanged. When the cap IS reached the loop no
	// longer hard-fails — it makes one forced-synthesis Chat call (decision A) —
	// so this ceiling now bounds tool spend, not the ability to answer.
	DefaultMaxIterations = 20

	// DefaultMaxMessages limits session history to prevent unbounded growth.
	DefaultMaxMessages = 100
)

// StopReason enumerates the reasons a run reported a terminal status of
// "completed" despite not ending on a naturally tool-call-free answer. It is a
// short, additive string enum (Session: loop-budget-exhaustion, decision B):
// the first value covers the iteration-cap forced synthesis, and the
// token-ceiling and context-overflow paths can adopt sibling values later
// without a schema change. An empty stop reason is the common case — a run that
// completed normally.
const (
	// StopReasonMaxIterations marks a run whose answer was produced by the
	// forced-synthesis Chat call after the iteration cap was reached, rather
	// than by the model electing to stop. The turn is a success (status
	// "completed") but the UI renders a truncation notice and the assistant
	// message persists this marker so a reloaded session still shows it.
	StopReasonMaxIterations = "max_iterations"

	// StopReasonCancelled marks a turn the caller stopped in flight: the client
	// aborted the streaming request (context.Canceled, distinct from the
	// DeadlineExceeded timeout path), so the loop returned with no answer. The
	// streaming handler persists a minimal assistant marker row carrying this
	// value so a reloaded session shows the turn was stopped rather than a
	// question with no reply. Additive sibling of StopReasonMaxIterations under
	// the same open-enum design (D-0099) — the terminal status vocabulary is
	// unchanged; stop_reason is the only new vocabulary.
	StopReasonCancelled = "cancelled"
)

// Per-message ingestion-truncation caps. A single message entering session
// history is bounded to a fraction of the turn's input token budget so one
// oversized message cannot, on its own, overflow the provider window (the
// token-budget pruner deliberately exempts the most recent user message and
// the newest tool result, so these caps are the per-message backstop).
const (
	// toolResultBudgetFraction caps each tool result at 25% of the turn's
	// token budget.
	toolResultBudgetFraction = 0.25
	// userMessageBudgetFraction caps the incoming user message at 50% of the
	// turn's token budget. The user message is never rejected — only
	// shortened with the marker — and the turn proceeds.
	userMessageBudgetFraction = 0.50
	// minTruncationTokenFloor is the absolute floor (in tokens) applied to
	// both caps so a small budget never reduces a message to nothing.
	minTruncationTokenFloor = 2000
)
