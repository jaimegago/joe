# Docs reconciliation sweep — total read-only audit of `docs/` against the live tree
Status: in-progress

Slug: `docs-reconcile-sweep`. A read-only Phase-1 audit that inventoried, classified, and
claim-verified every file under `docs/` against the live code tree. This file is a **plan**,
not a decision and not a mutation: no documentation file was modified, moved, or deleted to
produce it. Mutating work is broken into scoped follow-up sessions in the Sequencing section
at the bottom — the operator runs those independently after reading this.

**Scope:** 89 content files (+ one git-tracked `.DS_Store` OS artifact) under `docs/`.
**Coverage:** comprehensive — every NARRATIVE/PUBLIC load-bearing claim was verified against
code, and every PM-SPINE file was evaluated under PM rules (not code-match). No file was
sampled-without-verification. See the Coverage note at the very bottom.

**Classes** (the reconciliation rule differs per class):
- **PUBLIC** — content under `docs/public/`. **None exist** (no `docs/public/` directory). Class empty.
- **NARRATIVE** — current-state prose describing how Joe works; must match the live tree.
- **PM-SPINE** — `DECISIONS.md`, `docs/decisions/`, `docs/backlog/` (incl. `done/`), `docs/investigations/`.
  Evaluated by PM rules: a record of deferred/future/superseded work is CORRECT, not stale.
- **OTHER** — historical records (executed phase plans, milestone logs, prompt artifacts) and
  active process docs that fit no other class.

**Ground truth used** (verified in-tree this session): binary `ActionRead`/`ActionMutate`
(`internal/safety/tier.go:21,26`, D-0020) — no T1/T2/T3; read posture `team_flat`(default)/`zoned`
(`internal/readposture/readposture.go:60-61`, D-0041/D-0043) governing transport reads only, not
`agent:core`; single `joe` binary (`cmd/joe/`); panic = `cluster_panic_state` DB row id=1
(`internal/store/panic_store.go:13-14`, D-0018), no `panic.state` file; denial precedence
floor > incident > RBAC (`internal/tools/executor.go:201-214`, D-0022); component/componentID
(D-0021); providers `claude` + `gemini` only (`internal/llmfactory/factory.go:27-29`); credential
provider abstraction landed (`internal/credential/provider.go`, D-0026); `POST /api/v1/unlock`
removed — recovery is CLI-only (`internal/api/panic.go:18-19`, asserted gone by `panic_test.go:142-147`).

---

## §0 — CLAIMED-BUT-UNBUILT (highest severity)

Mechanisms a doc asserts as *built* but which are **absent from the tree**. Each line gives the
claiming doc and the tree location proving absence. This is the failure class the arch-doc audit
caught; treat these as the top priority of the rewrite sessions.

| Claimed mechanism | Doc asserting it as built | Proof of absence in tree |
|---|---|---|
| Separate `joe-security` binary / `internal/security` package / `internal/securitysvc` | `JOE_SECURITY.md` | `ls internal/security` → absent; `cmd/` contains only `joe` |
| "Remote (separate process)" + "embedded" security modes, `security.mode` config | `JOE_SECURITY.md` | no such config key; single in-process binary |
| `writeProtectedTables`/`appendOnlyTables`/`CanWriteTable` compiled table guard | `JOE_SECURITY.md` (§ Risk Tiers) | `grep -r CanWriteTable internal/` → none; `invariants.go` guards paths/commands only. `security-in-layers.md §8.2` correctly states this guard was never the mechanism — the two docs contradict each other |
| Joe safety "circuit breaker" as a built control | `JOE_SECURITY.md`, `case-study-kiro-incident.md` | only grep hit is `internal/tools/core/envoy_tools.go` — an Envoy *managed-system* proxy feature, not a Joe safety control |
| T3 "dry-run + countdown" mutation control | `JOE_SECURITY.md` | no dry-run/countdown path in `internal/safety/`; action axis is binary Read/Mutate |
| `internal/rbac/auth/{ldap,entra,aws,gcp,oidc,mtls,…}` + `internal/rbac/authz/` package tree | `JOE_RBAC_IMPLEMENTATION.md` | real `internal/rbac/` is flat (identity/middleware/policy/principals/zones/repository); no `auth/`,`authz/` subpackages |
| 8 identity providers (LDAP, Entra, AWS IAM, GCP, mTLS, …) | `JOE_RBAC_IMPLEMENTATION.md` | `grep -ri ldap\|entra internal/` → none; only OIDC + service-account keys exist (`internal/auth/`) |
| Role/group permission model + Constraint engine (TimeWindow/IPWhitelist/MFA), `RBACToolMiddleware` | `JOE_RBAC_IMPLEMENTATION.md` | live RBAC is zone-scoped, no roles/groups; no constraint engine in `internal/rbac/zones.go` |
| `~/.joe/panic.state` file written on panic / read on restart | `operations.md` (§ panic) | state is the `cluster_panic_state` DB row (`internal/store/panic_store.go:13`); no file (D-0018) |
| OpenTelemetry **logs** pipeline as a current capability | `observability.md` (header) | `internal/observability/otel.go` wires only traces + metrics providers; no log exporter |
| Blast-radius detector + `environment_operations`/`circuit_breaker` policy blocks as live controls | `case-study-kiro-incident.md` | not present in `internal/safety/policy.go`; not part of the binary Read/Mutate + write-floor model |

