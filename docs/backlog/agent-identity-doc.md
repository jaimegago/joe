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

Still open:

1. **Produce the full ADR** for the stance — the normative decision record that promotes
   D-0060's design-of-record into an implementable specification.
2. **Add the native Entra exchange** method (Azure Entra OAuth2 token exchange minting a
   short-lived bearer token for AKS) — slice C. Extends the per-component
   `auth_method`→Kind seam established in slice B with a second value, no field migration.
3. **Retire the kubeconfig-exec provider** for kubernetes once the static-bearer and
   Entra-exchange methods land — slice D. Removes the dead-but-present
   `internal/credential/kubeconfig_exec.go` provider, its `KindKubeconfigExec` Kind and
   requirements entry, and the `tildeguard` helper, and lets the break-test widen.
4. **Full both-methods public-docs polish** (deferred from slice B, tied to slice C). Slice
   B made a minimal truthful accuracy fix to the kubernetes credential path in
   `docs/public/{guides/register-kubernetes,integrations/_index,api-reference/_index,quickstart/_index,concepts/components-and-promotion}.md`,
   but the fuller prose describing **both** static-bearer and Entra-exchange must not be
   written until Entra exists (slice C). Reconcile the docs to describe both methods then.
5. **Implement the provenance assertion** — the Joe-internal originator/actor/action
   record (delegated vs. autonomous), never transmitted to the managed system.
6. **Publish the doc**: relocate `docs/drafts/agent-identity.md` into the Concepts
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
