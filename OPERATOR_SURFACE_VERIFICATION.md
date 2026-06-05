# Operator Surface Verification — Incident Mode & User Management

Read-only verification performed 2026-06-04 against current `main` (HEAD `2b16665`).
Every claim is cited to `file:line`. Verdicts are PRESENT / PARTIAL / ABSENT — no hedging.
Verified against live code, not against `ADMIN_SURFACE_AUDIT.md` or prior summaries.

Required reading consulted: `docs/DECISIONS.md` D-0010 (`internal/api/regime.go`-relevant captain gate), D-0011 (admin capability), D-0012 (admin gate), D-0013 (admin audit); `ADMIN_SURFACE_AUDIT.md` (still in tree).

---

## SECTION 1 — INCIDENT MODE

### 1. Backend enforcement — captain gate on both agentic paths, single shared impl, AST-guarded
**PRESENT.**

- **Gate package / single implementation:** `internal/captaingate/captaingate.go`. The sole production call to the gate primitive `sessiongate.Check` is at `internal/captaingate/captaingate.go:160`. A repo-wide grep confirms no other production package calls `sessiongate.Check`.
- **Composition site 1 — user task loop (`/api/v1/tasks` + `/tasks/stream`):** `internal/api/tasks.go:270` — `loopExec = captaingate.New(executor, h.server.services.SessionModel, h.server.services.Audit)`.
- **Composition site 2 — Core Agent onboarding/refresh executor:** `cmd/joe/server.go:658` — `gated := captaingate.New(durable, services.SessionModel, services.Audit)`. (Note: D-0010 cites `cmd/joe-core/main.go:520-531`; that entrypoint has since merged into `cmd/joe/server.go` — the composition is intact, the line reference in the decision log is stale.)
- **AST invariant test:** `internal/captaingate/single_impl_guard_test.go` — `TestPhaseG_SingleSharedCaptainGateImplementation` parses the whole repo and fails if any production package other than `internal/captaingate` calls `sessiongate.Check`.

Confirmed exactly as D-0010 describes.

### 2. Declare endpoint
**PRESENT.**

- **Route:** `internal/api/regime.go:55` — `POST {prefix}/regime/declare`.
- **Handler:** `internal/api/regime.go:74` (`regimeHandler.declare`).
- **Gate (regime-control zone, NOT admin):** `internal/api/regime.go:128` — `h.policy.HasZoneAccess(..., "regime-control", rbac.ActionDeclareIncident)`.
- **Audit (Phase F):** `internal/api/regime.go:147` writes the allow row, `:129` the deny row, via `writeRegimeAudit` (`internal/api/regime.go:270-291`) → `audit.KindRegimeTransition`, written BEFORE the mutation, fail-closed (`regime.go:290`).

### 3. Resolve endpoint
**PRESENT.**

- **Route:** `internal/api/regime.go:56` — `POST {prefix}/regime/resolve`.
- **Handler:** `internal/api/regime.go:184` (`regimeHandler.resolve`).
- **Gate:** `internal/api/regime.go:217` — `HasZoneAccess(..., "regime-control", rbac.ActionResolveIncident)`.
- **Audit:** `internal/api/regime.go:233` (allow) / `:218` (deny) via `writeRegimeAudit`, `KindRegimeTransition`, before mutation, fail-closed.

### 4. Active-state query endpoint
**PRESENT.**

- **Route:** `internal/api/regime.go:54` — `GET {prefix}/regime` → handler `regimeHandler.read` (`internal/api/regime.go:59`) → `repo.GetRegime` (`internal/sessionmodel/repository.go:306`).
- **Current details returned:** the `Regime` struct (`internal/sessionmodel/types.go:64-69`) carries `Mode`, `DeclaredAt`, `DeclaredByPrincipal`, `DeclaredKind` — i.e. whether incident mode is active and who declared it / when / what kind. Adequate for "is an incident active, with current details."

### 5. History query endpoint (past incidents)
**ABSENT.**

