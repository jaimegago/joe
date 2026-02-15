# Next Steps Plan (Step-by-Step)

Use this as a checklist. Complete each section fully before moving on.

## Progress Summary

- ✅ **Milestone 1: Core Agent Refresh Loop** - Complete (14 tests passing)
- ✅ **Milestone 2: Onboarding + Control Endpoints** - Complete (10+ tests passing)
- ✅ **Milestone 3: Clarifications System (MVP)** - Complete (20+ tests passing)
- ✅ **Milestone 4: .joe/ Processing and Cache Replay** - Complete (5 new tests passing)
- 🧭 **Current: Phase 6 - Cloud, Observability, Alerting Adapters**

## 5) Phase 6: Cloud, Observability & Alerting Adapters

All new adapters are read-only (T1) by default. Mutations require T3 classification + policy flag.

5.1 ⏳ Core foundations
- Add source types and registry wiring for new adapters
- Add API endpoints + core tools for each adapter
- Add graph edges: `metrics_in`, `logs_in`, `traces_in`, `alerts_in`, `paged_via`, `dashboard_in`, `is_k8s_node`

5.2 ⏳ Cloud adapters
- AWS: EC2, EKS, RDS, VPC (verify coverage + graph mapping) ✅
- Azure: VMs, AKS, Azure SQL, VNets ✅

5.3 ⏳ Observability adapters
- Open source: Prometheus/Mimir (PromQL), Loki (LogQL), Tempo/Jaeger (traces)
- Proprietary: Datadog, Splunk, Dynatrace, New Relic
- Cloud: CloudWatch, Azure Monitor

5.4 ⏳ Alerting + dashboards
- Alertmanager, PagerDuty, Grafana

5.5 ⏳ Safety and hardening
- Credential encryption at rest for `sources.config`
- TLS support for joe↔joecored
- Rate limiting middleware
- Classify each new tool as T1/T2/T3 with policy flags for T2/T3 actions

5.6 ⏳ Network & system diagnostic tools (Go-native, shared)
- Go-native implementations in `internal/tools/shared/` (no CLI shelling out)
- tcp_connect: TCP connectivity check via `net.DialTimeout`
- dns_lookup: DNS resolution via `net.LookupHost`, `net.LookupCNAME`, etc.
- http_request: HTTP probing via `net/http` client
- system_info: Disk, memory, load via `syscall`/`os`/`runtime`
- trace_route: Hop-by-hop path via `golang.org/x/net/icmp`
- Usable by both joe (local) and joecored (server-side perspective)

5.7 ⏳ Data store adapters (read-only diagnostic queries)
- PostgreSQL: pg_stat_activity, pg_stat_user_tables, pg_stat_replication, pg_stat_statements
- MySQL: SHOW PROCESSLIST, SHOW REPLICA STATUS, INNODB_TRX
- Redis: INFO, SLOWLOG GET, CLIENT LIST, DBSIZE
- MongoDB: serverStatus, rs.status, currentOp
- Kafka: topics, consumer group lag, broker metadata
- Elasticsearch: cluster health, indices, shards, node stats
- Graph edges: `stores_in`, `queues_in`

5.8 ⏳ GitOps, CD & IaC adapters
- Argo CD: full REST API adapter (apps, sync status, diff, history)
- Flux: via K8s CRDs (GitRepository, Kustomization, HelmRelease)
- Terraform: state file read, managed resources, outputs, drift detection
- Helm: release listing, status, values, revision history
- Graph edges: `managed_by`, `provisions`

5.9 ⏳ Networking & ingress adapters
- NGINX Ingress: K8s Ingress CRDs + status endpoint
- Envoy: admin API (clusters, config_dump, stats)
- Istio: via K8s CRDs (VirtualService, DestinationRule, Gateway, PeerAuthentication)
- Cilium: CRDs + Hubble API (network policies, endpoint health, flows)
- Graph edges: `ingress_for`, `proxies`, `mesh_for`, `policy_enforces`

5.10 ⏳ K8s CRD-based adapters (low effort — extends K8s adapter)
- cert-manager: Certificate, Issuer, ClusterIssuer (expiry, readiness)
- KEDA: ScaledObject, ScaledJob (scaling targets, triggers)
- OPA/Gatekeeper: ConstraintTemplate, Constraint (violation audit)
- Crossplane: Provider, XRD, Claims (sync status)
- Graph edges: `scaled_by`, `secures`, `policy_enforces`, `provisions`

5.11 ⏳ Security & runtime adapters
- Falco: runtime security events by severity/rule
- Graph edges: `alerts_in`

## 1) Core Agent Refresh Loop (Operational MVP)

