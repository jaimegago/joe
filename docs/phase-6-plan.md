# Phase 6 Plan (Detailed, Step-by-Step)

This plan is the execution checklist for Phase 6. Complete each step in order and keep changes small and testable.

## Goals

- Add cloud, observability, and alerting adapters (read-only T1 by default).
- Add data store adapters for databases and messaging (PostgreSQL, MySQL, Redis, MongoDB, Kafka, Elasticsearch).
- Add GitOps, CD, and IaC adapters (Argo CD full adapter, Flux, Terraform, Helm releases).
- Add networking and ingress adapters (NGINX Ingress, Envoy, Istio, Cilium).
- Add K8s CRD-based adapters with minimal effort (cert-manager, KEDA, OPA/Gatekeeper, Crossplane).
- Add security and runtime adapters (Falco).
- Extend the graph with new relationships: metrics_in, logs_in, traces_in, alerts_in, paged_via, dashboard_in, is_k8s_node, stores_in, queues_in, managed_by, ingress_for, mesh_for, policy_enforces, scaled_by, provisions.
- Harden safety: credential encryption, TLS, rate limiting, and tool tier classification.

## Working Rules

- T1 tools only unless explicitly marked T2/T3 and policy allows.
- Add tests with each new adapter/tool. Prefer table-driven unit tests.
- Keep business logic free of instrumentation; use middleware/decorators.

---

## 6.1 Core Foundations

### 6.1.1 Add source types and validation

- Add new source type constants.
- Update validation paths to reject unknown types.
- Update API error messages and docs.

Touchpoints
- internal/store/constants.go
- internal/api/sources.go
- internal/client (if any source-type validation exists)

Acceptance
- New source types appear in list and can be created (without adapter errors when supported).
- Unknown types rejected with clear error.

Tests
- Add or extend API tests for create/list.

Status
- Done (2026-02-15)

### 6.1.2 Adapter registry wiring

- Extend source creation to connect new adapters.
- Add proper disconnect/unregister behavior.
- Ensure registry lists source IDs for new adapters.

Touchpoints
- internal/api/sources.go
- internal/adapters/registry.go

Acceptance
- Creating a new source for a supported adapter registers it.
- Deleting a source unregisters and disconnects it.

Tests
- API tests: create source for each new adapter type.

Status
- Done (2026-02-15) for AWS create + startup wiring

### 6.1.3 Add graph edge definitions and usage

- Define the new relations in graph modeling (if central constants exist).
- Update mapping logic to emit new edges when adapters provide data.

Touchpoints
- internal/coreagent (refresh mapping)
- internal/graph (if relation constants exist)

Acceptance
- Graph store can ingest new relation types without errors.

Tests
- Graph mapping tests for at least one new relation.

Status
- Done (2026-02-15)

---

## 6.2 Cloud Adapters

### 6.2.1 AWS adapter verification and expansion

- Validate existing AWS adapter coverage: EC2, EKS, RDS, VPC.
- Ensure mapping rules align with the Phase 6 graph model.
- Add missing metadata fields where necessary.

