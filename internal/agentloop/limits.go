package agentloop

// Stream G phase G3a: session-lifetime token ceiling provider.
//
// The agentic loop reads the session token ceiling through SessionLimits
// rather than referring to the constant directly, so a later phase can
// drop in a storage-backed implementation (read out of an llm_runaway_limits
// settings table) behind the same interface without touching the check
// site in Agent.Run. The G3a wire is the static, hardcoded implementation;
// the swap seam is the interface.

// DefaultSessionTokenCeiling is the hardcoded session-lifetime token cap
// the StaticSessionLimits provider returns.
//
// Reasoning. The ceiling counts the sum of input AND output tokens
// accumulated across an ENTIRE session (every Chat call's Usage added
// together, the same number Session.TotalTokens carries). 10,000,000
// tokens is roughly two orders of magnitude beyond a busy legitimate
// multi-turn agentic session — even fifty turns each carrying a 200k
// context input only sums to ~10M, and real sessions almost never stack
// that deep. At today's frontier-model pricing the ceiling represents
// tens to hundreds of dollars of LLM spend in a single session: a sum
// large enough that a runaway is unambiguously a runaway, small enough
// that it cannot consume an absurd token volume before the loop is
// terminated.
//
// This is a SAFETY BACKSTOP against a runaway loop, NOT an operational
// budget. Cost budgeting is the cost-window gate added in the next phase
// (G3b) — a separate enforcement primitive with its own threshold
// semantics. Under normal operation the ceiling never fires; if it does,
// something is wrong upstream (a buggy tool result that keeps re-feeding
// the LLM, an inflated max-iterations setting, an adapter returning
// junk usage numbers).
//
// The value is intentionally TUNABLE in a later phase: the storage-backed
// provider will satisfy the same SessionLimits interface, so swapping
// from this constant to a settings-table read is a wiring change at
// construction time, not a rewrite of the check site.
const DefaultSessionTokenCeiling = 10_000_000

// SessionLimits surfaces the per-session enforcement bounds the agentic
// loop consults. The interface exposes a single method today — the
// session-lifetime token ceiling — and the loop depends on the
// interface rather than the constant so the swap to a storage-backed
// implementation is transparent to Agent.Run.
//
// A returned value of zero or below disables the ceiling (no check
// fires). The static implementation never returns zero; the storage
// implementation can, when an operator explicitly clears the limit.
type SessionLimits interface {
	SessionTokenCeiling() int
}

// StaticSessionLimits is the hardcoded implementation of SessionLimits
// the loop defaults to when no provider is supplied via WithSessionLimits.
// It returns DefaultSessionTokenCeiling unconditionally; the value
// cannot be tuned at runtime through this implementation. The
// storage-backed implementation arriving in a later phase satisfies the
// same interface and is dropped in at construction without touching
// any code in this package.
type StaticSessionLimits struct{}

// NewStaticSessionLimits returns the safe-default SessionLimits provider.
// Production construction sites pass this explicitly; tests and other
// callers that omit WithSessionLimits get the same provider by default
// (set in NewAgent), so the ceiling is enforced even without explicit
// wiring.
func NewStaticSessionLimits() SessionLimits { return StaticSessionLimits{} }

// SessionTokenCeiling returns the hardcoded session-lifetime ceiling.
func (StaticSessionLimits) SessionTokenCeiling() int { return DefaultSessionTokenCeiling }
