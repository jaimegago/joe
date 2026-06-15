# Component credential registration surface — current-state investigation

**Date:** 2026-06-10
**Type:** read-only investigation (no code changed)
**Scope:** How an operator creates/updates a `store.Component` and sets its credential config today, end to end, and whether the registration path is a gated/audited security surface or bypasses the access seam.

All claims below were re-derived against the live tree. Where the code contradicts an assumption in the tasking prompt or the D-0026 ADR, the contradiction is called out explicitly.

---

## PART A — the registration INPUT surface

### A.1 Every entry point that creates or updates a `store.Component`

**REST API (the only operator-facing create path):**

- `POST /api/v1/components` → `sourceHandler.handleCreate` ([server.go:158](internal/api/server.go:158), [server.go:320](internal/api/server.go:320)) → `Server.handleCreateComponent` ([components.go:119](internal/api/components.go:119)). This is the sole operator entry point that creates a `store.Component`.
- `DELETE /api/v1/components/{id}` → `handleDeleteComponent` ([components.go:220](internal/api/components.go:220), routed at [server.go:160](internal/api/server.go:160)).
- `GET /api/v1/components` / `GET /api/v1/components/{id}` → list/get ([components.go:36](internal/api/components.go:36), [components.go:196](internal/api/components.go:196)).
- `POST /api/v1/components/{id}/test` → `webUIHandler.handleTestComponent` ([webui.go:645](internal/api/webui.go:645), routed at [webui.go:729](internal/api/webui.go:729)). Re-connects an existing component; does not create/update the row's config (it only writes sync-status via `UpdateSyncStatus`, [webui.go:680](internal/api/webui.go:680)/[webui.go:695](internal/api/webui.go:695)).

**There is NO update (PUT/PATCH) endpoint for a component.** The route table registers only `GET`, `POST`, `GET /{id}`, `DELETE /{id}` ([server.go:157-160](internal/api/server.go:157)). `grep` for `PUT.*components`, `PATCH.*components`, `handleUpdateComponent` returns nothing. **This contradicts the tasking prompt's repeated phrasing of a "create/update-component path": no update path exists.** To change a component's config an operator must `DELETE` then `POST` again (full re-create), or edit the DB directly. The D-0026 ADR's phrase "component-management paths" is therefore accurate only for create + delete + test.

**CLI:** There is **no** `register_component` subcommand and no `joe` subcommand that creates a component. The dispatcher's full subcommand set is `panic`, `unlock`, `mcp`, `slack`, `skills`, `incident` ([main.go:591-608](cmd/joe/main.go:591)). (Note: CLAUDE.md lists `review`, `zone`, `admin` as subcommands — those do **not** exist in the live dispatcher; tangential to this investigation but a doc-drift contradiction. The comment at [main.go:613](cmd/joe/main.go:613) confirms RBAC zone/admin provisioning "is no longer a CLI surface — it runs over the admin REST API".)

**Web UI:** There is **no** create/update form. `ComponentsPage.tsx` renders a list with filters, a detail card, **Test Connection**, and **Remove** only ([ComponentsPage.tsx:33-238](ui/src/pages/ComponentsPage.tsx:33)). The API client `components.ts` exposes `fetchComponents`, `fetchComponent`, `testComponent`, `deleteComponent` — and **no `createComponent`/`updateComponent`** ([components.ts:1-27](ui/src/api/components.ts:1)). **The Web UI cannot register or edit a component at all today** — it is a read/test/delete console. Component creation is REST-only (e.g. `curl`, scripts, MCP).

**Non-operator (autonomous) create paths**, for completeness — these are agent/discovery writers, not operator registration: `internal/coreagent/discovery.go`, `internal/coreagent/git_refresh.go`, `internal/coreagent/agent.go`, `internal/knowledge/drafts/generator.go`, `internal/knowledge/proposals/service.go` all call `Components.Create`. They are out of scope for the operator-input question but share the same store writer.

### A.2 How the Config JSON is supplied — typed fields vs opaque blob

**Opaque blob, hand-authored.** The create request body is:

```go
type createComponentRequest struct {
    ID     string          `json:"id"`
    Type   string          `json:"type"`
    Name   string          `json:"name"`
    Config json.RawMessage `json:"config"`   // <-- opaque
}
```

[components.go:112-117](internal/api/components.go:112). `Config` is `json.RawMessage` — captured verbatim and stored unmodified onto `store.Component.Config` ([components.go:170-175](internal/api/components.go:170)). **No individual credential field is surfaced as a discrete input** anywhere in the create path: not `token`, not `credential_provider`, not `kubeconfig`, not `context`, not `value`, not `env_var`, not `in_cluster`. The operator hand-authors the entire `config` object as raw JSON and the API passes it through untouched.

