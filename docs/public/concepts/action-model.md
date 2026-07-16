---
title: The action model
weight: 100
description: Every action Joe takes is classified Read or Mutate and governed accordingly — reads pass the write floor, mutates are denied by default.
---

# The action model

Every action Joe can take is classified on a single binary axis — **Read** or
**Mutate** — and that classification decides how the action is governed. A Read
inspects a system's state without changing it; a Mutate changes something. Reads pass
the [write floor](../observation-mode-and-the-write-floor/) unconditionally. Mutates are
denied by default and run only when governance explicitly allows them. That axis is the
spine of this page.

The classification is not inferred at runtime. It is decided by Joe's authors when a
tool is written and pinned in the tool's definition, so whether an action mutates your
systems is a property of Joe's own code, not a guess about intent. A tool name Joe does
not recognize is treated as a **Mutate** and denied — the default is closed, so an
unclassified action can never slip through as a read.

Whether any action runs at all is still decided by governance —
[identity, RBAC, zones, and the read posture](../rbac-zones-and-read-posture/) govern *who*
may do *what*. This page is about the shape of the action surface and how each class is
gated; those pages are about the rules applied to it.

## The read surface

Most of what Joe does is reading. There are three ways it reads.

### Built-in tools

The first class needs nothing registered. Joe ships with a set of component-independent
diagnostic tools that run in-process and work from wherever the daemon runs:

- **Network diagnostics** — probe outward from wherever the daemon runs: check TCP
  connectivity to a host and port, scan a range of ports, resolve DNS records for a name,
  and trace the network path to a host hop-by-hop. These inspect other systems from Joe's
  network vantage point; none of them read or change the host Joe itself runs on.
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
keys and backend choice live in [Configuration](../../configuration/) — this page does not
repeat them.

### The component-backed observe surface

The second class is where Joe earns its keep. Once you **register and promote** a system
— a Kubernetes cluster, a Git host, an observability backend, a datastore, and so on —
Joe can read that system's live state: list resources, pull logs, query metrics and
traces, read alerts, inspect configuration. These reads feed the model Joe builds of your
estate and are what its answers draw on. Think of it as an **observe surface**: Joe
reading the managed system's live state into its own picture of the world.

This surface is exactly as wide as the set of systems Joe knows how to talk to; that
per-type set, and how each type is registered, live in [Components](../../components/). Every
read here flows through Joe's single governed accessor, so it is attributed to a principal
and checked before it happens — RBAC, zones, and the install-wide read posture decide
which principals may reach which components. See
[RBAC, zones, and the read posture](../rbac-zones-and-read-posture/) for how that governance
is applied. What matters for the action map is the shape: these are all reads, and none of
them change the system being observed.

### MCP: Joe as read-context for a coding agent

The third class turns Joe's reads outward. The `joe mcp` subcommand runs an MCP server
that exposes Joe's tools to any MCP-speaking client. The intended use is a **coding
agent** that connects to Joe and reads Joe's live infrastructure graph and state as
**context while it writes infrastructure-as-code**. The agent asks Joe what actually
exists — what's deployed, how components relate, what the metrics and logs say — and
folds that ground truth into the code it authors.

This is read-*as-context*, not change management. The MCP surface hands the agent Joe's
view of the world; it is not a pipeline through which the agent drives changes into your
systems. Every tool reachable over MCP is one of Joe's reads.

## The mutation surface

Joe is not only a reader. A real mutation surface exists, and it is deliberately narrow.
It is registered **only on the human-facing task loop** and scoped to **external
collaboration writes** — never to your running infrastructure. What it comprises is the
ability to comment on a GitHub pull request or a GitLab merge request, and to submit a
GitHub request-changes review. That is the whole of it. But in every
configuration Joe currently boots, none of it runs: the boot-resolved write floor denies
the entire Mutate class, because observation is the boot default and full mode is refused
at boot (the [observation-mode section](#running-joe-read-only) below has the mechanism).
Keeping these real, Mutate-classified tools registered is a deliberate choice — it means
the floor's denial is *demonstrated* against live tools rather than *asserted* against an
empty set, and this surface is the seam that the forthcoming full mode will open.

Two boundaries define it. First, these mutations run **only on the human-facing task
loop** — the loop a person drives. The autonomous core agent registers no mutating tools,
and neither does the MCP server, so nothing Joe does on its own or on an external agent's
behalf can change anything. Second, **no tool that mutates live infrastructure is
registered on any surface** — there is no Kubernetes apply, no cloud write, no database
write, no file write, and no shell. The writes Joe can perform are all about
*collaborating on proposed change*, never *enacting infrastructure change*.

Every mutate is subject to the same executor gate chain, checked in order:

1. **The write floor** — if the floor is up (observation mode, or a sticky safe-mode or
   panic), the entire Mutate class is denied before anything else is considered.
2. **The incident captain gate** — during an incident, mutating actions are restricted to
   the captain's session.
3. **Zone and namespace scope** — the action must fall within a scope the principal is
   granted.
4. **The compiled safety policy** — each mutate class must be enabled in the safety
   policy. The policy is the compiled default (`DefaultPolicy`), which denies every
   managed-system mutation; a class that is not enabled is denied. The per-task
   `safety_tier` can only narrow this further, never widen it.

Authority to mutate is therefore constrained by default, not granted by default. It is
bounded by the compiled default-deny policy, plus the captain-only restriction
that narrows who may act during an incident. There is no per-action confirmation prompt in
the server flow — the authority is the compiled policy and the approval the workflow
already captured, not a live dialog at the moment of the write.

## Running Joe read-only

Joe gives a categorical guarantee that it will not mutate anything by default: it **ships
in observation mode**, a hard read-only posture that raises the write floor for the life
of the process. The floor comes up when `JOE_MODE` is unset and, explicitly, with
`JOE_MODE=observation`. This is the boot default, not an opt-in: a governed
full-capabilities mode that would let the gate chain above decide each mutation is
forthcoming — `JOE_MODE=full` is refused at boot pending implementation — so today Joe
boots read-only regardless. The
[observation mode and the write floor](../observation-mode-and-the-write-floor/) page
explains the mechanism and how to recover from it.

## Where to go next

- Keys and the search backend → [Configuration](../../configuration/)
- The per-type set of registerable systems → [Components](../../components/)
- Registering a system, step by step → [Guides](../../guides/)
- Why every read and mutation is governed → [The governed-safety invariant](../governed-safety/)
