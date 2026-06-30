# Agent identity and authentication — explanation doc and the implementation it trickles into
Status: open

A held draft of Joe's agent-identity and authentication stance was authored this
session (D-0060) and is held, deliberately non-published, at
`docs/drafts/agent-identity.md`. The draft is design-of-record; parts of it are not
yet implemented, so it is staged outside `docs/public` (the sole published surface,
D-0052) until the implementing work lands.

## The stance, in brief

An agent is a **third identity class** — neither a human (authenticates assuming
presence) nor a service (authenticates on fixed deployment scope) — so its safety must
come from a mediation layer it enforces on itself. Every action carries **provenance**:
*delegated* (a human originated it; originator = human, actor = Joe) or *autonomous*
(Joe originated it; originator and actor both Joe). Joe is always the actor on the wire;
only the originator varies; provenance is orthogonal to read-vs-mutate.

Joe **authenticates only as its own non-human identity**, never the human path; **never
ingests a human's kubeconfig**; **never impersonates** (never replaces its identity with
another). When a human is the originator, that human is recorded **only inside Joe** as a
provenance assertion (originator, actor, action) derived from the session's creator
principal, and is never transmitted to the managed system, which sees only Joe's own
service identity. Three planes are never collapsed — identity, provenance, governance —
under the invariant that a valid credential never implies a permitted action.

The Kubernetes **target** is two transport methods, both native and both non-human: a
**static bearer** method (long-lived bearer token as an `Authorization: Bearer` header —
OpenShift, and self-managed/local clusters via a ServiceAccount token) and a native
**Entra exchange** method (Joe mints a short-lived bearer token via an Azure Entra OAuth2
exchange, for AKS). **Client-certificate authentication is permanently excluded** as a
matter of stance — it is a human authentication path.

## Current shipped state (what this supersedes later)

Not yet implemented. The shipped Kubernetes credential path remains the
**kubeconfig-or-in-cluster locator**: the `kubeconfig-exec` provider
(`KindKubeconfigExec`, `internal/credential/credential.go:40`) wired to `kubernetes` at
`internal/credential/wiring.go:46`, resolving an in-cluster service account or a
kubeconfig file (`internal/credential/kubeconfig_exec.go`), consumed by the adapter at
`internal/adapters/k8s/k8s.go:131`. Established by D-0026; current-state re-confirmed by
D-0059.

## Deferred implementation work (what this trickles into)

- **DONE — slice A, per-component env-var uniqueness keying (D-0061, session
  `agent-identity-doc-01`).** Static credential env-var names are now operator-supplied,
  stored verbatim, and **enforced unique per component** at the promotion seam
  (`staticEnvVarConflict` in `internal/api/components.go`), so two components can no longer
  collide on the same env var. This is the foundation the later static-bearer Kubernetes
  method depends on (a unique per-component token variable). Break-tested by
  `TestPromote_StaticEnvVarUniqueness`. Recon confirmed names were already stored verbatim
  (no computed-name switch and no migration were needed).
- **DONE — slice B, Kubernetes transport rewrite to static-bearer (D-0062, session
  `agent-identity-doc-02`).** The kubernetes adapter builds its `*rest.Config` by hand with
  no kubeconfig ingestion (`buildRESTConfig`), `static-bearer` is its own credential Kind
  (`KindStaticBearer`) with env-var and in-cluster token sources, in-cluster reads the
  pod-mounted token directly (not via `rest.InClusterConfig()`), `auth_method` is a stored
  per-component discriminator establishing the per-component Kind-selection seam, kubernetes
  is un-wired from `KindKubeconfigExec` (the provider package left dead-but-present for
  slice D), and the uniqueness guard is generalized to all `env_var` locators. The
  promotion UI form and a minimal truthful public-docs accuracy fix landed in-slice;
  break-tested by `internal/adapters/k8s/transport_break_test.go` +
  `internal/credential/static_bearer_test.go`.
- **DONE — slice C, native Entra exchange (D-0063, session `agent-identity-doc-03`).**
  `KindEntraExchange` (`internal/credential/entra_exchange.go`) is the second kubernetes
  auth method, exercising the per-component `auth_method`→Kind seam with a real second value
  (no field migration). Its provider mints a short-lived bearer token via an Azure Entra
  OAuth2 client-credentials exchange using the already-vendored
  `golang.org/x/oauth2/clientcredentials` (no new dependency, no Azure SDK); tenant id,
  client id, audience/scope, and the client-secret reference are all per-resolution config
  values, and the provider imports no kubernetes or Azure-SDK symbol (transport-agnostic,
  reusable by the deferred Azure track — pinned by an AST imports-only break-test). The
  client secret is referenced under a distinct `client_secret_env_var` field, intentionally
  exempt from the env-var uniqueness guard so a shared Azure app registration is allowed.
  The `BearerToken()` accessor was generalized to a bearer-Kind set so the minted token
  rides the identical adapter consume-seam; the promotion boundary learned `auth_method`→Kind
  dispatch for kubernetes. Federated workload-identity assertion is designed-for as an
  additive source (`federated_token_file` reserved) but not built. The Entra promotion UI
  and the full both-methods public-docs polish landed in-slice.

Still open:

1. **Produce the full ADR** for the stance — the normative decision record that promotes
   D-0060's design-of-record into an implementable specification.
2. **Retire the kubeconfig-exec provider** for kubernetes now that the static-bearer and
   Entra-exchange methods have landed — slice D. Removes the dead-but-present
   `internal/credential/kubeconfig_exec.go` provider, its `KindKubeconfigExec` Kind and
   requirements entry, and the `tildeguard` helper, and lets the break-test widen.
3. **Add the federated workload-identity assertion source** for entra-exchange — the
   additive second credential source designed-for in slice C (`federated_token_file`
   reserved under the at-least-one-of constraint) but not built, so AKS workload identity
   needs no client secret.
4. **Implement the provenance assertion** — the Joe-internal originator/actor/action
   record (delegated vs. autonomous), never transmitted to the managed system.
5. **Publish the doc**: relocate `docs/drafts/agent-identity.md` into the Concepts
   section as a single explanation page, wired to the component-registration guide and to
   Integrations.

## Intended Concepts destination and cross-link targets

- **Destination:** `docs/public/concepts/agent-identity.md` — a single Concepts
  explanation page (Concepts is explanation-only; weight-ordered in increments of ten,
  slotted near `principals-and-identity.md` / `components-and-promotion.md`).
- **Cross-link targets on publication:** the component-registration guide
  (`docs/public/guides/register-kubernetes.md`) and the Integrations section
  (`docs/public/integrations/`); and reciprocal links from
  `docs/public/concepts/principals-and-identity.md` and
  `docs/public/concepts/components-and-promotion.md`.

This thread stays **open** — the draft is held, not published, and the implementation
above remains undone — so this file does not move to `done/`.
