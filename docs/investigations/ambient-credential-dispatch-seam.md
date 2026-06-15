# Investigation: ambient-credential dispatch seam (A001-CHECK-01)

Read-only diagnosis. Every claim carries exact `file:line` evidence against the
working tree at investigation time. Items not provable from code are labelled
"not determinable from code." No recommendations — diagnosis only.

The `.claude/worktrees/...` copy of the tree was ignored; all paths are the
canonical `internal/...` tree.

================================================================================
VERDICTS
================================================================================

PART A (load-bearing) — Does a read via an ambient/platform credential cross
Accessor.permit and get denied absent a per-component zone grant?

  YES, for every read that flows through the Accessor's public dispatch
  methods. The seam is keyed on the *registered componentID*, NOT on the
  credential's backend reach. `guard` (internal/access/access.go:194) calls
  `permitForPrincipal` (access.go:203) BEFORE `a.registry.Get` (access.go:206);
  on a denied decision it returns `ErrPermissionDenied` and never resolves the
  adapter (access.go:203-205), so the ambient credential is never exercised.
  The decision is `permit(principals, sourceID, action)` (access.go:120) →
  `engine.Decide(ctx, principals, sourceID, action)` (access.go:133), which
  resolves the component's zone and grants from the componentID alone
  (internal/rbac/policy.go:109-167) — entirely independent of whether the
  underlying credential could reach the backend. So a read against component X
  is denied when X has no zone grant, even though the org-wide token sitting in
  X's adapter config would itself succeed at the backend.

  TWO IMPORTANT CARVE-OUTS to "every read":
  (a) Authorization granularity is the *component*, not the backend resource.
      A single grant on a github/datadog/registry component authorizes every
      repo / dashboard / image that component's one credential can reach,
      because the resource selector (owner/repo, query, namespace) is a free
      per-call PARAMETER, not a separate component (e.g. access/vcs.go:57,
      access/observe.go:50, access/k8s.go:12). The seam cannot deny "repo A but
      not repo B under the same github component" — that is exactly the
      scoped-ambient sub-grant the plan defers.
  (b) The autonomous Core Agent background graph-refresh path BYPASSES the seam.
      It resolves the adapter directly from the registry
      (internal/coreagent/refresh.go:172, `r.services.Adapters.Get(source.ID)`)
      and calls `adapter.ListResources(ctx, spec.Resource, "")`
      (internal/coreagent/k8s_refresh.go:62) — a cluster-wide, all-namespaces
      enumerate with NO per-component permit check. This is the documented
      allowlisted exception (internal/api/access_guard_test.go:38,69;
      internal/access/access.go:20-22).

PART B — Does a governed (admin-gated + audited) writer exist for a per-type
policy flag, and would the existing settings surface be a downgrade?

  Governed writer EXISTS: YES. The RBAC admin surface gates every route through
  `requireAdmin` (internal/api/admingate.go:41) and writes a KindAdminAccess
  audit row via `recordAdminAudit` (internal/api/admin.go:158), fail-closed for
  mutations (admin.go:55-65). That is the admin-gate + audit standard.

  Existing settings surface a DOWNGRADE: NO — it is NOT weaker. The Stream G LLM
  settings writers go through the SAME `requireAdmin` gate
  (internal/api/llmsettings.go:226,286,314,347) and the sole audited mutation
  service `MutationService` (internal/llmsettings/service.go:76), which commits
  the value and a KindLLMSettingsMutation audit row in ONE transaction
  (service.go:189-247, audit insert at service.go:227). Both surfaces share the
  identical admin gate and a fail-closed transactional audit. Attaching a
  privilege-affecting flag to the settings surface would NOT be a downgrade
  relative to the RBAC-admin writer. (Caveat: the LLM settings GET is open to
  any authenticated caller, llmsettings.go:13-17 — only the writes are gated.)

  Hot-reload (Part B item 8): policy/config VALUES at the permit decision are
  read LIVE per request. `PolicyEngine.Decide` performs fresh repository reads on
  every call — `GetAssignment` (policy.go:112), `GetZone` (policy.go:121),
  `IsAdmin` (policy.go:137), `ListPoliciesForPrincipal` (policy.go:154). Nothing
  is cached at boot except the engine pointer itself. A value changed at runtime
  through an audited writer would be seen by the next decision without a restart
  (assuming the flag is stored where the engine reads it).

