# Joe — Decisions

Append-only project decision log. Newest entries at the top. Each entry
records what was decided, the basis (verifiable source, not assertion), and
what it supersedes. This file is normative: where a decision here conflicts
with prose elsewhere, this file states the project's position and the
conflicting prose is stale.

Format per entry: ID, date, decision, basis, supersedes, status.

---

## D-0027 — Refuse to start without a usable identity configuration (engine-nil-at-runtime made unreachable)

- Date: 2026-06-15
- Status: IMPLEMENTED. In the tree as of this date (commits 91d472a, 3fd6d3a,
  44e9f5f). Implements the "RBAC inert/permissive when auth off must become
  UNREACHABLE" obstacle named in D-0019 decision 3 — the central obstacle that
  entry said the implementation "must close, not defer."
- Decision: Joe refuses to start unless the RBAC policy engine would be
  constructed non-nil. Missing or incomplete identity configuration is now a hard
  fail-fast at boot, in the SAME tier and exit semantics as missing LLM
  credentials and DB access — not a soft warning. "Joe is running" now
  structurally implies "Joe is governed."
- Threat closed: previously the policy engine was nil whenever no service account
  and no complete OIDC config existed, and a nil engine permitted every operation
  with reason `rbac_disabled` indefinitely, off any network bind, behind only a
  single soft boot warning. This was reachable not just by a fresh install but by
  a HALF-configured one — a partial OIDC block (issuer set, client_id/redirect_url
  empty) yielded engine-nil despite identity values being present, so an operator
  mid-setup would see Joe running and assume it was governed while it was silently
  allow-all. Refuse-to-start deletes this entire state class.
- Load-bearing design property: the refuse-to-start predicate IS the engine's own
  enable predicate, factored into one shared function so the guard and the engine
  constructors cannot drift. A new nil-safe method `config.(*Config).RBACEnabled()`
  — true iff service accounts are configured OR OIDC is `Configured()` — is the
  single source of truth, called by BOTH engine-construction sites and the boot
  guard. It reads raw config only (via the existing `ServiceAccountsConfigured` /
  OIDC `Configured` sub-predicates) and adds NO IdP reachability probe.
- What the guard does / does not fire on (both encode the decision):
  - FIRES (refuse to start): no identity at all; partial OIDC (any of
    issuer/client_id/redirect_url missing). The partial-OIDC case is the one that
    proves the half-configured gap is closed.
  - DOES NOT FIRE (start, governed): service-account-only; complete OIDC;
    complete-but-unreachable OIDC (IdP down). Completeness of config is the test,
    NOT IdP liveness — this deliberately avoids converting an IdP outage into a Joe
    outage. No reachability probe was added to the boot path.
