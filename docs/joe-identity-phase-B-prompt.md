# Joe — Identity Phase B implementation prompt (executed)

This is the verbatim implementation prompt used for Phase B of the identity
refactor. It is preserved per the phase-plan workflow ("each phase is a
separate CC session with its own implementation prompt"). The result is
recorded in `DECISIONS.md` D-0005; the progress tracker in
`joe-identity-phase-plan.md` marks Phase B complete.

---

Read docs/joe-identity-design.md and docs/joe-identity-phase-plan.md, and DECISIONS.md entry D-0004 (Phase A), before starting. You are implementing Phase B only. Do not implement any later phase. Do not remove the loopback (Phase E) — the loop must not be wired to the accessor in this phase.

CONTEXT
This is an unreleased, private project with no live deployments and no external consumers. There are no backwards-compatibility constraints: break and rebuild internal interfaces freely, and fix all callers in the same pass. "Behavior-preserving" below means only "the same authorization inputs produce the same allow/deny outcomes after this phase as before it" — a test contract to keep this phase a clean, attributable diff, not a compatibility obligation.

Phase A produced the access.Accessor (internal/access). Its enforcement chokepoint is permit(ctx, principal rbac.Principal, sourceID string, action rbac.Action) error, and its public dispatch methods already take an explicit principal parameter. Currently the HTTP EnforcementMiddleware is the authoritative check on the HTTP path and the accessor's decision is shadowed by it (it runs but the middleware decides first). That redundancy is intentional and stays for this phase.

GOAL
Two changes, both behavior-preserving in the sense above:
1. Convert the authorization decision to evaluate a SET of principals (union of grants), populated with size 1 at launch.
2. Thread the real principal from the request context into the accessor's dispatch methods, replacing any reliance on a single hardcoded or implicitly-configured principal at the accessor's callers.

REQUIREMENTS
1. Change IsAllowed (and the accessor's permit, and any signatures between them) so the authorization subject is a set of principals rather than a single principal. The decision is: permitted if ANY principal in the set has a matching grant for the zone+action. The model stays additive / allow-only — no deny rules. At launch the set is constructed with exactly one member: the caller's own principal. Build the set-handling now; do not populate more than one member.
2. The accessor's callers (the HTTP handlers rerouted in Phase A) must obtain the caller principal from the request context (the value IdentityMiddleware already places there) and pass it into the accessor, rather than the accessor or its callers assuming a single configured principal. Since the system is still single-principal in practice (one configured API key → one principal until Phase C), the principal flowing through will still be that one value — so outcomes do not change yet — but it must now arrive via the context-derived path, not a constant or an implicit default.
3. Leave the HTTP EnforcementMiddleware authoritative on the HTTP path. Do not demote or remove it in this phase. The accessor remains shadowed on HTTP. Middleware demotion is explicitly deferred to Phase E, gated by an equivalence test, per the design.
4. Do not wire the agentic loop to the accessor and do not touch the loopback. The loop path is out of scope for Phase B.

CONSTRAINTS
- Same authorization inputs produce the same allow/deny outcomes as post-Phase-A, for the single configured principal. This is the regression contract.
- The static invariant from Phase A (no ungoverned path to an adapter or graph store, per its allowlist) must still hold and its test must still pass.

ACCEPTANCE — write these tests and make them pass.
Behavioral:
- A principal set whose single member has a matching grant is allowed; a set whose single member lacks the grant is denied. Plus a forward-looking test: a set with multiple members where ANY one has a matching grant is allowed (proves union semantics even though size 1 is used in production).
- Regression: for the single configured principal, allow/deny outcomes through both the HTTP path and the accessor are identical to post-Phase-A.
Static:
- An assertion that the principal passed into the accessor on the request path derives from the request context (the IdentityMiddleware-populated value), not from a constant or hardcoded default. Express this in whatever way is robust given Phase A's actual accessor signature — if a precise static assertion is impractical, substitute a behavioral test that proves a context-injected principal reaches the accessor's decision (e.g. an injected non-default principal in context produces a decision consistent with that principal's grants).
- The Phase A no-ungoverned-access invariant test still passes unchanged.

DELIVERABLES
- The implementation, with all internal callers fixed in the same pass (no compat shims).
- The tests above, passing; the full suite green (build, vet, fmt).
- An append-only DECISIONS.md entry (D-0005) recording: the new set-shaped signatures, how the context principal is threaded to the accessor, confirmation the middleware was left authoritative (and that demotion is deferred to E), and any deviation with its reason.
- An end-of-phase summary stating: the final set-shaped signature of the decision function and the accessor, the exact mechanism by which the context principal reaches the accessor, and confirmation the loop/loopback were untouched — this feeds the Phase C prompt.

Do not proceed to Phase C.
