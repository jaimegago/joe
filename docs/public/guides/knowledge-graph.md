---
title: The knowledge graph
weight: 70
description: Query the graph and understand how components and relationships populate it.
---

# The knowledge graph

Joe keeps a graph of your infrastructure — services, clusters, the systems that hold
their metrics and logs, and the relationships between them. This page covers how to query
it and how it gets populated. For the model behind it, read
[The knowledge graph](../concepts/knowledge-graph/) in Concepts.

## Query the graph

Over HTTP:

- `GET /api/v1/graph/query?q=<text>` — search for nodes matching a query string.
- `GET /api/v1/graph/related?nodeID=<id>&depth=<1-3>` — find nodes related to one node.
- `GET /api/v1/graph/summary` — node and edge counts.

The web UI renders the same graph (`GET /api/v1/graph` returns the full node-and-edge
set, with `GET /api/v1/graph/node/{id}` and `…/related` for a single node), and the
[MCP tools](mcp/) `joe_graph_query` and `joe_graph_related` expose the same two queries to
an editor.

```sh
curl -s "http://localhost:7777/api/v1/graph/query?q=payment-svc" \
  -H "Authorization: Bearer $JOE_API_KEY"
```

## How the graph populates

You do not hand-author the graph node by node. It fills from two places:

- **Components you register.** Registering a managed system (the
  register → promote flow in [Components](../components/)) puts it under Joe's
  management. Registration alone is inert; once a component is promoted and armed, Joe's
  autonomous refresh cycle connects to it.
- **Autonomous discovery.** The refresh cycle reads each armed component, discovers the
  resources and relationships it exposes, and upserts the corresponding nodes and edges.
  Re-running refresh reconciles the graph against what the systems currently report —
  nodes and edges are upserted, not duplicated.

## Observability backends are resolved through graph edges

This is the payoff of the graph. When you ask an observability question about a service,
Joe does not need you to name the backend — it walks the graph edges from the service to
the system that holds that signal. The edges are typed by signal: `metrics_in`,
`logs_in`, `traces_in`, and `alerts_in` (with `paged_via` as a secondary alerting edge).

The category endpoints — `POST /api/v1/observe/{metrics,logs,traces,alerts,k8s}` — each
resolve their backend by following the matching edge from the named service, then
translate and run the query against whatever system that edge points to. The MCP tools
`joe_metrics`, `joe_logs`, `joe_traces`, `joe_alerts`, and `joe_k8s` are the same surface
from an editor. Connect the backing systems and wire their edges through
[Components](../components/) and these resolve automatically.

## Curated versus derived knowledge

The knowledge Joe stores is not all the same trust level, and the distinction matters
when you read it back:

- **Curated** knowledge is human-owned and highest-trust. It is authored through the
  knowledge API (`POST /api/v1/knowledge/entries`) with `"tier": "curated"` set
  explicitly, and is treated as authoritative. Ask for it by name: because curated
  entries can never be updated or deleted afterwards, a create that does not name a tier
  is filed as derived rather than committing you to something irreversible.
- **Synced** knowledge is fetched from an external source you have connected (for
  example a wiki), and carries that source's provenance.
- **Derived** knowledge is the lower-trust, **mutable** tier — it carries a confidence
  score, and unlike curated it can be updated and deleted through the API. It is the tier
  an entry lands in when you create one without naming a tier, and the tier reserved for
  machine-extracted knowledge. Joe does **not** write it for you today: no
  knowledge-writing tool is registered on any of Joe's agent surfaces, so every entry in
  the store is one you or a connected source put there.

Search and listing let you filter by this distinction, so you can ask for only curated
entries when you need ground truth. Use the curated/synced/derived wording rather than
bare tier numbers — the numbers collide with Joe's retired safety-tier vocabulary and
will mislead.

## Where to go next

- The model behind the graph and its knowledge tiers → [The knowledge graph](../concepts/knowledge-graph/)
- Connecting the systems that populate it → [Components](../components/)
- Promotion: why registering a component is inert until armed → [The component lifecycle](../concepts/component-lifecycle/)
