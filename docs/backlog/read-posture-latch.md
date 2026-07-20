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
- **Reframe the public docs for the two eras.** Rework the human-facing docs so
  zones and grant-based read are presented as the **full-mode (`zoned`) era**
  concept, not the default mental model. The launch story is team-flat read
  (team-public, integrity-and-accountability spine); zones/grants are the
  opt-in full-mode evolution. Audit `docs/reference/security-in-layers.md` and the
  architecture/README prose for places that assume grant-based read is the
  baseline.
- **v2 zoned-flip UI.** The posture flip is REST-only today (`POST
  /api/v1/admin/read-posture`). Build the admin UI affordance that flips an
  install from `team_flat` to `zoned` (and back), surfacing the current posture
  and the consequence of the flip. Gated to admins; reads the live posture.
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
