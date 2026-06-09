```
INVESTIGATION: RBAC read-enforcement on the agentic loop path
Date: 2026-06-08
Scope: /tasks, /tasks/stream (the "/chat" route in the premise no longer
exists — see §0). Read-only investigation; no code changed.
Method: every claim re-derived against the live tree, file:line cited.

================================================================================
VERDICT
================================================================================
The investigation's premise is REFUTED for the adapter-backed majority of
agentic-loop tools, and the true situation is 2c (mixed) — but NOT the mix the
premise describes.

The premise assumes RBAC is keyed on componentID in an HTTP seam that never
fires on /tasks, leaving agentic reads ungoverned. That mental model predates
the Identity refactor. In the live tree the source-keyed HTTP
`rbac.EnforcementMiddleware` is a PURE PASS-THROUGH (Phase E demotion,
internal/rbac/middleware.go:78-83); the single AUTHORITATIVE RBAC gate is the
guarded accessor `internal/access` (access.go:1-23), and BOTH the REST paths and
the in-process agentic loop flow through it. On the agentic path the loop's tools
reach the accessor via the in-process client (internal/api/inproc_client.go),
which reads the real caller principal from context and passes a `sourceID`
(componentID) plus an explicit action to `Accessor.permit`
(access.go:120-172) → `PolicyEngine.Decide` (rbac/policy.go:109-168). Reads are
gated as `rbac.ActionRead`; mutations as `rbac.ActionMutate`. So for every tool
that takes a `component_id` argument, the componentID IS resolvable at execution
time (it is the LLM-supplied tool arg) AND IS passed to the authorization call —
this is neither case 2a (plumbing gap) nor case 2b (no componentID). Those tools
are FULLY GOVERNED, reads included.

The genuine residual read-side gaps — case (b) — are a MINORITY:
  (1) graph_query / graph_related: gated against the reserved sentinel
      componentID "graph" (access.go:43, access/graph.go:18,29), which resolves
      to the "unassigned" zone, NOT the real per-component zones of the nodes
      they return. The returned nodes DO carry `.ComponentID`
      (internal/graph/store.go:58) but it is never consulted for authorization
      or post-filtering. This is the multi-component blind spot.
  (2) Non-adapter service tools — list_components, search_knowledge, and the
      doc co-pilot tools (detect_doc_drift, generate_doc_draft,
      publish_doc_update) — bypass the accessor entirely and are, by the
      inproc client's own admission, "NOT principal-gated today"
      (inproc_client.go:56-62). list_components in particular returns the full
      component inventory (id/type/name) regardless of zone.

Net: agentic reads are governed wherever a real componentID exists (the large
majority); the exceptions are the graph tools (coarse sentinel-key gating) and
the non-adapter service tools (no gate). A fix targets those two cases, not the
adapter tools.

================================================================================
§1  THE ENFORCEMENT SEAM
================================================================================
Accessor authorization method — the real name is `permit` (set-shaped) /
`permitForPrincipal` (single-principal wrapper), which delegate to
`PolicyEngine.Decide`; `PolicyEngine.IsAllowed` still exists but is a thin
boolean wrapper over `Decide` (rbac/policy.go:82-84) used by callers that don't
need the audit detail.

- rbac/policy.go:82-84   `PolicyEngine.IsAllowed(ctx, principals, sourceID, action)`
- rbac/policy.go:109-168 `PolicyEngine.Decide(ctx, principals, sourceID, action)`
                         — resolves the source's zone (default "unassigned",
                         lines 110-118), denies if the zone disallows the action
                         (129-131), admin short-circuit (136-146), else permits
                         if any principal holds a grant for the zone (153-165).
- access.go:120-172      `Accessor.permit(ctx, principals, sourceID, action)`
                         — the single enforcement chokepoint; calls
                         engine.Decide (133), writes one audit row, returns
                         ErrPermissionDenied on deny (168-170). nil engine ⇒
                         allow-all (128-131), mirroring middleware(nil).
- access.go:180-182      `permitForPrincipal` lifts the context principal into a
                         size-1 set and calls permit.
- access.go:194-218      `guard[T]` calls permitForPrincipal then resolves the
                         typed adapter — the generic gated resolve path.

CALL SITES of permit/permitForPrincipal (every accessor dispatch method routes
through one of these; sample, not exhaustive):

AGENTIC-LOOP path (reached via the in-process client, principal from context):
  - access/graph.go:18    GraphQuery        principal=ctx, sourceID=GraphComponentID("graph"), action=Read
  - access/graph.go:29    GraphRelated      principal=ctx, sourceID="graph",       action=Read
  - access/k8s.go:13      K8sListResources  principal=ctx, sourceID=tool arg,      action=Read
  - access/k8s.go:22      K8sGetResource    principal=ctx, sourceID=tool arg,      action=Read
  - access/k8s.go:31      K8sGetPodLogs     principal=ctx, sourceID=tool arg,      action=Read
  - access/vcs.go:58      GitHubGetPR       principal=ctx, sourceID=tool arg,      action=Read
  - access/vcs.go:82      GitHubPostComment principal=ctx, sourceID=tool arg,      action=Mutate
  - (all observability/datastore/gitops/networking/registry dispatch methods
     follow the same shape — each takes sourceID + an explicit ActionRead or
     ActionMutate; see inproc_client.go:114-668 for the full per-method list.)

CATEGORY/OBSERVE/ADMIN REST path (principal from context, sourceID resolved
server-side from the graph before the accessor call):
  - access/observe.go:36  observeResolve    permitForPrincipal(principal, sourceID, action)
  - access/observe.go:51  ObserveMetrics    action=Read   (called from api/observe.go:118)
  - access/observe.go:75  ObserveLogs       action=Read   (api/observe.go:172)
  - access/observe.go:96  ObserveTraces     action=Read   (api/observe.go:216)
  - access/observe.go:115 ObserveAlerts     action=Read   (api/observe.go:271)

Both path families converge on the SAME accessor and the SAME permit chokepoint.
The HTTP middleware is no longer a seam: rbac/middleware.go:78-83 returns
`next` unchanged.

The principal reaching permit on the agentic path is the real caller's:
  - cmd/joe/server.go:805-811  auth.EdgeAuth sets the principal via
                               rbac.WithPrincipal on r.Context().
  - auth/middleware.go:158/166/178  the WithPrincipal write sites.
  - internal/api/tasks.go:191  handleTask runs the loop on
                               context.WithTimeout(r.Context(), timeout) (181).
  - internal/api/tasks_stream.go:146  handleTaskStream — same r.Context()-derived
                               context (136). Neither path detaches to a fresh
                               background context, so the principal survives into
                               the loop and is read by inproc_client.go's
                               rbac.PrincipalFromContext(ctx) at each tool call.

Engine wiring (RBAC active iff a real principal can be established — service
account OR OIDC configured):
  - internal/api/server.go:59      access.New(adapters, graph, newPolicyEngine(services), audit)
  - internal/api/server.go:76-84   newPolicyEngine: nil unless SA or OIDC configured.
  - cmd/joe/server.go:736-739      same predicate at the binary entrypoint.
  When the engine is nil (local/dev, no auth), permit allows everything
  (access.go:128-131) — identical to the REST path; not an agentic-specific hole.

================================================================================
§2  THE AGENTIC PATH'S KNOWLEDGE OF A TARGET COMPONENT AT EXECUTION TIME
================================================================================
One full trace (prometheus_query, the representative single-target adapter tool):

1. agentloop/agent.go:309  a.executor.ExecuteBatch(ctx, toolCallRequests) — the
   loop hands the LLM's tool calls (name + args) to the executor on the
   request-derived ctx.
2. tools/executor.go:188   Executor.Execute(ctx, name, args) — the guarded
   accessor of the loop. Floor check (215, Mutate only), opt-in zone/namespace
   scope checks (222-259), safety policy (261), then tool.Execute (281).
3. tools/core/prometheusquery.go:69-71  the tool reads
   sourceID = args["component_id"] (LLM-chosen) — required, errors if absent (70).
4. tools/core/prometheusquery.go:81/112/128  passes sourceID to the client
   method (PrometheusTargets / PrometheusQueryRange / PrometheusQuery).
5. inproc_client.go:251-254  inProcessCoreClient.PrometheusQuery reads
   rbac.PrincipalFromContext(ctx) and calls
   accessor.PrometheusQuery(ctx, principal, sourceID, …).
6. access.go (PrometheusQuery → guard → permitForPrincipal → permit) →
   engine.Decide(ctx, {principal}, sourceID, ActionRead). RBAC decision made
   HERE, against the real component's zone, BEFORE the adapter is touched.

Empirical conclusion: a componentID DOES exist in scope at the moment the tool
executes — it is the `component_id` argument the LLM supplied, threaded as
`sourceID` all the way into the accessor's permit call. The authorization call
receives it. So for adapter-backed tools the answer is "fully governed."

Adjudication of 2a / 2b / 2c → **2c (mixed)**, decomposed:
  - Adapter-backed tools (k8s, prometheus, loki, tempo, jaeger, datadog,
    splunk, dynatrace, newrelic, alertmanager, pagerduty, grafana, postgres,
    mysql, redis, mongodb, kafka, elasticsearch, argocd, flux, terraform, helm,
    nginx, envoy, istio, cilium, certmanager, keda, opa, crossplane, falco,
    registry/artifactory/ecr, git_read/log/diff, github_*, gitlab_*):
    NEITHER 2a nor 2b — componentID resolvable AND passed; governed.
  - graph_query / graph_related: case (b) at decision time — the real target
    components are not resolved before execution; the accessor authorizes against
    the reserved sentinel "graph" (unassigned zone). A 2a-flavoured opportunity
    exists post-query (returned nodes carry .ComponentID, store.go:58) but it is
    not used.
  - list_components, search_knowledge, detect_doc_drift, generate_doc_draft,
    publish_doc_update: case (b) — free-form / no componentID, and they bypass
    the accessor entirely (inproc_client.go:56-62, 105-110, 535-544, 595-631).

================================================================================
§3  PER-TOOL TARGET RESOLUTION (tools reachable from the loop)
================================================================================
Registry the loop dispatches through: tools.NewCoreRegistry(h.server.inproc, …)
(api/tasks.go:269) → registerCoreTools (tools/default.go:49-148), plus shared
Go-native diagnostic tools (default.go:16-23) that touch no component.

component_id arg presence confirmed by grep over internal/tools/core/*.go and by
the inproc method signatures. Resolution to a componentID for RBAC happens
inside the accessor's permit call (the sourceID passed in).

GOVERNED — takes component_id, resolved → accessor permit(ActionRead/Mutate):
  alertmanager_alerts, argocd_tools, awsec2, awseks, awsrds, awsvpc,
  certmanager_tools, cilium_tools, crossplane_tools, datadogquery,
  dynatracequery, elasticsearch_health, envoy_tools, falco_tools, flux_tools,
  gitdiff, gitlog, gitread, github_pr_get, github_pr_diff, github_comment(M),
  github_request_changes(M), gitlab_mr_get, gitlab_mr_diff, gitlab_comment(M),
  grafana_dashboards, helm_tools, istio_tools, jaegertraces, k8sget, k8slogs,
  kafka_stat, keda_tools, lokiquery, mongodb_stat, mysql_stat, newrelicquery,
  nginx_tools, opa_tools, pagerduty_incidents, postgres_stat, prometheusquery,
  redis_info, registry_tools, splunkquery, temposearch, terraform_tools.
  (M) = mutating → ActionMutate at the accessor AND blocked by the write floor.
  Evidence: prometheusquery.go:39-42,64,69-71 (param+required+read); k8sget.go,
  k8slogs.go carry component_id (grep); accessor read/mutate split in
  access/k8s.go:12-36 and access/vcs.go:57-137.

NOT GOVERNED BY THE ACCESSOR:
  - graph_query (core/graphquery.go:32-43,45-60): param is `query` only, NO
    component_id. → accessor.GraphQuery under sentinel "graph" (graph.go:17-25).
  - graph_related (core/graphrelated.go): `node_id`+`depth`, NO component_id.
    → accessor.GraphRelated under sentinel "graph" (graph.go:28-36).
    Both span MULTIPLE real components; authorized once at coarse "graph"
    granularity. Returned graph.Node has .ComponentID (store.go:58) but it is
    not used to authorize or filter results.
  - list_components (core/listcomponents.go:32-43, Required:[]): param is an
    optional `type` filter only (the "component_id" grep hit is description prose
    at line 29). → inproc ListComponents (inproc_client.go:105-110) hits
    services.Store.Components directly; no accessor, no principal gate. Leaks the
    full inventory.
  - search_knowledge (core/knowledge_search.go): → inproc SearchKnowledge
    (inproc_client.go:535-544) hits services.Knowledge directly; no gate.
  - detect_doc_drift / generate_doc_draft / publish_doc_update: → inproc
    DetectDrift / CreateProposal / PublishProposal (inproc_client.go:595-631);
    drift/drafts/proposals services directly; no accessor. publish_doc_update is
    a Mutate, so the write floor (executor.go:215) still blocks it when up, but
    no RBAC zone decision is made.

================================================================================
§4  THE WRITE FLOOR'S ACTUAL COVERAGE ON THE AGENTIC PATH
================================================================================
- tools/executor.go:215-219  `if e.floor.Up() && classification.Class ==
  safety.ActionMutate { return WriteFloorError }`. The floor gates Mutate ONLY;
  reads (ActionRead) skip this branch entirely.
- Floor injected on the user-task executor at api/tasks.go:280
  (tools.WithWriteFloor) and again on the captaingate wrapper at tasks.go:323
  (captaingate.WithFloor), so floor > incident precedence holds.
- Classification source: safety/tier.go — list_components/graph_query/k8s_get/
  prometheus_query/etc. are ActionRead (tier.go:81-96+); mutating tools are
  ActionMutate.

Is there a read-side gate on the agentic path? YES — but it is NOT the floor.
The floor is mutate-only, exactly as the premise states. The read-side gate is
the ACCESSOR's permit(…, ActionRead) call, reached for every adapter-backed tool
(see §1–§3). So the premise's "reads are ungoverned" is false in general; reads
are ungoverned only for the graph tools (coarse sentinel gate) and the
non-adapter service tools (no gate at all).

Note the executor also has a SECOND, OPT-IN scope gate that fires on both reads
and writes: the `allowedComponents` / `allowedNamespaces` allowlist
(executor.go:222-259), keyed on args["component_id"] / args["namespace"]. This is
NOT principal-derived RBAC — it is populated only when the task REQUEST sets
`config.allowed_zones` (api/tasks.go:282-296; resolveZoneScope returns an empty
result, allowedComponentIDs=nil, when no zones are configured —
tasks.go:743-745, 826). By default it is inert. Treat it as caller-supplied
sandboxing, not as the RBAC gate.

================================================================================
§5  THE CATEGORY/OBSERVE REST PATH (CONTRAST CASE)
================================================================================
The observe REST handlers resolve componentID BEFORE the accessor call, then
call the SAME accessor methods the loop would:

- api/observe.go:32-69  resolveComponentForService(r, service, relation): runs
  accessor.GraphQuery (36) + accessor.GraphRelated (53) to walk a typed graph
  edge (metrics_in / logs_in / traces_in / alerts_in / paged_via) from the
  service node to a backing component, returning node.ComponentID (61-62).
- api/observe.go:94     handleObserveMetrics resolves sourceID via that helper,
  then api/observe.go:118 calls accessor.ObserveMetrics(ctx, principal, sourceID,
  …) → permit(ActionRead). Logs/traces/alerts/k8s handlers follow the same shape
  (148/172, 202/216, 251-254/271, 303/320-351).

So the "working reference pattern" is: resolve service→componentID from graph
edges server-side, then authorize at the accessor. The difference from the
agentic path is ONLY the resolution source — REST derives componentID from a
graph edge; the loop takes it from an LLM-supplied `component_id` arg. The
enforcement seam (accessor permit) is identical. The contrast the premise expects
(REST enforces, loop doesn't) does not hold for adapter-backed tools.

Side note: resolveComponentForService itself relies on the coarse "graph"
gate (GraphQuery/GraphRelated under the sentinel), so the REST observe path
inherits the same graph-read coarseness when discovering the target — but it then
re-authorizes against the resolved REAL componentID, so the final data access is
per-component governed.

================================================================================
SIDE FINDING — lingering "source"/sourceID symbols (D-0021 not changed here)
================================================================================
Per instruction, reported not changed. The accessor and inproc client still use
`sourceID` as the parameter name throughout (access.go:120 permit signature,
access/*.go dispatch methods, inproc_client.go:125+). Audit/Decision fields and
the tool-facing arg are already "component" (audit.Event.ComponentID
access.go:155; tool arg "component_id"; rbac.Decide logs "component_id"
policy.go:115). The reserved graph key constant is GraphComponentID
(access.go:43) but its string value is "graph". These are internal parameter
names only — the external entity is "component" per the rename. No functional
"source" entity remains on these paths.

================================================================================
FIX-SHAPE IMPLICATIONS  (shape only, not a fix)
================================================================================
The adapter-backed tool surface needs NO change — it is already governed. Closing
the real gap is a HYBRID of two narrowly-scoped changes plus one policy decision:

1. Non-adapter service tools (low effort, plumbing-shaped). Route
   list_components, search_knowledge, and the doc co-pilot tools through a
   principal-aware gate. list_components is the clearest leak: today it returns
   every component regardless of zone. Options: (a) post-filter the inventory to
   components in zones the principal can read (reuses Decide per component); or
   (b) add an accessor method that takes the principal and filters. This is a
   per-tool resolution layer because these tools have no single componentID —
   the unit of authorization is "each component in the result set."

2. Graph tools (the HARD case — genuinely multi-component). graph_query and
   graph_related can return nodes spanning many components/zones, and the
   authorization currently collapses to the single sentinel "graph"/unassigned
   zone. Three viable shapes, in increasing fidelity/cost:
     - (a) Keep the coarse gate but make it explicit policy: classify the "graph"
       sentinel's zone deliberately (today it silently rides "unassigned").
       Cheapest; does not stop cross-zone topology disclosure.
     - (b) Post-filter returned nodes by node.ComponentID against the principal's
       readable zones (the data is already on graph.Node, store.go:58). The
       authorization decision moves AFTER the query — a structural change to the
       accessor's "deny before infra touch" contract (access.go:117-119), since
       the graph read must happen first to know what to filter. Per-node Decide
       calls also need caching to avoid N policy lookups.
     - (c) Resolve query targets to components before execution — only feasible
       for the narrow `type:`/id-prefixed query forms; free-text queries can't be
       pre-resolved, so this can't be the sole mechanism.
   The hard constraint: the existing invariant is "no infrastructure/graph access
   before a permit decision." Per-component graph filtering inverts that for
   reads, so the design must either relax the invariant for the graph store
   specifically or accept coarse-grained graph authorization as policy.

3. Decide the desired posture for the "graph" sentinel and for the non-adapter
   knowledge/doc tools explicitly (product/security call): is topology-level and
   knowledge-level read access intended to be zone-scoped at all, or is it
   acceptable that any authenticated principal can read the graph and knowledge
   base? The current behavior is the latter by omission, not by decision.

A pure "plumbing" fix (just pass a componentID that's already in scope) does NOT
apply, because the ungoverned tools are precisely the ones with no single
componentID in scope. The work is a per-tool resolution/filtering layer for the
multi/zero-component tools, plus an explicit policy decision for the graph
sentinel.
```
