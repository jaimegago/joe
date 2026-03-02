# Joe Architecture

Reference architecture for implementation. This document is the source of truth for component structure and data flow.

---

## Safety-First Design

Joe is an AI-powered infrastructure copilot. Unlike tools that give LLMs direct access to production systems, Joe enforces **deterministic safety rules** — compiled into the binary, not instructed by the LLM. The LLM suggests actions; hardcoded policy gates decide what executes.

This matters because AI agents with production access will make catastrophic mistakes (see `docs/case-study-kiro-incident.md`). The question is not *if* the LLM hallucinates a dangerous action, but whether the system architecture makes that hallucination harmless.

**Joe's safety guarantees:**

- **Humans own all mutations.** No infrastructure, file, or configuration change without explicit human authorization.
- **Deterministic enforcement.** Safety rules are compiled code, not LLM instructions. Prompt injection cannot bypass them.
- **Defense in depth.** Six independent layers — RBAC, safety policy, environment-level blocking, risk tiers, human approval, and mutation circuit breaker — must all allow an operation. Any single layer blocks execution.
- **No permission inheritance.** Joe uses its own service account with pre-scoped permissions, never the calling user's credentials.
- **Default deny.** Every mutation starts disabled. Humans opt in per-action, per-environment.
- **Blast radius limits.** Operations targeting entire namespaces/clusters are categorically blocked. A circuit breaker halts runaway mutation sequences.

Full safety specification: `docs/security-in-layers.md`

---

## Design Principles

