# Stream G Verification — LLM Instrumentation & Admin Settings Subsystem

Read-only verification against actual code. Every claim is backed by `file:line`
evidence. `LAUNCH_READINESS.md` was deliberately ignored as a source of truth.

Tests were executed to confirm they pass, not merely that they exist:

- `go test ./internal/llmusage/ ./internal/llmsettings/ ./internal/agentloop/ ./internal/audit/ ./internal/api/ ./internal/store/` → **all pass**
- `npx vitest run src/components/llm/ src/auth/` → **7 files, 26 tests pass**

---

## Item 1 — Four new SQLite tables via standard migrations

**Status: PRESENT**

All four tables are created in a single standard migration, **migration 017**
(`internal/store/migrations/017_llm_instrumentation.up.sql`), which runs in the
golang-migrate runner used everywhere else.

- `llm_usage` — [017_llm_instrumentation.up.sql:60-80](internal/store/migrations/017_llm_instrumentation.up.sql#L60-L80). Columns: `principal` (nullable), `model`, `input_tokens`, `output_tokens`, `estimated_cost_nano` (+ `currency`), `created_at`, `session_id`, `task_id`. Indexes on `created_at`, `principal`, `model` at [:82-84](internal/store/migrations/017_llm_instrumentation.up.sql#L82-L84).
- `llm_settings` — singleton row, `active_model` + `last_modified`, `CHECK (id = 1)` at [:100-105](internal/store/migrations/017_llm_instrumentation.up.sql#L100-L105), seeded at [:107](internal/store/migrations/017_llm_instrumentation.up.sql#L107).
- `llm_cost_limits` — one row per window, `window_name`/`threshold`, `CHECK (window_name IN ('hourly','daily','monthly'))` at [:123-127](internal/store/migrations/017_llm_instrumentation.up.sql#L123-L127), seeded for all three windows at [:130-132](internal/store/migrations/017_llm_instrumentation.up.sql#L130-L132).
- `llm_runaway_limits` — singleton, `session_token_ceiling`, `CHECK (id = 1)` at [:142-147](internal/store/migrations/017_llm_instrumentation.up.sql#L142-L147), seeded at [:149](internal/store/migrations/017_llm_instrumentation.up.sql#L149).

**Migration runs cleanly.** Verified by the up/down/up roundtrip test
[TestMigration017_UpDownUp_RoundTrip](internal/store/migrations_017_test.go#L22),
which migrates up, asserts the four tables exist, steps `-4` down (020→019→018→017),
asserts the four tables are dropped, then re-applies. The index/schema shape is
additionally pinned by [TestMigration017_LLMUsage_IndexesByName](internal/audit/migration_017_test.go#L137).

> Note: the configured ceiling/threshold maps to a separate `llm_context_budget`
> table added later by migration 019 — orthogonal to the four locked tables and
> not counted here.

---

## Item 2 — Phase F audit table extended with a typed JSON details column; NO separate audit table

**Status: PRESENT** (with one precise nuance about *how* the column was realized)

- **No new column was physically added.** The Phase F audit table already carried
  a JSON `context TEXT NOT NULL DEFAULT '{}'` column from migration 015
  ([015_audit_log.up.sql:65-78](internal/store/migrations/015_audit_log.up.sql#L65-L78)).
  Migration 017's audit change is the **`kind` CHECK widening** to admit
  `'llm_settings_mutation'` and `'llm_limit_triggered'`
  ([017_llm_instrumentation.up.sql:172-191](internal/store/migrations/017_llm_instrumentation.up.sql#L172-L191)).
  The "typed details" requirement is satisfied by reusing that existing JSON
  `context` column with a **typed Go shape**, `audit.Details{Target, Before, After}`
  ([audit.go](internal/audit/audit.go) — `type Details struct`), and the locked
  key vocabulary `AuditCtxTarget/Before/After`
  ([service.go:39-42](internal/llmsettings/service.go#L39-L42)).
- **Settings-mutation writer call site:** `MutationService.runMutation` marshals
  `{target, before, after}` and writes it via `audit.Repository.InsertTx` with
  `Kind: KindLLMSettingsMutation`, `Principal` from context ("by whom")
  — [service.go:206-238](internal/llmsettings/service.go#L206-L238).
- **No separate `llm_settings_audit` table.** Grep for `settings_audit`,
  `llm_settings_audit`, `usage_audit` across `*.go`/`*.sql` returns **zero matches**
  (exit code 1). The only `CREATE TABLE ... audit` statements are the audit_log
  rebuilds in migrations 015/017/018/020 — one logical table.

**Test:** [TestService_SetActiveModel_AtomicWithAudit](internal/llmsettings/service_test.go#L95)
and [TestService_SetCostLimit_OnlyTargetedWindowChanges](internal/llmsettings/service_test.go#L146)
assert the `{target, before, after}` keys round-trip in the audit row;
[TestMigration017_AuditLog_AllKindConstantsInsertable](internal/audit/migration_017_test.go#L157)
asserts the widened CHECK accepts the new kinds.

---

## Item 3 — Adapter-layer instrumentation: every LLM call records a row with the real caller principal

**Status: PRESENT**

- **Single wire site.** The raw provider adapter is wrapped in
  `llmusage.RecorderAdapter` exactly once, in `Services.BuildLLMChain`
  ([llmchain.go:54-74](internal/core/llmchain.go#L54-L74)), invoked from the boot
  path ([server.go:585](cmd/joe/server.go#L585)) **and** both model-swap handlers
  — so a hot-swap carries identical recording (see cross-cutting trace below).
- **`Chat` records one row** keyed off the **context principal**:
  `RecorderAdapter.record` reads `rbac.PrincipalFromContext(ctx)` plus
  `agentctx.SessionID/TaskID` — [recorder.go:230-238](internal/llmusage/recorder.go#L230-L238).
  The call path passes the request context through: agent loop calls
  `a.llm.Chat(ctx, req)` at [agent.go:217](internal/agentloop/agent.go#L217).
- **`ChatStream` and `Embed` record nothing — and that is correct, not a bypass.**
  Both methods on the recorder pass through to the inner adapter
  ([recorder.go:208-225](internal/llmusage/recorder.go#L208-L225)), because in BOTH
  wired production providers these methods are unimplemented stubs that return an
  error and consume no tokens:
  - Claude: [claude.go:141-148](internal/llm/claude/claude.go#L141-L148) (`"streaming not yet implemented"`, `"embeddings not yet implemented"`)
  - Gemini: [gemini.go:202-209](internal/llm/gemini/gemini.go#L202-L209) (same)

  There is no token-producing call site that escapes recording. The knowledge
  embedder ([embedder.go:29](internal/knowledge/embeddings/embedder.go#L29)) calls
  `Embed`, but that resolves to the not-implemented stub, so no real usage exists.

**Tests:** [TestRecorder_Chat_RecordsOneRow](internal/llmusage/recorder_test.go#L290)
asserts `Principal == "user:alice"` from a context set via `rbac.WithPrincipal`,
plus model/tokens/session/task/cost. [TestRecorder_Chat_EmptyPrincipalMapsToNull](internal/llmusage/recorder_test.go#L346)
and [TestRecorder_Chat_UnknownPrincipalSentinelMapsToNull](internal/llmusage/recorder_test.go#L367)
cover the NULL convention; [TestRecorder_Chat_WithoutCancelPreservesPrincipal](internal/llmusage/recorder_test.go#L444)
covers the review-agent goroutine path; [TestRecorder_Embed_NotImplementedPropagatesNoRow](internal/llmusage/recorder_test.go#L547)
asserts the stub path records nothing. The single-wrap invariant is structurally
guarded by [TestPhaseG2_LLMAdapterConstructorWrappedOnce](internal/llmusage/wrap_once_guard_test.go#L46).

---

## Item 4 — Hard session token ceiling enforced in the agentic loop

**Status: PRESENT**

- **Reads the configured ceiling** via the `SessionLimits` interface (storage-backed
  in production): check at [agent.go:237](internal/agentloop/agent.go#L237).
- **Accumulates per-session tokens:** `session.AddTokenUsage(ctx, resp.Usage)` at
  [agent.go:225](internal/agentloop/agent.go#L225) → `s.TotalTokens += usage.TotalTokens`
  at [session.go:293](internal/agentloop/session.go#L293).
- **Terminates** when `ceiling > 0 && session.TotalTokens >= ceiling`, returning
  `ErrSessionTokenCeiling` wrapped with the ceiling+total, BEFORE iterating on tool
  calls — [agent.go:237-241](internal/agentloop/agent.go#L237-L241). An audit row is
  written by `writeRunawayAudit` ([agent.go:355-379](internal/agentloop/agent.go#L355-L379)).
- **Storage-backed ceiling** reads `llm_runaway_limits` with a hardcoded backstop on
  unset/zero/read-failure: `SessionLimitsProvider.SessionTokenCeiling`
  ([providers.go:142-156](internal/llmsettings/providers.go#L142-L156)), wired at
  [server.go:382](cmd/joe/server.go#L382) and into each task at
  [tasks.go:302-334](internal/api/tasks.go#L302-L334).

**Test:** [TestSessionTokenCeiling_TerminatesAtExpectedIteration](internal/agentloop/limits_test.go#L29)
drives a deliberately-exceeded ceiling (45,000 with 20k/call), asserts termination
at call 3 with `ErrSessionTokenCeiling`, the error message naming ceiling+total, AND
exactly one `KindLLMLimitTriggered`/`ActionLLMRunawayTerminated` audit row with the
ceiling/total in its context. [TestSessionTokenCeiling_HappyPathUnchanged](internal/agentloop/limits_test.go#L109)
and [TestSessionTokenCeiling_DefaultProviderActive](internal/agentloop/limits_test.go#L153)
guard the backstop.

---

## Item 5 — Hard cost limits enforced per window, blocking new calls when exceeded

**Status: PRESENT**

- **Pre-call gate** runs before the inner adapter: `RecorderAdapter.Chat` calls
  `r.gate(ctx)` and returns `ErrCostLimitExceeded` **before** `r.inner.Chat`
  — [recorder.go:191-205](internal/llmusage/recorder.go#L191-L205).
- **Queries usage / computes window aggregate:** `gate` sums each window live from
  the table via `repo.SumCostNano(ctx, start, end, currency)` over hourly/daily/
  monthly windows — [recorder.go:325-358](internal/llmusage/recorder.go#L325-L358).
  No tokens are consumed and no usage row is written for a refused call.
- **Blocks** when any window `sum >= limit`, writing a `KindLLMLimitTriggered` /
  `ActionLLMCostLimitRefused` audit row naming every tripped window
  — [recorder.go:385-398](internal/llmusage/recorder.go#L385-L398).
- **Storage-backed thresholds** read `llm_cost_limits` with hardcoded backstops:
  `CostLimitsProvider` ([providers.go:66-100](internal/llmsettings/providers.go#L66-L100)),
  wired at [server.go:381](cmd/joe/server.go#L381) and passed as `Config.Limits` in
  `BuildLLMChain` ([llmchain.go:59-67](internal/core/llmchain.go#L59-L67)).
- **Fail-open on read error** is deliberate and documented
  ([recorder.go:359-377](internal/llmusage/recorder.go#L359-L377)).

**Tests:** [TestGate_HourlyLimit_RefusesBeforeInnerCall](internal/llmusage/gate_test.go#L93)
asserts a blocked call (inner never invoked, sentinel returned);
[TestGate_GenerousLimits_HappyPathUnchanged](internal/llmusage/gate_test.go#L163),
[TestGate_AggregationFailure_FailsOpen](internal/llmusage/gate_test.go#L205),
[TestGate_ZeroLimit_NotEnforced](internal/llmusage/gate_test.go#L261),
[TestGate_MultipleWindowsOver_AllNamedInAuditContext](internal/llmusage/gate_test.go#L299),
[TestGate_DefaultProvider_GateActiveOnNilLimits](internal/llmusage/gate_test.go#L343).

---

## Item 6 — Audit entries for limit triggers, settings changes, active-model changes (existing table, typed details)

**Status: PRESENT**

All three event classes write to the Phase F `audit_log` table:

- **Settings change (cost / runaway / context-budget):** `MutationService.runMutation`,
  `Kind: KindLLMSettingsMutation`, actions `ActionLLMSetCostLimit` /
  `ActionLLMSetRunawayCeiling` / `ActionLLMSetContextBudget`, atomic with the
  mutation via `InsertTx` — [service.go:225-238](internal/llmsettings/service.go#L225-L238).
- **Active-model change:** same path, `ActionLLMSetActiveModel` via
  `SetActiveModel` — [service.go:118-130](internal/llmsettings/service.go#L118-L130).
- **Limit triggers:**
  - Runaway termination → `writeRunawayAudit`, `KindLLMLimitTriggered` /
    `ActionLLMRunawayTerminated`, decision `deny` — [agent.go:366-373](internal/agentloop/agent.go#L366-L373).
  - Cost-window refusal → `writeCostLimitRefusedAudit`, `KindLLMLimitTriggered` /
    `ActionLLMCostLimitRefused`, decision `deny` — [recorder.go:407-423](internal/llmusage/recorder.go#L407-L423).

**Tests:**
- Settings/active-model: [TestService_SetActiveModel_AtomicWithAudit](internal/llmsettings/service_test.go#L95) (asserts kind/action/principal/before/after), [TestService_SetRunawayCeiling_AtomicWithAudit](internal/llmsettings/service_test.go#L186), [TestService_SetActiveModel_RollsBackOnAuditFailure](internal/llmsettings/service_test.go#L231) (atomicity).
- HTTP-level: [TestSetActiveModel_AdminMutatesAndAudits](internal/api/llmadmin_test.go#L414), [TestSetCostLimit_AdminMutatesAndAudits](internal/api/llmadmin_test.go#L465), [TestSetRunawayCeiling_AdminMutatesAndAudits](internal/api/llmadmin_test.go#L501).
- Runaway-trigger audit: asserted inside [TestSessionTokenCeiling_TerminatesAtExpectedIteration](internal/agentloop/limits_test.go#L29).
- Cost-trigger audit: [TestGate_MultipleWindowsOver_AllNamedInAuditContext](internal/llmusage/gate_test.go#L299).

---

## Item 7 — Admin-only HTTP endpoints gated by the Phase H admin capability

**Status: PARTIAL** — all endpoints exist, are correctly gated, and have behavioral
tests; **but the D-0013 structural invariant guards do NOT cover them** (structural
gap, detailed in cross-cutting section).

Endpoints (registered at [server.go:136-138](internal/api/server.go#L136-L138)):

| Route | Handler | requireAdmin? | Evidence |
|---|---|---|---|
| `POST /api/v1/llm/settings/active-model` | `handleSetActiveModel` | **yes** | [llmsettings.go:215](internal/api/llmsettings.go#L215) |
| `POST /api/v1/llm/settings/cost-limit` | `handleSetCostLimit` | **yes** | [llmsettings.go:268](internal/api/llmsettings.go#L268) |
| `POST /api/v1/llm/settings/runaway-ceiling` | `handleSetRunawayCeiling` | **yes** | [llmsettings.go:295](internal/api/llmsettings.go#L295) |
| `POST /api/v1/llm/settings/context-budget` | `handleSetContextBudget` | **yes** | [llmsettings.go:330](internal/api/llmsettings.go#L330) |
| `GET /api/v1/llm/settings` (consumption-against-limit) | `handleGet` | no (intentional: reads open to any authed caller) | [llmsettings.go:139-205](internal/api/llmsettings.go#L139-L205) |
| `GET /api/v1/llm/usage/aggregate` (today/week/month) | `handleAggregate` | no (read) | [llmusageapi.go:107](internal/api/llmusageapi.go#L107) |
| `GET /api/v1/llm/usage/per-model` | `handlePerModel` | no (read) | [llmusageapi.go:144](internal/api/llmusageapi.go#L144) |
| `GET /api/v1/llm/usage/sessions/{id}` | `handleSession` | no (read) | [llmusageapi.go:83](internal/api/llmusageapi.go#L83) |
| `GET /api/v1/llm/usage/per-principal` | `handlePerPrincipal` | **yes** | [llmusageapi.go:161](internal/api/llmusageapi.go#L161) |

The gate is `server.requireAdmin` — the same Phase H admin capability used by the
RBAC admin API (D-0012). The GET-for-current-usage, GET-for-consumption-against-limit
(`handleGet` returns `Effective` from the live providers), and per-principal-breakdown
endpoints all exist as required.

**Behavioral tests (present and passing):**
[TestRequireAdmin_AdminAllowed](internal/api/llmadmin_test.go#L189),
[TestRequireAdmin_NonAdminForbidden](internal/api/llmadmin_test.go#L199) (also asserts
no audit row on denied write), [TestRequireAdmin_AuthDisabledPermits](internal/api/llmadmin_test.go#L218),
[TestSetActiveModel_NonAdminForbiddenNoMutationNoAudit](internal/api/llmadmin_test.go#L440),
[TestSetCostLimit_NonAdminForbiddenNoMutationNoAudit](internal/api/llmadmin_test.go#L480),
[TestUsagePerPrincipal_AdminVsNonAdmin](internal/api/llmadmin_test.go#L596).

**Why PARTIAL (the structural gap):** The D-0013 structural guards
[TestAdminRoutes_AllRequireAdminGate](internal/api/admin_gate_guard_test.go#L33) and
[TestAdminRoutes_AllAuditOnAllow](internal/api/admin_audit_guard_test.go#L37) parse
**only `admin.go`** and inspect **only `adminHandler`-receiver methods** registered in
`registerAdminRoutes` ([admin_gate_guard_test.go:39-46](internal/api/admin_gate_guard_test.go#L39-L46)).
Stream G's admin endpoints live on `llmSettingsHandler`/`llmUsageHandler`
(`llmsettings.go`/`llmusageapi.go`), registered via `registerLLMSettingsRoutes`/
`registerLLMUsageRoutes`, and sit under `/api/v1/llm/` — **not** `/api/v1/admin/`. No
AST guard parses those files. So a future `POST /api/v1/llm/settings/*` mutator added
without `requireAdmin` would NOT fail any structural test — the exact regression class
D-0013 closed for the RBAC surface remains open for the Stream G surface. This is a
test-coverage gap, not a runtime defect: today's endpoints are gated and behaviorally
tested.

---

## Item 8 — Web UI LLM Settings admin page

**Status: PRESENT**

- **Page + wiring:** [LLMSettingsPage.tsx](ui/src/pages/LLMSettingsPage.tsx), routed
  in the app shell under `<RequireAdmin>` at [App.tsx:44](ui/src/App.tsx#L44) (sibling
  of the `/admin` route at [:43](ui/src/App.tsx#L43)). Sidebar entry is `adminOnly`
  ([Sidebar.tsx:22,32](ui/src/components/layout/Sidebar.tsx#L22)). It is wired into the
  post-Stream-J authed shell, not floating.
- **Required displays, all present, consuming Item-7 endpoints
  ([api/llm.ts](ui/src/api/llm.ts)):**
  - Active model display + live-editable selector → `SettingsTab` (`setActiveModel`).
  - Current limit configuration + consumption/effective → `SettingsTab` reads `cost_limits`/`runaway_ceiling` with `effective`/`state`.
  - Live-editable cost & runaway limits → `setCostLimit`/`setRunawayCeiling`.
  - Today/week/month aggregate → `UsageTab` via `useUsageAggregate` ([UsageTab.tsx:8](ui/src/components/llm/UsageTab.tsx#L8)).
  - Per-model breakdown → `usePerModelUsage`.
  - Per-principal breakdown (admin only) → `usePerPrincipalUsage(window, isAdmin)`, rendered only when `isAdmin` ([UsageTab.tsx:42-104](ui/src/components/llm/UsageTab.tsx#L42-L104)).
- **Admin gating:** route-level `RequireAdmin` ([RequireAdmin.tsx](ui/src/auth/RequireAdmin.tsx))
  redirects non-admins; in-page per-principal section is `isAdmin`-gated.

**Tests (present, passing):**
- Admin vs non-admin rendering: [UsageTab.test.tsx:35-46](ui/src/components/llm/UsageTab.test.tsx#L35-L46) — "shows per-principal section and requests it for an admin" / "does not show or request it for a non-admin".
- Limit labelling + write + pending-disable: [SettingsTab.test.tsx](ui/src/components/llm/SettingsTab.test.tsx).
- Plus `ProvidersTab.test.tsx`, `UsageTable.test.tsx`.

> Minor caveat (non-blocking): the admin/non-admin test is at the component level
> (`UsageTab` per-principal section). The route-level wrapper `RequireAdmin` itself has
> no dedicated unit test, though it is the shared gate already exercised in production
> by the `/admin` route. The literal requirement ("a frontend test asserting the page
> renders for admin and is hidden/blocked for non-admin") is met at the section level.

---

## Item 9 — Read-only display of configured providers and key-status; no key entry

**Status: PRESENT**

- **Backing API:** `GET /api/v1/llm/providers` → `handleListProviders`
  ([llmproviders.go:42-71](internal/api/llmproviders.go#L42-L71)), registered at
  [server.go:138](internal/api/server.go#L138). Returns `configured` and `key_present`
  **booleans only** via `llmfactory.HasProviderAPIKey` — never key material (no value,
  prefix, or length).
- **UI:** `ProvidersTab` ([LLMSettingsPage.tsx:60-65](ui/src/pages/LLMSettingsPage.tsx#L60-L65))
  consumes `fetchLLMProviders` ([api/llm.ts](ui/src/api/llm.ts)). Read-only — no key
  entry field anywhere; env vars remain the contract.

**Tests:** [TestProviders_BooleansOnlyAndNoKeyLeak](internal/api/llmadmin_test.go#L622)
(backend, no key leakage) and `ProvidersTab.test.tsx` (UI render).

---

## Cross-cutting verification

### A. Identity propagation — three call sites traced

The real caller principal is set once at the HTTP edge and flows by Go context to the
`llm_usage` row; it is never the server principal or a default.

1. **Edge → context.** `auth.EdgeAuth` resolves the credential and calls
   `rbac.WithPrincipal(r.Context(), p)` — [middleware.go:158/166/178](internal/auth/middleware.go#L158).
   Mounted at [server.go:757](cmd/joe/server.go#L757).

2. **Streaming / non-streaming task (loop iteration).** The task handler keeps the
   request context and adds session/task IDs — [tasks.go:167-171](internal/api/tasks.go#L167-L171)
   — then `prepared.agent.Run(ctx, ...)`. Per loop iteration `a.llm.Chat(ctx, req)`
   ([agent.go:217](internal/agentloop/agent.go#L217)) reaches `RecorderAdapter.record`,
   which stamps `rbac.PrincipalFromContext(ctx)` on the row
   ([recorder.go:231](internal/llmusage/recorder.go#L231)). The loop deliberately does
   NOT re-auth as `svc:server` ([tasks.go:228-235](internal/api/tasks.go#L228-L235)).

3. **Review-agent goroutine (post-request).** The recorder writes the row on
   `context.WithoutCancel(ctx)` — cancellation dropped, principal/session/task values
   preserved — [recorder.go:271-273](internal/llmusage/recorder.go#L270-L274). Verified
   by [TestRecorder_Chat_WithoutCancelPreservesPrincipal](internal/llmusage/recorder_test.go#L444).

In all three, the row's principal is the real caller, asserted concretely by
[TestRecorder_Chat_RecordsOneRow](internal/llmusage/recorder_test.go#L317-L319)
(`user:alice`), with empty/`Unknown` mapping to SQL NULL
([recorder.go:288-296](internal/llmusage/recorder.go#L288-L296)).

### B. Phase F invariant integrity — single audit subsystem

**Confirmed: no parallel audit subsystem.** Grep for `settings_audit` /
`llm_settings_audit` / `usage_audit` across `*.go`/`*.sql` → **zero matches**. The only
audit table is `audit_log`; Stream G writes to it via the single insert-only
`audit.Repository` (`Insert`/`InsertTx`), preserving the append-only triggers
(re-created in 017 at [:215-225](internal/store/migrations/017_llm_instrumentation.up.sql#L215)).
Settings mutations write `KindLLMSettingsMutation`; limit triggers write
`KindLLMLimitTriggered` — both pre-existing kinds in the single table.

### C. Stream G's relationship to D-0013

**Stream G's admin endpoints are NOT covered by the D-0013 structural invariant tests.**
This is the gap the task asked to confirm:

- `TestAdminRoutes_AllRequireAdminGate` and `TestAdminRoutes_AllAuditOnAllow` parse
  **only `admin.go`** and only `adminHandler` methods in `registerAdminRoutes`
  ([admin_gate_guard_test.go:39](internal/api/admin_gate_guard_test.go#L39),
  [admin_audit_guard_test.go:42](internal/api/admin_audit_guard_test.go#L42)).
- Stream G endpoints are on different handler types in different files
  (`llmSettingsHandler`/`llmUsageHandler`), registered under `/api/v1/llm/` — outside
  both guards' parse scope. No equivalent AST guard exists for those files (confirmed:
  the only `ParseFile` guards in `internal/api` are `access_*`, `admin_*`, `regime_*`).
- Mitigation: Stream G endpoints DO admin-gate and DO audit (settings mutations audit
  via `MutationService`, not `recordAdminAudit`), and that behavior is covered by
  the `llmadmin_test.go` behavioral tests above. The missing piece is the
  *future-proofing structural* guarantee, not current behavior.

---

## Conclusion

**Stream G is PARTIAL.**

Items **1, 2, 3, 4, 5, 6, 8, 9 are PRESENT** with implementation and passing tests.
Item **7 is PARTIAL**: every endpoint exists, is correctly admin-gated, and has
behavioral admin-gate/audit tests — but the D-0013 structural invariant guards do not
extend to the Stream G admin surface.

### Gaps and disposition

- **Blocker (should ship before launch):** *None at the runtime level.* All locked
  functional requirements are implemented and behaviorally tested; the binary enforces
  the ceiling, the cost gate, the admin gate, and writes audit rows. There is no
  functional hole that ships broken.

- **Acceptable to defer with documentation (recommended to close, not launch-blocking):**
  1. **Item 7 structural-guard gap (cross-cutting C).** Add an AST guard parsing
     `llmsettings.go`/`llmusageapi.go` (mirroring `admin_gate_guard_test.go`) so a
     future ungated/unaudited `/api/v1/llm/settings/*` mutator fails the build. Today's
     endpoints are safe; this protects future ones. Document the gap until closed.
  2. **Item 8 route-gate test (minor).** `RequireAdmin` has no dedicated unit test for
     the LLM Settings route; admin gating is tested only at the per-principal section
     level. Add a route-level render test for completeness.
  3. **`ChatStream`/`Embed` recording (Item 3).** Pass-through-without-recording is
     correct only because both are unimplemented stubs. When streaming/embeddings land,
     usage recording for those paths must be added or the "every call records" property
     silently regresses. Documented in `recorder.go`; track it.
