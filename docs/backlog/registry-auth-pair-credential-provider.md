# Backlog — Registry-auth-pair credential provider (deferred from A003)

Status: deferred from A003 (promotion-boundary work) to its own future design
session.

## Problem

The container/artifact registry component types — `oci_registry`
(`ComponentTypeOCIRegistry`), `dockerhub` (`ComponentTypeDockerHub`), and
`artifactory` (`ComponentTypeArtifactory`, all in `internal/store/constants.go`)
— authenticate with a **registry-auth shape** that is *not* a single static
bearer token. It is a token-or-basic-auth pair: a username plus a
password/token. `artifactory` is additionally **bimodal** — it authenticates
either via an `X-JFrog-Art-Api` single-token header *or* via username basic-auth
— so even reading "the credential" off the config is ambiguous between two
distinct shapes.

The credential-provider seam (`internal/credential/provider.go`, the `Provider`
interface with `Resolve` / `Probe` / `Describe`) currently has only
`StaticProvider` (`internal/credential/static.go`) for single-token / env-var
credentials and `KubeconfigExecProvider`
(`internal/credential/kubeconfig_exec.go`) for kubernetes. Neither models a
registry-auth pair.

## Why the existing providers do not fit

`StaticProvider`'s `staticConfig` (`internal/credential/static.go`) is
token-shaped: it holds only an inline `value` or an `env_var` name, and
`Resolve` returns one opaque string. A registry-auth credential is a **pair**
(username + password/token), and for `artifactory` the provider would also have
to choose between the token-header mode and the basic-auth mode. There is no
field in `StaticProvider` for the second half of the pair and no way to express
the bimodal selection, so these types cannot go through the seam as static.
`KubeconfigExecProvider` is kubernetes-specific.

A registry-auth-shaped provider (or a basic-auth/pair-capable provider)
implementing `Resolve` / `Probe` / `Describe` is needed before these types can be
wired and promoted.

## Promotion impact

`oci_registry`, `dockerhub`, and `artifactory` **cannot be promoted under the
credential-reference model** until a registry-auth-shaped (pair-capable) provider
exists. The promotion endpoint keys on the wired-type registry
(`internal/credential/wiring.go`), so until such a provider is wired these types
are correctly rejected as unwired.

Notes on the current state:

- `dockerhub` has **no standalone adapter** — it is an OCI alias that uses the
  shared OCI adapter (`internal/store/constants.go:47`,
  `internal/adapters/registry/oci/`), so it inherits the OCI registry-auth shape.
- `artifactory` was wired onto the static seam in A003-W2 and then **reverted in
  the W2 follow-up** precisely because of this bimodal token-or-basic-auth shape;
  it does not fit the single-static-token model.
- The rationale for excluding all three currently lives **inline** in the
  "Deliberately ABSENT" comment in `internal/credential/wiring.go`. This file
  lifts that rationale to a tracked backlog item for parity with the
  `aws-credential-provider.md`, `azure-credential-provider-and-connect.md`, and
  `datastore-uri-credential-provider.md` deferrals.

## Why deferred

A003 is the promotion-boundary work (wiring component types onto the
credential-provider seam). Designing a registry-auth pair provider — including
artifactory's bimodal selection — is a distinct modeling exercise that would
front-run that design if attempted inside A003. It is deferred to its own future
session. No launch capability is blocked by deferring it.

Reference: `docs/project/adr/D-0026-credential-provider-abstraction.md`; D-0031
§DEFERRED (A003 promotion-boundary close-out).