- The only routes registered are `GET /regime`, `POST /regime/declare`, `POST /regime/resolve` (`internal/api/regime.go:54-56`). There is no `/regime/history` or any HTTP read of past declare/resolve pairs.
- The durable history exists in the append-only `audit_log` (kind `regime_transition`, written at `internal/api/regime.go:147,233`), but there is **no** HTTP endpoint that queries it. The `audit.Repository` interface exposes only `Insert` (no read method) — confirmed by D-0009's append-only design. History is reachable only by direct SQL.

### 6. CLI surface (`joe incident` / declare / resolve / list)
**ABSENT.**

- The subcommand dispatch (`cmd/joe/main.go:650-666`) handles exactly: `panic`, `unlock`, `review`, `mcp`, `slack`, `skills`, `zone`, `admin`. No `incident` or `regime` subcommand exists. A repo-wide grep for incident/regime under `cmd/` returns only comments and the server entrypoint, no cobra/flag command.

> Consequence worth flagging: items 5, 6, 7 together mean there is **no operator-facing surface at all** to declare or resolve an incident — only a raw authenticated `POST /regime/declare`. The enforcement (item 1) is solid; the human trigger is curl-only.

### 7. Web UI declare/resolve page or modal
**ABSENT.**

- A grep of all of `ui/src/` for `incident`, `regime`, `captain`, `declare`, `resolve` returns **zero matches** (confirmed by sub-agent sweep of components, pages, hooks, api client, router). No declare/resolve UI exists.

### 8. Web UI active-incident banner / indicator
**ABSENT.**

- The `/me` payload shape (`ui/src/api/schemas.ts:98-105`, `CurrentUserSchema`) carries only `principal`, `is_admin`, `rbac_enabled`, `oidc_enabled` — **no** regime/incident field. No component fetches `GET /regime`. There is no incident-state-aware rendering anywhere; non-admin users see no indication that writes are blocked during an incident.

### 9. Failed-write user feedback specific to incident mode
**ABSENT.**

- Error handling is generic. `ui/src/hooks/useChat.ts:207-213` renders a `failureLabel` + server `errorMessage`; `ui/src/api/client.ts:70-81` surfaces a generic `ApiRequestError{status,message}`. No branch distinguishes an incident-mode block from any other 403. When the captain gate refuses a write (`captaingate` `GateRefusalError`), the UI shows an undifferentiated error.

---

## SECTION 2 — USER MANAGEMENT

### 10. New-user OIDC onboarding path
**PRESENT** (path located and verified end to end).

When a previously-unseen, email-verified address completes OIDC:
- **Identity derived:** `internal/auth/handlers.go:184` — `PrincipalFromClaims(vt.Claims)`.
- **No admin grant** unless the email matches the configured `admin_email`: `internal/auth/handlers.go:200-209` (gated on `strings.EqualFold(vt.Claims.Email, h.adminEmail)`). A normal new user skips this entirely.
- **Session row created:** `internal/auth/handlers.go:211` — `h.sessions.Mint(ctx, principal)` → `internal/auth/session.go:56-72` → `repo.CreateSession(ctx, s)` persists a row with the principal string (`session.go:68`).
- **"Principal materialized":** there is no `principals` table — the principal exists only as the `Principal` string column on the session row (`internal/auth/session.go:62-67`). This is the design (D-0011 keeps admin in `admin_principals`; ordinary identity is just the session row).
- **Zero zones:** no `CreatePolicy` / zone assignment occurs anywhere in the callback path (`internal/auth/handlers.go:131-229`). A new user therefore resolves, via the RBAC engine, to no grants (effectively the read-only `unassigned` posture). Confirmed: session row ✓, principal-as-session-string ✓, zero zones ✓.

### 11. Zero-zone user UI feedback
**ABSENT.**

- The chat surface renders without any zone-awareness: `ui/src/pages/ChatPage.tsx` → `ui/src/components/chat/ChatWindow.tsx` have no empty-state for "no zones." The `/me` schema (`ui/src/api/schemas.ts:98-105`) does not even return the caller's zone assignments, so the UI cannot detect the zero-zone condition. A new user sees normal chat, then opaque denials with no "ask your admin" context.

### 12. Zone CRUD via HTTP
- **Create — PRESENT.** `internal/api/admin.go:54` (`POST /admin/zones` → `createZone`). Admin-gated `:136`; audited fail-closed before mutation `:153` (`audit.ActionAdminZoneCreate`).
- **Delete (`DELETE /admin/zones/{id}`) — ABSENT.** No such route in `registerAdminRoutes` (`internal/api/admin.go:53-63`).
- **Edit allowed-actions (`PUT`/`PATCH /admin/zones/{id}`) — ABSENT.** No such route.

