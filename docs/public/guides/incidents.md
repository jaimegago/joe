---
title: The incident regime
weight: 30
description: Declare and resolve an incident, and what the regime changes while active.
---

# The incident regime

The **incident regime** is Joe's "we are in an incident right now" switch. While it is
active, Joe tightens what mutations it will perform. This page covers declaring and
resolving an incident and what changes in between. For the model behind it, read
[The incident regime](../concepts/incident-regime/) in Concepts.

## Declare an incident

An incident is declared by **promoting an existing session in place** to become the
incident master — so you declare *from* a session, not by minting a new one. You need
the id of the session to promote.

From the CLI:

```sh
joe incident declare --session <session-id> [--kind human] [--reason "..."]
```

- `--session` is required — it names the session to promote.
- `--kind` defaults to `human`. (`joe` exists only as an inert seam the server refuses;
  declare incidents as `human`.)
- `--reason` is optional free text recorded with the declaration.

The CLI calls the daemon's `POST /api/v1/regime/declare`. The web UI offers the same
declaration by promoting a session to an incident
(`POST /api/v1/sessions/{id}/promote-incident`).

Declaring (and resolving) is authorized against the **regime-control** zone, not the
admin capability — granting someone the ability to drive incidents is a separate grant
from making them an admin.

## What the regime changes

While an incident is active, two things change:

- **The captain gate.** The principal who declared the incident is its *captain*. Joe's
  agentic tool path refuses a mutation attempted from any session that is **not** the
  incident master while the regime is active. The gate is deny-only: it narrows *which
  session* may mutate during an incident; it never grants authority a principal does not
  otherwise have, and read operations are unaffected. This gate sits in the middle of
  Joe's denial precedence — below the [write floor](../concepts/observation-mode-and-the-write-floor/),
  above [RBAC](../concepts/rbac-zones-and-read-posture/).
- **The incident banner.** The web UI shows an app-wide banner while the regime is in
  incident mode, naming the captain and warning that writes may be blocked, with a link
  to the incident master session.

## Check status

```sh
joe incident status
```

This reads `GET /api/v1/regime` and reports whether an incident is active and who
declared it.

## Resolve an incident

```sh
joe incident resolve [--reason "..."]
```

This calls `POST /api/v1/regime/resolve` and returns the system to normal: the captain
gate stops refusing non-master mutations and the banner clears.

## Limits to know

- **`joe incident list` is not a working listing.** There is no incident-history
  endpoint; the durable record lives in the append-only audit log. The `list` subcommand
  says so and exits non-zero rather than pretending to enumerate incidents. Query history
  through the audit layer, not this subcommand.
- **There is no dedicated captain UI or captain read surface.** Those are deferred. The
  incident master is an ordinary session reached through the banner link and the normal
  chat surface; do not expect a separate captain console.

## Where to go next

- What "incident regime" means and how it composes with the write floor → [The incident regime](../concepts/incident-regime/)
- Linking other sessions to the active incident → [Chat sessions](chat-sessions/)
