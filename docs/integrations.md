# Integrations

Joe ships two front-ends alongside the Web UI: an MCP server for editors and a
Slack bot. Both are subcommands of the `joe` binary and connect to a running
daemon over HTTP.

## MCP server (Claude Code / Cursor / Codex)

`joe mcp` exposes 8 Joe tools over the Model Context Protocol, letting your editor
query live infrastructure directly. It reads `JOE_SERVER` and `JOE_API_KEY` from
its environment.

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
        "JOE_API_KEY": "<service-account-key>"
      }
    }
  }
}
```

### Available MCP tools

All observability tools accept natural-language questions — Joe resolves the
backend from the graph automatically.

| Tool                   | Description                                                          |
| ---------------------- | ------------------------------------------------------------------- |
| `joe_graph_query`      | Search infrastructure graph nodes                                   |
| `joe_graph_related`    | Find nodes related to a given node                                  |
| `joe_k8s`              | Answer Kubernetes questions (pods, deployments, logs) for a service |
| `joe_metrics`          | Query metrics — Joe resolves Prometheus/Datadog/etc. from the graph |
| `joe_logs`             | Search logs — Joe resolves Loki/Splunk/etc. from the graph          |
| `joe_traces`           | Find traces — Joe resolves Tempo/Jaeger from the graph              |
| `joe_alerts`           | List active alerts from Alertmanager/PagerDuty                      |
| `joe_knowledge_search` | Semantic search over runbooks and docs                              |

## Slack bot

`joe slack` connects Joe to Slack via Socket Mode — no public URL required.

### Setup

```bash
export SLACK_BOT_TOKEN="xoxb-..."    # Bot User OAuth token
export SLACK_APP_TOKEN="xapp-..."    # App-Level token (connections:write scope)
export JOE_SERVER="http://localhost:7777"
export JOE_API_KEY="<service-account-key>"  # optional

joe slack
```

### Slash commands

| Command             | Description                                                     |
| ------------------- | --------------------------------------------------------------- |
| `/joe ask <query>`  | Query the infrastructure graph and knowledge store              |
| `/joe status`       | Show graph summary                                              |
| `/joe help`         | Show available commands                                         |

Unrecognized subcommands are treated as queries (same as `/joe ask <text>`).
