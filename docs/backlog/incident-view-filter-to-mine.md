# Backlog — "Filter to mine" in the incident view

Status: **deferred — ships only if explicitly chosen.** Not a numbered phase of
the sessions-view split (`docs/reference/DESIGN-SESSIONS-VIEW.md` §3). Not launch-blocking.

## Context

The sessions-view split (P1) renders the incident view as clusters: a master
(`type = 'incident'`) grouped with its linked children (`type = 'default'` with
`linked_incident_id` pointing at the master). The two views' default lists are
all-owners (newest-first). P2 adds sort-by-date and keyword-filter applied to
master rows.

A "filter to mine" toggle for the incident view is intentionally NOT in that
plan. It carries an unresolved rule that must be decided before it can ship.

## The open question

What makes an incident cluster **"mine"**?

- **Master-ownership.** A cluster is mine iff the master's `creator_principal`
  is me.
- **Any-participant.** A cluster is mine iff I own *any* session in the cluster
  (the master or any linked child).

These genuinely diverge in the live model:

- Declare is promote-in-place: it keeps the promoted session's original
  `creator_principal` while attaching the *declarer* as captain, so a master's
  owner and the incident's captain may differ
  (`internal/sessionmodel/repository.go`, `DeclareIncidentRegime` doc; §12.3 of
  `docs/reference/DESIGN-CHAT-SESSIONS.md`).
- Linked children are independent sessions that may be owned by yet other
  principals (`linked_incident_id` is a participation pointer, a fact, not a
  type — `internal/sessionmodel/types.go:50`).

So "mine" by master-ownership and "mine" by any-participant can select
different clusters for the same user.

## Decision needed before building

Pick one reading (or make it configurable). The cluster-membership data the
filter would key on already exists in the P0 read model
(`creator_principal` per row, `linked_incident_id`, the `incident_involved`
flag) — this is a UI + query-predicate feature, not a schema change.
