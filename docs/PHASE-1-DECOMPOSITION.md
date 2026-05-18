# Joe — Phase 1 Decomposition (Change Plan)

Status: PLANNING. This document is the change-level decomposition of Phase 1
("Session/run durable state") as scoped by
[docs/PLAN-OF-RECORD-RECONCILED.md](PLAN-OF-RECORD-RECONCILED.md) §3 and
specified by [docs/PHASE-0-SESSION-MODEL.md](PHASE-0-SESSION-MODEL.md) §5b, §D,
and the incremental-autonomy seam pattern.

No design decisions are reopened here. Where the design appears hard against
current code, that is recorded as an **implementation risk** under the change
that must absorb it — not as a redesign question.

## 0. Ground truth, workflow, and Phase 1 boundary

**Workflow.** Direct commits to `main`. There is no pull-request process, no
human reviewer, and no separate-LLM review step. Each numbered item in §1 is
an **independently committable change** (one commit or a tight series) with
self-verifying acceptance criteria the implementing session runs locally
before commit. Sequencing and dependency order make the chain coherent; no
external gate exists.

**Baseline.** Every change inherits the discipline already mandated by
[CLAUDE.md](../CLAUDE.md) and the `dev-standards` and `go-backend` skills:
verification-before-claiming, read-before-editing, minimal-changes,
unit + integration tests, error handling, Postgres-portable SQL via
`store.Rebind`, `gofmt -s -w .`, `go vet ./...`, `go test ./...`. This
document does **not** restate or re-teach that baseline. It states only what
is **additional** to it — invariant-specific structural guards, named
ordering / shape assertions, and the pre-implementation hard gates in §6.

Verified against the repo:

- **Persistence interface (5b-6, G5):** `internal/store/store.go` exposes
  `Store` with repository interfaces; `internal/sqlutil/rebind.go` translates
  `?` → `$N` for Postgres. SQLite and Postgres are both compiled paths.
  Several existing concerns (RBAC, graph, knowledge, review, proposals,
  panic-state) sit beside `Store` with raw `*sql.DB` rather than under it —
  that is the established secondary pattern.
- **Existing `sessions` table (migration 001).** A minimal primitive — `id,
  started_at, ended_at, summary, metadata` plus `session_messages` — used
  only by [`internal/api/webui.go`](../internal/api/webui.go). The CLI does
  not use it. Phase 1 builds the new model **alongside** it (new tables,
  e.g. `agent_sessions`) rather than mutating the existing rows; webui
  migration to the new model is post-Phase-1. Justification: scope
  isolation per change, and zero touch on webui's existing contract.
- **Two LLM adapters / two agentic loops still exist** (G1). Plan-of-Record
  §3 Phase 1 is explicit: Phase 1 **does not** collapse the loops. It builds
  the substrate. All executor-touching changes in this plan target
  **joe-core's executor instance only**; the CLI's `useragent.Agent` executor
  instance is not modified (Phase 2 removes it).
- **No session ID is associated with tool calls today.** No idempotency-key
  machinery exists. The `sessiongate` (§C) does not exist. These are all
  net-new in Phase 1.
- **RBAC layer is `*sql.DB`-direct, not behind `Store`** (G5). Phase 1
  consumes RBAC unchanged (§F1, B1); it does not "fix" the RBAC seam.

Phase 1 boundary, restated:

1. Schema + repositories + types: durable session/run/regime/captain/run-step
   /solicitation/world-handle/idempotency-key/action-ledger/finding/warning
   state behind the `internal/store`-style interface pattern.
2. HTTP control plane on joe-core for the model.
3. The **`sessiongate` captain-mutation enforcement primitive** (§C) and its
   **insertion upstream of the unchanged RBAC + Safety pipeline** in
   joe-core's executor.
4. The **idempotency-key persist-before-issue protocol** (§D5) in joe-core's
   executor.
5. The **incremental-autonomy seams** (R2/R4, `confirm_close` self-disposition,
   `joe`-type captain) as defined-but-inert entry points, with the **B-OVR**
   force-yield (R-OVR) compiled in.

Out of Phase 1: collapsing the loops (Phase 2); webui migration to the new
session model; autonomous-Joe behavior; scoped/per-agent unlock; retention
policy; Postgres production deploy.

## 1. Change sequence (real-code-dependency order)

| # | Change | New code / schema | Extends | Depends on |
|---|----|-------------------|---------|------------|
| 1 | Session-model schema + types + repo | migration 009; `internal/sessionmodel/` | `internal/core/services.go` (wiring) | — |
| 2 | Run-model schema + types + repo (runs, steps, solicitations, world handles, idempotency keys, action ledger) | migration 010; `internal/runmodel/` | `internal/core/services.go` | 1 |
| 3 | Findings + warnings schema + types + repo | migration 011; `internal/sessionmodel/findings.go`, `internal/warnings/` | `internal/core/services.go` | 1 |
| 4 | Session/findings/warnings HTTP CRUD | `internal/api/sessions.go`, `internal/api/findings.go`, `internal/api/warnings.go`; route registrations in `cmd/joe-core/main.go` | — | 1, 3 |
| 5 | Regime declare/resolve API + RBAC policy entries (`can_declare_incident`, `can_resolve_incident`) | `internal/api/regime.go`; RBAC policy seeding | `internal/rbac/` policy-entry registry, migration 012 (RBAC policy entries) | 1, 4 |
| 6 | Captain attach + transfer state machine | `internal/sessionmodel/captain.go`, `internal/api/captain.go` | Change 1 repo (extended) | 1, 4 |
| 7 | Run lifecycle HTTP API | `internal/api/runs.go` | — | 2, 4 |
| 8 | Captain-session gate primitive (callable, not yet wired) | `internal/sessiongate/` | — | 1, 2, 5, 6 |
| 9 | SessionMiddleware + idempotency-key wiring in joe-core executor | `internal/api/sessionmw.go`; wrapper around `internal/tools/executor.go` in joe-core only | `cmd/joe-core/main.go` middleware chain | 2, 7 |
| 10 | Captain-gate insertion in joe-core executor | extend the joe-core executor wrapper from Change 9 | — | 8, 9 |
| 11 | Hard-delete cascade (incident + linked investigations expunge) | `internal/sessionmodel/cascade.go`; extend delete endpoint | — | 1, 2, 3, 4 |
| 12 | Incremental-autonomy inert seams + R-OVR force-yield | seam stubs in `internal/api/regime.go`, `internal/sessionmodel/captain.go` | — | 5, 6 |

All 12 changes are independently committable. Sequencing is by FK / interface
dependency, not idealized order.

## 2. Per-change detail

### Change 1 — Session-model schema + Go types + repo

**Scope.** Establish the durable shape of an agent session, the system
regime record, and the per-session captain binding.

**Creates.**

