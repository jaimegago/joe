# CLAUDE.md - Joe Project Context

This file provides context for Claude Code when working on the Joe codebase.

## What is Joe?

Joe (Joe Operates Everything) is an AI-powered infrastructure copilot. It helps platform engineers understand, debug, and operate their infrastructure through natural conversation.

**Key characteristics:**
- AI-agnostic (Claude, OpenAI, Ollama)
- Two binaries: `joe` (Local) and `joecored` (Core daemon)
- Two agents: User Agent (in joe) + Core Agent (in joecored)
- HTTP API contract between joe and joecored
- Builds a graph of infrastructure relationships

## Two-Binary Architecture

Joe is built as two binaries from day one:

```
┌─────────────────────────────────────────────────────────────────────────┐
│                                                                         │
│  joe (Joe Local)                    joecored (Joe Core)                │
│  ────────────────                   ──────────────────                 │
│                                                                         │
│  User Agent                         HTTP API (:7777)                   │
│  • REPL                             • /api/v1/graph/...                │
│  • Agentic loop → LLM               • /api/v1/k8s/...                  │
│  • Local tools (direct)             • /api/v1/clarifications           │
│  • Core tools (HTTP) ──────────────►                                   │
│                                     Core Agent                         │
│  Local tools:                       • Background refresh               │
│  • read_file, write_file            • .joe/ processing                 │
│  • local_git_diff                   • Onboarding                       │
│  • local_git_status                 • Clarification queue              │
│  • run_command                                                         │
│                                     Core Services                      │
│                                     • Graph Store (Cayley)             │
│                                     • SQL Store (SQLite)               │
│                                     • Adapters (K8s, Git, ArgoCD...)   │
│                                     • LLM (for Core Agent reasoning)   │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘
```

**Development workflow:**
```
Terminal 1:                    Terminal 2:
$ joecored                     $ joe
API listening on :7777         Connecting to joecored... Connected.
Core Agent started             
                               > why is payment slow?
[logs: API request]            [queries joecored, responds]
```

**Core Agent Autonomy:**
- **Autonomous**: Deterministic API changes (pod added, replica count changed)
- **LLM + Auto**: High-confidence interpretations (.joe/ files, clear patterns)
- **Needs Human**: Low-confidence inferences → queued as clarifications

## Directory Structure

```
joe/
├── cmd/
│   ├── joe/                      # Joe Local (User Agent CLI)
│   │   └── main.go
│   └── joecored/                 # Joe Core (daemon)
│       └── main.go
│
├── internal/
│   ├── api/                      # HTTP API handlers (joecored)
│   ├── client/                   # HTTP client (joe → joecored)
│   ├── core/                     # Core Services
│   ├── coreagent/                # Core Agent
│   ├── useragent/                # User Agent
│   ├── llm/                      # LLM adapters (both agents)
│   ├── tools/
│   │   ├── local/                # Local tools (joe)
│   │   └── core/                 # Core tools (call joecored)
│   ├── graph/                    # Graph store (joecored)
│   ├── store/                    # SQL store (joecored)
│   ├── adapters/                 # K8s, Git, ArgoCD... (joecored)
│   ├── repl/                     # REPL (joe)
│   └── config/
└── docs/
```

## Key Interfaces

