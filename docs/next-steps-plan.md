# Next Steps Plan (Step-by-Step)

Use this as a checklist. Complete each section fully before moving on.

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

3.1 Implement store interactions
- Add read/list/create/update operations for clarifications.
- Define status transitions: pending -> answered/dismissed.

3.2 Add API handlers
- GET /api/v1/clarifications
- POST /api/v1/clarifications/{id}/answer
- POST /api/v1/clarifications/{id}/dismiss

3.3 Apply answers to graph
- When answered, apply stored graph operations.
- Record provenance and confidence.

3.4 Add tests
- Store tests for clarification lifecycle.
- API tests for list/answer/dismiss.

## 4) .joe/ Processing and Cache Replay

4.1 Define cache key and storage
- Hash .joe/ directory contents.
- Store/retrieve tool calls from cache table.

4.2 Implement .joe/ discovery path
- Read .joe/ files from repo sources.
- If cache hit, replay tool calls.
- If cache miss, call LLM to interpret.

4.3 Execute and persist tool calls
- Execute tool calls via core agent tools.
- Persist tool calls for future cache hits.

4.4 Add tests
- Cache hit: no LLM call, tool calls replayed.
- Cache miss: tool calls cached and replayed on next run.

## Suggested Order

- Step 1 (Core Agent refresh loop)
- Step 2 (Onboarding + control endpoints)
- Step 3 (Clarifications system)
- Step 4 (.joe/ processing and cache replay)
