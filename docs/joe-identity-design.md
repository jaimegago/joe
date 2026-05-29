# Joe — User Identity & Authentication Design

Status: design, pre-implementation. Target: CC implementation.
Scope: replace single-principal authn with real multi-principal identity, move RBAC enforcement below the transport, remove the loopback, make audit first-class, fix the captain gate.

This doc records decisions *and why*, so future-me reading it remembers the reasoning, not just the conclusion. Code facts cited as `file:line` come from two read-only CC investigations run during the design session (the "authorization layer" report and the "loopback vs in-process" report); treat those as the ground-truth snapshot the design was built against.

---

## 0. The problem this fixes

A launch-readiness audit found the authorization layer is multi-principal-ready but the authentication layer is single-principal. Three distinct correctness bugs, not one:

1. **Authz/authn mismatch.** `rbac_policies.principal` is free TEXT and `IsAllowed` can express rules for any number of principals, but `APIKeyProvider` maps the single configured API key to a single configured principal (`apikey.go:19-23`, wired `main.go:544-545`). There is no user directory, login, or session. The policy table speaks a vocabulary the front door cannot pronounce.

2. **Captain gate on the wrong loop.** The §C captain-session mutation gate and §B1 principal substitution live in `DurableExecutor`, which is wired only around the Core Agent (`main.go:512-515`) — onboarding/refresh, *not* the user task loop. The loop users actually drive (`/api/v1/tasks`, `/api/v1/tasks/stream` → `agentloop.Agent.Run`, `tasks.go:158`, `tasks_stream.go:147`) uses a plain executor with no gate. The incident-mode design is unenforced on the path that matters.

3. **Incident history erased on resolve.** `system_regime.declared_by_principal` is set to NULL when an incident resolves (`regime_transitions.go:210-214`); `session_captains` rows cascade-delete with the session (`009:62`). There is no audit table anywhere. The most consequential events in the system — incident declared, captain assumed, incident resolved — have no durable trail.

These are correctness bugs: the code does not do what its schema claims. Everything else discussed (groups, admin UI, JWT, MCP identity pass-through) is a *feature* and is deferred. The line is: ship the correctness fixes with minimum mechanism; leave cheap seams so deferring features does not force a re-open of this code later.

---

## 1. Root-cause framing (the one insight the whole design hangs on)

The three bugs share a single root cause: **the loopback HTTP hop is an identity reset.**

The loop's tools reach all infrastructure (k8s, prometheus, git, argocd, loki) *and* the graph store by calling joe-core's own `/api/v1` over localhost, authenticated with the server's own API key (`tasks.go:216-232`; all tools confirmed loopback-only, no in-process calls — loopback report items 2, 5). The real caller principal is present in the Go context right up to the point the loopback client is built (`tasks.go:149-158`, set at `middleware.go:51`), then discarded; the far side re-authenticates as the server key (loopback report item 7).

So RBAC-per-user, captain, and per-user audit are all broken for the same reason: identity dies at a network boundary that does not need to exist.

**That boundary is accidental.** It is a fossil of the old two-binary / two-agentic-loop design. joe-core is now the only "Joe"; CLI, Web UI, and MCP are thin external clients to the REST API; joe-core does not need to talk to itself over HTTP. The adapters and the graph store are already held in-process (`services.Adapters`, `services.Graph`, `core/services.go:38`) and take plain Go types — no HTTP dependency (loopback report items 3, 6). The graph case is the clearest tell: the loop takes an HTTP round-trip to reach a store it already has a direct handle to.

### Why K8s does the opposite, and why that does not apply here

K8s components authenticate to the API server over the network because they are genuinely separate processes on separate machines — the control plane is distributed from day one, and each hop is a real trust boundary. Joe inherited the *shape* of that inter-component auth without the distribution that justifies it. The design principle:

> **Authenticate at real boundaries; pass identity by context within a process.**

