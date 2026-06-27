# Credential-provider design — assumptions check

**Date:** 2026-06-09
**Scope:** Read-only verification of two assumptions underpinning a proposed
credential-provider ADR. Re-derived against the live tree; prior investigations,
DECISIONS.md, and the prompt's own framing were treated as claims to test, not
ground truth. Every concrete claim cites `file:line`.

---

## ASSUMPTION 1 — the kubeconfig/exec credential is consumable as a per-request credential SOURCE, not only a value baked at client construction.

### a. Where the client is constructed, and from what

The Kubernetes adapter builds its client-go clients in `Adapter.Connect`:

- The credential input is a `*rest.Config`, produced by `buildRESTConfig(cfg)`
  at `internal/adapters/k8s/k8s.go:63`.
- `buildRESTConfig` (`internal/adapters/k8s/k8s.go:112-132`) has two branches:
  - **In-cluster:** `rest.InClusterConfig()` (`k8s.go:114`).
  - **Kubeconfig:** `clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()`
    (`k8s.go:131`), where `rules` is a
    `clientcmd.NewDefaultClientConfigLoadingRules()` with an optional explicit
    path (`k8s.go:117-124`) and `overrides` carries an optional context
    (`k8s.go:126-129`).
- From that single `*rest.Config`, two clients are built **once** at connect time:
  - `dynamic.NewForConfig(restConfig)` → `a.dynClient` (`k8s.go:69`, stored `k8s.go:73`)
  - `kubernetes.NewForConfig(restConfig)` → `a.clientset` (`k8s.go:75`, stored `k8s.go:79`)
- The `*rest.Config` itself is also retained on the adapter (`a.restConfig`,
  `k8s.go:67`; field `k8s.go:32`).

Constructors/types in use: `rest.Config`, `rest.InClusterConfig`,
`clientcmd.NewDefaultClientConfigLoadingRules`, `clientcmd.ConfigOverrides`,
`clientcmd.NewNonInteractiveDeferredLoadingClientConfig(...).ClientConfig()`,
`dynamic.NewForConfig` (returns `dynamic.Interface`),
`kubernetes.NewForConfig` (returns `kubernetes.Interface`).

### b. Token SOURCE vs. single captured token string

The adapter carries a **source**, not a captured token string:

- It hands the full `*rest.Config` to `NewForConfig` (`k8s.go:69,75`) and never
  extracts, copies, or pins a bearer token. A targeted search for any token
  handling in the adapter package — `BearerToken`, `Token`, `ExecProvider`,
  `AuthProvider`, `TokenSource`, `RoundTrip`, `Transport`, `WrapTransport` —
  returns **zero matches** in `internal/adapters/k8s/` (non-test). The adapter
  does no credential manipulation of its own.
- Because the full `*rest.Config` is preserved, whatever auth mechanism the
  kubeconfig declares survives into the client-go transport: an `exec:` provider
  stanza, an `auth-provider`, or a `BearerTokenFile` (the in-cluster case sets a
  projected-token file). client-go's transport refreshes those on its own
  (exec credential plugins are re-minted on expiry; token files are re-read).

So the deciding fact is structural: **Joe passes a `*rest.Config` and reuses the
client; it never flattens the credential to a fixed token.** (`k8s.go:63,67,69,75`)

### c. What the code actually does at a representative operation

A representative read op is `ListResources` (`internal/adapters/k8s/resources.go:13-32`)
/ `GetResource` (`resources.go:35-54`). Both use the **stored** client
`a.dynClient.Resource(gvr)...` (`resources.go:26`, `resources.go:48`). There is
no per-request rebuild of the client or config — the client created at
`Connect` (`k8s.go:69`) is reused for the life of the connection.

What the code itself does at op time: nothing credential-related. It calls the
cached client. Any token refresh that occurs is performed **inside client-go's
transport**, not by Joe code, and only happens if the kubeconfig's auth mode is
a refreshing one (exec / token-file / in-cluster). A kubeconfig with a static
`token:` would present a fixed token forever — but even then Joe never "baked"
it at construction; it is read through the `*rest.Config` each transport uses.

