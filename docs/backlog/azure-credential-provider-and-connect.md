# Backlog — Azure credential provider + Connect implementation (deferred from A003)

Status: deferred from A003 (promotion-boundary work) to its own dedicated design
session.

## Problem

The `azure` component type (`ComponentTypeAzure`, `internal/store/constants.go`)
is blocked from promotion under the credential-reference model by **two** missing
pieces, not one:

1. **Skeleton Connect.** `internal/adapters/azure/azure.go` `Connect` parses and
   stores the component config and then ends at `a.config = cfg` /
   `a.connected = true` (`azure.go:107-108`). It builds no Azure SDK client and
   performs no connectivity check — it advertises a connection it has not made.
2. **No Azure-shaped credential provider.** The credential seam
   (`internal/credential/provider.go`) has only `StaticProvider`
   (`internal/credential/static.go`) and `KubeconfigExecProvider`
   (`internal/credential/kubeconfig_exec.go`). Neither models Azure credentials.

## Why the existing providers do not fit

`StaticProvider`'s `staticConfig` (`internal/credential/static.go`) is
token-shaped: an inline `value` or an `env_var` name, resolving to a single
opaque string. Azure authentication is not a single bearer token — it spans
client-secret / certificate service principals, managed identity, and (for AKS)
federated workload identity. There is no field or resolution path in
`StaticProvider` for a tenant/client/secret triple or for ambient managed
identity, and `KubeconfigExecProvider` is kubernetes-specific.

## Open question — azuremonitor's auth path (UNVERIFIED)

`azuremonitor` exists only as a component-type constant
(`ComponentTypeAzureMonitor`, `internal/store/constants.go:19`). **No adapter
implementation exists** for it under `internal/adapters/`, so its credential path
cannot be read from code. Whether `azuremonitor` authenticates via Azure
credentials (and therefore shares this provider's blocker) or via a separate
token (and therefore belongs with the static/token path) is an **open question**
that must be settled when the adapter is built — it is not asserted here.

## Promotion impact

`azure` **cannot be promoted under the credential-reference model** until both a
real `Connect` and an Azure-shaped credential provider exist. `azuremonitor`'s
promotion path depends on resolving the open question above.

## Why deferred

A003 is the promotion-boundary work. Azure needs its **own dedicated design
session** covering both the Connect implementation (which Azure SDK client, what
connectivity probe) and the credential-provider shape (which Azure auth modes the
provider resolves). Both are distinct design exercises that would front-run their
own decisions if attempted inside A003.

Related: `docs/backlog/azure-adapter-connect-skeleton.md` records the Connect
skeleton against D-0026's deferred ambient-workload-identity provider; this file
records the same gap from the A003 promotion-boundary angle plus the missing
credential provider.

Reference: `docs/decisions/D-0026-credential-provider-abstraction.md`.
