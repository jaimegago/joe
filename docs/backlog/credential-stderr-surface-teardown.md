# Backlog — Tear down the vestigial credential-stderr surface

Status: open (created by D-0065, agent-identity-doc-04)

## Context

The captured-stderr surface exists only because the (now-deleted) kubeconfig-exec
provider could capture an exec-plugin's raw stderr on a mint failure and surface it
through a deliberate human-facing "paste this" accessor. With that provider retired
in D-0065, **no provider sets captured stderr anymore**, so the surface is vestigial:

- `internal/credential/credential.go` — `Credential.stderr` field and
  `Resolution.CapturedStderr()`; `CapturedStderr()` now always returns `""`.
- `internal/api/admin.go` — the `POST .../credential-status/{componentID}/probe/stderr`
  route (`credentialStderr`), the `credentialStderrResponse` type, and the
  `StderrAvailable` field on the probe response (always `false` now).
- `ui/src/components/admin/CredentialStatusTable.tsx` — `fetchCredentialStderr`,
  the stderr mutation, and the `stderr_available`-gated reveal affordance (which will
  simply never render, since the flag is always false).
- `ui/src/api/credentialStatus.ts` — the `fetchCredentialStderr` client function.

It was deliberately left in place in D-0065 to keep that slice a clean deletion:
removing it ripples across the credential package, the admin API (route + response
types), and the React UI (requiring a `make build` UI re-embed), which is a distinct
change from retiring the provider.

## Proposed work

Remove the stderr field/accessor, the admin endpoint + route + response types + the
`StderrAvailable` signal, and the UI fetch + reveal affordance, in one change that
rebuilds and re-embeds the UI. Nothing reads the surface, so the removal is behavior-
preserving (the affordance is already inert).

If a future provider legitimately needs to surface captured backend output on a mint
failure, reintroduce the surface at that time against the real producer rather than
keeping this dead one.

Reference: D-0065 (`docs/project/DECISIONS.md`).
