```
================================================================================
INVESTIGATION: Is rbac_disabled a transient bootstrap state, or a supported
               standing deployment posture?
Claim under test: "RBAC is disabled only until someone has configured Joe; it
is a transient unconfigured-bootstrap state, not a supported standing
deployment mode."
Scope: read-only diagnosis. Evidence is file:line. Inference is labelled.
================================================================================

--------------------------------------------------------------------------------
VERDICT (one paragraph, answers the single question)
--------------------------------------------------------------------------------
rbac_disabled is reachable ONLY by absence of identity configuration. There is
NO explicit "turn RBAC off even though identity is configured" mechanism — no
flag, env var, or config key. The policy engine is constructed purely
positively: it is built (non-nil) when a service account OR OIDC is configured,
and is left nil otherwise. The deciding lines are the two mirrored construction
sites:

  cmd/joe/server.go:642-644
      if saResolver.Configured() || oidcConfigured {
          policyEngine = rbac.NewPolicyEngine(rbacRepo)
      }

  internal/api/server.go:80-83
      if !services.Config.Server.ServiceAccountsConfigured() && !services.Config.Auth.OIDC.Configured() {
          return nil
      }
      return rbac.NewPolicyEngine(services.RBAC)

Because the enable condition is a pure disjunction over identity inputs with no
negating override, the ONLY route to the nil engine (rbac_disabled, allow-all)
is "neither service accounts nor OIDC configured." This is consistent with the
bootstrap-only CLAIM in the narrow sense that the state is keyed to "not yet
configured." HOWEVER — and this is the load-bearing nuance the claim glosses —
nothing in the tree forces, nudges, or time-bounds an operator out of that
state: a fully unconfigured Joe runs allow-all indefinitely with only a single
startup WARN and no exposure guardrail tied to the unconfigured state. So:
"reachable only by absence of config" = YES (supports the literal claim), but
"strictly transient / not a runnable standing mode" = NOT ENFORCED by code —
absence-of-config is a stable, persistent runtime posture, not a self-clearing
bootstrap phase. The claim is TRUE about *how you reach* the state and
OVERSTATED about it being merely transient.

--------------------------------------------------------------------------------
1. WHAT MAKES THE ENGINE NIL (trace to construction)
--------------------------------------------------------------------------------
Enforcement seam: internal/access/access.go.
- access.go:128-131 — the allow-all short-circuit:
      if a.engine == nil {
          allowed = true
          zone = ""
          reason = "rbac_disabled"
      }
  A nil engine ⇒ every decision permitted, audit reason "rbac_disabled".
- access.go:70-74 (field doc): "A nil engine means RBAC is disabled (auth not
  configured) and every decision is permitted."
- The engine is injected, never self-constructed: access.go:90-92 New(...) takes
  `engine *rbac.PolicyEngine` and stores it verbatim. So nil-ness is decided
  entirely at the caller's construction site.

There are TWO construction sites, both gating on the SAME condition:

(A) Production daemon — cmd/joe/server.go:636-644:
      saResolver, saErr := auth.NewServiceAccountResolver(cfg.Server.ServiceAccounts)  // :630
      oidcConfigured := cfg.Auth.OIDC.Configured()                                     // :640
      var policyEngine *rbac.PolicyEngine
      if saResolver.Configured() || oidcConfigured {                                   // :642
          policyEngine = rbac.NewPolicyEngine(rbacRepo)                                 // :643
      }
    Note rbacRepo is ALWAYS wired earlier (cmd/joe/server.go:283), so in the real
    daemon the repo is never the missing input — only the identity config is.

(B) API server constructor — internal/api/server.go:76-84 (newPolicyEngine),
    the site the prompt cited as "server.go:76":
      if services.Config == nil || services.RBAC == nil { return nil }                 // :77-79
      if !services.Config.Server.ServiceAccountsConfigured()
          && !services.Config.Auth.OIDC.Configured() { return nil }                    // :80-82
      return rbac.NewPolicyEngine(services.RBAC)                                        // :83
    Comment (:68-75): "enforcement is enabled when a real caller principal can be
    established — i.e. a service account (Identity Phase D) OR OIDC login (Phase
    C) is configured. Otherwise the engine is nil and the accessor permits every
    decision."

PRECISE INPUTS THAT MAKE THE ENGINE NON-NIL — confirmed it is EXACTLY
"at least one service account OR OIDC configured":
- saResolver.Configured() ⇒ internal/auth/serviceaccount.go:74-76:
      return r != nil && len(r.byKey) > 0
  i.e. ≥1 service account in cfg.Server.ServiceAccounts (built at
  serviceaccount.go:36-57; mirror: config.go:187-189 ServiceAccountsConfigured
  = len(s.ServiceAccounts) > 0).
- oidcConfigured ⇒ internal/config/config.go:99-101:
      return o.Issuer != "" && o.ClientID != "" && o.RedirectURL != ""
  i.e. all three OIDC fields set.
CONFIRMED: the construction predicate is exactly the disjunction of those two.
There is no third positive input and (site B only) two extra NIL guards
(Config/RBAC nil) that the production path never trips.

--------------------------------------------------------------------------------
2. IS THERE AN EXPLICIT DISABLE SWITCH? (flag / env / config key)
--------------------------------------------------------------------------------
NONE FOUND. The construction logic is purely positive — "build when identity is
configured" — with no negating override anywhere.

- Config structs carry NO rbac-enable / disable-auth / dev-mode key:
  - ServerConfig (config.go:153-176): has ServiceAccounts, TLS*, RateLimit*,
    InsecureCookies — no `rbac.enabled`, no `disable_auth`.
  - AuthConfig (config.go:58-79): has OIDC, AdminEmail, SessionTTL,
    PostLoginRedirect — no enable/disable toggle.
  - A keyword search for rbac.enabled / rbac_enabled / disable_auth / dev_mode /
    "disable rbac" across internal/config and internal/rbac returned no matching
    config field or flag (only the Configured() helpers and method names).
- Env vars: the daemon reads only JOE_CONFIG (cmd/joe/server.go:164, config path)
  and JOE_MODE (cmd/joe/server.go:349 via internal/env/keys.go:21). JOE_MODE only
  accepts "observation", which raises the read-only WRITE FLOOR
  (cmd/joe/server.go:349-364) — that is an orthogonal write-suppression axis, NOT
  an RBAC toggle, and it makes Joe MORE restrictive, not less. No env var turns
  RBAC off.
- CLI flags: resolveConfigPath (cmd/joe/server.go:152-168) parses only --config.
  No --no-rbac / --insecure-style flag.
- The HTTP EnforcementMiddleware is a no-op regardless of engine
  (internal/rbac/middleware.go:78-83, "intentionally unused after Phase E
  demotion") — so it is not an alternate enable/disable lever either; all
  enforcement now lives at the accessor, gated solely by engine nil-ness.

CONCLUSION: There is NO path to rbac_disabled while identity IS configured.
The ONLY route to rbac_disabled is the ABSENCE of identity configuration
(no service accounts AND no OIDC). This is the finding the claim hinges on,
and it holds. (This supports the bootstrap-only claim; it does not, by itself,
prove the state is transient — see area 4.)

--------------------------------------------------------------------------------
3. PERSISTENCE / INTENT SIGNALS (what is emitted on entry to rbac_disabled)
--------------------------------------------------------------------------------
Joe DOES warn once at startup, but treats the state as an acceptable running
mode thereafter (no per-request warning, no refusal, no banner re-emit).

- Single startup WARN — cmd/joe/server.go:732-733:
      default:
          slog.Warn("API authentication disabled — set auth.oidc.issuer for
                     human login or server.service_accounts for machine access")
  This is the only "you are open" signal. It names the remedy but does NOT say
  "insecure", "do not use in production", or "configure before production".
  Tone is informational-with-remedy, not alarmed.
- The audit trail records every allowed call with reason "rbac_disabled"
  (access.go:131, written at access.go:150-159) — so the open posture is
  observable after the fact, but this is a silent data field, not an operator
  warning.
- Field/contract docs frame nil-engine as a normal supported mode, not a
  failure: access.go:70-74, access.go:108-111 ("A nil engine permits everything
  (RBAC disabled) ... the trail is complete even in unauthenticated dev mode"),
  and the construction-site comments (server.go:637-639, api/server.go:71-73)
  describe it as the intended "local/dev" / "single-user" posture.

NET: NOT silent — there is exactly ONE loud-ish WARN at boot — but the wording
stops short of "insecure / not for production," and after boot the state is
accepted silently as a normal operating mode. This is neutral-to-weakly-
supporting of "bootstrap intent": it tells you you're open and how to close it,
but does not assert the state is temporary or unfit for standing use.

--------------------------------------------------------------------------------
4. CAN IT BE MADE PERMANENT HARMLESSLY? (any guardrail / friction / forced exit)
--------------------------------------------------------------------------------
NO guardrail ties the open posture to reduced exposure, and NOTHING forces the
operator out of rbac_disabled. A fully unconfigured Joe runs indefinitely,
allow-all, with only the single boot WARN above.

- No setup wizard / first-run gate / refusal-to-start: runServerWithDeps
  (cmd/joe/server.go:170-811) boots to a listening server with auth unconfigured;
  the unconfigured branch (server.go:732-733) only logs and proceeds — there is
  no `return 1`, no interactive setup, no "configure before serving" stop.
- No bind restriction tied to the unconfigured state: the listen address is
  cfg.Server.Address (server.go:477), defaulting to "localhost:7777"
  (internal/config/constants.go:5, applied at config.go:409). BUT this default is
  a plain config default, fully overridable to a non-localhost / 0.0.0.0 bind,
  and it is in NO WAY conditioned on whether RBAC is configured. A keyword search
  for any "refuse non-localhost while unconfigured" / exposure guard in
  internal/config, cmd/joe, internal/api/server.go found none. So an operator can
  bind Joe to a public interface AND leave RBAC disabled with zero friction — the
  localhost default is convenience, not a security guardrail coupled to posture.
- Edge auth is fully open in this mode, compounding the exposure: when neither
  identity input is configured, EdgeAuth assigns every caller a fallback
  principal and rejects nothing — internal/auth/middleware.go:139,157-160
  ("Auth disabled ... every caller is the fallback principal and nothing is
  rejected. The policy engine is nil in this mode, so the downstream gate permits
  all").

NET: A fully-unconfigured Joe runs SILENTLY-OPEN after a one-line boot WARN, with
NO exposure guardrail (no localhost-only enforcement, no refusal to serve, no
forced setup) tied to the unconfigured state. The state can be made permanent
with no friction beyond that single log line. This is the strongest evidence
AGAINST the "merely transient bootstrap" framing: the code supports it as an
indefinitely-runnable posture.

--------------------------------------------------------------------------------
5. DOCUMENTATION / DECISION CORROBORATION
--------------------------------------------------------------------------------
Docs describe rbac_disabled as the intended LOCAL/DEV / pre-config posture, and
also openly acknowledge it as a runnable "permit-all" mode operators must guard
against relying on — i.e. docs treat it as a real standing mode, not a guaranteed
self-clearing phase. Doc intent and code reality AGREE (no divergence on the
mechanism); docs are actually MORE candid than the one-line WARN.

- docs/break-glass-access.md:106 — "Auth is enforced only when service accounts
  or OIDC are configured. With neither configured, Joe runs permit-all: every
  request is allowed and a bearer key grants nothing special." And :115-117 — a
  binary showing the "API authentication disabled" line is a "permit-all dev
  binary" and break-glass "is not a boundary." Frames it as dev posture, but
  treats it as a state a deployed binary can actually be in and must be checked
  for.
- docs/security-in-layers.md:511 — "Graceful degradation: auth disabled when no
  key configured (logs warning)." Explicitly a designed degradation mode.
- docs/DECISIONS.md:666-679 — records the open behaviour as "RBAC's current
  inert/permissive-by-default behavior": "the policy engine instantiates only
  when a service account or OIDC is configured ... with auth off the default
  identity is permissive and the access guard short-circuits allow-all with
  reason rbac_disabled." Crucially it calls permissive-default the "central
  obstacle the implementation must close" for a future full-capabilities/write
  mode (:677-679: "full mode demands auth ON and a live engine, so the fail-open
  path is UNREACHABLE in full mode") — i.e. the decision record itself does NOT
  treat rbac_disabled as a safe standing posture for write-capable operation; it
  treats today's permissive-default as a gap to be closed, while writes are
  floored off by other means.
- Prior investigations corroborate the mechanism verbatim:
  docs/investigations/accessor-promotion-state-axis.md:121 — "the engine is nil
  whenever services.RBAC == nil OR neither service accounts nor OIDC is
  configured ... In that posture the read-only floor on unassigned components
  does not execute"; and component-credential-registration-surface.md:100 —
  "if neither service accounts nor OIDC is configured, EdgeAuth is fully open."
- CLAUDE.md / MEMORY.md: no statement of intended posture for rbac_disabled
  (RBAC notes cover zones, middleware path, admin API — not the unconfigured
  default). Not determinable from those two files.

DOC-vs-CODE: No divergence on the mechanism (docs accurately state "enforced
only when SA or OIDC configured"). The only gap is emphasis: code emits one
mild WARN; DECISIONS.md treats permissive-default as an obstacle to close, not a
blessed standing mode — but neither code nor docs FORCE an exit from it.

--------------------------------------------------------------------------------
EXPLICIT ANSWERS TO ACCEPTANCE CRITERIA
--------------------------------------------------------------------------------
- Reachable ONLY by absence of identity config, or also by explicit disable?
  ONLY by absence of identity configuration. No explicit disable mechanism
  exists. Deciding lines: cmd/joe/server.go:642-644 and
  internal/api/server.go:80-83 (pure positive disjunction; no negating override).
- Silently-open or warns? WARNS ONCE at boot (cmd/joe/server.go:732-733), then
  accepts the state silently for the process lifetime; per-call audit rows carry
  reason "rbac_disabled" (internal/access/access.go:131) but emit no operator
  warning.
- Exposure guardrail tied to the unconfigured state? NONE. Default bind is
  "localhost:7777" (internal/config/constants.go:5) but it is an unconditional
  config default, freely overridable, NOT coupled to auth/RBAC configuration; no
  refusal-to-serve, no setup wizard, no non-localhost-bind block exists
  (cmd/joe/server.go:170-811 boots regardless).

BOTTOM LINE: The literal claim — "rbac_disabled is reached only by being
unconfigured, not by an explicit toggle" — is TRUE and code-confirmed. The
implied claim — "therefore it is strictly a transient bootstrap state, not a
supported standing mode" — is NOT enforced by the code: an unconfigured Joe is a
stable, indefinitely-runnable, allow-all posture with no forced exit and no
exposure guardrail. Whether that counts as "supported" is a policy/design
judgement — not determinable from code — but the runtime plainly permits it.
================================================================================
```
