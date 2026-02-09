# Phase 4 Finished: Copilot Analysis and Next Steps

**Date:** February 9, 2026  
**Status:** Core foundation complete, ready for Core Agent implementation

---

## Current State Summary

### ✅ Solid Foundation Built

Joe has a strong architectural foundation with all major components in place:

#### 1. **Two-Binary Architecture**
- `joe` (Joe Local) - User-facing CLI with User Agent
- `joecored` (Joe Core) - Daemon with HTTP API and Core Services
- Clean HTTP boundary enforces separation
- API routes defined and tested

#### 2. **LLM Integration**
- **LLM Adapter Interface** - AI-agnostic design (swappable backends)
- **Claude Adapter** - Anthropic Claude with tool support
- **Gemini Adapter** - Google Gemini with tool support
- **Hot Model Switching** - Change models without restart
- **OpenTelemetry Instrumentation** - Token tracking, latency, costs

#### 3. **User Agent (Interactive Mode)**
- **Agentic Loop** - LLM → tool calls → LLM → response
- **Session Management** - Conversation history, context tracking
- **REPL Interface** - Interactive chat with hot model switching
- **Tool Execution Framework** - Registry, executor, safety controls

#### 4. **Local Tools (Implemented)**
- `read_file` - Read user's local files
- `write_file` - Write to user's local files  
- `local_git_status` - Git working tree status
- `local_git_diff` - Show uncommitted changes
- `run_command` - Execute shell commands locally

#### 5. **Data Layer**
- **SQL Store (SQLite)** - Sources, sessions, cache tables
- **Graph Store (SQLite)** - Nodes, edges, relationships
- **Migration System** - Schema versioning
- **API Integration** - Full CRUD operations

#### 6. **Infrastructure Adapters**
- **Kubernetes Adapter** - client-go integration, dynamic discovery
- **Git Adapter** - go-git, clone/read/log/diff operations
- Adapter Registry with dynamic loading

#### 7. **API Server (joecored)**
- HTTP server with route registration
- Graph endpoints (query, related, summary)
- Sources CRUD endpoints
- K8s endpoints (resources, logs)
- Git endpoints (files, log, diff)
- Status and health checks

#### 8. **Testing Infrastructure**
- **Unit Tests** - Individual component testing
- **Integration Tests** - Mock LLM, filesystem, conversation flows
- **E2E Tests** - Full binary lifecycle testing
- **Test Harness** - Automated build and process management
- **CI/CD Ready** - GitHub Actions workflow

#### 9. **Configuration System**
- YAML-based config (`~/.joe/config.yaml`)
- Environment variable overrides
- Validation and defaults
- Multiple LLM provider support

#### 10. **Observability**
- **OpenTelemetry** - Traces, metrics, logs
- **LLM Middleware** - Automatic instrumentation
- **Prometheus Export** - Metrics endpoint
- **Development Logging** - Structured logging with levels

#### 11. **Documentation**
- Architecture documents
- Data flow diagrams
- Testing strategy
- API contracts
- Go coding standards
- Observability guide

---

## ❌ Critical Gaps

The architecture is designed, but the **Core Agent is stubbed out**. This is the main blocker:

### 1. **Core Agent Not Implemented**

