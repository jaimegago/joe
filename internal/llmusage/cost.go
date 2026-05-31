package llmusage

import (
	"math"
	"strings"

	"github.com/jaimegago/joe/internal/llm"
)

// PerTokenPrice is the source-currency per-token price for one provider/model
// pair. Prices are in units of the source currency (NOT nano-units): a value
// of 3e-6 for InputUSDPerToken means three dollars per million input tokens,
// quoted as the per-token unit. The recorder multiplies token counts by
// these per-token values, optionally applies the configured currency FX
// rate, then scales by llm.CostNanoUnitsPerUnit to land on the integer
// storage column.
//
// SourceCurrency is the currency the price is quoted in by the provider's
// pricing page. For Stream G's launch providers (Claude, Gemini) this is
// always "USD"; self-hosted / non-USD source currencies are out of scope
// for G2 (the recorder rejects FX conversion from any non-USD source).
type PerTokenPrice struct {
	InputPerToken  float64
	OutputPerToken float64
	SourceCurrency string
}

// modelKey is the composite (provider, model) key used by the cost table.
// Provider names are lowercased on construction so callers don't need to
// normalize at every lookup.
type modelKey struct {
	Provider string
	Model    string
}

func makeKey(provider, model string) modelKey {
	return modelKey{
		Provider: strings.ToLower(strings.TrimSpace(provider)),
		Model:    strings.TrimSpace(model),
	}
}

// builtinPrices is the compile-time cost table. Every model present in the
// default Joe configuration (config.example.yaml + the embedded defaults in
// internal/config/constants.go) has an entry here, sourced from the
// provider's current official pricing page. Each entry cites the source
// URL and the date the price was captured next to it so a future reader
// can detect drift without re-mining commit history.
//
// Add an entry the same way: provider lowercased, model name exactly as
// the adapter emits it, prices in per-token units of the source currency.
// Unknown models are NOT silently priced — they record a zero-cost row
// and emit a warning (see Recorder.priceFor).
var builtinPrices = map[modelKey]PerTokenPrice{
	// Claude Sonnet 4 — Anthropic API pricing.
	// Source: https://platform.claude.com/docs/en/about-claude/pricing
	// Captured: 2026-05-31. Quoted as $3 / MTok input, $15 / MTok output;
	// converted to per-token by dividing by 1_000_000.
	makeKey("claude", "claude-sonnet-4-20250514"): {
		InputPerToken:  3.0 / 1_000_000.0,
		OutputPerToken: 15.0 / 1_000_000.0,
		SourceCurrency: "USD",
	},
	// Gemini 2.5 Flash — Google AI Studio paid-tier pricing for the
	// text / image / video input category (audio input is more expensive
	// and is not exercised by Joe's chat path).
	// Source: https://ai.google.dev/gemini-api/docs/pricing
	// Captured: 2026-05-31. Quoted as $0.30 / MTok input, $2.50 / MTok
	// output; converted to per-token by dividing by 1_000_000.
	makeKey("gemini", "gemini-2.5-flash"): {
		InputPerToken:  0.30 / 1_000_000.0,
		OutputPerToken: 2.50 / 1_000_000.0,
		SourceCurrency: "USD",
	},
}

// CostTable bundles the built-in prices with a per-instance override layer
// (operator configuration; reserved for a later wiring phase that loads
// overrides from a settings store). An override entry replaces the
// built-in entry for that exact provider/model pair without recompiling.
type CostTable struct {
	overrides map[modelKey]PerTokenPrice
}

// NewCostTable builds a cost table backed by the built-in price map. The
// returned table has no overrides; callers add them via WithOverride.
func NewCostTable() *CostTable {
	return &CostTable{overrides: map[modelKey]PerTokenPrice{}}
}

// WithOverride registers (or replaces) the price for a provider/model
// pair. The override takes precedence over the built-in entry on every
// subsequent Lookup. Returns the receiver to support chained construction.
func (t *CostTable) WithOverride(provider, model string, price PerTokenPrice) *CostTable {
	t.overrides[makeKey(provider, model)] = price
	return t
}

// Lookup returns the price for the given provider/model pair. The
// override layer is consulted first; if no override is registered, the
// built-in table is consulted. The second return value reports whether a
// price was found — an unknown pair returns (zero, false) and the
// recorder writes a row with zero estimated cost and a warning.
func (t *CostTable) Lookup(provider, model string) (PerTokenPrice, bool) {
	key := makeKey(provider, model)
	if p, ok := t.overrides[key]; ok {
		return p, true
	}
	p, ok := builtinPrices[key]
	return p, ok
}

// EstimateCostNano computes the per-call estimated cost in integer
// nano-units of the configured currency, given the provider/model pair,
// token counts, and the USD-to-configured FX rate.
//
//   - When the source-currency price is in USD and the configured
//     currency equals USD, the FX rate is implicitly 1.0 and conversion
//     is a no-op (the caller may pass 1.0 or 0; 0 is interpreted as 1.0
//     to keep the no-op case ergonomic).
//   - When the source is USD and the configured currency differs, the
//     FX rate must be positive; a non-positive rate causes the function
//     to return (0, false) so the recorder records a zero-cost row
//     rather than a silently-misdenominated one.
//   - Source currencies other than USD are not supported in Stream G
//     phase G2 (no provider in the launch set quotes prices outside
//     USD); a non-USD source returns (0, false).
//
// The second return value reports whether the computation produced a
// valid (non-error) cost.
func EstimateCostNano(price PerTokenPrice, inputTokens, outputTokens int, usdToConfiguredRate float64) (int64, bool) {
	if !strings.EqualFold(price.SourceCurrency, "USD") {
		return 0, false
	}
	rate := usdToConfiguredRate
	if rate == 0 {
		rate = 1.0
	}
	if rate < 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		return 0, false
	}
	costSource := float64(inputTokens)*price.InputPerToken + float64(outputTokens)*price.OutputPerToken
	costConfigured := costSource * rate
	// Scale to integer nano-units. Rounding to the nearest integer makes
	// tiny per-call costs reproducible across SQLite and Postgres
	// (integer SUM is exact in both engines once we are on the integer
	// representation).
	scaled := math.Round(costConfigured * float64(llm.CostNanoUnitsPerUnit))
	// Guard against overflow on absurd inputs; clamp at int64 max.
	if scaled > float64(math.MaxInt64) {
		return math.MaxInt64, true
	}
	if scaled < 0 {
		// Negative tokens / negative rate already excluded above, but
		// keep the floor as defence-in-depth so a corrupt row never
		// reaches the database.
		return 0, true
	}
	return int64(scaled), true
}
