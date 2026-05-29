# Joe — Decisions

Append-only project decision log. Newest entries at the top. Each entry
records what was decided, the basis (verifiable source, not assertion), and
what it supersedes. This file is normative: where a decision here conflicts
with prose elsewhere, this file states the project's position and the
conflicting prose is stale.

Format per entry: ID, date, decision, basis, supersedes, status.

---

## D-0005 — Identity Phase B: set-shaped IsAllowed + real ctx principal

- Date: 2026-05-29
- Decision: Phase B of the identity refactor (joe-identity-design.md §2.7,
  joe-identity-phase-plan.md Phase B) makes the authorization subject a SET of
  principals (union of grants) and confirms the accessor enforces the real
  context-derived caller principal. Behaviour-preserving and still
  single-principal in practice (the set has exactly one member at launch).
  Specifics:
  - **New set type.** `rbac.PrincipalSet` (`= []Principal`) with constructor
    `rbac.NewPrincipalSet(principals ...Principal) PrincipalSet`
    (`internal/rbac/principalset.go`). It is the authorization subject:
    additive / allow-only, no deny rules. At launch every caller constructs it
    with one member — the caller's own principal.
  - **Set-shaped decision function.** `PolicyEngine.IsAllowed` is now
    `IsAllowed(ctx, principals rbac.PrincipalSet, sourceID string, action Action) bool`
    (`internal/rbac/policy.go`). It resolves the source's zone ONCE (zone
    resolution is principal-independent) and the zone-allows-action check once,
    then permits if ANY member holds a policy granting that zone. A
    per-member `ListPoliciesForPrincipal` error denies only that member
    (`continue`) rather than the whole decision; for a size-1 set this is byte-
    identical to the prior single-principal behaviour (immediate deny), which
    is the regression contract.
  - **Set-shaped accessor chokepoint.** `Accessor.permit` is now
    `permit(ctx, principals rbac.PrincipalSet, sourceID, action) error`
    (`internal/access/access.go`). A new private seam
    `permitForPrincipal(ctx, principal rbac.Principal, sourceID, action)` lifts
    the single caller principal into a size-1 set via `rbac.NewPrincipalSet`
    and delegates to `permit`. `guard[T]` (the adapter resolve path),
    `observeResolve` (the category-dispatch sibling), and all `graph.go`
    dispatch methods call `permitForPrincipal`. `permit` remains the single
    place `IsAllowed` is invoked from the accessor. This one seam is where
    Phase C adds `group:` members (from the IdP groups claim) with no change to
    any dispatch method.
  - **Public dispatch signatures unchanged (single principal).** The exported
    `<Family><Operation>(ctx, principal rbac.Principal, …)` methods keep taking
    a single `rbac.Principal` — the context-derived caller principal the
    handlers already pass. The SET is formed inside the accessor at the
    decision boundary, not at the public API. Rationale: Phase B req 2 says
    callers pass "the caller principal" (singular) from context; the Phase A
    action-declaration guard (`internal/access/guard_test.go`) asserts dispatch
    methods take an `rbac.Principal` parameter; and the §B static criterion
    presumes a singular principal crosses the accessor boundary. Keeping the
    public arity also makes Phase B a minimal, attributable diff (≈140 lines).
  - **Context principal threading — already in place from Phase A.** The §B
    goal "thread the real ctx principal into the accessor instead of a
    configured/hardcoded one" required NO new wiring: Phase A's rerouted
    handlers already obtain the principal via
    `rbac.PrincipalFromContext(r.Context())` (the value `IdentityMiddleware`
    sets) and pass it to the accessor. Phase B verifies and locks this with
    tests (below) rather than changing handler code. The mechanism is:
    `IdentityMiddleware` → `PrincipalFromContext` (handler) → public dispatch
    method `principal` arg → `permitForPrincipal` → size-1 `PrincipalSet` →
    `permit` → `IsAllowed`.
  - **Middleware left authoritative (demotion deferred to E).** The HTTP
    `EnforcementMiddleware` is unchanged except that it now lifts its
    context principal into a size-1 set for the set-shaped `IsAllowed`
    (`internal/rbac/middleware.go`). It remains the authoritative gate on the
    HTTP path; the accessor stays shadowed there. Middleware demotion (and the
    accessor becoming load-bearing on HTTP) is Phase E, gated by an equivalence
    test, per design §2.5/§3.