PART C — Complete, exact current component type values.

  Canonical definition: internal/store/constants.go:3-54 (the `ComponentType*`
  const block); enumerated by `AllowedComponentTypes()` (constants.go:57-96) and
  `IsValidComponentType()` (constants.go:99-142). 36 values, listed in full in
  Part C below.

================================================================================
PART A — AMBIENT-CREDENTIAL DISPATCH SEAM
================================================================================

A.0 The seam mechanism (shared by every dispatch method)
--------------------------------------------------------
- The registry is keyed by `sourceID` (componentID): one adapter instance per
  registered component — `adapters map[string]Adapter`
  (internal/adapters/registry.go:13); `Get(sourceID)` (registry.go:25-34).
- `guard[T]` is the single resolve path (internal/access/access.go:194-218):
  it calls `permitForPrincipal` (access.go:203) BEFORE `a.registry.Get`
  (access.go:206). On denial it returns `zero, err` (access.go:203-205) — no
  adapter resolved, no infra touched.
- `permit` is the chokepoint (access.go:120-172): decision via
  `engine.Decide(ctx, principals, sourceID, action)` (access.go:133); writes one
  audit row (access.go:150-166); denial returns `ErrPermissionDenied`
  (access.go:168-170). A nil engine permits all (RBAC disabled, access.go:128-131).
- `observeResolve` is the type-switch sibling of `guard` for the category API; it
  likewise calls `permitForPrincipal` before `registry.Get`
  (internal/access/observe.go:35-47).
- The decision is keyed on (principals, componentID, action) only. The
  credential never enters the decision: `Decide` resolves zone + grants from the
  componentID (internal/rbac/policy.go:109-167).

A.1 Credential model per adapter type
-------------------------------------
The credential is always ADAPTER-RESIDENT: one credential held in the one
adapter instance registered under a componentID. Resolved at `Connect` via the
credential provider abstraction (e.g. internal/adapters/github/adapter.go:86-100;
provider model internal/credential/credential.go:19-41). Whether a *specific*
stored token is org-wide or single-resource is a runtime property of the secret
value — NOT determinable from code. What IS determinable from code is the
"reach shape": whether the per-call resource selector is a free parameter (one
credential addressing many resources) or fixed by the component config (one
credential bound to one backend). Classified on that basis:

COMPONENT-SCOPED (config binds the credential to a single backend endpoint/resource):
- kubernetes — kubeconfig/context/in-cluster → one cluster.
  internal/adapters/k8s/config.go:9-12. (But see A.4: namespace="" enumerates
  the whole cluster.)
- git — url+auth → one repository. internal/adapters/git/config.go:9-14.
- postgresql / mysql / redis / mongodb / kafka / elasticsearch — host+db+creds →
  one instance. e.g. internal/adapters/datastore/postgres/config.go:9-15.
- prometheus / mimir / loki / alertmanager — url + optional bearer → one backend
  endpoint. internal/adapters/observability/prometheus/config.go:10-13;
  internal/adapters/observability/loki/config.go:10-13;
  internal/adapters/alerting/alertmanager/config.go:10-12.

AMBIENT / PLATFORM-SCOPED (one credential reaches many backend resources that are
NOT individually registered; resource chosen by a free per-call parameter):
- github — single `Token` (internal/adapters/github/config.go:13); every read
  takes free `owner, repo` params (internal/adapters/github/adapter.go:130,171,
  223). One token → every repo it can reach.
- gitlab — single `Token` (internal/adapters/gitlab/config.go:12); reads take
  free `projectID` (internal/access/vcs.go:91,99). One token → every project.
