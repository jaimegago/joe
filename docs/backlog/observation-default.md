# Full-mode boot posture: resolve the write floor down under the full-mode-requires-auth fail-closed guarantee

Status: open

## Context

D-0073 (session `observation-default`) inverted the boot write-floor default: an
unconfigured Joe (`JOE_MODE` unset) now boots in observation mode (read-only
below RBAC), and `JOE_MODE=full` is **refused at boot as not-yet-implemented**
rather than enabling writes or silently downgrading. Unrecognized values are
refused fail-closed. The decision is made by the pure `env.ResolveBootMode`
(`internal/env/keys.go`), which feeds the observation input of the unchanged
`safety.ResolveWriteFloor`; the writable resolution path (observation input
false) is retained as the seam for full mode.

## Deferred follow-up — actually implement full-mode boot posture

Make `JOE_MODE=full` resolve the write floor **down** at boot (observation input
false, so `ResolveWriteFloor` returns a down floor absent panic), removing the
not-yet-implemented refusal in `env.ResolveBootMode`. This must not land on its
own: full mode boots write-capable only **under the full-mode-requires-auth
fail-closed guarantee** — full mode requires authentication ON and a live policy
engine, and with zero write grants every managed-system write is still denied at
RBAC (the same observable behavior as observation mode). That RBAC half — the
fail-closed-empty-RBAC boundary, the dedicated autonomous principal, and the
inert/permissive-engine path made unreachable in full mode — is tracked in
`docs/backlog/full-mode-rbac-track.md` (design of record: D-0019). This item is
the write-floor seam half; it depends on that RBAC track landing first, so that
flipping `JOE_MODE=full` cannot produce a write-capable Joe running ungoverned.

When both halves land, `env.ResolveBootMode` returns `observation=false` for
`ModeFull` (and drops the refusal error), the boot site keeps the floor down for
full mode, and the acceptance criteria in `full-mode-rbac-track.md` gate the
change.

## References (link, do not duplicate)

- `docs/backlog/full-mode-rbac-track.md` — the RBAC half of full mode (fail-closed
  empty RBAC + the dedicated autonomous principal); this item's blocking dependency.
- docs/project/DECISIONS.md **D-0073** — the inverted boot default and the full/unknown boot refusal (design of record for this item's current state).
- docs/project/DECISIONS.md **D-0019** — the trust model: two boot postures and the fail-closed guarantee.
- docs/project/DECISIONS.md **D-0018** — the write floor (the layer this seam feeds).
