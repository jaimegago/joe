# Full RBAC v2 — role indirection, group subjects, and granular permissions

Status: open

## Frame

This is a design seed, not an active workstream. It captures the reasoning from
the CCh thread that produced read-posture-latch, so that the eventual v2 design
session opens mid-thought rather than from a blank page.

Joe today implements zone-scoped access control, not RBAC, despite the
`internal/rbac` package name. Stated honestly, the current model is: subjects are
`user:` (a verified-email human), `svc:` (a named service account / machine
identity, including the in-process boot identities `svc:agent:core` and
`svc:sweeper:sessions`), and the canonical `agent:core` Core-Agent identity;
`group:` is reserved but unminted — the prefix exists and is guarded against
spoofing, but nothing populates it. Grants are `(principal, zone)` and
action-less: a grant binds a principal to a zone and carries no verb of its own.
The Read/Mutate ceiling lives on the zone (`allowed_actions`) and therefore
applies uniformly to everyone granted that zone — there is no way to give one
principal read-write and another read-only on the same zone. There is no role
indirection: access is a flat set of principal-to-zone rows. Admin is a single
boolean — holding the row lets a principal act on any zone for any action the
zone itself permits, with no per-zone grant, and it is the only role-like
construct in the system.

v2 adds the role layer and the group subjects the package name already implies.

## Fixed points v2 must compose with and must not re-litigate

The write floor (D-0018) is unconditional and orthogonal. It denies all mutates
regardless of RBAC; RBAC governs mutates only in full mode. v2 does not touch
the floor and does not get to reason as though mutates are always reachable.

The Read/Mutate binary (D-0020) is the floor's decidable axis. Any finer verbs
v2 introduces live above the floor as an RBAC refinement and never widen past
the binary at the safety seam — whatever a role names, the seam still sees only
Read or Mutate, and the floor still has the last word on Mutate.

Denial precedence is floor > incident > RBAC (D-0022). RBAC is the last and
weakest gate in the chain; a v2 allow never overrides a floor or incident deny.

Authorization is additive: allow-only, no-deny. Grants only widen. The
subtractive dual is per-session attenuation — an operator voluntarily narrowing
Joe below their own grants for a single session — which is subtract-only and
never widening. The two directions stay separate: grants add, attenuation
subtracts, and neither inverts.

There is a single accessor chokepoint, and audit happens at the decision point
(D-0009, D-0013). v2 must keep authorization decisions flowing through that one
seam and must not introduce a second, un-audited decision path.

Running = governed (D-0027): Joe refuses to start without a usable identity
configuration. v2 must preserve fail-closed-on-absent-governance; a richer model
must not open a path to booting ungoverned.

## Migration entry point — the contract v2 must honor

read-posture-latch persists a read posture with two values. `team_flat` is the
launch default: every authenticated principal reads every component, regardless
of zone. `zoned` is the grant-based read path, where reads are filtered by the
same `(principal, zone)` grants that already govern the rest of access. v2 ships
the `zoned` path, the operator flip between the two, and the roles/groups model
on top.

Upgraded installs inherit `team_flat` and behave exactly as before until a
deliberate, admin-gated, audited flip to `zoned`. The upgrade itself changes no
behavior; the operator decides when to tighten. v2 must not flip read behavior
implicitly — in particular it must not infer `zoned` from the mere existence of
zones, or from any other side effect. The flat-to-zoned transition is one
explicit, logged act and nothing else may trigger it.

This deferral is recorded under the read-posture-latch decision; Phase 1 found no
corresponding numbered entry in `docs/DECISIONS.md`, so this seed references it by
the slug read-posture-latch.

## Use-case inventory to design from

1. Differentiated team access on the same systems. For example, `devops` is
   read-write on dev and staging but read-only on prod, while `sre` is
   read-write on prod. This is the case zone-only access cannot express, because
   the Read/Mutate ceiling is a property of the zone shared by everyone granted
   it, not of the grant.

2. Per-session voluntary attenuation. A read-write human chooses to run Joe
   read-only for a single session — the subtractive dual of the additive grant
   model.

3. Least-privilege machine principals. An MCP or CI `svc:` principal scoped to
   exactly one zone, read-only, with no incidental reach.

4. The graduation ladder (D-0019): observe, then write in dev, then staging,
   then one prod zone, then wider — per zone and per capability, for humans and
   for `agent:core` alike. The v2 model must be able to name the points on this
   ladder rather than collapsing them into a single boolean.

5. New-teammate onboarding. Zero-grant and fail-closed by default, then access
   conferred through group or role membership rather than many hand-written
   per-principal rows.

6. "Who can mutate prod right now, and why" answerable as a query against the
   model itself, rather than reconstructed by archaeology across grants, zones,
   and admin rows.

7. Sub-zone grain. Whether per-component or per-component-type permission is in
   scope. Note that auto-promote-read already needed sub-zone read grain and had
   to admit it via a live predicate in the policy engine, outside `rbac_policies`
   — the grant could not express the scope — so v2 inherits an existing precedent
   that the zone is sometimes too coarse a unit for reads.

8. Incident break-glass, which is out of RBAC scope. Captain elevation is a
   separate axis — the incident regime — that interacts with RBAC only through
   precedence (D-0022) and is not part of the RBAC model itself.

## Open questions to resolve in the v2 design session, not now

- Does the role layer carry verbs finer than Read/Mutate, or does it stay binary
  above the floor?
- Is the unit of permission the zone, the component, or the component-type?
- Do groups come from the IdP `groups` claim — with `group:` as the reserved seam
  already in place — or are they Joe-native?
- Is `internal/rbac` renamed to match what it actually implements?
- How does per-session attenuation compose with the additive, allow-only grant
  model and the floor > incident > RBAC precedence chain?
