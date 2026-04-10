# Joe — AI-Powered Infrastructure Copilot

Joe (Joe Operates Everything) helps platform engineers understand, debug, and operate their infrastructure through natural conversation.

## What Joe Does

- **Ask questions** — "Why is the payment service slow?" — Joe queries your live infrastructure and knowledge store
- **Debug incidents** — Joe correlates K8s events, metrics, logs, and alerts in a single conversation
- **Explore relationships** — Joe maintains a knowledge graph linking services, databases, clusters, and cloud resources
- **Document your systems** — Joe drafts runbooks and wiki pages from real infrastructure state

---

## Architecture

Joe runs as two binaries with a clean HTTP boundary:

```text
joe (User Agent)                    joe-core (Core Daemon)
─────────────────                   ──────────────────────
Interactive REPL           HTTP     API (:7777)
Agentic loop → LLM  ──────────────► Graph store (SQLite)
Local tools (direct)                SQL store (sources, sessions)
Core tools (via API)                Infrastructure adapters
                                    Core Agent (background refresh)
                                    Knowledge store (embeddings)
```

`joe` also ships two integrated modes via subcommands:

```text
joe mcp    — MCP stdio server for Claude Code / Cursor / Codex
joe slack  — Slack bot via Socket Mode (no public URL required)
```

---

## Quick Start

### Prerequisites

- Go 1.25 or later
- An LLM API key (Anthropic or Google)

### Build

```bash
git clone https://github.com/jaimegago/joe.git
cd joe
make build
```

Produces two binaries:

| Binary     | Purpose                          |
| ---------- | -------------------------------- |
| `joe`      | Interactive CLI + mode launcher  |
| `joe-core` | Background daemon                |

### Configure

Create `~/.joe/config.yaml`:

```yaml
llm:
  current: claude-sonnet          # active model key

  available:
    claude-sonnet:
      provider: claude
      model: claude-sonnet-4-20250514
    gemini-flash:
      provider: gemini
      model: gemini-2.5-flash

  # API keys are NEVER stored in config — set via env vars:
  #   ANTHROPIC_API_KEY   (Claude)
  #   GEMINI_API_KEY      (Gemini)

server:
  address: "localhost:7777"
  api_key: ""                     # Bearer token for API auth (recommended in production)
  principal: "default-operator"   # RBAC principal name for this API key

refresh:
  interval_minutes: 5

logging:
  level: info                     # debug | info | warn | error
```

Or copy the bundled example:

```bash
cp config.example.yaml ~/.joe/config.yaml
```

### Set API Keys

```bash
export ANTHROPIC_API_KEY="sk-ant-..."   # for Claude
export GEMINI_API_KEY="AIza..."         # for Gemini
```

### Run

```bash
# Terminal 1 — start the daemon
joe-core

# Terminal 2 — start the interactive client
joe
```

```text
Using claude/claude-sonnet-4-20250514
> why is the payment service slow?
[Joe queries K8s, metrics, graph, and responds]
```

---

## Configuration Reference

### `~/.joe/config.yaml`

```yaml
llm:
  current: <model-key>            # key from llm.available
  available:
    <key>:
      provider: claude | gemini | ollama
      model: <model-id>

server:
  address: "localhost:7777"       # joe-core listen address
  api_key: ""                     # Bearer token (empty = auth disabled)
  principal: "default-operator"   # RBAC identity for this API key
  tls_cert_file: ""               # path to TLS cert (enables HTTPS)
  tls_key_file: ""                # path to TLS key
  tls_enabled: false              # joe client: connect over HTTPS
  rate_limit_rps: 0               # requests/sec per IP (0 = disabled)
  rate_limit_burst: 10

refresh:
  interval_minutes: 5
  llm_budget:
    max_calls_per_hour: 100
    batch_threshold: 10
    batch_timeout_sec: 30

logging:
  level: info                     # debug | info | warn | error
  file: ""                        # log file path (empty = stderr only)

knowledge:
  embedding_model: ""             # model key for embeddings (defaults to llm.current)
  semantic_top_k: 5
  sync_enabled: false             # enable Confluence/Notion background sync
```

