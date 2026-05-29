# Joe — Identity Phase D implementation prompt (executed)

This is the verbatim implementation prompt used for Phase D of the identity
refactor. It is preserved per the phase-plan workflow ("each phase is a
separate CC session with its own implementation prompt"). The result is
recorded in `DECISIONS.md` D-0007; the progress tracker in
`joe-identity-phase-plan.md` marks Phase D complete.

---

Read docs/joe-identity-design.md (§2.4) and DECISIONS.md entries D-0004 through D-0006 before starting. You are implementing Phase D only. Do not implement later phases. Do not remove the loopback (Phase E). Do not wire the agentic loop to the accessor.

CONTEXT
Unreleased private project, no live deployments, no external consumers, no backwards-compatibility constraints. Break and rebuild internal interfaces freely; fix all callers in the same pass.

Today there is a single configured API key (Server.APIKey) mapped to a single configured principal, validated by BearerAuth and resolved by APIKeyProvider. Phase C added human OIDC identity flowing as user:<email> through auth.EdgeAuth → rbac.WithPrincipal → PrincipalFromContext → accessor/middleware. This phase generalizes the single machine key into multiple NAMED service-account keys, each mapping to a distinct svc:<name> principal, flowing through the SAME context mechanism.

GOAL
Replace the single-API-key machine-auth mechanism with a configurable map of named service-account keys to svc:<name> principals. Machines (MCP, CI, scripts) authenticate with their own key and resolve to their own svc: principal hitting the same accessor. Humans continue to use OIDC (unchanged). Two authentication mechanisms, one authorization path.

REQUIREMENTS
1. Service accounts are defined in joe-core config: a set of named entries, each with a name and an API key, resolving to principal svc:<name>. This generalizes the existing single Server.APIKey + Server.Principal into a collection. Reserve/enforce the svc: prefix on the minted principal. Keys are plaintext-at-rest in config — this is the same posture as today's single key, not a regression; do NOT add hashing or minting (deferred — see seam note).
2. A request presenting a service-account key authenticates as that key's svc:<name> principal and that principal enters the request context via the same rbac.WithPrincipal path Phase B/C established, so both EnforcementMiddleware and the accessor see it. A request presenting an unknown/invalid key is unauthenticated, exactly as an invalid bearer token is today.
3. Decide and document the interaction between the OIDC edge auth (C) and the service-account key auth on the same request path: a request carries either a session cookie (human) or a service-account bearer key (machine), never both meaningfully; define the precedence and ensure one mechanism's absence doesn't break the other. Both must converge on a single principal-in-context.
4. Service-account principals are provisioned zones via the SAME CLI surface Phase C built (joe zone grant/revoke/list already accepts a svc: principal per D-0006). Confirm svc: principals work through it; do not build a separate provisioning path.
5. MCP and CI use this mechanism. The MCP server (joe mcp, a separate external process using JOE_API_KEY) authenticates with a service-account key and resolves to its own svc: principal — it is a service account, not a human pass-through (pass-through is deferred). No in-process change to MCP is required beyond it using a service-account key like any external client.
6. Removal of the old single-key path: since there are no compat constraints, remove or fold the old single Server.APIKey→single-principal mechanism into the new map (e.g. the old key becomes one named entry, or is removed entirely). Do not leave two parallel machine-auth code paths. Fix all callers — including the loopback client's current use of Server.APIKey — BUT do not change the loopback's existence or behavior; it may continue using whichever service-account key represents the server for now (the loopback is removed in Phase E, so it just needs a valid key to keep working until then).

SEAM NOTE (for the deferred upgrade, record in DECISIONS): the resolution path key → svc: principal must be isolated so that a future change to DB-minted, hashed, runtime-revocable keys replaces only storage and lookup, not the downstream principal-in-context flow.

SCOPE FENCES
- PrincipalSet stays size 1 (one svc: member for a machine; do not add group: members).
- EnforcementMiddleware stays authoritative on the HTTP path (demotion is Phase E).
- Do not remove the loopback, do not wire the loop to the accessor, do not add OIDC changes.

CONSTRAINTS
- Phase A no-ungoverned-access invariant and Phase A/B/C regression tests still pass.
- The loopback must keep functioning (it still authenticates with a valid server-representing service-account key until Phase E removes it).

ACCEPTANCE — write these tests and make them pass.
Behavioral:
- Two distinct configured service-account keys resolve to two distinct svc: principals; each is allowed only on its own granted zones and denied on the other's (independent grants, proven through the accessor).
- A request with an unknown service-account key is unauthenticated.
- A service-account principal with zero granted zones is denied all infrastructure; after a CLI zone grant, allowed on the granted zone.
- A human OIDC session and a service-account key each independently produce the correct principal in context on the same protected endpoint (the two mechanisms coexist; precedence is deterministic and documented).
- The loopback still functions end-to-end (a loop run still reaches infra) using its service-account key.
Static:
- Assert service-account principals carry the svc: prefix.
- The Phase A no-ungoverned-access invariant test passes unchanged.

DELIVERABLES
- Implementation, all callers fixed in the same pass, old single-key path removed/folded (no parallel machine-auth paths). Full suite green (build, vet, fmt, test).
- The tests above, passing.
- DECISIONS.md entry D-0007 recording: the service-account config shape, the key→svc: resolution seam (and the deferred hashing/minting upgrade behind it), the OIDC-vs-service-key precedence on a shared request path, how the old single key was removed/folded, what the loopback now authenticates with, and any deviation with reason.
- End-of-phase summary stating: the service-account config shape and resolution mechanism, the human-vs-machine auth precedence, what credential the loopback currently uses, and confirmation the loop/loopback behavior is unchanged — this feeds the Phase E prompt.

Do not proceed to Phase E.