- Deviations from the Phase B prompt, with reasons:
  1. **Threading was confirmation, not new code.** The prompt anticipated
     "replacing any reliance on a single hardcoded or implicitly-configured
     principal at the accessor's callers." Phase A had already context-derived
     the principal at every accessor call site, so there was nothing to
     replace; Phase B's contribution to req 2 is the proof (one behavioural
     test + one static guard), not a wiring change.
  2. **Static criterion expressed behaviourally + a light static guard.** Per
     the prompt's explicit allowance, a precise AST data-flow assertion is
     brittle against Phase A's explicit-principal signature, so the primary
     proof is behavioural — `TestPhaseB_ContextPrincipalReachesAccessorDecision`
     omits `EnforcementMiddleware` (making the accessor the sole gate) and
     injects a non-default principal into the request context; the 200/403
     outcome tracks that principal's grants (alice allowed, mallory denied),
     proving a context-injected principal reaches the accessor's decision. A
     complementary static guard
     (`TestPhaseB_AccessorCallersDerivePrincipalFromContext`) asserts every
     principal-gated accessor call site in `internal/api` reads
     `rbac.PrincipalFromContext` and passes no hardcoded principal
     (string literal / `rbac.Unknown` / `rbac.Principal("…")`). Principal-less
     methods (`GitHubWebhookSecret`, `GitLabWebhookSecret`, `GraphAvailable`)
     are exempt, mirroring the D-0004 allowlist convention.
  3. **`HasZoneAccess` deliberately NOT set-shaped.** The sourceless sibling
     `PolicyEngine.HasZoneAccess` (used by regime declare/resolve in
     `internal/api/regime.go`) is outside the accessor enforcement chain
     (`permit`→`IsAllowed`) and belongs to the regime/captain path (Phases
     F/G). Converting it now would be scope creep into a later phase and touch
     handlers Phase B should not. It stays single-principal; its set-shaping,
     if wanted, lands with the captain/audit work.
- Basis: joe-identity-design.md §2.7 (set-shaped, size-1) / §2.5 (accessor is
  the authoritative point; middleware demotion deferred to E) / §6 (groups drop
  in as set members later); joe-identity-phase-plan.md Phase B. Verified against
  Phase A's accessor signature (D-0004) and the existing context-threading in
  `internal/api`. Tests: rbac union semantics (size-1 granted/ungranted +
  multi-member ANY-granted + empty-set deny + zone-bounded), accessor per-kind
  allow/deny (unchanged, regression through the accessor), HTTP RBAC regression
  (`TestPhaseA_HTTPRBACOutcomesPreserved`, still green ⇒ HTTP outcomes identical
  through the set-shaped path), Phase A no-ungoverned-access invariant (still
  green, unchanged), and the two Phase B principal-threading tests above.
- Supersedes: nothing — extends D-0004. Phases C–G remain pending (C: OIDC +
  sessions + bootstrap; E: remove loopback — gated on A+B, now both merged).
  The loop and loopback were not touched in this phase.
- Status: active. Phase B complete; do not proceed to Phase C without a new
  prompt.

---

## D-0004 — Identity Phase A: guarded accessor below the transport