Touchpoints
- internal/adapters/aws/*
- internal/coreagent/* (mapping helpers)

Acceptance
- Refresh populates AWS nodes with expected metadata.

Tests
- Unit tests for AWS mapping, table-driven.

Status
- Done (2026-02-15)

### 6.2.2 Azure adapter (new)

- Add adapter skeleton and config parsing.
- Implement list/read for VMs, AKS, SQL, VNets.
- Add mapping to graph nodes and edges.

Touchpoints
- internal/adapters/azure/* (new)
- internal/coreagent/*
- internal/api (sources wiring)

Acceptance
- Azure source can be created and refresh produces nodes.

Tests
- Adapter unit tests and mapping tests.

Status
- Done (2026-02-15)

---

## 6.3 Observability Adapters

### 6.3.1 Open source: Prometheus/Mimir

- Adapter with query-only PromQL interface.
- Add tool for queries; return structured series data.
- Map metrics source to services (metrics_in edges).

### 6.3.2 Loki

- Adapter with LogQL query tool.
- Map logs source to services (logs_in edges).

### 6.3.3 Tempo/Jaeger

- Adapter with trace search tool.
- Map traces source to services (traces_in edges).

Touchpoints
- internal/adapters/observability/* (new)
- internal/tools/core/* (new tools)
- internal/api (endpoints for query tools)

Acceptance
- Each adapter can execute read-only queries.
- Graph relations created when sources are associated to services.

Tests

- Tool execution unit tests with fake responses.

Status

- Done (2026-02-19)

### 6.3.4 Proprietary vendors

- Datadog, Splunk, Dynatrace, New Relic as separate adapters.
- Start with query endpoints only.

Acceptance
- Query-only T1 functionality; credentials stored safely.

Status

- Deferred to last in execution order (step 12).

---

## 6.4 Alerting and Dashboards

### 6.4.1 Alertmanager

- List active alerts, query by label.
- Map alerts to services (alerts_in edges).

### 6.4.2 PagerDuty

- List incidents, query by service or escalation policy.
- Map paging source to services (paged_via edges).

### 6.4.3 Grafana

- List dashboards, panels.
- Map dashboards to services (dashboard_in edges).

Touchpoints
- internal/adapters/alerting/* (new)
- internal/tools/core/* (new tools)

Acceptance
- Query-only T1 functionality.

Tests
- Unit tests for adapters and tools.

Status

- Done (2026-02-19)

---

## 6.5 Safety and Hardening

### 6.5.1 Credential encryption at rest

- Encrypt sources.config before storage.
- Add migration or storage layer support for encrypted config.

Touchpoints
- internal/store (sources repository)
- internal/env or internal/config (key management)

Acceptance
- Plaintext not stored in DB.
- Backward compatible migration or re-encryption path.

Tests

- Encrypt/decrypt round-trip tests.

Status

- Done (2026-02-19)

### 6.5.2 TLS support for joe <-> joecored

- Add config flags for TLS certs.
- Update server/client to use TLS when enabled.

Touchpoints

- internal/api (server)
- internal/client (http transport)
- config/config.go

Acceptance

- Can run with and without TLS.

Tests

- Unit tests for config parsing.

Status

- Done (2026-02-19)

### 6.5.3 Rate limiting middleware

- Add rate limiting to API middleware with config settings.

Touchpoints

- internal/api/middleware.go
- internal/config

Acceptance

- Requests over limit are rejected with clear error.

Tests

- Middleware tests for limit enforcement.

Status

- Done (2026-02-19)

### 6.5.4 Tool tier classification

- Classify each new tool (T1/T2/T3) and enforce policy flags.

Touchpoints

- internal/safety/tier.go
- internal/tools/executor.go

Acceptance

- T2/T3 tools blocked without policy allowance.

Tests

- Executor tests for new tool tiers.

Status

- Done (2026-02-19)

---

## 6.6 Network & System Diagnostic Tools (Go-native)

Go-native implementations of common troubleshooting tools. All T1 (read-only). These live in `internal/tools/shared/` so both `joe` (local) and `joecored` (server-side) can use them — the LLM picks the right perspective based on context ("can I reach X from my laptop?" vs "can the cluster reach X?").

**Design principle:** No shelling out to CLI tools. Use Go standard library (`net`, `net/http`, `os`, `syscall`) for structured JSON output the LLM can reason over directly.

### 6.6.1 TCP connectivity check (replaces `nc`, `telnet`)

- Use `net.DialTimeout` to check if a host:port is reachable.
- Return: reachable (bool), latency_ms, error detail.
- Support multiple ports in one call for port scanning.

Touchpoints
- internal/tools/shared/netcheck/ (new)

Tools

| Tool | Parameters | Use Case |
|------|------------|----------|
| tcp_connect | host, port, timeout_ms (default 5000) | Check if a host:port is reachable |
| port_scan | host, ports (array), timeout_ms | Check multiple ports, return which are open |

### 6.6.2 DNS lookup (replaces `dig`, `nslookup`, `host`)

- Use `net.LookupHost`, `net.LookupCNAME`, `net.LookupMX`, `net.LookupTXT`, `net.LookupNS`.
- Return structured records: A, AAAA, CNAME, MX, TXT, NS.
- Optionally specify a custom DNS resolver.

Touchpoints
- internal/tools/shared/dnsquery/ (new)

Tools

| Tool | Parameters | Use Case |
|------|------------|----------|
| dns_lookup | hostname, record_type (optional: A, AAAA, CNAME, MX, TXT, NS, all) | Resolve DNS records |

### 6.6.3 HTTP request (replaces `curl`)

- Use `net/http` client with configurable method, headers, timeout.
- Return: status_code, headers (map), body (truncated), latency_ms, TLS info.
- Default to GET. Allow HEAD for lightweight checks.
- Safety: block requests to localhost/internal metadata endpoints (169.254.169.254) unless explicitly allowed.

Touchpoints
- internal/tools/shared/httpreq/ (new)

Tools

| Tool | Parameters | Use Case |
|------|------------|----------|
| http_request | url, method (default GET), headers (optional), timeout_ms (default 10000) | Probe HTTP endpoint, check status/latency |

### 6.6.4 System info (replaces `df`, `free`, `uptime`, `ps`, `uname`)

- Use `syscall.Statfs` (disk), `runtime.MemStats` + `/proc/meminfo` (memory), `syscall.Sysinfo` (uptime/load), `os` (hostname, platform).
- Return structured stats, not formatted text.

Touchpoints
- internal/tools/shared/sysinfo/ (new)

Tools

| Tool | Parameters | Use Case |
|------|------------|----------|
| system_info | sections (optional: disk, memory, load, os, all) | System stats: disk usage, memory, load averages, OS info |

### 6.6.5 Traceroute (replaces `traceroute`, `tracepath`)

- Use `golang.org/x/net/icmp` for hop-by-hop path tracing.
- Return structured hop list: hop number, IP, hostname (reverse DNS), latency_ms.
- Configurable max hops and timeout.

Touchpoints
- internal/tools/shared/traceroute/ (new)

Tools

| Tool | Parameters | Use Case |
|------|------------|----------|
| trace_route | host, max_hops (default 30), timeout_ms (default 5000) | Network path with per-hop latency |

Note: ICMP requires elevated privileges on most systems. Fall back to UDP-based traceroute if ICMP is unavailable.

Acceptance (all 6.6.x)
- All tools return structured JSON, not text.
- Both joe and joecored can register and use these tools.
- No external CLI dependencies — pure Go implementations.
- Testable with mock interfaces for network operations.

Tests
- Unit tests with mock dialers/resolvers.
- Table-driven tests for edge cases (timeout, unreachable, DNS failure).

Status

- Done (2026-02-20) — all 6 tools implemented (tcp_connect, port_scan, dns_lookup, http_request, system_info, trace_route), registered in default registry and safety tier.go (T1). Coverage: netcheck 94.3%, httpreq 93.8%, sysinfo 90.9%, dnsquery 84.5%, traceroute 72.6% (ICMP ProbeHop requires root; all reachable paths covered via mockHopProber).

---

## 6.7 Data Store Adapters

Read-only diagnostic queries. Joe never writes application data. All T1.

### 6.7.1 PostgreSQL

- Connect via `lib/pq` or `pgx` driver with read-only connection string.
- Query `pg_stat_activity` (active connections, blocked queries), `pg_stat_user_tables` (seq scans, dead tuples), `pg_stat_replication` (replication lag).
- Slow query log via `pg_stat_statements` extension (if available).
- Map database to services via `stores_in` edges.

Touchpoints
- internal/adapters/postgres/ (new)
- internal/tools/core/postgres_stat.go, postgres_query.go (new)

Tools
| Tool | Parameters | Use Case |
|------|------------|----------|
| postgres_stat | source_id | Connection stats, replication lag, table stats |
| postgres_query | source_id, query (read-only enforced) | Run diagnostic SQL (SELECT only) |

Acceptance
- Connect to PostgreSQL, return structured stats.
- Query enforcement: only SELECT statements allowed (parsed before execution).

Tests
- Unit tests with mock driver responses.
- SQL parsing tests to verify write rejection.

### 6.7.2 MySQL

- Connect via `go-sql-driver/mysql` with read-only user.
- Query `SHOW PROCESSLIST`, `SHOW SLAVE STATUS` / `SHOW REPLICA STATUS`, `INFORMATION_SCHEMA.INNODB_TRX`.
- Map database to services via `stores_in` edges.

Touchpoints
- internal/adapters/mysql/ (new)
- internal/tools/core/mysql_stat.go, mysql_query.go (new)

Tools
| Tool | Parameters | Use Case |
|------|------------|----------|
| mysql_stat | source_id | Processlist, replication status, InnoDB stats |
| mysql_query | source_id, query (read-only enforced) | Run diagnostic SQL (SELECT only) |

### 6.7.3 Redis

- Connect via `go-redis/redis` client.
- Run `INFO` (server, memory, clients, stats, replication), `SLOWLOG GET`, `CLIENT LIST`, `DBSIZE`.
- No key reads — Joe sees operational stats only.
- Map cache/store to services via `stores_in` edges.

Touchpoints
- internal/adapters/redis/ (new)
- internal/tools/core/redis_info.go (new)

Tools
| Tool | Parameters | Use Case |
|------|------------|----------|
| redis_info | source_id, section (optional) | INFO stats by section |
| redis_slowlog | source_id, count (default 10) | Recent slow commands |

### 6.7.4 MongoDB

- Connect via `mongo-driver/mongo` with read-only user.
- Run `db.serverStatus()`, `rs.status()`, `db.currentOp()`, `system.profile` (if enabled).
- Map database to services via `stores_in` edges.

Touchpoints
- internal/adapters/mongodb/ (new)
- internal/tools/core/mongodb_stat.go (new)

Tools
| Tool | Parameters | Use Case |
|------|------------|----------|
| mongodb_stat | source_id | Server status, replica set health, current ops |
| mongodb_query | source_id, database, collection, filter (read-only) | Diagnostic find queries |

### 6.7.5 Kafka

- Connect via `confluent-kafka-go` or `segmentio/kafka-go` admin client.
- List topics, describe consumer groups (lag per partition), broker metadata, cluster health.
- No message consumption — admin metadata only.
- Map queues to services via `queues_in` edges.

Touchpoints
- internal/adapters/kafka/ (new)
- internal/tools/core/kafka_topics.go, kafka_consumers.go (new)

Tools
| Tool | Parameters | Use Case |
|------|------------|----------|
| kafka_topics | source_id, topic (optional) | List topics or describe one (partitions, config) |
| kafka_consumers | source_id, group (optional) | Consumer group lag, members, offsets |
| kafka_brokers | source_id | Broker list, controller, cluster ID |

### 6.7.6 Elasticsearch

- Connect via HTTP REST API (compatible with OpenSearch).
- Query `_cluster/health`, `_cat/indices`, `_cat/shards`, `_nodes/stats`.
- Map search/logging backend to services via `stores_in` or `logs_in` edges.

Touchpoints
- internal/adapters/elasticsearch/ (new)
- internal/tools/core/elasticsearch_health.go (new)

Tools
| Tool | Parameters | Use Case |
|------|------------|----------|
| elasticsearch_health | source_id | Cluster health, shard status, node stats |
| elasticsearch_indices | source_id, pattern (optional) | Index list with doc count, size, health |

Acceptance (all 6.7.x)
- Each adapter connects with read-only credentials.
- Stats returned as structured JSON.
- Graph edges created linking data stores to consuming services.

Tests
- Table-driven unit tests with mock responses for each adapter.

Status

- Done (2026-02-20) — all 6 adapters implemented (PostgreSQL, MySQL, Redis, MongoDB, Kafka, Elasticsearch) with interface-injectable test seams. 10 core tools (postgres_stat, postgres_query, mysql_stat, mysql_query, redis_info, redis_slowlog, mongodb_stat, kafka_topics, kafka_brokers, kafka_consumers, elasticsearch_health, elasticsearch_indices). API endpoints and HTTP client methods wired. All source types registered. Safety tier classified (T1). Coverage: tools/core 94.9%, elasticsearch 82.1%, postgres 70.3%, redis 70.1%, mongodb 73.8%, mysql 65.3%, kafka 62.6%.

---

## 6.8 GitOps, CD & IaC Adapters

### 6.8.1 Argo CD (full adapter)

- Upgrade from `run_command` passthrough to a proper REST API adapter.
- List applications, get app details (sync status, health, resources), get app diff, get sync history.
- Map apps to K8s resources via `managed_by` edges.

Touchpoints
- internal/adapters/argocd/ (new, replaces run_command usage)
- internal/tools/core/argocd_apps.go, argocd_diff.go (new)

Tools
| Tool | Parameters | Use Case |
|------|------------|----------|
| argocd_apps | source_id, project (optional) | List apps with sync/health status |
| argocd_app | source_id, name | App detail: resources, sync status, conditions |
| argocd_diff | source_id, name | Live vs desired state diff |
| argocd_history | source_id, name, limit | Recent sync operations |

### 6.8.2 Flux

- Query Flux CRDs via K8s adapter: `GitRepository`, `Kustomization`, `HelmRelease`, `HelmRepository`.
- Show reconciliation status, last applied revision, conditions.
- Map Flux resources to K8s workloads via `managed_by` edges.

Touchpoints
- Extends K8s adapter CRD resource list
- internal/tools/core/flux_status.go (new, convenience wrapper)

Tools
| Tool | Parameters | Use Case |
|------|------------|----------|
| flux_status | source_id, namespace (optional) | All Flux resources with reconciliation status |
| flux_resource | source_id, kind, namespace, name | Detail for one Flux resource |

### 6.8.3 Terraform

- Read Terraform state files (local or remote backend via `terraform show -json`).
- Parse `tfstate` JSON: list managed resources, detect drift (planned vs actual).
- Show output values and provider configuration.
- Map Terraform-managed resources to graph nodes via `provisions` edges.

Touchpoints
- internal/adapters/terraform/ (new)
- internal/tools/core/terraform_state.go (new)

Tools
| Tool | Parameters | Use Case |
|------|------------|----------|
| terraform_state | source_id, resource_type (optional) | List managed resources from state |
| terraform_resource | source_id, address | Detail for one resource (attributes, dependencies) |
| terraform_outputs | source_id | List output values |

Note: Terraform state may contain secrets. Adapter must redact sensitive attributes marked `sensitive: true` in state.

### 6.8.4 Helm

- Use Helm SDK (`helm/v3`) to query release history.
- List releases, get release status, get values, get manifest diff between revisions.
- Map Helm releases to K8s resources via `managed_by` edges.

Touchpoints
- internal/adapters/helm/ (new)
- internal/tools/core/helm_releases.go (new)

Tools
| Tool | Parameters | Use Case |
|------|------------|----------|
| helm_releases | source_id, namespace (optional) | List releases with status, revision, chart |
| helm_release | source_id, namespace, name | Release detail: values, notes, manifest |
| helm_history | source_id, namespace, name, limit | Revision history with status |

Acceptance (all 6.8.x)
- Each adapter returns structured data from real sources.
- Graph edges link managed resources back to their managing tool.

Tests
- Unit tests with mock API/state responses.

Status

- Done (2026-02-20) — Argo CD HTTP REST adapter (argocd_apps, argocd_app, argocd_diff, argocd_history), Flux CRD wrapper tools (flux_status, flux_resource), Terraform state file reader (terraform_state, terraform_resource, terraform_outputs), Helm K8s-secret-based adapter (helm_releases, helm_release, helm_history). 12 new T1 tools registered. API endpoints, client methods, source wiring, and safety tier classification complete. Coverage: argocd 76.2%, terraform 72.1%, helm 67.5%, tools/core 90.8%.

---

## 6.9 Networking & Ingress Adapters

### 6.9.1 NGINX Ingress Controller

- Query NGINX status via:
  - K8s Ingress CRDs (ingress rules, backends, TLS)
  - NGINX status endpoint (`/nginx_status` or Prometheus metrics endpoint)
  - ConfigMaps for NGINX configuration
- Map ingress rules to backend services via `ingress_for` edges.

Touchpoints
- Extends K8s adapter for Ingress CRD queries
- internal/adapters/nginx/ (new, for status/metrics endpoint)
- internal/tools/core/nginx_ingress.go (new)

Tools
| Tool | Parameters | Use Case |
|------|------------|----------|
| nginx_ingresses | source_id, namespace (optional) | List ingress rules with hosts, paths, backends |
| nginx_status | source_id | Active connections, request rates, upstream health |
| nginx_config | source_id, namespace | Effective NGINX configuration from ConfigMaps |

### 6.9.2 Envoy

- Query Envoy admin API (`/config_dump`, `/clusters`, `/stats`, `/server_info`).
- Show cluster health, upstream endpoints, circuit breaker state.
- Compatible with standalone Envoy and Envoy sidecars (Istio data plane).
- Map Envoy proxies to services via `proxies` edges.

Touchpoints
- internal/adapters/envoy/ (new)
- internal/tools/core/envoy_clusters.go, envoy_config.go (new)

Tools
| Tool | Parameters | Use Case |
|------|------------|----------|
| envoy_clusters | source_id | Cluster health, endpoints, circuit breaker state |
| envoy_config | source_id, section (optional) | Config dump (listeners, routes, clusters) |
| envoy_stats | source_id, filter (optional) | Stats filtered by prefix |

### 6.9.3 Istio

- Query Istio CRDs via K8s adapter: `VirtualService`, `DestinationRule`, `Gateway`, `PeerAuthentication`, `AuthorizationPolicy`.
- Show mTLS status, traffic policies, fault injection rules.
- Map mesh config to services via `mesh_for` edges.

Touchpoints
- Extends K8s adapter CRD resource list
- internal/tools/core/istio_mesh.go (new, convenience wrapper)

Tools
| Tool | Parameters | Use Case |
|------|------------|----------|
| istio_config | source_id, namespace (optional), kind (optional) | List Istio CRDs with status |
| istio_resource | source_id, kind, namespace, name | Detail for one Istio resource |

### 6.9.4 Cilium

- Query Cilium CRDs via K8s adapter: `CiliumNetworkPolicy`, `CiliumClusterwideNetworkPolicy`, `CiliumEndpoint`.
- Query Hubble API for network flow visibility (if available).
- Map network policies to workloads via `policy_enforces` edges.

Touchpoints
- Extends K8s adapter CRD resource list
- internal/adapters/cilium/ (new, for Hubble API)
- internal/tools/core/cilium_policy.go (new)

Tools
| Tool | Parameters | Use Case |
|------|------------|----------|
| cilium_policies | source_id, namespace (optional) | List network policies with enforcement status |
| cilium_endpoints | source_id, namespace (optional) | Endpoint health and identity |
| cilium_flows | source_id, namespace, pod (optional), limit | Recent network flows via Hubble |

Acceptance (all 6.9.x)
- Each adapter returns structured config/status data.
- Graph edges map networking resources to the services they front or protect.

Tests
- Unit tests with mock API/CRD responses.

Status

- Done (2026-02-20) — NGINX adapter (K8s Ingress CRDs + HTTP status endpoint, nginx_ingresses, nginx_status, nginx_config), Envoy HTTP REST adapter (envoy_clusters, envoy_config, envoy_stats), Istio CRD wrapper tools (istio_config, istio_resource), Cilium CRD wrapper tools (cilium_policies, cilium_endpoints). 10 new T1 tools registered. API endpoints, client methods, source wiring (nginx-ingress, envoy source types), and safety tier classification complete. Coverage: envoy 71.8%, nginx 66.4%, tools/core 88.1%.

---

## 6.10 K8s CRD-Based Adapters (Low Effort)

These adapters piggyback on the existing K8s adapter by adding CRD resource types to the supported list. Minimal new code — mainly tool wrappers for convenience.

### 6.10.1 cert-manager

- Query CRDs: `Certificate`, `CertificateRequest`, `Issuer`, `ClusterIssuer`, `Order`, `Challenge`.
- Surface certificate expiry, readiness, issuer status.
- Map certificates to services/ingresses via `secures` edges.

Tools
| Tool | Parameters | Use Case |
|------|------------|----------|
| certmanager_certs | source_id, namespace (optional) | List certificates with expiry, readiness |
| certmanager_issuers | source_id | List issuers/cluster issuers with status |

### 6.10.2 KEDA

- Query CRDs: `ScaledObject`, `ScaledJob`, `TriggerAuthentication`.
- Surface scaling targets, trigger types, current replicas vs desired.
- Map scaled objects to workloads via `scaled_by` edges.

Tools
| Tool | Parameters | Use Case |
|------|------------|----------|
| keda_scaledobjects | source_id, namespace (optional) | List scaled objects with trigger info |

### 6.10.3 OPA / Gatekeeper

- Query CRDs: `ConstraintTemplate`, constraints (dynamic GVK), `Config`.
- Surface constraint violations from audit results.
- Map policies to namespaces/workloads via `policy_enforces` edges.

Tools
| Tool | Parameters | Use Case |
|------|------------|----------|
| opa_constraints | source_id, template (optional) | List constraints with violation counts |
| opa_violations | source_id, constraint | Violation details for a specific constraint |

### 6.10.4 Crossplane

- Query CRDs: `Provider`, `ProviderConfig`, `Composite Resource Definitions (XRD)`, `Claims`, `Managed Resources`.
- Surface provider health, resource sync status, composition status.
- Map Crossplane resources to cloud resources via `provisions` edges.

Tools
| Tool | Parameters | Use Case |
|------|------------|----------|
| crossplane_providers | source_id | List providers with health status |
| crossplane_resources | source_id, kind (optional), namespace (optional) | List managed/composite resources with sync status |

Touchpoints (all 6.10.x)
- internal/adapters/k8s/ (add CRD types to supported resources)
- internal/tools/core/ (convenience tool wrappers)

Acceptance
- CRD resources queryable through existing K8s adapter.
- Convenience tools return structured, human-readable status.

Tests
- Unit tests with mock CRD responses.

Status

- Done (2026-02-20) — cert-manager (certmanager_certs, certmanager_issuers), KEDA (keda_scaledobjects), OPA/Gatekeeper (opa_constraints, opa_violations with dynamic constraint CRD discovery), Crossplane (crossplane_providers, crossplane_resources). 7 new T1 tools registered in default registry and safety tier.go. Coverage: tools/core 88.9%.

---

## 6.11 Security & Runtime Adapters

### 6.11.1 Falco

- Query Falco gRPC or HTTP output API for runtime security events.
- List recent alerts by severity, rule, source (syscall, k8s_audit).
- Map alerts to pods/nodes via `alerts_in` edges.

Touchpoints
- internal/adapters/falco/ (new)
- internal/tools/core/falco_alerts.go (new)

Tools
| Tool | Parameters | Use Case |
|------|------------|----------|
| falco_alerts | source_id, priority (optional), limit | Recent runtime security events |
| falco_rules | source_id | List loaded rules with priority |

Acceptance
- Adapter connects to Falco output and returns structured events.

Tests
- Unit tests with mock event responses.

Status

- Done (2026-02-20) — Falco HTTP adapter targeting Falco Sidekick UI backend. `ListEvents` queries `/api/v1/events` with optional priority/source/rule/limit filters. `ListRules` derives unique rules from recent events (name, priority, source, count). 2 T1 tools registered (`falco_alerts`, `falco_rules`). API endpoints (`GET /api/v1/falco/{sourceID}/events`, `GET /api/v1/falco/{sourceID}/rules`), client methods, source wiring (create + startup reconnect), and safety tier classification complete. Coverage: falco adapter 89.0%.

---

## Execution Order (Recommended)

1) 6.1 Core foundations (done)
2) 6.2 Cloud adapters (done)
3) 6.3 Open source observability (Prometheus, Loki, Tempo/Jaeger) (done)
4) 6.4 Alerting and dashboards (done)
5) 6.5 Safety and hardening (done)
6) 6.6 Network & system diagnostic tools (Go-native, shared) ← done (2026-02-20)
7) 6.7 Data store adapters (PostgreSQL, MySQL, Redis, MongoDB, Kafka, Elasticsearch) ← done (2026-02-20)
8) 6.8 GitOps, CD & IaC (Argo CD, Flux, Terraform, Helm) ← done (2026-02-20)
9) 6.9 Networking & ingress (NGINX, Envoy, Istio, Cilium) ← done (2026-02-20)
10) 6.10 K8s CRD-based (cert-manager, KEDA, OPA/Gatekeeper, Crossplane) ← done (2026-02-20)
11) 6.11 Security & runtime (Falco)
12) 6.3.4 Proprietary observability vendors (Datadog, Splunk, Dynatrace, New Relic)

Note: 6.10 (CRD-based) can be done earlier as quick wins since effort is minimal.

---

## New Graph Edge Types

| Edge | From | To | Added In |
|------|------|----|----------|
| metrics_in | service | prometheus source | 6.3 |
| logs_in | service | loki source | 6.3 |
| traces_in | service | tempo/jaeger source | 6.3 |
| alerts_in | service | alertmanager/falco source | 6.4, 6.11 |
| paged_via | service | pagerduty source | 6.4 |
| dashboard_in | service | grafana source | 6.4 |
| is_k8s_node | cloud instance | K8s node | 6.2 |
| stores_in | service | database (pg/mysql/mongo/es/redis) | 6.7 |
| queues_in | service | kafka/rabbitmq | 6.7 |
| managed_by | k8s resource | argocd app / flux / helm release | 6.8 |
| provisions | terraform resource / crossplane | cloud resource | 6.8, 6.10 |
| ingress_for | nginx ingress | backend service | 6.9 |
| proxies | envoy | service | 6.9 |
| mesh_for | istio config | service | 6.9 |
| policy_enforces | opa constraint / cilium policy | namespace/workload | 6.9, 6.10 |
| scaled_by | keda scaled object | workload | 6.10 |
| secures | certificate | service/ingress | 6.10 |

---

## Tracking

- Update docs/next-steps-plan.md if scope changes.
- Log major decisions in docs/phase-6-plan.md under a "Decisions" section.

## Decisions

- Source types are centrally defined in store constants with AllowedSourceTypes/IsValidSourceType helpers.
- AWS adapter is wired at source creation and joecored startup.
- Phase 6 relations are defined as graph relation constants and referenced in .joe interpretation and graph_add_edge tool docs.
- (2026-02-15) Expanded Phase 6 scope beyond cloud/observability/alerting to include data stores, GitOps/IaC, networking/ingress, CRD-based tools, and security/runtime adapters. Rationale: cover the tools platform engineers encounter daily during incidents. CRD-based adapters (6.9) are low-effort since they reuse the K8s adapter. Proprietary observability vendors moved to last in execution order — open-source and infrastructure-adjacent tools come first.
- (2026-02-16) Tool directory convention: `tools/local/` = joe-only tools that interact with the user's machine or REPL (readfile, writefile, gitdiff, gitstatus, runcmd, echo, askuser). `tools/core/` = joe tools that call joecored via HTTP. `tools/shared/` = Go-native tools usable by both binaries (diagnostic tools: netcheck, dnsquery, httpreq, sysinfo, traceroute). Existing local tools stay in `local/` — they shell out to git or use `os.Getwd()` and are fundamentally tied to the user's machine. Shared tools use only Go stdlib/x packages, return structured JSON, and accept mock interfaces for testing.
