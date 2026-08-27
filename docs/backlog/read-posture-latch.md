Read-posture latch — launch as team_flat, defer the zoned (full-mode) surfaces

Status: in-progress — the posture mechanism, the admin flip endpoint, and the audit trail have landed (D-0041); the axis-coupling between the read posture and the agent:core autonomous read surface introduced by D-0041 was corrected (D-0043): the posture governs human-facing transport reads only. This file tracks the deferred work that the launch default (`team_flat`) lets us postpone.
Priority: now

## Context

D-0041 introduced a persisted, install-wide read posture. The launch default is
`team_flat` (every authenticated principal reads every component); `zoned` is the
grant-based full-mode read path. Because a fresh/upgraded install runs as
`team_flat` and never needs an operator to touch zones, policies, or grants to be
usable, the entire zoned-era surface area becomes **full-mode-only** and can be
deferred out of the launch build without losing any capability.

## Deferred work

- **Hide the zoned-era admin UI from the launch build — scope corrected; Policies
  done (read-posture-latch, D-0072).** The original three-surface scope (Zones,
  Policies, and component-zone assignment) was **wrong**. The zone-allows-action
  gate (`zone.Allows`, `internal/rbac/policy.go`) runs **ahead** of the
  `team_flat` read admit, so Zones and component-zone assignment still shape
  **which** reads are permitted even under `team_flat` — they are **not** inert
  and must **stay visible**. Only the **Policies** page (grant rows) was truly
  inert under `team_flat`: the boot-resolved write floor denies every Mutate below
  RBAC, and the `team_flat` admit widens read to every authenticated principal
  ahead of the read-grant logic, so grants admit nothing either way. The Policies
  page is now **posture-gated in the UI** by this session: its sidebar nav entry
  and `/admin/policies` route render only when the live posture is `zoned`,
  redirecting/hiding under `team_flat`. The gate is **client-side only** — the
  backing `/api/v1/admin/policies` REST endpoints stay registered and admin-gated
  so an operator can manage grants over REST in either posture. The posture is
  fetched via `useReadPosture` (GET `/api/v1/admin/read-posture`); no backend
  endpoint was added. Zones and component-zone assignment are explicitly
  **untouched** and stay visible. The v2 zoned-flip admin UI (below) remains
  deferred.
- **Reframe the public docs for the two eras — done (read-posture-docs-reframe).**
  The audit this bullet asked for was run and **found the two-era framing already in
  place** across the named targets: `docs/reference/security-in-layers.md` §3.5 and
  §8.1 (which already carries a launch note), `docs/reference/joe-architecture.md`,
  `README.md`, and `docs/public/api-reference/_index.md` all name `team_flat` as the
  launch default and `zoned` as the opt-in full-mode read path. No location was found
  presenting grant-based read as the baseline. That work appears to have landed with
  D-0041/D-0043 without this bullet being updated. Two real gaps were found and
  closed instead:

  - `docs/public/guides/web-ui.md` listed **Policies** among the admin surfaces every
    admin sees. It is posture-gated (`ui/src/auth/RequireZonedPosture.tsx`,
    `ui/src/components/layout/Sidebar.tsx`) and does not render under `team_flat`. The
    guide now says so, including that the REST endpoints stay available and that Zones
    and component-zone assignment are **not** gated.
  - `docs/public/concepts/rbac-zones-and-read-posture.md` was correct but let a reader
    infer zones are inert at launch. It now separates the zone gate (live in every
    posture) from the read grant (the `zoned`-era part).

  Residue, not closed: `docs/web-ui.md` — the internal UI *specification* — still
  describes a tabbed `/admin` surface with a Policies tab, while the shipped UI has a
  flat sidebar with an Admin subgroup. That drift is wider than the read posture and
  was left alone rather than half-fixed.
- **v2 zoned-flip UI — done (read-posture-zoned-flip-ui, D-0157).** The flip is
  no longer REST-only. `/admin/read-posture` is a standalone admin route under
  the Admin nav subgroup showing the live posture and flipping it `team_flat` ⇄
  `zoned`, behind `<RequireAdmin>`; no backend endpoint was added. The
  confirmation states the consequence rather than the mechanism: switching to
  `zoned` narrows non-admin reads to grants while **admins are unaffected** (the
  admin short-circuit admits regardless of grant), and it names the grant count,
  stating the lockout outright when that count is zero; switching back widens
  read to every authenticated principal, `svc:` API keys included, leaving
  existing grants kept but inert. A successful flip writes the applied posture to
  `QUERY_KEYS.readPosture`, so the Policies nav entry and route guard follow
  without a reload. **The page is deliberately NOT posture-gated** the way
  Policies is — gating it on `zoned` would hide the only control that can leave
  the launch-default `team_flat`.
- **Roles and groups for full RBAC v2.** The grant model the `zoned` posture
  drives is still per-principal, per-zone. Full RBAC v2 (role indirection, group
  subjects, granular permissions) is the larger evolution that makes the `zoned`
  era ergonomic at scale — tracked in [`rbac-v2`](rbac-v2.md) and the related
  [`full-mode-rbac-track`](full-mode-rbac-track.md); this item is the read-posture
  framing of why that work is deferrable behind the `team_flat` launch default.
- **Decide whether `team_flat` should admit `user:` principals only (transport
  scope of the admit).** Current behavior (verified read-posture-latch-02): the
  `team_flat` admit on the **transport** engine fires for **any** non-empty
  principal set on `ActionRead` — there is no principal-type check, so it admits
  named `svc:` API-key principals as well as `user:` principals. The follow-on
  decision is whether `team_flat` should be a **human** read-sharing posture that
  admits `user:` principals only and leaves all `svc:` named API-key principals on
  the grant-based path (so a machine integration's read surface stays explicitly
  granted regardless of the human posture). This is purely the transport-engine
  question; the agent:core autonomous read surface is already off the posture axis
  (D-0043). Not changed in this build — recorded here as the verified starting
  point for the decision.