- Date: 2026-05-29
- Decision: Phase A of the identity refactor (joe-identity-design.md §2.5/§2.8,
  joe-identity-phase-plan.md Phase A) introduces a single guarded accessor as
  the only path to infrastructure adapters and the graph store, with
  `IsAllowed` evaluated inside it. Behaviour-preserving and still
  single-principal. Specifics:
  - **Accessor location & signature.** New package `internal/access`, type
    `*access.Accessor`. Constructor:
    `access.New(registry *adapters.Registry, graphStore graph.GraphStore, engine *rbac.PolicyEngine) *Accessor`.
    Enforcement chokepoint: `permit(ctx, principal rbac.Principal, sourceID string, action rbac.Action) error`
    (nil engine ⇒ permit-all, mirroring `EnforcementMiddleware(nil)`). Generic
    resolve+enforce: `guard[T any](a, ctx, principal, sourceID, action, typeName) (T, error)` —
    the ONLY caller of `registry.Get`. Public dispatch methods are
    `<Family><Operation>(ctx, principal rbac.Principal, sourceID string, …args) (…, error)`
    (e.g. `K8sListResources`, `PrometheusQuery`, `GitReadFile`, `ArgoCDApps`,
    `GraphQuery`); each enforces, then delegates to the resolved adapter/graph
    store and returns its result. On deny it returns `access.ErrPermissionDenied`
    and performs no infra call. Errors: `ErrPermissionDenied`, `ErrSourceNotFound`
    (wraps `store.ErrSourceNotFound` ⇒ 404 preserved), `ErrWrongAdapterType`,
    `ErrGraphUnavailable`. Wired in `api.New` via `newPolicyEngine(services)`,
    which reproduces `cmd/joe-core/main.go`'s enable-condition exactly (engine
    non-nil only when `Server.APIKey != ""`).
  - **Action declared on the method.** Each dispatch method passes its
    `rbac.Action` literal to `guard`/`permit` adjacent to the delegated adapter
    call — not inferred from the HTTP verb. Classification mirrors the prior
    verb mapping for behaviour parity: all current (GET) reads ⇒ `ActionRead`;
    the three GitHub/GitLab POST mutations ⇒ `ActionMutate`. `ActionQuery` is
    supported by the mechanism but assigned to no current method (see deviation
    2). Static guard `internal/access/guard_test.go` asserts every exported
    `*Accessor` method that takes an `rbac.Principal` references an
    `rbac.Action*` constant.
  - **Rerouted call sites (transport only).** All `internal/api` handlers that
    reached adapters/graph directly now go through `s.accessor`/
    `h.server.accessor`: `k8s.go`, `git.go`, `aws.go`, `observability.go`,
    `alerting.go`, `datastore.go`, `networking.go`, `registry.go`, `security.go`,
    `gitops.go`, `review.go`, `observe.go` (graph + category dispatch),
    `webui.go` (graph), `tasks.go` (graph summary), and `server.go` (graph
    routes). The typed `getXxxAdapter` helpers and `handleAdapterLookupError`
    were removed; `helpers.go` now exposes `writeAccessError`/`handleAccessError`.
    The HTTP `EnforcementMiddleware` remains as a coarse OUTER gate; the accessor
    is the authoritative point.
