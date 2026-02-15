# Phase 6 Plan (Detailed, Step-by-Step)

This plan is the execution checklist for Phase 6. Complete each step in order and keep changes small and testable.

## Goals

- Add cloud, observability, and alerting adapters (read-only T1 by default).
- Extend the graph with new relationships: metrics_in, logs_in, traces_in, alerts_in, paged_via, dashboard_in, is_k8s_node.
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

### 6.3.4 Proprietary vendors

- Datadog, Splunk, Dynatrace, New Relic as separate adapters.
- Start with query endpoints only.

Acceptance
- Query-only T1 functionality; credentials stored safely.

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

### 6.5.3 Rate limiting middleware

- Add rate limiting to API middleware with config settings.

Touchpoints
- internal/api/middleware.go
- internal/config

Acceptance
- Requests over limit are rejected with clear error.

Tests
- Middleware tests for limit enforcement.

### 6.5.4 Tool tier classification

- Classify each new tool (T1/T2/T3) and enforce policy flags.

Touchpoints
- internal/safety/tier.go
- internal/tools/executor.go

Acceptance
- T2/T3 tools blocked without policy allowance.

Tests
- Executor tests for new tool tiers.

---

## Execution Order (Recommended)

1) 6.1 Core foundations (done)
2) 6.2.1 AWS verification
3) 6.2.2 Azure adapter
4) 6.3 Open source observability (Prometheus, Loki, Tempo)
5) 6.4 Alerting and dashboards
6) 6.3.4 Proprietary vendors
7) 6.5 Safety and hardening

---

## Tracking

- Update docs/next-steps-plan.md if scope changes.
- Log major decisions in docs/phase-6-plan.md under a "Decisions" section.

## Decisions

- Source types are centrally defined in store constants with AllowedSourceTypes/IsValidSourceType helpers.
- AWS adapter is wired at source creation and joecored startup.
- Phase 6 relations are defined as graph relation constants and referenced in .joe interpretation and graph_add_edge tool docs.
