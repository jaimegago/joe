# Design: First-Class Chat Sessions (Web UI)

> Status: **decisions locked 2026-06-06** — see §10 for the resolved calls and §11
> for the phased plan. **Phase 1 (ownership & isolation), Phase 2 (browse tab +
> titles), Phase 3 (binary private/public sharing), Phase 4 (incident linkage), and
> Phase 5 (shared-sessions discovery) implemented 2026-06-06.** §2–§8 retained as
> rationale.

## 0. Implementation kickoff (read first in a fresh session)

Read this whole doc, then start with **Phase 1 (§11)** — it closes a live cross-user
chat data leak and is independently shippable. Anchors:

- **Current state + leak locations:** §2 (file:line refs into `internal/api/webui.go`,
  the two parallel session models, and the decomposition note that this feature *is* the
  planned "webui migration to the new session model").
- **Locked decisions + access matrix:** §10 — do not relitigate these.
- **Build/test:** `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -s -w .`;
  frontend `npm run lint` / `npm run test` in `ui/`.
- **Manual auth testing:** a local Dex OIDC lives at `~/.joe/dex/` — run
  `~/.joe/dex/run-dex.sh` (Docker), users `alice|bob|carol@example.com`, password
  `password`, alice is bootstrap admin. The server loads `~/.joe/config.yaml`. Use
  Chrome/Firefox (session cookies are `Secure`; Safari rejects them on http://localhost).
- **Scope guard:** Phase 1 touches ownership/isolation only — no browse tab, titles,
  sharing, or incident linkage (Phases 2–4).

## 1. Motivation

Today the Web UI chat treats sessions as a transparent persistence detail. They have
no owner, can't be browsed deliberately, can't be named, and have no relationship to
incident mode. The immediate trigger was a **cross-user data leak** (any logged-in
user can list and read any other user's chat history), but the fix is the front edge
of a larger feature: chat sessions should be first-class objects.

Target capabilities:
- **Owned** — a session belongs to the principal who started it.
- **Stored** — durable, on the authoritative session model (not the legacy table).
- **Browsable** — a dedicated tab to find, open, and manage your sessions.
- **Renamable** — human-editable title (auto-suggested, user-overridable).
- **Incident-aware** — a session can be linked to an active incident.

## 2. Current state (grounding)

Two session models run in parallel (see comparison table in the design discussion):

- **Legacy `sessions` / `session_messages`** (`internal/store/migrations/001_initial.up.sql`).
  No owner column. Consumed by `internal/api/webui.go` (chat list/create/messages),
  the task-stream handler (`internal/api/tasks.go`), and knowledge extraction.
- **New `agent_sessions`** (`internal/store/migrations/009_session_model.up.sql`) +
  `agent_runs`/`run_steps` (`010_run_model.up.sql`). Has `creator_principal`, a
  `type` discriminator (`incident`/`investigation`/`other`), incident lifecycle,
  `linked_incident_id`, captains, and `system_regime`. Exposed at
  `/api/v1/agent-sessions` (`internal/api/sessions.go`) and driven by regime
  declare/resolve + the captain gate.
  - **Superseded by §12 (2026-06-20):** the three-type model
    (`incident`/`investigation`/`other`) collapses to two types
    (`default`/`incident`); incident participation is the `linked_incident_id`
    pointer, not a `type` value, and the `investigation` type is removed. See §12.3.

The leak, precisely:
- `internal/api/webui.go:184` `handleListSessions` → `Sessions.ListRecent(ctx, limit)` — no principal filter.
- `internal/api/webui.go:233` `handleGetSessionMessages` — returns any session's messages by id, no ownership check.
- `internal/api/webui.go:213` `handleCreateSession` — never records the caller as owner.
- Principal **is** available (EdgeAuth sets it in context via `rbac.PrincipalFromContext`); the handlers just ignore it. These paths aren't `sourceID`-keyed, so RBAC zone enforcement doesn't fire on them either.

The decomposition plan already calls for collapsing these: `docs/PHASE-1-DECOMPOSITION.md`
lists "webui migration to the new session model" as explicit post-Phase-1 work, with a
note in `internal/api/sessions.go` that `/agent-sessions` runs parallel to the legacy
`/sessions` route until "the post-Phase-1 webui migration will collapse the two."

## 3. Target architecture (recommended)

Build the feature on `agent_sessions`; retire the legacy `sessions` table for the Web UI.

- A Web UI chat session = `agent_sessions` row with `type='other'` (plain chat) and
  `creator_principal` = the logged-in user.
- Ownership scoping enforced at the store + handler layer: list returns only the
  caller's sessions; get/messages 404 (not 403) when the caller isn't the owner.
- Incident linkage reuses the existing `linked_incident_id` seam (the schema already
  permits a non-incident session to point at an incident session).
- Title is a new human-editable field on `agent_sessions` (see §8.3).
- Message persistence: decision in §8.2 (reuse `run_steps` vs. a lightweight messages
  table keyed to `agent_sessions`).

## 4. Feature breakdown

Maps to the requested scope, plus the implied "and so forth":

1. **Ownership & isolation** (closes the leak). Owner recorded on create; list/get/
   messages scoped to owner. Admin visibility per §8.4.
2. **Storage / model unification.** Web UI chat handlers move from `store.Sessions`
   to the `sessionmodel.Repository`. Legacy rows migrated or left read-only (§8.5).
3. **Sessions browse tab.** New `/sessions` nav item + page: list the user's sessions
   (title, last activity, message count, incident badge), open/rename/delete.
4. **Title editing.** Auto-suggested title from the first user message (LLM or
   heuristic), user-overridable. New `PATCH /api/v1/sessions/{id}`.
5. **Incident-mode linkage.** From a chat session, "attach to current incident"
   (sets `linked_incident_id` / promotes to `type='investigation'`); surface the link
   in the existing `IncidentBanner` and on the session row. Semantics in §8.6.
6. **Lifecycle / housekeeping.** Delete (with confirm), retention class seam, and an
   audit trail for create/rename/delete/link.

## 5. API surface (new / changed)

Unify under `/api/v1/sessions` (the legacy route, now backed by `agent_sessions`), or
keep `/agent-sessions` — naming decision in §8.1.

- `GET    /sessions` — list **caller's** sessions (was: all sessions).
- `POST   /sessions` — create, owner = caller.
- `GET    /sessions/{id}` — single session (owner-checked). *(new)*
- `PATCH  /sessions/{id}` — rename / set title (owner-checked). *(new)*
- `DELETE /sessions/{id}` — delete (owner-checked). *(new)*
- `GET    /sessions/{id}/messages` — owner-checked.
- `POST   /sessions/{id}/link-incident` — attach to active incident. *(new, §8.6)*

## 6. UI surface

- **Sidebar**: add `Sessions` between Chat and Admin (`ui/src/components/layout/Sidebar.tsx`).
- **Sessions page**: list with title, relative last-activity, message count, incident
  badge; row actions open/rename (inline)/delete; "New chat" CTA.
- **Chat page**: rename affordance in the header; incident-link control when regime is
  `incident`; on first message, show the auto-suggested title.
- **Dashboard `RecentSessions`**: label by title instead of `summary ?? 'Session'`.
- **API client/types** (`ui/src/api/chat.ts`, `schemas.ts`): add `title`, and
  `updateSession`/`deleteSession`/`linkIncident`.

## 7. Migration plan

1. Add `title` (and any needed columns) to `agent_sessions` via a new migration.
2. Point webui chat handlers at `sessionmodel.Repository`; record owner on create.
3. Backfill: migrate legacy `sessions`/`session_messages` rows into `agent_sessions`
   (`type='other'`) + the chosen message store, attributing `creator_principal`
   (decision §8.5 — best-effort vs. drop vs. quarantine to an admin).
4. Repoint the task-stream + knowledge-extraction consumers, or keep a thin legacy
   read-path during transition.
5. Remove the unfiltered legacy endpoints once the UI is cut over.

## 8. Open decisions

Each needs a call before implementation.

**8.1 Route namespace.** Keep `/api/v1/sessions` (back it with the new model) vs.
move the UI to `/api/v1/agent-sessions`. *Recommend:* keep `/sessions` for the UI;
fewer client changes, and `/agent-sessions` was only ever a Phase-1 scope-isolation
workaround.

**8.2 Message persistence in the new model.** (a) Reuse `agent_runs`→`run_steps`
(every chat turn is a run; converges with the durable executor and `internal/agentloop`),
vs. (b) a lightweight `session_messages_v2` table keyed to `agent_sessions` (simpler,
preserves flat-message rendering, but doesn't capture tool steps and diverges from the
run model). *Recommend:* (a) as the end-state; (b) acceptable only as a short interim.

**8.3 Title generation.** Auto from first message via LLM vs. heuristic (first ~6
words) vs. none-until-user-types. *Recommend:* heuristic immediately + async LLM
upgrade, always user-overridable.

**8.4 Admin visibility.** Do admins see all users' sessions, or strictly their own?
Privacy vs. oversight. *Recommend:* own-only by default; if oversight is needed, a
separate explicitly-audited admin view rather than widening the default list.

**8.5 Legacy backfill.** Existing legacy sessions have no owner. Options: best-effort
attribute (only the single-user dev data exists today, so likely yours), drop them, or
quarantine to an admin-only bucket. *Recommend:* for a pre-launch dev DB, drop or
quarantine — don't invent owners.

**8.6 Incident-link semantics.** What does "link a chat to an incident" mean?
(a) Pure reference (`linked_incident_id`) for forensics/cascade-expunge; (b) promote
the chat to a `type='investigation'` participant of the incident; (c) the chatter
becomes a captain/participant. *Recommend:* start with (a)+(b); leave captaincy to the
incident-mode UI work.

**8.7 Scope/sequencing.** Ship the ownership fix first (closes the leak) on the new
model, then browse tab + titles, then incident linkage? Or one cut-over? *Recommend:*
phase it — security first.

## 9. Out of scope (for now)

- Captain transfer UI, incident declare/resolve UI (separate incident-mode work).
- Per-principal sharing ("share with user:bob"). Phase 3 ships binary
  private/public only; granular sharing remains future (§10).
- Cross-session search.
- Postgres-specific concerns.

## 10. Decisions (locked 2026-06-06)

- **Storage (§8.2):** flat, owner-scoped message table keyed to `agent_sessions` now;
  `agent_runs`→`run_steps` convergence (durable agentic trace in history) is the
  committed endgame that retires the flat table. Not a launch-week change.
  - **Superseded by §12 (2026-06-20):** the transcript is a permanent, first-class
    store; `agent_runs`→`run_steps` does **not** retire the flat transcript table.
    The run model remains the system of record for agentic *execution* only. See §12.4.
- **Route (§8.1):** keep `/api/v1/sessions`, backed by the new model.
- **Incident link (§8.6):** reference **+** participation — set `linked_incident_id`,
  promote to `type='investigation'`, allow posting to the incident via the existing
  `findings` table. No captaincy in this feature.
  - **Superseded by §12 (2026-06-20):** incidents come into existence by
    promote-in-place (an existing `default` session is promoted to the incident
    master in one transaction), reversing the as-built mint-fresh
    `DeclareIncidentRegime`; the `investigation` type is removed and participation
    is expressed solely by `linked_incident_id`. See §12.3.
- **Sequencing (§8.7):** security-first, phased (see §11).
- **Title (§8.3):** heuristic title immediately + async LLM upgrade, user-overridable.
- **Legacy backfill (§8.5):** dev DB — drop or quarantine; don't invent owners.
- **Access & sharing (supersedes §8.4):** sessions are **private by default,
  owner-scoped.** The owner can **"make public"** → other authenticated principals get
  **read-only** access. No admin-sees-all; if oversight is ever needed it's a separate,
  explicitly-audited view (deferred).

### Access-control matrix

| Action | Owner | Non-owner, private | Non-owner, public | Notes |
|---|---|---|---|---|
| Appears in *my* list | ✅ (own) | — | ✅ "Shared with you" | auto-listed + owner-attributed (Phase 5; supersedes the original "link/id only" call) |
| Read messages | ✅ | 404 | ✅ read-only | |
| Send / continue | ✅ | ❌ | ❌ | fork-to-continue = future |
| Rename / delete | ✅ | ❌ | ❌ | |
| Toggle visibility | ✅ | ❌ | ❌ | |

### Sharing sub-decisions (defaults — confirm)

- **"Public" = any authenticated Joe principal** (OIDC session or service account), not
  unauthenticated / internet. This is an OIDC-gated internal tool.
- **Discoverability:** ~~a public session is reachable by id/link (read-only), **not**
  auto-listed in other users' browse.~~ **Superseded by Phase 5:** public sessions
  *are* auto-listed in every other authenticated user's "Shared with you" section,
  read-only and attributed to their owner. This also relaxes the "`creator_principal`
  is never projected" rule for this one list: a public session's owner is disclosed
  precisely because the owner chose to share it (the per-id GET still does not project
  it).
- **Granularity:** v1 is binary private/public. Per-principal sharing ("share with
  user:bob") is future.

## 11. Phased implementation plan

**Phase 1 — Ownership & isolation (security; ship first). ✅ Implemented.**
- Migration (`022_chat_sessions`): interim `chat_messages` table keyed to
  `agent_sessions` (`session_id` FK, `seq`, `role`, `content`, `tool_name`,
  `tool_args`, `created_at`); added `title` and `visibility` (default `'private'`)
  columns to `agent_sessions` so the schema is sharing-ready without a second
  migration. `seq` (mirroring `run_steps.step_number`) is the ordering key —
  RFC3339 second-precision timestamps tie, so lexical time ordering is unsafe.
- Repointed webui chat (`internal/api/webui.go`): `handleCreateSession` records
  owner = `PrincipalFromContext`; `handleListSessions` returns own only (new
  `sessionmodel.ListSessionsByCreator`); `handleGetSessionMessages` owner-checked
  (404 on miss — existence not disclosed). Responses keep the legacy JSON shape so
  the frontend is unchanged this phase.
- Task path (`internal/api/tasks*.go`): `buildTaskRun` stamps an owner on the
  `agent_sessions` row; `persistTaskMessages`/`seedHistory` use `chat_messages`; a
  `sessionAccessAllowed` guard refuses continuing another user's session (404),
  closing the `seedHistory` cross-user read vector on `/tasks` + `/tasks/stream`.
- Unfiltered legacy endpoints retired (handlers repointed off `store.Sessions`;
  the legacy `sessions`/`session_messages` tables remain dormant pending the §7
  full retirement/backfill).
- Tests: cross-user read returns 404; owner happy-path; list isolation; task-path
  cross-user rejection; `022` migration up/down/up round-trip.

**Phase 2 — Browse tab + titles. ✅ Implemented.**
- API (`internal/api/webui.go`): `PATCH /sessions/{id}` (rename, owner-checked,
  404-on-miss, trimmed-non-empty title) and `DELETE /sessions/{id}` (owner-checked,
  204; `chat_messages` FK CASCADE expunges messages). The session list/get JSON
  gains `last_activity_at` for the browse list's recency label.
- Repository (`internal/sessionmodel`): `UpdateSessionTitle` — a rename does NOT
  bump `last_activity_at` (metadata, not chat activity, so it must not reorder the
  recency-sorted browse list).
- Auto-title (`internal/api/sessiontitle.go`, wired in `persistTaskMessages`): on a
  session's opening turn, `generateTitleAsync` makes a single LLM call
  (`prompts.ChatTitleSystem`, `context.WithoutCancel` + timeout) and writes the
  result — but only while the session is still untitled, so a manual rename and a
  second turn never overwrite it. claude.ai-style: there is no synchronous
  first-words heuristic, so the UI shows a "New chat" placeholder until the async
  title lands (`ChatPage` polls `GET /sessions/{id}` until `title` is non-null,
  then stops). Best-effort: an unconfigured/failed LLM leaves the session
  untitled and the placeholder persists.
- UI: `/sessions` browse page (`SessionsPage.tsx`) + sidebar nav item (between Chat
  and Admin); inline rename, delete-with-confirm, open, "New chat" CTA.
  `RecentSessions` labels by `title ?? summary`. Client: `updateSessionTitle` /
  `deleteSession` (`ui/src/api/chat.ts`), `SessionSchema` gains `title` /
  `visibility` / `last_activity_at`.
- Store fix (`internal/store/store.go`): an unshared in-memory SQLite DSN is pinned
  to a single connection — extra pooled connections each open a private empty DB, a
  latent "no such table" surfaced once the async title write forced concurrent
  access in tests.
- Tests: repo `UpdateSessionTitle` (persist + no activity bump); webui PATCH/DELETE
  owner-only (404 cross-user, empty-title 400, list reflects rename, cascade delete);
  auto-title generated-on-first-message + does-not-overwrite; `sanitizeLLMTitle`
  units; `SessionsPage` list/empty/rename/delete frontend tests.

**Phase 3 — Sharing. ✅ Implemented.**
- API (`internal/api/webui.go`): `PATCH /sessions/{id}` now also accepts
  `visibility` (`private`|`public`); title and visibility are independent
  optional fields, validated up front so a bad value never half-applies, and a
  PATCH with neither is a 400. Owner-checked, 404-on-miss (existence not
  disclosed). New `GET /sessions/{id}` returns a single session's metadata: the
  owner always (`read_only=false`), a non-owner only when `public`
  (`read_only=true`); a private session owned by another principal — or a
  missing one — 404s. `creator_principal` is never projected, so a public
  viewer never learns the owner's identity. `handleGetSessionMessages` gained
  the same public read path (owner OR public → 200; else 404). Send/continue
  stays owner-only (`sessionAccessAllowed`, tasks path) per the §10 matrix.
- Repository (`internal/sessionmodel`): `UpdateSessionVisibility` — an
  unconditional write by ID (handler owner-checks first) that, like
  `UpdateSessionTitle`, does NOT bump `last_activity_at` (visibility is
  metadata, not chat activity).
- UI: `ChatPage` fetches session metadata (`GET /sessions/{id}`) keyed on the
  active session id; owner sees a "Make public/Make private" toggle plus a
  "Copy link" control when public; a non-owner public viewer sees a "Read-only"
  badge and a composer-less `ChatWindow` (new `readOnly` prop) and is exempt
  from the zero-zone access gate (reading shared content needs no zone).
  `SessionsPage` rows show a "Public" badge. Client: `fetchSession` /
  `updateSessionVisibility` (`ui/src/api/chat.ts`); `SessionSchema` gains
  `read_only`.
- Tests: repo `UpdateSessionVisibility` (persist both directions + no activity
  bump); webui public-read-path (private→404, public→200 read-only, no
  creator leak, re-privatize→404) and authorization (non-owner toggle 404,
  invalid value 400, empty PATCH 400, missing-session GET 404); `ChatPage`
  sharing controls (make-public toggle, make-private + copy-link, read-only
  viewer, no controls pre-session).

**Phase 4 — Incident linkage. ✅ Implemented.**
- API (`internal/api/webui.go`): `POST /sessions/{id}/link-incident` attaches the
  caller's chat session to the currently-active incident. Owner-checked with the
  same 404-on-miss posture as the other mutators (a non-owner or missing session
  is indistinguishable). Resolves the active incident via the new
  `ActiveIncidentSession`; returns 409 when none is active (no incident regime),
  when the session is itself an incident, or when it would self-link. The session
  list/get JSON gains `linked_incident_id` so the browse list and chat header can
  show an incident badge (`creator_principal` is still never projected).
- Repository (`internal/sessionmodel`): `LinkSessionToIncident` sets
  `linked_incident_id` **and** promotes `type` to `'investigation'` in one write
  (reference **+** participation, §10) — the migration-009 CHECK permits
  `linked_incident_id` on any non-incident type. Like the title/visibility
  mutators it does NOT bump `last_activity_at` (linkage is metadata, not chat
  activity, so it must not reorder the recency-sorted browse list).
  `ActiveIncidentSession` returns the active incident (`type='incident'`,
  `incident_state ∉ {resolved, reviewed}`), mirroring `ResolveIncidentRegime`'s
  active-incident lookup. **Participation**: once promoted to `investigation` and
  linked, the session posts to the incident via the existing `findings` table
  (`POST /agent-sessions/{id}/findings`) — no new findings code; captaincy stays
  out of scope.
- UI: `ChatPage` gains an "Attach to incident" control (owner-only, shown only
  while `useRegime().mode === 'incident'` and the session is unlinked) and a
  "Linked to incident" badge once linked; `SessionsPage` rows show an "Incident"
  badge for linked sessions. The app-shell `IncidentBanner` stays session-
  agnostic by design (its own comment: "an app-shell concern, not chat content"),
  so per-session linkage surfaces on the chat header/row rather than in the global
  banner. Client: `linkSessionToIncident` (`ui/src/api/chat.ts`); `SessionSchema`
  gains `linked_incident_id`.
- Tests: repo `LinkSessionToIncident`/`ActiveIncidentSession` (link persists +
  type promotion + no activity bump + active lookup goes nil on resolve); webui
  link endpoint (409 with no active incident, non-owner 404, owner-link promotes +
  records linkage, missing-session 404); `ChatPage` incident-link control
  (attach-during-incident, hidden when normal, linked badge) and `SessionsPage`
  incident badge.

**Phase 5 — Shared-sessions discovery. ✅ Implemented.**
- Supersedes the §10 "public = link/id reachable, not auto-listed" call: a public
  session is now auto-listed in every *other* authenticated principal's browse,
  read-only and attributed to its owner ("read-only · shared by <owner>").
- Repository (`internal/sessionmodel`): `ListPublicSessionsByOthers` — mirrors
  `ListSessionsByCreator` (same LEFT JOIN message count, recency order) but filters
  to `visibility='public' AND creator_principal != caller`, so a user's own public
  sessions are not duplicated across the two lists. It carries `creator_principal`
  on each row — the one read path that intentionally projects the owner.
- API (`internal/api/webui.go`): `GET /sessions/shared` (`handleListSharedSessions`)
  flags every row `read_only=true` and sets `shared_by` (the owner's principal).
  This is the deliberate exception to "creator_principal is never projected"; the
  owner-scoped list, create/rename, and the per-id GET still never project it.
  Send/continue stays owner-only, so these are read-only entry points into the
  existing Phase 3 public read path (clicking a shared row opens the read-only
  `ChatPage`). Route note: literal `/sessions/shared` outranks `/sessions/{id}` in
  the Go 1.22 mux, so order is irrelevant.
- UI: `SessionsPage` gains a "Shared with you" section below the owner's own
  sessions (headings appear only when both lists are non-empty); each shared row is
  a read-only link with a "Read-only" badge and a "shared by <owner>" sub-label,
  and exposes no rename/delete. Client: `fetchSharedSessions` (`ui/src/api/chat.ts`);
  `SessionSchema` gains `shared_by`.
- Tests: repo `ListPublicSessionsByOthers` (excludes private + own, recency order,
  owner + count projection, limit); webui `GET /sessions/shared` (private excluded,
  public listed read-only + `shared_by`, no `creator_principal` leak, owner's own
  excluded, re-privatize removes); `SessionsPage` shared-section render (read-only
  badge, owner attribution, no mutators).

**Later — convergence & extensions.**
- Chat through the run model (durable tool-call trace in history) → retire flat table.
  - **Superseded by §12 (2026-06-20):** the flat transcript table is permanent and is
    **not** retired; the run model remains the durable system of record for agentic
    execution only and never absorbs the transcript. See §12.4.
- Per-principal sharing; fork-a-public-session-to-continue; admin audited oversight view.

## 12. Clean-room redesign (B001)

> Status: **clean-room redesign, settled 2026-06-20.** This section is a from-scratch
> redesign of the session subsystem, produced by re-deriving requirements from first
> principles (actors → threats → ontology → storage → API → lifecycle) and then
> reconciling the result against the as-built system. **Where this section conflicts
> with any earlier section of this document, this section wins.** It supersedes the
> specific earlier decisions called out below (and at each superseded location:
> §2 type discriminator, §10 storage, §10 incident link, "Later — convergence").
> Items flagged "reconciliation note" are deliberate open convergence deltas to be
> resolved in the diff/convergence layer, not here.

> **Amendment — team-wide read model (2026-06-21).** A later review reversed the
> private-by-default posture an earlier fold-in had baked into this section. **Session
> read access is now team-wide by default: any authenticated principal may read any
> session,** analogous to how a whole team can see its pull/merge requests. **Privacy
> between team members is explicitly a non-goal.** The security spine is **integrity and
> accountability, not secrecy**: what stays gated is **mutation and governance**, never
> reading. The PR/MR analogy is **visibility-only** — **no git-like session mechanics
> exist anywhere in this design**: no fork, no branch, no merge, no diff between
> sessions, no checkout, no version history. The points this amendment reverses are
> marked *(superseded within §12, 2026-06-21)* at each location below, with the original
> text retained per this section's supersession convention; the new model text follows
> each marker. Affected subsections: §12.1, §12.2, §12.4, §12.7, §12.8, §12.9, and the
> new §12.10.

### 12.1 Actors and trust boundary

- **Five actors.**
  - **Owner** — the human authenticated principal who created the session; the *sole
    mutator*.
  - **Team member (other authenticated principal)** — read-only on **any** session.
    *(Supersedes within §12, 2026-06-21, the prior "read-only on shared sessions"; read
    is team-wide — see the amendment banner above §12.1. Reading is the default for any
    authenticated principal, not a per-session grant.)*
  - **Admin / operator** — governed, audited, cross-tenant.
  - **Autonomous agent** — acts *within* a session, never owns one; its writes are
    attributed under its own principal, but the session's `creator_principal` stays
    the human.
  - **Unauthenticated** — no access.
- **`creator_principal` is always a human and always the context-resolved
  authenticated principal.** It is *never* accepted from a request body. This kills
  the spoofable-creator defect by construction.
- **One seam per operation.** Every session operation crosses exactly one authz seam
  with a resolved principal; effective permission is one computed decision per
  resource class.

### 12.2 Threat model (each threat is an actor crossing a boundary)

> *Amended 2026-06-21 (team-wide read).* Cross-tenant **read**, **sharing escalation**,
> and **content exfiltration via projection** are **no longer threats**: universal
> authenticated read is the intended model, there is no sharing write-path to escalate,
> and there is nothing to exfiltrate between teammates. Metadata-first projection
> survives only as a payload/performance choice, never as a confidentiality control. The
> three bullets so marked below are *(superseded within §12, 2026-06-21)*; the surviving
> threats are about integrity and accountability, not secrecy.

- **Ownership forgery** — mitigated by context-derived `creator_principal` (§12.1).
- **Cross-tenant read / cross-tenant mutate-delete** — mitigated by the single seam
  (§12.7) plus the per-user/admin route split (§12.8). *(Superseded within §12,
  2026-06-21: cross-tenant **read** is removed as a threat and is now the intended
  model; only cross-tenant **mutate/delete by a non-owner** survives — mitigated by
  write-own enforcement plus the admin-route split.)*
- **Sharing escalation** — the sharing write-path is designed closed; only the owner
  sets visibility. *(Superseded within §12, 2026-06-21: removed — there is no sharing
  write-path and no visibility to set.)*
- **Content exfiltration via projection** — list/metadata views must project away raw
  transcript content and must treat auto-derived titles as content-bearing.
  *(Superseded within §12, 2026-06-21: removed as a privacy threat for team members;
  metadata-first projection may be retained elsewhere only as a payload/performance
  choice, not as a confidentiality control.)*
- **Operator action without attribution** — every admin and sweeper transition writes
  an audit row naming the actor.
- **Unbounded growth** — the retention pipeline (§12.5).
- **Agent-within-session escalation** — agent writes are message/run content only,
  never session-control fields or lifecycle state.

- **Surviving threats (post-amendment), consolidated:** ownership/accountability forgery
  (mitigated by context-derived `creator_principal`); cross-tenant mutate or delete by a
  non-owner (mitigated by write-own enforcement and the admin-route split); operator or
  sweeper action without attribution (mitigated by audit on every transition); unbounded
  growth (mitigated by the retention pipeline); agent-within-session escalation beyond
  message/run content into session-control or lifecycle fields.

### 12.3 Ontology (supersedes the three-type model)

- **Two session types only: `default` and `incident`.** The as-built third type
  `investigation` is collapsed: participation in an incident is expressed by the
  `linked_incident_id` pointer (a fact/relationship), not a type.
  - *Reconciliation note:* today the stored literal for an ordinary chat is `'other'`,
    and link-to-incident currently *also* flips `type` to `'investigation'` with zero
    runtime divergence from `'other'`. The redesign removes the `investigation` type
    value. *(Supersedes the §2 type discriminator and the §10 incident-link decision.)*
- **Incident comes into existence by PROMOTE-IN-PLACE, not mint-fresh.** This
  explicitly reverses the as-built `DeclareIncidentRegime` behavior (which inserts a
  fresh `type=incident` row). Declaring an incident promotes an existing `default`
  session into the incident master in **one transaction**: flip `type` to `incident`,
  set `incident_state=declared`, attach the declarer as captain, flip the global
  regime.
  - **Both UI entry paths resolve to a promote.** A principal already in a session
    promotes *that* session. A principal elsewhere is routed to the sessions tab and
    asked whether to promote an existing session or start a new (empty) one — and
    "start new" is *create-empty-then-promote*, so even the empty case is a promoted
    regular session.
- **Consequence (recorded, accepted):** the session's `creator_principal` (original
  human owner) and the captain (declarer) may differ. Creator stays the owner; captain
  is the incident lead.
- **The incident model otherwise stands as already documented:** one global single-row
  regime, one master incident session, one captain bound to it; linked sessions point
  inward at the incident via `linked_incident_id`.

### 12.4 Storage (single source of truth)

- **One session table** (clean `agent_sessions`), **one transcript table** FK'd to it.
- **THE TRANSCRIPT IS A PERMANENT, FIRST-CLASS STORE.** This reverses the locked
  decision (§10) that `agent_runs`→`run_steps` is the committed endgame that retires
  the flat message table. *(Supersedes the §10 storage decision and the
  "Later — convergence" retire-flat-table item.)*
  - *Rationale:* a human-facing transcript (user/assistant turns) and an agentic
    execution trace (reasoning, tool intent/result, solicitations, world handles) are
    genuinely different shapes. `run_steps` has no message kind and cannot represent
    flat turns; forcing one into the other is the conflation that produced three
    coexisting message representations. The run model remains the durable system of
    record for agentic **execution**; it never absorbs the transcript. The
    previously-deferred `chat_messages`→`run_steps` convergence and its unresolved
    message-kind schema gap are closed as a **non-problem**.
- **Lifecycle expressed by timestamps, not a redundant state enum column:**
  `trashed_at` / `trashed_by` / `purge_after`, and `archived_at` / `archived_by` /
  `archive_ref`. **Active = all null.**
- **Session types:** `type ∈ {default, incident}`; `incident_state` is non-null **if
  and only if** `type=incident`; `linked_incident_id` is the participation pointer.
- **`linked_incident_id` is `ON DELETE SET NULL`, not cascade.** Linked sessions are
  independent `default` conversations owned by possibly-other principals; purging an
  incident *severs the link* (linked sessions revert to plain `default` sessions) and
  must never destroy them. Cascade-as-a-unit applies only to a session's own dependent
  rows: its transcript (`ON DELETE CASCADE`) and its captain binding.
- **`retention_class` becomes the per-session resolution of the active admin retention
  policy** — no longer inert.
- **No `visibility` column.** *(Amended 2026-06-21, team-wide read.)* Team-wide read
  makes per-session visibility inert, so the session table carries **no `visibility`
  column**; the as-built `visibility` column is dropped (§12.9). `retention_class`, the
  lifecycle timestamps, `linked_incident_id ON DELETE SET NULL`, transcript
  `ON DELETE CASCADE`, and the permanent-transcript decision are all **unchanged** by
  this amendment.
- **Legacy `sessions` / `session_messages` tables are retired,** gated on the
  learn-from-sessions fate decision (`docs/backlog/learn-from-sessions-fate.md`), since
  those tables are that feature's only data source.

### 12.5 Retention and lifecycle pipeline (one pipeline, not parallel mechanisms)

- **One admin-configured retention policy** defines an inactivity window and a terminal
  action, chosen per deployment: `trash_then_purge` or `archive`. Scaffolded so v1
  ships behind a clean seam.
- **One background sweeper is the only automated driver,** timestamp-driven against
  `last_activity_at`:
  - `trash_then_purge` sets `trashed_at`/`purge_after`; a later pass purges trashed
    sessions past `purge_after`.
  - `archive` sets `archived_at`/`archive_ref` and moves the transcript to cold storage.
  - The same sweeper also drains abandoned `auth_login_flows` rows.
- **Manual transitions reuse the same state transitions the sweeper applies:** owner
  soft-delete = manual entry to trashed; owner/admin restore; admin purge = manual fire
  of terminal purge; admin archive/restore. **No divergent code paths.**
- **macOS-trash semantics:** soft delete puts a session into trash (recoverable); hard
  delete (purge) trashes and empties in one operation. Owners can soft-delete and
  restore their own sessions; **PURGE IS ADMIN-ONLY.**
  - *Accepted tradeoff (reworded 2026-06-21, team-public model):* a soft-deleted
    session sits in trash, admin-restorable, until the sweeper or an admin purges it.
    Soft-delete is a recoverable lifecycle state, not an erasure: because the session
    model is team-public (§12.1, §12.7), trash adds reversibility, not a new
    confidentiality boundary, and purge — not soft-delete — is what makes removal final.
    A GDPR-style erasure therefore routes through **admin purge**, not a new owner
    capability.
- **Two configurable knobs, both in the retention policy:** the inactivity window
  (default **OFF** / effectively infinite — nothing auto-expires until an admin opts
  in) and trash-grace (default **30 days**). Auto-expiration is default-off by design
  for the regulated posture.
- **Every transition** (trash, restore, purge, archive, unarchive, incident
  link-sever) writes an audit row naming actor and session. The sweeper acts under a
  **boot-minted service principal** so automated expirations are attributed. Manual
  admin purge surfaces a **manifest-with-hard-stop** (count of messages and linked
  children about to be irreversibly destroyed) then explicit confirm; the sweeper's
  automated purge is governed by the policy the admin already approved and does **not**
  re-prompt.

### 12.6 Archive backend (scaffolded behind a provider seam)

- **v1 ships filesystem export:** archived sessions are serialized to versioned files
  under a configured path, hot rows removed, restore parses them back; `archive_ref`
  holds the file locator. An object-store (S3-compatible) provider is a designed-for
  later addition behind the **same seam**, not built in v1.
- **The serialized artifact carries a schema version;** restore must *refuse or
  migrate* a version it does not understand rather than mis-parse. The artifact is
  self-contained: session metadata plus full transcript in one file, so restore needs
  nothing but the file.

### 12.7 Authorization seam (dedicated, per resource class)

> *Amended 2026-06-21 (team-wide read).* The **`sessionAccess` seam and its
> single-decision-per-resource-class property are unchanged**; only the **read
> relationship broadens**. Relationships now resolve to: **owner** (creator; the *only*
> principal that may mutate the session), **team member** (any other authenticated
> principal — may read any session, may not mutate it), **admin** (governance and
> cross-tenant mutation), and **unauthenticated** (no access). Non-owners are strictly
> read-only on sessions they do not own. **Reading is not a grant — it is the default
> for any authenticated principal.** A team member who wants to act on what they read
> simply **starts a separate, independent session of their own**; there is **no fork,
> branch, or copy** of the session they read. `creator_principal` stays context-derived,
> now justified by **accountability** (who actually ran this) rather than secrecy. The
> shared-viewer-as-a-distinct-grant framing and the `share` action are removed (below).

- **Sessions get their OWN single enforcement seam,** separate from the component
  RBAC/accessor seam. Session-relationship logic never touches zones or policies;
  component RBAC never learns session ownership. The single-enforcement-site invariant
  holds *within each resource class*.
- **One function, `sessionAccess(principal, sessionID, action) -> decision`,**
  evaluated once below the transport. It resolves the session, derives the principal's
  relationship (owner / team-member / admin / none — see the amendment note above
  §12.7), and returns allow/deny plus the resolved relationship so handlers do not
  re-query. **No route reimplements ownership.**
- **Relationship resolution:**
  - **owner** = `creator_principal` equals principal.
  - **team member** = principal authenticated **AND** not owner — **read-only on any
    session** (read is the default, not a grant). *(Supersedes within §12, 2026-06-21,
    the prior `shared-viewer` = "session is shared/public AND authenticated AND not
    owner"; visibility no longer gates read.)*
  - **admin** = principal carries the existing dynamic admin capability — reuse the
    **D-0011 check, do not reimplement**. Admin is cross-tenant and audited.
  - **none** = deny.
- **The sweeper principal is a system actor** that bypasses relationship resolution for
  its policy-authorized transitions, attributed in audit; it is neither owner nor admin.
- **Action vocabulary over sessions:** `read`, `write` (rename, link, send-message),
  `soft_delete`, `restore`, and admin-only `purge`, `archive`, `unarchive`,
  `configure_retention`. *(Supersedes within §12, 2026-06-21: the `share` action is
  removed — there is no visibility toggle.)*

### 12.8 API contract (one per-user namespace, one admin namespace)

- **The parallel `/api/v1/agent-sessions` namespace is removed;** it was a Phase-1
  scope-isolation workaround and the home of the team-global, spoofable-creator CRUD.
  Its captain / findings / runs sub-resources re-home under the surviving
  `/api/v1/sessions` path. *(Amended 2026-06-21: the **duplicate namespace** and the
  **spoofable creator** are removed, but its **team-global read** is now the intended
  model, not a defect — see §12.9.)*
- **Per-user routes under `/api/v1/sessions`,** all behind `sessionAccess`, resolve to
  owner / team-member / none. *(Supersedes within §12, 2026-06-21: the prior "resolve
  only to owner / shared-viewer / none and **cannot return another principal's session
  regardless of caller**" is removed for **reads** — per-user read routes **can and
  should** return another principal's session, because read is team-wide. Mutation
  routes remain owner-only.)*
  - **list** — the team-wide list with a `mine` filter (no longer an owner-scoped
    list); **get**; **get messages** — any authenticated principal may read any session;
    **create** (creator = resolved principal); **patch** (owner, title); **soft-delete**
    (owner, to trash); **restore** (owner); **list own trash**; **promote-incident** (the
    promote-in-place transition); **link-incident** (owner).
  - *(Supersedes within §12, 2026-06-21: the separate **list shared** route and the
    **share** / visibility-setting endpoint are **removed** — there is no visibility
    toggle.)*
- **Admin routes under `/api/v1/admin/sessions`,** all `requireAdmin` and audited,
  cross-tenant — mirroring the component-governance admin surface:
  - list all (filter by principal/type/state); get and get-messages (ordinary content
    reads — see the admin-read amendment below); purge (manifest-with-hard-stop); archive
    and restore-archive; list all trash; retention-policy get and put.
- **Defense in depth:** cross-tenant **governance and mutation** requires **BOTH** the
  admin route prefix **AND** an admin relationship from `sessionAccess`; a per-user
  route can never be elevated to an admin relationship. *(Amended 2026-06-21: this
  applies to **governance and cross-tenant mutation only** — cross-tenant **read** is
  the intended model and is not gated by the admin prefix.)*
- **Admin transcript read returns the metadata projection by default** (roles,
  timestamps, counts, titles) and requires an explicit `include=content` query
  parameter to return raw message bodies, so browsing the console does not incidentally
  pull raw transcripts into a list view. Because `chat_messages.content` is stored
  un-redacted, an admin content read writes a distinct audit verb
  (`session.content.read`).
  - *(Superseded within §12, 2026-06-21: the **content-projection privacy gate**, the
    **`include=content` deliberate-reveal requirement**, and the **`session.content.read`
    audit verb** are all **removed**. Admin content reads are **ordinary reads**;
    metadata-first rendering survives only as a payload/performance choice. See §12.10.)*

### 12.9 Reconciliation notes (open convergence deltas)

These are recorded as deltas to be resolved in the diff/convergence layer, **not now**.

- **Reversals of §10 (deliberate).** This section reverses §10 on two points: the
  transcript is permanent (`run_steps` does not retire it, §12.4), and incidents are
  promote-in-place (not mint-fresh, §12.3). Both are deliberate.
- **Team-wide read agrees with the as-built behavior** *(added 2026-06-21).* One
  namespace already exposed sessions team-globally — the B001 inventory verified that the
  `agent-sessions` namespace had **no owner filter** on read. Under the team-wide read
  model that former *divergence becomes the intended model*, not a defect to correct.
- **Still removed (unchanged by the amendment):** the as-built spoofable
  `creator_principal` taken from the request body, and the duplicate
  `/api/v1/agent-sessions` namespace, both remain removed (§12.1, §12.8).
- **Per-session visibility scaffolding is removed, not wired** *(amended 2026-06-21).*
  The B001 inventory verified a `visibility` column and an unwired
  `UpdateSessionVisibility` path with **no caller**. The earlier fold-in would have
  *wired* this; the team-wide read model **removes it** instead (§12.4, §12.8) — there is
  no visibility to set.
- **Governance-and-mutation vs. universal read is the only surviving axis** *(supersedes
  within §12, 2026-06-21, the prior "admin governance vs. sharing visibility" and
  "agreement with §8.1 / §10" bullets).* There is **no sharing-visibility axis anymore**:
  read is universal for any authenticated principal. The one distinction still worth
  preserving is **governance-and-mutation gating versus universal read** — the admin
  governance console (§12.8) governs and mutates cross-tenant; it is **not** a privacy
  boundary over reading. The earlier framing of sharing as private-by-default and
  owner-controlled, and the recorded "agreement with §8.1 / §10" on that basis, are
  withdrawn: the team-wide read model intentionally diverges from a private-by-default
  reading of §8.1 / §10 (the namespace collapse still agrees).

### 12.10 UI surface (per-user and admin)

> *Added 2026-06-21 as part of the team-wide read amendment.* This subsection records the
> UI consequences of universal authenticated read. It introduces **no** version-control
> affordances of any kind — no fork, branch, merge, diff, checkout, or version history.

- **Per-user surface.**
  - The **sessions page** shows the **team-wide list** with a **`mine` filter**.
  - Opening a session owned by another principal is a **read-only viewer**.
  - The user **delete** action is **soft-delete to trash** (never hard delete).
  - A per-user **trash view** shows the user's own trashed sessions with the **remaining
    time before purge** and a **restore** action; trashed sessions are removed from the
    normal team list.
  - There is **no share or visibility control**, and **no fork, branch, or copy
    affordance of any kind**.
  - A **global declare-incident control** lives in top-level chrome and is **always
    reachable**. Triggered **outside** a session it routes to the sessions area and
    presents a **promote-or-start-new** disambiguation, where **start-new is
    create-empty-then-promote**. A secondary **promote-this-session-to-incident**
    affordance lives in the **chat view**. Both resolve to the **promote-in-place**
    transition (§12.3). The incident regime and **captain banner** is **reused, not
    rebuilt**.
- **Admin surface.**
  - A **sessions console** alongside the existing component-governance admin pages.
  - A **cross-tenant list** filterable by **principal, type, and state**.
  - **Purge** with a **manifest-and-hard-stop** confirm naming the **message and
    linked-child counts** to be destroyed.
  - **Archive** and **restore-archive**; an **all-trash** view.
  - A **retention-policy editor** exposing the **inactivity-window** knob (default
    **off**) and the **trash-grace** knob (default **30 days**), plus the
    **terminal-action selector** (**trash-then-purge** or **archive**).
  - **Admin content viewing is an ordinary content read** — **no privacy confirm** and
    **no special audit verb** (§12.8). **Metadata-first** rendering in the list is a
    **payload/performance** choice only.

## 13. B001 implementation convergence ledger

This is the **canonical implementation sequence** for the §12 redesign: §12 remains the
design spec, and this section is the **execution sequence against it**. Each node below is
an **independent Claude Code build session against the live tree**, driven by **§12 as the
source of truth**, and every node **begins with a read-only verification phase that
re-derives file locations before changing anything**. Nodes are ordered **backend before
UI**, and **backend enforcement is never modified within a UI node**. This is a
**status-and-next-action ledger, not a design narrative**.

### 13.1 Node ledger

**B002 — Storage schema rewrite.**
- *Depends on:* B001 closed.
- *Scope:* Schema-replace the session tables (nothing is deployed, no data to preserve):
  collapse the type enum to two values `default` and `incident`, remove `investigation`,
  rename the ordinary-chat literal to `default`, change `linked_incident_id` to
  `ON DELETE SET NULL`, add the lifecycle timestamp columns `trashed_at` / `trashed_by` /
  `purge_after` / `archived_at` / `archived_by` / `archive_ref`, drop the `visibility`
  column, keep `retention_class` but redefine it as the per-session resolution of the admin
  retention policy, and keep the transcript table as the permanent first-class store. **Hard
  constraint:** do **NOT** drop the legacy `sessions` / `session_messages` tables — they are
  retained as the future learn-from-sessions feature's data source per the backlog decision.
- *Acceptance:* Schema matches §12.4; the `incident_state` ⇒ `type=incident` CHECK is
  preserved; build and tests green; legacy tables still present.

**B003 — Session authorization seam.**
- *Depends on:* B002.
- *Scope:* A dedicated `sessionAccess` decision function, separate from the component RBAC
  accessor, evaluated once below the transport, computing the relationship (owner /
  team-member / admin / none) and returning allow/deny plus the resolved relationship; reuse
  the existing dynamic admin capability check, do not reimplement it; no routes wired yet.
- *Acceptance:* Break-tests on each relationship; team-member resolves to read-only; creator
  is context-derived so the spoofable-creator path is structurally impossible.

**B004 — Incident promote-in-place.**
- *Depends on:* B002.
- *Scope:* Rework incident declaration from minting a fresh incident-typed row to an in-place
  transition that takes an existing session id and, in one transaction, flips its `type` to
  `incident`, sets `incident_state` to `declared`, attaches the declarer as captain, and
  flips the global regime; rework link-to-incident to set `linked_incident_id` only, with no
  type flip. Fold in the one-line fix of the stale callable-but-unwired comment in the
  session gate package.
- *Acceptance:* Declaration promotes an existing session; no mint-fresh path remains; the
  captain gate behavior is unchanged, proven by a break-test, since the gate keys on
  `type=incident` and on being the active incident session and neither changes.

**B005 — Per-user API on the new seam.**
- *Depends on:* B003, B004.
- *Scope:* Collapse to a single per-user sessions namespace with team-wide read plus a `mine`
  filter, context-derived create, owner-only mutate, soft-delete and restore and own-trash
  list, promote-incident, and link-incident, all routed through `sessionAccess`; remove the
  parallel team-global namespace and re-home its captain findings and runs sub-resources
  under the surviving path.
- *Acceptance:* Creator forgery is gone; any authenticated principal can read any session;
  no route reimplements ownership logic.

**B006 — Admin API namespace.**
- *Depends on:* B005.
- *Scope:* An admin sessions namespace, all admin-gated and audited, with cross-tenant list,
  purge with a manifest-and-hard-stop, archive and restore-archive, all-trash list, and
  retention-policy get and put.
- *Acceptance:* Cross-tenant mutation requires both the admin route prefix and an admin
  relationship; every transition writes an audit row.

**B007 — Retention sweeper and archive provider.**
- *Depends on:* B002, B006.
- *Scope:* A single policy-driven background sweeper under a boot-minted service principal
  that applies the policy terminal action of trash-then-purge or archive against
  `last_activity_at` and also drains abandoned `auth_login_flows` rows; a filesystem-export
  archive behind a provider seam emitting a versioned self-contained artifact.
- *Acceptance:* Sweeper actions are attributed in audit; inactivity window defaults off and
  trash-grace defaults to thirty days; archive export and restore round-trip; restore refuses
  an unknown artifact version.

**B008 — UI surfaces.**
- *Depends on:* B005, B006, B007.
- *Scope:* The per-user surface (team-wide list with a `mine` filter, read-only viewer for
  other principals' sessions, soft-delete to trash, a trash view with remaining time and
  restore, a global declare-incident control in top-level chrome with a
  promote-or-start-new disambiguation where start-new is create-empty-then-promote, a
  secondary promote-this-session affordance in the chat view, the reused incident and captain
  banner, and no share or visibility or fork or branch or copy affordance) and the admin
  console (cross-tenant list, purge confirm, archive and restore, all-trash, retention-policy
  editor). **No backend enforcement is modified in this node.**
- *Acceptance:* Matches §12.10; no git-like affordances present.

### 13.2 Cleanup and artifacts (tracked, not blocking nodes)

These are tracked items, **not blocking nodes**:

- Reword the §12.5 accepted-tradeoff text that still uses privacy framing so it fits the
  team-public model — keep the admin-purge-routes-erasure point, drop the
  sensitive-and-admin-visible framing.
- The three B001 inventory reports are landed under `docs/investigations` as the dated
  as-built reference.
- The learn-from-sessions feature is a decided future feature with its legacy tables retained
  per `docs/backlog`.

**The legacy-table drop is intentionally absent from every node above and is gated on that
future feature.**
