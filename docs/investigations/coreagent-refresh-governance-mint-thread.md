# A001-COREGOV · CC-01 — Mint + Thread agent:core: dependency re-verification

Read-only investigation. No code edited. Citations are against the current tree
(branch `main`). This note re-verifies ONLY the three new dependencies that
minting + threading the `svc:` principal `agent:core` introduces. The settled
ground (refresh.go:172 raw `Adapters.Get`, no permit; no principal today;
guard seam ordering; `svc:` kind exists; live policy reads) is treated as
established and cited, not re-derived.

---

## ⚠️ CONTRADICTION / EXPANSION of settled ground — read first

One item in the brief understates the surface. The brief frames the
discovery-**write** path as a *separate* consumer of `agent:core`. In the
current tree there is **no distinct discovery-write subsystem**: the autonomous
graph mutations attributed to "discovery-write" are performed **inside the
refresh cycle itself**, by every `*_refresh.go` file, via
`ApplyGraphDelta(ctx, r.services.Graph, delta)` calling
`graph.GraphStore.AddNode` / `AddEdge` on the **raw** `services.Graph` store —
NOT through the Accessor (`internal/coreagent/alerting_refresh.go:59,188,228`,
`aws_refresh.go:212`, `azure_refresh.go:180`, `datastore_refresh.go:86,124`,
`git_refresh.go:61,151,180`; helper `internal/coreagent/graphdelta.go:119`).

Consequences for the build:

1. The "single shared principal consumed by both paths" assumption is **sound
   in intent but the two paths are not two seams — they are one loop** that
   today touches infrastructure adapters (read) AND the internal graph store
   (write) under the *same* unprincipled `ctx`. Threading `agent:core` once onto
   the refresh `ctx` covers both, which is *easier* than the brief implies, not
   harder.
2. The graph-store writes are also **outside the Accessor** today, not just the
   adapter read at refresh.go:172. The access-guard allowlist explicitly covers
   this: `internal/api/access_guard_test.go:38-55` allowlists the entire
   `internal/coreagent/` prefix, and its VERDICT-A note (lines 44-55) records
   that the refresh side mutates "only the INTERNAL graph store via
   graph_add_node / graph_add_edge" and that the allowlist must be revisited
   "should the refresh path ever gain a mutating call." `AddNode`/`AddEdge` are
   in the guarded `graphMethods` set (same file, lines 74-79). So the design's
   "refresh.go:172 will resolve through an `*access.Accessor`" only re-homes the
   *adapter-read* half; the graph-**write** half is a second, currently-raw
   call site that the same principal+Accessor work will eventually need to
   cover. Flagged, not relitigated.

Nothing else contradicts the settled ground.

---

## Q1 — Principal mint + hold site

**Mechanism shape.** Principal-in-context is `rbac.WithPrincipal(ctx, p)` →
stores under private `principalKey{}` (`internal/rbac/middleware.go:33-40`); read
back via `rbac.PrincipalFromContext(ctx)`, defaulting to `rbac.Unknown`
(middleware.go:15-20). `WithPrincipal` is already exported for non-`rbac`
callers (middleware.go:31-35 — `coreagent.DurableExecutor` uses it today).

**What a constructed `svc:` value looks like.** `rbac.ServicePrincipal(name)`
is the single mint point; it trims, rejects empty / reserved-prefix names, and
returns `Principal("svc:" + name)` (`internal/rbac/identity.go:62-74`). So
`agent:core` → `rbac.ServicePrincipal("agent:core")` →
`Principal("svc:agent:core")`. (`agent:core` contains no reserved prefix, so it
passes the guard.) `PrefixSvc = "svc:"` is reserved exactly so this can be
provisioned (identity.go:28).

**Existing boot-time `svc:` mint precedent to mirror.**
`auth.NewServiceAccountResolver` (`internal/auth/serviceaccount.go:36-57`) is
the precedent: at boot it loops configured accounts and mints each through
`rbac.ServicePrincipal(sa.Name)` (line 40), holding the results in a map. It is
constructed once in boot at `cmd/joe/server.go:671`
(`saResolver, saErr := auth.NewServiceAccountResolver(...)`). This is the
pattern to mirror: mint once at boot via `rbac.ServicePrincipal`, hold the value.

**Boot wiring around the Core Agent / Refresher.**
- The server `ctx` originates from `signal.NotifyContext` in
  `cmd/joe/main.go:276`, flows into `deps.runServer(ctx)` (main.go:616) →
  `runServer(ctx)` (server.go:176) → `runServerWithDeps(ctx, deps)`
  (server.go:205).
- The Core Agent is constructed at `cmd/joe/server.go:610`
  (`coreAgent := deps.newCoreAgent(services, llmAdapter, metrics)`), which calls
  `coreagent.New` (`internal/coreagent/agent.go:60`), which constructs the
  Refresher via `NewRefresher(services, ...)` (agent.go:82 →
  `internal/coreagent/refresh.go:59`).
