---
title: The MCP server
weight: 50
description: Connect an editor or other MCP client and the tool surface Joe exposes.
---

# The MCP server

`joe mcp` exposes Joe over the **Model Context Protocol** so an editor or other MCP
client — Claude Code, Cursor, and anything else that speaks MCP — can query your live
infrastructure through Joe. This is the page the
[Components](../../components/) section points to.

The MCP server is a **stdio** front-end: the client launches `joe mcp` as a subprocess
and talks to it over standard input/output. It is a thin client of a running daemon — it
does not manage infrastructure itself; it forwards to a `joe` server over HTTP.

## Point it at a daemon

`joe mcp` reads two environment variables:

- `JOE_SERVER` — the daemon base URL. Defaults to `http://localhost:7777`.
- `JOE_API_KEY` — the bearer token used to authenticate to the daemon. Optional, but
  required in practice whenever the daemon enforces auth.

## Connect a client

Configure your MCP client to launch `joe mcp` with those variables set. The shape varies
by client, but it is always "run this command with this environment." For example:

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

## The tool surface

Joe registers seven tools over MCP. They are **category-based**: you describe *what* you
want, and Joe resolves the backend from its [knowledge graph](../knowledge-graph/),
translates the question, executes it, and returns a normalized result.

| Tool | What it does |
| --- | --- |
| `joe_graph_query` | Search the infrastructure graph for nodes matching a query string. |
| `joe_graph_related` | Find nodes related to a given node id within a depth (1–3). |
| `joe_k8s` | Answer a Kubernetes question about a service (pods, deployments, logs). |
| `joe_metrics` | Query metrics for a service; Joe resolves the metrics backend from the graph. |
| `joe_logs` | Search logs for a service; Joe resolves the log backend from the graph. |
| `joe_traces` | Find recent traces for a service; Joe resolves the tracing backend. |
| `joe_alerts` | List active alerts for a service; Joe resolves the alerting backend. |

The observability tools (`joe_metrics`, `joe_logs`, `joe_traces`, `joe_alerts`,
`joe_k8s`) take a `service` plus a natural-language `question`; the graph tools take a
`query`. Every tool reaches the daemon — what it can see is governed by the
daemon's posture and the credential in `JOE_API_KEY`, not by the MCP client.

## Where to go next

- How backends are resolved from the graph → [The knowledge graph](../knowledge-graph/)
- Connecting the managed systems these tools reach → [Components](../../components/)
- Why Joe serves MCP but will not consume it → [Joe and MCP](../../concepts/joe-and-mcp/)