- Deviations from the Phase A prompt, with reasons:
  1. **Static guard scope.** The "no package other than the accessor" invariant
     is enforced repo-wide by `internal/api/access_guard_test.go` (forbids
     `*.Adapters.Get(...)` and `*.Graph.<method>(...)`) with an explicit
     allowlist: `internal/access` (the accessor), `internal/coreagent` (in-process
     Core Agent refresh — its convergence is Phase E; rerouting it now would add
     RBAC to the refresh path and change behaviour), and `cmd/joe-core` (a
     process-level OTel business-metrics gauge reading `graph.Summary`, with no
     caller principal). Registry lifecycle (`Register`/`Unregister`/`List`) and
     `services.Graph == nil` checks are not access and are allowed.
  2. **`ActionQuery` not yet assigned.** Reclassifying graph/PromQL/LogQL reads
     to `query` would deny them on the `unassigned` zone (which allows only
     `read`), changing 200→403 for a principal scoped to that zone. To honour
     "observable behaviour unchanged", reads keep `ActionRead`; semantic `query`
     classification is deferred.
  3. **Graph gating uniformity.** The accessor gates all graph access via the
     reserved `GraphSourceID = "graph"` (→ `unassigned`, `ActionRead`). This
     closes a pre-existing transport quirk where some graph sub-paths were
     ungated (their parsed path segment was empty) while others were gated under
     nonsense sourceIDs; all such sourceIDs resolved to `unassigned`, so the
     decision is identical for the gated ones. Invisible under RBAC-off
     (default) and for any normally-granted principal; only `GET /api/v1/graph`
     (full list) gains a gate it previously lacked.
  4. **Webhook secret reads are unenforced.** `GitHubWebhookSecret`/
     `GitLabWebhookSecret` resolve through the accessor but take no principal
     and run no RBAC: webhook receivers execute pre-auth and authenticate the
     sender via HMAC, so no caller principal exists. The action-declaration
     guard exempts principal-less methods by design.
  5. **Error-precedence micro-change.** Because the accessor bundles
     resolve+execute, handlers validate params before calling it; on a
     doubly-malformed request (bad source AND bad param) a 400 may now precede a
     404 where 404 previously won. Never affects a 200/403 outcome.
  6. **Accessor deny path unreachable via HTTP in Phase A.** Since the unchanged
     middleware uses the same engine and a verb-matched action, it makes the
     identical decision and blocks denied requests first; the accessor's
     enforcement becomes load-bearing in Phase E. Its deny path is proven by
     direct unit tests (`internal/access/access_test.go`), and the HTTP
     regression (`internal/api/access_regression_test.go`) proves
     middleware+accessor == middleware-alone for the configured principal.
- Basis: joe-identity-design.md §2.5/§2.8/§5; joe-identity-phase-plan.md Phase A;
  code verified against migration 006 zone seeds and `cmd/joe-core/main.go`'s
  RBAC wiring. Tests: per-kind allow/deny + no-infra-call-on-deny + nil-engine
  (access pkg), action-declaration + ungoverned-access AST guards, HTTP RBAC
  regression + RBAC-disabled.
- Supersedes: nothing — first identity-refactor decision. Phases B–G remain
  pending (B: set-shaped `IsAllowed`; E: remove loopback — gated on A+B).
- Status: active. Phase A complete; do not proceed to Phase B without a new prompt.

---

## D-0003 — Phase 2 streaming protocol and tool-execution boundary

- Date: 2026-05-28
- Decision: Phase 2 (single agentic runtime, Plan-of-Record §3 / D-0001) is
  implemented with two binding protocol/architecture choices:
  1. **Streaming protocol: Server-Sent Events (SSE).** joe-core → CLI streaming
     of the single agentic loop uses `text/event-stream` with named events
     (`step`, `local_tool_call`, `final`) on `POST /api/v1/tasks/stream`. The
     control direction (CLI → joe-core, delivering delegated tool results) is an
     ordinary `POST /api/v1/tasks/stream/{taskID}/tool-results`, so SSE's
     unidirectionality is not a constraint. Chosen over chunked newline-JSON
     because SSE is self-describing and the existing React Web UI can later
     consume the same endpoint via the browser `EventSource` API — one protocol
     for CLI and Web UI.
  2. **Tool-execution boundary: local tools execute in the CLI (callback path).**
     Local tools (`read_file`, `write_file`, `run_command`, `local_git_*`,
     `ask_user`) keep executing in the `joe` CLI process. The CLI advertises them
     as `client_tools`; joe-core registers delegating stubs so the LLM can call
     them, emits a `local_tool_call` event, and suspends the loop until the CLI
     posts the result. Rationale: preserve the security property that the CLI's
     filesystem/shell access is bounded by the operator's own shell, not by
     joe-core's process (which may run as a daemon/container/remote host).
     Shared (Go-native diagnostic) and core tools run inside joe-core's loop.
