# Joe Architecture

Reference architecture for implementation. This document is the source of truth for component structure and data flow.

---

## Design Principles

1. **Two binaries from day one** - `joe` (Local) and `joecored` (Core daemon) in a monorepo
2. **Two agents, clear boundaries** - Core Agent maintains graph, User Agent assists users
3. **HTTP API is the contract** - Joe Local calls Joe Core via HTTP, never direct function calls
4. **Local context stays local** - User's files accessed by Joe Local only, never by Joe Core
5. **Core Agent has autonomy levels** - Deterministic changes auto-apply, ambiguous ones queue for human

---

## Two-Agent Architecture

Joe has two distinct agents with different jobs:

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                                                                          │
│  CORE AGENT (maintains infrastructure knowledge)                                        │
│  ───────────────────────────────────────────────                                        │
│                                                                                          │
│  Runs:        Server-side (or background goroutine in MVP)                              │
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
└─────────────────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                                                                          │
│  USER AGENT (assists users with questions and tasks)                                    │
│  ───────────────────────────────────────────────────                                    │
│                                                                                          │
│  Runs:        Client-side (CLI, IDE extension, Web UI)                                  │
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

# Graph queries (User Agent tools)
GET  /api/v1/graph/query?q=...              Query graph
GET  /api/v1/graph/related/:nodeID          Get related nodes
GET  /api/v1/graph/summary                  Graph summary for LLM context

# Infrastructure queries (User Agent tools)  
GET  /api/v1/k8s/:cluster/:resource/:ns/:name    Get K8s resource
GET  /api/v1/k8s/:cluster/logs/:ns/:pod          Get pod logs
GET  /api/v1/argocd/:instance/apps/:name         Get ArgoCD app
POST /api/v1/prom/query                          Query Prometheus
POST /api/v1/git/:repo/read                      Read file from cloned repo

# Sources
GET  /api/v1/sources                        List sources
POST /api/v1/sources                        Register source

# Clarifications (for human-in-the-loop)
GET  /api/v1/clarifications                 List pending clarifications
POST /api/v1/clarifications/:id/answer      Answer a clarification
POST /api/v1/clarifications/:id/dismiss     Dismiss a clarification

# Control
POST /api/v1/onboarding                     Start onboarding flow
POST /api/v1/refresh                        Trigger manual refresh
GET  /api/v1/status                         Core status (health, graph stats)
```

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
│  Runs: Background goroutine (MVP) or in joedaemon (future)          │
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
│  func (a *CoreAgent) StartBackgroundRefresh(ctx)                    │
│  func (a *CoreAgent) ProcessJoeFiles(ctx, repo)                     │
│                                                                      │
│  // Triggered jobs                                                   │
│  func (a *CoreAgent) RunOnboarding(ctx, input) error                │
│  func (a *CoreAgent) TriggerRefresh(ctx) error                      │
│                                                                      │
│  Tools available (for LLM reasoning):                               │
│  • graph_add_node, graph_add_edge, graph_update                     │
│  • register_source                                                  │
│  • save_onboarding_fact                                             │
│  • k8s_*, git_*, argocd_* (for discovery)                          │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### User Agent (assists users)

```
┌─────────────────────────────────────────────────────────────────────┐
│  User Agent                                                          │
│  ──────────                                                          │
│                                                                      │
│  Runs: In client (CLI, IDE, browser)                                │
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
│  MVP: Hosts both User Agent and Core Agent (+ Core Services)        │
│  Future: Hosts only User Agent, calls joedaemon for Core Services  │
│                                                                      │
│  Location: cmd/joe/                                                 │
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
│  MVP Startup:                                                       │
│    1. Load config from ~/.joe/config.yaml                           │
│    2. Initialize Core Services (LLM, Graph, SQL, Adapters)          │
│    3. Start Core Agent (background goroutine)                       │
│    4. Create User Agent (with Core Services access)                 │
│    5. Enter REPL loop                                               │
│    6. On exit: graceful shutdown                                    │
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
│    internal/llm/gemini/     Google Gemini API                       │
│    internal/llm/openai/     OpenAI GPT-4 (future)                   │
│    internal/llm/ollama/     Local Ollama models (future)            │
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
│  │  Kubernetes (internal/adapters/k8s/)                        │   │
│  │  - Uses client-go                                           │   │
│  │  - Multiple contexts support                                │   │
│  │  - Dynamic resource discovery                               │   │
│  │  - CRD support                                              │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  ArgoCD (internal/adapters/argocd/)                         │   │
│  │  - REST API client                                          │   │
│  │  - Token authentication                                     │   │
│  │  - App listing, sync, diff                                  │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Git (internal/adapters/git/)                               │   │
│  │  - Uses go-git                                              │   │
│  │  - Clone, pull, read                                        │   │
│  │  - SSH and HTTPS auth                                       │   │
│  │  - Local repo cache (~/.joe/repos/)                         │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Prometheus (internal/adapters/prometheus/)                 │   │
│  │  - HTTP API client                                          │   │
│  │  - Query, range query                                       │   │
│  │  - Target discovery                                         │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  Loki (internal/adapters/loki/)                             │   │
│  │  - HTTP API client                                          │   │
│  │  - LogQL queries                                            │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  HTTP (internal/adapters/http/)                             │   │
│  │  - Generic HTTP client                                      │   │
│  │  - For status pages, external APIs                          │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Data Layer

