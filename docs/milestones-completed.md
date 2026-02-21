# Milestones 1-7: Completed Phases (Historical Reference)

This document records the completed phases of Joe's development (Phases 1–7). It serves as a historical reference for implementation decisions and architectural patterns established during the build.

**Current Status:** ✅ Phases 1–7 complete

**Current Phase:** Phase 8 - Documentation Co-Pilot (see `CLAUDE.md` for planning)

---

## Progress Summary

- ✅ **Milestone 1: Core Agent Refresh Loop** - Complete (14 tests passing)
- ✅ **Milestone 2: Onboarding + Control Endpoints** - Complete (10+ tests passing)
- ✅ **Milestone 3: Clarifications System (MVP)** - Complete (20+ tests passing)
- ✅ **Milestone 4: .joe/ Processing and Cache Replay** - Complete (5 new tests passing)
- ✅ **Phase 5.5: Action Safety Framework** - Complete (self-protection, policy gate, path sandboxing, command allowlists)
- ✅ **Phase 6: Infrastructure Adapters** - Complete (40+ adapters, 17 graph edge types, AES-256-GCM credential encryption)
- ✅ **Phase 7: Knowledge Store** - Complete (three-tier knowledge model, Confluence/Notion sync, LLM-derived insights, semantic search with embeddings)

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

---

## 5) Phase 5.5: Action Safety Framework

5.1 ✅ DONE: Policy loader and action tiers
- Safety policy loaded from `~/.joe/safety-policy.yaml` with default-deny for T3.
- T1/T2/T3 action tiers enforced by tool executor gate.

5.2 ✅ DONE: Self-protection invariants
- Hardcoded blocks for `~/.joe/` access and dangerous commands (joe/joecored/kill).
- Path normalization, symlink resolution, and case-insensitive checks on macOS/Windows.

5.3 ✅ DONE: Path sandboxing for write_file
- `allowed_directories` enforced with symlink-aware boundary checks.

5.4 ✅ DONE: Subcommand validation for kubectl/helm/argocd
- Compiled-in read-only subcommand allowlists.

5.5 ✅ DONE: T3 notifications
- Blocking pre-execution notification with cancel window; post-execution summary.

---

## 6) Phase 6: Infrastructure Adapters

6.1 ✅ DONE: Core foundations

- 30 source type constants in `internal/store/constants.go`
- Generic adapter registry in `internal/adapters/registry.go`
- 17 graph relation types in `internal/graph/relations.go`: `metrics_in`, `logs_in`, `traces_in`, `alerts_in`, `paged_via`, `dashboard_in`, `is_k8s_node`, `stores_in`, `queues_in`, `managed_by`, `provisions`, `ingress_for`, `proxies`, `mesh_for`, `policy_enforces`, `scaled_by`, `secures`

6.2 ✅ DONE: Cloud adapters

- AWS: EC2, EKS, RDS, VPC — `internal/adapters/aws/`
- Azure: VMs, AKS, SQL databases, VNets — `internal/adapters/azure/`

6.3 ✅ DONE: Observability open-source

- Prometheus/Mimir (PromQL) — `internal/adapters/observability/prometheus/`
- Loki (LogQL) — `internal/adapters/observability/loki/`
- Tempo — `internal/adapters/observability/tempo/`
- Jaeger — `internal/adapters/observability/jaeger/`

6.4 ✅ DONE: Alerting & dashboards

- Alertmanager — `internal/adapters/alerting/alertmanager/`
- PagerDuty — `internal/adapters/alerting/pagerduty/`
- Grafana — `internal/adapters/alerting/grafana/`

6.5 ✅ DONE: Safety & hardening

- AES-256-GCM credential encryption at rest — `internal/store/encrypted_sources.go`
- TLS support for joe ↔ joecored API
- Rate limiting middleware
- T1/T2/T3 tool tier classification enforced at executor gate

6.6 ✅ DONE: Network & system diagnostics (Go-native shared tools)

- `internal/tools/shared/`: `netcheck/`, `dnsquery/`, `httpreq/`, `sysinfo/`, `traceroute/`
- Tools: tcp_connect, port_scan, dns_lookup, http_request, system_info, trace_route
- Used by both joe and joecored; no CLI dependencies

6.7 ✅ DONE: Data store adapters

- PostgreSQL, MySQL, Redis, MongoDB, Kafka, Elasticsearch — `internal/adapters/datastore/`
- Refresh orchestration: `internal/coreagent/datastore_refresh.go`
- Graph edges: `stores_in` (services → DBs), `queues_in` (services → Kafka)

6.8 ✅ DONE: GitOps, CD & IaC adapters

