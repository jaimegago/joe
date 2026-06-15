# Investigation: auto_promote_reads flag + grant-wiring dependencies (A001-COREGOV · CC-03)

```text
================================================================================
A001-COREGOV CC-03 — READ-ONLY investigation. Re-verifies, against the live tree,
ONLY what the next unit (CC-04: per-component-type auto_promote_reads boolean,
default OFF, admin-gated/audited/hot-reloadable, admits a component into the
agent:core READ floor) newly depends on. Settles ONE fork: DYNAMIC PERMIT
PREDICATE vs MATERIALIZED GRANT ROWS. Reports engine capability; does NOT pick.

Every claim carries file:line evidence against the working tree at investigation
time. No code edits. No design decision.
================================================================================

--------------------------------------------------------------------------------
ANCHOR RE-CHECK (anything contradicting the settled anchors, loudly, up top)
--------------------------------------------------------------------------------
NO CONTRADICTIONS FOUND. All four settled anchors re-verified against the live
tree:
- Grant model principal-string-keyed / kind-agnostic — CONFIRMED:
  rbac_policies lookup is a plain `WHERE principal = ?` (repository.go:518-521),
  decision compares p.ZoneID == zoneID for any principal (policy.go:160-164).
- Admin REST accepts svc: grant targets — CONFIRMED: createPolicy validates only
  that the principal carries a reserved prefix (user:/group:/svc:) via
  rbac.HasReservedPrefix (admin.go:324-329); svc: passes.
- Policy values read live per request, no boot cache — CONFIRMED: Decide reads
  GetAssignment + GetZone + ListPoliciesForPrincipal from the repo on every call
  (policy.go:109-167); the engine struct holds only `repo` (policy.go:10-17).
- agent:core identity exists — CONFIRMED: rbac.CoreAgentServiceName /
  AgentCorePrincipal() (identity.go:87, :96-98). Reference, do not re-type.

--------------------------------------------------------------------------------
Q1 — AUDITED CONFIG-SETTINGS SURFACE TO MIRROR ("Stream G settings" precedent)
--------------------------------------------------------------------------------
The precedent is the llmsettings stack (Stream G phase G4/G5). It is the exact
shape CC-04's "auto_promote_reads per type" setting should mirror.

HTTP handler(s) + admin gate:
  - internal/api/llmsettings.go registers GET/POST under /api/v1/llm/settings
    (registerLLMSettingsRoutes, llmsettings.go:55-62). GET is open to any
    authenticated caller (policy knobs, not credentials, llmsettings.go:13-17).
  - EVERY mutator is admin-gated by the SAME gate as RBAC grants:
    `if _, gated := h.server.requireAdmin(w, r); gated { return }` —
    handleSetActiveModel (llmsettings.go:226), handleSetCostLimit (:286),
    handleSetRunawayCeiling (:314), handleSetContextBudget (:347).
    server.requireAdmin is the identical gate admin.go's grant writer uses
    (admin.go:307,342 etc.; admingate.go:19-51).

Persistence (table/store/migration) + how a per-type boolean would be stored:
  - Today's tables are singleton/keyed-row UPDATE-only, pre-seeded at migration
    time, NO INSERT path needed (llmsettings/repository.go:7-12, :88-91):
      * llm_settings        — singleton row id=1 (active_model)
      * llm_cost_limits     — three rows keyed by window_name
      * llm_runaway_limits  — singleton row id=1
      * llm_context_budget  — singleton row id=1 (migration 019)
  - The KEYED-ROW precedent (llm_cost_limits, one row per window_name, UPDATE by
    name — repository.go:300-314) is the closest fit for "one boolean per
    component type": a settings table keyed by the type string, one row per type,
    UPDATE-by-key. NOTE: llm_cost_limits rows are pre-SEEDED by migration; a
    per-type table over the 36-value enum (store/constants.go:3-142) would need
    either a 36-row migration seed OR an upsert path (the current Stream G
    repository has no INSERT — see "ordering note"). A single JSON/map column on
    a singleton row is an alternative the tree does NOT yet have a precedent for;
    the keyed-row shape is the one with a live mirror.

Fail-closed transactional audit wrapper used on writes:
  - llmsettings.MutationService is the SOLE write path
    (service.go:62-90). runMutation opens ONE tx on repo.DB(), reads the prior
    value IN-tx, writes the new value, writes ONE audit row via
    audit.Repository.InsertTx against the SAME tx, commits — or rolls back so
    NEITHER row persists (service.go:189-248; atomicity contract :62-68). A
    marshal or audit-insert failure aborts the whole mutation (:218-241).
    Acting principal stamped from rbac.PrincipalFromContext(ctx) (:228).
    Audit vocabulary target/before/after is the established contract
    (service.go:39-60).
  - (Sibling precedent: the RBAC repo's own `mutate` wrapper, repository.go:
    146-175, is the same pattern for grant rows — one tx, one audit Event,
    both-or-neither. Relevant to Q4.)

EXACT SEAM CC-04 SHOULD MIRROR:
  storage  → a new settings table keyed by component type (mirror llm_cost_limits
             keyed-row shape), with read + UpdateXxxTx methods on a Repository
             (mirror llmsettings/repository.go:88-149).
  write    → a MutationService.SetAutoPromoteReads(ctx, type, bool) that wraps
             read-before-write + audit row in one tx (mirror service.go:132-147
             / runMutation:189-248).
  HTTP     → GET (open) + POST behind h.server.requireAdmin (mirror
             llmsettings.go:55-62, :285-307).

--------------------------------------------------------------------------------
Q2 — POLICY-ENGINE EXTENSIBILITY (THE FORK)  ***most important output***
--------------------------------------------------------------------------------
Composition point:
  - The accessor policy engine is rbac.PolicyEngine, constructed at exactly ONE
    site: cmd/joe/server.go:713-716 — `policyEngine = rbac.NewPolicyEngine(rbacRepo)`,
    guarded by the shared enable predicate `if cfg.RBACEnabled()` (config.go:
    191-204). A nil engine means RBAC disabled (services.RBACEnabled =
    policyEngine != nil, server.go:723). The engine struct holds only a
    Repository (policy.go:10-17) — NO predicate list, NO admit-chain, NO
    plug-in seam.
  - "RBACEnabled enable-predicate pattern" (commits 91d472a / 3fd6d3a) is an
    ENABLE/DISABLE composition (build-the-engine-or-not + refuse-to-start +
    services flag share ONE predicate, server.go:706-723, config.go:191-204). It
    is NOT an admit-predicate registry inside the engine. It governs whether the
    engine exists, not how a single decision is reached.

Decision interface + order of evaluation (policy.go:109-167, Decide):
  1. Resolve component zone via repo.GetAssignment (default "unassigned").
  2. Load zone via repo.GetZone; missing zone → DENY (ReasonZoneNotFound).
  3. zone.Allows(action)? if not → DENY (ReasonActionNotInZone). Admin does NOT
     widen this (D-0011).
  4. Admin short-circuit: any principal with repo.IsAdmin → ALLOW
     (ReasonAdminCapability).
  5. Otherwise ALLOW iff any principal holds a matching rbac_policies grant for
     the zone (ReasonPolicyAllow); else DENY (ReasonNoGrant).
  The Accessor consults this single Decide call (access.go:120-137); a nil engine
  short-circuits to allow-all (access.go:128-132). There is NO second admit path.

Can a NEW dynamic ADMIT predicate be added (agent:core + ActionRead + target's
type has auto_promote_reads ON, evaluated live at check time)?
  - Decide reads everything live from the repo on each call (no cache), so a new
    branch placed BETWEEN step 4 (admin) and step 5 (grant) would be evaluated
    live per request — the data-freshness requirement is already met
    (policy.go:109-167; anchor: values read live per request).
  - Such a branch needs only: (a) the principal set (already a Decide param),
    (b) the componentID (already a Decide param), (c) a component-type lookup
    (single cheap query, see Q3), (d) a read of the auto_promote_reads setting
    for that type (the new Q1 table). All four are reachable from inside Decide
    via the repo handle — no new engine field strictly required if the type
    lookup + setting read are added to the rbac.Repository surface (or a small
    collaborator is injected at the single construction site server.go:715).
  - Is "grant rows" the ONLY admit path? NO. The engine ALREADY has a
    grant-less admit path: the admin short-circuit (policy.go:136-146) permits
    WITHOUT any rbac_policies row, purely from a live repo.IsAdmin check. That is
    a working precedent for a predicate admitting without a materialized grant —
    an auto_promote_reads branch is structurally the same kind of grant-less
    admit, just keyed on (principal==agent:core ∧ action==read ∧ type-ON) instead
    of (IsAdmin). CAVEAT: the admin path still passes through the zone gate at
    step 3 first; a dynamic agent:core-read branch placed after step 3 would
    inherit the same zone.Allows(read) precondition — acceptable for a READ
    admit, but note it is bounded by the resolved zone's allowed_actions, NOT a
    pure type-keyed bypass.

  VERDICT: dynamic predicate viable: YES via a new branch inside
  rbac.PolicyEngine.Decide (internal/rbac/policy.go:109-167), placed alongside
  the existing grant-less admin short-circuit (policy.go:136-146), reading the
  per-type setting + component type live through the engine's repo handle.
  The engine already admits without a materialized grant (the admin
  short-circuit is the proof), so a materialized grant row is NOT required for an
  admit path to exist.

--------------------------------------------------------------------------------
Q3 — COMPONENT -> TYPE LOOKUP AND ENUMERATION
--------------------------------------------------------------------------------
Type from componentID at permit-check time:
  - store.ComponentRepository.Get(ctx, id) returns *store.Component, whose .Type
    field is the component-type string (components.go:19, :58-90; the SELECT
    pulls `type` at :63 and scans into s.Type at :71). Single indexed-PK lookup
    by id — one cheap query.
  - The permit-check context ALREADY HAS the componentID: it is the `sourceID`
    parameter threaded through Accessor.permit / guard (access.go:120, :180-182,
    :194-206) and into PolicyEngine.Decide as `componentID` (policy.go:109).
    Decide already issues two repo queries per call (GetAssignment, GetZone,
    policy.go:112,121); adding one ComponentRepository.Get is a third single-row
    query of the same cost class — cheap, not a scan.
  CAVEAT: rbac.Repository (the engine's current handle) does NOT today expose a
  component-type lookup — the component store is store.ComponentRepository, a
  different interface. CC-04 must either (i) add a GetComponentType-style method
  to rbac.Repository, or (ii) inject the component store as a small collaborator
  at the single engine-construction site (server.go:715). Either is a narrow
  one-method addition; no schema change for the lookup itself.

Enumeration by type (needed ONLY if materialized):
  - store.ComponentRepository.ListByType(ctx, type) returns []*Component
    (components.go:21, :109-124) — one indexed query per type. This is the call a
    flip-ON materialization would iterate to insert per-component grant rows, and
    a flip-OFF would iterate to revoke. Not needed at all for the dynamic-
    predicate approach.

--------------------------------------------------------------------------------
Q4 — GRANT CREATE/REVOKE PATH (only if materialized is on the table)
--------------------------------------------------------------------------------
Programmatic create/revoke for a grant (principal=svc:agent:core, action=read):
  - CREATE: SQLRepository.CreatePolicy(ctx, rbac.Policy{Principal, ZoneID},
    actor) — repository.go:540-559. It runs under the RBAC repo's own audited
    `mutate` wrapper (repository.go:544, mutate at :146-175): the INSERT and the
    audit.ActionAdminPolicyGrant Event commit in ONE tx or neither
    (repository.go:544-554).
  - REVOKE: SQLRepository.DeletePolicy(ctx, id, actor) — repository.go:561+,
    also under `mutate`, capturing the revoked grant as audit Before.

  IMPORTANT MISMATCH for "same audited transaction as Q1's settings write":
  Q1's settings write commits under llmsettings.MutationService.runMutation's tx
  (service.go:189-248); CreatePolicy commits under the RBAC repo's SEPARATE
  `mutate` tx (repository.go:146-175). These are TWO different transactions on
  (potentially) the same DB handle. A flip-ON that writes BOTH the setting row
  AND N grant rows atomically would need a SINGLE shared tx spanning both — which
  NEITHER wrapper exposes today (each owns/commits its own tx). So materialized
  grants are NOT atomically co-committable with the setting flip under the
  current wrappers without new plumbing. The dynamic-predicate approach has no
  such cross-tx problem (it writes only the setting row).

  Representability without new schema — CONFIRMED:
  - principal: rbac_policies.principal is a free TEXT column matched by string
    (repository.go:518-521, :546-549); svc:agent:core is a valid value (admin
    REST already accepts svc: targets, admin.go:324-329). agent:core principal
    via rbac.AgentCorePrincipal() (identity.go:96-98).
  - BUT a grant row is (principal, zone_id) — it is ZONE-keyed, NOT
    component-keyed and NOT action-keyed. rbac_policies has NO component_id and
    NO action column (repository.go:496-499, :546-549; the row is principal +
    zone_id + created_at). The action a grant permits is the zone's
    allowed_actions (Decide step 3, policy.go:129), and the target is the zone,
    not a single component. So "a grant row for (svc:agent:core, componentID,
    read)" is NOT representable as-is: grants attach a principal to a ZONE, not to
    a component, and carry no per-action field. Materialization would therefore
    grant agent:core the WHOLE zone of each ON-type component (read bounded only
    by that zone's allowed_actions), not a component-scoped read — a meaningful
    over-grant vs. the dynamic predicate's component+action-precise admit. New
    schema (a component-scoped or action-scoped grant) WOULD be needed to make
    materialized grants as precise as the design intends.

--------------------------------------------------------------------------------
COMPARISON (input to the human's decision — NOT a decision)
--------------------------------------------------------------------------------
For THIS design (default-OFF, hot-reloadable, agent:core READ-only, components
may be added AFTER a flag is ON), the tree's existing seams favor the DYNAMIC
PREDICATE, on these tree-grounded facts:
  - Late-added components: a dynamic predicate covers a component the instant it
    exists, because type is resolved live at check time (Q3) — no backfill.
    Materialized grants would need a hook on component-create to insert a grant
    for every already-ON type (no such hook investigated/exists on the create
    path, components.go:33-56), or the new component silently escapes the floor
    until a re-flip. This mirrors the exact gap Phase H's admin short-circuit was
    built to close (policy.go:100-106 "zone-created-after-bootstrap gap").
  - Grant-less admit already exists: the admin short-circuit (policy.go:136-146)
    proves the engine admits without a materialized row, so a dynamic branch is
    not a new architectural concept — it is a second instance of an existing one.
  - Atomicity: dynamic writes ONE setting row under ONE audited tx (Q1). 
    Materialized must co-commit a setting row + N grant rows; the two wrappers
    own separate txs (Q4) — no shared-tx seam today.
  - Precision: grant rows are zone-keyed and action-less (Q4); they cannot
    express component-scoped read-only without new schema. The dynamic predicate
    expresses (principal==agent:core ∧ action==read ∧ type-ON) exactly.
  - Live read of policy is already the engine's contract (anchor; policy.go:
    109-167) — a dynamic predicate fits the no-cache model with no new caching
    concern; flip-ON/flip-OFF take effect on the next decision automatically,
    which is precisely "hot-reloadable".
  Materialized's only structural edge: a grant row is INSPECTABLE via the
  existing GET /admin/policies surface (admin.go:111, :288-301), whereas a
  dynamic admit is invisible there unless a new read endpoint surfaces the
  per-type settings. (The Q1 settings GET would be that surface.)

--------------------------------------------------------------------------------
ORDERING / PREREQUISITE NOTE
--------------------------------------------------------------------------------
- DOES CC-04 NEED A DB MIGRATION? YES, for the SETTINGS STORAGE regardless of
  fork: there is no existing table for a per-component-type boolean. Latest
  applied migration is 023_source_to_component (internal/store/migrations/);
  CC-04's settings table would be migration 024. Mirror the llm_cost_limits
  keyed-row shape (Q1). NOTE the Stream G repositories have NO INSERT path
  (UPDATE-only on pre-seeded rows, llmsettings/repository.go:88-91); over a
  36-value enum CC-04 must either seed 36 rows in the migration OR add an upsert
  — a deliberate divergence from the pure-UPDATE Stream G precedent.
- The DYNAMIC PREDICATE fork needs NO ADDITIONAL schema beyond that settings
  table: it reuses ComponentRepository.Get for type (Q3) and adds a branch in
  Decide (Q2). It needs ONE small interface addition (component-type lookup
  reachable from the engine, Q3 caveat) wired at the single engine-construction
  site (server.go:715).
- The MATERIALIZED fork needs the settings migration PLUS new schema to be
  component-and-action precise (Q4): rbac_policies as-is is zone-keyed/action-
  less and would over-grant; making it precise is a second migration.
- NOTHING here BLOCKS CC-04. The agent:core identity exists (anchor; identity.go:
  87,96-98), the engine reads live (anchor), and the admin-gated/audited/
  hot-reloadable settings precedent is fully present to copy (Q1). The one
  hard prerequisite is the migration-024 settings table; the one design-shaping
  prerequisite is the fork itself, which Q2's verdict shows is unblocked toward
  the dynamic-predicate option.

--------------------------------------------------------------------------------
EVIDENCE INDEX (primary anchors)
--------------------------------------------------------------------------------
- Settings precedent HTTP + admin gate: internal/api/llmsettings.go:55-62,
  :226,:286,:314,:347; gate internal/api/admingate.go:19-51.
- Settings storage (keyed-row + singleton, UPDATE-only): internal/llmsettings/
  repository.go:7-12,:88-149,:300-314; migration set internal/store/migrations/
  (latest 023).
- Fail-closed transactional audit write: internal/llmsettings/service.go:62-90,
  :132-147,:189-248; sibling RBAC `mutate`: internal/rbac/repository.go:146-175.
- Engine composition / enable predicate: cmd/joe/server.go:706-723; config.go:
  191-204; engine struct internal/rbac/policy.go:10-17.
- Decision order + grant-less admin admit: internal/rbac/policy.go:109-167
  (admin short-circuit :136-146; grant match :153-164).
- Accessor single consult of Decide: internal/access/access.go:120-137,:180-182,
  :194-206.
- Component type lookup / enumeration: internal/store/components.go:19,:21,
  :58-90,:109-124.
- Grant create/revoke + representability: internal/rbac/repository.go:496-499,
  :518-521,:540-559,:561+; svc: acceptance internal/api/admin.go:324-329;
  GET /admin/policies internal/api/admin.go:111,:288-301.
- agent:core identity: internal/rbac/identity.go:87,:96-98.
- Component-type enum (36 values): internal/store/constants.go:3-142.
================================================================================
```