1.1 ✅ DONE: Define refresh inputs and outputs
- Inputs
	- Sources from store: id, type, config, status, last_sync_at, last_error.
	- Connected adapter instances from registry (k8s, git, aws).
	- Optional filter: source_id list for manual refresh.
	- Existing graph state for the source (need a way to list nodes/edges by source_id).
- Outputs
	- Graph delta: nodes upserted, nodes deleted, edges upserted, edges deleted.
	- Source sync update: last_sync_at + last_error.
	- Metrics/logs: per-source timing and refresh outcome.
- Minimal node metadata (MVP)
	- Required: id, type, source_id, last_seen.
	- K8s: namespace, name, labels, kind-specific fields (replicas for deployments/statefulsets, selector for services).
	- Git: repo URL, default branch, latest commit hash.
	- AWS: id, type, region, tags.
- Minimal edge metadata (MVP)
	- Required: from, to, relation, confidence, source, created_at.
	- Deterministic edges only (explicit confidence) until clarifications are live.
- Decision needed: node ID scheme
	- Option A (existing tests): kind/namespace/name (example: deployment/prod/payment-svc).
	- Option B (avoid collisions): k8s/{source_id}/kind/namespace/name.
	- Pick one and use it consistently in refresh + graph tests.
	- Decision: Option B (k8s/{source_id}/kind/namespace/name).

1.2 ✅ DONE: Implement source refresh in Core Agent
- Update coreagent refresher to load connected sources and call adapters.
- Add per-source refresh functions (k8s, git, aws if available).
- Add deterministic graph updates (add/update/remove nodes/edges).
- Define per-source refresh contracts (what data to collect in MVP):
	- K8s: list deployments, statefulsets, daemonsets, services, configmaps, secrets, namespaces, nodes.
	- Git: repo metadata (url, branch, head), .joe/ directory detection.
	- AWS: EC2 instances, EKS clusters, RDS instances, VPCs.
- Implement a shared helper that maps source-specific objects to graph nodes/edges.
	- Status: shared graph-delta helpers implemented and tested.
	- Status: K8s refresh fully implemented and tested.
	- Status: Git refresh fully implemented and tested.
✅ DONE: 
1.2a Node and edge mapping rules (MVP)
- Node ID scheme
	- K8s: k8s/{source_id}/{kind}/{namespace}/{name}
	- K8s cluster-scoped: k8s/{source_id}/{kind}/{name} (no namespace)
	- Git repo: git/{source_id}/repo
	- AWS: aws/{source_id}/{service}/{resource_id}
- Node types
	- K8s: deployment, statefulset, daemonset, service, configmap, secret, namespace, node
	- Git: git_repo
	- AWS: ec2_instance, eks_cluster, rds_instance, vpc
- Core node metadata (all)
	- source_id, name, namespace (if any), labels (if any), last_seen
- K8s node metadata
	- kind, api_version, uid, creation_timestamp
	- deployment/statefulset: replicas, selector
	- service: type, selector, ports
	- configmap/secret: data_keys (names only)
	- node: labels, taints (names only), capacity summary
- Git node metadata
	- url, branch, head_commit, repo_path (local)
	- joe_dir_present (bool)
- AWS node metadata
	- region, arn (if available), tags
	- ec2_instance: instance_type, state, vpc_id, subnet_id
	- eks_cluster: version, status, vpc_config.subnet_ids
	- rds_instance: engine, engine_version, status, endpoint
	- vpc: cidr_block, is_default
