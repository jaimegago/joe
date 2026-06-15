# Accessor seam — does the permit decision read a component promotion/zone/read-only state axis?

**Date:** 2026-06-14
**Type:** read-only investigation (no code, tests, or docs changed)
**Scope:** Whether the guarded accessor seam (`internal/access`) evaluates any component-state axis (zone assignment / promotion status / read-only / lifecycle) as an input to its permit decision today, and whether such an axis — if present — is enforced at the permit point or merely present in the schema. Diagnosis only; no recommendations.

---

## VERDICT (the single load-bearing question)

**Yes — the permit decision reads ONE component-state axis today: the component's *zone assignment*, and it is ENFORCED at the permit point, not merely present in the schema.** The seam's decision call `PolicyEngine.Decide` resolves the component's zone from the `component_zone_assignments` table keyed by componentID, defaulting to the literal `"unassigned"` zone when no assignment row exists ([policy.go:111-118](internal/rbac/policy.go:111)). It then denies any action the resolved zone does not list in its `allowed_actions`, and that check is evaluated *before* — and is NOT bypassed by — the admin short-circuit ([policy.go:129-131](internal/rbac/policy.go:129)). The seeded `unassigned` zone allows only `["read"]` ([006_rbac.up.sql:34](internal/store/migrations/006_rbac.up.sql:34)), so a freshly-registered component with no zone assignment is, at the permit point, hard-confined to reads: every `mutate`/`delete` is denied with reason `action_not_in_zone`, regardless of the principal's grants and regardless of whether the backend credential would accept the mutation. **Two load-bearing caveats:** (1) there is NO promotion/read-only/lifecycle field on the component record itself — the only state axis read is the *separate* zone-assignment row (Q3); the "promotion state" the downstream design wants does not exist as a component field, only as presence/absence of a zone-assignment row. (2) The whole permit decision is a no-op when the RBAC engine is nil, which it is whenever neither service accounts nor OIDC is configured — in that default/dev posture every decision is permitted with reason `rbac_disabled` and the read-only floor does not run at all ([access.go:128-131](internal/access/access.go:128), [server.go:76-84](internal/api/server.go:76)). The axis exists and is enforced **only when RBAC is enabled and only for dispatch that crosses the seam** (Q5).

---

## Q1 — The permit decision inputs

The seam's single enforcement chokepoint is `Accessor.permit` ([access.go:120-172](internal/access/access.go:120)), called via `permitForPrincipal` ([access.go:180-182](internal/access/access.go:180)) from the generic `guard[T]` ([access.go:194-218](internal/access/access.go:194)). `permit` reads exactly these inputs to reach allow/deny:

- **principals** (`rbac.PrincipalSet`) — the authorization subject; size-1 today, derived from the caller's context principal ([access.go:120](internal/access/access.go:120), [access.go:180-181](internal/access/access.go:180)).
- **sourceID / componentID** (string) — passed straight into the engine ([access.go:133](internal/access/access.go:133)).
- **action** (`rbac.Action`) — declared at the call site (Q2).

`permit` itself makes no decision; it delegates to `a.engine.Decide(ctx, principals, sourceID, action)` ([access.go:133](internal/access/access.go:133)) and consumes `d.Allowed` / `d.Zone` / `d.Reason`. The decision logic lives in `PolicyEngine.Decide` ([policy.go:109-168](internal/rbac/policy.go:109)), which reads:

1. **The component's zone assignment** — `e.repo.GetAssignment(ctx, componentID)`; on nil/err it defaults `zoneID = "unassigned"` ([policy.go:111-118](internal/rbac/policy.go:111)).
2. **The resolved zone's `allowed_actions`** — `e.repo.GetZone(ctx, zoneID)` then `zone.Allows(action)` ([policy.go:121-131](internal/rbac/policy.go:121)).
3. **Admin status of each principal** — `e.repo.IsAdmin` ([policy.go:136-146](internal/rbac/policy.go:136)).
4. **Per-principal zone grants** — `e.repo.ListPoliciesForPrincipal` matched against `zoneID` ([policy.go:153-165](internal/rbac/policy.go:153)).

Quoted decision core ([policy.go:111-131](internal/rbac/policy.go:111)):

```go
zoneID := "unassigned"
assignment, err := e.repo.GetAssignment(ctx, componentID)
if err != nil { ... } else if assignment != nil { zoneID = assignment.ZoneID }

zone, err := e.repo.GetZone(ctx, zoneID)
if err != nil || zone == nil {
    return Decision{Allowed: false, Zone: zoneID, Reason: ReasonZoneNotFound}
}
if !zone.Allows(action) {
    return Decision{Allowed: false, Zone: zoneID, Reason: ReasonActionNotInZone}
}
```