```go
// LLM Adapter - implement for each provider (used by both agents)
type LLMAdapter interface {
    Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
    ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error)
    Embed(ctx context.Context, text string) ([]float32, error)
}

// Tool - each tool implements this
type Tool interface {
    Name() string
    Description() string
    Parameters() ParameterSchema
    Execute(ctx context.Context, args map[string]any) (any, error)
}

// CoreClient - how joe calls joecored (HTTP client in internal/client/)
type CoreClient interface {
    GraphQuery(ctx context.Context, query string) ([]Node, error)
    GraphRelated(ctx context.Context, nodeID string, depth int) (*Subgraph, error)
    K8sGet(ctx context.Context, cluster, resource, ns, name string) (any, error)
    K8sLogs(ctx context.Context, cluster, pod, ns string, lines int) (string, error)
    GitRead(ctx context.Context, repo, path string) (string, error)
    Clarifications(ctx context.Context) ([]Clarification, error)
    AnswerClarification(ctx context.Context, id, answer string) error
    // ... etc
}

// Graph Store (used by Core Services)
type GraphStore interface {
    AddNode(ctx context.Context, node Node) error
    AddEdge(ctx context.Context, edge Edge) error
    Query(ctx context.Context, q string) ([]Node, error)
    Related(ctx context.Context, nodeID string, depth int) (*Subgraph, error)
}
```

## Implementation Phases

We're building incrementally. Each phase should be working before moving on.

### Phase 1: Foundation (Two Binaries)
- [ ] Restructure: `cmd/joe/` + `cmd/joecored/`
- [ ] HTTP API skeleton (joecored)
- [ ] HTTP client skeleton (joe)
- [ ] Config loading
- [ ] LLM Adapter interface + Claude
- [ ] **Milestone: joecored serves /api/v1/status, joe connects**

### Phase 2: User Agent Loop
- [ ] Tool interface + executor + registry
- [ ] User Agent with agentic loop
- [ ] Basic local tools: `echo`, `ask_user`
- [ ] REPL
- [ ] **Milestone: joe connects to joecored, echo tool works**

### Phase 3: Core Services + API
- [ ] SQL Store + migrations (joecored)
- [ ] Graph Store (joecored)
- [ ] API handlers for graph
- [ ] Core tools in joe: `graph_query`, `graph_related`
- [ ] **Milestone: User Agent queries graph via HTTP**

### Phase 4: Infrastructure
- [ ] K8s adapter + API + tools
- [ ] Git adapter + API + tools
- [ ] Local tools: `read_file`, `write_file`, `local_git_diff`
- [ ] **Milestone: "why is pod X failing?" works**

### Phase 5: Core Agent
- [ ] Core Agent + background refresh (joecored)
- [ ] Clarifications queue + API
- [ ] Onboarding + .joe/ processing
- [ ] **Milestone: Graph auto-updates, clarifications work**

### Phase 6+: Extensions
- [ ] ArgoCD, Prometheus, notifications, more LLM adapters
- [ ] Web UI, VS Code extension

## Testing Strategy

**Testing pyramid:**
- **Unit tests** (many): Fast, test business logic with mocked dependencies
- **Integration tests** (some): Test components together, use build tag `//go:build integration`
- **E2E tests** (few): Full flows against Kind cluster (separate environment)

**Unit tests:**
- Same package, `_test.go` suffix
- Table-driven with `t.Run()` subtests
- Mock interfaces, not concrete types
- Target >80% coverage

**Integration tests:**
- Separate with `//go:build integration`
- Use containers or in-memory DBs
- Run separately: `go test -tags=integration ./...`

**For now:** Focus on unit tests. Integration/E2E come when we have adapters.

## Common Patterns

### Tool Implementation
```go
// internal/tools/echo/echo.go
type EchoTool struct{}

func (t *EchoTool) Name() string { return "echo" }

func (t *EchoTool) Description() string {
    return "Echoes back the input. Useful for testing."
}

func (t *EchoTool) Parameters() tool.ParameterSchema {
    return tool.ParameterSchema{
        Type: "object",
        Properties: map[string]tool.Property{
            "message": {Type: "string", Description: "Message to echo"},
        },
        Required: []string{"message"},
    }
}

func (t *EchoTool) Execute(ctx context.Context, args map[string]any) (any, error) {
    msg, _ := args["message"].(string)
    return map[string]string{"echoed": msg}, nil
}
```

