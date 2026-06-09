# Feasibility & Shape Investigation: Unified Authorization / Tool-Execution Architecture

Investigation date: 2026-06-09. Scope: can Joe's current authorization and
tool-execution architecture support, without a structural refactor, a design
where (1) "what Joe can do now" is a single computed decision parameterized by
mode/principal-set/target/action/target-class; (2) effective permission can be a
function of multiple principals; (3) read/mutate is a structural property of the
interface handed to a tool. Every claim below was re-derived against the live
tree; file:line citations are load-bearing. No code was changed.

---

## VERDICT (one paragraph per sub-question)

1. **Single-mutation-seam — REQUIRES STRUCTURAL REFACTOR.** The write floor is
   NOT a single seam. It is enforced only in the agentic tool-execution path
   (`internal/tools/executor.go:215` and `internal/captaingate/captaingate.go:194`).
   At least two managed-system mutation routes reach mutating adapter methods
   without ever passing the floor: the **Review Agent background job**
   (`internal/review/agent.go:194,196` → `cmd/joe/server.go:104-114` →
   `gh.PostComment` directly, bypassing BOTH floor and RBAC accessor) and the
   **REST review endpoints** (`internal/api/review.go:37-38,45` →
   `accessor.GitHubPostComment/RequestChanges/GitLabPostNote`, which carry RBAC
   but NOT the floor). The repo-walk guard test defends only against a *lowering
   symbol* reappearing and against re-deriving the floor from disk — it does NOT
   assert that all mutation routes through the seam.

2. **Action-classification — FEASIBLE-WITH-LOCALIZED-CHANGE (split today).**
   There are TWO independent action notions. The floor's read/mutate is a
   property declared ONCE, bound to the tool *name* in a compile-time map
   (`internal/safety/tier.go:71-261`), consumed by name at the floor check. The
   accessor's `rbac.Action` is a per-call argument passed at every dispatch
   method (`internal/access/*.go`, e.g. `vcs.go:82` passes `rbac.ActionMutate`).
   Making read/mutate a structural interface property is close for the floor side
   (already a declared property) but the adapter side hard-codes the action at
   ~150 call sites; the accessor's generic `guard[T]` is a single seam where the
   shape *could* change, but the action is currently an argument there, not a
   property of `T`.

3. **Accessor signature & principal model — FEASIBLE-AS-IS for set plumbing;
   LOCALIZED-CHANGE for non-additive composition.** The authoritative chokepoint
   is `Accessor.permit` (`internal/access/access.go:120`) → `PolicyEngine.Decide`
   (`internal/rbac/policy.go:109`). It ALREADY takes a `rbac.PrincipalSet`
   (`internal/rbac/principalset.go:19`), so "effective grants = f(multiple
   principals)" can be *expressed at the signature as it exists*. BUT the
   semantics are hard-coded union-only / additive (allow if ANY member grants —
   `policy.go:136-165`). A non-additive composition like intersection (driving
   principal ∩ agent budget) cannot be expressed today and requires changing the
   engine's evaluation loop, not the signature.

4. **Target-class — FEASIBLE-WITH-LOCALIZED-CHANGE (implicit 3-way exists, but
   flat).** The three target-classes are already gated by three *different*
   mechanisms: managed-system mutation → write floor + RBAC `ActionMutate`;
   Joe's own graph/model → classified `ActionRead` (`tier.go:192-203`), floor
   never applies, RBAC gates it as a read on the reserved `graph` component
   (`internal/access/graph.go:18`); authz-config (zones/policies/admins) →
   `Server.requireAdmin` admin-capability gate (`internal/api/admingate.go:41`),
   NOT the floor and NOT per-component RBAC. The distinction exists implicitly
   but is not a single parameter — it is three separate enforcement sites.

