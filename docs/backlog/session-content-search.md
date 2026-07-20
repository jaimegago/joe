# Backlog — conversational session content-search

Status: **deferred — post-launch shelf.** Distinct from the P2 title filter on
Priority: later
the sessions list (`docs/reference/DESIGN-SESSIONS-VIEW.md` §2.1). Not launch-blocking.

## What this is

A capability for Joe to find a session by its **message content**, invoked
conversationally in a chat — "find the session where we discussed the replica
lag fix", "which session had the Terraform drift conversation". The match is
over the **transcript** (what was said), not the session's title or summary.

## How it differs from the P2 list filter

The P2 keyword-filter on the Sessions page is a **title-only** substring match
over the list projection, applied client-side to the rows already on screen. It
deliberately does **not** search message bodies: the list projection
(`webUISession`, `internal/api/webui.go`) carries no message text — only
`title`, `summary`, counts, and the incident fields. Title-filter and
content-search are different features with different indexes and different entry
points; P2 explicitly scoped to the former.

## Shape of the capability

- A **retrieval tool/skill** Joe exposes (not a list-UI control), invoked from a
  Joe chat conversationally.
- Searches `chat_messages` — **full-text over transcripts**. This needs a
  **content index** the list projection does not carry (e.g. an FTS index over
  message bodies, or an embedding index for semantic recall). Building/scoping
  that index is the bulk of the work.
- Governed through the **session-authz seam as a READ**: results are subject to
  the same read model as any session access (team-public read today, §12.7 of
  `docs/reference/DESIGN-CHAT-SESSIONS.md`), so content-search never surfaces a transcript
  the caller could not already open.

## Why deferred

It is a net-new index + a new tool surface + an authz pass — unrelated to the
list-filter UI shipped in P2, and not needed for launch. Shelved until there is
a concrete user demand for transcript recall.
