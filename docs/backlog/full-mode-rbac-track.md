# Full-capabilities-mode RBAC: fail-closed at empty RBAC + a dedicated autonomous principal

Status: deferred — design-approved, unimplemented
Priority: later
Design of record: docs/project/DECISIONS.md D-0019 (the trust model). This entry is the implementation backlog item, not a new decision; it does not duplicate D-0019.

## Context

The write-floor work (D-0018) is complete: the boot-resolved, runtime-immutable
floor governs managed-system Mutates in observation and safe mode, enforced below
RBAC (`internal/safety/floor.go`, wired in `internal/tools/executor.go` and the
two captaingate sites per D-0022 Task 1).

The remaining trust-model work is the **full-capabilities-mode posture** from
D-0019. When an operator flips the boot env var to full mode, the hard ceiling is
removed and **RBAC becomes the floor**. The load-bearing safety property: full
mode with no write grants must deny every managed-system write (fail-closed),
enforced at the backend (the RBAC layer), never resting on a setup wizard or any
UI screen that can be skipped or that runs after the backend is already
write-capable. The two dangerous acts — flipping the env var and granting
capability — stay separate by construction. This is the single most dangerous
configuration change Joe has, and its safety is a backend invariant, not UX.

See D-0019 for the full design (the two postures, the principal model, the
capability ladder, the empty-RBAC fail-closed guarantee, and the UI distinction
between the two "Joe does nothing" states). This entry records only the
implementation track and its verified starting state.

## Verified current state — the obstacles (re-derive exact file:line from live code before acting; verified 2026-06-08 against the live tree)

1. **RBAC is inert / permissive by default.** The policy engine is instantiated
   only when a service account OR OIDC is configured — `cmd/joe/server.go:737`:
   `if saResolver.Configured() || oidcConfigured { policyEngine = rbac.NewPolicyEngine(rbacRepo) }`,
   otherwise `policyEngine` stays nil (`server.go:736`). With a nil engine the
   access guard short-circuits allow-all — `internal/access/access.go:128-131`:
   `if a.engine == nil { allowed = true; reason = "rbac_disabled" }` (the audit
   row is still written with reason `rbac_disabled`). So with auth off, every
   action is permitted. CONFIRMED as described.

2. **The agentic task/stream/chat routes are not source-keyed but carry a context
   principal.** These routes have no `/{sourceID}/` segment (the RBAC enforcement
   middleware fires only on sourceID-bearing paths — per CLAUDE.md, to-be-verified
   at the middleware registration). They derive the principal from context at the
   access guard: `internal/api/tasks.go:149,257,332,513` use
   `rbac.PrincipalFromContext(...)`, and `internal/agentloop/agent.go:391` records
   `Principal: string(rbac.PrincipalFromContext(ctx))`. The principal is set at the
   edge via `rbac.WithPrincipal` (noted at `tasks.go:268`). CONFIRMED: not
   source-keyed, principal carried by context and evaluated at the accessor.

3. **With auth ON and empty policy rows, RBAC already fails closed.** A source
   with no zone assignment defaults to the `unassigned` zone
   (`internal/rbac/policy.go:111`), whose allowed_actions default to `["read"]`
   (`internal/store/migrations/006_rbac.up.sql:34`, column default at line 8). A
   Mutate against an unassigned source is denied at the zone gate before any grant
   lookup — `policy.go:129-131` returns `ReasonActionNotInZone`; and any action
   with no matching policy returns `ReasonNoGrant` (`policy.go:167`). So with the
   engine live and zero policy rows, writes are denied (no grant; unassigned zone
   is read-only). CONFIRMED. NOTE the gap obstacle 1 names: this fail-closed
   behavior only holds when the engine is live — which today requires auth to be
   configured. That is exactly what obstacle 1 must make non-optional in full mode.

4. **The dedicated autonomous principal does NOT exist in live code.** Only three
   reserved principal-kind prefixes are defined — `internal/rbac/identity.go:22,25,27`:
   `PrefixUser = "user:"`, `PrefixGroup = "group:"`, `PrefixSvc = "svc:"`, collected
   in `reservedPrefixes` (`identity.go:33`). There is no `agent:core` or equivalent.
   CONFIRMED (this matches D-0022 Task 2 finding §3). Introducing one is an
   identity-model change (a new reserved kind).

