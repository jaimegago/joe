package llmusage_test

import (
	"testing"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/llmusage"
)

// TestEstimateCostNano_SameCurrencyAsSource is the arithmetic-product
// case the prompt calls out: a known model under a configured currency
// equal to the source currency records the literal token×price product
// scaled to nano-units, with no FX conversion. We use a hand-picked
// price (input 2/MTok, output 5/MTok) so the arithmetic stays
// inspectable.
func TestEstimateCostNano_SameCurrencyAsSource(t *testing.T) {
	price := llmusage.PerTokenPrice{
		InputPerToken:  2.0 / 1_000_000.0,
		OutputPerToken: 5.0 / 1_000_000.0,
		SourceCurrency: "USD",
	}

	// 10_000 input * (2 / 1e6) = 0.02 USD
	// 4_000 output * (5 / 1e6) = 0.02 USD
	// total                    = 0.04 USD
	// nano-units                = 0.04 * 1e9 = 40_000_000
	got, ok := llmusage.EstimateCostNano(price, 10_000, 4_000, 1.0)
	if !ok {
		t.Fatalf("EstimateCostNano returned ok=false; expected success")
	}
	want := int64(40_000_000)
	if got != want {
		t.Errorf("EstimateCostNano = %d nano-units, want %d", got, want)
	}

	// A zero FX rate must be treated as 1.0 (no-op) so callers that
	// never set the field for USD configurations still get correct
	// arithmetic.
	got2, ok := llmusage.EstimateCostNano(price, 10_000, 4_000, 0.0)
	if !ok {
		t.Fatalf("EstimateCostNano(fx=0) returned ok=false")
	}
	if got2 != want {
		t.Errorf("EstimateCostNano(fx=0) = %d, want %d (zero FX must be treated as 1.0)", got2, want)
	}
}

// TestEstimateCostNano_DifferingCurrencyAppliesFX is the FX-applied
// branch: the same price + token counts under a configured currency
// that differs from the source produces the source product scaled by
// the FX rate, then expressed in nano-units.
func TestEstimateCostNano_DifferingCurrencyAppliesFX(t *testing.T) {
	price := llmusage.PerTokenPrice{
		InputPerToken:  2.0 / 1_000_000.0,
		OutputPerToken: 5.0 / 1_000_000.0,
		SourceCurrency: "USD",
	}
	// Source product = 0.04 USD (same arithmetic as the same-currency
	// test). FX rate 0.9 USD->EUR ⇒ 0.036 EUR ⇒ 36_000_000 nano-EUR.
	got, ok := llmusage.EstimateCostNano(price, 10_000, 4_000, 0.9)
	if !ok {
		t.Fatalf("EstimateCostNano returned ok=false")
	}
	want := int64(36_000_000)
	if got != want {
		t.Errorf("EstimateCostNano (FX=0.9) = %d nano-units, want %d", got, want)
	}
}

// TestEstimateCostNano_NonUSDSourceRefuses guards the Stream-G-G2
// scope fence: only USD source currencies are priced this phase. A
// non-USD source returns (0, false) so the recorder records a zero
// row and warns rather than silently misdenominating.
func TestEstimateCostNano_NonUSDSourceRefuses(t *testing.T) {
	price := llmusage.PerTokenPrice{
		InputPerToken:  1.0 / 1_000_000.0,
		OutputPerToken: 1.0 / 1_000_000.0,
		SourceCurrency: "EUR",
	}
	got, ok := llmusage.EstimateCostNano(price, 1_000, 1_000, 1.0)
	if ok {
		t.Errorf("EstimateCostNano(non-USD source) ok=true; want false")
	}
	if got != 0 {
		t.Errorf("EstimateCostNano(non-USD source) = %d; want 0", got)
	}
}

// TestEstimateCostNano_InvalidFXRefuses — a non-positive or NaN/Inf FX
// rate is treated the same way as an unpriced model: zero row, no
// silent misdenomination.
func TestEstimateCostNano_InvalidFXRefuses(t *testing.T) {
	price := llmusage.PerTokenPrice{
		InputPerToken:  1.0 / 1_000_000.0,
		OutputPerToken: 1.0 / 1_000_000.0,
		SourceCurrency: "USD",
	}
	if _, ok := llmusage.EstimateCostNano(price, 1, 1, -0.5); ok {
		t.Errorf("EstimateCostNano(fx=-0.5) ok=true; want false")
	}
}

