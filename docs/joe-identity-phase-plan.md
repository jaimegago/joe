# Joe — Identity Implementation Phase Plan

Companion to `joe-identity-design.md`. That doc holds the decisions and rationale; this one is the execution roadmap and progress tracker.

## How this plan is used

- Each phase is a separate CC session with its own implementation prompt.
- The implementation prompt for a phase is written **after** the previous phase is merged and reported back, against the actual interfaces that phase produced — not against this plan's assumptions.
- Acceptance criteria are **tests CC writes and must pass**. Structural invariants become **static/architectural tests** (parse-the-code assertions, like the existing incident-exit AST guard at `regime_transitions.go:120-128`), not review notes. Logic correctness becomes **behavioral tests**. Where an invariant is "X is unbypassable," it needs both: behavioral (does enforcement work) + static (is it impossible to bypass).
- After each phase: report back the phase summary, key `file:line`, and any interface that differs from this plan. Then the next prompt is written.

## Resolution disclaimer

Phase A is specified precisely (we know the code it touches). Phases B–G decrease in resolution the further down they sit, because each depends on the shape the previous phases produce. Sections marked **[refined at report-back]** will be sharpened once their prerequisites land. This is intentional; do not treat the coarse later sections as final requirements.

## The non-negotiable ordering

A → B → E is a safety chain. The invariant "no path to an adapter or the graph store bypasses the guarded accessor" must hold at **every merge point**, not only at the end. Concretely: enforcement must be below the transport (A) and evaluating the real principal (B) **before** the loopback is removed (E). Removing the loopback first creates a window where the loop runs ungoverned — the exact bug class this whole effort fixes. F and G require per-session principals and so come after C.

Within that constraint: A → B → C → D → E → F → G is the planned order. D (service-account keys) can move earlier if convenient; it does not block the safety chain.

---

## Phase A — Enforcement below the transport

**Goal:** Introduce the guarded accessor in front of `services.Adapters` and `services.Graph`, with `IsAllowed` evaluated inside it. Route the existing HTTP handlers through it. Declare actions on adapter methods. Behavior-preserving and still single-principal.

**Entry conditions:** none (first phase).

**Exit conditions:**
- A single guarded-access seam is the only path to any adapter or the graph store.
- `IsAllowed` is called inside that seam, keyed on `(principal, sourceID, action)`, where `action` is read from the adapter method's own declaration (not inferred from HTTP verb).
- HTTP handlers delegate through the seam; they no longer call adapters directly.
- Observable behavior is unchanged (single principal still resolves as today; same allow/deny outcomes for the same inputs).

**Acceptance tests:**
- *Static:* no package other than the accessor package reaches the adapter packages / `services.Adapters.Get` / `services.Graph` directly. (AST/import-graph assertion. This is the load-bearing invariant.)
- *Static:* every adapter method that the accessor can dispatch declares an action.
- *Behavioral:* for each infra kind, a principal with a matching grant is allowed and one without is denied, through the accessor.
- *Behavioral:* existing HTTP-path RBAC outcomes are preserved (regression — same requests, same 200/403 as before A).

**Known risk:** the action-declaration mechanism on adapter methods is the one place A touches many call sites. Keep the declaration next to the method.

---

## Phase B — Set-shaped IsAllowed + real principal threading

**Goal:** Convert `IsAllowed` to evaluate a set of principals (union of grants), populated with size 1 at launch. Thread the real ctx principal into the accessor instead of relying on the single configured principal.

**Entry conditions:** A merged; accessor signature known.

**Exit conditions:**
- `IsAllowed` takes a principal set; permitted if any member has a matching grant; model stays additive/allow-only.
- The accessor receives the caller principal from ctx (`PrincipalFromContext`), not a hardcoded/server principal.
- Still behavior-preserving in practice (single configured principal until C), so outcomes don't change yet.

**Acceptance tests:**
- *Behavioral:* set with one granted principal → allow; set with one ungranted principal → deny; (forward-looking) set where any member is granted → allow.
- *Static:* the accessor's principal argument originates from ctx on the request path, not a constant. **[refined at report-back — exact assertion depends on A's signature]**
- *Behavioral:* regression — outcomes identical to post-A for the single-principal case.

---

## Phase C — OIDC login + sessions + principal mapping + admin bootstrap

**Goal:** Real human identity at the edge. Single configurable OIDC issuer (auth-code + PKCE, JWKS validation, `email_verified` required). Server-side session + `HttpOnly; Secure; SameSite=Lax` cookie. First-login creates a zero-zone `user:` principal; config-designated admin email becomes admin on first login. CLI zone provisioning.

**Entry conditions:** B merged; accessor evaluates real ctx principal.

**Exit conditions:**
- A human can log in via OIDC and receive a session cookie.
- Their verified email becomes a `user:<email>` principal flowing to the accessor.
- First login = authenticated but zero zones until an operator grants them via CLI.
- Configured admin email → admin authority on first login.
- `email_verified != true` tokens are rejected.

