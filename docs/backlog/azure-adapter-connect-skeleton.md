# Backlog — Azure adapter Connect is a skeleton (no SDK client built)

Status: deferred (recorded by D-0026; tied to the deferred ambient-workload-identity provider — do not build it as part of D-0026).
Priority: later

## Context

`internal/adapters/azure/azure.go` `Connect` parses and stores the component
config but builds no Azure SDK client and performs no connectivity check
(`azure.go:107` — the method ends at `a.config = cfg` / `a.connected = true`).
It is honest-incomplete: the adapter advertises a connection it has not made.

D-0026 (`docs/project/adr/D-0026-credential-provider-abstraction.md`) records this
under "Documented gaps" and ties it to the **deferred** ambient-workload-identity
provider: the Azure path is exactly the explicit, opt-in "use the runtime's
projected identity to federate" case (AKS workload identity) that the ADR places
out of launch scope. Per the ADR, the azure adapter "stays a skeleton" at launch.

## Proposed future work

When the deferred ambient-workload-identity provider is built, give the azure
adapter a real `Connect` that constructs an SDK client through that provider
(opt-in per component, never a default — see the D-0026 invariant that Joe's home
identity must never silently become the ambient credential for everything).

## Why deferred

The ambient-WI provider is explicitly deferred by D-0026; building the azure SDK
client now would either hardcode a credential path the ADR rejects or front-run
the provider design. No launch capability depends on the azure adapter today.
