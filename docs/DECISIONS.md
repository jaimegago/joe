# Joe — Decisions

Append-only project decision log. Newest entries at the top. Each entry
records what was decided, the basis (verifiable source, not assertion), and
what it supersedes. This file is normative: where a decision here conflicts
with prose elsewhere, this file states the project's position and the
conflicting prose is stale.

Format per entry: ID, date, decision, basis, supersedes, status.

---

## D-0016 — Identity registry (the `principals` table) + full RBAC admin REST/UI surface; the `zone`/`admin` operator CLI removed, REST is the sole RBAC writer

- Date: 2026-06-05
- Provenance (stated honestly): this body of work ("Identity Stages 1–5") shipped
  as five commits on `main` without a decision of record. Two read-only audits
  (`IDENTITY_MODEL_INVESTIGATION.md`, `UI_WIRING_AUDIT.md` — since moved to the
  private launch archive) had enumerated the exact gaps these stages close; the
  stages were implemented directly against those findings (migration 021's header
  cites `IDENTITY_MODEL_INVESTIGATION.md` Step 1). This entry records and ratifies
  what the shipped code does after the fact; it does not change it.

- Decision: Joe gains an authoritative, mutable identity registry and a complete
  admin surface over HTTP + UI, and the direct-DB operator CLI is retired so that
  the audited REST API is the single writer of RBAC/identity state.

  **(a) Identity registry — the `principals` table (Stage 1, commit `ef3d634`).**
  Migration `021_principals` adds the mutable per-user record the append-only
  `audit_log` could not provide: `principal` (PK), `created_at`, `status`
  (`active`|`disabled`, CHECK-constrained), `disabled_at`/`disabled_by`
  (disable provenance), `display_name`, `last_seen_at`. The read/write surface is
  a separate `rbac.PrincipalRepository` (`internal/rbac/principals.go`:
  `UpsertPrincipal`/`GetPrincipal`/`ListPrincipals`/`SetPrincipalStatus`) so
  existing `rbac.Repository` implementers need not grow identity methods.
  `SetPrincipalStatus` writes its audit row in the same transaction as the status
  change. This closes the "there is no users/principals/identities table" finding.

  **(b) Provisioning + disabled-at-mint enforcement (Stage 2, commit `2bc6ecc`).**
  The OIDC callback upserts the registry row on every login
  (`internal/auth/handlers.go` `UpsertPrincipal`, refreshing only `display_name`/
  `last_seen_at`; status/created_at/provenance are owned by `SetPrincipalStatus`).
  Session mint consults `status` and refuses a `disabled` principal at mint time —
  a disable takes effect on the next request, not just for new grants.

  **(c) Full admin REST surface (Stage 3, commit `95a4b63`).** All routes
  admin-gated via `requireAdmin` and audited (`internal/api/admin.go`): zones
  `GET/POST/PATCH/DELETE`; source-zones `GET/POST/DELETE` (assign + unassign);
  policies `GET/POST/POST /revoke/DELETE {id}`; **admins `GET` (roster) / `POST`
  (promote) / `DELETE` (demote)** — previously reachable only via CLI/bootstrap;
  **principals `GET` (Users page) / `POST {p}/disable` / `POST {p}/enable`**. The
  structural guard `admin_gate_guard_test.go` fails the build if any `/admin/`
  route is registered without the gate.

  **(d) The `zone`/`admin` operator CLI is removed; REST is the sole RBAC writer
  (commit `205448a`, breaking).** `cmd/joe/zone.go` and `cmd/joe/admin.go` — which
  wrote SQLite directly and so bypassed the HTTP gate + audit — are deleted. The
  remaining subcommands are `panic`, `unlock`, `review`, `mcp`, `slack`, `skills`,
  `incident`. Rationale: a single audited writer for RBAC/identity state; the
  direct-DB CLI was the last gate-and-audit-bypassing writer, and (c) made it
  redundant. This supersedes the Phase C CLI zone-provisioning of D-0006 and the
  `CLI_REMOVAL_CHECK.md` finding that the operator CLI persisted.

  **(e) UI admin management surface (Stage 5, commit `07fb2a4`).** `ui/src/pages/
  UsersPage.tsx`, `PrincipalsTable`, `AdminsTable`, `AdminForm`; the
  `ui/src/api/security.ts` functions `updateZone`/`deleteZone`/`removeZone`/
  `fetchPrincipals`/`disablePrincipal`/`fetchAdmins` are real implementations, no
  longer stubs. Closes the `UI_WIRING_AUDIT.md` gaps (no user discovery, no admin
  roster, no promote/demote, no zone edit/delete, no source-zone unassign).

- Basis: commits `ef3d634`, `2bc6ecc`, `95a4b63`, `205448a`, `07fb2a4` (all
  2026-06-05 on `main`); migration `internal/store/migrations/021_principals.up.sql`;
  `internal/rbac/principals.go`; `internal/auth/handlers.go` +
  `internal/auth/principal_admin.go`; `internal/api/admin.go` +
  `admin_gate_guard_test.go`; the `ui/src/pages` / `ui/src/components/admin` /
  `ui/src/api/security.ts` surface. The pre-state is the two archived audits, which
  documented every gap above as open.
- Supersedes: the relevant findings of `IDENTITY_MODEL_INVESTIGATION.md` and
  `UI_WIRING_AUDIT.md` (now closed); the CLI zone-provisioning path of D-0006; and
  `CLI_REMOVAL_CHECK.md`'s "the operator CLI is still present" conclusion (true when
  written, no longer true). Builds on D-0011 (admin as a dynamic capability),
  D-0012 (the admin gate), and D-0013 (admin-mutation audit) — this entry extends
  that gated+audited surface to identity and makes REST its sole writer.
- Status: active. Stages 1–5 committed on `main`.

---

## D-0015 — Context-management architecture: FIFO pruning, ingestion truncation, conservative model-window registry, and a distinct context-overflow terminal status

- Date: 2026-06-05
- Provenance (stated honestly): this entry exists because a read-only
  verification (`CONTEXT_MANAGEMENT_VERIFICATION.md`, lines 7 and 197) found that
  the context-management workstream — described in-task as a "locked
  launch-blocking decision" — had **no decision of record**. The work was
  self-labelled "Stream G context pass" in code comments only
  (`internal/store/migrations/019_llm_context_budget.up.sql:1`,
  `internal/api/tasks.go:362`); a code comment is not a decision of record. The
  verification graded the engine PRESENT-and-tested but the build narrative
  incomplete. Closing that narrative gap is part of the engineer-who-tests-
  honestly posture, so the locked choices are recorded here after the fact.
  The decisions below describe what the shipped code does; this entry documents
  and ratifies it, it does not change it.

