# Backlog — Consolidate the captain detach/attach write patterns behind one tx-aware seam

Status: deferred (recorded by D-0025; do not act on it as part of D-0025).
Priority: later

## Context

After D-0025 there are **three** distinct ways the codebase writes the
`session_captains` rows, each with its own detach/attach SQL:

1. **Resolve path (D-0024)** — `ResolveIncidentRegimeWithHook` in
   `internal/sessionmodel/regime_transitions.go` detaches the active captain
   with an inline `UPDATE ... SET detached_at = ?, transfer_state = NULL,
   incoming_principal = NULL, transfer_initiator = NULL WHERE session_id = ?
   AND detached_at IS NULL` on the resolve transaction.
2. **Non-transactional primitives** — `MarkCaptainDetached` and
   `AttachCaptain` on `*SQLRepository` (`internal/sessionmodel/repository.go`),
   each a standalone write on `r.db`. Still used by the non-swap call sites
   (`Attach` / R-CAP2 first-human-attach uses `AttachCaptain`).
3. **Transfer swap (D-0025)** — `SwapCaptain` / `swapCaptainWithHook` on
   `*SQLRepository` performs detach (inline `UPDATE`, keyed by `id`) + attach
   (shared `attachCaptainExec`) atomically on one `*sql.Tx`.

The detach `SET` clause is now duplicated in (1) and (3) (and mirrored in
(2)'s `MarkCaptainDetached`). The attach INSERT is shared between
`AttachCaptain` and `SwapCaptain` via `attachCaptainExec`, but the detach is
not yet shared.

## Proposed future work

Introduce a single tx-aware detach/attach seam (e.g. `detachCaptainExec(ctx,
exec sqlExecer, ...)` paralleling `attachCaptainExec`, plus a small helper for
the shared detach `SET` clause) and route all three sites through it, so the
detach SQL exists in exactly one place and every caller — resolve, swap, and
the standalone primitives — composes the same building blocks against either
`r.db` or a `*sql.Tx`.

## Why deferred

D-0025 is scoped to making `completeTransfer`'s swap atomic. Consolidating the
three patterns is a larger refactor that touches the resolve path (D-0024) and
the still-used non-tx primitives; doing it now would expand the blast radius of
a targeted durability fix. The duplication is small (one `SET` clause) and the
behavior is identical across sites, so deferring carries low risk.