// TestNewCostTable_BuiltinClaudeSonnet4 verifies the launch built-in
// price for claude-sonnet-4-20250514 (sourced from Anthropic's pricing
// page) is wired into the table. The literal value asserts the
// per-token product matches the published $3 / MTok input, $15 / MTok
// output exactly so a future careless edit of the price table fails
// this test immediately.
func TestNewCostTable_BuiltinClaudeSonnet4(t *testing.T) {
	tbl := llmusage.NewCostTable()
	p, ok := tbl.Lookup("claude", "claude-sonnet-4-20250514")
	if !ok {
		t.Fatalf("built-in price missing for claude/claude-sonnet-4-20250514")
	}
	if p.SourceCurrency != "USD" {
		t.Errorf("source currency = %q, want USD", p.SourceCurrency)
	}
	if want := 3.0 / 1_000_000.0; p.InputPerToken != want {
		t.Errorf("input per-token = %g, want %g", p.InputPerToken, want)
	}
	if want := 15.0 / 1_000_000.0; p.OutputPerToken != want {
		t.Errorf("output per-token = %g, want %g", p.OutputPerToken, want)
	}
}

// TestNewCostTable_BuiltinGemini25Flash — same assertion for the
// second launch model. Gemini 2.5 Flash is the cheap-end Google
// model; its presence here means a Gemini deployment is priced
// correctly out of the box.
func TestNewCostTable_BuiltinGemini25Flash(t *testing.T) {
	tbl := llmusage.NewCostTable()
	p, ok := tbl.Lookup("gemini", "gemini-2.5-flash")
	if !ok {
		t.Fatalf("built-in price missing for gemini/gemini-2.5-flash")
	}
	if p.SourceCurrency != "USD" {
		t.Errorf("source currency = %q, want USD", p.SourceCurrency)
	}
	if want := 0.30 / 1_000_000.0; p.InputPerToken != want {
		t.Errorf("input per-token = %g, want %g", p.InputPerToken, want)
	}
	if want := 2.50 / 1_000_000.0; p.OutputPerToken != want {
		t.Errorf("output per-token = %g, want %g", p.OutputPerToken, want)
	}
}

// TestCostTable_OverrideReplacesBuiltin is the operator-override path:
// a registered override for a built-in pair must replace the built-in
// entry on every Lookup. This is what lets an operator correct an out-
// of-date built-in without recompiling.
func TestCostTable_OverrideReplacesBuiltin(t *testing.T) {
	override := llmusage.PerTokenPrice{
		InputPerToken:  100.0 / 1_000_000.0,
		OutputPerToken: 200.0 / 1_000_000.0,
		SourceCurrency: "USD",
	}
	tbl := llmusage.NewCostTable().WithOverride("claude", "claude-sonnet-4-20250514", override)
	p, ok := tbl.Lookup("claude", "claude-sonnet-4-20250514")
	if !ok {
		t.Fatalf("Lookup missing after override")
	}
	if p != override {
		t.Errorf("Lookup = %+v, want override %+v", p, override)
	}
}

// TestCostTable_UnknownModelReturnsFalse — the recorder branches on
// the second return value to decide whether to emit a warning, so the
// false return is load-bearing.
func TestCostTable_UnknownModelReturnsFalse(t *testing.T) {
	tbl := llmusage.NewCostTable()
	if _, ok := tbl.Lookup("madeup-provider", "made-up-model-7b"); ok {
		t.Errorf("Lookup(unknown) ok=true; want false")
	}
}

// TestCostNanoUnitsPerUnit_MatchesLLMConstant — sanity check that the
// nano-unit scale used in cost arithmetic is the same constant
// declared in internal/llm. If the schema column ever changes scale,
// this test fails next to the cost code so the discrepancy is
// caught here, not in a downstream cost-window query.
func TestCostNanoUnitsPerUnit_MatchesLLMConstant(t *testing.T) {
	if llm.CostNanoUnitsPerUnit != 1_000_000_000 {
		t.Errorf("llm.CostNanoUnitsPerUnit = %d, want 1_000_000_000", llm.CostNanoUnitsPerUnit)
	}
}
