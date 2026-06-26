> **SUPERSEDED — point-in-time record.** This investigation's load-bearing verdict — that engine-nil is a reachable runtime state — is reversed by D-0027 (refuse-to-start), under which that state cannot be reached at runtime, because Joe refuses to boot unless the policy engine would be constructed non-nil. The findings below are kept as a point-in-time record whose verdict no longer holds. Annotated 2026-06-26, session docs-reconcile-historical-annotations.

```
INVESTIGATION: Identity wiring and runtime configurability of the RBAC policy engine
Scope: read-only diagnosis. Every claim carries file:line evidence. Anything not
provable from code is labelled "not determinable from code." No recommendations.

================================================================================
VERDICT (answers both load-bearing questions)
================================================================================

(a) Is engine-nil reachable with OIDC "present in env but not wired"?
    The engine's enable decision reads ONLY two config-value-presence predicates,
    never a constructed OIDC verifier/principal-source. The engine is non-nil iff
    `saResolver.Configured() || oidcConfigured` (cmd/joe/server.go:642-643), where
    `oidcConfigured = cfg.Auth.OIDC.Configured()` (cmd/joe/server.go:640) and
    `Configured()` is the pure value check `Issuer != "" && ClientID != "" &&
    RedirectURL != ""` (internal/config/config.go:99-101). The engine constructor
    itself takes ONLY the RBAC repo — `NewPolicyEngine(repo Repository)`
    (internal/rbac/policy.go:15) — so OIDC is never "wired into" the engine as a
    principal source; there is nothing to fail to wire. Consequences:
      - If all three OIDC fields are set, the engine is non-nil REGARDLESS of
        whether the IdP is reachable, valid, or ever successfully constructed
        (OIDC discovery is lazy, internal/auth/oidc.go:62-66, 70-90), so a
        malformed/unreachable OIDC does NOT fall through to a nil (allow-all)
        engine — it builds the engine and only login fails later.
      - YES, engine-nil IS reachable while an OIDC value is "present": a partial
        OIDC config (e.g. only `issuer` set, `client_id`/`redirect_url` empty)
        makes `Configured()` return false (internal/config/config.go:100), so with
        no service account the engine stays nil. Deciding line:
        internal/config/config.go:100.
      - Note: OIDC has NO environment-variable override at all (only the YAML
        config file populates it; applyEnvOverrides covers LLM, log level, server
        address, JOE_API_KEY, DB DSN — internal/config/config.go:487-546). So
        "OIDC URL in env" literally cannot occur; OIDC is config-file only.

(b) Can a running, identity-less Joe configure its own identity without a restart?
    NO. Both gating inputs are boot-only:
      - Service accounts (machine identities / bearer keys) come EXCLUSIVELY from
        config loaded at boot (cfg.Server.ServiceAccounts, internal/config/config.go
        :156-160) plus the JOE_API_KEY env fold (internal/config/config.go:534-536,
        552-560). There is NO HTTP/admin/MCP endpoint that creates a service
        account at runtime (admin surface enumerated below:
        internal/api/admin.go:102-133 — zones/policies/admins/principal-status
        only, no account/key minting).
      - The policy engine's nil-ness is decided once, at boot, in the server
        startup function (cmd/joe/server.go:642-643) and once in the accessor
        builder called during route registration at boot
        (internal/api/server.go:76-84). Neither is re-evaluated; there is no
        config reload path for Auth/Server (the only hot-reload in the codebase is
        for skills — internal/config/config.go:43-47, internal/api/skills.go:13).
        Deciding lines: cmd/joe/server.go:642-643 and internal/api/server.go:80-83.
    Therefore identity configuration is exclusively boot-time, and a
    "provisioning mode" where a running identity-less Joe mints its own identity
    and then flips the engine on WITHOUT a process restart is not buildable on the
    current architecture without new code.

================================================================================
PART 1 — WHAT MAKES THE ENGINE NON-NIL
================================================================================

ITEM 1 — Inputs read at the two construction sites; what "OIDC is configured"
resolves to.

Site A: cmd/joe/server.go:640-644
    640  oidcConfigured := cfg.Auth.OIDC.Configured()
    641  var policyEngine *rbac.PolicyEngine
    642  if saResolver.Configured() || oidcConfigured {
    643      policyEngine = rbac.NewPolicyEngine(rbacRepo)
    644  }
  Inputs read to decide engine vs nil:
    - `saResolver.Configured()` — true iff the resolver holds >=1 key
      (internal/auth/serviceaccount.go:74-76: `return r != nil && len(r.byKey) > 0`).
      The resolver is built from cfg.Server.ServiceAccounts at cmd/joe/server.go:630.
    - `oidcConfigured` — `cfg.Auth.OIDC.Configured()`, the config-value predicate
      `o.Issuer != "" && o.ClientID != "" && o.RedirectURL != ""`
      (internal/config/config.go:99-101).
  The downstream signals are set from the SAME predicate at the same site:
    services.RBACEnabled = policyEngine != nil  (cmd/joe/server.go:651)
    services.OIDCEnabled = oidcConfigured       (cmd/joe/server.go:656)

Site B: internal/api/server.go:76-84 (newPolicyEngine, used by the guarded accessor)
    76  func newPolicyEngine(services *core.Services) *rbac.PolicyEngine {
    77      if services.Config == nil || services.RBAC == nil {
    78          return nil
    79      }
    80      if !services.Config.Server.ServiceAccountsConfigured() && !services.Config.Auth.OIDC.Configured() {
    81          return nil
    82      }
    83      return rbac.NewPolicyEngine(services.RBAC)
    84  }
  Inputs read: services.Config != nil, services.RBAC != nil, then the same
  disjunction — `ServiceAccountsConfigured()` (internal/config/config.go:187-189:
  `len(s.ServiceAccounts) > 0`) OR `Auth.OIDC.Configured()`
  (internal/config/config.go:99-101). Built once when the accessor is created
  (internal/api/server.go:59), which is at boot.

  RESOLUTION OF "OIDC IS CONFIGURED": it resolves to the PRESENCE OF CONFIG VALUES
  (Issuer + ClientID + RedirectURL all non-empty), NOT to the successful
  construction of an OIDC-backed verifier or principal source. Proof:
    - `Configured()` reads struct string fields only (internal/config/config.go:99-101).
    - The engine constructor takes only the repo (internal/rbac/policy.go:15-17);
      it never receives an OIDC provider/verifier.
    - The OIDC provider (`NewOIDCProvider`) is constructed LATER and SEPARATELY,
      inside the `if oidcConfigured { ... }` block (cmd/joe/server.go:677-691), and
      is wired ONLY into the login Handlers (cmd/joe/server.go:678-689) and into
      EdgeAuth's `OIDCConfigured` boolean (cmd/joe/server.go:713) — never into the
      policy engine. So the engine's non-nil-ness is independent of whether any
      OIDC verifier was or could be built.

ITEM 2 — Is "OIDC endpoint present, but engine still nil" reachable? Does a
malformed/unreachable/partial OIDC fall through to nil (allow-all) or fail loudly?

  - PARTIAL config -> engine NIL is reachable: `Configured()` requires ALL of
    Issuer, ClientID, RedirectURL (internal/config/config.go:100). With only the
    issuer set (and no service account), `oidcConfigured` is false and the engine
    stays nil. This is the "an OIDC value is present but engine nil" state.
  - FULL-but-malformed/unreachable config -> engine NON-NIL, NO fall-through:
    once all three fields are non-empty, `Configured()` is true and the engine is
    built at cmd/joe/server.go:642-643 with zero validation of the issuer's
    reachability. OIDC discovery is explicitly deferred:
      internal/auth/oidc.go:62-66 — "NewOIDCProvider ... does NOT perform discovery
      here; the first request that needs the issuer triggers it."
      internal/auth/oidc.go:70-90 — `ensure()` runs `oidc.NewProvider(ctx, Issuer)`
      lazily; a failure returns `ErrOIDCUnavailable` (internal/auth/oidc.go:76-79)
      only to the login request, NOT to startup. Confirmed by the startup comment
      at cmd/joe/server.go:674-676: "Discovery is lazy, so a missing/unreachable
      IdP at startup is not fatal — only new logins fail."
    So a malformed/unreachable-but-complete OIDC config neither fails startup
    loudly NOR falls through to a nil allow-all engine: it produces a NON-NIL
    engine (enforcement ON) plus a login path that errors at request time.
  - Service-account config IS validated loudly at startup (contrast):
    NewServiceAccountResolver rejects empty/duplicate keys and duplicate names as a
    fatal error (internal/auth/serviceaccount.go:36-57), and cmd/joe/server.go:630-634
    returns exit code 1 on that error. OIDC has no equivalent boot validation.

ITEM 3 — What must exist for the service-account arm to flip the engine on, and
where it is read.

  - What must exist: at least one entry in cfg.Server.ServiceAccounts with a
    NON-EMPTY Key and a valid Name (internal/config/config.go:145-151, 156-160).
    Mechanism, not a DB row/file: service accounts are plaintext entries in the
    YAML config (internal/config/config.go:145-151 doc: "plaintext-at-rest in
    config"), OR injected via the JOE_API_KEY env var which folds into the reserved
    "server" account (internal/config/config.go:529-537, 552-560).
  - Where it is read for the gate:
      Site A: `saResolver.Configured()` (cmd/joe/server.go:642), backed by the
      resolver built at cmd/joe/server.go:630 from cfg.Server.ServiceAccounts;
      `Configured()` = `len(r.byKey) > 0` (internal/auth/serviceaccount.go:74-76).
      Site B: `services.Config.Server.ServiceAccountsConfigured()`
      (internal/api/server.go:80) = `len(s.ServiceAccounts) > 0`
      (internal/config/config.go:187-189).
  - There is no row/key/file lookup in a datastore for this gate — it is a
    length check over the in-memory config slice populated at boot.

================================================================================
PART 2 — BOOT-TIME VS RUNTIME IDENTITY CONFIGURATION
================================================================================

ITEM 4 — How service accounts come to exist; every creation path, labelled.

  ALL service-account creation paths are BOOT-TIME. There is no runtime path.
    [BOOT] YAML config: cfg.Server.ServiceAccounts is populated by config load
           (struct internal/config/config.go:145-160). Consumed by the resolver at
           cmd/joe/server.go:630.
    [BOOT] JOE_API_KEY env fold: setServerServiceAccountKey appends/updates the
           reserved "server" account during applyEnvOverrides
           (internal/config/config.go:534-537 calling 552-560). applyEnvOverrides
           runs at config load (boot), not at runtime.
  RUNTIME paths that create a service account / machine identity / bearer key:
    NONE. The admin REST surface (internal/api/admin.go:102-133) exposes
    zones (102-105), component-zones (107-109), policies (111-114), unassigned
    (116), admins (118-120), principal status disable/enable (122-124), and
    credential-status (131-133) — NO endpoint mints a service account or a bearer
    key. The MCP tool set is observability/graph/knowledge only and creates no
    identity (per project memory: joe_graph_query, joe_graph_related, joe_k8s,
    joe_metrics, joe_logs, joe_traces, joe_alerts, joe_knowledge_search — none
    identity-creating; not re-verified line-by-line in this pass —
    "not determinable from code" only insofar as exact MCP file:lines were not
    re-read here).

  Related-but-distinct RUNTIME writes (do NOT create service accounts and do NOT
  flip the engine):
    [RUNTIME] OIDC login upserts a HUMAN principal row (status=active) into the
              principals table on each successful login
              (internal/auth/handlers.go:279-288 calling UpsertPrincipal,
              internal/rbac/principals.go:66-85). This is a human identity-registry
              record, not a machine credential, and the login flow only exists when
              OIDC was configured at boot (cmd/joe/server.go:677-691).
    [RUNTIME] Admin bootstrap on login grants admin capability to the configured
              admin_email principal (internal/auth/handlers.go:247-255 ->
              GrantAdmin; idempotent UPSERT into admin_principals,
              internal/rbac/repository.go:659-680). Grants a capability to an
              already-authenticated principal; creates no authentication identity
              and does not change engine nil-ness.
    [RUNTIME] POST /api/v1/admin/admins (internal/api/admin.go:119) similarly marks
              an existing principal string as admin; no identity/credential minted.

ITEM 5 — How OIDC becomes active; constructed once at boot or runtime-(re)configurable?

  Constructed ONCE at boot, from config, and not reconfigurable while running:
    - The gate `oidcConfigured` is read once at cmd/joe/server.go:640.
    - The OIDC provider is built once inside the boot-time `if oidcConfigured`
      block (cmd/joe/server.go:677-691, `NewOIDCProvider(cfg.Auth.OIDC)` at line 679)
      and handed to the login Handlers registered then (cmd/joe/server.go:689).
    - There is no env override and no runtime mutation of cfg.Auth.OIDC (env
      overrides list, internal/config/config.go:487-546, contains no OIDC key).
    - The provider's only runtime state change is the lazy one-time discovery
      cache (`ready` flag, internal/auth/oidc.go:53-90); that activates the SAME
      boot-configured issuer on first use — it does not let a new/different issuer
      be configured at runtime.
    No code path re-reads or re-applies OIDC config after boot.

ITEM 6 — Can a nil engine become non-nil without a process restart? One-shot or
re-evaluated?

  ONE-SHOT at startup; no re-evaluation/reload path. Evidence:
    - cmd/joe/server.go:641-644 assigns `policyEngine` exactly once during server
      startup; it is a local within the startup function and is never recomputed.
    - internal/api/server.go:59 builds the accessor (and its engine via
      newPolicyEngine, internal/api/server.go:76-84) once, when the API server is
      constructed at boot (newAPIServer at cmd/joe/server.go:620).
    - The only "reload" machinery in the repo targets skills, not auth/RBAC config
      (internal/config/config.go:43-47; internal/api/skills.go:13-29). No SIGHUP,
      fsnotify, or config-watch path mutates Auth/Server identity config (grep over
      internal/ and cmd/ for reload/SIGHUP/fsnotify/config-watch found only the
      skills watcher and core.Services.SkillsWatcher,
      internal/core/services.go:150).
  Therefore a boot-nil engine stays nil for the life of the process; recovery to a
  non-nil (enforcing) engine requires a restart with changed config.

  SEPARATE PER-REQUEST ENGINE (does not change the above): the regime route builds
  its own engine on demand at internal/api/regime.go:49-52, gated only by
  `services.RBAC != nil` — i.e. it is non-nil even when the identity disjunction is
  unconfigured. This is a route-local capability gate for /regime, NOT the
  identity/auth gate, and it does not affect the boot-resolved policyEngine or the
  accessor's engine.

ITEM 7 — Any "setup"/"provision"/"first-run"/"bootstrap"/"onboarding" flow that
establishes IDENTITY from a running unconfigured state?

  NONE for identity-from-unconfigured. Specifically:
    - "Admin bootstrap" (internal/auth/handlers.go:25-26, 243-255;
      internal/rbac/repository.go:659-680) is a CAPABILITY grant to an
      already-authenticated OIDC principal; it presupposes OIDC was configured at
      boot. It does not establish authentication identity from an unconfigured
      state.
    - "Onboarding" in the codebase (internal/coreagent/discovery.go:15-56,
      internal/coreagent/agent.go:115-117, 472-490) is INFRASTRUCTURE discovery /
      .joe-file fact ingestion — unrelated to authentication identity.
    - "Provisioning" references are the per-login principal-registry upsert and
      CLI grant validation (internal/rbac/principals.go:46-54;
      internal/rbac/identity.go:77) — none mint a credential or flip the engine.
    There is NO HTTP/CLI/MCP flow by which a running, identity-less Joe configures
    its own service account or OIDC issuer and transitions the engine from nil to
    non-nil without a restart.

================================================================================
SUMMARY STATEMENT — boot-time vs runtime
================================================================================
Identity configuration is EXCLUSIVELY boot-time. The two engine-enabling inputs
(service accounts and OIDC) are both read once from config at startup
(cmd/joe/server.go:630, 640-644; internal/api/server.go:76-84), neither has a
runtime creation/reload path (service-account creation: config + JOE_API_KEY only,
internal/config/config.go:156-160, 534-537, 552-560; OIDC: config-file only, no env
override, built once at cmd/joe/server.go:677-691), and the engine's nil-ness is a
one-shot boot decision with no re-evaluation. The only runtime identity writes
(human principal upsert on OIDC login, admin-capability grants) presuppose identity
was already configured at boot and never change the engine's nil-ness. A runtime
provisioning/unlock mode that makes an identity-less Joe configure its own identity
and enable enforcement without a restart is therefore NOT buildable on the current
architecture without new code.
```
