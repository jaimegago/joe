# CLAUDE.md — Joe Project Context

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
│   │   ├── local/                # Local tools (joe only): readfile, writefile, gitstatus, gitdiff, runcmd, echo, askuser
│   │   ├── core/                 # Core tools (joe → joecored via HTTP): graphquery, k8sget, awsec2, etc.
│   │   └── shared/               # Go-native tools (both joe and joecored): netcheck, dnsquery, httpreq, sysinfo, traceroute
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

## Implementation Phases

We're building incrementally. Each phase should be working before moving on.

### Phase 1: Foundation ✅ COMPLETE
### Phase 2: User Agent Loop ✅ COMPLETE  
### Phase 3: Core Services + API ✅ COMPLETE
- SQL Store (8 tables), Graph Store (SQLite-based), API handlers, Core tools

### Phase 4: Infrastructure Adapters ✅ COMPLETE
- K8s adapter (Connect, ListResources, GetResource, GetPodLogs)
- Git adapter (Connect, ReadFile, ListFiles, Log, Diff)
- API endpoints and core tools for both

### Phase 5: Core Agent ✅ COMPLETE
- [x] Background refresh loop
- [x] Auto-discovery implementation
- [x] Clarifications system (list, answer, dismiss + graph ops)
- [x] Onboarding flow
- [x] .joe/ file processing with cache

### Phase 5.5: Action Safety Framework ✅ COMPLETE

Prerequisite for Phase 6. Hardcoded safety enforcement. See `docs/security-in-layers.md`.

- [x] Safety policy loader (`~/.joe/safety-policy.yaml`) — `internal/safety/policy.go`
- [x] Action tier registry (T1: Observe, T2: Record, T3: Act) — `internal/safety/tier.go`
- [x] Tool executor gate (check tier + policy before every Execute) — `internal/tools/executor.go`
- [x] Self-protection invariants (Joe can't touch `~/.joe/`, can't run joe/joecored/kill) — `internal/safety/invariants.go`
- [x] Path sandboxing for write_file (`allowed_directories`) — `internal/tools/local/writefile/writefile.go`
- [x] run_command subcommand validation for kubectl/helm/argocd — `internal/tools/local/runcmd/subcommands.go`
- [x] T3 blocking notification in REPL, T2 post-execution log — `internal/repl/notifier.go`, `internal/safety/notifier.go`
- [x] API auth (Bearer token) + request size limits — `internal/api/middleware.go`

### Phase 6: Infrastructure Adapters ✅ COMPLETE

All new adapters T1 (read-only) by default. Mutations require T3 classification + policy flag.
- [x] 6.1 Core foundations (source types, registry wiring, graph edges)
- [x] 6.2 Cloud adapters (AWS: EC2/EKS/RDS/VPC, Azure: VMs/AKS/SQL/VNets)
- [x] 6.3 Observability open-source (Prometheus/Mimir, Loki, Tempo/Jaeger)
- [x] 6.4 Alerting & dashboards (Alertmanager, PagerDuty, Grafana)
- [x] 6.5 Safety & hardening (credential encryption, TLS, rate limiting, tool tier classification)
- [x] 6.6 Network & system diagnostics — Go-native shared tools (tcp_connect, dns_lookup, http_request, system_info, trace_route)
- [x] 6.7 Data stores (PostgreSQL, MySQL, Redis, MongoDB, Kafka, Elasticsearch)
- [x] 6.8 GitOps, CD & IaC (Argo CD full adapter, Flux, Terraform, Helm)
- [x] 6.9 Networking & ingress (NGINX Ingress, Envoy, Istio, Cilium)
- [x] 6.10 K8s CRD-based — low effort (cert-manager, KEDA, OPA/Gatekeeper, Crossplane)
- [x] 6.11 Security & runtime (Falco)
- [x] 6.12 Proprietary observability vendors (Datadog, Splunk, Dynatrace, New Relic)
- [x] 6.13 Artifact registries — OCI/DockerHub/GHCR/Harbor/Quay, JFrog Artifactory, AWS ECR (image_stored_in + publishes_to graph edges)
- [x] Graph edges: all 19 types wired (metrics_in, logs_in, traces_in, alerts_in, paged_via, dashboard_in, is_k8s_node, stores_in, queues_in, managed_by, ingress_for, proxies, mesh_for, policy_enforces, scaled_by, secures, provisions, image_stored_in, publishes_to)
- [x] Safety: credential encryption (AES-256-GCM), TLS, rate limiting, T1/T2/T3 tier classification