Joe has exactly one real boundary: the edge where external clients (CLI, Web UI, MCP) hit the REST API. Authenticate there and nowhere else. If joe-core is ever distributed, that is the moment Joe becomes K8s-like and the explicit-principal parameter (below) becomes a wire-carried identity — deliberately, not by accident. Designing that now would be over-engineering.

---

## 2. Decisions

Each decision states the choice, then why, then what it explicitly rejects.

### 2.1 Human identity source: OIDC, single configurable issuer

Humans authenticate via a single configurable OIDC issuer (authorization-code flow with PKCE; validate the ID token against the issuer's JWKS). Configure issuer URL + client ID + client secret. One code path serves Google, Entra/Azure AD, Okta, Auth0, Keycloak, Cognito, Dex, GitLab.

Why: Joe stores zero human credentials and inherits none of the password surface (hashing, reset, lockout, rotation). A reviewer trusts the IdP's crypto, not Joe's. Offline / zero-external-dependency human login was explicitly ruled out as a launch requirement (decision: external IdP, full stop), so a local users table is dead weight.

Rejects: local users + passwords; a multi-provider matrix; pluggable-multi-IdP from day one.

GitHub caveat (verify before relying on it): GitHub's *user* login is OAuth2, not OIDC — no ID token, no standard discovery. Supporting "Login with GitHub" means either a GitHub-specific adapter or fronting GitHub with Dex/Keycloak so Joe only speaks OIDC. Treat GitHub-direct as v2; the single-OIDC-issuer model covers the rest of the list today.

### 2.2 Principal representation: prefix-typed string, no migration

Keep the existing `principal TEXT` column (`006:23`). Encode kind as a reserved prefix:

- `user:<verified-email>` — a human, keyed on the OIDC `email` claim
- `group:<name>` — an IdP group (seam only in v1; see 2.7)
- `svc:<name>` — a service account / machine identity

Why: the only job "typing" does is stop a user named `alice` colliding with a group named `alice`. A prefix gives that for free with zero schema change. K8s uses the same trick (`system:` reserved). Reserve the three prefixes; reject any IdP-supplied email that would collide with a reserved prefix (does not occur in practice — emails do not start with `group:`).

Email keying requires `email_verified == true` on the token. An IdP that lets users set an arbitrary unverified email is otherwise an impersonation vector. **Reject tokens where `email_verified` is absent or false.** This is a hard assertion, not a nicety.

Known cost accepted: `email` is reassignable (person leaves, IT recycles the address, new hire inherits zones). This is the same tradeoff K8s operators accept for readable audit logs. Mitigation is operational (deprovision on offboarding), not structural, for v1. If this bites, the upgrade path is a minted internal ID bound to `sub` with email as display — but that is deferred, not built.

Rejects: a `principal_kind` column (migration 012 already declined one); keying on opaque `sub` (unreadable audit logs).

### 2.3 Token model for the Web UI: server-side session + cookie

On successful OIDC login, mint a server-side session (row in the existing SQLite store) and set a cookie: `HttpOnly`, `Secure`, `SameSite=Lax`.

Why server-side session over JWT: JWT's only real advantage is stateless validation across a fleet. joe-core is a single non-distributed binary with the database right there, so statelessness buys nothing and costs a revocation problem (a JWT cannot be un-issued; a session row can be deleted). Server-side sessions give immediate logout/revoke.

Why `SameSite=Lax`, not `Strict`: `Strict` would break the OIDC login flow itself — the browser will not send the session cookie on the callback redirect because it originates from the IdP's domain (a cross-site navigation), so the app treats the returning user as a new visitor (verified, web-security sources this session). `Lax` sends cookies on top-level navigations including the OAuth redirect, while still blocking cross-site POSTs.

CSRF posture: defense-in-depth, because SameSite is necessary but not sufficient (no SameSite level is guaranteed immunity, and it does not cover same-origin client-side CSRF — verified, OWASP). Required:
- `SameSite=Lax` session cookie (above).
- The API must not perform state-changing actions via GET (so Lax's allowance of cross-site GET is harmless).
- The OIDC login flow uses the `state` parameter (and PKCE) to protect the callback.
- The temporary state cookie used during the OAuth flow needs its own treatment if the flow crosses sites.

Rejects: bearer-JWT-in-localStorage (XSS-exfiltratable, no revocation); "paste your API key into a text field" as a login.

### 2.4 Service accounts / machine identity: named API keys

Machines (MCP server, CI, scripts) authenticate with named API keys, each mapped to a `svc:<name>` principal. Two authentication mechanisms total — OIDC for humans, API keys for machines — which is correct: interactive vs non-interactive are genuinely different, and forcing machines through OIDC client-credentials is more complex for a script author, not less. They converge where it matters: both resolve to a principal hitting the same `IsAllowed`.

MCP is a service account. Confirmed: the MCP server is a genuine external HTTP client in a separate process (`joe mcp` → `JOE_SERVER`/`JOE_API_KEY` → `client.Client` over stdio-to-HTTP, `cmd/joe/main.go:286-303`, loopback report item 8). It is not in-process and not a pass-through, so "MCP carries the human caller's identity" is not even tempting for v1 — it authenticates at the edge with its own key like any other external client.

Rejects: unifying humans and machines onto one mechanism; MCP identity pass-through (deferred).

### 2.5 Enforcement moves below the transport (the spine)

This is the central structural change. Today `IsAllowed` is called in exactly one production place: `EnforcementMiddleware` on the HTTP transport (`middleware.go:105`). No adapter, tool, graph store, or executor calls it (loopback report item 4). Therefore: **the moment the loop calls the in-process adapters directly, RBAC vanishes from the loop's path** unless enforcement is moved first.

Introduce a **guarded accessor** at the point both paths already converge — `services.Adapters.Get(sourceID)` and `services.Graph`. Described as a behavior, not code:

- A single guarded-access function/type that takes `(ctx, principal, sourceID, action)`.
- It calls `IsAllowed(ctx, principalSet, sourceID, action)`; on deny it returns a typed permission error and performs no infra call; on allow it delegates to the resolved adapter (or graph store) and returns its result.
- It is the *only* path to adapters/graph for both callers: the HTTP handlers and the in-process executor both go through it.
- It is also the single point that writes the audit record (see 2.6) — decision is made and recorded in the same place.

The HTTP `EnforcementMiddleware` may remain as a coarse outer gate, but it is no longer the authoritative check. Authoritative enforcement is at the convergence point.

Invariant to assert in review: **there is no path to an adapter or the graph store that does not pass through the guarded accessor.** A second ungoverned path would recreate bug #2 in a new location.

### 2.6 Audit: one append-only table, written at the decision point

One audit table in the existing SQLite store. Columns (minimal useful set): timestamp, principal, action, zone, source, decision (allow/deny), reason, and a context discriminator for incident/captain events. Insert-only in code; add a SQLite trigger that raises on UPDATE/DELETE as belt-and-suspenders for the append-only guarantee.

Written by the guarded accessor (every infra decision) and by the regime/captain transitions (declare, attach, transfer, resolve) — the latter redirected here instead of into the mutable rows that currently get erased (bug #3). This is not new mechanism for transitions; it is pointing existing writes at a durable target.

Rejects: event-sourcing, a separate audit store, hash-chaining (over-engineering for v1; the trigger + insert-only discipline is the v1 integrity guarantee).

### 2.7 IsAllowed becomes set-shaped (size 1 at launch)

Change `IsAllowed` to evaluate a *set* of principals (union of grants — permitted if any principal in the set has a matching grant), but populate the set with only the user's own `user:` principal at launch.

Why now: this change is nearly free while `IsAllowed` and its callers are already being rewritten to take the real principal. Retrofitting it later means re-touching the central decision function, every caller, and the loop threading a second time — exactly the expensive surface being opened now. The model stays additive/allow-only (no deny rules — consistent with current `policy.go:57-63` and with K8s). Groups (`group:` entries in the set, populated from the IdP `groups` claim) drop in as v2 with no change to the evaluation shape.

Rejects: keeping single-principal arity (forces a second rewrite later); adding deny rules (K8s deliberately rejects them; union-of-grants cannot express a clean per-user exception, and we are not smarter than sig-auth here).

### 2.8 Action is declared on the adapter method

Each adapter method declares its action (read / query / mutate / delete) as a property of the method. The guarded accessor reads it from there.

Why: the method already knows its own semantics (`GetPodLogs` is read; a future `ScaleDeployment` is mutate), so the declaration sits next to its truth and cannot drift. The current HTTP-method→action guess (`actionFromRequest`) cannot even produce `query` and is a transport-layer inference that does not belong below the transport. Reversible if wrong (move a tag, not a redesign).

### 2.9 Principal mapping & bootstrap

- New human, first login: identity is authenticated by the IdP; **authority is operator-provisioned.** First login creates the `user:` principal binding with zero zones. The user can authenticate but can do nothing until an operator grants zones. (Self-service join is deferred.)
- First admin (chicken-and-egg): a config-designated admin email. The first principal whose verified email matches the configured admin email is granted admin authority on first login. This is the only bootstrap path; document it as such.
- Zone provisioning (day-one and day-100 operator experience): **CLI command only for v1.** Operator grants/revokes zones to a `user:` or `svc:` principal via CLI. Admin UI and admin HTTP endpoint are deferred behind this seam.

Rejects: self-service zone acquisition; admin UI in v1.

### 2.10 Captain: unchanged in concept, fixed in wiring

Captain is a session-ownership concurrency control, not a privilege escalation and not a loop-autonomy mode (confirmed: captaincy never widens what `IsAllowed`/`HasZoneAccess` returns; the §C gate is deny-only and structurally isolated from RBAC — captain report items 2, 3). The settled invariant stands:

> **Incident mode changes who-can-mutate-through-Joe (concurrency), never what-authority-they-have (RBAC). RBAC is the floor; incident never lifts it.**

The fix is wiring, not redesign: the §C gate and §B1 substitution must move onto (or be duplicated on) the `agentloop` executor — the loop users drive — so the incident-mode design is actually enforced (fixes bug #2). Captain is, mechanically, *another consumer* of multi-principal authn: the captain transfer state machine, the "current captain unreachable → incoming assumes command" path, and the human-override-of-Joe-captain seam all assume sessions carry distinct authenticated identities. Single-principal made all sessions indistinguishable; this design supplies the per-session principal captain already depends on.

No captain re-investigation is needed; the prior design session is the source of truth for its semantics. Only the wiring changes here.

---

## 3. Sequencing (hard ordering constraint)

The order is not arbitrary. **Do not remove the loopback before enforcement is below the transport**, or there is a window where the loop runs ungoverned.

- **Phase A — Enforcement below transport.** Build the guarded accessor (2.5) with `IsAllowed` inside it; declare actions on adapter methods (2.8); route the HTTP handlers through the accessor. Behavior-preserving, still single-principal. Mergeable alone.
- **Phase B — Set-shaped IsAllowed.** Convert `IsAllowed` to a principal set, size 1 (2.7). Thread the real ctx principal into the accessor. Still single configured principal in practice until Phase C, so still behavior-preserving.
- **Phase C — OIDC + sessions.** Edge authn for humans (2.1), session + cookie (2.3), principal mapping + admin bootstrap (2.9). Now real per-user principals exist and flow to the accessor.
- **Phase D — Service-account keys.** Named API keys → `svc:` principals (2.4); MCP and CI use these.
- **Phase E — Remove the loopback.** Point `NewCoreRegistry` at the in-process guarded accessor instead of a loopback `client.Client`; tools call adapters/graph in-process via the accessor. Safe *because* the destination is guarded (Phases A/B). Delete the redundant graph HTTP round-trip too.
- **Phase F — Audit.** Append-only table (2.6); write at the accessor decision point; redirect regime/captain transitions there (fixes bug #3).
- **Phase G — Captain wiring.** Move the §C gate + §B1 substitution onto the `agentloop` path (2.10) (fixes bug #2).

Phases A→B→E are the critical safety chain. F and G can land once per-session principals exist (after C).

---

## 4. Failure modes

- **IdP unreachable.** Existing sessions (server-side, cookie-backed) keep working until expiry; only new human logins fail. Service-account API keys have no IdP dependency and are unaffected — important for incident time: machine/automation paths and any break-glass-via-service-account do not hard-depend on the IdP being up exactly when an incident may have taken it down.
- **Audit write fails (disk full, etc.).** Fail-closed for mutations: no audit record, no mutation. Fail-open for reads: a read proceeds even if its audit row cannot be written (lower stakes, availability preserved), with a loud operational alert. Rationale: audit is first-class for state-changing actions; blocking reads on audit-store failure trades too much availability for too little. State this tradeoff explicitly; it is a judgment, not a forced choice.
- **Session/token expires mid-task.** The running loop carries the principal in the Go context, not the cookie, so an in-flight task completes under the principal it started with. New requests require re-auth. (A long-running task does not get killed mid-execution by cookie expiry; it was authorized at kickoff and each tool call re-checks against the same in-ctx principal via the accessor.)
- **Deny mid-loop.** A tool call that the accessor denies returns a typed permission error to the loop; the loop surfaces the specific refused action and zone (consistent with the existing safety-articulation prompt expectation) rather than silently stalling.

---

## 5. Invariants to assert in review

These are the things a reviewer (or a future safety eval) will check. State them as testable assertions.

1. Identity is established exactly once, at the external edge; no component re-authenticates internally. (No loopback; no internal token minting.)
2. Every path to an adapter or the graph store passes through the guarded accessor; there is no ungoverned in-process infra path.
3. `IsAllowed` evaluates the real caller principal, not a server principal, on the user task loop.
4. Incident mode never increases any principal's authority; the §C gate can only deny. RBAC is evaluated identically in and out of incident mode.
5. The audit log is append-only (insert-only code path + DB trigger); incident transitions are recorded as durable events and are not erased on resolve.
6. The captain gate is enforced on the `agentloop` path that serves user requests, not only on the Core Agent.

---

## 6. Explicitly deferred (with the seam that makes deferral cheap)

- **IdP groups / group-based authz** — seam: `IsAllowed` is already set-shaped; add `group:` entries to the set from the `groups` claim. No evaluation change.
- **MCP human-identity pass-through** — seam: MCP is a normal service-account client today; pass-through is an additive identity-forwarding feature later.
- **Admin UI / admin HTTP endpoint for provisioning** — seam: CLI provisioning exists; UI is another caller of the same provisioning operations.
- **JWT / stateless sessions** — only relevant if joe-core is distributed; revisit then.
- **Distributed joe-core / remote inter-component auth** — seam: the accessor takes an explicit principal parameter; if a process boundary appears, that parameter becomes a wire-carried identity (the deliberate K8s model).
- **GitHub-direct login** — seam: single OIDC issuer today; add a GitHub OAuth2 adapter or front with Dex later.
- **Minted internal ID bound to `sub`** — only if email reassignment becomes a real problem; principal column already opaque-string-capable.

---

## 7. One-paragraph summary

Authenticate humans at the edge via a single configurable OIDC issuer and machines via named API keys; carry the resulting prefix-typed principal (`user:`/`group:`/`svc:`) by Go context, never re-authenticating internally. Move RBAC enforcement off the HTTP transport into a guarded accessor in front of `services.Adapters`/`services.Graph` that both the HTTP handlers and the in-process loop call, evaluating a set-shaped (size-1-at-launch) `IsAllowed` on `(principal, sourceID, action)` with the action declared on the adapter method, and writing every decision to one append-only audit table. Then — and only then — delete the loopback, so the loop's tool calls run in-process under the real user principal. Move the captain gate onto the user task loop and write incident transitions to the audit table so incident history survives resolve. Defer groups, admin UI, JWT, and MCP identity pass-through behind seams that do not require re-opening this code.