Net: zone HTTP is create-only; changing a zone's `allowed_actions` or removing a zone requires DB-level intervention.

### 13. Policy CRUD via HTTP
- **Grant — PRESENT.** `internal/api/admin.go:60` (`POST /admin/policies` → `createPolicy`). Gated `:245`; audited fail-closed `:260` (`ActionAdminPolicyGrant`).
- **Revoke — PRESENT.** `internal/api/admin.go:61` (`DELETE /admin/policies/{id}` → `deletePolicy`). Gated `:277`; audited fail-closed with before-state capture `:302` (`ActionAdminPolicyRevoke`).
- **List — PRESENT.** `internal/api/admin.go:59` (`GET /admin/policies` → `listPolicies`). Gated `:226`; read-class audit fail-open `:236` (`ActionAdminPolicyRead`).

All three admin-gated (post-D-0012) and audited (post-D-0013).

### 14. Source-zone assignment via HTTP
**PRESENT.**

- `internal/api/admin.go:57` — `POST /admin/source-zones` → `assignSourceZone`. Gated `:188`; audited fail-closed with before-state `:210` (`ActionAdminSourceZoneAssign`). List at `:56` (`GET /admin/source-zones`, gated `:170`, read-audit `:179`). Unassigned roster at `:63`.

### 15. Admin roster query (HTTP)
**ABSENT.**

- No `GET /admin/admins` route exists. The only references in the codebase are the two guard tests naming it as an endpoint that *doesn't exist yet* (`internal/api/admin_gate_guard_test.go:27`, `internal/api/admin_audit_guard_test.go:32`). `ListAdmins` remains CLI-only (`joe admin list`, below). No HTTP endpoint has been added since `ADMIN_SURFACE_AUDIT.md`.

### 16. Admin promotion / demotion (HTTP)
**ABSENT.**

- No HTTP route grants or revokes admin. The only paths are (a) bootstrap on configured `admin_email` login (`internal/auth/handlers.go:200-209`) and (b) the CLI (`joe admin grant/revoke`). No `/api/v1/admin/admins` POST/DELETE exists.

### 17. CLI surfaces for user/zone/admin management
**PRESENT** (enumerated):

| Command | Args | Action | Source |
|---|---|---|---|
| `joe zone grant` | `--principal <user:\|svc:>` `--zone <zone-id>` | Create a principal→zone policy | `cmd/joe/zone.go:106` → `runZoneGrant` `:132` |
| `joe zone revoke` | `--principal` `--zone` | Delete a principal→zone policy | `cmd/joe/zone.go:108` → `runZoneRevoke` `:182` |
| `joe zone list` | `[--principal]` | List policies (optionally filtered) | `cmd/joe/zone.go:110` → `runZoneList` `:208` |
| `joe admin grant` | `--principal <user:\|svc:>` `[--reason]` | Upsert `admin_principals` row; cleans up snapshot grants | `cmd/joe/admin.go:60` → `runAdminGrant` `:84` |
| `joe admin revoke` | `--principal` | Remove admin row (re-granted on next admin_email login — D-0011 caveat) | `cmd/joe/admin.go:62` → `runAdminRevoke` `:145` |
| `joe admin list` | — | Print admin roster (principal, granted_by, granted_at) | `cmd/joe/admin.go:64` → `runAdminList` `:170` |

Usage banners: `cmd/joe/zone.go:79-84`, `cmd/joe/admin.go:33-39`. There is **no** CLI for zone create/delete or source-zone assignment (those are HTTP-only); `joe zone` operates on policies, not zone definitions.

### 18. Web UI user-management page (users/principals + zones + admin status)
**ABSENT.**

- The admin page (`ui/src/pages/AdminPage.tsx`) has three tabs only — Zones, Sources, Policies (`AdminPage.tsx:63-96`) — and no Users/Principals tab. The sidebar (`ui/src/components/layout/Sidebar.tsx:16-23`) routes to Dashboard, Graph, Sources, Chat, Admin, LLM Settings — no users page. `PoliciesTable.tsx` shows principals only as a column of existing policies, not a roster of all users.

