# Joe Architecture

Reference architecture for implementation. This document is the source of truth for component structure and data flow.

---

## Design Principles

1. **Joe Core is independent of how it's run** - Same code whether CLI or daemon
2. **Start simple, evolve when needed** - MVP is single binary, daemon comes later
3. **HTTP + SSE when we need networking** - No gRPC, keep it debuggable

---

## MVP: Single Binary

The MVP is a single `joe` binary that stays running in interactive mode. No separate daemon process, no IPC protocol. Background refresh runs as goroutines within the same process.

```
$ joe
Joe is ready. Background refresh active.

> why is payment slow?
[streams response]

> exit
Goodbye.
```

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                                                                          │
│  $ joe                                                                                   │
│       │                                                                                  │
│       ▼                                                                                  │
│  ┌───────────────────────────────────────────────────────────────────────────────────┐  │
│  │  Joe Process (single binary, stays running)                                       │  │
│  │                                                                                   │  │
│  │  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐                   │  │
│  │  │      REPL       │  │  Agentic Loop   │  │   Background    │                   │  │
│  │  │  (stdin/stdout) │  │                 │  │    Refresh      │                   │  │
│  │  │                 │  │  prompt → LLM   │  │   (goroutine)   │                   │  │
│  │  │  read input     │  │      ↓          │  │                 │                   │  │
│  │  │  show response  │  │  tool calls     │  │  every 5min:    │                   │  │
│  │  │                 │  │      ↓          │  │  refresh graph  │                   │  │
│  │  │                 │  │  execute        │  │                 │                   │  │
│  │  │                 │  │      ↓          │  │                 │                   │  │
│  │  │                 │  │  loop until done│  │                 │                   │  │
│  │  └────────┬────────┘  └────────┬────────┘  └────────┬────────┘                   │  │
│  │           │                    │                    │                            │  │
│  │           └────────────────────┼────────────────────┘                            │  │
│  │                                │                                                 │  │
│  │                                ▼                                                 │  │
│  │                       ┌────────────────┐                                         │  │
│  │                       │                │                                         │  │
│  │                       │   JOE CORE     │  ◄── Same code used in daemon later    │  │
│  │                       │                │                                         │  │
│  │                       └────────┬───────┘                                         │  │
│  │                                │                                                 │  │
│  │           ┌────────────────────┼────────────────────┐                           │  │
│  │           │                    │                    │                           │  │
│  │           ▼                    ▼                    ▼                           │  │
│  │    ┌─────────────┐     ┌─────────────┐     ┌─────────────┐                      │  │
│  │    │ LLM Adapter │     │ Tool        │     │ Discovery   │                      │  │
│  │    │             │     │ Executor    │     │ Engine      │                      │  │
│  │    │ Claude      │     │             │     │             │                      │  │
│  │    │ OpenAI      │     │ graph_*     │     │ onboarding  │                      │  │
│  │    │ Ollama      │     │ k8s_*       │     │ .joe/ files │                      │  │
│  │    │             │     │ git_*       │     │             │                      │  │
│  │    └─────────────┘     │ argocd_*    │     └─────────────┘                      │  │
│  │                        │ prom_*      │                                          │  │
│  │                        │ ...         │                                          │  │
│  │                        └──────┬──────┘                                          │  │
│  │                               │                                                 │  │
│  │                               ▼                                                 │  │
│  │                       ┌─────────────┐                                           │  │
│  │                       │  Adapters   │                                           │  │
│  │                       │             │                                           │  │
│  │                       │ K8s, ArgoCD │                                           │  │
│  │                       │ Git, Prom   │                                           │  │
│  │                       │ Loki, HTTP  │                                           │  │
│  │                       └──────┬──────┘                                           │  │
│  │                              │                                                  │  │
│  └──────────────────────────────┼──────────────────────────────────────────────────┘  │
│                                 │                                                     │
│                                 ▼                                                     │
│  ┌───────────────────────────────────────────────────────────────────────────────────┐│
│  │                            DATA LAYER                                             ││
│  │                                                                                   ││
│  │     ┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐          ││
│  │     │   Graph Store   │     │    SQL Store    │     │   File Store    │          ││
│  │     │    (Cayley)     │     │    (SQLite)     │     │ (~/.joe/repos/) │          ││
│  │     │                 │     │                 │     │                 │          ││
│  │     │  ~/.joe/graph.db│     │  ~/.joe/joe.db  │     │  cloned repos   │          ││
│  │     └─────────────────┘     └─────────────────┘     └─────────────────┘          ││
│  │                                                                                   ││
│  └───────────────────────────────────────────────────────────────────────────────────┘│
│                                                                                          │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