**File:** [internal/coreagent/discovery.go](internal/coreagent/discovery.go#L5)
```go
type Engine struct {
	// TODO: Implement discovery engine in Phase 5
}
```

**File:** [internal/coreagent/refresh.go](internal/coreagent/refresh.go#L5)
```go
type Refresher struct {
	// TODO: Implement background refresh in Phase 5
}
```

**File:** [cmd/joecored/main.go](cmd/joecored/main.go#L153)
```go
// TODO: Start Core Agent background refresh here
```

### 2. **Missing Core Tools**

User Agent can only execute **local tools**. Missing **Core Tools** that call joecored API:

- `graph_query()` - Query infrastructure graph
- `graph_related()` - Get connected nodes
- `graph_summary()` - Get graph context for LLM
- `k8s_get()` - Get K8s resources (via API, not direct)
- `k8s_logs()` - Get pod logs (via API)
- `git_read()` - Read from cloned repos (via API)

These are partially implemented as API routes but not exposed as User Agent tools.

### 3. **Onboarding Flow**

The phased onboarding system (Phase 1-3) is documented but not implemented:

- Phase 1: Collect (structured input gathering)
- Phase 2: Validate (deterministic checks)
- Phase 3: Explore (timeboxed LLM discovery)

### 4. **Clarifications System**

Human-in-the-loop for ambiguous discoveries:

- Clarifications table exists but not used
- No API handlers for answering/dismissing
- No notification integration

### 5. **Notification Service**

**File:** [internal/notify/service.go](internal/notify/service.go#L5)
```go
// TODO: Implement notification service in Phase 6
```

Currently empty stub. Needed for:
- Desktop notifications
- Pending clarification alerts
- Anomaly detection alerts

### 6. **Additional Adapters**

Infrastructure coverage is limited:

- **Missing:** ArgoCD adapter
- **Missing:** Prometheus adapter  
- **Missing:** Loki adapter

---

## Recommended Build Order

### **Phase 5: Core Agent Foundation** ⭐️ **BUILD THIS NEXT**

The architecture is complete but the Core Agent is missing. This is the critical path.

#### 5.1 Core Agent Structure

**Create:** `internal/coreagent/agent.go`

```go
package coreagent

import (
    "context"
    "time"
    "github.com/jaimegago/joe/internal/core"
    "github.com/jaimegago/joe/internal/llm"
    "github.com/jaimegago/joe/internal/tools"
)

// Agent is the Core Agent that maintains infrastructure knowledge
type Agent struct {
    services  *core.Services
    llm       llm.LLMAdapter
    tools     *tools.Registry
    executor  *tools.Executor
    refresher *Refresher
    discovery *Engine
}

// New creates a new Core Agent
func New(services *core.Services, llmAdapter llm.LLMAdapter) *Agent {
    // Create Core Agent tools (graph manipulation)
    toolRegistry := tools.NewRegistry()
    registerCoreTools(toolRegistry, services)
    
    executor := tools.NewExecutor(toolRegistry)
    
    return &Agent{
        services:  services,
        llm:       llmAdapter,
        tools:     toolRegistry,
        executor:  executor,
        refresher: NewRefresher(services, llmAdapter),
        discovery: NewEngine(services, llmAdapter),
    }
}

// Start begins background operations
func (a *Agent) Start(ctx context.Context) error {
    // Start background refresh
    return a.refresher.Start(ctx)
}

// Stop gracefully shuts down the agent
func (a *Agent) Stop(ctx context.Context) error {
    return a.refresher.Stop(ctx)
}
```

#### 5.2 Core Agent Tools

**Create:** `internal/coreagent/tools.go`

Core Agent needs its own tools for graph manipulation:

- `graph_add_node` - Add infrastructure node to graph
- `graph_add_edge` - Create relationship between nodes
- `graph_update_node` - Update node metadata
- `register_source` - Register new infrastructure source
- `save_onboarding_fact` - Store user-provided facts

#### 5.3 Background Refresh Loop

**Implement:** `internal/coreagent/refresh.go`

```go
type Refresher struct {
    services *core.Services
    llm      llm.LLMAdapter
    interval time.Duration
    stopCh   chan struct{}
}

func (r *Refresher) Start(ctx context.Context) error {
    ticker := time.NewTicker(r.interval)
    go func() {
        for {
            select {
            case <-ticker.C:
                r.refresh(ctx)
            case <-r.stopCh:
                ticker.Stop()
                return
            }
        }
    }()
    return nil
}

func (r *Refresher) refresh(ctx context.Context) error {
    // 1. Load connected sources
    sources, err := r.services.Store.Sources.List(ctx)
    
    // 2. For each source, query current state
    // 3. Diff against existing graph
    // 4. Apply deterministic changes
    // 5. Queue ambiguous findings for clarification
    
    return nil
}
```

#### 5.4 Wire Up in joecored

**Update:** [cmd/joecored/main.go](cmd/joecored/main.go#L153)

```go
// Initialize Core Agent
coreAgent := coreagent.New(coreServices, llmAdapter)
if err := coreAgent.Start(ctx); err != nil {
    slog.Error("failed to start core agent", "error", err)
    os.Exit(1)
}
defer coreAgent.Stop(context.Background())

slog.Info("core agent started")
```

**Why Phase 5 First:**
- Unlocks the two-agent architecture vision
- User Agent already works for interactive queries
- Core Agent autonomously maintains graph in background
- Required before onboarding can populate the graph

---

### **Phase 6: Core Tools for User Agent**

User Agent needs tools that query joecored for infrastructure data.

#### 6.1 HTTP Client Tools

**Create:** `internal/tools/core/graphquery.go`

```go
func GraphQuery(client *client.Client) tools.Tool {
    return tools.Tool{
        Name: "graph_query",
        Description: "Search the infrastructure graph for nodes matching a query",
        Parameters: tools.Parameters{
            Type: "object",
            Properties: map[string]tools.Property{
                "query": {
                    Type: "string",
                    Description: "Search query (service name, namespace, type, etc.)",
                },
            },
            Required: []string{"query"},
        },
        Handler: func(ctx context.Context, args map[string]any) (any, error) {
            query := args["query"].(string)
            return client.Graph.Query(ctx, query)
        },
    }
}
```

Similar pattern for:
- `graph_related(nodeID, depth)` - Get connected nodes
- `graph_summary()` - Get graph stats for LLM context

#### 6.2 Remote K8s Tools

**Create:** `internal/tools/core/k8sget.go`

```go
func K8sGet(client *client.Client) tools.Tool {
    return tools.Tool{
        Name: "k8s_get",
        Description: "Get a Kubernetes resource from a cluster",
        Parameters: tools.Parameters{
            Type: "object",
            Properties: map[string]tools.Property{
                "source_id": {Type: "string", Description: "K8s source ID"},
                "resource": {Type: "string", Description: "Resource type (pod, deployment, etc.)"},
                "namespace": {Type: "string", Description: "Namespace"},
                "name": {Type: "string", Description: "Resource name"},
            },
            Required: []string{"source_id", "resource", "namespace", "name"},
        },
        Handler: func(ctx context.Context, args map[string]any) (any, error) {
            // Call joecored API
            return client.K8s.GetResource(ctx, ...)
        },
    }
}
```

Similar for:
- `k8s_list()` - List resources
- `k8s_logs()` - Get pod logs

#### 6.3 Register Core Tools

**Update:** `cmd/joe/main.go`

```go
// Create HTTP client to joecored
coreClient := client.New(cfg.Server.Address)

// Register local tools
registry.Register(local.ReadFile())
registry.Register(local.WriteFile())
// ... other local tools

// Register Core Tools (call joecored API)
registry.Register(coretoolsgraphquery.GraphQuery(coreClient))
registry.Register(coretools.GraphRelated(coreClient))
registry.Register(coretools.K8sGet(coreClient))
registry.Register(coretools.K8sLogs(coreClient))
```

**Why Phase 6 Second:**
- Makes User Agent useful for distributed system debugging
- Leverages existing API endpoints
- Enables cross-source queries (K8s + Git + Graph)

---

### **Phase 7: Onboarding Flow**

How the graph gets populated initially.

#### 7.1 Onboarding Command

**Create:** `cmd/joe/commands/init.go`

```go
func InitCommand(coreClient *client.Client) *cobra.Command {
    return &cobra.Command{
        Use:   "init",
        Short: "Run onboarding to set up Joe",
        Run: func(cmd *cobra.Command, args []string) {
            ctx := cmd.Context()
            
            // Phase 1: Collect structured input
            input := collectInput()
            
            // Phase 2: Validate connections
            validated := validateSources(ctx, input)
            
            // Phase 3: Trigger Core Agent discovery
            coreClient.Onboarding.Start(ctx, validated)
        },
    }
}
```

#### 7.2 Discovery Engine

**Implement:** `internal/coreagent/discovery.go`

```go
type Engine struct {
    services *core.Services
    llm      llm.LLMAdapter
}

func (e *Engine) RunOnboarding(ctx context.Context, input OnboardingInput) error {
    // 1. Store raw input
    e.services.Store.OnboardingInput.Save(ctx, input)
    
    // 2. For each source, run deterministic discovery
    for _, src := range input.Sources {
        e.discoverSource(ctx, src)
    }
    
    // 3. LLM-based exploration (timeboxed)
    e.exploreWithLLM(ctx, time.Minute * 2)
    
    return nil
}

func (e *Engine) ProcessJoeFiles(ctx context.Context, repoPath string) error {
    // 1. Hash .joe/ directory
    hash := hashDir(filepath.Join(repoPath, ".joe"))
    
    // 2. Check cache
    cached, err := e.services.Store.Cache.GetJoeFile(ctx, repoPath, hash)
    if err == nil && cached != nil {
        // Replay cached tool calls
        return e.replayToolCalls(ctx, cached.ToolCalls)
    }
    
    // 3. LLM interprets .joe/ files
    files := readJoeFiles(repoPath)
    response := e.interpretJoeFiles(ctx, files)
    
    // 4. Execute and cache
    e.executor.ExecuteBatch(ctx, response.ToolCalls)
    e.services.Store.Cache.SetJoeFile(ctx, repoPath, hash, response.ToolCalls)
    
    return nil
}
```

#### 7.3 API Handlers

**Create:** `internal/api/onboarding.go`

```go
func (s *Server) handleOnboarding(w http.ResponseWriter, r *http.Request) {
    var input OnboardingInput
    if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
        return
    }
    
    // Trigger Core Agent onboarding
    if err := s.coreAgent.RunOnboarding(r.Context(), input); err != nil {
        writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
        return
    }
    
    writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
}
```

**Why Phase 7 Third:**
- Requires Core Agent to be running
- Populates the graph with initial data
- Enables the "joe init" first-run experience

---

### **Phase 8: Clarifications System**

Human-in-the-loop for ambiguous discoveries.

#### 8.1 API Handlers

**Create:** `internal/api/clarifications.go`

```go
func (s *Server) handleListClarifications(w http.ResponseWriter, r *http.Request) {
    clarifications, err := s.services.Store.Clarifications.List(r.Context())
    if err != nil {
        writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
        return
    }
    writeJSON(w, http.StatusOK, clarifications)
}

func (s *Server) handleAnswerClarification(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    var answer struct {
        Answer string `json:"answer"`
    }
    json.NewDecoder(r.Body).Decode(&answer)
    
    // Store answer and apply graph operations
    if err := s.coreAgent.AnswerClarification(r.Context(), id, answer.Answer); err != nil {
        writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
        return
    }
    
    writeJSON(w, http.StatusOK, map[string]string{"status": "answered"})
}
```

#### 8.2 Notification Service

**Implement:** `internal/notify/service.go`

```go
type Service struct {
    channels []Channel
}

type Channel interface {
    Send(ctx context.Context, notification Notification) error
}

// Implementations
type DesktopChannel struct{} // notify-send / osascript
type SlackChannel struct{}   // webhook
type WebChannel struct{}     // websocket (future)
```

#### 8.3 Integration

**Update:** `internal/coreagent/refresh.go`

When Core Agent finds something ambiguous:

```go
func (r *Refresher) handleAmbiguousDiscovery(ctx context.Context, finding Finding) {
    // Create clarification record
    clarification := &Clarification{
        Type:     ClarificationTypeNewService,
        Question: fmt.Sprintf("What is deployment '%s'?", finding.Name),
        Context:  finding,
    }
    
    r.services.Store.Clarifications.Create(ctx, clarification)
    
    // Send notification
    r.notifier.Send(ctx, Notification{
        Type:     NotificationGraphClarification,
        Priority: PriorityMedium,
        Title:    "Joe needs input",
        Body:     clarification.Question,
    })
}
```

**Why Phase 8 Fourth:**
- Refines graph over time through conversation
- Handles edge cases Core Agent can't resolve
- Enables gradual learning from user feedback

---

### **Phase 9: Additional Adapters**

Expand infrastructure coverage.

#### 9.1 ArgoCD Adapter

**Create:** `internal/adapters/argocd/argocd.go`

```go
type Adapter struct {
    url    string
    token  string
    client *http.Client
}

func (a *Adapter) ListApps(ctx context.Context) ([]Application, error) {
    // Call ArgoCD API
}

func (a *Adapter) GetApp(ctx context.Context, name string) (*Application, error) {
    // Call ArgoCD API
}

func (a *Adapter) GetAppDiff(ctx context.Context, name string) (*Diff, error) {
    // Call ArgoCD API
}
```

#### 9.2 Prometheus Adapter

**Create:** `internal/adapters/prometheus/prometheus.go`

```go
type Adapter struct {
    url    string
    client api.Client
}

func (a *Adapter) Query(ctx context.Context, promql string) (model.Value, error) {
    // Query instant vector
}

func (a *Adapter) QueryRange(ctx context.Context, promql string, start, end time.Time) (model.Value, error) {
    // Query range vector
}
```

#### 9.3 Loki Adapter (Optional)

**Create:** `internal/adapters/loki/loki.go`

Similar to Prometheus but for LogQL queries.

**Why Phase 9 Fifth:**
- Adds more data sources for User Agent
- Enables richer debugging capabilities
- ArgoCD crucial for GitOps workflows

---

## Immediate Next Steps

### Step 1: Implement Core Agent Foundation

**Tasks:**
1. Create `internal/coreagent/agent.go` with Agent struct
2. Create `internal/coreagent/tools.go` with Core Agent tools
3. Implement basic refresh loop in `internal/coreagent/refresh.go`
4. Wire up in `cmd/joecored/main.go`
5. Write tests

**Success Criteria:**
- `joecored` starts without errors
- Background refresh runs on timer
- Logs show periodic graph updates
- Graceful shutdown works

### Step 2: Add Core Tools to User Agent

**Tasks:**
1. Create `internal/tools/core/` directory
2. Implement `graphquery.go`, `graphrelated.go`, `k8sget.go`, `k8slogs.go`
3. Register Core Tools in `cmd/joe/main.go`
4. Test with User Agent queries

**Success Criteria:**
- User can ask "show me pods in namespace X"
- User Agent calls graph_query tool
- Tool makes HTTP call to joecored
- Results returned to user

### Step 3: Basic Onboarding

**Tasks:**
1. Create `joe init` command
2. Implement Phase 1 (collect input)
3. Implement Phase 2 (validate)
4. Basic discovery in Core Agent
5. Test end-to-end

**Success Criteria:**
- `joe init` collects K8s, Git, etc. sources
- Sources stored in database
- Core Agent discovers initial resources
- Graph populated with nodes and edges

---

## Architecture Strengths

✅ **Two-binary design enforces separation** - No shortcuts, clean API boundary  
✅ **LLM-agnostic from day one** - Swappable backends, instrumented  
✅ **Tool-based architecture** - Extensible, testable  
✅ **Graph + SQL hybrid** - Right tool for each data type  
✅ **Testing infrastructure** - Mock LLM, E2E harness  
✅ **Observability built-in** - OpenTelemetry, structured logging  

---

## Risk Areas

⚠️ **Core Agent complexity** - Background jobs, LLM reasoning, state management  
⚠️ **Graph consistency** - Concurrent updates, refresh races  
⚠️ **LLM costs** - Unbounded discovery could be expensive  
⚠️ **Error handling** - Partial failures in multi-source refresh  
⚠️ **Cache invalidation** - When to re-interpret .joe/ files  

---

## Summary

Joe has a **solid foundation** with excellent separation of concerns, testing, and observability. The next critical milestone is **implementing the Core Agent** to unlock the two-agent architecture vision.

**Priority Order:**
1. ⭐️ **Core Agent** (Phase 5) - Unlocks everything else
2. **Core Tools** (Phase 6) - Makes User Agent useful for distributed debugging
3. **Onboarding** (Phase 7) - Populates the graph initially
4. **Clarifications** (Phase 8) - Refines graph over time
5. **More Adapters** (Phase 9) - Expands coverage

The architecture is **production-ready** once Core Agent is implemented. Everything else builds on that foundation.

---

## Files to Create/Update

### Create
- `internal/coreagent/agent.go`
- `internal/coreagent/tools.go`
- `internal/tools/core/graphquery.go`
- `internal/tools/core/graphrelated.go`
- `internal/tools/core/k8sget.go`
- `internal/tools/core/k8slogs.go`
- `cmd/joe/commands/init.go`
- `internal/api/onboarding.go`
- `internal/api/clarifications.go`

### Update
- `internal/coreagent/refresh.go` (implement)
- `internal/coreagent/discovery.go` (implement)
- `internal/notify/service.go` (implement)
- `cmd/joecored/main.go` (start Core Agent)
- `cmd/joe/main.go` (register Core Tools)
- `internal/api/server.go` (add onboarding routes)

---

**Next Build:** Core Agent Foundation (Phase 5)
