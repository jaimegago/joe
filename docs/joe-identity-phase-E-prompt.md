# Joe — Identity Phase E implementation prompt (executed)

This is the verbatim implementation prompt used for Phase E of the identity
refactor. It is preserved per the phase-plan workflow ("each phase is a
separate CC session with its own implementation prompt"). The result is
recorded in `DECISIONS.md` D-0008; the progress tracker in
`joe-identity-phase-plan.md` marks Phase E complete.

---

Read docs/joe-identity-design.md (§1, §2.5, §3 sequencing) and DECISIONS.md D-0004 through D-0007 before starting. You are implementing Phase E only. This is the safety-critical phase the earlier phases were ordered to make safe; follow the sequencing constraint exactly.

CONTEXT
Unreleased private project, no live deployments, no external consumers, no backwards-compatibility constraints. Break and rebuild internal interfaces freely; fix all callers in the same pass.

State after Phase D:
- The accessor (internal/access) is the guarded chokepoint to all infra adapters and the graph store; it evaluates a size-1 PrincipalSet via permitForPrincipal and enforces IsAllowed inside it.
- The real caller principal flows through request context (rbac.WithPrincipal / PrincipalFromContext) for both humans (user:<email> via OIDC) and machines (svc:<name> via service-account keys).
- The agentic loop's tools STILL reach infra and the graph store via a loopback HTTP client (built in internal/api/tasks.go, also tasks_stream.go) that authenticates with the svc:server service-account key (LoopbackKey()). This loopback re-authenticates every tool call as svc:server, discarding the real caller principal that is present in the Go context up to the point the loopback client is built. This identity reset is the bug Phase E removes.
- EnforcementMiddleware is still authoritative on the HTTP path; the accessor is shadowed there.
- The external HTTP API (CLI SSE, Web UI fetch, MCP via JOE_SERVER/JOE_API_KEY) are genuine external clients and MUST remain fully functional and unchanged.

GOAL
Remove the loopback for the loop's in-process infrastructure and graph access. Wire the loop's tools to call the in-process accessor directly, passing the REAL caller principal (already in the Go context), so the loop's tool calls are evaluated against the actual user/service principal instead of svc:server. Then demote EnforcementMiddleware from authoritative to coarse outer gate, gated by an equivalence test, now that the accessor governs both the HTTP path and the loop path.

REQUIREMENTS

Loopback removal:
1. The loop's tool registry (currently NewCoreRegistry given a loopback *client.Client in tasks.go) must instead be given an in-process path to the accessor. The infra tools (k8s, prometheus, git, argocd, loki) and the graph tools (graph_query, graph_related, and the other graph methods) call the accessor's dispatch methods directly — no HTTP, no loopback client — passing the caller principal obtained from the Go context (the same principal EnforcementMiddleware/EdgeAuth resolved for the originating request).
2. The caller principal must reach the accessor on the loop path. The principal is already in the request context that initiates the task (per D-0006/D-0007); ensure it is carried to the executor and into the accessor dispatch call. Do NOT reintroduce any credential or re-authentication on this path — identity is established once at the edge and passed by context (design §1).
3. Delete the loopback client construction for in-process tool execution and the now-dead svc:server LoopbackKey path IF it has no other remaining consumer. Verify the CLI/REPL/MCP do NOT depend on LoopbackKey for their external access (they are external HTTP clients with their own keys); remove only what is genuinely dead. If LoopbackKey/svc:server has any surviving legitimate use, document why it remains.
4. Remove the redundant graph HTTP round-trip specifically: the loop must reach services.Graph in-process via the accessor, not via an HTTP hop to joe-core's own /api/v1/graph.

Middleware demotion (the deferred reconciliation from Phases B/D):
5. Now that the accessor governs both the HTTP path (Phase A) and the loop path (this phase), demote EnforcementMiddleware from authoritative per-zone enforcement to a coarse outer gate (authenticated-or-not), OR remove its per-zone IsAllowed decision entirely if the accessor fully covers the HTTP path. The accessor becomes the single authoritative enforcement point.
6. This demotion MUST be gated by an equivalence test: prove that the accessor alone produces the same allow/deny (200/403) outcomes on the HTTP path that middleware-plus-accessor produced before the demotion. Do not demote until that test exists and passes.

SCOPE FENCES
- Do not change the external HTTP API surface or break CLI/Web UI/MCP external clients.
- Do not modify OIDC or service-account resolution.
- PrincipalSet stays size 1.
- Do not implement audit (Phase F) or move the captain gate (Phase G).

CRITICAL ORDERING (within this phase)
Wire the loop to the guarded accessor with the real principal (req 1–2) and prove it enforces per-user BEFORE removing the loopback client (req 3) and BEFORE demoting the middleware (req 5–6). At no point may there be a state where the loop reaches infra/graph through neither the loopback's enforcement nor the accessor's enforcement. There must be no window of ungoverned access.

CONSTRAINTS
- Phase A no-ungoverned-access invariant test must still pass — and its allowlist must now be TIGHTENED: internal/coreagent (and whatever loop-execution package) should no longer be allowlisted as an exception if it now reaches infra through the accessor. Update the invariant so the loop path is covered by it, not excepted from it. This is a key signal that Phase E achieved its purpose.
- All Phase A/B/C/D regression tests still pass.

ACCEPTANCE — write these tests and make them pass.
Behavioral:
- A loop run initiated by a principal WITH a grant on the target zone succeeds at the tool call; the SAME loop run initiated by a principal WITHOUT that grant is denied at the tool call. This proves the loop now enforces against the real caller principal, not svc:server. (This test should be impossible to satisfy on pre-Phase-E code — call that out.)
- A loop run no longer requires svc:server to hold zone grants; remove/adjust any test or fixture that granted svc:server infra zones for the loop to function, and show the loop works via the caller's own grants instead.
- Graph access from the loop works in-process (no HTTP hop) and is governed by the accessor.
- External clients unchanged: CLI SSE task stream, Web UI API calls, and MCP all still function over the external HTTP API.
- Equivalence test (req 6): accessor-alone HTTP outcomes == prior middleware+accessor outcomes, across allow/deny/unauth cases.
Static:
- The no-ungoverned-access invariant test, with the loop-execution package NO LONGER allowlisted as an exception — asserting the loop reaches infra/graph only through the accessor.
- Assert no loopback HTTP client is constructed for in-process tool execution.

DELIVERABLES
- Implementation, all callers fixed in the same pass, dead loopback/svc:server code removed. Full suite green (build, vet, fmt, test).
- The tests above, passing.
- DECISIONS.md entry D-0008 recording: how the caller principal is carried to the accessor on the loop path, what was deleted (loopback client, svc:server/LoopbackKey if dead), the middleware demotion (what it is now vs. before) and the equivalence test that gated it, the tightened invariant allowlist, and any deviation with reason.
- End-of-phase summary stating: the loop's new in-process accessor call path and how the principal reaches it, what the middleware is now, what was deleted, confirmation external clients are intact, and the state of the no-ungoverned-access invariant (now covering the loop) — this feeds Phase F.

Do not proceed to Phase F.
