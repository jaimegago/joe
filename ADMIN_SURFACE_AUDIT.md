# Admin Surface Audit — post-Stream-K Joe

Read-only scoping report on two operator-facing surfaces ahead of a v1 public
release: **user/zone management** and **incident mode**. All claims below were
verified against current code (not prior summaries); the load-bearing security
claims were re-read directly. Nothing was modified.

Scope of verification: OIDC onboarding path, RBAC admin API gating, captain-gate
composition, and the CLI subcommand dispatcher were each read at the source.

---

## Investigation 1 — User and Zone Management

### 1. New-user onboarding flow

When a previously-unseen verified email completes OIDC sign-in:

- The callback handler runs at [internal/auth/handlers.go:131-231](internal/auth/handlers.go#L131-L231). It converts the verified email to a principal via `PrincipalFromClaims` ([internal/auth/handlers.go:184](internal/auth/handlers.go#L184)), which hard-rejects unverified emails and mints `user:<email>` ([internal/auth/claims.go:35-43](internal/auth/claims.go#L35-L43), [internal/rbac/identity.go:41-53](internal/rbac/identity.go#L41-L53)).
- The only row written for a brand-new user is a **session** row: `Mint` → `CreateSession` → `INSERT INTO auth_sessions (id, principal, created_at, expires_at)` ([internal/auth/session.go:56-72](internal/auth/session.go#L56-L72), [internal/auth/repository.go:75-84](internal/auth/repository.go#L75-L84)).
- **There is no `principals`/`users` table.** The `auth_sessions` schema ([internal/store/migrations/014_auth_sessions.up.sql](internal/store/migrations/014_auth_sessions.up.sql)) keys on the principal *string*; it is not a foreign key into any identity table. A "user" exists only as a session plus whatever rows reference its principal string.
- **Initial zone state: zero.** No policy row and no zone assignment are created on first login. The callback never calls `CreatePolicy`. The only conditional write is the admin bootstrap (see §4), gated on the configured admin email ([internal/auth/handlers.go:196-209](internal/auth/handlers.go#L196-L209)).
- At authorization time a user with no policy resolves to the seeded **`unassigned`** zone, which permits only `["read"]` ([internal/rbac/policy.go:109-119](internal/rbac/policy.go#L109-L119), [internal/store/migrations/006_rbac.up.sql:30-34](internal/store/migrations/006_rbac.up.sql#L30-L34)).
- **What the user sees on first login (zero zones):** they land authenticated. `/me` returns `is_admin:false` ([internal/api/currentuser.go:36-57](internal/api/currentuser.go#L36-L57)). The Sidebar hides Admin / LLM-Settings nav for non-admins ([ui/src/components/layout/Sidebar.tsx:31-32](ui/src/components/layout/Sidebar.tsx#L31-L32)). They can reach Dashboard, Graph, Sources, Chat. There is **no empty-state / "you have no access, contact an admin" messaging** anywhere in the UI — a zero-zone user simply gets read-only behavior with no explanation.

### 2. Zone-assignment mechanics (policy rows: `rbac_policies`)

Every path that inserts into the zone-assignment table:

| Trigger | Entry point | Writer |
|---|---|---|
| CLI `joe zone grant --principal <p> --zone <z>` | [cmd/joe/zone.go:132-180](cmd/joe/zone.go#L132-L180) | `repo.CreatePolicy` ([cmd/joe/zone.go:174](cmd/joe/zone.go#L174)) |
| HTTP `POST /api/v1/admin/policies` | [internal/api/admin.go:121-138](internal/api/admin.go#L121-L138) (route reg [admin.go:33](internal/api/admin.go#L33)) | `repo.CreatePolicy` ([internal/api/admin.go:132](internal/api/admin.go#L132)) |
| Direct repository | [internal/rbac/repository.go:239-252](internal/rbac/repository.go#L239-L252) | `INSERT INTO rbac_policies` |

No migration seeds policy rows — policies are provisioned only post-bootstrap.

> ⚠️ The HTTP path is **not admin-gated** — see §5 and Launch Blockers.

### 3. Zone CRUD (`security_zones`)

- **Create (HTTP):** `POST /api/v1/admin/zones` → `repo.CreateZone` ([internal/api/admin.go:53-73](internal/api/admin.go#L53-L73), [internal/rbac/repository.go:115-131](internal/rbac/repository.go#L115-L131)).
- **Create (seed):** default zones `prod-readonly`, `prod-write`, `dev-full`, `unassigned` ([internal/store/migrations/006_rbac.up.sql:30-34](internal/store/migrations/006_rbac.up.sql#L30-L34)); `regime-control` with `["declare_incident","resolve_incident"]` ([internal/store/migrations/012_regime_rbac.up.sql:14-20](internal/store/migrations/012_regime_rbac.up.sql#L14-L20)).
- **Update / edit allowed-actions: not found.** No CLI command, no HTTP endpoint. Allowed-actions are set at creation only; the UNIQUE id constraint blocks re-creating to "edit."
- **Delete: not found.** No CLI or HTTP path deletes a `security_zones` row. The FK `ON DELETE CASCADE` chains exist but are never triggered by any entry point.
- Source→zone assignment (distinct from policies) is created via `POST /api/v1/admin/source-zones` → `UpsertAssignment` ([internal/api/admin.go:89-105](internal/api/admin.go#L89-L105)).

### 4. Admin promotion / demotion

- **Bootstrap:** config field `auth.admin_email` ([internal/config/config.go:65-69](internal/config/config.go#L65-L69)), read at [cmd/joe/server.go:725-736](cmd/joe/server.go#L725-L736). On any login whose verified email matches, `GrantAdmin` upserts an `admin_principals` row with `granted_by="bootstrap_admin_email"` ([internal/auth/handlers.go:196-209](internal/auth/handlers.go#L196-L209), [internal/auth/provision.go:78-99](internal/auth/provision.go#L78-L99)).
- **Promote after launch:** CLI only — `joe admin grant --principal <p> [--reason ...]` → `AddAdmin` ([cmd/joe/admin.go:84-139](cmd/joe/admin.go#L84-L139), [internal/rbac/repository.go:331-350](internal/rbac/repository.go#L331-L350)). Also cleans up redundant `rbac_policies` rows for the new admin (single source of truth).
- **Demote after launch:** CLI only — `joe admin revoke --principal <p>` → `RemoveAdmin` ([cmd/joe/admin.go:145-168](cmd/joe/admin.go#L145-L168), [internal/rbac/repository.go:352-363](internal/rbac/repository.go#L352-L363)). Note: if the principal still matches `auth.admin_email`, the next login re-promotes them.
- **HTTP / UI promotion: not found.** `registerAdminRoutes` exposes zones/source-zones/policies/unassigned only ([internal/api/admin.go:19-37](internal/api/admin.go#L19-L37)). No `/admin/admins`, `/promote`, `/demote`. Admin lifecycle is CLI + bootstrap-email exclusively.
- Storage: `admin_principals(principal, granted_at, granted_by, reason)` ([internal/store/migrations/016_admin_principals.up.sql](internal/store/migrations/016_admin_principals.up.sql)). The policy engine short-circuits to allow for admins ([internal/rbac/policy.go:133-146](internal/rbac/policy.go#L133-L146)).

### 5. Authorization-state inspection

- **Self:** `GET /api/v1/me` → `{principal, is_admin, rbac_enabled, oidc_enabled}` ([internal/api/currentuser.go:36-57](internal/api/currentuser.go#L36-L57)). Intentionally not admin-gated.
- **Full state (admin view):** read endpoints exist — `GET /admin/zones`, `/admin/policies`, `/admin/source-zones`, `/admin/unassigned` ([internal/api/admin.go:41-167](internal/api/admin.go#L41-L167)). Between them an operator can see who has which zone and what each zone permits.
- **"Who are the admins?": not exposed over HTTP.** `ListAdmins` exists in the repository ([internal/rbac/repository.go:303-323](internal/rbac/repository.go#L303-L323)) but no HTTP handler calls it. The only way to enumerate admins is the CLI `joe admin list` ([cmd/joe/admin.go:170-191](cmd/joe/admin.go#L170-L191)), which opens the DB directly. So the admin roster is invisible to the Web UI and to any API consumer.
- **Gating gap:** the four `/admin/*` read **and write** handlers do **not** call `requireAdmin` (see Launch Blockers).

### 6. Web UI admin surfaces (Stream J)

Complete route inventory ([ui/src/App.tsx:36-45](ui/src/App.tsx#L36-L45)): `/` Dashboard, `/graph`, `/sources`, `/chat`, `/chat/:sessionId`, `/admin` (admin-only), `/llm-settings` (admin-only).

Admin-only routes are wrapped in `RequireAdmin`, which redirects non-admins client-side based on the `/me` `is_admin` flag ([ui/src/auth/RequireAdmin.tsx:10-14](ui/src/auth/RequireAdmin.tsx#L10-L14)).

`AdminPage` ([ui/src/pages/AdminPage.tsx](ui/src/pages/AdminPage.tsx)) has tabs for:
- **Zones** — list + create ([components/admin/ZonesTable.tsx](ui/src/components/admin/ZonesTable.tsx), [ZoneForm.tsx](ui/src/components/admin/ZoneForm.tsx)); no edit/delete (matches backend §3).
- **Source-zone assignments** ([SourceZoneAssign.tsx](ui/src/components/admin/SourceZoneAssign.tsx)).
- **Policies** — list / create / delete ([PoliciesTable.tsx](ui/src/components/admin/PoliciesTable.tsx), [PolicyForm.tsx](ui/src/components/admin/PolicyForm.tsx)); principal is a free-text input.

`LLMSettingsPage` is the other admin page ([ui/src/pages/LLMSettingsPage.tsx](ui/src/pages/LLMSettingsPage.tsx)).

**Missing UI:** no user/principal-management page, no admin-roster page (consistent with backend gaps §4/§5). Purely backend (no UI): admin grant/revoke, zone CRUD beyond create, admin listing.

> Note: `RequireAdmin` is client-side only. It hides the page; it does **not** protect the API. See Launch Blockers.

### 7. CLI surfaces

Subcommand dispatcher: switch at [cmd/joe/main.go:651-666](cmd/joe/main.go#L651-L666). Full set: `panic`, `unlock`, `review`, `mcp`, `slack`, `skills`, `zone`, `admin` (bare invocation starts the server). Admin-related ones:

- **`joe zone <grant|revoke|list>`** ([cmd/joe/zone.go:77-117](cmd/joe/zone.go#L77-L117)) — opens the RBAC DB directly.
  - `grant --principal <user:|svc:> --zone <id>` → create policy ([zone.go:132-180](cmd/joe/zone.go#L132-L180)).
  - `revoke --principal <p> --zone <id>` → delete policy ([zone.go:182-206](cmd/joe/zone.go#L182-L206)).
  - `list [--principal <p>]` → list all / one principal's policies ([zone.go:208-236](cmd/joe/zone.go#L208-L236)).
- **`joe admin <list|grant|revoke>`** ([cmd/joe/admin.go:31-71](cmd/joe/admin.go#L31-L71)) — opens the RBAC DB directly.
  - `list` → all `admin_principals` ([admin.go:170-191](cmd/joe/admin.go#L170-L191)).
  - `grant --principal <p> [--reason ...]` → upsert admin ([admin.go:84-139](cmd/joe/admin.go#L84-L139)).
  - `revoke --principal <p>` → remove admin ([admin.go:145-168](cmd/joe/admin.go#L145-L168)).

Both subcommands talk to SQLite directly via `openRBACRepo`, **not** through the HTTP API — they require local filesystem/DB access on the server host.

---

## Investigation 2 — Incident Mode

### 1. Backend completeness (captain-session gate on the agentic loop)

**Confirmed: single gate implementation, composed into both agentic paths.**

- The pure decision function is `sessiongate.Check` ([internal/sessiongate/sessiongate.go:76-137](internal/sessiongate/sessiongate.go#L76-L137)); the only production wrapper around it is `captaingate.Wrapper.Execute` ([internal/captaingate/captaingate.go:99-197](internal/captaingate/captaingate.go#L99-L197), call at [captaingate.go:160](internal/captaingate/captaingate.go#L160)).
- **Path A — user task loop** (`/api/v1/tasks`, `/api/v1/tasks/stream`): `captaingate.New(executor, …)` at [internal/api/tasks.go:253](internal/api/tasks.go#L253).
- **Path B — Core Agent / durable executor:** `captaingate.New(durable, …)` at [cmd/joe/server.go:656](cmd/joe/server.go#L656) (wiring comment [server.go:644-645](cmd/joe/server.go#L644-L645)).
- **Structural invariant test:** `TestPhaseG_SingleSharedCaptainGateImplementation` ([internal/captaingate/single_impl_guard_test.go:35](internal/captaingate/single_impl_guard_test.go#L35)) is an AST guard asserting `sessiongate.Check` is called from exactly one production package; a companion import guard lives at [internal/sessiongate/import_guard_test.go](internal/sessiongate/import_guard_test.go).

**What the gate does** ([internal/sessiongate/sessiongate.go:53-161](internal/sessiongate/sessiongate.go#L53-L161)):
- T1 reads always pass; in **normal** regime everything passes.
- In **incident** regime: it finds the active incident session, then refuses any mutating (T2/T3) call that is not (a) on the active incident session and (b) by the principal who is the current captain — redirecting the caller to the incident session. No captain yet (`pending_captain`) ⇒ refuse.
- **Declare:** human path `POST /api/v1/regime/declare` — the declaring human atomically becomes captain ([internal/api/regime.go:74-171](internal/api/regime.go#L74-L171)). Authorized by RBAC zone `regime-control` action `declare_incident` ([regime.go:128](internal/api/regime.go#L128)). Joe-autonomous declare is an inert seam, 403 in Phase 1 ([regime.go:109-113](internal/api/regime.go#L109-L113)).
- **Resolve:** `POST /api/v1/regime/resolve` ([regime.go:184-261](internal/api/regime.go#L184-L261)), authorized by `regime-control`/`resolve_incident` ([regime.go:217](internal/api/regime.go#L217)); requires the incident to have reached `believed_mitigated` ([regime.go:249-251](internal/api/regime.go#L249-L251)), then transitions session→`resolved` and regime→normal.

### 2. Incident state storage

- **Session state:** `agent_sessions.incident_state` enum {declared, being_worked, believed_mitigated, resolved, reviewed}; only `type='incident'` sessions may set it ([internal/store/migrations/009_session_model.up.sql:17-34](internal/store/migrations/009_session_model.up.sql#L17-L34)).
- **Current regime (mutable, single row):** `system_regime(mode, declared_at, declared_by_principal, declared_kind)` ([009_session_model.up.sql:41-54](internal/store/migrations/009_session_model.up.sql#L41-L54)). `declared_by_principal` is nulled on resolve.
- **Durable history:** `audit_log` with `kind` discriminator `regime_transition` ([internal/store/migrations/015_audit_log.up.sql](internal/store/migrations/015_audit_log.up.sql)), append-only (code + DB triggers). Phase F deliberately writes the declare/resolve record here *before* touching `system_regime`, so history survives resolve.
- **Writers:** declare → `DeclareIncidentRegime[WithHook]` ([internal/sessionmodel/regime_transitions.go:32-118](internal/sessionmodel/regime_transitions.go#L32-L118)); resolve → `ResolveIncidentRegime[WithHook]` ([regime_transitions.go:135-222](internal/sessionmodel/regime_transitions.go#L135-L222)); state advance → `UpdateIncidentState` ([internal/sessionmodel/repository.go:244-254](internal/sessionmodel/repository.go#L244-L254)); audit row → `writeRegimeAudit` ([internal/api/regime.go:270-291](internal/api/regime.go#L270-L291)).
- **Query current state:** `GET /api/v1/regime` → `GetRegime` ([internal/api/regime.go:59-65](internal/api/regime.go#L59-L65)).
- **Query past incidents:** no dedicated endpoint. History lives in `audit_log` (`kind=regime_transition`) and in resolved `agent_sessions` rows, but there is no HTTP/CLI surface that lists past incidents.

### 3. Incident invocation surfaces

| Operation | HTTP | CLI |
|---|---|---|
| Declare | `POST /api/v1/regime/declare` ([regime.go:55](internal/api/regime.go#L55), handler [74-171](internal/api/regime.go#L74-L171)) | **none** |
| Resolve | `POST /api/v1/regime/resolve` ([regime.go:56](internal/api/regime.go#L56), handler [184-261](internal/api/regime.go#L184-L261)) | **none** |
| Read regime | `GET /api/v1/regime` ([regime.go:54](internal/api/regime.go#L54)) | **none** |

Both write paths are HTTP-only and RBAC-gated on the `regime-control` zone. **No CLI subcommand** for declare/resolve/read exists (the subcommand switch at [cmd/joe/main.go:651-666](cmd/joe/main.go#L651-L666) has none; the one "incident" string in `cmd/joe` is the unrelated `unlock` usage text at [main.go:130](cmd/joe/main.go#L130)).

### 4. Web UI surfaces

**None.** A full search of `ui/src/` found no incident/captain/regime page, route, component, banner, or API call. The `/api/v1/regime` endpoints are not called from the frontend; there is no client for declare/resolve/read. The Dashboard's `AlertsList` ([ui/src/components/dashboard/AlertsList.tsx](ui/src/components/dashboard/AlertsList.tsx)) shows infrastructure alerts only — unrelated to system regime.

### 5. Visibility for non-admin users

**None.** Incident state is not surfaced anywhere in the UI — no app-shell banner, no chat indicator. The chat components ([ui/src/pages/ChatPage.tsx](ui/src/pages/ChatPage.tsx), [components/chat/ChatWindow.tsx](ui/src/components/chat/ChatWindow.tsx), [MessageList.tsx](ui/src/components/chat/MessageList.tsx)) have no regime-aware logic. Consequence: when a regime is active, a non-captain user's T2/T3 actions are refused by the gate, but the UI gives **no explanation** — the user sees tool failures with no indication the system is in incident mode or who the captain is.

---

## Prioritized Gaps

### 🚫 LAUNCH BLOCKERS

1. **RBAC admin API is not admin-gated → privilege escalation.** The handlers in [internal/api/admin.go](internal/api/admin.go) (`POST /admin/zones`, `POST /admin/policies`, `POST /admin/source-zones`, `DELETE /admin/policies/{id}`) require only bearer auth — they never call `requireAdmin` ([internal/api/admingate.go:38](internal/api/admingate.go#L38)), which today gates only the LLM settings/usage endpoints. **Any authenticated principal — including a brand-new zero-zone OIDC user — can create a zone with arbitrary allowed-actions and grant themselves a policy into it**, fully escalating their own access. The UI's `RequireAdmin` is client-side only and does not protect the API.
   *Fix:* add `if _, gated := s.requireAdmin(w, r); gated { return }` at the top of each write handler in admin.go (and ideally the reads too, since they leak the full authorization map). Add a regression test asserting a non-admin principal gets 403.

2. **`GET /api/v1/regime` and the regime reads are part of an otherwise-gated surface but the admin write API above can be abused to grant `regime-control`.** This is a direct consequence of Blocker 1 (a non-admin can grant themselves `declare_incident`/`resolve_incident`), so it is fixed by Blocker 1 but must be verified in the same regression test: confirm a non-admin cannot reach declare/resolve after the admin-gate fix.

### 📝 DEFER WITH HONEST DOCUMENTATION

3. **Admin promotion/demotion is CLI-only (+ bootstrap email).** Acceptable for v1. *Docs needed:* operator guide section — "Promoting admins: run `joe admin grant --principal user:<email>` on the server host; the first admin comes from `auth.admin_email`. No UI in v1."

4. **Zone editing/deletion not implemented (create-only).** Acceptable if known. *Docs needed:* "Zones are immutable after creation and cannot be deleted in v1; to change allowed-actions, create a new zone and reassign. UI shows create-only."

5. **Incident declare/resolve are API-only (no UI, no CLI).** Acceptable for v1 if operators are told how. *Docs needed:* runbook with the exact `curl` for `POST /api/v1/regime/declare` and `/resolve`, the `regime-control` zone grant prerequisite, and the `believed_mitigated`-before-resolve requirement.

6. **No incident-mode visibility in the UI (no banner, no chat indicator).** Acceptable for v1 only if documented, because the failure mode is confusing. *Docs needed:* "During an active incident, non-captain users' write actions are refused server-side; the UI shows generic tool-failure errors. A status banner is planned for v1.1." Strongly consider a one-line app-shell banner reading `GET /api/v1/regime` as a cheap v1 mitigation.

7. **No HTTP/UI way to list admins, and no "past incidents" query.** Acceptable for v1. *Docs needed:* "Use `joe admin list` to see admins; incident history is in the audit log (`kind=regime_transition`), queryable only via DB in v1."

8. **No onboarding empty-state for zero-zone users.** A freshly signed-in user with no zones gets silent read-only behavior. *Docs needed:* "New users have read-only (`unassigned` zone) access until an admin grants a zone" — or add a small empty-state hint in the UI.

### ✅ NON-ISSUES (looked at, confirmed fine)

- **Captain-gate backend is complete and correctly composed.** Single `sessiongate.Check`, wrapped only by `captaingate`, composed into both the task loop ([tasks.go:253](internal/api/tasks.go#L253)) and the durable/Core executor ([server.go:656](cmd/joe/server.go#L656)), with an AST invariant test enforcing the single implementation. (§Inv2.1)
- **Incident history survives resolve.** Phase F's audit-before-mutate ordering ([regime.go:142-154](internal/api/regime.go#L142-L154)) means the durable `audit_log` record persists even though `system_regime` is nulled. (§Inv2.2)
- **Declare/resolve are RBAC-gated and fail-closed on audit-write failure** ([regime.go:128-140](internal/api/regime.go#L128-L140), [217-237](internal/api/regime.go#L217-L237)). The authorization model itself is sound; the only weakness is the upstream admin-API gap (Blocker 1).
- **New-user onboarding defaults to least privilege** (`unassigned`/read-only), not to an open default. Safe-by-default. (§Inv1.1)
- **Web UI admin pages for zones/source-zones/policies exist** and are client-gated for non-admins ([AdminPage.tsx](ui/src/pages/AdminPage.tsx), [RequireAdmin.tsx](ui/src/auth/RequireAdmin.tsx)) — they just need the server-side gate of Blocker 1 behind them.
- **`/me` is intentionally not admin-gated** and returns exactly the fields the UI needs ([currentuser.go:36-57](internal/api/currentuser.go#L36-L57)). Correct by design.

---

*No code was modified. Awaiting review before any follow-up work.*