**Explicitly NOT claimed-but-unbuilt (verified built — do not flag):**
- `DESIGN-SESSIONS-VIEW.md` P0 (master-title self-join + `linked_incident_title` on the list
  projection + `idx_agent_sessions_linked_incident`) — an auditor flagged this as unbuilt; **direct
  verification refuted that**: the self-join exists at `internal/sessionmodel/repository.go:525,564,604`
  and the index at `internal/store/migrations/025_session_schema_rewrite.up.sql:94`. Only the doc's
  line-number citations drifted (light refresh, §NARRATIVE below), not the deliverable.
- `security-in-layers.md §3.7` and `security-architecture-direction.md` correctly label circuit-breaker /
  env-block / credential-isolation as **designed-but-not-yet-built** — that honest framing is accurate,
  not a claimed-but-unbuilt defect.

---

## §1 — NARRATIVE (must match the live tree)

### Accurate / near-accurate (light, surgical fixes)

| Path | Verdict | Justification / claims to fix |
|---|---|---|
| `joe-architecture.md` | REWRITE (light) | Gold-standard post arch-doc-staleness rewrite; binary axis, single binary, panic DB row, read posture, providers all VERIFIED. One fix: `joe review owner/repo#123` CLI example (line ~226) — **no `review` subcommand exists** (`cmd/joe/main.go` switch has no `review` case; review is webhook/API-only). |
| `integrations.md` | ACCURATE | All 8 MCP tool names, `joe mcp`/`joe slack` env + Socket Mode, slash commands VERIFIED against `internal/mcp/tools.go`, `internal/slack/`. Cleanest file in the set. |
| `configuration.md` | REWRITE (light) | One fix: provider comment lists `claude \| gemini \| ollama` (line ~20) — **`ollama` is not a supported provider** (only `claude`/`gemini` in `validation.go`; `ollama` appears in tests solely as a *rejected* value). Drop it. Everything else VERIFIED. |
| `observability.md` | REWRITE (light) | Metric tables + package layout VERIFIED. Fix: header asserts OTel **logs** pipeline (claimed-but-unbuilt, §0); trim stale "Joe CLI" framing; optionally note the dual instrumentation path (`llm_middleware.go` vs `llm/instrumented.go` use different metric names). |
| `joe-skills-design.md` | REWRITE (light) | Registry/CLI/hot-reload/quarantine/API all VERIFIED against `internal/skills/`. Fix two stale lines: "Joe's LLM (currently Gemini)" → claude+gemini; "T3 mutation notification / tool tiers" → binary Read/Mutate (D-0020). |
| `security-in-layers.md` | REWRITE (light) | The authoritative, accurate security doc (executor gate order, denial precedence, read posture, panic DB row, "no `CanWriteTable`" all VERIFIED). One concrete fix: the `POST /api/v1/unlock` curl example (§7.4) and endpoint-table row (§7.5) — **that HTTP endpoint was removed**; unlock is CLI-only (`joe unlock`). Relabel to CLI-only. |
| `break-glass-access.md` | ACCURATE | Service-account `svc:<name>` model, OIDC admin bootstrap, break-glass audit row, dedup window all VERIFIED against `internal/auth/` + `internal/config/`. Best-aligned file. |
| `security-findings-punchlist.md` | ACCURATE | Open-items tracker; line citations (`tier.go:115,117,229-233`) check out. A2/A3 still open in tree. Reports gaps, asserts no mechanisms. |
| `security-architecture-direction.md` | ACCURATE | Self-labeled forward design-intent ("decided, NOT yet verified"); nothing claims to be live. Optional: note `internal/credential/` landed (D-0026) since its §9 "deferred" framing. |

