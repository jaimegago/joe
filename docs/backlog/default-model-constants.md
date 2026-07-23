# Per-provider default model constants will go stale like the version literals did

Status: open
Priority: later

`internal/config/constants.go:8-9` and `:14-15` hardcode the per-provider default
models auto-selection falls back to:

```
defaultLLMCurrent    = "claude-sonnet"
defaultLLMModel      = "claude-sonnet-4-20250514"
...
defaultGeminiCurrent = "gemini-flash"
defaultGeminiModel   = "gemini-2.5-flash"
```

Observed live during the `quickstart-corrections` session (2026-07-23): with only
`GEMINI_API_KEY` set, `AutoSelectProvider` (`internal/config/validation.go:38-70`)
lands on `gemini-2.5-flash` with no indication to the operator that this is an
internal constant rather than a considered, current choice — the same
accidental-correctness-expires class of defect that `docs/backlog/done/` (see
`hardcoded-version-literals.md`-style items and D-0137's own note on the two
`0.1.0` literals outside `buildinfo`) already names for version strings.

Per D-0138, no page may name these strings — they are documented as "internal
constants that move without notice" precisely so a future change doesn't falsify
published copy. That's the right call for docs, but it leaves the constants
themselves undermanaged: nothing currently prompts a review when a provider ships a
new default model, and nothing surfaces to the operator, louder than the boot log
line at `validation.go:64`/`:67`, that a specific model version was silently chosen
for them.

## Open question

Should these defaults be:
- release-time-reviewed (a checklist item, like the `RELEASING.md` pre-tag checklist,
  prompting "are these still each provider's recommended default"), or
- surfaced louder at boot (e.g. a startup banner naming the auto-selected model, not
  just a debug/info log line), or
- replaced with a different mechanism entirely (e.g. querying the provider's model
  list at boot, though this adds a network dependency to auto-selection that doesn't
  exist today)?

Not decided here — this item records the observation and the question, not a chosen
fix.
