# Backlog — Governed connectivity-check surface for components

Status: open; CC-actionable when picked up. Not launch-blocking unless a launch
decision says otherwise — that is an **open question**, not asserted here.

## Problem

There are two divergent "does this component work?" paths:

- The **legacy** route `POST /api/v1/components/{id}/test` (handler
  `handleTestComponent`, `internal/api/webui.go:645`; registered in
  `registerWebUIRoutes` at `internal/api/webui.go:729`) is **not admin-gated** —
  it sits on the webUI handler group with no `requireAdmin` call. It is also
  effectively dead for an inert, credential-less component: it builds a fresh
  adapter and calls `adapter.Connect` (`webui.go:678`), but an un-promoted
  component has nothing to authenticate with and will fail to connect.
- The real post-A003 connectivity check is the admin **credential-status Probe**,
  `POST /api/v1/admin/credential-status/{componentID}/probe` (handler
  `probeCredentialStatus`, `internal/api/admin.go:852`; registered at
  `internal/api/admin.go:139`). It runs Resolve+Probe only on explicit human
  action and is **admin-gated** via `requireAdmin` (`internal/api/admin.go:853`).

The frontend still wires its test affordance to the legacy route:
`testComponent` (`ui/src/api/components.ts:18`) posts to
`/api/v1/components/${id}/test`. So the operator has no single governed
connectivity-check path, and the UI points at the wrong one.

## Desired outcome

One governed connectivity-check path for the operator UI:

- Decide whether to **deprecate** or **admin-gate** the legacy
  `/components/{id}/test` route.
- Repoint the frontend test affordance (`testComponent`,
  `ui/src/api/components.ts:18`) at the credential-status Probe for armed
  components.
- An inert (un-promoted) component has nothing to probe — the affordance should
  only be meaningful once a component is armed.

## Origin

Surfaced as **Finding 3** of the A002 current-state re-derivation, explicitly
parked as a separate surface — it is **not** part of the A002 registration /
promotion / `auto_promote_reads` input surface.

For context, the admin-gated/audited standard the legacy test route diverges from
was established by A003: **D-0029** (govern component registration as a
credential-less, admin-gated, same-tx-audited boundary) and **D-0030** (the
component promotion endpoint as the single governed read-only-to-armed
transition). Both in `docs/project/DECISIONS.md`.