### 19. Web UI zone-management page (create/edit/delete)
**PARTIAL.**

- **Create — PRESENT:** `ui/src/pages/AdminPage.tsx:72` (button) → `ZoneForm` `:99-104` → `POST /api/v1/admin/zones` (`ui/src/api/security.ts:12-21`).
- **Edit — ABSENT:** `ui/src/api/security.ts:23-25` — `updateZone` returns `Promise.reject(new Error('Update zone not supported'))`.
- **Delete — ABSENT:** `ui/src/api/security.ts:27-29` — `deleteZone` returns `Promise.reject(new Error('Delete zone not supported'))`.

(Edit/delete are stubbed in the client because no backend route exists — consistent with item 12.)

### 20. Web UI policy/grant page (grant/revoke)
**PRESENT.**

- **Grant:** `ui/src/pages/AdminPage.tsx:88` (button) → `PolicyForm` `:106-112` → `POST /api/v1/admin/policies` (`ui/src/api/security.ts:76-83`).
- **Revoke:** `ui/src/components/admin/PoliciesTable.tsx:50` (Delete button) → `DELETE /api/v1/admin/policies/{id}` (`ui/src/api/security.ts:85-87`).
- Both behind the `/admin` route, admin-gated client-side by `RequireAdmin` (`ui/src/auth/RequireAdmin.tsx:11-12`). No in-place edit, but grant + revoke is the full operator loop. PRESENT.

### 21. Web UI admin-roster page (who is admin)
**ABSENT.**

- No page lists admins. The current user's own `is_admin` flag drives nav visibility only (`ui/src/components/layout/Sidebar.tsx:31`); there is no view of the `admin_principals` roster.

---

## SECTION 3 — CROSS-CUTTING

### 22. RequireAdmin coverage + gate distinction
**Confirmed post-fix; two distinct gates.**

- **Admin gate (`requireAdmin`) on all eight RBAC admin handlers:** `listZones` `internal/api/admin.go:116`, `createZone` `:136`, `listAssignments` `:170`, `assignSourceZone` `:188`, `listPolicies` `:226`, `createPolicy` `:245`, `deletePolicy` `:277`, `listUnassigned` `:318`. Gate impl: `internal/api/admingate.go:41` (`requireAdmin` → `services.RBAC.IsAdmin`, fail-closed on error `admingate.go:60-69`). Structurally enforced by `internal/api/admin_gate_guard_test.go` (`TestAdminRoutes_AllRequireAdminGate`).
- **Incident declare/resolve use a DIFFERENT gate — the regime-control zone, not the admin capability:** `internal/api/regime.go:128` and `:217` call `HasZoneAccess(..., "regime-control", ...)`. This is by design (D-0010 / D-0012): incident control is a *zone capability* (`can_declare_incident` / `can_resolve_incident`), deliberately decoupled from `IsAdmin`. The two gates are not interchangeable and the code keeps them separate. Correct as designed.

### 23. Audit coverage of incident endpoints
**PRESENT.**

- D-0013 wired `recordAdminAudit` into the eight admin handlers (cited in items 12-14). Independently, the incident endpoints **do** write audit rows: `declare` writes allow/deny rows (`internal/api/regime.go:147`, `:129`) and `resolve` writes allow/deny rows (`:233`, `:218`), both via `writeRegimeAudit` (`regime.go:270-291`) as `audit.KindRegimeTransition`, BEFORE the mutation, fail-closed (`regime.go:290`). So the audit picture for declare/resolve — left unverified by `ADMIN_SURFACE_AUDIT.md` — is confirmed complete.

### 24. Structural invariants vs. new admin endpoints
**Confirmed; no new endpoint escaped — with one scope note.**

