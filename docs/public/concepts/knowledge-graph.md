---
title: The knowledge graph
weight: 90
description: Components, relationships, and how observability backends are resolved.
---

# The knowledge graph

At Joe's center is a **graph** of your infrastructure. Its anchor nodes are the
[components](../component-lifecycle/) you register — the external systems Joe manages.
Around each promoted component, Joe's background refresh discovers the resources that
system exposes and adds them as nodes in their own right: for a Kubernetes cluster, for
example, the workloads, services, and namespaces inside it; for a cloud account, the
networks and instances under it. The **edges** connect these nodes into a model of how
your environment fits together. The graph is persisted locally (SQLite-backed), and it is
what gives Joe a model of your environment to reason over rather than treating every
question as a blank slate.

## Anchors and discovered resources

A component on its own is just a registered system — an anchor with nothing hanging off
it yet. As Joe refreshes an armed component, it reads what that system currently exposes
and records those resources as nodes beneath the component, reconciling them on each pass
so the graph tracks reality rather than drifting. A discovered resource is a node in its
own right, distinct from the component that anchors it.

The value is in the **edges**: this service runs in that cluster; this cluster's metrics
live in that Prometheus; this component's alerts are handled by that alertmanager. Joe
builds these relationships as it discovers and refreshes your infrastructure (see [The
agent loop and autonomy levels](../agent-loop-and-autonomy/) for how the background loop
keeps the graph current), and they are what let Joe answer a question about one system by
reaching the systems related to it.

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

Separate from the graph, Joe keeps a **knowledge store** of runbooks, docs, and facts,
and not all of it carries the same authority. **Curated** knowledge is established by a
human and treated as ground truth. **Derived** is the store's lower-trust, **mutable**
tier, the one an entry lands in by default and the one reserved for machine-extracted
knowledge. Curated entries are **immutable once written** — nothing can overwrite what a
human deliberately asserted, and that immutability is enforced in the store rather than
left to convention.

Today the derived tier is written by people, not by Joe: **no knowledge-writing tool is
registered on any of Joe's agent surfaces**, so Joe does not extract knowledge from your
sessions on its own. The tier exists, and the immutability boundary around curated is real
and enforced now; the machinery that would let Joe populate derived by itself is
deliberately not wired. This authority distinction lives in the knowledge store; it is not
a property of the infrastructure graph above.

For connecting the systems that become components and their backends, see
[Components](../../components/); for working with the graph through Joe, see
[Guides](../../guides/).