- The **same** boot `ctx` is handed to the agent at
  `coreAgent.Start(ctx)` (server.go:639). `Start` forwards it to
  `refresher.Start(ctx)` (agent.go:94), which wraps it with
  `context.WithCancel(ctx)` and runs `refreshLoop(loopCtx)`
  (refresh.go:76-78). Every refresh tick's `ctx` derives from this one boot
  context. **Nothing on this path calls `rbac.WithPrincipal`** — confirmed; the
  ctx flows unwrapped, matching the settled ground (agent.go:71-74 note still
  reads "the agent:core principal does not yet exist").

**Most natural single ownership point.** Mint **once at boot**, not per tick.
The cleanest seam: stamp it onto the context handed to the agent at the
`coreAgent.Start(ctx)` call (server.go:639) — i.e. mint
`p, _ := rbac.ServicePrincipal("agent:core")` near the existing `saResolver`
mint (server.go:671) and pass `rbac.WithPrincipal(ctx, p)` into `Start`. That
single wrap propagates through `Start → WithCancel → refreshLoop → refresh →
refreshComponent` to the consumption point at refresh.go:172 with **no
per-method threading** — the principal rides the context that is already
plumbed end-to-end.

> **Reachable as-is?** The *context plumbing* is reachable as-is (one
> ctx already flows boot → :639 → refresh.go:172). What is missing is the mint
> call + the single `WithPrincipal` wrap at :639. No new parameter needs to be
> threaded through any function for Q1 — the principal travels in the ctx that
> is already there.

---

## Q2 — Accessor construction + reachability

**Constructor.**
`access.New(registry *adapters.Registry, graphStore graph.GraphStore, engine *rbac.PolicyEngine, auditRepo audit.Repository) *Accessor`
(`internal/access/access.go:90-92`). Four deps: the adapter registry, the graph
store, the policy engine (nil ⇒ RBAC disabled, permit-all), and the audit repo
(nil ⇒ audit skipped). The guarded permit runs **before** `registry.Get`
(access.go:203-206), keyed on `(principal, sourceID, action)` — confirmed.

**Every non-test construction site today.** Exactly **one**:
`internal/api/server.go:59`
(`accessor := access.New(services.Adapters, services.Graph, newPolicyEngine(services), auditRepo)`),
held privately on the `*api.Server` struct (`server.go:62`) and wrapped into the
in-process core client (`server.go:63`). All other `access.New` calls are tests
(`internal/access/*_test.go`, `internal/api/access_*_test.go`,
`test/integration/rbac_test.go`).

**What the Refresher holds today.** Only the **raw** registry. The Refresher
struct carries `services *core.Services` (`internal/coreagent/refresh.go:48`);
`core.Services.Adapters` is `*adapters.Registry` (`internal/core/services.go:52`)
and `core.Services.Graph` is `graph.GraphStore` (services.go:42). refresh.go:172
calls `r.services.Adapters.Get(...)` directly; the graph writes call
`r.services.Graph.*` directly. **The Refresher does not hold, and cannot
currently reach, an `*access.Accessor`** — the only one constructed lives inside
`*api.Server`, which is built *after* the Core Agent (api server at
server.go:655, agent at server.go:610) and is not injected back into
`core.Services` or the agent.

**Minimal threading needed.** Two viable seams; both require new wiring (NOT
reachable as-is):

- **Construct a dedicated Accessor for `agent:core`.** Call `access.New` a
  second time at boot with the same `services.Adapters`, `services.Graph`,
  `newPolicyEngine(...)`/`rbac.NewPolicyEngine(services.RBAC)`, and
  `services.Audit`, then hand that `*access.Accessor` to the Refresher (e.g. via
  a new field on the Refresher or on `core.Services`, set before
  `coreAgent.Start`). The accessor itself is principal-agnostic — it reads the
  principal from ctx via `permitForPrincipal`/`PrincipalFromContext`
  (access.go:180-182) — so a "dedicated agent:core accessor" is really just *an*
  accessor consumed under the agent:core ctx from Q1. This is the lower-coupling
  option: it avoids ordering the agent construction after the api server.
- **Reuse the existing `api.Server` accessor.** Requires reordering boot so the
  accessor (or `*api.Server`) exists before the agent, then injecting it — more
  invasive given the current `agent-then-api` order (server.go:610 vs :655).

> **Reachable as-is?** **No.** The Refresher holds only the raw
> `services.Adapters` (`*adapters.Registry`) and raw `services.Graph`
> (`graph.GraphStore`); it must be given an `*access.Accessor`. Minimal seam:
> construct one at boot (reuse `access.New` with `services.Adapters/Graph`,
> `newPolicyEngine`, `services.Audit`) and thread the `*access.Accessor` into
> the Refresher via a new field. Types involved: `*access.Accessor`,
> `*adapters.Registry`, `graph.GraphStore`, `*rbac.PolicyEngine`,
> `audit.Repository`. (Seam named; code not written.)

