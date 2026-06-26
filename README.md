# Joe — AI-Powered Infrastructure Copilot

Joe (Joe Operates Everything) helps platform engineers understand, debug, and operate their infrastructure through natural conversation.

## What Joe Does

- **Ask questions** — "Why is the payment service slow?" — Joe queries your live infrastructure and knowledge store
- **Debug incidents** — Joe correlates K8s events, metrics, logs, and alerts in a single conversation
- **Explore relationships** — Joe maintains a knowledge graph linking services, databases, clusters, and cloud resources
- **Document your systems** — Joe drafts runbooks and wiki pages from real infrastructure state

---

## Architecture

Joe is a **single binary**. Run bare `joe` and it starts the Core daemon — the
HTTP API on `:7777`, the agentic loop, the LLM adapter, the graph, and the
infrastructure adapters all run in that one process:

```text
joe (the daemon)
─────────────────────────────────
HTTP API (:7777)
Agentic loop (internal/agentloop)
LLM adapter (holds provider API keys)
Tool execution + safety-tier gating
Graph store (SQLite)
SQL store (sources, sessions)
Infrastructure adapters
Core Agent (background refresh)
Knowledge store (embeddings)
```

You talk to the daemon through one of its front-ends — the **Web UI**, **Slack**,
or your editor over **MCP**. The same subcommands that ship in the binary also
cover operator tasks (panic, RBAC, skills, code review, incidents):

```text
joe            — start the Core daemon (default, no subcommand)
joe mcp        — MCP stdio server for Claude Code / Cursor / Codex
joe slack      — Slack bot via Socket Mode (no public URL required)
joe panic      — emergency shutdown
joe unlock     — clear safe mode
joe skills     — install/manage Agent Skills
joe review     — code-review integration
joe incident   — declare/resolve an incident regime
```

Every tool is classified into a safety tier and gated **before** dispatch, so the
same policy applies no matter which front-end issued the request. See
[docs/operations.md](docs/operations.md) for safety tiers and emergency controls.

---

## Quick Start

### Prerequisites

- Go 1.25 or later
- Node 18+ (to build the Web UI)
- One LLM API key — Anthropic **or** Google — set in the environment that runs
  `joe`.

### Build

```bash
git clone https://github.com/jaimegago/joe.git
cd joe
make build
```

This builds the Web UI and compiles a single `joe` binary
(`go build -o joe ./cmd/joe`).

### Set an API key

Provider keys live **only** in the environment that runs `joe`. Set just **one**;
`joe` auto-selects the matching provider:

```bash
export ANTHROPIC_API_KEY="sk-ant-..."   # for Claude
# — or —
export GEMINI_API_KEY="AIza..."         # for Gemini  (GOOGLE_API_KEY also works)
```

If both keys are present, `joe` defaults to Claude; override with
`JOE_LLM_PROVIDER=gemini` (optionally `JOE_LLM_MODEL=<model>`). If neither is set,
`joe` exits at startup with instructions. Configuration is otherwise optional —
see [docs/configuration.md](docs/configuration.md) to pin a model, enable auth, or
tune the server.

### Run

```bash
# Start the daemon (this is where the provider key lives)
joe
```

`joe` logs the model it selected at startup (e.g.
`llm.model=claude/claude-sonnet-4-20250514`).

### Talk to Joe

The primary interface is the **Web UI**:

```bash
# First time only: install UI dependencies
cd ui && npm install && cd ..

# Run the daemon and the UI dev server (each in its own terminal)
make run-joe
make run-ui
```

Open `http://localhost:5173`. The UI connects to `joe` at `localhost:7777` and
gives you chat, a graph explorer, a dashboard, and the RBAC admin panel.

```text
> why is the payment service slow?
[Joe queries K8s, metrics, graph, and responds — with each tool call shown]
```

Prefer Slack or your editor? Joe also ships a Slack bot and an MCP server — see
[docs/integrations.md](docs/integrations.md).

---

## Web UI

A browser dashboard for graph exploration, admin tasks, and chat:

- **Dashboard** — source health, active alerts, recent sessions
- **Graph explorer** — interactive React Flow visualization of infrastructure relationships
- **Admin panel** — manage RBAC zones, policies, and source-zone assignments
- **Chat** — conversational interface to Joe with tool-call display, including a `/model` switcher

See [docs/web-ui.md](docs/web-ui.md) for the full specification.

---

## Safety & Emergency Controls

Every tool Joe can execute is classified into one of three tiers:

| Tier | Name    | Examples                                        | Safe Mode  |
| ---- | ------- | ----------------------------------------------- | ---------- |
| T1   | Observe | `read_file`, `k8s_get`, `graph_query`           | ✅ Allowed |
| T2   | Record  | `write_file`, `graph_add_node`                  | ❌ Blocked |
| T3   | Act     | `run_command` (mutations), `kubectl apply`      | ❌ Blocked |

Joe also has a kill switch. `joe panic` (or `kill -USR1 <joe-pid>`, or
`POST /api/v1/panic`) halts all operations and restarts `joe` in **safe mode**,
where only T1 tools run. Clear it with `joe unlock --reason "..."`.

Full details — the `safety-policy.yaml` format, all four panic triggers, and how
to resume — are in [docs/operations.md](docs/operations.md).

---

## RBAC

Joe supports security zones for multi-user scenarios. Sources are assigned to
zones; principals are granted access to zones.

| Zone            | Allowed Actions                      |
| --------------- | ------------------------------------ |
| `prod-readonly` | read, query                          |
| `prod-write`    | read, query, mutate                  |
| `dev-full`      | read, query, mutate, delete          |
| `unassigned`    | read (default for new sources)       |

Machine callers authenticate with a service-account bearer token; humans log in
via OIDC. Manage zones and policies from the Web UI admin panel or the admin REST
API (`/api/v1/admin/...`) — the single audited writer to RBAC state. See
[docs/operations.md](docs/operations.md) for the admin recipes and
[docs/security-in-layers.md](docs/security-in-layers.md) for the security model.

---

## Skills

