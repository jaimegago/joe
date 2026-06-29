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

1. **Produce the full ADR** for the stance — the normative decision record that promotes
   D-0060's design-of-record into an implementable specification.
2. **Rewire the Kubernetes credential path** off the kubeconfig-or-in-cluster locator
   (`KindKubeconfigExec`) to the **static bearer** method.
3. **Add the native Entra exchange** method (Azure Entra OAuth2 token exchange minting a
   short-lived bearer token for AKS).
4. **Verify per-component environment-variable keying** in the credential references so
   two same-type components cannot collide on the same env var.
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