**Acceptance tests:**
- *Behavioral:* successful OIDC flow yields a session and a `user:` principal; a token with `email_verified=false` is rejected.
- *Behavioral:* a freshly-provisioned user with no zones is denied all infra; after CLI grant, allowed on the granted zone.
- *Behavioral:* the configured admin email gains admin on first login; a non-admin does not.
- *Behavioral:* `SameSite=Lax` does not break the OIDC callback (the cookie survives the redirect). **[refined at report-back — test harness for the redirect TBD]**

**Open/verify before this phase:** GitHub-direct login is OAuth2 not OIDC — keep GitHub out of C (single OIDC issuer only); revisit as a later adapter.

---

## Phase D — Service-account keys

**Goal:** Named API keys → `svc:<name>` principals for machines (MCP, CI, scripts).

**Entry conditions:** B merged (accessor evaluates real principal). Can run before or after C.

**Exit conditions:**
- A named API key resolves to a distinct `svc:` principal.
- MCP and CI authenticate as their own `svc:` principal hitting the same accessor.

**Acceptance tests:**
- *Behavioral:* two distinct keys resolve to two distinct `svc:` principals with independent grants.
- *Behavioral:* MCP calls resolve to a `svc:` principal, not a human one. **[refined at report-back]**

---

## Phase E — Remove the loopback

**Goal:** Point the loop's tool registry at the in-process guarded accessor instead of a loopback `client.Client`. Tools call adapters/graph in-process through the accessor. Delete the redundant graph HTTP round-trip.

**Entry conditions:** A and B merged (accessor enforces on the real principal). **This is the gate — E must not precede B.**

**Exit conditions:**
- The loop reaches infra/graph in-process via the accessor; no loopback HTTP self-call remains.
- The loop's tool calls are evaluated against the real caller principal (no more server-key re-auth).
- External HTTP API (CLI, Web UI, MCP) is unchanged.

**Acceptance tests:**
- *Static:* no loopback `client.Client` is constructed for in-process tool execution; the registry is wired to the accessor. **[refined at report-back — depends on A's accessor shape and E's wiring]**
- *Behavioral:* a loop run as principal P with a grant succeeds; the same run as a principal without the grant is denied at the tool call (proves the loop now enforces per-user).
- *Behavioral:* external API surface regression — CLI/Web UI/MCP still function over HTTP.

---

## Phase F — Audit, append-only

**Goal:** One append-only audit table in the existing store. Written at the accessor decision point. Redirect regime/captain transitions (declare/attach/transfer/resolve) into it so incident history survives resolve.

**Entry conditions:** C merged (real per-user principals exist to attribute). E ideally merged (so loop decisions are attributable to real principals).

**Exit conditions:**
- Every accessor decision (allow and deny) writes one audit row: timestamp, principal, action, zone, source, decision, reason.
- Incident transitions write durable audit rows; `declared_by_principal` (or its successor) is no longer destroyed on resolve.
- Append-only is enforced by insert-only code + a DB trigger that raises on UPDATE/DELETE.

**Acceptance tests:**
- *Behavioral:* an allowed action and a denied action each produce exactly one correct audit row.
- *Behavioral:* declaring then resolving an incident leaves a durable record of who declared it and when (the bug-3 regression test — must fail on today's code, pass after F).
- *Static/Behavioral:* UPDATE or DELETE against the audit table is rejected (trigger test).

---

## Phase G — Captain gate on the user loop

**Goal:** Move the §C captain-session gate and §B1 principal substitution onto the `agentloop` executor (the loop users drive), so incident-mode mutation ownership is actually enforced. Concept unchanged; wiring fixed.

**Entry conditions:** B and E merged (per-session principal flows through the loop). **[refined at report-back — exact dependency on E's executor wiring]**

**Exit conditions:**
- In incident regime, the §C gate evaluates on the user task loop, not only the Core Agent.
- The gate can only deny; RBAC authority is unchanged in/out of incident mode (the standing invariant).
- Becoming captain / entering / resolving incident is audited (overlaps F).

**Acceptance tests:**
- *Behavioral:* in incident regime, a non-captain session's mutation through the loop is refused and redirected; the captain session's mutation proceeds (subject to its normal RBAC).
- *Behavioral:* RBAC outcomes are identical in and out of incident mode for the same principal (proves no elevation).
- *Static:* the gate is wired on the `agentloop` path. **[refined at report-back]**

---

## Progress tracker

- [x] A — enforcement below transport (merged 2026-05-29; see DECISIONS.md D-0004)
- [x] B — set-shaped IsAllowed + real principal (merged 2026-05-29; see DECISIONS.md D-0005)
- [ ] C — OIDC + sessions + bootstrap
- [ ] D — service-account keys
- [ ] E — remove loopback
- [ ] F — audit append-only
- [ ] G — captain gate on user loop

Resume-cold protocol: design doc + this tracker + "Phase X merged, here is what it produced" is sufficient to regenerate the next prompt from a fresh session.
