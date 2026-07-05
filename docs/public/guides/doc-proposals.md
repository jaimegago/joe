---
title: Documentation proposals
weight: 80
description: Draft, approve, and publish a doc update — and why approval and publication are separate.
---

# Documentation proposals

Joe can draft documentation updates, hold them for human review, and publish the approved
ones to their target system. The important thing to get right is that **approval and
publication are two separate steps**: approving a proposal marks it ready, but it does
**not** publish it. Publication is a deliberate, governed second action.

## Detect drift and draft a proposal

A proposal usually starts from a conversation with Joe. Ask it to check for documentation
drift, and it inspects the knowledge store against your connected sources; ask it to draft
an update, and it creates a **pending** proposal for you to review. The draft is a
read-only act — it writes a proposal into Joe's own store, it does not touch the target
system.

You can also create a proposal directly over HTTP:

```sh
curl -s http://localhost:7777/api/v1/knowledge/proposals \
  -H "Authorization: Bearer $JOE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{ "topic": "...", "target_type": "confluence", "target_id": "..." }'
```

A new proposal lands in the **pending** state. List and inspect proposals with
`GET /api/v1/knowledge/proposals` and `GET /api/v1/knowledge/proposals/{id}`.

## Step 1 — Approve (marks ready, does not publish)

Approval is a human decision recorded over HTTP:

```sh
curl -s http://localhost:7777/api/v1/knowledge/proposals/<id>/approve \
  -H "Authorization: Bearer $JOE_API_KEY" \
  -X POST
```

This moves the proposal from **pending** to **approved** and stamps who approved it.
Nothing is written to the target system yet — approval only makes the proposal *eligible*
to be published. (To discard one instead, `POST /api/v1/knowledge/proposals/{id}/reject`.)

## Step 2 — Publish (the separate, governed step)

Publication is **not** an HTTP call you make directly — there is no publish REST route.
Publishing an approved proposal happens through Joe's **agentic tool path**: you ask Joe
to publish the approved proposal, and Joe runs its publish tool, which writes the content
out to the target system (Confluence, Notion, or Git).

Because publishing **writes to an external system**, it is a *mutate* action, and that has
two consequences you must plan for:

- **The proposal must already be approved.** Joe refuses to publish a proposal that is
  still pending; approve it first.
- **The deployment must not be read-only.** A mutate is denied whenever Joe's
  [write floor](../concepts/observation-mode-and-the-write-floor/) is up — which it is in
  **observation mode** and in **safe mode** after a panic. An observation-mode install
  will *not* publish, no matter how the proposal was approved. Publication succeeds only
  on a deployment whose write floor is down — the governed full-capabilities mode that
  allows this is **forthcoming, not yet runnable** (`JOE_MODE=full` is refused at boot
  pending implementation), so today publication is denied in Joe's shipped read-only
  posture.

On a successful publish the proposal moves to **published**. If the write floor blocks it,
the proposal stays **approved** and you will see the read-only denial. Lowering the floor
requires the governed full-capabilities mode, which is **forthcoming** (`JOE_MODE=full` is
refused at boot pending implementation); once it lands you will be able to run with the
floor down — subject to clearing any panic and restarting, see
[Operations](../operations/) — and ask Joe to publish again.

## The lifecycle at a glance

1. **Draft** → proposal is *pending* (a read; touches nothing external).
2. **Approve** → proposal is *approved* (a human decision; still nothing external).
3. **Publish** → ask Joe; the publish tool writes to the target and the proposal becomes
   *published* — only if it was approved and the write floor is down.

## Where to go next

- Why a write is refused in observation or safe mode → [Observation mode and the write floor](../concepts/observation-mode-and-the-write-floor/)
- Curated versus derived knowledge → [The knowledge graph](knowledge-graph/)
- The governed full-capabilities mode that will let publication succeed (forthcoming) → [Operations](../operations/)