### Environment Variables

| Variable              | Config equivalent  | Notes                              |
| --------------------- | ------------------ | ---------------------------------- |
| `ANTHROPIC_API_KEY`   | —                  | Required for Claude                |
| `GEMINI_API_KEY`      | —                  | Required for Gemini                |
| `JOE_SERVER_ADDRESS`  | `server.address`   |                                    |
| `JOE_API_KEY`         | `server.api_key`   | Bearer token                       |
| `JOE_LOG_LEVEL`       | `logging.level`    |                                    |
| `JOE_DATABASE_DSN`    | —                  | Override database path/DSN         |
| `JOE_LLM_PROVIDER`   | `llm.current` key  | Override LLM provider at runtime   |
| `JOE_LLM_MODEL`      | model ID           | Override LLM model at runtime      |

---

## REPL Commands

Inside `joe`, type `/` followed by a command:

| Command   | Description                                                              |
| --------- | ------------------------------------------------------------------------ |
| `/model`  | Interactively switch LLM models without restart                         |
| `/panic`  | **Emergency shutdown** — halt all ops, restart in safe mode             |
| `/help`   | Show all available commands                                             |
| `/exit`   | Exit                                                                    |

### Model Switching

```text
> /model

Select model:
  • claude-sonnet (current)
    gemini-flash
    gemini-pro

Use ↑/↓ to navigate, Enter to select, Esc to cancel
```

---

## Emergency Shutdown (Panic Mode)

Joe has a kill switch for runaway operations. Four ways to trigger it:

**From the REPL:**

```text
> /panic
⚠️  This will halt all Joe operations and restart joe-core in safe mode.
Type 'yes' to confirm: yes
Emergency shutdown triggered. joe-core will restart in safe mode.
```

**From the CLI:**

```bash
joe panic --reason "runaway mutation detected"
```

**Via HTTP API:**

```bash
curl -X POST http://localhost:7777/api/v1/panic \
  -H "Authorization: Bearer $JOE_API_KEY" \
  -d '{"reason": "runaway mutation detected"}'
```

**Via Unix signal:**

```bash
kill -USR1 $(pidof joe-core)
```

### What Happens

1. joe-core writes `~/.joe/panic.state` and exits with code 2
2. On restart, joe-core reads `panic.state` and boots in **safe mode**
3. In safe mode, only T1 (read-only) tools are allowed — no writes or mutations
4. Joe logs a warning on every startup until safe mode is cleared

### Check Status

```bash
curl http://localhost:7777/api/v1/panic/status \
  -H "Authorization: Bearer $JOE_API_KEY"
# {"safe_mode":true,"triggered_at":"...","trigger_source":"api","trigger_reason":"..."}
```

### Resume Normal Operation

A reason is required for the audit log:

```bash
# CLI
joe unlock --reason "false alarm — incident resolved"

# HTTP
curl -X POST http://localhost:7777/api/v1/unlock \
  -H "Authorization: Bearer $JOE_API_KEY" \
  -d '{"reason": "false alarm — incident resolved"}'
```

---

## Action Safety Tiers

Every tool Joe can execute is classified into one of three tiers. The tier determines confirmation behavior and safe-mode restrictions.

| Tier | Name    | Examples                                        | Safe Mode  |
| ---- | ------- | ----------------------------------------------- | ---------- |
| T1   | Observe | `read_file`, `k8s_get`, `graph_query`           | ✅ Allowed |
| T2   | Record  | `write_file`, `graph_add_node`                  | ❌ Blocked |
| T3   | Act     | `run_command` (mutations), `kubectl apply`      | ❌ Blocked |

Configure tier behavior in `~/.joe/safety-policy.yaml`:

