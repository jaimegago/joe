# DESIGN — Sessions View (two-view split: incidents vs conversations)

Status: **P0 landed (this node) — derived predicate + read-model projection +
this doc. No UI.** P1–P3 and the deferred "filter to mine" item are planned
below, not built.

This document is a sibling of `docs/DESIGN-CHAT-SESSIONS.md` and inherits its
storage model (migration 025, the §12 clean-room schema). Where this prompt's
prose and the live tree disagreed, **the tree won and this doc records the
tree**; the Phase 1 verification basis (below) is the receipt.

## 1. The model

### 1.1 The load-bearing computed property

The session list splits into two user-facing views by ONE derived predicate,
computed at ONE seam:

```
incident_involved := (linked_incident_id IS NOT NULL) OR (type = 'incident')
```

A session is **incident-involved** if it is either an incident *master*
(`type = 'incident'`) or a *participant* linked to one
(`linked_incident_id` set). Everything else is **incident-free**
(`type = 'default' AND linked_incident_id IS NULL`).

This is a USER-FACING split, not a schema-type split. The schema type domain is
exactly `{default, incident}` (migration 025); "incident-involved" is broader
than `type = 'incident'` because it also pulls in linked `default` children.
The predicate is expressible over the live columns *exactly* — both inputs are
real, indexed columns (see §6.D).

### 1.2 Two views

- **Incident view** = all incident-involved sessions, rendered as **clusters**.
  Each cluster is one master (`type = 'incident'`) grouped with its linked
  children (`type = 'default'` whose `linked_incident_id` points at that
  master).
- **Conversation view** = incident-free sessions only
  (`type = 'default' AND linked_incident_id IS NULL`).

### 1.3 Membership consequence (stated explicitly)

- A **linked** session appears ONLY in the incident view. It is pulled OUT of
  the conversation list — to the user a linked session is part of the incident
  world, not a loose conversation.
- A **master** appears ONLY in the incident view.
- The **conversation view is incident-free** by construction.

There is no session that appears in both views; `incident_involved` partitions
the set.

### 1.4 A resolved incident stays in the incident view — permanently

Terminal resolution (`incident_state` → `resolved` / `reviewed`) does **not**
migrate the cluster back to conversations. The link pointer survives resolution
(it is severed only by a hard purge — `ON DELETE SET NULL`, §12.4), and the
master keeps `type = 'incident'`. So a resolved cluster remains in the incident
view forever. **This is intended, not a bug** — recorded here so it is not
later "fixed."

### 1.5 Children ride in their master's row payload

A cluster is **one list row**: the master is the row, and its children are part
of that row's payload (not independent top-level rows). Consequences:

- A cluster cannot split across a future page boundary — the master row carries
  its children whole.
- List operations (sort, filter, paging) in later phases target **master
  rows**, never children. Children are fixed detail under their master.

This is the property that makes the P3 paging phase forward-compatible: the
paging unit is the master row, regardless of how children are counted.

## 2. Phasing (the agreed plan)

- **P0 — this node.** The derived predicate, the additive read-model
  projection P1 consumes, and this doc. **No UI.**
- **P1.** The two-view split with **default views only**: newest-first, all
  owners, incident clusters with children grouped. Includes the
  **active-vs-resolved cluster styling distinction** (a resolved cluster reads
  as terminal/dimmed; an active cluster reads as live).
- **P2.** Sort-by-date + keyword-filter, applied **uniformly to both views'
  rows**. Children are fixed detail under their master; **list operations
  target master rows, not children**.
- **P3.** Per-tab, per-row paging. Resolves the deferred paging-unit question
  (is the page measured in master rows, or in master-rows-plus-children?) with
  real constraints then in hand. P0's read model is built so **either**
  resolution works — children already ride in the master row payload (§1.5).

## 3. Deferred (NOT a numbered phase)

**"Filter to mine" in the incident view.** Carries an unresolved cross-owner
rule: is a cluster "mine" by **master-ownership** (the master's
`creator_principal` is me) or by **any-participant** (I own any session in the
cluster)? A master's creator and an incident's captain may already differ
(promote-in-place keeps the original `creator_principal`, §12.3), and linked
children may be owned by yet other principals — so the two readings genuinely
diverge. This ships ONLY if explicitly chosen. Tracked at
`docs/backlog/incident-view-filter-to-mine.md`.

## 4. P0 read-model projection (what this node delivered)

Additive, read-only. No schema change to existing columns, no write-path change,
no change to what sessions exist or how they are created.

Added to the session LIST projection (`webUISession`, `internal/api/webui.go`):

- `incident_involved` (bool, always present) — the §1.1 predicate, computed in
  `sessionToWebUI` from the row's own `type` and `linked_incident_id`. No query
  needed; both inputs are already on the `AgentSession` row.
