---
title: The agent loop and autonomy levels
weight: 60
description: The interactive and background loops, and how autonomous Joe really is today.
---

# The agent loop and autonomy levels

Joe runs two distinct loops, and it is worth keeping them separate in your head
because they have different jobs and different degrees of autonomy.

- The **interactive agentic loop** runs per request. It is the chat/agent surface: you
  ask Joe something, and it reasons, calls tools (always through the governed accessor),
  and answers. A human is in the conversation.
- The **background refresh loop** (the Core Agent) runs on its own, periodically,
  under the `svc:agent:core` principal. Its job is to keep the [knowledge
  graph](../knowledge-graph/) current as your infrastructure changes.

Both loops reach managed systems only through the same governed path, and both are
subject to the [write floor](../observation-mode-and-the-write-floor/). Nothing about
being "the agent" exempts Joe from its own governance.

## The autonomy model

Joe's design describes a graduated autonomy model — work is handled at the lowest level
of human involvement that is safe for it:

1. **Autonomous** — deterministic changes Joe can make on its own, such as applying an
   unambiguous graph delta when it observes that the world changed.
2. **LLM + Auto** — high-confidence inferences the model can act on without a human in
   the loop.
3. **Needs-Human** — ambiguous findings that should be queued for a person to resolve
   rather than guessed at.

This is the intended shape of the spectrum, from fully mechanical to "ask a human."

## An honest account of what ships today

The autonomy model is best understood as a direction, not a finished feature, and one
part of it is explicitly incomplete:

- The background refresh loop **does** ship the deterministic, *Autonomous*-tier
  behavior: it applies graph deltas it can derive unambiguously.
- The refresh loop does **not** ship the *Needs-Human* branch. The step that would
  queue an ambiguous finding from the periodic loop for human clarification is a
  stub — it is not built. Clarifications exist as a subsystem in the codebase but are
  not exposed in this release; where they are populated at all, it is by the onboarding
  and discovery flows, not by the periodic refresh.

So if you read about Joe "queuing ambiguous findings for clarification," understand
that this is true of its onboarding/discovery path, not (yet) of the autonomous
refresh loop. Treat the refresh loop as a deterministic graph-keeper, not as a system
that escalates its own uncertainty.

One related detail to set expectations: the refresh loop runs on a **fixed cadence**.
It is not a tunable interval today, regardless of what a configuration field might
appear to suggest.

For how to *use* the interactive agent, see [Guides](../../guides/); for what the
background loop maintains, see [The knowledge graph](../knowledge-graph/).