The discrete fields that *are* surfaced are only the three component-identity scalars (`id`, `type`, `name`) plus the raw `config` passthrough. The per-provider credential fields exist only as Go struct tags read **later, inside the adapter**, never at the HTTP boundary:
- static provider: `value`, `env_var`, `credential_provider`, `audience` ([static.go:12-17](internal/credential/static.go:12)).
- kubeconfig-exec provider: `kubeconfig`, `context`, `in_cluster`, `credential_provider`, `audience` ([kubeconfig_exec.go:22-25](internal/credential/kubeconfig_exec.go:22)).

So the answer to the core Part A question — *is setting `credential_provider` a form field / CLI flag, or hand-authored JSON?* — **is hand-authored JSON.** There is no form field and no CLI flag; the operator must place `"credential_provider": "..."` (and the matching per-provider keys) inside the raw `config` blob by hand.

### A.3 Validation / documentation of the credential discriminator at registration time

**Registration-time validation is limited to component identity, not credential config:**

`handleCreateComponent` validates exactly three things before persisting:
1. `id`/`type`/`name` are non-empty ([components.go:132-147](internal/api/components.go:132)).
2. `type` is a known component type via `store.IsValidComponentType` ([components.go:149-155](internal/api/components.go:149)).
3. No component with that `id` already exists ([components.go:158-168](internal/api/components.go:158)).

**No validation of the `config` contents, the `credential_provider` discriminator, or the per-provider fields happens at the HTTP boundary.** `json.RawMessage` only guarantees the bytes are syntactically valid JSON (enforced by the outer `json.Unmarshal` at [components.go:127](internal/api/components.go:127)); it does not validate shape, required keys, or discriminator value.

**However, `Connect` is invoked synchronously during create** ([components.go:181](internal/api/components.go:181) — see Part B), and `Connect` runs the adapter's `ParseConfig` plus `credential.Select`/`Resolve`. So for **adapter-backed types**, a malformed `config` or unresolvable credential *does* fail the create with a `400` (`writeBadRequest`, [components.go:182](internal/api/components.go:182)). This is **Connect-time validation that happens to run at registration**, not registration-time schema validation:
- k8s adapter `Connect` → `ParseConfig(source.Config)` ([k8s.go:59](internal/adapters/k8s/k8s.go:59)) → `applyResolvedCredential` → `credential.Select` + `provider.Resolve` ([k8s.go:69](internal/adapters/k8s/k8s.go:69), [k8s.go:130-148](internal/adapters/k8s/k8s.go:130)).
- `credential.Select` reads the `credential_provider` discriminator; an **absent discriminator silently defaults to `KindStatic`** ([credential.go:28-30](internal/credential/credential.go:28), [provider.go:59-62](internal/credential/provider.go:59)). An unknown/typo'd value is the only discriminator that errors.

For **config-only / metadata types** (those where `newAdapterForType` returns `nil`, [components.go:106-108](internal/api/components.go:106)), `Connect` is skipped entirely ([components.go:180](internal/api/components.go:180)) and **the `config` blob is persisted with zero validation of any kind.**

**Documentation:** the field names are not surfaced or documented at registration. An operator must read `internal/credential/static.go` and `internal/credential/kubeconfig_exec.go` (or the adapter `config.go` files) to learn that `credential_provider`, `value`, `env_var`, `kubeconfig`, `context`, `in_cluster` are the keys. There is no schema endpoint, no OpenAPI for the body, no example surfaced by the API or UI.

---

## PART B — registration as a SECURITY surface

### B.1 Does create invoke `adapter.Connect`, and via the seam or directly?

**Confirmed: create calls `adapter.Connect` DIRECTLY, bypassing the Accessor/permit seam.** [components.go:180-186](internal/api/components.go:180):

```go
if adapter := newAdapterForType(req.Type); adapter != nil {
    if err := adapter.Connect(ctx, *source); err != nil {        // line 181 — direct Connect
        writeBadRequest(w, err, "connect "+req.Type+" source", ...)
        return
    }
    s.services.Adapters.Register(req.ID, adapter)                 // line 185 — registers live adapter
}
```

