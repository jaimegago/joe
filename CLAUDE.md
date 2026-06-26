# CLAUDE.md

## Project Identity

Joe (Joe Operates Everything) is an AI-powered infrastructure copilot for platform engineers. A single binary — `joe` — runs the Core daemon (HTTP API on :7777) as its default behavior, with subcommands (`joe mcp`, `joe slack`, etc.) riding alongside. Joe is AI-agnostic — the code supports and validates two LLM providers, `claude` and `gemini` (see `internal/llmfactory/factory.go` and `internal/config/validation.go`) — and builds a graph of infrastructure relationships backed by SQLite.

**License posture:** Joe is a personal, open-source portfolio project licensed **Apache-2.0** (`LICENSE` at the repo root is ground truth), distributed today as **build-from-source only** (no release binaries or install tooling). Release tooling is **scaffolded and CI-validated but not publishing**: a `.goreleaser.yaml` exists and CI runs `goreleaser build --snapshot --clean` to prove the build/injection, but it never releases, tags, or uploads artifacts (`release.disable: true`); flipping it to publish is a deliberate posture change with its own decision entry. This is independent of the separate `joe-sre-skills` starter repo, which is MIT.

## Architectural Invariants

> Before re-deciding anything architectural, consult [`docs/DECISIONS.md`](docs/DECISIONS.md) — the normative, append-only decision log (newest-on-top); where it conflicts with prose here, the log is the source of truth.

- Single `joe` binary: bare `joe` (or `joe --config ...`) starts the server (HTTP API + Core Agent + adapters + graph); subcommands (mcp, slack, skills, incident, panic, unlock) dispatch ahead of it. The server entrypoint lives in `cmd/joe/server.go`; the CLI dispatcher in `cmd/joe/main.go`
- Graph store is SQLite-backed — no Cayley. Edge types: the named set is the relation constants declared in `internal/graph/relations.go`, but `graph_edges.relation` is free-form TEXT with no CHECK constraint, so further types enter as inline string literals (e.g. in `internal/coreagent/`) that bypass the constant set — the declared constants are authoritative as names, but no fixed count is authoritative as the total
- Action Safety Framework: T1 (read-only, auto) / T2 (write, confirm) / T3 (dangerous, policy-gated)
- Write floor (`internal/safety/floor.go`): boot-resolved, runtime-immutable read-only value (observation mode or sticky safe-mode/panic) — once up, nothing in the binary lowers it; recovery is restart, never a live down-transition
- Denial precedence is floor > incident > RBAC, ordered by resolvability depth and enforced by check order in the executor (`internal/tools/executor.go`) — not by per-check return values
- Read posture (D-0041, `internal/readposture/` + `internal/rbac/policy.go`): a single persisted, install-wide scalar selects the read decision. The launch default is **`team_flat`** — every authenticated principal reads every component, regardless of grant; the admit sits after the zone-allows-action gate (it widens WHO may read a permitted action, never WHICH actions a zone allows) and fires for the read action only. **`zoned`** is the grant-based full-mode read path (the prior zone+grant behaviour, unchanged). The posture is read live per request (no boot cache); the flip is an admin-gated, audited operator act on the admin REST surface. The write floor and write-RBAC govern mutates **independently of read posture** — a `team_flat` install with the floor up still denies every mutate
- LLM can create/update Tier 3 knowledge, but cannot touch Tier 1 (curated)
- OpenTelemetry instrumentation goes in middleware/decorators, NOT in business logic
- Build truth has a single source, `internal/buildinfo`: every consumer (the `GET /api/v1/status` and `GET /api/v1/version` handlers, the `joe_build_info` gauge) reads it via `buildinfo.Get()`. The ldflags `-X` injection targets are that package's `Version`/`Commit`/`BuildTime` vars **addressed by full import path** (e.g. `github.com/jaimegago/joe/internal/buildinfo.Version`); no other package declares build-identity vars, and `dev`/`none`/`unknown` appear only on a deliberately unset build. The `ui_digest` field is **not** injected — it is computed once at boot from the embedded UI FS (`webui.DistFS()` → `buildinfo.Init`), so it cannot disagree with the bytes the binary serves
- Technical layer organization (`internal/llm/`, `internal/tools/`, `internal/graph/`) is intentional — Joe is a single-purpose tool, not a multi-domain business app
- Core Agent autonomy levels: Autonomous (deterministic) -> LLM+Auto (high-confidence) -> Needs Human (queued as clarifications)
- All LLM prompt strings live in `internal/prompts/` — not scattered across packages
- Chat sessions are a core subsystem (first-class owned/shareable/incident-linked sessions); its as-built specification is normative in [`docs/DESIGN-CHAT-SESSIONS.md`](docs/DESIGN-CHAT-SESSIONS.md) — consult it before changing session behavior

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

