# Backlog — Unify the tilde-expansion helpers

Status: deferred (recorded by D-0026; the unit-1 Probe fix duplicated the helper on purpose — do not unify as part of D-0026).

## Context

Three helpers expand a leading `~` in a path, and two of them are byte-identical
hand-copies:

- `internal/adapters/k8s/k8s.go:172` — `expandPath` (canonical): `os.UserHomeDir`
  + `~` / `~/x` handling, **no** `filepath.Abs`.
- `internal/credential/kubeconfig_exec.go:179` — `expandKubeconfigPath` (a
  deliberate hand-copy of the above): same logic, byte-for-byte.
- `internal/paths/defaults.go:47` — `ExpandPath`: `getSecureHomeDir` (bypasses the
  `HOME` env var) **plus** `filepath.Abs`.

The first two are duplicated because of an import cycle: the D-0026 unit-1 Probe
fix needed the k8s adapter's expansion inside the credential package, but
`internal/adapters/k8s` imports `internal/credential` (D-0026 unit 2), so
`credential` cannot import `k8s` back to share the helper. Copying was the
seam-free option at the time.

`paths.ExpandPath` is **not** a drop-in replacement for the other two, despite
looking similar:

- It resolves home via `getSecureHomeDir`, which deliberately bypasses `HOME` to
  prevent `HOME=/tmp/fake` bypass attacks. The adapter helpers use
  `os.UserHomeDir` (which honors `HOME`). Swapping changes behavior under a
  modified `HOME`.
- It calls `filepath.Abs`, so it rewrites relative paths to absolute. The adapter
  helpers leave a relative path **unchanged**. clientcmd / the kubeconfig loader
  rely on the adapter's no-`Abs` semantics; making paths absolute is a behavior
  change, not a refactor.

## Proposed future work (the eventual target)

A single shared `~`-expansion that the k8s adapter, the credential provider, and
ideally `paths` all converge on. Reaching it requires:

1. Breaking the cycle so a shared helper has a home neither `k8s` nor
   `credential` has to import the other to reach (e.g. a small leaf package both
   already-or-could depend on).
2. Reconciling the semantic difference: decide whether the shared helper resolves
   home via `os.UserHomeDir` or `getSecureHomeDir`, and whether it applies
   `filepath.Abs`. The adapter's `os.UserHomeDir` / no-`Abs` behavior is the one
   the kubeconfig path depends on, so converging `paths.ExpandPath` onto it (or
   parameterizing the two axes) means touching `paths` callers too.

Because it touches the adapter and the security-sensitive `getSecureHomeDir`/`Abs`
behavior, it is not a mechanical merge — hence deferred.

## Why deferring is safe

`internal/credential/tildeguard` (added under D-0026) asserts the two duplicated
helpers produce identical output for a shared input table and fails the moment
either diverges. The duplication cannot silently drift while this stays deferred.

Reference: `docs/project/adr/D-0026-credential-provider-abstraction.md`.