- Deterministic edges (explicit only)
	- K8s
		- namespace contains workload/service/config: namespace -> deployment/statefulset/daemonset/service/configmap/secret (relation: contains)
		- service selects deployment: service -> deployment (relation: routes_to) when selector matches pod labels
		- workload uses configmap/secret: deployment/statefulset/daemonset -> configmap/secret (relation: references) via envFrom/env/volumes
	- Git
		- repo defines deployment/service (if .joe/ files present): git_repo -> k8s/* (relation: defines) after .joe/ processing
	- AWS
		- eks_cluster in vpc: eks_cluster -> vpc (relation: in_vpc)
		- ec2_instance in vpc: ec2_instance -> vpc (relation: in_vpc)
		- rds_instance in vpc: rds_instance -> vpc (relation: in_vpc)
- Edge defaults
	- confidence: explicit
	- source: k8s_api | git | aws_api
	- context: short string (e.g., selector match, envFrom, vpc_id)

1.3 ✅ DONE: Add diff logic for graph updates
- Decide how to detect changes vs existing graph state.
- Implement add/update/remove behavior and record confidence/source.
	- Status: skip edge deletions when their nodes are deleted (cascade handles).
	- Status: preserve node first_seen on refresh.
	- Status: dedupe desired edges and only upsert when edge metadata changes.

1.4 ✅ DONE: Add structured logging and metrics
- Log refresh start/end, source duration, and errors.
- Record metrics per source and per refresh cycle.

1.5 ✅ DONE: Add unit tests
- Table-driven tests for diff decisions and error handling.
- Mock adapters and graph store to validate updates.
	- Status: graph delta helpers tested (4 tests).
	- Status: k8s refresh mapping tests added and extended (2 tests).
	- Status: git refresh implementation tested (2 tests).

## 2) Onboarding + Control Endpoints

2.1 ✅ DONE: Add onboarding handler
- Implement POST /api/v1/onboarding to trigger Core Agent onboarding.
- Validate payload and return a clear status response.

2.2 ✅ DONE: Add refresh handler
- Implement POST /api/v1/refresh to trigger a manual refresh.
- Support optional source_id filtering if helpful.

2.3 ✅ DONE: Wire handlers to Core Agent
- Inject core agent into API server.
- Ensure context cancellation and timeouts are handled.

2.4 ✅ DONE: Add tests
- HTTP tests for onboarding and refresh routes.
- Validate success and error cases.

## 3) Clarifications System (MVP)

3.1 ✅ DONE: Implement store interactions
- Add read/list/create/update operations for clarifications.
- Define status transitions: pending -> answered/dismissed.
	- Status: All CRUD operations implemented and tested (Create, Get, ListPending, ListByStatus, Answer, Dismiss, MarkNotified).

3.2 ✅ DONE: Add API handlers
- GET /api/v1/clarifications - Lists pending clarifications
- POST /api/v1/clarifications/{id}/answer - Answers a clarification with optional answered_by
- POST /api/v1/clarifications/{id}/dismiss - Dismisses a clarification
	- Status: All endpoints implemented with validation and error handling.

3.3 ✅ DONE: Apply answers to graph
- When answered, apply stored graph operations.
- Record provenance and confidence.
	- Status: ClarificationService executes operations (add_node, add_edge, delete_node, delete_edge).
	- Status: Provenance recorded on nodes with confirmed_by, confirmed_at, clarification_id, confidence.
	- Status: User-confirmed edges marked with UserConfirmed confidence level.
	- Status: Handler calls service after answering, logs failures but doesn't fail response.

3.4 ✅ DONE: Add tests
- Store tests for clarification lifecycle.
- API tests for list/answer/dismiss.
	- Status: TestClarificationRepository (6 subtests) covering create, get, list pending, answer, dismiss, mark notified.
	- Status: TestClarificationsAPI (11 subtests) covering all endpoints and edge cases.
	- Status: TestClarificationService_ApplyAnswer (3 subtests) covering add_node, add_edge, no operations.
	- Status: All tests passing. Total 60+ tests in api/core/store packages.

## 4) .joe/ Processing and Cache Replay

4.1 ✅ DONE: Define cache key and storage
- Extended JoeFileCache model with tool_calls and processed_at fields.
- Added migration 003_joe_file_cache_extensions.up.sql.
- Cache.Get/Set handle nullable tool_calls and processed_at.

4.2 ✅ DONE: Implement .joe/ discovery path
- Created JoeFileService in internal/coreagent/joefile_service.go.
- ProcessJoeFiles() discovers .joe/ YAML files via git adapter.
- Computes SHA256 content hash for cache keying.
- Cache hit: Deserializes and returns cached tool calls (no LLM call).
- Cache miss: Calls LLM for interpretation, caches result.

4.3 ✅ DONE: Execute and persist tool calls
- LLM system prompt interprets .joe/ files and returns tool calls.
- Supports graph_add_node, graph_add_edge, save_onboarding_fact.
- Tool calls serialized as JSON and stored in cache.

4.4 ✅ DONE: Integration into git_refresh
- Refresher now includes joeFileService dependency.
- refreshGitSource calls ProcessJoeFiles and executeJoeFileToolCalls.
- Tool execution handlers: executeAddNode, executeAddEdge, executeSaveOnboardingFact.
- joe_dir_present metadata set based on .joe/ YAML file existence.

4.5 ✅ DONE: Add tests
- TestJoeFileService_CacheHit: Cached content returns without LLM call.
- TestJoeFileService_CacheMiss: Missing cache triggers LLM, stores result.
- TestJoeFileService_HashChange: Changed hash causes re-processing.
- TestJoeFileService_NoJoeFiles: No .joe/ files returns empty tool calls.
- TestJoeFileService_MultipleFiles: Both files processed and cached.
- All cache tests passing (5 new tests).

**Status: ✅ Complete - 70+ tests passing across all packages**

## Suggested Order

- Step 1 (Core Agent refresh loop)
- Step 2 (Onboarding + control endpoints)
- Step 3 (Clarifications system)
- Step 4 (.joe/ processing and cache replay)
