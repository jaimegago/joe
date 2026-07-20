CRD GVR resolution in the on-demand core tools (deferred from crd-gvr-resolution / D-0094)
Status: open
Priority: later

D-0094 fixed the CRD refresher's `crdRefreshSpecs`: the strings were in the
never-resolving `resource.group` form (e.g. `scaledobjects.keda.sh`) while
`k8s.ResolveGVR` (`internal/adapters/k8s/resolve.go`) only accepts a known short
name or the 3-part `group/version/resource` form. The version is mandatory
because the dynamic client addresses `/apis/{group}/{version}/{resource}`
directly with no discovery-client lookup. The refresher specs now carry
`group/version/resource` and are pinned by `TestCRDRefreshSpecsResolveGVR`.

The **same latent bug remains** in the on-demand core CRD tools under
`internal/tools/core/`, which were out of scope for the refresher fix:

- `keda_tools.go` — `KEDACRDTypes` (`scaledobjects.keda.sh`, `scaledjobs.keda.sh`,
  `triggerauthentications.keda.sh`)
- `cilium_tools.go` — `CiliumCRDTypes` (`ciliumnetworkpolicies.cilium.io`,
  `ciliumclusterwidenetworkpolicies.cilium.io`, `ciliumendpoints.cilium.io`)
- `certmanager_tools.go` — `CertManagerCertCRDTypes`, `CertManagerIssuerCRDTypes`
- `opa_tools.go` — `OPACRDTypes` and dynamically-built constraint resource names
- `istio_tools.go` — Istio CRD names
- `crossplane_tools.go` — `CrossplaneProviderCRDTypes`,
  `CrossplaneCompositionCRDTypes`
- `flux_tools.go` — Flux CRD names

Each passes its `resource.group` string through `K8sListResources` /
`K8sGetResource` → the accessor → `adapter.ListResources` → `ResolveGVR`, which
fails resolution; the tool's `err != nil` path then skips or returns empty, so
these on-demand CRD listings have likewise never resolved. Because these tools
run on the user task loop and swallow the error, the failure is silent rather
than fatal.

Work to do:

- Convert every core-tool CRD identifier to `group/version/resource` form (pick a
  served version per CRD, as the refresher did), OR extend the resolution layer
  so a `resource.group` string is resolved to a GVR via a discovery-client
  preferred-version lookup — the latter is the more general fix and would let
  both surfaces keep the shorter `resource.group` spelling, but adds a discovery
  round-trip and a discovery client the transport does not currently hold.
- Add a structural test analogous to `TestCRDRefreshSpecsResolveGVR` over the
  core-tool CRD-type maps so a wrong-form addition fails tests.
- Some tools build resource names dynamically (e.g. `opa_tools.go` constraint
  kinds, `istio_tools.go`); those code paths need their own resolution handling,
  not just a constant-map rewrite.

Origin: read-only investigation during the crd-gvr-resolution session; see
D-0094 for the refresher-side fix and the resolver contract.