5. **Mode — FEASIBLE-WITH-LOCALIZED-CHANGE (value is first-class, not yet at the
   RBAC decision point).** Mode IS a first-class value: the boot-resolved
   `safety.WriteFloor` (`internal/safety/floor.go:28-54`), threaded as
   `services.WriteFloor` and readable via `Up()`/`Reason()` with a typed
   `FloorReason`. It is already read at the executor, the captaingate wrapper, and
   the `GET /mutate-status` API (`internal/api/mutatestatus.go`). But it is NOT
   passed into the RBAC decision (`PolicyEngine.Decide` has no mode parameter).
   If mode became an input to one computed authorization decision, the value
   exists and is threadable — it would need to be threaded to that point.

---

## SECTION 1 — The single-mutation-seam question

### Floor enforcement points (the gated seam, as it exists)

The write floor is a boot-resolved, runtime-immutable value
(`internal/safety/floor.go:28-54`). `ResolveWriteFloor(panicStatePresent,
observationEnvSet)` is pure; boot calls it once (`cmd/joe/server.go:429`) and
threads the result as `services.WriteFloor`. The floor denies ONLY
`safety.ActionMutate` (`floor.go:33-34`, semantics at `tier.go:16-27`).

The floor is checked in exactly two code locations, both on the agentic
tool-execution path:

- `internal/tools/executor.go:215` — `if e.floor.Up() && classification.Class ==
  safety.ActionMutate { return WriteFloorError }`, BEFORE `tool.Execute()` at
  line 281.
- `internal/captaingate/captaingate.go:194` — `if w.floor.Up() { return
  WriteFloorError }`, upstream of the §C gate and of `inner.Execute`.

Both are injected with the same boot-sealed value: `tools.WithWriteFloor`
(`executor.go:114`) and `captaingate.WithFloor` (`captaingate.go:121`).

### Routes that DO pass the floor (agentic paths)

- **User task loop** (`/api/v1/tasks`, `/api/v1/tasks/stream`): composed in
  `internal/api/tasks.go:278-297` — `tools.NewExecutor(... WithWriteFloor(
  services.WriteFloor))`, wrapped by `captaingate.New(...)`. The loop
  (`internal/agentloop/agent.go:309`) only ever calls `ExecuteBatch` on its
  `BatchExecutor` interface (`agentloop/agent.go:89-90`), so every tool call goes
  through the wrapper → executor → floor.
- **Core Agent loop** (autonomous refresh/onboarding): composed in
  `cmd/joe/server.go:685-696` — `captaingate.New(durable, ...,
  WithFloor(services.WriteFloor))` around `DurableExecutor` around the inner
  `*tools.Executor` (which carries the floor via
  `internal/coreagent/agent.go:75`).

For these paths the floor is checked at the executor BEFORE the tool runs,
regardless of whether the tool's downstream client is the HTTP client
(`internal/client/`) or the in-process client (`internal/api/inproc_client.go`).

### Routes that REACH infra mutation WITHOUT the floor (findings)

**FINDING 1A — Review Agent background job bypasses BOTH the floor and the RBAC
accessor.** The Review Agent is constructed with `adapterRegistryOps`
(`cmd/joe/server.go:645-647`) as its GitHub/GitLab ops. Those ops resolve the
adapter DIRECTLY via the registry and call the mutating method with no floor and
no `access.Accessor`:

- `cmd/joe/server.go:104-114` `adapterRegistryOps.GitHubPostComment` → `o.registry.Get(sourceID)` → `gh.PostComment(...)`.
- `cmd/joe/server.go:140-150` `adapterRegistryOps.GitLabPostNote` → `gl.PostNote(...)`.
- Called from `internal/review/agent.go:194` (`a.github.GitHubPostComment`) and
  `:196` (`a.gitlab.GitLabPostNote`), inside `postReview` → `runReview` → `Run`
  (`agent.go:85-110`).
- `Run` is dispatched in a **background goroutine** from the webhook/enqueue
  handlers: `internal/api/review.go:147` `go func() { agent.Run(...) }()`.

Posting a PR/MR review comment is a managed-system mutation (external system,
`tier.go:245-253` classifies the equivalent tools as `ActionMutate`). This route
neither classifies the action nor consults the floor nor calls the RBAC accessor.
Note this is also the documented "Core Agent refresh path" exception's cousin —
but the Review Agent is not on the allowlisted exception described in
`internal/access/access.go:19-22`; it resolves adapters from a *second* registry
wrapper entirely.