1. **Safety enforcement is hardcoded, not LLM-instructed** — Deterministic policy gates control all mutations. See [Action Safety Framework](#action-safety-framework) and `docs/security-in-layers.md`
2. **Two binaries from day one** — `joe` (Local) and `joecored` (Core daemon) in a monorepo
3. **Two agents, clear boundaries** — Core Agent maintains graph, User Agent assists users
4. **HTTP API is the contract** — Joe Local calls Joe Core via HTTP, never direct function calls
5. **Local context stays local** — User's files accessed by Joe Local only, never by Joe Core
6. **Core Agent has autonomy levels** — Deterministic changes auto-apply, ambiguous ones queue for human
7. **Humans own all mutations** — Joe never changes infrastructure, files, or configuration without explicit human authorization

---

## Two-Agent Architecture

Joe has two distinct agents with different jobs:

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                                                                          │
│  CORE AGENT (maintains infrastructure knowledge)                                        │
│  ───────────────────────────────────────────────                                        │
│                                                                                          │
│  Runs:        Server-side background daemon (joecored)                                  │
│  Triggered:   Timer, webhooks, API calls, onboarding                                    │
│  Reads:       Infrastructure (K8s, Git repos, ArgoCD, Prometheus)                       │
│  Writes:      Graph DB (nodes, edges, relationships)                                    │
│  LLM calls:   For interpretation ("what is this service?", "what connects to what?")   │
│  User interaction: None (or notifications)                                              │
│                                                                                          │
│  Jobs:                                                                                  │
│  • Onboarding - interpret user input, discover infrastructure                          │
│  • .joe/ file interpretation - understand repo context, update graph                   │
│  • Background refresh - poll sources, detect changes, update graph                     │
│  • Anomaly detection - notice issues, queue notifications                              │
│                                                                                          │
│  ✅ IMPLEMENTED: Core Agent with 5 tools, background refresh, lifecycle management     │
│                                                                                          │
└─────────────────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                                                                          │
│  USER AGENT (assists users with questions and tasks)                                    │
│  ───────────────────────────────────────────────────                                    │
│                                                                                          │
│  Runs:        Client-side (CLI, MCP Server, Web UI)                                     │
│  Triggered:   User message                                                              │
│  Reads:       Local files + Core API (graph, K8s, Git, etc.)                           │
│  Writes:      User's local files (with permission)                                      │
│  LLM calls:   For conversation and reasoning                                            │
│  User interaction: Direct chat                                                          │
│                                                                                          │
│  Tools (Local):                      Tools (via Core API):                              │
│  • read_file (user's filesystem)     • graph_query                                      │
│  • write_file (user's filesystem)    • k8s_get, k8s_list, k8s_logs                     │
│  • local_git_diff                    • git_read (cloned repos)                          │
│  • local_git_status                  • argocd_get, argocd_diff                          │
│  • run_command                       • prom_query                                       │
│                                                                                          │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

**Why two agents?**

| Need | Core Agent | User Agent |
|------|------------|------------|
| Keep graph updated when no user online | ✅ | - |
| Access user's local files | - | ✅ |
| Reason about infrastructure relationships | ✅ | Reads result |
| Answer user questions | - | ✅ |
| Run continuously | ✅ | Only when user active |

---

## Two-Binary Architecture

Joe is built as two binaries from day one, communicating via HTTP:

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                                                                          │
│  DEVELOPMENT / LOCAL                                                                     │
│                                                                                          │
│  Terminal 1:                            Terminal 2:                                     │
│  ────────────────────────────           ────────────────────────────                    │
│  $ joecored                             $ joe                                           │
│  Joe Core starting...                   Connecting to joecored...                       │
│  API listening on :7777                 Connected.                                      │
│  Core Agent started                                                                     │
│  Background refresh active              > why is payment slow?                          │
│                                         [queries core API, responds]                    │
│  [logs: refresh cycle]                                                                  │
│  [logs: API request]                    > look at my local changes                      │
│                                         [reads local files directly]                    │
│                                                                                          │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                                                                          │
│  joe (Joe Local)                         joecored (Joe Core)                            │
│  ────────────────                        ──────────────────                             │
│                                                                                          │
│  ┌────────────────────────────┐         ┌────────────────────────────────────────────┐ │
│  │  User Agent                │         │  HTTP API (:7777)                          │ │
│  │                            │         │                                            │ │
│  │  REPL ──► Agent Loop ──► LLM         │  /api/v1/graph/query                       │ │
│  │               │            │         │  /api/v1/k8s/:cluster/...                  │ │
│  │               ▼            │         │  /api/v1/argocd/...                        │ │
│  │         tool_call(...)     │         │  /api/v1/clarifications                    │ │
│  │               │            │         │  /api/v1/status                            │ │
│  │        ┌──────┴──────┐     │         │                                            │ │
│  │        ▼             ▼     │         └──────────────┬─────────────────────────────┘ │
│  │   Local Tools    Core Tools│                        │                               │
│  │   (direct)       (HTTP)────┼────────────────────────┘                               │
│  │                            │                        │                               │
│  │   • read_file              │         ┌──────────────┴─────────────────────────────┐ │
│  │   • write_file             │         │  Core Agent                                │ │
│  │   • local_git_diff         │         │                                            │ │
│  │   • local_git_status       │         │  Background:                               │ │
│  │   • run_command            │         │  • Refresh graph (every 5min)              │ │
│  │                            │         │  • Process .joe/ changes                   │ │
│  └────────────────────────────┘         │  • Detect anomalies                        │ │
│                                         │                                            │ │
│                                         │  Triggered:                                │ │
│                                         │  • Onboarding (via API)                    │ │
│                                         │  • Manual refresh (via API)                │ │
│                                         │                                            │ │
│                                         │  Clarifications:                           │ │
│                                         │  • Queue ambiguous findings                │ │
│                                         │  • Send notifications                      │ │
│                                         │                                            │ │
│                                         └──────────────┬─────────────────────────────┘ │
│                                                        │                               │
│                                         ┌──────────────┴─────────────────────────────┐ │
│                                         │  Core Services                             │ │
│                                         │                                            │ │
│                                         │  ┌──────────┐ ┌──────────┐ ┌──────────┐   │ │
│                                         │  │  Graph   │ │   SQL    │ │ Adapters │   │ │
│                                         │  │  Store   │ │  Store   │ │ K8s,Git  │   │ │
│                                         │  │ (SQLite) │ │ (SQLite) │ │ ArgoCD.. │   │ │
│                                         │  └──────────┘ └──────────┘ └──────────┘   │ │
│                                         │                                            │ │
│                                         │  ┌──────────┐                              │ │
│                                         │  │   LLM    │ (for Core Agent reasoning)  │ │
│                                         │  │ Adapter  │                              │ │
│                                         │  └──────────┘                              │ │
│                                         │                                            │ │
│                                         └────────────────────────────────────────────┘ │
│                                                                                          │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

**Why two binaries from day one:**
- Clean separation enforced by HTTP boundary
- Each component can be tested independently  
- Deployment flexibility (same machine, different machines, in-cluster)
- No "refactor to daemon later" tech debt
- API design happens upfront

---

## HTTP API Contract

Joe Local communicates with Joe Core exclusively via HTTP:

```
joecored HTTP API (default :7777)

# Status
GET  /api/v1/status                              Core status (health, version, time)

# Graph queries
GET  /api/v1/graph/query?q=...                   Query graph by string
GET  /api/v1/graph/related?nodeID=...&depth=...  Get related nodes (depth optional)
GET  /api/v1/graph/summary                       Graph summary for LLM context

# Sources
GET    /api/v1/sources                           List sources
POST   /api/v1/sources                           Register source
GET    /api/v1/sources/{id}                      Get source by ID
DELETE /api/v1/sources/{id}                      Delete and disconnect source

# Kubernetes
GET  /api/v1/k8s/{sourceID}/resources                                  List resources (kind via ?kind=)
GET  /api/v1/k8s/{sourceID}/resources/{resource}/{namespace}/{name}    Get a K8s resource
GET  /api/v1/k8s/{sourceID}/logs/{namespace}/{pod}                     Get pod logs

# Git
GET  /api/v1/git/{sourceID}/file?path=...        Read file from cloned repo
GET  /api/v1/git/{sourceID}/files?path=...       List files in directory
GET  /api/v1/git/{sourceID}/log                  Git commit log
GET  /api/v1/git/{sourceID}/diff                 Git diff

# AWS
GET  /api/v1/aws/{sourceID}/ec2/instances                Get EC2 instance list
GET  /api/v1/aws/{sourceID}/ec2/instances/{instanceID}   Get EC2 instance
GET  /api/v1/aws/{sourceID}/eks/clusters                 List EKS clusters
GET  /api/v1/aws/{sourceID}/eks/clusters/{clusterName}   Get EKS cluster
GET  /api/v1/aws/{sourceID}/rds/instances                List RDS instances
GET  /api/v1/aws/{sourceID}/rds/instances/{dbInstanceID} Get RDS instance
GET  /api/v1/aws/{sourceID}/vpc/vpcs                     List VPCs
GET  /api/v1/aws/{sourceID}/vpc/vpcs/{vpcID}             Get VPC

# Clarifications (human-in-the-loop)
GET  /api/v1/clarifications                      List pending clarifications
POST /api/v1/clarifications/{id}/answer          Answer a clarification
POST /api/v1/clarifications/{id}/dismiss         Dismiss a clarification

# Control
POST /api/v1/onboarding                          Start onboarding flow
POST /api/v1/refresh                             Trigger full or per-source refresh

# Emergency shutdown (Phase 9.1)
POST /api/v1/panic                               Trigger emergency shutdown
GET  /api/v1/panic/status                        Check safe mode state
POST /api/v1/unlock                              Exit safe mode (requires reason)

# Admin / RBAC (Phase 9.3)
GET  /api/v1/admin/zones                         List security zones
POST /api/v1/admin/zones                         Create/update zone
GET  /api/v1/admin/source-zones                  Get source→zone assignments
POST /api/v1/admin/source-zones                  Assign source to zone
GET  /api/v1/admin/source-zones/unassigned       List sources with no zone assignment
GET  /api/v1/admin/policies                      List RBAC policies
POST /api/v1/admin/policies                      Create/update RBAC policy
```

---

## REPL Commands (Joe Local)

The Joe Local REPL supports slash commands for control operations:

| Command | Description |
|---------|-------------|
| `/model` | Interactive model selector |
| `/panic` | Emergency shutdown (prompts for confirmation, then halts joecored in safe mode) |
| `/help` | Show available commands |
| `/exit`, `/quit` | Exit Joe |

### /model Command

Opens an interactive selector for switching LLM models:

```
> /model

Select model:
    claude-sonnet
  • gemini-flash (current)
    claude-opus
    ollama-llama
    
Use ↑/↓ to navigate, Enter to select, Esc to cancel
```

- Lists all models from `config.yaml` under `llm.available`
- Current model (from `llm.current`) marked with `•` and `(current)`
- Arrow keys navigate up/down
- Enter selects and hot-swaps the model (conversation continues)
- Esc cancels without changing
- After switch, User Agent uses new LLM adapter for subsequent calls

---

## Core Services

Core Services run inside `joecored` and are accessed via HTTP API:

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                                                                          │
│  CORE SERVICES                                                                          │
│                                                                                          │
│  ┌───────────────────────────────────────────────────────────────────────────────────┐  │
│  │                                                                                   │  │
│  │  type CoreServices struct {                                                       │  │
│  │      config     *Config                                                           │  │
│  │      llm        LLMAdapter                                                        │  │
│  │      graph      GraphStore                                                        │  │
│  │      store      *SQLStore                                                         │  │
│  │      adapters   *AdapterRegistry  // K8s, ArgoCD, Git, Prom, etc.                │  │
│  │  }                                                                                │  │
│  │                                                                                   │  │
│  │  // Graph operations                                                              │  │
│  │  func (c *CoreServices) GraphQuery(ctx, query) ([]Node, error)                   │  │
│  │  func (c *CoreServices) GraphRelated(ctx, nodeID, depth) (*Subgraph, error)      │  │
│  │  func (c *CoreServices) GraphAddNode(ctx, node) error           // Core Agent    │  │
│  │  func (c *CoreServices) GraphAddEdge(ctx, edge) error           // Core Agent    │  │
│  │                                                                                   │  │
│  │  // Infrastructure queries (called by User Agent via tools)                      │  │
│  │  func (c *CoreServices) K8sGet(ctx, cluster, resource, ns, name) (any, error)    │  │
│  │  func (c *CoreServices) K8sList(ctx, cluster, resource, ns) ([]any, error)       │  │
│  │  func (c *CoreServices) K8sLogs(ctx, cluster, pod, ns, lines) (string, error)    │  │
│  │  func (c *CoreServices) GitRead(ctx, repo, path) (string, error)                 │  │
│  │  func (c *CoreServices) ArgoCDGet(ctx, instance, app) (any, error)               │  │
│  │  func (c *CoreServices) PromQuery(ctx, promql) (any, error)                      │  │
│  │                                                                                   │  │
│  │  // Source management                                                             │  │
│  │  func (c *CoreServices) ListSources(ctx) ([]Source, error)                       │  │
│  │  func (c *CoreServices) RegisterSource(ctx, source) error       // Core Agent    │  │
│  │                                                                                   │  │
│  └───────────────────────────────────────────────────────────────────────────────────┘  │
│                                                                                          │
│                                         │                                                │
│         ┌───────────────────────────────┼───────────────────────────────┐               │
│         │                               │                               │               │
│         ▼                               ▼                               ▼               │
│  ┌─────────────┐                ┌─────────────┐                ┌─────────────┐          │
│  │ LLM Adapter │                │  Adapters   │                │   Stores    │          │
│  │             │                │             │                │             │          │
│  │ Claude      │                │ K8s         │                │ GraphStore  │          │
│  │ OpenAI      │                │ ArgoCD      │                │ SQLStore    │          │
│  │ Ollama      │                │ Git         │                │             │          │
│  │             │                │ Prometheus  │                │             │          │
│  └─────────────┘                └─────────────┘                └─────────────┘          │
│                                                                                          │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## Agent Definitions

### Core Agent (maintains infrastructure knowledge)

```
┌─────────────────────────────────────────────────────────────────────┐
│  Core Agent                                                          │
│  ──────────                                                          │
│                                                                      │
│  Runs: Background daemon in joecored (fully implemented)            │
│  Purpose: Keep infrastructure graph accurate and up-to-date         │
│                                                                      │
│  type CoreAgent struct {                                            │
│      services   *CoreServices                                       │
│      llm        LLMAdapter      // For reasoning during discovery   │
│      refresher  *BackgroundRefresher                                │
│      discovery  *DiscoveryEngine                                    │
│  }                                                                  │
│                                                                      │
│  // Background jobs                                                  │
│  func (a *CoreAgent) Start(ctx) error                               │
│  func (a *CoreAgent) Stop(ctx) error                                │
│  func (a *CoreAgent) ProcessOnboarding(ctx, input) error            │
│                                                                      │
│  // Trigger jobs                                                     │
│  func (a *CoreAgent) ExecuteTool(ctx, name, args) (any, error)      │
│                                                                      │
│  ✅ IMPLEMENTED Tools (for LLM reasoning):                          │
│  • graph_add_node, graph_add_edge, graph_update_node                │
│  • register_source, save_onboarding_fact                            │
│  • Background refresh with configurable interval                    │
│  • Graceful start/stop lifecycle management                         │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### User Agent (assists users)

```
┌─────────────────────────────────────────────────────────────────────┐
│  User Agent                                                          │
│  ──────────                                                          │
│                                                                      │
│  Runs: In client (CLI, MCP Server, browser)                          │
│  Purpose: Help user understand and operate infrastructure           │
│                                                                      │
│  type UserAgent struct {                                            │
│      llm          LLMAdapter                                        │
│      coreClient   CoreClient      // HTTP client in daemon mode,    │
│                                   // direct CoreServices in MVP     │
│      localTools   *LocalToolExecutor                                │
│      session      *Session                                          │
│  }                                                                  │
│                                                                      │
│  func (a *UserAgent) Chat(ctx, message string) (<-chan Chunk, error)│
│                                                                      │
│  Tools available to LLM:                                            │
│                                                                      │
│  LOCAL TOOLS (execute on client):                                   │
│  • read_file(path) → content                                        │
│  • write_file(path, content)                                        │
│  • local_git_status() → status                                      │
│  • local_git_diff(ref) → diff                                       │
│  • run_command(cmd) → output                                        │
│                                                                      │
│  CORE TOOLS (call Core Services):                                   │
│  • graph_query(query) → nodes                                       │
│  • graph_related(node, depth) → subgraph                           │
│  • k8s_get(cluster, resource, ns, name) → resource                 │
│  • k8s_logs(cluster, pod, ns) → logs                               │
│  • git_read(repo, path) → content  (remote cloned repos)           │
│  • argocd_get(app) → app details                                   │
│  • prom_query(promql) → metrics                                    │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### Review Agent (infrastructure-aware code reviews) — Phase 10

```
┌─────────────────────────────────────────────────────────────────────┐
│  Review Agent                                                        │
│  ────────────                                                        │
│                                                                      │
│  Runs: In joecored (triggered by webhook or manual request)         │
│  Purpose: Provide infrastructure-aware PR/MR reviews                │
│                                                                      │
│  Trigger methods:                                                   │
│  • Webhook: POST /api/v1/webhooks/github (PR opened/updated)        │
│  • Manual: joe review owner/repo#123                                │
│  • Manual: joe review https://github.com/owner/repo/pull/123        │
│                                                                      │
│  Flow:                                                              │
│    1. Fetch PR diff from GitHub/GitLab                              │
│    2. Parse changed files → identify affected resources             │
│       (Helm values, K8s manifests, Terraform, Dockerfiles)          │
│    3. Query graph: what services/infra depend on these?             │
│    4. Query knowledge: relevant runbooks, past incidents            │
│    5. Query live state: does change match current reality?          │
│    6. LLM analysis: generate infrastructure-aware review            │
│    7. Post review comments via GitHub/GitLab API                    │
│    8. Submit review (approve/request changes/comment)               │
│                                                                      │
│  Tools available:                                                   │
│  • github_get_pr(repo, number) → PR metadata (T1)                   │
│  • github_get_diff(repo, number) → diff content (T1)                │
│  • github_post_comment(repo, pr, file, line, body) → comment (T2)   │
│  • github_submit_review(repo, pr, status, body) → review (T3)       │
│  • All Core tools (graph_query, k8s_get, prom_query, etc.)          │
│                                                                      │
│  Example review output:                                             │
│  ─────────────────────                                              │
│  "This change sets replicas: 1 for payment-svc.                     │
│                                                                      │
│   **Graph context:**                                                │
│   - 3 services depend on payment-svc: checkout, refunds, reporting  │
│   - payment-svc connects to rds/prod/payment-db                     │
│                                                                      │
│   **Historical context:**                                           │
│   - Incident #4521 (2025-01-15): Single replica caused 23min outage │
│   - Runbook recommends minimum 2 replicas for production            │
│                                                                      │
│   **Current state:**                                                │
│   - payment-svc running 3 replicas (healthy)                        │
│   - Node maintenance scheduled tomorrow                             │
│                                                                      │
│   **Recommendation:** Keep replicas >= 2 for production stability." │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Core Agent Decision Flow

Core Agent operates with varying levels of autonomy depending on confidence:

```
┌─────────────────────────────────────────────────────────────────────┐
│  AUTONOMOUS (no human needed)                                        │
│  ────────────────────────────                                        │
│                                                                      │
│  Deterministic changes from API data:                               │
│  • New pod appears in existing deployment    → Update node metadata │
│  • Replica count changed                     → Update node metadata │
│  • ConfigMap content changed                 → Update node          │
│  • Resource deleted                          → Remove from graph    │
│  • Known deployment scaled                   → Update graph         │
│                                                                      │
│  Cached operations:                                                 │
│  • .joe/ file unchanged (cache hit)          → Replay cached calls  │
│                                                                      │
│  Explicit relationships from infra:                                 │
│  • Service selector → Pod                    → Add edge (explicit)  │
│  • ArgoCD app → Git repo                     → Add edge (explicit)  │
│  • Deployment → ConfigMap mount              → Add edge (explicit)  │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│  LLM REASONING (may need human confirmation)                        │
│  ───────────────────────────────────────────                        │
│                                                                      │
│  LLM interprets, confidence determines action:                      │
│                                                                      │
│  HIGH CONFIDENCE → Apply automatically:                             │
│  • .joe/ file clearly states relationship                           │
│  • Standard naming pattern recognized                               │
│  • Explicit annotation on K8s resource                              │
│                                                                      │
│  LOW CONFIDENCE → Queue for clarification:                          │
│  • New service discovered, purpose unclear                          │
│  • Inferred relationship (e.g., "payment" calls "user"?)            │
│  • .joe/ file is ambiguous or contradictory                         │
│  • Multiple possible interpretations                                │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│  ALWAYS REQUIRES HUMAN                                              │
│  ────────────────────                                               │
│                                                                      │
│  • Onboarding (user provides sources and context)                   │
│  • Adding new source (user provides credentials)                    │
│  • Semantic relationships ("this service handles payments")         │
│  • Business context ("this is customer-facing")                     │
│  • Destructive actions (removing sources, major graph changes)      │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### Clarification Flow

```
┌─────────────────────────────────────────────────────────────────────┐
│                                                                      │
│  Core Agent discovers something ambiguous                           │
│       │                                                              │
│       ▼                                                              │
│  Create clarification record                                        │
│       │                                                              │
│       ├──► Store in clarifications table (status: pending)          │
│       │                                                              │
│       └──► Send notification                                        │
│            • Desktop notification (if enabled)                      │
│            • Slack (if configured)                                  │
│            • Show in Joe Local on next interaction                  │
│                                                                      │
│  ─────────────────────────────────────────────────────────────────  │
│                                                                      │
│  User responds (via Joe Local or notification):                     │
│       │                                                              │
│       ▼                                                              │
│  "It's the authentication service, depends on postgres"            │
│       │                                                              │
│       ▼                                                              │
│  Core Agent processes answer:                                       │
│  1. Update clarification record (status: answered)                  │
│  2. Execute graph_operations from record                            │
│  3. Store as onboarding_fact (for future rebuild)                   │
│       │                                                              │
│       ▼                                                              │
│  Graph updated with confirmed information                           │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### Example Clarification Types

| Type | Trigger | Question | Options |
|------|---------|----------|---------|
| `new_service` | Unknown deployment found | "What is deployment/prod/mystery-svc?" | Free text |
| `edge_confirm` | LLM inferred relationship | "Does payment-svc call user-db?" | Yes / No / Not sure |
| `ambiguous_joe_file` | .joe/ file unclear | "In repo X, what does 'depends on auth' mean?" | List of possible services |
| `new_source` | Discovered reference | "Found reference to cluster 'staging'. Add as source?" | Yes / No |
| `service_purpose` | New service, unclear role | "What does order-processor do?" | Free text |

### Clarification in Joe Local

When user starts Joe Local, pending clarifications are shown:

```
$ joe
Joe is ready.

📋 Pending clarifications (2):

1. [new_service] Found deployment 'mystery-svc' in prod cluster.
   What is this service?

2. [edge_confirm] I think payment-svc calls user-db based on
   network traffic patterns. Is this correct? (yes/no)

> 1: It's the new authentication service, it talks to redis and postgres

Got it. Updated graph:
  + node: deployment/prod/mystery-svc (authentication service)
  + edge: mystery-svc → redis (depends_on, confirmed)
  + edge: mystery-svc → postgres (depends_on, confirmed)

> 2: yes

Confirmed. Added edge: payment-svc → user-db (calls, confirmed)

> why is payment slow?
[continues with normal conversation...]
```

---

## Component Details

### 1. CLI (User Agent Host)

```
┌─────────────────────────────────────────────────────────────────────┐
│  CLI (joe)                                                          │
│  ─────────                                                          │
│  Client only - connects to joecored daemon via HTTP               │
│  Location: cmd/joe/                                                  │
│                                                                      │
│  Commands:                                                          │
│    joe                     # Interactive mode (stays running)       │
│    joe init                # Run onboarding (triggers Core Agent)   │
│    joe ask "question"      # One-shot query                         │
│    joe refresh             # Force discovery refresh (Core Agent)   │
│    joe sources             # List known sources                     │
│    joe graph               # Show graph stats                       │
│    joe cache clear         # Clear .joe/ interpretation cache       │
│                                                                      │
│  Current Startup:                                                   │
│    1. Load config from ~/.joe/config.yaml                           │
│    2. Create HTTP client for joecored API                           │
│    3. Create User Agent (with Core API access)                      │
│    4. Enter REPL loop                                               │
│    5. On exit: graceful shutdown                                    │
│                                                                      │
│  REPL Loop:                                                         │
│    while true:                                                      │
│        input := readline()                                          │
│        if input == "exit": break                                    │
│        response := userAgent.Chat(ctx, input)                       │
│        stream response to stdout                                    │
│                                                                      │
│  Local tools execute here (read_file, write_file, local_git_*)     │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### 1.5 MCP Server (Claude Code / Cursor / Codex Integration)

```
┌─────────────────────────────────────────────────────────────────────┐
│  MCP Server                                                          │
│  ──────────                                                          │
│  Exposes Joe as an MCP server for AI coding assistants              │
│  Location: cmd/joe-mcp/ (planned)                                   │
│                                                                      │
│  Purpose:                                                           │
│    - Claude Code, Cursor, Codex, any MCP-compatible AI can use Joe │
│    - Provides infrastructure intelligence during coding             │
│    - Replaces need for dedicated VS Code extension                  │
│                                                                      │
│  MCP Tools exposed:                                                 │
│    joe_graph_query      Query infrastructure relationships          │
│    joe_graph_related    Traverse from a node                        │
│    joe_k8s_get          Get K8s resources                           │
│    joe_k8s_logs         Get pod logs                                │
│    joe_metrics_query    Query Prometheus/Datadog/etc                │
│    joe_logs_search      Search Loki/Splunk/etc                      │
│    joe_knowledge_search Search runbooks/tribal knowledge            │
│    joe_incidents        List PagerDuty incidents                    │
│                                                                      │
│  Architecture:                                                      │
│    Claude Code ──► MCP Server ──► joecored HTTP API                │
│                                                                      │
│  The MCP server is a thin wrapper that:                             │
│    1. Exposes Joe tools as MCP tool definitions                     │
│    2. Translates MCP tool calls to joecored HTTP API calls          │
│    3. Returns results in MCP format                                 │
│                                                                      │
│  Use cases:                                                         │
│    - "Will this Helm change break anything?" (checks graph)         │
│    - "What services depend on this API?" (traverses graph)          │
│    - "Is prod healthy right now?" (queries metrics/alerts)          │
│    - "Show me the runbook for this service" (knowledge search)      │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### 2. Session Manager

```
┌─────────────────────────────────────────────────────────────────────┐
│  Session Manager                                                     │
│  ───────────────                                                     │
│  Manages conversation sessions for User Agent.                      │
│                                                                      │
│  Location: internal/session/                                        │
│                                                                      │
│  Responsibilities:                                                  │
│    - Create/destroy sessions                                        │
│    - Maintain message history per session                           │
│    - Timeout inactive sessions                                      │
│    - Trigger summarization on session end                           │
│    - Store session summary + embedding for memory search            │
│                                                                      │
│  Session struct:                                                    │
│    ID          string                                               │
│    UserID      string             // For multi-user (daemon mode)   │
│    StartedAt   time.Time                                            │
│    Messages    []Message          // conversation history           │
│    Context     map[string]any     // working memory                 │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### 3. User Agent Loop

```
┌─────────────────────────────────────────────────────────────────────┐
│  User Agent Loop                                                     │
│  ───────────────                                                     │
│  Handles user conversation. Calls LLM, executes tools.              │
│                                                                      │
│  Location: internal/useragent/                                      │
│                                                                      │
│  Flow:                                                              │
│                                                                      │
│    User Message                                                     │
│         │                                                            │
│         ▼                                                            │
│    ┌─────────────────────────┐                                      │
│    │  Build Prompt           │                                      │
│    │  - System prompt        │                                      │
│    │  - Graph summary        │                                      │
│    │  - Tool definitions     │                                      │
│    │  - Conversation history │                                      │
│    │  - User message         │                                      │
│    └───────────┬─────────────┘                                      │
│                │                                                     │
│                ▼                                                     │
│    ┌─────────────────────────┐                                      │
│    │  Send to LLM            │◄──────────────────┐                  │
│    └───────────┬─────────────┘                   │                  │
│                │                                  │                  │
│                ▼                                  │                  │
│    ┌─────────────────────────┐                   │                  │
│    │  Response has           │                   │                  │
│    │  tool_calls?            │                   │                  │
│    └───────────┬─────────────┘                   │                  │
│                │                                  │                  │
│       ┌────────┴────────┐                        │                  │
│       │ YES             │ NO                     │                  │
│       ▼                 │                        │                  │
│    ┌──────────────┐     │                        │                  │
│    │ Execute      │     │                        │                  │
│    │ Tool Calls   │     │                        │                  │
│    └──────┬───────┘     │                        │                  │
│           │             │                        │                  │
│           ▼             │                        │                  │
│    ┌──────────────┐     │                        │                  │
│    │ Append       │─────┼────────────────────────┘                  │
│    │ Results      │     │                                           │
│    └──────────────┘     │                                           │
│                         │                                           │
│                         ▼                                           │
│               ┌──────────────────┐                                  │
│               │  Stream response │                                  │
│               │  to user         │                                  │
│               └──────────────────┘                                  │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### 4. LLM Adapter

```
┌─────────────────────────────────────────────────────────────────────┐
│  LLM Adapter                                                         │
│  ───────────                                                         │
│  Abstraction over LLM providers. Swappable backends.                │
│                                                                      │
│  Location: internal/llm/                                            │
│                                                                      │
│  Interface:                                                         │
│    type LLMAdapter interface {                                      │
│        Chat(ctx, req ChatRequest) (*ChatResponse, error)            │
│        StreamChat(ctx, req ChatRequest) (<-chan Chunk, error)       │
│        Embed(ctx, text string) ([]float32, error)                   │
│    }                                                                │
│                                                                      │
│    type ChatRequest struct {                                        │
│        SystemPrompt  string                                         │
│        Messages      []Message                                      │
│        Tools         []ToolDefinition                               │
│        MaxTokens     int                                            │
│    }                                                                │
│                                                                      │
│    type ChatResponse struct {                                       │
│        Content       string                                         │
│        ToolCalls     []ToolCall                                     │
│        Usage         TokenUsage                                     │
│    }                                                                │
│                                                                      │
│    type ToolCall struct {                                           │
│        ID    string                                                 │
│        Name  string                                                 │
│        Args  map[string]any                                         │
│    }                                                                │
│                                                                      │
│  Implementations:                                                   │
│    internal/llm/claude/     Anthropic Claude API                    │
│    internal/llm/openai/     OpenAI GPT-4                            │
│    internal/llm/ollama/     Local Ollama models                     │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### 5. Tool Executor

```
┌─────────────────────────────────────────────────────────────────────┐
│  Tool Executor                                                       │
│  ─────────────                                                       │
│  Executes tool calls from LLM or replays cached tool calls.         │
│                                                                      │
│  Location: internal/tools/                                          │
│                                                                      │
│  Core:                                                              │
│    internal/tools/executor.go     Main executor                     │
│    internal/tools/registry.go     Tool registration                 │
│                                                                      │
│  Tool Categories:                                                   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Graph Tools (internal/tools/graph/)                        │   │
│  │  ───────────                                                │   │
│  │  graph_query(query)              → matching nodes           │   │
│  │  graph_related(node_id, depth)   → subgraph                 │   │
│  │  graph_add_node(id, type, meta)  → add node                 │   │
│  │  graph_add_edge(from, to, rel)   → add edge                 │   │
│  │  graph_update_node(id, meta)     → update node              │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Source Tools (internal/tools/sources/)                     │   │
│  │  ────────────                                               │   │
│  │  register_source(type, url, name, env, ...)  → store source │   │
│  │  update_source(id, ...)                      → update       │   │
│  │  list_sources(type?, env?)                   → sources      │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  K8s Tools (internal/tools/k8s/)                            │   │
│  │  ─────────                                                  │   │
│  │  k8s_get(resource, ns, name)     → resource                 │   │
│  │  k8s_list(resource, ns)          → resources                │   │
│  │  k8s_logs(pod, ns, lines)        → logs                     │   │
│  │  k8s_events(ns)                  → events                   │   │
│  │  k8s_describe(resource, ns, name)→ description              │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Git Tools (internal/tools/git/)                            │   │
│  │  ─────────                                                  │   │
│  │  git_clone(url)                  → local_path               │   │
│  │  git_ls(repo, path)              → files                    │   │
│  │  git_read(repo, file)            → content                  │   │
│  │  git_log(repo, n)                → commits                  │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  ArgoCD Tools (internal/tools/argocd/)                      │   │
│  │  ─────────────                                              │   │
│  │  argocd_list()                   → apps                     │   │
│  │  argocd_get(app)                 → app details              │   │
│  │  argocd_diff(app)                → sync diff                │   │
│  │  argocd_sync(app)                → trigger sync [approval]  │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Telemetry Tools (internal/tools/telemetry/)                │   │
│  │  ───────────────                                            │   │
│  │  prom_query(promql)              → metrics                  │   │
│  │  prom_range(promql, start, end)  → series                   │   │
│  │  loki_query(logql, limit)        → logs                     │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Memory Tools (internal/tools/memory/)                      │   │
│  │  ────────────                                               │   │
│  │  memory_search(query)            → similar sessions         │   │
│  │  memory_store(session_summary)   → store                    │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  User Tools (internal/tools/user/)                          │   │
│  │  ──────────                                                 │   │
│  │  ask_user(question)              → answer                   │   │
│  │  notify_user(type, priority, msg)→ queued                   │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  HTTP Tools (internal/tools/http/)                          │   │
│  │  ──────────                                                 │   │
│  │  http_get(url)                   → response                 │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### 6. Discovery Engine

```
┌─────────────────────────────────────────────────────────────────────┐
│  Discovery Engine                                                    │
│  ────────────────                                                    │
│  Handles onboarding and .joe/ file processing.                      │
│                                                                      │
│  Location: internal/discovery/                                      │
│                                                                      │
│  Onboarding (internal/discovery/onboarding.go):                     │
│    Phase 1: Collect user input (structured prompts)                 │
│    Phase 2: Validate connections (ping sources)                     │
│    Phase 3: LLM exploration (timeboxed)                             │
│                                                                      │
│  .joe/ Processing (internal/discovery/joefile.go):                  │
│                                                                      │
│    func ProcessJoeFiles(repoPath string) error {                    │
│        // 1. Hash .joe/ directory                                   │
│        hash := hashDir(repoPath + "/.joe")                          │
│                                                                      │
│        // 2. Check cache                                            │
│        if cached := cache.Get(repoPath, hash); cached != nil {      │
│            // 3a. Replay cached tool calls (no LLM)                 │
│            return executor.ExecuteBatch(cached.ToolCalls)           │
│        }                                                            │
│                                                                      │
│        // 3b. LLM interprets .joe/ files                            │
│        files := readJoeFiles(repoPath)                              │
│        response := llm.Chat(buildJoeFilePrompt(files))              │
│                                                                      │
│        // 4. Execute tool calls                                     │
│        executor.ExecuteBatch(response.ToolCalls)                    │
│                                                                      │
│        // 5. Cache for next time                                    │
│        cache.Set(repoPath, hash, response.ToolCalls)                │
│                                                                      │
│        return nil                                                   │
│    }                                                                │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### 7. Background Refresh

```
┌─────────────────────────────────────────────────────────────────────┐
│  Background Refresh                                                  │
│  ──────────────────                                                  │
│  Periodic job to keep graph current.                                │
│                                                                      │
│  Location: internal/refresh/                                        │
│                                                                      │
│  Schedule: Every 5 minutes (configurable)                           │
│                                                                      │
│  Flow:                                                              │
│    1. Load sources from SQL                                         │
│    2. For each source with status="connected":                      │
│       a. Query current state via adapter                            │
│       b. Diff against existing graph nodes                          │
│       c. Categorize changes:                                        │
│          - Deterministic: apply directly                            │
│          - Ambiguous: queue for LLM                                 │
│    3. Process LLM queue (batched, budget-limited)                   │
│    4. Update timestamps                                             │
│                                                                      │
│  Deterministic (no LLM):                                            │
│    - New pod in existing deployment                                 │
│    - Replica count changed                                          │
│    - ConfigMap content changed                                      │
│    - Resource deleted                                               │
│                                                                      │
│  LLM-Required:                                                      │
│    - New deployment (what is this?)                                 │
│    - New namespace (what's its purpose?)                            │
│    - Unknown CRD                                                    │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### 8. Notification Service

```
┌─────────────────────────────────────────────────────────────────────┐
│  Notification Service                                                │
│  ────────────────────                                                │
│  Pushes notifications to user.                                      │
│                                                                      │
│  Location: internal/notify/                                         │
│                                                                      │
│  Types:                                                             │
│    graph_clarification  - Joe needs user input                      │
│    anomaly_detected     - Unusual pattern detected                  │
│    incident_likely      - Error rate, latency spike                 │
│    action_required      - Pending approval                          │
│                                                                      │
│  Channels:                                                          │
│    Desktop   - notify-send (Linux) / osascript (macOS)              │
│    Slack     - Webhook                                              │
│    CLI       - If session active                                    │
│    Web       - WebSocket (future)                                   │
│                                                                      │
│  Features:                                                          │
│    - Deduplication by type + target                                 │
│    - Throttling per channel                                         │
│    - Quiet hours                                                    │
│    - Priority thresholds per channel                                │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### 9. Adapter Layer

```
┌─────────────────────────────────────────────────────────────────────┐
│  Adapter Layer                                                       │
│  ─────────────                                                       │
│  Concrete implementations for infrastructure systems.               │
│                                                                      │
│  Location: internal/adapters/                                       │
│                                                                      │
│  Common Interface:                                                  │
│    type Adapter interface {                                         │
│        Connect(source Source) error                                 │
│        Disconnect() error                                           │
│        Status() Status                                              │
│    }                                                                │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Kubernetes (internal/adapters/k8s/) ✅ IMPLEMENTED         │   │
│  │  - Uses client-go with dynamic client                       │   │
│  │  - Multiple contexts support                                │   │
│  │  - Methods: Connect, ListResources, GetResource, GetPodLogs │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Git (internal/adapters/git/) ✅ IMPLEMENTED                │   │
│  │  - Uses go-git                                              │   │
│  │  - Methods: Connect, ReadFile, ListFiles, Log, Diff         │   │
│  │  - SSH and HTTPS auth                                       │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  AWS (internal/adapters/aws/) ✅ IMPLEMENTED                │   │
│  │  - Uses aws-sdk-go-v2                                       │   │
│  │  - EC2, EKS, RDS, ALB/NLB, VPC, CloudWatch                  │   │
│  │  - Multi-account support via profiles/roles                 │   │
│  │  - Links EC2 instances to K8s nodes in graph                │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Azure (internal/adapters/azure/) ✅ IMPLEMENTED            │   │
│  │  - Uses azure-sdk-for-go                                    │   │
│  │  - VMs, AKS, Azure SQL, VNets, NSGs, Monitor                │   │
│  │  - Multi-subscription support                               │   │
│  │  - Links VMs to K8s nodes in graph                          │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  ArgoCD (internal/adapters/argocd/) ✅ IMPLEMENTED         │   │
│  │  - REST API client                                          │   │
│  │  - Token authentication                                     │   │
│  │  - App listing, sync, diff                                  │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  OBSERVABILITY ADAPTERS                                             │
│  ──────────────────────                                             │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Prometheus/Mimir (internal/adapters/prometheus/) ✅ IMPLEMENTED│   │
│  │  - HTTP API client (compatible with Mimir, Thanos, Cortex) │   │
│  │  - Instant query, range query                               │   │
│  │  - Label discovery, series metadata                         │   │
│  │  - Methods: Query, QueryRange, Labels, Series               │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Loki (internal/adapters/loki/) ✅ IMPLEMENTED              │   │
│  │  - HTTP API client                                          │   │
│  │  - LogQL queries (filter, parse, aggregate)                 │   │
│  │  - Label discovery, stream metadata                         │   │
│  │  - Methods: Query, QueryRange, Labels, Series               │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Tempo (internal/adapters/tempo/) ✅ IMPLEMENTED            │   │
│  │  - HTTP API client                                          │   │
│  │  - TraceQL queries                                          │   │
│  │  - Trace by ID lookup                                       │   │
│  │  - Methods: GetTrace, Search, SearchTags                    │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Jaeger (internal/adapters/jaeger/) ✅ IMPLEMENTED          │   │
│  │  - HTTP/gRPC API client                                     │   │
│  │  - Trace search by service, operation, tags                 │   │
│  │  - Trace by ID lookup                                       │   │
│  │  - Methods: GetTrace, FindTraces, GetServices               │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  CloudWatch (internal/adapters/cloudwatch/) 🚧 PLANNED      │   │
│  │  - AWS SDK client                                           │   │
│  │  - Metrics: GetMetricData, ListMetrics                      │   │
│  │  - Logs: FilterLogEvents, Insights queries                  │   │
│  │  - Unified for AWS observability                            │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Azure Monitor (internal/adapters/azuremonitor/) 🚧 PLANNED │   │
│  │  - Azure SDK client                                         │   │
│  │  - Metrics: query metrics by resource                       │   │
│  │  - Logs: Log Analytics KQL queries                          │   │
│  │  - Unified for Azure observability                          │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  PROPRIETARY OBSERVABILITY                                          │
│  ─────────────────────────                                          │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Datadog (internal/adapters/datadog/) ✅ IMPLEMENTED        │   │
│  │  - REST API client (api.datadoghq.com)                      │   │
│  │  - Metrics: query, submit                                   │   │
│  │  - Logs: search, analytics                                  │   │
│  │  - APM: traces, spans, service map                          │   │
│  │  - Methods: QueryMetrics, SearchLogs, GetTrace, ListServices│   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Splunk (internal/adapters/splunk/) ✅ IMPLEMENTED          │   │
│  │  - REST API client (Splunk Enterprise / Cloud)              │   │
│  │  - Search: SPL queries                                      │   │
│  │  - Observability Cloud: metrics, traces                     │   │
│  │  - Methods: Search, GetJob, GetResults, QueryMetrics        │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Dynatrace (internal/adapters/dynatrace/) ✅ IMPLEMENTED    │   │
│  │  - REST API client (Environment API v2)                     │   │
│  │  - Metrics: query with DQL                                  │   │
│  │  - Problems: list, get details                              │   │
│  │  - Entities: hosts, services, processes                     │   │
│  │  - Methods: QueryMetrics, ListProblems, GetEntity, GetTrace │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  New Relic (internal/adapters/newrelic/) ✅ IMPLEMENTED     │   │
│  │  - NerdGraph API (GraphQL)                                  │   │
│  │  - NRQL queries for metrics, events, logs                   │   │
│  │  - Distributed tracing                                      │   │
│  │  - Methods: Query (NRQL), GetTrace, ListEntities            │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ALERTING & INCIDENT MANAGEMENT                                     │
│  ──────────────────────────────                                     │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Alertmanager (internal/adapters/alertmanager/) ✅ IMPLEMENTED│   │
│  │  - HTTP API client                                          │   │
│  │  - List alerts (active, silenced)                           │   │
│  │  - Get alert groups                                         │   │
│  │  - Create/delete silences                                   │   │
│  │  - Methods: ListAlerts, GetAlertGroups, CreateSilence       │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  PagerDuty (internal/adapters/pagerduty/) ✅ IMPLEMENTED    │   │
│  │  - REST API client                                          │   │
│  │  - Incidents: list, get, acknowledge, resolve               │   │
│  │  - On-call: who's on call now                               │   │
│  │  - Services: list, get status                               │   │
│  │  - Methods: ListIncidents, GetIncident, GetOnCall, AckIncident │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Grafana (internal/adapters/grafana/) ✅ IMPLEMENTED        │   │
│  │  - HTTP API client                                          │   │
│  │  - Dashboards: list, get, create, update                    │   │
│  │  - Alerts: list rules, get state, silence                   │   │
│  │  - Annotations: create, query                               │   │
│  │  - Datasources: list, query via unified alerting            │   │
│  │  - Methods: GetDashboard, ListAlerts, CreateAnnotation      │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  DATA STORE ADAPTERS                                                  │
│  ────────────────────                                                  │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  PostgreSQL (internal/adapters/postgres/) ✅ IMPLEMENTED    │   │
│  │  - pgx driver, read-only connection                         │   │
│  │  - pg_stat_activity, pg_stat_user_tables, pg_stat_replication│  │
│  │  - Methods: Stat, Query (SELECT only)                       │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  MySQL (internal/adapters/mysql/) ✅ IMPLEMENTED            │   │
│  │  - go-sql-driver/mysql, read-only user                      │   │
│  │  - SHOW PROCESSLIST, SHOW REPLICA STATUS, INNODB_TRX        │   │
│  │  - Methods: Stat, Query (SELECT only)                       │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Redis (internal/adapters/redis/) ✅ IMPLEMENTED            │   │
│  │  - go-redis client, operational stats only                  │   │
│  │  - INFO, SLOWLOG GET, CLIENT LIST, DBSIZE                   │   │
│  │  - Methods: Info, SlowLog                                   │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  MongoDB (internal/adapters/mongodb/) ✅ IMPLEMENTED        │   │
│  │  - mongo-driver, read-only user                             │   │
│  │  - serverStatus, rs.status, currentOp                       │   │
│  │  - Methods: Stat, Query (find only)                         │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Kafka (internal/adapters/kafka/) ✅ IMPLEMENTED            │   │
│  │  - Admin client, no message consumption                     │   │
│  │  - Topics, consumer groups, lag, broker metadata            │   │
│  │  - Methods: Topics, Consumers, Brokers                      │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Elasticsearch (internal/adapters/elasticsearch/) ✅ IMPLEMENTED│   │
│  │  - HTTP REST API (compatible with OpenSearch)               │   │
│  │  - _cluster/health, _cat/indices, _nodes/stats              │   │
│  │  - Methods: Health, Indices                                 │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  GITOPS, CD & IAC ADAPTERS                                            │
│  ─────────────────────────                                            │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  ArgoCD (internal/adapters/argocd/) ✅ IMPLEMENTED         │   │
│  │  - REST API client, token auth                              │   │
│  │  - App listing, sync status, diff, history                  │   │
│  │  - Methods: Apps, App, Diff, History                        │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Flux (via K8s CRDs) ✅ IMPLEMENTED                         │   │
│  │  - GitRepository, Kustomization, HelmRelease CRDs           │   │
│  │  - Reconciliation status, conditions                        │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Terraform (internal/adapters/terraform/) ✅ IMPLEMENTED    │   │
│  │  - State file parser (JSON), sensitive attribute redaction  │   │
│  │  - Managed resources, outputs, drift detection              │   │
│  │  - Methods: State, Resource, Outputs                        │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Helm (internal/adapters/helm/) ✅ IMPLEMENTED              │   │
│  │  - Helm v3 SDK                                              │   │
│  │  - Release listing, status, values, revision history        │   │
│  │  - Methods: Releases, Release, History                      │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  NETWORKING & INGRESS ADAPTERS                                        │
│  ─────────────────────────────                                        │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  NGINX Ingress (internal/adapters/nginx/) ✅ IMPLEMENTED    │   │
│  │  - K8s Ingress CRDs + NGINX status endpoint                │   │
│  │  - Ingress rules, backends, upstream health                 │   │
│  │  - Methods: Ingresses, Status, Config                       │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Envoy (internal/adapters/envoy/) ✅ IMPLEMENTED            │   │
│  │  - Admin API (/config_dump, /clusters, /stats)              │   │
│  │  - Cluster health, endpoints, circuit breaker state         │   │
│  │  - Methods: Clusters, Config, Stats                         │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Istio (via K8s CRDs) ✅ IMPLEMENTED                        │   │
│  │  - VirtualService, DestinationRule, Gateway CRDs            │   │
│  │  - mTLS status, traffic policies                            │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Cilium (internal/adapters/cilium/) ✅ IMPLEMENTED          │   │
│  │  - CiliumNetworkPolicy CRDs + Hubble API                   │   │
│  │  - Network policies, endpoint health, flow visibility       │   │
│  │  - Methods: Policies, Endpoints, Flows                      │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  K8S CRD-BASED ADAPTERS (LOW EFFORT)                                  │
│  ───────────────────────────────────                                  │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  cert-manager (via K8s CRDs) ✅ IMPLEMENTED                 │   │
│  │  - Certificate, Issuer, ClusterIssuer CRDs                  │   │
│  │  - Expiry, readiness, issuer status                         │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  KEDA (via K8s CRDs) ✅ IMPLEMENTED                         │   │
│  │  - ScaledObject, ScaledJob, TriggerAuthentication CRDs      │   │
│  │  - Scaling targets, triggers, replica counts                │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  OPA/Gatekeeper (via K8s CRDs) ✅ IMPLEMENTED               │   │
│  │  - ConstraintTemplate, Constraint CRDs                      │   │
│  │  - Constraint violations from audit                         │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Crossplane (via K8s CRDs) ✅ IMPLEMENTED                   │   │
│  │  - Provider, ProviderConfig, XRD, Claims CRDs               │   │
│  │  - Provider health, resource sync status                    │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  SECURITY & RUNTIME ADAPTERS                                          │
│  ────────────────────────────                                          │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Falco (internal/adapters/falco/) ✅ IMPLEMENTED            │   │
│  │  - gRPC/HTTP output API                                     │   │
│  │  - Runtime security events by severity/rule                 │   │
│  │  - Methods: Alerts, Rules                                   │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  OTHER                                                                │
│  ─────                                                                │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  HTTP (internal/adapters/http/)                             │   │
│  │  - Generic HTTP client                                      │   │
│  │  - For status pages, external APIs                          │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### Cloud Adapter Details

Cloud adapters discover infrastructure that backs Kubernetes clusters and applications.

**AWS Adapter Node Types:**

| Node Type | Example ID | Key Edges |
|-----------|------------|-----------|
| aws/{account}/ec2/{instance-id} | aws/prod/ec2/i-abc123 | is_k8s_node, in_vpc, has_sg |
| aws/{account}/eks/{cluster} | aws/prod/eks/main | in_vpc, has_nodegroup |
| aws/{account}/eks/nodegroup/{name} | aws/prod/eks/ng-workers | in_cluster, in_subnet |
| aws/{account}/rds/{db-id} | aws/prod/rds/payments-db | in_vpc, has_sg |
| aws/{account}/alb/{lb-name} | aws/prod/alb/main-lb | in_vpc, targets_service |
| aws/{account}/vpc/{vpc-id} | aws/prod/vpc/vpc-123 | has_subnet, peers_with |
| aws/{account}/sg/{sg-id} | aws/prod/sg/sg-web | allows_from, allows_to |

**Azure Adapter Node Types:**

| Node Type | Example ID | Key Edges |
|-----------|------------|-----------|
| azure/{subscription}/vm/{name} | azure/prod/vm/aks-node-1 | is_k8s_node, in_vnet |
| azure/{subscription}/aks/{name} | azure/prod/aks/main | in_vnet, has_nodepool |
| azure/{subscription}/sql/{name} | azure/prod/sql/payments | in_vnet, has_pe |
| azure/{subscription}/vnet/{name} | azure/prod/vnet/main | has_subnet, peers_with |
| azure/{subscription}/nsg/{name} | azure/prod/nsg/web | allows_from, allows_to |
| azure/{subscription}/appgw/{name} | azure/prod/appgw/main | in_vnet, targets_service |

**Key Edge: is_k8s_node**

This edge links cloud compute (EC2/VM) to Kubernetes nodes, enabling Joe to traverse from K8s problems to cloud infrastructure:

```
k8s/prod/node/ip-10-0-1-5 ──is_instance──► aws/prod/ec2/i-abc123
                                                │
                                                ├── in_vpc ──► aws/prod/vpc/vpc-123
                                                ├── has_sg ──► aws/prod/sg/sg-nodes
                                                └── in_subnet ──► aws/prod/subnet/private-1a
```

### Observability Adapter Details

Observability adapters query metrics, logs, and traces. Each is specific to its backend — the LLM picks the right tool based on graph context.

**Graph Linking:**

Services link to their observability sources via edges:

```
k8s/prod/deploy/payment-svc
    ├── metrics_in ──► prometheus/prod
    ├── logs_in ──► loki/prod
    └── traces_in ──► tempo/prod
```

When Joe investigates payment-svc, it sees these edges and knows which tools to use.

**Prometheus/Mimir Tools:**

| Tool | Parameters | Use Case |
|------|------------|----------|
| prom_query | source_id, query (PromQL), time | Instant metric value |
| prom_query_range | source_id, query, start, end, step | Metric over time |
| prom_labels | source_id, label_name | Discover label values |

Example queries Joe might run:
- `rate(http_requests_total{service="payment-svc",status="500"}[5m])`
- `histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))`
- `container_memory_usage_bytes{pod=~"payment-.*"}`

**Loki Tools:**

| Tool | Parameters | Use Case |
|------|------------|----------|
| loki_query | source_id, query (LogQL), limit, start, end | Search logs |
| loki_labels | source_id, label_name | Discover label values |

Example queries:
- `{namespace="payments"} |= "error" | json | level="error"`
- `{app="payment-svc"} | pattern "<_> status=<status> <_>" | status >= 500`

**Tempo Tools:**

| Tool | Parameters | Use Case |
|------|------------|----------|
| tempo_trace | source_id, trace_id | Get full trace by ID |
| tempo_search | source_id, service, operation, tags, min_duration | Find traces |

**Jaeger Tools:**

| Tool | Parameters | Use Case |
|------|------------|----------|
| jaeger_trace | source_id, trace_id | Get full trace by ID |
| jaeger_search | source_id, service, operation, tags, start, end | Find traces |
| jaeger_services | source_id | List available services |

**CloudWatch Tools:**

| Tool | Parameters | Use Case |
|------|------------|----------|
| cloudwatch_metrics | source_id, namespace, metric, dimensions, stat, period | Get metric data |
| cloudwatch_logs | source_id, log_group, query (Insights), start, end | Query logs |

**Azure Monitor Tools:**

| Tool | Parameters | Use Case |
|------|------------|----------|
| azure_metrics | source_id, resource_id, metric, aggregation, interval | Get metric data |
| azure_logs | source_id, workspace_id, query (KQL) | Query Log Analytics |

**Datadog Tools:**

| Tool | Parameters | Use Case |
|------|------------|----------|
| datadog_metrics | source_id, query, from, to | Query metrics |
| datadog_logs | source_id, query, from, to, limit | Search logs |
| datadog_trace | source_id, trace_id | Get trace by ID |
| datadog_services | source_id | List APM services |

**Splunk Tools:**

| Tool | Parameters | Use Case |
|------|------------|----------|
| splunk_search | source_id, query (SPL), earliest, latest | Run search |
| splunk_job_results | source_id, job_id | Get async job results |

**Dynatrace Tools:**

| Tool | Parameters | Use Case |
|------|------------|----------|
| dynatrace_metrics | source_id, selector, from, to | Query metrics (DQL) |
| dynatrace_problems | source_id, status | List problems |
| dynatrace_entity | source_id, entity_id | Get entity details |
| dynatrace_trace | source_id, trace_id | Get distributed trace |

**New Relic Tools:**

| Tool | Parameters | Use Case |
|------|------------|----------|
| newrelic_query | source_id, nrql, account_id | Run NRQL query |
| newrelic_trace | source_id, trace_id | Get distributed trace |
| newrelic_entities | source_id, query | Search entities |

**Alertmanager Tools:**

| Tool | Parameters | Use Case |
|------|------------|----------|
| alertmanager_alerts | source_id, filter, silenced, active | List alerts |
| alertmanager_groups | source_id | Get alert groups |
| alertmanager_silence | source_id, matchers, duration, comment | Create silence |

**PagerDuty Tools:**

| Tool | Parameters | Use Case |
|------|------------|----------|
| pagerduty_incidents | source_id, status, service_ids | List incidents |
| pagerduty_incident | source_id, incident_id | Get incident details |
| pagerduty_oncall | source_id, schedule_ids | Who's on call |
| pagerduty_ack | source_id, incident_id | Acknowledge incident |

**Grafana Tools:**

| Tool | Parameters | Use Case |
|------|------------|----------|
| grafana_dashboards | source_id, folder_id, query | List/search dashboards |
| grafana_dashboard | source_id, uid | Get dashboard JSON |
| grafana_alerts | source_id, state | List alert rules |
| grafana_annotations | source_id, dashboard_id, from, to | Get annotations |
| grafana_annotate | source_id, dashboard_id, text, tags | Create annotation |

**Graph Edges for Alerting:**

```
k8s/prod/deploy/payment-svc
    ├── metrics_in ──► prometheus/prod
    ├── alerts_in ──► alertmanager/prod
    ├── paged_via ──► pagerduty/prod
    └── dashboard_in ──► grafana/prod
```

**How Joe Uses Observability:**

```
> why is payment-svc returning 500s?

[Joe queries graph: payment-svc → metrics_in → prometheus/prod]
[Joe calls prom_query: rate(http_requests_total{status="500"}[5m])]

[Joe queries graph: payment-svc → logs_in → loki/prod]
[Joe calls loki_query: {app="payment-svc"} |= "error" | json]

[Joe finds trace_id in error log]
[Joe queries graph: payment-svc → traces_in → tempo/prod]
[Joe calls tempo_trace: trace_id=abc123]

The 500s correlate with auth-service latency. Traces show auth-service 
responding in 2.3s (p99). Auth-service metrics show memory at 95%.
```

---

## Data Layer

### Graph Store (SQLite-based)

```
┌─────────────────────────────────────────────────────────────────────┐
│  Graph Store                                                         │
│  ───────────                                                         │
│  Stores infrastructure topology using SQLite tables.                │
│                                                                      │
│  Location: internal/graph/                                          │
│  Storage: Uses graph_nodes and graph_edges tables in joe.db         │
│                                                                      │
│  Interface:                                                         │
│    type GraphStore interface {                                      │
│        AddNode(node Node) error                                     │
│        AddEdge(edge Edge) error                                     │
│        GetNode(id string) (*Node, error)                            │
│        Query(q string) ([]Node, error)                              │
│        Related(nodeID string, depth int) (*Subgraph, error)         │
│        Path(from, to string) ([]Edge, error)                        │
│        DeleteNode(id string) error                                  │
│        DeleteEdge(from, to, relation string) error                  │
│        Summary() GraphSummary  // For LLM context                   │
│    }                                                                │
│                                                                      │
│  Implementation: SQLiteStore (sqlite.go)                            │
│    - Uses graph_nodes and graph_edges tables                        │
│    - Supports type-based queries (type:deployment)                  │
│    - Implements graph traversal for related nodes                   │
│    - Path finding between nodes                                     │
│    - Graph summaries for LLM context                                │
│                                                                      │
│  Node:                                                              │
│    ID        string            // "deployment/payments/payment-svc" │
│    Type      string            // "deployment"                      │
│    SourceID  string            // "k8s/prod-us"                     │
│    Metadata  map[string]any                                         │
│    FirstSeen time.Time                                              │
│    LastSeen  time.Time                                              │
│                                                                      │
│  Edge:                                                              │
│    From       string                                                │
│    To         string                                                │
│    Relation   string           // "calls", "depends_on", etc.       │
│    Confidence string           // "explicit", "inferred", "confirmed"│
│    Source     string           // "k8s_api", "llm", "user"          │
│    Context    string           // Why this edge exists              │
│                                                                      │
│  Node Types:                                                        │
│    deployment, statefulset, daemonset, service, ingress,            │
│    configmap, secret, argocd_app, git_repo, kafka_topic,            │
│    external_service                                                 │
│                                                                      │
│  Relation Types:                                                    │
│    calls, depends_on, references, deploys, manages,                 │
│    defines, produces, consumes, exposes, routes_to                  │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### SQL Store (SQLite)

```
┌─────────────────────────────────────────────────────────────────────┐
│  SQL Store                                                           │
│  ─────────                                                           │
│  Stores relational data.                                            │
│                                                                      │
│  Location: internal/store/                                          │
│  File: ~/.joe/joe.db                                                │
│                                                                      │
│  Tables:                                                            │
│                                                                      │
│  sources                         Source registry                    │
│  ──────────────────────────────────────────────────────────────    │
│  id                  TEXT PK     "k8s/prod-us"                      │
│  type                TEXT        "kubernetes"                       │
│  url                 TEXT        cluster API URL                    │
│  name                TEXT        "Production US"                    │
│  environment         TEXT        "prod"                             │
│  categories          JSON        ["orchestration"]                  │
│  connection_details  JSON        {kubeconfig, context}              │
│  status              TEXT        "connected"                        │
│  discovered_from     TEXT        "user_input" or source_id          │
│  discovery_context   TEXT        "User provided during onboarding"  │
│  last_connected      TIMESTAMP                                      │
│  created_at          TIMESTAMP                                      │
│                                                                      │
│  source_secrets                  Encrypted credentials              │
│  ──────────────────────────────────────────────────────────────    │
│  source_id           TEXT PK                                        │
│  secret_type         TEXT        "token", "ssh_key"                 │
│  encrypted_value     BLOB                                           │
│                                                                      │
│  sessions                        Chat session history               │
│  ──────────────────────────────────────────────────────────────    │
│  id                  TEXT PK                                        │
│  user_id             TEXT        For multi-user (daemon mode)       │
│  started_at          TIMESTAMP                                      │
│  ended_at            TIMESTAMP                                      │
│  summary             TEXT        LLM-generated summary              │
│  issue               TEXT        What was the problem               │
│  root_cause          TEXT        What caused it                     │
│  resolution          TEXT        How it was resolved                │
│  components          JSON        Involved graph nodes               │
│  tags                JSON        For categorization                 │
│  embedding           BLOB        For similarity search              │
│                                                                      │
│  clarifications                  Human confirmation queue           │
│  ──────────────────────────────────────────────────────────────    │
│  id                  TEXT PK     UUID                               │
│  type                TEXT        "new_service", "edge_confirm",     │
│                                  "ambiguous_joe_file", "new_source" │
│  context             JSON        What was discovered                │
│  question            TEXT        Human-readable question            │
│  options             JSON        Suggested answers (if applicable)  │
│  status              TEXT        "pending", "answered", "dismissed" │
│  answer              TEXT        Human response                     │
│  answered_by         TEXT        user_id                            │
│  answered_at         TIMESTAMP                                      │
│  graph_operations    JSON        Operations to apply when answered  │
│  created_at          TIMESTAMP                                      │
│  notified_at         TIMESTAMP   When notification was sent         │
│                                                                      │
│  onboarding_input                Raw onboarding data                │
│  ──────────────────────────────────────────────────────────────    │
│  id                  INT PK                                         │
│  phase               INT         1, 2, or 3                         │
│  data                JSON        User input for that phase          │
│  created_at          TIMESTAMP                                      │
│                                                                      │
│  onboarding_facts                For graph rebuild                  │
│  ──────────────────────────────────────────────────────────────    │
│  id                  INT PK                                         │
│  statement           TEXT        Raw user statement                 │
│  graph_operations    JSON        Tool calls to replay               │
│  confirmed           BOOL                                           │
│  created_at          TIMESTAMP                                      │
│                                                                      │
│  joe_file_cache                  .joe/ interpretation cache         │
│  ──────────────────────────────────────────────────────────────    │
│  repo_id             TEXT                                           │
│  joe_dir_hash        TEXT        SHA256 of .joe/ contents           │
│  tool_calls          JSON        Cached LLM tool calls              │
│  cached_at           TIMESTAMP                                      │
│  PRIMARY KEY (repo_id, joe_dir_hash)                                │
│                                                                      │
│  audit_log                       Action audit trail                 │
│  ──────────────────────────────────────────────────────────────    │
│  id                  INT PK                                         │
│  timestamp           TIMESTAMP                                      │
│  session_id          TEXT                                           │
│  action              TEXT        "k8s_apply", "argocd_sync"         │
│  target              TEXT        Resource affected                  │
│  args                JSON        Tool call arguments                │
│  dry_run             BOOL                                           │
│  approved            BOOL                                           │
│  result              TEXT                                           │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### Knowledge Store

The Knowledge Store captures tribal knowledge, runbooks, and learned insights with different trust tiers:

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                           KNOWLEDGE TIERS                                        │
├─────────────────────────────────────────────────────────────────────────────────┤
│                                                                                  │
│  TIER 1: Human-Curated (highest trust)                                          │
│  ─────────────────────────────────────                                          │
│  • Node notes attached by humans                                                │
│  • Explicit facts from onboarding                                               │
│  • Human-approved session learnings                                             │
│  → Never auto-modified, human deletes/updates                                   │
│                                                                                  │
│  TIER 2: Synced Sources (external source of truth)                              │
│  ─────────────────────────────────────────────────                              │
│  • Company wiki / Confluence                                                    │
│  • Runbooks                                                                     │
│  • Standards docs, ADRs                                                         │
│  → Joe fetches periodically, re-parses on change                               │
│  → External doc is truth, Joe's copy is cache                                  │
│                                                                                  │
│  TIER 3: LLM-Derived (lower trust, visible provenance)                          │
│  ────────────────────────────────────────────────────                           │
│  • Patterns extracted from sessions                                             │
│  • Tribal knowledge from conversations                                          │
│  • Inferred relationships                                                       │
│  → LLM manages autonomously                                                     │
│  → Shows provenance: "Learned from session 2025-02-10"                         │
│  → Can be promoted to Tier 1 if human confirms                                 │
│  → Auto-decays if contradicted                                                 │
│                                                                                  │
└─────────────────────────────────────────────────────────────────────────────────┘
```

**Schema:**

```
knowledge_items
──────────────────────────────────────────────────────────────
id                  TEXT PK     UUID
tier                TEXT        "curated" | "synced" | "derived"
type                TEXT        "note" | "runbook" | "standard" | "pattern" | "tribal"
subject             TEXT        Service name, topic, etc.
content             TEXT        The knowledge itself
source_url          TEXT        For synced (Confluence URL, etc.)
source_session_id   TEXT        For derived (which session)
content_hash        TEXT        For synced (detect changes)
confidence          TEXT        For derived: "high" | "medium" | "low"
related_node_ids    JSON        Links to graph nodes
created_at          TIMESTAMP
updated_at          TIMESTAMP
synced_at           TIMESTAMP   For synced sources
invalidated_at      TIMESTAMP   Soft delete for derived
```

**LLM Autonomy Rules:**

| Tier | LLM Can | LLM Cannot |
|------|---------|------------|
| Tier 1 (Curated) | Read | Modify, delete |
| Tier 2 (Synced) | Re-fetch, re-parse, update cache, link nodes | Change source of truth |
| Tier 3 (Derived) | Create, invalidate if contradicted, adjust confidence | Delete permanently, promote to Tier 1 |

**Write Capabilities:**

Joe can propose documentation updates based on knowledge:

```
┌─────────────────────────────────────────────────────────────────┐
│                    KNOWLEDGE → DOCS FLOW                         │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   Session Resolution                                            │
│         │                                                        │
│         ▼                                                        │
│   Joe extracts insight                                          │
│   "Connection pool exhaustion causes timeouts"                  │
│         │                                                        │
│         ▼                                                        │
│   Stored as Tier 3 (derived)                                    │
│         │                                                        │
│         ▼                                                        │
│   Joe notices runbook missing this info                         │
│         │                                                        │
│         ▼                                                        │
│   Joe proposes update ───────────► Human reviews                │
│         │                                 │                      │
│         │                                 ▼                      │
│         │                          Approves / Rejects            │
│         │                                 │                      │
│         ▼                                 ▼                      │
│   If approved:                     Update pushed to wiki        │
│   • Confluence/Notion API                                       │
│   • Or: Git PR for docs-as-code                                 │
│         │                                                        │
│         ▼                                                        │
│   Tier 2 re-syncs updated doc                                   │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

**Write Trust Levels:**

| Action | Human Approval Required |
|--------|------------------------|
| Create draft doc | No (it's a draft) |
| Suggest edit in chat | No (just a suggestion) |
| Update Tier 3 insights | No (LLM-managed) |
| Publish to wiki | Yes |
| Update existing runbook | Yes |
| Delete/archive doc | Yes |

**Retrieval Priority:**

When Joe investigates a service, knowledge is retrieved in order:
1. Tier 1 notes attached to the service node
2. Tier 2 synced sources mentioning the service
3. Tier 3 derived insights about the service

Each tier is presented with provenance: "According to your runbook..." vs "From a previous incident..."

---

## Concurrency Model

Joe is designed for concurrent multi-user access. Understanding the concurrency model is critical for safely implementing features like PR reviews and background jobs.

### What's Already Thread-Safe

| Component | Concurrency Model | Notes |
|-----------|------------------|-------|
| HTTP Server | goroutine-per-request | Go's http.Server handles this |
| SQLite | WAL mode with serialized writes | Concurrent reads safe, writes queue |
| Adapter state | Mutex-protected | Connection state in adapters uses sync.Mutex |
| External API calls | Stateless | K8s, GitHub, Prometheus calls are independent |
| Context cancellation | Propagated | Each request has isolated context |

### Areas Requiring Coordination

**1. Graph Mutations During Background Refresh**

```
Timeline:
  00:00  Background refresh starts, reading K8s state
  00:01  User request: "add note to payment-svc node"
  00:02  Refresh writes: delete stale nodes, add new nodes
  00:03  User request writes: update payment-svc metadata

Problem: Refresh might delete/recreate node that user is annotating
```

**Solution:** Optimistic locking with node version field. User writes fail if node version changed; retry with fresh data.

**2. Clarification Answer Races**

```
User A: GET /clarifications → sees #42 pending
User B: GET /clarifications → sees #42 pending
User A: POST /clarifications/42/answer {answer: "yes"}
User B: POST /clarifications/42/answer {answer: "no"}  ← Should fail
```

**Solution:** Optimistic locking via WHERE status = 'pending':
```sql
UPDATE clarifications SET status = 'answered', answer = ?
WHERE id = ? AND status = 'pending'
-- Returns 0 rows if already answered → return 409 Conflict
```

**3. Circuit Breaker Counter**

The mutation circuit breaker must use atomic operations:
```go
// Correct: atomic increment
func (cb *CircuitBreaker) RecordMutation() bool {
    count := atomic.AddInt64(&cb.count, 1)
    return count <= cb.max
}
```

### Job Queue Pattern (for Webhooks, PR Reviews)

For background processing (webhooks, PR reviews, scheduled tasks), use a SQLite-backed job queue with single worker:

```
┌─────────────────────────────────────────────────────────────────────┐
│  Webhook Receiver                                                    │
│  ─────────────────                                                   │
│  POST /api/v1/webhooks/github                                       │
│    1. Validate signature (HMAC)                                     │
│    2. Check delivery_id (dedupe)                                    │
│    3. Parse event                                                   │
│    4. INSERT into job queue                                         │
│    5. Return 202 Accepted (don't block)                             │
└───────────────────────────────────────────────────────────────────┬─┘
                                                                    │
                                                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│  Job Queue (SQLite table)                                           │
│  ─────────────────────────                                          │
│  review_jobs:                                                       │
│    id, repo, pr_number, commit_sha,                                │
│    status (queued|in_progress|completed|failed),                   │
│    created_at, started_at, completed_at, error                     │
│                                                                      │
│  webhook_events (for deduplication):                                │
│    delivery_id PK, event_type, received_at, processed_at           │
└───────────────────────────────────────────────────────────────────┬─┘
                                                                    │
                                                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│  Job Worker (single goroutine)                                      │
│  ─────────────────────────────                                      │
│  Loop:                                                              │
│    1. SELECT ... WHERE status = 'queued' ORDER BY created_at       │
│    2. UPDATE status = 'in_progress' WHERE id = ? AND status = 'queued'│
│    3. If 0 rows updated → job claimed by another worker, skip      │
│    4. Execute job (Review Agent, etc.)                              │
│    5. UPDATE status = 'completed' or 'failed'                       │
│                                                                      │
│  Single worker = no job races                                       │
│  Multiple workers = use SELECT FOR UPDATE or advisory locks         │
└─────────────────────────────────────────────────────────────────────┘
```

**Why single worker?** Simpler, no job races, sufficient throughput for PR reviews (not high-volume). Scale to multiple workers later if needed with pessimistic locking.

### Idempotency Requirements

| Operation | Idempotency Key | Behavior on Duplicate |
|-----------|----------------|----------------------|
| GitHub webhook | X-GitHub-Delivery header | Skip (already processed) |
| GitLab webhook | X-Gitlab-Event-UUID header | Skip (already processed) |
| PR review job | (repo, pr_number, commit_sha) | Skip if in_progress/completed |
| Clarification answer | (id, status=pending) | 409 Conflict if already answered |

### Summary: Concurrency Checklist for New Features

- [ ] HTTP handlers: Rely on goroutine-per-request isolation
- [ ] Database writes: Use transactions, optimistic or pessimistic locking
- [ ] Shared counters: Use atomic operations
- [ ] Background jobs: Use job queue with idempotency tracking
- [ ] Webhooks: Validate signature, dedupe by delivery ID, respond 202 fast
- [ ] Long-running work: Use context cancellation, respect panic shutdown

---

## Directory Structure

```
joe/
├── cmd/
│   ├── joe/                      # Joe Local (User Agent CLI)
│   │   └── main.go               # Connects to joecored, runs REPL
│   │
│   ├── joecored/                 # Joe Core (daemon)
│   │   └── main.go               # Starts API server, Core Agent
│   │
│   └── joe-security/             # Security Service (optional, for hardened deployments)
│       └── main.go               # Separate process with own DB
│
├── internal/
│   ├── api/                      # HTTP API (for joecored)
│   │   ├── server.go             # HTTP server setup
│   │   ├── handlers.go           # Route handlers
│   │   ├── middleware.go         # Logging, auth
│   │   └── admin.go              # Admin API handlers (zones, policies)
│   │
│   ├── security/                 # Security policy (pluggable)
│   │   ├── interface.go          # SecurityPolicy, SecurityAdmin interfaces
│   │   ├── embedded.go           # EmbeddedSecurityPolicy (same DB)
│   │   ├── remote.go             # RemoteSecurityPolicy (gRPC client)
│   │   └── zones.go              # Zone definitions and evaluation
│   │
│   ├── securitysvc/              # joe-security server (for remote mode)
│   │   ├── server.go             # gRPC/HTTP server
│   │   ├── store.go              # security.db access
│   │   └── admin.go              # Admin API implementation
│   │
│   ├── client/                   # HTTP client (for joe)
│   │   └── client.go             # CoreClient HTTP implementation
│   │
│   ├── core/                     # Core Services
│   │   └── services.go           # CoreServices struct
│   │
│   ├── coreagent/                # Core Agent
│   │   ├── agent.go              # CoreAgent struct
│   │   ├── refresh.go            # Background refresh
│   │   ├── discovery.go          # .joe/ processing
│   │   └── onboarding.go         # Onboarding flow
│   │
│   ├── useragent/                # User Agent
│   │   ├── agent.go              # UserAgent struct
│   │   ├── loop.go               # Agentic loop
│   │   └── prompt.go             # Prompt building
│   │
│   ├── session/                  # Session management
│   │   └── session.go
│   │
│   ├── llm/                      # LLM adapters (used by both agents)
│   │   ├── adapter.go            # Interface
│   │   ├── claude/
│   │   │   └── claude.go
│   │   ├── openai/
│   │   │   └── openai.go
│   │   └── ollama/
│   │       └── ollama.go
│   │
│   ├── tools/                    # Tool implementations
│   │   ├── executor.go           # Tool executor
│   │   ├── registry.go           # Tool registry
│   │   ├── local/                # LOCAL TOOLS (run in joe)
│   │   │   ├── readfile.go
│   │   │   ├── writefile.go
│   │   │   ├── gitdiff.go
│   │   │   ├── gitstatus.go
│   │   │   └── runcmd.go
│   │   └── core/                 # CORE TOOLS (call joecored API)
│   │       ├── graphquery.go
│   │       ├── graphrelated.go
│   │       ├── k8sget.go
│   │       ├── k8slogs.go
│   │       ├── gitread.go
│   │       ├── argocdget.go
│   │       ├── promquery.go       # Prometheus/Mimir
│   │       ├── lokiquery.go       # Loki
│   │       ├── tempotrace.go      # Tempo/Jaeger
│   │       ├── datadog_tools.go   # Datadog
│   │       ├── splunk_tools.go    # Splunk
│   │       ├── falco_tools.go     # Falco
│   │       ├── knowledge_search.go# Knowledge Store
│   │       └── ...                # (many more: certmanager, keda, opa, crossplane, registry, etc.)
│   │
│   ├── graph/                    # Graph store (used by joecored)
│   │   ├── store.go              # Interface
│   │   └── sqlite.go             # SQLite implementation
│   │
│   ├── store/                    # SQL store (used by joecored)
│   │   ├── store.go
│   │   ├── sources.go
│   │   ├── sessions.go
│   │   ├── clarifications.go
│   │   ├── cache.go
│   │   └── migrations/
│   │
│   ├── adapters/                 # Infrastructure adapters (used by joecored)
│   │   ├── k8s/                  # ✅ Implemented
│   │   ├── git/                  # ✅ Implemented
│   │   ├── aws/                  # ✅ Implemented (EC2, EKS, RDS, VPC)
│   │   ├── azure/                # ✅ Implemented (VMs, AKS, SQL, VNets)
│   │   ├── gitops/argocd/        # ✅ Implemented
│   │   ├── packaging/helm/       # ✅ Implemented
│   │   ├── iac/terraform/        # ✅ Implemented
│   │   ├── observability/
│   │   │   ├── prometheus/       # ✅ Implemented (also covers Mimir)
│   │   │   ├── loki/             # ✅ Implemented
│   │   │   ├── tempo/            # ✅ Implemented
│   │   │   ├── jaeger/           # ✅ Implemented
│   │   │   ├── datadog/          # ✅ Implemented
│   │   │   ├── splunk/           # ✅ Implemented
│   │   │   ├── dynatrace/        # ✅ Implemented
│   │   │   └── newrelic/         # ✅ Implemented
│   │   ├── alerting/
│   │   │   ├── alertmanager/     # ✅ Implemented
│   │   │   ├── pagerduty/        # ✅ Implemented
│   │   │   └── grafana/          # ✅ Implemented
│   │   ├── datastore/            # ✅ Implemented (PostgreSQL, MySQL, Redis, MongoDB, Kafka, Elasticsearch)
│   │   ├── networking/
│   │   │   ├── nginx/            # ✅ Implemented
│   │   │   └── envoy/            # ✅ Implemented
│   │   ├── security/falco/       # ✅ Implemented
│   │   ├── registry/
│   │   │   ├── oci/              # ✅ Implemented (DockerHub, GHCR, Harbor, Quay)
│   │   │   ├── artifactory/      # ✅ Implemented
│   │   │   └── ecr/              # ✅ Implemented
│   │   └── http/                 # Generic HTTP client
│   │
│   ├── repl/                     # REPL (used by joe)
│   │   └── repl.go
│   │
│   ├── notify/                   # Notifications (used by joecored)
│   │   ├── service.go
│   │   ├── desktop.go
│   │   └── slack.go
│   │
│   └── config/                 # Configuration
│       └── config.go
│
├── ui/                           # Web UI (React + TypeScript)
│   ├── src/
│   │   ├── api/                  # API client + types
│   │   ├── components/           # React components
│   │   │   ├── ui/               # shadcn/ui components
│   │   │   ├── graph/            # Infrastructure graph (React Flow)
│   │   │   ├── dashboard/        # Dashboard widgets
│   │   │   ├── admin/            # Security zones, policies
│   │   │   └── chat/             # Chat interface
│   │   ├── pages/                # Route pages
│   │   ├── hooks/                # Custom React hooks
│   │   └── lib/                  # Utilities
│   ├── package.json
│   ├── vite.config.ts
│   └── tailwind.config.js
│
├── docs/
│   ├── architecture.md         # This file
│   ├── web-ui.md               # Web UI specification
│   ├── joe-dataflow.md
│   └── joe-prompt.md
│
├── go.mod
├── go.sum
└── README.md
```

---

## Configuration Files

```
~/.joe/
├── config.yaml                 # User configuration
├── joe.db                      # SQLite database (includes graph)
└── repos/                      # Cloned git repos
    └── <host>/<owner>/<repo>/
```

**config.yaml:**

```yaml
# LLM Configuration
llm:
  current: claude-sonnet          # Currently active model (key from 'available')
  available:                      # All configured models
    claude-sonnet:
      provider: claude
      model: claude-sonnet-4-20250514
      api_key_env: ANTHROPIC_API_KEY
    claude-opus:
      provider: claude
      model: claude-opus-4-20250514
      api_key_env: ANTHROPIC_API_KEY
    gemini-flash:
      provider: gemini
      model: gemini-2.0-flash
      api_key_env: GOOGLE_API_KEY
    ollama-llama:
      provider: ollama
      model: llama3:latest
      endpoint: http://localhost:11434

# Server (for joecored)
server:
  address: ":7777"

# Background Refresh
refresh:
  interval: 5m
  llm_budget:
    max_calls_per_hour: 10
    batch_threshold: 5
    batch_timeout: 15m

# Notifications
notifications:
  desktop:
    enabled: true
    priority_threshold: medium   # low | medium | high | urgent
  slack:
    enabled: false
    webhook_url_env: SLACK_WEBHOOK
    priority_threshold: high
  quiet_hours:
    enabled: true
    start: "22:00"
    end: "08:00"
    timezone: Europe/Madrid

# Logging
logging:
  level: info                   # debug | info | warn | error
  file: ~/.joe/joe.log
```

---

## Action Safety Framework

Joe enforces a hardcoded safety layer that governs what it can change and under what conditions. This is not LLM-instructed — it is compiled into the binary and configured by humans outside Joe's reach.

Full details in `docs/security-in-layers.md`. For a real-world case study demonstrating these controls, see `docs/case-study-kiro-incident.md`.

### Action Tiers

Every tool and API action is classified at registration time. The LLM cannot change a tool's tier.

| Tier | Label | Description | Default |
|------|-------|-------------|---------|
| T1 | Observe | Read-only, cannot change any state | Allowed |
| T2 | Record | Changes Joe's internal state (graph, facts, sources) | Requires opt-in |
| T3 | Act | Changes external systems (files, infra, deployments) | Denied, per-action opt-in |

### Safety Policy File

Policy lives in `~/.joe/safety-policy.yaml` — human-editable only. Joe cannot read, write, or influence this file:

- Excluded from `read_file` and `write_file` allowed directories (hardcoded)
- Loaded once at startup, not re-readable by the agent at runtime
- Controls which T2/T3 actions are permitted

### Hardcoded Self-Protection Invariants

These are constants in the source code. No configuration can override them:

- Joe cannot read or write `~/.joe/safety-policy.yaml`
- Joe cannot write to `~/.joe/` (its own config directory)
- Joe cannot run `joe`, `joecored`, `kill`, `pkill`, or `killall` via `run_command`
- `kubectl`, `helm`, `argocd` are not in the default `run_command` allowlist; when enabled by policy, only read-only subcommands (`get`, `describe`, `logs`) are permitted unless the human explicitly allows mutation subcommands

### Deterministic Blast Radius Controls

These rules are hardcoded and cannot be bypassed by LLM reasoning, user permissions, or policy configuration:

- **Environment-level blocking:** Operations targeting entire namespaces, clusters, or environments (e.g., `kubectl delete namespace`, `--all` selectors, `terraform destroy`) are categorically blocked unless the specific environment is explicitly allow-listed. See §3.6 in `docs/security-in-layers.md`.
- **Mutation circuit breaker:** A rolling-window rate limiter on T3 actions trips after a configurable threshold (default: 5 mutations in 10 minutes), suspending all further mutations until a human explicitly resets. See §3.7 in `docs/security-in-layers.md`.
- **Credential isolation:** Joe uses its own service account with pre-scoped permissions, never the calling user's credentials. Three independent permission boundaries (RBAC, Safety Policy, Service Account IAM) must all allow an operation. See §3.8 in `docs/security-in-layers.md`.

### Notification Contract

Hardcoded in the tool executor, not in LLM instructions:

- **T3 (Act):** Blocking notification before execution ("I'm about to..."), human can cancel. Summary after execution.
- **T2 (Record):** Post-execution notification in session log ("Updated graph: added node...").
- **T1 (Observe):** No notification required.

### Per-Phase Safety Requirements

Every new phase that introduces tools, adapters, or mutation capabilities must:

1. Classify each new action as T1, T2, or T3
2. Add a corresponding policy flag in `safety-policy.yaml` for T2/T3 actions
3. Wire the action through the safety gate before execution
4. Implement the notification contract for T2/T3 actions
5. Add tests verifying that denied actions are rejected and notifications are emitted
6. Document the action's blast radius in `docs/security-in-layers.md`

---

## Implementation Phases

### Phase 1: Foundation (Two Binaries) ✅ COMPLETE
- [x] Two binaries: `cmd/joe/`, `cmd/joecored/`
- [x] HTTP API server in joecored
- [x] HTTP client in joe (connects to joecored)
- [x] Config loading with env var overrides
- [x] LLM Adapter interface + Claude + Gemini implementations

### Phase 2: User Agent Loop ✅ COMPLETE
- [x] Tool interface + executor + registry
- [x] User Agent with agentic loop
- [x] Local tools: `echo`, `ask_user`, `read_file`, `write_file`, `local_git_status`, `local_git_diff`, `run_command`
- [x] REPL with `/model` command (bubbletea TUI)
- [x] Session management

### Phase 3: Core Services + API ✅ COMPLETE
- [x] SQL Store with migrations (8 tables: sources, sessions, session_messages, clarifications, joe_file_cache, onboarding_facts, graph_nodes, graph_edges)
- [x] Graph Store (SQLite-based, not Cayley)
- [x] API handlers: `/api/v1/graph/query`, `/api/v1/graph/related`, `/api/v1/graph/summary`
- [x] API handlers: `/api/v1/sources` CRUD
- [x] Core tools in joe: `graph_query`, `graph_related`, `list_sources`

### Phase 4: Infrastructure Adapters ✅ COMPLETE
- [x] K8s adapter (Connect, ListResources, GetResource, GetPodLogs)
- [x] K8s API endpoints + core tools (`k8s_get`, `k8s_logs`)
- [x] Git adapter (Connect, ReadFile, ListFiles, Log, Diff)
- [x] Git API endpoints + core tools (`git_read`, `git_log`, `git_diff`)

### Phase 5: Core Agent ✅ COMPLETE
- [x] Core Agent background refresh loop
- [x] Core Agent tools (graph manipulation, source registration)
- [x] Agent lifecycle management (start/stop with graceful shutdown)
- [x] Discovery engine for onboarding processing
- [x] Background refresh with configurable intervals
- [x] Integration with joecored daemon
- [x] Comprehensive test coverage
- [x] **Milestone: Two-agent architecture fully operational**

### Phase 5.5: Action Safety Framework ✅ COMPLETE
Implements the safety enforcement layer before any new adapters or mutation capabilities are added. This phase is a prerequisite for Phase 6.

- [x] **Safety policy loader:** Load `~/.joe/safety-policy.yaml` at startup in both joe and joecored
- [x] **Action tier registry:** Classify every existing tool as T1/T2/T3 at registration time
- [x] **Tool executor gate:** Check tier + policy before every `Execute()` call; reject unauthorized actions
- [x] **Self-protection invariants:** Hardcode exclusions — Joe cannot touch `~/.joe/`, cannot run `joe`/`joecored`/`kill` commands
- [x] **Path sandboxing:** `read_file`/`write_file` restricted to `allowed_directories` from policy; `..` and symlink escape rejected
- [x] **run_command hardening:** Split allowlist into read-only vs mutation-capable; add subcommand allowlist for kubectl/helm/argocd (default: read-only subcommands only)
- [x] **T3 notification contract:** Blocking pre-execution notification in REPL, post-execution summary
- [x] **T2 notification contract:** Post-execution log entry for all graph/store mutations
- [x] **API authentication:** Bearer token middleware on all `/api/v1/` routes
- [x] **Request size limits:** `http.MaxBytesReader` middleware (default 1 MB)
- [x] **Tests:** Safety gate rejects denied actions; notifications emitted; self-protection paths blocked; policy loading works
- [x] **Milestone: No tool can mutate anything without passing through the safety gate**

### Phase 6: Infrastructure Adapters ✅ COMPLETE

All new adapters in this phase are read-only (T1) by default. Any mutation capability (silence creation, incident acknowledgment) must be registered as T3 with a corresponding policy flag.

- [x] **6.1 Core foundations:** Source types, adapter registry wiring, graph edge definitions
- [x] **6.2 Cloud:** AWS (EC2, EKS, RDS, VPC), Azure (VMs, AKS, SQL, VNets)
- [x] **6.3 Observability open-source:** Prometheus/Mimir (PromQL), Loki (LogQL), Tempo/Jaeger (traces)
- [x] **6.4 Alerting & Dashboards:** Alertmanager, PagerDuty, Grafana
- [x] **6.5 Safety & Hardening:** Credential encryption (AES-256-GCM), TLS, rate limiting, tool tier classification
- [x] **6.6 Network & system diagnostics:** Go-native shared tools (tcp_connect, dns_lookup, http_request, system_info, trace_route)
- [x] **6.7 Data Stores:** PostgreSQL, MySQL, Redis, MongoDB, Kafka, Elasticsearch (read-only diagnostic queries)
- [x] **6.8 GitOps, CD & IaC:** Argo CD (full adapter), Flux (via K8s CRDs), Terraform (state), Helm (releases)
- [x] **6.9 Networking & Ingress:** NGINX Ingress, Envoy (admin API), Istio (via K8s CRDs), Cilium (CRDs + Hubble)
- [x] **6.10 K8s CRD-Based (low effort):** cert-manager, KEDA, OPA/Gatekeeper, Crossplane
- [x] **6.11 Security & Runtime:** Falco (runtime events)
- [x] **6.12 Proprietary observability:** Datadog, Splunk, Dynatrace, New Relic
- [x] **6.13 Artifact registries:** OCI/DockerHub/GHCR/Harbor/Quay, JFrog Artifactory, AWS ECR
- [x] API endpoints + core tools for each adapter
- [x] Graph edges: all 19 types wired (`metrics_in`, `logs_in`, `traces_in`, `alerts_in`, `paged_via`, `dashboard_in`, `is_k8s_node`, `stores_in`, `queues_in`, `managed_by`, `ingress_for`, `proxies`, `mesh_for`, `policy_enforces`, `scaled_by`, `secures`, `provisions`, `image_stored_in`, `publishes_to`)
- [x] **Milestone: Joe queries cloud, observability, data stores, GitOps, networking, and policy with safety enforcement on all mutation paths**

### Phase 7: Knowledge Store ✅ COMPLETE

- [x] Knowledge items table (tiers: curated, synced, derived) — SQLite-backed, migration 004
- [x] Tier 1: Human-curated notes attached to nodes (immutability enforced at service layer)
- [x] Tier 2: Synced sources (Confluence, Notion adapters with background sync coordinator)
- [x] Tier 3: LLM-derived insights from sessions (provenance metadata, deduplication)
- [x] Knowledge retrieval integrated into User Agent context via `search_knowledge` tool (T1)
- [x] Embeddings for semantic search (cosine similarity, tier/confidence filtering)
- [x] 10 API endpoints: entries CRUD, semantic search, source management, manual sync trigger
- [x] **Milestone: Joe references runbooks and past learnings**

### Phase 8: Documentation Co-Pilot ✅ COMPLETE

- [x] Write adapters for Confluence (PUT), Notion (block replace), Git (commit + push)
- [x] Draft generation via LLM + knowledge store search → unified diff → proposal
- [x] Human approval flow (pending → approved → published / rejected)
- [x] Drift detection for Tier 2 synced entries (SHA-256 hash comparison)
- [x] API endpoints: `/api/v1/knowledge/proposals`, `/api/v1/knowledge/drift`
- [x] Client bindings + 3 new tools: `detect_doc_drift` (T1), `generate_doc_draft` (T2), `publish_doc_update` (T3)
- [x] SQL migration 005, proposals repository + service, tier registry entries
- [x] **Milestone: Joe can propose and publish doc updates with safety enforcement**

### Phase 9: Security Architecture + Additional Clients ← CURRENT

**Security Service (pluggable architecture):**
- [ ] `cmd/joe-security/` binary (optional, for hardened deployments)
- [ ] `internal/security/interface.go` — SecurityPolicy interface
- [ ] `internal/security/embedded.go` — EmbeddedSecurityPolicy (same DB, protected tables)
- [ ] `internal/security/remote.go` — RemoteSecurityPolicy (gRPC client to joe-security)
- [ ] `internal/securitysvc/` — joe-security server implementation
- [ ] Config: `security.mode: embedded | remote`

**Security Zones:**
- [ ] Zone definitions: prod-readonly, prod-write, dev-full, unassigned (default)
- [ ] Source → Zone assignments (admin-controlled, LLM cannot modify)
- [ ] Zone-based permission evaluation in tool executor
- [ ] Notification for unassigned sources

**Protected Tables (hardcoded invariants):**
- [ ] `internal/safety/invariants.go` — writeProtectedTables, appendOnlyTables
- [ ] Tables protected: security_zones, source_zone_assignments, rbac_policies
- [ ] Audit log: append-only (INSERT allowed, UPDATE/DELETE blocked)
- [ ] Enforcement in tool executor before any SQL operation

**RBAC Implementation:**
- [ ] Principal → Zones policy model
- [ ] Authentication adapters (Entra ID, LDAP, OIDC, API keys)
- [ ] Token validation and caching
- [ ] Policy evaluation middleware

**Admin API (separate from LLM-accessible API):**
- [ ] `POST /api/v1/admin/zones` — create/update zones
- [ ] `POST /api/v1/admin/source-zones` — assign sources to zones
- [ ] `POST /api/v1/admin/policies` — manage RBAC policies
- [ ] `GET /api/v1/admin/source-zones/unassigned` — list unassigned sources
- [ ] Requires admin authentication (separate from user auth)

**Emergency Shutdown (Panic Mode):**
- [ ] `/panic` REPL command, `joe panic` CLI, `POST /api/v1/panic`, SIGUSR1
- [ ] Safe mode on restart (T1 only until explicit unlock)
- [ ] `joe unlock --reason "..."` to resume
- [ ] Panic state persistence (~/.joe/panic.state)

**Additional Clients:**
- [ ] Web UI — see `docs/web-ui.md` for full specification
  - React + TypeScript + Vite + Tailwind + shadcn/ui
  - React Flow for infrastructure graph visualization
  - TanStack Query for server state
  - Location: `ui/` directory (monorepo)
  - Pages: Graph, Dashboard, Sources, Admin (zones/policies), Chat
- [ ] MCP Server (Claude Code, Cursor, Codex — replaces VS Code extension)
- [ ] Slack Bot (ChatOps for on-call, optional)
- [ ] In-cluster deployment for joecored

**Database Migrations:**
- [ ] Migration 006: security_zones table
- [ ] Migration 007: source_zone_assignments table
- [ ] Migration 008: rbac_policies table (replaces environment-based model)

**Milestone: Multi-user Joe with zone-based security, pluggable security service, and emergency controls**

### Phase 10: Code Review Integration

- [ ] GitHub adapter (`internal/adapters/github/`)
  - PR read operations (T1): GetPullRequest, GetPullRequestDiff, ListPullRequestComments
  - Review write operations: PostReviewComment (T2), SubmitReview (T3)
  - Webhook parsing and HMAC signature validation
- [ ] GitLab adapter (`internal/adapters/gitlab/`)
  - MR read operations (T1): GetMergeRequest, GetMergeRequestDiff, ListMergeRequestComments
  - Review write operations: PostNote (T2), ApproveMergeRequest (T3)
  - Webhook parsing and token validation
- [ ] Webhook receiver endpoints
  - `POST /api/v1/webhooks/github` — receives PR events
  - `POST /api/v1/webhooks/gitlab` — receives MR events
  - Idempotency via delivery_id/event_id tracking in `webhook_events` table
- [ ] Review job queue (`internal/reviewagent/queue.go`)
  - SQLite-backed queue with single worker (avoids concurrency complexity)
  - Job states: queued → in_progress → completed/failed
  - Deduplication: one active job per (repo, pr_number, commit_sha)
- [ ] Review Agent (`internal/reviewagent/agent.go`)
  - Triggered by webhook or manual `joe review PR#123`
  - Flow: fetch diff → identify affected resources → query graph → query knowledge → LLM analysis → post review
  - Infrastructure-aware reviews (blast radius, dependencies, incident history)
- [ ] Safety policy for reviews:
  ```yaml
  act:
    github_comment:
      enabled: true              # T2: can post comments
    github_approve:
      enabled: false             # T3: cannot auto-approve (default)
    github_request_changes:
      enabled: true              # T3: can request changes
    github_merge:
      enabled: false             # T3: never auto-merge
  ```
- [ ] **Milestone: Joe as infrastructure-aware code reviewer**