```yaml
# ~/.joe/safety-policy.yaml

# Allow T3 tools (disabled by default — explicit opt-in required)
allow_t3: false

# Directories write_file is allowed to write to (empty = unrestricted)
allowed_directories:
  - /tmp/joe-workspace
  - /home/me/projects

# run_command allowlist by subcommand
run_command:
  allowed_subcommands:
    - kubectl get
    - kubectl logs
    - kubectl describe
    - helm list
```

---

## RBAC (Role-Based Access Control)

Joe supports security zones for multi-user scenarios. Sources are assigned to zones; principals are granted access to zones.

### Default Zones

| Zone            | Allowed Actions                      |
| --------------- | ------------------------------------ |
| `prod-readonly` | read, query                          |
| `prod-write`    | read, query, mutate                  |
| `dev-full`      | read, query, mutate, delete          |
| `unassigned`    | read (default for new sources)       |

### Managing Zones via Admin API

All admin endpoints require Bearer auth.

**List zones:**

```bash
curl http://localhost:7777/api/v1/admin/zones \
  -H "Authorization: Bearer $JOE_API_KEY"
```

**Create a zone:**

```bash
curl -X POST http://localhost:7777/api/v1/admin/zones \
  -H "Authorization: Bearer $JOE_API_KEY" \
  -d '{"id":"staging","name":"Staging","allowed_actions":["read","query","mutate"]}'
```

**Assign a source to a zone:**

```bash
curl -X POST http://localhost:7777/api/v1/admin/source-zones \
  -H "Authorization: Bearer $JOE_API_KEY" \
  -d '{"source_id":"k8s-prod","zone_id":"prod-readonly","assigned_by":"alice","reason":"initial setup"}'
```

**Grant a principal access to a zone:**

```bash
curl -X POST http://localhost:7777/api/v1/admin/policies \
  -H "Authorization: Bearer $JOE_API_KEY" \
  -d '{"principal":"alice","zone_id":"prod-readonly"}'
```

**List unassigned sources:**

```bash
curl http://localhost:7777/api/v1/admin/unassigned \
  -H "Authorization: Bearer $JOE_API_KEY"
```

### Configure the Principal for Your API Key

```yaml
# ~/.joe/config.yaml
server:
  api_key: "my-secret-token"
  principal: "ops-team"   # RBAC identity mapped to this token
```

---

## MCP Server (Claude Code / Cursor / Codex)

`joe mcp` exposes 8 Joe tools over the Model Context Protocol, letting your editor query live infrastructure directly.

### Setup

