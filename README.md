<p align="center">
  <a href="https://joeagent.dev">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="docs/assets/joe-lockup-dark.svg">
      <img src="docs/assets/joe-lockup.svg" alt="Joe" width="320">
    </picture>
  </a>
</p>

<p align="center">
  <strong>Joe</strong> (Joe Operates Everything): a self-hosted, open-source AI agent for infrastructure operations. Single Go binary with embedded web UI (<code>go:embed</code>), MCP server, SQLite persistence. Every action — from every surface — passes one governed executor.
</p>

**Why Joe exists** — read the launch essay: [Announcing Joe, and why I was the safety layer](https://jaimegago.dev/writing/announcing-joe-safety-layer/)

---

## What Joe does

- **Understand live infrastructure.** Ask Joe about your systems in natural language. Joe queries your clusters, cloud accounts, observability backends, and datastores through registered components, and builds a graph of how they relate.
- **Make changes with full context and governance.** Joe reasons over live state and its knowledge store before acting, and every action it takes is classified and gated. Nothing runs outside the governed executor.
- **Ground your coding agent.** Run `joe mcp` and Joe exposes its governed read tools over the Model Context Protocol, so Claude Code, Cursor, or any MCP client can pull real infrastructure context into a coding session.

---

## Governed by construction

Joe's safety posture is resolved at boot and enforced on one path.

- **Observation is the default.** An unconfigured Joe (`JOE_MODE` unset) boots with the write floor up and runs read-only. `JOE_MODE=full` is refused at boot as not yet implemented; any unrecognized value is refused fail-closed (`internal/env`).
- **The write floor is boot-resolved and runtime-immutable.** Once Joe is up, nothing in the process lowers the floor. Recovery is a restart, never a live down-transition.
- **Actions are classified Read or Mutate.** Every tool carries a binary classification authored in code. Reads pass the floor unconditionally; Mutates are denied by default. An unrecognized tool name is treated as a Mutate and denied — fail-closed. There are no other action classes.
- **Denial precedence is fixed:** write floor, then the incident gate, then RBAC.
- **One governance seam.** The web UI, the MCP server, and the REST API all converge on the same governed executor — the policy is identical no matter which surface issued the request.
- **No shell-out.** Joe's shared diagnostic tools are Go-native and depend on no external CLI.
- **MCP is server-only.** Joe runs as an MCP server but is deliberately not an MCP client: it does not consume external MCP servers' tools, because that protocol carries no enforceable action classification.
- **Kill switch.** `joe panic`, `SIGUSR1` to the server process, or `POST /api/v1/panic` puts Joe into safe mode, which restricts it to reads. `joe unlock --reason "..."` clears the panic state; the change takes effect on restart.

See [docs/reference/security-in-layers.md](docs/reference/security-in-layers.md) for the full model.

---

## Quick start

### Prerequisites

- Go 1.25 or later
- Node.js 20+ and npm (to build the web UI)
- One LLM API key — Anthropic **or** Google

### Build

```bash
git clone https://github.com/jaimegago/joe.git
cd joe
make build
```

`make build` builds the production web UI with Vite, stages it into the embed directory, and compiles the `joe` binary with the UI embedded and build-truth injected via `ldflags -X`. A plain `go build ./...` compiles too, but produces a binary with the unset `dev` build defaults and without the packaged UI.

### Configure the LLM

Set exactly one provider key in the environment that runs `joe`:

```bash
export ANTHROPIC_API_KEY="..."   # Claude
# — or —
export GEMINI_API_KEY="..."      # Gemini (GOOGLE_API_KEY also works)
```

If both keys are present, Joe defaults to Claude. Override the choice with `JOE_LLM_PROVIDER=claude|gemini` and, optionally, `JOE_LLM_MODEL=<model-name>`. If no supported key is set, Joe exits at boot with an actionable message.

See [docs/configuration.md](docs/configuration.md) for the full configuration reference.

### Run

```bash
./joe
```

Joe starts the daemon and logs the model it selected.

---

## Interfaces

Bare `joe` (or `joe --config ...`) starts the server. Subcommands dispatch ahead of it:

```
joe mcp        Run Joe as an MCP stdio server
joe slack      Run Joe as a Slack bot (Socket Mode)
joe skills     Manage Agent Skills
joe incident   Declare, resolve, or inspect the incident regime
joe admin      Bootstrap the first admin on a database that has none
joe panic      Trigger an emergency shutdown
joe unlock     Clear the panic state (takes effect on restart)
```

- **Web UI** — the embedded browser app for chat, the infrastructure graph, dashboards, and the admin panel.
- **`joe mcp`** — an MCP stdio server exposing Joe's governed read tools to MCP clients; reads `JOE_SERVER` and `JOE_API_KEY`.
- **REST API** — the HTTP surface on `:7777` that every client, including the web UI, calls.
- **Slack** — `joe slack` connects over Socket Mode (no public URL required), reading `SLACK_BOT_TOKEN` and `SLACK_APP_TOKEN`.

---

## Access control

**Write RBAC.** Components are assigned to zones, and principals are granted access to zones. A mutate is allowed only when the caller's grants permit it *and* the write floor is down — the write floor and write-RBAC govern mutates independently of any read setting.

**Read posture.** A single install-wide scalar selects the human-facing read decision. The launch default is `team_flat`: every authenticated principal may read every component. The opt-in `zoned` posture makes reads grant-gated instead. Flipping the posture is an admin-gated, audited operator action.

**Admin surface.** Every handler under `/api/v1/admin/` is admin-gated and writes an audit row — both properties are pinned by structural guard tests, so an admin endpoint added without a gate or without an audit write fails the build.

See [docs/reference/security-in-layers.md](docs/reference/security-in-layers.md) for the security model.

---

## Skills

Joe loads Agent Skills — portable folders that encode how a senior engineer frames a class of situation. Installed skills follow a quarantine-then-approve lifecycle: a newly installed skill lands quarantined and must be explicitly approved before Joe will load it. Skills do not bypass governance — a skill's suggested action still passes the same classified executor as any other tool.

```
joe skills install <repo-url> [--ref <branch|tag>] [--subdir <path>]
joe skills list
joe skills remove <skill-name> [--force]
joe skills update [<skill-name>]
joe skills approve <skill-name>
joe skills reject <skill-name>
joe skills reload
```

See [docs/reference/joe-skills-design.md](docs/reference/joe-skills-design.md) for the design.

---

## Components

Joe connects to your infrastructure through **components**. A component reaches its live, credentialed state in two governed steps.

**Register** a component — credential-less by construction:

```bash
curl -X POST http://localhost:7777/api/v1/components \
  -H "Authorization: Bearer $JOE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "id": "prod-cluster",
    "type": "kubernetes",
    "name": "Production Cluster"
  }'
```

Registration lands the component inert: it has no credential and cannot authenticate.

**Promote** it — the single governed transition that arms a component with a credential reference — via the admin-gated `POST /api/v1/components/{id}/promote`. Promotion writes a credential *reference*, never an inline secret. For a Kubernetes component the credential model is the API server URL (`api_server`), an inline CA bundle (`ca_data`), and an auth method (`auth_method`) — there is no kubeconfig path anywhere.

For the set of supported adapters, see [`internal/adapters/`](internal/adapters/). For setup walkthroughs, see [docs/integrations.md](docs/integrations.md).

---

## Knowledge store

Joe's knowledge store holds three kinds of entry:

- **Curated** — human-owned knowledge. Immutable through the API.
- **Synced** — entries pulled from an external source.
- **Derived** — knowledge Joe extracts from its own operation. Mutable.

The curated/synced/derived distinction is a property of the knowledge store, separate from the infrastructure graph. See [docs/configuration.md](docs/configuration.md).

---

## Development and testing

```bash
go build ./...   # compile
go test ./...    # tests
go vet ./...     # vet
gofmt -s -w .    # format
```

`make build` produces the release-shaped binary (embedded UI + injected build truth). Integration tests run under a build tag: `go test -tags=integration ./...`.

See [CLAUDE.md](CLAUDE.md) for repo conventions and [test/README.md](test/README.md) for the test tree layout.

---

## Contributing

Contributions are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md) for the development setup, testing expectations, and the safety invariants a PR must not violate.

---

## Documentation

| Doc | What it covers |
| --- | --- |
| [docs/configuration.md](docs/configuration.md) | Config file reference and environment variables |
| [docs/operations.md](docs/operations.md) | Running Joe, safety controls, and RBAC administration |
| [docs/integrations.md](docs/integrations.md) | Registering components, the MCP server, and the Slack bot |
| [docs/web-ui.md](docs/web-ui.md) | Web UI specification |
| [docs/reference/joe-architecture.md](docs/reference/joe-architecture.md) | Full architecture and component diagrams |
| [docs/reference/security-in-layers.md](docs/reference/security-in-layers.md) | Security authority: action model, write floor, RBAC, read posture, panic mode |
| [docs/reference/joe-skills-design.md](docs/reference/joe-skills-design.md) | Skills system design |
| [docs/reference/observability.md](docs/reference/observability.md) | OpenTelemetry instrumentation |

Published documentation lives at [joeagent.dev](https://joeagent.dev).

---

## License

Joe is licensed under the Apache License 2.0 — see [LICENSE](LICENSE). It is distributed as published release binaries — archives plus a checksums file, attached to a GitHub Release — and as source you can build yourself. There is no signing, installer, or package-manager tooling.
