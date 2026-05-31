package llmusage

import "github.com/jaimegago/joe/internal/llm"

// Stream G phase G3b — cost-window threshold provider.
//
// The recorder's pre-call gate reads its per-window thresholds through
// CostLimits rather than referring to the constants directly, so a
// later phase can drop in a storage-backed implementation (read from an
// llm_cost_limits settings table) behind the same interface without
// touching the gate site in RecorderAdapter.Chat. The G3b wire is the
// static, hardcoded implementation; the swap seam is the interface.
//
// Thresholds are a SINGLE numeric value per window applied in whatever
// the configured currency happens to be — they are not per-currency.
// The reasoning is asymmetric: a runaway backstop sized to bound the
// blast radius of unattended runaway spend does not need
// currency-precise calibration. That precision belongs to the
// operator-tunable storage-backed phase. If the configured currency is,
// say, EUR while the static defaults were sized against USD, the gate
// may fire slightly earlier or later than it would against USD — that
// imprecision is irrelevant for a backstop whose only job is to keep an
// unattended runaway from burning a survivable amount before someone
// notices. Under normal interactive load the gate does not fire; when
// it does fire, the system is in a runaway and a ~10% currency
// mismatch is far below the noise floor.

// Hardcoded per-window cost limits in integer nano-units of the
// configured currency. They are sized to bound the BLAST RADIUS of an
// unattended runaway to a survivable amount while sitting comfortably
// above normal interactive load so they do not fire in legitimate use.
//
// Worked sizing. A single agentic turn at the launch built-in prices
// (Claude Sonnet 4 at $3 / MTok input, $15 / MTok output) running a
// 200k input + 4k output cycle costs roughly $0.66; a much cheaper
// model like Gemini 2.5 Flash sits an order of magnitude below that.
// An interactive operator session runs a handful to a few dozen turns
// — well under ten units per hour for a normal Claude-backed workload
// and a small fraction of that for Flash. The hourly default at ~100
// units leaves an order of magnitude of headroom above heavy
// legitimate use but bounds an unattended runaway loop (re-feeding a
// large context every turn at maximum iteration cap) to roughly that
// figure before the gate stops the bleed. The daily default at ~500
// units allows several heavy interactive hours plus background usage
// while still bounding a multi-hour runaway to a low-three-figure
// amount. The monthly default at ~5,000 units accommodates a month of
// the same pace with similar margin.
//
// These are SAFETY BACKSTOPS that bound the blast radius of an
// unattended runaway to a survivable amount — NOT operational budgets.
// An operator sets real budgets via the storage-backed limits in a
// later phase (the same interface; one wiring change at the
// construction site). The hardcoded defaults exist so a misconfigured
// deployment cannot run completely uncapped. A threshold of zero or
// below disables that window's check (matching the disable convention
// used by SessionLimits.SessionTokenCeiling); the static provider
// never returns zero, but the storage-backed one can when the operator
// explicitly clears a window's limit.
//
// Units are integer nano-units (1e-9) of whatever currency is
// configured, matching llm_usage.estimated_cost_nano. The constants
// below are written as the human-readable unit count multiplied by
// llm.CostNanoUnitsPerUnit so the magnitude is immediately readable.
const (
	// DefaultHourlyCostLimitNano ≈ 100 units of the configured currency.
	DefaultHourlyCostLimitNano = 100 * llm.CostNanoUnitsPerUnit
	// DefaultDailyCostLimitNano ≈ 500 units of the configured currency.
	DefaultDailyCostLimitNano = 500 * llm.CostNanoUnitsPerUnit
	// DefaultMonthlyCostLimitNano ≈ 5,000 units of the configured currency.
	DefaultMonthlyCostLimitNano = 5_000 * llm.CostNanoUnitsPerUnit
)

// CostLimits surfaces the per-window cost thresholds the recorder's
// pre-call gate consults. The interface exposes three methods today —
// one per window — and the recorder depends on the interface rather
// than the constants so the swap to a storage-backed implementation is
// transparent to the gate site.
//
// A returned value of zero or below disables that window's check (no
// sum, no comparison, no audit row for that window). The static
// implementation never returns zero or below; the storage
// implementation can, when an operator explicitly clears the window's
// limit. The disable convention matches SessionLimits.SessionTokenCeiling.
type CostLimits interface {
	HourlyLimitNano() int64
	DailyLimitNano() int64
	MonthlyLimitNano() int64
}

// StaticCostLimits is the hardcoded implementation of CostLimits the
// recorder defaults to when no provider is supplied via WithCostLimits.
// It returns the Default*CostLimitNano constants unconditionally; the
// values cannot be tuned at runtime through this implementation. The
// storage-backed implementation arriving in a later phase satisfies
// the same interface and is dropped in at construction without
// touching any code in this package.
type StaticCostLimits struct{}

// NewStaticCostLimits returns the safe-default CostLimits provider.
// Production construction sites pass this explicitly; tests and other
// callers that omit WithCostLimits get the same provider by default
// (set in NewRecorderAdapter), so the gate is enforced even without
// explicit wiring.
func NewStaticCostLimits() CostLimits { return StaticCostLimits{} }

// HourlyLimitNano returns the hardcoded hourly threshold.
func (StaticCostLimits) HourlyLimitNano() int64 { return DefaultHourlyCostLimitNano }

// DailyLimitNano returns the hardcoded daily threshold.
func (StaticCostLimits) DailyLimitNano() int64 { return DefaultDailyCostLimitNano }

// MonthlyLimitNano returns the hardcoded monthly threshold.
func (StaticCostLimits) MonthlyLimitNano() int64 { return DefaultMonthlyCostLimitNano }