Add to your MCP client config (e.g. Claude Code's `.claude/mcp.json`):

```json
{
  "mcpServers": {
    "joe": {
      "command": "joe",
      "args": ["mcp"],
      "env": {
        "JOE_SERVER": "http://localhost:7777",
        "JOE_API_KEY": "<your-api-key>"
      }
    }
  }
}
```

### Available MCP Tools

All observability tools accept natural language questions — Joe resolves the backend from the graph automatically.

| Tool                   | Description                                                        |
| ---------------------- | ------------------------------------------------------------------ |
| `joe_graph_query`      | Search infrastructure graph nodes                                  |
| `joe_graph_related`    | Find nodes related to a given node                                 |
| `joe_k8s`              | Answer Kubernetes questions (pods, deployments, logs) for a service |
| `joe_metrics`          | Query metrics — Joe resolves Prometheus/Datadog/etc. from the graph |
| `joe_logs`             | Search logs — Joe resolves Loki/Splunk/etc. from the graph          |
| `joe_traces`           | Find traces — Joe resolves Tempo/Jaeger from the graph              |
| `joe_alerts`           | List active alerts from Alertmanager/PagerDuty                     |
| `joe_knowledge_search` | Semantic search over runbooks and docs                             |

---

## Slack Bot

`joe slack` connects Joe to Slack via Socket Mode — no public URL required.

### Slack Setup

```bash
export SLACK_BOT_TOKEN="xoxb-..."    # Bot User OAuth token
export SLACK_APP_TOKEN="xapp-..."    # App-Level token (connections:write scope)
export JOE_SERVER="http://localhost:7777"
export JOE_API_KEY="<your-api-key>"  # optional

joe slack
```

### Available Slash Commands

| Command             | Description                                                     |
| ------------------- | --------------------------------------------------------------- |
| `/joe ask <query>`  | Query the infrastructure graph and knowledge store              |
| `/joe status`       | Show graph summary                                              |
| `/joe help`         | Show available commands                                         |

Unrecognized subcommands are treated as queries (same as `/joe ask <text>`).

---

## Web UI

Joe includes a browser-based dashboard for graph exploration, admin tasks, and chat.

### Run the Web UI

```bash
# Install dependencies (first time only)
cd ui && npm install && cd ..

# Start joe-core + Web UI together
make run-stack

# Or start the UI dev server separately (requires joe-core running)
make run-ui
```

Open `http://localhost:5173`. The UI connects to joe-core at `localhost:7777`.

### Features

- **Dashboard** — source health, active alerts, recent sessions
- **Graph explorer** — interactive React Flow visualization of infrastructure relationships
- **Admin panel** — manage RBAC zones, policies, and source-zone assignments
- **Chat** — conversational interface to Joe with tool call display

See [docs/web-ui.md](docs/web-ui.md) for the full specification.

---

## Infrastructure Adapters

Joe connects to your infrastructure through registered sources. Add sources via the API:

```bash
# Register a Kubernetes cluster
curl -X POST http://localhost:7777/api/v1/sources \
  -H "Authorization: Bearer $JOE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "id": "k8s-prod",
    "name": "Production Cluster",
    "type": "kubernetes",
    "config": {"kubeconfig_path": "/home/me/.kube/config"}
  }'

# Register a Prometheus instance
curl -X POST http://localhost:7777/api/v1/sources \
  -H "Authorization: Bearer $JOE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "id": "prom-prod",
    "name": "Production Prometheus",
    "type": "prometheus",
    "config": {"endpoint": "http://prometheus.monitoring.svc:9090"}
  }'
```

**Supported adapter types:** Kubernetes, Git, AWS (EC2/EKS/RDS/VPC), Azure, Prometheus/Mimir, Loki, Tempo, Jaeger, Alertmanager, PagerDuty, Grafana, Datadog, Splunk, Dynatrace, New Relic, PostgreSQL, MySQL, Redis, MongoDB, Kafka, Elasticsearch, Argo CD, Flux, Terraform, Helm, NGINX Ingress, Envoy, Istio, Cilium, cert-manager, KEDA, OPA/Gatekeeper, Crossplane, Falco, OCI/DockerHub/GHCR/Harbor, ECR, Artifactory

---

## Knowledge Store

Joe learns from your documentation and operations:

- **Curated** (Tier 1) — Notes attached to graph nodes; human-managed, immutable by LLM
- **Synced** (Tier 2) — Confluence / Notion pages fetched and cached
- **Derived** (Tier 3) — Patterns inferred from sessions; shown with provenance

Enable background sync in config:

```yaml
knowledge:
  sync_enabled: true
  embedding_model: claude-sonnet  # model used for semantic embeddings
```

---

## Testing

```bash
# Run all unit tests
go test ./...

# With coverage
go test -cover ./...

# Integration tests only
go test -tags=integration ./...

# Build check
go build ./...

# Lint
go vet ./...
```

---

## Project Structure

```text
joe/
├── cmd/
│   ├── joe/            # Interactive CLI + mcp/slack subcommands
│   └── joe-core/       # Background daemon
├── internal/
│   ├── adapters/       # Infrastructure adapters (K8s, AWS, Prometheus, ...)
│   ├── api/            # HTTP API handlers (joe-core) + Web UI endpoints
│   ├── client/         # HTTP client (joe → joe-core)
│   ├── config/         # Configuration loading
│   ├── constants/      # Shared constants
│   ├── core/           # Core services container
│   ├── coreagent/      # Core Agent (background refresh, onboarding)
│   ├── crypto/         # Cryptography utilities
│   ├── env/            # Environment variable handling
│   ├── graph/          # Graph store (SQLite)
│   ├── knowledge/      # Knowledge store, embeddings, sync, proposals
│   ├── llm/            # LLM adapter interface + Claude/Gemini implementations
│   ├── llmfactory/     # LLM adapter factory
│   ├── logging/        # Logging configuration
│   ├── mcp/            # MCP server implementation
│   ├── notify/         # Notification system
│   ├── observability/  # OpenTelemetry metrics + tracing
│   ├── observe/        # Normalized observability result types + LLM translator
│   ├── paths/          # ~/.joe/ path helpers
│   ├── prompts/        # All LLM prompt strings (centralized)
│   ├── rbac/           # RBAC zones, policy engine, middleware
│   ├── repl/           # Interactive REPL + model selector
│   ├── review/         # Code review integration (GitHub/GitLab)
│   ├── safety/         # Action tiers, panic mode, safe mode, policy loader
│   ├── session/        # Session management
│   ├── slack/          # Slack bot (Socket Mode)
│   ├── sqlutil/        # SQL utilities
│   ├── store/          # SQL store (SQLite) + migrations (001–006)
│   ├── tools/          # Tool registry, executor, tier enforcement
│   │   ├── core/       # Core tools (graph, K8s, cloud via HTTP)
│   │   ├── local/      # Local tools (file I/O, git, run_command)
│   │   └── shared/     # Shared tools (dns, http, netcheck, traceroute)
│   ├── uid/            # UID generation
│   └── useragent/      # User Agent orchestration + session
├── ui/                 # Web UI (React 18 + Vite + Tailwind + shadcn/ui)
├── docs/               # Architecture and design docs
├── test/               # Integration + E2E test harness
├── config.example.yaml
└── Makefile
```

---

## Development Status

| Phase | Description                                                                                    | Status      |
| ----- | ---------------------------------------------------------------------------------------------- | ----------- |
| 1     | Foundation — two-binary architecture, LLM interface                                           | ✅ Complete |
| 2     | User Agent loop — REPL, tools, session management                                             | ✅ Complete |
| 3     | Core Services + API — SQL/graph store, API handlers                                           | ✅ Complete |
| 4     | Infrastructure Adapters — K8s, Git                                                            | ✅ Complete |
| 5     | Core Agent — background refresh, clarifications, onboarding                                   | ✅ Complete |
| 5.5   | Action Safety Framework — tiers, policy, self-protection                                      | ✅ Complete |
| 6     | Infrastructure Adapters — cloud, observability, alerting, data stores, GitOps, networking     | ✅ Complete |
| 7     | Knowledge Store — curated, synced, derived; semantic search                                   | ✅ Complete |
| 8     | Documentation Co-Pilot — draft generation, drift detection, proposal flow                     | ✅ Complete |
| 9     | Emergency Controls + MCP Server + RBAC                                                        | ✅ Complete |
| 10    | Code Review Integration — GitHub/GitLab PR adapters, review agent                            | ✅ Complete |
| 11    | Slack Bot                                                                                      | ✅ Complete |
| 12    | Web UI — React + graph explorer, dashboard, admin, chat                                       | ✅ Complete |

---

## Documentation

- [docs/joe-architecture.md](docs/joe-architecture.md) — Full architecture and component diagrams
- [docs/joe-dataflow.md](docs/joe-dataflow.md) — Data flow and `.joe/` file processing
- [docs/security-in-layers.md](docs/security-in-layers.md) — Action Safety Framework and Panic Mode spec
- [docs/JOE_SECURITY.md](docs/JOE_SECURITY.md) — Security architecture overview
- [docs/JOE_RBAC_IMPLEMENTATION.md](docs/JOE_RBAC_IMPLEMENTATION.md) — RBAC spec
- [docs/web-ui.md](docs/web-ui.md) — Web UI specification
- [docs/observability.md](docs/observability.md) — OpenTelemetry instrumentation
- [docs/testing-strategy.md](docs/testing-strategy.md) — Testing strategy (unit, integration, E2E)

---

## License

TBD
