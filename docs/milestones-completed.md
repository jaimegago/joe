# Milestones 1-12: Completed Phases (Historical Reference)

This document records the completed phases of Joe's development (Phases 1–12). It serves as a historical reference for implementation decisions and architectural patterns established during the build.

**Current Status:** ✅ Phases 1–12 complete

---

## Progress Summary

- ✅ **Milestone 1: Core Agent Refresh Loop** - Complete (14 tests passing)
- ✅ **Milestone 2: Onboarding + Control Endpoints** - Complete (10+ tests passing)
- ✅ **Milestone 3: Clarifications System (MVP)** - Complete (20+ tests passing)
- ✅ **Milestone 4: .joe/ Processing and Cache Replay** - Complete (5 new tests passing)
- ✅ **Phase 5.5: Action Safety Framework** - Complete (self-protection, policy gate, path sandboxing, command allowlists)
- ✅ **Phase 6: Infrastructure Adapters** - Complete (40+ adapters, 19 graph edge types, AES-256-GCM credential encryption)
- ✅ **Phase 6.13: Artifact Registries** - Complete (OCI/DockerHub, Artifactory, ECR adapters; registry_query, artifactory_query, ecr_query tools)
- ✅ **Phase 7: Knowledge Store** - Complete (three-tier knowledge model, Confluence/Notion sync, LLM-derived insights, semantic search with embeddings)
- ✅ **Phase 8: Documentation Co-Pilot** - Complete (write adapters for Confluence/Notion/Git, draft generation via LLM + knowledge search, human approval flow, drift detection, proposals API)
- ✅ **Phase 9.1: Emergency Shutdown / Panic Mode** - Complete (REPL `/panic`, CLI `joe panic`, API endpoints, SIGUSR1 signal handler, safe mode persistence, `joe unlock`)
- ✅ **Phase 9.2: MCP Server** - Complete (`joe mcp` subcommand, stdio transport, 8 Joe tools, Claude Code / Cursor integration)
- ✅ **Phase 9.3: RBAC** - Complete (`internal/rbac/`, migration 006, 4 default zones, Admin API, API key identity provider, RBAC enforcement middleware)
- ✅ **Phase 10: Code Review Integration** - Complete (GitHub/GitLab adapters, webhook receiver, review job queue, Review Agent, 7 core tools, `joe review` CLI)
- ✅ **Phase 11: Slack Bot** - Complete (`joe slack` subcommand, Socket Mode, `/joe ask|status|help`, DM + mention handling, Block Kit formatting)
- ✅ **Phase 12: Web UI** - Complete (`ui/`, React 18 + Vite + Tailwind + shadcn/ui, 5 pages, React Flow graph visualization, chat interface)

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
- 19 graph relation types in `internal/graph/relations.go`: `metrics_in`, `logs_in`, `traces_in`, `alerts_in`, `paged_via`, `dashboard_in`, `is_k8s_node`, `stores_in`, `queues_in`, `managed_by`, `provisions`, `ingress_for`, `proxies`, `mesh_for`, `policy_enforces`, `scaled_by`, `secures`, `image_stored_in`, `publishes_to`

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

6.13 ✅ DONE: Artifact registry adapters

- OCI Registry adapter (DockerHub, GHCR, Harbor, Quay, any OCI Distribution Spec v2) — `internal/adapters/registry/oci/`
  - Connect: `GET /v2/` (200 or 401 both valid)
  - Catalog pagination via Link header
  - Manifest fetching with `org.opencontainers.image.revision` label extraction (git commit SHA)
- JFrog Artifactory adapter — `internal/adapters/registry/artifactory/`
  - Connect: `GET /api/system/ping`
  - Lists Docker + Helm repositories (PackageType filter); optional key allowlist
  - Auth via `X-JFrog-Art-Api` header (API key) or HTTP Basic
- AWS ECR adapter — `internal/adapters/registry/ecr/`
  - AWS SDK v2; reuses `buildAWSConfig` credential pattern from existing AWS adapters
  - `DescribeRegistry` for connectivity; paginated `DescribeRepositories`/`DescribeImages`
  - Scan findings summary (`HIGH:N,MEDIUM:N`) included in `ImageDetail`
- Refresh orchestration — `internal/coreagent/registry_refresh.go`
  - Node types: `artifact_registry` (source), `image_repository` (per-repo)
  - Node ID scheme: `registry/<sourceType>/<sourceID>` / `registry/<sourceID>/repo/<repoName>`
  - Name-based `image_stored_in` edge inference against existing deployment/service nodes (`graph.Inferred`)
