---
title: Capabilities
weight: 100
description: The full surface of what Joe can do — and why all of it is reading, never changing, your systems.
---

# Capabilities

Joe's job is to understand your infrastructure and answer questions about it. Everything
it does out of the box is a **read**: an operation that inspects the managed system's
state without changing it. That is the spine of this page. Every capability catalogued
below is classified as a Read, which means it passes the [write floor](observation-mode-and-the-write-floor/)
unconditionally — the floor only ever stands between Joe and a *mutation*, and none of
these operations mutate anything.

There are three ways Joe reads: built-in tools that need no setup, the observe surface it
opens against the systems you register, and an MCP bridge that lets a coding agent borrow
Joe's view of your infrastructure. Whether any of it runs at all is still decided by
governance — [identity, RBAC, zones, and the read posture](rbac-zones-and-read-posture/)
govern *who* may read *what*. This page is about the shape of the capability surface;
those pages are about the rules applied to it.

## Built-in tools

The first class needs nothing registered. Joe ships with a set of component-independent
diagnostic tools that run in-process and work from wherever the daemon runs:

- **Network and system diagnostics** — check TCP connectivity to a host and port, scan a
  range of ports, resolve DNS records for a name, trace the network path to a host
  hop-by-hop, and report local system stats (disk, memory, load, OS).
- **An HTTP fetch** — retrieve an HTTP or HTTPS URL the model *already holds* and return
  its status, headers, and a snippet of the body. It is deliberately restricted to the
  safe methods `GET` and `HEAD`; a request using any mutating method is refused. It is a
  diagnostic probe, not a write tool.
- **A web search** — discover URLs on the open web. It returns ranked results as title,
  URL, and snippet only. It never fetches page contents.

Fetch and search are two different tools that **compose rather than overlap**: search
*discovers* a URL Joe did not previously know; fetch *retrieves* a URL Joe already holds.
To read a page a search surfaced, Joe passes that URL to the fetch tool. Keeping them
separate is what keeps each one narrow and read-only.

Web search is inert until an operator points Joe at a search backend; with none
configured the tool is still offered but every call reports that no backend is set. The
keys and backend choice live in [Configuration](../configuration/) — this page does not
repeat them.

## The component-backed observe surface

The second class is where Joe earns its keep. Once you **register and promote** a system
— a Kubernetes cluster, a Git host, an observability backend, a datastore, and so on —
Joe can read that system's live state: list resources, pull logs, query metrics and
traces, read alerts, inspect configuration. These reads feed the model Joe builds of your
estate and are what its answers draw on. Think of it as an **observe surface**: Joe
reading the managed system's live state into its own picture of the world.

This surface is exactly as wide as the set of systems Joe knows how to talk to; that
per-type set, and how each type is registered, live in [Integrations](../integrations/).
Every read here flows through Joe's single governed accessor, so it is attributed to a
principal and checked before it happens — RBAC, zones, and the install-wide read posture
decide which principals may reach which components. See
[RBAC, zones, and the read posture](rbac-zones-and-read-posture/) for how that governance
is applied. What matters for the capability map is the shape: these are all reads, and
none of them change the system being observed.

## MCP: Joe as read-context for a coding agent

The third class turns Joe's reads outward. The `joe mcp` subcommand runs an MCP server
that exposes Joe's tools to any MCP-speaking client. The intended use is a **coding
agent** that connects to Joe and reads Joe's live infrastructure graph and state as
**context while it writes infrastructure-as-code**. The agent asks Joe what actually
exists — what's deployed, how components relate, what the metrics and logs say — and
folds that ground truth into the code it authors.

This is read-*as-context*, not change management. The MCP surface hands the agent Joe's
view of the world; it is not a pipeline through which the agent drives changes into your
systems. Every tool reachable over MCP is one of Joe's reads.

## Running Joe read-only

The whole surface above is reads, but Joe is not only a reader — with governance
satisfied it can also perform authorized mutations. When you want a categorical
guarantee that it will not, Joe can be run under a **hard read-only posture** by starting
the daemon in observation mode (`JOE_MODE=observation`), which raises the write floor for
the life of the process. This is an *available posture*, not the boot default: a normally
started Joe boots writable and lets governance decide each mutation. Observation mode is
the switch you throw when you want the floor up regardless. The
[observation mode and the write floor](observation-mode-and-the-write-floor/) page
explains the mechanism and how to recover from it.

## Where to go next

- Keys and the search backend → [Configuration](../configuration/)
- The per-type set of registerable systems → [Integrations](../integrations/)
- Registering a system, step by step → [Guides](../guides/)
- Why every read and mutation is governed → [The governed-safety invariant](governed-safety/)