- `internal/api/admin_gate_guard_test.go` (`TestAdminRoutes_AllRequireAdminGate`) and `internal/api/admin_audit_guard_test.go` (`TestAdminRoutes_AllAuditOnAllow`) both parse `admin.go`, extract every handler registered by `registerAdminRoutes`, and fail the build if any lacks `requireAdmin` (gate) or `recordAdminAudit` (audit). They cover endpoints that don't exist yet.
- No new route has been added under `/api/v1/admin/` since D-0013 — the registration set is still the original eight (`internal/api/admin.go:53-63`). The guards are therefore green and have nothing new to catch. **Not a finding.**
- **Scope note (by design, not a gap):** the incident endpoints live under `/api/v1/regime`, NOT `/api/v1/admin/`, so the two admin guards do not — and should not — cover them. The regime surface has its own structural invariant: the single-resolve-call-site AST guard (`internal/api/regime_invariant_test.go`) and the captain-gate single-impl guard (item 1). Coverage is partitioned correctly by surface.

---

## CONCLUSIONS

- **Incident mode is PARTIAL.** Backend is strong: enforcement on both agentic paths (1), declare (2), resolve (3), active-state query (4), and full audit (23) are PRESENT and AST-guarded. Missing: **history query endpoint (5 — ABSENT)**, **CLI surface (6 — ABSENT)**, **Web UI declare/resolve (7 — ABSENT)**, **active-incident banner (8 — ABSENT)**, **incident-specific failed-write feedback (9 — ABSENT)**. The enforcement is real but has no human-facing trigger or feedback surface.

- **User management is PARTIAL.** OIDC onboarding (10), policy grant/revoke/list HTTP (13), source-zone assignment HTTP (14), the CLI surfaces (17), the zone-create + policy-grant/revoke Web UI (19 create / 20) are PRESENT, all admin-gated and audited. Missing: **zero-zone UI feedback (11 — ABSENT)**, **zone delete + edit-allowed-actions HTTP (12 — ABSENT)**, **admin-roster HTTP (15 — ABSENT)**, **admin promotion/demotion HTTP (16 — ABSENT)**, **user-management UI page (18 — ABSENT)**, **zone edit/delete UI (19 — PARTIAL)**, **admin-roster UI (21 — ABSENT)**.

---

## PRIORITIZED GAPS

### LAUNCH BLOCKERS
- **No operator surface to declare/resolve an incident at all (items 6 + 7).** Enforcement exists, but the only way to enter incident mode is a hand-crafted authenticated `POST /regime/declare`. If incident mode is part of the launch narrative, an operator must be able to trigger it without curl — minimally a CLI (`joe incident declare/resolve`), ideally the admin UI modal. Shipping the enforcement without the trigger reads as a non-feature.
- **Zero-zone new user dead-ends silently (items 11 + 9).** A brand-new OIDC user lands in a fully-rendered chat UI (10 confirms the session is minted with zero zones) and every write 403s with a generic error and no "you have no zones — ask your admin" guidance. First-run experience is a wall of opaque denials. Combined with item 8 (no incident banner), the UI never explains *why* a write was refused — incident mode and zero-zone look identical to the user.

### DEFER WITH HONEST DOCUMENTATION
- **Incident history endpoint (item 5).** The durable record exists in `audit_log`; only a read API is missing. Acceptable for v1 if docs state incident history is queried at the DB/audit layer, not via HTTP.
- **Admin roster + promotion over HTTP/UI (items 15, 16, 18, 21).** Fully covered by the CLI (`joe admin list/grant/revoke`, item 17). Acceptable if the README states admin management is operator-on-host CLI, matching the `joe zone` model (D-0011's stated posture).
- **Zone edit/delete (items 12, 19).** Zone *create* + policy grant/revoke cover the common path; changing `allowed_actions` currently means recreate, and deletion is DB-level. Acceptable for v1 if documented as "zones are create-and-grant; edits via reprovisioning."

### NON-ISSUES
- Incident declare/resolve gated on the **regime-control zone** rather than the admin capability (item 22) — this is the intended design (D-0010), not a missing admin gate.
- The admin AST guards **not** covering the regime endpoints (item 24) — correct partitioning; regime has its own invariants (`regime_invariant_test.go`, captain single-impl guard).
- No `principals` table backing onboarded users (item 10) — by design; the session row is the principal record, admin status lives in `admin_principals`.
- D-0010's stale `cmd/joe-core/main.go` line reference for the Core Agent composition site — the composition is intact at `cmd/joe/server.go:658`; only the decision-log citation predates the entrypoint merge.