- Migration `internal/store/migrations/009_session_model.up.sql` (+ down).
  Tables:
  - `agent_sessions` (id, type ∈ {`incident`,`investigation`,`other`},
    created_at, last_activity_at, creator_principal, linked_incident_id
    FK→`agent_sessions(id)` **`ON DELETE CASCADE`** nullable, retention_class
    TEXT). Type=`incident` carries `incident_state` ∈
    {`declared`, `being_worked`, `believed_mitigated`, `resolved`,
    `reviewed`}. Non-incident rows have `incident_state` NULL (§5b-1/2).
    Postgres-safe types only. **No `deleted_at` column** — §5b-5 mandates
    expunge, not tombstone; the §5b-4 retention seam is `retention_class`
    only. **Two-level expunge is a pure schema property:** because
    `linked_incident_id` is a self-FK `ON DELETE CASCADE` on
    `agent_sessions(id)`, deleting an incident row cascades to its linked
    investigation rows in the same SQL statement; those investigations'
    child rows then cascade per their own `ON DELETE CASCADE` FKs
    (Changes 2 and 3). The cascade is one DB statement, no application
    code.
  - `system_regime` single-row table (`mode` ∈ {`normal`,`incident`},
    `declared_at`, `declared_by_principal`, `declared_kind` ∈ {`human`,
    `joe`}). Single-row pattern follows `cluster_panic_state` from
    migration 008.
  - `session_captains` (session_id FK **`ON DELETE CASCADE`**, captain_type
    ∈ {`human`,`joe`}, principal, attached_at, detached_at nullable,
    transfer_state ∈ {`active`,`transfer_requested`,`transfer_confirmed`}
    nullable, incoming_principal nullable, transfer_initiator ∈ {`outgoing`,
    `incoming`} nullable).
- `internal/sessionmodel/` package: types
  (`AgentSession`, `SessionType`, `IncidentState`, `Captain`, `CaptainType`,
  `Regime`, `RegimeMode`, `TransferState`) + `sessionmodel.Repository`
  interface + `sessionmodel.SQLRepository` implementation using `*sql.DB`
  + driver + `store.Rebind`.
- Wire `services.SessionModel sessionmodel.Repository` in
  `internal/core/services.go`. Initialize from `sqlStore.DB()` +
  `sqlStore.Driver()` in `cmd/joe-core/main.go` (parallel to the existing
  RBAC repo wiring pattern; not under `store.Store`, matching §5b-6's
  observation that secondary repos live beside it).

**Does not create.** HTTP, business rules, behavior of any kind.

**Acceptance criteria (self-verified in the implementing session).**

- Baseline (CLAUDE.md/skills): unit + integration tests, gofmt/vet/test
  all clean.
- Schema runs against **both** drivers in the change's own test suite: a
  test in `internal/store/store_test.go` (or `internal/sessionmodel/schema_test.go`)
  applies `Store.Migrate()` against an in-memory SQLite and against a Postgres
  test instance (via `testcontainers-go` or equivalent already used in the
  repo, or skipped with `t.Skip` if the env doesn't expose one — the SQLite
  half must always run) and round-trips the down migration on each.
- §6-C cascade migration test ships with this change (see §6-C).

**Implementation risks (additional to the baseline).**

- **Invariant 6 (no SQLite-specific coupling) — named structural guard:**
  the cross-driver migration test described above. If the test does not run
  Postgres locally, the SQLite half plus the Postgres-portable-SQL rule
  (use only types and constructs accepted by `pgx`; no `AUTOINCREMENT`, no
  `STRICT`, no SQLite-only `JSON1` calls; use `?` + `Rebind`) is the only
  protection — recorded as a residual risk in §6.
- **§6-C applies (see §6 hard gates).**
- **Scope isolation (existing `sessions` table untouched):** the change's
  diff does not modify `internal/store/sessions.go`, migration 001, or
  `internal/api/webui.go`. Covered by ordinary diff scope; no separate
  guard needed.

### Change 2 — Run-model + action-ledger + idempotency-key schema/types/repo

**Scope.** The §D durable run substrate.

**Creates.**

- Migration `010_run_model.up.sql` (+ down). Tables (all FKs to
  `agent_sessions(id)` are **`ON DELETE CASCADE`**, all FKs to `agent_runs(id)`
  are **`ON DELETE CASCADE`**):
  - `agent_runs` (id, session_id FK, state ∈ {`running`,`awaiting_input`,
    `awaiting_world`,`completed`,`failed`,`cancelled`}, started_at,
    ended_at nullable, last_step_id nullable).
  - **Partial unique index** `idx_agent_runs_one_running_per_session`
    on `(session_id) WHERE state = 'running'` — the named structural guard
    for D3 / Invariant 1.
  - `run_steps` (id, run_id FK, step_number, kind ∈ {`reasoning`,
    `tool_call_intent`,`tool_call_result`,`solicitation_open`,
    `solicitation_resolved`,`world_handle_recorded`,`world_handle_observed`},
    payload JSON/TEXT, persisted_at). The durable unit per §D4.
  - `run_solicitations` (id, run_id FK, kind ∈ {`decision`,`provide_data`,
    `confirm_close`}, payload, created_at, resolved_at nullable, resolution
    payload nullable, liveness_flag ∈ {`attached_human_now`,
    `out_of_band_human_work`} for `provide_data`). One state, payload
    discriminates per §D taxonomy.
  - `run_world_handles` (id, run_id FK, locator, query_meta, recorded_at,
    last_poll_at nullable, last_observed_state nullable).
  - `tool_idempotency_keys` (key PK, run_id FK, step_id FK NULL, tool_name,
    args_hash, created_at, completed_at nullable, result JSON nullable,
    status ∈ {`issued`,`completed`,`failed`}). §D5.
  - `action_ledger` (id, run_id FK, idempotency_key FK, tool_name, tier
    ∈ {1,2,3}, principal, source_id nullable, summary, recorded_at,
    completed_at nullable, status). The §D8 attaching-SRE view.
- `internal/runmodel/` package: types + `runmodel.Repository` interface +
  SQL implementation. `services.RunModel runmodel.Repository` wired in
  `internal/core/services.go`.

**Acceptance criteria (self-verified).**

- Baseline applies.
- Migration up/down round-trip on SQLite (always) and Postgres (when env
  available; otherwise SQLite-only with the Postgres-portable-SQL rule as
  residual risk per §6).
- **D3 single-running structural guard test:** unit test in
  `internal/runmodel/schema_test.go` attempts to insert a second `running`
  row for the same `session_id` and asserts a UNIQUE-constraint violation.
  Test must fail loudly if the index is missing.