Joe loads [Agent Skills](https://agentskills.io) — small, portable folders containing a `SKILL.md` with YAML frontmatter and a markdown body — and surfaces relevant ones into the LLM's context at decision time. Skills encode *how a senior SRE thinks about a class of situation* (judgment frames), not `if-this-then-that` rules. The LLM still does all situational reasoning; skills just ensure it reasons with the right frame loaded.

Skills do **not** bypass safety enforcement. A skill that says "scale aggressively" still goes through the same T3 policy check before any scaling happens.

```bash
# Install all skills from a repo, or a single skill (sparse checkout)
joe skills install github.com/jaimegago/joe-sre-skills
joe skills install github.com/jaimegago/joe-sre-skills/restart-loop-diagnosis

# Inspect, update, remove
joe skills list
joe skills status
joe skills update
joe skills remove restart-loop-diagnosis
```

Installed skills live under `~/.joe/skills/`, tracked by
`~/.joe/skills/skills.lock.yaml`. New or untrusted skills land in quarantine and
require `joe skills approve <name>` before Joe will load them — `~/.joe/skills-policy.yaml`
defines trusted sources. A starter library of senior-SRE judgment skills lives at
[github.com/jaimegago/joe-sre-skills](https://github.com/jaimegago/joe-sre-skills)
(MIT). See [docs/joe-skills-design.md](docs/joe-skills-design.md) for the design.

---

## Infrastructure Adapters

Joe connects to your infrastructure through registered sources. Add a source via
the API:

```bash
curl -X POST http://localhost:7777/api/v1/sources \
  -H "Authorization: Bearer $JOE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "id": "k8s-prod",
    "name": "Production Cluster",
    "type": "kubernetes",
    "config": {"kubeconfig_path": "/home/me/.kube/config"}
  }'
```

**Supported adapter types:** Kubernetes, Git, AWS (EC2/EKS/RDS/VPC), Azure, Prometheus/Mimir, Loki, Tempo, Jaeger, Alertmanager, PagerDuty, Grafana, Datadog, Splunk, Dynatrace, New Relic, PostgreSQL, MySQL, Redis, MongoDB, Kafka, Elasticsearch, Argo CD, Flux, Terraform, Helm, NGINX Ingress, Envoy, Istio, Cilium, cert-manager, KEDA, OPA/Gatekeeper, Crossplane, Falco, OCI/DockerHub/GHCR/Harbor, ECR, Artifactory

---

## Knowledge Store

Joe learns from your documentation and operations:

- **Curated** (Tier 1) — Notes attached to graph nodes; human-managed, immutable by LLM
- **Synced** (Tier 2) — Confluence / Notion pages fetched and cached
- **Derived** (Tier 3) — Patterns inferred from sessions; shown with provenance

Enable background sync under `knowledge:` in your config — see
[docs/configuration.md](docs/configuration.md).

---

## Testing

```bash
go test ./...                    # unit tests
go test -cover ./...             # with coverage
go test -tags=integration ./...  # integration tests
go build ./...                   # build check
go vet ./...                     # lint
```

See the [`test/`](test/) tree (`test/mocks`, `test/integration`, `test/e2e`) and the **Build / Test / Lint** section of [CLAUDE.md](CLAUDE.md) for the full test harness and commands.

---

## Project Structure

```text
joe/
├── cmd/
│   └── joe/            # The single binary: daemon (server.go) + subcommands
├── internal/
│   ├── access/         # In-process infra access with caller principal
│   ├── adapters/       # Infrastructure adapters (K8s, AWS, Prometheus, ...)
│   ├── agentctx/       # Agent execution context
│   ├── agentloop/      # The agentic loop (single runtime)
│   ├── api/            # HTTP API handlers
│   ├── audit/          # Append-only audit log
│   ├── auth/           # Authentication (service accounts, OIDC login)
│   ├── captaingate/    # Captain-reachability gating
│   ├── client/         # HTTP client (subcommands → daemon)
│   ├── config/         # Configuration loading
│   ├── constants/      # Shared constants
│   ├── core/           # Core services container
│   ├── coreagent/      # Core Agent (background refresh, onboarding)
│   ├── crypto/         # Cryptography utilities
│   ├── env/            # Environment variable handling
│   ├── findings/       # Code-review findings
│   ├── graph/          # Graph store (SQLite)
│   ├── knowledge/      # Knowledge store, embeddings, sync, proposals
│   ├── llm/            # LLM adapter interface + Claude/Gemini implementations
│   ├── llmfactory/     # LLM adapter factory
│   ├── llmsettings/    # Runtime LLM settings (model switching)
│   ├── llmusage/       # Token/cost accounting
│   ├── logging/        # Logging configuration
│   ├── mcp/            # MCP server implementation
│   ├── notify/         # Notification system
│   ├── observability/  # OpenTelemetry metrics + tracing
│   ├── observe/        # Normalized observability result types + LLM translator
│   ├── paths/          # ~/.joe/ path helpers
│   ├── prompts/        # All LLM prompt strings (centralized)
│   ├── rbac/           # RBAC zones, policy engine, middleware
│   ├── review/         # Code review integration (GitHub/GitLab)
│   ├── runmodel/       # Run/execution model
│   ├── safety/         # Action tiers, panic mode, safe mode, policy loader
│   ├── seams/          # Cross-cutting seams / extension points
│   ├── session/        # Session management
│   ├── sessiongate/    # Session-level gating
│   ├── sessionmodel/   # Session data model
│   ├── slack/          # Slack bot (Socket Mode)
│   ├── sqlutil/        # SQL utilities
│   ├── store/          # SQL store (SQLite) + migrations (001–020)
│   ├── tools/          # Tool registry, executor, tier enforcement
│   │   ├── core/       # Core tools (graph, K8s, cloud via HTTP)
│   │   ├── local/      # Local tools (file I/O, git, run_command)
│   │   └── shared/     # Shared tools (dns, http, netcheck, traceroute)
│   ├── uid/            # UID generation
│   ├── warnings/       # Operator warnings surface
│   └── webui/          # Web UI backend endpoints
├── ui/                 # Web UI (React 18 + Vite + Tailwind + shadcn/ui)
├── docs/               # Architecture, design, and operator docs
├── test/               # Integration + E2E test harness
├── config.example.yaml
└── Makefile
```

---

## Documentation

- [docs/configuration.md](docs/configuration.md) — Config file reference and environment variables
- [docs/operations.md](docs/operations.md) — Safety tiers, emergency shutdown, RBAC admin
- [docs/integrations.md](docs/integrations.md) — MCP server and Slack bot setup
- [docs/joe-architecture.md](docs/joe-architecture.md) — Full architecture and component diagrams
- [docs/web-ui.md](docs/web-ui.md) — Web UI specification
- [docs/security-in-layers.md](docs/security-in-layers.md) — Security authority: Action Safety Framework, RBAC, read posture, Panic Mode
- [docs/joe-skills-design.md](docs/joe-skills-design.md) — Skills system design
- [docs/observability.md](docs/observability.md) — OpenTelemetry instrumentation
- [test/README.md](test/README.md) — Test harness layout (`test/mocks`, `test/integration`, `test/e2e`)

---

## License

Joe is licensed under the Apache License 2.0. See [LICENSE](LICENSE) for the full text.
