# CLAUDE.md

## Project Identity

Joe (Joe Operates Everything) is an AI-powered infrastructure copilot for platform engineers. A single binary — `joe` — runs the Core daemon (HTTP API on :7777) as its default behavior, with subcommands (`joe mcp`, `joe slack`, etc.) riding alongside. Joe is AI-agnostic (Claude, OpenAI, Ollama) and builds a graph of infrastructure relationships backed by SQLite.

## Architectural Invariants

- Single `joe` binary: bare `joe` (or `joe --config ...`) starts the server (HTTP API + Core Agent + adapters + graph); subcommands (mcp, slack, panic, unlock, review, skills, zone, admin) dispatch ahead of it. The server entrypoint lives in `cmd/joe/server.go`; the CLI dispatcher in `cmd/joe/main.go`
- Graph store is SQLite-backed (20 edge types) — no Cayley
- Action Safety Framework: T1 (read-only, auto) / T2 (write, confirm) / T3 (dangerous, policy-gated)
- Write floor (`internal/safety/floor.go`): boot-resolved, runtime-immutable read-only value (observation mode or sticky safe-mode/panic) — once up, nothing in the binary lowers it; recovery is restart, never a live down-transition
- Denial precedence is floor > incident > RBAC, ordered by resolvability depth and enforced by check order in the executor (`internal/tools/executor.go`) — not by per-check return values
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

**Verify UI features in the running app before claiming them done.** Green Vitest is necessary but not sufficient: the suite mocks the API boundary, so it cannot catch the recurring bug class here — boundary/integration bugs (react-query cache across mounts, navigation timing, real backend response shapes like `read_only`). Boot the real `joe` binary, open the UI, and click through the flows that historically break (navigate away mid-stream then reopen; open a session as a read-only non-owner; hard reload; multi-principal in separate browser profiles). Use the `/run` and `/verify` skills to drive the real stack.

## Repo-Specific Conventions

- `joe` subcommands: `joe mcp`, `joe slack`, `joe panic`, `joe unlock`, `joe review`
- Core tools (in `internal/tools/core/`) reach the server's API via `internal/client/`; shared tools (in `internal/tools/shared/`) are Go-native
- Category-based observability API: `POST /api/v1/observe/{metrics,logs,traces,alerts,k8s}` resolves backend via graph edges
- RBAC enforcement middleware fires only on paths with a componentID (`/api/v1/{adapter}/{componentID}/...`)
- The registered-external-system entity is a "component" (`store.Component`, `components` table). "source" was renamed per D-0021; the unrelated `knowledge_sources` concept keeps its name
- Panic state persisted to the `cluster_panic_state` DB row (single row, id=1) via `internal/store/panic_store.go` — there is no `panic.state` file; safe mode blocks T2/T3 tools in executor
- MCP server (`joe mcp`) reads `JOE_SERVER` + `JOE_API_KEY` env vars
- **repo-specific override:** do not add a `Co-Authored-By` trailer to commit messages

## Reference Documents

- `docs/joe-architecture.md` — Full architecture with diagrams
- `docs/joe-dataflow.md` — Data flow details, .joe/ file processing
- `docs/joe-prompt.md` — Prompt for coding LLMs to generate .joe/ files
- `docs/security-in-layers.md` — Action Safety Framework, Emergency Shutdown
- `docs/JOE_SECURITY.md` — Security architecture overview (RBAC + Safety)
- `docs/JOE_RBAC_IMPLEMENTATION.md` — RBAC middleware spec