**Explicit answer on component state:** the decision reads exactly ONE kind of component state — the component's **zone assignment** (a row in `component_zone_assignments`, keyed by componentID). It reads **no** promotion-status field, **no** read-only flag, **no** lifecycle/registration-state field, because none exists on the component record (Q3). The "unassigned" status is not a stored component attribute either — it is the *default zoneID literal* the engine substitutes when `GetAssignment` returns no row ([policy.go:111](internal/rbac/policy.go:111),[policy.go:116-118](internal/rbac/policy.go:116)). So the only component-state axis at the permit point is "which zone is this component assigned to (or unassigned)," and that axis is consumed via the zone's `allowed_actions`.

---

## Q2 — Action classification (read vs mutation)

**There is a read/mutate/delete classification, and it is declared per-dispatch-method at the call site — not inferred and not stored on the component.** The action vocabulary is the `rbac.Action` enum ([zones.go:7-33](internal/rbac/zones.go:7)):

```go
ActionRead   Action = "read"    // read-only observations (T1)
ActionQuery  Action = "query"   // graph/search/aggregation (T1)
ActionMutate Action = "mutate"  // writes/mutations (T2/T3)
ActionDelete Action = "delete"  // destructive (T3)
```

Each exported `*Accessor` dispatch method hard-codes its action as a literal argument to `guard[T]`, adjacent to the adapter call it gates. Examples:

- Reads: `K8sListResources` / `K8sGetResource` / `K8sGetPodLogs` all pass `rbac.ActionRead` ([k8s.go:13](internal/access/k8s.go:13),[k8s.go:22](internal/access/k8s.go:22),[k8s.go:31](internal/access/k8s.go:31)).
- Mutations: the GitHub/GitLab write methods pass `rbac.ActionMutate` ([vcs.go:74](internal/access/vcs.go:74),[vcs.go:82](internal/access/vcs.go:82),[vcs.go:108](internal/access/vcs.go:108)).

**Where the classification is *read* for the decision:** in `Zone.Allows`, which linear-scans the zone's `AllowedActions` for the requested action ([zones.go:46-53](internal/rbac/zones.go:46)), invoked at [policy.go:129](internal/rbac/policy.go:129).

A structural test enforces that every principal-gated `*Accessor` method declares an action: `TestEveryDispatchMethodDeclaresAnAction` AST-checks that each exported method taking an `rbac.Principal` references some `rbac.Action*` constant in its body ([guard_test.go:27-71](internal/access/guard_test.go:27)). So the read/mutate axis is a property of the *method*, statically guaranteed present.

**Scope note (the mutation universe is small today):** the only `ActionMutate` declarations anywhere in `internal/access` are the three VCS write methods ([vcs.go:74](internal/access/vcs.go:74),[vcs.go:82](internal/access/vcs.go:82),[vcs.go:108](internal/access/vcs.go:108)); there are **no** `ActionDelete` declarations in the package. Every other accessor dispatch method is `ActionRead` or `ActionQuery`. (Grep: `ActionMutate`/`ActionDelete`/`ActionQuery` across `internal/access/*.go` non-test hits only `vcs.go` plus doc comments in `access.go`.)

---

## Q3 — Component lifecycle / zone state in schema and model

**The component record carries NO zone-assignment, promotion, read-only, or lifecycle-authorization field.** `store.Component` is ([models.go:9-19](internal/store/models.go:9)):

```go
type Component struct {
    ID         string
    Type       string
    Name       string
    Config     json.RawMessage
    Status     string          // connectivity/sync status, NOT an authz axis
    LastSyncAt *time.Time
    LastError  string
    CreatedAt  time.Time
    UpdatedAt  time.Time
}
```

`Status`/`LastSyncAt`/`LastError` are connectivity/sync telemetry (written by `UpdateSyncStatus` on the test/refresh paths), not an authorization state. There is no `zone_id`, no `promoted`, no `read_only`, no `lifecycle`/`registration_state` column.

**Zone assignment is a separate table, not a component field.** It lives in `component_zone_assignments` (originally `source_zone_assignments`, [006_rbac.up.sql:12-19](internal/store/migrations/006_rbac.up.sql:12); renamed to `component_zone_assignments` by migration [023_source_to_component.up.sql](internal/store/migrations/023_source_to_component.up.sql), and that is the table the live query reads — [repository.go:395-400](internal/rbac/repository.go:395)). Its Go model is `ComponentZoneAssignment{ComponentID, ZoneID, AssignedBy, Reason, AssignedAt}` ([zones.go:56-62](internal/rbac/zones.go:56)).

**Default at creation: a newly-created component is fully usable at the schema level and confined ONLY by the unassigned-zone default at the permit point.** The create handler `handleCreateComponent` writes the component row and registers the adapter but **creates no zone-assignment row** (grep for `zone` in [internal/api/components.go](internal/api/components.go) and [internal/store/components.go](internal/store/components.go) returns nothing). There is no DB trigger or default that inserts an assignment. Consequently a fresh component has *no* `component_zone_assignments` row, and `GetAssignment` returns nil → the engine substitutes `"unassigned"` ([policy.go:111-118](internal/rbac/policy.go:111)). So:

- At the **record** level: the component defaults to no zone, no read-only marker — nothing.
- At the **permit** level: that absence resolves to the `unassigned` zone, whose seed allows only `["read"]` ([006_rbac.up.sql:34](internal/store/migrations/006_rbac.up.sql:34)) — i.e. effectively read-only, *but only when RBAC is enabled* (Q4 caveat).

There is no stored "promotion state." The closest analog is binary and derived: *has an assignment row to a non-unassigned zone, or not.*

---

## Q4 — Zone enforcement reality ("unassigned = read-only")

**The "unassigned zone = read-only for new sources" invariant IS backed by an enforcement path in the current code — conditional on RBAC being enabled.** The enforcement is:

1. Default-to-unassigned on missing assignment — [policy.go:111](internal/rbac/policy.go:111),[policy.go:116-118](internal/rbac/policy.go:116).
2. The seeded `unassigned` zone's `allowed_actions = '["read"]'` — [006_rbac.up.sql:34](internal/store/migrations/006_rbac.up.sql:34) (`('unassigned', 'Unassigned', 'Default zone for new sources', '["read"]', ...)`). The column default is also `'["read"]'` ([006_rbac.up.sql:8](internal/store/migrations/006_rbac.up.sql:8)).
3. The deny when the action is not in the zone's list — **the file:line of enforcement is [policy.go:129-131](internal/rbac/policy.go:129)**:

```go
if !zone.Allows(action) {
    return Decision{Allowed: false, Zone: zoneID, Reason: ReasonActionNotInZone}
}
```

This check sits **above** the admin short-circuit ([policy.go:136-146](internal/rbac/policy.go:136)) and the per-principal grant loop ([policy.go:153-165](internal/rbac/policy.go:153)), so neither a grant nor admin capability can widen an `unassigned` component beyond `read`. The design intent is documented in the struct comments ("A zone classified readonly stays readonly even for an admin", [zones.go:84-87](internal/rbac/zones.go:84); D-0011 rationale at [policy.go:94-99](internal/rbac/policy.go:94)) and is matched by the code path above. So for `unassigned`, `mutate`/`delete` deny with `action_not_in_zone` even for an admin.

**The one place the invariant is unbacked:** when the RBAC engine is nil. `permit` returns allow with reason `rbac_disabled` and never calls `Decide` ([access.go:128-131](internal/access/access.go:128)); the engine is nil whenever `services.RBAC == nil` OR neither service accounts nor OIDC is configured ([server.go:76-84](internal/api/server.go:76), mirrored at [server.go:643](cmd/joe/server.go:643)). In that posture the read-only floor on unassigned components does not execute. So the invariant is **enforced in code when RBAC is enabled, and entirely absent when it is not** — it is not merely a design-doc artifact, but it is also not unconditional.

---

## Q5 — The dispatch floor (every path to an adapter, and whether it crosses the seam)

A structural test makes the seam the *only* resolve-for-use path: `TestInvariant_NoUngovernedAdapterOrGraphAccess` walks all non-test `.go` files and fails the build if any package outside the allowlist calls `services.Adapters.Get(...)` or a graph-store method ([access_guard_test.go:64-158](internal/api/access_guard_test.go:64)); the allowlist is exactly `internal/access/`, `internal/coreagent/`, `cmd/joe/` ([access_guard_test.go:67-71](internal/api/access_guard_test.go:67)). **Important limitation: this guard only catches `Adapters.Get` (resolve an *already-registered* adapter) and graph methods. It does NOT catch construct-then-`Connect` on a fresh adapter** — which is exactly how the create/test paths reach the backend.

Enumerated dispatch paths to an adapter / backend:

| # | Path | File:line | Crosses permit seam? |
|---|------|-----------|----------------------|
| 1 | **Operation tool dispatch** (every read/mutate tool: K8s, Git/VCS, AWS, Prometheus, Loki, Tempo, Jaeger, alerts, graph, …) → in-process accessor client → `c.accessor.<Method>(... PrincipalFromContext(ctx) ...)` → `guard[T]` → `permit` → `registry.Get` | [inproc_client.go:115-356+](internal/api/inproc_client.go:115); guard at [access.go:203-206](internal/access/access.go:203) | **YES** — `permit` runs before `registry.Get` |
| 2 | **Component create** → `newAdapterForType` → `adapter.Connect(ctx, *source)` directly, then `Adapters.Register` | [components.go:180-185](internal/api/components.go:180) (`newAdapterForType` def [components.go:58](internal/api/components.go:58)) | **NO** — direct `Connect`, no accessor import/call (confirmed by prior investigation, component-credential-registration-surface.md §B.1) |
| 3 | **Component test** ("Test Connection") → `newAdapterForType` → `adapter.Connect(ctx, *src)` directly, then `Register` | [webui.go:668](internal/api/webui.go:668),[webui.go:678](internal/api/webui.go:678),[webui.go:693](internal/api/webui.go:693) | **NO** — direct `Connect`, no accessor call |
| 4 | **Server bootstrap reconnect** (re-establish adapters for persisted components at startup) → `adapter.Connect(ctx, *src)` ×N | [server.go:844-986](cmd/joe/server.go:844) | **NO** — direct `Connect`; in the `cmd/joe` composition-root allowlist |
| 5 | **Core Agent background refresh** → `r.services.Adapters.Get(source.ID)` → adapter read methods | [refresh.go:172](internal/coreagent/refresh.go:172) | **NO** — allowlisted exception; documented & audited READ-ONLY (every `*_refresh.go` calls List/Get/Describe/Status only — [access_guard_test.go:44-55](internal/api/access_guard_test.go:44)) |
| 6 | **cmd/joe telemetry gauge** → `graph.Summary` | allowlisted, [access_guard_test.go:56-61](internal/api/access_guard_test.go:56) | **NO** — read-only graph summary, no principal |

**What this means for a read-only floor:** every *tier-classified operation* — including the only mutations that exist today (VCS writes, Q2) — flows through path #1 and crosses the seam. Paths #2/#3/#4 bypass the seam, but they invoke `adapter.Connect`, which is a connectivity/credential-validation step (e.g. the k8s `Connect` parses config, resolves the credential, builds a REST config, and does a `ServerVersion` liveness probe — [k8s.go:55-75](internal/adapters/k8s/k8s.go:55)), **not** a tier-classified `mutate`/`delete` operation. So today no *mutating operation* (in the `rbac.Action` sense) reaches an adapter without crossing the seam; however, `Connect` itself reaches the backend with the live credential on an ungated path, and `Connect`'s behavior is adapter-defined and not action-classified. Path #5 is the one ongoing principal-less adapter path, and it is constrained to reads by the audited VERDICT-A invariant, not by the permit decision.

---

## Q6 — Credential scope vs Joe-enforced scope

**The permit decision is purely Joe-side and runs strictly before any backend contact on the operation path; nothing in the permit decision defers authorization to the backend.** In `guard[T]`, `permit` is called first and, on denial, returns `ErrPermissionDenied` and never reaches `a.registry.Get` — so no adapter is resolved and no backend call is made ([access.go:203-206](internal/access/access.go:203)):

```go
if err := a.permitForPrincipal(ctx, principal, sourceID, action); err != nil {
    return zero, err           // denial: returns here
}
adapter, err := a.registry.Get(sourceID)   // only reached on allow
```

The package contract states this explicitly: "Every dispatch method evaluates rbac.IsAllowed BEFORE resolving and calling the underlying adapter" and "permit performs NO infrastructure access" ([access.go:6-9](internal/access/access.go:6),[access.go:117-119](internal/access/access.go:117)). `ErrPermissionDenied` "wraps no infrastructure error because, by contract, no infrastructure call is attempted on denial" ([access.go:45-48](internal/access/access.go:45)).

Therefore, on dispatch that crosses the seam, Joe can and does refuse a `mutate` against an `unassigned` (un-promoted) component **before** the backend is contacted, irrespective of what the underlying credential would permit — the deny at [policy.go:129-131](internal/rbac/policy.go:129) is reached without any backend round-trip. The architecture can enforce "Joe refuses even though the backend credential would accept it," **provided the mutation crosses the seam (Q5 path #1) and RBAC is enabled (Q4 caveat)**. The decision relies on no backend rejection. The only places authorization is effectively left to the backend are the seam-bypassing `Connect` paths (#2/#3/#4), where the credential is exercised against the backend with no Joe-side permit check — but those are connectivity validations, not tier-classified mutations.

---

## Appendix — dispatch-path summary (plain enumeration)

- Operation tool calls (reads + VCS writes) — **cross the seam** (`inproc_client` → `accessor.<Method>` → `guard` → `permit` → `registry.Get`).
- Component create (`POST /api/v1/components`) — **bypasses the seam** (`newAdapterForType` → `adapter.Connect`).
- Component test (`POST /api/v1/components/{id}/test`) — **bypasses the seam** (`adapter.Connect`).
- Server bootstrap reconnect (startup) — **bypasses the seam** (`adapter.Connect`; cmd/joe allowlist).
- Core Agent background refresh — **bypasses the seam** (`Adapters.Get`; allowlisted, audited read-only).
- cmd/joe telemetry gauge — **bypasses the seam** (`graph.Summary`; allowlisted, read-only).
