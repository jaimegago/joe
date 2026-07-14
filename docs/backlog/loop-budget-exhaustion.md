# Backlog — Loop budget-exhaustion follow-ups (deferred from loop-budget-exhaustion / D-0096–D-0100)

Status: open

The loop-budget-exhaustion session (D-0100..D-0096) made the agentic loop's
iteration-cap hit a soft stop: on exhaustion `Agent.Run` makes one tool-less
forced-synthesis call, a successful synthesis **completes** with `stop_reason`
`max_iterations` (persisted + surfaced with an amber UI notice), a failed one
falls through to the retained `ErrMaxIterations` / `max_iterations_reached`
status, and exactly one `llm_max_iterations_reached` audit row is written per
hit. `DefaultMaxIterations` was raised to 20 and the duplicated resolution
consolidated into `resolveMaxIterations`. This file carries what that session
deliberately deferred.

## 1. Deployment-level admin knob for the iteration cap

Today the cap is `DefaultMaxIterations` (a compile-time constant) with a
per-request override on `taskConfig.MaxIterations`. There is **no operator-facing
deployment-level setting** — an admin cannot raise or lower the default for an
install without a rebuild. The follow-up is a persisted, admin-gated,
runtime-read cap that `resolveMaxIterations` consults as the default (per-request
override still winning), mirroring how the session token ceiling moved from a
static value to a storage-backed limit. Scope includes the admin REST surface,
an audited operator-change action (sibling to `ActionLLMSetRunawayCeiling`), and
a boot/runtime resolution seam.

## 2. Sibling termination paths adopting the synthesis seam

The forced-synthesis seam (`synthesizeFinalAnswer`) currently fires only for the
iteration-cap path. The **session token-ceiling** termination
(`ErrSessionTokenCeiling` → `runaway_terminated`) and the **context-overflow**
termination (`llm.ErrContextOverflow` → `context_overflow`) are still hard-fails
that surface no answer, even when substantial evidence was gathered. They could
reuse the same seam to synthesize a partial answer before terminating.

**Blocker (token-ceiling):** the ceiling path terminates precisely because the
session's token budget is exhausted, so a synthesis call — itself an LLM call
that consumes tokens — needs a **reserved headroom design**: a token reserve set
aside up front (or a ceiling defined to exclude a synthesis allowance) so the
synthesis call cannot itself breach the very limit that triggered termination.
That headroom design is the prerequisite and is not yet done. The context-overflow
path has an analogous constraint (the prompt already overflows the window, so
synthesis would need aggressive pruning to fit) and is likewise deferred.

## 3. `stop_reason` adoption by the runaway and overflow paths

`stop_reason` was introduced as a generic short-string enum (first value
`max_iterations`) precisely so the sibling terminations can adopt it. Once item 2
lands (or independently, for the terminal-without-synthesis case), the
`runaway_terminated` and `context_overflow` paths should populate `stop_reason`
with their own sibling values (e.g. `token_ceiling`, `context_overflow`) so the
UI and persisted transcript can distinguish *why* a turn was truncated rather
than inferring it from the terminal status alone. This is additive to the field,
the migration, and the UI notice already in place.
