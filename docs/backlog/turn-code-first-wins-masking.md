Turn-level error_code is first-wins, so an early scope refusal hides a later write denial
Status: open
Priority: later

A turn can carry more than one refused tool call. Only one of them reaches the
chat as a notice, and which one is decided by ordering rather than by severity:
[`firstWriteFailureCode`](../../internal/api/tasks.go) returns the **first**
non-empty code across the turn's steps and tool results, and
`finalizeTaskResponse` puts that on the response's turn-level `error_code`.

The chat renders exactly one amber notice per turn, from that one code, via
`writeFailureMessage` in [`ui/src/hooks/useChat.ts`](../../ui/src/hooks/useChat.ts).
So in a turn where a read is refused by the session's zone or namespace scope and a
later write is refused by the write floor, the user is told about the scope
refusal and is never told the write was blocked.

## Why this is worth recording now

The behaviour is not new — first-wins has been the rule since the typed codes
shipped, and any two denials in one turn have always had it. What changed on
2026-08-19 is which combinations are reachable.

Before `scope_denial` existed, the executor's own zone and namespace scope checks
classified to `""` and were skipped by `firstWriteFailureCode` entirely. A
scope-refused read could not occupy the slot. Now it can, and it is *typically*
first: the scope check runs ahead of the action class, so it refuses reads, and a
read usually precedes a write in an investigate-then-act turn.

The combination that makes this concrete is the ordinary read-only evaluation
posture. A zone-scoped session in observation mode: the agent reads a component
outside its zones (`scope_denial`), then attempts a mutate the write floor denies
(`observation`). The turn now reports the scope refusal, and the sentence
explaining that Joe is in observation mode and will not make changes never renders.

Both sentences are true. The one that renders is the less useful one, because the
scope refusal is usually a configuration the operator chose and the write denial is
usually the thing they wanted to know about.

## What was already done, and what is left

The *silent* half of this was closed when `scope_denial` shipped:
`writeFailureMessage` gained a branch for it, plus a test asserting that every code
the backend can put on the turn-level field has a message. Without that, the
combination above would have rendered **nothing at all** — a regression rather than
a mis-ranking. That part is fixed.

What is left is the ranking. Options, cheapest first:

- **Rank rather than take the first.** A fixed precedence over the five codes, so a
  write denial outranks a scope refusal. Cheap, and it makes the choice explicit
  instead of incidental — but it still shows one notice and still hides one fact.
- **Carry all distinct codes for the turn** and let the view render one notice per
  distinct code. Honest, and the view already renders a list-shaped block.
  Changes the response shape, so the adapter contract wants checking first —
  though only joe's own UI reads the turn-level field today. OASIS reads the
  per-action codes, which are unaffected either way.
- **Leave it.** Defensible: two denials of different kinds in one turn is not the
  common case, and the per-step codes are all present in the response for anyone
  reading the API rather than the chat.

Filed by `joe-pm` `threads/infra-failure-detector-class.md`, which introduced
`scope_denial` and made the combination reachable, rather than absorbed into that
thread's diff.