**MVP gives us:**
- Warm connections (no cold start per query)
- Background refresh (goroutines)
- State persistence (during session)
- Simple architecture (no IPC)

**MVP trade-offs:**
- No notifications when Joe isn't running
- No Web UI (yet)
- Process must stay open for background work

---

## Future: Daemon + Clients

When we need Web UI, push notifications, or in-cluster deployment, we split into daemon + thin clients. **Joe Core stays the same** - we just wrap it in an HTTP server.

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                                                                          │
│                                    CLIENTS                                               │
│                                                                                          │
│    ┌──────────────┐      ┌──────────────┐      ┌──────────────┐                         │
│    │     CLI      │      │    Web UI    │      │  Slack Bot   │                         │
│    │    (joe)     │      │              │      │              │                         │
│    │              │      │              │      │              │                         │
│    │  thin client │      │   browser    │      │   webhook    │                         │
│    └──────┬───────┘      └──────┬───────┘      └──────┬───────┘                         │
│           │                     │                     │                                  │
│           └─────────────────────┼─────────────────────┘                                  │
│                                 │                                                        │
│                                 │  HTTP + SSE                                            │
│                                 │                                                        │
│                                 ▼                                                        │
└─────────────────────────────────┼────────────────────────────────────────────────────────┘
                                  │