**FINDING 1B — REST review mutation endpoints carry RBAC but NOT the floor.** The
HTTP handlers `handleGitHubPostComment` (`internal/api/review.go:428`),
`handleGitHubRequestChanges` (`:461`), and `handleGitLabPostNote` (route at
`:45`) call `h.server.accessor.GitHubPostComment` / `GitHubRequestChanges` /
`GitLabPostNote` (`review.go:451,484` and the GitLab analogue). Those accessor
methods enforce RBAC `ActionMutate` (`internal/access/vcs.go:82,90,124,132`) but
the accessor never checks the floor (it has no floor field —
`access.go:67-92`). `review.go` contains zero floor/`WriteFloor`/`safe_mode`
references (verified by grep). The route comment at `review.go:36` claims "T2/T3
via tool executor — guarded by safety policy", but the handler calls the accessor
directly, not the tool executor, so that comment is inaccurate for a direct REST
caller. On the agentic path the floor is enforced upstream at the executor; a
*direct* authenticated REST call to these endpoints is floor-ungated.

### The floor guard test — what invariant it actually checks

`internal/safety/floor_guard_test.go` contains three guards:

- `TestWriteFloor_NoRuntimeLoweringPath` (`:30`) — walks every production `.go`
  file and fails if a *lowering symbol* reappears (`safeModeActive`,
  `ActivateSafeMode`, `DeactivateSafeMode`, `IsSafeModeActive`,
  `ErrSafeModeActive`, `func Reset(`, `safety.Reset(` — `:35-45`). It defends the
  immutability-by-absence property only.
- `TestWriteFloor_NotReDerivedFromDiskInExecutor` (`:100`) — asserts
  `executor.go` does not reference `ReadPanicState`/`PanicStore`/
  `cluster_panic_state` etc. (`:111-114`). Defends "floor read from boot-sealed
  value, not re-derived".
- `TestPanicState_SingleHomeNoFileConcept` (`:130`) — forbids the panic.state
  *file* concept.

**None of these verify that all mutation routes through the seam.** There is no
guard asserting that every mutating adapter method is reachable only via a
floor-checked executor. (The access package has its own guard — `guard` is the
ONLY caller of `registry.Get`, `access.go:19-22` — but that guard is about the
RBAC seam, and it explicitly allowlists exceptions; it says nothing about the
floor, and `adapterRegistryOps` resolves adapters from outside the access package
entirely.)

---

## SECTION 2 — The action-classification mechanism

There are two distinct, independently-maintained representations of "what kind of
operation is this":

### (a) Floor/safety action — a property bound once to the tool NAME

`safety.ActionClass` is `{ActionRead, ActionMutate}` (`internal/safety/tier.go:14-27`).
Classification is a compile-time map keyed by tool name:
`toolRegistry` (`tier.go:71-261`), read via `ClassifyTool(name)` (`tier.go:265`),
which defaults unknown tools to `ActionMutate` (deny-by-default, `:269-273`).

Consumed by name at three points: `executor.go:199,215` (floor + notify),
`captaingate.go:178-182` (gate bypass for reads), `executor_durable.go:100`
(durability). The action is NEVER passed as an argument here — it is looked up
from the tool's identity. So for the floor, read/mutate is ALREADY a declared
property, just keyed by name rather than by the interface handed to the tool.

Representative origins: `write_file`/`run_command` → `ActionMutate`
(`tier.go:221-222`); `github_comment`/`gitlab_comment`/`github_request_changes` →
`ActionMutate` (`tier.go:252-258`); graph maintenance
(`graph_add_node`/`graph_add_edge`/`graph_update_node`) → deliberately
`ActionRead` (`tier.go:192-194`); all observability/query tools → `ActionRead`.

### (b) RBAC action — a per-call argument at every accessor dispatch method