---

## Q3 — Discovery-write path principal status

**Where the writes happen / under what principal.** The autonomous graph
mutations run inside the refresh cycle (see the contradiction section): every
`*_refresh.go` builds a delta and calls
`ApplyGraphDelta(ctx, r.services.Graph, delta)` →
`graph.GraphStore.AddNode/AddEdge` (`internal/coreagent/graphdelta.go:119`;
call sites e.g. `git_refresh.go:61`, `aws_refresh.go:212`,
`alerting_refresh.go:59`). The `ctx` is the same unprincipled refresh ctx from
Q1. There is **no `rbac.WithPrincipal` anywhere on the coreagent path**, and the
writes do not go through the Accessor at all — they hit `services.Graph`
directly, which the access-guard test explicitly allowlists for `coreagent`
(`internal/api/access_guard_test.go:38-55`, graph methods at lines 74-79).

So the discovery-write path is **equally unprincipled** as the refresh adapter
read. It is not a second subsystem carrying its own principal; it is the *write
half of the same loop*, and it carries no principal today.

**Single shared mint, from one site — confirmed correct, with a caveat.**
Because both halves share one `ctx` derived from the boot context handed at
`coreAgent.Start(ctx)` (server.go:639), minting `agent:core` **once** and
stamping it on that ctx (Q1) makes *both* the adapter read (refresh.go:172) and
the graph writes observe the same principal — no per-path stamping needed. The
"single shared principal" intent is therefore **confirmed, not contradicted**,
at the principal layer.