┌─────────────────────────────────┼────────────────────────────────────────────────────────┐
│                                 │                                                        │
│  joedaemon                      ▼                                                        │
│                        ┌────────────────┐                                                │
│                        │  HTTP Server   │                                                │
│                        │                │                                                │
│                        │  POST /chat    │  → SSE stream                                  │
│                        │  GET  /status  │  → JSON                                        │
│                        │  POST /init    │  → JSON                                        │
│                        │  GET  /sources │  → JSON                                        │
│                        │  ...           │                                                │
│                        └───────┬────────┘                                                │
│                                │                                                         │
│                                ▼                                                         │
│                       ┌────────────────┐                                                 │
│                       │                │                                                 │
│                       │   JOE CORE     │  ◄── Identical to MVP                          │
│                       │                │                                                 │
│                       └────────────────┘                                                 │
│                                                                                          │
└──────────────────────────────────────────────────────────────────────────────────────────┘
```

**HTTP API (when needed):**

| Endpoint | Method | Response | Purpose |
|----------|--------|----------|---------|
| `/chat` | POST | SSE stream | Streaming conversation |
| `/status` | GET | JSON | Daemon & graph status |
| `/init` | POST | JSON | Run onboarding |
| `/sources` | GET | JSON | List sources |
| `/sources` | POST | JSON | Add source |
| `/refresh` | POST | JSON | Trigger refresh |
| `/notifications` | GET | JSON | Pending notifications |
| `/notifications/:id/ack` | POST | JSON | Acknowledge |

---

## Joe Core

The core is the same regardless of how Joe is run. This is what we build first.

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                                                                          │
│                                    JOE CORE                                              │
│                                                                                          │
│  ┌───────────────────────────────────────────────────────────────────────────────────┐  │
│  │                                                                                   │  │
│  │  type Joe struct {                                                                │  │
│  │      config     *Config                                                           │  │
│  │      llm        LLMAdapter                                                        │  │
│  │      graph      GraphStore                                                        │  │
│  │      store      *SQLStore                                                         │  │
│  │      executor   *ToolExecutor                                                     │  │
│  │      discovery  *DiscoveryEngine                                                  │  │
│  │      refresher  *BackgroundRefresher                                              │  │
│  │  }                                                                                │  │
│  │                                                                                   │  │
│  │  // Core methods - used by REPL or HTTP handlers                                 │  │
│  │  func (j *Joe) Chat(ctx, sessionID, message) (<-chan Chunk, error)               │  │
│  │  func (j *Joe) Init(ctx) error                                                   │  │
│  │  func (j *Joe) Status() Status                                                   │  │
│  │  func (j *Joe) Sources() []Source                                                │  │
│  │  func (j *Joe) Refresh(ctx) error                                                │  │
│  │                                                                                   │  │
│  │  // Lifecycle                                                                     │  │
│  │  func (j *Joe) Start(ctx) error    // Start background refresh                   │  │
│  │  func (j *Joe) Stop() error        // Graceful shutdown                          │  │
│  │                                                                                   │  │
│  └───────────────────────────────────────────────────────────────────────────────────┘  │
│                                                                                          │
│                                         │                                                │
│         ┌───────────────────────────────┼───────────────────────────────┐               │
│         │                               │                               │               │
│         ▼                               ▼                               ▼               │
│  ┌─────────────┐                ┌─────────────┐                ┌─────────────┐          │
│  │             │                │             │                │             │          │
│  │ LLM Adapter │                │    Tool     │                │  Discovery  │          │
│  │             │                │  Executor   │                │   Engine    │          │
│  │             │                │             │                │             │          │
│  └─────────────┘                └─────────────┘                └─────────────┘          │
│                                                                                          │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## Component Details

### 1. CLI / REPL

```
┌─────────────────────────────────────────────────────────────────────┐
│  CLI (joe)                                                          │
│  ─────────                                                          │
│  MVP: Single binary that runs Joe Core directly.                    │
│  Future: Thin client connecting to joedaemon via HTTP.              │
│                                                                      │
│  Location: cmd/joe/                                                 │
│                                                                      │
│  Commands:                                                          │
│    joe                     # Interactive mode (stays running)       │
│    joe init                # Run onboarding                         │
│    joe ask "question"      # One-shot query (still starts Joe Core) │
│    joe refresh             # Force discovery refresh                │
│    joe sources             # List known sources                     │
│    joe graph               # Show graph stats                       │
│    joe cache clear         # Clear .joe/ interpretation cache       │
│                                                                      │
│  MVP Startup (joe command):                                         │
│    1. Load config from ~/.joe/config.yaml                           │
│    2. Create Joe Core instance                                      │
│    3. Start background refresh (goroutine)                          │
│    4. Enter REPL loop                                               │
│    5. On exit: graceful shutdown                                    │
│                                                                      │
│  REPL Loop:                                                         │
│    while true:                                                      │
│        input := readline()                                          │
│        if input == "exit": break                                    │
│        response := joe.Chat(ctx, sessionID, input)                  │
│        stream response to stdout                                    │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### 2. Session Manager

```
┌─────────────────────────────────────────────────────────────────────┐
│  Session Manager                                                     │
│  ───────────────                                                     │
│  Manages conversation sessions.                                     │
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
│    StartedAt   time.Time                                            │
│    Messages    []Message       // conversation history              │
│    Context     map[string]any  // working memory                    │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### 3. Agentic Loop

```
┌─────────────────────────────────────────────────────────────────────┐
│  Agentic Loop                                                        │
│  ────────────                                                        │
│  Core reasoning engine. LLM + tool execution loop.                  │
│                                                                      │
│  Location: internal/agent/                                          │
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

### Graph Store (Cayley)

