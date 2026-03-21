# CLAUDE.md — Joe Project Context

This file provides context for Claude Code when working on the Joe codebase.

## What is Joe?

Joe (Joe Operates Everything) is an AI-powered infrastructure copilot. It helps platform engineers understand, debug, and operate their infrastructure through natural conversation.

**Key characteristics:**
- AI-agnostic (Claude, OpenAI, Ollama)
- Two binaries: `joe` (Local) and `joe-core` (Core daemon)
- Two agents: User Agent (in joe) + Core Agent (in joe-core)
- HTTP API contract between joe and joe-core
- Builds a graph of infrastructure relationships

## Two-Binary Architecture

Joe is built as two binaries from day one:

```
┌─────────────────────────────────────────────────────────────────────────┐
│                                                                         │
│  joe (Joe Local)                    joe-core (Joe Core)               │
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
$ joe-core                     $ joe
API listening on :7777         Connecting to joe-core... Connected.
Core Agent started
                               > why is payment slow?
[logs: API request]            [queries joe-core, responds]
```

**Core Agent Autonomy:**
- **Autonomous**: Deterministic API changes (pod added, replica count changed)
- **LLM + Auto**: High-confidence interpretations (.joe/ files, clear patterns)
- **Needs Human**: Low-confidence inferences → queued as clarifications

## Directory Structure

```
joe/
├── cmd/
│   ├── joe/                      # Joe Local (User Agent CLI + mcp/slack subcommands)
│   │   └── main.go
│   └── joe-core/                 # Joe Core (daemon)
│       └── main.go
│
├── internal/
│   ├── api/                      # HTTP API handlers (joe-core)
│   ├── client/                   # HTTP client (joe → joe-core)
│   ├── core/                     # Core Services
│   ├── coreagent/                # Core Agent
│   ├── useragent/                # User Agent
│   ├── llm/                      # LLM adapters (both agents)
│   ├── tools/
│   │   ├── local/                # Local tools (joe only): readfile, writefile, gitstatus, gitdiff, runcmd, echo, askuser
│   │   ├── core/                 # Core tools (joe → joe-core via HTTP): graphquery, k8sget, awsec2, etc.
│   │   └── shared/               # Go-native tools (both joe and joe-core): netcheck, dnsquery, httpreq, sysinfo, traceroute
│   ├── graph/                    # Graph store (joe-core)
│   ├── store/                    # SQL store (joe-core)
│   ├── adapters/                 # K8s, Git, ArgoCD... (joe-core)
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

// CoreClient - how joe calls joe-core (HTTP client in internal/client/)
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

## REPL Commands (Joe Local)

The REPL supports slash commands for control operations:

```
> /model                    # Interactive model selector
> /help                     # Show available commands
> /exit or /quit            # Exit Joe
```

### /model Command

Opens an interactive selector showing configured models from config.yaml:

```
> /model

Select model:
    claude-sonnet-4
  • gemini-2.0-flash (current)
    ollama/llama3
    