Release-shaped build (embeds the UI and injects build truth into `internal/buildinfo` via ldflags `-X`): `make build`. A plain `go build ./...` still compiles and reports the unset `dev` defaults. CI validates the release path without publishing: `goreleaser build --snapshot --clean`.

Integration tests: `go test -tags=integration ./...`

Frontend (`ui/`):
```
npm run lint
npm run test
```

**Verify UI features in the running app before claiming them done.** Green Vitest is necessary but not sufficient: the suite mocks the API boundary, so it cannot catch the recurring bug class here — boundary/integration bugs (react-query cache across mounts, navigation timing, real backend response shapes like `read_only`). Boot the real `joe` binary, open the UI, and click through the flows that historically break (navigate away mid-stream then reopen; open a session as a read-only non-owner; hard reload; multi-principal in separate browser profiles). Use the `/run` and `/verify` skills to drive the real stack.

## Repo-Specific Conventions

- `joe` subcommands: `joe mcp`, `joe slack`, `joe skills`, `joe incident`, `joe panic`, `joe unlock`
- Core tools (in `internal/tools/core/`) reach the server's API via `internal/client/`; shared tools (in `internal/tools/shared/`) are Go-native
- Category-based observability API: `POST /api/v1/observe/{metrics,logs,traces,alerts,k8s}` resolves backend via graph edges
- Build-identity surface: `GET /api/v1/version` serializes the full `buildinfo.Info` (`version`/`commit`/`build_time`/`ui_digest`) and is the single place `ui_digest` is read; `GET /api/v1/status` reports only `version` (no digest). The `joe_build_info` Prometheus gauge (constant `1`, build identity in labels, registered in the metrics-setup layer beside the business gauges) carries the same fields; its `ui_digest` label makes a stale UI embed visible across replicas. `ui_digest` is a sha256 over the embedded UI bytes the binary serves, recomputable byte-for-byte by an external harness per the canonical serialization documented on `buildinfo.Compute`
- RBAC enforcement middleware fires only on paths with a componentID (`/api/v1/{adapter}/{componentID}/...`)
- The registered-external-system entity is a "component" (`store.Component`, `components` table). "source" was renamed per D-0021; the unrelated `knowledge_sources` concept keeps its name
- Panic state persisted to the `cluster_panic_state` DB row (single row, id=1) via `internal/store/panic_store.go` — there is no `panic.state` file; safe mode blocks T2/T3 tools in executor
- MCP server (`joe mcp`) reads `JOE_SERVER` + `JOE_API_KEY` env vars
- **repo-specific override:** do not add a `Co-Authored-By` trailer to commit messages

## Reference Documents

- `docs/backlog/INDEX.md` — open-work entry point: the index of active backlog items (finished items move to `docs/backlog/done/`)
- `docs/pm-convention.md` — the project-management and session-tracking convention (the slug that joins chat, Claude Code, commits, and decisions)
- `docs/claude_joe_project_instructions.md` — the version-controlled master of the claude.ai project instructions (pure paste-source for the project's instructions field)
- `docs/joe-architecture.md` — Full architecture with diagrams
- `docs/security-in-layers.md` — Action Safety Framework, Emergency Shutdown
- `docs/JOE_SECURITY.md` — Security architecture overview (RBAC + Safety)
- `docs/JOE_RBAC_IMPLEMENTATION.md` — RBAC middleware spec