- New graph relations (2): `image_stored_in` (K8s deployment → image_repository), `publishes_to` (git_repo → image_repository)
- API routes (9) — `internal/api/registry.go`: repos, tags/images, manifest/artifact/image detail per registry type
- HTTP client (9 methods) — `internal/client/registry.go`
- LLM-callable core tools (3, all T1 Observe):
  - `registry_query` — OCI-compatible registries (list repos / list tags / get manifest)
  - `artifactory_query` — Artifactory (list repos / list Docker tags / get artifact info)
  - `ecr_query` — ECR (list repos / list images / get image detail)

Status: ✅ Complete — 40+ adapters, all 19 graph edge types wired, test coverage 62–95% across packages

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

## 8) Phase 9: Security Architecture + Additional Clients

### 9.1 Emergency Shutdown (Panic Mode)

9.1.1 ✅ DONE: Panic trigger system

- Global atomic panic flag; `TriggerPanic(source, user, reason)` sets flag and cancels root context
- Signal handler: `SIGUSR1` on joecored triggers panic sequence
- REPL `/panic` command: confirmation prompt ("Type 'CONFIRM PANIC'"), then calls panic API
- CLI `joe panic`: subcommand in `cmd/joe/main.go`

9.1.2 ✅ DONE: Panic state persistence

- `PanicState` YAML written to `~/.joe/panic.state` on panic exit
- Loaded at joecored startup; if present, safe mode is activated automatically
- Fields: `triggered_at`, `trigger_source`, `trigger_user`, `reason`, `incomplete_operations`

9.1.3 ✅ DONE: Safe mode enforcement

- `SafeMode` checks flag before every tool execute; T2/T3 tools return `ErrSafeMode`
- Tool executor gate queries safe mode state from `internal/safety/safemode.go`
- Background refresh loop paused while in safe mode

9.1.4 ✅ DONE: Unlock flow

- `POST /api/v1/unlock` + `joe unlock --reason "..."` CLI
- Reason field mandatory; unlock event logged with user and reason
- Clears safe mode flag and removes `~/.joe/panic.state`

9.1.5 ✅ DONE: API endpoints + tests

- `POST /api/v1/panic` — trigger shutdown
- `GET /api/v1/panic/status` — check safe mode state
- `POST /api/v1/unlock` — exit safe mode

**Status: ✅ Complete** — `internal/safety/panic.go`, `panic_state.go`, `safemode.go`, `unlock.go`

---

### 9.2 MCP Server

9.2.1 ✅ DONE: MCP subcommand

- `joe mcp` subcommand in `cmd/joe/main.go`: reads `JOE_SERVER` + `JOE_API_KEY` env vars
- Stdio transport (JSON-RPC over stdin/stdout) using `github.com/mark3labs/mcp-go v0.44.1`

9.2.2 ✅ DONE: 8 tool definitions

- `graph_query`, `graph_related` (graph traversal)
- `k8s_get`, `k8s_logs` (Kubernetes)
- `metrics_query` (Prometheus/Datadog)
- `logs_search` (Loki/Splunk)
- `knowledge_search` (knowledge store semantic search)
- `incidents` (PagerDuty/Alertmanager incidents)

9.2.3 ✅ DONE: Dispatcher + client wiring

- `internal/mcp/dispatcher.go` routes tool calls to `*client.Client`
- `internal/mcp/server.go` creates MCP server with tool registrations

**Status: ✅ Complete** — `joe mcp` subcommand, `internal/mcp/`

---

### 9.3 RBAC

9.3.1 ✅ DONE: Security zones

- 4 default zones: `prod-readonly`, `prod-write`, `dev-full`, `unassigned`
- Zone definitions and source-to-zone assignments in `internal/rbac/zones.go`
- New sources default to `unassigned` (read-only) until admin assigns a zone

9.3.2 ✅ DONE: Policy engine + identity provider

- `internal/rbac/policy.go`: principal → zone permission evaluation
- `internal/rbac/identity.go`: API key → principal mapping via `rbac.NewAPIKeyProvider`
- `server.Principal` config field for API key → principal name mapping

9.3.3 ✅ DONE: RBAC middleware

- `internal/rbac/middleware.go`: enforces zone permissions on adapter routes (`/api/v1/{adapter}/{sourceID}/...`)
- Wired in `cmd/joecored/main.go` as `services.RBAC`
- Non-source routes (graph, control, admin) bypass zone enforcement

