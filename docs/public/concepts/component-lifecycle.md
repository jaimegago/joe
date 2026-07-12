---
title: The component lifecycle
weight: 50
description: The registered-system entity and the single governed path that arms it.
aliases:
  - /docs/concepts/components-and-promotion/
---

# The component lifecycle

A **component** is Joe's name for a registered external system — a Kubernetes
cluster, a Prometheus, a GitHub org, a Grafana, and so on. Components anchor
the [knowledge graph](../knowledge-graph/) — the resources Joe discovers inside them
become further nodes — and are the things RBAC and zones grant access to.
Bringing a system under Joe's management is a two-step lifecycle, and the split between
the two steps is a deliberate security boundary.

## Registration lands a component inert

Registering a component does **not** connect to anything. It creates a record and
nothing more: no adapter is connected, no credential is stored, and no network call is
made to the target system. The component lands **inert**.

Inertness is enforced, not requested. Credential-bearing fields presented at
registration are **rejected**, not quietly stripped — you cannot smuggle a secret in
through the registration door. An inert component is assigned to no zone, so it
resolves to the read-only `unassigned` zone. The reason registration refuses to
connect is that connecting at registration would be exactly the attacker-controllable
network-call-and-credential-dereference moment that the inert landing is designed to
close. A component you just registered can do nothing until a second, separate decision
arms it.

## Promotion is the single governed path to "armed"

Moving a component from inert (read-only) to **armed** is owned by one transition:
**promotion**. Promotion is the only place credentials enter. It is admin-gated and
audited, and it writes a credential **reference** — an environment-variable indirection,
or for Kubernetes the cluster coordinates plus a credential for the chosen authentication
method (a static bearer-token source — an environment variable or the in-cluster token —
or an Entra exchange that mints a token from an Azure app registration's client secret,
referenced by variable name) — never an inline secret value. The secret itself lives where
it always did (your environment, the cluster); Joe stores only the pointer.

Promotion does the bookkeeping of arming a component but does **not** itself reach out
and authenticate: it performs no connect and no probe. Whether the referenced
credential actually works is a separate, explicit check, kept distinct from the act of
recording the reference. Re-promoting an already-armed component overwrites its
reference as another gated, audited event — so even *changing* a credential is a
governed transition, not a delete-and-recreate that slips outside the audit trail.

A component whose type has no wired credential provider can never be armed at all; the
absence of a governed credential path is itself the gate.

## Why the split exists

The two-step lifecycle means there is exactly one moment in a component's life when a
credential is introduced, and that moment is admin-gated, audited, and reference-only.
Registration is safe to do liberally — it commits to nothing. Promotion is the
deliberate, governed act. This is the same shape as the [write
floor](../observation-mode-and-the-write-floor/): the capable, dangerous state is never
the default and is only reached by an explicit, recorded decision.

For *which* component types exist and how to connect each, see
[Components](../../components/); for the configuration of credential references,
see [Configuration](../../configuration/).
