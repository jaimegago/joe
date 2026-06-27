---
title: Chat sessions
weight: 70
description: First-class, team-public sessions and what their security actually protects.
---

# Chat sessions

A **chat session** is a first-class object in Joe, not an ephemeral scrollback. A
session is owned, can be linked to an incident, and persists as a durable record of a
conversation with Joe and the actions it took. Sessions are a core subsystem, and
their access model is deliberately chosen — so it is worth being precise about what
that model protects.

## Team-public, like pull requests

The session model is **team-public**. Every authenticated member of the team can read
every session — the same way a whole team can see all of its pull or merge requests.
A session is not a private channel between one person and Joe; it is shared working
context.

Within that shared visibility, mutation is scoped to the owner:

- **Anyone on the team can read** any session.
- **Only the creator can mutate** their own session.
- **Admins govern** sessions — listing, retention, archival, purge — as an
  administrative function.

## The security spine is integrity and accountability, not secrecy

This is the load-bearing point. The session model's security goal is **integrity and
accountability**, not **secrecy**. What stays gated is *mutation and governance* — who
can change a session, who governs retention — never *who can look*. Privacy between
teammates is an **explicit non-goal**: the model does not try to hide one team
member's sessions from another, and you should not rely on it to.

Framed positively: the question Joe answers about a session is "who actually ran this,
and has the record been tampered with?" — not "can my colleague see this?" The threats
the model defends against are forged ownership and unauthorized mutation, the things
that would undermine the record's trustworthiness. Concealment from teammates is simply
not one of the protected properties.

This is a coherent stance rather than an omission. In a platform-engineering team,
visibility into what Joe has been asked to do — and what it did — is usually the
property you *want*; treating sessions like pull requests makes that visibility the
default and focuses the security effort on keeping the record honest.

## Incident linkage

Because sessions are first-class, a session can be linked to an incident, which is how
the conversation that diagnosed or drove a response becomes part of the
[incident regime](../incident-regime/)'s record. The linkage is an ordinary session
operation, governed like any other session mutation.

For how to share, link, or manage sessions in practice, see [Guides](../../guides/)
and [Operations](../../operations/).