9.3.4 ✅ DONE: Admin API

- `GET/POST /api/v1/admin/zones` — list/create zones
- `GET/POST /api/v1/admin/source-zones` — get/assign source→zone mappings
- `GET /api/v1/admin/source-zones/unassigned` — list unassigned sources
- `GET/POST /api/v1/admin/policies` — manage principal policies

9.3.5 ✅ DONE: SQL migration

- Migration `006_rbac.up.sql`: tables `security_zones`, `source_zone_assignments`, `rbac_policies` (write-protected per invariants)

**Status: ✅ Complete** — `internal/rbac/`, `internal/store/migrations/006_rbac.up.sql`

---

## 9) Phase 10: Code Review Integration

### 10.1 GitHub & GitLab Adapters

10.1.1 ✅ DONE: GitHub adapter

- `internal/adapters/github/` — REST API client for PRs, diffs, and comments
- `GetPR`, `GetDiff`, `PostComment`, `RequestChanges` methods
- HMAC signature validation for webhook payloads

10.1.2 ✅ DONE: GitLab adapter

- `internal/adapters/gitlab/` — REST API client for MRs, diffs, and comments
- `GetMR`, `GetDiff`, `PostComment` methods
- Token-based webhook validation

### 10.2 Webhook Receiver

10.2.1 ✅ DONE: Webhook endpoints

- `POST /api/v1/webhooks/github` — HMAC + token validation, idempotent via `event_id UNIQUE`
- `POST /api/v1/webhooks/gitlab` — token validation, idempotent
- Both enqueue a `ReviewJob` into the SQLite-backed job queue (`INSERT OR IGNORE`)

### 10.3 Review Job Queue

10.3.1 ✅ DONE: Job store and repository

- `internal/review/job.go`: `ReviewJob` struct with status lifecycle (pending → running → done/failed)
- `internal/review/repository.go`: SQLite-backed, idempotent enqueue via `event_id UNIQUE + INSERT OR IGNORE`
- `internal/review/service.go`: job orchestration, retry handling

### 10.4 Review Agent

10.4.1 ✅ DONE: Automated review pipeline

- `internal/review/agent.go`: fetch diff → query graph → query knowledge → LLM analysis → post review
- Pulls PR/MR diff via GitHub/GitLab adapter
- Enriches context with graph nodes (services touched by changed files)
- Searches knowledge store for relevant runbooks/patterns
- LLM generates structured review comment
- Posts comment via adapter (T2 safety) or requests changes (T3 safety)

### 10.5 Core Tools

10.5.1 ✅ DONE: 7 core tools registered

- `github_pr_get` (T1) — fetch PR metadata
- `github_pr_diff` (T1) — fetch PR unified diff
- `github_comment` (T2) — post review comment
- `github_request_changes` (T3) — request changes on PR
- `gitlab_mr_get` (T1) — fetch MR metadata
- `gitlab_mr_diff` (T1) — fetch MR unified diff
- `gitlab_comment` (T2) — post review comment on MR

### 10.6 CLI & Client

10.6.1 ✅ DONE: `joe review` subcommand

- `joe review enqueue --provider github --repo owner/repo --pr 42` — manually enqueue a review job
- `joe review list` — list recent review jobs with status
- `joe review get <id>` — fetch a specific review job

10.6.2 ✅ DONE: Client bindings

- `internal/client/review.go`: `EnqueueReview`, `ListReviews`, `GetReview` methods

**Status: ✅ Complete** — `internal/adapters/{github,gitlab}/`, `internal/review/`, `internal/api/review.go`, `internal/tools/core/github_*.go`, `internal/tools/core/gitlab_*.go`, `internal/client/review.go`

---

## 10) Phase 11: Slack Bot (ChatOps)

### 11.1 Bot Binary

11.1.1 ✅ DONE: `joe slack` subcommand

- Reads `SLACK_BOT_TOKEN`, `SLACK_APP_TOKEN`, `JOE_SERVER`, `JOE_API_KEY` env vars
- Uses Socket Mode (WebSocket) via `github.com/slack-go/slack v0.18.0` — no public URL required
- Connects to joecored via the existing `*client.Client`

### 11.2 Event Handling

11.2.1 ✅ DONE: Slash commands

- `/joe ask <query>` — searches graph + knowledge store, posts Block Kit reply
- `/joe status` — posts graph summary (node counts by type, edge count, recently added)
- `/joe help` — shows available commands

11.2.2 ✅ DONE: Conversational interface

