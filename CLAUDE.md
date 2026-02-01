# CLAUDE.md - Joe Project Context

This file provides context for Claude Code when working on the Joe codebase.

## What is Joe?

Joe (Joe Operates Everything) is an AI-powered infrastructure copilot. It helps platform engineers understand, debug, and operate their infrastructure through natural conversation.

**Key characteristics:**
- AI-agnostic (Claude, OpenAI, Ollama)
- Runs locally on engineer's machine (MVP is single binary)
- Builds a graph of infrastructure relationships
- Uses agentic loop: LLM reasons → calls tools → gets results → continues

## Architecture Overview

Joe is a **single binary** for MVP. The process stays running in interactive mode.

```
$ joe
Joe is ready.

> why is payment-service slow?
[Joe queries graph, calls k8s tools, queries prometheus, responds]
```

**Core components:**
- **Joe Core** (`internal/joe/`) - Main struct, orchestrates everything
- **REPL** (`internal/repl/`) - Interactive command loop
- **Agentic Loop** (`internal/agent/`) - LLM + tool execution loop
- **LLM Adapter** (`internal/llm/`) - Interface for AI providers
- **Tool Executor** (`internal/tools/`) - Executes tool calls from LLM
- **Adapters** (`internal/adapters/`) - K8s, Git, ArgoCD, Prometheus, etc.
- **Graph Store** (`internal/graph/`) - Cayley graph database
- **SQL Store** (`internal/store/`) - SQLite for sources, sessions, cache

**Data flow:**
```
User input → REPL → Joe.Chat() → Agent.Run() → LLM
                                      ↓
                              tool_calls in response
                                      ↓
                              ToolExecutor.Execute()
                                      ↓
                              Adapter (K8s, Git, etc.)
                                      ↓
                              Results back to LLM
                                      ↓
                              Loop until final response
                                      ↓
                              Stream to user
```

## Key Design Decisions

1. **No daemon for MVP** - Single binary, background refresh via goroutines
2. **No gRPC** - HTTP + SSE when we add daemon later (Phase 7)
3. **Joe Core is reusable** - Same code whether run from REPL or HTTP server
4. **LLM interprets .joe/ files** - With hash-based caching to avoid re-interpretation
5. **Graph is rebuildable** - Sources persist in SQL, graph can be reconstructed

## Directory Structure

```
joe/
├── cmd/joe/                    # CLI entry point
├── internal/
│   ├── joe/                    # Joe Core struct
│   ├── agent/                  # Agentic loop
│   ├── session/                # Session management  
│   ├── llm/                    # LLM adapters (claude/, openai/, ollama/)
│   ├── tools/                  # Tool executor + implementations
│   ├── discovery/              # Onboarding, .joe/ processing
│   ├── refresh/                # Background refresh
│   ├── notify/                 # Notifications
│   ├── graph/                  # Graph store (Cayley)
│   ├── store/                  # SQL store (SQLite)
│   ├── adapters/               # Infrastructure adapters
│   ├── repl/                   # REPL / interactive mode
│   └── config/                 # Configuration
└── docs/                       # Architecture docs
```

## Key Interfaces

```go
// LLM Adapter - implement for each provider
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

// Graph Store
type GraphStore interface {
    AddNode(ctx context.Context, node Node) error
    AddEdge(ctx context.Context, edge Edge) error
    Query(ctx context.Context, q string) ([]Node, error)
    Related(ctx context.Context, nodeID string, depth int) (*Subgraph, error)
}
```

## Implementation Phases

We're building incrementally. Each phase should be working before moving on.

### Phase 1: Foundation (Current)
- [x] Scaffold directory structure
- [ ] Config loading
- [ ] LLM Adapter interface + Claude implementation
- [ ] Joe Core struct (shell)

### Phase 2: Core Loop
- [ ] Tool interface + executor + registry
- [ ] Agentic loop (prompt → LLM → tools → loop)
- [ ] Basic tools: `echo`, `ask_user`
- [ ] REPL
- [ ] **Milestone: `joe` runs, can chat, executes tools**

### Phase 3: State
- [ ] SQL Store + migrations
- [ ] Graph Store (Cayley)
- [ ] Graph tools: `graph_query`, `graph_add_node`, `graph_add_edge`
- [ ] Source tools: `register_source`, `list_sources`
- [ ] **Milestone: Can build and query graph through chat**

### Phase 4: Infrastructure
- [ ] K8s adapter
- [ ] K8s tools: `k8s_get`, `k8s_list`, `k8s_logs`
- [ ] Git adapter
- [ ] Git tools: `git_read`, `git_ls`
- [ ] **Milestone: "why is pod X failing?" works**

### Phase 5: Discovery
- [ ] .joe/ file processing with hash cache
- [ ] Onboarding flow (collect → validate → explore)
- [ ] Background refresh goroutine

### Phase 6: Extensions
- [ ] ArgoCD, Prometheus, Loki adapters
- [ ] Session memory with embeddings
- [ ] Notifications

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