- Decision: the context-management engine binds every assembled LLM prompt to the
  active model's published context window via six locked choices.

  **(a) Pruning strategy — FIFO oldest-first drop.** History that exceeds the
  per-turn input budget is trimmed by dropping whole messages from the OLDEST
  end until the estimated total fits (`internal/agentloop/session.go`
  `pruneToTokenBudget`), NOT by summarization and NOT by a sliding-window count
  as the primary mechanism (a `MaxMessages=100` count cap remains a secondary
  backstop). Rationale: deterministic, cheap, and requires no external LLM
  summarization call on the hot path — the behaviour an operator reads in the
  audit/SSE trail is predictable and reconstructable, never a model's lossy
  paraphrase.

  **(b) Most-recent-user-message protection invariant.** Pruning never advances
  past the last GENUINE user message — a `Role:"user"` turn with no
  `ToolResultID`, distinguished from a tool result that also carries
  `Role:"user"` (`session.go` `lastUserMessageIndex`) — even when that one
  message alone exceeds the budget. Tool-call/tool-result pair integrity is
  likewise preserved (never a leading orphaned result). Rationale: preserves the
  user's most recent intent; combined with per-message truncation (c) this keeps
  the turn coherent rather than dropping the very message being answered.

  **(c) Per-message ingestion truncation fractions — 25% / 50% with a 2000-token
  floor.** Before a message enters history, an oversized tool result is capped at
  `toolResultBudgetFraction = 0.25` of the turn budget and an oversized incoming
  user message at `userMessageBudgetFraction = 0.50`, with a
  `minTruncationTokenFloor = 2000` floor (`internal/agentloop/constants.go`,
  `session.go` truncate*, `tokens.go` `TruncateContent`). The elided middle is
  replaced with an explicit, recoverable marker ("re-invoke the tool with a
  narrower query"). Rationale: tool results are typically large and recoverable
  (re-invoke narrower); user input is small but cannot be re-fetched, so it gets
  the larger share and is shortened, never rejected. The floor protects small
  budgets from collapsing to nothing.

  **(d) Conservative unknown-model default — 100,000-token window / 4096
  output.** Any `{provider, model}` pair absent from the compile-time
  capabilities registry resolves to `defaultContextWindowTokens = 100000` /
  `defaultMaxOutputTokens = 4096` (`internal/llmusage/capabilities.go`
  `LookupCapabilities`), never an optimistic guess and never an error. Rationale:
  safety over capability — under-using a model's context is recoverable;
  overrunning a window fails unpredictably. Trade-off acknowledged: an operator
  running a new model loses available context until a registry entry (with cited
  source + capture date, as the shipped Claude/Gemini rows carry) is added.

  **(e) Context overflow is a distinct TERMINAL STATUS, separate from the D-0014
  `error_code` write-failure vocabulary.** A provider rejection for an oversized
  prompt is classified at the LLM boundary into the typed sentinel
  `llm.ErrContextOverflow` and mapped to the terminal task status
  `"context_overflow"` (`internal/api/tasks.go` `taskStatus`, via `errors.Is`,
  never a string match), a sibling of `runaway_terminated` /
  `cost_limit_exceeded`. It is deliberately NOT folded into D-0014 Item 8's
  `error_code` codes (`incident_mode` / `zone_denial` / `internal_error`).
  Rationale (per `CONTEXT_MANAGEMENT_VERIFICATION.md` Cross-Cutting C): terminal
  `status` is for a turn that FAILED; `error_code` is for a non-fatal tool-write
  denial that rides on an otherwise-`completed` turn. Distinct lifecycles,
  distinct vocabularies — overflow fails the turn, so it owns the status channel.
  Detection-and-reporting only: no retry, no automatic budget adjustment.

  **(f) Audit policy — overflow audited, pruning and truncation not (per
  Cross-Cutting A).** Pruning writes NO audit row (high-volume, routine; the SSE
  `history_trimmed`/`messages_dropped` flags are the right surface).
  Per-message truncation writes NO audit row today (a deferred fast-follow:
  the user sees the marker; an auditor does not — tracked, not closed here).
  Context overflow IS audited — a `KindLLMLimitTriggered` /
  `llm_context_overflow` row for parity with the runaway ceiling's
  `llm_runaway_terminated` row — closed in this session (commit "feat(audit):
  write a context-overflow audit row…", `internal/api/tasks.go`
  `writeContextOverflowAudit`). The UI control gap (verification Item 9 /
  launch blocker) is likewise closed this session (commit "feat(ui):
  context-budget control…").

- Basis: `CONTEXT_MANAGEMENT_VERIFICATION.md` (read against `main` at HEAD;
  Items 1–9, Cross-Cutting A/B/C, and the prioritized gaps) verified against the
  code it cites: migration 019 (`internal/store/migrations/019_llm_context_budget.up.sql`),
  the `internal/agentloop` package (`session.go`, `tokens.go`, `constants.go`,
  `contextbudget.go`, `agent.go`), the `internal/llmusage` capabilities registry
  (`capabilities.go`), and the typed sentinel + status mapping
  (`internal/llm/errors.go`, `internal/api/tasks.go`). The per-turn context fit
  is applied before request assembly and is independent of the Stream G
  session-token ceiling, which is checked post-`Chat` from real provider usage
  (verification Item 6); the two map to distinct terminal statuses and distinct
  audit actions.
- Supersedes: nothing — first context-management decision. Relates to D-0014
  (the context budget is exposed through the same admin-gated, audited
  `/api/v1/llm/settings/*` surface whose structural guards D-0014 added; this
  entry's `error_code`-vs-`status` boundary is the deliberate counterpart to
  D-0014 Item 8) and D-0003 (the `context_overflow` status rides the SSE final
  event as an additive, `omitempty`-compatible field, consistent with the Phase 2
  streaming protocol).
- Status: active. Engine PRESENT and tested; the UI control and the
  context-overflow audit row are closed this session. The two
  defer-with-documentation fast-follows the verification named — per-message
  truncation auditing (Cross-Cutting A(b)) and per-turn budget-consumption
  telemetry ("used X of Y", verification gap 4) — remain open and are NOT closed
  by this entry.

---

## D-0014 — Close the Stream G structural-guard gap and the operator-surface launch blockers (incident CLI, zero-zone dead-end, incident banner, write-failure feedback)

- Date: 2026-06-04
- Decision: Close the remaining launch blockers found by two read-only
  verifications — `STREAM_G_VERIFICATION.md` (the LLM instrumentation / admin
  settings subsystem) and `OPERATOR_SURFACE_VERIFICATION.md` (incident mode +
  user management). Eight discrete changes, each its own commit, build-green
  and tests-passing after each. The work is two clusters:

  **(a) Stream G structural-guard gap.** `STREAM_G_VERIFICATION.md` (Item 7 /
  cross-cutting C) found the D-0013 admin guards parse only `admin.go` /
  `adminHandler`, so the LLM admin mutators on `llmSettingsHandler` /
  `llmUsageHandler` (`internal/api/llmsettings.go`, `llmusageapi.go`),
  registered under `/api/v1/llm/`, were covered by NO AST invariant — the
  exact regression class D-0012/D-0013 closed for the RBAC surface remained
  open here. Closed with two structural guards
  (`internal/api/llm_admin_guard_test.go`) mirroring
  `admin_gate_guard_test.go` / `admin_audit_guard_test.go`:
  - `TestLLMAdminRoutes_MutatorsRequireAdminGate` — every mutating
    (POST/PUT/DELETE/PATCH) route, plus the per-principal usage GET
    (admin-only by design), must call `requireAdmin`. GET reads stay open per
    Stream G design.
  - `TestLLMAdminRoutes_MutatorsAudit` — every mutating route must route
    through `services.LLMSettings` (the MutationService), which is the
    ACTUAL Stream G audit writer: it persists the change AND writes the
    `KindLLMSettingsMutation` audit row in one transaction
    (`internal/llmsettings/service.go`), NOT `recordAdminAudit`. The guard
    asserts this path structurally rather than asserting a `recordAdminAudit`
    call that does not exist on this surface.
  Verb+pattern are parsed from the `fmt.Sprintf` registration literal. The
  gate invariant was break-tested (removing the gate from `handleSetCostLimit`
  turns the guard red naming that handler; restoring returns green) — an
  invariant never seen red proves nothing. Two adjacent Stream G test gaps were
  also closed: a route-level `RequireAdmin` test for `/llm-settings`
  (`ui/src/auth/RequireAdmin.test.tsx`; the section-level UsageTab test was the
  only prior coverage), and a skip-staged regression net for ChatStream/Embed
  usage recording (`internal/llmusage/recorder_test.go`) that activates when
  either provider stub
  (`internal/llm/claude/claude.go:141-148`,
  `internal/llm/gemini/gemini.go:202-209`) is implemented.

  **(b) Operator-surface launch blockers.** `OPERATOR_SURFACE_VERIFICATION.md`
  found the incident-mode enforcement is solid on both agentic paths (D-0010)
  but had NO human-facing trigger or feedback surface, and a new zero-zone
  user dead-ended silently. Closed:
  - **Incident CLI** (`cmd/joe/incident.go`, dispatched at
    `cmd/joe/main.go`): `joe incident status|declare|resolve` over the HTTP API
    (`GET/POST /api/v1/regime[/declare|/resolve]`), mirroring
    `cmd/joe/zone.go` / `admin.go`. `list` is an intentional stub — no
    `/regime/history` endpoint exists (verified ABSENT; durable history lives
    in the append-only `audit_log`) — printing audit-log guidance and exiting
    non-zero so the v1 limitation is explicit, not implied success.
  - **`/me` extended with the caller's reachable zones**
    (`internal/api/currentuser.go`): admin → every zone; non-admin → their
    granted zones; zero-zone → non-nil `[]`. This is the data dependency for
    the next two items.
  - **Zero-zone empty state** (`ui/src/components/chat/ZeroZoneEmptyState.tsx`):
    ChatPage renders "Access pending" in place of the doomed chat input when
    `rbacEnabled && !isAdmin && zones.length === 0`.
  - **Active-incident banner** (`ui/src/components/layout/IncidentBanner.tsx`):
    app-shell-wide, polls `/api/v1/regime` (30s), shows a top-of-page alert in
    incident mode. An app-shell concern — it never enters the chat-history
    snapshot.
  - **Differentiated write-failure feedback** (Item 8): a denied write surfaces
    a typed code — `incident_mode` (captain gate) vs `zone_denial` (RBAC) vs
    `internal_error` — classified by `classifyWriteFailure`
    (`internal/api/writefailure.go`) and injected into the loop via the new
    `agentloop.WithToolErrorClassifier`, so `agentloop` stays unaware of the
    gate/RBAC error types. Because a denied write does NOT terminate the
    agentic loop (the LLM receives the tool error and the turn still
    completes), the code rides on the turn-level `error_code` of the final
    event (first per-tool denial seen); the chat UI dispatches it to a specific
    message via `writeFailureMessage`.

- Honest scope notes:
  - The Item 8 backend classifier is exercised by unit tests over each typed
    error; the end-to-end chat path surfaces the code through the final
    event's turn-level `error_code` rather than a pre-stream HTTP 403, because
    in this architecture a tool-level denial is non-terminal and the original
    typed error is stringified onto the wire. The pre-stream 403 code path
    (`streamTask` onError) is wired too, for any future gate that refuses the
    whole task before streaming.
  - The Stream G GET reads remain intentionally un-gated (policy knobs, not
    credentials); only the per-principal usage GET is admin-gated, and the
    guard encodes that distinction explicitly.
- Basis: `STREAM_G_VERIFICATION.md` (Items 3, 7; cross-cutting A/C) and
  `OPERATOR_SURFACE_VERIFICATION.md` (items 5, 6, 8, 9, 11; prioritized
  launch blockers), both read against `main` at HEAD `2b16665`; the D-0013
  structural-guard pattern and the D-0010 captain-gate single-impl invariant.
- Supersedes: nothing. Extends D-0013 (structural gate+audit guards) to the
  Stream G admin surface and adds the operator-facing incident + RBAC surfaces
  the verifications found missing. The pre-existing invariants (D-0012 admin
  gate, D-0013 admin audit, D-0010 captaingate single-impl,
  `regime_invariant_test.go`) are unchanged and still pass alongside the new
  Item 1 guards.
- Status: active.

---

## D-0013 — The RBAC admin surface was gated (D-0012) but wrote zero audit rows; extend the audit vocabulary to cover authorization-config mutations

- Date: 2026-06-04
- Decision: D-0012 closed the GATE gap on the RBAC admin HTTP API
  (`internal/api/admin.go`) but explicitly left a second gap open and tracked
  it: the surface wrote **no** audit rows at all. The most
  authorization-critical mutations in the system — mint a zone, grant/revoke a
  policy, assign a source to a zone — were unrecorded, and gate denials left
  no trail. D-0013 closes that gap by extending Phase F's (D-0009) audit
  machinery to this surface, with Phase F's failure posture preserved
  exactly. Specifics:
  - **(a) The gap, precisely.** Phase F (D-0009) modelled the guarded
    accessor's DECISION point — every allow/deny the accessor makes against an
    infra adapter or the graph store, recorded as a `kind=infra_access` row.
    It never modelled mutations of the authorization CONFIGURATION the
    accessor reads. A zone's `allowed_actions`, a principal→zone policy, a
    source→zone assignment: changing these changes what every future accessor
    decision will permit, but the change itself produced no row. That is a
    different decision shape than the one Phase F's scope (the accessor's
    chokepoint) covered, which is why the vocabulary never grew to include it.
  - **(b) The fix — action vocabulary.** Nine action verbs were added to the
    existing audit vocabulary file (`internal/audit/audit.go`), NOT a parallel
    taxonomy: `zone.create`, `zone.read`, `policy.grant`, `policy.revoke`,
    `policy.read`, `source_zone.assign`, `source_zone.read`, plus
    `admin.grant` / `admin.revoke` (declared for the CLI-only admin-promotion
    path per `ADMIN_SURFACE_AUDIT.md`, so a future HTTP endpoint reuses them
    rather than inventing new strings), and `admin.access_denied` for gate
    refusals. All carry a new `kind=admin_access` — the admin-surface parallel
    of `infra_access`: one kind for the whole surface, discriminated by action
    + decision.
  - **(c) The fix — typed details.** Each row's `context` column uses the
    `{target, before, after}` shape Stream G locked for `llm_settings_mutation`
    rows (`internal/llmsettings.AuditCtxTarget`/`Before`/`After`, written by
    `MutationService.runMutation`). That shape is now a named type,
    `audit.Details`, documented inline; admin and settings mutations share the
    audit table with this typed details column, per the locked decision. For a
    create/grant the row carries the after-state; for a revoke the
    before-state; for a read that leaks structure, the requested resource.
  - **(d) The fix — failure posture, unchanged from Phase F §4.** The eight
    handlers write their row through `recordAdminAudit`, which routes the
    write's outcome through the same `audit.FailurePosture` helper every Phase
    F caller uses. Mutating actions fail CLOSED: the audit row is written
    BEFORE the repository mutation, and a failed write aborts before any state
    change (no row ⇒ no mutation), the same audit-before-act ordering the
    accessor uses. The three `.read` actions are read-class and fail OPEN: the
    read proceeds even if the row cannot be written, logged loudly. The
    read/mutate split lives in `isFailOpen`, extended to admit the three admin
    read verbs — the §4 invariant itself is untouched. Denials write a
    `decision=deny` row from `requireAdmin` (the only behavioural change to the
    gate, scoped to the optional denial-audit D-0012's `requireAdmin` left
    room for); the denial is enforced regardless of whether the row lands
    (denying is the fail-closed-safe direction), matching
    `captaingate.Wrapper.writeRefusalAudit`'s posture.
  - **(e) Schema touch — one migration, required not optional.** Admin-RBAC
    events had no semantically-correct home among the six kinds the
    migration-018 `audit_log.kind` CHECK admitted. An INSERT with an
    unadmitted kind fails the CHECK, and under (d)'s fail-closed posture that
    would break every admin mutation outright — so the kind had to be added to
    the CHECK to record admin actions at all. Migration 020
    (`020_admin_audit.up.sql`/`.down.sql`) widens the CHECK to add
    `admin_access`, following the byte-for-byte table-rebuild sequence
    migrations 017 and 018 established (SQLite cannot alter a CHECK in place);
    all other columns, defaults, the decision CHECK, the indexes, and the two
    append-only triggers are preserved verbatim. This is the only schema
    change; the typed details column (`context`) was already present from
    Stream G. The choice between this and reusing an existing (wrong-named)
    kind was made explicitly: a `kind` named for one surface holding another
    surface's rows would corrupt kind-based forensic queries, so a minimal
    widening was preferred — the same reasoning, inverted, that led
    `captaingate` to REUSE `captain_transition` for gate refusals (there the
    existing kind was semantically correct; here none was).
  - **(f) The structural invariant.** `TestAdminRoutes_AllAuditOnAllow`
    (`internal/api/admin_audit_guard_test.go`) is the sibling of D-0012's
    `TestAdminRoutes_AllRequireAdminGate`, in the same AST-guard style as
    `captaingate.TestPhaseG_SingleSharedCaptainGateImplementation`. It parses
    `admin.go` and fails the build if any handler registered under
    `/api/v1/admin/` does not call `recordAdminAudit` in its body. Together
    the two guards pin both halves of the admin-surface contract: every admin
    endpoint admin-gates AND leaves an audit trail. A future
    `POST /api/v1/admin/admins` added without an audit write fails the build
    naming the unaudited handler. The guard was break-tested: removing the
    audit writer from `createPolicy` makes it fail with `admin handler
    "createPolicy" is registered under /api/v1/admin/ but its body never calls
    recordAdminAudit`, and the companion regression tests simultaneously go red
    (`no policy.grant audit row written`; a grant returns 201 even with a
    failing audit injected — the live gap); restoring the writer returns all
    to green.
  - **Regression tests** (`internal/api/admin_audit_test.go`) reuse the
    Stream G fixture: each new action's row is asserted written with the
    correct action, principal, decision, and target details; each mutating
    action is asserted to fail CLOSED under an injected audit-write failure
    (the mutation does not commit); reads fail OPEN (the read still 200s); and
    a non-admin attempt records an `admin.access_denied` row naming the
    attempting principal and the attempted endpoint.
- Basis: D-0012's "Honest scope note" (this entry closes the gap it tracked);
  `internal/llmsettings/service.go` (the `{target, before, after}` shape and
  the InsertTx atomic-write pattern reused here); `internal/captaingate/
  captaingate.go::writeRefusalAudit` (the fail-closed deny-audit pattern
  matched); migrations 017/018 (the kind-CHECK-widening rebuild pattern
  followed by 020); `internal/audit/audit.go` §4 `FailurePosture`/`isFailOpen`
  (the read/mutate split extended, not altered). Verified by `go test ./...`
  green, plus the break-test described in (f). NOTE: Stream G's audit-table
  extension (the `llm_settings_mutation` kind, migration 017) has no standalone
  DECISIONS entry — it is documented in the migration and `audit.go` code
  comments; D-0013 is the first DECISIONS entry to record the shared-table /
  typed-details arrangement explicitly.
- Supersedes: nothing — it closes the gap D-0012 tracked as deferred. D-0012
  closed the GATE gap (privilege escalation); D-0013 closes the AUDIT gap.
  Together they make the admin RBAC surface match what the README claims about
  RBAC and audit. D-0011's admin capability and `IsAdmin` semantics are
  untouched. Touched files: `internal/audit/audit.go` (new kind, action verbs,
  `Details` type, `isFailOpen` extension), `internal/api/admin.go` (the
  `recordAdminAudit` writer + eight handler call sites),
  `internal/api/admingate.go` (`recordAdminDenial` + three gate call sites),
  `internal/store/migrations/020_admin_audit.{up,down}.sql`, three new/updated
  test files (`admin_audit_guard_test.go`, `admin_audit_test.go`,
  `migrations_020_test.go`), and the step-count bumps in the 017/018/019
  migration round-trip tests (each now steps down one further past the new
  head — a pre-existing brittleness in those tests, not a behaviour change).
- Status: active. Admin-audit gap closed; structural invariant in place and
  break-tested; both admin-surface invariants (gate + audit) now enforced
  together. Known follow-up: the README's "All admin endpoints require Bearer
  auth" (RBAC section) is stale post-D-0012 (they require admin capability) and
  silent about D-0013's audit trail — flagged for the later combined README
  rewrite pass, not edited here.

---

## D-0012 — We found a privilege escalation in our own RBAC admin API: the admin gate was never applied to it

- Date: 2026-06-03
- Decision: This is not a polish entry. The post-Stream-K admin-surface
  read (`ADMIN_SURFACE_AUDIT.md`, Launch Blocker 1) found that the RBAC
  admin HTTP API in `internal/api/admin.go` — `POST /api/v1/admin/zones`,
  `POST /api/v1/admin/policies`, `POST /api/v1/admin/source-zones`,
  `DELETE /api/v1/admin/policies/{id}`, and the four `GET` read endpoints —
  was never admin-gated. Every handler required only bearer auth. **Any
  authenticated principal, including a brand-new zero-zone OIDC user who
  resolves to the read-only `unassigned` zone, could `POST` itself a policy
  into any zone — or mint a new zone with `allowed_actions`
  `["read","query","mutate","delete"]` and grant itself that — fully
  escalating its own access.** Because the same path can grant
  `regime-control`, it also reopened `declare_incident`/`resolve_incident`
  to anyone (Blocker 2). The fix applies the EXISTING `requireAdmin` gate
  (`internal/api/admingate.go`) to every admin handler — the same one-line
  `if _, gated := h.server.requireAdmin(w, r); gated { return }` inlining
  Stream G uses on the LLM settings/usage endpoints. No new gate, no
  changed gate semantics, no audit-writer change. Specifics:
  - **(a) The gap.** `registerAdminRoutes` wired eight handlers under
    `/api/v1/admin/`; none called `requireAdmin`. The UI's `RequireAdmin`
    React component (`ui/src/auth/RequireAdmin.tsx`) gated the admin *pages*
    client-side off the `/me` `is_admin` flag, which hides the nav but does
    nothing for a direct `curl`. The server-side check that should have sat
    behind it did not exist. The reads were gated too (not just the writes):
    `GET /admin/policies` and `GET /admin/zones` expose the full
    authorization map — who holds which zone and what each zone permits — so
    leaving them open leaks the access-control topology to any caller.
  - **(b) Why it existed — an honest account.** `requireAdmin` was
    introduced by Stream G (phase G5) specifically to gate the LLM
    instrumentation endpoints (settings writes, the per-principal usage
    breakdown). It was applied there and only there. The RBAC admin
    endpoints predate that gate (Phase 9.3 / migration 006) and shipped when
    the *only* deployed posture was "bearer key == trusted operator." When
    Phase C/H turned principals into a spectrum — OIDC users, `svc:` keys,
    zero-zone newcomers — the admin API's "bearer auth is enough" assumption
    silently became false, and the new gate was never retroactively swept
    back across the older surface. The gate's own doc comment even scoped
    itself to "the Stream G phase G5 LLM-instrumentation HTTP endpoints" —
    the narrow framing is exactly how the older surface got missed. No test
    asserted the property for `admin.go`, so nothing failed when the gate
    skipped it. This is the classic shape of a self-inflicted gap: a control
    added for one stream, correct in that stream, never generalized to the
    sibling surface that needed it just as much.
  - **(c) The structural invariant added to prevent recurrence.**
    `TestAdminRoutes_AllRequireAdminGate`
    (`internal/api/admin_gate_guard_test.go`) is an AST guard in the style
    of the identity refactor's single-implementation guards
    (`captaingate.TestPhaseG_SingleSharedCaptainGateImplementation`,
    `sessiongate`'s import guard). It parses `admin.go`, extracts the set of
    `adminHandler` methods registered by `registerAdminRoutes` (the handler
    arg of each `mux.HandleFunc`) and the set whose body calls
    `requireAdmin`, and fails the build if the former is not a subset of the
    latter. The regression tests pin the endpoints that exist today; this
    invariant pins the property for endpoints that do not exist yet — a
    future `POST /api/v1/admin/admins` added without the gate fails the
    build naming the ungated handler, rather than silently re-opening the
    escalation. The guard was break-tested: removing the gate from
    `createPolicy` makes it fail with `admin handler "createPolicy" is
    registered under /api/v1/admin/ but its body never calls requireAdmin`,
    and the companion regression test simultaneously shows the live
    escalation (a non-admin `POST` returns 201 with a `regime-control`
    policy granted to itself); restoring the gate returns both to green.
  - **Regression tests** (`internal/api/admin_gate_test.go`) reuse the
    Stream G fixture (`llmadminFixture`): a non-admin `POST` to
    `/admin/policies` and `/admin/zones` each returns 403 with the resource
    not created; an admin gets 201 with the resource created; a non-admin
    cannot self-grant `regime-control` (Blocker 2 closed); and the
    auth-disabled (`rbacEnabled=false`) local/dev posture still permits, so
    the fix does not block keyless deployments.
- Honest scope note — audit of admin mutations is a SEPARATE, still-open
  gap. The audit flagged (and the fix confirmed) that the RBAC admin
  endpoints write **no** audit rows at all: `admin.go` imports no audit
  package, `requireAdmin` does not audit denials, and there is no
  `audit.Kind`/`audit.Action` for zone/policy/source-zone mutations. Phase
  F's machinery covers infra-access decisions, regime/captain transitions,
  LLM-settings mutations, and auth-login — not this surface, and not gate
  denials in general (LLM-settings denials are not audited either). This fix
  is deliberately scoped to the privilege escalation and does NOT add an
  audit writer (that would change Phase F's mechanics). The
  zone/policy-mutation audit gap is recorded here as known and deferred; it
  is not closed by D-0012.
- Basis: `ADMIN_SURFACE_AUDIT.md` (Investigation 1 §2/§5, Prioritized Gaps
  Blockers 1–2), read against current code; `internal/api/admingate.go`
  (the existing gate, unchanged); the Stream G application pattern in
  `internal/api/llmsettings.go` / `internal/api/llmusageapi.go`. Verified by
  `go test ./internal/api/` green, plus the break-test described in (c).
- Supersedes: nothing — it closes a defect, it does not revise a prior
  decision. D-0011's admin capability and its `IsAdmin` semantics are
  untouched and unchanged; this entry only applies the gate that consults
  them to a surface that was missing it. Touched files:
  `internal/api/admin.go` (gate applied to all eight handlers; `adminHandler`
  gains a `*Server` back-reference), `internal/api/admin_internal_test.go`
  (repo-error fixture now constructs a permissive server), and two new test
  files. The audit table, its writers, and `requireAdmin`'s implementation
  are unchanged.
- Status: active. Privilege escalation closed; structural invariant in
  place. The admin-mutation audit gap remains open and is tracked above.

---

## D-0011 — Identity Phase H: admin as a dynamic capability evaluated at decision time; snapshot grants removed

- Date: 2026-05-30
- Decision: Phase H (joe-identity-design.md §2.9, §5 Invariant 2;
  joe-identity-phase-plan.md Phase H) closes the day-100 correctness gap
  D-0006 left open: admin authority is no longer the snapshot of grants
  captured at bootstrap (which silently failed to cover any zone created
  AFTER the configured admin's first login). It is now a DYNAMIC capability
  evaluated by the policy engine at decision time. The result: a zone
  created months after admin designation is covered automatically, with no
  re-snapshot, no operator action, and no silent gap. Specifics:
  - **Admin status is a principal-scoped row in a new table.** Migration
    `016_admin_principals` (one CREATE TABLE + one index) introduces
    `admin_principals(principal TEXT PK, granted_at TEXT NOT NULL,
    granted_by TEXT NOT NULL DEFAULT '', reason TEXT NOT NULL DEFAULT '')`.
    The schema matches the conventions of the existing tables (TEXT
    RFC3339 timestamps; TEXT NOT NULL DEFAULT '' for free-text columns; no
    sentinel rows). It is the SINGLE source of truth for admin status — the
    decision function reads it, the bootstrap and CLI write it, and nothing
    else stores admin information. A principal is admin iff a row exists
    for them in this table; there is no boolean column on `rbac_policies`,
    no flag elsewhere, no derivation.
  - **The decision function treats admin as an allow short-circuit, NOT a
    bypass of the zone classification.** `rbac.PolicyEngine.Decide`
    (`internal/rbac/policy.go`) gains exactly one new branch: after the
    zone-allows-action gate, before the per-principal-grant loop, the
    engine calls `IsAdmin(ctx, principal)` for each member of the
    PrincipalSet and returns `Decision{Allowed: true, Reason:
    ReasonAdminCapability}` on the first hit. The check is bounded by the
    zone's `allowed_actions` list — Phase H deliberately keeps that check
    UPSTREAM of the admin short-circuit (req 2). Same rule on the
    sourceless path: `HasZoneAccess` mirrors the structure with a boolean
    return.
  - **Why admin bypasses only the grant requirement and not the zone's
    allowed_actions (the stricter sensible interpretation, req 2).** The
    zone's `allowed_actions` is a property of the zone's classification —
    "this is a read-only zone", "this is a delete-permitted zone" — not a
    per-principal limit. An admin who could delete in `prod-readonly`
    would change what the zone is for; the zone classification would
    cease to communicate "no destructive actions". The principal-grant
    requirement, by contrast, IS per-principal: it gates who reaches the
    zone at all. Admin overriding the per-principal gate matches the
    operator's mental model ("admin can do anything anywhere"); admin
    overriding the zone classification would require introducing a second
    notion of "admin in zone X" that breaks the zone's primary purpose.
    The interpretation also matches what the Phase C snapshot used to do
    in aggregate: it wrote grant rows on every zone, but the zone's
    `allowed_actions` still bound what admin could do on each. Phase H
    preserves that ceiling.
  - **Reason vocabulary extended with one tag, audit row carries it.** A
    new constant `rbac.ReasonAdminCapability` ("admin_capability") joins
    the Phase F reason vocabulary (`policy_allow`, `no_grant`,
    `action_not_in_zone`, `zone_not_found`). The accessor's `permit`
    chokepoint records `Decision.Reason` into the audit row's `reason`
    column unchanged — no new migration, no new column, no new
    `audit.Kind`. An operator querying
    `audit_log WHERE reason = 'admin_capability'` sees only the decisions
    admin would not have reached through a per-zone grant; queries for
    `policy_allow` continue to surface ordinary zone-grant allows.
    `TestPhaseH_AdminAllowAuditReasonDistinguishedFromZoneGrant`
    (internal/access/audit_test.go) issues both an admin allow and a
    zone-grant allow against the same source and asserts the two audit
    rows differ in their `reason` field — the audit-trail
    distinguishability requirement (Phase H req 5).
  - **Bootstrap path: snapshot logic removed; same trigger, new behaviour.**
    `auth.Provisioner.GrantAdmin` (`internal/auth/provision.go`) no longer
    iterates `ListZones` and writes `CreatePolicy` per zone. It calls
    `repo.AddAdmin(ctx, rbac.Admin{Principal, GrantedBy:
    "bootstrap_admin_email", Reason: "auth.admin_email match"})` and then
    `repo.DeletePoliciesForPrincipal(ctx, principal)`. The OIDC callback
    (`internal/auth/handlers.go:179-185`) still calls `GrantAdmin` for
    every login matching `auth.admin_email`; the call is still idempotent;
    the failure-loud policy is preserved (admin bootstrap failure aborts
    the login with HTTP 500). What changed: the row goes to
    `admin_principals` instead of N rows going to `rbac_policies`, and any
    pre-existing `rbac_policies` rows for the admin are cleaned up so
    "single source of truth" holds structurally. The bootstrap trigger
    (config-designated admin_email becomes admin on first login) is
    preserved verbatim — the design's §2.9 contract holds.
  - **Migration of the existing admin: no SQL data migration, runtime
    cleanup at first matching login.** The pre-Phase-H snapshot lived in
    `rbac_policies`. Migration 016 creates `admin_principals` empty; it
    does not back-port any rows because the migration cannot know which
    principal is the configured admin (`auth.admin_email` lives in YAML
    config, not in the database). The cleanup runs at runtime instead: the
    first matching admin_email login under Phase H code inserts the
    `admin_principals` row AND deletes any leftover `rbac_policies` rows
    for that principal in the same call. The prompt allows a clean
    migration; this is the cleanest one given the configured-in-YAML
    constraint. The `TestPhaseH_NoLeftoverSnapshotGrants` test seeds
    snapshot grants explicitly and asserts they are gone after
    `GrantAdmin`, proving the cleanup path. The unreleased-project
    assumption holds either way: no production DB has snapshot grants to
    migrate; the cleanup is a structural defence, not a back-port.
  - **CLI surface: `joe admin {grant,revoke,list}`, parallel to
    `joe zone`.** New subcommand at `cmd/joe/admin.go`. `joe admin grant
    --principal <user:|svc:> [--reason ...]` upserts the row (idempotent;
    re-issue with a new `--reason` updates the rationale) AND deletes any
    `rbac_policies` rows for the principal (the same cleanup the
    bootstrap path performs). `joe admin revoke --principal ...` deletes
    the row. `joe admin list` prints the rows in a 3-column table
    (principal, granted_by, granted_at). The command opens the SQLite
    database directly (operator-on-host), mirroring `joe zone`'s Phase C
    surface (D-0006). The `runDeps.openRBACRepo` factory's return type
    widened from `zoneRepo` to a new `rbacRepo` interface that adds
    `IsAdmin`/`ListAdmins`/`AddAdmin`/`RemoveAdmin`; `*rbac.SQLRepository`
    satisfies both. The configured `auth.admin_email` path keeps working
    regardless of the CLI — both routes converge on the same `AddAdmin`
    repository call. Justification for including grant/revoke (the
    prompt left this as a scope choice): consistent with `joe zone`'s
    operator-on-host model and the Phase C precedent; lets an operator
    delegate additional admins without editing YAML or restarting
    joe-core. Without it, the only admin path is the configured email,
    which fails the "day-100 operator experience" lens that motivated
    Phase H in the first place — adding a second admin must not require
    a config change + restart cycle.
  - **Revoke caveat: `auth.admin_email` re-grants on next matching login.**
    `joe admin revoke` removes the `admin_principals` row, but the
    bootstrap path is idempotent and will re-insert it the next time a
    matching email logs in. To make a revocation stick, the operator must
    also clear `auth.admin_email` from config. This is the right default:
    the configured admin_email IS the bootstrap path, and silently
    pre-empting it via a CLI revoke would create a subtle drift between
    config and behaviour. Documented in the CLI's command surface and in
    the `runAdminRevoke` doc comment.
  - **Non-admin authorization outcomes: unchanged from post-Phase-G.** The
    Phase H short-circuit only fires when at least one principal in the
    set holds an `admin_principals` row. For every other principal it is
    a no-op — `Decide`/`HasZoneAccess` flow through the existing
    per-principal-grant loop, returning the same allow/deny outcomes the
    Phase G regression tests proved. `TestPhaseH_NonAdminOutcomesUnchanged`
    asserts this regression — granted allow, ungranted deny, in-zone
    action denied — each with the same reason vocabulary
    (`policy_allow`/`no_grant`/`action_not_in_zone`) it had pre-Phase-H.
  - **Failure posture on `IsAdmin` errors: warn + fall through, not deny.**
    An `IsAdmin` repository error for one principal logs WARN and
    continues to the next principal in the set, then falls through to the
    per-zone grant loop. The principal's grant lookup still runs; the
    overall decision is whatever the grant path produces. Rationale:
    `IsAdmin` is a one-table single-row read; a transient failure should
    not collapse to deny on a principal who legitimately holds an
    rbac_policies grant. The behaviour mirrors how `ListPoliciesForPrincipal`
    handles its own failures (`continue`, not return). Verified by
    `TestPhaseH_AdminIsAdminErrorFallsBackToGrant`.
- Deviations from the Phase H prompt, with reasons:
  1. **No SQL data migration; cleanup runs at the first matching admin
     login (and at every CLI promotion).** The configured admin_email is
     in YAML config, not in the database, so an SQL migration cannot
     identify the admin without operator input. The cleanup runs in
     Go at the existing `GrantAdmin` site instead, which is invoked on
     every matching login (idempotent). The behaviour the prompt wants
     ("remove leftover snapshot grants") is realised on first contact
     with the new code; the `TestPhaseH_NoLeftoverSnapshotGrants` test
     seeds pre-existing snapshot rows to prove the cleanup.
  2. **CLI grant/revoke is in scope.** The prompt left this as a scope
     decision; rejected "inspect only" as inconsistent with the
     `joe zone` precedent (Phase C's surface includes grant/revoke). The
     configured admin_email path is preserved verbatim; the CLI is the
     operator-on-host route for any ADDITIONAL admins. Without it, the
     deployment can have at most one admin without a config-restart
     cycle, which fails the "day-100 operator experience" lens that
     motivated Phase H.
  3. **`runDeps.openRBACRepo` return type widened from `zoneRepo` to
     `rbacRepo`.** `rbacRepo` is the union of methods both `joe zone`
     and `joe admin` need; `*rbac.SQLRepository` satisfies it
     trivially. Keeping two parallel factories (one per command) would
     have meant duplicating the DB-open ceremony.
  4. **No new `audit.Kind` and no new column in `audit_log`.** The Phase F
     contract is "one reason field captures the basis"; adding a column
     for admin specifically would balloon the schema for one capability.
     The `ReasonAdminCapability` tag is in the existing `reason` column;
     audit queries discriminate on that, not on `kind`. Migration 015's
     CHECK constraint on `kind` is unchanged.
  5. **`PrincipalSet` stays size 1.** Per the explicit scope fence; no
     group-member additions, no multi-tier RBAC. Admin is a single
     boolean capability — the only role beyond per-zone grants.
- Basis: joe-identity-design.md §2.9 (principal mapping & bootstrap),
  §5 Invariant 2 (every path to infra passes through the guarded accessor
  — admin is part of that decision now), §6 (admin UI deferred behind a
  CLI seam that exists);  joe-identity-phase-plan.md Phase H. Verified
  against Phase A's accessor signature (D-0004), Phase B's set-shaped
  path (D-0005), Phase C's admin bootstrap (D-0006, superseded by this
  entry's snapshot replacement), Phase D's service-account wiring
  (D-0007), Phase E's accessor-on-both-paths (D-0008), Phase F's audit
  chokepoint (D-0009), and Phase G's captain-gate-on-loop + set-shaped
  HasZoneAccess (D-0010). New tests:
  - `internal/rbac/policy_test.go`:
    `TestPhaseH_AdminAllowedOnZoneCreatedAfterDesignation` (the bug-fix
    demonstration — FAILS pre-Phase-H, PASSES now),
    `TestPhaseH_AdminAllowedAcrossMultipleZonesWithoutGrants` (breadth +
    allowed_actions ceiling),
    `TestPhaseH_NonAdminOutcomesUnchanged` (regression: non-admin
    unchanged from post-Phase-G),
    `TestPhaseH_AdminDecisionReasonIsDistinct` (audit-trail
    distinguishability at the Decision struct level),
    `TestPhaseH_HasZoneAccessAdminCapability` (sourceless path coverage),
    `TestPhaseH_AdminIsAdminErrorFallsBackToGrant` (failure posture).
  - `internal/access/audit_test.go`:
    `TestPhaseH_AdminAllowAuditReasonDistinguishedFromZoneGrant`
    (audit-trail distinguishability through the accessor +
    audit.Repository),
    `TestPhaseH_AdminAllowedOnPostBootstrapZone` (bug fix verified on
    the audit path too).
  - `internal/auth/provision_test.go`:
    `TestPhaseH_GrantAdminMarksDynamicCapability` (bootstrap writes
    admin_principals, not rbac_policies),
    `TestPhaseH_NoLeftoverSnapshotGrants` (the no-snapshot
    structural assertion: seeded snapshot rows are cleaned up),
    `TestPhaseH_GrantAdminIsIdempotent` (safe re-run on every login).
  - `internal/auth/handlers_test.go::TestCallback_AdminBootstrap`:
    rewritten for Phase H semantics (admin_principals row exists; no
    rbac_policies rows; non-admin login still gains nothing).
  - `cmd/joe/admin_test.go`:
    `TestPhaseH_AdminGrantListRevoke` (end-to-end CLI),
    `TestPhaseH_AdminGrantCleansUpZoneSnapshots` (CLI enforces single
    source of truth),
    `TestPhaseH_AdminListEmpty` and
    `TestPhaseH_AdminGrantUnprefixedPrincipalRejected` (CLI error
    handling).
  All prior-phase tests (Phase A no-ungoverned-access invariant, Phase
  A/B/C/D/E/F/G regressions including the executable authority-
  invariance, captain-on-loop, and append-only audit guards) still
  green and unchanged.
- Supersedes: D-0006's snapshot definition of admin authority. The
  rest of D-0006 (OIDC + sessions + CLI zone provisioning + the
  config-designated admin_email TRIGGER) stands. The touched packages
  are `internal/store/migrations` (new 016), `internal/rbac` (new Admin
  type, repository methods, policy engine short-circuit, reason
  constants), `internal/auth` (Provisioner.GrantAdmin replaced),
  `cmd/joe` (new admin subcommand; zoneRepo → rbacRepo); OIDC,
  service-account resolution, the accessor's transport wiring, the
  captain gate, the audit_log schema, and PrincipalSet are untouched.
  With this phase the planned identity work — Phases A–H — is complete.
- Status: active. Phase H complete. This closes the tracked identity
  follow-ups: no further phase in this sequence is planned.

---

## D-0010 — Identity Phase G: shared §C captain gate on the agentloop path; HasZoneAccess set-shaped; coreagent refresh confirmed read-only

- Date: 2026-05-30
- Decision: Phase G (joe-identity-design.md §0 bug #2, §2.10, §5
  Invariant 6; joe-identity-phase-plan.md Phase G) fixes the wiring
  hole the design called out as "the incident-mode design is unenforced
  on the path that matters." Three things changed; the captain
  *concept* did not (it remains a session-ownership concurrency control,
  DENY-ONLY, never widening RBAC authority). Specifics:
  - **One shared §C gate, extracted into a new package
    `internal/captaingate`.** Pre-Phase-G the gate logic + §B1 principal
    substitution lived inside `coreagent.DurableExecutor`, which wraps
    only the Core Agent's onboarding/refresh executor — NOT the user
    task loop (`agentloop.Agent.Run` behind
    `/api/v1/tasks` and `/api/v1/tasks/stream`). Phase G moves the
    gate into `captaingate.Wrapper` (a tool-executor wrapper that
    implements both single `Execute` and batch `ExecuteBatch` +
    `ResultsToMessages` so it is a drop-in for `*tools.Executor` in
    `agentloop.NewAgent`), and composes it around BOTH paths:
    1. **Core Agent path** (`cmd/joe-core/main.go:520-531`):
       `captaingate.New(coreagent.NewDurableExecutor(inner, runRepo),
       sessRepo, auditRepo)`. The gate now runs UPSTREAM of §D5
       persistence, so a refused mutation is never persisted as an
       issued intent — nothing happened to record. `DurableExecutor`
       lost its `sessRepo` parameter and is now pure §D5 idempotency.
    2. **User task loop path** (`internal/api/tasks.go:240-258`):
       `var loopExec agentloop.BatchExecutor = executor; if
       services.SessionModel != nil { loopExec = captaingate.New(...) }`.
       This wraps the SAME `*tools.Executor` the loop has always used.
       `agentloop.Agent.executor` is now an interface
       (`agentloop.BatchExecutor`) so both `*tools.Executor` and
       `*captaingate.Wrapper` satisfy it without further plumbing.
    The static guard
    `internal/captaingate/single_impl_guard_test.go::TestPhaseG_SingleSharedCaptainGateImplementation`
    parses the whole repo and FAILS if any production package other
    than `internal/captaingate` calls `sessiongate.Check`. This is the
    structural enforcement of the "do not duplicate the logic in two
    places that can drift" requirement: there is exactly one production
    `sessiongate.Check` call site, and both agentic paths reach it
    through the same `captaingate.Wrapper.Execute`.
  - **Gate-then-accessor ordering on the loop path (req 2).** Inside
    `Wrapper.Execute`: classify tier → T1 bypass → on T2/T3 call
    `sessiongate.Check(ctx, sessRepo, sessionID, principal, tier)`. On
    refusal the wrapper returns `*captaingate.GateRefusalError`
    immediately — no inner `Execute`, no accessor call, no infra. On
    allow the wrapper performs the §B1 substitution (in incident regime
    only) and then calls `inner.Execute`, which is the path that
    eventually reaches the accessor's RBAC check via the in-process
    client. The Phase G behavioural test
    `TestPhaseG_LoopPathNonCaptainMutationRefused` proves this on the
    LOOP path specifically — pre-Phase-G the user task loop used a
    naked `*tools.Executor` with no gate, so this test would have
    SUCCEEDED at the mutation (the bug); it now correctly refuses with
    a `*GateRefusalError`. The captain-session mutation still succeeds
    (`TestCaptainGate_EndToEnd`), and non-captain READS still succeed
    (`TestPhaseG_LoopPathNonCaptainReadsStillSucceed`) — the gate only
    constrains mutation, never read/investigation.
  - **Gate stays DENY-ONLY; authority-invariance is now a passing test
    (req 3 + acceptance criterion).** The Phase G test
    `TestPhaseG_GateIsDenyOnly_RBACAuthorityInvariance` seeds a
    principal/source/policy combo, computes the `IsAllowed` outcome
    under normal regime, declares an incident, and asserts the SAME
    outcome under incident regime for the SAME principal/action/zone.
    Identical in both directions: a granted principal stays granted; an
    ungranted principal stays denied. This is the executable form of
    the §2.10 invariant ("incident mode never increases any principal's
    authority"). If a future change ever leaks regime state into
    `IsAllowed`/`Decide`, this test catches it.
  - **Loop-path gate refusal lands in the audit trail (req 4).** The
    wrapper writes ONE row per refusal — kind=`captain_transition`,
    action=`captain_gate_refused`, decision=`deny`, principal=caller
    ctx principal, context={tool, session_id, captain_session_id} — via
    the SAME `audit.Repository` Phase F wired into the accessor and
    the regime/captain handlers. The audit kind reuses the existing
    `KindCaptainTransition` (a gate refusal IS a captain-mechanism
    event), so migration 015's CHECK constraint on `kind` is unchanged;
    no new migration is needed. Failure posture follows the Phase F
    helper `audit.FailurePosture`: the refusal action is not in the
    read-class enum, so an audit-insert failure fail-CLOSES (returns
    the audit error rather than the refusal). The mutation does not
    proceed either way because the gate already refused it; the only
    observable difference is which error the LLM-facing layer surfaces.
    Verified by `TestPhaseG_GateRefusalRecordedInAuditTrail` which
    asserts: row exists, kind/decision/principal are correct, and the
    context blob names the captain session id.
  - **`HasZoneAccess` set-shaped (req 5).** The sourceless authorization
    function now takes `rbac.PrincipalSet` instead of a single
    `rbac.Principal`, mirroring `IsAllowed`/`Decide` (D-0005). Same
    union-of-grants semantics: allowed if ANY member holds a matching
    grant; denied if none do; per-member lookup failures degrade to
    deny-that-member only; the zone's allowed-actions cap is unchanged
    (no widening via union). Production callers — both `declare` and
    `resolve` in `internal/api/regime.go` — build the set as
    `rbac.NewPrincipalSet(principal)` from the caller's context
    principal, size 1, consistent with the rest of the system. Phase B
    deliberately left this single-principal as "out-of-chain" (D-0005
    deviation 3); Phase G is where the regime/captain path joins. Test
    coverage:
    `internal/rbac/policy_test.go::TestPolicyEngine_HasZoneAccess_SetSingleMember`
    (the production size-1 outcome — granted allow / ungranted deny;
    identical to the pre-Phase-G single-principal call) and
    `TestPolicyEngine_HasZoneAccess_SetUnion` (the forward-looking
    multi-member contract: any-granted allow, none-granted deny, empty
    set deny, no zone-action widening). The existing Phase A/B
    regression `TestRegime_6B_NoIncidentalSourceWidening` was updated
    to call `HasZoneAccess(ctx, NewPrincipalSet(principal), ...)`; its
    behavioural assertions are unchanged and still pass, so the
    single-principal outcome is byte-identical.
  - **coreagent refresh: VERDICT-A — READ-ONLY on infrastructure,
    allowlist retained (req 6).** The Phase G investigation enumerated
    every adapter call on the
    `internal/coreagent/{alerting,aws,azure,crd,datastore,git,gitops,k8s,networking,observability,registry}_refresh.go`
    paths and confirmed each one is List/Get/Describe/Status only — no
    Create/Update/Delete/Apply/Post/Put/Patch on any adapter. The
    onboarding side (the Core Agent's own agentic loop in
    `executor_durable.go`, now gated by `captaingate`) mutates only
    INTERNAL state via `graph_add_node` / `graph_add_edge` /
    `save_onboarding_fact` — these never touch customer infrastructure.
    Conclusion: no path on the coreagent side issues an infrastructure
    mutation, so the no-ungoverned-access allowlist exception for
    `internal/coreagent/` stays. The exception's rationale is now
    documented in `internal/api/access_guard_test.go` (the same place
    the invariant is enforced) with a Phase-G paragraph that states the
    audit was performed, what was checked, and what future change would
    make this allowlist line a violation again.
- Deviations from the Phase G prompt, with reasons:
  1. **Gate refusal audit re-uses `KindCaptainTransition` instead of
     adding a new `kind` value.** Migration 015's CHECK constraint
     allows only `infra_access`, `regime_transition`,
     `captain_transition`. The gate IS a captain-mechanism event, so
     re-using the existing kind is the natural home and avoids a
     migration. The action verb `captain_gate_refused` discriminates
     the row from attach/transfer rows. Operators querying for
     captain-mechanism events get gate refusals alongside attaches and
     transfers without a schema change.
  2. **Loop-path test does NOT spin the full LLM loop.** The acceptance
     criterion is "demonstrated on the agentloop path specifically";
     the wrapper is the same object both paths get, and
     `agentloop.Agent.Run` calls `executor.ExecuteBatch` each
     iteration. Driving `Wrapper.ExecuteBatch` directly with crafted
     `[]tools.ToolCallRequest` exercises EXACTLY the code path
     `Agent.Run` would, without the cost of a fake LLM. The test
     comment calls out explicitly that pre-Phase-G this would have
     succeeded (the bug); the fact that it now refuses is the signal.
  3. **`agentloop.Agent.executor` field changed from
     `*tools.Executor` to interface `agentloop.BatchExecutor`.** The
     prompt allows breaking internal interfaces freely; this is the
     minimum surface change to make the captaingate wrapper a drop-in.
     `*tools.Executor` and `*captaingate.Wrapper` both satisfy
     `BatchExecutor`, so tests that don't care about the gate (e.g.
     `test/e2e/agent_flow_test.go`) keep passing `*tools.Executor`
     directly without modification.
  4. **Gate refusal audit fails CLOSED with the audit error wrapped
     rather than the refusal.** The mutation is denied either way
     (gate refused, inner not invoked), so the visible difference is
     only which error the LLM-facing layer surfaces. The choice keeps
     consistency with Phase F's failure-posture helper without
     special-casing.
  5. **Test ergonomics: gate behavioural tests moved from
     `coreagent/executor_gate_test.go` to
     `captaingate/captaingate_test.go`.** Same scenarios, same
     `principalSpyExecutor`, same end-to-end + ordering + B1 +
     T1-bypass assertions; what changed is the wrapper under test
     (`captaingate.Wrapper` instead of `coreagent.DurableExecutor`)
     because the gate moved.
- Basis: joe-identity-design.md §0 (bug #2 statement), §2.10 (captain
  is a session-ownership concurrency control, DENY-only, never widens
  RBAC), §2.7 (set-shaped authorization), §5 Invariants 4 and 6 (the
  authority-invariance and captain-on-loop invariants);
  joe-identity-phase-plan.md Phase G. Verified against Phase A's
  accessor signature (D-0004), Phase B's set-shaped path (D-0005),
  Phase C's edge-auth (D-0006), Phase D's service-account wiring
  (D-0007), Phase E's accessor-on-both-paths (D-0008), and Phase F's
  audit chokepoint (D-0009). New tests:
  - `internal/captaingate/captaingate_test.go`:
    `TestCaptainGate_EndToEnd`, `TestCaptainGate_RefusalNeverCallsInner`,
    `TestCaptainGate_B1_PrincipalSubstitution`,
    `TestCaptainGate_AllowsT1ReadsInIncident` (migrated equivalents of
    the Change-10 executor_gate_test.go cases);
    `TestPhaseG_LoopPathNonCaptainMutationRefused` (the bug-#2 fix
    demonstration on the LOOP path),
    `TestPhaseG_LoopPathNonCaptainReadsStillSucceed`,
    `TestPhaseG_GateRefusalRecordedInAuditTrail`,
    `TestPhaseG_GateIsDenyOnly_RBACAuthorityInvariance`.
  - `internal/captaingate/single_impl_guard_test.go::TestPhaseG_SingleSharedCaptainGateImplementation`
    — repo-wide AST guard, fails if `sessiongate.Check` is called from
    any production package other than `internal/captaingate`.
  - `internal/rbac/policy_test.go::TestPolicyEngine_HasZoneAccess_SetSingleMember`
    and `TestPolicyEngine_HasZoneAccess_SetUnion` — the set-shaped
    contract.
  All prior-phase tests (Phase A no-ungoverned-access invariant + loop
  coverage, Phase A/B/C/D/E/F regressions, sessiongate import-closure
  guard, executor §D5 idempotency tests now without sessRepo) still
  green and unchanged.
- Supersedes: nothing — extends D-0009. Phase H (admin-zones-snapshot)
  remains the sole tracked follow-up. The touched packages are
  `internal/captaingate` (new), `internal/coreagent` (gate removed
  from `DurableExecutor`; `sessRepo` parameter dropped),
  `internal/agentloop` (executor field is now an interface),
  `internal/api` (`buildTaskRun` wraps the executor),
  `internal/rbac` (`HasZoneAccess` set-shaped),
  `internal/audit` (new action + reason constants;
  `KindCaptainTransition` doc widened), `cmd/joe-core/main.go`
  (composition); OIDC, service-account resolution, accessor RBAC
  logic, and migration 015 are untouched. With this phase the planned
  identity work is complete except for the tracked Phase H follow-up.
- Status: active. Phase G complete; do not proceed to Phase H without a
  new prompt.

---

## D-0009 — Identity Phase F: append-only audit at the decision point; regime/captain transitions redirected; bug #3 fixed

- Date: 2026-05-30
- Decision: Phase F (joe-identity-design.md §2.6, §4;
  joe-identity-phase-plan.md Phase F) introduces ONE append-only audit
  table backed by a new package `internal/audit`, written by the guarded
  accessor on every authorization decision (allow and deny alike) AND by
  the regime/captain transition handlers as their durable record. The
  per-decision write site is the accessor's `permit()` chokepoint
  (`internal/access/access.go`); the transition write sites are the
  regime and captain HTTP handlers (`internal/api/regime.go`,
  `internal/api/captain.go`). Bug #3 (joe-identity-design.md §0 bug #3)
  is fixed: incident history now lives in the audit log, independent of
  `system_regime.declared_by_principal` and `session_captains`, both of
  which still get cleared/cascaded on resolve. Specifics:
  - **One table, one migration (015_audit_log).** Schema:
    `audit_log(id INTEGER PRIMARY KEY AUTOINCREMENT, created_at TEXT,
    principal TEXT NULL, action TEXT, zone TEXT NULL, source TEXT NULL,
    decision TEXT CHECK IN ('allow','deny'), reason TEXT, kind TEXT
    CHECK IN ('infra_access','regime_transition','captain_transition'),
    context TEXT DEFAULT '{}')`. Column rationale captured in the
    migration's header comment. Three indices: `created_at`, `principal`,
    `kind`. Nullables only where the row kind legitimately produces no
    value (zone/source for sourceless transitions). Encoding follows
    existing schema conventions: TEXT RFC3339 timestamps (009, 010, 011,
    014), `INTEGER PRIMARY KEY AUTOINCREMENT` id (006, 007), CHECK
    constraints for enum-shaped TEXT (009).
  - **Dual append-only enforcement (Phase F req 2, design §2.6).**
    1. **Code:** `audit.Repository` is an interface declaring exactly
       one method, `Insert(ctx, Event) error`. There is NO Update,
       Delete, Truncate, Erase, or Remove on the API surface. The
       concrete `sqlRepository` is unexported and returned through the
       interface so no caller has a path to a mutator. Two AST guards
       (`internal/audit/audit_test.go::TestRepositoryAPISurface_AppendOnly`
       parses the package, finds the `Repository` interface, and fails
       if any method other than `Insert` is declared;
       `TestRepositoryAPISurface_NoMutatorPackageFunctions` fails if any
       top-level function name in the package starts with a mutator verb).
    2. **Database:** Migration 015 creates two triggers,
       `audit_log_no_update` and `audit_log_no_delete`, each
       `RAISE(ABORT, 'audit_log is append-only: <verb> is not permitted')`.
       Verified by `internal/audit/sql_test.go::TestMigration015_TriggerBlocksUpdate`
       and `TestMigration015_TriggerBlocksDelete`. Even an operator with
       raw SQL access cannot rewrite or erase history.
  - **Per-decision write at the accessor's chokepoint (req 3).** The
    accessor's `permit(ctx, principals, sourceID, action)` is the single
    point both HTTP and loop paths converge on (D-0004, D-0008). It now
    calls a new `rbac.PolicyEngine.Decide(...)` method that returns
    `Decision{Allowed, Zone, Reason}` — `IsAllowed` is now a thin
    boolean wrapper over `Decide`. `permit` then writes ONE audit row
    capturing principal, action, zone, source, decision (allow/deny),
    structured reason (`policy_allow` / `no_grant` / `action_not_in_zone`
    / `zone_not_found` / `rbac_disabled`), and `kind=infra_access`. The
    accessor's allow/deny OUTCOME is unchanged from D-0005/D-0008 (audit
    is observation, not policy) — verified by all prior-phase tests
    still passing.
  - **Failure posture split (req 4, design §4).** The fail-CLOSED on
    mutate / fail-OPEN on read decision lives in one helper,
    `audit.FailurePosture(ctx, action, err, where)`, called from every
    audit caller so the split cannot drift. The helper inspects the
    action string: `read` and `query` → fail-open (returns nil after a
    loud WARN log naming the where/action/error); everything else,
    including `mutate`, `delete`, and ALL transition verbs (declare /
    resolve / captain_attach / captain_transfer_*) → fail-closed (returns
    the original audit error after an ERROR log). The accessor's
    `permit` wraps the returned error in `"audit write failed for
    mutating action: %w"` so callers can distinguish it from
    `ErrPermissionDenied`. Behavioural tests:
    `internal/access/audit_test.go::TestPhaseF_FailClosedOnMutate`
    (mutate adapter is NOT called when audit insert errors;
    `GitHubPostComment` returns a non-permission error) and
    `TestPhaseF_FailOpenOnRead` (read adapter IS called when audit
    insert errors; `K8sListResources` returns nil error). Unit-level
    coverage of the helper across all read and mutate verbs in
    `internal/audit/audit_test.go::TestFailurePosture_FailOpenOnRead` /
    `FailClosedOnMutate`.
  - **Regime transitions redirected to durable rows (req 5, bug #3).**
    The regime handler (`internal/api/regime.go`) now writes one audit
    row of kind `regime_transition` BEFORE every `DeclareIncidentRegime`
    / `ResolveIncidentRegime` repository call. Denials of either
    capability ALSO write one deny row, so rejected transitions are
    durably recorded too. The write is fail-closed: if the audit insert
    fails the regime mutation does NOT proceed (the
    `system_regime`/`agent_sessions` rows are untouched). After resolve,
    `system_regime.declared_by_principal` is still nulled (the existing
    code in `sessionmodel/regime_transitions.go:210-214` is unchanged —
    the live-state row stays mutable, per the design's "may remain as
    live-state"), but the durable record of who declared the incident
    and when lives in the audit log and is independent of that row.
    Bug #3 regression test:
    `internal/api/audit_phasef_test.go::TestPhaseF_Bug3_IncidentHistorySurvivesResolve`
    declares as alice → resolves → asserts both that
    `system_regime.declared_by_principal IS NULL` (the test's premise
    that the bug behaviour is still present in the mutable row) AND that
    `audit_log` still holds one allow row for
    `action=declare_incident principal=alice`. This test would FAIL on
    pre-Phase-F code (no audit table existed); it PASSES now.
  - **Captain transitions redirected to durable rows (req 5).** The
    captain handler (`internal/api/captain.go`) writes one audit row of
    kind `captain_transition` BEFORE every captain mutation: attach,
    transfer begin, transfer confirm, transfer cancel. Same fail-closed
    posture. Bug #3 companion test:
    `internal/api/audit_phasef_test.go::TestPhaseF_CaptainTransitionsSurviveResolve`
    deletes the session after resolve (triggering the
    `session_captains ON DELETE CASCADE` from migration 009:62) and
    asserts the audit rows are still present.
    `TestPhaseF_CaptainAttachWritesAuditRow` covers the HTTP attach path
    end-to-end.
  - **R-CAP1 (declare+captain atomic) coverage.** R-CAP1 attaches the
    declaring principal as captain inside the same transaction as the
    `system_regime` flip (`regime_transitions.go:96-104`). Phase F does
    NOT write a separate `captain_attach` audit row for R-CAP1 — the
    `declare_incident` audit row already captures who took command and
    when, and adding a duplicate row would either require a second
    write inside the transaction (mixing the audit and regime layers
    needlessly) or a write outside the atomic boundary (where it could
    diverge from the regime state on rollback). The single
    `declare_incident` row with `principal=alice` IS the captain-
    attached-at-declare record. Subsequent attaches via the HTTP
    endpoint do produce dedicated `captain_attach` rows.
  - **Wiring.** `cmd/joe-core/main.go` constructs
    `audit.NewRepository(sqlStore.DB(), sqlStore.Driver())` after the
    migrations run and stores it on `services.Audit` (a new field on
    `core.Services`). `api.New` reads `services.Audit` and passes it to
    `access.New(...)`; `regimeHandler` and `captainHandler` read it
    from `s.services.Audit`. A nil `Audit` field is treated as
    "audit disabled" by every caller (a NoopRepository is also provided
    in `internal/audit/noop.go` for explicit test use). This nil-safety
    lets the existing accessor tests
    (`internal/access/access_test.go`) keep working without churn —
    they pass `nil` for the audit repo and verify the same allow/deny
    outcomes the rest of the suite proves.
  - **rbac.PolicyEngine surface changes.** `IsAllowed(ctx, principals,
    sourceID, action) bool` is preserved — it now delegates to a new
    `Decide(ctx, principals, sourceID, action) Decision` whose return
    struct also carries the resolved Zone and a machine-readable Reason
    used by the audit row. Existing IsAllowed callers (the policy
    tests, regime handler's HasZoneAccess sibling) are unaffected. The
    behavioural outcome — which principals on which sources/actions
    return true — is unchanged across every Phase A/B/C/D/E regression
    test still passing.
  - **No retention/rotation in v1.** Out of scope per the prompt's
    explicit scope fence. The table grows monotonically; an operator
    needing space management would `DROP TABLE audit_log` via the
    Phase F down migration (the only sanctioned way out of the
    append-only contract) and re-migrate. Adding a retention policy
    behind a separate insert-rotate-only repository is a clean v2
    extension — the existing `Repository` interface stays as-is.
- Deviations from the Phase F prompt, with reasons:
  1. **R-CAP1 captain-attach row not separately written.** See the
     R-CAP1 paragraph above — the `declare_incident` row covers the
     same "who and when" information, and a separate row would either
     leak into the atomic-declare transaction or risk diverging from it.
     The `TestPhaseF_CaptainTransitionsSurviveResolve` test exercises
     the durable record via the declare/resolve rows (which both
     reference alice as the declaring captain in spirit, even if the
     `audit_log.action` discriminates kinds).
  2. **`NoopRepository` provided alongside nil-safe accessors.** The
     prompt's "every authorization decision writes one audit row"
     requirement is held for the production path
     (`cmd/joe-core/main.go` always wires the SQL repo); tests that don't
     care about audit pass nil (skipping the write entirely) or the
     NoopRepository (accepting and discarding writes). The Phase F
     behavioural tests use a recording in-memory implementation
     (`internal/access/audit_test.go::recordingAudit`) and the SQL
     repository for integration coverage.
  3. **Reason vocabulary is structured tags, not free-form text.**
     `policy_allow` / `no_grant` / `action_not_in_zone` /
     `zone_not_found` / `rbac_disabled` for accessor rows, and
     `transition_recorded` / `no_grant` for transition rows. Tags are
     stable and machine-parseable; future operator queries against
     `audit_log.reason` get a small enumerable set, not English prose.
- Basis: joe-identity-design.md §0 (bug #3 statement), §2.6 (append-only
  audit at the decision point), §4 (failure posture split), §5
  Invariant 5 (append-only + transitions not erased on resolve);
  joe-identity-phase-plan.md Phase F. Verified against Phase A's
  accessor signature (D-0004), Phase B's set-shaped path (D-0005),
  Phase C's edge-auth (D-0006), Phase D's service-account wiring
  (D-0007), and Phase E's accessor-on-both-paths (D-0008). New tests:
  - `internal/audit/audit_test.go`: append-only API guard,
    no-mutator-package-function guard, FailurePosture fail-open/
    fail-closed split coverage, NoopRepository.
  - `internal/audit/sql_test.go`: Insert round-trip, NULL handling for
    sourceless rows, UPDATE-blocked trigger, DELETE-blocked trigger.
  - `internal/access/audit_test.go`: allow writes one allow row with
    correct fields; deny writes one deny row with denial reason;
    fail-closed on mutate (audit insert error blocks adapter call);
    fail-open on read (audit insert error proceeds to adapter call).
  - `internal/api/audit_phasef_test.go`:
    `TestPhaseF_Bug3_IncidentHistorySurvivesResolve` (the named
    regression — fails on pre-Phase-F code), `CaptainTransitionsSurviveResolve`,
    `CaptainAttachWritesAuditRow`, `DeclareDenialWritesAuditRow`.
  All prior-phase tests (Phase A no-ungoverned-access invariant, Phase
  A/B/C/D/E regressions, Phase E equivalence, Phase E loop coverage)
  still green and unchanged.
- Supersedes: nothing — extends D-0008. Phase G (captain wiring onto
  the agentloop path) remains pending. The accessor, the regime
  handler, the captain handler, and the SessionModel repository are
  the touched packages; OIDC, service-account resolution, the loop
  client, and the policy decision logic are untouched.
- Status: active. Phase F complete; do not proceed to Phase G without a
  new prompt.

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
