# Joe — Identity Phase A implementation prompt (executed)

This is the verbatim implementation prompt used for Phase A of the identity
refactor. It is preserved per the phase-plan workflow ("each phase is a
separate CC session with its own implementation prompt"). The result is
recorded in `DECISIONS.md` D-0004; the progress tracker in
`joe-identity-phase-plan.md` marks Phase A complete.

---

Read docs/joe-identity-design.md and docs/joe-identity-phase-plan.md in this repo before starting. You are implementing Phase A only. Do not implement any later phase. Do not change observable behavior.

GOAL
Introduce a single guarded accessor that is the only path to infrastructure adapters and the graph store. Move RBAC enforcement off the HTTP transport and into that accessor. Leave the system single-principal and behavior-preserving — this phase changes structure and enforcement location, not outcomes.

BACKGROUND FROM THE CODE (verify against current code; these are from a prior read-only investigation)
- IsAllowed is currently called in exactly one production location: EnforcementMiddleware on the HTTP transport. No adapter, tool, graph store, or executor calls it.
- HTTP handlers resolve an adapter via services.Adapters.Get(sourceID) and call its methods directly; the graph handler calls services.Graph directly. Adapter methods take plain Go types (ctx + scalars), no HTTP types.
- Action is currently inferred from HTTP method in middleware (actionFromRequest) and cannot even express the "query" action.

REQUIREMENTS
1. Create a guarded accessor: the single seam through which all infrastructure-adapter and graph-store access must flow. Its decision input is the principal, the source identifier, and the action. On a permitted decision it delegates to the resolved adapter or graph store and returns the result. On a denied decision it returns a typed permission error and performs no infrastructure call.
2. IsAllowed is evaluated inside the accessor. The accessor is the authoritative enforcement point. The existing HTTP EnforcementMiddleware may remain as a coarse outer gate but must no longer be the only enforcement point.
3. The action for each call is declared as a property of the adapter method itself (read / query / mutate / delete), not inferred from the HTTP verb. The accessor reads the action from that declaration. Choose a mechanism that keeps the action declaration adjacent to the method it describes.
4. Route every existing HTTP handler that currently calls an adapter or the graph store directly so that it instead goes through the accessor. Handlers keep doing request parsing and JSON writing; they stop reaching adapters/graph directly.
5. This phase remains single-principal. The principal the accessor evaluates is whatever the current identity resolution already produces (the configured principal). Do not add OIDC, sessions, principal sets, or service-account keys — those are later phases. Do not remove the loopback — that is Phase E and must not happen before enforcement evaluates the real principal.

CONSTRAINTS
- Observable behavior must be unchanged: the same request that returns 200 or 403 today must return the same today after this phase, for the single configured principal.
- Do not introduce any second path to an adapter or the graph store. After this phase there must be exactly one way to reach them.

ACCEPTANCE — write these tests and make them pass. Tests are the contract.
Static / architectural tests (assert structure, not runtime):
- A test that parses the codebase (AST or import-graph) and asserts that no package other than the accessor package imports or calls the infrastructure adapter packages, services.Adapters.Get, or services.Graph directly. This is the load-bearing invariant: there is no ungoverned path to an adapter. Model it on the existing incident-exit AST guard already in the repo.
- A test asserting every adapter method dispatchable through the accessor declares an action.
Behavioral tests (assert runtime outcomes):
- For each infrastructure kind (k8s, prometheus, git, argocd, loki) and for the graph store: a principal with a matching grant is allowed through the accessor; a principal without a matching grant is denied, and on denial no infrastructure call occurs.
- A regression test demonstrating that the HTTP path produces the same allow/deny outcomes after this phase as before it, for the single configured principal.

DELIVERABLES
- The implementation.
- The tests above, passing.
- An append-only entry in the repo's DECISIONS.md recording: what the accessor's signature and location are, how actions are declared on adapter methods, which handlers were rerouted, and any deviation from this prompt with its reason.
- An end-of-phase summary stating the accessor's exact signature and file location, the action-declaration mechanism, and the list of rerouted call sites — this is what feeds the Phase B prompt.

Do not proceed to Phase B.
