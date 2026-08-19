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
to the maintainer's ledger as `queue/oasis-da1-phase.md` in `joe-pm`, under the
cross-repository criterion. This item's closure is a `joe` commit; that horizon's
is not.

## What is not established

Whether the denial is a missing grant on the component, a defect in how the
refresh principal is resolved, or a fail-closed posture behaving as designed
against an environment that was never granted. The observation is the denial and
the empty graph; the cause is unexamined.

## Narrowed by a later tree reading

A read of the decision path, made while investigating a separate question, narrows
the three candidates above to the third — **fail-closed behaviour against an
environment that was never granted** — and says why the install-wide default does
not rescue it. Read it as a lead for the eventual fix, not as the fix:

- **The refresh engine has no read-posture seam.** `cmd/joe/server.go` builds the
  transport engine with `rbac.NewPolicyEngineWithGovernance` and the `agent:core`
  refresh engine with `rbac.NewPolicyEngineWithPromote` — deliberately, and
  break-tested as `TestReadPosture_AxisSeparation_RefreshEngineIgnoresPosture` in
  `internal/rbac/policy_readposture_test.go`. So the `team_flat` launch default,
  which admits every authenticated principal's read on the transport path, cannot
  reach the refresher. Grants plus `auto_promote_reads` are its complete read
  authority.
- **`auto_promote_reads` is OFF for every type on a fresh install.** Migration
  `024_agent_read_promotions` seeds no rows and an absent row means OFF, so no
  component type is promoted until an operator flips one.
- **The denial is therefore the designed steady state, and it is silent by
  design.** `internal/coreagent/refresh.go` treats a permit denial as expected for
  any component whose type is neither granted to `agent:core` nor promoted, logs it
  at debug, and skips to the next component without stamping a sync error.

What that leaves is a product question rather than a bug hunt: whether standing a
component up for evaluation should also arm the autonomous refresher for it, and by
which of the two routes. Nothing here changes the observation the item was filed on —
the graph is still never populated.
