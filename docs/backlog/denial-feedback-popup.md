# Denial-feedback pop-up: a reactive notification when a user action is refused

Status: deferred — design-approved in spirit, unimplemented
Entry created: 2026-06-08. This is an implementation backlog item (a UI
enhancement), not a new decision — it does not add a DECISIONS.md entry.
Relates to: D-0022 (denial precedence floor > incident > RBAC), D-0019 (trust
model / postures), D-0018 (the write floor).
Independent of: the trust-model UI track (posture endpoint + observation banner +
system-prompt posture line), which deliberately excludes this surface.

## Subject

A denial-feedback pop-up / notification, surfaced when a **user-initiated** action
is refused by any of the three denial layers in the D-0022 precedence chain. This
is distinct in kind from the at-rest persistent posture banner: the banner reports
system state *before any action* (the resting read-only posture); this pop-up is a
*reaction to a specific tool-call outcome* — "the action you just attempted was
refused, and here is why."

## Problem statement

Today the reason a write was denied reaches the UI only as a **per-message inline
rendering** inside the chat turn. There is no distinct pop-up/toast/modal surface
that announces a refusal as an event.

### Verified reactive path (re-derive exact file:line from live code before acting; verified 2026-06-08 against the live tree)

1. **A denied write returns a typed error.** The executor raises a typed write-floor
   error for a Mutate while the floor is up —
   `internal/tools/executor.go:215-216`:
   `if e.floor.Up() && classification.Class == safety.ActionMutate { err := &safety.WriteFloorError{Reason: e.floor.Reason()} ... }`.
   CONFIRMED.

2. **`classifyWriteFailure` maps typed errors to stable codes.** It lives in the
   api layer (not agentloop) — `internal/api/writefailure.go:49-73` — and is
   injected into the loop via `agentloop.WithToolErrorClassifier`, running on the
   TYPED error before it is stringified onto the wire. CONFIRMED.

3. **`useChat.ts` renders those codes as inline per-message text.** The hook stores
   the code on the turn (`ui/src/hooks/useChat.ts:274`,
   `writeFailureCode: final.error_code`) and dispatches it to a sentence via the
   pure exported `writeFailureMessage` (`useChat.ts:64-79`). The render site is
   `ui/src/components/chat/AssistantTurnView.tsx:96-103` — an inline amber `<div>`
   notice within the assistant turn (`data-testid="write-failure-notice"`), shown
   because a denied write does NOT fail the turn (the LLM still answers). CONFIRMED.

**Codes branched on** (defined `internal/api/constants.go:26-29`, dispatched in
`useChat.ts:64-79`):

- `zone_denial` — "Access to this zone has not been granted to you. Ask your administrator."
- `incident_mode` — "System is in incident mode. Writes are temporarily blocked."
- `safe_mode` — "System is in safe mode. Only read-only operations are permitted — run `joe unlock` to resume writes."
- `observation` — "Joe is in observation mode — it can read and explain but will not make changes. This is the intended read-only posture."
- `internal_error` — "Unexpected error. Please try again."

**Surface today: inline-message-only.** The reason is rendered as a per-turn inline
notice inside the chat transcript (`AssistantTurnView.tsx:96-103`) and, on a failed
turn or a pre-stream 403, as the turn's `errorMessage` (`useChat.ts:261-285`).
There is NO toast / modal / pop-up surface for denials today. (A `Dialog`
primitive exists at `ui/src/components/ui/dialog.tsx` and is used for admin forms
and confirmations, but nothing wires denial feedback to it.)

## The gap

There is no distinct pop-up/notification that announces, as an event, that the
action the user just attempted was refused and why. The current inline notice is
easy to miss — it renders below the (still-produced) answer, inside the scrolling
transcript. This is different in kind from the persistent posture banner, which
reports system state at rest, before any action.

## Scope-defining design note (why this is parked as its own item)

