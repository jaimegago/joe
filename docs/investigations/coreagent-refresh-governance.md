# Investigation: Core Agent refresh governance (A001-CHECK-02)

```text
================================================================================
A001-CHECK-02 — Bringing Core Agent background refresh READS under the
per-component RBAC / RO-floor seam the Accessor enforces (observation/safe mode).

Read-only diagnosis. Every claim carries exact file:line evidence against the
working tree at investigation time. Items not provable from code are labelled
"not determinable from code." No recommendations, no design — diagnosis only.
Self-healing / autonomous mutating "full mode" actions are OUT OF SCOPE.
================================================================================

--------------------------------------------------------------------------------
VERDICT (the two load-bearing questions)
--------------------------------------------------------------------------------
(a) Does the Core Agent refresh have a PRINCIPAL identity today?  NO.
    The refresh loop runs under the plain server-lifecycle context. coreAgent
    is started with the run context at cmd/joe/server.go:639 (coreAgent.Start(ctx)),
    which flows unchanged through internal/coreagent/agent.go:90-100 (Start →
    refresher.Start(ctx)), internal/coreagent/refresh.go:73-78 (Start → go
    refreshLoop), :104-121 (refreshLoop), :124-154 (refresh), :157-172
    (refreshComponent) to the adapter resolution at internal/coreagent/refresh.go:172.
    Nothing on this path calls rbac.WithPrincipal (the only context-principal
    setter, internal/rbac/middleware.go:33-40); a grep for WithPrincipal in
    internal/coreagent and the coreAgent start block finds none. Therefore
    rbac.PrincipalFromContext(ctx) on this path returns rbac.Unknown
    (internal/rbac/middleware.go:15-20). The code says so explicitly:
    internal/coreagent/agent.go:71-74 — "the agent:core principal does not yet
    exist." Refresh has NO principal today.

(b) Does refresh READ and PERSIST secret VALUES (not just metadata)?  NO — only
    metadata (key NAMES) is persisted. The VALUES are fetched from the backend
    into memory but are never written to the graph.
      - Fetch: the k8s adapter's ListResources issues a dynamic List with NO
        field selector (internal/adapters/k8s/resources.go:26), so for the
        "secrets" kind it returns the FULL Secret objects, `data` field included
        (base64 values present in obj.Object["data"]).
      - Persist: buildK8sMetadata, for nodeType "configmap"/"secret", stores ONLY
        metadata["data_keys"] = mapKeys(data) (internal/coreagent/k8s_refresh.go:256-259),
        and mapKeys returns the map's KEYS only (k8s_refresh.go:291-297). The
        value side of each data entry is never copied into the node. So what
        lands in the graph is the secret's existence, name, namespace, labels,
        and the LIST OF KEY NAMES — not the secret values.
    Net: a permit floor at component granularity would gate "can refresh read
    component X at all," but even within a granted component the refresh persists
    no secret values today, so secret-value INGESTION is not occurring regardless
    of the grant.

--------------------------------------------------------------------------------
PRINCIPAL IDENTITY
--------------------------------------------------------------------------------

1. Does the refresh path run under any principal?  NO.
   - Entry chain (all carry the same ctx, none wrapped with a principal):
       cmd/joe/server.go:639            coreAgent.Start(ctx)
       internal/coreagent/agent.go:90   func (a *Agent) Start(ctx)
       internal/coreagent/agent.go:94   a.refresher.Start(ctx)
       internal/coreagent/refresh.go:73 func (r *Refresher) Start(ctx) → go refreshLoop(loopCtx)
       internal/coreagent/refresh.go:104 refreshLoop(ctx)
       internal/coreagent/refresh.go:124 refresh(ctx)
       internal/coreagent/refresh.go:157 refreshComponent(ctx, source)
       internal/coreagent/refresh.go:172 adapter, err := r.services.Adapters.Get(source.ID)
   - The Refresher struct holds *core.Services, an llm adapter, logger, metrics,
     interval (internal/coreagent/refresh.go:46-56). It carries NO principal, NO
     identity, NO Accessor — there is nothing in the struct or the ctx to supply
     one.
   - loopCtx is derived purely with context.WithCancel (refresh.go:76); no value
     is attached. So at refresh.go:172, rbac.PrincipalFromContext(ctx) would
     yield rbac.Unknown (internal/rbac/middleware.go:13, :15-20).
   - PLAIN STATEMENT: refresh has no principal today.

2. Is there a system / service-account principal it COULD adopt?  The concept
   exists, but nothing on the refresh path holds or mints one.
   - The svc: machine-identity kind is defined: rbac.PrefixSvc = "svc:"
     (internal/rbac/identity.go:28) and minted by rbac.ServicePrincipal(name)
     (internal/rbac/identity.go:62-74) — the single point that applies the svc:
     prefix.
   - The ONLY minting caller is the edge auth layer: auth.NewServiceAccountResolver
     builds svc:<name> principals from configured config.ServiceAccount entries
     (internal/auth/serviceaccount.go:36-57, mint at :40) and Resolve() maps a
     presented bearer KEY → principal per HTTP request (serviceaccount.go:63-69).
     This is a request-time, key-presentation mechanism; it has no in-process
     producer for a background loop.
   - No Core Agent boot identity is established. grep for "agent:", "svc:server",
     "system:", ServicePrincipal, PrefixSvc across internal/coreagent and
     cmd/joe/server.go finds only the agent.go:73 comment stating the
     agent:core principal "does not yet exist." The JOE-IDBOOT work
     (refuse-to-start on absent identity) gates ADMIN identity configuration, not
     a Core Agent service principal — not determinable from code that any
     boot-minted Core Agent principal exists.
   - PLAIN STATEMENT: a svc: principal type exists and could in principle be
     minted, but the refresh path cannot obtain one today — none is in its
     context, struct, or any boot wiring it touches.

3. Can the RBAC grant model express grants TO a service/system principal?  YES —
   the grant model is principal-string-keyed and kind-agnostic.
   - The grant row is rbac_policies(principal TEXT, zone_id, created_at); the
     lookup the engine uses is ListPoliciesForPrincipal, a plain
     `WHERE principal = ?` string match (internal/rbac/repository.go:518-538).
     Nothing restricts the principal to user:/group:.
   - CreatePolicy inserts whatever principal string is given
     (internal/rbac/repository.go:540-559); the decision side compares
     p.ZoneID == zoneID for ANY principal in the set
     (internal/rbac/policy.go:153-165). A svc:<name> grant is therefore matched
     identically to a user:<email> grant.
   - The admin REST surface explicitly accepts all three reserved kinds for a
     grant principal — user:/group:/svc: (internal/api/admin.go:327, :586,
     referencing rbac.PrefixUser/PrefixGroup/PrefixSvc).
   - PLAIN STATEMENT: grants CAN target a svc: (service/system) principal; the
     model is not user/group-only. CAVEAT: this is moot for refresh today because
     refresh presents NO principal at all (see #1), so no grant row — svc: or
     otherwise — would ever be consulted on that path.

--------------------------------------------------------------------------------
SECRET-CONTENT SCOPE
--------------------------------------------------------------------------------

4. What the k8s refresh READS and WRITES; do SECRET values get persisted?
   - Resource kinds enumerated (internal/coreagent/k8s_refresh.go:21-30):
       deployments, statefulsets, daemonsets, services, configmaps, secrets,
       namespaces, nodes
     plus CRD kinds via refreshK8sCRDs (k8s_refresh.go:187, crd_refresh.go:32-81:
     KEDA scaledobjects, cert-manager certificates, OPA constrainttemplates,
     Cilium ciliumnetworkpolicies, Istio virtualservices, Crossplane composites).
   - Each kind is listed all-namespaces: adapter.ListResources(ctx, spec.Resource, "")
     (k8s_refresh.go:62), and ListResources lists with no field selector
     (internal/adapters/k8s/resources.go:13-32) — so the FULL secret objects
     (including base64 `data` values) are returned into memory.
   - What is STORED for a secret node: buildK8sMetadata builds metadata =
     {name, namespace, labels, api_version, kind, uid, creation_timestamp}
     (k8s_refresh.go:213-236) and, for case "configmap","secret", adds ONLY
     metadata["data_keys"] = mapKeys(data) (k8s_refresh.go:256-259). mapKeys
     returns the KEY NAMES only (k8s_refresh.go:291-297). The data VALUES are
     never read out of the `data` map into the node.
   - FIELD SELECTION QUOTE (k8s_refresh.go:256-259):
       case "configmap", "secret":
           if data, found, _ := unstructured.NestedMap(obj.Object, "data"); found {
               metadata["data_keys"] = mapKeys(data)
           }
   - PLAIN STATEMENT: "refresh reads all secrets in a granted component" means
     metadata + KEY NAMES are persisted; secret VALUES are fetched from the
     backend but NOT persisted to the graph.

5. configmaps and other sensitive-content resources — values or metadata?
   - configmaps: identical to secrets — same case branch, data_keys (key names)
     only, no values (internal/coreagent/k8s_refresh.go:256-259).
   - Workload→configmap and workload→secret linkages are captured as graph EDGES
     keyed by referenced NAME only (extractWorkloadInfo collects volume
     configMap.name / secret.secretName and envFrom/valueFrom configMapRef.name /
     secretRef.name / secretKeyRef.name — internal/coreagent/k8s_refresh.go:371-446;
     edges emitted at k8s_refresh.go:160-184). No key values, no secret values —
     names only.
   - CRD nodes store {name, namespace, kind} only (crd_refresh.go:122-132).
   - Other adapters' refresh reads are metadata/inventory enumerations, not
     secret-content reads (see #7 list): e.g. datastore Topics
     (datastore_refresh.go:72), registry ListRepositories
     (registry_refresh.go:22-98), observability ListServices
     (observability_refresh.go:143-371). Whether any of these adapter calls
     return credential-bearing fields downstream is not determinable from the
     refresh code alone, but the refresh layer persists the enumerated
     names/inventory it builds, not raw payloads.
   - PLAIN STATEMENT: for configmaps (and the secret/configmap reference edges)
     the refresh persists metadata / names only, never values.

6. Where refresh output is PERSISTED, and is there any redaction/filtering?
   - Write path: each per-type refresh builds desiredNodes/desiredEdges, then
     LoadGraphStateForComponent → BuildGraphDelta → ApplyGraphDelta
     (k8s_refresh.go:191-199; graphdelta.go:19-40 load, :44-116 diff, :119-145
     apply). ApplyGraphDelta calls store.AddNode / store.AddEdge / store.DeleteEdge
     / store.DeleteNode directly (graphdelta.go:120-142).
   - There is NO redaction or secret-material filtering in this write path. The
     delta apply persists whatever metadata the build step put on the node,
     verbatim. The reason no secret VALUE reaches the store is upstream and
     incidental: the build step (k8s_refresh.go:256-259) only ever copies
     data_keys, so there is no value in the node to redact. State plainly: no
     redaction layer exists in graphdelta.go; the value-exclusion is a property
     of what buildK8sMetadata chooses to copy, not of any filter.

--------------------------------------------------------------------------------
SEAM-CROSSING SHAPE
--------------------------------------------------------------------------------

7. Every Core Agent adapter-resolution site that BYPASSES the Accessor.
   - There is exactly ONE registry-resolution (Accessor-bypassing) site in the
     Core Agent: internal/coreagent/refresh.go:172
         adapter, err := r.services.Adapters.Get(source.ID)
     A grep for "Adapters.Get" across internal/coreagent returns only this line.
     The resolved adapter is then handed by type-switch (refresh.go:180-372) to
     the per-type refresh functions, which call adapter READ/LIST methods on the
     ALREADY-RESOLVED adapter. crd_refresh.go:106 is NOT a separate registry
     resolution — it calls ListResources on the adapter passed into
     refreshK8sComponent (k8s_refresh.go:187 → refreshK8sCRDs → refreshCRDSpec).
     So the resolution bypass count is ONE (refresh.go:172); the downstream
     backend-read call sites that flow from it are many. CONFIRMED: refresh.go:172
     is the sole resolution site; crd_refresh.go:106 is a downstream read, not a
     second resolution.
   - Backend READ/LIST/ENUMERATE calls flowing from that single resolution (the
     complete governing-change target list; all are reads, none cross permit):
       k8s          ListResources all-namespaces      k8s_refresh.go:62
       k8s (CRDs)   ListResources all-namespaces      crd_refresh.go:106
       git          Log                               git_refresh.go:87
       aws          ListVPCs / ListEKSClusters /
                    ListRDSInstances                  aws_refresh.go:26, :104, :153
       azure        ListVNets / ListVMs /
                    ListAKSClusters / ListSQLDatabases azure_refresh.go:22, :48, :89, :130
       prometheus   Targets                           observability_refresh.go:46
       loki/tempo/
       jaeger       ListServices                      observability_refresh.go:143, :213, :283
       datadog/…    ListActiveServices /
                    ListLogServices                   observability_refresh.go:358, :371
       nginx        ListIngresses                     networking_refresh.go:36
       envoy        Clusters                          networking_refresh.go:106
       argocd       Apps                              gitops_refresh.go:35
       helm         Releases                          gitops_refresh.go:103
       terraform    Resources                         gitops_refresh.go:170
       registry     ListRepositories                  registry_refresh.go:22, :30, :53, :98
       kafka        Topics                            datastore_refresh.go:72
       alertmanager ListAlerts                        alerting_refresh.go:41
       pagerduty    ListIncidents                     alerting_refresh.go:150
     (Other datastore types — postgres/mysql/redis/mongodb/elasticsearch — refresh
     via their adapters from the same refresh.go:172 resolution; each performs
     read/inventory enumeration, not mutation.)
   - This bypass is the documented allowlisted exception: internal/access/access.go:19-22
     ("The in-process Core Agent refresh path … is the single documented
     exception"); asserted by the static guard allowlist in
     internal/api/access_guard_test.go.

8. Concrete INPUT GAP between "refresh calls registry.Get directly" and "refresh
   calls through guard with a principal" (what guard needs that refresh lacks).
   - guard's signature requires a principal and an action, and runs permit BEFORE
     resolving the adapter:
       internal/access/access.go:194-205
         func guard[T any](a *Accessor, ctx, principal rbac.Principal,
                           sourceID string, action rbac.Action, typeName string)
         → a.permitForPrincipal(ctx, principal, sourceID, action)  (access.go:203)
         → a.registry.Get(sourceID)                                (access.go:206)
       internal/access/access.go:180-182  permitForPrincipal → permit(NewPrincipalSet(principal),…)
       internal/access/access.go:120-133  permit → engine.Decide(ctx, principals, sourceID, action)
   - What the permit/guard seam REQUIRES:
       (i)   an *access.Accessor instance (holds registry, graph, engine, auditRepo)
             — internal/access/access.go:67-92.
       (ii)  a rbac.Principal subject — guard's `principal` parameter; permit
             forms a PrincipalSet from it (access.go:180-182) and Decide keys the
             decision on it (rbac/policy.go:109, :153-165).
       (iii) a rbac.Action — e.g. rbac.ActionRead for a refresh enumerate.
       (iv)  the componentID/sourceID.
   - What the refresh path HAS: the componentID (source.ID, refresh.go:172) and an
     implied action (read). It is missing:
       (a) PRINCIPAL — refresh carries none in ctx and none in the Refresher
           struct (refresh.go:46-56; ctx unwrapped from server.go:639). guard's
           `principal` argument has no value to bind.
       (b) ACCESSOR ROUTE — the Refresher resolves adapters via
           r.services.Adapters.Get (the raw *adapters.Registry, refresh.go:172),
           not via an *access.Accessor. There is no Accessor reference on the
           Core Agent refresh path at all; it would have to be supplied to call
           guard.
   - PLAIN STATEMENT of the gap: guard needs (1) a principal identity and (2) an
     Accessor to call through. The refresh path supplies the componentID and the
     (read) action, but has NEITHER a principal NOR an Accessor — it holds only
     the raw registry. Those two missing inputs are the concrete gap. (No fix or
     design is proposed here.)

--------------------------------------------------------------------------------
EVIDENCE INDEX (primary anchors)
--------------------------------------------------------------------------------
- Refresh entry / no-principal chain: cmd/joe/server.go:639;
  internal/coreagent/agent.go:90-100; internal/coreagent/refresh.go:73-78,
  104-121, 124-154, 157-172.
- "agent:core principal does not yet exist": internal/coreagent/agent.go:71-74.
- Principal-in-context machinery: internal/rbac/middleware.go:8-20 (PrincipalFromContext,
  Unknown), :33-40 (WithPrincipal — only setter).
- svc: principal model: internal/rbac/identity.go:19-29 (prefixes), :62-74
  (ServicePrincipal); minting only via internal/auth/serviceaccount.go:36-69.
- Grant model kind-agnostic: internal/rbac/repository.go:496-538
  (ListPolicies / ListPoliciesForPrincipal string match), :540-559 (CreatePolicy);
  decision internal/rbac/policy.go:109-167; admin prefix acceptance
  internal/api/admin.go:327, :586.
- Secret/configmap capture (keys only): internal/coreagent/k8s_refresh.go:21-30
  (kinds), :256-259 (data_keys), :291-297 (mapKeys), :371-446 (reference names);
  full-object fetch internal/adapters/k8s/resources.go:13-32.
- Graph write path / no redaction: internal/coreagent/graphdelta.go:19-40,
  44-116, 119-145.
- Sole registry-resolution bypass + downstream reads: internal/coreagent/refresh.go:172;
  per-type read calls listed in #7; allowlist exception
  internal/access/access.go:19-22, internal/api/access_guard_test.go.
- Guard seam inputs: internal/access/access.go:120-133 (permit/Decide), :180-182
  (permitForPrincipal), :194-218 (guard), :67-92 (Accessor fields / New).
================================================================================
```
