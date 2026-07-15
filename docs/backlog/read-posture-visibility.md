# Read posture is invisible in every UI and CLI surface

Status: open

## Context

The install-wide read posture (`team_flat` | `zoned`, `internal/readposture`) is a
load-bearing scalar: under `team_flat` every authenticated principal reads every
component, under `zoned` reads are grant-based. It is admin-flippable over REST
(`GET`/`POST /api/v1/admin/read-posture`) and each flip is audited. But the
**effective posture is not surfaced anywhere a human can see it** — there is no
Web UI affordance, no `joe` CLI subcommand, and no line in `joe`'s startup logs
that reports the resolved value. The admin `GET` endpoint exists, yet nothing
renders it; you can only learn the current posture by calling the REST API by hand
or reading the SQLite row.

## Why this matters (provenance)

Session `rbac-engine-split` diagnosed a wiring bug where the launch-default
`team_flat` read admit was structurally unreachable on the transport path (the
guarded accessor enforced with a bare engine that carried no read-posture
resolver, so `team_flat` reads returned 403). Diagnosing it took far longer than
it should have precisely because there was **no way to observe the effective
posture** — a first instinct ("is this install actually in `team_flat`?") had no
answer short of querying the DB, and the symptom (a non-admin getting 403 on a
read) looks identical whether the posture is `zoned` (working as designed) or
`team_flat` with the admit broken. Surfacing the posture would have turned an
evening of engine-tracing into a one-glance check. The fix landed the reason on
the deny path (403 body + request log) and the admit path (audit row), which
helps, but the standing posture value itself is still invisible.

## Proposed work

- Surface the effective read posture read-only in the Web UI (e.g. a small badge
  in the admin/security area, fed by the existing `GET /api/v1/admin/read-posture`
  via a `useReadPosture`-style hook — no new backend endpoint needed).
- Add a `joe` CLI read of the current posture (a subcommand or a field on an
  existing status surface) for operators without the Web UI.
- Log the resolved posture once at boot alongside the other governance signals,
  so it is visible in the daemon's startup output.

## Relationship to existing work

This is read-only *visibility* of the current posture and is distinct from — but
partially overlaps — the **v2 zoned-flip UI** item in
[`read-posture-latch`](read-posture-latch.md), which is a *write* affordance (an
admin control to flip `team_flat` ⇄ `zoned`) that would necessarily also display
the current posture. If the zoned-flip UI is built first it likely subsumes the
Web UI half of this item; the CLI read and the boot-log line stand on their own
regardless. `read-posture-latch` is scoped to deferring the zoned-era surfaces
behind the `team_flat` launch default; this papercut applies under `team_flat`
too, which is why it is tracked separately.

Provenance: session `rbac-engine-split`.