`rbac.Action` is a string enum `{read, query, mutate, delete, declare_incident,
resolve_incident}` (`internal/rbac/zones.go:8-33`). It is passed EXPLICITLY at
each accessor call site, adjacent to the adapter method it gates. Example call
sites (verified): `internal/access/vcs.go:82` (`rbac.ActionMutate`),
`vcs.go:58` (`rbac.ActionRead`), and ~140 `rbac.ActionRead` arguments across
`access/aws.go`, `k8s.go`, `observability.go`, `datastore.go`, `gitops.go`, etc.
The accessor's package doc states this is by design: "The action for each
operation is declared at the call site of the generic guard / permit helper,
immediately adjacent to the adapter method it gates — NOT inferred"
(`access.go:11-14`).

### Refactor-distance assessment (could read/mutate become an interface property?)

- The single seam where the adapter type is resolved is the generic `guard[T
  any](a, ctx, principal, sourceID, action, typeName)` (`access.go:194`), the
  ONLY caller of `a.registry.Get` (`:206`). This IS the place a structural change
  would land — but today `action` is a parameter to `guard`, not a property of
  `T`. Making it a property of the interface handed in (a Read-declared tool
  handed an adapter surface with no mutating methods) is conceivable at this one
  seam, but the mutating methods are not separated into a distinct interface
  today: e.g. `githubadapter.GitHubAdapter` exposes both `GetPR` (read) and
  `PostComment`/`RequestChanges` (mutate) on one interface
  (`internal/adapters/github/adapter.go:40,187`). So the adapter surface is a
  *single* seam (good) but the read/mutate split is NOT structural in the
  interface today — it is enforced by which action constant the call site passes.
- Net: the floor side is essentially already an interface-adjacent property
  (name-keyed). The adapter/RBAC side would need the typed adapter interfaces
  split into read-only vs mutating sub-interfaces (or the `guard` seam to derive
  the action from `T`), which is localized-to-moderate (one generic seam, but
  ~150 call sites and the adapter interface definitions to restructure).

---

## SECTION 3 — Accessor authorization signature & principal model

### The authoritative chokepoint and its exact signature

- `Accessor.permit(ctx, principals rbac.PrincipalSet, sourceID string, action
  rbac.Action) error` (`internal/access/access.go:120`) — the single enforcement
  chokepoint; performs NO infra access; writes one audit row; returns
  `ErrPermissionDenied` on deny.
- It delegates the decision to `PolicyEngine.Decide(ctx, principals
  PrincipalSet, componentID string, action Action) Decision`
  (`internal/rbac/policy.go:109`). `IsAllowed` (`policy.go:82`) is a thin wrapper
  over `Decide`. `HasZoneAccess(ctx, principals, zoneID, action)`
  (`policy.go:192`) is the componentless variant.
- The caller principal is lifted into the set by `permitForPrincipal`
  (`access.go:180`): `a.permit(ctx, rbac.NewPrincipalSet(principal), ...)`.

So the principal slot is **already a set** (`rbac.PrincipalSet`,
`internal/rbac/principalset.go:19`), the target slot is a `componentID string`,
the action slot is a single `rbac.Action`.

### Empirically: set of principals, evaluated union-only

`Decide` evaluates the set as a UNION of grants (additive / allow-only):

- Admin short-circuit loops the set, allows if ANY member is admin
  (`policy.go:136-146`).
- Grant check loops the set, allows if ANY member holds a policy for the
  component's zone (`policy.go:153-165`); a per-member lookup failure denies only
  that member and `continue`s.
- `PrincipalSet` doc is explicit: "permitted if ANY member holds a matching
  grant (union of grants) ... The model is additive / allow-only — there are no
  deny rules" (`principalset.go:4-8`). At launch every set is constructed size-1
  (`access.go:113-115`, `principalset.go:11-14`).

### Can "effective grants = f(multiple principals)" be expressed here?

- **Multi-principal plumbing: feasible-as-is.** The signature already accepts a
  set; adding a second principal (an agent budget) as another set member requires
  no signature change — the set shape was deliberately built for exactly this
  ("group: members can be added later with no change here", `access.go:114-115`).
