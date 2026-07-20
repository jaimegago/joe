# Backlog — Drive component promotion validation from a single per-Kind requirements source

Status: open; CC-actionable when picked up. Not launch-blocking — the
Priority: later
guard-tested describe-only table is safe to ship (stated assessment, not an
asserted launch decision).

## Context

A002 added a describe-only `promotionRequirements` table (in
`internal/credential`, beside the wiring registry `internal/credential/wiring.go`)
plus a promotion-requirements read endpoint, so the operator UI can render the
correct provider-conditional locator fields per component type. That table is
DESCRIBE-ONLY: it states the per-Kind reference rules (which locator fields are
required, static's inline-value rejection, kubeconfig-exec's either-or) for the
endpoint to read, while `buildArmedConfig` (`internal/api/components.go:589`)
continues to ENFORCE those same rules via its own inline branching — the per-Kind
`switch` at `internal/api/components.go:608`–642 (static: `env_var` required +
inline `value` rejected, lines 609–620; kubeconfig-exec: `in_cluster`-or-
`kubeconfig` either-or, lines 621–639). The two are kept in agreement by a guard
test, but they remain two declarations of the same rules.

## Problem

This is a deliberate drift surface — the requirements table and the handler's
enforcement branching encode the same per-Kind rules in two places. The guard
test catches divergence, but the cleaner end state has one source.

## Desired outcome

Refactor `buildArmedConfig` to VALIDATE FROM the `promotionRequirements` table
rather than inline branching, so the table becomes the literal enforcement
authority and the describe endpoint reads the exact same source the handler
enforces — eliminating the table-vs-handler drift entirely (nothing left to drift
against).

This is a backend enforcement change to a governed security seam (the D-0030
promotion handler), so it must be its own decision with its own break-testing:
the refactor must be proven behavior-preserving against the current inline rules.
The existing promotion validation tests must pass unchanged, plus the
structural-invariant break-tests for inline-secret rejection and either-or must
still hold.

## Origin

Surfaced by the A002-PROMOTE-REQUIREMENTS-FEASIBILITY investigation as the
strongest no-drift posture, explicitly deferred from A002 because A002 is a
UI/read surface over the settled promotion backend and does not modify governed
enforcement.

Reference: D-0030 (the component promotion endpoint, `docs/project/DECISIONS.md`).
