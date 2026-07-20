# Backlog — AWS-shaped credential provider (deferred from A003)

Status: deferred from A003 (promotion-boundary work) to its own future design session.
Priority: later

## Problem

The credential-provider seam (`internal/credential/provider.go`, the `Provider`
interface with `Resolve` / `Probe` / `Describe`) currently has two concrete
providers: `StaticProvider` (`internal/credential/static.go`) for single-token /
env-var credentials, and `KubeconfigExecProvider`
(`internal/credential/kubeconfig_exec.go`) for kubernetes. No AWS-shaped provider
exists.

The AWS-typed component types — `cloudwatch` (`ComponentTypeCloudWatch`),
`ecr` (`ComponentTypeECR`), and the `aws` type itself (`ComponentTypeAWS`,
`internal/store/constants.go`) — do not authenticate with a single static bearer
token. They authenticate via the **AWS credential chain**: IAM role, static
access keys, EC2/instance profile, or an assumed role (STS). The existing `aws`
adapter already does this directly from its component config rather than through
the seam — it loads the chain via `config.LoadDefaultConfig`, `credentials`, and
`stscreds` and resolves a `role_arn` (`internal/adapters/aws/aws.go`, the
`Config` struct around line 68 and `Connect` around line 185).

## Why the existing providers do not fit

`StaticProvider`'s credential model is token-shaped: its `staticConfig` struct
(`internal/credential/static.go`) holds only an inline `value` or an `env_var`
name. It cannot represent an AWS credential chain — there is no field for a role
ARN, an access-key/secret-key pair, an instance-profile selection, or
assume-role parameters, and `Resolve` returns a single opaque string rather than
a chain the AWS SDK can consume. `KubeconfigExecProvider` is kubernetes-specific.

## What the provider must resolve (captured, not designed here)

A future AWS-shaped provider implementing `Resolve` / `Probe` / `Describe` must
resolve the **AWS credential chain shape**, not a single token:

- IAM role assumption (role ARN + STS assume-role).
- Static access key / secret key (and optional session token).
- EC2/ECS instance-profile / ambient-environment credentials.
- The precedence/selection between these (the "chain").

This file captures only *what must be resolved*. The actual provider design —
config schema, which `Resolution` stages apply, how `Probe` proves connectivity
without leaking secrets — is out of scope here and needs its own design session.

## Promotion impact

`cloudwatch` and `ecr` (and the `aws` type) **cannot be promoted under the
credential-reference model** until an AWS-shaped provider exists. Until then their
authentication stays adapter-local and outside the provider seam.

## Why deferred

A003 is the promotion-boundary work (wiring component types onto the
credential-provider seam). Designing an AWS credential-chain provider is a
distinct modeling exercise that would front-run that design if attempted inside
A003. No launch capability is blocked by deferring it.

Reference: `docs/project/adr/D-0026-credential-provider-abstraction.md`.