## Requirements and acceptance criteria

Framed as requirements; the implementer derives file:line and approach.

- **Full mode must require authentication ON and a live policy engine.** The
  inert/permissive fail-open path (obstacle 1) must be UNREACHABLE in full mode —
  full mode with a nil engine must not boot write-capable (refuse to start, or
  force the engine live). Acceptance: there is no configuration in which full mode
  runs with `engine == nil`.
- **Full mode + zero grants denies every managed-system write**, with the SAME
  observable behavior as observation mode (Joe performs no managed-system
  mutation), enforced at the RBAC layer — a DIFFERENT layer than the write floor.
  This is the load-bearing fail-closed guarantee. Acceptance: a Mutate attempted
  in full mode with empty policy rows is denied at RBAC (not the floor), and Joe's
  externally observable effect is identical to observation mode.
- **A dedicated autonomous principal is introduced** (naming to match the existing
  reserved-prefix convention — e.g. `agent:core`), carried by the Core Agent path,
  starting with ZERO write grants, so autonomous Joe is read-only by enforcement.
  Acceptance: the Core Agent's actions resolve to this principal and go through the
  same enforcement seam as everything else; with no grant it can read but cannot
  perform a managed-system write.
- **Interactive writes continue to gate against the launching human's grants** —
  unchanged. Acceptance: a human-initiated write is authorized against
  `user:<email>` (or their group/role), not the autonomous principal.
- **The capability ladder (per-zone, per-capability graduation) is the mechanism**
  by which an operator grants write capability over time. Autonomous-write
  capability is a FUTURE grant on this ladder for the autonomous principal —
  explicitly NOT built in this track.

## Dependency chain

Record the order explicitly:

  autonomous principal  →  full-mode fail-closed RBAC  →  autonomous-path routing

Autonomous-path seam routing (D-0022 Task 2, deferred) — routing the Core Agent's
graph-write path through the shared executor seam, which it currently BYPASSES — is
BLOCKED on this track. The bypass is confirmed: each refresher calls
`BuildGraphDelta` → `ApplyGraphDelta(ctx, store, delta)`
(`internal/coreagent/graphdelta.go:119`), which calls `store.AddNode` /
`store.AddEdge` / `store.DeleteEdge` / `store.DeleteNode` directly
(`graphdelta.go:121-139`), never touching `Executor.Execute` or the captaingate
wrapper. Routing requires (a) the autonomous principal to EXIST (obstacle 4) and
(b) the inert-RBAC behavior RESOLVED (obstacle 1) — otherwise routing a
no-principal background context through the accessor risks DENYING the graph Reads
that must keep passing.

The graph operations the Core Agent performs are **Reads under the binary model
(D-0020)** — arg-keyed idempotent upserts of Joe's OWN model, not managed-system
mutations — and must continue to pass any seam they are routed through.
**Observation mode must not freeze Joe's own model** (a settled design point; see
D-0022 Task 2 and D-0010 VERDICT-A).

## Out of scope (this track deliberately does NOT)

- Build autonomous-write capability or any autonomous-write subsystem (a future
  grant on the existing ladder; D-0019 point 5).
- Build the first-login setup / awareness UI as a safety gate. The fail-closed
  backend floor is the boundary; any setup UI is advisory UX on top of it
  (D-0019 point 2 and "deliberately does NOT").
- Build tool-surface pruning by posture. Exposed-and-deny is retained per D-0019
  point 6.

## References (link, do not duplicate)

- docs/project/DECISIONS.md **D-0019** — the trust model; design of record for this track.
- docs/project/DECISIONS.md **D-0020** — the binary Read/Mutate model; defines why the
  Core Agent's graph operations are Reads.
- docs/project/DECISIONS.md **D-0018** — the write floor (done; the prerequisite layer).
- docs/project/DECISIONS.md **D-0022** — denial precedence (Task 1, done) and the deferred
  autonomous-path seam routing (Task 2) this track unblocks.