- Consequential choices recorded here:
  - **`/model` is now a global operation on the single runtime.** Switching the
    model hot-swaps joe-core's `services.LLM` (a `SwappableAdapter`) for all
    consumers; there is no per-CLI-session model. This is the direct consequence
    of "one LLM contact point"; a per-session override is out of scope because
    it conflicts with "exactly one adapter instantiated process-wide".
  - **The CLI requires no provider API keys.** It makes no LLM calls; keys live
    only in joe-core.
  - **Token accounting simplifies (not implemented here).** With one loop,
    joe-core's loop is the single authoritative tally (`taskResponse.total_tokens`);
    there is no second CLI-side count to reconcile. Token *visibility* is
    deferred polish, explicitly not built in Phase 2.
  - **The agentic loop was relocated** from `internal/useragent` to
    `internal/agentloop` (joe-core-owned). `internal/useragent` no longer exists;
    the CLI's build closure links no adapter-factory, provider, or loop package
    (asserted by a guard test in `cmd/joe`).
- Basis: PLAN-OF-RECORD-RECONCILED.md §3 (Phase 2 + completion gate);
  PHASE-0-SESSION-MODEL.md Invariants 1 and 6; docs/PHASE-2-IMPLEMENTATION-NOTES.md.
- Supersedes: nothing — first streaming/tool-boundary decision. The pre-Phase-2
  CLI-local agentic loop + CLI LLM adapter are removed.
- Status: active. Phase 2 complete.

---

## D-0002 — Phase 0 session model is the normative session-model design

- Date: 2026-02-16
- Decision: `docs/PHASE-0-SESSION-MODEL.md` is the normative output of the
  refactor's Phase 0. Phase 1 implementation is built from it. Its
  accompanying state diagram is a comprehension aid; where the document and
  the diagram disagree, the document governs.
- Scope closed by it: the original six refactor open questions; incident mode
  (emerged during design, load-bearing); the authority-layer integration
  verified against current code (§G of that document).
- Basis: PHASE-0-SESSION-MODEL.md (closed); Security & LLM-Execution
  Architecture Findings (code verification).
- Supersedes: nothing — first normative session-model artifact.
- Status: active. Phase 1 references this document, not chat history.

---

## D-0001 — Refactor Phase 3 deleted; refactor is two phases

- Date: 2026-02-16
- Decision: The agentic-runtime refactor collapses from three phases to two.
  Phase 3 ("collapse the /tasks loopback HTTP-to-self") is **deleted**, not
  merged or rescoped. The end-state acceptance criteria formerly nominal
  under Phase 3 are reattached to Phase 2 as Phase 2's completion gate. No
  work is lost; no work is created.
- Why delete (not merge/rescope): Phase 3's entire scope was collapsing a
  core→core HTTP loopback. Code verification found that loopback does not
  exist (joe-core's Core Agent uses its own `*tools.Registry` directly and
  does not call joe-core's own HTTP API). Merge implies residual Phase 3
  content to fold in — there is none. Rescope would invent work to preserve a
  phase number — the consistency-for-its-own-sake failure mode the project
  rejects (cf. PHASE-0-SESSION-MODEL.md R5). The single-agentic-runtime end
  state is reached as a consequence of Phase 2 (removing the CLI's own loop +
  LLM adapter), with no further step.
- Basis: Security & LLM-Execution Architecture Findings §4;
  PHASE-0-SESSION-MODEL.md §G1/§G2. The phantom phase is recorded here so a
  future reader does not rediscover the absent loopback and re-add a Phase 3
  to "complete" the sequence.
- Supersedes: the prior plan-of-record's "sessions → migrate REPL → collapse
  loopback" three-phase sequencing. The reconciled plan-of-record
  (`docs/PLAN-OF-RECORD-RECONCILED.md`, §5 Reconciliation Record) is the full
  audit trail; this entry is the index pointer to it.
- Status: active. The original plan-of-record file carries a supersession
  header pointing here and to the reconciled plan.
