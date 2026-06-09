# D-0026 — Credential provider abstraction

Status: Accepted (design); launch scope buildable without an Accessor refactor.
Date: 2026-06-09
Supersedes: nothing. Builds on the security-architecture-direction record §9
(the one credential commitment already made) and decides the parts §9 deferred.
Verification basis: design rests on three read-only investigations against the
live tree, cited inline — credential-handling-current-state.md,
adapter-credential-refresh-tolerance.md, credential-design-assumptions-check.md.
Where this ADR states a fact about current code it carries a file:line from one
of those; everything else is design intent.

## Context

§9 of the security-architecture direction committed one thing and deferred the
rest to a dedicated session: credential resolution is a property of the target
component, resolved and applied at the guarded accessor seam that already decides
allow/deny — never ambient to a tool or adapter. A tool never holds, fetches, or
knows a credential. "Which credential" is a component property alongside "which
zone" (authz) and "which sensitivity class" (egress).

This ADR decides the deferred parts: the provider abstraction, the credential
type strategy, the launch-vs-deferred scope, and the instrumentation contract.
The method was threat-first: derive the credential-specific threat model, let the
requirements fall out, then shape the interface to the requirements and the
verified code.

## Verified ground truth (the design is built on these, not assumptions)

1. Credentials are not disconnected from the component model — they are carried by
   it. Every adapter receives its credential via Connect(ctx, store.Component) and
   reads it from the freeform Component.Config JSON (store/models.go:13). Uniform
   across ~25 adapters. A transparent encrypt-at-rest decorator
   (store/encrypted_components.go) AES-256-GCMs Config at rest but hands it back
   fully decrypted on every read.

2. No shared credential resolver/provider/factory exists today. The only shared
   things are the Connect convention and the encrypt-at-rest decorator.

3. Refresh tolerance is already good. For the ~18 HTTP adapters no credential is
   baked into the cached client — the secret is re-read from config per request.
   For k8s/AWS/ECR the cached SDK client holds a credential provider, not a value,
   and refreshes per request. Eager value-binding is real only for the
   connection-pool datastores. No adapter is RED; the hardest (postgres, mysql,
   mongodb, kafka) are YELLOW (reconstruct-the-pool, not architectural rework).

4. The k8s credential is a refreshing source, not a baked value
   (k8s.go:63,69,75; reused at resources.go:26,48). The adapter builds clients
   from a full *rest.Config and never extracts a token; an exec/in-cluster token
   rotates underneath the reused client inside client-go's transport. The k8s
   credential is NOT a value in the component record: Config for k8s decodes to
   {kubeconfig, context, in_cluster} only (k8s/config.go:9-13) — a pointer to a
   file, not credential material.

5. The authz seam holds only bare identifiers. Accessor.permit
   (access/access.go:120) receives (principals, sourceID, action); the Accessor
   struct (access.go:67-83) holds registry/graph/engine/auditRepo and NO component
   store handle. The seam resolves an adapters.Adapter by sourceID
   (access.go:206), never a store.Component. Per-operation callers pass a bare
   sourceID string (e.g. networking.go:13). The componentID key is in hand at the
   seam; the record is not.

## Threat model (credentials specifically)

Credentials have an attack surface the rest of the security model does not fully
cover. Four threats, each mapped to the verified surface and the requirement it
forces.

T1 — Credential theft / standing-token blast radius. Config holds static
long-lived secrets, encrypted at rest but decrypted on every read and held live
in connected adapters. A memory dump, key compromise, or store exfiltration
yields usable standing prod creds — the "agent holds a standing long-lived prod
token" finding the bank lens rejects.
=> R1 (reference-not-value): the store holds a credential reference / federation
config, not a value; the static secret is the degenerate case, not the base case.

T2 — Confused deputy. The seam decides allow/deny on (principal, componentID,
action); the credential the component carries has a scope unrelated to what the
principal was authorized to do. Passing authz for "may touch componentX" silently
confers componentX's full credential power.
=> R2 (scope-tracks-authz): resolution runs only post-authz, keyed strictly on the
authz'd componentID, no ambient fallback. Launch minimum: observation never
resolves a broader-than-read credential. Full per-zone scoping deferred.

T3 — Leakage through logs / audit / errors / LLM context. Verified live leaks:
the three /api/v1/components handlers return the full decrypted Config, and the
mongodb ping error interpolates cfg.URI (can embed user:password@host) at
mongodb.go:106. The LLM-context vector is the dangerous one — a resolved
credential entering prompt assembly is a one-way leak.
=> R3 (cannot-serialize): a resolved credential is a type that cannot be
serialized into responses, audit rows, errors, or prompt payloads.

T4 — Diagnosis blindness ("no Joe to troubleshoot Joe"). Connectivity and authz
failures are the hardest incidents and today collapse into one opaque failure: a
403 from a cluster could be wrong-component-keyed, exec-plugin-can't-reach-IdP,
expired token, wrong-audience mint, RBAC-denied-before-resolution, or backend
reject. The human must distinguish these cold, without an agent to help.
=> R4 (observable-per-stage): resolution reports which stage it reached as a typed
result, not an inferred string; success and failure both name their stage; and
observability never widens the leak surface (R3 and R4 are in tension and both
bind).

