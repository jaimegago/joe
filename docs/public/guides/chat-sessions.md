---
title: Chat sessions
weight: 20
description: Create a session, who can read it, and link one to an incident.
---

# Chat sessions

A **chat session** is a first-class, durable object: it has an owner (its creator), it
persists the conversation and the actions Joe took, and it can be linked to an incident.
This page is procedural. For the model behind it — team-public like a pull request,
creator-mutates, admin-governs — read
[Chat sessions](../../concepts/chat-sessions/) in Concepts.

## Create a session

In the web UI, the **Chat** surface is where sessions live; starting a conversation
works against a session you own.

Programmatically, create one with an empty `POST`:

```sh
curl -s http://localhost:7777/api/v1/sessions \
  -H "Authorization: Bearer $JOE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{}'
```

The owner is taken from your credential — the session's creator is whoever made the call.
The response carries the new session `id`. Read a session's transcript with
`GET /api/v1/sessions/{id}/messages`.

## Sharing: there is nothing to share

Sessions are **team-public by default**, the same way a pull request is visible to the
whole team. Any authenticated principal can read any session — there is no per-session
visibility toggle and no "share" action to perform. Creating a session already makes it
readable team-wide.

What ownership protects is **mutation**, not visibility. Only the creator (or an admin
acting through the admin surface) may rename, delete, restore, or link a session. A
non-owner who attempts to mutate a session they do not own is refused as if it were not
theirs.

List sessions team-wide, or filter to just your own:

```sh
# Every session (team-wide):
curl -s http://localhost:7777/api/v1/sessions \
  -H "Authorization: Bearer $JOE_API_KEY"

# Only the ones you created:
curl -s "http://localhost:7777/api/v1/sessions?mine=true" \
  -H "Authorization: Bearer $JOE_API_KEY"
```

Owner-only mutations follow the usual REST shapes: rename with
`PATCH /api/v1/sessions/{id}`, soft-delete to trash with `DELETE /api/v1/sessions/{id}`,
and restore with `POST /api/v1/sessions/{id}/restore`.

## Link a session to an incident

A session can be attached to the **active** incident as a participant. As the session's
owner:

```sh
curl -s http://localhost:7777/api/v1/sessions/<id>/link-incident \
  -H "Authorization: Bearer $JOE_API_KEY" \
  -X POST
```

This sets the session's incident pointer to the current incident master. It does not
change the session's type — it stays an ordinary conversation that now points at the
incident. There must be an incident active for the link to take; linking when none is
declared is refused.

Promoting a session to *become* the incident master is a different, more privileged act —
that is the **declare** step, covered in [The incident regime](../incidents/).

## Where to go next

- Why sessions are owned and team-readable → [Chat sessions](../../concepts/chat-sessions/)
- Declaring and resolving incidents → [The incident regime](../incidents/)