Note: `Connect` makes one liveness call, `clientset.Discovery().ServerVersion()`
(`k8s.go:82`), once; this is not on the per-request op path.

### d. VERDICT — Assumption 1: **TRUE** (with scope)

The k8s credential is held as a refreshing source, not a value baked at client
construction. **Deciding evidence:** `internal/adapters/k8s/k8s.go:63,69,75` —
the adapter builds clients from a full `*rest.Config` and reuses them
(`resources.go:26,48`); it never extracts a token string (zero token-handling
matches in `internal/adapters/k8s/`). For exec-provider and in-cluster
kubeconfigs, a short-lived token therefore rotates underneath the reused client
with no client rebuild — exactly the property the design wants.

**Two scoping caveats the design must hold in mind:**

1. The refresh is performed **entirely by client-go's transport**, not by any
   Joe code. If the design intends *Joe* to actively pull/inject a current token
   per request, that code does not exist (`internal/adapters/k8s/` has no token
   handling). What exists is "reuse the client, let client-go rotate underneath."
2. Refresh is **contingent on the kubeconfig's auth mode.** Exec / token-file /
   in-cluster → refreshes. A static-token kubeconfig → fixed. The adapter does
   nothing to guarantee one over the other.

**Contradiction to surface (affects the design's premise, not Assumption 1's
verdict):** the k8s credential does **not live in the component record** in any
token form. `store.Component` carries config only in `Config json.RawMessage`
(`internal/store/models.go:13`), and for k8s that decodes to
`{kubeconfig, context, in_cluster}` only (`internal/adapters/k8s/config.go:9-13`)
— a *path/flag pointer*, not credential material. The actual credential is in a
kubeconfig file on disk (or the in-cluster SA token file), dereferenced by
client-go and never surfaced to Joe. Any design that speaks of "resolving the
k8s credential value" from the component record has a category mismatch: there
is no token value there to resolve for the k8s adapter.

---

## ASSUMPTION 2 — the access/authz seam can reach a resolved component record (carrying Config) at the decision point without a new store handle or signature change.

### a. The decision point and its current signature

The single enforcement chokepoint is `Accessor.permit`
(`internal/access/access.go:120`):

```
func (a *Accessor) permit(ctx context.Context, principals rbac.PrincipalSet, sourceID string, action rbac.Action) error
```

It is reached via `permitForPrincipal(ctx, principal rbac.Principal, sourceID string, action rbac.Action)`
(`access.go:180`) and, for adapter dispatch, via the generic
`guard[T](a, ctx, principal, sourceID, action, typeName)` (`access.go:194-203`),
which calls `permitForPrincipal` first (`access.go:203`).

Inputs at the decision point are **bare identifiers only**: a `rbac.Principal`
(lifted to a `rbac.PrincipalSet`), a `sourceID string`, and an `rbac.Action`.

The `Accessor` struct holds exactly four fields (`access.go:67-83`):
`registry *adapters.Registry`, `graph graph.GraphStore`,
`engine *rbac.PolicyEngine`, `auditRepo audit.Repository`. **There is no
component store handle and no component record** anywhere on the Accessor.
Construction confirms this — `access.New` takes only those four
(`access.go:90-92`), and the production wiring passes only those four:
`access.New(services.Adapters, services.Graph, newPolicyEngine(services), auditRepo)`
(`internal/api/server.go:59`).

### b. Is a full `store.Component` reachable at the decision point?

**No.** After permit, `guard` resolves an **adapter**, not a component:
`a.registry.Get(sourceID)` (`access.go:206`) returns an `adapters.Adapter`
(`internal/adapters/registry.go:25-34`). The registry is a
`map[string]Adapter` keyed by sourceID (`registry.go:12-15`); it stores no
`store.Component`. The `Adapter` interface exposes only
`Connect/Disconnect/Status` (`internal/adapters/adapter.go:10-20`) and does not
expose the `store.Component` it was connected with. The k8s adapter, for its
part, keeps only the parsed `config Config` after Connect
(`internal/adapters/k8s/k8s.go:31`, set at `k8s.go:61`), discarding the rest of
the `store.Component`.

