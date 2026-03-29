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
│                                     • Graph Store (SQLite)             │
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

## Architectural Invariants

- Graph store is SQLite-backed (19 edge types) — no Cayley
- Action Safety Framework: T1 (read-only, auto) / T2 (write, confirm) / T3 (dangerous, policy-gated)
- LLM can create/update Tier 3 knowledge, but cannot touch Tier 1 (curated)
- OpenTelemetry instrumentation goes in middleware/decorators, NOT in business logic
- Technical layer organization (`internal/llm/`, `internal/tools/`, `internal/graph/`) is intentional — Joe is a single-purpose tool, not a multi-domain business app
- Core Agent autonomy levels: Autonomous (deterministic) → LLM+Auto (high-confidence) → Needs Human (queued as clarifications)

## Directory Structure

```
joe/
├── cmd/
│   ├── joe/                      # Joe Local (User Agent CLI + mcp/slack subcommands)
│   └── joe-core/                 # Joe Core (daemon)
├── internal/
│   ├── api/                      # HTTP API handlers (joe-core)
│   ├── client/                   # HTTP client (joe → joe-core)
│   ├── core/                     # Core Services
│   ├── coreagent/                # Core Agent
│   ├── useragent/                # User Agent
│   ├── llm/                      # LLM adapters (both agents)
│   ├── llmfactory/               # LLM provider factory
│   ├── tools/
│   │   ├── local/                # Local tools (joe only): readfile, writefile, gitstatus, gitdiff, runcmd, echo, askuser
│   │   ├── core/                 # Core tools (joe → joe-core via HTTP): graphquery, k8sget, awsec2, etc.
│   │   └── shared/               # Go-native tools (both): netcheck, dnsquery, httpreq, sysinfo, traceroute
│   ├── graph/                    # Graph store — SQLite (joe-core)
│   ├── store/                    # SQL store (joe-core)
│   ├── adapters/                 # K8s, Git, ArgoCD... (joe-core)
│   ├── knowledge/                # Knowledge store (3 tiers)
│   ├── mcp/                      # MCP stdio server (joe mcp)
│   ├── slack/                    # Slack bot (joe slack)
│   ├── rbac/                     # RBAC: zones, policy engine, middleware
│   ├── safety/                   # Panic mode, safe mode, unlock
│   ├── review/                   # Code review agent (GitHub/GitLab)
│   ├── observe/                  # Normalized observability result types + LLM translator
│   ├── observability/            # OpenTelemetry setup
│   ├── session/                  # Session management
│   ├── crypto/                   # Credential encryption (AES-256-GCM)
│   ├── repl/                     # REPL (joe)
│   ├── config/                   # Configuration
│   ├── notify/                   # Notification dispatch
│   ├── logging/                  # Structured logging setup
│   └── ...                       # constants, env, paths, sqlutil, uid
├── ui/                           # Web UI: React 18 + Vite + Tailwind + shadcn/ui
├── docs/                         # Architecture, security, dataflow docs
└── test/                         # Integration tests, mocks
```

## Capabilities

| Binary | Purpose |
|--------|---------|
| `cmd/joe` | User Agent CLI + REPL; subcommands: `joe mcp`, `joe slack`, `joe panic`, `joe unlock`, `joe review` |
| `cmd/joe-core` | Core daemon, HTTP API on :7777 |

All features implemented: graph store, core agent, 35+ infrastructure adapters (K8s, AWS, Azure, observability, data stores, GitOps, networking, security), action safety framework, knowledge store, documentation co-pilot, RBAC + security zones, emergency shutdown, code review, web UI.

See `docs/joe-architecture.md` for full details.

## Build / Test / Lint

```
go build ./...
go test ./...
go test -cover ./...    # target >80%
go vet ./...
gofmt -s -w .
```

Integration tests: `go test -tags=integration ./...`

## Conventions

- **Error handling**: Return errors, don't panic. Wrap with context: `fmt.Errorf("failed to X: %w", err)`. Log at boundaries, not deep in libraries.
- **Testing**: Table-driven with `t.Run()` subtests. Mock interfaces, not concrete types. Integration tests use `//go:build integration`.
- **Interfaces**: Defined at point of use — business logic defines what it needs, infrastructure implements it.
- **Logging**: `log/slog` structured logging at boundaries only.

## Skills

- **Go backend**: Follow `.claude/skills/go-backend/` for all Go code.
- **Frontend**: Follow `.claude/skills/frontend-dev/` for all `ui/` work.

## Reference Documents

- `docs/joe-architecture.md` — Full architecture with diagrams
- `docs/joe-dataflow.md` — Data flow details, .joe/ file processing
- `docs/joe-prompt.md` — Prompt for coding LLMs to generate .joe/ files
- `docs/security-in-layers.md` — Action Safety Framework, Emergency Shutdown
- `docs/JOE_SECURITY.md` — Security architecture overview (RBAC + Safety)
- `docs/JOE_RBAC_IMPLEMENTATION.md` — RBAC middleware spec