- **D5 idempotency-key repo-API shape test:** unit tests in
  `internal/runmodel/repository_test.go` assert structural protocol:
  - `RecordToolIntent(ctx, key, runID, toolName, argsHash)` is idempotent
    on duplicate key — returns the existing row, does **not** overwrite.
  - `MarkToolCompleted(key, result)` returns an error against a row whose
    status is already `completed` and does **not** overwrite the prior
    result.
  - No method exists on the repo that lets a caller record a tool result
    without an already-issued key — assertion is the repo's interface
    definition itself (`MarkToolCompleted` requires an existing key by
    contract). Pinning the interface shape is sufficient because Go's
    type system makes a "write-result-without-prior-intent" path
    structurally absent.
- §6-C cascade migration test extension (this change adds `agent_runs`,
  `run_steps`, etc.; the §6-C test in Change 1 must be extended to assert
  zero orphaned child rows after incident delete).

**Implementation risks (additional to the baseline).**

- **Invariant 2 (D5) — named structural guards listed above** in
  `internal/runmodel/repository_test.go`.
- **Invariant 1 (D3) — named structural guard** is the partial unique
  index plus its test in `internal/runmodel/schema_test.go`.
- **§6-C applies.**

### Change 3 — Findings + Joe-warnings schema/types/repo

**Scope.** §A4 cross-session attribution and §E warnings surface.

**Creates.**

- Migration `011_findings_warnings.up.sql` (+ down):
  - `findings` (id, source_session_id FK→agent_sessions
    **`ON DELETE CASCADE`**, target_session_id FK→agent_sessions
    **`ON DELETE CASCADE`**, author_principal, body TEXT, posted_at,
    referenced_investigation_session_id FK
    **`ON DELETE CASCADE`** nullable). Annotation semantic only.
  - `joe_warnings` (id, raised_at, signal_reference, body,
    source_investigation_session_id FK→agent_sessions
    **`ON DELETE CASCADE`** nullable, reviewed_at nullable,
    reviewed_by_principal nullable). Append-only.
- `internal/findings/` and `internal/warnings/`: repo + types.
- Wired into `services.Findings` and `services.Warnings`.

**Acceptance criteria (self-verified).**

- Baseline applies.
- **Append-only structural guard for warnings:** the `warnings.Repository`
  interface defined in `internal/warnings/repository.go` exposes exactly
  `RaiseWarning`, `ListWarnings`, `MarkReviewed` — no `Update*`, no
  `Delete*`, no queue/state methods. A unit test in
  `internal/warnings/repository_test.go` uses `reflect.TypeOf` over the
  interface to assert the method set is exactly those three names. Adding
  a method without updating the test fails the test.
- §6-C cascade migration test extension (this change adds `findings` and
  `joe_warnings`; the §6-C test must be extended to assert cascade of
  `findings` rows on incident delete).

**Implementation risks (additional to the baseline).**

- **R9 / §E2 (no queue semantics) — named structural guard** is the
  interface-shape test above.
- **§6-C applies.**

### Change 4 — Session / findings / warnings HTTP CRUD

**Scope.** Data-plane HTTP for sessions, findings, warnings, and the regime
**read** endpoint. No behavioral rules.

**Creates.**

- `internal/api/sessions.go`: `POST/GET/DELETE /api/v1/sessions`,
  `GET /api/v1/sessions/{id}`, `GET /api/v1/sessions?type=...`.
  `DELETE` in this change is a simple per-row delete; cascade lives in
  Change 11 (which is layered on top of the schema-level
  `ON DELETE CASCADE` shipped in Changes 1/2/3 per §6-C).
- `internal/api/findings.go`: `POST/GET /api/v1/sessions/{id}/findings`.
- `internal/api/warnings.go`: `GET /api/v1/warnings`, internal-only
  raise path (no `POST /api/v1/warnings` for humans — humans don't raise
  warnings, only review them; the raise path is for joe-core internal use).
- `internal/api/regime.go`: `GET /api/v1/regime` (read-only here; declare/
  resolve in Change 5).
- Route registration in `cmd/joe-core/main.go`. Routes pass through existing
  `BearerAuth → IdentityMiddleware → EnforcementMiddleware` chain.

**Acceptance criteria (self-verified).**

- Baseline applies.
- Integration tests: create session of each type; list filtered by type;
  read; delete (non-cascading); post finding; list findings; list warnings.
- **§5b-3 team-global specific assertion:** an integration test creates a
  session using one API-keyed principal and reads it back using a *different*
  API-keyed principal that holds read authorization. Asserts the second
  principal sees the same row. If a future handler quietly adds a
  `WHERE created_by_principal = ?` filter, this test fails.

### Change 5 — Regime declare/resolve + RBAC policy entries

**Scope.** §R2/R4/R6/R7 — declare and resolve incident regime.

**Creates.**

- Migration `012_regime_rbac.up.sql`: seed two new RBAC policy entries
  (`can_declare_incident`, `can_resolve_incident`). Use the existing
  `rbac_policies` table shape from migration 006. **Per §6-B, the
  encoding is chosen only after the read-only investigation of how
  `IsAllowed` evaluates an unmatched / sentinel / "*" sourceID, recorded
  as a file:line finding in the commit message.** Do not redesign RBAC.
- `internal/api/regime.go` extension: `POST /api/v1/regime/declare`,
  `POST /api/v1/regime/resolve`.
  - Declare (human path live): `can_declare_incident` checked; on success,
    creates an `incident`-type session and immediately attaches the
    declaring human as captain (R-CAP1).
  - Resolve (human path live): `can_resolve_incident` checked; transitions
    the active incident session `believed_mitigated → resolved` and clears
    `system_regime` to `normal`. The incident-lifecycle → regime-resolve
    coupling per §5b-1.
  - Joe-autonomous declare/resolve paths: **defined-but-inert seam stubs**
    in this change (always returns 403 / "not authorized in v1"). The
    incremental-autonomy wiring lives in Change 12.

**Acceptance criteria (self-verified).**

- Baseline applies.
- §6-B investigation finding recorded in the commit message; integration
  test in `internal/api/regime_test.go` asserts (a) the chosen RBAC
  encoding grants `can_declare_incident` to the configured principal and
  (b) the same encoding does **not** incidentally grant that principal
  any new source authority over arbitrary sources (assertion: pre-existing
  source-permission tests still produce the same allow/deny matrix).
- Declare flips regime to `incident`, creates session, attaches captain —
  asserted as a single-transaction operation by injecting a forced rollback
  after the captain attach and asserting the regime row is also rolled back.
- Resolve clears regime, transitions session — same single-transaction
  assertion.
- Declare without `can_declare_incident` → 403; resolve without
  `can_resolve_incident` → 403.
- **Invariant 4 (R5 declare-may-auto / resolve-may-not) — named
  structural guard:** a CI grep test in `internal/api/regime_invariant_test.go`
  (a `go test` that reads source files with `go/parser`) asserts that the
  call `regimeRepo.SetMode(regime.ModeNormal, ...)` appears in exactly one
  function in the codebase: the human-resolve handler in
  `internal/api/regime.go`. Any other occurrence (test fixtures excepted
  by a documented path-skip list inside the guard test) fails the build.
  This guard re-fires when Change 12 lands and is the structural enforcement
  of "no auto-resolve via confirm_close" (also referenced from Change 12).