### LLM Request/Response
```go
req := llm.ChatRequest{
    SystemPrompt: systemPrompt,
    Messages:     messages,
    Tools:        toolDefs,  // From tool registry
}

resp, err := adapter.Chat(ctx, req)
if err != nil { ... }

if len(resp.ToolCalls) > 0 {
    // Execute tools, append results, loop back to LLM
} else {
    // Final response, return to user
}
```

### Error Handling
- Return errors, don't panic
- Wrap errors with context: `fmt.Errorf("failed to query graph: %w", err)`
- Log at boundaries, not deep in libraries

## Reference Documents

- `docs/joe-architecture.md` - Full architecture with diagrams
- `docs/joe-dataflow.md` - Data flow details, .joe/ file processing
- `docs/joe-prompt.md` - Prompt for coding LLMs to generate .joe/ files

## Go Standards

Follow `docs/go-standards.md` (the full Go Backend Standards document). Key points for Joe:

### Package Organization Note
The standards say "organize by domain, not technical layer." Joe uses technical layers (`internal/llm/`, `internal/tools/`, `internal/graph/`) because Joe itself IS the domain—it's a single-purpose tool, not a multi-domain business application. The "layers" here represent distinct capabilities, not arbitrary technical groupings.

### Error Handling
```go
// Wrap errors with context
if err != nil {
    return fmt.Errorf("failed to query graph for node %s: %w", nodeID, err)
}

// Check errors with errors.Is/As
if errors.Is(err, ErrNotFound) { ... }
```

### Testing Patterns
```go
// Table-driven tests with subtests
func TestEchoTool_Execute(t *testing.T) {
    tests := []struct {
        name    string
        args    map[string]any
        want    any
        wantErr bool
    }{
        {
            name: "echoes message",
            args: map[string]any{"message": "hello"},
            want: map[string]string{"echoed": "hello"},
        },
        {
            name:    "missing message returns error",
            args:    map[string]any{},
            wantErr: true,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            tool := &EchoTool{}
            got, err := tool.Execute(context.Background(), tt.args)
            if (err != nil) != tt.wantErr {
                t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if !reflect.DeepEqual(got, tt.want) {
                t.Errorf("Execute() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

**Coverage target: >80%** — measure with `go test -cover ./...`

**Integration tests:** Use build tag `//go:build integration` to separate from unit tests.

### Interfaces at Point of Use
The business logic defines interfaces it needs. Infrastructure implements them.

```go
// internal/agent/agent.go - Agent defines what it needs from LLM
type LLM interface {
    Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
}

type Agent struct {
    llm LLM  // Depends on interface, not concrete type
}

// internal/llm/claude/claude.go - Claude implements the interface
type Client struct { ... }
func (c *Client) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) { ... }
```

### Instrumentation (OpenTelemetry)
Instrumentation goes in middleware/decorators, NOT in business logic.

```go
// Middleware wraps handler with metrics/logging
func InstrumentedHandler(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        // ... record metrics, structured logging
        next.ServeHTTP(w, r)
        // ... record latency
    })
}

// Business logic stays clean - no instrumentation imports
func (a *Agent) Run(ctx context.Context, input string) (string, error) {
    // Pure business logic, no metrics/logging calls here
}
```

For Joe MVP, we'll add instrumentation in Phase 6. Keep business logic clean from the start.

### Structured Logging
Use `log/slog` for structured logging at boundaries:

```go
slog.Info("tool executed",
    "tool", toolName,
    "duration_ms", duration.Milliseconds(),
    "session_id", sessionID,
)
```

---

## Current Task

When starting work, check the phase we're in and pick the next unchecked item. Keep changes focused and testable.

**Before implementing:**
1. Understand which component you're building
2. Check the interface it needs to implement
3. Write the test first (or at least the test signature)
4. Implement minimally to pass the test

**After implementing:**
1. `go build ./...` — must compile
2. `go test ./...` — tests must pass
3. `go test -cover ./...` — check coverage (target >80%)
4. `go vet ./...` — no warnings
5. `gofmt -s -w .` — format code