## Decision

### The resolved-credential type — two structurally separated halves

Resolution returns one typed result with two halves:

- A serializable diagnostic half: component identity, provider kind, credential
  audience, expiry timestamp if known, the stage reached, and a non-sensitive
  failure reason. This flows freely to logs, traces, and the UI.
- A non-serializable credential half: a means/source, not a value — for
  kubeconfig-exec the selected kubeconfig/context the adapter turns into a
  *rest.Config; for static the value in a non-serializable holder; for the
  deferred workload-identity case a federated token source. It has no String(),
  no JSON tag, no loggable representation. Only the seam/adapter consumes it.

This split is R1+R3 made into a type: the diagnostic half satisfies R4, the
credential half satisfies R3, and "means not value" satisfies R1.

### The stage enum — the diagnostic spine (R4)

provider-selected -> mint-attempted -> mint-succeeded -> connectivity-probed.

A failure result stops at the stage it failed and names a non-sensitive reason.
mint-succeeded WITHOUT connectivity-probed is a legal terminal success — the
lazy-connectivity posture ("minted, not yet proven; first real call will prove
it") expressed as a typed state rather than a side-effect.

For the launch providers, the later stages are OBSERVED outcomes, not Joe-driven
calls: k8s "mint" is client-go's transport, opaque to Joe. Joe reports the stage
it can see; it does not pretend to drive client-go's refresh. Exec-plugin failure
surfaces as a generic mint-failed with the plugin's raw stderr captured verbatim
for the human to paste into a downstream debugging agent — Joe does not parse the
plugin's internals. That captured stderr is untrusted, possibly-secret-bearing
text: it is surfaced to the human deliberately (the "paste this" affordance) and
is NOT swept into general trace/log emission, preserving R3.

### Launch model vs deferred model — the split, and why it exists

This split is the load-bearing decision, and Assumption-2-FALSE
(credential-design-assumptions-check.md) is the recorded reason it exists.

Launch model — provider-selects-the-source, credential stays adapter-resident.
The provider chooses WHICH credential source a component's adapter uses (which
kubeconfig/context; which static/env-var value), expressed as component config the
adapter already consumes. The credential never crosses the seam because it never
has to — it stays where it already is (HTTP adapters re-read per request; k8s
holds its *rest.Config). The seam keys on componentID (works today, access.go:120),
authz runs as stage 0 (works today), and no store.Component needs to reach the
Accessor. Buildable now with no Accessor signature change.

Deferred model — resolve-a-value-at-the-seam. The richer model where the seam
mints/fetches a credential value keyed on the authz'd component — needed for
ambient-workload-identity, per-zone credential scoping, and mutation-credential
separation. THIS is what requires a store.ComponentRepository handle on the
Accessor (one field on the struct access.go:67, one parameter on access.New
access.go:90, one updated call site server.go:59 — per-operation method signatures
unchanged). We design the provider interface to admit this so adding it is not a
rewrite, but we do not wire the seam dependency at launch.

The interface inversion this forces: the provider's primary operation is
"configure the adapter's credential source for this component," NOT "return a
credential value." Returning a value is the degenerate case the static provider
uses internally; the seam-resolved-value case is the deferred capability.

### The provider interface — three operations

Selected by a component property. Exposes exactly three operations.

Resolve — given the component's config, produce the credential source the adapter
will consume, plus the typed staged result. For static, the source is the wrapped
value. For kubeconfig-exec, the source is the selected kubeconfig/context (Resolve
selects WHICH *rest.Config the adapter builds; it does not build it). Reaches at
most provider-selected / mint-succeeded — never connectivity-probed, because
Resolve does not touch the backend.

Probe — given a resolved source, attempt the connectivity check against the
backend; return a staged result reaching connectivity-probed (success, or the
failure stage with non-sensitive reason; the captured-but-not-logged plugin stderr
lands here for the kubeconfig case). Probe is OPTIONAL to invoke — its separateness
is what makes lazy-connectivity ("minted, not yet proven") a legal state rather
than a side-effect. Eager verification calls Probe; lazy defers it to first real
call.

Describe — a pure, side-effect-free reporter: provider kind, and for a given
component the non-sensitive descriptor (provider kind, audience, context name,
expiry if known). Safe to call anytime. This is what the per-component UI status
surface renders as static config-derived fact, distinct from the live
last-resolution outcome — so the dashboard does not have to call Resolve to
populate itself.

Deliberately excluded, and why (records the boundary):
- No Refresh() — refresh is client-go's / the adapter's per-request re-read at
  launch; a Joe-driven Refresh is the deferred value-resolving model.
- No Rotate() — rotation orchestration is explicitly deferred.
- No store/seam dependency in the signature — Resolve takes the component config
  from wherever the provider is invoked (adapter Connect), consistent with
  Assumption-2-FALSE. Baking the store.ComponentRepository-at-the-seam dependency
  into the interface would make launch unbuildable-without-refactor.

