# Backlog — Attach a former (resolved) incident master as a participant of a new incident

Status: **deferred — post-launch structural feature.** Not a chrome fix and not
launch-blocking. Lands on the same post-launch shelf as the incident-regime /
captain redesign (`docs/security-architecture-direction.md` §8, "the
incident/captain gate is post-launch as a feature").

## Context

A session can today be the master of an incident, or a plain participant linked
to one, but never both — and never across incidents. Two facts in the live tree
fix that:

- **Type model.** `agent_sessions.type` is the two-value domain
  `{default, incident}` (migration 025,
  `internal/store/migrations/025_session_schema_rewrite.up.sql:56`). Incident
  participation is NOT a type — it is the `linked_incident_id` self-pointer (a
  fact, `internal/sessionmodel/types.go:50`).
- **Declare is promote-in-place.** Declaring an incident does not mint a fresh
  master; it promotes an existing `default` session IN PLACE to the incident
  master, keeping its id (`internal/sessionmodel/regime_transitions.go:15`,
  `:24`). The promote UPDATE also CLEARS `linked_incident_id` — required because
  the CHECK forbids an incident row from carrying one (`regime_transitions.go:96`,
  `:107`).

The feature: let a session that was ITSELF an incident master, now resolved, be
attached later as a participant of a DIFFERENT, currently-active incident master
— i.e. give a resolved master a live `linked_incident_id` into another incident.

## Structural blockers

1. **The model forbids it by invariant.** A row with `type = 'incident'` may not
   carry `linked_incident_id`:
   `CHECK (linked_incident_id IS NULL OR type <> 'incident')`
   (`025_session_schema_rewrite.up.sql:63`, carried forward from migration 009).
   A resolved master is still an `incident`-type row (resolve moves
   `incident_state` to `resolved`; it does not change `type`). So participation
   would require one of:
   - **Relaxing the invariant** — permit a row that is simultaneously a resolved
     master AND a linked participant of another incident. This is a real schema
     change (new CHECK semantics) and a new conceptual state.
   - **Adding a demote-to-default transition** — a reverse of promote-in-place
     that flips the row back to `type = 'default'` and erases its master
     identity (its `incident_state`), after which the existing link path applies.
     No such transition exists today; promote-in-place is one-way
     (`regime_transitions.go`), and demotion would have to define what happens to
     the resolved master's own former participants and captain bindings.

2. **It reintroduces an attach affordance onto incident-type rows.** The link
   path is deliberately closed to incident sessions:
   `handleLinkIncident` refuses any `type = 'incident'` session with a 409
   ("an incident session cannot be linked to an incident",
   `internal/api/webui.go:769`–`774`). The current incident-chrome work
   (commit `0695653`, "mark incident session in UI and add resolve flow") is
   moving in the opposite direction — making incident-type rows visibly distinct
   and removing participant-attach affordances from them. This feature would
   re-open exactly that affordance.

3. **It needs new cycle-freedom guards.** `linked_incident_id` is a self-FK on
   `agent_sessions` (`025_session_schema_rewrite.up.sql:47`). Today
   `handleLinkIncident` keeps the graph a shallow star by two guards: link ONLY
   to the single currently-active incident
   (`ActiveIncidentSession`, `webui.go:776`) and never to self
   (`webui.go:785`–`788`). Allowing former masters to link reintroduces the risk
   of chains/cycles (A→B→A across resolved incidents), so the guards must be
   re-proven: link only to the currently active master, never to self, and never
   forming a cycle through any prior linkage.

## Launch-time alternative

A resolved master reaches a new incident by **reference only**: open both
sessions and copy the link between them. No mutable participation, no schema
change, no new transition. This is what ships.

## Post-launch target shape

A dedicated **cross-incident related-incidents pointer**, decoupled from
participant linking — i.e. a separate "this incident relates to that incident"
relation that does NOT overload `linked_incident_id` and does NOT require an
incident row to masquerade as a participant. Participant linking
(`linked_incident_id`) stays default-session-only; relatedness between masters
becomes its own typed relation with its own guards.

## Scope boundary

This entry does NOT cover **Tier-1 re-linking of ordinary `default` sessions
across incidents** — overwriting a default session's `linked_incident_id` from a
resolved incident to an active one. That is a separate, simpler node: the row is
already `default`-type (no CHECK conflict, no demotion needed), so it is purely a
question of whether `LinkSessionToIncident` / `handleLinkIncident`
(`internal/sessionmodel/repository.go:473`, `internal/api/webui.go:739`) should
permit re-pointing an already-linked default session. Track that on its own; it
is not blocked by the structural items above.

## Pointers

- Two-type model + linkage rules: `docs/DESIGN-CHAT-SESSIONS.md` §12.3.
- Promote-in-place declare: `internal/sessionmodel/regime_transitions.go`.
- Link path + incident-row refusal: `internal/api/webui.go` `handleLinkIncident`.
- Post-launch incident/captain shelf:
  `docs/security-architecture-direction.md` §8.