Use ↑/↓ to navigate, Enter to select, Esc to cancel
```

- Shows all models from `config.yaml` `llm.available` list
- Current model marked with `•` and `(current)`
- Arrow key navigation (up/down)
- Enter selects and switches model
- Esc cancels without changing
- Model switch is hot (no restart needed, conversation continues)

## Capabilities

All features are implemented and working. Joe ships two binaries:

| Binary | Purpose |
|--------|---------|
| `cmd/joe` | User Agent CLI + REPL; subcommands: `joe mcp` (MCP server), `joe slack` (Slack bot), `joe panic`, `joe unlock`, `joe review` |
| `cmd/joe-core` | Core daemon, HTTP API on :7777 |

**Feature summary:**

- **Graph store** — SQLite-backed infrastructure topology (19 edge types)
- **Core Agent** — background refresh, auto-discovery, clarification queue, .joe/ file processing
- **Infrastructure adapters** — K8s, Git, AWS, Azure, Prometheus/Mimir, Loki, Tempo/Jaeger, Datadog, Splunk, Dynatrace, New Relic, Alertmanager, PagerDuty, Grafana, PostgreSQL, MySQL, Redis, MongoDB, Kafka, Elasticsearch, Argo CD, Flux, Terraform, Helm, NGINX Ingress, Envoy, Istio, Cilium, cert-manager, KEDA, OPA/Gatekeeper, Crossplane, Falco, artifact registries
- **Action Safety Framework** — T1/T2/T3 tiers, safety policy (`~/.joe/safety-policy.yaml`), self-protection invariants, credential encryption (AES-256-GCM)
- **Knowledge store** — three tiers (curated/synced/derived), Confluence + Notion sync, semantic search with embeddings
- **Documentation co-pilot** — draft generation, human approval flow, drift detection
- **RBAC + Security zones** — 4 default zones, API key → principal, admin API, protected tables
- **Emergency shutdown** — `/panic` REPL, `joe panic` CLI, `SIGUSR1`, safe mode on restart, `joe unlock`
- **Code review** — GitHub/GitLab webhooks, review agent, PR/MR comment/request-changes tools
- **Web UI** — React 18 + Vite + Tailwind + shadcn/ui; pages: Graph, Dashboard, Sources, Admin, Chat

## Knowledge Store

Joe captures tribal knowledge in three tiers:

| Tier        | Trust    | Management      | Examples                                 |
|------------ |--------- |----------------|------------------------------------------ |
| 1: Curated  | Highest  | Human only     | Notes attached to nodes, onboarding facts |
| 2: Synced   | High     | External source| Runbooks, wiki pages (fetched, cached)    |
| 3: Derived  | Lower    | LLM autonomous | Session learnings, inferred patterns      |

Key principles:
- LLM can create/update Tier 3, but cannot touch Tier 1
- Synced sources are cache; external doc is truth
- Derived insights show provenance ("Learned from session X")
- Joe can propose doc updates, but human approves publish

## Infrastructure Adapters

**Scope (T1 read-only by default):**
- Cloud: AWS (EC2, EKS, RDS, ALB, VPC, SecurityGroups), Azure (VMs, AKS, SQL, VNets, NSGs)
- Observability: Prometheus/Mimir (PromQL), Loki (LogQL), Tempo/Jaeger (traces), Datadog, Splunk (SPL), Dynatrace (DQL), New Relic (NRQL), CloudWatch, Azure Monitor (KQL)
- Alerting: Alertmanager, PagerDuty, Grafana (alerts, dashboards, annotations)
- Network & system diagnostics: Go-native tools in `internal/tools/shared/` (tcp_connect, dns_lookup, http_request, system_info, trace_route) — used by both joe and joe-core
- Data stores: PostgreSQL (pg_stat_*), MySQL (processlist, replication), Redis (INFO, SLOWLOG), MongoDB (serverStatus, rs.status), Kafka (topics, consumer lag, brokers), Elasticsearch (cluster health, indices)
- GitOps/CD/IaC: Argo CD (full REST API), Flux (K8s CRDs), Terraform (state read), Helm (release status)
- Networking/Ingress: NGINX Ingress (rules, status, config), Envoy (admin API), Istio (K8s CRDs), Cilium (CRDs + Hubble)
- K8s CRD-based (low effort): cert-manager, KEDA, OPA/Gatekeeper, Crossplane
- Security/Runtime: Falco (runtime events)

**Key graph edges:**
- `is_k8s_node`: cloud instance → K8s node
- `metrics_in`, `logs_in`, `traces_in`: service → observability source
- `alerts_in`: service → Alertmanager/Grafana/Falco
- `paged_via`: service → PagerDuty
- `stores_in`: service → database (PostgreSQL, MySQL, MongoDB, Redis, Elasticsearch)
- `queues_in`: service → message broker (Kafka)
- `managed_by`: K8s resource → Argo CD app / Flux / Helm release
- `provisions`: Terraform resource / Crossplane → cloud resource
- `ingress_for`: NGINX ingress → backend service
- `proxies`: Envoy → service
- `mesh_for`: Istio config → service
- `policy_enforces`: OPA constraint / Cilium policy → namespace/workload
- `scaled_by`: KEDA scaled object → workload
- `secures`: certificate → service/ingress

The LLM picks the right tool based on graph context — no generic abstraction layer.

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

**Default:** Focus on unit tests. Add integration/E2E tests for critical adapter paths.

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
- `docs/security-in-layers.md` - Security posture, Action Safety Framework, Emergency Shutdown (Panic Mode)
- `docs/JOE_SECURITY.md` - Security architecture overview (RBAC + Safety layers)
- `docs/JOE_RBAC_IMPLEMENTATION.md` - RBAC middleware spec (identity providers, policy engine, audit)

## Skills

- **Go backend**: Follow the `go-backend` skill (`.claude/skills/go-backend/`) for all Go code — services, APIs, tools, adapters.
- **Frontend**: Follow the `frontend-dev` skill (`.claude/skills/frontend-dev/`) for all `ui/` work — React, TypeScript, Tailwind, shadcn/ui.

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

OpenTelemetry instrumentation is in place. Keep business logic clean from instrumentation — it belongs in middleware/decorators only.

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

## Working on Joe

Keep changes focused and testable.

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
