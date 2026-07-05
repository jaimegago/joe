# Posture endpoint: a coarse "any write grants exist" signal (full-mode only)

Status: deferred — design-approved in spirit, unimplemented
Entry created: 2026-06-08. This is an implementation backlog item (a posture-read
enhancement), not a new decision — it does not add a DECISIONS.md entry.
Design of record: docs/project/DECISIONS.md D-0019 (the trust model). This entry records
only the implementation track; it does not duplicate D-0019.
Relates to: D-0019 (trust model / postures), D-0018 (the write floor).
Ships with: the full-mode/RBAC track (docs/backlog/full-mode-rbac-track.md) and
its deferred on-demand "evaluate write capability" UI control — NOT before.

## Subject

A coarse, boolean-ish "do any write-granting policy rows exist" signal, to be
added to the posture read endpoint. It was explicitly **deferred** from the
initial posture-endpoint work, which deliberately reports the write-floor
tri-state ONLY.

## Context

A posture read endpoint is being built (or has just been built) that reports the
resolved write-floor tri-state — observation / safe_mode / full — by reading the
boot-resolved floor's reason off the services struct. That endpoint deliberately
reports the floor tri-state ONLY. The grants signal described here is NOT part of
that initial endpoint.

### Verified current state (re-derive exact file:line from live code before acting; corrected 2026-07-05 against the live tree)

1. **The posture read endpoint HAS since landed** — as GET `/api/v1/mutate-status`
   (`internal/api/mutatestatus.go`), which reports the write-floor tri-state this
   item's endpoint was to carry. Its response struct is `mutateStatusResponse`
   with `can_mutate` (mirrors `!floor.Up()`) and a `reason` field sourced from the
   **boot-resolved** floor's reason (`full` / `observation` / `safe_mode`, always
   non-empty). Both fields come from a single read of the boot-resolved floor, so
   they cannot disagree. (Earlier this section claimed no posture endpoint existed
   under `internal/api/` as of 2026-06-08; that is now stale.) The **coarse
   any-write-grants-exist signal described by this item remains unimplemented and
   deferred** — `mutate-status` reports the floor tri-state ONLY and carries no
   grants field. This backlog item still tracks that deferred additive field; the
   implementer would add it to `mutateStatusResponse` (or its successor) per the
   snake_case-tag note below, gated on the floor being down.

2. **The floor tri-state source is confirmed.** The resolved floor is
   `s.services.WriteFloor` — field `WriteFloor safety.WriteFloor` on the Services
   struct (`internal/core/services.go:52`, documented as the boot-resolved,
   runtime-immutable floor at lines 46–52). Its reason is read via
   `WriteFloor.Reason()` (`internal/safety/floor.go:37`), returning a
   `FloorReason` whose three values are `FloorReasonNone` ("" — floor down =
   full mode), `FloorReasonObservation` ("observation"), and `FloorReasonSafeMode`
   ("safe_mode") (`floor.go:11–19`); `WriteFloor.Up()` (`floor.go:34`) reports
   whether the floor is up. CONFIRMED — this is the tri-state the initial endpoint
   reports.

## What the deferred signal is

Per D-0019, when Joe is in **full mode** (floor down, RBAC governing), the UI
needs to distinguish "write grants are configured" from "zero write grants exist"
so a future on-demand control can invite the operator to configure capability.

The signal is **coarse** — a boolean-ish "do any write-granting policy rows
exist," NOT a per-zone or per-principal breakdown.

## Why it was deferred (record this reasoning)

1. **It is only meaningful when the floor is down.** An up floor denies every
   write regardless of grants, so in observation and safe_mode the field is moot.
2. **Its only consumer is the deferred on-demand "evaluate write capability"
   button**, which is not built.
3. **Computing it requires reading the RBAC policy store for write-granting
   rows** — surface owned by the separate, backlogged full-mode/RBAC track (the
   `agent:core` principal and the empty-RBAC fail-closed work; see
   docs/backlog/full-mode-rbac-track.md). Adding it to the posture endpoint now
   would straddle two tracks for a field with no live reader.

## Dependency / ordering

- Ships **WITH** the full-mode/RBAC track
  (docs/backlog/full-mode-rbac-track.md) and the on-demand
  evaluate-write-capability button — NOT before.
- It should be computed against whatever the RBAC track's **final** grant model
  turns out to be, rather than a coarse version built now and reworked later.

## Implementation note for whoever picks it up (record — do NOT implement)

The addition is a new **optional snake_case JSON field** on the existing posture
endpoint's response struct — additive and non-breaking. It must carry an explicit
snake_case `json` tag, consistent with the endpoint's other tags and with
`panicStatusResponse` (whose fields all carry snake_case tags —
`internal/api/panic.go:51–56`, e.g. `SafeMode bool \`json:"safe_mode"\``), NOT
the tagless PascalCase pattern of the `Regime` struct
(`internal/sessionmodel/types.go:101–106`, fields `Mode` / `DeclaredAt` / … with
no json tags).

## Acceptance criteria (record only — do NOT implement)

- The posture endpoint, **when and only when the floor is down (full mode)**,
  reports a coarse write-grants-exist signal sourced from the RBAC policy store.
- The field is **absent or moot when the floor is up** (observation / safe_mode).
- The on-demand evaluate-write-capability UI control **consumes** it.
- **No per-zone / per-principal detail** is required for this coarse signal.

## References (link, do not duplicate)

- docs/project/DECISIONS.md **D-0019** — the trust model; design of record for this item
  (the two postures, the "configured vs zero grants" distinction, and the future
  on-demand capability control).
- docs/project/DECISIONS.md **D-0018** — the write floor (the observation / safe_mode
  reasons; why the field is moot when the floor is up).
- docs/backlog/full-mode-rbac-track.md — the full-mode/RBAC track this ships with;
  owns the RBAC policy-store surface this signal reads from.