- datadog — `api_key` + `app_key` (internal/adapters/observability/datadog/
  config.go:14-15); queries are free-form (internal/access/observe.go:60,84).
  One key pair → the whole Datadog account (all dashboards/metrics/logs).
- pagerduty — `api_key` (internal/adapters/alerting/pagerduty/config.go:13);
  lists all services/incidents (internal/access/observe.go:136). One key → whole
  PD account.
- aws — region + access/secret/role (internal/adapters/aws/config.go:11-15) →
  whole account/region.
- azure — subscription + client secret (internal/adapters/azure/config.go:10-16)
  → whole subscription/resource-group.
- ecr / oci_registry / dockerhub / artifactory — registry creds → all
  repositories in the registry (internal/adapters/registry/ecr/config.go:10-16;
  enumerated by access/registry.go:14,40,66).
- newrelic / splunk / dynatrace — account-level API key → account-wide query
  reach (config structs at internal/adapters/observability/{newrelic,splunk,
  dynatrace}/config.go:10/10/11; dispatch in access/observability.go).
- grafana — config at internal/adapters/alerting/grafana/config.go:10.
- terraform / helm / argocd / nginx-ingress / envoy / falco — not separately
  audited here; argocd/k8s-tier tools route through the same guard/k8s seam.

A.2 Dispatch path for ambient-credential READS
----------------------------------------------
Every read through the Accessor crosses `permit` identically to component-scoped
reads, keyed on componentID. Examples, each `guard[...](..., rbac.ActionRead, ...)`:
- VCS reads: internal/access/vcs.go:57 (GitHubGetPR), :65 (GitHubGetPRDiff),
  :91 (GitLabGetMR), :99 (GitLabGetMRDiff).
- Telemetry (typed): internal/access/observability.go:20 (Prometheus),
  :46 (Loki), :108 (Datadog), :126 (Splunk), :136 (Dynatrace), :154 (NewRelic),
  :64/:82 (Tempo/Jaeger).
- Telemetry (category API): internal/access/observe.go:50 (ObserveMetrics),
  :74 (ObserveLogs), :95 (ObserveTraces), :114 (ObserveAlerts) — all via
  `observeResolve(... rbac.ActionRead)` (observe.go:35-47).
- k8s: internal/access/k8s.go:12,21,30 (all ActionRead via guard).
- Registry enumerate: internal/access/registry.go:14,40,66 (ActionRead via guard).

There is NO separate "platform/org-level client" route that the Accessor exposes
for ambient credentials: github's adapter-level `ListPRs`
(internal/adapters/github/adapter.go:223) is NOT surfaced by any Accessor method
(grep of internal/access, internal/api, internal/client finds no GitHub/GitLab
list-repos/list-PRs accessor). The only org/platform-level enumerates the
Accessor exposes are the registry list-repositories calls (A.4), and they are
seam-gated on the componentID.

The one route that DOES reach the backend without a per-component permit is the
Core Agent refresh (A.4, carve-out b).

A.3 Per-component denial under an ambient credential
----------------------------------------------------
Authorization is keyed on component identity independent of credential reach.
`Decide` (internal/rbac/policy.go:109) computes the outcome from the componentID's
zone assignment (policy.go:112-118) and the principals' grants (policy.go:153-165)
— the credential is never consulted. Because `permitForPrincipal` runs before
`registry.Get` (access.go:203 vs :206), a denied decision returns before the
adapter (and its ambient credential) is ever resolved. So YES: the seam can deny
a read against ONE specific component while the ambient credential remains fully
capable of reading it. Reachability via the credential does NOT authorize the
read. Granularity caveat from the Verdict still applies: the unit of denial is
the component, not the underlying repo/dashboard/namespace.