- **R7 (no unwatched ambiguous incident) specific assertion:** integration
  test asserts that **every** code path that transitions regime to
  `incident` also produces an `agent_sessions` row in the same transaction.
  Implemented by parameterized test over all declare entry points (currently
  one; future autonomous path inherits the test).

**Implementation risks (additional to the baseline).**

- **§6-B applies.**

### Change 6 — Captain attach + transfer state machine

**Scope.** §B captain state machine: `active → transfer_requested →
transfer_confirmed → active`, dual initiation, B3 contraction handling.

**Creates.**

- `internal/sessionmodel/captain.go`: business rules over `session_captains`.
  - `Attach(sessionID, principal)`: rules per R-CAP1/2/3. For a
    `pending_captain` (autonomous-declared) incident, the first
    RBAC-authorized human attach becomes captain. For a normal-regime
    session, attach is informational (no captain semantics outside incident
    regime, §B4).
  - `BeginTransfer(sessionID, initiator ∈ {outgoing, incoming}, target?,
    currentCaptainReachable bool)`: enforces the §B state machine.
    - outgoing-initiated → `transfer_requested` waiting on outgoing's choice
      (B3 finish-or-cancel).
    - incoming-initiated when current captain reachable → records `decision`
      solicitation on the current run (calls Change 2's solicitation repo);
      stays `active`.
    - incoming-initiated when current captain unreachable → proceeds
      directly to `transfer_confirmed`. **"Unreachable" must resolve to a
      reachability signal cited per §6-D** — either an existing signal in
      the codebase (file:line in the commit message) or a net-new signal
      built in this change (also recorded in the commit message and listed
      under Creates).
  - `ConfirmTransfer(sessionID)`: completes; new captain becomes active;
    principal-to-captain binding (§B1) is recomputed.
- `internal/api/captain.go`: `POST /api/v1/sessions/{id}/captain/attach`,
  `POST /api/v1/sessions/{id}/captain/transfer/begin`,
  `POST /api/v1/sessions/{id}/captain/transfer/confirm`,
  `POST /api/v1/sessions/{id}/captain/transfer/cancel`.
- **If §6-D finds reachability must be built:** the implementation
  (e.g. attach-freshness column on `session_captains` or equivalent) ships
  in this change, in the same commit series. Not a footnote, not deferred.

**Acceptance criteria (self-verified).**

- Baseline applies.
- §6-D finding recorded in the commit message (existing-signal cite, or
  net-new-signal description + its tests).
- Full state-machine matrix tested in
  `internal/sessionmodel/captain_test.go`: outgoing-initiated happy path;
  incoming when outgoing reachable produces a `decision` solicitation on
  the run; incoming when outgoing unreachable yields direct
  `transfer_confirmed` against the actual reachability mechanism (existing
  or net-new per §6-D — the test must exercise the real path, not a mock
  with no real-world tie); decline by current captain → stays `active`.
- **§B1 principal-threading specific assertion:** integration test asserts
  that after transfer, `sessionModelRepo.CurrentCaptainPrincipal(sessionID)`
  returns the new principal. The repo must expose this getter because
  Change 10 consumes it.
- **§B2 null-authority on `pending_captain` specific assertion:** unit test
  asserts `CurrentCaptainPrincipal(sessionID)` returns `(_, false)` for an
  unattached incident session.
- **B3 finish-or-cancel specific assertion:** test exercises the
  outgoing-initiated path and asserts a `decision` solicitation row is
  created on the run; no silent / automatic disposition path is present.
- B-OVR test lives in Change 12 (not this change), with a TODO marker in
  the transfer code pointing to that change.

**Implementation risks (additional to the baseline).**

- **§6-D applies.**

### Change 7 — Run lifecycle HTTP API

**Scope.** HTTP control plane for runs / steps / solicitations / world
handles / action ledger. This is the API joe-core's Core Agent (and, post-
Phase-2, the unified runtime) will call to make execution durable.

**Creates.**

- `internal/api/runs.go`:
  - `POST /api/v1/sessions/{id}/runs` — start a run.
  - `POST /api/v1/runs/{id}/steps` — record a step (kind + payload).
  - `POST /api/v1/runs/{id}/solicitations` — open a solicitation (`decision`/
    `provide_data`/`confirm_close`); transitions run state to
    `awaiting_input`.
  - `POST /api/v1/solicitations/{id}/resolve` — resolves; transitions run
    state back to `running`.
  - `POST /api/v1/runs/{id}/world_handles` — record world handle; transitions
    to `awaiting_world`.
  - `POST /api/v1/runs/{id}/world_handles/{hid}/observe` — record poll
    result; if terminal, returns run to `running`.
  - `POST /api/v1/runs/{id}/terminate` — `cancelled` (§D7 third form).
  - `POST /api/v1/runs/{id}/complete` — `completed`.
  - `POST /api/v1/runs/{id}/fail` — `failed`.
  - **Idempotency-key API contract:** every mutation endpoint that crosses
    into `world_handles` or that backs a tool call requires the caller to
    supply an idempotency key in the request body. The server rejects
    requests that omit one. The executor wiring in Change 9 is what
    actually *uses* this API.

**Acceptance criteria (self-verified).**

- Baseline applies.
- State-machine matrix tested at the HTTP layer: legal transitions
  accepted, illegal transitions (e.g. `completed → running`) refused with
  4xx.
- `awaiting_input` solicitation payload validation: `decision` requires a
  bounded option set; `provide_data` requires a `liveness` flag;
  `confirm_close` requires an action-ledger snapshot in the payload.
  Asserted by table-driven request validation tests.
- **§D3 single-threaded specific assertion:** an integration test starts a
  run on a session, then attempts a second `POST /sessions/{id}/runs`
  against the same session and asserts a 409 (backed by Change 2's partial
  unique index — this is the API surface for that structural guard).
- **§D6 never-re-issue specific assertion:** an integration test asserts
  the world-handle observe endpoint's response schema contains no field
  named `retry`, `re_issue`, or equivalent. A JSON-schema or struct-tag
  assertion at the test boundary suffices.