- Argo CD (full REST API) — `internal/adapters/gitops/argocd/`
- Helm (K8s secret-based) — `internal/adapters/packaging/helm/`
- Terraform (state file reader) — `internal/adapters/iac/terraform/`
- Flux: handled via K8s CRD discovery (see 6.10)
- Refresh orchestration: `internal/coreagent/gitops_refresh.go`

6.9 ✅ DONE: Networking & ingress adapters

- NGINX Ingress (K8s CRDs + HTTP status endpoint) — `internal/adapters/networking/nginx/`
- Envoy (admin API: clusters, config, stats) — `internal/adapters/networking/envoy/`
- Istio and Cilium: handled via K8s CRD discovery (see 6.10)
- Refresh orchestration: `internal/coreagent/networking_refresh.go`

6.10 ✅ DONE: K8s CRD-based adapters (dynamic discovery)

- Dynamic CRD discovery in `internal/coreagent/crd_refresh.go`
- KEDA, cert-manager, OPA/Gatekeeper, Cilium, Istio, Crossplane — each mapped to the appropriate graph edge type
- CRD-specific tools: `internal/tools/core/certmanager_tools.go`, `keda_tools.go`, `opa_tools.go`, `crossplane_tools.go`

6.11 ✅ DONE: Security & runtime adapters

- Falco (runtime events via HTTP adapter) — `internal/adapters/security/falco/`
- Tools: `internal/tools/core/falco_tools.go` (falco_alerts, falco_rules)

6.12 ✅ DONE: Proprietary observability vendors

- Datadog — `internal/adapters/observability/datadog/`
- Splunk — `internal/adapters/observability/splunk/`
- Dynatrace — `internal/adapters/observability/dynatrace/`
- New Relic — `internal/adapters/observability/newrelic/`

Status: ✅ Complete — 40+ adapters, all 17 graph edge types wired, test coverage 62–95% across packages

---

## 7) Phase 7: Knowledge Store

7.1 ✅ DONE: Three-tier knowledge model

- `internal/knowledge/knowledge.go`: Tier constants (`TierCurated`, `TierSynced`, `TierDerived`), `Entry` struct with embeddings, confidence, provenance
- `internal/knowledge/service.go`: CRUD with Tier 1 immutability enforced at service layer (not just API)
- `internal/knowledge/repository.go`: SQLite-backed, 2 tables — `knowledge_entries`, `knowledge_sources`
- Migration: `internal/store/migrations/004_knowledge.up.sql`

7.2 ✅ DONE: Semantic search with embeddings

- `internal/knowledge/search.go`: Cosine similarity over float32 vector embeddings
- Supports tier filtering and configurable confidence thresholds
- `internal/knowledge/embeddings/`: LLM-backed embedding generation wrapping existing LLM adapters
- Tracks embedding model name for cache invalidation on model change

7.3 ✅ DONE: Synced sources (Tier 2)

- Confluence adapter: `internal/knowledge/sync/confluence/confluence.go` — Confluence REST API v2, cursor-based pagination
- Notion adapter: `internal/knowledge/sync/notion/notion.go` — Notion REST API, database query + block content extraction
- Background sync coordinator: `internal/knowledge/sync/syncer.go` — polls `knowledge_sources` table, respects per-source `sync_interval_minutes`
- Deduplication via SHA256 content hash (avoids unnecessary re-embeddings)

7.4 ✅ DONE: LLM-derived insights (Tier 3)

- `internal/knowledge/learning/extractor.go`: Analyzes completed sessions, extracts patterns/failure modes/insights via LLM
- Stores entries with provenance metadata (`session_id`, `extracted_at`)
- Deduplicates via `source_type + source_id`; LLM cannot create/modify Tier 1

7.5 ✅ DONE: API, client, and agent integration

- API handlers: `internal/api/knowledge.go` — 10 endpoints (entries CRUD, semantic search, source management, manual sync trigger)
- HTTP client: `internal/client/knowledge.go`
- Core tool: `internal/tools/core/knowledge_search.go` — `search_knowledge` (T1), registered for both User Agent and Core Agent
- Core Agent tool: `save_knowledge_entry` (T2) registered in `internal/coreagent/agent.go`

Status: ✅ Complete — knowledge tiers enforced, Confluence/Notion sync live, semantic search operational, session learning wired

---

## Key Implementation Decisions

For architectural decisions and rationale, see:
- [docs/joe-architecture.md](docs/joe-architecture.md) - Architecture overview
- [docs/joe-dataflow.md](docs/joe-dataflow.md) - Data flow and .joe/ processing
- [docs/security-in-layers.md](docs/security-in-layers.md) - Security posture and Action Safety Framework
- [CLAUDE.md](CLAUDE.md) - Phase planning and standards