The caveat is at the *enforcement* layer, not the principal layer: routing the
adapter read through an Accessor (the brief's stated CC-scope) does **not** by
itself route the graph **writes** through one. If the design's
`auto_promote_reads` floor is meant to also govern the "discovery-write path,"
that write half is a **separate, still-raw `services.Graph` call site** that a
later unit must move under the Accessor (or otherwise gate) — the principal is
shared, but the guarded seam is not yet shared. Flagged per the brief's
"flag any mismatch" instruction.

> **Reachable as-is?** The *principal* will be shared as-is once Q1's single
> ctx-stamp lands (no extra mint, no per-path threading). The *write-path
> enforcement* is **not** reachable as-is — graph writes bypass the Accessor
> (`graphdelta.go:119` on raw `services.Graph`) and are out of scope for the
> read-only refresh.go:172 re-homing.

---

## Ordering / dependency note for the build

- **Hidden prerequisite for the mint unit (CC-02):** the mint itself has **no
  hidden prerequisite** — `rbac.ServicePrincipal` (identity.go:62) and
  `rbac.WithPrincipal` (middleware.go:33) both exist and are exported; the
  precedent (`auth.NewServiceAccountResolver`, serviceaccount.go:36) shows the
  boot-mint pattern. CC-02 can mint + stamp at `coreAgent.Start(ctx)`
  (server.go:639) using only existing APIs.
- **But the mint is decoupled from the Accessor work:** stamping the principal
  (Q1, trivial, no threading) and giving the Refresher an `*access.Accessor`
  (Q2, requires a new field + a second `access.New` at boot, or a boot reorder)
  are **independent units**. The principal can land first and be inert (read by
  nothing on the path) until the Accessor is threaded; or the Accessor can be
  threaded first and run under `rbac.Unknown` until the principal lands. Either
  order compiles. The natural order is **mint+stamp (Q1) → thread Accessor
  (Q2) → re-home refresh.go:172 through it**, so that the first request the
  Accessor ever sees already carries `agent:core`.
- **Q3 confirms the single-shared-principal assumption** at the principal layer
  (one ctx, both halves) and **surfaces one mismatch** at the enforcement layer:
  the graph-write half is a second raw `services.Graph` call site outside the
  Accessor that the read-only refresh.go:172 re-homing does not cover. Build the
  shared principal as planned; do not assume routing the adapter read through
  the Accessor also governs the graph writes.

## CC-06 — orphan-write-path enumeration (post-CC-05)

Once CC-05 routed the refresh adapter read through the floored
`access.ResolveAdapter` (`internal/coreagent/refresh.go` `resolveAdapter` ->
`r.accessor.ResolveAdapter`, denied components return early at
`refreshComponent` before any `refresh*Component` runs), every
`ApplyGraphDelta` graph-write call site was re-enumerated to determine whether
it can be reached WITHOUT a floored adapter read (an ORPHAN) or only downstream
of one (floored-upstream).

**Method.** Every `refresh*Component` method takes an already-resolved typed
`adapter` parameter and is called *exclusively* from `refreshComponent`'s type
switch (`refresh.go:238-419`), which obtains that adapter from the floored
`resolveAdapter`. A denied component yields no adapter, no delta, and no write.
The intermediate helpers (`refreshDataStoreComponent`,
`refreshRegistryComponent`, `applyRegistryDelta`) are themselves only called
from those typed methods.

**Per-site verdict — all floored-upstream, ZERO orphans:**

| ApplyGraphDelta call site | Enclosing fn | Reached only via refreshComponent? | Verdict |
|---|---|---|---|
| registry_refresh.go:175 | applyRegistryDelta (← refreshOCI/DockerHub/Artifactory/ECR) | yes | floored-upstream |
| k8s_refresh.go:197 | refreshK8sComponent | yes | floored-upstream |
| git_refresh.go:61 | refreshGitComponent | yes | floored-upstream |
| observability_refresh.go:64 | refreshPrometheusComponent | yes | floored-upstream |
| observability_refresh.go:176 | refreshLokiComponent | yes | floored-upstream |
| observability_refresh.go:246 | refreshTempoComponent | yes | floored-upstream |
| observability_refresh.go:316 | refreshJaegerComponent | yes | floored-upstream |
| observability_refresh.go:389 | refreshDatadogComponent | yes | floored-upstream |
| observability_refresh.go:486 | refreshSplunkComponent | yes | floored-upstream |
| observability_refresh.go:522 | refreshDynatraceComponent | yes | floored-upstream |
| observability_refresh.go:558 | refreshNewRelicComponent | yes | floored-upstream |
| networking_refresh.go:73 | refreshNginxComponent | yes | floored-upstream |
| networking_refresh.go:127 | refreshEnvoyComponent | yes | floored-upstream |
| datastore_refresh.go:86 | refreshDataStoreComponent (← Postgres/MySQL/Redis/Mongo/Elastic) | yes | floored-upstream |
| datastore_refresh.go:124 | refreshKafkaComponent | yes | floored-upstream |
| azure_refresh.go:180 | refreshAzureComponent | yes | floored-upstream |
| aws_refresh.go:212 | refreshAWSComponent | yes | floored-upstream |
| alerting_refresh.go:59 | refreshAlertmanagerComponent | yes | floored-upstream |
| alerting_refresh.go:188 | refreshPagerDutyComponent | yes | floored-upstream |
| alerting_refresh.go:228 | refreshGrafanaComponent | yes | floored-upstream |
| gitops_refresh.go:69 | refreshArgoCDComponent | yes | floored-upstream |
| gitops_refresh.go:136 | refreshHelmComponent | yes | floored-upstream |
| gitops_refresh.go:206 | refreshTerraformComponent | yes | floored-upstream |

No ORPHAN found — there is no graph-write reachable without a floored adapter
read, so no new soft spot is recorded here (the only graph-write soft spot
remains the no-redaction note in `coreagent-refresh-governance.md`). Per the
A001-COREGOV write-half decision the graph write stays an intentional non-gate:
internal Tier-3 knowledge write, governed upstream by the agent:core read floor,
principal-stamped for audit, no write permit by design.

(Note: the onboarding-tool graph writes in `agent.go:242/317/388` and
`git_refresh.go:151/180` are not `ApplyGraphDelta` sites; they are the same
intentional internal Tier-3 write half and are likewise covered by the
coreagent allowlist entry, not by an infrastructure permit.)

## Key file:line index

- Principal mint: `internal/rbac/identity.go:62-74` (`ServicePrincipal`), `:28` (`PrefixSvc`)
- Context plumbing: `internal/rbac/middleware.go:33-40` (`WithPrincipal`), `:15-20` (`PrincipalFromContext`)
- Boot ctx + agent start: `cmd/joe/main.go:276`, `cmd/joe/server.go:176,205,610,639,671`
- Refresher construction / hold: `internal/coreagent/agent.go:60,82`, `internal/coreagent/refresh.go:48,59,76-78`
- Consumption point (adapter read, no permit): `internal/coreagent/refresh.go:172`
- Accessor constructor: `internal/access/access.go:90-92`; permit-before-Get `:203-206`
- Only non-test Accessor site: `internal/api/server.go:59-63`
- `core.Services` field types: `internal/core/services.go:42` (Graph), `:52` (Adapters)
- Discovery-write (graph mutations on raw store): `internal/coreagent/graphdelta.go:119`;
  `alerting_refresh.go:59,188,228`, `aws_refresh.go:212`, `azure_refresh.go:180`,
  `datastore_refresh.go:86,124`, `git_refresh.go:61,151,180`
- Allowlist exception covering both halves: `internal/api/access_guard_test.go:38-55,74-79`
- svc: boot-mint precedent: `internal/auth/serviceaccount.go:36-57`