### Invocation point

Resolve is invoked inside the adapter's Connect (where credentials already enter
the system per Connect(ctx, store.Component), verified ground truth #1). Minimal,
seam-free, ships now. Trade-off recorded: when the deferred resolve-value-at-seam
model lands it adds a SECOND call site at the Accessor rather than reusing this
one. Accepted — a small, later cost in exchange for a launch with no refactor.

### UI

A per-component authz/connectivity status surface rendering the stage outcomes:
green through connectivity-probed, or e.g. "kubeconfig-exec / mint failed / 02:14
/ <non-sensitive reason>" red at the failing stage, with the captured plugin
stderr available on-demand (not auto-logged). Describe supplies the static
descriptor; the last Resolve/Probe result supplies the live outcome. Sits next to
the existing admin/Users surface (project-knowledge §4.1).

## Launch scope (buildable now)

- Provider interface: Resolve / Probe / Describe, the two-half typed staged result,
  lazy-connectivity as a legal terminal state.
- Two providers: static/env-var (degenerate wrapped-value — covers GitHub/GitLab
  PAT, datastore URIs, HTTP backends with static tokens) and kubeconfig-exec
  (first-class, vendor-neutral, refresh-native via client-go, GREEN no adapter
  rewrite — the SRE-reaches-several-clusters case; AKS/EKS/GKE/kind all reached the
  same way, the per-target federation lives in that component's kubeconfig exec
  plugin, not in Joe).
- Per-component authz/connectivity status UI.
- Invocation inside adapter Connect; no Accessor signature change.

### GitHub dual-source credential precedence (unit 2, as built)

The GitHub adapter carries two possible credential sources: the provider
(selected by `credential_provider` in the component config, with `value` /
`env_var`) and the legacy `Config.Token` field. The wiring
(internal/adapters/github/adapter.go:86-100) fixes their precedence:

- Connect selects the provider, calls `Resolve`, and if the diagnostic is not OK
  it FAILS the connect (no silent fallback) — a configured provider that cannot
  resolve is an error, not a downgrade to the legacy token.
- On a successful resolve, a non-empty `StaticValue()` OVERRIDES `Config.Token`
  (provider value wins).
- A component with no discriminator selects the static provider, which yields no
  value; `Config.Token` is then left untouched and serves as the fallback — this
  is what preserves existing token-only configs unchanged.

So: provider-resolved value wins; the legacy `token` field is the fallback that
preserves existing configs; a provider that is configured but fails to resolve
fails the connect rather than falling back. Recorded as a decision, not an
accident of wiring order.

## Deferred (designed-for, not built)

- Ambient-workload-identity provider (the explicit, opt-in "use the runtime's
  projected identity to federate" case — AKS workload identity / IRSA for a target
  that trusts Joe's home identity). MUST be opt-in per component, NEVER a default:
  defaulting it lets Joe's home-cluster identity become the ambient credential for
  everything, reintroducing the standing-broad-credential blast radius through the
  back door. The azure adapter Connect is a skeleton today (azure.go:107) — it
  stays a skeleton; this provider is not launch scope.
- The resolve-value-at-the-seam model and its store.ComponentRepository-on-Accessor
  dependency.
- Rotation orchestration.
- Per-zone credential scoping.
- Mutation-credential separation.
- YELLOW datastore pool-reconstruction (postgres/mysql/mongodb/kafka).

## Documented gaps (record as DECISIONS notes / issues; some may be fix-before-launch)

- Kafka parses Username/Password/SASLMechanism but never applies them — the broker
  client is currently unauthenticated (kafka.go:163-166). Security finding: an
  operator who configured SASL believes auth is on; it is not. Wants at minimum a
  DECISIONS note; arguably fix-before-launch.
- Azure Connect is a skeleton that stores config but builds no SDK client
  (azure.go:107). Honest-incomplete; tied to the deferred ambient-WI provider.
- /api/v1/components returns the full decrypted Config from three handlers, and the
  mongodb ping error interpolates cfg.URI (mongodb.go:106). Both are live credential
  leaks (T3). Arguably fix-before-launch, since this ADR adds trace/log surface to
  exactly this path.
- Component-management paths call adapter.Connect DIRECTLY, bypassing the
  permit/guard seam (components.go:181, webui.go:678). An existing authz gap —
  connecting a component (which triggers the boot probe, and under the deferred
  model would trigger credential minting) does not go through permit. Same family as
  the §6 "single audited writer" concern. Out of scope for this ADR; flag as issue.

## Invariants this ADR establishes

- Strict componentID keying: a credential is resolved only from the exact component
  the authz decision was keyed on. No ambient fallback. No "Joe's home identity"
  default ever substituting for a target credential.
- Cannot-serialize: the credential half of the resolved-credential type has no
  loggable/serializable representation, structurally separated from the diagnostic
  half. Holds even as this ADR adds observability surface to the resolution path.
- Short-lived is the base shape, static is the degenerate case — never the reverse.
  Joe's resolution code does not branch on static-vs-refreshing; the provider
  encapsulates the difference. That uniformity is the safety property.