- `linked_incident_id` (already present) — the master's id on a linked child.
- `linked_incident_title` (string, omitempty) — the master's title on a linked
  child, so the child can NAME and LINK to its master. This closes the
  bare-badge defect (§6.C): the list previously carried only the id. Sourced by
  a self-join (§5), NOT an N+1.

Field names are aligned with the per-id GET (`linked_incident_id`,
`linked_incident_title`) so P1 consumes ONE consistent shape from both surfaces.

## 5. Grouping-data assembly (the children-query-cost decision)

The list query already returns every active session in a single
`LEFT JOIN chat_messages … GROUP BY s.id` query (no N+1 for message counts).
Masters and their children are all already in that one result set, so **P1 can
build clusters in application code from the single existing result with no extra
query**.

For the master *title* on child rows, P0 adds a **self-join** to the same single
query:

```sql
LEFT JOIN agent_sessions p ON p.id = s.linked_incident_id
… SELECT …, p.title AS master_title
```

The join resolves the parent by PRIMARY KEY (`agent_sessions.id`), so it needs
no new index. The reverse direction (a master's children, used by P1 grouping)
is served by the **existing** `idx_agent_sessions_linked_incident`
(`internal/store/migrations/025_session_schema_rewrite.up.sql:94`). **No index
migration was needed** — Phase 1.B confirmed the index already exists. This
self-join replaces the per-id GET's N+1 `GetSession(linked_incident_id)` pattern
for the list surface.

## 6. Phase 1 verification basis (conformed to the tree, 2026-06-22)

### 6.A — list endpoint contract

- Handler: `handleListSessions`, `internal/api/webui.go:339`. Route
  `GET /api/v1/sessions` (`internal/api/webui.go:910`).
- Read model: `webUISession` (`internal/api/webui.go:197`), projected by
  `sessionToWebUI` (`internal/api/webui.go:281`). Fields currently returned:
  `id`, `started_at`, `last_activity_at`, `summary`, `message_count`, `title`,
  `read_only` (always present), `linked_incident_id` (when linked), `type`
  (always), `incident_state` (incident only), `shared_by` (non-owned rows, set
  in the handler at `internal/api/webui.go:376`). `purge_after` only on trashed
  rows. The list did NOT carry `linked_incident_title` before P0.
- **Paging: none.** There is only a `limit` cap (default 20,
  `internal/api/webui.go:345`), emitted as a bare SQL `LIMIT ?` with no `OFFSET`
  and no cursor (`internal/sessionmodel/repository.go:531`, `:582`). The full
  capped top-N is returned in one shot. P3 introduces paging from scratch.
- **Sort:** `ORDER BY s.last_activity_at DESC` (newest activity first), imposed
  in SQL — `internal/sessionmodel/repository.go:530` (mine /
  `ListSessionsByCreator`) and `:581` (team-wide / `ListRecentSessions`). The
  default unfiltered list calls `ListRecentSessions`; `?mine=true` calls
  `ListSessionsByCreator` (`internal/api/webui.go:359`–`363`).
- Per-id GET: `handleGetSession`, `internal/api/webui.go:418`. Same
  `sessionToWebUI` projection, plus `read_only` from the owner check
  (`:448`) and `linked_incident_title` resolved (`:454`–`458`).

### 6.B — master→children query cost

- Index on `agent_sessions.linked_incident_id`: **present** —
  `idx_agent_sessions_linked_incident`,
  `internal/store/migrations/025_session_schema_rewrite.up.sql:94`. (Also
  `idx_agent_sessions_type` at `:93`.)
- Cheapest correct assembly: a **single set query** already returns masters and
  children together; grouping is application-side over that one result. The
  master-title-on-children projection is a **self-join** in the same query
  (parent by PK). **Not N+1**, **no new index**. See §5.

### 6.C — linked-master title resolution (the original defect)

- Per-id GET resolves it via `GetSession(*sess.LinkedIncidentID).Title` →
  `out.LinkedIncidentTitle`, `internal/api/webui.go:454`–`456`. **Confirmed.**
- The LIST projection did NOT carry it: `sessionToWebUI` sets
  `linked_incident_id` (`internal/api/webui.go:289`–`291`) but never
  `linked_incident_title`; the struct comment at `internal/api/webui.go:226`
  explicitly noted it was "Set ONLY on the per-id GET (it would be an N+1 on the
  list projection)." This is the **bare-badge defect** P0 closes (via §5's
  self-join, which is not an N+1). **Confirmed.**

### 6.D — the predicate's two inputs

- `type`: `TEXT NOT NULL`, `CHECK (type IN ('default', 'incident'))` —
  `internal/store/migrations/025_session_schema_rewrite.up.sql:42`, `:56`.
- `linked_incident_id`: `TEXT REFERENCES agent_sessions(id) ON DELETE SET NULL`
  (self-FK) — `internal/store/migrations/025_session_schema_rewrite.up.sql:47`.
- The predicate `linked_incident_id IS NOT NULL OR type = 'incident'` is
  expressible over these live columns **exactly**. **Confirmed.**