### Stale current-state prose (substantive rewrites)

| Path | Verdict | Claims to fix → verified-correct state |
|---|---|---|
| `JOE_SECURITY.md` | REWRITE (major) or DELETE | Nearly every load-bearing claim is stale or describes unbuilt subsystems (see §0: `joe-security` binary, remote/embedded modes, table guard, circuit breaker, T3 dry-run). Plus three-tier T1/T2/T3 → binary Read/Mutate; `source`/`sources` table → component/`components` (D-0021). `security-in-layers.md` already supersedes it accurately. CLAUDE.md's "Reference Documents" pointer to it is misleading. Recommend: DELETE (fold any unique-and-true content into `security-in-layers.md`), or full REWRITE down to the as-built model. |
| `JOE_RBAC_IMPLEMENTATION.md` | REWRITE (major) or DELETE | A pre-implementation RFC the as-built RBAC diverged from fundamentally: two-binary `joecored`, `internal/rbac/auth\|authz` tree, 8 identity providers, role/group model, constraint engine (all §0 claimed-but-unbuilt). Live RBAC is zone-scoped, flat package, OIDC + svc-keys only. CLAUDE.md cites it as "RBAC middleware spec" — that pointer is now wrong. Recommend DELETE or relabel-as-historical-design + rewrite the as-built spec into `security-in-layers.md §8`. |
| `operations.md` | REWRITE (major) | Most-drifted ops doc. Fixes: (1) "Action safety tiers" T1/T2/T3 Observe/Record/Act → binary Read/Mutate (D-0020); (2) panic "writes `~/.joe/panic.state`" → `cluster_panic_state` DB row (D-0018, §0); (3) `/api/v1/admin/source-zones` + `source_id` body → `/component-zones` + `componentID` (D-0021); (4) `joe incident declare --reason …` → requires `--session <id>` and hits `/api/v1/regime/declare` (`cmd/joe/incident.go`). Default-zones table + panic triggers are sound. |
| `web-ui.md` | REWRITE (major) | Reads as a Phase-12 implementation brief the build diverged from. Fixes: (1) auth `POST /api/v1/auth/login` bearer-from-localStorage → OIDC `GET /auth/login` + server-side session cookie (`internal/auth/handlers.go`); (2) "Source"/`/api/v1/sources`/`SourceZoneAssign` → component/`/api/v1/components`/`/component-zones` (D-0021); (3) 4-action `SecurityZone.actions` Read/Query/Mutate/Delete → binary Read/Mutate; (4) file map (`api/sources.ts`, `SourcesPage.tsx`, `useSources`, `useAuth`) → real `components.ts`/`ComponentsPage.tsx`/`useComponents`/`useCurrentUser` (+ missing `adminSessions.ts`, `regime.ts`, `skills.ts`, `panic.ts`). The `POST /api/v1/tasks/stream` SSE route is VERIFIED. |
| `testing-strategy.md` | REWRITE (or DELETE) | Code samples would not compile against the live tree: references nonexistent `internal/useragent` (loop is `internal/agentloop/`), `internal/tools/local` (real: `tools/core` + `tools/shared`), `joecored` binary (single `joe`), wrong constructors (`api.New()`/`NewWithStore` → `api.New(services)`), two-binary harness. Real `test/{mocks,integration,e2e}/` layout differs. Recommend DELETE in favor of the real harness, or full rewrite against as-built `test/`. CLAUDE.md "Build/Test/Lint" is the current authority. |

### Normative/design docs that shipped — annotate, don't rewrite