- Implementation points (the landed shape; future readers should re-derive these
  against the tree rather than trust any line numbers, which are intentionally
  omitted):
  - Shared predicate: `config.(*Config).RBACEnabled()` in
    `internal/config/config.go`.
  - Boot guard + rich remediation-message constant (`noIdentityConfigMessage`,
    mirroring `noProviderKeyMessage`): `cmd/joe/server.go`, positioned AFTER the
    service-account-resolver fatal-validation gate and BEFORE engine
    construction. The post-gate ordering is load-bearing: it is what makes
    raw-config SA presence equivalent to the resolved resolver at that site (a
    malformed account map exits at the resolver gate before reaching the
    predicate).
  - SITE 1 (`cmd/joe`) builds the engine via `cfg.RBACEnabled()`. SITE 2
    (`internal/api` `newPolicyEngine`) was swapped to the same predicate, keeping
    its Config-nil / RBAC-nil guards (for `api.New`'s looser contract); no second
    refuse-to-start is added there because `api.New` has exactly one production
    caller, downstream of the `cmd/joe` guard.
  - Exit semantics: `slog.Error` + `return 1` bubbling to `os.Exit(1)`. No
    `log.Fatal`.
- Scope / explicitly deferred (conscious non-goals):
  - No runtime identity-provisioning / setup-wizard / first-run flow — boot-time
    config satisfies the guard.
  - The promotion / read-only-confinement / autonomous-discovery model is the
    separate work this unblocks, NOT part of this unit.
  - The soft nil-engine warning was retired: its default arm became unreachable
    and was replaced with an unreachable-state assertion (an internal-invariant-
    breach `slog.Error`, not an operator-misconfiguration warning).
- Consequence to flag (newly unreachable, parked as follow-up): EdgeAuth's
  open-when-unconfigured branch (`internal/auth/middleware.go`) is now unreachable
  via the boot path for the same reason the nil engine is — post-guard,
  service-account-or-OIDC is always true. This is now a dead branch in the same
  equivalence class as this fix; a future follow-up should retire it, likely with
  the same unreachable-state-assertion pattern.
- Basis: the three commits above, re-verified against the live tree on landing —
  `internal/config/config.go` (`RBACEnabled`), `cmd/joe/server.go`
  (`requireIdentityConfigured`, `noIdentityConfigMessage`, the guard placement and
  the retired warning arm), `internal/api/server.go` (`newPolicyEngine`). Guard
  tests cover all five identity states, asserting the two non-negotiable cases:
  partial-OIDC refuses, complete-but-unreachable starts
  (`internal/config/rbacenabled_test.go`, `cmd/joe/identityguard_test.go`).
- Supersedes: nothing — implements the engine-nil obstacle from D-0019 decision 3.
  References D-0018 (the write floor it sits beside in the boot fail-fast tier).

---

## D-0026 — Credential provider abstraction (Resolve/Probe/Describe, two-half resolved-credential type, launch-vs-deferred split)

- Date: 2026-06-09
- Status: ACCEPTED (design). Launch scope buildable without an Accessor refactor; not yet implemented.
- Decision: Adopt a credential-provider abstraction in which "which credential" is
  a property of the target component, resolved and applied at the guarded accessor
  seam, keyed strictly on the authz'd componentID with no ambient fallback. Full
  record in `docs/decisions/D-0026-credential-provider-abstraction.md`. In brief:
  resolution returns one typed result with two structurally separated halves — a
  serializable diagnostic half (component identity, provider kind, audience, expiry,
  stage reached, non-sensitive reason) and a non-serializable credential half (a
  means/source, never a value). A four-stage enum (provider-selected → mint-attempted
  → mint-succeeded → connectivity-probed) is the diagnostic spine, with
  mint-succeeded-without-probe a legal lazy-connectivity terminal state. The provider
  exposes exactly three operations — Resolve / Probe / Describe — and deliberately
  excludes Refresh/Rotate and any store/seam dependency in its signature. Launch ships
  two providers (static/env-var and kubeconfig-exec) invoked inside adapter Connect
  with no Accessor signature change; the resolve-value-at-the-seam model (and its
  store.ComponentRepository-on-Accessor dependency), ambient-workload-identity,
  rotation orchestration, per-zone scoping, and mutation-credential separation are
  designed-for but deferred.
- Basis: three read-only investigations against the live tree —
  credential-handling-current-state.md, adapter-credential-refresh-tolerance.md,
  credential-design-assumptions-check.md — cited file:line throughout the ADR and
  re-verified against the tree on landing (one citation corrected: networking.go:20
  → networking.go:13).
- Supersedes: nothing. Builds on the security-architecture-direction record §9 (the
  one credential commitment already made) and decides the parts §9 deferred.
- Documented gaps (tracked separately as issues/backlog, dispositions preserved):
  kafka parses-but-never-applies SASL auth (security finding, arguably
  fix-before-launch); two live credential leaks — /api/v1/components returns decrypted
  Config + mongodb URI interpolated into a ping error (T3, arguably fix-before-launch);
  azure Connect skeleton (deferred, tied to ambient-WI); component-management paths
  bypass the permit/guard seam (existing authz gap, flagged as issue).

---

## D-0025 — Captain transfer swap (detach old + attach new) is a single atomic transaction (transfer-half of the no-auto-lapse captaincy model)

- Date: 2026-06-08
- Status: IMPLEMENTED. In the tree as of this date.
- Gap: `CaptainService.completeTransfer` performed the captaincy handoff as two
  independent, sequential repository writes — `MarkCaptainDetached(outgoing)`
  then `AttachCaptain(incoming)` — with no shared transaction. A failure (or
  crash) after the detach committed but before the attach committed left the
  active incident with the old captain row detached and no successor row: a
  permanently captain-less incident. The captaingate then reads that as the
  pending-captain / null-authority state and refuses mutations, and nothing
  in-process re-attaches — recovery would require a fresh `Attach`. The prior
  code self-documented this as a Phase 1 gap ("a failure between them leaves
  the session captain-less, which a subsequent Attach would heal"); that
  heal is not automatic. (This is gap #6 in
  `docs/investigations/incident-captain-flow.md`.)
- Decision: `completeTransfer` now performs the detach-old + attach-new swap
  atomically through a new repository method, `SwapCaptain`, which runs both
  writes inside one DB transaction — either both commit or neither does. There
  is no committed state in which the old captain is detached and the new one is
  not attached, so a mid-swap failure can never strand the incident
  captain-less. The detach is an inline `UPDATE ... SET detached_at = ?,
  transfer_state = NULL, incoming_principal = NULL, transfer_initiator = NULL`
  on the transaction — the same `SET` clause the D-0024 resolve detach uses,
  keyed here by the outgoing captain's `id`. The attach reuses the existing
  insert logic: `AttachCaptain`'s body was extracted into an unexported,
  executor-accepting core (`attachCaptainExec(ctx, exec sqlExecer, c)`) called
  by both `AttachCaptain` (on `r.db`) and `SwapCaptain` (on the `*sql.Tx`), so
  the INSERT and its §6-D `last_seen_at` seeding are defined once and the two
  callers cannot drift.
- Scope: this is the **transfer-half of the no-auto-lapse captaincy model** —
  the counterpart to D-0024's resolve-half. Only `completeTransfer`'s
  transactionality changed; the §B state machine, the D-0017 confirm/cancel
  authorization binding, the deny-only sessiongate, and the resolve-path detach
  are untouched.
- Deliberately deferred: this fix leaves **three** captain-write patterns in
  the tree — resolve's inline tx detach (D-0024), the still-used non-tx
  `MarkCaptainDetached` / `AttachCaptain` primitives, and `completeTransfer`'s
  new tx swap. Consolidating them behind one tx-aware detach/attach seam is
  recorded as a backlog item (`docs/backlog/captain-write-consolidation.md`)
  and is **out of scope** here — collapsing the patterns would expand the blast
  radius of a targeted durability fix.
- Basis: fix in `internal/sessionmodel/captain.go` (`completeTransfer` now
  calls `s.repo.SwapCaptain`) and `internal/sessionmodel/repository.go`
  (`SwapCaptain` / `swapCaptainWithHook`, the shared `attachCaptainExec`, and
  the `sqlExecer` seam). True rollback test
  `TestCaptain_TransferSwapAtomicOnAttachFailure` in
  `internal/sessionmodel/captain_test.go` injects a fault between the detach and
  attach (via the `SwapCaptainWithHook` test seam, mirroring D-0024's
  `ResolveIncidentRegimeWithHook`) and asserts the swap rolled back: the
  original captain is still active with `detached_at` NULL and no incoming row
  was inserted. The test fails if the two writes are taken off the shared
  transaction (proven by re-running it with the detach moved onto `r.db`: the
  detach commits before the fault and the session goes captain-less). The
  happy-path transfer is covered by the existing
  `TestCaptain_B1_PrincipalThreadedAfterConfirm` and
  `TestCaptain_6D_IncomingInitiatedWhenUnreachableDirectConfirm`, which still
  pass through the new swap.
- Supersedes: nothing — closes gap #6 from the incident-captain-flow audit.

---

## D-0024 — Incident resolve detaches the active captain atomically with the regime flip (resolve-half of the no-auto-lapse captaincy model)

- Date: 2026-06-08
- Status: IMPLEMENTED. In the tree as of this date.
- Gap: `ResolveIncidentRegime` flipped `system_regime` back to `normal` and
  transitioned the incident session to `resolved`, but performed no write to the
  `session_captains` row. The active-captain row therefore survived resolution
  with `detached_at IS NULL`, so `GetActiveCaptain` / `CurrentCaptainPrincipal`
  kept reporting a phantom captain on a resolved incident — a dangling
  active-captain row that reads treated as live. (This is gap #8 in
  `docs/investigations/incident-captain-flow.md`.)
- Decision: resolve now detaches the resolving incident's active captain. The
  detach is a `session_captains` UPDATE (`detached_at` set; transfer columns
  cleared, mirroring `MarkCaptainDetached`) keyed by `session_id` where
  `detached_at IS NULL`, executed **inside the existing resolve transaction**
  alongside the session-state transition and the regime→normal flip. They commit
  as a single unit, so there is no observable intermediate state where the regime
  is normal but a captain is still active, nor where the captain is detached but
  the regime still says incident. (The resolve writes already ran in one tx; the
  detach joined that tx — no new transaction was introduced.)
- Scope: this is the **resolve-half of the no-auto-lapse captaincy model** —
  captaincy ends only on explicit transfer or on incident resolve; there is no
  idle-timeout lapse. `session_captains` has no detach-reason column, so no reason
  is recorded (out of scope to add one). The transfer-swap path
  (`completeTransfer`) and its separate non-atomic finding (D-0017 area, gap #6)
  are untouched.
- Basis: fix in `internal/sessionmodel/regime_transitions.go`
  (`ResolveIncidentRegimeWithHook`, the detach UPDATE between the session-state
  transition and the regime clear). Break test
  `TestCaptain_ResolveDetachesActiveCaptain` in
  `internal/sessionmodel/captain_test.go` fails if the detach is removed;
  `TestCaptain_ResolveAtomicRegimeAndCaptain` asserts the joint post-condition
  (regime normal AND no active captain);
  `TestCaptain_DeclareAfterResolveAttachesCleanly` confirms a fresh declare after
  a prior resolve attaches the new captain without interference.
- Supersedes: nothing — closes gap #8 from the incident-captain-flow audit.

---

## D-0023 — Write-floor posture line in the task system prompt (proactive articulation, observation/safe_mode only)

- Date: 2026-06-08
- Status: IMPLEMENTED. In the tree as of this date. Realizes D-0019's observation
  posture in the LLM-facing system prompt; refines neither D-0018 nor D-0020.
- Decision: when the boot-resolved write floor is up, the task system prompt now
  carries a posture section telling the model its current posture, so it declines
  managed-system writes proactively with articulation instead of only reacting
  after the floor denies the tool call at execution. The section is **conditional
  on the floor reason** and added at the single prompt-assembly site
  (`internal/api/tasks.go`, `buildTaskRun`):
  - `observation` → an observation-mode posture line framed as Joe's intended
    read-only resting state. **No recovery/unlock language** — observation is the
    intended default, there is nothing for the user to fix or clear.
  - `safe_mode` → a different, safe-mode posture line framed as an emergency halt.
    **No user-directed recovery instruction**: restoration is framed as an operator
    action, and the model is told NOT to direct the user to clear the state or run
    any command (no `joe unlock`, no "see docs to restore"). Recovery guidance
    already lives in the reactive denial UI message; the prompt must not duplicate
    or contradict it.
  - `none` (full mode) → **nothing injected**. Full-mode write behaviour is
    governed by RBAC, not a prompt line; a "you can write" line would be
    behaviorally risky noise.
- Scope: this changes ONLY the model's proactive explanation. The **tool surface
  is unchanged** (no pruning — every tool stays advertised) and **enforcement is
  unchanged** (the floor still denies every Mutate at execution regardless of what
  the model does). Reads the same boot-sealed `services.WriteFloor` value the
  executor and the captaingate floor wrapper use; nothing is re-resolved.
- Basis: prompt text and the reason→section mapping live in
  `internal/prompts/posture.go` (`PostureSection`), per the invariant that all LLM
  prompt strings live in `internal/prompts/`. Tests in
  `internal/prompts/posture_test.go` assert the three cases and — the load-bearing
  guard — the absence of unlock/recovery language in both posture strings.
- Supersedes: nothing — implements part of D-0019.

---

## D-0022 — Denial-message precedence (floor > incident > RBAC) enforced by check order; autonomous-path seam routing deferred

- Date: 2026-06-08
- Status: PARTIALLY IMPLEMENTED. Task 1 (precedence) is in the tree as of this
  date. Task 2 (routing the autonomous Core Agent path through the shared seam)
  is DEFERRED with findings recorded below. Implements D-0019 decision 9 and the
  "autonomous path must route through the shared seam" item under D-0019's
  "Current state being changed". References D-0018 (the write floor) and D-0010
  (the shared §C captaingate; coreagent refresh confirmed read-only).

### Task 1 — denial-message precedence (IMPLEMENTED)

- Decision: when more than one denial could apply to a single attempted write,
  the user sees ONE reason, ordered **floor > incident > RBAC** (and within the
  floor, `safe_mode` > `observation`). Rationale: resolvability depth — show the
  reason the user can least readily fix, because it is the one actually blocking
  them. A floored Joe is read-only until restart (least fixable); an incident
  redirect needs the captain (less fixable than a zone grant); a zone denial is
  an ordinary RBAC grant away (most fixable).

- **Co-occurrence is possible, and is resolved by CHECK ORDER, not by the
  classifier.** Enforcement short-circuits at the first failing check, so for any
  single write attempt exactly ONE typed error is ever produced. The three error
  types (`*safety.WriteFloorError`, `*captaingate.GateRefusalError`,
  `access.ErrPermissionDenied`) are therefore mutually exclusive on a single
  `err`. The classifier `classifyWriteFailure` (`internal/api/writefailure.go`)
  only maps the one error that fired to its UI code — it does NOT decide
  precedence. Its branch order was realigned to floor → incident → RBAC as
  documentation of intent, but that change is functionally a no-op.

- The denials live in TWO layers, so precedence is enforced by reordering checks
  across BOTH:
  1. **`tools.Executor.Execute`** (`internal/tools/executor.go`): the write-floor
     check was moved ABOVE the zone/namespace scope checks, so for a Mutate that
     trips both, the `WriteFloorError` is the one error produced (floor > RBAC
     scope). Pinned by `TestExecutor_Floor_PrecedesZoneScope`.
  2. **`captaingate.Wrapper.Execute`** (`internal/captaingate/captaingate.go`):
     the §C incident gate sits in a wrapper UPSTREAM of the executor, so the
     executor reorder alone cannot make floor > incident. The wrapper now takes
     an optional `WithFloor` and checks the floor BEFORE the §C gate; a floored
     Mutate is refused with the floor reason and the gate is never consulted (no
     `GateRefusalError`, no `inner.Execute`, no `captain_gate_refused` audit row).
     §C2 (gate upstream of RBAC) is preserved — the floor simply becomes the new
     outermost gate. Pinned by `TestFloorPrecedesIncidentGate`; the inert-floor
     and read-through cases by `TestFloorDownGateStillRefuses` /
     `TestFloorAllowsReadsThroughGate`.
  - `safe_mode > observation` needs no runtime ordering: the floor resolves to
    exactly ONE reason at boot (`safety.ResolveWriteFloor`, panic wins over the
    env var), pinned by the pre-existing `TestResolveWriteFloor_Precedence`.

- **Behavior on the autonomous path is unchanged; the user-task path now
  enforces the floor.** The Core Agent executor (`internal/coreagent/agent.go`)
  issues only Reads (per D-0010), so neither the floor nor the gate fires on it
  today — the reorders there remain no-ops, correct BY CONSTRUCTION for the day
  an autonomous Mutate exists. The user-task executor (`internal/api/tasks.go`)
  now carries the floor (see "Discovered gap — CLOSED" below), so a user-task
  Mutate under an up floor IS denied with the floor reason, and `WithFloor` is
  wired at BOTH captaingate sites so floor > incident holds on both agentic
  paths.

- Discovered gap — **CLOSED** (2026-06-08): the D-0018 write floor was originally
  injected ONLY on the Core Agent executor, not on the user-task-loop executor —
  `internal/api/tasks.go` built its `tools.Executor` without
  `tools.WithWriteFloor`, so in observation/safe mode a user-task Mutate
  (`write_file`, `run_command`, `publish_doc_update_*`, `github_comment`, …) was
  NOT floor-blocked, contradicting the `WithWriteFloor` doc comment's claim that
  both construction sites are wired. Closed by adding
  `tools.WithWriteFloor(services.WriteFloor)` to the user-task `execOpts` and
  `captaingate.WithFloor(services.WriteFloor)` to the user-task captaingate
  wrapper (mirroring the Core-Agent site in `cmd/joe/server.go`), so the floor is
  enforced and the floor > incident precedence holds on the user-task path too.
  Pinned by `TestTaskEndpoint_WriteFloorBlocksMutate` (Mutate denied with the
  observation code), `TestTaskEndpoint_WriteFloorAllowsReads` (Reads still flow),
  `TestUserTaskExecutorFloor_ErrorsIs` (the seam returns a
  `*safety.WriteFloorError`, `errors.Is ErrWriteFloor`), and
  `TestTaskEndpoint_FloorPrecedesIncidentOnUserTaskPath` (with the floor up AND
  an incident regime active, a user-task Mutate surfaces the floor reason, not
  `incident_mode` — the `captaingate.WithFloor` line is what makes this hold; the
  test regresses to `incident_mode` if it is removed). The boot-floor
  immutability guards (`internal/safety/floor_guard_test.go`) remain green.

### Task 2 — route the autonomous Core Agent path through the shared seam (DEFERRED)

- The Core Agent's background graph-refresh path writes the graph DIRECTLY:
  each refresher (`internal/coreagent/{k8s,aws,azure,git,gitops,observability,
  networking,datastore,alerting,registry,crd}_refresh.go`) calls
  `BuildGraphDelta` → `ApplyGraphDelta(ctx, r.services.Graph, delta)`
  (`internal/coreagent/graphdelta.go`), which calls `store.AddNode/AddEdge/
  DeleteEdge/DeleteNode` on the graph store — bypassing the executor seam where
  the floor, classification, and §C gate live. ~25 call sites across the refresh
  files. Confirmed read-only on infrastructure per D-0010 (VERDICT-A); these
  graph writes are Reads under the binary model (arg-keyed idempotent upserts of
  Joe's own model), so they pass the floor and MUST keep flowing — observation
  mode must not freeze Joe's own model (a settled design point).

- Routing this through the seam is NOT a clean, mechanical reroute. It is
  non-trivially entangled, so per the staged rule it was not implemented:
  1. **Shape mismatch.** The seam (`Executor.Execute` / `captaingate.Wrapper`)
     is keyed by TOOL NAME + `map[string]any` args and classifies via
     `safety.ClassifyTool(name)`. The refresh path operates on typed
     `graph.Node`/`graph.Edge` via the store. There is no clean adapter.
  2. **Missing tools.** `ApplyGraphDelta` does AddNode, AddEdge, DeleteEdge,
     DeleteNode. Tools exist for `graph_add_node`/`graph_add_edge`/
     `graph_update_node` (all Read), but there is NO `graph_delete_edge` /
     `graph_delete_node` tool; and the existing graph tools are CORE tools that
     round-trip the in-process client/accessor, not the direct in-process
     `services.Graph` store writes the refresh uses. Routing through them would
     change the write MECHANISM, not just its path.
  3. **Autonomous principal does not exist.** The "autonomous principal"
     (`agent:core`) referenced by the design is NOT in live code — only
     `user:`/`group:`/`svc:` reserved prefixes are defined
     (`internal/rbac/identity.go`). Carrying it requires introducing a new
     reserved principal-kind — an identity-model change.
  4. **Behavior-change risk on the Reads.** The refresh runs in a background
     context with no principal. Routed through the accessor with an empty/new
     principal while RBAC is live, the accessor could DENY
     (`access.ErrPermissionDenied`) — the graph Reads must keep passing. This is
     precisely the "surface, do not paper over" case the task called out.

- Decision: DEFER routing to a dedicated follow-up that (a) defines the
  `agent:core` principal kind, (b) decides the seam shape for typed store writes
  (a store-level governed wrapper vs. new delete tools), and (c) proves the
  graph Reads still pass through the seam unchanged. The precedence work above
  already makes the floor > incident ordering correct in `captaingate` by
  construction, so the day that routing lands and an autonomous managed-system
  Mutate flows through the seam, the floor governs it before the gate. The
  deferral note at `internal/coreagent/agent.go` (New) is retained.

- Basis: code investigation 2026-06-08 against the live tree (executor.go,
  captaingate.go, writefailure.go, sessiongate.go, graphdelta.go + refreshers,
  tasks.go, server.go, agent.go, rbac/identity.go). Build/vet/test/gofmt clean;
  the boot-floor immutability break-tests (`internal/safety/floor_guard_test.go`)
  still pass.
- Supersedes: nothing. Implements part of D-0019; refines neither D-0018 nor
  D-0020 (the binary model and floor lifecycle are unchanged).

---

## D-0021: Rename "source" → "component"; flat model with type as a routing discriminator

Date: 2026-06-08
Status: IMPLEMENTED (2026-06-08). The lexical sweep landed: Go (`store.Source`→`store.Component`, `SourceRepository`→`ComponentRepository`, the `SourceType*` constants, the `sourceID`/`SourceID` seam→`componentID`/`ComponentID`, audit `Event.Source`→`ComponentID`, client/handler CRUD, `register_source`→`register_component`, `list_sources`→`list_components`); SQL via migration 023 (`sources`→`components`, `source_zone_assignments`→`component_zone_assignments`, the provenance `source_id` columns and `audit_log.source`→`component_id`, indexes, plus the `<type>_source`→`<type>_component` graph-label data migration); REST routes (`/api/v1/components`, `/admin/component-zones`, `{componentID}` path param) and `component_id` JSON; and the UI (`ComponentsPage`, `useComponents`, schemas/types, wire contract). `knowledge_sources`, `skills.TrustedSources`, panic `trigger_source`, the investigation "source session" columns, `Edge.Source`, and `onboarding_facts.source` were deliberately left untouched.

### Context

The model's top-level concept for a registered external system was named "source" (table `sources`, `sourceID` in the RBAC seam). "Source" names only the read direction. Joe now reads AND mutates these systems (apply a k8s manifest, push to a git repo, create a Grafana dashboard, edit an alert), so "source" no longer captures what the thing is. These systems span three rough categories — observability/telemetry backends, infrastructure platforms (k8s/AWS), and code repositories (IaC, app config, app code) — all of which Joe both reads and mutates.

### Decision

Rename the concept to "component": a part of the managed system that Joe represents as a node in its graph and reads or mutates as the situation needs. One flat top-level type, not a kind-split.

The earlier idea of splitting into two top-level kinds (read-only "sources" for telemetry vs mutable "components" for infra/repos) is REJECTED. Telemetry backends are mutable too (dashboards, alerts), so the write boundary does not run between the categories — it runs through each of them, at the operation level. The three categories are the same kind of thing under the only axis that structures Joe (the write definition, D-0018).

The existing `type` field is retained as a routing-and-presentation discriminator. It drives adapter dispatch, available-operation set, and node labels — NOT safety classification and NOT a structural kind split. Type values themselves are unchanged (aws, kubernetes, prometheus, …).

### Why this is safe to rename

A code investigation (2026-06-08) confirmed the safety layer is completely type-blind: tier classification keys on tool name, the write floor is a pure function of two boot booleans, RBAC keys on (principal, sourceID, action). No safety/tier/floor/RBAC decision reads source.Type. The ratified write-definition — "the boundary is the operation's effect on the managed system, not the kind of target" — is upheld in code. A rename therefore cannot disturb the trust model.

### Scope of the rename (lexical only)

- SQL: `sources` table → `components`; `source_zone_assignments` and any FK/index naming; a NEW migration, not an edit to an existing one.
- Go: `store.Source` → `store.Component`; `sourceID` → `componentID` in the guarded accessor / IsAllowed path and throughout.
- Admin REST: source-zones routes and JSON field names (breaking API change — acceptable pre-launch).
- Audit vocabulary: any audit row referencing a source.
- UI: SourcesPage and security API; be deliberate where the `Component` domain type meets React components (import naming).
- OASIS-facing API: check whether any scenario references a source field name through POST /api/v1/tasks; if so, OASIS needs a matching pass.
- Graph node labels: the `<type>_source` idiom becomes `<type>_component` (e.g. "prometheus_component"). Requires a data migration for existing graph rows carrying the old `_source` label.
- Docs: CLAUDE.md, identity/design docs, project-knowledge file.

### Explicitly out of scope (separate follow-ups)

- Adapter-construction consolidation: dispatch is fragmented across divergent type-keyed paths (see docs/backlog/adapter-dispatch-consolidation.md). This is a pre-existing latent bug. Fix AFTER the rename — rename is lexical and low-risk, consolidation is structural; interleaving them makes a coverage bug mid-sweep unattributable.
- The knowledge.Source model (knowledge_sources table; human/confluence/notion/session) is a different, unrelated concept. NOT renamed.

### Type values reference (unchanged by this decision)

Enforced in Go via AllowedSourceTypes() — no SQL CHECK/enum. ~37 values incl. aws, azure, git, kubernetes, prometheus, mimir, loki, tempo, jaeger, datadog, splunk, dynatrace, newrelic, cloudwatch, azuremonitor, alertmanager, pagerduty, grafana, postgresql, mysql, redis, mongodb, kafka, elasticsearch, argocd, terraform, helm, nginx-ingress, envoy, falco, oci_registry, dockerhub, artifactory, ecr, github, gitlab.

---

## D-0020 — Collapse the three-tier action classification (Observe/Record/Act) into a binary Read/Mutate axis

- Date: 2026-06-07
- Status: IMPLEMENTED. Unlike D-0018/D-0019 (design-pending), this entry records
  a code change that is in the tree as of this date. Refines D-0018/D-0019 — it
  realizes their "write = mutation of the managed system" definition as the
  single decidable axis the classifier carries.
- Decision: the action classification is now binary. `safety.ActionTier`
  (`TierObserve`/`TierRecord`/`TierAct`, the old T1/T2/T3) is replaced by
  `safety.ActionClass` with exactly two states: `ActionRead` (does not mutate
  the managed system) and `ActionMutate` (mutates the managed system). The
  classifier `ClassifyTool` returns one of the two; the struct field
  `ToolClassification.Tier`/`ActionInfo.Tier`/`AccessDeniedError.Tier` is
  renamed `Class`. `ActionClass.String()` returns `"read"`/`"mutate"`.

  Rationale: severity-of-mutation is DELIBERATELY NOT a classification tier. A
  static blast-radius taxonomy is hard to get right and hard to evaluate on a
  non-deterministic LLM. The classification answers one decidable question —
  does this operation mutate the managed system. Blast-radius safety lives in
  tools, skills, OASIS testing, and the per-zone/per-capability graduation
  ladder (D-0019), not in a tier.

  **Mapping (old → new).** Former Observe → Read; former Act → Mutate. The
  middle tier (Record) was already vacant after the prior reclassification
  (commit d3c34d3 / D-0018/D-0019): no registered tool was Record. It is
  DELETED from the type, not merged as a live value. Every tool carries forward
  its already-decided read/write nature unchanged (Joe's own graph/model
  maintenance — graph_add_*, register_source, save_onboarding_fact,
  save_knowledge_entry, generate_doc_draft — stays Read; external/managed-system
  writes — write_file, run_command, publish_doc_update_*, github/gitlab_comment,
  github_request_changes — are Mutate). The unknown-tool default is `ActionMutate`
  (deny-by-default, unchanged conservatism).

  **Consumer map (every layer that keyed off the tier; the binary question each
  now asks).** All behavior is preserved exactly; only the type changed.
  - Write floor — executor safe-mode check (`internal/tools/executor.go`): was
    `IsSafeModeActive() && tier > TierObserve`, now `&& class == ActionMutate`.
    Question: "is this Mutate" (deny Mutate). Equivalent set: with Record gone,
    "tier > Observe" and "is Mutate" denote the identical tool set. (The
    boot-resolved D-0018 floor proper is still design-pending; this safe-mode
    check is the live floor today.)
  - Pre-execution blocking notification (executor): was `== TierAct`, now
    `== ActionMutate`. Same set (Act == Mutate, Record vacant).
  - Post-execution audit notification (executor): was `>= TierRecord`, now
    `== ActionMutate`. Same set (Record vacant ⇒ fired only for Act before).
  - Captain/incident gate (`internal/captaingate`, `internal/sessiongate`):
    Read bypasses the gate; Mutate runs the §C captain-session check. Question:
    "is this Mutate". `sessiongate.Check`'s 5th param renamed `tier`→`class`
    (§C4 signature-pin guard unaffected — `class` is not a forbidden name).
  - Policy gate (`internal/safety/policy.go`): `IsT3Allowed` still gates Mutate
    by per-action grant. Question: "is this Mutate" + which capability is
    granted.
  - DurableExecutor (`internal/coreagent/executor_durable.go`): Read bypasses
    idempotency persistence; Mutate gets the §D5 RecordToolIntent → execute →
    MarkToolCompleted crash-resume protocol. Keyed off `ActionRead` now — the
    IDENTICAL operation set as before (former Observe bypassed; former Act
    persisted).

  **DurableExecutor coupling — temporary preservation, not the final design.**
  Keying crash-resume durability off the action class is a behavior-preserving
  stopgap. "Does this operation need crash-resume idempotency" is NOT the same
  question as "does this mutate the managed system." Joe's own model-maintenance
  creates are now Read and therefore bypass durability, losing idempotency on
  crash-resume — named casualties: register_source, save_onboarding_fact (and
  the graph_add_* family). This is a KNOWN, OUTSTANDING gap, intentionally NOT
  fixed here. The follow-up idempotency/durability-decoupling task will replace
  the binary key with a durability-specific predicate so durability tracks
  "needs crash-resume," independent of the Read/Mutate axis.

  **DurableExecutor decoupling — IMPLEMENTED (follow-up to the coupling note
  above; durability is now opt-in per tool, default OFF).** The
  DurableExecutor's wrap decision no longer reads the action class. A new
  per-tool boolean `ToolClassification.NeedsDurability` (in
  `internal/safety/tier.go`, declared alongside `Class`/`PolicyKey`) drives it:
  `executor_durable.go` wraps an op IFF `ClassifyTool(name).NeedsDurability` is
  set, else it bypasses (no key, no persistence). The §D5 protocol
  (RecordToolIntent → execute → MarkToolCompleted, replay short-circuit,
  crash-resume on 'issued') is unchanged — only what selects INTO it changed.

  Rationale for default-OFF (fail toward declare-the-few, not default-on): each
  wrapped op costs two synchronous fsyncs (persist 'issued', then the terminal
  status) plus an unbounded, never-pruned `tool_idempotency_keys` row carrying
  the full serialized result. Negligible for the handful of genuine creates;
  a material I/O and storage tax on the high-frequency read path. Worse,
  durability on a naturally re-runnable read risks serving a STALE cached
  result on a same-key replay (e.g. a metrics query that should re-run). So
  reads stay OFF and only non-idempotent operations opt in.

  Per-tool accounting (the audit; re-derived from each tool's actual
  create/append site and the underlying store method):
  - DECLARE NeedsDurability (wrapped):
    - `register_source` — `Source.Create` is a plain INSERT; the row ID is a
      crypto-random `type-<rand>` generated server-side OUTSIDE the args, so a
      retry creates a second source. (Casualty fixed.)
    - `save_onboarding_fact` — `Facts.Create` is a plain INSERT with an
      autoincrement `RETURNING id`, no natural key. (Casualty fixed.)
    - `save_knowledge_entry` — `knowledge.Service.Create` sets `id = uid.New()`
      server-side, plain INSERT, no unique key. (Additional casualty found by
      the audit, beyond the two named in the task.)
    - `generate_doc_draft` — `proposals.Create` sets `id = uid.New()`, plain
      INSERT; also wraps an expensive LLM draft generation. (Additional
      casualty found by the audit.)
    - `github_comment`, `gitlab_comment` — each posts a NEW comment/note to the
      PR/MR thread (server-assigned comment ID, no natural idempotency); a
      retry double-posts. Kept durable.
    - `github_request_changes` — files a NEW review; a retry duplicates it.
      Kept durable.
  - DO NOT declare (intentionally non-durable; idempotent / no duplicate risk):
    - `graph_add_node`, `graph_add_edge`, `graph_update_node` — UPSERTs keyed on
      caller-supplied args (node_id, or the `(from,to,relation)` edge key via
      `ON CONFLICT … DO UPDATE`). Re-running converges; no duplicate. (Note:
      the coupling note above loosely listed `graph_add_*` as casualties — the
      audit corrects that: they are idempotent and correctly NOT declared.)
    - `write_file` — overwrites a path (the path IS the natural key); rewriting
      with the same content is idempotent.
    - `run_command` — creates no Joe-side record; caching a command's output by
      args and replaying it would be WRONG (a fresh run must re-execute), and
      side-effect replay-safety is the command's own concern.
    - `publish_doc_update`, `publish_doc_update_{confluence,notion,git}` —
      guarded at the data layer: `PublishProposal` requires `status==approved`
      and flips it to `published`, so a re-publish of the same proposal fails
      closed rather than duplicating (a natural idempotency key). The
      crash-after-target-write/before-MarkPublished window is unprotected by
      durability either (an 'issued' key re-runs), so no protection is lost by
      dropping it.

  Wrapping-status changes vs. the old Mutate-only set:
  - Newly wrapped (were bypassed as Read): register_source,
    save_onboarding_fact, save_knowledge_entry, generate_doc_draft.
  - No longer wrapped (were wrapped solely for being Mutate): write_file,
    run_command, publish_doc_update, publish_doc_update_confluence,
    publish_doc_update_notion, publish_doc_update_git. Two distinct reasons,
    not one: write_file, the graph upserts, and the publish_doc_update variants
    are idempotent or data-layer-guarded (re-running converges / fails closed);
    run_command is non-idempotent but durability CANNOT protect it — replaying
    cached output would be wrong, so crash-resume re-executes by design and the
    command's own side-effect safety is out of durability's scope. Either way,
    no operation that needs (and can use) replay-safety silently lost it.
  - Unchanged-wrapped (Mutate, still declared): github_comment, gitlab_comment,
    github_request_changes.

  Casualty fix: register_source and save_onboarding_fact are durable again — an
  in-run duplicate call or crash-resume with identical args short-circuits to
  the cached result and creates no second row (the §D5 key is stable because
  their IDs are generated server-side, outside the args-hash).

  STILL OPEN (named explicitly, NOT built here):
  1. Dedup is PER-RUN only, not cross-run. The idempotency key is
     `SHA256(runID + tool + sorted-args-hash)`, so it deduplicates within a
     single run; it does NOT guarantee cross-run uniqueness (two separate runs
     each registering "prod-cluster" still create two rows). True "never two
     rows for the same logical source/fact" needs a natural unique key or a
     get-or-create at the data layer (`sources`/`onboarding_facts`), not
     durability. Separate, unaddressed follow-up.
  2. The `tool_idempotency_keys` table has no pruning/TTL — rows live for the
     run/session lifetime, reclaimed only via FK cascade when the run/session
     is deleted. Acceptable for the small declared-durable set, but should be
     noted; a high-volume durable tool would grow it unbounded.

  **Backward-compat shim retained.** Existing `~/.joe/safety-policy.yaml` files
  may carry a `record:` block. The `SafetyPolicy.Record`/`RecordPolicy` struct
  and `IsT2Allowed` are RETAINED in `internal/safety/policy.go` purely so those
  files still deserialize; `CheckAccess` no longer calls `IsT2Allowed` (no tool
  is Record). Separately, `internal/runmodel` has its OWN `Tier` type
  (`TierT1/T2/T3` = 1/2/3) persisted to the `action_ledger.tier` column. It is a
  DISTINCT type, never converted from the safety classification, and
  `AppendLedger` has no production caller (test-only). It is out of scope for
  this collapse and left untouched; flagged here so the next task knows the
  ledger still encodes 1/2/3 by number.
- Basis: re-derived against the live tree — `internal/safety/tier.go` (type,
  classifier, registry, `CheckAccess`), `policy.go` (retained shim),
  `internal/tools/executor.go`, `internal/captaingate/captaingate.go`,
  `internal/sessiongate/sessiongate.go`, `internal/coreagent/executor_durable.go`,
  `internal/safety/notifier.go`. Break-tests added: `TestActionClass_IsBinary`
  (exactly two states, middle gone), `TestClassifyTool_UnknownDefaultIsMutate`,
  `TestCheckAccess_ModelMaintenanceAlwaysAllowed` (a graph/model read passes the
  floor), `TestClassifyTool_ExternalCommentsAreMutate`,
  `TestClassifyTool_GraphMutationFamilyIsRead`. Captain-gate and policy-gate
  behavior-preservation are covered by the existing captaingate/policy tests,
  which exercise the real classifier (write_file = Mutate, read_file = Read) and
  pass unchanged.

  Durability decoupling (implementation note above) basis: re-derived the wrap
  site (`internal/coreagent/executor_durable.go`, formerly the
  `classification.Class == ActionRead` bypass, now `!NeedsDurability`), the key
  derivation (`computeIdempotencyKey` = SHA256(runID|tool|argsHash)), and each
  tool's create/append site against its store method (`store.sources.go`,
  `store.facts.go`, `knowledge/{service,repository}.go`,
  `knowledge/proposals/service.go`, `graph/sqlite.go AddEdge` ON CONFLICT,
  `api/inproc_client.go PublishProposal` status guard). Break-tests added:
  `safety.TestClassifyTool_NonIdempotentCreatesNeedDurability` (pins the seven
  declared tools — guards the default-OFF silent-gap risk),
  `TestClassifyTool_IdempotentToolsAreNotDurable`,
  `TestClassifyTool_UnknownToolNotDurable` (default OFF), and in coreagent
  `TestDurableExecutor_DrivenByProperty` (Read+declared wrapped, Mutate+
  undeclared bypassed — durability no longer reads the class),
  `TestDurableExecutor_UndeclaredBypass`,
  `TestDurableExecutor_InRunReplayDedupsCreate` (computed runID+args key
  short-circuit on a declared create). The pre-existing durable tests
  (D5Ordering, ReplayShortCircuit, CrashResume, NoGoroutineFanOut) were
  repointed from `write_file` (no longer wrapped) to `register_source` (now
  declared durable); their intent — ordering, replay short-circuit,
  crash-resume re-run, no goroutine fan-out — is unchanged.
- Supersedes: nothing — refines D-0018/D-0019. Follow-up status: the
  idempotency/durability decoupling (named casualties above) is now RESOLVED
  (see "DurableExecutor decoupling — IMPLEMENTED"). Two items remain open and
  are NOT this change: cross-run uniqueness for sources/facts (needs a natural
  unique key or get-or-create at the data layer), and pruning/TTL for the
  `tool_idempotency_keys` table.
- Known cleanup debt — persisted three-valued tier (deferred, NOT blocking).
  Discovered after the collapse: a three-valued tier concept survives in the
  run-model persistence layer. It is INERT but contradicts the binary model.
  - What survives: the `runmodel.Tier` type with constants
    `TierT1`/`TierT2`/`TierT3` (= 1/2/3) in `internal/runmodel/types.go`, used
    as the `Tier` field of `LedgerEntry`; and the `action_ledger.tier` column,
    `tier INTEGER NOT NULL CHECK (tier IN (1, 2, 3))`, from
    `internal/store/migrations/010_run_model.up.sql`. (This is the `runmodel`
    `Tier` flagged out-of-scope under "Backward-compat shim retained" above,
    now fully characterized.)
  - Current status (honest): there is NO production writer of the action
    ledger — `AppendLedger` is called only from tests, and the production
    `DurableExecutor` path persists idempotency keys, not ledger rows — so the
    column is unpopulated in real deployments. There IS one production reader,
    `getSITREP` (the `GET /api/v1/runs/{id}` handler, `internal/api/runs.go`,
    via `ListLedgerForRun`), but it does NOT interpret the tier: it passes the
    raw int straight through to JSON. So the concept is inert, not actively
    buggy — no production path reads-and-interprets a stale three-valued
    semantics.
  - The landmine: the `CHECK (tier IN (1, 2, 3))` constraint actively
    contradicts the binary model. If any writer is reintroduced under the
    Read/Mutate classification, it has no natural 1/2/3 value to write; a
    zero-value `Tier` (0) would VIOLATE the CHECK and fail the insert. The
    schema must not be left constrained this way indefinitely — this is why the
    cleanup eventually has to happen, rather than being purely cosmetic.
  - Cleanup scope (when done, not now): a new migration dropping the `tier`
    column and its CHECK; remove the `Tier` field from `LedgerEntry`; delete the
    `Tier` type and its `TierT1/T2/T3` constants; remove `tier` from the
    INSERT/SELECT and the `Tier(tier)` mapping in the `internal/runmodel`
    repository (`repository.go`); update the three test files that reference the
    old tier constants (`internal/runmodel/schema_test.go`,
    `internal/runmodel/cascade_schema_test.go`,
    `internal/api/cascade_delete_test.go`).
  - Adjacent item, same cleanup: `LedgerEntry` has NO JSON tags, so its fields
    (including `Tier`) serialize with capitalized Go-default names through the
    `GET /runs/{id}` response — the same no-JSON-tags issue D-0019 flagged for
    the regime endpoint. Fix when the ledger is cleaned.
  - Deferred, not blocking: it is inert today (no writer), and the trust-model
    floor work (D-0018/D-0019) takes priority.

---

## D-0019 — Joe's trust model: two boot postures, graduated capability, and fail-closed-empty-RBAC as the load-bearing safety boundary

- Date: 2026-06-07
- Status: design decision of record; implementation PENDING. No code currently
  realizes this trust model — nothing here is implemented yet, and the live
  code diverges from this entry. This entry records the target design and the
  divergence honestly; it does NOT claim any code implements it. (Same
  "accepted-as-design, build pending" posture as D-0018.)
- Companion to D-0018. D-0018 recorded the write floor's lifecycle, immutability,
  sticky-panic semantics, and the load-bearing definition of a write. THIS entry
  records the broader trust model the floor sits INSIDE: the posture model, the
  principal model, the capability ladder, and the empty-RBAC fail-closed
  guarantee. Where the two meet, this entry references D-0018 and does not
  repeat it.
- Context: Joe must be safe to adopt by default and must scale from read-only
  observation to eventual lights-out autonomous operation as a GRADIENT, not a
  single read-only/read-write bit. The definition of a write — mutation of the
  managed system (live infrastructure and the code/config that governs it),
  where reads include source queries, Joe's own graph/model maintenance, and
  notifications to humans — is established in D-0018 and assumed here.

  The decision, as numbered points:

  1. **Two boot postures, env-var-selected, restart-to-change.** Observation
     mode is the day-one default: a hard read-only floor (D-0018) where Joe
     reads but performs no managed-system mutation regardless of RBAC, enforced
     BELOW RBAC so no policy or grant can override it. It is the intended
     resting state, not an emergency, and the UI presents it calmly.
     Full-capabilities mode permits writes at the binary level but boots Joe at
     the BOTTOM of its capability ladder with zero write grants; RBAC becomes
     the floor.

  2. **Fail-closed-with-empty-RBAC is the real safety boundary — not a setup
     wizard.** The day-1-to-day-2 transition (flipping the env var to full mode)
     is Joe's single most dangerous configuration change. Its safety must NOT
     rest on any UI screen that can be skipped or that runs after the backend is
     already write-capable. It rests on two backend properties: full mode
     requires authentication ON, and with no policy rows every write is denied.
     "Full mode, no grants yet" must be a genuine fail-closed floor with the
     SAME observable behavior as observation mode (Joe performs no
     managed-system mutation), enforced at a different layer (RBAC rather than
     the hard floor). The env-var flip removes the hard ceiling; empty RBAC
     remains a floor. The two dangerous acts — flipping the env var and granting
     capability — stay separate by construction. This is the load-bearing safety
     property of the trust model.

  3. **This requires fixing RBAC's current inert/permissive-by-default
     behavior.** As of the investigation (verify against live code): the policy
     engine instantiates only when a service account or OIDC is configured
     (around `cmd/joe/server.go`); with auth off the default identity is
     permissive and the access guard short-circuits allow-all with reason
     `rbac_disabled` (around `internal/access/access.go`); the agentic task,
     stream, and chat routes are not source-keyed but do carry a context
     principal evaluated at the access guard (around `internal/api/tasks.go` and
     `internal/agentloop`). With auth ON and empty policy rows, the engine
     already fails closed (no grant, and the default `unassigned` zone allows
     only read). The trust model requires that full-capabilities mode cannot run
     write-capable with a permissive/absent engine — full mode demands auth ON
     and a live engine, so the fail-open path is UNREACHABLE in full mode. This
     is the central obstacle the implementation must close, not defer.

  4. **The principal model — who authorizes a write.** Interactive
     (human-initiated) writes gate against the launching human's grants;
     graduation means granting that human or their role write capability in a
     zone. Autonomous (Core Agent) actions gate against a dedicated autonomous
     principal (named to match the existing `user:`/`svc:` convention, e.g.
     `agent:core` — verify the convention against live code). Both resolve to a
     principal and both go through the SAME enforcement seam; neither has a path
     that skips gating. The autonomous principal exists from day one with zero
     write grants, so autonomous Joe is read-only by enforcement, and its
     current operations (source queries, graph/model refresh) are reads under
     the D-0018 write definition and pass the floor. The current divergence to
     close (as of the investigation, verify against live code): the autonomous
     Core Agent refresh bypasses the executor seam entirely, writing the graph
     directly via the graph-delta path (around `internal/coreagent`), carrying
     no principal — it must be routed through the shared seam so that the day a
     managed-system autonomous write exists, it is governed by the same floor
     and RBAC as everything else, by construction.

  5. **The capability ladder.** In full mode, graduation is per-zone and
     per-capability: observe, then granted writes in dev, then staging, then one
     production zone, then wider, toward eventual lights-out autonomous
     operation. The trust model is a gradient, not a single bit.
     Autonomous-write capability is a FUTURE grant on this existing ladder for
     the autonomous principal — explicitly NOT built now; no autonomous-write
     subsystem is built in this work. The mechanism for lights-out already
     exists the moment the autonomous principal and the uniform gate exist: when
     lights-out is real, an operator grants the autonomous principal write in a
     zone via the same ladder. No new subsystem is required at that point.

  6. **The LLM's tool surface under posture: exposed-and-deny, not hidden.** All
     tools remain advertised to the model regardless of posture or grants;
     authorization is enforced at execution and denials are fed back to the
     model as tool-results (this is already the codebase behavior as of the
     investigation, verify against live code — the full registry is always
     advertised, around `internal/tools/registry.go` and `internal/agentloop`,
     and denied calls return error tool-results). Tool-surface pruning by
     posture is deliberately NOT built: a prior zone-violation finding (treat as
     a lead, not verified here) showed Joe lost its zone-first refusal language
     when the tool surface changed between read and write — hiding tools removes
     the refusal there is to articulate, which degrades safety-evaluation
     behavior. To gain proactive (rather than only reactive) refusal
     articulation, the model is TOLD its posture: a posture line is added to the
     system prompt in observation mode (and, in full mode, the zone-scope prompt
     mechanism already conveys authorized zones, around
     `internal/prompts/zones.go`), so the model can refuse with articulation
     before attempting a denied call. The model is NOT told it is in safe mode
     today (as of the investigation, verify against live code, no safe-mode or
     panic reference exists in the prompts package) — adding the
     observation-posture line is net-new.

  7. **Two distinct "Joe does nothing" states must be presented differently —
     and they are different mechanisms, not one state rendered twice.**
     Observation mode is the hard env-var floor (D-0018) — a deliberate ceiling;
     the UI reassures ("Joe is running in observation mode — no changes will be
     made," calm, with a link to an explanatory doc), bound to a posture read
     endpoint. Full-mode-with-zero-grants is RBAC denying for lack of a grant —
     a soft floor and an invitation to configure; it is surfaced by an on-demand
     "evaluate Joe's write capability" PULL mechanism (a button, optionally
     scheduled), NOT a pushed banner. The banner reads the floor; the
     grant-state is pulled on demand. This distinction is UI on top of the two
     backend mechanisms; it carries no safety weight because the fail-closed
     floor (point 2) holds regardless of what the UI shows.

  8. **The read path.** A posture read endpoint reports the current mode
     (observation versus full) and, in full mode, a coarse "any write grants
     exist" signal sufficient for the UI to distinguish configured from
     zero-grants — derived from the audit trail of write-policy creation (the
     admin REST API is the sole audited writer of RBAC state per prior
     decisions, so this is derivable rather than a new mutable flag; verify that
     sole-writer property against live code). The endpoint is auth-gated only,
     consistent with the existing panic-status and regime endpoints, and uses
     explicit snake_case JSON tags — NOT Go default serialization (as of the
     investigation, verify against live code, the regime endpoint serializes a
     struct with no JSON tags, emitting capitalized Go-default field names around
     `internal/sessionmodel` and `internal/api/regime.go`; do not repeat that).
     A calm observation-mode banner is bound to a real fetch of this endpoint
     and mounted alongside the existing safe-mode and incident banners.

  9. **Denial-message precedence when more than one denial could apply to a
     single write:** the floor first (and within the floor, `safe_mode` over
     `observation`, per D-0018), then incident/captain gate, then RBAC zone
     denial. Ordered by resolvability depth — show the user the reason they can
     least readily fix, because it is the one actually blocking them.
     Implementation note (verify against live code, do not act here): the
     current classifier evaluates incident, then permission-denied, then
     safe-mode (around `internal/api/writefailure.go`), which does not match this
     precedence; and the in-`Execute` checks place RBAC scope before the floor
     (around `internal/tools/executor.go`). Whether precedence is a real runtime
     collision or is already foreclosed by enforcement short-circuit order must
     be determined in implementation, and enforced by reordering the checks, the
     classifier branches, or both.

- What this deliberately does NOT do:
  - No runtime posture toggle (boot + restart only; the runtime stop-all-writes
    need is served by panic, per D-0018).
  - No autonomous-write capability or any autonomous-write subsystem (a future
    grant on the existing ladder).
  - No tool-surface pruning by posture (exposed-and-deny is retained
    deliberately).
  - Does NOT rest the day-2 safety on any setup wizard or first-login UI: the
    fail-closed empty-RBAC floor is the boundary; any setup or awareness UI is
    advisory UX on top of a hard backend floor, not the floor itself.
  - Does NOT finalize the first-login full-mode setup/awareness flow or a
    write-configuration latch (parked — their shape depends on enumerating what
    full mode requires configured beyond the first grant, which is deferred;
    whether such a latch is its own concept or merely the setup-step completion
    state is unresolved and downstream of that flow design).

- Relationship to other decisions:
  - References D-0018 for the floor lifecycle, immutability, sticky panic, and
    the write definition.
  - The principal model and the empty-RBAC fail-closed work are the
    implementation track that FOLLOWS this entry.
  - The RBAC sole-writer and audit-trail properties this entry relies on come
    from the prior identity-stage decisions — D-0016 (the admin REST API as the
    sole RBAC/identity writer; the audited admin surface) building on D-0012 (the
    admin gate) and D-0013 (admin-mutation audit). If the sole-writer/audit-trail
    mapping needs finer confirmation against those entries, treat that
    cross-reference as to-be-confirmed.

- Current state being changed (target diverging from live code; every item is
  "as of the investigation, verify against live code" and is NOT acted on here):
  - RBAC inert/permissive when auth off must become UNREACHABLE in full mode
    (full mode requires auth on and a live engine).
  - The autonomous Core Agent path must be routed through the shared enforcement
    seam and carry the autonomous principal.
  - A posture read endpoint with snake_case tags is net-new.
  - An observation-posture system-prompt line is net-new.
  - The observation-mode banner and the on-demand write-capability evaluation
    are net-new UI.
  - The denial precedence may require reordering enforcement and/or
    classification.

- Basis: a prior trust-model / safe-mode investigation (the file:line
  coordinates above are from that investigation and are marked
  verify-against-live-code; they were not re-verified for this entry, which is
  documentation-only). This entry records a DESIGN decision; no code change
  accompanies it, and the live behavior described under "Current state being
  changed" is what the design supersedes once implemented.
- Supersedes: nothing yet — the design is not yet implemented. Companion to
  D-0018 (the write floor's lifecycle and immutability), which this entry
  surrounds with the broader trust model. Builds on the identity-stage
  decisions D-0016/D-0013/D-0012 for the RBAC sole-writer and audit-trail
  properties it relies on. Adjacent pending decisions: the
  floor-vs-other-gate precedence (point 9) and the first-login full-mode flow
  (parked, above).

---

## D-0018 — The read-only write floor as a boot-resolved, runtime-immutable security boundary; safe mode is absorbed as one reason the floor is up, not a separate mechanism

- Date: 2026-06-07
- Status: design decision of record; the floor proper (points 1–5, 8) is now
  IMPLEMENTED — see the dated "boot-resolved, runtime-immutable write floor
  landed" implementation note at the end of this entry. The design narrative
  below was written before the build and is preserved as-is; where it says
  "PENDING"/"diverges from live code", read it against that note, which records
  what landed and what remains deferred (autonomous-path seam routing, the
  posture read endpoint/observation banner, and floor-vs-other-gate precedence).
- Decision: Joe's trust model has two boot postures — observation mode (a hard
  read-only floor, the day-one default) and full-capabilities mode (RBAC is the
  floor) — which, together with the pre-existing panic/safe mode, are unified
  into a single notion: "Joe cannot mutate the managed system right now." This
  entry covers only the floor's lifecycle and security guarantees. It does NOT
  cover full-mode graduation or empty-RBAC fail-closed behavior; those are
  separate pending decisions. The floor is treated as a SECURITY BOUNDARY, not
  a feature flag.

  **Threat model (what the floor must withstand).** (i) A fully compromised or
  erratic LLM emitting arbitrary tool calls. (ii) A human attacker reaching
  Joe's API as an authenticated caller. It explicitly does NOT defend against
  an attacker who controls the deployment substrate — host, environment, state
  file, supervisor — because such an attacker owns Joe regardless. That is the
  boundary's honest edge.

  **Definition of a write (load-bearing).** A write is an operation that
  mutates the state of the MANAGED SYSTEM — the live infrastructure Joe
  operates and the code/config that governs it (the sources). The test is
  whether the managed system is in a different state after the operation.
  Everything else is a read, even when it mutates local storage or emits
  something a human receives:
  - querying sources is a read;
  - recording observed state into Joe's own graph DB (the Core Agent refresh)
    is a read, because the graph is Joe's MODEL of the system, not the system;
  - emitting notifications or alerts to humans is a read, because it changes
    what humans know and Joe's own state, not the managed system.
  Consequence: graph-mutation operations are read-tier (T1) under this
  definition, NOT T2, and the floor governs only managed-system mutations.

  The decision, as numbered points:

  1. **One floor, two reasons, one error.** A single write floor denies all
     managed-system mutations (T2/T3) for every principal, including the
     autonomous one. It is up at boot if EITHER the observation env var is set
     OR a sticky panic state file is present. The reason rides out of the
     single denial as data — one write-floor error carrying a `reason` field,
     `reason` being either `observation` or `safe_mode` — not two code paths.
     Presentation differs per reason; enforcement is identical and singular.

  2. **Boot-resolved, runtime-immutable.** The floor is resolved exactly once
     at boot from its inputs into a sealed read-only value and is never
     re-derived from disk during the process lifetime. Within a process
     lifetime it is immutable — no endpoint, tool, LLM output, or operator
     action short of a restart can lower it. The live binary contains no
     down-transition capability; any deactivate or reset is removed from
     production and is test-only at most. Immutability is guaranteed by the
     lowering operation NOT EXISTING in the running program, not by permission
     guards.

  3. **One way down: change boot inputs and restart.** No live-clear path
     exists. Leaving observation mode and recovering from panic are the same
     operation: set the writable env var, ensure no sticky panic, restart.

  4. **The unlock HTTP endpoint and the unlock HTTP client command are
     deleted.** Panic recovery is a deploy-access operation, not an API
     operation. Clearing the sticky panic reason is done by a purely local
     host CLI that edits the panic state file while Joe is down, records an
     acknowledgment reason for audit, and never contacts or signals a live
     process. There is NO authorized API surface for clearing the floor.

  5. **Panic is sticky and wins over the env var.** When boot inputs disagree
     — env var writable but panic state present — the floor stays up. A panic
     must be explicitly acknowledged by clearing the panic state; flipping the
     env var alone is insufficient. This keeps acknowledging-the-panic and
     arming-writes as distinct deliberate steps, neither sufficient alone.

  6. **Protection A — no self-API reach.** No tool may call Joe's own
     control-plane endpoints or issue arbitrary HTTP to Joe's own API. If
     nothing in the tool registry routes to a floor, panic, or admin control,
     the LLM cannot reach the floor. Asserted by break-test over the registry.

  7. **Protection B — no self-restart influence.** Joe can stop itself (panic
     via process exit) but cannot start itself or influence the terms of its
     own restart. Restart authority is external — a supervisor with a restart
     policy, or a human. Panic uses process exit, never self-re-exec, because
     re-exec would place restart authority inside Joe and let a compromised Joe
     choose to return writable. Joe must have no ability to modify its own boot
     inputs (env var, panic state file location, supervisor config); those live
     outside Joe's writable surface.

  8. **Within-floor reason precedence.** `safe_mode` outranks `observation`
     when surfacing the reason. Precedence between the floor and OTHER denial
     sources (incident/captain gate, RBAC zone denial) is deferred to a
     separate precedence decision.

- Recovery process (the focused human work, ordered): the human triggers
  panic; Joe exits and writes the panic state; Joe is down. The human
  investigates by hand. A naive supervised restart brings Joe back read-only
  because the panic state is sticky, with a startup message stating the panic
  was not cleared and how to resume — Joe observes but cannot write. When the
  human decides it is safe, they clear the panic state via the local host CLI
  (recording a reason), set the env var to the intended posture, and restart.
  On boot: panic cleared + env writable yields a writable Joe; panic cleared +
  env read-only yields observation mode. All failure modes are safe-by-default:
  doing nothing leaves Joe down (most inert); restarting without clearing panic
  yields a read-only Joe; clearing panic but leaving the read-only env var
  yields an observation Joe. No careless path yields a writable Joe.

- What this deliberately does NOT do:
  - No runtime toggle into or out of observation mode (boot + restart only; a
    runtime stop-all-writes need is served by panic, not a second control).
  - No API path to clear panic or lower the floor (the surface is ELIMINATED,
    not protected).
  - No defense against a deployment-substrate attacker (whoever controls env
    vars, the panic state file, and restart authority controls the floor —
    this is the boundary's honest edge).
  - Panic-recovery semantics REQUIRE a supervisor: bare Joe with no restart
    policy means panic equals halt and Joe stays down (a safe fail). So "panic
    puts Joe in safe mode" precisely means "panic stops Joe; a supervised Joe
    reboots into safe mode" — running under a supervisor with a restart policy
    is a documented deployment requirement.
  - No autonomous-write capability: the Core Agent path is routed through the
    shared seam and classified read-tier, with managed-system write capability
    deferred to a future graduation step.

- Current state being changed (target diverging from live code). Every
  coordinate below is from a prior investigation, may be STALE, and is NOT to
  be acted on as part of recording this decision — each is "as of the
  investigation, verify against live code":
  - Safe mode is currently boot-set but live-clearable, with the unlock path
    calling an in-process deactivate with no restart (as of the investigation,
    around `internal/safety/unlock.go` and `internal/api/panic.go` — verify
    against live code). The live down-transition must be removed and
    reset/deactivate become test-only. Note the reset function's own comment
    already claims it is restart-only for testing, which the live call
    contradicts.
  - The floor is currently a mutable process-global atomic boolean with a
    public setter (as of the investigation, around
    `internal/safety/safemode.go` — verify against live code) and becomes a
    resolve-once read-only value.
  - The single denial branch currently keys on a plain safe-mode sentinel
    error with no reason field (as of the investigation, around
    `internal/tools/executor.go` and `internal/safety/safemode.go` — verify
    against live code) and is subsumed into the write-floor error with a
    `reason` field. The break-set that must continue to satisfy `errors.Is`
    against the floor sentinel: the classifier in
    `internal/api/writefailure.go`, the executor safe-mode test, and two
    assertions in the writefailure test.
  - The autonomous Core Agent refresh currently bypasses the executor seam
    entirely, writing the graph directly via the graph-delta path (as of the
    investigation, around `internal/coreagent` — verify against live code),
    carrying no principal and no tier check. It must be routed through the
    shared seam so future managed-system writes are governed by construction,
    while its current graph mutations classify as read-tier and pass the floor
    so observation Joe's graph stays live.
  - Graph-mutation tools are currently classified T2 (as of the investigation,
    around `internal/safety/tier.go` — verify against live code) and must be
    reclassified to T1 per the write definition. The full tier map must be
    audited against the managed-system-mutation test, with the dangerous
    direction being any UNDER-classified real infrastructure mutation rather
    than the over-classified graph ops.
  - The unlock CLI command is currently an HTTP client call (as of the
    investigation, around `cmd/joe/main.go` — verify against live code) and is
    replaced by a local file-only operation, with the endpoint and HTTP-client
    paths deleted.

- Invariants to assert (the break-tests the implementation must add):
  - The production binary contains no runtime down-transition of the floor (no
    production caller of any deactivate or reset).
  - The floor value is read from a single boot-resolved source and never
    re-derived from disk mid-process.
  - No registered tool at any tier routes to a floor, panic, or admin control,
    or can issue HTTP to Joe's own control plane.
  - A write-floor error satisfies `errors.Is` against the floor sentinel
    (preserving the existing dependents).
  - With the panic state present, the floor boots up regardless of the env var.
  - Panic uses process exit rather than self-re-exec, with no production path
    letting Joe alter its own boot inputs.

- Basis: a prior trust-model / safe-mode investigation (the file:line
  coordinates above are from that investigation and are marked
  verify-against-live-code; they were not re-verified for this entry, which is
  documentation-only). This entry records a DESIGN decision; no code change
  accompanies it, and the live behavior described under "Current state being
  changed" is what the design supersedes once implemented.
- Supersedes: nothing yet — the previous standalone safe-mode lifecycle is
  absorbed into the unified floor by this design, but that supersession takes
  effect only when the implementation lands. Relates to the existing
  panic/safe-mode mechanism (`internal/safety/`), which this design unifies
  with the observation-mode floor. Adjacent pending decisions (NOT covered
  here): full-mode graduation, empty-RBAC fail-closed, and floor-vs-other-gate
  precedence.

- Implementation note (2026-06-07) — tier-map reclassification landed. This
  note records the partial implementation of the write definition above
  (D-0018) and its trust-model application (D-0019) in the tool tier
  classification only. The floor's lifecycle/immutability (points 1–8) remains
  PENDING; only the classifier in `internal/safety/tier.go` was changed.
  - Classifier confirmed: `ClassifyTool` + `toolRegistry` in
    `internal/safety/tier.go`; tiers `TierObserve`=1 < `TierRecord`=2 <
    `TierAct`=3; unknown tools default to `TierAct` (the most conservative
    tier) — left unchanged, now guarded by a test.
  - Reclassified Joe-own-model maintenance from T2 (Record) to T1 (Observe),
    per the write definition (these mutate Joe's model, not the managed
    system): `graph_add_node`, `graph_add_edge`, `graph_update_node`,
    `register_source`, `save_onboarding_fact`, `generate_doc_draft`. This
    realizes the "graph-mutation operations are read-tier (T1), NOT T2"
    consequence stated above.
  - Added four registered tools that were MISSING from the tier map and so
    fell through to the unknown→TierAct default (permanently denied — a
    safe-direction but functionally broken state): `save_knowledge_entry`
    (Joe-own knowledge store) and the read-only `registry_query`,
    `artifactory_query`, `ecr_query`. All added at T1 (Observe).
  - `github_comment` / `gitlab_comment` were T2 (Record). Per a deliberate
    decision (posting to a PR/MR mutates an external system and is not
    idempotent on retry), they are reclassified UP to T3 (Act) as
    managed-system writes — not down to observe. They were already
    deny-by-default (their policy keys are unrecognized by `IsT3Allowed`/
    `IsT2Allowed`, same pre-existing gap as `github_request_changes` and
    `publish_doc_update*`); the change corrects the label and routes them
    through the T3 blocking pre-execution notification.
  - Latent floor hole found and closed (the dangerous under-classified
    direction this audit was meant to catch): `http_request` was T1 but
    accepted POST/PUT/PATCH/DELETE with a body to any URL — a write-capable
    tool classified read-only, always allowed and ungated. It is a live
    registered tool. Resolved by restricting the tool to GET/HEAD in
    `internal/tools/shared/httpreq/httpreq.go` (mutating verbs now rejected
    before any request), making its T1 classification correct rather than
    bumping it to T3 (which would have broken legitimate probing and, lacking
    a policy key, denied it permanently).
  - No entries were left unclear: every tool's managed-system effect was
    determinable from its implementation. The two genuinely consequential
    judgments (comment-tool direction; http_request remediation) were taken as
    explicit human decisions rather than guessed.
  - Consequence on the Record tier: T2 is now VACANT — no registered tool is
    record-tier. The tier constant and its policy/enforcement plumbing
    (`RecordPolicy`, `IsT2Allowed`) are retained for forward compatibility but
    are currently vestigial.
  - Enforcement-behavior changes surfaced (the demotions un-gate where the
    T1 bypass applies; conscious, consistent with the intent that Joe's model
    stays live in safe mode / incident regimes): the reclassified
    model-maintenance tools no longer consult the safety policy, no longer fire
    the after-action audit notification, and bypass the safe-mode block, the
    captain/session incident gates, and the DurableExecutor crash-resume
    idempotency wrapper. The last point means `register_source` (random-ID
    create) and `save_onboarding_fact` lose retry de-duplication — flagged for
    a conscious follow-up if idempotency is desired for Joe-own writes; not
    changed here.
  - Break-tests added/updated (`internal/safety/tier_test.go`): graph-mutation
    family asserted T1; unknown-tool default asserted most-conservative;
    comment tools asserted T3. Gate/executor/durability tests that used
    `graph_add_node` as their representative write were repointed to a real
    managed-system write (`write_file`).

- Implementation note (2026-06-07) — IMPLEMENTED: the boot-resolved,
  runtime-immutable write floor (points 1–5, 8) landed. This realizes the
  floor's lifecycle and immutability; the items in "What this deliberately does
  NOT do" that are out of THIS task's scope are enumerated as deferred below.
  Phase 1 re-verified the divergence coordinates against the live tree before
  any change; all matched as described.
  - Sentinel subsumed into a reason-carrying floor error. The former plain
    safe-mode sentinel (`var ErrSafeModeActive` in the now-deleted
    `internal/safety/safemode.go`) is replaced by `*safety.WriteFloorError`
    (`internal/safety/floor.go`) carrying a `Reason` of `observation` or
    `safe_mode`. errors.Is compatibility is preserved by a new floor-identity
    sentinel `safety.ErrWriteFloor` plus a `WriteFloorError.Is` method that
    matches it, so the FOUR pre-existing dependents (verified: the classifier in
    `internal/api/writefailure.go`; the executor floor test; and two assertions
    in `internal/api/writefailure_test.go`) keep matching via
    `errors.Is(err, ErrWriteFloor)`. The executor's single write-denial branch
    (`internal/tools/executor.go`) now returns this error.
  - Mutable boolean + public setter replaced by a resolve-once read-only value.
    The process-global `atomic.Bool` and `ActivateSafeMode/DeactivateSafeMode/
    IsSafeModeActive` are deleted. The floor is `safety.WriteFloor`, a value type
    exposing only `Up()/Reason()`. It is computed once at boot by the PURE
    `safety.ResolveWriteFloor(panicStatePresent, observationEnvSet)` and sealed
    into `core.Services.WriteFloor` — the single process-wide source, injected
    into both tool executors via `tools.WithWriteFloor` and read by the panic
    status handler. It is never re-derived from disk mid-process.
  - Live deactivate removed from the production binary (THE immutability
    guarantee). `internal/safety/unlock.go` (which called the in-process
    `DeactivateSafeMode()` + panic-flag `Reset()`) and `Reset()` itself are
    deleted. No production code transitions the floor up→down at runtime; the
    lowering operation does not exist in the running program. Enforced
    structurally by a repo-walk guard (`internal/safety/floor_guard_test.go`,
    `TestWriteFloor_NoRuntimeLoweringPath`) that fails if any of `safeModeActive
    / ActivateSafeMode / DeactivateSafeMode / IsSafeModeActive / ErrSafeModeActive
    / func Reset( / safety.Reset(` reappears in production code.
  - Observation env var added as a boot input with its own code/message distinct
    from safe mode. `JOE_MODE=observation` (`internal/env/keys.go`,
    read once in `cmd/joe/server.go` consistent with the other `JOE_*` boot env
    vars) raises the floor with reason `observation`. New write-failure code
    `errorCodeObservation` ("observation") with its own classifier branch and a
    CALM frontend message in `ui/src/hooks/useChat.ts` ("…intended read-only
    posture") that does NOT mention unlock or safe mode. Note: per this task's
    contract the floor is DOWN when neither input is set (writable, RBAC
    governs); D-0019's "observation is the day-one default" posture is a
    separate graduation decision, not implemented here.
  - Panic made sticky with `safe_mode`-wins precedence. `ResolveWriteFloor`
    resolves panic-present → `safe_mode` REGARDLESS of the observation env var;
    observation-only → `observation`; neither → down. All four combinations are
    pinned by `TestResolveWriteFloor_Precedence`
    (`internal/safety/floor_test.go`).
  - Unlock endpoint/CLI replaced by a local-file-only clear with restart-required
    semantics. The `POST /api/v1/unlock` endpoint, its `*client.Client.Unlock`
    HTTP-client method, and `safety.Unlock` are deleted. `joe unlock --reason`
    now calls `safety.AcknowledgePanic(joeDir, reason)` — clears the persisted
    `panic.state` file locally (recording the reason to the audit log), contacts
    no process, references no floor, and prints "panic state cleared — restart
    joe to resume writes." Recovery is now: clear panic state (this CLI) + set
    `JOE_MODE` to the intended posture + restart.
  - Break-tests (all passing): precedence over all four input combinations incl.
    both-set; `WriteFloorError` satisfies `errors.Is(ErrWriteFloor)` and carries
    the right reason incl. when wrapped; the no-runtime-lowering repo-walk guard;
    floor-not-re-derived-from-disk (an executor with an up floor still denies
    after `ClearPanicState` removes the file mid-process) plus a guard asserting
    `executor.go` references neither `ReadPanicState` nor the panic-state file;
    distinct presentation (a denied Mutate under observation → `observation`
    code/message, under safe mode → `safe_mode`, single enforcement branch),
    asserted in Go (`writefailure_test.go`) and TS (`writeFailureMessage.test.ts`).
  - Sticky-panic recovery rests on the panic→exit→restart mechanism, which Phase
    1 re-verified holds: all three trigger paths exit the process via
    `os.Exit(2)` (API handler in `internal/api/panic.go`; SIGUSR1 handler and the
    panic CLI's server-side trigger in `cmd/joe/server.go`) and never flip the
    floor in-process; boot re-reads the persisted panic state in
    `cmd/joe/server.go`. No path was found that lowers the floor in-process
    without exiting, so the guarantee holds (no prerequisite gap).
  - DEFERRED to following tasks (explicitly NOT in this change): routing the
    autonomous Core Agent graph-refresh path through the executor seam (the Core
    Agent's LLM-tool executor IS floor-governed here, but its direct graph-delta
    writes still bypass the seam); the posture read endpoint and the observation
    banner (only the write-failure code/message landed so a denied write surfaces
    correctly); and denial precedence between the floor and other denial sources
    (incident/captain gate, RBAC zone denial) — the classifier ordering is left
    unchanged. Cluster note: the local-file-only `joe unlock` clears only the
    local `panic.state`; in a clustered deployment where boot also reads the
    shared `cluster_panic_state` row, that row must be cleared separately — the
    former live cluster-clear rode on the now-deleted `safety.Unlock`.

- Implementation note (2026-06-08) — IMPLEMENTED: panic state consolidated to a
  single store (the DB row); the `panic.state` file deleted entirely. This
  closes the clustered-recovery divergence flagged in the prior note above —
  by REMOVING the second store, not by patching a cluster-clear into the CLI.
  Phase 1 re-verified every coordinate against the live tree before any change.
  - Single home. Panic state had TWO stores — a local `panic.state` file AND the
    shared `cluster_panic_state` DB row — and boot OR'd both. The file-only
    acknowledge (`safety.AcknowledgePanic`) cleared only the file, leaving the
    row set: a recovery hole. There is no clustered Joe today, so the split was
    unnecessary. Panic state now has ONE home, the DB row — the same single-store
    principle this entry applied to the write floor itself (one boot-resolved
    value, no drift between sources).
  - Panic entry writes ONLY the row. All three entry paths now persist via
    `safety.Trigger` → the boot-registered cluster store
    (`store.sqlPanicStore.SetPanicked`, extended to record source/reason/
    triggered_at into the existing migration-008 columns), then `os.Exit(2)`:
    the API handler (`internal/api/panic.go`), the SIGUSR1 handler
    (`cmd/joe/server.go`), and the panic CLI (an HTTP call to the API handler).
    The `safety.WritePanicState` file write was removed from every path.
  - Boot reads ONLY the row. `cmd/joe/server.go` resolves the floor from
    `clusterPanicStore.IsPanicked(ctx)` alone (file read deleted). Boot-order was
    VERIFIED SAFE in Phase 1: the panic read already sat AFTER store init/migrate
    and BEFORE the floor is sealed into `Services` and any tool executor is wired,
    so moving to DB-only required no reordering — nothing in boot needs panic
    state before the store is available. Floor resolution logic
    (`ResolveWriteFloor`: panic→`safe_mode` regardless of env var; observation
    env→`observation`; neither→down) is UNCHANGED — only its panic-state INPUT
    moved from file-or-DB to DB-only.
  - File deleted as a concept. `internal/safety/panic_state.go` (the
    `panic.state` writer `WritePanicState`, reader `ReadPanicState`, clearer
    `ClearPanicState`, the `panicStateFile` constant, the file-serialization
    `PanicState` struct, and `AcknowledgePanic`) and its test are deleted. The
    in-memory `safety.PanicInfo` struct + `ClusterPanicStore.PanicInfo` carry
    who/when/why for boot logging and the status endpoint, sourced from the row.
  - Acknowledge CLI rewritten to the DB row. `joe unlock` opens the store
    DIRECTLY (config + `store.New` + `Migrate`, the daemon's own pattern) and
    NEVER contacts or signals a running process. It is read-then-report-
    conditionally and idempotent: reads the row first, and only when a panic is
    present clears it ("panic state was present and has been cleared; restart to
    resume writes if no other read-only posture is set"); when no panic is
    present it clears nothing and says so ("Joe is not in a panicked state;
    nothing to clear") — neither message asserts the daemon's live state nor
    promises writes resume unconditionally (observation mode may independently
    hold the floor up). The two functional cases both exit 0; only a genuine
    store-access failure exits non-zero, so the report never lies. `--reason` is
    now optional (logged when given), not required.
  - Single-node contention handled. Panic entry exits the process, so during
    recovery the daemon is down and the CLI opening the SQLite store does not
    contend. In the not-panicked case (operator runs `joe unlock` on a healthy
    running Joe) the CLI only READS the row — no write — and SQLite WAL +
    `busy_timeout(5000)` make the short-lived second open non-disruptive.
  - Immutability guarantees intact. No runtime lowering path, no live setter,
    floor not re-derived mid-process — all preserved. Clearing the DB row affects
    only the NEXT boot; a running process's sealed floor is unchanged by the CLI.
  - Break-tests (all passing): `TestPanicState_SingleHomeNoFileConcept`
    (repo-walk guard that fails if any `panic.state` file writer/reader/clearer/
    constant reappears in production code, analogous to the no-lowering guard);
    `TestExecutor_Floor_NotReDerivedFromDBRow` (an executor with an up floor still
    denies after the panic row is cleared mid-process — the not-re-derived
    guarantee re-expressed against the DB row); the executor source-scan guard
    `TestWriteFloor_NotReDerivedFromDiskInExecutor` extended to also forbid the
    DB-row readers (`PanicStore`/`IsPanicked`/`PanicInfo`/`cluster_panic_state`)
    in `executor.go`; `TestRunUnlockCommand_PanicPresent` /
    `TestRunUnlockCommand_NoPanic` (conditional clear + exit-0 in both cases);
    store `PanicInfo` round-trip in `TestPanicStore_StateTransitions`. The
    pre-existing `TestWriteFloor_NoRuntimeLoweringPath` and
    `TestResolveWriteFloor_Precedence` still pass unchanged.

---

## D-0017 — The captaincy transfer handshake authenticated confirm/cancel but never bound the caller to the transfer; any authenticated principal could complete or abort a transfer it was not part of

- Date: 2026-06-07
- Decision: This is a defect entry, not a polish entry — an authorization
  bypass in the captaincy control plane, the same family as D-0012 (a
  control that authenticated the caller but never checked that the caller
  was entitled to the specific action). The §B captain transfer handshake
  (`internal/sessionmodel/captain.go`, exposed over HTTP at
  `internal/api/captain.go`) is a three-step state machine: `transfer/begin`
  opens an in-flight solicitation, then `transfer/confirm` completes the
  captaincy swap or `transfer/cancel` aborts it. EdgeAuth resolves the
  caller's principal into request context for all three. **`begin` already
  persisted both parties of the handshake** — the in-flight record lives on
  the active `session_captains` row (scoped to the active incident session
  via `detached_at IS NULL`), whose `principal` column is the
  soliciting/outgoing captain and whose `incoming_principal` column is the
  solicited incoming principal. But `confirm` and `cancel` **read the
  caller's principal only to write their audit row and never compared it to
  either party.** `CaptainService.ConfirmTransfer(ctx, sessionID)` and
  `CancelTransfer(ctx, sessionID)` took no caller principal at all:
  - **(a) The gap.** Any authenticated principal — including one who is
    neither the outgoing captain nor the solicited incoming principal —
    could `POST .../transfer/confirm` and finalize the swap to the recorded
    `incoming_principal`, or `POST .../transfer/cancel` and abort a transfer
    it had no part in. The caller was authenticated; it was never
    *authorized* against the handshake. (The two handlers also did not even
    reject the `rbac.Unknown` principal the way `attach`/`heartbeat`/`begin`
    do.) The binding data existed in the row the whole time — only the check
    was missing.
  - **(b) The binding model now enforced.** The caller principal is threaded
    into both service methods. **Confirm** is authorized to exactly one
    principal: the solicited `incoming_principal` named in the in-flight
    record. The outgoing captain cannot confirm in the incoming principal's
    place; a third principal cannot confirm at all → `ErrNotSolicitedIncoming`.
    **Cancel** is authorized to *either* party — the soliciting/outgoing
    captain (`principal`) or the solicited incoming (`incoming_principal`);
    any third principal → `ErrNotTransferParty`. A confirm/cancel with no
    matching in-flight solicitation is still rejected first with
    `ErrNoTransferInFlight` (the binding is checked against a real record, or
    there is nothing to act on). No new persistence was added — `begin`
    already recorded both parties; this fix is enforcement, not schema.
  - **(c) The authorization-failure convention used.** Both new sentinel
    errors map at the HTTP layer to `403` `"forbidden"`, matching the
    existing captain-control surface convention — the same shape
    `heartbeat` uses for `ErrCaptainPrincipalMismatch` (typed sentinel in
    `sessionmodel`, matched via `errors.Is`, rendered as a stable 403). No
    new error-code vocabulary was invented.
  - **(d) The break-tested invariant.** `TestCaptain_ConfirmBoundToSolicitedIncoming`
    and `TestCaptain_CancelBoundToHandshakeParties`
    (`internal/sessionmodel/captain_test.go`) assert the negative cases
    structurally: a non-party confirm/cancel returns the typed forbidden
    error *and* leaves the in-flight transfer untouched (captain unchanged,
    state still `transfer_requested`), and confirm by the outgoing captain in
    the incoming principal's place is rejected. `TestCaptainAPI_TransferConfirmCancelBindToParties`
    (`internal/api/captain_test.go`) pins the 403 wire mapping. All three
    were break-tested: neutralizing either binding in `captain.go` turns the
    rejections into successes and fails the suite (confirm-by-third-party
    returns nil/200 and swaps the captain; cancel-by-third-party resolves the
    transfer), confirming the tests fail if the principal binding is removed.
- Scope held: the transactionality of the captaincy swap itself
  (`completeTransfer` — two sequential repo writes, no shared tx; call site
  unchanged at `ConfirmTransfer` and the `begin` shortcut paths) and the
  resolve-path dangling-row behavior are SEPARATE findings and were not
  touched here. The non-transactional swap is noted, not fixed.
- Basis: `internal/sessionmodel/captain.go` (`ConfirmTransfer`/`CancelTransfer`
  now take and check `callerPrincipal`; `ErrNotSolicitedIncoming`/
  `ErrNotTransferParty` added) and `internal/api/captain.go` (handlers thread
  `string(principal)`, reject `rbac.Unknown`, map the typed errors to 403),
  verified by `go build ./...`, `go vet ./...`, `gofmt -s -w .`, and
  `go test ./...` green, plus the break-test described in (d). The pre-state
  is the captain/incident investigation under `docs/investigations/`, read
  against current code and confirmed.
- Supersedes: nothing — it closes a defect, it does not revise a prior
  decision. Same family as D-0012 (authenticate-without-authorize); builds on
  D-0010 (the shared §C captain gate) and D-0009 (captain-transition audit),
  neither of which is changed.
- Status: active. Authorization bypass closed; binding break-tested. The
  non-transactional `completeTransfer` swap and the resolve-path dangling-row
  behavior remain open as separate findings.

---

## D-0016 — Identity registry (the `principals` table) + full RBAC admin REST/UI surface; the `zone`/`admin` operator CLI removed, REST is the sole RBAC writer

- Date: 2026-06-05
- Provenance (stated honestly): this body of work ("Identity Stages 1–5") shipped
  as five commits on `main` without a decision of record. Two read-only audits
  (`IDENTITY_MODEL_INVESTIGATION.md`, `UI_WIRING_AUDIT.md` — since moved to the
  private launch archive) had enumerated the exact gaps these stages close; the
  stages were implemented directly against those findings (migration 021's header
  cites `IDENTITY_MODEL_INVESTIGATION.md` Step 1). This entry records and ratifies
  what the shipped code does after the fact; it does not change it.

- Decision: Joe gains an authoritative, mutable identity registry and a complete
  admin surface over HTTP + UI, and the direct-DB operator CLI is retired so that
  the audited REST API is the single writer of RBAC/identity state.

  **(a) Identity registry — the `principals` table (Stage 1, commit `ef3d634`).**
  Migration `021_principals` adds the mutable per-user record the append-only
  `audit_log` could not provide: `principal` (PK), `created_at`, `status`
  (`active`|`disabled`, CHECK-constrained), `disabled_at`/`disabled_by`
  (disable provenance), `display_name`, `last_seen_at`. The read/write surface is
  a separate `rbac.PrincipalRepository` (`internal/rbac/principals.go`:
  `UpsertPrincipal`/`GetPrincipal`/`ListPrincipals`/`SetPrincipalStatus`) so
  existing `rbac.Repository` implementers need not grow identity methods.
  `SetPrincipalStatus` writes its audit row in the same transaction as the status
  change. This closes the "there is no users/principals/identities table" finding.

  **(b) Provisioning + disabled-at-mint enforcement (Stage 2, commit `2bc6ecc`).**
  The OIDC callback upserts the registry row on every login
  (`internal/auth/handlers.go` `UpsertPrincipal`, refreshing only `display_name`/
  `last_seen_at`; status/created_at/provenance are owned by `SetPrincipalStatus`).
  Session mint consults `status` and refuses a `disabled` principal at mint time —
  a disable takes effect on the next request, not just for new grants.

  **(c) Full admin REST surface (Stage 3, commit `95a4b63`).** All routes
  admin-gated via `requireAdmin` and audited (`internal/api/admin.go`): zones
  `GET/POST/PATCH/DELETE`; source-zones `GET/POST/DELETE` (assign + unassign);
  policies `GET/POST/POST /revoke/DELETE {id}`; **admins `GET` (roster) / `POST`
  (promote) / `DELETE` (demote)** — previously reachable only via CLI/bootstrap;
  **principals `GET` (Users page) / `POST {p}/disable` / `POST {p}/enable`**. The
  structural guard `admin_gate_guard_test.go` fails the build if any `/admin/`
  route is registered without the gate.

  **(d) The `zone`/`admin` operator CLI is removed; REST is the sole RBAC writer
  (commit `205448a`, breaking).** `cmd/joe/zone.go` and `cmd/joe/admin.go` — which
  wrote SQLite directly and so bypassed the HTTP gate + audit — are deleted. The
  remaining subcommands are `panic`, `unlock`, `review`, `mcp`, `slack`, `skills`,
  `incident`. Rationale: a single audited writer for RBAC/identity state; the
  direct-DB CLI was the last gate-and-audit-bypassing writer, and (c) made it
  redundant. This supersedes the Phase C CLI zone-provisioning of D-0006 and the
  `CLI_REMOVAL_CHECK.md` finding that the operator CLI persisted.

  **(e) UI admin management surface (Stage 5, commit `07fb2a4`).** `ui/src/pages/
  UsersPage.tsx`, `PrincipalsTable`, `AdminsTable`, `AdminForm`; the
  `ui/src/api/security.ts` functions `updateZone`/`deleteZone`/`removeZone`/
  `fetchPrincipals`/`disablePrincipal`/`fetchAdmins` are real implementations, no
  longer stubs. Closes the `UI_WIRING_AUDIT.md` gaps (no user discovery, no admin
  roster, no promote/demote, no zone edit/delete, no source-zone unassign).

- Basis: commits `ef3d634`, `2bc6ecc`, `95a4b63`, `205448a`, `07fb2a4` (all
  2026-06-05 on `main`); migration `internal/store/migrations/021_principals.up.sql`;
  `internal/rbac/principals.go`; `internal/auth/handlers.go` +
  `internal/auth/principal_admin.go`; `internal/api/admin.go` +
  `admin_gate_guard_test.go`; the `ui/src/pages` / `ui/src/components/admin` /
  `ui/src/api/security.ts` surface. The pre-state is the two archived audits, which
  documented every gap above as open.
- Supersedes: the relevant findings of `IDENTITY_MODEL_INVESTIGATION.md` and
  `UI_WIRING_AUDIT.md` (now closed); the CLI zone-provisioning path of D-0006; and
  `CLI_REMOVAL_CHECK.md`'s "the operator CLI is still present" conclusion (true when
  written, no longer true). Builds on D-0011 (admin as a dynamic capability),
  D-0012 (the admin gate), and D-0013 (admin-mutation audit) — this entry extends
  that gated+audited surface to identity and makes REST its sole writer.
- Status: active. Stages 1–5 committed on `main`.

---

## D-0015 — Context-management architecture: FIFO pruning, ingestion truncation, conservative model-window registry, and a distinct context-overflow terminal status

- Date: 2026-06-05
- Provenance (stated honestly): this entry exists because a read-only
  verification (`CONTEXT_MANAGEMENT_VERIFICATION.md`, lines 7 and 197) found that
  the context-management workstream — described in-task as a "locked
  launch-blocking decision" — had **no decision of record**. The work was
  self-labelled "Stream G context pass" in code comments only
  (`internal/store/migrations/019_llm_context_budget.up.sql:1`,
  `internal/api/tasks.go:362`); a code comment is not a decision of record. The
  verification graded the engine PRESENT-and-tested but the build narrative
  incomplete. Closing that narrative gap is part of the engineer-who-tests-
  honestly posture, so the locked choices are recorded here after the fact.
  The decisions below describe what the shipped code does; this entry documents
  and ratifies it, it does not change it.

- Decision: the context-management engine binds every assembled LLM prompt to the
  active model's published context window via six locked choices.

  **(a) Pruning strategy — FIFO oldest-first drop.** History that exceeds the
  per-turn input budget is trimmed by dropping whole messages from the OLDEST
  end until the estimated total fits (`internal/agentloop/session.go`
  `pruneToTokenBudget`), NOT by summarization and NOT by a sliding-window count
  as the primary mechanism (a `MaxMessages=100` count cap remains a secondary
  backstop). Rationale: deterministic, cheap, and requires no external LLM
  summarization call on the hot path — the behaviour an operator reads in the
  audit/SSE trail is predictable and reconstructable, never a model's lossy
  paraphrase.

  **(b) Most-recent-user-message protection invariant.** Pruning never advances
  past the last GENUINE user message — a `Role:"user"` turn with no
  `ToolResultID`, distinguished from a tool result that also carries
  `Role:"user"` (`session.go` `lastUserMessageIndex`) — even when that one
  message alone exceeds the budget. Tool-call/tool-result pair integrity is
  likewise preserved (never a leading orphaned result). Rationale: preserves the
  user's most recent intent; combined with per-message truncation (c) this keeps
  the turn coherent rather than dropping the very message being answered.

  **(c) Per-message ingestion truncation fractions — 25% / 50% with a 2000-token
  floor.** Before a message enters history, an oversized tool result is capped at
  `toolResultBudgetFraction = 0.25` of the turn budget and an oversized incoming
  user message at `userMessageBudgetFraction = 0.50`, with a
  `minTruncationTokenFloor = 2000` floor (`internal/agentloop/constants.go`,
  `session.go` truncate*, `tokens.go` `TruncateContent`). The elided middle is
  replaced with an explicit, recoverable marker ("re-invoke the tool with a
  narrower query"). Rationale: tool results are typically large and recoverable
  (re-invoke narrower); user input is small but cannot be re-fetched, so it gets
  the larger share and is shortened, never rejected. The floor protects small
  budgets from collapsing to nothing.

  **(d) Conservative unknown-model default — 100,000-token window / 4096
  output.** Any `{provider, model}` pair absent from the compile-time
  capabilities registry resolves to `defaultContextWindowTokens = 100000` /
  `defaultMaxOutputTokens = 4096` (`internal/llmusage/capabilities.go`
  `LookupCapabilities`), never an optimistic guess and never an error. Rationale:
  safety over capability — under-using a model's context is recoverable;
  overrunning a window fails unpredictably. Trade-off acknowledged: an operator
  running a new model loses available context until a registry entry (with cited
  source + capture date, as the shipped Claude/Gemini rows carry) is added.

  **(e) Context overflow is a distinct TERMINAL STATUS, separate from the D-0014
  `error_code` write-failure vocabulary.** A provider rejection for an oversized
  prompt is classified at the LLM boundary into the typed sentinel
  `llm.ErrContextOverflow` and mapped to the terminal task status
  `"context_overflow"` (`internal/api/tasks.go` `taskStatus`, via `errors.Is`,
  never a string match), a sibling of `runaway_terminated` /
  `cost_limit_exceeded`. It is deliberately NOT folded into D-0014 Item 8's
  `error_code` codes (`incident_mode` / `zone_denial` / `internal_error`).
  Rationale (per `CONTEXT_MANAGEMENT_VERIFICATION.md` Cross-Cutting C): terminal
  `status` is for a turn that FAILED; `error_code` is for a non-fatal tool-write
  denial that rides on an otherwise-`completed` turn. Distinct lifecycles,
  distinct vocabularies — overflow fails the turn, so it owns the status channel.
  Detection-and-reporting only: no retry, no automatic budget adjustment.

  **(f) Audit policy — overflow audited, pruning and truncation not (per
  Cross-Cutting A).** Pruning writes NO audit row (high-volume, routine; the SSE
  `history_trimmed`/`messages_dropped` flags are the right surface).
  Per-message truncation writes NO audit row today (a deferred fast-follow:
  the user sees the marker; an auditor does not — tracked, not closed here).
  Context overflow IS audited — a `KindLLMLimitTriggered` /
  `llm_context_overflow` row for parity with the runaway ceiling's
  `llm_runaway_terminated` row — closed in this session (commit "feat(audit):
  write a context-overflow audit row…", `internal/api/tasks.go`
  `writeContextOverflowAudit`). The UI control gap (verification Item 9 /
  launch blocker) is likewise closed this session (commit "feat(ui):
  context-budget control…").

- Basis: `CONTEXT_MANAGEMENT_VERIFICATION.md` (read against `main` at HEAD;
  Items 1–9, Cross-Cutting A/B/C, and the prioritized gaps) verified against the
  code it cites: migration 019 (`internal/store/migrations/019_llm_context_budget.up.sql`),
  the `internal/agentloop` package (`session.go`, `tokens.go`, `constants.go`,
  `contextbudget.go`, `agent.go`), the `internal/llmusage` capabilities registry
  (`capabilities.go`), and the typed sentinel + status mapping
  (`internal/llm/errors.go`, `internal/api/tasks.go`). The per-turn context fit
  is applied before request assembly and is independent of the Stream G
  session-token ceiling, which is checked post-`Chat` from real provider usage
  (verification Item 6); the two map to distinct terminal statuses and distinct
  audit actions.
- Supersedes: nothing — first context-management decision. Relates to D-0014
  (the context budget is exposed through the same admin-gated, audited
  `/api/v1/llm/settings/*` surface whose structural guards D-0014 added; this
  entry's `error_code`-vs-`status` boundary is the deliberate counterpart to
  D-0014 Item 8) and D-0003 (the `context_overflow` status rides the SSE final
  event as an additive, `omitempty`-compatible field, consistent with the Phase 2
  streaming protocol).
- Status: active. Engine PRESENT and tested; the UI control and the
  context-overflow audit row are closed this session. The two
  defer-with-documentation fast-follows the verification named — per-message
  truncation auditing (Cross-Cutting A(b)) and per-turn budget-consumption
  telemetry ("used X of Y", verification gap 4) — remain open and are NOT closed
  by this entry.

---

## D-0014 — Close the Stream G structural-guard gap and the operator-surface launch blockers (incident CLI, zero-zone dead-end, incident banner, write-failure feedback)

- Date: 2026-06-04
- Decision: Close the remaining launch blockers found by two read-only
  verifications — `STREAM_G_VERIFICATION.md` (the LLM instrumentation / admin
  settings subsystem) and `OPERATOR_SURFACE_VERIFICATION.md` (incident mode +
  user management). Eight discrete changes, each its own commit, build-green
  and tests-passing after each. The work is two clusters:

  **(a) Stream G structural-guard gap.** `STREAM_G_VERIFICATION.md` (Item 7 /
  cross-cutting C) found the D-0013 admin guards parse only `admin.go` /
  `adminHandler`, so the LLM admin mutators on `llmSettingsHandler` /
  `llmUsageHandler` (`internal/api/llmsettings.go`, `llmusageapi.go`),
  registered under `/api/v1/llm/`, were covered by NO AST invariant — the
  exact regression class D-0012/D-0013 closed for the RBAC surface remained
  open here. Closed with two structural guards
  (`internal/api/llm_admin_guard_test.go`) mirroring
  `admin_gate_guard_test.go` / `admin_audit_guard_test.go`:
  - `TestLLMAdminRoutes_MutatorsRequireAdminGate` — every mutating
    (POST/PUT/DELETE/PATCH) route, plus the per-principal usage GET
    (admin-only by design), must call `requireAdmin`. GET reads stay open per
    Stream G design.
  - `TestLLMAdminRoutes_MutatorsAudit` — every mutating route must route
    through `services.LLMSettings` (the MutationService), which is the
    ACTUAL Stream G audit writer: it persists the change AND writes the
    `KindLLMSettingsMutation` audit row in one transaction
    (`internal/llmsettings/service.go`), NOT `recordAdminAudit`. The guard
    asserts this path structurally rather than asserting a `recordAdminAudit`
    call that does not exist on this surface.
  Verb+pattern are parsed from the `fmt.Sprintf` registration literal. The
  gate invariant was break-tested (removing the gate from `handleSetCostLimit`
  turns the guard red naming that handler; restoring returns green) — an
  invariant never seen red proves nothing. Two adjacent Stream G test gaps were
  also closed: a route-level `RequireAdmin` test for `/llm-settings`
  (`ui/src/auth/RequireAdmin.test.tsx`; the section-level UsageTab test was the
  only prior coverage), and a skip-staged regression net for ChatStream/Embed
  usage recording (`internal/llmusage/recorder_test.go`) that activates when
  either provider stub
  (`internal/llm/claude/claude.go:141-148`,
  `internal/llm/gemini/gemini.go:202-209`) is implemented.

  **(b) Operator-surface launch blockers.** `OPERATOR_SURFACE_VERIFICATION.md`
  found the incident-mode enforcement is solid on both agentic paths (D-0010)
  but had NO human-facing trigger or feedback surface, and a new zero-zone
  user dead-ended silently. Closed:
  - **Incident CLI** (`cmd/joe/incident.go`, dispatched at
    `cmd/joe/main.go`): `joe incident status|declare|resolve` over the HTTP API
    (`GET/POST /api/v1/regime[/declare|/resolve]`), mirroring
    `cmd/joe/zone.go` / `admin.go`. `list` is an intentional stub — no
    `/regime/history` endpoint exists (verified ABSENT; durable history lives
    in the append-only `audit_log`) — printing audit-log guidance and exiting
    non-zero so the v1 limitation is explicit, not implied success.
  - **`/me` extended with the caller's reachable zones**
    (`internal/api/currentuser.go`): admin → every zone; non-admin → their
    granted zones; zero-zone → non-nil `[]`. This is the data dependency for
    the next two items.
  - **Zero-zone empty state** (`ui/src/components/chat/ZeroZoneEmptyState.tsx`):
    ChatPage renders "Access pending" in place of the doomed chat input when
    `rbacEnabled && !isAdmin && zones.length === 0`.
  - **Active-incident banner** (`ui/src/components/layout/IncidentBanner.tsx`):
    app-shell-wide, polls `/api/v1/regime` (30s), shows a top-of-page alert in
    incident mode. An app-shell concern — it never enters the chat-history
    snapshot.
  - **Differentiated write-failure feedback** (Item 8): a denied write surfaces
    a typed code — `incident_mode` (captain gate) vs `zone_denial` (RBAC) vs
    `internal_error` — classified by `classifyWriteFailure`
    (`internal/api/writefailure.go`) and injected into the loop via the new
    `agentloop.WithToolErrorClassifier`, so `agentloop` stays unaware of the
    gate/RBAC error types. Because a denied write does NOT terminate the
    agentic loop (the LLM receives the tool error and the turn still
    completes), the code rides on the turn-level `error_code` of the final
    event (first per-tool denial seen); the chat UI dispatches it to a specific
    message via `writeFailureMessage`.

- Honest scope notes:
  - The Item 8 backend classifier is exercised by unit tests over each typed
    error; the end-to-end chat path surfaces the code through the final
    event's turn-level `error_code` rather than a pre-stream HTTP 403, because
    in this architecture a tool-level denial is non-terminal and the original
    typed error is stringified onto the wire. The pre-stream 403 code path
    (`streamTask` onError) is wired too, for any future gate that refuses the
    whole task before streaming.
  - The Stream G GET reads remain intentionally un-gated (policy knobs, not
    credentials); only the per-principal usage GET is admin-gated, and the
    guard encodes that distinction explicitly.
- Basis: `STREAM_G_VERIFICATION.md` (Items 3, 7; cross-cutting A/C) and
  `OPERATOR_SURFACE_VERIFICATION.md` (items 5, 6, 8, 9, 11; prioritized
  launch blockers), both read against `main` at HEAD `2b16665`; the D-0013
  structural-guard pattern and the D-0010 captain-gate single-impl invariant.
- Supersedes: nothing. Extends D-0013 (structural gate+audit guards) to the
  Stream G admin surface and adds the operator-facing incident + RBAC surfaces
  the verifications found missing. The pre-existing invariants (D-0012 admin
  gate, D-0013 admin audit, D-0010 captaingate single-impl,
  `regime_invariant_test.go`) are unchanged and still pass alongside the new
  Item 1 guards.
- Status: active.

---

## D-0013 — The RBAC admin surface was gated (D-0012) but wrote zero audit rows; extend the audit vocabulary to cover authorization-config mutations

- Date: 2026-06-04
- Decision: D-0012 closed the GATE gap on the RBAC admin HTTP API
  (`internal/api/admin.go`) but explicitly left a second gap open and tracked
  it: the surface wrote **no** audit rows at all. The most
  authorization-critical mutations in the system — mint a zone, grant/revoke a
  policy, assign a source to a zone — were unrecorded, and gate denials left
  no trail. D-0013 closes that gap by extending Phase F's (D-0009) audit
  machinery to this surface, with Phase F's failure posture preserved
  exactly. Specifics:
  - **(a) The gap, precisely.** Phase F (D-0009) modelled the guarded
    accessor's DECISION point — every allow/deny the accessor makes against an
    infra adapter or the graph store, recorded as a `kind=infra_access` row.
    It never modelled mutations of the authorization CONFIGURATION the
    accessor reads. A zone's `allowed_actions`, a principal→zone policy, a
    source→zone assignment: changing these changes what every future accessor
    decision will permit, but the change itself produced no row. That is a
    different decision shape than the one Phase F's scope (the accessor's
    chokepoint) covered, which is why the vocabulary never grew to include it.
  - **(b) The fix — action vocabulary.** Nine action verbs were added to the
    existing audit vocabulary file (`internal/audit/audit.go`), NOT a parallel
    taxonomy: `zone.create`, `zone.read`, `policy.grant`, `policy.revoke`,
    `policy.read`, `source_zone.assign`, `source_zone.read`, plus
    `admin.grant` / `admin.revoke` (declared for the CLI-only admin-promotion
    path per `ADMIN_SURFACE_AUDIT.md`, so a future HTTP endpoint reuses them
    rather than inventing new strings), and `admin.access_denied` for gate
    refusals. All carry a new `kind=admin_access` — the admin-surface parallel
    of `infra_access`: one kind for the whole surface, discriminated by action
    + decision.
  - **(c) The fix — typed details.** Each row's `context` column uses the
    `{target, before, after}` shape Stream G locked for `llm_settings_mutation`
    rows (`internal/llmsettings.AuditCtxTarget`/`Before`/`After`, written by
    `MutationService.runMutation`). That shape is now a named type,
    `audit.Details`, documented inline; admin and settings mutations share the
    audit table with this typed details column, per the locked decision. For a
    create/grant the row carries the after-state; for a revoke the
    before-state; for a read that leaks structure, the requested resource.
  - **(d) The fix — failure posture, unchanged from Phase F §4.** The eight
    handlers write their row through `recordAdminAudit`, which routes the
    write's outcome through the same `audit.FailurePosture` helper every Phase
    F caller uses. Mutating actions fail CLOSED: the audit row is written
    BEFORE the repository mutation, and a failed write aborts before any state
    change (no row ⇒ no mutation), the same audit-before-act ordering the
    accessor uses. The three `.read` actions are read-class and fail OPEN: the
    read proceeds even if the row cannot be written, logged loudly. The
    read/mutate split lives in `isFailOpen`, extended to admit the three admin
    read verbs — the §4 invariant itself is untouched. Denials write a
    `decision=deny` row from `requireAdmin` (the only behavioural change to the
    gate, scoped to the optional denial-audit D-0012's `requireAdmin` left
    room for); the denial is enforced regardless of whether the row lands
    (denying is the fail-closed-safe direction), matching
    `captaingate.Wrapper.writeRefusalAudit`'s posture.
  - **(e) Schema touch — one migration, required not optional.** Admin-RBAC
    events had no semantically-correct home among the six kinds the
    migration-018 `audit_log.kind` CHECK admitted. An INSERT with an
    unadmitted kind fails the CHECK, and under (d)'s fail-closed posture that
    would break every admin mutation outright — so the kind had to be added to
    the CHECK to record admin actions at all. Migration 020
    (`020_admin_audit.up.sql`/`.down.sql`) widens the CHECK to add
    `admin_access`, following the byte-for-byte table-rebuild sequence
    migrations 017 and 018 established (SQLite cannot alter a CHECK in place);
    all other columns, defaults, the decision CHECK, the indexes, and the two
    append-only triggers are preserved verbatim. This is the only schema
    change; the typed details column (`context`) was already present from
    Stream G. The choice between this and reusing an existing (wrong-named)
    kind was made explicitly: a `kind` named for one surface holding another
    surface's rows would corrupt kind-based forensic queries, so a minimal
    widening was preferred — the same reasoning, inverted, that led
    `captaingate` to REUSE `captain_transition` for gate refusals (there the
    existing kind was semantically correct; here none was).
  - **(f) The structural invariant.** `TestAdminRoutes_AllAuditOnAllow`
    (`internal/api/admin_audit_guard_test.go`) is the sibling of D-0012's
    `TestAdminRoutes_AllRequireAdminGate`, in the same AST-guard style as
    `captaingate.TestPhaseG_SingleSharedCaptainGateImplementation`. It parses
    `admin.go` and fails the build if any handler registered under
    `/api/v1/admin/` does not call `recordAdminAudit` in its body. Together
    the two guards pin both halves of the admin-surface contract: every admin
    endpoint admin-gates AND leaves an audit trail. A future
    `POST /api/v1/admin/admins` added without an audit write fails the build
    naming the unaudited handler. The guard was break-tested: removing the
    audit writer from `createPolicy` makes it fail with `admin handler
    "createPolicy" is registered under /api/v1/admin/ but its body never calls
    recordAdminAudit`, and the companion regression tests simultaneously go red
    (`no policy.grant audit row written`; a grant returns 201 even with a
    failing audit injected — the live gap); restoring the writer returns all
    to green.
  - **Regression tests** (`internal/api/admin_audit_test.go`) reuse the
    Stream G fixture: each new action's row is asserted written with the
    correct action, principal, decision, and target details; each mutating
    action is asserted to fail CLOSED under an injected audit-write failure
    (the mutation does not commit); reads fail OPEN (the read still 200s); and
    a non-admin attempt records an `admin.access_denied` row naming the
    attempting principal and the attempted endpoint.
- Basis: D-0012's "Honest scope note" (this entry closes the gap it tracked);
  `internal/llmsettings/service.go` (the `{target, before, after}` shape and
  the InsertTx atomic-write pattern reused here); `internal/captaingate/
  captaingate.go::writeRefusalAudit` (the fail-closed deny-audit pattern
  matched); migrations 017/018 (the kind-CHECK-widening rebuild pattern
  followed by 020); `internal/audit/audit.go` §4 `FailurePosture`/`isFailOpen`
  (the read/mutate split extended, not altered). Verified by `go test ./...`
  green, plus the break-test described in (f). NOTE: Stream G's audit-table
  extension (the `llm_settings_mutation` kind, migration 017) has no standalone
  DECISIONS entry — it is documented in the migration and `audit.go` code
  comments; D-0013 is the first DECISIONS entry to record the shared-table /
  typed-details arrangement explicitly.
- Supersedes: nothing — it closes the gap D-0012 tracked as deferred. D-0012
  closed the GATE gap (privilege escalation); D-0013 closes the AUDIT gap.
  Together they make the admin RBAC surface match what the README claims about
  RBAC and audit. D-0011's admin capability and `IsAdmin` semantics are
  untouched. Touched files: `internal/audit/audit.go` (new kind, action verbs,
  `Details` type, `isFailOpen` extension), `internal/api/admin.go` (the
  `recordAdminAudit` writer + eight handler call sites),
  `internal/api/admingate.go` (`recordAdminDenial` + three gate call sites),
  `internal/store/migrations/020_admin_audit.{up,down}.sql`, three new/updated
  test files (`admin_audit_guard_test.go`, `admin_audit_test.go`,
  `migrations_020_test.go`), and the step-count bumps in the 017/018/019
  migration round-trip tests (each now steps down one further past the new
  head — a pre-existing brittleness in those tests, not a behaviour change).
- Status: active. Admin-audit gap closed; structural invariant in place and
  break-tested; both admin-surface invariants (gate + audit) now enforced
  together. Known follow-up: the README's "All admin endpoints require Bearer
  auth" (RBAC section) is stale post-D-0012 (they require admin capability) and
  silent about D-0013's audit trail — flagged for the later combined README
  rewrite pass, not edited here.

---

## D-0012 — We found a privilege escalation in our own RBAC admin API: the admin gate was never applied to it

- Date: 2026-06-03
- Decision: This is not a polish entry. The post-Stream-K admin-surface
  read (`ADMIN_SURFACE_AUDIT.md`, Launch Blocker 1) found that the RBAC
  admin HTTP API in `internal/api/admin.go` — `POST /api/v1/admin/zones`,
  `POST /api/v1/admin/policies`, `POST /api/v1/admin/source-zones`,
  `DELETE /api/v1/admin/policies/{id}`, and the four `GET` read endpoints —
  was never admin-gated. Every handler required only bearer auth. **Any
  authenticated principal, including a brand-new zero-zone OIDC user who
  resolves to the read-only `unassigned` zone, could `POST` itself a policy
  into any zone — or mint a new zone with `allowed_actions`
  `["read","query","mutate","delete"]` and grant itself that — fully
  escalating its own access.** Because the same path can grant
  `regime-control`, it also reopened `declare_incident`/`resolve_incident`
  to anyone (Blocker 2). The fix applies the EXISTING `requireAdmin` gate
  (`internal/api/admingate.go`) to every admin handler — the same one-line
  `if _, gated := h.server.requireAdmin(w, r); gated { return }` inlining
  Stream G uses on the LLM settings/usage endpoints. No new gate, no
  changed gate semantics, no audit-writer change. Specifics:
  - **(a) The gap.** `registerAdminRoutes` wired eight handlers under
    `/api/v1/admin/`; none called `requireAdmin`. The UI's `RequireAdmin`
    React component (`ui/src/auth/RequireAdmin.tsx`) gated the admin *pages*
    client-side off the `/me` `is_admin` flag, which hides the nav but does
    nothing for a direct `curl`. The server-side check that should have sat
    behind it did not exist. The reads were gated too (not just the writes):
    `GET /admin/policies` and `GET /admin/zones` expose the full
    authorization map — who holds which zone and what each zone permits — so
    leaving them open leaks the access-control topology to any caller.
  - **(b) Why it existed — an honest account.** `requireAdmin` was
    introduced by Stream G (phase G5) specifically to gate the LLM
    instrumentation endpoints (settings writes, the per-principal usage
    breakdown). It was applied there and only there. The RBAC admin
    endpoints predate that gate (Phase 9.3 / migration 006) and shipped when
    the *only* deployed posture was "bearer key == trusted operator." When
    Phase C/H turned principals into a spectrum — OIDC users, `svc:` keys,
    zero-zone newcomers — the admin API's "bearer auth is enough" assumption
    silently became false, and the new gate was never retroactively swept
    back across the older surface. The gate's own doc comment even scoped
    itself to "the Stream G phase G5 LLM-instrumentation HTTP endpoints" —
    the narrow framing is exactly how the older surface got missed. No test
    asserted the property for `admin.go`, so nothing failed when the gate
    skipped it. This is the classic shape of a self-inflicted gap: a control
    added for one stream, correct in that stream, never generalized to the
    sibling surface that needed it just as much.
  - **(c) The structural invariant added to prevent recurrence.**
    `TestAdminRoutes_AllRequireAdminGate`
    (`internal/api/admin_gate_guard_test.go`) is an AST guard in the style
    of the identity refactor's single-implementation guards
    (`captaingate.TestPhaseG_SingleSharedCaptainGateImplementation`,
    `sessiongate`'s import guard). It parses `admin.go`, extracts the set of
    `adminHandler` methods registered by `registerAdminRoutes` (the handler
    arg of each `mux.HandleFunc`) and the set whose body calls
    `requireAdmin`, and fails the build if the former is not a subset of the
    latter. The regression tests pin the endpoints that exist today; this
    invariant pins the property for endpoints that do not exist yet — a
    future `POST /api/v1/admin/admins` added without the gate fails the
    build naming the ungated handler, rather than silently re-opening the
    escalation. The guard was break-tested: removing the gate from
    `createPolicy` makes it fail with `admin handler "createPolicy" is
    registered under /api/v1/admin/ but its body never calls requireAdmin`,
    and the companion regression test simultaneously shows the live
    escalation (a non-admin `POST` returns 201 with a `regime-control`
    policy granted to itself); restoring the gate returns both to green.
  - **Regression tests** (`internal/api/admin_gate_test.go`) reuse the
    Stream G fixture (`llmadminFixture`): a non-admin `POST` to
    `/admin/policies` and `/admin/zones` each returns 403 with the resource
    not created; an admin gets 201 with the resource created; a non-admin
    cannot self-grant `regime-control` (Blocker 2 closed); and the
    auth-disabled (`rbacEnabled=false`) local/dev posture still permits, so
    the fix does not block keyless deployments.
- Honest scope note — audit of admin mutations is a SEPARATE, still-open
  gap. The audit flagged (and the fix confirmed) that the RBAC admin
  endpoints write **no** audit rows at all: `admin.go` imports no audit
  package, `requireAdmin` does not audit denials, and there is no
  `audit.Kind`/`audit.Action` for zone/policy/source-zone mutations. Phase
  F's machinery covers infra-access decisions, regime/captain transitions,
  LLM-settings mutations, and auth-login — not this surface, and not gate
  denials in general (LLM-settings denials are not audited either). This fix
  is deliberately scoped to the privilege escalation and does NOT add an
  audit writer (that would change Phase F's mechanics). The
  zone/policy-mutation audit gap is recorded here as known and deferred; it
  is not closed by D-0012.
- Basis: `ADMIN_SURFACE_AUDIT.md` (Investigation 1 §2/§5, Prioritized Gaps
  Blockers 1–2), read against current code; `internal/api/admingate.go`
  (the existing gate, unchanged); the Stream G application pattern in
  `internal/api/llmsettings.go` / `internal/api/llmusageapi.go`. Verified by
  `go test ./internal/api/` green, plus the break-test described in (c).
- Supersedes: nothing — it closes a defect, it does not revise a prior
  decision. D-0011's admin capability and its `IsAdmin` semantics are
  untouched and unchanged; this entry only applies the gate that consults
  them to a surface that was missing it. Touched files:
  `internal/api/admin.go` (gate applied to all eight handlers; `adminHandler`
  gains a `*Server` back-reference), `internal/api/admin_internal_test.go`
  (repo-error fixture now constructs a permissive server), and two new test
  files. The audit table, its writers, and `requireAdmin`'s implementation
  are unchanged.
- Status: active. Privilege escalation closed; structural invariant in
  place. The admin-mutation audit gap remains open and is tracked above.

---

## D-0011 — Identity Phase H: admin as a dynamic capability evaluated at decision time; snapshot grants removed

- Date: 2026-05-30
- Decision: Phase H (joe-identity-design.md §2.9, §5 Invariant 2;
  joe-identity-phase-plan.md Phase H) closes the day-100 correctness gap
  D-0006 left open: admin authority is no longer the snapshot of grants
  captured at bootstrap (which silently failed to cover any zone created
  AFTER the configured admin's first login). It is now a DYNAMIC capability
  evaluated by the policy engine at decision time. The result: a zone
  created months after admin designation is covered automatically, with no
  re-snapshot, no operator action, and no silent gap. Specifics:
  - **Admin status is a principal-scoped row in a new table.** Migration
    `016_admin_principals` (one CREATE TABLE + one index) introduces
    `admin_principals(principal TEXT PK, granted_at TEXT NOT NULL,
    granted_by TEXT NOT NULL DEFAULT '', reason TEXT NOT NULL DEFAULT '')`.
    The schema matches the conventions of the existing tables (TEXT
    RFC3339 timestamps; TEXT NOT NULL DEFAULT '' for free-text columns; no
    sentinel rows). It is the SINGLE source of truth for admin status — the
    decision function reads it, the bootstrap and CLI write it, and nothing
    else stores admin information. A principal is admin iff a row exists
    for them in this table; there is no boolean column on `rbac_policies`,
    no flag elsewhere, no derivation.
  - **The decision function treats admin as an allow short-circuit, NOT a
    bypass of the zone classification.** `rbac.PolicyEngine.Decide`
    (`internal/rbac/policy.go`) gains exactly one new branch: after the
    zone-allows-action gate, before the per-principal-grant loop, the
    engine calls `IsAdmin(ctx, principal)` for each member of the
    PrincipalSet and returns `Decision{Allowed: true, Reason:
    ReasonAdminCapability}` on the first hit. The check is bounded by the
    zone's `allowed_actions` list — Phase H deliberately keeps that check
    UPSTREAM of the admin short-circuit (req 2). Same rule on the
    sourceless path: `HasZoneAccess` mirrors the structure with a boolean
    return.
  - **Why admin bypasses only the grant requirement and not the zone's
    allowed_actions (the stricter sensible interpretation, req 2).** The
    zone's `allowed_actions` is a property of the zone's classification —
    "this is a read-only zone", "this is a delete-permitted zone" — not a
    per-principal limit. An admin who could delete in `prod-readonly`
    would change what the zone is for; the zone classification would
    cease to communicate "no destructive actions". The principal-grant
    requirement, by contrast, IS per-principal: it gates who reaches the
    zone at all. Admin overriding the per-principal gate matches the
    operator's mental model ("admin can do anything anywhere"); admin
    overriding the zone classification would require introducing a second
    notion of "admin in zone X" that breaks the zone's primary purpose.
    The interpretation also matches what the Phase C snapshot used to do
    in aggregate: it wrote grant rows on every zone, but the zone's
    `allowed_actions` still bound what admin could do on each. Phase H
    preserves that ceiling.
  - **Reason vocabulary extended with one tag, audit row carries it.** A
    new constant `rbac.ReasonAdminCapability` ("admin_capability") joins
    the Phase F reason vocabulary (`policy_allow`, `no_grant`,
    `action_not_in_zone`, `zone_not_found`). The accessor's `permit`
    chokepoint records `Decision.Reason` into the audit row's `reason`
    column unchanged — no new migration, no new column, no new
    `audit.Kind`. An operator querying
    `audit_log WHERE reason = 'admin_capability'` sees only the decisions
    admin would not have reached through a per-zone grant; queries for
    `policy_allow` continue to surface ordinary zone-grant allows.
    `TestPhaseH_AdminAllowAuditReasonDistinguishedFromZoneGrant`
    (internal/access/audit_test.go) issues both an admin allow and a
    zone-grant allow against the same source and asserts the two audit
    rows differ in their `reason` field — the audit-trail
    distinguishability requirement (Phase H req 5).
  - **Bootstrap path: snapshot logic removed; same trigger, new behaviour.**
    `auth.Provisioner.GrantAdmin` (`internal/auth/provision.go`) no longer
    iterates `ListZones` and writes `CreatePolicy` per zone. It calls
    `repo.AddAdmin(ctx, rbac.Admin{Principal, GrantedBy:
    "bootstrap_admin_email", Reason: "auth.admin_email match"})` and then
    `repo.DeletePoliciesForPrincipal(ctx, principal)`. The OIDC callback
    (`internal/auth/handlers.go:179-185`) still calls `GrantAdmin` for
    every login matching `auth.admin_email`; the call is still idempotent;
    the failure-loud policy is preserved (admin bootstrap failure aborts
    the login with HTTP 500). What changed: the row goes to
    `admin_principals` instead of N rows going to `rbac_policies`, and any
    pre-existing `rbac_policies` rows for the admin are cleaned up so
    "single source of truth" holds structurally. The bootstrap trigger
    (config-designated admin_email becomes admin on first login) is
    preserved verbatim — the design's §2.9 contract holds.
  - **Migration of the existing admin: no SQL data migration, runtime
    cleanup at first matching login.** The pre-Phase-H snapshot lived in
    `rbac_policies`. Migration 016 creates `admin_principals` empty; it
    does not back-port any rows because the migration cannot know which
    principal is the configured admin (`auth.admin_email` lives in YAML
    config, not in the database). The cleanup runs at runtime instead: the
    first matching admin_email login under Phase H code inserts the
    `admin_principals` row AND deletes any leftover `rbac_policies` rows
    for that principal in the same call. The prompt allows a clean
    migration; this is the cleanest one given the configured-in-YAML
    constraint. The `TestPhaseH_NoLeftoverSnapshotGrants` test seeds
    snapshot grants explicitly and asserts they are gone after
    `GrantAdmin`, proving the cleanup path. The unreleased-project
    assumption holds either way: no production DB has snapshot grants to
    migrate; the cleanup is a structural defence, not a back-port.
  - **CLI surface: `joe admin {grant,revoke,list}`, parallel to
    `joe zone`.** New subcommand at `cmd/joe/admin.go`. `joe admin grant
    --principal <user:|svc:> [--reason ...]` upserts the row (idempotent;
    re-issue with a new `--reason` updates the rationale) AND deletes any
    `rbac_policies` rows for the principal (the same cleanup the
    bootstrap path performs). `joe admin revoke --principal ...` deletes
    the row. `joe admin list` prints the rows in a 3-column table
    (principal, granted_by, granted_at). The command opens the SQLite
    database directly (operator-on-host), mirroring `joe zone`'s Phase C
    surface (D-0006). The `runDeps.openRBACRepo` factory's return type
    widened from `zoneRepo` to a new `rbacRepo` interface that adds
    `IsAdmin`/`ListAdmins`/`AddAdmin`/`RemoveAdmin`; `*rbac.SQLRepository`
    satisfies both. The configured `auth.admin_email` path keeps working
    regardless of the CLI — both routes converge on the same `AddAdmin`
    repository call. Justification for including grant/revoke (the
    prompt left this as a scope choice): consistent with `joe zone`'s
    operator-on-host model and the Phase C precedent; lets an operator
    delegate additional admins without editing YAML or restarting
    joe-core. Without it, the only admin path is the configured email,
    which fails the "day-100 operator experience" lens that motivated
    Phase H in the first place — adding a second admin must not require
    a config change + restart cycle.
  - **Revoke caveat: `auth.admin_email` re-grants on next matching login.**
    `joe admin revoke` removes the `admin_principals` row, but the
    bootstrap path is idempotent and will re-insert it the next time a
    matching email logs in. To make a revocation stick, the operator must
    also clear `auth.admin_email` from config. This is the right default:
    the configured admin_email IS the bootstrap path, and silently
    pre-empting it via a CLI revoke would create a subtle drift between
    config and behaviour. Documented in the CLI's command surface and in
    the `runAdminRevoke` doc comment.
  - **Non-admin authorization outcomes: unchanged from post-Phase-G.** The
    Phase H short-circuit only fires when at least one principal in the
    set holds an `admin_principals` row. For every other principal it is
    a no-op — `Decide`/`HasZoneAccess` flow through the existing
    per-principal-grant loop, returning the same allow/deny outcomes the
    Phase G regression tests proved. `TestPhaseH_NonAdminOutcomesUnchanged`
    asserts this regression — granted allow, ungranted deny, in-zone
    action denied — each with the same reason vocabulary
    (`policy_allow`/`no_grant`/`action_not_in_zone`) it had pre-Phase-H.
  - **Failure posture on `IsAdmin` errors: warn + fall through, not deny.**
    An `IsAdmin` repository error for one principal logs WARN and
    continues to the next principal in the set, then falls through to the
    per-zone grant loop. The principal's grant lookup still runs; the
    overall decision is whatever the grant path produces. Rationale:
    `IsAdmin` is a one-table single-row read; a transient failure should
    not collapse to deny on a principal who legitimately holds an
    rbac_policies grant. The behaviour mirrors how `ListPoliciesForPrincipal`
    handles its own failures (`continue`, not return). Verified by
    `TestPhaseH_AdminIsAdminErrorFallsBackToGrant`.
- Deviations from the Phase H prompt, with reasons:
  1. **No SQL data migration; cleanup runs at the first matching admin
     login (and at every CLI promotion).** The configured admin_email is
     in YAML config, not in the database, so an SQL migration cannot
     identify the admin without operator input. The cleanup runs in
     Go at the existing `GrantAdmin` site instead, which is invoked on
     every matching login (idempotent). The behaviour the prompt wants
     ("remove leftover snapshot grants") is realised on first contact
     with the new code; the `TestPhaseH_NoLeftoverSnapshotGrants` test
     seeds pre-existing snapshot rows to prove the cleanup.
  2. **CLI grant/revoke is in scope.** The prompt left this as a scope
     decision; rejected "inspect only" as inconsistent with the
     `joe zone` precedent (Phase C's surface includes grant/revoke). The
     configured admin_email path is preserved verbatim; the CLI is the
     operator-on-host route for any ADDITIONAL admins. Without it, the
     deployment can have at most one admin without a config-restart
     cycle, which fails the "day-100 operator experience" lens that
     motivated Phase H.
  3. **`runDeps.openRBACRepo` return type widened from `zoneRepo` to
     `rbacRepo`.** `rbacRepo` is the union of methods both `joe zone`
     and `joe admin` need; `*rbac.SQLRepository` satisfies it
     trivially. Keeping two parallel factories (one per command) would
     have meant duplicating the DB-open ceremony.
  4. **No new `audit.Kind` and no new column in `audit_log`.** The Phase F
     contract is "one reason field captures the basis"; adding a column
     for admin specifically would balloon the schema for one capability.
     The `ReasonAdminCapability` tag is in the existing `reason` column;
     audit queries discriminate on that, not on `kind`. Migration 015's
     CHECK constraint on `kind` is unchanged.
  5. **`PrincipalSet` stays size 1.** Per the explicit scope fence; no
     group-member additions, no multi-tier RBAC. Admin is a single
     boolean capability — the only role beyond per-zone grants.
- Basis: joe-identity-design.md §2.9 (principal mapping & bootstrap),
  §5 Invariant 2 (every path to infra passes through the guarded accessor
  — admin is part of that decision now), §6 (admin UI deferred behind a
  CLI seam that exists);  joe-identity-phase-plan.md Phase H. Verified
  against Phase A's accessor signature (D-0004), Phase B's set-shaped
  path (D-0005), Phase C's admin bootstrap (D-0006, superseded by this
  entry's snapshot replacement), Phase D's service-account wiring
  (D-0007), Phase E's accessor-on-both-paths (D-0008), Phase F's audit
  chokepoint (D-0009), and Phase G's captain-gate-on-loop + set-shaped
  HasZoneAccess (D-0010). New tests:
  - `internal/rbac/policy_test.go`:
    `TestPhaseH_AdminAllowedOnZoneCreatedAfterDesignation` (the bug-fix
    demonstration — FAILS pre-Phase-H, PASSES now),
    `TestPhaseH_AdminAllowedAcrossMultipleZonesWithoutGrants` (breadth +
    allowed_actions ceiling),
    `TestPhaseH_NonAdminOutcomesUnchanged` (regression: non-admin
    unchanged from post-Phase-G),
    `TestPhaseH_AdminDecisionReasonIsDistinct` (audit-trail
    distinguishability at the Decision struct level),
    `TestPhaseH_HasZoneAccessAdminCapability` (sourceless path coverage),
    `TestPhaseH_AdminIsAdminErrorFallsBackToGrant` (failure posture).
  - `internal/access/audit_test.go`:
    `TestPhaseH_AdminAllowAuditReasonDistinguishedFromZoneGrant`
    (audit-trail distinguishability through the accessor +
    audit.Repository),
    `TestPhaseH_AdminAllowedOnPostBootstrapZone` (bug fix verified on
    the audit path too).
  - `internal/auth/provision_test.go`:
    `TestPhaseH_GrantAdminMarksDynamicCapability` (bootstrap writes
    admin_principals, not rbac_policies),
    `TestPhaseH_NoLeftoverSnapshotGrants` (the no-snapshot
    structural assertion: seeded snapshot rows are cleaned up),
    `TestPhaseH_GrantAdminIsIdempotent` (safe re-run on every login).
  - `internal/auth/handlers_test.go::TestCallback_AdminBootstrap`:
    rewritten for Phase H semantics (admin_principals row exists; no
    rbac_policies rows; non-admin login still gains nothing).
  - `cmd/joe/admin_test.go`:
    `TestPhaseH_AdminGrantListRevoke` (end-to-end CLI),
    `TestPhaseH_AdminGrantCleansUpZoneSnapshots` (CLI enforces single
    source of truth),
    `TestPhaseH_AdminListEmpty` and
    `TestPhaseH_AdminGrantUnprefixedPrincipalRejected` (CLI error
    handling).
  All prior-phase tests (Phase A no-ungoverned-access invariant, Phase
  A/B/C/D/E/F/G regressions including the executable authority-
  invariance, captain-on-loop, and append-only audit guards) still
  green and unchanged.
- Supersedes: D-0006's snapshot definition of admin authority. The
  rest of D-0006 (OIDC + sessions + CLI zone provisioning + the
  config-designated admin_email TRIGGER) stands. The touched packages
  are `internal/store/migrations` (new 016), `internal/rbac` (new Admin
  type, repository methods, policy engine short-circuit, reason
  constants), `internal/auth` (Provisioner.GrantAdmin replaced),
  `cmd/joe` (new admin subcommand; zoneRepo → rbacRepo); OIDC,
  service-account resolution, the accessor's transport wiring, the
  captain gate, the audit_log schema, and PrincipalSet are untouched.
  With this phase the planned identity work — Phases A–H — is complete.
- Status: active. Phase H complete. This closes the tracked identity
  follow-ups: no further phase in this sequence is planned.

---

## D-0010 — Identity Phase G: shared §C captain gate on the agentloop path; HasZoneAccess set-shaped; coreagent refresh confirmed read-only

- Date: 2026-05-30
- Decision: Phase G (joe-identity-design.md §0 bug #2, §2.10, §5
  Invariant 6; joe-identity-phase-plan.md Phase G) fixes the wiring
  hole the design called out as "the incident-mode design is unenforced
  on the path that matters." Three things changed; the captain
  *concept* did not (it remains a session-ownership concurrency control,
  DENY-ONLY, never widening RBAC authority). Specifics:
  - **One shared §C gate, extracted into a new package
    `internal/captaingate`.** Pre-Phase-G the gate logic + §B1 principal
    substitution lived inside `coreagent.DurableExecutor`, which wraps
    only the Core Agent's onboarding/refresh executor — NOT the user
    task loop (`agentloop.Agent.Run` behind
    `/api/v1/tasks` and `/api/v1/tasks/stream`). Phase G moves the
    gate into `captaingate.Wrapper` (a tool-executor wrapper that
    implements both single `Execute` and batch `ExecuteBatch` +
    `ResultsToMessages` so it is a drop-in for `*tools.Executor` in
    `agentloop.NewAgent`), and composes it around BOTH paths:
    1. **Core Agent path** (`cmd/joe-core/main.go:520-531`):
       `captaingate.New(coreagent.NewDurableExecutor(inner, runRepo),
       sessRepo, auditRepo)`. The gate now runs UPSTREAM of §D5
       persistence, so a refused mutation is never persisted as an
       issued intent — nothing happened to record. `DurableExecutor`
       lost its `sessRepo` parameter and is now pure §D5 idempotency.
    2. **User task loop path** (`internal/api/tasks.go:240-258`):
       `var loopExec agentloop.BatchExecutor = executor; if
       services.SessionModel != nil { loopExec = captaingate.New(...) }`.
       This wraps the SAME `*tools.Executor` the loop has always used.
       `agentloop.Agent.executor` is now an interface
       (`agentloop.BatchExecutor`) so both `*tools.Executor` and
       `*captaingate.Wrapper` satisfy it without further plumbing.
    The static guard
    `internal/captaingate/single_impl_guard_test.go::TestPhaseG_SingleSharedCaptainGateImplementation`
    parses the whole repo and FAILS if any production package other
    than `internal/captaingate` calls `sessiongate.Check`. This is the
    structural enforcement of the "do not duplicate the logic in two
    places that can drift" requirement: there is exactly one production
    `sessiongate.Check` call site, and both agentic paths reach it
    through the same `captaingate.Wrapper.Execute`.
  - **Gate-then-accessor ordering on the loop path (req 2).** Inside
    `Wrapper.Execute`: classify tier → T1 bypass → on T2/T3 call
    `sessiongate.Check(ctx, sessRepo, sessionID, principal, tier)`. On
    refusal the wrapper returns `*captaingate.GateRefusalError`
    immediately — no inner `Execute`, no accessor call, no infra. On
    allow the wrapper performs the §B1 substitution (in incident regime
    only) and then calls `inner.Execute`, which is the path that
    eventually reaches the accessor's RBAC check via the in-process
    client. The Phase G behavioural test
    `TestPhaseG_LoopPathNonCaptainMutationRefused` proves this on the
    LOOP path specifically — pre-Phase-G the user task loop used a
    naked `*tools.Executor` with no gate, so this test would have
    SUCCEEDED at the mutation (the bug); it now correctly refuses with
    a `*GateRefusalError`. The captain-session mutation still succeeds
    (`TestCaptainGate_EndToEnd`), and non-captain READS still succeed
    (`TestPhaseG_LoopPathNonCaptainReadsStillSucceed`) — the gate only
    constrains mutation, never read/investigation.
  - **Gate stays DENY-ONLY; authority-invariance is now a passing test
    (req 3 + acceptance criterion).** The Phase G test
    `TestPhaseG_GateIsDenyOnly_RBACAuthorityInvariance` seeds a
    principal/source/policy combo, computes the `IsAllowed` outcome
    under normal regime, declares an incident, and asserts the SAME
    outcome under incident regime for the SAME principal/action/zone.
    Identical in both directions: a granted principal stays granted; an
    ungranted principal stays denied. This is the executable form of
    the §2.10 invariant ("incident mode never increases any principal's
    authority"). If a future change ever leaks regime state into
    `IsAllowed`/`Decide`, this test catches it.
  - **Loop-path gate refusal lands in the audit trail (req 4).** The
    wrapper writes ONE row per refusal — kind=`captain_transition`,
    action=`captain_gate_refused`, decision=`deny`, principal=caller
    ctx principal, context={tool, session_id, captain_session_id} — via
    the SAME `audit.Repository` Phase F wired into the accessor and
    the regime/captain handlers. The audit kind reuses the existing
    `KindCaptainTransition` (a gate refusal IS a captain-mechanism
    event), so migration 015's CHECK constraint on `kind` is unchanged;
    no new migration is needed. Failure posture follows the Phase F
    helper `audit.FailurePosture`: the refusal action is not in the
    read-class enum, so an audit-insert failure fail-CLOSES (returns
    the audit error rather than the refusal). The mutation does not
    proceed either way because the gate already refused it; the only
    observable difference is which error the LLM-facing layer surfaces.
    Verified by `TestPhaseG_GateRefusalRecordedInAuditTrail` which
    asserts: row exists, kind/decision/principal are correct, and the
    context blob names the captain session id.
  - **`HasZoneAccess` set-shaped (req 5).** The sourceless authorization
    function now takes `rbac.PrincipalSet` instead of a single
    `rbac.Principal`, mirroring `IsAllowed`/`Decide` (D-0005). Same
    union-of-grants semantics: allowed if ANY member holds a matching
    grant; denied if none do; per-member lookup failures degrade to
    deny-that-member only; the zone's allowed-actions cap is unchanged
    (no widening via union). Production callers — both `declare` and
    `resolve` in `internal/api/regime.go` — build the set as
    `rbac.NewPrincipalSet(principal)` from the caller's context
    principal, size 1, consistent with the rest of the system. Phase B
    deliberately left this single-principal as "out-of-chain" (D-0005
    deviation 3); Phase G is where the regime/captain path joins. Test
    coverage:
    `internal/rbac/policy_test.go::TestPolicyEngine_HasZoneAccess_SetSingleMember`
    (the production size-1 outcome — granted allow / ungranted deny;
    identical to the pre-Phase-G single-principal call) and
    `TestPolicyEngine_HasZoneAccess_SetUnion` (the forward-looking
    multi-member contract: any-granted allow, none-granted deny, empty
    set deny, no zone-action widening). The existing Phase A/B
    regression `TestRegime_6B_NoIncidentalSourceWidening` was updated
    to call `HasZoneAccess(ctx, NewPrincipalSet(principal), ...)`; its
    behavioural assertions are unchanged and still pass, so the
    single-principal outcome is byte-identical.
  - **coreagent refresh: VERDICT-A — READ-ONLY on infrastructure,
    allowlist retained (req 6).** The Phase G investigation enumerated
    every adapter call on the
    `internal/coreagent/{alerting,aws,azure,crd,datastore,git,gitops,k8s,networking,observability,registry}_refresh.go`
    paths and confirmed each one is List/Get/Describe/Status only — no
    Create/Update/Delete/Apply/Post/Put/Patch on any adapter. The
    onboarding side (the Core Agent's own agentic loop in
    `executor_durable.go`, now gated by `captaingate`) mutates only
    INTERNAL state via `graph_add_node` / `graph_add_edge` /
    `save_onboarding_fact` — these never touch customer infrastructure.
    Conclusion: no path on the coreagent side issues an infrastructure
    mutation, so the no-ungoverned-access allowlist exception for
    `internal/coreagent/` stays. The exception's rationale is now
    documented in `internal/api/access_guard_test.go` (the same place
    the invariant is enforced) with a Phase-G paragraph that states the
    audit was performed, what was checked, and what future change would
    make this allowlist line a violation again.
- Deviations from the Phase G prompt, with reasons:
  1. **Gate refusal audit re-uses `KindCaptainTransition` instead of
     adding a new `kind` value.** Migration 015's CHECK constraint
     allows only `infra_access`, `regime_transition`,
     `captain_transition`. The gate IS a captain-mechanism event, so
     re-using the existing kind is the natural home and avoids a
     migration. The action verb `captain_gate_refused` discriminates
     the row from attach/transfer rows. Operators querying for
     captain-mechanism events get gate refusals alongside attaches and
     transfers without a schema change.
  2. **Loop-path test does NOT spin the full LLM loop.** The acceptance
     criterion is "demonstrated on the agentloop path specifically";
     the wrapper is the same object both paths get, and
     `agentloop.Agent.Run` calls `executor.ExecuteBatch` each
     iteration. Driving `Wrapper.ExecuteBatch` directly with crafted
     `[]tools.ToolCallRequest` exercises EXACTLY the code path
     `Agent.Run` would, without the cost of a fake LLM. The test
     comment calls out explicitly that pre-Phase-G this would have
     succeeded (the bug); the fact that it now refuses is the signal.
  3. **`agentloop.Agent.executor` field changed from
     `*tools.Executor` to interface `agentloop.BatchExecutor`.** The
     prompt allows breaking internal interfaces freely; this is the
     minimum surface change to make the captaingate wrapper a drop-in.
     `*tools.Executor` and `*captaingate.Wrapper` both satisfy
     `BatchExecutor`, so tests that don't care about the gate (e.g.
     `test/e2e/agent_flow_test.go`) keep passing `*tools.Executor`
     directly without modification.
  4. **Gate refusal audit fails CLOSED with the audit error wrapped
     rather than the refusal.** The mutation is denied either way
     (gate refused, inner not invoked), so the visible difference is
     only which error the LLM-facing layer surfaces. The choice keeps
     consistency with Phase F's failure-posture helper without
     special-casing.
  5. **Test ergonomics: gate behavioural tests moved from
     `coreagent/executor_gate_test.go` to
     `captaingate/captaingate_test.go`.** Same scenarios, same
     `principalSpyExecutor`, same end-to-end + ordering + B1 +
     T1-bypass assertions; what changed is the wrapper under test
     (`captaingate.Wrapper` instead of `coreagent.DurableExecutor`)
     because the gate moved.
- Basis: joe-identity-design.md §0 (bug #2 statement), §2.10 (captain
  is a session-ownership concurrency control, DENY-only, never widens
  RBAC), §2.7 (set-shaped authorization), §5 Invariants 4 and 6 (the
  authority-invariance and captain-on-loop invariants);
  joe-identity-phase-plan.md Phase G. Verified against Phase A's
  accessor signature (D-0004), Phase B's set-shaped path (D-0005),
  Phase C's edge-auth (D-0006), Phase D's service-account wiring
  (D-0007), Phase E's accessor-on-both-paths (D-0008), and Phase F's
  audit chokepoint (D-0009). New tests:
  - `internal/captaingate/captaingate_test.go`:
    `TestCaptainGate_EndToEnd`, `TestCaptainGate_RefusalNeverCallsInner`,
    `TestCaptainGate_B1_PrincipalSubstitution`,
    `TestCaptainGate_AllowsT1ReadsInIncident` (migrated equivalents of
    the Change-10 executor_gate_test.go cases);
    `TestPhaseG_LoopPathNonCaptainMutationRefused` (the bug-#2 fix
    demonstration on the LOOP path),
    `TestPhaseG_LoopPathNonCaptainReadsStillSucceed`,
    `TestPhaseG_GateRefusalRecordedInAuditTrail`,
    `TestPhaseG_GateIsDenyOnly_RBACAuthorityInvariance`.
  - `internal/captaingate/single_impl_guard_test.go::TestPhaseG_SingleSharedCaptainGateImplementation`
    — repo-wide AST guard, fails if `sessiongate.Check` is called from
    any production package other than `internal/captaingate`.
  - `internal/rbac/policy_test.go::TestPolicyEngine_HasZoneAccess_SetSingleMember`
    and `TestPolicyEngine_HasZoneAccess_SetUnion` — the set-shaped
    contract.
  All prior-phase tests (Phase A no-ungoverned-access invariant + loop
  coverage, Phase A/B/C/D/E/F regressions, sessiongate import-closure
  guard, executor §D5 idempotency tests now without sessRepo) still
  green and unchanged.
- Supersedes: nothing — extends D-0009. Phase H (admin-zones-snapshot)
  remains the sole tracked follow-up. The touched packages are
  `internal/captaingate` (new), `internal/coreagent` (gate removed
  from `DurableExecutor`; `sessRepo` parameter dropped),
  `internal/agentloop` (executor field is now an interface),
  `internal/api` (`buildTaskRun` wraps the executor),
  `internal/rbac` (`HasZoneAccess` set-shaped),
  `internal/audit` (new action + reason constants;
  `KindCaptainTransition` doc widened), `cmd/joe-core/main.go`
  (composition); OIDC, service-account resolution, accessor RBAC
  logic, and migration 015 are untouched. With this phase the planned
  identity work is complete except for the tracked Phase H follow-up.
- Status: active. Phase G complete; do not proceed to Phase H without a
  new prompt.

---

## D-0009 — Identity Phase F: append-only audit at the decision point; regime/captain transitions redirected; bug #3 fixed

- Date: 2026-05-30
- Decision: Phase F (joe-identity-design.md §2.6, §4;
  joe-identity-phase-plan.md Phase F) introduces ONE append-only audit
  table backed by a new package `internal/audit`, written by the guarded
  accessor on every authorization decision (allow and deny alike) AND by
  the regime/captain transition handlers as their durable record. The
  per-decision write site is the accessor's `permit()` chokepoint
  (`internal/access/access.go`); the transition write sites are the
  regime and captain HTTP handlers (`internal/api/regime.go`,
  `internal/api/captain.go`). Bug #3 (joe-identity-design.md §0 bug #3)
  is fixed: incident history now lives in the audit log, independent of
  `system_regime.declared_by_principal` and `session_captains`, both of
  which still get cleared/cascaded on resolve. Specifics:
  - **One table, one migration (015_audit_log).** Schema:
    `audit_log(id INTEGER PRIMARY KEY AUTOINCREMENT, created_at TEXT,
    principal TEXT NULL, action TEXT, zone TEXT NULL, source TEXT NULL,
    decision TEXT CHECK IN ('allow','deny'), reason TEXT, kind TEXT
    CHECK IN ('infra_access','regime_transition','captain_transition'),
    context TEXT DEFAULT '{}')`. Column rationale captured in the
    migration's header comment. Three indices: `created_at`, `principal`,
    `kind`. Nullables only where the row kind legitimately produces no
    value (zone/source for sourceless transitions). Encoding follows
    existing schema conventions: TEXT RFC3339 timestamps (009, 010, 011,
    014), `INTEGER PRIMARY KEY AUTOINCREMENT` id (006, 007), CHECK
    constraints for enum-shaped TEXT (009).
  - **Dual append-only enforcement (Phase F req 2, design §2.6).**
    1. **Code:** `audit.Repository` is an interface declaring exactly
       one method, `Insert(ctx, Event) error`. There is NO Update,
       Delete, Truncate, Erase, or Remove on the API surface. The
       concrete `sqlRepository` is unexported and returned through the
       interface so no caller has a path to a mutator. Two AST guards
       (`internal/audit/audit_test.go::TestRepositoryAPISurface_AppendOnly`
       parses the package, finds the `Repository` interface, and fails
       if any method other than `Insert` is declared;
       `TestRepositoryAPISurface_NoMutatorPackageFunctions` fails if any
       top-level function name in the package starts with a mutator verb).
    2. **Database:** Migration 015 creates two triggers,
       `audit_log_no_update` and `audit_log_no_delete`, each
       `RAISE(ABORT, 'audit_log is append-only: <verb> is not permitted')`.
       Verified by `internal/audit/sql_test.go::TestMigration015_TriggerBlocksUpdate`
       and `TestMigration015_TriggerBlocksDelete`. Even an operator with
       raw SQL access cannot rewrite or erase history.
  - **Per-decision write at the accessor's chokepoint (req 3).** The
    accessor's `permit(ctx, principals, sourceID, action)` is the single
    point both HTTP and loop paths converge on (D-0004, D-0008). It now
    calls a new `rbac.PolicyEngine.Decide(...)` method that returns
    `Decision{Allowed, Zone, Reason}` — `IsAllowed` is now a thin
    boolean wrapper over `Decide`. `permit` then writes ONE audit row
    capturing principal, action, zone, source, decision (allow/deny),
    structured reason (`policy_allow` / `no_grant` / `action_not_in_zone`
    / `zone_not_found` / `rbac_disabled`), and `kind=infra_access`. The
    accessor's allow/deny OUTCOME is unchanged from D-0005/D-0008 (audit
    is observation, not policy) — verified by all prior-phase tests
    still passing.
  - **Failure posture split (req 4, design §4).** The fail-CLOSED on
    mutate / fail-OPEN on read decision lives in one helper,
    `audit.FailurePosture(ctx, action, err, where)`, called from every
    audit caller so the split cannot drift. The helper inspects the
    action string: `read` and `query` → fail-open (returns nil after a
    loud WARN log naming the where/action/error); everything else,
    including `mutate`, `delete`, and ALL transition verbs (declare /
    resolve / captain_attach / captain_transfer_*) → fail-closed (returns
    the original audit error after an ERROR log). The accessor's
    `permit` wraps the returned error in `"audit write failed for
    mutating action: %w"` so callers can distinguish it from
    `ErrPermissionDenied`. Behavioural tests:
    `internal/access/audit_test.go::TestPhaseF_FailClosedOnMutate`
    (mutate adapter is NOT called when audit insert errors;
    `GitHubPostComment` returns a non-permission error) and
    `TestPhaseF_FailOpenOnRead` (read adapter IS called when audit
    insert errors; `K8sListResources` returns nil error). Unit-level
    coverage of the helper across all read and mutate verbs in
    `internal/audit/audit_test.go::TestFailurePosture_FailOpenOnRead` /
    `FailClosedOnMutate`.
  - **Regime transitions redirected to durable rows (req 5, bug #3).**
    The regime handler (`internal/api/regime.go`) now writes one audit
    row of kind `regime_transition` BEFORE every `DeclareIncidentRegime`
    / `ResolveIncidentRegime` repository call. Denials of either
    capability ALSO write one deny row, so rejected transitions are
    durably recorded too. The write is fail-closed: if the audit insert
    fails the regime mutation does NOT proceed (the
    `system_regime`/`agent_sessions` rows are untouched). After resolve,
    `system_regime.declared_by_principal` is still nulled (the existing
    code in `sessionmodel/regime_transitions.go:210-214` is unchanged —
    the live-state row stays mutable, per the design's "may remain as
    live-state"), but the durable record of who declared the incident
    and when lives in the audit log and is independent of that row.
    Bug #3 regression test:
    `internal/api/audit_phasef_test.go::TestPhaseF_Bug3_IncidentHistorySurvivesResolve`
    declares as alice → resolves → asserts both that
    `system_regime.declared_by_principal IS NULL` (the test's premise
    that the bug behaviour is still present in the mutable row) AND that
    `audit_log` still holds one allow row for
    `action=declare_incident principal=alice`. This test would FAIL on
    pre-Phase-F code (no audit table existed); it PASSES now.
  - **Captain transitions redirected to durable rows (req 5).** The
    captain handler (`internal/api/captain.go`) writes one audit row of
    kind `captain_transition` BEFORE every captain mutation: attach,
    transfer begin, transfer confirm, transfer cancel. Same fail-closed
    posture. Bug #3 companion test:
    `internal/api/audit_phasef_test.go::TestPhaseF_CaptainTransitionsSurviveResolve`
    deletes the session after resolve (triggering the
    `session_captains ON DELETE CASCADE` from migration 009:62) and
    asserts the audit rows are still present.
    `TestPhaseF_CaptainAttachWritesAuditRow` covers the HTTP attach path
    end-to-end.
  - **R-CAP1 (declare+captain atomic) coverage.** R-CAP1 attaches the
    declaring principal as captain inside the same transaction as the
    `system_regime` flip (`regime_transitions.go:96-104`). Phase F does
    NOT write a separate `captain_attach` audit row for R-CAP1 — the
    `declare_incident` audit row already captures who took command and
    when, and adding a duplicate row would either require a second
    write inside the transaction (mixing the audit and regime layers
    needlessly) or a write outside the atomic boundary (where it could
    diverge from the regime state on rollback). The single
    `declare_incident` row with `principal=alice` IS the captain-
    attached-at-declare record. Subsequent attaches via the HTTP
    endpoint do produce dedicated `captain_attach` rows.
  - **Wiring.** `cmd/joe-core/main.go` constructs
    `audit.NewRepository(sqlStore.DB(), sqlStore.Driver())` after the
    migrations run and stores it on `services.Audit` (a new field on
    `core.Services`). `api.New` reads `services.Audit` and passes it to
    `access.New(...)`; `regimeHandler` and `captainHandler` read it
    from `s.services.Audit`. A nil `Audit` field is treated as
    "audit disabled" by every caller (a NoopRepository is also provided
    in `internal/audit/noop.go` for explicit test use). This nil-safety
    lets the existing accessor tests
    (`internal/access/access_test.go`) keep working without churn —
    they pass `nil` for the audit repo and verify the same allow/deny
    outcomes the rest of the suite proves.
  - **rbac.PolicyEngine surface changes.** `IsAllowed(ctx, principals,
    sourceID, action) bool` is preserved — it now delegates to a new
    `Decide(ctx, principals, sourceID, action) Decision` whose return
    struct also carries the resolved Zone and a machine-readable Reason
    used by the audit row. Existing IsAllowed callers (the policy
    tests, regime handler's HasZoneAccess sibling) are unaffected. The
    behavioural outcome — which principals on which sources/actions
    return true — is unchanged across every Phase A/B/C/D/E regression
    test still passing.
  - **No retention/rotation in v1.** Out of scope per the prompt's
    explicit scope fence. The table grows monotonically; an operator
    needing space management would `DROP TABLE audit_log` via the
    Phase F down migration (the only sanctioned way out of the
    append-only contract) and re-migrate. Adding a retention policy
    behind a separate insert-rotate-only repository is a clean v2
    extension — the existing `Repository` interface stays as-is.
- Deviations from the Phase F prompt, with reasons:
  1. **R-CAP1 captain-attach row not separately written.** See the
     R-CAP1 paragraph above — the `declare_incident` row covers the
     same "who and when" information, and a separate row would either
     leak into the atomic-declare transaction or risk diverging from it.
     The `TestPhaseF_CaptainTransitionsSurviveResolve` test exercises
     the durable record via the declare/resolve rows (which both
     reference alice as the declaring captain in spirit, even if the
     `audit_log.action` discriminates kinds).
  2. **`NoopRepository` provided alongside nil-safe accessors.** The
     prompt's "every authorization decision writes one audit row"
     requirement is held for the production path
     (`cmd/joe-core/main.go` always wires the SQL repo); tests that don't
     care about audit pass nil (skipping the write entirely) or the
     NoopRepository (accepting and discarding writes). The Phase F
     behavioural tests use a recording in-memory implementation
     (`internal/access/audit_test.go::recordingAudit`) and the SQL
     repository for integration coverage.
  3. **Reason vocabulary is structured tags, not free-form text.**
     `policy_allow` / `no_grant` / `action_not_in_zone` /
     `zone_not_found` / `rbac_disabled` for accessor rows, and
     `transition_recorded` / `no_grant` for transition rows. Tags are
     stable and machine-parseable; future operator queries against
     `audit_log.reason` get a small enumerable set, not English prose.
- Basis: joe-identity-design.md §0 (bug #3 statement), §2.6 (append-only
  audit at the decision point), §4 (failure posture split), §5
  Invariant 5 (append-only + transitions not erased on resolve);
  joe-identity-phase-plan.md Phase F. Verified against Phase A's
  accessor signature (D-0004), Phase B's set-shaped path (D-0005),
  Phase C's edge-auth (D-0006), Phase D's service-account wiring
  (D-0007), and Phase E's accessor-on-both-paths (D-0008). New tests:
  - `internal/audit/audit_test.go`: append-only API guard,
    no-mutator-package-function guard, FailurePosture fail-open/
    fail-closed split coverage, NoopRepository.
  - `internal/audit/sql_test.go`: Insert round-trip, NULL handling for
    sourceless rows, UPDATE-blocked trigger, DELETE-blocked trigger.
  - `internal/access/audit_test.go`: allow writes one allow row with
    correct fields; deny writes one deny row with denial reason;
    fail-closed on mutate (audit insert error blocks adapter call);
    fail-open on read (audit insert error proceeds to adapter call).
  - `internal/api/audit_phasef_test.go`:
    `TestPhaseF_Bug3_IncidentHistorySurvivesResolve` (the named
    regression — fails on pre-Phase-F code), `CaptainTransitionsSurviveResolve`,
    `CaptainAttachWritesAuditRow`, `DeclareDenialWritesAuditRow`.
  All prior-phase tests (Phase A no-ungoverned-access invariant, Phase
  A/B/C/D/E regressions, Phase E equivalence, Phase E loop coverage)
  still green and unchanged.
- Supersedes: nothing — extends D-0008. Phase G (captain wiring onto
  the agentloop path) remains pending. The accessor, the regime
  handler, the captain handler, and the SessionModel repository are
  the touched packages; OIDC, service-account resolution, the loop
  client, and the policy decision logic are untouched.
- Status: active. Phase F complete; do not proceed to Phase G without a
  new prompt.

---

## D-0008 — Identity Phase E: remove the loopback; loop runs through the accessor as the real caller; middleware demoted

- Date: 2026-05-29
- Decision: Phase E (joe-identity-design.md §1, §2.5, §3 sequencing;
  joe-identity-phase-plan.md Phase E) removes the in-process loopback. The
  agentic loop's tool registry no longer constructs a loopback `*client.Client`
  that re-authenticates as `svc:server`; instead it is wired to an in-process
  accessor-backed client that reads the real caller principal from Go context
  (the SAME principal `auth.EdgeAuth` set via `rbac.WithPrincipal` at the edge)
  and dispatches to `internal/access` directly. `EnforcementMiddleware` is
  demoted from the authoritative per-zone gate to a pass-through, gated by an
  equivalence test. Specifics:
  - **In-process client for the loop's tools.** `internal/api/inproc_client.go`
    introduces `inProcessCoreClient`, which implements every per-tool
    `*Client` interface in `internal/tools/core/`. Each method reads
    `rbac.PrincipalFromContext(ctx)` at the call site (literally — not via a
    helper, so the Phase B static guard
    `TestPhaseB_AccessorCallersDerivePrincipalFromContext` sees the context
    derivation) and calls the matching `*access.Accessor` dispatch method.
    There is NO HTTP, NO `client.New`, NO bearer key, and NO re-authentication
    on this path. Identity is established once at the edge and carried by
    context, per design §1 ("authenticate at real boundaries; pass identity
    by context within a process").
  - **Aggregate `coretools.CoreToolsClient` interface.** Defined in
    `internal/tools/core/coreclient.go` as the union of every per-tool
    `*Client` interface used by `registerCoreTools`. `tools.NewCoreRegistry`
    and `tools.NewDefaultRegistryWithClient` now take this interface instead
    of `*client.Client`. The HTTP `*client.Client` still satisfies it
    (preserving the e2e/integration test harness in `test/e2e`,
    `test/integration`, and the schema-validity test in
    `internal/tools/default_test.go`); the in-process client is the second
    implementation, used by the loop.
  - **Wiring.** `api.Server` now holds an `*inProcessCoreClient` built once
    by `api.New` alongside the accessor (`internal/api/server.go`).
    `internal/api/tasks.go`'s `buildTaskRun` passes `h.server.inproc` to
    `tools.NewCoreRegistry`. The deleted block (≈18 lines that built the
    scheme/loopbackURL/loopbackClient with `client.New(loopbackURL, ...)` and
    bearer-keyed it with `ServerConfig.LoopbackKey()`) is the entire loopback
    construction site for in-process tool execution. The static guard
    `TestPhaseE_NoLoopbackClientForInProcessToolExecution`
    (`internal/api/access_phasee_test.go`) parses `tasks.go`, `tasks_stream.go`,
    and `inproc_client.go` and fails if any of them reintroduces a
    `client.New(...)` call.
  - **Non-adapter tool dependencies.** A handful of core tools do not touch
    an adapter or the graph store: `list_sources` (reads
    `services.Store.Sources`), `search_knowledge` (calls
    `services.Knowledge.Search`), `detect_doc_drift`/`generate_doc_draft`
    (use `services.DriftDet`/`services.DocDrafter`), and
    `publish_doc_update`. These reach the in-process service directly. None
    of them is principal-gated today (they predate the Phase A accessor) and
    NONE is what the no-ungoverned-access invariant covers — that invariant
    is about adapters and the graph store
    (`internal/api/access_guard_test.go`). For `publish_doc_update`, the
    publish dispatch logic was extracted from `s.publishProposal` into the
    package-level `publishProposalToTarget(ctx, services, proposal)` helper
    in `internal/api/publish.go` so both the HTTP handler and the in-process
    client share it without either path going through an HTTP loopback.
  - **`EnforcementMiddleware` demoted to a pass-through.** With the accessor
    now authoritative on BOTH paths (HTTP via Phase A, loop via this phase),
    the middleware's per-zone `IsAllowed` call is redundant. It is now a
    no-op: `EnforcementMiddleware(engine)` returns a middleware that calls
    `next` directly, with `engine` retained as an argument only so existing
    test harnesses that build the middleware do not need to change. The
    obsolete tests in `internal/rbac/middleware_test.go` that asserted
    middleware-level IsAllowed behaviour are deleted; the new
    `TestEnforcementMiddleware_Passthrough` documents the demotion.
  - **Equivalence test gating the demotion (req 6).**
    `TestPhaseE_AccessorAloneMatchesPriorOutcomes` constructs two production
    chains over the same routes + RBAC state:
    `(EdgeAuth → demoted middleware → accessor)` and
    `(EdgeAuth → accessor)`. It asserts identical 200/403/401 outcomes
    across granted-read, ungranted-zone, missing-token, and invalid-token
    cases. The Phase A regression test
    `TestPhaseA_HTTPRBACOutcomesPreserved` continues to pass unchanged,
    proving the same outcomes match the pre-Phase-E expectations.
  - **`svc:server` and `LoopbackKey()`: what survives and why.** The reserved
    `svc:server` service account and `ServerConfig.LoopbackKey()` REMAIN.
    They are still presented by the joe CLI (`cmd/joe/main.go`) and the REPL
    panic command (`internal/repl/repl.go`) — these are external co-located
    HTTP clients to joe-core, NOT loopback in the in-process sense. The
    LoopbackKey docstring is updated to reflect the post-Phase-E reality
    (historical name, surviving consumer set). The
    `TestPhaseD_LoopbackKeyReachesInfra` test is renamed
    `TestPhaseD_ColocatedServerKeyReachesInfra` and its docstring rewritten
    to describe the CLI auth path, not the in-process loopback. The
    "JOE_API_KEY → server service account" env override
    (`internal/config/config.go`) is untouched.
  - **Phase A invariant: loop path covered, allowlist commentary updated.**
    The agent-loop execution package (`internal/api`, where `tasks.go` and
    the in-process client live) was already NOT in the allowlist; this phase
    makes that meaningful — the loop now reaches infra through the accessor
    only. The remaining allowlist entries are documented in the test:
    `internal/access` (the accessor itself), `internal/coreagent` (the
    timer-driven background refresh that runs without a caller principal —
    structurally outside the accessor), and `cmd/joe-core` (a process-level
    business-metric gauge with no caller principal). The
    `TestInvariant_NoUngovernedAdapterOrGraphAccess` text is updated to
    state explicitly that the loop path is now covered, not excepted.
  - **K8s return-shape conversion.** The accessor returns
    `[]unstructured.Unstructured` for K8s list/get; the tools expect
    `[]map[string]any`. The in-process client extracts `.Object` from each
    item before returning — matching the JSON shape the loopback HTTP client
    used to produce so no tool change is needed. AWS list calls similarly
    convert the accessor's value slices (e.g. `[]awsadapter.EC2Instance`)
    to the tool's pointer slices (`[]*awsadapter.EC2Instance`).
- Deviations from the Phase E prompt, with reasons:
  1. **`svc:server`/`LoopbackKey()` retained, not deleted.** The prompt
     allowed deletion only "IF it has no other remaining consumer". The joe
     CLI and the REPL are surviving external co-located clients (separate
     processes that share joe-core's config), and the `JOE_API_KEY` env
     override folds into this same account. The name remains "LoopbackKey"
     to minimise churn at every call site (cmd/joe x5, internal/repl x1),
     but every docstring is rewritten to reflect the post-Phase-E reality
     ("co-located CLI key, not loopback"). A rename to `CoLocatedKey()` is
     an isolated follow-up not required by Phase E.
  2. **`coreagent` refresh path NOT routed through the accessor.** The
     Phase A allowlist commentary said the coreagent exception should be
     removed in Phase E, but the refresh path is timer-driven background
     work with no caller principal — the accessor's enforcement model
     (`permitForPrincipal(ctx, principal, ...)`) does not fit it without
     either granting svc:server every zone or special-casing the principal
     in the accessor (both defeat the purpose). Phase E's scope is the
     LOOP path (per the design doc §3), which is now governed by the
     accessor. The coreagent allowlist remains, with its rationale updated
     to spell out the structural difference. If the refresh path is later
     refactored to take a principal, the allowlist entry should be removed
     then.
  3. **In-process equivalence test instead of replaying the legacy chain.**
     The pre-Phase-E "middleware does IsAllowed" chain no longer exists in
     the codebase (the demotion is the change being shipped). The
     equivalence test asserts that the two surviving chains —
     `(demoted middleware + accessor)` and `(accessor alone)` — agree on
     200/403/401 across the matrix, AND that the Phase A regression test
     (the pre-Phase-E behavioural contract) still passes through the
     demoted chain. Together these prove the demotion preserves outcomes.
  4. **Aggregate interface defined in `internal/tools/core`, not a new
     package.** The simplest seam keeping the per-tool small interfaces
     intact for unit testing is a composition interface alongside them;
     `coreClient.go` does exactly that. A new `tools/inproc` package would
     be heavier without producing different behaviour.
- Basis: joe-identity-design.md §1 (root-cause: loopback IS the identity
  reset), §2.5 (accessor is the authoritative point), §3 (sequencing — E
  must follow A+B, which both merged in D-0004/D-0005), §5-Invariants 1–3;
  joe-identity-phase-plan.md Phase E. Verified against Phase A's accessor
  signature (D-0004), Phase B's set-shaped path (D-0005), Phase C's
  edge-auth + CLI provisioning (D-0006), and Phase D's service-account
  resolver (D-0007). Tests added:
  `TestPhaseE_LoopEnforcesAgainstRealCallerPrincipal` (alice succeeds,
  mallory denied, svc:server not granted — impossible on pre-Phase-E code),
  `TestPhaseE_LoopGraphAccessIsInProcess` (graph access works without an
  HTTP server),
  `TestPhaseE_AccessorAloneMatchesPriorOutcomes` (equivalence test),
  `TestPhaseE_NoLoopbackClientForInProcessToolExecution` (static guard
  against re-introducing `client.New(...)`),
  `TestEnforcementMiddleware_Passthrough` (documents the demotion). Phase A
  no-ungoverned-access invariant and Phase A/B/C/D regressions still green
  and unchanged.
- Supersedes: nothing — extends D-0007. Phases F (audit) and G (captain
  wiring) remain pending. The in-process loopback construction in
  `tasks.go`/`tasks_stream.go` is deleted; the in-process accessor is the
  new path. External clients (CLI SSE, Web UI API, MCP) are unchanged —
  they remain external HTTP clients that authenticate at the edge.
- Status: active. Phase E complete; do not proceed to Phase F without a new
  prompt.

---

## D-0007 — Identity Phase D: named service-account keys → svc: principals

- Date: 2026-05-29
- Decision: Phase D (joe-identity-design.md §2.4; joe-identity-phase-plan.md
  Phase D) replaces the single machine-auth key (`Server.APIKey` →
  `Server.Principal`) with a configurable collection of NAMED service-account
  keys, each resolving to a distinct `svc:<name>` principal that flows through
  the SAME context mechanism Phase B/C established (`rbac.WithPrincipal` →
  `rbac.PrincipalFromContext` → accessor + `EnforcementMiddleware`). Two
  authentication mechanisms (OIDC for humans, keys for machines), one
  authorization path. Specifics:
  - **Service-account config shape.** `config.ServerConfig.ServiceAccounts
    []ServiceAccount` (yaml `service_accounts`), each entry
    `{Name string, Key string}` resolving to principal `svc:<Name>`. This
    generalizes the old single `api_key`+`principal` into a set; the
    `Server.APIKey` and `Server.Principal` fields are REMOVED (no compat
    constraints). Keys are plaintext-at-rest — the same posture as the single
    key they replace, NOT a regression; no hashing/minting was added (deferred —
    see seam below). The `svc:` prefix is reserved/enforced at mint time by
    `rbac.ServicePrincipal(name)` (`internal/rbac/identity.go`), which mirrors
    `UserPrincipal`: it rejects an empty name and a name already carrying a
    reserved prefix (`user:`/`group:`/`svc:`) so a config typo cannot
    double-encode or kind-spoof.
  - **The key → svc: resolution seam (isolated, per the prompt's seam note).**
    `auth.ServiceAccountResolver` (`internal/auth/serviceaccount.go`) is the
    SINGLE place that owns "plaintext key, exact-match lookup → svc principal":
    `NewServiceAccountResolver([]config.ServiceAccount)` builds an immutable
    `map[key]rbac.Principal` (minting each via `rbac.ServicePrincipal`) and
    `Resolve(key) (rbac.Principal, bool)` performs the lookup. A future upgrade
    to DB-minted, hashed, runtime-revocable keys replaces ONLY this type's
    storage (the map) and lookup (`Resolve`) — the downstream
    principal-in-context flow (`EdgeAuth` → `rbac.WithPrincipal` → accessor) is
    untouched because it depends only on `Resolve` returning a principal. The
    constructor fails LOUDLY (fatal startup error in `cmd/joe-core/main.go`) on
    a malformed config — empty key, empty name, duplicate name, duplicate key,
    or reserved-prefix name — so a typo never silently drops an identity's
    authority or makes resolution ambiguous.
  - **OIDC-vs-service-key precedence on a shared request path.** `auth.EdgeAuth`
    resolves the caller principal in deterministic order: (1) a valid session
    cookie (human) WINS; (2) otherwise a service-account bearer key (machine) is
    tried via the resolver; (3) otherwise the request is unauthenticated → 401
    on a protected path. A request carries either a session cookie or a bearer
    key, never both meaningfully; when both are present the human session takes
    precedence. The two mechanisms are independent: `Sessions` may be nil
    (machine-only) and `ServiceAccounts` may be nil/empty (human-only) without
    breaking the other. An unknown bearer key is unauthenticated, exactly as an
    invalid token was. Both converge on one principal in context, which
    `EnforcementMiddleware` and the accessor evaluate identically regardless of
    which mechanism produced it. `EnforcementMiddleware` stays authoritative on
    the HTTP path (demotion is Phase E).
  - **Removal/fold of the old single-key path.** The single
    `Server.APIKey`→single-`Server.Principal` mechanism is removed, not kept in
    parallel: (a) the config fields are deleted; (b) `EdgeConfig.APIKey`/
    `APIKeyPrincipal` are replaced by `EdgeConfig.ServiceAccounts
    *ServiceAccountResolver` (plus an optional `DisabledPrincipal` defaulting to
    `default-operator` for auth-disabled mode); (c) `rbac.APIKeyProvider` (the
    literal single-key→single-principal `IdentityProvider`) and `api.BearerAuth`
    (the pre-Phase-C single-token gate) are DELETED along with their unit tests;
    (d) the engine enable-condition in both `cmd/joe-core/main.go` and
    `api.newPolicyEngine` becomes `ServiceAccountsConfigured() || OIDC`
    (was `APIKey != "" || OIDC`). The generic `rbac.IdentityMiddleware` +
    `rbac.IdentityProvider` interface are KEPT — they are not the single-key
    mechanism; many tests inject principals through them with their own
    providers. `JOE_API_KEY` env now folds into the reserved `server` service
    account (creating/overriding its key in `config.applyEnvOverrides`) — the
    literal "old key becomes one named entry" the prompt suggested; it only
    affects processes that load config (joe-core, joe CLI, REPL). MCP/Slack are
    separate external processes that read `JOE_API_KEY` directly from env and
    were untouched (req 5: no in-process MCP change — it presents a key like any
    external client and resolves to whatever svc account that key belongs to).
  - **What the loopback now authenticates with.** The in-process loopback
    (`internal/api/tasks.go`), the joe CLI (`cmd/joe`), and the REPL
    (`internal/repl`) are co-located client processes that present
    `ServerConfig.LoopbackKey()` — the key of the reserved `server` service
    account (principal `svc:server`), the direct fold of the old single key.
    `LoopbackKey()` returns the `server` account's key, else the first
    configured account (deterministic, config order), else "" (auth-disabled,
    no bearer presented). The loopback's existence and behaviour are UNCHANGED:
    it still presents a valid server-representing key so the loop's tools reach
    infra (the loopback is removed in Phase E). For the loop to reach infra
    under RBAC, `svc:server` must be granted zones via `joe zone grant
    --principal svc:server` — the same CLI surface Phase C built.
  - **CLI provisioning for svc: principals (req 4 — unchanged path).** `joe
    zone grant/revoke/list` already accepts a `svc:` principal (it validates via
    `rbac.HasReservedPrefix`, which includes `svc:`); confirmed by the existing
    `cmd/joe/zone_test.go` (`grant --principal svc:ci-bot`). No separate
    provisioning path was built.
- Deviations from the Phase D prompt, with reasons:
  1. **Loopback proven end-to-end via its credential through the real chain,
     not by driving the LLM loop.** `TestPhaseD_LoopbackKeyReachesInfra`
     presents `ServerConfig.LoopbackKey()` through the production-shaped chain
     (`EdgeAuth` + authoritative `EnforcementMiddleware`, as in main.go) and
     asserts it resolves to `svc:server` and reaches infra — denied (403) before
     a grant, allowed (200) after. The loopback's HTTP transport to infra IS
     this path; spinning the full agentloop (LLM + SSE) would test the loop, not
     the credential, and is heavier without adding auth coverage.
  2. **Phase A regression chain rewritten onto the production auth path.**
     Removing `Server.APIKey`/`Server.Principal` forced touching
     `access_regression_test.go`; rather than keep the dead
     `BearerAuth`+`APIKeyProvider` scaffold alive, the chain now uses the live
     `auth.EdgeAuth` + a one-entry resolver. The asserted Phase A outcomes
     (granted read 200 / ungranted 403 / missing token 401; disabled permits
     all) are preserved exactly — the regression contract is unchanged; only the
     mechanism establishing the principal moved to the current production path.
  3. **`svc:` prefix invariant proven behaviourally, not by an AST guard.** Per
     the prompt's static criterion, `rbac.ServicePrincipal` always applies the
     `svc:` prefix by construction (the single mint point); this is asserted in
     `internal/rbac/identity_test.go` and across every resolver output in
     `internal/auth/serviceaccount_test.go`. A data-flow AST assertion would be
     brittle and redundant given the single mint point.
  4. **`group:` reserved but unminted.** Per scope fence, the PrincipalSet stays
     size 1; nothing populates `group:` (a v2 seam). Service-account principals
     are single `svc:` members.
- Basis: joe-identity-design.md §2.4 (named API keys, MCP-is-a-service-account,
  rejects pass-through), §2.2 (reserved `svc:` prefix), §2.7 (set stays size 1),
  §2.5 (EnforcementMiddleware authoritative until E), §6 (deferred
  hashing/minting + MCP pass-through behind seams); joe-identity-phase-plan.md
  Phase D. Verified against Phase A's accessor signature (D-0004), Phase B's
  set-shaped path (D-0005), and Phase C's edge-auth + CLI provisioning (D-0006).
  Tests: two distinct keys → two distinct svc principals, each allowed only on
  its own granted zone and denied on the other's through the accessor
  (`TestPhaseD_TwoServiceAccountsIndependentZones`); unknown key → 401
  (`TestPhaseD_UnknownKeyUnauthenticated`, `TestEdgeAuth_UnknownServiceAccount…`);
  zero-zone svc denied then allowed after a CLI-equivalent grant
  (`TestPhaseD_ZeroZoneDeniedThenGrantAllows`); OIDC session and svc key each
  produce the correct principal on the same endpoint with session-wins
  precedence (`TestPhaseD_OIDCAndServiceKeyCoexist`); loopback key reaches infra
  end-to-end (`TestPhaseD_LoopbackKeyReachesInfra`); resolver
  reject-invalid-config + svc: prefix assertions; `ServicePrincipal` mint/reject;
  `JOE_API_KEY` folds into the `server` account
  (`config.TestLoad_EnvOverrides_APIKey`); Phase A no-ungoverned-access invariant
  and Phase A/B/C regressions still green and unchanged.
- Supersedes: nothing — extends D-0006. The single configured API-key principal
  is now removed/folded into the service-account map. Phases E–G remain pending
  (E: remove loopback — gated on A+B, both merged; F: audit; G: captain wiring).
  The loop, the loopback's existence/behaviour, OIDC, and the accessor were NOT
  changed in this phase.
- Status: active. Phase D complete; do not proceed to Phase E without a new prompt.

---

## D-0006 — Identity Phase C: OIDC login + sessions + admin bootstrap + CLI provisioning

- Date: 2026-05-29
- Decision: Phase C (joe-identity-design.md §2.1–§2.3, §2.9;
  joe-identity-phase-plan.md Phase C) replaces the SOURCE of the human principal
  with a real OIDC-authenticated identity, without changing the Phase B
  set machinery (the PrincipalSet stays size 1; no `group:` members are
  populated). A human logs in via a single configurable OIDC issuer; the
  verified `email` claim becomes a `user:<email>` principal carried by a
  server-side session cookie, flowing through the SAME context path Phase B
  established (`rbac.WithPrincipal` → `rbac.PrincipalFromContext` → accessor).
  Specifics:
  - **OIDC library: `github.com/coreos/go-oidc/v3` + `golang.org/x/oauth2`.**
    go-oidc handles discovery (`.well-known/openid-configuration`), JWKS
    fetching, and ID-token signature/issuer/audience/expiry verification;
    x/oauth2 handles the authorization-code flow and PKCE (`GenerateVerifier`,
    `S256ChallengeOption`, `VerifierOption`). Chosen because the prompt named
    go-oidc/v3 as the expected choice and because JWKS fetching and signature
    verification must NOT be hand-rolled (design §2.1). The IdP-facing surface
    is an interface (`auth.Provider`) so the flow is testable without a live
    issuer; the production implementation (`auth.NewOIDCProvider`) lazy-inits
    discovery on first use and caches it, so startup does not hard-depend on IdP
    reachability (design §4: IdP unreachable ⇒ only new logins fail).
  - **Single configurable issuer.** `config.AuthConfig.OIDC` carries issuer URL,
    client id, client secret, redirect URL. One code path; GitHub-direct
    (OAuth2, not OIDC) stays out, per design §2.1 caveat.
  - **`user:<email>` derivation + `email_verified` enforcement.** The single
    point where verified OIDC identity becomes a principal is
    `auth.PrincipalFromClaims` → `rbac.UserPrincipal(email)`
    (`internal/auth/claims.go`, `internal/rbac/identity.go`). It rejects with
    `ErrEmailNotVerified` when `email_verified` is absent or not true — the gate
    runs BEFORE any principal is minted, so an unverified token never yields a
    principal or a session. `email_verified` is decoded with a `flexBool` that
    accepts native-bool or string-encoded ("true"/"false", Azure-style) and
    fails closed on anything else. `UserPrincipal` also rejects an email that
    already carries a reserved prefix (`user:`/`group:`/`svc:`) — an
    impersonation guard that does not trigger in practice.
  - **Session model + cookie (design §2.3).** On a successful callback a
    server-side session row is minted in SQLite (`auth_sessions`: id, principal,
    created_at, expires_at — migration 014) and a cookie is set carrying ONLY
    the opaque id. Cookie attributes are exactly **HttpOnly + Secure +
    SameSite=Lax**. Lax (not Strict) is required: Strict would not send the
    session cookie on the cross-site top-level navigation returning from the IdP
    to the callback, so the app would treat the returning user as a new visitor.
    Sessions have a **bounded lifetime** (`auth.session_ttl`, default 12h; a
    non-positive value falls back to a bounded default — never unbounded) and a
    **server-side revocation path** (deleting the row = immediate logout). The
    `SessionManager.Resolve` rejects a session at/after `expires_at` even if the
    row still exists. Server-side sessions were chosen over JWT because joe-core
    is a single non-distributed binary with the DB right there, so statelessness
    buys nothing and costs revocation.
  - **OIDC flow CSRF/PKCE.** Login generates `state`, `nonce`, and a PKCE
    verifier; the in-flight flow (verifier + nonce) is persisted server-side in
    `auth_login_flows` keyed by `state` (migration 014), and a temporary
    HttpOnly/Secure/SameSite=Lax `joe_oidc_state` cookie binds the browser to
    that state. The callback validates query-state == cookie-state (login CSRF),
    loads the single-use flow row (deleted regardless of outcome), completes the
    PKCE exchange, verifies the ID token, and checks the token nonce against the
    flow nonce. The API performs no state-changing GET and logout is POST, per
    the §2.3 CSRF posture.
  - **Edge authentication middleware.** `auth.EdgeAuth` (`internal/auth/middleware.go`)
    replaces the prior `api.BearerAuth` + `rbac.IdentityMiddleware` pair in the
    production chain (`cmd/joe-core/main.go`). It resolves the caller principal
    from a session cookie (humans) or the bearer API key, sets it via
    `rbac.WithPrincipal`, and **rejects unauthenticated requests on protected
    paths with 401 — exactly as today**. The OIDC flow endpoints
    (`/api/v1/auth/`) are public (you cannot require a session to log in). When
    NEITHER an API key NOR OIDC is configured, the edge is in auth-disabled mode
    and behaves exactly as pre-Phase-C (every caller is the configured fallback
    principal; nothing rejected). `rbac.EnforcementMiddleware` remains the
    authoritative source-keyed RBAC gate beneath it (demotion is Phase E). The
    old `BearerAuth`/`IdentityMiddleware`/`APIKeyProvider` remain in the codebase
    (used by the Phase A/B regression tests and unchanged).
  - **Endpoints.** `GET /api/v1/auth/login` (initiate), `GET /api/v1/auth/callback`
    (complete), `POST /api/v1/auth/logout` (revoke + clear cookie), registered by
    `auth.Handlers.RegisterRoutes` only when an issuer is configured.
  - **First-login provisioning + admin bootstrap (design §2.9).** There is no
    user directory: a `user:<email>` principal exists implicitly by being
    referenced by a session and/or policies, so "first login creates the
    binding with ZERO zones" is literally a no-op — a freshly-authenticated user
    has no policy rows and `IsAllowed` denies everything until an operator
    grants zones. The ONLY bootstrap path is the config-designated
    `auth.admin_email`: on every login whose verified email matches it,
    `auth.Provisioner.GrantAdmin` runs. **Admin authority means, concretely, an
    `rbac_policies` grant on EVERY security zone present at grant time** —
    prod-readonly, prod-write, dev-full, unassigned, and regime-control — which,
    because RBAC is zone-scoped and additive/allow-only, yields
    read/query/mutate/delete on every source assigned to any of those zones plus
    the sourceless declare/resolve-incident capabilities. It is idempotent
    (existing grants skipped) and grants only zones existing when it ran (a
    later login picks up newer zones); a grant failure fails the login loudly
    rather than masquerading as a working admin.
  - **CLI provisioning (design §2.9 — CLI only, no admin UI, no NEW admin HTTP
    endpoint).** New `joe zone` subcommand (`cmd/joe/zone.go`): `grant
    --principal <user:|svc:…> --zone <id>`, `revoke --principal … --zone …`,
    `list [--principal …]`. It writes/removes `rbac_policies` rows by opening the
    SQLite DB **directly** (operator-on-host) — this sidesteps the bootstrap
    chicken-and-egg (no already-authorized session is needed to grant the first
    one) and honours "no admin HTTP endpoint" for this phase. Grants are
    validated (zone must exist; principal must carry a reserved prefix) and
    idempotent. Source→zone assignment is unchanged (existing admin API).
- Deviations from the Phase C prompt, with reasons:
  1. **SameSite=Lax tested by attribute assertion, not a true cross-site
     redirect.** An `httptest` harness cannot simulate a real cross-site
     top-level navigation from an IdP origin, so per the prompt's explicit
     allowance the test asserts the cookie is exactly HttpOnly+Secure+Lax and
     documents why Lax (not Strict) is what makes the callback return work
     (`TestSessionManager_CookieAttributes`). The full login→callback flow is
     exercised end-to-end with an injected verified ID token.
  2. **`email_verified` enforcement proven behaviourally (static impractical).**
     Per the prompt's allowance, the prefix/verified invariants are asserted by
     `internal/auth/claims_test.go` (verified→`user:` prefix; false/absent→
     `ErrEmailNotVerified` with no principal; reserved-prefix collision
     rejected) rather than by an AST guard.
  3. **Engine enable-condition widened to include OIDC.** `api.newPolicyEngine`
     and `cmd/joe-core/main.go` now build the policy engine when the API key OR
     OIDC is configured (previously API-key only), so a pure-OIDC deployment is
     enforced. Behaviour for the existing API-key and RBAC-disabled cases is
     unchanged (Phase A/B regression tests still green).
  4. **Edge auth replaces (not augments) BearerAuth+IdentityMiddleware in the
     production chain.** The prompt allows breaking/rebuilding internal
     interfaces; consolidating session + bearer resolution into one middleware
     is cleaner than chaining BearerAuth (which would 401 a cookie-only request
     before session resolution). The old middlewares are retained for the
     regression tests, which construct their own chains and are untouched.
  5. **`group:` reserved but unminted.** Per scope fence 9, the set stays size 1;
     `rbac.PrefixGroup` is reserved for v2 and nothing populates it.
- Basis: joe-identity-design.md §2.1 (single OIDC issuer), §2.2 (`user:<email>`
  + `email_verified` hard rejection + reserved prefixes), §2.3 (server-side
  session + HttpOnly/Secure/Lax + CSRF/PKCE), §2.9 (zero-zone first login,
  config admin bootstrap, CLI provisioning), §4 (IdP-unreachable failure mode);
  joe-identity-phase-plan.md Phase C. Verified against Phase A's accessor
  signature (D-0004) and Phase B's set-shaped path (D-0005). Tests: OIDC
  callback success → session + `user:` principal; `email_verified=false`/absent
  rejected with no session; zero-zone user denied then allowed after a CLI grant
  and still denied elsewhere (`TestPhaseC_OIDCSessionPrincipalReachesAccessor`);
  admin email gains all zones, non-admin none; logout deletes the session
  (immediate); expired session treated as unauthenticated; cookie attribute
  assertion; state/nonce-mismatch rejection; `joe zone` grant/revoke/list +
  validation; Phase A no-ungoverned-access invariant and Phase A/B RBAC
  regressions still green and unchanged.
- Supersedes: nothing — extends D-0005. The single configured API-key principal
  remains usable for machine access and is repurposed for service accounts in
  Phase D. Phases D–G remain pending (D: service-account keys; E: remove
  loopback — gated on A+B, both merged; F: audit; G: captain wiring). The loop,
  the loopback, and service-account API keys were NOT touched in this phase.
- Status: active. Phase C complete; do not proceed to Phase D without a new prompt.

---

## D-0005 — Identity Phase B: set-shaped IsAllowed + real ctx principal

- Date: 2026-05-29
- Decision: Phase B of the identity refactor (joe-identity-design.md §2.7,
  joe-identity-phase-plan.md Phase B) makes the authorization subject a SET of
  principals (union of grants) and confirms the accessor enforces the real
  context-derived caller principal. Behaviour-preserving and still
  single-principal in practice (the set has exactly one member at launch).
  Specifics:
  - **New set type.** `rbac.PrincipalSet` (`= []Principal`) with constructor
    `rbac.NewPrincipalSet(principals ...Principal) PrincipalSet`
    (`internal/rbac/principalset.go`). It is the authorization subject:
    additive / allow-only, no deny rules. At launch every caller constructs it
    with one member — the caller's own principal.
  - **Set-shaped decision function.** `PolicyEngine.IsAllowed` is now
    `IsAllowed(ctx, principals rbac.PrincipalSet, sourceID string, action Action) bool`
    (`internal/rbac/policy.go`). It resolves the source's zone ONCE (zone
    resolution is principal-independent) and the zone-allows-action check once,
    then permits if ANY member holds a policy granting that zone. A
    per-member `ListPoliciesForPrincipal` error denies only that member
    (`continue`) rather than the whole decision; for a size-1 set this is byte-
    identical to the prior single-principal behaviour (immediate deny), which
    is the regression contract.
  - **Set-shaped accessor chokepoint.** `Accessor.permit` is now
    `permit(ctx, principals rbac.PrincipalSet, sourceID, action) error`
    (`internal/access/access.go`). A new private seam
    `permitForPrincipal(ctx, principal rbac.Principal, sourceID, action)` lifts
    the single caller principal into a size-1 set via `rbac.NewPrincipalSet`
    and delegates to `permit`. `guard[T]` (the adapter resolve path),
    `observeResolve` (the category-dispatch sibling), and all `graph.go`
    dispatch methods call `permitForPrincipal`. `permit` remains the single
    place `IsAllowed` is invoked from the accessor. This one seam is where
    Phase C adds `group:` members (from the IdP groups claim) with no change to
    any dispatch method.
  - **Public dispatch signatures unchanged (single principal).** The exported
    `<Family><Operation>(ctx, principal rbac.Principal, …)` methods keep taking
    a single `rbac.Principal` — the context-derived caller principal the
    handlers already pass. The SET is formed inside the accessor at the
    decision boundary, not at the public API. Rationale: Phase B req 2 says
    callers pass "the caller principal" (singular) from context; the Phase A
    action-declaration guard (`internal/access/guard_test.go`) asserts dispatch
    methods take an `rbac.Principal` parameter; and the §B static criterion
    presumes a singular principal crosses the accessor boundary. Keeping the
    public arity also makes Phase B a minimal, attributable diff (≈140 lines).
  - **Context principal threading — already in place from Phase A.** The §B
    goal "thread the real ctx principal into the accessor instead of a
    configured/hardcoded one" required NO new wiring: Phase A's rerouted
    handlers already obtain the principal via
    `rbac.PrincipalFromContext(r.Context())` (the value `IdentityMiddleware`
    sets) and pass it to the accessor. Phase B verifies and locks this with
    tests (below) rather than changing handler code. The mechanism is:
    `IdentityMiddleware` → `PrincipalFromContext` (handler) → public dispatch
    method `principal` arg → `permitForPrincipal` → size-1 `PrincipalSet` →
    `permit` → `IsAllowed`.
  - **Middleware left authoritative (demotion deferred to E).** The HTTP
    `EnforcementMiddleware` is unchanged except that it now lifts its
    context principal into a size-1 set for the set-shaped `IsAllowed`
    (`internal/rbac/middleware.go`). It remains the authoritative gate on the
    HTTP path; the accessor stays shadowed there. Middleware demotion (and the
    accessor becoming load-bearing on HTTP) is Phase E, gated by an equivalence
    test, per design §2.5/§3.
- Deviations from the Phase B prompt, with reasons:
  1. **Threading was confirmation, not new code.** The prompt anticipated
     "replacing any reliance on a single hardcoded or implicitly-configured
     principal at the accessor's callers." Phase A had already context-derived
     the principal at every accessor call site, so there was nothing to
     replace; Phase B's contribution to req 2 is the proof (one behavioural
     test + one static guard), not a wiring change.
  2. **Static criterion expressed behaviourally + a light static guard.** Per
     the prompt's explicit allowance, a precise AST data-flow assertion is
     brittle against Phase A's explicit-principal signature, so the primary
     proof is behavioural — `TestPhaseB_ContextPrincipalReachesAccessorDecision`
     omits `EnforcementMiddleware` (making the accessor the sole gate) and
     injects a non-default principal into the request context; the 200/403
     outcome tracks that principal's grants (alice allowed, mallory denied),
     proving a context-injected principal reaches the accessor's decision. A
     complementary static guard
     (`TestPhaseB_AccessorCallersDerivePrincipalFromContext`) asserts every
     principal-gated accessor call site in `internal/api` reads
     `rbac.PrincipalFromContext` and passes no hardcoded principal
     (string literal / `rbac.Unknown` / `rbac.Principal("…")`). Principal-less
     methods (`GitHubWebhookSecret`, `GitLabWebhookSecret`, `GraphAvailable`)
     are exempt, mirroring the D-0004 allowlist convention.
  3. **`HasZoneAccess` deliberately NOT set-shaped.** The sourceless sibling
     `PolicyEngine.HasZoneAccess` (used by regime declare/resolve in
     `internal/api/regime.go`) is outside the accessor enforcement chain
     (`permit`→`IsAllowed`) and belongs to the regime/captain path (Phases
     F/G). Converting it now would be scope creep into a later phase and touch
     handlers Phase B should not. It stays single-principal; its set-shaping,
     if wanted, lands with the captain/audit work.
- Basis: joe-identity-design.md §2.7 (set-shaped, size-1) / §2.5 (accessor is
  the authoritative point; middleware demotion deferred to E) / §6 (groups drop
  in as set members later); joe-identity-phase-plan.md Phase B. Verified against
  Phase A's accessor signature (D-0004) and the existing context-threading in
  `internal/api`. Tests: rbac union semantics (size-1 granted/ungranted +
  multi-member ANY-granted + empty-set deny + zone-bounded), accessor per-kind
  allow/deny (unchanged, regression through the accessor), HTTP RBAC regression
  (`TestPhaseA_HTTPRBACOutcomesPreserved`, still green ⇒ HTTP outcomes identical
  through the set-shaped path), Phase A no-ungoverned-access invariant (still
  green, unchanged), and the two Phase B principal-threading tests above.
- Supersedes: nothing — extends D-0004. Phases C–G remain pending (C: OIDC +
  sessions + bootstrap; E: remove loopback — gated on A+B, now both merged).
  The loop and loopback were not touched in this phase.
- Status: active. Phase B complete; do not proceed to Phase C without a new
  prompt.

---

## D-0004 — Identity Phase A: guarded accessor below the transport

- Date: 2026-05-29
- Decision: Phase A of the identity refactor (joe-identity-design.md §2.5/§2.8,
  joe-identity-phase-plan.md Phase A) introduces a single guarded accessor as
  the only path to infrastructure adapters and the graph store, with
  `IsAllowed` evaluated inside it. Behaviour-preserving and still
  single-principal. Specifics:
  - **Accessor location & signature.** New package `internal/access`, type
    `*access.Accessor`. Constructor:
    `access.New(registry *adapters.Registry, graphStore graph.GraphStore, engine *rbac.PolicyEngine) *Accessor`.
    Enforcement chokepoint: `permit(ctx, principal rbac.Principal, sourceID string, action rbac.Action) error`
    (nil engine ⇒ permit-all, mirroring `EnforcementMiddleware(nil)`). Generic
    resolve+enforce: `guard[T any](a, ctx, principal, sourceID, action, typeName) (T, error)` —
    the ONLY caller of `registry.Get`. Public dispatch methods are
    `<Family><Operation>(ctx, principal rbac.Principal, sourceID string, …args) (…, error)`
    (e.g. `K8sListResources`, `PrometheusQuery`, `GitReadFile`, `ArgoCDApps`,
    `GraphQuery`); each enforces, then delegates to the resolved adapter/graph
    store and returns its result. On deny it returns `access.ErrPermissionDenied`
    and performs no infra call. Errors: `ErrPermissionDenied`, `ErrSourceNotFound`
    (wraps `store.ErrSourceNotFound` ⇒ 404 preserved), `ErrWrongAdapterType`,
    `ErrGraphUnavailable`. Wired in `api.New` via `newPolicyEngine(services)`,
    which reproduces `cmd/joe-core/main.go`'s enable-condition exactly (engine
    non-nil only when `Server.APIKey != ""`).
  - **Action declared on the method.** Each dispatch method passes its
    `rbac.Action` literal to `guard`/`permit` adjacent to the delegated adapter
    call — not inferred from the HTTP verb. Classification mirrors the prior
    verb mapping for behaviour parity: all current (GET) reads ⇒ `ActionRead`;
    the three GitHub/GitLab POST mutations ⇒ `ActionMutate`. `ActionQuery` is
    supported by the mechanism but assigned to no current method (see deviation
    2). Static guard `internal/access/guard_test.go` asserts every exported
    `*Accessor` method that takes an `rbac.Principal` references an
    `rbac.Action*` constant.
  - **Rerouted call sites (transport only).** All `internal/api` handlers that
    reached adapters/graph directly now go through `s.accessor`/
    `h.server.accessor`: `k8s.go`, `git.go`, `aws.go`, `observability.go`,
    `alerting.go`, `datastore.go`, `networking.go`, `registry.go`, `security.go`,
    `gitops.go`, `review.go`, `observe.go` (graph + category dispatch),
    `webui.go` (graph), `tasks.go` (graph summary), and `server.go` (graph
    routes). The typed `getXxxAdapter` helpers and `handleAdapterLookupError`
    were removed; `helpers.go` now exposes `writeAccessError`/`handleAccessError`.
    The HTTP `EnforcementMiddleware` remains as a coarse OUTER gate; the accessor
    is the authoritative point.
- Deviations from the Phase A prompt, with reasons:
  1. **Static guard scope.** The "no package other than the accessor" invariant
     is enforced repo-wide by `internal/api/access_guard_test.go` (forbids
     `*.Adapters.Get(...)` and `*.Graph.<method>(...)`) with an explicit
     allowlist: `internal/access` (the accessor), `internal/coreagent` (in-process
     Core Agent refresh — its convergence is Phase E; rerouting it now would add
     RBAC to the refresh path and change behaviour), and `cmd/joe-core` (a
     process-level OTel business-metrics gauge reading `graph.Summary`, with no
     caller principal). Registry lifecycle (`Register`/`Unregister`/`List`) and
     `services.Graph == nil` checks are not access and are allowed.
  2. **`ActionQuery` not yet assigned.** Reclassifying graph/PromQL/LogQL reads
     to `query` would deny them on the `unassigned` zone (which allows only
     `read`), changing 200→403 for a principal scoped to that zone. To honour
     "observable behaviour unchanged", reads keep `ActionRead`; semantic `query`
     classification is deferred.
  3. **Graph gating uniformity.** The accessor gates all graph access via the
     reserved `GraphSourceID = "graph"` (→ `unassigned`, `ActionRead`). This
     closes a pre-existing transport quirk where some graph sub-paths were
     ungated (their parsed path segment was empty) while others were gated under
     nonsense sourceIDs; all such sourceIDs resolved to `unassigned`, so the
     decision is identical for the gated ones. Invisible under RBAC-off
     (default) and for any normally-granted principal; only `GET /api/v1/graph`
     (full list) gains a gate it previously lacked.
  4. **Webhook secret reads are unenforced.** `GitHubWebhookSecret`/
     `GitLabWebhookSecret` resolve through the accessor but take no principal
     and run no RBAC: webhook receivers execute pre-auth and authenticate the
     sender via HMAC, so no caller principal exists. The action-declaration
     guard exempts principal-less methods by design.
  5. **Error-precedence micro-change.** Because the accessor bundles
     resolve+execute, handlers validate params before calling it; on a
     doubly-malformed request (bad source AND bad param) a 400 may now precede a
     404 where 404 previously won. Never affects a 200/403 outcome.
  6. **Accessor deny path unreachable via HTTP in Phase A.** Since the unchanged
     middleware uses the same engine and a verb-matched action, it makes the
     identical decision and blocks denied requests first; the accessor's
     enforcement becomes load-bearing in Phase E. Its deny path is proven by
     direct unit tests (`internal/access/access_test.go`), and the HTTP
     regression (`internal/api/access_regression_test.go`) proves
     middleware+accessor == middleware-alone for the configured principal.
- Basis: joe-identity-design.md §2.5/§2.8/§5; joe-identity-phase-plan.md Phase A;
  code verified against migration 006 zone seeds and `cmd/joe-core/main.go`'s
  RBAC wiring. Tests: per-kind allow/deny + no-infra-call-on-deny + nil-engine
  (access pkg), action-declaration + ungoverned-access AST guards, HTTP RBAC
  regression + RBAC-disabled.
- Supersedes: nothing — first identity-refactor decision. Phases B–G remain
  pending (B: set-shaped `IsAllowed`; E: remove loopback — gated on A+B).
- Status: active. Phase A complete; do not proceed to Phase B without a new prompt.

---

## D-0003 — Phase 2 streaming protocol and tool-execution boundary

- Date: 2026-05-28
- Decision: Phase 2 (single agentic runtime, Plan-of-Record §3 / D-0001) is
  implemented with two binding protocol/architecture choices:
  1. **Streaming protocol: Server-Sent Events (SSE).** joe-core → CLI streaming
     of the single agentic loop uses `text/event-stream` with named events
     (`step`, `local_tool_call`, `final`) on `POST /api/v1/tasks/stream`. The
     control direction (CLI → joe-core, delivering delegated tool results) is an
     ordinary `POST /api/v1/tasks/stream/{taskID}/tool-results`, so SSE's
     unidirectionality is not a constraint. Chosen over chunked newline-JSON
     because SSE is self-describing and the existing React Web UI can later
     consume the same endpoint via the browser `EventSource` API — one protocol
     for CLI and Web UI.
  2. **Tool-execution boundary: local tools execute in the CLI (callback path).**
     Local tools (`read_file`, `write_file`, `run_command`, `local_git_*`,
     `ask_user`) keep executing in the `joe` CLI process. The CLI advertises them
     as `client_tools`; joe-core registers delegating stubs so the LLM can call
     them, emits a `local_tool_call` event, and suspends the loop until the CLI
     posts the result. Rationale: preserve the security property that the CLI's
     filesystem/shell access is bounded by the operator's own shell, not by
     joe-core's process (which may run as a daemon/container/remote host).
     Shared (Go-native diagnostic) and core tools run inside joe-core's loop.
- Consequential choices recorded here:
  - **`/model` is now a global operation on the single runtime.** Switching the
    model hot-swaps joe-core's `services.LLM` (a `SwappableAdapter`) for all
    consumers; there is no per-CLI-session model. This is the direct consequence
    of "one LLM contact point"; a per-session override is out of scope because
    it conflicts with "exactly one adapter instantiated process-wide".
  - **The CLI requires no provider API keys.** It makes no LLM calls; keys live
    only in joe-core.
  - **Token accounting simplifies (not implemented here).** With one loop,
    joe-core's loop is the single authoritative tally (`taskResponse.total_tokens`);
    there is no second CLI-side count to reconcile. Token *visibility* is
    deferred polish, explicitly not built in Phase 2.
  - **The agentic loop was relocated** from `internal/useragent` to
    `internal/agentloop` (joe-core-owned). `internal/useragent` no longer exists;
    the CLI's build closure links no adapter-factory, provider, or loop package
    (asserted by a guard test in `cmd/joe`).
- Basis: PLAN-OF-RECORD-RECONCILED.md §3 (Phase 2 + completion gate);
  PHASE-0-SESSION-MODEL.md Invariants 1 and 6; docs/PHASE-2-IMPLEMENTATION-NOTES.md.
- Supersedes: nothing — first streaming/tool-boundary decision. The pre-Phase-2
  CLI-local agentic loop + CLI LLM adapter are removed.
- Status: active. Phase 2 complete.

---

## D-0002 — Phase 0 session model is the normative session-model design

- Date: 2026-02-16
- Decision: `docs/PHASE-0-SESSION-MODEL.md` is the normative output of the
  refactor's Phase 0. Phase 1 implementation is built from it. Its
  accompanying state diagram is a comprehension aid; where the document and
  the diagram disagree, the document governs.
- Scope closed by it: the original six refactor open questions; incident mode
  (emerged during design, load-bearing); the authority-layer integration
  verified against current code (§G of that document).
- Basis: PHASE-0-SESSION-MODEL.md (closed); Security & LLM-Execution
  Architecture Findings (code verification).
- Supersedes: nothing — first normative session-model artifact.
- Status: active. Phase 1 references this document, not chat history.

---

## D-0001 — Refactor Phase 3 deleted; refactor is two phases

- Date: 2026-02-16
- Decision: The agentic-runtime refactor collapses from three phases to two.
  Phase 3 ("collapse the /tasks loopback HTTP-to-self") is **deleted**, not
  merged or rescoped. The end-state acceptance criteria formerly nominal
  under Phase 3 are reattached to Phase 2 as Phase 2's completion gate. No
  work is lost; no work is created.
- Why delete (not merge/rescope): Phase 3's entire scope was collapsing a
  core→core HTTP loopback. Code verification found that loopback does not
  exist (joe-core's Core Agent uses its own `*tools.Registry` directly and
  does not call joe-core's own HTTP API). Merge implies residual Phase 3
  content to fold in — there is none. Rescope would invent work to preserve a
  phase number — the consistency-for-its-own-sake failure mode the project
  rejects (cf. PHASE-0-SESSION-MODEL.md R5). The single-agentic-runtime end
  state is reached as a consequence of Phase 2 (removing the CLI's own loop +
  LLM adapter), with no further step.
- Basis: Security & LLM-Execution Architecture Findings §4;
  PHASE-0-SESSION-MODEL.md §G1/§G2. The phantom phase is recorded here so a
  future reader does not rediscover the absent loopback and re-add a Phase 3
  to "complete" the sequence.
- Supersedes: the prior plan-of-record's "sessions → migrate REPL → collapse
  loopback" three-phase sequencing. The reconciled plan-of-record
  (`docs/PLAN-OF-RECORD-RECONCILED.md`, §5 Reconciliation Record) is the full
  audit trail; this entry is the index pointer to it.
- Status: active. The original plan-of-record file carries a supersession
  header pointing here and to the reconciled plan.
