# Capability and price tables cover two models, so every other model is a 100,000-token unknown

Status: open
Priority: later

`internal/llmusage/capabilities.go` and `internal/llmusage/cost.go` carry
`builtinCapabilities` and `builtinPrices` rows for exactly two models —
`gemini-2.5-flash` and `claude-sonnet-4-20250514`. Any other model joe is
pointed at is treated as unknown, which means a **100,000-token context window**
rather than the model's real one, and no price.

The agentic path reads the capabilities table's max-output and passes it through
`llm.ChatRequest.MaxTokens`, so an unknown model is not merely unpriced — it is
budgeted against a window that is not its own.

## Why it is filed now

It was measured as **inert for the diagnostic-accuracy corpus and not inert in
general**: that corpus peaked at 19,304 input tokens, well inside the 100,000
placeholder, so nothing surfaced. The measurement is joe-pm
`threads/da-category-model-tier.md`.

It became reachable on 2026-08-26. Until then joe could run exactly one model
with tools, and that model has a row; `joe-pm/threads/gemini-sdk-migration.md`
migrated the Gemini adapter to `google.golang.org/genai`, so pro-tier Gemini
models now work — and none of them has a row.

## Open question

Whether the tables should be extended per model as models are adopted, or
whether an unknown model should stop defaulting silently — a 100,000-token
assumption presented as a measurement is the part that bites, not the absence of
a row. `docs/backlog/default-model-constants.md` is adjacent: it is the same
class of defect one layer up, where the model *name* is an internal constant
nothing prompts a review of.
