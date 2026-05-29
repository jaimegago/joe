# Joe — Decisions

Append-only project decision log. Newest entries at the top. Each entry
records what was decided, the basis (verifiable source, not assertion), and
what it supersedes. This file is normative: where a decision here conflicts
with prose elsewhere, this file states the project's position and the
conflicting prose is stale.

Format per entry: ID, date, decision, basis, supersedes, status.

---

## D-0008 — Identity Phase E: remove the loopback; loop runs through the accessor as the real caller; middleware demoted

- Date: 2026-05-29
- Decision: Phase E (joe-identity-design.md §1, §2.5, §3 sequencing;
  joe-identity-phase-plan.md Phase E) removes the in-process loopback. The
  agentic loop's tool registry no longer constructs a loopback `*client.Client`
  that re-authenticates as `svc:server`; instead it is wired to an in-process
  accessor-backed client that reads the real caller principal from Go context
  (the SAME principal `auth.EdgeAuth` set via `rbac.WithPrincipal` at the edge)
  and dispatches to `internal/access` directly. `EnforcementMiddleware` is
  demoted from the authoritative per-zone gate to a pass-through, gated by an
  equivalence test. Specifics:
  - **In-process client for the loop's tools.** `internal/api/inproc_client.go`
    introduces `inProcessCoreClient`, which implements every per-tool
    `*Client` interface in `internal/tools/core/`. Each method reads
    `rbac.PrincipalFromContext(ctx)` at the call site (literally — not via a
    helper, so the Phase B static guard
    `TestPhaseB_AccessorCallersDerivePrincipalFromContext` sees the context
    derivation) and calls the matching `*access.Accessor` dispatch method.
    There is NO HTTP, NO `client.New`, NO bearer key, and NO re-authentication
    on this path. Identity is established once at the edge and carried by
    context, per design §1 ("authenticate at real boundaries; pass identity
    by context within a process").
  - **Aggregate `coretools.CoreToolsClient` interface.** Defined in
    `internal/tools/core/coreclient.go` as the union of every per-tool
    `*Client` interface used by `registerCoreTools`. `tools.NewCoreRegistry`
    and `tools.NewDefaultRegistryWithClient` now take this interface instead
    of `*client.Client`. The HTTP `*client.Client` still satisfies it
    (preserving the e2e/integration test harness in `test/e2e`,
    `test/integration`, and the schema-validity test in
    `internal/tools/default_test.go`); the in-process client is the second
    implementation, used by the loop.
  - **Wiring.** `api.Server` now holds an `*inProcessCoreClient` built once
    by `api.New` alongside the accessor (`internal/api/server.go`).
    `internal/api/tasks.go`'s `buildTaskRun` passes `h.server.inproc` to
    `tools.NewCoreRegistry`. The deleted block (≈18 lines that built the
    scheme/loopbackURL/loopbackClient with `client.New(loopbackURL, ...)` and
    bearer-keyed it with `ServerConfig.LoopbackKey()`) is the entire loopback
    construction site for in-process tool execution. The static guard
    `TestPhaseE_NoLoopbackClientForInProcessToolExecution`
    (`internal/api/access_phasee_test.go`) parses `tasks.go`, `tasks_stream.go`,
    and `inproc_client.go` and fails if any of them reintroduces a
    `client.New(...)` call.
  - **Non-adapter tool dependencies.** A handful of core tools do not touch
    an adapter or the graph store: `list_sources` (reads
    `services.Store.Sources`), `search_knowledge` (calls
    `services.Knowledge.Search`), `detect_doc_drift`/`generate_doc_draft`
    (use `services.DriftDet`/`services.DocDrafter`), and
    `publish_doc_update`. These reach the in-process service directly. None
    of them is principal-gated today (they predate the Phase A accessor) and
    NONE is what the no-ungoverned-access invariant covers — that invariant
    is about adapters and the graph store
    (`internal/api/access_guard_test.go`). For `publish_doc_update`, the
    publish dispatch logic was extracted from `s.publishProposal` into the
    package-level `publishProposalToTarget(ctx, services, proposal)` helper
    in `internal/api/publish.go` so both the HTTP handler and the in-process
    client share it without either path going through an HTTP loopback.
  - **`EnforcementMiddleware` demoted to a pass-through.** With the accessor
    now authoritative on BOTH paths (HTTP via Phase A, loop via this phase),
    the middleware's per-zone `IsAllowed` call is redundant. It is now a
    no-op: `EnforcementMiddleware(engine)` returns a middleware that calls
    `next` directly, with `engine` retained as an argument only so existing
    test harnesses that build the middleware do not need to change. The
    obsolete tests in `internal/rbac/middleware_test.go` that asserted
    middleware-level IsAllowed behaviour are deleted; the new
    `TestEnforcementMiddleware_Passthrough` documents the demotion.
  - **Equivalence test gating the demotion (req 6).**
    `TestPhaseE_AccessorAloneMatchesPriorOutcomes` constructs two production
    chains over the same routes + RBAC state:
    `(EdgeAuth → demoted middleware → accessor)` and
    `(EdgeAuth → accessor)`. It asserts identical 200/403/401 outcomes
    across granted-read, ungranted-zone, missing-token, and invalid-token
    cases. The Phase A regression test
    `TestPhaseA_HTTPRBACOutcomesPreserved` continues to pass unchanged,
    proving the same outcomes match the pre-Phase-E expectations.
  - **`svc:server` and `LoopbackKey()`: what survives and why.** The reserved
    `svc:server` service account and `ServerConfig.LoopbackKey()` REMAIN.
    They are still presented by the joe CLI (`cmd/joe/main.go`) and the REPL
    panic command (`internal/repl/repl.go`) — these are external co-located
    HTTP clients to joe-core, NOT loopback in the in-process sense. The
    LoopbackKey docstring is updated to reflect the post-Phase-E reality
    (historical name, surviving consumer set). The
    `TestPhaseD_LoopbackKeyReachesInfra` test is renamed
    `TestPhaseD_ColocatedServerKeyReachesInfra` and its docstring rewritten
    to describe the CLI auth path, not the in-process loopback. The
    "JOE_API_KEY → server service account" env override
    (`internal/config/config.go`) is untouched.
  - **Phase A invariant: loop path covered, allowlist commentary updated.**
    The agent-loop execution package (`internal/api`, where `tasks.go` and
    the in-process client live) was already NOT in the allowlist; this phase
    makes that meaningful — the loop now reaches infra through the accessor
    only. The remaining allowlist entries are documented in the test:
    `internal/access` (the accessor itself), `internal/coreagent` (the
    timer-driven background refresh that runs without a caller principal —
    structurally outside the accessor), and `cmd/joe-core` (a process-level
    business-metric gauge with no caller principal). The
    `TestInvariant_NoUngovernedAdapterOrGraphAccess` text is updated to
    state explicitly that the loop path is now covered, not excepted.
  - **K8s return-shape conversion.** The accessor returns
    `[]unstructured.Unstructured` for K8s list/get; the tools expect
    `[]map[string]any`. The in-process client extracts `.Object` from each
    item before returning — matching the JSON shape the loopback HTTP client
    used to produce so no tool change is needed. AWS list calls similarly
    convert the accessor's value slices (e.g. `[]awsadapter.EC2Instance`)
    to the tool's pointer slices (`[]*awsadapter.EC2Instance`).
- Deviations from the Phase E prompt, with reasons:
  1. **`svc:server`/`LoopbackKey()` retained, not deleted.** The prompt
     allowed deletion only "IF it has no other remaining consumer". The joe
     CLI and the REPL are surviving external co-located clients (separate
     processes that share joe-core's config), and the `JOE_API_KEY` env
     override folds into this same account. The name remains "LoopbackKey"
     to minimise churn at every call site (cmd/joe x5, internal/repl x1),
     but every docstring is rewritten to reflect the post-Phase-E reality
     ("co-located CLI key, not loopback"). A rename to `CoLocatedKey()` is
     an isolated follow-up not required by Phase E.
  2. **`coreagent` refresh path NOT routed through the accessor.** The
     Phase A allowlist commentary said the coreagent exception should be
     removed in Phase E, but the refresh path is timer-driven background
     work with no caller principal — the accessor's enforcement model
     (`permitForPrincipal(ctx, principal, ...)`) does not fit it without
     either granting svc:server every zone or special-casing the principal
     in the accessor (both defeat the purpose). Phase E's scope is the
     LOOP path (per the design doc §3), which is now governed by the
     accessor. The coreagent allowlist remains, with its rationale updated
     to spell out the structural difference. If the refresh path is later
     refactored to take a principal, the allowlist entry should be removed
     then.
  3. **In-process equivalence test instead of replaying the legacy chain.**
     The pre-Phase-E "middleware does IsAllowed" chain no longer exists in
     the codebase (the demotion is the change being shipped). The
     equivalence test asserts that the two surviving chains —
     `(demoted middleware + accessor)` and `(accessor alone)` — agree on
     200/403/401 across the matrix, AND that the Phase A regression test
     (the pre-Phase-E behavioural contract) still passes through the
     demoted chain. Together these prove the demotion preserves outcomes.
  4. **Aggregate interface defined in `internal/tools/core`, not a new
     package.** The simplest seam keeping the per-tool small interfaces
     intact for unit testing is a composition interface alongside them;
     `coreClient.go` does exactly that. A new `tools/inproc` package would
     be heavier without producing different behaviour.
- Basis: joe-identity-design.md §1 (root-cause: loopback IS the identity
  reset), §2.5 (accessor is the authoritative point), §3 (sequencing — E
  must follow A+B, which both merged in D-0004/D-0005), §5-Invariants 1–3;
  joe-identity-phase-plan.md Phase E. Verified against Phase A's accessor
  signature (D-0004), Phase B's set-shaped path (D-0005), Phase C's
  edge-auth + CLI provisioning (D-0006), and Phase D's service-account
  resolver (D-0007). Tests added:
  `TestPhaseE_LoopEnforcesAgainstRealCallerPrincipal` (alice succeeds,
  mallory denied, svc:server not granted — impossible on pre-Phase-E code),
  `TestPhaseE_LoopGraphAccessIsInProcess` (graph access works without an
  HTTP server),
  `TestPhaseE_AccessorAloneMatchesPriorOutcomes` (equivalence test),
  `TestPhaseE_NoLoopbackClientForInProcessToolExecution` (static guard
  against re-introducing `client.New(...)`),
  `TestEnforcementMiddleware_Passthrough` (documents the demotion). Phase A
  no-ungoverned-access invariant and Phase A/B/C/D regressions still green
  and unchanged.
- Supersedes: nothing — extends D-0007. Phases F (audit) and G (captain
  wiring) remain pending. The in-process loopback construction in
  `tasks.go`/`tasks_stream.go` is deleted; the in-process accessor is the
  new path. External clients (CLI SSE, Web UI API, MCP) are unchanged —
  they remain external HTTP clients that authenticate at the edge.
- Status: active. Phase E complete; do not proceed to Phase F without a new
  prompt.

---

## D-0007 — Identity Phase D: named service-account keys → svc: principals

- Date: 2026-05-29
- Decision: Phase D (joe-identity-design.md §2.4; joe-identity-phase-plan.md
  Phase D) replaces the single machine-auth key (`Server.APIKey` →
  `Server.Principal`) with a configurable collection of NAMED service-account
  keys, each resolving to a distinct `svc:<name>` principal that flows through
  the SAME context mechanism Phase B/C established (`rbac.WithPrincipal` →
  `rbac.PrincipalFromContext` → accessor + `EnforcementMiddleware`). Two
  authentication mechanisms (OIDC for humans, keys for machines), one
  authorization path. Specifics:
  - **Service-account config shape.** `config.ServerConfig.ServiceAccounts
    []ServiceAccount` (yaml `service_accounts`), each entry
    `{Name string, Key string}` resolving to principal `svc:<Name>`. This
    generalizes the old single `api_key`+`principal` into a set; the
    `Server.APIKey` and `Server.Principal` fields are REMOVED (no compat
    constraints). Keys are plaintext-at-rest — the same posture as the single
    key they replace, NOT a regression; no hashing/minting was added (deferred —
    see seam below). The `svc:` prefix is reserved/enforced at mint time by
    `rbac.ServicePrincipal(name)` (`internal/rbac/identity.go`), which mirrors
    `UserPrincipal`: it rejects an empty name and a name already carrying a
    reserved prefix (`user:`/`group:`/`svc:`) so a config typo cannot
    double-encode or kind-spoof.
  - **The key → svc: resolution seam (isolated, per the prompt's seam note).**
    `auth.ServiceAccountResolver` (`internal/auth/serviceaccount.go`) is the
    SINGLE place that owns "plaintext key, exact-match lookup → svc principal":
    `NewServiceAccountResolver([]config.ServiceAccount)` builds an immutable
    `map[key]rbac.Principal` (minting each via `rbac.ServicePrincipal`) and
    `Resolve(key) (rbac.Principal, bool)` performs the lookup. A future upgrade
    to DB-minted, hashed, runtime-revocable keys replaces ONLY this type's
    storage (the map) and lookup (`Resolve`) — the downstream
    principal-in-context flow (`EdgeAuth` → `rbac.WithPrincipal` → accessor) is
    untouched because it depends only on `Resolve` returning a principal. The
    constructor fails LOUDLY (fatal startup error in `cmd/joe-core/main.go`) on
    a malformed config — empty key, empty name, duplicate name, duplicate key,
    or reserved-prefix name — so a typo never silently drops an identity's
    authority or makes resolution ambiguous.
  - **OIDC-vs-service-key precedence on a shared request path.** `auth.EdgeAuth`
    resolves the caller principal in deterministic order: (1) a valid session
    cookie (human) WINS; (2) otherwise a service-account bearer key (machine) is
    tried via the resolver; (3) otherwise the request is unauthenticated → 401
    on a protected path. A request carries either a session cookie or a bearer
    key, never both meaningfully; when both are present the human session takes
    precedence. The two mechanisms are independent: `Sessions` may be nil
    (machine-only) and `ServiceAccounts` may be nil/empty (human-only) without
    breaking the other. An unknown bearer key is unauthenticated, exactly as an
    invalid token was. Both converge on one principal in context, which
    `EnforcementMiddleware` and the accessor evaluate identically regardless of
    which mechanism produced it. `EnforcementMiddleware` stays authoritative on
    the HTTP path (demotion is Phase E).
  - **Removal/fold of the old single-key path.** The single
    `Server.APIKey`→single-`Server.Principal` mechanism is removed, not kept in
    parallel: (a) the config fields are deleted; (b) `EdgeConfig.APIKey`/
    `APIKeyPrincipal` are replaced by `EdgeConfig.ServiceAccounts
    *ServiceAccountResolver` (plus an optional `DisabledPrincipal` defaulting to
    `default-operator` for auth-disabled mode); (c) `rbac.APIKeyProvider` (the
    literal single-key→single-principal `IdentityProvider`) and `api.BearerAuth`
    (the pre-Phase-C single-token gate) are DELETED along with their unit tests;
    (d) the engine enable-condition in both `cmd/joe-core/main.go` and
    `api.newPolicyEngine` becomes `ServiceAccountsConfigured() || OIDC`
    (was `APIKey != "" || OIDC`). The generic `rbac.IdentityMiddleware` +
    `rbac.IdentityProvider` interface are KEPT — they are not the single-key
    mechanism; many tests inject principals through them with their own
    providers. `JOE_API_KEY` env now folds into the reserved `server` service
    account (creating/overriding its key in `config.applyEnvOverrides`) — the
    literal "old key becomes one named entry" the prompt suggested; it only
    affects processes that load config (joe-core, joe CLI, REPL). MCP/Slack are
    separate external processes that read `JOE_API_KEY` directly from env and
    were untouched (req 5: no in-process MCP change — it presents a key like any
    external client and resolves to whatever svc account that key belongs to).
  - **What the loopback now authenticates with.** The in-process loopback
    (`internal/api/tasks.go`), the joe CLI (`cmd/joe`), and the REPL
    (`internal/repl`) are co-located client processes that present
    `ServerConfig.LoopbackKey()` — the key of the reserved `server` service
    account (principal `svc:server`), the direct fold of the old single key.
    `LoopbackKey()` returns the `server` account's key, else the first
    configured account (deterministic, config order), else "" (auth-disabled,
    no bearer presented). The loopback's existence and behaviour are UNCHANGED:
    it still presents a valid server-representing key so the loop's tools reach
    infra (the loopback is removed in Phase E). For the loop to reach infra
    under RBAC, `svc:server` must be granted zones via `joe zone grant
    --principal svc:server` — the same CLI surface Phase C built.
  - **CLI provisioning for svc: principals (req 4 — unchanged path).** `joe
    zone grant/revoke/list` already accepts a `svc:` principal (it validates via
    `rbac.HasReservedPrefix`, which includes `svc:`); confirmed by the existing
    `cmd/joe/zone_test.go` (`grant --principal svc:ci-bot`). No separate
    provisioning path was built.
- Deviations from the Phase D prompt, with reasons:
  1. **Loopback proven end-to-end via its credential through the real chain,
     not by driving the LLM loop.** `TestPhaseD_LoopbackKeyReachesInfra`
     presents `ServerConfig.LoopbackKey()` through the production-shaped chain
     (`EdgeAuth` + authoritative `EnforcementMiddleware`, as in main.go) and
     asserts it resolves to `svc:server` and reaches infra — denied (403) before
     a grant, allowed (200) after. The loopback's HTTP transport to infra IS
     this path; spinning the full agentloop (LLM + SSE) would test the loop, not
     the credential, and is heavier without adding auth coverage.
  2. **Phase A regression chain rewritten onto the production auth path.**
     Removing `Server.APIKey`/`Server.Principal` forced touching
     `access_regression_test.go`; rather than keep the dead
     `BearerAuth`+`APIKeyProvider` scaffold alive, the chain now uses the live
     `auth.EdgeAuth` + a one-entry resolver. The asserted Phase A outcomes
     (granted read 200 / ungranted 403 / missing token 401; disabled permits
     all) are preserved exactly — the regression contract is unchanged; only the
     mechanism establishing the principal moved to the current production path.
  3. **`svc:` prefix invariant proven behaviourally, not by an AST guard.** Per
     the prompt's static criterion, `rbac.ServicePrincipal` always applies the
     `svc:` prefix by construction (the single mint point); this is asserted in
     `internal/rbac/identity_test.go` and across every resolver output in
     `internal/auth/serviceaccount_test.go`. A data-flow AST assertion would be
     brittle and redundant given the single mint point.
  4. **`group:` reserved but unminted.** Per scope fence, the PrincipalSet stays
     size 1; nothing populates `group:` (a v2 seam). Service-account principals
     are single `svc:` members.
- Basis: joe-identity-design.md §2.4 (named API keys, MCP-is-a-service-account,
  rejects pass-through), §2.2 (reserved `svc:` prefix), §2.7 (set stays size 1),
  §2.5 (EnforcementMiddleware authoritative until E), §6 (deferred
  hashing/minting + MCP pass-through behind seams); joe-identity-phase-plan.md
  Phase D. Verified against Phase A's accessor signature (D-0004), Phase B's
  set-shaped path (D-0005), and Phase C's edge-auth + CLI provisioning (D-0006).
  Tests: two distinct keys → two distinct svc principals, each allowed only on
  its own granted zone and denied on the other's through the accessor
  (`TestPhaseD_TwoServiceAccountsIndependentZones`); unknown key → 401
  (`TestPhaseD_UnknownKeyUnauthenticated`, `TestEdgeAuth_UnknownServiceAccount…`);
  zero-zone svc denied then allowed after a CLI-equivalent grant
  (`TestPhaseD_ZeroZoneDeniedThenGrantAllows`); OIDC session and svc key each
  produce the correct principal on the same endpoint with session-wins
  precedence (`TestPhaseD_OIDCAndServiceKeyCoexist`); loopback key reaches infra
  end-to-end (`TestPhaseD_LoopbackKeyReachesInfra`); resolver
  reject-invalid-config + svc: prefix assertions; `ServicePrincipal` mint/reject;
  `JOE_API_KEY` folds into the `server` account
  (`config.TestLoad_EnvOverrides_APIKey`); Phase A no-ungoverned-access invariant
  and Phase A/B/C regressions still green and unchanged.
- Supersedes: nothing — extends D-0006. The single configured API-key principal
  is now removed/folded into the service-account map. Phases E–G remain pending
  (E: remove loopback — gated on A+B, both merged; F: audit; G: captain wiring).
  The loop, the loopback's existence/behaviour, OIDC, and the accessor were NOT
  changed in this phase.
- Status: active. Phase D complete; do not proceed to Phase E without a new prompt.

---

## D-0006 — Identity Phase C: OIDC login + sessions + admin bootstrap + CLI provisioning

- Date: 2026-05-29
- Decision: Phase C (joe-identity-design.md §2.1–§2.3, §2.9;
  joe-identity-phase-plan.md Phase C) replaces the SOURCE of the human principal
  with a real OIDC-authenticated identity, without changing the Phase B
  set machinery (the PrincipalSet stays size 1; no `group:` members are
  populated). A human logs in via a single configurable OIDC issuer; the
  verified `email` claim becomes a `user:<email>` principal carried by a
  server-side session cookie, flowing through the SAME context path Phase B
  established (`rbac.WithPrincipal` → `rbac.PrincipalFromContext` → accessor).
  Specifics:
  - **OIDC library: `github.com/coreos/go-oidc/v3` + `golang.org/x/oauth2`.**
    go-oidc handles discovery (`.well-known/openid-configuration`), JWKS
    fetching, and ID-token signature/issuer/audience/expiry verification;
    x/oauth2 handles the authorization-code flow and PKCE (`GenerateVerifier`,
    `S256ChallengeOption`, `VerifierOption`). Chosen because the prompt named
    go-oidc/v3 as the expected choice and because JWKS fetching and signature
    verification must NOT be hand-rolled (design §2.1). The IdP-facing surface
    is an interface (`auth.Provider`) so the flow is testable without a live
    issuer; the production implementation (`auth.NewOIDCProvider`) lazy-inits
    discovery on first use and caches it, so startup does not hard-depend on IdP
    reachability (design §4: IdP unreachable ⇒ only new logins fail).
  - **Single configurable issuer.** `config.AuthConfig.OIDC` carries issuer URL,
    client id, client secret, redirect URL. One code path; GitHub-direct
    (OAuth2, not OIDC) stays out, per design §2.1 caveat.
  - **`user:<email>` derivation + `email_verified` enforcement.** The single
    point where verified OIDC identity becomes a principal is
    `auth.PrincipalFromClaims` → `rbac.UserPrincipal(email)`
    (`internal/auth/claims.go`, `internal/rbac/identity.go`). It rejects with
    `ErrEmailNotVerified` when `email_verified` is absent or not true — the gate
    runs BEFORE any principal is minted, so an unverified token never yields a
    principal or a session. `email_verified` is decoded with a `flexBool` that
    accepts native-bool or string-encoded ("true"/"false", Azure-style) and
    fails closed on anything else. `UserPrincipal` also rejects an email that
    already carries a reserved prefix (`user:`/`group:`/`svc:`) — an
    impersonation guard that does not trigger in practice.
  - **Session model + cookie (design §2.3).** On a successful callback a
    server-side session row is minted in SQLite (`auth_sessions`: id, principal,
    created_at, expires_at — migration 014) and a cookie is set carrying ONLY
    the opaque id. Cookie attributes are exactly **HttpOnly + Secure +
    SameSite=Lax**. Lax (not Strict) is required: Strict would not send the
    session cookie on the cross-site top-level navigation returning from the IdP
    to the callback, so the app would treat the returning user as a new visitor.
    Sessions have a **bounded lifetime** (`auth.session_ttl`, default 12h; a
    non-positive value falls back to a bounded default — never unbounded) and a
    **server-side revocation path** (deleting the row = immediate logout). The
    `SessionManager.Resolve` rejects a session at/after `expires_at` even if the
    row still exists. Server-side sessions were chosen over JWT because joe-core
    is a single non-distributed binary with the DB right there, so statelessness
    buys nothing and costs revocation.
  - **OIDC flow CSRF/PKCE.** Login generates `state`, `nonce`, and a PKCE
    verifier; the in-flight flow (verifier + nonce) is persisted server-side in
    `auth_login_flows` keyed by `state` (migration 014), and a temporary
    HttpOnly/Secure/SameSite=Lax `joe_oidc_state` cookie binds the browser to
    that state. The callback validates query-state == cookie-state (login CSRF),
    loads the single-use flow row (deleted regardless of outcome), completes the
    PKCE exchange, verifies the ID token, and checks the token nonce against the
    flow nonce. The API performs no state-changing GET and logout is POST, per
    the §2.3 CSRF posture.
  - **Edge authentication middleware.** `auth.EdgeAuth` (`internal/auth/middleware.go`)
    replaces the prior `api.BearerAuth` + `rbac.IdentityMiddleware` pair in the
    production chain (`cmd/joe-core/main.go`). It resolves the caller principal
    from a session cookie (humans) or the bearer API key, sets it via
    `rbac.WithPrincipal`, and **rejects unauthenticated requests on protected
    paths with 401 — exactly as today**. The OIDC flow endpoints
    (`/api/v1/auth/`) are public (you cannot require a session to log in). When
    NEITHER an API key NOR OIDC is configured, the edge is in auth-disabled mode
    and behaves exactly as pre-Phase-C (every caller is the configured fallback
    principal; nothing rejected). `rbac.EnforcementMiddleware` remains the
    authoritative source-keyed RBAC gate beneath it (demotion is Phase E). The
    old `BearerAuth`/`IdentityMiddleware`/`APIKeyProvider` remain in the codebase
    (used by the Phase A/B regression tests and unchanged).
  - **Endpoints.** `GET /api/v1/auth/login` (initiate), `GET /api/v1/auth/callback`
    (complete), `POST /api/v1/auth/logout` (revoke + clear cookie), registered by
    `auth.Handlers.RegisterRoutes` only when an issuer is configured.
  - **First-login provisioning + admin bootstrap (design §2.9).** There is no
    user directory: a `user:<email>` principal exists implicitly by being
    referenced by a session and/or policies, so "first login creates the
    binding with ZERO zones" is literally a no-op — a freshly-authenticated user
    has no policy rows and `IsAllowed` denies everything until an operator
    grants zones. The ONLY bootstrap path is the config-designated
    `auth.admin_email`: on every login whose verified email matches it,
    `auth.Provisioner.GrantAdmin` runs. **Admin authority means, concretely, an
    `rbac_policies` grant on EVERY security zone present at grant time** —
    prod-readonly, prod-write, dev-full, unassigned, and regime-control — which,
    because RBAC is zone-scoped and additive/allow-only, yields
    read/query/mutate/delete on every source assigned to any of those zones plus
    the sourceless declare/resolve-incident capabilities. It is idempotent
    (existing grants skipped) and grants only zones existing when it ran (a
    later login picks up newer zones); a grant failure fails the login loudly
    rather than masquerading as a working admin.
  - **CLI provisioning (design §2.9 — CLI only, no admin UI, no NEW admin HTTP
    endpoint).** New `joe zone` subcommand (`cmd/joe/zone.go`): `grant
    --principal <user:|svc:…> --zone <id>`, `revoke --principal … --zone …`,
    `list [--principal …]`. It writes/removes `rbac_policies` rows by opening the
    SQLite DB **directly** (operator-on-host) — this sidesteps the bootstrap
    chicken-and-egg (no already-authorized session is needed to grant the first
    one) and honours "no admin HTTP endpoint" for this phase. Grants are
    validated (zone must exist; principal must carry a reserved prefix) and
    idempotent. Source→zone assignment is unchanged (existing admin API).
- Deviations from the Phase C prompt, with reasons:
  1. **SameSite=Lax tested by attribute assertion, not a true cross-site
     redirect.** An `httptest` harness cannot simulate a real cross-site
     top-level navigation from an IdP origin, so per the prompt's explicit
     allowance the test asserts the cookie is exactly HttpOnly+Secure+Lax and
     documents why Lax (not Strict) is what makes the callback return work
     (`TestSessionManager_CookieAttributes`). The full login→callback flow is
     exercised end-to-end with an injected verified ID token.
  2. **`email_verified` enforcement proven behaviourally (static impractical).**
     Per the prompt's allowance, the prefix/verified invariants are asserted by
     `internal/auth/claims_test.go` (verified→`user:` prefix; false/absent→
     `ErrEmailNotVerified` with no principal; reserved-prefix collision
     rejected) rather than by an AST guard.
  3. **Engine enable-condition widened to include OIDC.** `api.newPolicyEngine`
     and `cmd/joe-core/main.go` now build the policy engine when the API key OR
     OIDC is configured (previously API-key only), so a pure-OIDC deployment is
     enforced. Behaviour for the existing API-key and RBAC-disabled cases is
     unchanged (Phase A/B regression tests still green).
  4. **Edge auth replaces (not augments) BearerAuth+IdentityMiddleware in the
     production chain.** The prompt allows breaking/rebuilding internal
     interfaces; consolidating session + bearer resolution into one middleware
     is cleaner than chaining BearerAuth (which would 401 a cookie-only request
     before session resolution). The old middlewares are retained for the
     regression tests, which construct their own chains and are untouched.
  5. **`group:` reserved but unminted.** Per scope fence 9, the set stays size 1;
     `rbac.PrefixGroup` is reserved for v2 and nothing populates it.
- Basis: joe-identity-design.md §2.1 (single OIDC issuer), §2.2 (`user:<email>`
  + `email_verified` hard rejection + reserved prefixes), §2.3 (server-side
  session + HttpOnly/Secure/Lax + CSRF/PKCE), §2.9 (zero-zone first login,
  config admin bootstrap, CLI provisioning), §4 (IdP-unreachable failure mode);
  joe-identity-phase-plan.md Phase C. Verified against Phase A's accessor
  signature (D-0004) and Phase B's set-shaped path (D-0005). Tests: OIDC
  callback success → session + `user:` principal; `email_verified=false`/absent
  rejected with no session; zero-zone user denied then allowed after a CLI grant
  and still denied elsewhere (`TestPhaseC_OIDCSessionPrincipalReachesAccessor`);
  admin email gains all zones, non-admin none; logout deletes the session
  (immediate); expired session treated as unauthenticated; cookie attribute
  assertion; state/nonce-mismatch rejection; `joe zone` grant/revoke/list +
  validation; Phase A no-ungoverned-access invariant and Phase A/B RBAC
  regressions still green and unchanged.
- Supersedes: nothing — extends D-0005. The single configured API-key principal
  remains usable for machine access and is repurposed for service accounts in
  Phase D. Phases D–G remain pending (D: service-account keys; E: remove
  loopback — gated on A+B, both merged; F: audit; G: captain wiring). The loop,
  the loopback, and service-account API keys were NOT touched in this phase.
- Status: active. Phase C complete; do not proceed to Phase D without a new prompt.

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