There is no call to `internal/access` here. The authoritative RBAC gate is `access.Accessor.Permit`/`permit`, keyed on `(principal, componentID, action)` ([access.go:120](internal/access/access.go:120), [access.go:181](internal/access/access.go:181)); `components.go` neither imports `internal/access` nor calls the accessor. So create reaches the adapter and the backend without passing through the seam that governs every operation-path tool call.

**The test path does the same:** `handleTestComponent` calls `adapter.Connect(ctx, *src)` directly ([webui.go:678](internal/api/webui.go:678)), also with no accessor call.

**ADR claim verdict:** The D-0026 ADR (DECISIONS.md line 45-46) states "component-management paths bypass the permit/guard seam (existing authz gap, flagged as issue)." **Confirmed correct** for the create path ([components.go:181](internal/api/components.go:181)) and the test path ([webui.go:678](internal/api/webui.go:678)). One correction to the prompt's framing: the cited paths are **create and test**, not "create/update" — there is no update path (Part A.1).

### B.2 What authz gating the registration endpoints DO carry

**The component routes are NOT admin-gated and NOT accessor/RBAC-gated. The only gate is edge authentication (any authenticated principal).**

- `registerComponentRoutes` wires the handlers with bare `mux.HandleFunc` and **no `requireAdmin`** ([server.go:155-161](internal/api/server.go:155)). Contrast the admin surface, where every handler calls `h.server.requireAdmin(w, r)` (e.g. [admin.go:195](internal/api/admin.go:195) and ~25 more sites), including the unit-3 credential-status endpoints ([admin.go:131-132](internal/api/admin.go:131)). `handleCreateComponent`/`handleDeleteComponent` contain **no `requireAdmin` call**.
- The global middleware chain is `CORS → RateLimit → metrics → EdgeAuth → SessionMiddleware → EnforcementMiddleware → MaxRequestBody → mux` ([server.go:703-723](cmd/joe/server.go:703)).
  - `EdgeAuth` requires *authentication* on protected paths (`/api/v1/components` is protected — not under the public prefix), returning `401` to unauthenticated callers ([middleware.go:145-187](internal/auth/middleware.go:145)). **But if neither service accounts nor OIDC is configured, EdgeAuth is fully open** — every caller becomes the fallback `disabledPrincipal` and nothing is rejected ([middleware.go:157-160](internal/auth/middleware.go:157)). So in a default/dev posture, component create is effectively **unauthenticated**.
  - `rbac.EnforcementMiddleware` is **a pass-through no-op** — its per-zone decision was demoted to the accessor in Phase E ([middleware.go:78-83](internal/rbac/middleware.go:78)). It does **not** gate `/components`.
- The RBAC enforcement that "fires only on paths with a componentID" (per project memory) lives in the **accessor**, which the create path never calls (B.1). So no zone/`IsAllowed` check governs who may register a component.

**Net: any authenticated principal — of any zone, with no write permission to any component — can `POST /api/v1/components`**, and where auth is unconfigured, any caller can. There is no admin gate and no componentID-keyed RBAC gate on the registration surface.

### B.3 Is component registration/update/delete audited?

**No.** `handleCreateComponent`, `handleDeleteComponent`, and `handleTestComponent` write **no audit row**. `grep` for `Audit`/`audit` across `components.go` and `webui.go` returns nothing in these handlers. There is no `component.create`, `component.delete`, or equivalent verb — `grep` for `ActionComponent*`/`component.create`/`component.delete` in `internal/audit/` returns nothing.

This is distinct from the unit-3 read verb `credential_status.read` (`audit.ActionAdminCredentialStatusRead = "credential_status.read"`, [audit.go:264-273](internal/audit/audit.go:264)), which the admin credential-status listing/probe endpoints emit (verified by [credential_status_test.go:78-80](internal/api/credential_status_test.go:78)). So a **read** of credential *status* through the admin surface is audited, while a **create/delete of an actual component** — which triggers `Connect`, credential `Resolve`, and (for k8s) a live cluster probe — writes **no audit row at all**. The only audit emission anywhere near this surface is the break-glass service-account login row from `EdgeAuth` ([middleware.go:196-223](internal/auth/middleware.go:196)), which records authentication, not the component mutation.

### B.4 Blast radius — what credential-adjacent action actually fires at registration

At `POST /api/v1/components`, gated only by edge-auth (B.2), the following fires synchronously inside `handleCreateComponent` → `adapter.Connect` ([components.go:181](internal/api/components.go:181)), for adapter-backed types:

