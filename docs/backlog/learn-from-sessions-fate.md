# Backlog — Fate of the learn-from-sessions (knowledge extraction) feature

Status: open — **the prior "decided" disposition is void**, see below. Re-decide.
Priority: next

## Why this reopened (session knowledge-store-prune, D-0113)

This item previously recorded a settled decision: the extractor would be **rewired and
governed as a future feature, not retired**, and formal deletion of
`internal/knowledge/learning/` was "off the table". Both halves of that decision's premise
are now gone:

- **The extractor package no longer exists.** `internal/knowledge/learning/` was already
  absent from the tree before this session — the file's `file:line` citations point at
  code that had been deleted earlier, so the item was describing something that was not
  there.
- **The store it wrote into no longer exists.** D-0113 deleted the whole knowledge
  subsystem — `knowledge.Service` and `UpsertSynced`, the curated/synced/derived tier
  taxonomy, the `knowledge_entries` table (dropped by migration 031), and semantic search.
  Every element of the chosen direction below refers to something deleted: there is no
  `UpsertSynced` to replace, no tier contradiction to resolve, and no tier to write into.

So the decision "revive it" cannot stand as written — there is nothing to revive it *into*.
This is not a reversal to "kill it"; it is the observation that the decision was made
against a world that no longer exists. Re-decide against whatever a knowledge-store v2
turns out to be, or against no store at all.

## The live question this leaves: the legacy sessions tables

The load-bearing consequence of the old decision was a **hard constraint on the B001
sessions consolidation**: the legacy `sessions` / `session_messages` tables (migration 001)
must not be dropped, *because* they were this feature's only data source and the feature
was going to be revived.

**That justification is now gone, and the constraint must be re-decided rather than
silently inherited or silently dropped.** Nothing in the D-0113 prune touched those tables,
so the status quo is unchanged and no data was lost — but the reason they were being kept
has evaporated. Either:

- something else justifies retaining them (audit, history, a future consumer) — then record
  that reason here, because it is no longer this feature's reason; or
- nothing does, and the B001 nodes that were blocked on this item
  (DESIGN-CHAT-SESSIONS §7 step 4 "repoint … or keep a thin legacy read-path"; §7 step 5
  "remove the unfiltered legacy endpoints") are unblocked, and the tables become a drop
  candidate in their own session.

Do not treat the tables as droppable on the strength of this file alone — that is a B001
decision with its own scope. This item only records that its former blocker no longer
applies.

## What a v2 would have to answer

If a knowledge-store v2 is ever designed and session-derived extraction is wanted:

- it needs a governed write path — the old one threaded no principal, consulted no
  accessor/RBAC seam, and wrote no audit row, which was the substance of the original
  complaint and remains the bar;
- it needs an invocation path (there was never one — nothing constructed or called the
  extractor);
- it needs a config switch defaulting off;
- it must read the **live** `agent_sessions` / `chat_messages` store, not the legacy tables;
- and it inherits the D-0110 constraint that LLM-derived inferences may never enter the
  infrastructure graph — the derived tier that was to hold them is gone, so v2 must decide
  their home before that work starts.

## Pointers

- Consolidation context:
  [docs/reference/DESIGN-CHAT-SESSIONS.md](../reference/DESIGN-CHAT-SESSIONS.md) §12.
- The prune that voided this item's premise: D-0113 in
  [docs/project/DECISIONS.md](../project/DECISIONS.md), and
  [docs/backlog/done/knowledge-store-prune.md](done/knowledge-store-prune.md).
- The former current-state record
  (`docs/reference/learn-from-sessions-current-state.md`) describes deleted code; read it as
  history, not as a description of the tree.