- **§D7 override forms specific assertion:** integration tests cover all
  three forms; no fourth path (a "rewind to pre-effect" / "treat-as-never-
  happened" endpoint) exists. The test enumerates the registered routes
  for the runs subtree and asserts the set matches exactly the listed
  endpoints.
- **§D8 SITREP-shape specific assertion:** `GET /api/v1/runs/{id}` returns
  rehydration payload = persisted synthesized understanding + current state
  + open solicitation / world handle + action ledger. No reasoning trace.
  Tested by struct-field allowlist on the response type.

### Change 8 — Captain-session gate primitive (callable, not yet wired)

**Scope.** §C session-model-owned mutation gate, as a pure function the
executor will call in Change 10.

**Creates.**

- `internal/sessiongate/` package:
  - `Decision` enum: `Allow | RefuseRedirect{captainSessionID}`.
  - `Check(ctx, sessionModelRepo, regimeRepo, sessionID, principal,
    tier classification.Tier) Decision`.
  - Logic:
    - T1 → `Allow` always (reads/discovery unaffected per A1/C1).
    - Regime `normal` → `Allow` (§R1, §B4: no captain outside incident).
    - Regime `incident` → session must be the captain session of the
      current incident **and** principal must equal current captain's
      RBAC principal. Otherwise `RefuseRedirect{captainSessionID}`.
    - Captain is `pending_captain` (no captain attached yet) →
      `RefuseRedirect` with empty target (B2 null authority).

**Acceptance criteria (self-verified).**

- Baseline applies.
- Matrix unit tests: {normal, incident} × {captain session, non-captain
  session, pending_captain} × {T1, T2, T3} × {captain principal, other
  principal}.
- **Invariant 5 (C2) — named structural guard:** the
  `internal/sessiongate/` package does not import `internal/rbac`. A unit
  test in `internal/sessiongate/import_guard_test.go` runs
  `go list -deps github.com/jaimegago/joe/internal/sessiongate` (or uses
  `go/packages` programmatically) and asserts `internal/rbac` is not in
  the import closure. Equivalent to a `golangci-lint depguard` rule but
  self-contained in the change.
- **§C4 positional, not semantic — named structural guard:** the `Check`
  function signature pinned by the package's exported API; a unit test in
  the same `import_guard_test.go` uses `go/ast` (or `reflect.TypeOf` on
  a stand-in function value) to assert the parameter list contains no
  parameter named `sourceID`, `tool`, `blast`, or `radius`. Tier is
  permitted (it short-circuits T1 reads only).

### Change 9 — SessionMiddleware + idempotency-key wiring in joe-core executor

**Scope.** Thread session/run context through joe-core's HTTP, and persist
an idempotency key before every world-mutating tool call **in joe-core's
executor instance**. The CLI's `useragent.Agent` executor is **not modified**
(Phase 2 removes it; modifying it now is wasted work).

**Creates.**

- `internal/api/sessionmw.go`: `SessionMiddleware` that reads
  `X-Joe-Session-ID` and `X-Joe-Run-ID` request headers and threads them
  into request context. Installed in the joe-core middleware chain
  **after `IdentityMiddleware`, before `EnforcementMiddleware`**.
- A new executor wrapper in joe-core only (e.g.
  `internal/coreagent/executor_durable.go`) that:
  1. Reads sessionID + runID from context.
  2. For T2/T3 tools: derives an idempotency key (caller-supplied via
     `X-Joe-Idempotency-Key` or computed `hash(runID, stepNumber,
     toolName, argsHash)`).
  3. Calls `runmodel.RecordToolIntent(key, ...)` — persists the key.
  4. **Then** invokes the underlying `internal/tools/executor.Execute`.
  5. On return, calls `runmodel.MarkToolCompleted(key, result)`.
  6. On resume (key already exists, status=`completed`), returns the stored
     result without re-executing — §D5 invariant.
- Wired into the Core Agent's executor field in `cmd/joe-core/main.go` so
  joe-core's tool calls go through the durable wrapper.

**Acceptance criteria (self-verified).**

- Baseline applies.
- **D5 ordering — named structural guard:** test in
  `internal/coreagent/executor_durable_test.go` uses a spy/fake
  `runmodel.Repository` and a spy tool to record call order; asserts the
  sequence is `RecordToolIntent` → `tool.Execute` → `MarkToolCompleted`
  for every T2/T3 call. Test must fail if the order is reversed or if
  any of the three calls is skipped.
- **Replay specific assertion:** test issues a T2 tool with key K, then
  re-issues with key K and asserts the underlying tool is invoked exactly
  once (spy count == 1) and the cached result is returned the second
  time.
- **Crash-resume specific assertion:** test issues a T2 tool, force-fails
  before `MarkToolCompleted`, then re-issues with the same key and asserts
  the tool re-executes cleanly (status stays `issued` until completion;
  re-issue is permitted while status=`issued`).
- **T1 bypass specific assertion:** T1 tool invoked → no
  `RecordToolIntent` call (spy count == 0).
- **Single-loop / no goroutine fan-out — named structural guard:** test
  in the same file uses a sentinel goroutine ID (captured via
  `runtime.Stack` or a `chan struct{}` synchronization) to assert that
  `tool.Execute` runs on the caller's goroutine. Any `go func()` wrap
  inside the wrapper makes this test fail.
- **Phase 1 boundary — scope-isolation specific assertion:** the change's
  diff does not modify `internal/useragent/agent.go` or
  `internal/tools/executor.go` itself. Covered by ordinary diff scope.

### Change 10 — Captain-gate insertion in joe-core executor

**Scope.** Insert the Change 8 gate **upstream** of the unchanged RBAC +
Safety pipeline in joe-core's executor wrapper.

**Creates.**

- Extends Change 9's wrapper: before any other check, call
  `sessiongate.Check(ctx, ...)`. On `RefuseRedirect`, return a structured
  refusal carrying the captain session ID. On `Allow`, proceed into the
  existing pipeline (RBAC `IsAllowed` with the **current captain's
  principal** per B1 — the wrapper substitutes the captain principal into
  the context before forwarding, when in incident regime; in normal regime,
  the request-time principal is used unchanged).
- A refusal-response shape that gives the LLM the redirect-to-captain
  semantic so it can synthesize a finding (A4 path) rather than retrying.

**Acceptance criteria (self-verified).**

- Baseline applies.
- **§6-A applies — the staging-soak checklist item is a mandatory
  acceptance criterion, self-certified in the commit message, and the
  end-to-end integration test below ships in this change.**
- End-to-end integration test (in `internal/coreagent/executor_durable_test.go`
  or `internal/api/regime_integration_test.go`): incident regime declared
  → captain attached → T2 tool from captain session → allowed; same T2
  tool from a non-captain session → refused with redirect payload
  carrying the captain session ID; regime returned to normal → both
  sessions can mutate again.
- **Invariant 5 (C2) ordering — named structural guard:** spy on
  `policyEngine.IsAllowed` asserts it is **not** called on the refusal
  path. Pipeline order: gate → `IsAllowed` → tier/safety → execute.
- **§C5 non-configurable floor — behavioral permutation test (primary):**
  a table-driven test in `internal/coreagent/executor_durable_test.go`
  enumerates every meaningful configuration permutation the binary
  supports — every env var, config-file flag, and feature toggle present
  in `internal/config/` (and anything referenced from
  `cmd/joe-core/main.go`'s config wiring). For each permutation, the test
  declares incident regime, attaches a captain, issues a T2 mutation from
  a non-captain session, and asserts it is refused. The gate
  cannot be made conditional on configuration because no permutation
  flips the refusal to an allow. An optional secondary AST signal in
  `internal/coreagent/executor_durable_guard_test.go` may flag a
  `sessiongate.Check` nested inside a config-predicated `if` as a
  code-smell — but the behavioral permutation test, not the AST signal,
  is the primary enforcement (AST guards both false-pass via helper
  extraction and false-fail on legitimate refactors; the permutation test
  asserts the property end-to-end).
- **§B1 principal substitution — specific assertion:** integration test
  asserts that in incident regime, the principal passed to `IsAllowed`
  equals the current captain's principal (not the request-time principal);
  in normal regime, the request-time principal is passed unchanged.
  Implemented by spy on `policyEngine.IsAllowed`.

**Implementation risks (additional to the baseline).**

- **§6-A applies.**
- **Phase 1 boundary (G1):** this change alters joe-core executor
  behavior but does not unify or remove either loop. The CLI loop bypasses
  this gate entirely until Phase 2 removes the CLI loop. Covered by
  Change 9's scope-isolation assertion; no separate guard needed here.

### Change 11 — Hard-delete cascade (incident + linked investigations expunge)

**Scope.** §5b-5 — incident session + linked investigations expunge as a
single cascade. No tombstone. Non-incident sessions with no linkage delete
independently. The two-level cascade itself is a **pure schema property**
shipped in Changes 1/2/3 per §6-C (incident → linked investigations via
`linked_incident_id ON DELETE CASCADE`; each session → its child rows via
their own `ON DELETE CASCADE` FKs). This change is the API handler only —
it routes `DELETE /api/v1/sessions/{id}` to the single SQL DELETE the
schema cascade already handles. No gather step, no application-level
fan-out.

**Creates.**

- `DELETE /api/v1/sessions/{id}` handler in `internal/api/sessions.go`:
  executes `DELETE FROM agent_sessions WHERE id = ?` (one statement) for
  any session type. For `incident`, the schema's self-FK cascades to
  linked investigations; for any session, the schema's child-table FKs
  cascade to runs / findings / etc. No type-conditional logic is required
  in the handler beyond the existing authorization check.
- No new package, no `CascadeDeleteIncident` function — the schema does
  the work. If the handler grows a multi-statement gather/delete sequence,
  §6-C has failed and the schema must be the fix.

**Acceptance criteria (self-verified).**

- Baseline applies.
- Integration test: incident I with two investigations J1, J2
  (`linked_incident_id=I`), each with runs, steps, idempotency keys,
  action ledger entries, findings → delete I → all three sessions and all
  their child rows gone in one transaction. Implemented by inspecting
  per-table row counts before and after.
- Integration test: investigation J with no `linked_incident_id` →
  delete J → only J deleted; nothing else touched.
- **Resolved-incident postmortem property — specific assertion:** test
  asserts that resolving an incident (Change 5's resolve handler) does
  **not** delete its linked investigations. The negative case: count
  investigations before and after resolve; equal.
- **§5b-5 expunge intent — named structural guard (absolute, no
  allowlist):** a CI grep test in `internal/store/expunge_guard_test.go`
  (in the `store` package, which owns the embedded `migrations/*.sql` via
  `//go:embed`) reads every file under `migrations/*.sql` through the
  embedded FS and asserts **zero occurrences** of the identifiers
  `deleted_at`, `archived_at`, or `tombstone`. The Phase 1 schema carries
  no soft-delete column anywhere — §5b-5 expunge plus the §5b-4 retention
  seam expressed by `retention_class` alone leave no path to a tombstone
  column. Any future appearance of those identifiers fails the build with
  no exception.
- This change ships **no migration**. If it requires one, §6-C has failed
  — that itself is the §6-C self-check.

### Change 12 — Incremental-autonomy inert seams + R-OVR force-yield

**Scope.** The "incremental-autonomy seam pattern" stated once in
PHASE-0-SESSION-MODEL.md: build the seams, not the behavior. Plus the
non-negotiable B-OVR (R-OVR) invariant, compiled in.

**Creates.**

- `internal/seams/` package: `JoeAutonomousDeclareEnabled`,
  `JoeAutonomousResolveEnabled`, `JoeConfirmCloseDispositionEnabled`,
  `JoeCaptainTypeEnabled` — **compile-time `const false`**, NOT
  config-driven; the seams are inert at the same tier as the invariants
  per the §"incremental-autonomy seam pattern" section. Doc comment on
  `JoeCaptainTypeEnabled` explicitly records the §B R-OVR limitation
  (current panic/unlock is a global boolean; enabling Joe-captain is only
  safe alongside scoped per-agent unlock, which Phase 1 does not build).
- Regime declare handler (Change 5): the autonomous path stub now gates
  on `seams.JoeAutonomousDeclareEnabled` — always false in v1, returns
  403.
- Regime resolve handler (Change 5): same pattern.
- `confirm_close` self-disposition (Change 7 solicitations): same
  pattern.
- Captain attach (Change 6): support `captain_type=joe` in the *type
  system* and persistence, but `Attach(..., type=joe)` always returns
  403 in v1.
- **R-OVR force-yield:** in Change 6's transfer state machine, the branch
  where `currentCaptain.Type == joe` and an RBAC-authorized human
  requests command is **structurally** routed to immediate
  `transfer_confirmed`, bypassing approve/decline/timeout. Not behind a
  flag — compiled in.

**Acceptance criteria (self-verified).**

- Baseline applies.
- **Inert-seam specific assertions:** for each of the four seam flags, a
  test calls the autonomous path and asserts 403; a paired test uses a
  build-tag-isolated test file (e.g. `_seam_enabled_test.go` with
  `//go:build seam_enabled`) that overrides the constant via a parallel
  test-only constant and asserts the path becomes reachable. Future
  enablement is therefore a one-line constant change, not a wiring
  exercise.
- **R-OVR force-yield — named structural guard:** a test in
  `internal/sessionmodel/captain_bovr_test.go` directly inserts a
  `session_captains` row with `captain_type='joe'` (bypassing
  `Attach`), then exercises the transfer-begin API as an RBAC-authorized
  human and asserts the response is **immediate** `transfer_confirmed`
  with no `decision`-solicitation row created on the run. A second
  assertion: a Joe-captain "decline" code path is structurally absent
  (the state-machine code uses a `switch` on captain type whose `joe`
  arm has no decline branch; `go/ast` test enumerates the cases).
- **Compile-in (const-not-var) — named structural guard:** a unit test
  in `internal/seams/seams_guard_test.go` uses `go/ast` to parse
  `internal/seams/seams.go` and asserts every exported `Joe*Enabled`
  identifier is declared with `const`, has an untyped-bool literal value,
  and is not assigned to from elsewhere (the `go/ast` walk also asserts
  no `var Joe*Enabled` and no `os.Getenv("JOE_...")` or
  `cfg.Joe*Enabled` reference anywhere referencing those names).
- **R8 / Invariant 4 (no auto-resolve via confirm_close):** structurally
  enforced by Change 5's `regimeRepo.SetMode(ModeNormal, ...)` grep
  guard, which re-fires in this change's CI run. Stated here for
  traceability; no new guard added.

**Implementation risks (additional to the baseline).**

- **§B R-OVR limitation (global-blunt unlock) — explicit residual risk
  (§6):** cannot be mechanically enforced now; the doc-comment on
  `JoeCaptainTypeEnabled` is the only artifact and a future contributor
  enabling the seam without first landing scoped unlock is the failure
  mode. Recorded in §6 residual risks.

## 3. Invariant coverage map (PHASE-0-SESSION-MODEL.md §Invariants)

| Invariant | Primary change(s) | Automated enforcement |
|---|---|---|
| 1. One agentic loop; no suspended-but-running runs | Change 2; Change 9 | Partial unique index `idx_agent_runs_one_running_per_session` in `internal/store/migrations/010_run_model.up.sql` + its rejection test in `internal/runmodel/schema_test.go`; goroutine-identity test in `internal/coreagent/executor_durable_test.go` |
| 2. Idempotency key persisted before every world-mutating call | Changes 2, 7, 9 | Repo-API shape tests in `internal/runmodel/repository_test.go` (no result-without-prior-intent path); HTTP contract assertion in `internal/api/runs_test.go` (idempotency key required); call-order spy test in `internal/coreagent/executor_durable_test.go` |
| 3. Human-override-always-wins for `joe`-captain (R-OVR), compiled in | Change 12 | B-OVR force-yield test in `internal/sessionmodel/captain_bovr_test.go`; const-not-var AST guard in `internal/seams/seams_guard_test.go` |
| 4. Incident-mode entry may be automated; exit may not (R5) | Change 5 (re-fires in Change 12) | AST grep guard in `internal/api/regime_invariant_test.go` asserting `regimeRepo.SetMode(ModeNormal, ...)` exists in exactly one function (the human-resolve handler) |
| 5. §C gate is session-model-owned, upstream of unchanged authz | Changes 8, 10 | Import-closure guard in `internal/sessiongate/import_guard_test.go` (no `internal/rbac` dependency); signature-pin AST guard in same file (no `sourceID`/`tool`/`blast` params); ordering spy test in `internal/coreagent/executor_durable_test.go`; **non-configurable floor — behavioral permutation test (primary)** in `internal/coreagent/executor_durable_test.go` that enumerates every config permutation and asserts non-captain mutation in incident regime is refused in all of them; optional secondary AST signal in `internal/coreagent/executor_durable_guard_test.go` |
| 6. No client instantiates own LLM adapter; no SQLite-coupling above persistence | Changes 1, 2, 3 | Cross-driver migration test in `internal/sessionmodel/schema_test.go` (SQLite always; Postgres when env available — Postgres-portable-SQL rule + explicit residual risk in §6 when env absent). Loop-collapse half of this invariant is Phase 2 scope, not Phase 1, and is explicit residual risk in §6 for the Phase 1 horizon |

No row in this table depends on human or LLM review. Every entry resolves to
either a named guard with a file location, or the CLAUDE.md/skills baseline
+ a specific assertion, or an explicit residual risk in §6.

## 4. Cross-change coordination points

- **§6-C couples Changes 1/2/3 and Change 11.** Schema-level
  `ON DELETE CASCADE` is shipped by 1/2/3; the §6-C cascade migration test
  also ships with whichever of 1/2/3 introduces the table being asserted on
  (extending the same test file as each change lands). Change 11 ships **no
  migration**; that is the §6-C self-check.
- **§6-D couples to Change 6.** Reachability finding (existing signal or
  net-new) is in Change 6's scope; not deferrable.
- **§6-B couples to Change 5.** RBAC encoding finding is in Change 5's
  scope.
- **§6-A couples to Change 10.** Staging-soak is part of Change 10's
  acceptance.
- **Existing primitive `sessions` table (migration 001) is not touched** by
  any Phase 1 change. Post-Phase-1 webui migration to `agent_sessions` is
  out of scope. Enforced per change by the scope-isolation assertion noted
  in Change 1 and Change 9 (ordinary diff scope is sufficient because each
  change is implemented and committed in isolation).

## 5. Out of scope (do not let scope creep into Phase 1)

- Collapsing the two LLM adapters / two agentic loops — Phase 2.
- Webui migration from primitive `sessions` to `agent_sessions`.
- Retention policy enforcement (the seam is built; the policy is not — §5b-4).
- Postgres production deploy (separate dedicated session — §5b-6).
- Scoped per-agent unlock — prerequisite for live `joe`-captain, not v1.
- Structured multi-field `provide_data` payload variant — deferred.
- Joe-side timeouts beyond the **single sanctioned** captain-transfer-on-
  unreachable exception (§B). The waiting-state notification webhook
  mentioned in §D9 is recorded for a future remote-mode and is **not built
  in v1** (embedded-only deployment makes §D9 not-applicable — G3).
- Any change to RBAC's storage shape beyond seeding new policy entries
  (Change 5). RBAC's raw-`*sql.DB` access is acknowledged (§G5) and
  unchanged.

## 6. Pre-implementation resolutions (hard gates)

These four resolutions are **binding pre-implementation conditions** on
specific changes. Each is self-verified by the implementing session via the
named assertion and the named commit-message artifact. None depends on
external review.

### §6-A — Captain-gate-insertion (Change 10): staging-soak is acceptance

The Change 10 commit is not complete — and not committable to `main` — until
incident regime has been exercised end-to-end in staging with the gate
active and at least one captain mutation observed passing through
successfully.

**Enforced by:**

1. The end-to-end integration test described in Change 10's acceptance
   criteria (incident-declared → captain-attached → captain-session T2
   passes; non-captain T2 refused) ships with the change.
2. A staging-soak checklist item, self-certified in the Change 10 commit
   message in this exact form:

   ```
   STAGING-SOAK §6-A:
   - declared incident at <timestamp> as <principal>
   - attached captain <principal>
   - captain T2 mutation <tool> against <source_id>: PASSED
   - non-captain T2 mutation from session <id>: REFUSED with redirect
   - resolved incident at <timestamp>
   - regime returned to normal; both sessions mutate again
   ```

   The checklist is the artifact; the commit-message presence of all six
   lines is the self-check. If staging is not available, the change is not
   committable to `main`.

**Rationale:** a no-config-flag gate refusing previously-succeeding
mutations fails closed on a live incident — the highest-consequence failure
window. The staging soak is the only mechanism that exercises the gate
against real-world latency, real RBAC config, and real tooling before it
runs in prod.

### §6-B — Regime-RBAC encoding (Change 5): investigate IsAllowed first

The Change 5 commit's first task is a read-only investigation of how the
existing `rbac.PolicyEngine.IsAllowed` evaluates a sourceID that is
unmatched, sentinel (e.g. `"*"`), or unknown to the policy table. The
finding is recorded as a `file:line` citation in the commit message in this
exact form:

```
§6-B FINDING: IsAllowed unmatched-sourceID behavior verified at
internal/rbac/<file>.go:<line>:
  <one-sentence description of the behavior — allow/deny/skip>.
can_declare_incident encoding chosen: <sentinel-row | sourceID-NULL | new
policy_kind column | other>.
Encoding traceably follows from the verified behavior because: <one
sentence>.
```

The chosen encoding for `can_declare_incident` / `can_resolve_incident`
must traceably follow from that verified behavior. Do not redesign RBAC.

**Enforced by:**

1. The finding text above in the commit message.
2. The integration test in `internal/api/regime_test.go` described in
   Change 5's acceptance asserts both (a) the chosen encoding grants the
   capability and (b) it does **not** incidentally widen source authority
   for the same principal. The test exists and passes before commit.

**Rationale:** a claimed RBAC encoding that the existing engine evaluates
differently than assumed is the exact code-verification failure
`dev-standards` exists to catch. The cost of getting it wrong is silent
over-privilege on a security-critical capability.

### §6-C — Schema cascade (Changes 1, 2, 3): cascade ships with schema

Every foreign key referencing `agent_sessions(id)` or `agent_runs(id)`
introduced in Changes 1, 2, or 3 must be declared `ON DELETE CASCADE` in
the migration that introduces it, with `incident-expunge per §5b-5` named
as the rationale in the commit message in this exact form:

```
§6-C: FKs to agent_sessions(id) and agent_runs(id) in migration NNN are
ON DELETE CASCADE. Rationale: incident-expunge per PHASE-0-SESSION-MODEL.md
§5b-5.
```

A migration-level **two-level cascade** test ships **with the schema
commits** — not with Change 11. The test lives in
`internal/sessionmodel/cascade_schema_test.go` (introduced by Change 1,
extended by Changes 2 and 3) and proves both levels of expunge as a pure
schema property:

1. Insert incident session **I**.
2. Insert linked-investigation sessions **J1** and **J2** with
   `linked_incident_id = I` (the self-FK that makes two-level cascade a
   schema property — pinned in Change 1's schema definition).
3. Insert at least one child row per child table introduced by the change
   under test, under each of I, J1, and J2 (so child tables introduced by
   Change 2 — `agent_runs`, `run_steps`, `run_solicitations`,
   `run_world_handles`, `tool_idempotency_keys`, `action_ledger` — and by
   Change 3 — `findings`, `joe_warnings` — each have at least one row tied
   to each of the three sessions).
4. Execute a raw single SQL statement: `DELETE FROM agent_sessions WHERE
   id = <I>`. No handler, no application code, no transaction wrapper
   beyond the implicit single-statement transaction.
5. Assert: `agent_sessions` rows for I, J1, and J2 are all gone (J1 and J2
   cascade via `linked_incident_id ON DELETE CASCADE`; this is the
   second level).
6. Assert: in every child table introduced by Changes 2 and 3, zero rows
   remain whose session/run lineage traces to I, J1, or J2 (each child
   table's own `ON DELETE CASCADE` to its parent carries the cascade
   downward).

**Self-check:** Change 11 introduces a cascade *handler* but **no
migration**. The handler's only job is the API-layer routing of
`DELETE /api/v1/sessions/{id}` to the same single SQL DELETE the schema
cascade already handles — it is not where two-level expunge lives. If
Change 11 needs a migration, §6-C has failed and the schema commits must
be amended (as a follow-up correction) before Change 11 proceeds.

**Rationale:** the cascade behavior is structural — it belongs in the
schema, not in application code. Discovering missing `ON DELETE CASCADE`
during Change 11 means the handler is patching around a schema bug, and
the schema bug ships through every intervening change.

### §6-D — Captain-transfer reachability (Change 6): verify the signal exists

The Change 6 commit's first task is to verify whether a
reachability / liveness signal applicable to the **net-new captain concept**
exists in the codebase, or must be built. The captain concept is net-new
in Phase 1, so any reuse of an existing signal must be cited; assumed reuse
is forbidden.

The finding is recorded in the commit message in this exact form:

```
§6-D FINDING: captain reachability signal source verified.
[EXISTING] at <file:line>: <signal name and description>; binds to captain
because <one sentence>.
  -- OR --
[NET-NEW] built in this change as <mechanism>; lives in <file:line>;
binds to captain because <one sentence>; reuses no existing signal because
<one sentence>.
```

If the finding is `[NET-NEW]`, the implementation ships in Change 6 in the
same commit series — explicitly listed under Change 6's *Creates*, not as
a footnote, not as a deferred follow-up.

**Enforced by:**

1. The finding text above in the commit message.
2. The state-machine test in `internal/sessionmodel/captain_test.go`
   exercises the captain-unreachable transfer branch against the **actual**
   reachability mechanism cited or built. A mock-only assertion (no real
   binding to either an existing or net-new signal) does not satisfy this
   gate.

**Rationale:** a claimed reuse that is actually net-new is the exact
failure code-verification discipline exists to catch. If the signal is
net-new and not built explicitly here, the captain-transfer state machine
ships with a code path that has no real-world implementation behind it —
the worst kind of seam (looks wired, isn't).

### Residual risks (cannot be mechanically enforced)

- **Cross-driver schema portability (Invariant 6) when Postgres test env
  is absent.** The cross-driver migration test runs SQLite unconditionally
  and Postgres conditionally. When the Postgres half is skipped, only the
  Postgres-portable-SQL rule (no SQLite-only constructs) protects the
  invariant. Mitigation: a follow-up commit that lands a Postgres
  containerized test in CI removes this residual risk; that work is not in
  Phase 1's scope but is logged here.
- **§B R-OVR limitation (global-blunt unlock) at the moment a future
  contributor enables `JoeCaptainTypeEnabled`.** The doc-comment on the
  constant is the only artifact. A future contributor who flips the
  constant without first landing scoped per-agent unlock re-introduces the
  failure mode §B explicitly calls out. No structural guard prevents this
  in Phase 1; the prereq is recorded in PHASE-0-SESSION-MODEL.md "Open /
  deferred".
- **Phase 1 horizon for Invariant 6's "no client instantiates own LLM
  adapter" half.** Phase 1 does not collapse the loops; the CLI continues
  to instantiate its own adapter through the end of Phase 1. The invariant
  is satisfied by the end of Phase 2, not Phase 1. Recorded here so the
  Phase 1 completion criteria are not mis-read as satisfying it.