Obtaining a `store.Component` (which carries `Config`,
`internal/store/models.go:9-19`) at this point would therefore require a **new
store lookup via a dependency the Accessor does not have** — a
`store.ComponentRepository` handle — or a **signature/interface change** to make
the adapter expose its record.

### c. Does the caller already hold a resolved component record on this path?

**No, not on the guarded per-operation path.** The HTTP handlers that call the
accessor pass a bare `sourceID` string and never load a `store.Component`:

- `networking.go:20` — `sourceID := r.PathValue("componentID")`, then
  `s.accessor.NginxListIngresses(..., sourceID, ...)` (`networking.go:25`). Same
  shape across registry/security/observe handlers (e.g. `observe.go:303-320`,
  `internal/api/registry.go`, `internal/api/security.go`).

A `store.Component` **is** loaded on the **component-management** paths, but
those do **not** go through the accessor seam:

- `internal/api/components.go:158` (`Components.Get`) → `adapter.Connect(ctx, *source)`
  at `components.go:181`.
- `internal/api/webui.go:658` (`Components.Get`) → `adapter.Connect(ctx, *src)`
  at `webui.go:678`.

Both call `adapter.Connect` **directly**, bypassing `permit`/`guard`. So the
only places a component record is in hand are off the authz'd-operation path; on
the authz'd path neither the caller nor the accessor holds one.

### d. VERDICT — Assumption 2: **FALSE**

At the authz decision point, neither the Accessor (`access.go:67-83`,
`access.go:120`) nor its per-operation callers (`networking.go:20`,
`observe.go:303-320`) hold a `store.Component`, and the seam reaches only an
`adapters.Adapter` (`access.go:206`), which does not expose the record. A
resolved component record carrying `Config` cannot be reached or trivially
obtained without introducing a new dependency.

One sub-part of the assumption *is* true and worth keeping: the **componentID
the decision is keyed on is in hand** at the seam — it is the `sourceID`
argument to `permit`/`guard` (`access.go:120,194`), the same string used for the
registry lookup. So keying credential resolution on the authz'd componentID is
well-defined; what is missing is the record itself.

**Smallest change that would make it true (stated as a requirement):** Give the
`Accessor` a `store.ComponentRepository` handle at construction (one new field
on the struct `access.go:67`, one new parameter on `access.New` `access.go:90`,
one updated call site `internal/api/server.go:59`), so that inside
`permit`/`guard` it can load the `store.Component` by the same `sourceID` it
already authorizes on. This is precisely the "new store handle" the assumption
hoped to avoid; the per-operation method signatures need not change.

(Alternative requirement, also a change the assumption hoped to avoid: widen the
`adapters.Adapter` interface — `internal/adapters/adapter.go:10` — to expose the
`store.Component`/credential it received at `Connect`. That is an
interface/signature change touching every adapter.)

**Combined design implication:** even after the Accessor can load the
`store.Component`, the k8s credential is *not in that record* (see Assumption 1
contradiction): `Config` holds only a kubeconfig path/context/in-cluster flag
(`config.go:9-13`). For Kubernetes specifically, "resolve the credential at the
authz seam keyed on componentID" yields a pointer to a file, not a token — the
token resolution still happens inside client-go, not at the seam.

---

## Summary

| Assumption | Verdict | Deciding file:line |
|---|---|---|
| **1** — k8s credential is a refreshing per-request source, not a baked value | **TRUE** (scoped: refresh is client-go's, contingent on kubeconfig auth mode) | `internal/adapters/k8s/k8s.go:63,69,75`; reused at `resources.go:26,48`; no token handling in `internal/adapters/k8s/` |
| **2** — authz seam can reach a resolved component record without a new store handle/signature change | **FALSE** | `internal/access/access.go:67-83,120,206`; callers pass bare `sourceID` (`internal/api/networking.go:20`); fix needs a `store.ComponentRepository` on the Accessor (`access.go:90`, `internal/api/server.go:59`) |
