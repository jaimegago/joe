---
title: Observation mode and the write floor
weight: 20
description: The boot-resolved, runtime-immutable gate that denies managed-system mutations.
---

# Observation mode and the write floor

Joe classifies every tool it can run by what it is capable of: read-only tools can
only gather information; mutation-capable tools can change something out in your
infrastructure. The classification is by capability, not by invocation — a tool that
could mutate is treated as a mutation even when a particular call would only read. The
**write floor** is the gate that decides, at the coarsest level, whether mutations are
permitted at all.

## Boot-resolved and runtime-immutable

The write floor is resolved exactly once, at boot, from the daemon's startup inputs.
After that it is immutable for the entire life of the process. No request, no admin
action, no policy edit, and no internal state change can lower it while Joe is running.

This is deliberate. A gate that could be lowered at runtime is a gate an attacker — or
a confused automation — could try to lower. By sealing the value at boot, Joe removes
that entire class of move: the only way to change the floor is to change the boot
inputs and restart. Recovery is a restart, never a live downgrade.

## It denies before any other gate

When the floor is up and a mutation is attempted, the executor denies it **first**,
before it consults zone scope, namespace scope, RBAC, or anything else. The denial
carries a reason but does not fall through to the finer-grained checks. This ordering
is intentional: the floor is the reason a caller can least readily fix, so it is the
reason they are told about. There is no combination of zone grants or roles that lets a
mutation past a raised floor.

## The reason axis: observation versus safe mode

The floor can be up for one of two distinct reasons, and Joe keeps them distinct so an
operator can tell *why* writes are blocked:

- **Observation mode** — Joe's **default read-only posture**. The floor is up because
  Joe ships in observation mode: it comes up read-only when `JOE_MODE` is unset and,
  explicitly, with `JOE_MODE=observation`. This is the posture in which Joe sees and
  reasons about your infrastructure without being able to change it. `JOE_MODE=full`
  is recognized but refused at boot as not yet implemented, so today observation is the
  only posture Joe boots into.
- **Safe mode** — the floor is up because Joe was put into an emergency
  shutdown/panic state. This is a reaction to something going wrong, not a starting
  posture.

Both raise the same floor and block the same set of mutations; the reason axis only
changes how the state is explained, not what it forbids.

## Gate order: floor over incident over RBAC

The write floor sits at the top of a fixed denial precedence:

1. **Write floor** — is mutation allowed at all?
2. **Incident regime** — during a declared incident, are non-captain mutations
   refused? (See [The incident regime](../incident-regime/).)
3. **RBAC and zones** — is *this* principal allowed *this* action on *this* component?
   (See [RBAC, zones, and the read posture](../rbac-zones-and-read-posture/).)

The order is by how readily a caller could resolve the denial: the floor is the
hardest to change (it needs a restart), so it is checked first and its reason outranks
the others. A mutation that would trip several gates is stopped at the highest one and
reported with that gate's reason.

## Reads and the read posture are separate

The write floor governs **mutations**. It says nothing about reads — what a human or
the autonomous agent may *read* is a different axis entirely, covered in
[RBAC, zones, and the read posture](../rbac-zones-and-read-posture/). An install can have
its read breadth wide open and still deny every mutation, because the floor is up.

To actually run Joe in observation mode, or to recover from safe mode, see
[Operations](../../operations/) and [Configuration](../../configuration/).
