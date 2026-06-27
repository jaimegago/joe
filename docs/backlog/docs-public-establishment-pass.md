# Backlog — Establish docs/public from the operator-facing root docs

Status: open

Deferred out of the `docs-tree-restructure` session, which reorganized the docs
tree into `docs/project` (build-meta plus the ADR annex), `docs/reference` (system
truth), and `docs/backlog/investigations` (live findings), but deliberately left
the operator-facing how-to docs at the `docs/` root as the agreed handoff to a
separate, later pass.

## What stays at `docs/` root (the inputs to this pass)

These five operator-facing documents were **not** moved by `docs-tree-restructure`.
They remain directly under `docs/` as the agreed operator-facing basis, and they are
the inputs to a future `docs/public` establishment pass:

- `docs/configuration.md`
- `docs/integrations.md`
- `docs/operations.md`
- `docs/web-ui.md`
- `docs/break-glass-access.md`

## The pass

Stand up a `docs/public` area (operator- and end-user-facing documentation) from
these five as the source material — deciding final placement, naming, and any
splits/merges at that time. Their cross-links into and out of `docs/reference` and
`docs/project` were already repaired during `docs-tree-restructure`, so this pass
starts from a tree with no dangling references.

This is a structure/placement decision, out of scope for the restructure session,
and is recorded here so the handoff is not lost.
