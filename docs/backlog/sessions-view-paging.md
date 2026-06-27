# Backlog — P3: paging for the sessions two-view split

Status: **deferred — P3 of the sessions-view split** (`docs/reference/DESIGN-SESSIONS-VIEW.md`
§2). Not pressing: the unpaged capped top-N (`LIMIT 50`, newest-activity-first)
is adequate until real session volume arrives. Not launch-blocking.

## Context

The Conversations and Incidents views render from a single unpaged query — a
capped top-N (`LIMIT 50`, no `OFFSET`/cursor,
`internal/sessionmodel/repository.go:532`) ordered `last_activity_at DESC`. P1
groups that list into conversations + incident clusters client-side; P2 added
client-side keyword-filter + sort over the grouped output. Both are correct
**only because the whole list is already on the client**.

When real volume arrives, the top-N cap stops being adequate and per-tab,
per-row paging is needed.

## Hard dependency — P2's controls MUST move server-side

P2's sort/filter are client-side pure functions (`ui/src/lib/sessionFilterSort.ts`).
They are correct only while unpaged: a client-side filter/sort over a single
page would filter/sort **only that page**, not the whole history — silently
wrong. So P3 is two bodies of work that ship together:

1. Introduce paging (the open decision below).
2. Move keyword-filter + sort **server-side**, into the list query, preserving
   the exact P2 semantics (title-only case-insensitive substring; cluster-level
   atomic filter; newest/oldest-activity + title-A–Z sort with masters as the
   sort unit and children riding along). See §2.1 of the design doc for the
   semantics to reproduce.

## The open architecture decision — the paging unit

How is a "page" measured when the list contains incident clusters (a master +
its children) that must stay atomic (§1.5)?

- **Option A — page flat rows, group within a page.** The server pages flat
  session rows, each carrying a stable group key (its master id). The client
  groups within a page. Requires a **cluster-no-split ordering rule**: the sort
  must guarantee a cluster's master and all its children fall within the same
  page boundary, or a cluster could split across two pages.
  - *Trade:* simpler server-side query, but needs the no-split ordering
    constraint and the client still assembles clusters per page.
- **Option B — page incident-clusters as atomic units.** The server treats a
  master + its children as **one pageable item**: a cluster is one unit of the
  page, returned whole.
  - *Trade:* keeps clusters atomic by construction (no split possible), but
    needs a **group-aware paged query** (page over masters, attach children) —
    more server-side machinery.

P0's read model was deliberately built to support **either** resolution:
children already ride in the master row's payload (§1.5), the predicate and
linked-master title are on the list projection, and the master row is the
natural paging unit regardless of how children are counted. Pick A or B with
real volume/UX constraints then in hand.

## Not in scope here

The "filter to mine" cross-owner rule is tracked separately
(`docs/backlog/incident-view-filter-to-mine.md`) and is orthogonal to paging.