- **Non-additive composition (intersection): requires localized engine change.**
  The union is hard-coded into `Decide`'s "return allowed on first matching
  member" loops. An intersection (allow only if the driving principal AND the
  agent budget both grant) cannot be expressed by adding set members — union
  semantics would allow if EITHER grants. Expressing intersection requires
  changing the evaluation in `PolicyEngine.Decide`/`HasZoneAccess` (e.g.
  evaluating two sub-decisions and ANDing them, or introducing a composition
  operator). That is a localized change to one engine file, but it is a semantic
  change, not a signature change — the current set is union-only by construction.

---

## SECTION 4 — Target-class representation

The enforcement is NOT flat — three target-classes are gated by three different
mechanisms, but as three separate sites rather than one parameterized decision:

1. **Mutating the managed system** (live infra + external PR/MR threads): gated
   by the write floor (`executor.go:215`, `captaingate.go:194`) AND by RBAC
   `ActionMutate`/`ActionDelete` in the accessor (`access/vcs.go:82,90,124,132`;
   the only mutating accessor methods in the tree — verified by grep, all in
   `vcs.go`). Mutating tools classified `ActionMutate` (`tier.go:213-258`).

2. **Mutating Joe's own graph/model**: classified `ActionRead`
   (`tier.go:192-203`, with the rationale at `tier.go:180-191`: "they only record
   observed state into Joe's own graph/store ... by the write definition they are
   reads"). Consequence: the floor NEVER gates them (floor only denies
   `ActionMutate`), and "Joe never freezes its own model in safe mode". RBAC
   still gates graph ops, but as reads against the reserved `GraphComponentID =
   "graph"` resolving to the `unassigned` zone (`internal/access/graph.go:10-18`,
   `access.go:37-43`).

3. **Mutating Joe's authz config** (zones / policies / component-zone
   assignments / admins / principals): gated by `Server.requireAdmin`
   (`internal/api/admingate.go:41`), which reads the caller principal and checks
   `services.RBAC.IsAdmin(...)` (`admingate.go`, the `IsAdmin` call), failing
   CLOSED. Every admin route calls it first (e.g. `admin.go:184` `requireAdmin`
   in `listZones`; routes registered `admin.go:100-122` cover
   POST/PATCH/DELETE zones, policies, component-zones, admins, principals). This
   is NEITHER the floor NOR per-component RBAC — it is a distinct
   admin-capability gate. The admin capability itself is `rbac.Admin`
   (`zones.go:88-93`), a property of the principal evaluated by `repo.IsAdmin`
   and short-circuited inside `Decide` (`policy.go:137`).

So there IS already an implicit three-way notion of target-class, but it lives as
three enforcement sites with three mechanisms. Confirmed: authz-config mutation
is gated by the admin capability (`admingate.go:41` → `IsAdmin`), and notably is
NOT gated by the write floor — observation/safe mode does not stop an admin from
editing zones/policies.

---

## SECTION 5 — Mode representation

Mode is the boot-resolved `safety.WriteFloor` (`internal/safety/floor.go:28-54`).
It is resolved once at boot (`cmd/joe/server.go:429`
`safety.ResolveWriteFloor(dbPanicked, observationMode)`) and threaded as
`services.WriteFloor`. The mode value is FIRST-CLASS and readable:

- `WriteFloor.Up() bool` (`floor.go:34`) and `WriteFloor.Reason() FloorReason`
  (`floor.go:37`), with a typed reason enum `{None, Observation, SafeMode}`
  (`floor.go:10-20`).
- Already read at multiple points: the executor (`executor.go:40,215`), the
  captaingate wrapper (`captaingate.go:103,194`), and surfaced on the wire by
  `GET /api/v1/mutate-status` (`internal/api/mutatestatus.go:34-89`, mapping the
  typed reason to `full`/`observation`/`safe_mode`).

