# Joe Architecture

Reference architecture for Joe. This document describes the current component structure and data flow. Where it conflicts with [`docs/project/DECISIONS.md`](../project/DECISIONS.md), the decision log is the source of truth.

---

## Safety-First Design

Joe is an AI-powered infrastructure copilot. Unlike tools that give LLMs direct access to production systems, Joe enforces **deterministic safety rules** — compiled into the binary, not instructed by the LLM. The LLM suggests actions; hardcoded policy gates decide what executes.

This matters because AI agents with production access will make catastrophic mistakes. The question is not *if* the LLM hallucinates a dangerous action, but whether the system architecture makes that hallucination harmless.

**Joe's safety guarantees:**

- **Humans own all mutations.** No infrastructure, file, or configuration change without explicit human authorization, expressed in the safety policy.
- **Deterministic enforcement.** Safety rules are compiled code, not LLM instructions. Prompt injection cannot bypass them.
- **A boot-resolved write floor.** Observation mode and safe mode raise a read-only floor that nothing in the running binary can lower (D-0018).
- **Default deny.** Every managed-system mutation starts disabled. Humans opt in per action.
- **Binary action axis.** Every tool is classified Read or Mutate — Reads always run, Mutates are gated. There is no third tier (D-0020).

Full safety specification: [`docs/reference/security-in-layers.md`](security-in-layers.md).

---

## Design Principles