The load-bearing reason this is its own backlog item rather than a quick add-on:
the pop-up must cover **all three denial layers** in the D-0022 precedence chain —
write floor, incident/captain gate, and RBAC — not just the floor reasons. Building
it floor-only would mean rebuilding it when incident and RBAC denials want the same
treatment.

### Verified: all three layers already produce distinguishable typed errors AND already surface to the frontend with distinct codes (re-derive file:line before acting; verified 2026-06-08)

The precedence chain is `*safety.WriteFloorError` > `*captaingate.GateRefusalError`
> `access.ErrPermissionDenied`, enforced by check order upstream (floor first in
`internal/tools/executor.go:215`; the captain gate checks the same floor before its
§C gate; the RBAC accessor denies inside the tool). Exactly one typed error reaches
the classifier per turn.

- **Write floor** — `*safety.WriteFloorError` carries a `Reason`
  (`internal/safety/floor.go`); classified to `observation` or `safe_mode`
  (`writefailure.go:56-65`).
- **Incident / captain gate** — `*captaingate.GateRefusalError`
  (`internal/captaingate/captaingate.go:61-65`, fields `SessionID`, `Tool`,
  `CaptainSessionID`); classified to `incident_mode` (`writefailure.go:66-67`).
- **RBAC** — `access.ErrPermissionDenied` (possibly wrapped by the inproc client's
  `mapAccessError`); classified to `zone_denial` (`writefailure.go:68-69`).

**Finding — the premise that only the floor path is wired is FALSE.** All three
layers are already classified to distinguishable codes (`writefailure.go:55-72`)
AND all three already map to distinct user-facing messages in the frontend
(`useChat.ts:64-79`). The classifier comment was written for all three from the
start (D-0014 write-failure feedback).

**Consequence for sizing.** The backend write-failure-code vocabulary the pop-up
needs **already exists and already distinguishes all three layers**. This is
therefore predominantly a **frontend presentation** task — introduce a distinct
pop-up/notification surface that consumes the existing `writeFailureCode` — not a
backend wiring task. The incident and RBAC paths do NOT need to be newly surfaced;
they are already on the wire as `incident_mode` / `zone_denial`. (Confirm the
pre-stream 403 path in `useChat.ts:277-286` carries the same codes for denials
raised before streaming begins.)

## Dependency / ordering

- Independent of the trust-model UI track (posture endpoint + observation banner +
  system-prompt posture line), which deliberately excludes this reactive surface.
- No hard ordering. The note that "the incident and RBAC denial paths surfacing
  cleanly to the frontend may be a prerequisite" is, per the verification above,
  **already satisfied** — both already surface with distinguishable codes. No
  backend prerequisite remains for this item on the strength of the current tree.

## Acceptance criteria (record only — do NOT implement)

- A single denial-feedback surface that fires on a refused **user-initiated**
  action (a specific tool-call outcome), as a pop-up/notification — not just the
  existing inline-in-transcript notice.
- Renders a reason **distinguishable per denial layer**: floor
  (`observation` / `safe_mode`), incident gate (`incident_mode`), RBAC
  (`zone_denial`).
- Does NOT duplicate or replace the persistent posture banner (system state at
  rest). The two surfaces coexist: banner = posture before action; pop-up =
  reaction to one refused action.
- **Reuses the existing write-failure-code mapping** (`writeFailureMessage` in
  `ui/src/hooks/useChat.ts` and the backend codes in `internal/api/constants.go`)
  rather than inventing a parallel vocabulary.

## References (link, do not duplicate)

- docs/project/DECISIONS.md **D-0022** — denial precedence (floor > incident > RBAC).
- docs/project/DECISIONS.md **D-0019** — trust model and postures; the posture banner the
  pop-up must not duplicate.
- docs/project/DECISIONS.md **D-0018** — the write floor (the `safe_mode` / `observation`
  reasons).
- docs/project/DECISIONS.md **D-0014** — write-failure feedback (origin of the
  differentiated per-message mapping this item builds on).