- DM handling: responds to any message in IM channels (bot ignores sub-types and other bots)
- Channel mention: strips `<@UXXXXXX>` prefix, treats rest as query

### 11.3 Agent & Formatter

11.3.1 ✅ DONE: `JoeClient` interface + Agent

- `internal/slack/agent.go`: `JoeClient` interface (GraphQuery, GraphSummary, SearchKnowledge)
- `Agent.Ask()` — graph query + knowledge search → formatted text
- `Agent.Status()` — wraps GraphSummary
- Interface-based design allows full unit testing with mock client

11.3.2 ✅ DONE: Block Kit formatter

- `internal/slack/formatter.go`: StatusBlocks, AskBlocks, ErrorBlock, HelpBlocks
- Sorted node-type breakdown in status, recent-additions section
- Graceful truncation for large result sets (max 5 nodes shown)

### 11.4 Tests

11.4.1 ✅ DONE: Unit tests

- `agent_test.go`: 6 tests (graph nodes, knowledge entries, no-results, errors, best-effort knowledge)
- `handler_test.go`: 4 tests (strip mention, no-results, truncation, knowledge entries)
- `formatter_test.go`: 5 tests (all block builders)
- All 15 tests passing

**Status: ✅ Complete** — `joe slack` subcommand, `internal/slack/{agent,server,handler,formatter}.go`

---

## 11) Phase 12: Web UI

### 12.1 Tech Stack

- React 18 + TypeScript 5 + Vite 5
- Tailwind CSS 3 + shadcn/ui component library
- React Flow 11 for graph visualization
- TanStack Query 5 for data fetching
- React Router 6 for navigation
- Zod v4 for schema validation
- Vitest + Testing Library for tests

### 12.2 Pages

12.2.1 ✅ DONE: 5 main pages

- `DashboardPage` — metrics cards, sources health, alerts list, recent sessions
- `GraphPage` — interactive infrastructure graph (React Flow), node details panel, graph controls
- `SourcesPage` — source listing and management
- `AdminPage` — security zones, source-zone assignments, RBAC policies
- `ChatPage` — conversational interface with message history, tool call display, session management

### 12.3 API Layer

12.3.1 ✅ DONE: Typed API client

- `ui/src/api/client.ts` — fetch wrapper with auth and error handling
- `ui/src/api/schemas.ts` — Zod v4 schemas for API responses
- `ui/src/api/types.ts` — TypeScript types derived via `z.infer<>`
- Domain modules: `graph.ts`, `sources.ts`, `security.ts`, `chat.ts`, `alerts.ts`

### 12.4 Backend Endpoints

12.4.1 ✅ DONE: 9 web UI endpoints in `internal/api/webui.go`

- `GET /api/v1/graph` — full graph for visualization
- `GET /api/v1/graph/node/{id}` — node details
- `GET /api/v1/graph/node/{id}/related` — node relationships
- `GET/POST /api/v1/sessions` — list/create sessions
- `GET /api/v1/sessions/{id}/messages` — session message history
- `POST /api/v1/chat` — send message to core agent
- `GET /api/v1/alerts` — alerts list
- `POST /api/v1/sources/{id}/test` — test source connectivity

### 12.5 Component Architecture

- `ui/src/components/layout/` — AppShell, Sidebar, Header, PageContainer
- `ui/src/components/graph/` — InfraGraph (React Flow), NodeDetails, GraphControls
- `ui/src/components/dashboard/` — MetricsCard, SourcesHealth, AlertsList, RecentSessions
- `ui/src/components/admin/` — ZonesTable, ZoneForm, SourceZoneAssign, PoliciesTable, PolicyForm
- `ui/src/components/chat/` — ChatWindow, MessageList, MessageBubble, ChatInput, ToolCallDisplay

### 12.6 Tests

- ESLint with `recommendedTypeChecked` + Prettier
- Vitest + Testing Library (27 tests across 4 files)
- `ui/vitest.config.ts` — jsdom environment

**Status: ✅ Complete** — `ui/`, `internal/api/webui.go`, `internal/graph/store.go` (`ListAll` added)

---

## Key Implementation Decisions

For architectural decisions and rationale, see:
- [docs/joe-architecture.md](docs/joe-architecture.md) - Architecture overview
- [docs/joe-dataflow.md](docs/joe-dataflow.md) - Data flow and .joe/ processing
- [docs/security-in-layers.md](docs/security-in-layers.md) - Security posture and Action Safety Framework
- [CLAUDE.md](CLAUDE.md) - Phase planning and standards
