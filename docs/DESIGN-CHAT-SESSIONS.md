# Design: First-Class Chat Sessions (Web UI)

> Status: **decisions locked 2026-06-06** — see §10 for the resolved calls and §11
> for the phased plan. **Phase 1 (ownership & isolation), Phase 2 (browse tab +
> titles), and Phase 3 (binary private/public sharing) implemented 2026-06-06.**
> §2–§8 retained as rationale.

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
- **Route (§8.1):** keep `/api/v1/sessions`, backed by the new model.
- **Incident link (§8.6):** reference **+** participation — set `linked_incident_id`,
  promote to `type='investigation'`, allow posting to the incident via the existing
  `findings` table. No captaincy in this feature.
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
| Appears in *my* list | ✅ (own) | — | — (not in their list) | public = link/id reachable, not auto-listed |
| Read messages | ✅ | 404 | ✅ read-only | |
| Send / continue | ✅ | ❌ | ❌ | fork-to-continue = future |
| Rename / delete | ✅ | ❌ | ❌ | |
| Toggle visibility | ✅ | ❌ | ❌ | |

### Sharing sub-decisions (defaults — confirm)

- **"Public" = any authenticated Joe principal** (OIDC session or service account), not
  unauthenticated / internet. This is an OIDC-gated internal tool.
- **Discoverability:** a public session is reachable by id/link (read-only), **not**
  auto-listed in other users' browse. An opt-in "public sessions" gallery is a later
  nicety.
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
  session's opening turn, a first-words heuristic title is written synchronously,
  then an async LLM upgrade (`prompts.ChatTitleSystem`, `context.WithoutCancel` +
  timeout) replaces it — but only while the title is still the heuristic, so a
  manual rename always wins. A no-op once the session already has a title.
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
  auto-title heuristic-on-first-message + does-not-overwrite; `heuristicTitle` /
  `sanitizeLLMTitle` units; `SessionsPage` list/empty/rename/delete frontend tests.

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

**Phase 4 — Incident linkage.**
- `POST /sessions/{id}/link-incident` → set `linked_incident_id` + promote to
  `type='investigation'`; findings participation; surface link in `IncidentBanner` and
  the session row.

**Later — convergence & extensions.**
- Chat through the run model (durable tool-call trace in history) → retire flat table.
- Per-principal sharing; fork-a-public-session-to-continue; admin audited oversight view.