A.4 Enumerate / list operations
-------------------------------
Platform-wide enumerates that EXIST and CROSS the seam (gated on componentID,
one ungated-internally result set per permitted call):
- Registry "list all repositories": access/registry.go:14 (OCI), :40
  (Artifactory), :66 (ECR) — `guard[...](..., rbac.ActionRead, ...)`. Returns
  every repo the registry credential can see, behind ONE permit on the registry
  component.
- k8s "list across all namespaces": `K8sListResources` with namespace=""
  (access/k8s.go:12 → adapter lists all namespaces, internal/adapters/k8s/
  resources.go:11-13,26). One permit on the k8s component → cluster-wide listing.
- Alert/incident enumerate: ObserveAlerts → alertmanager ListAlerts / pagerduty
  "all services+incidents" (access/observe.go:114-151) behind one permit.

Enumerate that REACHES THE BACKEND UNGATED (does NOT cross the seam):
- Core Agent k8s graph refresh. `refresh.go:172` resolves the adapter directly
  from the registry (`r.services.Adapters.Get(source.ID)`), bypassing the
  Accessor; `k8s_refresh.go:62` then calls `adapter.ListResources(ctx,
  spec.Resource, "")` across deployments/services/configmaps/secrets/namespaces/
  nodes (k8s_refresh.go:21-30) cluster-wide, with NO `permit` call. Documented
  allowlist exception: internal/api/access_guard_test.go:38-51,69;
  internal/access/access.go:20-22. CRD refresh does the same
  (internal/coreagent/crd_refresh.go:106).

