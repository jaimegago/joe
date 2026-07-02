---
title: The knowledge graph
weight: 90
description: Components, relationships, and how observability backends are resolved.
---

# The knowledge graph

At Joe's center is a **graph** of your infrastructure. The nodes are
[components](../component-lifecycle/) — the external systems Joe has registered — and
the edges are the relationships between them. The graph is persisted locally
(SQLite-backed), and it is what gives Joe a model of your environment to reason over
rather than treating every question as a blank slate.

## Components and relationships

A component on its own is just a registered system. The value is in the **edges**: this
service runs in that cluster; this cluster's metrics live in that Prometheus; this
component's alerts are handled by that alertmanager. Joe builds these relationships as
it discovers and refreshes your infrastructure (see [The agent loop and autonomy
levels](../agent-loop-and-autonomy/) for how the background loop keeps the graph current),
and they are what let Joe answer a question about one system by reaching the systems
related to it.

## Observability backends are resolved through edges

The most concrete payoff of the graph is how Joe answers observability questions. When
you ask about a service's metrics, logs, traces, or alerts, Joe does not require you to
name the backend. It **follows the graph edges** from the subject of your question to
the component that actually holds that signal — the metrics backend, the logs backend,
the tracing backend — and queries there.

So "what do the metrics for this service look like?" becomes, internally, "which
component is this service's metrics backend, per the graph?" followed by a query
against that backend. The edge *is* the resolution mechanism. This is why registering
your systems and letting their relationships form is what makes Joe's observability
answers work without per-question backend wiring.

## Curated versus derived knowledge

Not all knowledge in the graph has the same authority. Joe distinguishes **curated**
knowledge — facts a human established and Joe must not overwrite — from **derived**
knowledge that Joe's own reasoning produced. The autonomous agent may create and update
derived knowledge, but it cannot touch curated facts. The separation means Joe's
self-maintenance can keep the graph fresh without eroding the things a human
deliberately asserted.

For connecting the systems that become components and their backends, see
[Components](../../components/); for working with the graph through Joe, see
[Guides](../../guides/).
