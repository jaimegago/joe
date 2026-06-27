# Backlog — Fate of the learn-from-sessions (knowledge extraction) feature

Status: **decided — deferred to a future feature.** The fate is no longer an open
decision. The feature will be rewired and governed as a future feature, not
retired: formal retirement and deletion of `internal/knowledge/learning/` is off
the table. Because the feature will be revived, its only data source — the legacy
`sessions` / `session_messages` tables — must be **retained, not dropped**.

## Context

The learn-from-sessions feature (`internal/knowledge/learning/extractor.go`) is
dormant and orphaned: nothing constructs or calls it, it reads the legacy
`session_messages` table rather than the live `agent_sessions`/`chat_messages`
store, and its write path threads no principal, consults no accessor/RBAC seam,
and writes no audit row. Full current-state analysis with `file:line` citations:
[docs/reference/learn-from-sessions-current-state.md](../reference/learn-from-sessions-current-state.md).

The feature's *only* data source is the legacy `sessions` / `session_messages`
tables (migration 001). The B001 sessions consolidation
([docs/reference/DESIGN-CHAT-SESSIONS.md](../reference/DESIGN-CHAT-SESSIONS.md) §12, and §7 step 4
which names the "knowledge-extraction consumers" as one of the legacy consumers
that must be repointed or dropped) is the consolidation context. The decision
recorded below settles how the two interact.

## Decision

The fate is **decided: the feature is deferred to a future feature, not
retired.** When the future feature is built, the extractor will be rewired and
governed rather than deleted. Formal retirement and deletion of
`internal/knowledge/learning/` is off the table.

### Chosen direction (when the future feature is built)

- Repoint the extractor at the **live** `agent_sessions`/`chat_messages` store
  (replace the `store.Sessions.GetMessages` read against the legacy
  `session_messages` table).
- Route the write through the **governed accessor seam** with a **real
  principal** and an **audit row** (instead of the current bare
  `knowledge.Service.UpsertSynced` call that threads no principal and writes no
  audit).
- **Resolve the tier contradiction** (comments say `derived`/Tier 3;
  `UpsertSynced` forces `synced`/Tier 2) and set the intended tier explicitly.
- Add an explicit **invocation path** (e.g. a post-session hook or a background
  pass) — there is none today.
- Add a **config switch defaulting off** — no config field, flag, or env var
  governs the feature today.

## Hard constraint for the B001 sessions consolidation

**The legacy `sessions` and `session_messages` tables must NOT be dropped.** They
remain this feature's data source until the rewire (above) repoints the extractor
at the live store. Retaining them is a precondition of reviving the feature, which
is now the chosen direction.

Any B001 node that would retire the legacy `sessions` / `session_messages` tables
(e.g. DESIGN-CHAT-SESSIONS §7 step 4 "repoint ... or keep a thin legacy
read-path"; §7 step 5 "remove the unfiltered legacy endpoints") is **blocked** on
either this future feature being built (extractor repointed at the live store) or
the feature being **explicitly re-killed** (a deliberate reversal of this
decision). Absent one of those, the legacy tables stay.

## Pointers

- Current-state record (every claim cited `file:line`):
  [docs/reference/learn-from-sessions-current-state.md](../reference/learn-from-sessions-current-state.md).
- Consolidation context:
  [docs/reference/DESIGN-CHAT-SESSIONS.md](../reference/DESIGN-CHAT-SESSIONS.md) §12.
