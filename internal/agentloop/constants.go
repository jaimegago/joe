package agentloop

const (
	// DefaultMaxIterations prevents infinite agentic loops.
	DefaultMaxIterations = 10

	// DefaultMaxMessages limits session history to prevent unbounded growth.
	DefaultMaxMessages = 100
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