### Phase 7: Knowledge Store ✅ COMPLETE

- [x] Knowledge tiers (curated, synced, derived)
- [x] Synced sources (Confluence, Notion)
- [x] LLM-derived insights from sessions
- [x] Semantic search with embeddings

### Phase 8: Documentation Co-Pilot ✅ COMPLETE

- [x] Write adapters for wikis (Confluence PUT, Notion block replace, Git commit+push)
- [x] Draft generation via LLM + knowledge store search → unified diff → proposal
- [x] Human approval flow (pending → approved → published / rejected)
- [x] Drift detection for Tier 2 synced entries (SHA-256 hash comparison)
- [x] API endpoints: `/api/v1/knowledge/proposals`, `/api/v1/knowledge/drift`
- [x] Client bindings + 3 new tools: `detect_doc_drift` (T1), `generate_doc_draft` (T2), `publish_doc_update` (T3)
- [x] SQL migration 005, proposals repository + service, tier registry entries

### Phase 9: Security Architecture + Additional Clients ← CURRENT

**Security Zones & Pluggable Architecture:**
- [ ] `cmd/joe-security/` binary (optional, for hardened deployments)
- [ ] Security zones: prod-readonly, prod-write, dev-full, unassigned (default)
- [ ] Source → Zone assignments (admin-controlled, LLM cannot modify)
- [ ] Pluggable: embedded mode (same DB) or remote mode (separate joe-security process)
- [ ] Protected tables: security_zones, source_zone_assignments, rbac_policies (hardcoded, LLM cannot write)

**RBAC & Admin API:**
- [ ] Principal → Zones policy model
- [ ] Admin API: `/api/v1/admin/zones`, `/api/v1/admin/source-zones`, `/api/v1/admin/policies`
- [ ] Authentication adapters (Entra ID, LDAP, OIDC, API keys)
- [ ] Notification for unassigned sources

**Emergency Shutdown (Panic Mode):**
- [ ] `/panic` REPL, `joe panic` CLI, `POST /api/v1/panic`, SIGUSR1
- [ ] Safe mode on restart (T1 only until explicit unlock)
- [ ] `joe unlock --reason "..."` to resume

**Additional Clients:**
- [ ] Web UI (dashboards, graph visualization, planning)
- [ ] MCP Server (Claude Code, Cursor, Codex)
- [ ] Slack Bot (ChatOps, optional)

**See:** `docs/JOE_SECURITY.md` (comprehensive), `docs/security-in-layers.md` Part 7

### Phase 10: Code Review Integration
- [ ] GitHub/GitLab adapters with PR/MR capabilities
- [ ] Webhook receiver (`POST /api/v1/webhooks/github`, `/gitlab`)
- [ ] Review job queue (SQLite-backed, idempotent)
- [ ] Review Agent: fetch diff → query graph → query knowledge → LLM analysis → post review
- [ ] Safety policy: github_comment (T2), github_request_changes (T3), github_approve (T3, disabled)

## Knowledge Store (Phase 7)

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

## Infrastructure Adapters (Phase 6)

**Scope (T1 read-only by default):**
- Cloud: AWS (EC2, EKS, RDS, ALB, VPC, SecurityGroups), Azure (VMs, AKS, SQL, VNets, NSGs)
- Observability: Prometheus/Mimir (PromQL), Loki (LogQL), Tempo/Jaeger (traces), Datadog, Splunk (SPL), Dynatrace (DQL), New Relic (NRQL), CloudWatch, Azure Monitor (KQL)
- Alerting: Alertmanager, PagerDuty, Grafana (alerts, dashboards, annotations)
- Network & system diagnostics: Go-native tools in `internal/tools/shared/` (tcp_connect, dns_lookup, http_request, system_info, trace_route) — used by both joe and joecored
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
- `docs/security-in-layers.md` - Security posture, Action Safety Framework, Emergency Shutdown (Panic Mode)
- `docs/JOE_SECURITY.md` - Security architecture overview (RBAC + Safety layers)
- `docs/JOE_RBAC_IMPLEMENTATION.md` - RBAC middleware spec (identity providers, policy engine, audit)

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

OpenTelemetry instrumentation is in place. Phase 6 extends it to new adapters while keeping business logic clean from the start.

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
