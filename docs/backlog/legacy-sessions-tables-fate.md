# Backlog — Fate of the legacy `sessions` / `session_messages` tables

Status: open — re-decide; the former reason for retaining them is void.
Priority: next

Re-filed from [`learn-from-sessions-fate`](done/learn-from-sessions-fate.md) when that
item closed under `node-notes-filing`. Its knowledge-extraction half was answered by the
node-anchored notes design (D-0161); **this half was not**, and closing the item without
re-filing it would have buried it. The priority is carried over from that item unchanged.

## The question

The legacy migration-001 `sessions` / `session_messages` tables are frozen: no live
writer, no delete path, one dormant reader. They were held back from the B001 sessions
consolidation for one stated reason — they were the learn-from-sessions feature's only
data source and that feature was going to be revived. D-0113 voided that reason, and the
notes design does not revive it: notes read neither table, and any future session-derived
extraction must read the **live** `agent_sessions` / `chat_messages` store.

So the retention constraint has no justification on record. Either:

- something else justifies retaining them (audit, history, a future consumer) — record
  that reason here; or
- nothing does, and the B001 nodes blocked on this question
  ([DESIGN-CHAT-SESSIONS](../reference/DESIGN-CHAT-SESSIONS.md) §7 step 4, "repoint … or
  keep a thin legacy read-path"; §7 step 5, "remove the unfiltered legacy endpoints") are
  unblocked, and the tables become a drop candidate in their own session.

Do not treat the tables as droppable on the strength of this file alone — that is a B001
decision with its own scope. Until it is made, treat them as retained-but-frozen, as
[`db-retention-story`](db-retention-story.md) § 4 does.

## Pointers

- Prior history: [`done/learn-from-sessions-fate.md`](done/learn-from-sessions-fate.md),
  D-0113 and D-0161 in [`docs/project/DECISIONS.md`](../project/DECISIONS.md).
- `internal/store/migrations/025_session_schema_rewrite.up.sql:31` still names
  `docs/backlog/learn-from-sessions-fate.md` in a comment; that path now resolves under
  `done/`.