| Path | Verdict | Justification |
|---|---|---|
| `DESIGN-CHAT-SESSIONS.md` | ARCHIVE-ANNOTATE (keep §12+ normative) | §12 ontology + §12.4–12.8 storage/authz/admin/archive + §13 ledger all VERIFIED against migration 025, `internal/sessionauthz/`, `internal/sessionarchive/`, `internal/api/webui.go`. Defect is structural: §1–§11 (Phases 1–5, e.g. the dropped `visibility` column) are reversed-by-§12 yet a reader hits 350 lines of them first. Add a header banner demoting §1–§11 to "historical rationale, superseded by §12"; keep §12–§13 as the normative as-built spec. No content rewrite. |
| `joe-identity-design.md` | ARCHIVE-ANNOTATE | Every Phase A–G mechanism shipped (guarded accessor `internal/access/`, OIDC+sessions, `svc:` keys, loopback removal `tasks.go:275-280`, audit triggers migration 015). But the header still reads "design, pre-implementation" and its inline `file:line` refs point at the pre-build tree. Add an "as-built: Phases A–G landed" status banner; do not present its stale code citations as current. Reasoning content is valuable — annotate, don't rewrite. |
| `DESIGN-SESSIONS-VIEW.md` | REWRITE (light — citations only) | Storage predicate, P2 client-side filter/sort, list ordering all VERIFIED. **P0 IS built** (refuting the auditor's unbuilt flag — see §0). Only defect: every `file:line` citation drifted ~15–70 lines post-025-rewrite (`handleListSessions` :339→:353, `handleGetSession` :418→:436, `sessionToWebUI` :281→:292, etc.). Refresh the line numbers; correct the status header to reflect P0-landed. |

---

## §2 — OTHER (historical records + active process docs)

### Historical records — CORRECT-IN-PLACE (leave; a record of past work is not "stale")

| Path | Verdict | Note |
|---|---|---|
| `PHASE-0-SESSION-MODEL.md` | CORRECT-IN-PLACE | "Status: CLOSED" post-refactor design record; T1/T2/T3 + "source" are point-in-time. |
| `PHASE-1-DECOMPOSITION.md` | CORRECT-IN-PLACE | "Status: PLANNING" change plan; `joecored`/tier language is historical. |
| `PHASE-2-IMPLEMENTATION-NOTES.md` | CORRECT-IN-PLACE | Planning record; work landed (loop → `internal/agentloop`), superseded by PLAN-OF-RECORD-RECONCILED. |
| `PLAN-OF-RECORD-RECONCILED.md` | CORRECT-IN-PLACE | Phase-2 COMPLETE record; completion claims verify in-tree. |
| `may_16th_refactor_plan.txt` | CORRECT-IN-PLACE | Origin refactor plan-of-record; back-referenced by the reconciled plan. |
| `milestones-completed.md` | CORRECT-IN-PLACE | "Historical Reference" log; already self-annotates drift in-place (the right pattern). |
| `joe-identity-phase-plan.md` | CORRECT-IN-PLACE | A–E merged, F/G open; still-live tracker, content accurate. |
| `joe-identity-phase-A-prompt.md` | CORRECT-IN-PLACE | Verbatim "(executed)" prompt artifact, result in D-0004. |
| `joe-identity-phase-B-prompt.md` | CORRECT-IN-PLACE | "(executed)", D-0005. |
| `joe-identity-phase-D-prompt.md` | CORRECT-IN-PLACE | "(executed)", D-0007. |
| `joe-identity-phase-E-prompt.md` | CORRECT-IN-PLACE | "(executed)", D-0008. |
| `prompts/safety-reasoning-articulation.prompt.md` | CORRECT-IN-PLACE | Dated executed prompt; edit target `TaskSystem` exists in `internal/prompts/prompts.go`. (Aside: the OASIS eval *harness* was never landed in-repo — only scenario-ID strings remain — but this prompt's own target exists.) |

### Current-state OTHER prose

| Path | Verdict | Note |
|---|---|---|
| `case-study-kiro-incident.md` | ARCHIVE-ANNOTATE | The one OTHER file written as **current-state** prose asserting now-false mechanisms: T1/T2/T3 Observe/Record/Act (→ binary Read/Mutate, D-0020); `joecored` process name; `~/.joe/safety-policy.yaml` path; blast-radius/circuit-breaker as live controls (§0). Portfolio value is real — add an "as-of / superseded by D-0020 binary model" banner or refresh those sections. Not DELETE. |
| `pm-convention.md` | ACCURATE | Active process doc; all referenced paths exist; D-0031/D-0032 present. Convention holds. |
| `claude_joe_project_instructions.md` | ACCURATE | Paste-source for claude.ai project instructions; consistent with pm-convention; referenced files exist. |
| `.DS_Store` (`docs/.DS_Store`) | DELETE (untrack) | Not a documentation file — a git-tracked macOS artifact (`git ls-files` confirms tracked, not gitignored). Candidate to `git rm --cached` + add to `.gitignore`. Bundle with a cleanup session, not a doc rewrite. |

---

## §3 — PM-SPINE (evaluated by PM rules, not code-match)

No historical decision is proposed for edit-to-match-code; no pending backlog item is proposed for deletion.

### Decisions

| Path | Verdict | Note |
|---|---|---|
| `DECISIONS.md` | CORRECT-IN-PLACE (no edits) | 44 entries, D-0001→D-0044, contiguous, unique, append-only newest-on-top. Every supersession link resolves and is correctly directed (D-0044→D-0032/D-0035, D-0043 corrects D-0041, D-0040 corrects D-0039, D-0033 supersedes D-0003, etc.). All six high-churn chains (D-0020, D-0041/D-0043, D-0021, D-0018, D-0022, D-0026) internally consistent. No duplicate numbers, no gaps, no unlinked contradiction. |
| `decisions/D-0026-credential-provider-abstraction.md` | CORRECT-IN-PLACE | Standalone ADR detail-of-record; `DECISIONS.md` D-0026 (lines 1051-1086) correctly **references** it rather than duplicating (ADR-summary pattern, not drift). Resolve/Probe/Describe interface matches `internal/credential/provider.go`. Status reads "design" while the tree shows it shipped — expected PM record behavior, not a defect. Optional enhancement only: a dated "implemented" addendum line. |

### Investigations (22 files)

20 CORRECT-IN-PLACE (valid point-in-time findings; code drift alone is not flagged). The two flagged
are reversed by **D-0027** (refuse-to-start makes engine-nil unreachable) — ARCHIVE-ANNOTATE, add a
"superseded by D-0027" note, no deletion, no edits-to-match-code:

| Path | Verdict | Note |
|---|---|---|
| `rbac-disabled-bootstrap-claim.md` | ARCHIVE-ANNOTATE | Conclusion ("`rbac_disabled` is an indefinitely-runnable standing allow-all posture") explicitly reversed by D-0027. |
| `identity-wiring-and-runtime-config.md` | ARCHIVE-ANNOTATE | Load-bearing verdict ("engine-nil is a reachable runtime state") reversed by D-0027. |
| `accessor-promotion-state-axis.md`, `adapter-credential-refresh-tolerance.md`, `agentic-path-rbac-read-enforcement.md`, `ambient-credential-dispatch-seam.md`, `authz-tool-execution-feasibility.md`, `backlog-triage.md`, `component-credential-registration-surface.md`, `coreagent-refresh-governance-autopromote.md`, `coreagent-refresh-governance-mint-thread.md`, `coreagent-refresh-governance.md`, `credential-design-assumptions-check.md`, `credential-handling-current-state.md`, `direct-http-mutation-surface.md`, `edge-type-count-arbitration.md`, `incident-captain-flow.md`, `jpk-migration-triage.md`, `learn-from-sessions-current-state.md`, `llm-egress-chokepoint-and-provenance-feasibility.md`, `managed-system-egress-map.md`, `operational-modes-ui-status.md` | CORRECT-IN-PLACE | Valid point-in-time records; not superseded by any decision. (Note: `operational-modes-ui-status.md` Axis-1 "no read-only `operational_mode` flag" is NOT superseded by D-0041 — the read *posture* is an orthogonal read-sharing axis.) |

### Backlog — active (28 items) + INDEX + done/

All 28 active items are CORRECT-IN-PLACE: each describes pending/deferred unbuilt work, none is fully
built (so none MOVE-TO-DONE) and none references a vanished mechanism (none orphaned). Spot-verified
"is it built?" probes: credential-provider items (aws/azure/datastore-uri/registry-auth-pair) — only
`KindStatic`/`KindKubeconfigExec` wired in `internal/credential/`, the specific provider shapes are
unbuilt; `health-readiness-surface` — no `/livez`,`/readyz` in `internal/api/`; `edge-type-literal-consolidation`
— `graph_edges.relation` still `TEXT NOT NULL` with no CHECK; `read-posture-latch` — mechanism landed
(D-0041/D-0043) but its four deferred work-streams (hide zoned UI, docs reframe, v2 flip UI, principal-type
decision) are unbuilt, so in-progress not done.

| Path | Verdict |
|---|---|
| `adapter-dispatch-consolidation.md`, `aws-credential-provider.md`, `azure-adapter-connect-skeleton.md`, `azure-credential-provider-and-connect.md`, `build-version-instrumentation.md`, `captain-write-consolidation.md`, `cross-incident-relink.md`, `datastore-uri-credential-provider.md`, `denial-feedback-popup.md`, `edge-type-literal-consolidation.md`, `full-mode-rbac-track.md`, `governed-connectivity-check-surface.md`, `health-readiness-surface.md`, `incident-view-filter-to-mine.md`, `launch-positioning-and-lgt-decoupling.md`, `launch-ui-polish.md`, `learn-from-sessions-fate.md`, `oasis-relationship.md`, `posture-endpoint-grants-signal.md`, `promotion-requirements-single-source.md`, `rbac-v2.md`, `read-posture-latch.md`, `registry-auth-pair-credential-provider.md`, `session-content-search.md`, `session-doc-debt.md`, `sessions-view-paging.md`, `tilde-expansion-helper-unification.md`, `tool-class-break-tests.md` | CORRECT-IN-PLACE (pending) |
| `backlog/INDEX.md` | REGENERATE (mechanical) — currently accurate for the 28 items, but **this sweep adds `docs-reconcile-sweep.md`**, so INDEX must be regenerated to list the 29th row. (Done in this session as an acceptance criterion.) |
| `backlog/done/README.md`, `backlog/done/arch-doc-staleness.md`, `backlog/done/credential-reject-single-source.md`, `backlog/done/post-joefile-cleanup.md` | CORRECT-IN-PLACE | Genuinely done (cite D-0030/D-0021/arch rewrite); correctly filed. |

---

## §4 — Cross-cutting: duplication / drift surfaces

- **Security model maintained in three places that disagree:** `security-in-layers.md` (accurate),
  `JOE_SECURITY.md` (stale, asserts unbuilt table-guard + second binary), `JOE_RBAC_IMPLEMENTATION.md`
  (stale role-based RFC). They state *opposite* things about the table guard. Consolidate to
  `security-in-layers.md`; retire/relabel the other two. CLAUDE.md "Reference Documents" points at the
  two stale ones — update those pointers when they change.
- **Config block duplicated:** `joe-architecture.md` restates `configuration.md`; keep detail in
  `configuration.md`, thin the architecture copy.
- **Action-safety model duplicated:** `joe-architecture.md` (binary, correct) vs `operations.md`
  (three-tier, stale) — reconcile operations.md to the binary axis or point it at `security-in-layers.md`.
- **Auth model duplicated:** `joe-identity-design.md` (correct authority) vs `web-ui.md` (wrong
  credential-POST model) — fix web-ui.md to match.
- **Session storage model** duplicated across `DESIGN-CHAT-SESSIONS.md §12.4` and `DESIGN-SESSIONS-VIEW.md`
  (self-declared siblings — acceptable, both cite migration 025).
- **No doc whose entire subject vanished** → no whole-file DELETE-for-obsolescence candidate among
  documentation (the `JOE_SECURITY.md`/`testing-strategy.md` DELETE recommendations are
  superseded-by-a-better-doc, not subject-vanished). The only pure-artifact delete is `docs/.DS_Store`.

---

## §5 — Proposed sequencing (scoped follow-up sessions)

Each becomes its own slugged build the operator runs and reviews independently. Deletes are **not**
bundled with rewrites. Ordering favors retiring/​consolidating the stale security docs first (they are
the highest-risk and the source of the §0 claimed-but-unbuilt findings), then the substantive rewrites,
then light fixes, then annotations, then mechanical cleanups.

1. **`docs-reconcile-security-consolidation`** — Resolve the three-way security-doc conflict (§4).
   Decide DELETE-vs-relabel for `JOE_SECURITY.md` and `JOE_RBAC_IMPLEMENTATION.md`, fold any unique-true
   content into `security-in-layers.md`, and update the CLAUDE.md "Reference Documents" pointers. Clears
   the bulk of §0. (Touches only these three docs + CLAUDE.md pointers; no code.)

2. **`docs-reconcile-narrative-ops`** — ✅ COMPLETE. Major rewrites of the stale current-state NARRATIVE docs:
   `operations.md` (tiers/panic-file/source-zones/incident-signature), `web-ui.md` (auth/component/action-axis/file-map).
   One coherent "make current-state ops + UI docs match the tree" unit, split across two sessions; both have now landed.
   - `operations.md` — ✅ DONE (session `docs-reconcile-narrative-ops-01`): targeted-major rewrite — five
     corrections applied (T1/T2/T3 → binary Read/Mutate + pointer to `security-in-layers.md`; panic
     `~/.joe/panic.state` → `cluster_panic_state` DB row + host-CLI `joe unlock` recovery, no HTTP unlock
     endpoint; `/api/v1/admin/source-zones`+`source_id` → `/component-zones`+`component_id`; `joe incident
     declare` now shows required `--session <id>`). Default-zones table + panic-triggers preserved untouched.
   - `web-ui.md` — ✅ DONE (session `docs-reconcile-narrative-ops-02`): targeted-major rewrite — three in-place
     corrections + a wholesale file-map rebuild. (1) auth model: `POST /api/v1/auth/login` + localStorage-bearer
     → OIDC auth-code+PKCE with a server-side HttpOnly session cookie (`GET /api/v1/auth/login` →
     `/auth/callback` → `/auth/logout`, current-user from `/api/v1/me`; break-glass bearer noted as
     `sessionStorage`-only, never localStorage); (2) component rename (D-0021): `Source`/`/api/v1/sources`/
     `SourceZoneAssignment`/`source-zones` → `Component`/`/api/v1/components`/`ComponentZoneAssignment`/
     `component-zones`+`component_id`, across types, routes, the Admin/Components pages, and the
     implementation-order list; (3) action axis: four-action `SecurityZone.actions` Read/Query/Mutate/Delete →
     binary `allowed_actions: ('Read'|'Mutate')[]` (D-0020), incl. the zones-tab table; (4) the frontend file
     map was rebuilt from the live `ui/src` tree (real `api/components.ts`/`pages/ComponentsPage.tsx`/
     `hooks/useComponents.ts`/`useCurrentUser.ts` + previously-omitted `adminSessions.ts`/`regime.ts`/`skills.ts`/
     `panic.ts`/`auth/` etc.). The `POST /api/v1/tasks/stream` SSE section was confirmed correct and left
     untouched. **Item 2 is now COMPLETE.**

3. **`docs-reconcile-narrative-light`** — ✅ DONE (session `docs-reconcile-narrative-light`). Surgical
   single-claim fixes that didn't need a full rewrite; all six applied, none skipped. Outcomes:
   - `joe-architecture.md` — **applied**: removed the `joe review owner/repo#123` CLI example (no `review`
     case in `cmd/joe/main.go`), reframed the Review Agent trigger as webhook/API-only.
   - `configuration.md` — **applied**: dropped `ollama` from the provider comment (validation accepts only
     `claude`/`gemini`), leaving `claude | gemini`.
   - `observability.md` — **applied**: removed the logs claim from the header sentence (OTel wiring has only
     `TracerProvider`+`MeterProvider`, no log exporter — `internal/observability/otel.go`), traces+metrics
     only; trimmed the stale `Joe CLI` ASCII-diagram box to `joe`. Line-9 "Why OpenTelemetry?" bullet left
     as-is (it describes the OTel framework's signal coverage, not a Joe pipeline claim).
   - `joe-skills-design.md` — **applied**: "currently Gemini" → "Claude or Gemini" (both validated
     providers); "T3 mutation notification" → "Mutate-action notification" (D-0020 binary Read/Mutate).
   - `security-in-layers.md` — **applied**: removed the §7.4 `curl POST /api/v1/unlock` example (route is
     gone — `internal/api/panic_test.go` asserts its absence; `joe unlock` is the recovery path) and
     re-annotated the §7.5 endpoint-table row as CLI-only with no HTTP endpoint.
   - `DESIGN-SESSIONS-VIEW.md` — **applied** (citation refresh): re-derived every drifted `internal/api/webui.go`
     and `internal/sessionmodel/repository.go` `file:line` against the live tree (handlers shifted +14, the
     two list queries shifted; migration-025 citations unchanged). The P0 **status header was already
     correct** ("P0–P2 landed") — the audit lead had already flipped it from the false-positive unbuilt flag;
     P0 mechanisms re-confirmed present (master-title self-join `repository.go:525`/`:528`, linked-incident
     index `025_session_schema_rewrite.up.sql:94`), so no header edit was needed. One residual nuance: the
     §6.C struct-comment citation now points at `webui.go:227`, whose live comment text was itself rewritten
     by P0 (now documents the fixed both-surfaces state); only the line number was refreshed, the historical
     past-tense narrative quote was left as-is per the line-number-only scope.

   No decision-log entry (executes the audited plan, no new decision).

4. **`docs-reconcile-testing-strategy`** — Decide DELETE vs rewrite-against-as-built for `testing-strategy.md`
   (the won't-compile doc). Isolated because it's a delete-or-rebuild judgment call, kept off the other rewrites.

5. **`docs-reconcile-historical-annotations`** — ✅ DONE (session `docs-reconcile-historical-annotations`).
   Annotation-only; all five banners landed, each a single top-of-file blockquote with the body left
   byte-for-byte unchanged (2 insertions / 0 deletions per file): `DESIGN-CHAT-SESSIONS.md`
   (**SUPERSEDED**, demotes §1–§11, points to §12+), `joe-identity-design.md` (**AS-BUILT**),
   `case-study-kiro-incident.md` (**SUPERSEDED**, pre-D-0020), and the two investigations
   (`rbac-disabled-bootstrap-claim.md`, `identity-wiring-and-runtime-config.md`, both **SUPERSEDED** by
   D-0027). No file skipped. Phase-1 placement note: the two investigation files carry no markdown `#`
   H1 — each is a single fenced code block whose first line is the `INVESTIGATION:` title — so their
   banner sits at the very top of the file (above the opening fence) to render as a blockquote and keep
   the fenced body unchanged. PM-SPINE investigation findings left untouched. No decision-log entry
   (executes the audited plan, no new decision).
   - **Known defect pending (found in session `docs-reconcile-narrative-ops-02`):** the
     `case-study-kiro-incident.md` banner added by this session contains one wrong clause — it asserts there is
     no `~/.joe/safety-policy.yaml`, but that file is real (`internal/safety/policy.go` declares
     `PolicyFileName = "safety-policy.yaml"` and documents the `~/.joe/safety-policy.yaml` default location).
     Only the *tier-based content* that clause referenced is stale (collapsed to binary Read/Mutate by D-0020);
     the file path itself is correct. This banner clause is to be corrected in session
     `docs-reconcile-security-consolidation`.

6. **`docs-reconcile-artifact-cleanup`** — ✅ DONE (session `docs-reconcile-artifact-cleanup`). Pure
   cleanup, no doc content. Outcome differed from the audit's premise: a fresh `git ls-files` scan over
   the full OS/editor artifact set found **`docs/.DS_Store` was never tracked** (it's covered by the
   existing `.DS_Store` `.gitignore` rule and has no git history) — the §0/§2/§4 "confirmed tracked"
   note was a false positive. The **only** tracked artifact was **`.vscode/settings.json`**, which holds
   hand-authored shared config (Go build tags + a ~200-term curated `cSpell.words` dictionary), surfaced
   per the Phase-1.3 review gate; the operator chose to untrack it. Actions taken: `git rm --cached
   .vscode/settings.json` (left on disk; `.vscode/` was already ignored). `.gitignore` extended with the
   missing artifact patterns under OS/editor headers: `._*` (AppleDouble) under macOS; a new Windows
   block (`Thumbs.db`, `Desktop.ini`); and `*.swn`, `*~`, `*.sublime-workspace`, `*.sublime-project`
   under the IDE/editor header. No duplicate lines added; no decision-log entry (mechanical hygiene).

INDEX.md regeneration is handled per-session as items move; no standalone session needed.

---

### Coverage note
Comprehensive, not sampled. All 89 content files were classified and assessed; every NARRATIVE/OTHER
current-state load-bearing claim was verified against code (with five consequential auditor findings
re-verified directly by the lead, one of which — `DESIGN-SESSIONS-VIEW.md` P0 — was corrected from a
false-positive unbuilt flag to built). `DECISIONS.md` (4281 lines) was assessed for structural integrity
(numbering, supersession chains) rather than line-by-line prose, which is the correct PM-SPINE treatment
for an append-only log — not a coverage gap. `docs/.DS_Store` is a non-doc artifact, noted for untracking.