But the mode value is NOT an input to the RBAC authorization decision. The floor
is checked at a DIFFERENT seam (the executor/captaingate) than the RBAC decision
(`PolicyEngine.Decide`, which has no floor/mode parameter — `policy.go:109`). The
denial PRECEDENCE between them is enforced by *check ordering* across two seams
(floor checked before the §C gate in captaingate; floor checked before
zone/RBAC/safety in the executor — `executor.go:201-264`), not by a single
computed decision.

If mode became an explicit input to one computed authorization decision: the
value already exists as a first-class, boot-sealed, readable `services.WriteFloor`
and is already threaded to multiple decision-adjacent points. It would need to be
THREADED into the unified decision point (today it is not passed to
`Decide`/`permit`). That is a localized plumbing change, not a re-modeling — the
value is first-class, just not yet at the RBAC chokepoint.

---

## REFACTOR-DISTANCE SUMMARY (cheapest → most expensive to evolve toward target)

1. **Mode as an explicit decision input (Section 5) — CHEAPEST.** The value is
   already a first-class boot-sealed `services.WriteFloor` with `Up()/Reason()`,
   already threaded to executor/captaingate/API. Cost: thread it into the unified
   decision point. No re-modeling.

2. **Multi-principal effective permission, additive case (Section 3 plumbing).**
   The chokepoint already takes a `rbac.PrincipalSet`. Adding an agent-budget
   principal as a set member needs no signature change. Cost: near-zero for the
   union case.

3. **Target-class as a parameter (Section 4).** The three-way distinction already
   exists implicitly (floor / admin-capability / RBAC-read-on-`graph`). Cost:
   unify three enforcement sites under one parameterized decision — modeling +
   rewiring three call sites, but the conceptual distinctions are already drawn.

4. **Non-additive principal composition / intersection (Section 3 semantics) +
   read/mutate as an interface property (Section 2).** Both are localized-to-
   moderate. Intersection requires changing `PolicyEngine.Decide`'s union loop
   (one file, semantic change). The interface-property change has a single seam
   (`guard[T]`, the sole `registry.Get` caller) but needs the typed adapter
   interfaces split into read vs mutate (e.g. `GitHubAdapter` currently mixes
   `GetPR` and `PostComment`) and ~150 call sites adjusted.

5. **One gated mutation seam (Section 1) — MOST EXPENSIVE.** The floor is not a
   single seam today; the Review Agent background job
   (`review/agent.go:194,196` → `server.go:104-114`, no floor, no accessor) and
   the direct REST review endpoints (`api/review.go:428,461` → accessor, no
   floor) reach managed-system mutation off the gated path. Converging on a
   single computed decision means re-routing these mutation paths through one
   seam AND adding a guard test that asserts the seam is the only mutation route
   (none exists today). This is structural — it touches the Review subsystem's
   wiring, the REST surface, and the adapter-resolution boundary.

---

## Appendix — key file:line index

- Floor value/resolution: `internal/safety/floor.go:28-54`; boot
  `cmd/joe/server.go:429`.
- Floor checks: `internal/tools/executor.go:215`;
  `internal/captaingate/captaingate.go:194`.
- Floor guard tests: `internal/safety/floor_guard_test.go:30,100,130`.
- Action class (name-keyed): `internal/safety/tier.go:71-261,265`.
- RBAC action (per-call): `internal/rbac/zones.go:8-33`; e.g.
  `internal/access/vcs.go:58,82`.
- Authoritative chokepoint: `internal/access/access.go:120,180,194`;
  `internal/rbac/policy.go:82,109,192`.
- Principal set (union-only): `internal/rbac/principalset.go:4-27`;
  `internal/rbac/policy.go:136-165`.
- Admin gate (authz-config target-class): `internal/api/admingate.go:41`;
  routes `internal/api/admin.go:100-122`.
- Graph as read-class target: `internal/safety/tier.go:192-203`;
  `internal/access/graph.go:10-18`.
- Mutation bypasses: `cmd/joe/server.go:104-114,140-150,645-647`;
  `internal/review/agent.go:85-110,191-196`; `internal/api/review.go:37-38,45,
  147,428,461,484`.
- Mode surface: `internal/api/mutatestatus.go:34-89`.
</content>
</invoke>
