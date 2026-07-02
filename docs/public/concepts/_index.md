---
title: Concepts
weight: 30
description: Why Joe is governed by construction, and how its parts fit together.
---

# Concepts

This section explains *why* Joe works the way it does and how its pieces fit
together. It is explanation, not instruction: where you need to actually do
something — install, configure, connect a system, operate the daemon — these pages
link onward to the section that covers it.

Read [The governed-safety invariant](governed-safety/) first; it is the idea the
rest depend on.

## Pages

- [The governed-safety invariant](governed-safety/) — why "running implies governed"
  is structural, not a matter of discipline.
- [Observation mode and the write floor](observation-mode-and-the-write-floor/) — the
  boot-resolved, runtime-immutable gate that denies managed-system mutations.
- [Principals and identity](principals-and-identity/) — how humans and machines are
  named and authenticated.
- [RBAC, zones, and the read posture](rbac-zones-and-read-posture/) — how
  authorization is grouped and how read breadth is chosen.
- [The component lifecycle](component-lifecycle/) — the registered-system entity
  and the single governed path that arms it.
- [The agent loop and autonomy levels](agent-loop-and-autonomy/) — the interactive
  and background loops, and how autonomous Joe really is today.
- [Chat sessions](chat-sessions/) — first-class, team-public sessions and what their
  security actually protects.
- [The incident regime](incident-regime/) — what an incident changes about Joe's
  posture.
- [The knowledge graph](knowledge-graph/) — components, relationships, and how
  observability backends are resolved.
- [The action model](action-model/) — the full surface of what Joe can do, how every
  action is classified Read or Mutate, and how each class is governed.