```
┌─────────────────────────────────────────────────────────────────────┐
│  Graph Store                                                         │
│  ───────────                                                         │
│  Stores infrastructure topology.                                    │
│                                                                      │
│  Location: internal/graph/                                          │
│  File: ~/.joe/graph.db (BoltDB backend)                             │
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
│   └── joe/                    # CLI + REPL (single binary for MVP)
│       └── main.go
│
├── internal/
│   ├── joe/                    # Joe Core - the heart of the system
│   │   └── joe.go              # Joe struct, Chat(), Init(), Start(), Stop()
│   │
│   ├── agent/                  # Agentic loop
│   │   ├── agent.go            # Agent struct
│   │   ├── prompt.go           # Prompt building
│   │   └── loop.go             # Tool execution loop
│   │
│   ├── session/                # Session management
│   │   └── session.go
│   │
│   ├── llm/                    # LLM adapters
│   │   ├── adapter.go          # Interface
│   │   ├── claude/
│   │   │   └── claude.go
│   │   ├── openai/
│   │   │   └── openai.go
│   │   └── ollama/
│   │       └── ollama.go
│   │
│   ├── tools/                  # Tool implementations
│   │   ├── executor.go
│   │   ├── registry.go
│   │   ├── graph/
│   │   ├── sources/
│   │   ├── memory/
│   │   ├── k8s/
│   │   ├── git/
│   │   ├── argocd/
│   │   ├── telemetry/
│   │   ├── http/
│   │   └── user/
│   │
│   ├── discovery/              # Onboarding & .joe/ handling
│   │   ├── discovery.go
│   │   ├── onboarding.go
│   │   └── joefile.go
│   │
│   ├── refresh/                # Background refresh
│   │   └── refresh.go
│   │
│   ├── notify/                 # Notifications
│   │   ├── service.go
│   │   ├── desktop.go
│   │   └── slack.go
│   │
│   ├── graph/                  # Graph store
│   │   ├── store.go            # Interface
│   │   └── cayley.go           # Implementation
│   │
│   ├── store/                  # SQL store
│   │   ├── store.go
│   │   ├── sources.go
│   │   ├── sessions.go
│   │   ├── cache.go
│   │   └── migrations/
│   │
│   ├── adapters/               # Infrastructure adapters
│   │   ├── k8s/
│   │   ├── argocd/
│   │   ├── git/
│   │   ├── prometheus/
│   │   ├── loki/
│   │   └── http/
│   │
│   ├── repl/                   # REPL / interactive mode
│   │   └── repl.go
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
├── joe.db                      # SQLite database
├── graph.db                    # Cayley graph
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

### Phase 1: Foundation
- [ ] Project setup (go.mod, directory structure)
- [ ] Config loading
- [ ] SQL Store with migrations
- [ ] LLM Adapter interface + Claude implementation
- [ ] Joe Core struct

### Phase 2: Core Loop
- [ ] Agentic Loop (prompt building, tool execution)
- [ ] Tool Executor with registry
- [ ] Basic tools: graph_query, ask_user
- [ ] REPL / interactive mode
- [ ] `joe` command works end-to-end

### Phase 3: Discovery
- [ ] Onboarding flow (3 phases)
- [ ] Git Adapter
- [ ] .joe/ file processing with hash cache
- [ ] Source registration

### Phase 4: Infrastructure
- [ ] K8s Adapter
- [ ] Graph Store (Cayley)
- [ ] Full graph tools
- [ ] K8s tools

### Phase 5: Background & Notifications
- [ ] Background refresh (goroutine in same process)
- [ ] Notification service
- [ ] Desktop notifications

### Phase 6: Extensions
- [ ] ArgoCD Adapter
- [ ] Prometheus Adapter
- [ ] Memory/session management (embeddings, search)
- [ ] Additional LLM adapters (OpenAI, Ollama)

### Phase 7: Daemon + Clients (Future)
- [ ] HTTP + SSE server (`joedaemon`)
- [ ] Thin CLI client
- [ ] Web UI
- [ ] In-cluster deployment
