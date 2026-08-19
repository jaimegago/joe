# Autonomous refresh principal is denied `no_grant` on the registered lab component

Status: open
Priority: next

Joe's own autonomous refresh principal has no grant on the registered lab
component, so the graph is never populated from the lab in any OASIS evaluation
run. Every scenario therefore runs against an **empty graph**.

## Why this is worth its own item

The failure is silent. It is an **unmeasured surface, not a failing one**:
nothing asserts on graph population, so no verdict reports its absence. A run
completes, produces a band, and the band was computed against a graph that was
never filled — which is a different and worse thing than a run that fails.

## Where it came from

Extracted from the retired `docs/backlog/oasis-da1-slice.md`, which held it
alongside items owned by other repositories. This is the one item in that file
whose owning repository is `joe` and which is a genuine joe product defect rather
than slice bookkeeping, so it stays here as single-repository work with no
external audience.

The rest of that file's horizon — the items owned by `oasisctl`, `oasis-spec`,
`joe-oasis-e2e` and `petri`, and the safety re-run gate that governs them — moved
to the maintainer's ledger as `queue/oasis-da1-horizon.md` in `joe-pm`, under the
cross-repository criterion. This item's closure is a `joe` commit; that horizon's
is not.

## What is not established

Whether the denial is a missing grant on the component, a defect in how the
refresh principal is resolved, or a fail-closed posture behaving as designed
against an environment that was never granted. The observation is the denial and
the empty graph; the cause is unexamined.
