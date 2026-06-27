---
title: The incident regime
weight: 80
description: What a declared incident changes about Joe's posture.
---

# The incident regime

The **incident regime** is Joe's notion of "we are in an incident right now," and it
changes how the daemon treats mutations while it is active. It sits in the middle of
the [denial precedence](../observation-mode-and-the-write-floor/) — below the write floor,
above RBAC — so it can tighten what is allowed during an incident without ever
loosening the floor.

## What it is, conceptually

An incident regime is a declared state. When an incident is active, Joe constrains
mutating actions: during an incident, the executor refuses mutations that are not
authorized for the incident, so changes to managed systems do not happen ad hoc in the
middle of a response. The point is to keep an incident's changes deliberate and
attributable rather than letting any authorized principal mutate freely while everyone
is reacting.

The incident regime is **orthogonal to the write floor**. Safe mode (a raised floor)
and an active incident are independent, composable states: you can be in one, the
other, both, or neither. The floor asks "are mutations allowed at all?"; the incident
regime asks "during this incident, whose mutations are allowed?" — and the floor is
checked first.

## What ships: the incident banner

Joe surfaces the active incident state in the web UI as an app-wide **incident
regime banner** — a persistent indicator that an incident is in effect, so no one
operating Joe is unaware of the regime they are working under. The banner is a
real, shipped part of the interface, and it composes with the separate observation-mode
and safe-mode banners.

## What is deferred

The conceptual incident regime is broader than what ships today, and this page is
careful not to over-claim. In particular, a richer incident **captain UI** and a
dedicated incident **read surface** are **deferred** — they are not part of the current
documentation surface and should not be relied on. What you can count on now is the
regime state itself, the way it constrains mutations, and the banner that makes the
state visible.

For how to declare and resolve an incident, and how it interacts with day-to-day
operation, see [Operations](../../operations/).