1. **`ParseConfig`** of the operator-supplied blob (e.g. [k8s.go:59](internal/adapters/k8s/k8s.go:59)).
2. **Credential `Resolve`** via the provider selected by `credential_provider` ([k8s.go:69](internal/adapters/k8s/k8s.go:69) → [k8s.go:135](internal/adapters/k8s/k8s.go:135)):
   - **static provider:** reads the inline `value` or **reads the named `env_var` from the Joe process environment** ([static.go:58-68](internal/credential/static.go:58)). No backend contact at Resolve, but an operator-chosen env var of the *Joe process* is dereferenced — an actor who can register a component chooses which process env var is read into a credential.
   - **kubeconfig-exec provider:** `Resolve` only selects the kubeconfig/context; it does **not** itself exec or contact the backend ([kubeconfig_exec.go:66-90](internal/credential/kubeconfig_exec.go:66)).
3. **Live backend contact.** For k8s, `Connect` builds the client from the resolved selection and **eagerly probes the cluster**: `clientset.Discovery().ServerVersion()` ([k8s.go:94](internal/adapters/k8s/k8s.go:94)). This is the point at which **client-go runs the kubeconfig exec plugin to mint a token** (the "mint" is client-go's transport, observed not driven — [credential.go:37-40](internal/credential/credential.go:37)) and makes a real network call to the operator-specified API server. Other adapters perform their own connectivity check inside `Connect`.
4. On success the live adapter is **registered into the running process** (`s.services.Adapters.Register`, [components.go:185](internal/api/components.go:185)), so subsequent refresh loops and tools use it.

**Characterized finding:** An actor who can hit `POST /api/v1/components` (any authenticated principal; anyone at all when auth is unconfigured) can thereby cause, at registration time and with no accessor/permit check and no admin gate and no audit row:
- **credential resolution** keyed on a componentID they just invented (the no-ambient-fallback property D-0026 designs for the *operation* path does not protect the *registration* path, because registration never reaches the accessor);
- **dereferencing of an arbitrary Joe-process environment variable** into a static credential (operator-chosen `env_var`);
- for kubeconfig-exec, **execution of the local exec plugin and a live network call to an attacker-chosen API server URL** (SSRF-shaped: the destination is whatever the registrant put in the kubeconfig/context, and the eager `ServerVersion` probe guarantees the contact happens).

This is precisely the class of action the operation-path seam exists to gate (`access.Accessor.permit` on `(principal, componentID, action)`), and registration sidesteps it. **Adjacent leak (documented in D-0026, line 43-44, and worth flagging here):** `GET /api/v1/components` / `GET /{id}` return the full `store.Component` including its `Config` ([components.go:47-50](internal/api/components.go:47), [components.go:217](internal/api/components.go:217)) — per the ADR this is decrypted config — so the same un-admin-gated surface also reads back stored credential config to any authenticated principal. Not the core question, but it compounds the registration surface's exposure.

---

## BOTTOM LINE

**(i) Input surface — needs design, not just a build task.** Today `config` is a hand-authored opaque `json.RawMessage` ([components.go:116](internal/api/components.go:116)); there is no typed field for `credential_provider` or any per-provider key on any surface, no CLI command, and the Web UI has no create/edit form at all (it is read/test/delete only). The typed *fields* exist deep in the credential providers, but exposing them is more than mechanical: there is no create/update UI or CLI to hang them on, no update endpoint to extend (only delete-and-recreate exists), and no registration-time schema/discriminator validation to build against. Surfacing `credential_provider` as a first-class input is a design task (new form/flag surface + validation + an update path), not a "fields exist, wire them up" change.

**(ii) Security-surface gap — real, and genuine design, not just mechanical gating.** The gap is confirmed: create ([components.go:181](internal/api/components.go:181)) and test ([webui.go:678](internal/api/webui.go:678)) call `adapter.Connect` directly, bypassing the accessor; the routes carry no `requireAdmin` and no componentID-keyed RBAC (the only gate is edge-auth, which is fully open when auth is unconfigured); and no create/delete audit verb exists. Slapping `requireAdmin` on the routes would be mechanical and would close the "who can call it" hole, but the deeper question is genuinely a design session: at registration a component triggers credential `Resolve`, env-var dereference, and a live attacker-controllable backend probe/mint *before* it is an authz'd entity, so there is no `componentID` to key the accessor on yet — the seam D-0026 designed for the operation path does not naturally cover "creating the thing the permission is about." Deciding whether registration routes through the accessor, defers the eager `Connect`/probe, gates the env-var/SSRF blast radius, and emits an audit verb is a design problem, not a one-line gate. **Recommended follow-up: both a build track (input surface) and a design session (registration as a security surface), with the security design being the blocking one.**