1. **Safety enforcement is hardcoded, not LLM-instructed** — deterministic policy gates control all mutations. See [Action Safety Framework](#action-safety-framework) and `docs/reference/security-in-layers.md`.
2. **A single `joe` binary** — bare `joe` starts the server (HTTP API + Core Agent + adapters + graph); subcommands (`joe mcp`, `joe slack`, `joe skills`, `joe incident`, `joe panic`, `joe unlock`) dispatch ahead of it.
3. **Two agent roles, one process** — the Core Agent maintains the graph in the background; the task/chat loop assists users. Both run inside the same process and share one tool-executor governance path; there is no inter-process HTTP boundary between them.
4. **The HTTP API is the integration contract** — external clients (Web UI, MCP server, Slack, CLI subcommands) reach Joe over `/api/v1/`.
5. **AI-agnostic** — the LLM adapter abstracts the provider; Joe supports and validates two providers, `claude` and `gemini`.
6. **The Core Agent has autonomy levels** — deterministic changes auto-apply, ambiguous ones queue as clarifications for a human.
7. **Humans own all mutations** — Joe never changes infrastructure, files, or configuration without explicit human authorization.

---

## Single-Process Architecture

Joe is one binary. Running bare `joe` starts the server, which hosts every subsystem in-process:

```
┌───────────────────────────────────────────────────────────────────────────┐
│  joe  (single process)                                                     │
│                                                                            │
│  ┌──────────────────────────┐      ┌──────────────────────────────────┐   │
│  │  HTTP API (:7777)        │      │  Core Agent (background)         │   │
│  │                          │      │                                  │   │
│  │  /api/v1/graph/...       │      │  • Refresh loop (poll sources,   │   │
│  │  /api/v1/components/...   │      │    diff, update graph)           │   │
│  │  /api/v1/observe/...      │      │  • Discovery / onboarding        │   │
│  │  /api/v1/tasks (chat)     │      │  • Clarification queueing        │   │
│  │  /api/v1/admin/...        │      │  • Tools: graph_add_*, register_ │   │
│  │  /api/v1/panic, /unlock   │      │    component, save_* (Read class)│   │
│  └───────────┬──────────────┘      └────────────────┬─────────────────┘   │
│              │                                       │                     │
│              ▼                                       ▼                     │
│  ┌──────────────────────────────────────────────────────────────────┐    │
│  │  Shared tool executor + governance                               │    │
│  │  (write floor → §C captain gate → zone/namespace scope →         │    │
│  │   safety policy → notify)                                        │    │
│  └────────────────────────────────┬─────────────────────────────────┘    │
│                                    │                                       │
│  ┌──────────┐ ┌──────────┐ ┌───────┴──────┐ ┌──────────┐ ┌──────────┐     │
│  │  Graph   │ │   SQL    │ │  Adapters    │ │   LLM    │ │ Knowledge│     │
│  │  Store   │ │  Store   │ │ K8s,Git,AWS, │ │ Adapter  │ │  Store   │     │
│  │ (SQLite) │ │ (SQLite) │ │ Prom, …      │ │claude/   │ │          │     │
│  │          │ │          │ │              │ │gemini    │ │          │     │
│  └──────────┘ └──────────┘ └──────────────┘ └──────────┘ └──────────┘     │
│                                                                            │
└───────────────────────────────────────────────────────────────────────────┘

External clients reach Joe over the HTTP API:
  Web UI (embedded) · MCP server (joe mcp) · Slack (joe slack) · CLI subcommands
```

**Entry / dispatch:**
- `cmd/joe/main.go` — the subcommand dispatcher. A non-flag first argument routes to `panic`, `unlock`, `mcp`, `slack`, `skills`, or `incident`; otherwise bare `joe` (or server-only flags) falls through to `runServer`.
- `cmd/joe/server.go` — the server entrypoint: config + DB migration, adapter registry wiring, Core Agent startup, the middleware chain (CORS → rate limit → metrics → edge auth → session headers → RBAC → request size), and the embedded Web UI mount.

**Why one process:** the Core Agent (background refresh) and the user task loop share the same graph, SQL store, adapters, LLM adapter, and — critically — the same tool-executor governance (the §C captain gate lives in `internal/captaingate/` and is composed by both paths, so incident-mode enforcement cannot drift between them). The retired two-binary `joe`/`joecored` model and its HTTP boundary no longer exist.

---

## HTTP API Contract

External clients communicate with Joe over `/api/v1/`. The surface is large; the groups below are representative, not exhaustive (the authoritative registration is in `internal/api/`). All routes sit behind edge auth; admin routes are admin-gated and audited.

```
# Status / build identity
GET  /api/v1/status                      Health + version (no ui_digest)
GET  /api/v1/version                      Full build identity (version, commit, build_time, ui_digest)
GET  /api/v1/mutate-status               Boot-resolved write floor + reason

# Graph
GET  /api/v1/graph/query?q=...            Query graph by string
GET  /api/v1/graph/related?...            Related nodes (depth optional)
GET  /api/v1/graph/summary                Graph summary for LLM context
GET  /api/v1/graph                        Full graph (Web UI format)

# Components (the registered-system entity — renamed from "source", D-0021)
GET    /api/v1/component-types            Component types for the registration UI
GET    /api/v1/components                 List components
POST   /api/v1/components                 Register a component
GET    /api/v1/components/{id}            Get a component
DELETE /api/v1/components/{id}            Delete + disconnect a component
POST   /api/v1/components/{id}/promote    Arm a component (readonly → armed)

# Category-based observability (backend resolved via graph edges)
POST /api/v1/observe/{metrics,logs,traces,alerts,k8s}

# Per-component read endpoints (examples)
GET  /api/v1/alertmanager/{id}/alerts     /pagerduty/{id}/incidents  /grafana/{id}/dashboards
GET  /api/v1/postgresql/{id}/stat  /redis/{id}/info  /kafka/{id}/topics  …
GET  /api/v1/registry/{oci,artifactory,ecr}/{id}/...

# Clarifications (human-in-the-loop)
GET  /api/v1/clarifications
POST /api/v1/clarifications/{id}/answer
POST /api/v1/clarifications/{id}/dismiss

# Control
POST /api/v1/onboarding                   Start onboarding
POST /api/v1/refresh                       Trigger full or per-component refresh

# Chat / agentic turns / sessions
POST /api/v1/tasks                         Execute an agentic turn (non-streaming)
POST /api/v1/tasks/stream                  Execute an agentic turn (SSE streaming)
GET/POST /api/v1/sessions[/{id}...]        Owner-scoped chat sessions, messages, findings, runs

# Knowledge
GET/POST /api/v1/knowledge/entries[/{id}]  CRUD
POST     /api/v1/knowledge/search          Semantic search
…        /api/v1/knowledge/{sources,drift,proposals}

# Emergency shutdown
POST /api/v1/panic                         Trigger emergency shutdown
GET  /api/v1/panic/status                  Panic / floor state
POST /api/v1/unlock                        Clear panic state (requires reason)

# Admin / RBAC (admin-gated, audited)
GET/POST/PATCH/DELETE /api/v1/admin/zones
GET/POST/DELETE       /api/v1/admin/component-zones
GET/POST/DELETE       /api/v1/admin/policies
GET                   /api/v1/admin/unassigned
GET/POST              /api/v1/admin/read-posture        team_flat | zoned (D-0041)
GET/POST              /api/v1/admin/read-promotions     Per-type autonomous-read promotion
GET/POST/DELETE       /api/v1/admin/admins · /principals/...
GET                   /api/v1/admin/credential-status
GET/POST              /api/v1/admin/sessions/...         Cross-tenant session governance

# LLM control plane
GET/POST /api/v1/models · /api/v1/llm/{settings,usage,providers}
```

---

## Subcommands

Subcommands dispatch ahead of the server (`cmd/joe/main.go`):

| Subcommand | Purpose |
|------------|---------|
| `joe` (bare) | Start the server (HTTP API + Core Agent + adapters + graph) |
| `joe mcp` | MCP stdio server for AI coding assistants (`internal/mcp/`) |
| `joe slack` | Slack bot (`internal/slack/`) |
| `joe skills` | Skills management |
| `joe incident` | Incident operations |
| `joe panic` | Trigger emergency shutdown |
| `joe unlock --reason "..."` | Clear panic state (audited) |

---

## Two Agent Roles (one process)

Joe has two agent *roles*, both hosted in the single binary.

### Core Agent (maintains infrastructure knowledge)

```
┌─────────────────────────────────────────────────────────────────────┐
│  Core Agent                                                          │
│  ──────────                                                          │
│  Runs: background, in the joe server process                        │
│  Purpose: keep the infrastructure graph accurate and up-to-date     │
│                                                                      │
│  • Background refresh: poll connected components, diff against the   │
│    graph, apply deterministic deltas, queue ambiguous findings       │
│  • Discovery / onboarding: interpret user-provided input            │
│  • Tools (all Read class — they maintain Joe's own model, not infra):│
│      graph_add_node, graph_add_edge, graph_update_node,             │
│      register_component, save_onboarding_fact, save_knowledge_entry  │
│                                                                      │
│  Location: internal/coreagent/                                       │
└─────────────────────────────────────────────────────────────────────┘
```

The Core Agent refresh path stamps the `svc:agent:core` principal on its context and resolves each component's adapter through the access guard. Its read surface is governed by `auto_promote_read` (per-component-type promotion) plus grants — **not** by the human read posture (D-0043).

> **`.joe/` ingestion removed (D-0042).** Joe no longer reads, interprets, or ingests any repo-authored `.joe/` directory. The git-refresh path still builds a `git_repo` node from HEAD commit identity (hash, date, author) on every refresh — only the `.joe`-derived metadata is gone.

### Task / Chat Loop (assists users)

```
┌─────────────────────────────────────────────────────────────────────┐
│  Task / Chat Loop                                                    │
│  ────────────────                                                    │
│  Runs: per request, behind /api/v1/tasks and /api/v1/tasks/stream    │
│  Purpose: help a user understand and operate infrastructure          │
│                                                                      │
│  Per turn (internal/agentloop/):                                     │
│    1. Add the user message to session history                        │
│    2. Build the prompt (system + graph summary + tools + history)    │
│    3. Call the LLM; if it returns tool calls, execute them through   │
│       the shared governed executor; loop until the LLM stops calling  │
│    4. Stream the response; enforce per-turn token/cost ceilings       │
│                                                                      │
│  Tools available: the Read-class query/observability tools, plus     │
│  the gated Mutate tools (write_file, run_command, doc-publish,       │
│  code-review) when their act policy keys are enabled.                │
└─────────────────────────────────────────────────────────────────────┘
```

Both loops compose the **same** tool-executor governance (`internal/captaingate/` §C gate over the base `internal/tools/` executor), so the write floor, incident gate, zone scope, safety policy, and notification contract apply identically on the autonomous and the user-facing paths.

### Review Agent (infrastructure-aware code review — Phase 10)

Triggered by webhook or API call (there is no `joe review` CLI subcommand — review is webhook/API-only), the Review Agent fetches a PR/MR diff, identifies affected resources, queries the graph (what depends on this?), knowledge (relevant runbooks/incidents), and live state, then produces an infrastructure-aware review. Its read tools are Read class; posting a comment (`github_comment`/`gitlab_comment`) or requesting changes (`github_request_changes`) are Mutate-class actions gated by their `act` policy keys.

---

## Core Agent Decision Flow

The Core Agent operates with varying autonomy depending on confidence:

```
AUTONOMOUS (no human needed)
  Deterministic changes from API data:
    • New pod in existing deployment      → update node metadata
    • Replica count / ConfigMap changed   → update node
    • Resource deleted                    → remove from graph
  Explicit relationships from infra:
    • Service selector → Pod              → add edge (explicit)
    • ArgoCD app → Git repo               → add edge (explicit)

LLM REASONING (confidence determines action)
  HIGH  → apply automatically (standard naming, explicit annotation)
  LOW   → queue a clarification for a human

ALWAYS REQUIRES A HUMAN
  • Adding a component (credentials)
  • Semantic / business context ("this service handles payments")
```

### Clarification flow

When the Core Agent finds something ambiguous, it stores a clarification (status `pending`) carrying the graph operations to apply once answered. A human answers via `POST /api/v1/clarifications/{id}/answer`; the clarification service applies the stored ops with confirmed provenance (answered_by, answered_at) and marks the row answered. Answer races are resolved by an optimistic `WHERE status = 'pending'` update (a second answer gets a 409).

Key files: `internal/core/clarification_service.go`, `internal/store/clarifications.go`, `internal/api/clarifications.go`.

---

## Component Details

### MCP Server (`joe mcp`)

```
┌─────────────────────────────────────────────────────────────────────┐
│  MCP Server                                                          │
│  ──────────                                                          │
│  Exposes Joe to AI coding assistants (Claude Code, Cursor, Codex).   │
│  Location: internal/mcp/  (joe mcp subcommand)                       │
│                                                                      │
│  Category-based tools (no component_id required):                    │
│    joe_graph_query      joe_graph_related   joe_k8s                  │
│    joe_metrics          joe_logs            joe_traces               │
│    joe_alerts           joe_knowledge_search                         │
│                                                                      │
│  Architecture: assistant → MCP server → Joe HTTP API                 │
│  Env: JOE_SERVER (default http://localhost:7777), JOE_API_KEY        │
└─────────────────────────────────────────────────────────────────────┘
```

The MCP server is a thin client over the HTTP API: it exposes Joe's category tools as MCP tool definitions and translates calls into HTTP requests (`internal/mcp/server.go`, `dispatcher.go`, `tools.go`).

### Chat Sessions

Chat sessions are a first-class subsystem — owned, shareable, and incident-linkable. A session row carries its creator principal (taken from context at create time, never from the request body), type (`default` vs `incident`), linked incident, title, and retention class. Ownership is enforced: a non-owner's list/get/messages are scoped or refused. The as-built specification is normative in [`docs/reference/DESIGN-CHAT-SESSIONS.md`](DESIGN-CHAT-SESSIONS.md).

Locations: `internal/sessionmodel/` (model, captain state machine, lifecycle, regime transitions), `internal/api/sessions.go` (owner-scoped routes), `internal/api/adminsessions.go` (cross-tenant governance).

### Agent Loop

The agentic loop (`internal/agentloop/`) drives a chat turn: build prompt → call LLM → if tool calls, execute through the governed executor → loop → stream. It is wired per request by `internal/api/tasks.go`, which composes the shared §C captain gate over the base executor and ensures the session's ownership row exists.

### LLM Adapter

```
type LLMAdapter interface {
    Chat(ctx, req ChatRequest) (*ChatResponse, error)
    Embed(ctx, text string) ([]float32, error)
}
```

Location: `internal/llm/`; provider selection in `internal/llmfactory/factory.go`. Joe supports and validates exactly **two** providers — `claude` (Anthropic) and `gemini` (Google). There is no OpenAI or Ollama adapter. Boot validates the active model's API key (`ANTHROPIC_API_KEY` for claude; `GEMINI_API_KEY` or `GOOGLE_API_KEY` for gemini) and fails fast if it is missing (`internal/config/validation.go`).

### Tool Executor

```
Location: internal/tools/
  executor.go   Main executor (the safety gate)
  registry.go   Tool registration
  core/         Core tools — reach the server API via internal/client/
  shared/       Go-native diagnostic tools (dnsquery, httpreq, netcheck,
                sysinfo, traceroute)
```

The executor classifies each tool (Read/Mutate) and runs the ordered gate: write floor → zone/component scope → namespace scope → safety policy → pre-execution notification (Mutate only) → execute → post-execution notification (Mutate only). See [`docs/reference/security-in-layers.md`](security-in-layers.md) §3.3.

Core tools are organized by subsystem (graph, k8s, git, aws, the observability/datastore/GitOps/networking/registry families, code review, knowledge, doc publishing). They call the local HTTP API through `internal/client/`, which keeps the tool surface uniform whether invoked by the Core Agent or the task loop.

### Discovery / Onboarding

Onboarding collects user-provided input via `POST /api/v1/onboarding`, validates connections, and lets the Core Agent interpret the input into graph operations and facts. The general onboarding/Facts store is unrelated to the removed `.joe/` ingestion path (D-0042).

### Background Refresh

A periodic job (default 5 minutes, configurable) keeps the graph current: load connected components, query current state via each adapter, diff against existing nodes, apply deterministic changes directly, and queue ambiguous findings for the LLM/clarifications under a budget. Location: `internal/coreagent/refresh.go`.

### Adapters

```
Location: internal/adapters/
```

All adapters are **read-only** except the doc-publish Git path and the code-review GitHub/GitLab path.

| Family | Adapters |
|--------|----------|
| Cloud | AWS (EC2, EKS, RDS, VPC), Azure (VMs, AKS, SQL, VNets) |
| Orchestration | Kubernetes (dynamic client, multi-context) |
| Source / repos | Git (read: file/log/diff; doc-publish: commit + push) |
| GitOps / CD / IaC | ArgoCD, Flux, Terraform (state), Helm (releases) |
| Observability | Prometheus/Mimir, Loki, Tempo, Jaeger, Datadog, Splunk, Dynatrace, New Relic |
| Alerting / dashboards | Alertmanager, PagerDuty, Grafana |
| Data stores | PostgreSQL, MySQL, Redis, MongoDB, Kafka, Elasticsearch |
| Networking | NGINX Ingress, Envoy, Istio (CRDs), Cilium |
| K8s CRDs | cert-manager, KEDA, OPA/Gatekeeper, Crossplane |
| Security / runtime | Falco |
| Registries | OCI (DockerHub/GHCR/Harbor/Quay), Artifactory, ECR |
| Code review | GitHub, GitLab (read PR/MR; mutate: comment, request-changes) |

Adapters implement a common connect/disconnect/status interface and are wired into the registry at boot. New adapters are added as Joe grows; this table is illustrative, not a fixed inventory.

### Observability: how Joe picks the right tool

Components link to their observability backends via graph edges (`metrics_in`, `logs_in`, `traces_in`, `alerts_in`, `paged_via`, `dashboard_in`). When Joe investigates a service, it reads those edges to know which backend to query. The category-based `POST /api/v1/observe/{metrics,logs,traces,alerts,k8s}` endpoints resolve the backend from the graph, translate the natural-language question via the LLM, execute against the resolved adapter, and return a normalized result — so a caller asks "metrics for payment-svc" without naming Prometheus vs Datadog.

```
> why is payment-svc returning 500s?

[graph: payment-svc → metrics_in → prometheus/prod]   → query error rate
[graph: payment-svc → logs_in    → loki/prod]          → search error logs
[finds trace_id in a log]
[graph: payment-svc → traces_in  → tempo/prod]         → fetch the trace

The 500s correlate with auth-service latency (p99 2.3s); auth-service memory at 95%.
```

---

## Data Layer

### Graph Store (SQLite)

Joe stores infrastructure topology in SQLite — **not** Cayley. The store interface lives in `internal/graph/store.go` and the SQLite implementation in `internal/graph/sqlite.go` (`NewSQLStore`).

```
graph_nodes
  id           TEXT PK     "deployment/payments/payment-svc"
  type         TEXT        "deployment"
  component_id TEXT        owning component (renamed from source_id, D-0021)
  metadata     TEXT(JSON)
  first_seen   TIMESTAMP
  last_seen    TIMESTAMP

graph_edges
  from_node    TEXT  ┐
  to_node      TEXT  ├─ composite PK
  relation     TEXT  ┘  free-form TEXT, no CHECK constraint
  confidence   INTEGER
  source       TEXT        "k8s_api" | "llm" | "user"
  context      TEXT        why this edge exists
  component_id TEXT
  created_at   TIMESTAMP
```

**Edge relations.** `internal/graph/relations.go` declares the named relation constants (`metrics_in`, `logs_in`, `traces_in`, `alerts_in`, `paged_via`, `dashboard_in`, `is_k8s_node`, `stores_in`, `queues_in`, `managed_by`, `provisions`, `ingress_for`, `proxies`, `mesh_for`, `policy_enforces`, `scaled_by`, `secures`, `image_stored_in`, `publishes_to`, …). Because `graph_edges.relation` is free-form TEXT with no CHECK constraint, additional relation types may also enter as inline string literals (e.g. in `internal/coreagent/`). The declared constants are authoritative as *names*; no fixed total count is authoritative.

### SQL Store (SQLite)

Relational data lives in the SQL store (`internal/store/`, default `~/.joe/joe.db`), migrated via embedded golang-migrate migrations under `internal/store/migrations/` (supporting both SQLite and PostgreSQL drivers). The migration set grows over time — recent additions include `028_read_posture` (the install-wide read posture singleton) and `029_drop_joe_file_cache` (the `.joe/` cleanup). Representative tables:

```
components            Registered-system registry (id, type, url, name, environment,
                      categories, connection config, status, …)
sessions / agent_*    Chat sessions, messages, findings, runs, captain state
clarifications        Human-confirmation queue (status, question, graph_operations, …)
onboarding_facts      User-stated facts + graph ops to replay
graph_nodes/edges     The graph store tables (above)
knowledge_*           Knowledge entries, sources, proposals, drift
security_zones / component_zone_assignments / rbac_policies   Zone-scoped RBAC
read_posture / auto_promote_reads   Read governance singletons
cluster_panic_state   Single-row panic state (id=1) — there is no panic.state file
audit_log             Append-only audit trail
```

### Knowledge Store

The Knowledge Store captures runbooks, synced docs, and learned insights across trust tiers (`internal/knowledge/`):

| Tier | Constant | Trust | LLM can | LLM cannot |
|------|----------|-------|---------|------------|
| **Curated** | `curated` | Highest (human-owned) | Read | Modify / delete (immutable — enforced at the service layer) |
| **Synced** | `synced` | High (external source of truth, e.g. Confluence/Notion) | Re-fetch, re-parse, update cache, link nodes | Change the source of truth |
| **Derived** | `derived` | Lower (LLM-extracted, confidence-scored) | Create, invalidate, adjust confidence | Touch curated entries |

The curated-immutability rule is enforced in code: `Update`/`Delete` on a `curated` entry returns an error (`internal/knowledge/service.go`). This matches the architectural invariant — the LLM can create/update Tier-3 (derived) knowledge but cannot touch Tier-1 (curated).

> **Not wired (as of 2026-06-20).** The derived "patterns extracted from sessions" capability describes the *intended* design. The extractor that would produce these (`internal/knowledge/learning/`) is dormant and orphaned — never called, reading the legacy `session_messages` table, writing ungoverned. No session-derived Tier-3 entries are produced today. See [docs/reference/learn-from-sessions-current-state.md](learn-from-sessions-current-state.md).

**Knowledge → docs flow.** Joe can extract an insight (stored Tier-3), notice a runbook missing it, and generate a **draft** proposal (`generate_doc_draft`, Read class — it writes only to Joe's proposal store). Publishing the proposal to an external system (`publish_doc_update_*`) is a Mutate action requiring human approval and the relevant `act` policy key; a published doc later re-syncs as Tier-2.

---

## Concurrency Model

Joe serves concurrent multi-user access. Understanding the model matters for features like background refresh and code-review jobs.

### Already thread-safe

| Component | Model | Notes |
|-----------|-------|-------|
| HTTP server | goroutine-per-request | Go's `http.Server` |
| SQLite | WAL mode, serialized writes | Concurrent reads safe, writes queue |
| Adapter state | Mutex-protected | Connection state uses `sync.Mutex` |
| External API calls | Stateless | K8s, GitHub, Prometheus calls are independent |
| Context cancellation | Propagated | Each request has an isolated context; panic cancels the root |

### Areas requiring coordination

- **Graph mutations during background refresh** — refresh may delete/recreate a node a user is annotating. Reconcile with last-seen/version awareness.
- **Clarification answer races** — resolved by optimistic `UPDATE … WHERE status = 'pending'` (a second answer gets a 409 Conflict).
- **Per-run tool durability** — non-idempotent model-maintenance inserts (e.g. `register_component`) carry a `NeedsDurability` key so the durable executor dedups an in-run retry or crash-resume (D-0020).

### Idempotency

| Operation | Key | On duplicate |
|-----------|-----|--------------|
| Clarification answer | `(id, status=pending)` | 409 Conflict |
| Durable tool call | `(runID, tool, args-hash)` | Replay short-circuits |
| Code-review comment / request-changes | `NeedsDurability` per-run key | Dedup on retry |

---

## Directory Structure

```
joe/
├── cmd/
│   └── joe/                      # The single binary (server + subcommands)
│       ├── main.go               # Subcommand dispatcher
│       └── server.go             # Server entrypoint
│
├── internal/
│   ├── api/                      # HTTP API (server, routes, middleware, admin)
│   ├── client/                   # HTTP client used by core tools
│   ├── coreagent/                # Core Agent (refresh, discovery, onboarding)
│   ├── agentloop/                # Per-turn chat/task loop
│   ├── captaingate/              # §C captain-session incident gate (shared)
│   ├── sessiongate/ sessionmodel/ session*  # Chat-session subsystem
│   ├── llm/                      # LLM adapters (claude, gemini)
│   ├── llmfactory/               # Provider selection
│   ├── tools/                    # Tool executor + registry
│   │   ├── executor.go
│   │   ├── core/                 # Core tools (call the API via internal/client)
│   │   ├── shared/               # Go-native diagnostic tools
│   │   └── local/                # readfile, writefile, runcmd
│   ├── safety/                   # Action axis, write floor, policy, invariants, panic
│   ├── rbac/                     # Zone-scoped RBAC + read posture resolver
│   ├── readposture/              # Install-wide read posture singleton
│   ├── graph/                    # Graph store (SQLite)
│   ├── store/                    # SQL store + migrations
│   ├── knowledge/                # Knowledge store (curated/synced/derived)
│   ├── adapters/                 # Infrastructure adapters
│   ├── buildinfo/                # Build identity (ldflags + ui_digest)
│   ├── mcp/                      # MCP server (joe mcp)
│   ├── slack/                    # Slack bot (joe slack)
│   ├── notify/                   # Notifications
│   └── config/                   # Configuration
│
├── ui/                           # Web UI (React 18 + Vite + Tailwind + shadcn/ui)
│   └── (embedded into the binary at build via //go:embed)
│
├── docs/
├── go.mod / go.sum
└── README.md
```

---

## Configuration

```
~/.joe/
├── config.yaml          # User configuration
├── safety-policy.yaml   # Safety policy (Joe cannot read/write this)
├── joe.db               # SQLite database (includes the graph)
└── repos/               # Cloned git repos
```

**config.yaml** (representative; `internal/config/`):

```yaml
llm:
  current: claude-sonnet           # active model (key into 'available')
  available:
    claude-sonnet:
      provider: claude
      model: claude-sonnet-4-20250514
    gemini-flash:
      provider: gemini
      model: gemini-2.0-flash
  currency: USD                    # USD | EUR (Stream G)

server:
  address: ":7777"
  # tls_enabled / tls_cert_file / tls_key_file
  # rate_limit_rps / rate_limit_burst
  # service_accounts: [{name, key}]

refresh:
  interval_minutes: 5
  llm_budget: { max_calls_per_hour: 10, batch_threshold: 5, batch_timeout_sec: 900 }

knowledge:
  embedding_model: ...
  semantic_top_k: ...

logging:
  level: info                      # debug | info | warn | error
```

**Environment overrides** (`internal/config/`, `internal/env/keys.go`): `JOE_LLM_PROVIDER` (claude|gemini), `JOE_LLM_MODEL`, `JOE_SERVER`, `JOE_API_KEY`, `JOE_DATABASE_DSN`, `JOE_LOG_LEVEL`, and `JOE_MODE=observation` (raises the write floor at boot). Provider auto-selection: an explicit preference wins; with both keys present Joe keeps Claude; with one key it switches to that provider; with none it errors naming both providers.

**Build identity** (`internal/buildinfo/`): `Version`/`Commit`/`BuildTime` are injected via ldflags `-X` at the full import path; `ui_digest` is a sha256 over the embedded UI bytes, computed once at boot from the embedded FS. `GET /api/v1/version` serializes the full `Info` (including `ui_digest`); `GET /api/v1/status` reports only `version`; the `joe_build_info` Prometheus gauge (constant 1) carries the same identity in labels. A plain `go build ./...` reports the unset `dev`/`none`/`unknown` defaults; `make build` injects real values and embeds the UI.

---

## Action Safety Framework

Joe enforces a hardcoded safety layer that governs what it can change and under what conditions. It is compiled into the binary and configured by humans outside Joe's reach. Full details in [`docs/reference/security-in-layers.md`](security-in-layers.md).

### Action axis (binary)

Every tool is classified at registration into one of two classes — the LLM cannot change a class (`internal/safety/tier.go`):

| Class | Description | Default |
|-------|-------------|---------|
| **Read** | Does not mutate the managed system (includes Joe's own graph/model maintenance) | Always allowed |
| **Mutate** | Mutates the managed system (files, infra, external PR/MR threads, published docs) | Denied; per-action opt-in via the `act` policy |

This replaces the former three-tier Observe/Record/Act (T1/T2/T3) scheme — collapsed to a binary axis by **D-0020**. Unknown tools default to Mutate (deny by default).

### Write floor, panic, and read posture

- **Write floor (D-0018):** boot-resolved and runtime-immutable. Observation mode (`JOE_MODE=observation`) and a sticky safe-mode/panic state each raise it; nothing in the running binary lowers it — recovery is a restart (`internal/safety/floor.go`).
- **Panic / safe mode:** persisted to the `cluster_panic_state` DB row (no `panic.state` file); safe mode is the write floor raised at the next boot with reason `safe_mode`. Triggers: `joe panic`, `POST /api/v1/panic`, `SIGUSR1`. Clear with `joe unlock --reason "..."` + restart.
- **Denial precedence (D-0022):** floor > incident > RBAC, enforced by check order in the executor (the incident half is the §C captain gate).
- **Read posture (D-0041, D-0043):** an install-wide scalar — `team_flat` (launch default: any authenticated principal reads any component) or `zoned` (grant-based full-mode read). It governs human-facing transport reads only; the autonomous `agent:core` read surface is governed by `auto_promote_read` + grants. Orthogonal to the write floor; flipped by an audited admin act.

### Safety policy

The policy lives in `~/.joe/safety-policy.yaml` — human-editable only, excluded from Joe's file tools, loaded once at startup. Its `act` section gates each Mutate tool (default deny). The legacy `record` section is a retained, inert compatibility shim (model-maintenance tools are now Reads).

### Self-protection invariants

Constants in source (`internal/safety/invariants.go`): Joe cannot read/write `~/.joe/` (incl. its safety and skills policies), and cannot run `joe`, `kill`, `pkill`, or `killall`. `kubectl`/`helm`/`argocd` are excluded from the default `run_command` allowlist; when enabled, compiled-in subcommand allowlists restrict them to read-only verbs.

### Designed but not yet built

Environment-level operation blocking, the mutation circuit breaker, and credential-isolation enforcement are designed (see `docs/reference/security-in-layers.md` §3.7) but **not** yet enforced in code. They become relevant as Joe gains infrastructure-mutating adapters.

---

## Implementation Phases

Historical milestone log. (The "two binaries" framing of early phases is retired — Joe is a single `joe` binary; the milestones below describe capabilities, which remain accurate.)

| Phase | Capability |
|-------|------------|
| 1 — Foundation | Binary + HTTP API + config + LLM adapter (Claude + Gemini) |
| 2 — Task loop | Tool interface + executor + registry; agentic loop; local tools; sessions |
| 3 — Core services + API | SQL store + migrations; graph store (SQLite); graph/component API + tools |
| 4 — Infra adapters | K8s + Git adapters, endpoints, and tools |
| 5 — Core Agent | Background refresh loop; graph-maintenance tools; lifecycle; discovery |
| 5.5 — Action Safety Framework | Action classification, safety policy, executor gate, self-protection, notification contract, edge auth, request limits |
| 6 — Infra adapters (broad) | Cloud, observability, alerting, data stores, GitOps/CD/IaC, networking, K8s CRDs, security/runtime, proprietary observability, registries |
| 7 — Knowledge store | Curated / synced / derived tiers (derived extractor dormant — see note above) |
| 8 — Documentation co-pilot | Draft generation → human approval → publish (Confluence/Notion/Git); drift detection |
| 9 — Security + clients | Emergency shutdown / panic; MCP server; zone-scoped RBAC; Web UI; Slack bot |
| 10 — Code review | GitHub + GitLab adapters; webhook receiver; Review Agent; `joe review` |

Subsequent work (single-binary consolidation, the write floor and binary axis, read posture, incident regime, session-model decomposition) is recorded in [`docs/project/DECISIONS.md`](../project/DECISIONS.md).