NOT a backend enumerate (reads Joe's own DB, not an ambient credential):
- `GET /api/v1/components` / `list_components` reads
  `s.services.Store.Components.List` directly (internal/api/components.go:36-37)
  — registered-component METADATA from SQLite, not a backend call. It does not
  cross `permit`, but it never touches an adapter or ambient credential.

A.5 VCS read vs mutate
----------------------
Exactly THREE ActionMutate VCS ops, all crossing the seam via
`guard[...](..., rbac.ActionMutate, ...)`:
- internal/access/vcs.go:74 (GitHubPostComment), :82 (GitHubRequestChanges),
  :108 (GitLabPostNote). Confirms the reported vcs.go:74 mutate classification.

VCS READS are ALSO seamed and action-classified (NOT ungoverned):
- internal/access/vcs.go:57 (GitHubGetPR), :65 (GitHubGetPRDiff), :91
  (GitLabGetMR), :99 (GitLabGetMRDiff) — each `guard[...](..., rbac.ActionRead,
  ...)`. So both VCS reads and VCS mutates cross Accessor.permit; reads are
  ActionRead, mutates are ActionMutate.
- Two webhook-secret config resolvers take NO principal and are deliberately
  un-gated pre-auth config reads (vcs.go:23,40, rationale vcs.go:14-22) — not
  infrastructure reads.

================================================================================
PART B — GOVERNED CONFIG WRITER FOR A PER-TYPE POLICY FLAG
================================================================================

B.6 Admin-gated audited writer (the RBAC admin path)
----------------------------------------------------
- Admin gate: `requireAdmin` (internal/api/admingate.go:41-76). Reads principal
  from context, consults `services.RBAC.IsAdmin` (admingate.go:60); non-admin /
  read-error → 403, fail-CLOSED (admingate.go:61-74). Auth-disabled permit when
  `!RBACEnabled` (admingate.go:47-49).
- Audit write: every admin route writes a KindAdminAccess row via
  `recordAdminAudit` (internal/api/admin.go:158), mutations fail-CLOSED — no row
  ⇒ no mutation (admin.go:55-65,136-157). Denials also audited
  (`recordAdminDenial`, admingate.go:99-113).
- Build-time invariants force the gate + audit onto any new admin route
  (admin.go:51-53,63-65): `admin_gate_guard_test.go`, `admin_audit_guard_test.go`.
This is the admin-gate + audit standard a privilege-affecting flag must match.

B.7 Existing settings-storage surface controls (Stream G)
---------------------------------------------------------
- Gate: the LLM settings WRITES go through the SAME `requireAdmin`
  (internal/api/llmsettings.go:226 SetActiveModel, :286 SetCostLimit, :314
  SetRunawayCeiling, :347 SetContextBudget).
- Audit: the sole write path is `MutationService` (internal/llmsettings/
  service.go:62-90), which opens a tx, reads prior value, writes new value, and
  writes ONE KindLLMSettingsMutation audit row via `audit.InsertTx` in the SAME
  transaction (service.go:189-247; insert at service.go:227-234); any failure
  rolls back both rows (service.go:199-205,235-241). Service refuses to
  construct without an audit sink (service.go:88-89).
- Standard match: gate is identical (`requireAdmin`); audit is fail-closed and
  transactional, the same posture as the RBAC admin writer.
- PLAIN ANSWER: attaching a privilege-affecting flag to the existing settings
  surface would be a DOWNGRADE relative to the RBAC-admin writer? NO. The write
  controls are equivalent (same admin gate, fail-closed transactional audit).
  Caveat: the settings GET is unauthenticated-read-open to any authenticated
  caller (llmsettings.go:13-17,144) — only WRITES are gated; that is a read-
  exposure difference, not a write-governance downgrade.

B.8 Hot-reload reality for policy values
----------------------------------------
- Read live per request. `PolicyEngine.Decide` (internal/rbac/policy.go:109)
  issues fresh repository reads every call: `GetAssignment` (policy.go:112),
  `GetZone` (policy.go:121), `IsAdmin` (policy.go:137),
  `ListPoliciesForPrincipal` (policy.go:154). No boot-time cache of values; the
  only boot-resolved thing is the engine pointer (nil-ness), which is the
  intended boot-only property (access.go:128-131; access.go:70-74). A per-type
  flag changed at runtime through an audited writer would be observed by the
  next permit decision with NO restart, PROVIDED the flag is stored where the
  decision path reads it live (the current decision path reads the rbac
  repository tables; a new flag's storage location is hypothetical and not
  determinable from code).

================================================================================
PART C — AUTHORITATIVE COMPONENT TYPE ENUM
================================================================================

Canonical source: internal/store/constants.go:3-54 (`ComponentType*` consts).
Full enumerations: `AllowedComponentTypes()` constants.go:57-96 and
`IsValidComponentType()` constants.go:99-142 (both list the same 36 values).

COMPLETE current list (constant → string literal, with defining line):
   1. ComponentTypeAWS            "aws"            constants.go:4
   2. ComponentTypeAzure          "azure"          constants.go:5
   3. ComponentTypeGit            "git"            constants.go:6
   4. ComponentTypeKubernetes     "kubernetes"     constants.go:7
   5. ComponentTypePrometheus     "prometheus"     constants.go:9
   6. ComponentTypeMimir          "mimir"          constants.go:10
   7. ComponentTypeLoki           "loki"           constants.go:11
   8. ComponentTypeTempo          "tempo"          constants.go:12
   9. ComponentTypeJaeger         "jaeger"         constants.go:13
  10. ComponentTypeDatadog        "datadog"        constants.go:14
  11. ComponentTypeSplunk         "splunk"         constants.go:15
  12. ComponentTypeDynatrace      "dynatrace"      constants.go:16
  13. ComponentTypeNewRelic       "newrelic"       constants.go:17
  14. ComponentTypeCloudWatch     "cloudwatch"     constants.go:18
  15. ComponentTypeAzureMonitor   "azuremonitor"   constants.go:19
  16. ComponentTypeAlertmanager   "alertmanager"   constants.go:21
  17. ComponentTypePagerDuty      "pagerduty"      constants.go:22
  18. ComponentTypeGrafana        "grafana"        constants.go:23
  19. ComponentTypePostgreSQL     "postgresql"     constants.go:26
  20. ComponentTypeMySQL          "mysql"          constants.go:27
  21. ComponentTypeRedis          "redis"          constants.go:28
  22. ComponentTypeMongoDB        "mongodb"        constants.go:29
  23. ComponentTypeKafka          "kafka"          constants.go:30
  24. ComponentTypeElasticsearch  "elasticsearch"  constants.go:31
  25. ComponentTypeArgoCd         "argocd"         constants.go:34
  26. ComponentTypeTerraform      "terraform"      constants.go:35
  27. ComponentTypeHelm           "helm"           constants.go:36
  28. ComponentTypeNginx          "nginx-ingress"  constants.go:39
  29. ComponentTypeEnvoy          "envoy"          constants.go:40
  30. ComponentTypeFalco          "falco"          constants.go:43
  31. ComponentTypeOCIRegistry    "oci_registry"   constants.go:46
  32. ComponentTypeDockerHub      "dockerhub"      constants.go:47
  33. ComponentTypeArtifactory    "artifactory"    constants.go:48
  34. ComponentTypeECR            "ecr"            constants.go:49
  35. ComponentTypeGitHub         "github"         constants.go:52
  36. ComponentTypeGitLab         "gitlab"         constants.go:53

Observations a per-type flag author should not assume away:
- String literals are NOT always the constant suffix lower-cased: nginx-ingress
  is "nginx-ingress" (constants.go:39), OCIRegistry is "oci_registry"
  (constants.go:46), AzureMonitor is "azuremonitor" (constants.go:19). A flag
  keyed on these must use the literal, not a derived name.
- Aliasing: "mimir" shares the prometheus adapter, and "dockerhub" shares the OCI
  adapter (adapter mapping at internal/api/components.go:68-69 for prometheus/
  mimir; per-type registry adapters at internal/coreagent/refresh.go:347 for
  oci/dockerhub). A per-type flag keyed by type would treat these as distinct
  keys even though they share a backend adapter.
- Types present in the enum that have NO live adapter constructor in the
  type→adapter map `newAdapterForType` (internal/api/components.go:58-...):
  notably "cloudwatch" (constants.go:18) and "azuremonitor" (constants.go:19)
  appear in the type enum but are not constructed there — they are accepted as
  valid component types yet have no adapter wired, so a per-type flag could key
  on a type that never produces a backend-reaching adapter. (Exact set of
  unconstructed types beyond these two: verify against the full switch in
  components.go before relying on it — not exhaustively enumerated here.)
- No type values were found referenced in code outside this constants.go set;
  any additional types named only in design docs are NOT present in the tree
  (the tree's authoritative set is exactly the 36 above).

================================================================================
EVIDENCE INDEX (primary anchors)
================================================================================
- Seam: internal/access/access.go:120 (permit), :133 (Decide), :168-170 (deny),
  :194-218 (guard, :203 permit-before-:206-Get), :180-182 (permitForPrincipal).
- Registry keying: internal/adapters/registry.go:13,25-34.
- VCS: internal/access/vcs.go:57,65,74,82,91,99,108 (reads ActionRead /
  3 mutates ActionMutate).
- Telemetry: internal/access/observability.go:20,46,108,126,136,154;
  internal/access/observe.go:35-47,50,74,95,114.
- k8s: internal/access/k8s.go:12,21,30; internal/adapters/k8s/resources.go:11-13,26.
- Registry enumerate: internal/access/registry.go:14,40,66.
- Seam bypass (Core Agent): internal/coreagent/refresh.go:172;
  internal/coreagent/k8s_refresh.go:62; internal/coreagent/crd_refresh.go:106;
  internal/api/access_guard_test.go:38-51,69.
- Policy live reads: internal/rbac/policy.go:109-167.
- Admin gate/audit: internal/api/admingate.go:41-113; internal/api/admin.go:55-65,
  136-158.
- LLM settings gate/audit: internal/api/llmsettings.go:226,286,314,347;
  internal/llmsettings/service.go:62-90,189-247.
- Credential model: internal/credential/credential.go:19-41;
  internal/adapters/github/adapter.go:86-100; per-type config.go files cited inline.
- Component types: internal/store/constants.go:3-142.