### Graph Store (SQLite)

```
┌─────────────────────────────────────────────────────────────────────┐
│  Graph Store                                                         │
│  ───────────                                                         │
│  Stores infrastructure topology.                                    │
│                                                                      │
│  Location: internal/graph/                                          │
│  Backend: SQLite (same joe.db, tables: graph_nodes, graph_edges)    │
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
│    Confidence ConfidenceLevel  // Inferred (1), Explicit (3), etc.   │
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

---

## Directory Structure

```
joe/
├── cmd/
│   ├── joe/                      # Joe Local (User Agent CLI)
│   │   └── main.go               # Connects to joecored, runs REPL
│   │
│   └── joecored/                 # Joe Core (daemon)
│       └── main.go               # Starts API server, Core Agent
│
├── internal/
│   ├── api/                      # HTTP API (for joecored)
│   │   ├── server.go             # HTTP server setup
│   │   ├── handlers.go           # Route handlers
│   │   └── middleware.go         # Logging, auth
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
│   │   ├── instrumented.go       # Observability wrapper
│   │   ├── claude/
│   │   │   └── claude.go
│   │   └── gemini/
│   │       └── gemini.go
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
│   │       └── promquery.go
│   │
│   ├── graph/                    # Graph store (used by joecored)
│   │   ├── store.go              # Interface + types
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
│   │   ├── k8s/
│   │   ├── argocd/
│   │   ├── git/
│   │   ├── prometheus/
│   │   ├── loki/
│   │   └── http/
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
├── docs/
│   ├── architecture.md         # This file
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
├── joe.db                      # SQLite database (SQL store + graph store)
└── repos/                      # Cloned git repos
    └── <host>/<owner>/<repo>/
```

**config.yaml:**

```yaml
# LLM Configuration
llm:
  provider: claude              # claude | openai | ollama
  model: claude-sonnet-4-20250514
  # API key via env: ANTHROPIC_API_KEY

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

## Implementation Phases

### Phase 1: Foundation (Two Binaries) -- COMPLETE
- [x] Restructure for two binaries: `cmd/joe/`, `cmd/joecored/`
- [x] HTTP API skeleton in joecored (server setup, health endpoint)
- [x] HTTP client skeleton in joe (connects to joecored)
- [x] Config loading (shared config package)
- [x] LLM Adapter interface + Claude + Gemini implementations
- [x] **Milestone: `joecored` starts and serves /api/v1/status, `joe` connects**

### Phase 2: User Agent Loop -- COMPLETE
- [x] Tool interface + executor + registry
- [x] User Agent with agentic loop (in joe)
- [x] Basic local tools: `echo`, `ask_user`
- [x] Additional local tools: `read_file`, `write_file`, `local_git_status`, `local_git_diff`, `run_command`
- [x] REPL with `/model` command for hot-swapping LLMs
- [x] **Milestone: `joe` runs, connects to joecored, echo tool works**

### Phase 3: Core Services + API -- COMPLETE
- [x] SQL Store with migrations (in joecored)
- [x] Graph Store with SQLite (in joecored) -- uses same joe.db, recursive CTEs for traversal
- [x] Core Services wired up with Graph + SQL stores
- [x] API handlers: `/api/v1/graph/query`, `/api/v1/graph/related/{nodeID}`, `/api/v1/graph/summary`
- [x] HTTP client methods: `GraphQuery`, `GraphRelated`, `GraphSummary`
- [x] Core tools in joe calling API: `graph_query`, `graph_related`
- [x] `NewDefaultRegistryWithClient` wires local + core tools together
- [x] **Milestone: User Agent queries graph via HTTP**

### Phase 4: Infrastructure
- [ ] K8s adapter (joecored) + API endpoints + tools (joe)
- [ ] Git adapter (joecored) + API endpoints + tools (joe)
- [ ] **Milestone: "why is pod X failing?" works end-to-end**

### Phase 5: Core Agent
- [ ] Core Agent struct (in joecored)
- [ ] Clarifications API endpoints (table already exists)
- [ ] Onboarding flow via API
- [ ] .joe/ file processing with cache
- [ ] Background refresh goroutine
- [ ] **Milestone: Graph auto-updates, clarifications work**

### Phase 6: Extensions
- [ ] ArgoCD adapter + API + tools
- [ ] Prometheus adapter + API + tools
- [ ] Notifications (desktop, Slack)
- [ ] Session memory (embeddings, search)
- [ ] Additional LLM adapters (OpenAI, Ollama)

### Phase 7: Additional Clients
- [ ] Web UI
- [ ] VS Code extension
- [ ] In-cluster deployment for joecored
