package llmusage

// ModelCapabilities describes the prompt-assembly limits of one
// provider/model pair: the total context window and the default maximum
// output the agentic path reserves and caps requests at.
//
// Both values are in tokens. ContextWindowTokens is the provider-published
// total window (input + output); MaxOutputTokens is the conservative
// per-request output cap Joe sets on the agentic path so a request never
// relies on an implicit provider default (the Claude adapter defaulted to
// 4096, the Gemini adapter set no limit at all). Holding the seeded default
// at 4096 means wiring this table can only make behaviour MORE bounded,
// never less.
type ModelCapabilities struct {
	ContextWindowTokens int
	MaxOutputTokens     int
}

// Conservative defaults returned for any provider/model pair NOT present in
// builtinCapabilities. The window is deliberately small and the output cap
// matches the Claude adapter's prior default — an unknown model is treated
// pessimistically rather than guessed optimistically, so a model added to
// the catalogue without a capabilities entry still prunes aggressively and
// never over-promises context room.
const (
	defaultContextWindowTokens = 100000
	defaultMaxOutputTokens     = 4096
)

// builtinCapabilities is the compile-time capabilities table, keyed by the
// SAME {provider, model} composite as builtinPrices (see cost.go). Every
// shipped model has an entry; each cites the source doc URL and capture
// date next to it exactly like the price rows so a future reader can detect
// drift without re-mining commit history.
//
// Add an entry the same way as a price row: provider lowercased, model name
// exactly as the adapter emits it, window + max-output in tokens.
var builtinCapabilities = map[modelKey]ModelCapabilities{
	// Claude Sonnet 4 — Anthropic's published default context window is
	// 200,000 tokens. (A 1M-token window exists but is an opt-in public
	// beta on the Developer Platform, not the default; 200k is the correct
	// conservative figure for the always-on path.)
	// Source: https://platform.claude.com/docs/en/build-with-claude/context-windows
	// Captured: 2026-06-03. MaxOutput held at 4096 to match the Claude
	// adapter's prior implicit default (claude/constants.go defaultMaxTokens).
	makeKey("claude", "claude-sonnet-4-20250514"): {
		ContextWindowTokens: 200000,
		MaxOutputTokens:     4096,
	},
	// Gemini 2.5 Flash — Google's published input token limit is 1,048,576.
	// Source: https://ai.google.dev/gemini-api/docs/models
	// Captured: 2026-06-03. MaxOutput held at 4096 to match the agentic
	// path's prior Claude default — the Gemini model supports a far larger
	// output ceiling (~65k) but Joe's agentic turns never need it, and a
	// uniform conservative cap keeps the budget arithmetic identical across
	// providers.
	makeKey("gemini", "gemini-2.5-flash"): {
		ContextWindowTokens: 1048576,
		MaxOutputTokens:     4096,
	},
}

// CapabilitiesTable bundles the built-in capabilities with a per-instance
// override layer, mirroring CostTable. The override layer is provided for
// symmetry with the cost table and a future operator-configuration phase;
// it is intentionally left unwired today (no production caller registers an
// override), exactly as the cost table's override layer is.
type CapabilitiesTable struct {
	overrides map[modelKey]ModelCapabilities
}

// NewCapabilitiesTable builds a capabilities table backed by the built-in
// map with no overrides.
func NewCapabilitiesTable() *CapabilitiesTable {
	return &CapabilitiesTable{overrides: map[modelKey]ModelCapabilities{}}
}

// WithOverride registers (or replaces) the capabilities for a
// provider/model pair. The override takes precedence over the built-in
// entry on every subsequent Lookup. Returns the receiver for chaining.
func (t *CapabilitiesTable) WithOverride(provider, model string, caps ModelCapabilities) *CapabilitiesTable {
	t.overrides[makeKey(provider, model)] = caps
	return t
}

// Lookup returns the capabilities for the given provider/model pair. The
// override layer is consulted first, then the built-in table. The second
// return value reports whether an entry was found — an unknown pair returns
// (zero, false); callers wanting the conservative default use the
// package-level LookupCapabilities helper instead.
func (t *CapabilitiesTable) Lookup(provider, model string) (ModelCapabilities, bool) {
	key := makeKey(provider, model)
	if c, ok := t.overrides[key]; ok {
		return c, true
	}
	c, ok := builtinCapabilities[key]
	return c, ok
}

// LookupCapabilities returns the capabilities for the given provider/model
// pair from the built-in table, falling back to the conservative default
// (small window, 4096 output) for an unknown pair. This is the lookup the
// agentic path uses: it must NEVER guess an optimistic window for a model
// it does not recognise, so an unknown model is bounded by the small
// default window and the same 4096 output cap.
func LookupCapabilities(provider, model string) ModelCapabilities {
	if c, ok := builtinCapabilities[makeKey(provider, model)]; ok {
		return c
	}
	return ModelCapabilities{
		ContextWindowTokens: defaultContextWindowTokens,
		MaxOutputTokens:     defaultMaxOutputTokens,
	}
}
