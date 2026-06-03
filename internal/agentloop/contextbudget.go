package agentloop

// Context-budget provider for token-based history pruning.
//
// The agentic loop's per-turn input budget is a FRACTION of the active
// model's context window (the rest of the window is reserved for the output
// cap and the fixed system-prompt + tool-definition overhead). The fraction
// is read through this interface so a storage-backed implementation (an
// operator-tunable settings row) can drop in behind the same seam the
// static default occupies, exactly as SessionLimits does for the runaway
// ceiling. buildTaskRun reads the fraction and computes the concrete token
// budget (see ComputeInputTokenBudget); the loop itself only sees the
// resolved Session.TokenBudget.

// DefaultContextBudgetFraction is the hardcoded backstop fraction applied
// when no operator value is configured (the settings row is unset/zero) or
// a settings read fails. 0.7 leaves a generous margin below the model's
// context window for the reserved output and prompt/tool overhead while
// still admitting most of a typical multi-turn history.
const DefaultContextBudgetFraction = 0.7

// ContextBudget surfaces the per-turn input budget fraction the agentic
// path consults. A returned value of zero or below is treated by the
// storage-backed provider as "unset, fall back to the backstop"; the static
// implementation always returns the positive default.
type ContextBudget interface {
	BudgetFraction() float64
}

// StaticContextBudget is the hardcoded ContextBudget the agentic path
// defaults to when no storage-backed provider is wired. It returns
// DefaultContextBudgetFraction unconditionally.
type StaticContextBudget struct{}

// NewStaticContextBudget returns the safe-default ContextBudget provider.
func NewStaticContextBudget() ContextBudget { return StaticContextBudget{} }

// BudgetFraction returns the hardcoded backstop fraction.
func (StaticContextBudget) BudgetFraction() float64 { return DefaultContextBudgetFraction }
