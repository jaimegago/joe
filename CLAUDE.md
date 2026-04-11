# CLAUDE.md

## Project Identity

Joe (Joe Operates Everything) is an AI-powered infrastructure copilot for platform engineers. Two binaries — `joe` (User Agent CLI) and `joe-core` (Core daemon on :7777) — communicate over HTTP. Joe is AI-agnostic (Claude, OpenAI, Ollama) and builds a graph of infrastructure relationships backed by SQLite.

## Architectural Invariants

- Two-binary split: `joe` (User Agent + REPL + local tools) talks to `joe-core` (HTTP API + Core Agent + adapters + graph) — never merge them
- Graph store is SQLite-backed (20 edge types) — no Cayley
- Action Safety Framework: T1 (read-only, auto) / T2 (write, confirm) / T3 (dangerous, policy-gated)
- LLM can create/update Tier 3 knowledge, but cannot touch Tier 1 (curated)
- OpenTelemetry instrumentation goes in middleware/decorators, NOT in business logic
- Technical layer organization (`internal/llm/`, `internal/tools/`, `internal/graph/`) is intentional — Joe is a single-purpose tool, not a multi-domain business app
- Core Agent autonomy levels: Autonomous (deterministic) -> LLM+Auto (high-confidence) -> Needs Human (queued as clarifications)
- All LLM prompt strings live in `internal/prompts/` — not scattered across packages

## Applicable Skills

- **dev-standards** (`~/.claude/skills/dev-standards/`): universal — verification-before-claiming, read-before-editing, minimal-changes discipline
- **go-backend** (`~/.claude/skills/go-backend/`): authoritative for Go conventions, error handling, testing patterns
- **frontend-dev** (`~/.claude/skills/frontend-dev/`): authoritative for React/TypeScript component structure, styling, and frontend testing

> **Precedence.** When this CLAUDE.md and a referenced skill give conflicting guidance, the skill wins for topics within its scope. This CLAUDE.md only overrides a skill when it explicitly says "repo-specific override:" followed by the rule.

## Build / Test / Lint

```
go build ./...
go test ./...
go vet ./...
gofmt -s -w .
```

Integration tests: `go test -tags=integration ./...`

Frontend (`ui/`):
```
npm run lint
npm run test
```

## Repo-Specific Conventions

- `joe` subcommands: `joe mcp`, `joe slack`, `joe panic`, `joe unlock`, `joe review`
- Core tools (in `internal/tools/core/`) call joe-core over HTTP via `internal/client/`; local tools (in `internal/tools/local/`) run directly in the joe process; shared tools (in `internal/tools/shared/`) are Go-native and used by both
- Category-based observability API: `POST /api/v1/observe/{metrics,logs,traces,alerts,k8s}` resolves backend via graph edges
- RBAC enforcement middleware fires only on paths with sourceID (`/api/v1/{adapter}/{sourceID}/...`)
- Panic state persisted to `~/.joe/panic.state` (YAML); safe mode blocks T2/T3 tools in executor
- MCP server (`joe mcp`) reads `JOE_SERVER` + `JOE_API_KEY` env vars

## Reference Documents

- `docs/joe-architecture.md` — Full architecture with diagrams
- `docs/joe-dataflow.md` — Data flow details, .joe/ file processing
- `docs/joe-prompt.md` — Prompt for coding LLMs to generate .joe/ files
- `docs/security-in-layers.md` — Action Safety Framework, Emergency Shutdown
- `docs/JOE_SECURITY.md` — Security architecture overview (RBAC + Safety)
- `docs/JOE_RBAC_IMPLEMENTATION.md` — RBAC middleware spec
