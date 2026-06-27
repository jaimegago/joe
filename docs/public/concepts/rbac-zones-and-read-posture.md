---
title: RBAC, zones, and the read posture
weight: 40
description: How authorization is grouped, and how read breadth is chosen at launch.
---

# RBAC, zones, and the read posture

Once Joe knows *who* is asking (a [principal](../principals-and-identity/)) and *whether*
mutations are allowed at all (the [write floor](../observation-mode-and-the-write-floor/)),
it still has to decide *what* this particular principal may reach. That is the job of
RBAC, zones, and the read posture.

## Zones are the authorization grouping

A **zone** is a named grouping of components. Components are assigned to zones, and
policies bind principals to zones and actions. Rather than granting a principal access
to individual systems one by one, you grant access to a zone, and membership in that
zone carries the grant. A component that has not been assigned anywhere resolves to an
`unassigned` zone, which is read-only by default — an unplaced component is never
accidentally writable.

This is the grouping over which authorization decisions are made: "may this principal
perform this action on a component in this zone?" is the question the policy engine
answers for every governed access.

## The read posture: how wide reads are at launch

Joe separates a coarse, install-wide choice about *read breadth* from the per-zone
grant machinery. This is the **read posture**, a single persisted scalar with two
settings:

- **`team_flat`** — the **launch default**. Every authenticated principal may read
  every component, regardless of zone grants. Reads are flat across the team; the
  grant machinery still governs *mutations*, but not who can look. This is the simple,
  legible posture for a team that trusts its members to see everything and wants
  governance focused on what changes the world.
- **`zoned`** — the full, grant-based read path. Reads are governed by zone grants
  exactly like mutations are: a principal reads a component only where a policy grants
  it.

The posture governs **human-facing reads only**. Flipping it is an admin action that
is audited; it widens or narrows *who may read a permitted action*, never *which
actions a zone permits*.

## Flipping the posture does not change what the agent can read

This is the part most worth internalizing: the read posture is a **human-facing
transport** concern, and it has **no effect on Joe's autonomous agent**. The
background loop's read surface (`svc:agent:core`) is a separate axis, governed solely
by per-component-type read promotion plus grants. The two axes are separated where the
policy engines are constructed — the human transport engine carries the posture
resolver; the agent's refresh engine does not — so changing the human read posture
*cannot* change what the autonomous agent is able to read. Widening human reads to
`team_flat` does not widen the agent; narrowing them to `zoned` does not narrow it.

## Reads and writes are independent axes

Read posture decides read breadth. The [write floor](../observation-mode-and-the-write-floor/)
and write-side RBAC decide mutations. They are independent: a `team_flat` install with
the write floor up lets everyone read everything and still denies every mutation. Wide
visibility and locked-down action are not in tension here; they are different knobs.

To change the posture or manage zones and policies in a running install, see
[Operations](../../operations/); for the configuration that bootstraps identity and
admin access, see [Configuration](../../configuration/).
