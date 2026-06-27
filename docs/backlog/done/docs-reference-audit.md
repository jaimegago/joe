# Reference-documentation audit — drift against the live codebase
Status: done

## Summary

Audited **13** reference documents under `docs/reference/` against the live tree
(re-derived from code, not from memory or `DECISIONS.md`). The original audit
classified **VERIFIED = 287**, **MISALIGNED = 67**, **UNVERIFIABLE = 40**.

**Campaign progress: CLOSED.** Open MISALIGNED entries remaining: **0** (recomputed
by summing the per-file counts of the sections below — every section is now marked
RESOLVED or RETIRED). Resolved: `operational-modes-ui-status.md` (17, in
`docs-reference-audit-01`); `security-in-layers.md` (6, in `docs-reference-audit-02`);
and the final slice `docs-reference-audit-05` (13 across `joe-architecture.md` (3),
`DESIGN-SESSIONS-VIEW.md` (3), `joe-identity-design.md` (2), `joe-skills-design.md`
(2), `observability.md` (2), and `learn-from-sessions-current-state.md` (1)). Retired
(doc deleted, survivors absorbed): `direct-http-mutation-surface.md` (11) and
`managed-system-egress-map.md` (11), both in `docs-reference-audit-03`;
`accessor-promotion-state-axis.md` (9, in `docs-reference-audit-04`). Total actioned:
17 + 6 + 13 resolved + 31 retired = **67 MISALIGNED**, matching the original count.

This is an **audit only** — no doc or code was changed. Each MISALIGNED claim below
carries the doc location, the authoritative code location, a one-line discrepancy,
and a fix **classification** (one of: `rename`, `stale default`, `removed feature`,
`renamed path`, `schema change`, `other`). No fixes are proposed here; a later
sequenced campaign actions them one slice at a time.

**Shape of the drift.** Three high-impact structural removals account for most of
the substantive findings, each independently confirmed against the live tree:

1. **The `internal/tools/local/` tree was removed** (read_file / write_file /
   run_command / git tools / ask_user). `internal/tools/` now holds only `core/`
   and `shared/`. `security-in-layers.md` documents this tree as a live mutation
   surface.
2. **The direct-HTTP managed-system mutation surface was removed** (commit
   `540f5e5`): `internal/api/vcs.go`, `internal/api/gitops.go`,
   `internal/client/vcs.go`, and the `/knowledge/proposals/{id}/publish` route no
   longer exist. `direct-http-mutation-surface.md` and `managed-system-egress-map.md`
   are built around this surface.
3. **The file-based panic/safe-mode mechanism was replaced by the boot-resolved
   write floor** (D-0018): `internal/safety/safemode.go` and
   `internal/safety/panic_state.go` are gone, `panic.state` is deleted, and a new
   observation-mode posture + `GET /api/v1/mutate-status` endpoint exists.
   `operational-modes-ui-status.md` describes the deleted mechanism and denies the
   posture that now exists.

Beyond those, the dated investigation docs (`accessor-promotion-state-axis.md`,
`managed-system-egress-map.md`, `direct-http-mutation-surface.md`) carry extensive
**line-citation drift** — `file:line` references that no longer point at the cited
code after package growth. These are genuine misalignments but lower-severity than
the structural removals; they are itemized per file below.

**Clean docs (no MISALIGNED claims):** `DESIGN-CHAT-SESSIONS.md` (normative §12
as-built verified), `security-architecture-direction.md` (self-declared design
intent; forward-looking claims classified UNVERIFIABLE, no present-tense
contradictions).

**Drift-vector spot-checks (verified clean across the set):** the source→component
rename is correctly applied in the reference docs (no stale registered-system
`source`/`sourceID` survives); the safety policy file name is `safety-policy.yaml`
(`internal/safety/policy.go:14`) and the skills policy `skills-policy.yaml`
(`internal/skills/policy.go:18`); the binary Read/Mutate axis
(`internal/safety/tier.go`), the boot-resolved/runtime-immutable write floor
(`internal/safety/floor.go`), and the `team_flat` read-posture launch default
(`internal/readposture/readposture.go`) all match where docs reference them.

---

## docs/reference/operational-modes-ui-status.md  — MISALIGNED: 17 — RESOLVED in docs-reference-audit-01

This doc predated the D-0018 write-floor refactor. Its central §1 thesis (that no
global read-only posture exists) and its entire panic/safe-mode mechanism described
deleted code; most cited UI line numbers had also drifted. The doc was rewritten in
`docs-reference-audit-01` against the live tree — observation mode documented as the
boot-resolved write-floor read-only posture, panic state placed in the
`cluster_panic_state` DB row, the typed `WriteFloorError` and four-code write-failure
set (pointed at `internal/api/constants.go` per D-0032), the captain prefix corrected
to `/api/v1/sessions/{id}/captain/*`, the three AppShell banners inventoried, and the
T1/T2/T3 vocabulary replaced by the binary Read/Mutate axis (D-0020). All 17 entries
below are addressed; they are retained for traceability.

1. DOC `:25,109-112,116,120,189,364-368` — panic state persisted to a
   `<joeDir>/panic.state` file via `internal/safety/panic_state.go`.
   CODE `internal/safety/panic.go:22-43`; `internal/safety/panic_state.go` does not
   exist; `internal/safety/floor_guard_test.go:124-181` forbids reintroducing it;
   git `8da88d6` deleted `panic.state`.
   DISCREPANCY: panic state lives only in the `cluster_panic_state` DB row, not a file.
   CLASS: removed feature

2. DOC `:108` — in-memory `safety.safeModeActive atomic.Bool` at
   `internal/safety/safemode.go:18`.
   CODE: `internal/safety/safemode.go` does not exist; `floor_guard_test.go:18-40`
   guards that `safeModeActive`/`ActivateSafeMode`/`DeactivateSafeMode`/
   `IsSafeModeActive`/`ErrSafeModeActive` are deleted.
   DISCREPANCY: safe-mode state is now the boot-resolved `WriteFloor`, not an atomic.
   CLASS: removed feature

3. DOC `:116` — startup calls `safety.ActivateSafeMode()` (`cmd/joe/server.go:413-424`).
   CODE `cmd/joe/server.go:411-430` boots via `safety.ResolveWriteFloor(...)` and
   seals the floor; no `ActivateSafeMode`.
   DISCREPANCY: recovery model is the boot-resolved immutable floor, not a live setter.
   CLASS: removed feature

4. DOC `:37-88,187,294-299,329` — "Read mode … ABSENT … the concept does not exist
   as a single state."
   CODE `internal/safety/floor.go:39-54` (`FloorReasonObservation`);
   `internal/env/keys.go:21,27` (`JOE_MODE=observation`);
   `internal/api/mutatestatus.go:44` (`GET /api/v1/mutate-status`).
   DISCREPANCY: a daemon-wide, boot-resolved, restart-surviving read-only posture
   (observation mode) exists and is readable via a dedicated endpoint.
   CLASS: removed feature

5. DOC §5/§6 inventory — only `/me`, `/panic/status`, `/regime` listed; AppShell
   described as mounting two banners.
   CODE `internal/api/mutatestatus.go:44`; `ui/src/components/layout/ObservationBanner.tsx`;
   `ui/src/components/layout/AppShell.tsx:25-27` mounts SafeModeBanner, ObservationBanner,
   IncidentBanner.
   DISCREPANCY: the whole observation read-path + UI indicator is missing from the inventory.
   CLASS: schema change

6. DOC `:249-254,321-323,351-355` — executor denies with `safety.ErrSafeModeActive`
   (`internal/safety/safemode.go:14-16`), matched to `safe_mode`.
   CODE `internal/tools/executor.go:215-216` returns `&safety.WriteFloorError{Reason: ...}`;
   `internal/api/writefailure.go:53-65` branches into `safe_mode` OR `observation`.
   DISCREPANCY: typed denial is now reason-carrying `WriteFloorError` with two outcomes.
   CLASS: schema change

7. DOC `:230,238-259` — write-failure codes are `zone_denial`/`incident_mode`/`safe_mode` (three).
   CODE `internal/api/constants.go:27-30` defines four, adding `observation`.
   DISCREPANCY: doc omits the fourth `observation` write-failure code.
   CLASS: schema change

8. DOC `:236` — UI `writeFailureMessage` at `ui/src/hooks/useChat.ts:64-75`.
   CODE `ui/src/hooks/useChat.ts:79-94` handles 5 cases incl. `observation`+`internal_error`.
   DISCREPANCY: cited line range wrong; doc omits `observation`/`internal_error` cases.
   CLASS: other (line drift)

9. DOC `:382` — captain routes under `/api/v1/agent-sessions/{id}/captain/*`.
   CODE `internal/api/captain.go:21-22,55-59` — routes are `/api/v1/sessions/{id}/captain/*`
   ("the legacy /agent-sessions namespace was removed in B005").
   DISCREPANCY: captain path prefix renamed.
   CLASS: renamed path

10. DOC `:96` — `registerPanicRoutes` wired at `internal/api/server.go:111`.
    CODE `internal/api/server.go:124`.
    CLASS: other (line drift)

11. DOC `:131,190` — `registerRegimeRoutes` wired at `server.go:124`.
    CODE `internal/api/server.go:147`.
    CLASS: other (line drift)

12. DOC `:47` — session `read_only` at `ui/src/api/schemas.ts:126-129`.
    CODE `ui/src/api/schemas.ts:232-237` (`:126-129` is `PromotionCandidatesSchema`).
    CLASS: other (line drift)

13. DOC `:201` — `CurrentUserSchema.allowed_actions` at `ui/src/api/schemas.ts:141-150`.
    CODE `CurrentUserSchema` at `schemas.ts:333-345`; structure is `zones[].allowed_actions`.
    CLASS: other (line drift)

14. DOC `:202,391` — `PanicStatusSchema` at `ui/src/api/schemas.ts:192-204`.
    CODE `ui/src/api/schemas.ts:388-400`.
    CLASS: other (line drift)

15. DOC `:203,388` — `RegimeSchema` at `ui/src/api/schemas.ts:171-183`.
    CODE `ui/src/api/schemas.ts:354+`.
    CLASS: other (line drift)

16. DOC `:202,203,217,221` — SafeModeBanner at `AppShell.tsx:14`, IncidentBanner at
    `:15`; only two banners.
    CODE `ui/src/components/layout/AppShell.tsx:25-27` — three banners (adds ObservationBanner).
    CLASS: other (line drift) / removed feature
    
17. DOC `:71-72` — "T1 is always read-only (`internal/safety/tier.go:7`)"; T1/T2/T3
    framing throughout §1/§7.
    CODE `internal/safety/tier.go:21` (`ActionRead`); the T1/T2/T3 tier scheme was
    collapsed to the binary Read/Mutate axis (D-0020).
    DISCREPANCY: obsolete tier vocabulary; wrong line.
    CLASS: rename (vocabulary)

---

## docs/reference/direct-http-mutation-surface.md  — MISALIGNED: 11 — RETIRED in docs-reference-audit-03

**Disposition: retire-and-absorb (doc deleted).** The doc audits three VCS POST
routes (`vcs.go`) + one publish POST route, an HTTP surface **deleted by commit
`540f5e5`**. Its central verdict (a vestigial direct-HTTP mutation surface that
bypasses the floor) and its per-route tables describe routes that no longer exist,
so the doc has no live premise. Per the survivor check in `docs-reference-audit-03`,
the only still-true present-tense invariants it asserted that were **not** already in
`security-in-layers.md` were absorbed into that doc's Part 2 "Managed-system
mutations" section: the single in-process enforcement path (executor → in-process
core client → accessor → adapter), the write floor being checked only in the
executor while the accessor carries no floor check, and the accessor being the sole
RBAC gate because `rbac.EnforcementMiddleware` is a pass-through. **Dropped** (died
with the doc): all bypass verdicts, the deleted-route/handler/`registerVCSRoutes`
tables, the deleted `internal/client/vcs.go` methods, the deleted orphan
`accessor.GitLabRequestChanges`, and every `file:line` citation. The 11 entries below
are retained for traceability.

1. DOC `:40-42` — three VCS POST routes with handlers `vcsHandler.handleGitHubPostComment`
   (`vcs.go:116`) etc.
   CODE: `internal/api/vcs.go` does not exist; no such handlers/`registerVCSRoutes` anywhere.
   CLASS: removed feature

2. DOC `:43,192-211` — `POST /api/v1/knowledge/proposals/{id}/publish` →
   `handlePublishProposal` (`proposals.go:118`).
   CODE `internal/api/proposals.go:13-20` registers only create/list/get/approve/reject;
   no `/publish` route or handler. Only the in-process `publishProposalToTarget`
   (`publish.go:19`) survives.
   CLASS: removed feature

3. DOC `:49-50` — read-only VCS GET routes at `vcs.go:39-114` / `:184-244`.
   CODE: `internal/api/vcs.go` does not exist.
   CLASS: removed feature

4. DOC `:51` — `GET /argocd|terraform|helm/...` at `gitops.go:313`.
   CODE: `internal/api/gitops.go` does not exist.
   CLASS: removed feature

5. DOC `:158,225-234` — orphan `accessor.GitLabRequestChanges` at `access/vcs.go:131`.
   CODE `internal/access/vcs.go` (113 lines) has no `GitLabRequestChanges` (only
   `GitHubRequestChanges` at `:81`).
   DISCREPANCY: the orphan method the §5c finding is built around does not exist.
   CLASS: removed feature

6. DOC `:78,217,226` — `access/vcs.go:82,90,124,131` ActionMutate declarations.
   CODE `access/vcs.go` ActionMutate at `:74,82,108`; file ends at 113 (the 4th cite
   points past EOF).
   CLASS: schema change (line drift + removed method)

7. DOC `:73-74,243` — `guard[T]` at `access.go:194`; `permit` at `access.go:120`.
   CODE `internal/access/access.go` — `guard[T]` at `:243`, `permit` at `:132`.
   CLASS: schema change (line drift)

8. DOC `:87-89,249` — `EnforcementMiddleware` `return next` at `rbac/middleware.go:78-83`.
   CODE: func at `:78`, body `return next` at `:81`; behaviorally still a pass-through.
   CLASS: schema change (line drift)

9. DOC `:90,250` — `auth.EdgeAuth` at `cmd/joe/server.go:710`.
   CODE `cmd/joe/server.go:923`.
   CLASS: schema change (line drift)

10. DOC `:85,245` — captain-gate `tools.WithWriteFloor` at `cmd/joe/server.go:599`.
    CODE `cmd/joe/server.go:681`.
    CLASS: schema change (line drift)

11. DOC `:114,248` — `tools.NewCoreRegistry(...)` at `tasks.go:269`.
    CODE `internal/api/tasks.go:280`.
    CLASS: schema change (line drift)

---

## docs/reference/managed-system-egress-map.md  — MISALIGNED: 11 — RETIRED in docs-reference-audit-03

**Disposition: retire-and-absorb (doc deleted).** Dated (2026-06-09) investigation
whose headline VERDICT (a "surviving bypass-both" via the HTTP transport) rests on
the same VCS/publish HTTP surface deleted in `540f5e5`. Per the survivor check in
`docs-reference-audit-03`, its still-true mechanism invariants overlap entirely with
those absorbed from `direct-http-mutation-surface.md` (write floor in the executor
only; accessor as the sole RBAC gate with `EnforcementMiddleware` a pass-through; the
single in-process mutation path) — captured once in `security-in-layers.md` Part 2.
**Dropped** (died with the doc): the bypass-both verdict; the deleted VCS/publish
HTTP-path findings; the orphan `accessor.GitLabRequestChanges`; the now-false
"publish_doc_update is gated under `confluence_publish` for ALL targets" claim
(the tool selects its policy key per target — `internal/safety/tier.go:229-233`); the
now-false "Core Agent refresh bypasses the accessor" claim (refresh is governed
through the accessor per CC-05); and all `file:line` citations. Two still-true
read-side properties were judged **findings/caveats, not protective mutation
invariants**, and out of this retirement's mutation-surface scope, so they were
**not** absorbed: the SELECT-only datastore-query enforcement living inside the
adapter rather than in the classification/RBAC axis (`tier.go:115,117`), and the
doc-drift detector's own `http.Client` read egress bypassing the accessor. The 11
entries below are retained for traceability.

1. DOC `:75-81,132,134,205-207` — VCS-mutation HTTP path via `internal/api/vcs.go:24,25,32`.
   CODE: `internal/api/vcs.go` does not exist; no `/comments`/`/reviews`/`/notes` routes.
   Only the agentic in-proc client calls these accessor methods (`inproc_client.go:645,650,665`).
   CLASS: removed feature

2. DOC `:105-110,191-203` — `POST /api/v1/knowledge/proposals/{id}/publish`
   (`proposals.go:118 handlePublishProposal`) as "SURVIVING bypass-both."
   CODE `internal/api/proposals.go` registers only create/list/get/approve/reject; no
   `/publish` route/handler. Publish reachable only via floor-checked agentic path.
   CLASS: removed feature

3. DOC `:81` — `internal/client/vcs.go:67,79,138`.
   CODE: no `vcs.go` under `internal/client/`.
   CLASS: removed feature

4. DOC `:83-85` — orphan `accessor.GitLabRequestChanges` (`access/vcs.go:131`).
   CODE: no `GitLabRequestChanges` anywhere; `access/vcs.go` is 114 lines.
   CLASS: removed feature

5. DOC `:63-67` — accessor VCS-mutate at `access/vcs.go:81,89,123`.
   CODE `internal/access/vcs.go` — `:73,81,107`.
   CLASS: other (line drift)

6. DOC `:69,90,186` — tool registration at `internal/tools/default.go:143,144,147,138,136`.
   CODE: these tools are now individual files under `internal/tools/core/`
   (`github_comment.go`, `publish_doc_update.go`, `detect_doc_drift.go`); not literals
   in `default.go`.
   CLASS: renamed path

7. DOC `:32,72` — floor injection at `coreagent/agent.go:75` and `tasks.go:280`.
   CODE `internal/coreagent/agent.go:92` and `internal/api/tasks.go:291`.
   CLASS: other (line drift)

8. DOC `:32` — middleware chain at `cmd/joe/server.go:703-723`.
   CODE: chain at `cmd/joe/server.go:918-935` (order VERIFIED).
   CLASS: other (line drift)

9. DOC `:44` — `rbac.EnforcementMiddleware` pass-through at `internal/rbac/middleware.go:78-83`.
   CODE: pass-through VERIFIED; func spans ~`:79-86`.
   CLASS: other (line drift)

10. DOC `:40,198` — `permit` at `access.go:120-172`; `guard[T]` sole caller at `access.go:206`.
    CODE `permit` at `:132-184`; `guard` registry.Get at `:255`. Logic VERIFIED, lines drifted.
    CLASS: other (line drift)

11. DOC `:42` — tool registry `registry.Get` at `executor.go:192, coreagent/agent.go:161`.
    CODE `executor.go:192` VERIFIED; `coreagent/agent.go:161` does not match a `registry.Get`
    call (executor built ~`agent.go:92`).
    CLASS: other (line drift)

---

## docs/reference/accessor-promotion-state-axis.md  — MISALIGNED: 9 — RETIRED in docs-reference-audit-04

**Disposition: retire-and-absorb (doc deleted; D-0050).** Dated (2026-06-14)
investigation whose core verdict — "the only component-state axis the permit decision
reads is zone assignment; no promotion/read-only field exists" — was overtaken by
D-0028 (`auto_promote_reads`), D-0030 (promotion endpoint), and the CC-05/CC-08
refresh-through-seam change. A read-only survivor check partitioned every present-tense
claim. The still-true governance properties **not** already in `security-in-layers.md`
were absorbed into that doc: (a) component creation lands the component **inert** —
credential-less, no adapter connected, resolving to the read-only `unassigned` zone
(Part 2, new "Component lifecycle" subsection); (b) credential entry is owned by the
single governed, admin-gated, audited read-only→armed **promotion** transition, which
writes a reference (never an inline secret) and performs no Connect/Probe (same
subsection; the stale "create … registers its adapter" endpoint-table row was corrected
in the same edit); (c) the autonomous `agent:core` refresh resolves adapters **through
the access seam** (`access.ResolveAdapter`, `ActionRead`) and **fails closed** when
unwired, with the seam guarded as the sole adapter path by a build-failing structural
test (§3.5). **Dropped** (died with the doc): the overtaken "single zone-assignment
axis / no promotion field" verdict; the now-false "component create → `Connect` then
`Register`, bypasses the seam" and "refresh → `Adapters.Get`, bypasses the seam" claims;
the "four-constant `rbac.Action` enum" claim (the live six are already correctly in
`security-in-layers.md` §8.1); the whole Q5 bypass enumeration; and every `file:line`
citation. The RBAC-disabled (nil-engine) permit-all caveat and the
permit-precedes-backend property were judged still-true findings/caveats rather than
missing protective invariants and were **not** absorbed (the former overlaps
`full-mode-rbac-track`; the latter is substantially covered by Part 2). No claim was
left unresolved. The 9 entries below are retained for traceability.

1. DOC `:137,167` — Core Agent refresh `→ Adapters.Get(source.ID)`; "bypasses the seam."
   CODE `internal/coreagent/refresh.go:215` calls `r.accessor.ResolveAdapter(ctx, principal,
   source.ID, rbac.ActionRead)`; `internal/access/access.go:21-25` states the refresh read is
   now GOVERNED via the accessor (CC-05).
   DISCREPANCY: refresh crosses the seam; the "bypass exception" was removed.
   CLASS: removed feature

2. DOC `:134,164` — component create `→ adapter.Connect(...)` then `Adapters.Register`.
   CODE `internal/api/components.go:192-199,274-277` — create performs no eager Connect/Register;
   component lands inert (unassigned zone, read-only floor, no credential). Credential entry
   moved to promotion (D-0030).
   CLASS: removed feature

3. DOC `:11,46,101` — "there is NO promotion/read-only/lifecycle field … only zone-assignment presence."
   CODE `internal/api/server.go:211` (`POST /components/{id}/promote`);
   `internal/store/migrations/024_agent_read_promotions.up.sql`;
   `internal/rbac/policy.go:285-301` (`auto_promote_reads` predicate).
   DISCREPANCY: a promotion transition (D-0030) and per-type auto_promote_reads axis (D-0028) now exist.
   CLASS: removed feature (claim of absence now false)

4. DOC `:11,121` — nil-engine wiring at `server.go:76-84`, mirrored at `cmd/joe/server.go:643`.
   CODE `internal/api/server.go:90-106`; build site `cmd/joe/server.go:856`
   (`NewPolicyEngineWithGovernance`).
   CLASS: other (line drift)

5. DOC `:17,23,30,109-118` — `permit` at `access.go:120-172`; `permitForPrincipal` at `:180-182`;
   `guard[T]` at `:194-218`; `Decide` core at `policy.go:109-168`.
   CODE `internal/access/access.go:132/192/243`; `internal/rbac/policy.go:229` (Decide), admit
   blocks `:253-310+`.
   CLASS: other (line drift)

6. DOC `:52-59` — `rbac.Action` enum is four constants at `zones.go:7-33`.
   CODE `internal/rbac/zones.go:10-33` — six constants (adds `ActionDeclareIncident` `:28`,
   `ActionResolveIncident` `:32`).
   DISCREPANCY: two incident-regime actions omitted from the action vocabulary.
   CLASS: schema change

7. DOC `:25,34` — quoted `Decide` core uses an elided `if err != nil { ... }`.
   CODE `internal/rbac/policy.go:232-238` has a real `slog.Warn` body plus team_flat/auto_promote
   admit branches (`:275-283`, `:301+`).
   CLASS: other (stale code excerpt)

8. DOC `:94` — `ComponentZoneAssignment{...}` at `zones.go:56-62`.
   CODE `internal/rbac/zones.go:55-62` (off-by-one; field set VERIFIED).
   CLASS: other (line drift)

9. DOC `:140` — k8s `Connect` "does a `ServerVersion` liveness probe — `k8s.go:55-75`."
   CODE `internal/adapters/k8s/k8s.go:55` (Connect), `:94` (ServerVersion probe, outside range).
   CLASS: other (line drift)

---

## docs/reference/security-in-layers.md  — MISALIGNED: 6 — RESOLVED in docs-reference-audit-02

The sole security authority. Its highest-load-bearing concrete surface — the local
file/command tools and their `internal/tools/local/...` paths — described a tool tree
removed from the codebase. Rewritten in `docs-reference-audit-02` against the live
tree: the Part-2 "Local tools" table was replaced by a "Local file/command tools —
removed" subsection; the managed-system mutation surface now lists only the registered
core tools (`publish_doc_update*`, `github_comment`, `gitlab_comment`,
`github_request_changes`) with correct `internal/tools/core/` paths; the §3.2 sample
policy now shows only the live `act` keys with the removed `write_file`/`run_command`
sections (and the `DefaultPolicy()` `run_command: enabled: true` relic) documented as
legacy-inert, reconciling the "default-deny" headline; the self-protection invariants
are described as defense-in-depth guards that remain compiled in regardless of
registration; and every `internal/tools/local/...` "Key files" citation was repointed
at `internal/tools/default.go` / `internal/tools/core/` / `internal/safety/invariants.go`.
The orphaned classification/policy/invariant entries the rewrite surfaced are filed
separately in `docs/backlog/orphaned-tool-registration-cleanup.md` for a code slice.
All 6 entries below are addressed; they are retained for traceability.

1. DOC `:51,77,331,367` — `run_command` allowlist at `internal/tools/local/runcmd/subcommands.go`;
   detail at `…/runcmd/runcmd.go`.
   CODE: `internal/tools/local/` does not exist; `internal/agentloop/echotool_test.go:11` notes
   the local-tool tree "was removed."
   CLASS: removed feature

2. DOC `:75` — `write_file` detail at `internal/tools/local/writefile/writefile.go`.
   CODE: no `writefile/` package; `internal/tools/default.go:26` lists `write_file` only as an
   omitted local tool (no constructor).
   CLASS: removed feature

3. DOC `:310,330` — Key files cite `internal/tools/local/readfile/` and `…/writefile/`.
   CODE: neither path exists; no `NewReadFileTool`/`NewWriteFileTool`/`NewRunCommandTool`/`NewAskUserTool`.
   CLASS: removed feature

4. DOC `:67-77,99-100,142-143,221-222` — Part-2 "Local tools" table presents write_file,
   run_command, read_file, local_git_status/diff, ask_user as operative tools with live
   authorization/notification behavior.
   CODE `internal/tools/default.go:26-28` explicitly omits these from registration; they survive
   only as `tier.go` classification entries (`:75-78,221-222`) with no backing tool.
   CLASS: removed feature

5. DOC `:18-19,49,217-219` — `~/.joe/skills-policy.yaml` self-protection invariant
   ("Joe cannot read/write its skills policy").
   CODE `internal/safety/invariants.go:95-98` — invariant present and correct, BUT it guards file
   tools (read_file/write_file) that are no longer registered (see #4).
   DISCREPANCY: invariant is VERIFIED in isolation; flagged only as context — it currently guards
   unwired tools. (Borderline; downgrade if counting strictly.)
   CLASS: other

6. DOC `:48,170` — headline "default-deny for mutating actions" paired with the §3.2 sample policy
   showing `run_command: enabled: true`.
   CODE `internal/safety/policy.go:77-82` — `DefaultPolicy()` ships `RunCommand.Enabled: true`
   (read-only allowlist).
   DISCREPANCY: low-severity wording tension; the §3.2 sample at `:170` reconciles it.
   CLASS: stale default (low severity)

---

## docs/reference/joe-architecture.md  — MISALIGNED: 3 — RESOLVED in docs-reference-audit-05

Re-verified against the live tree and corrected: the directory tree no longer
lists `internal/tools/local/` (only `core/`+`shared/` exist); the Chat Sessions
"Locations" line now points at `internal/api/sessiongate.go` (the owner-scoped
session-authorization seam; `internal/api/sessions.go` does not exist); the
config.yaml example shows `gemini-2.5-flash` (`internal/config/constants.go:15`).
All 3 entries below addressed; retained for traceability.

1. DOC `:487` — directory tree lists `internal/tools/local/` ("readfile, writefile, runcmd").
   CODE: `internal/tools/` holds only `core/` and `shared/`; local tools registered in
   `internal/tools/default.go:26`; no `internal/tools/local/` directory.
   CLASS: renamed path (removed)

2. DOC `:289` — "`internal/api/sessions.go` (owner-scoped routes)."
   CODE: no `internal/api/sessions.go`; owner-scoped session authorization lives in
   `internal/api/sessiongate.go`, wired via `server.go`/`webui.go`.
   CLASS: renamed path

3. DOC `:531` — config.yaml example shows `model: gemini-2.0-flash`.
   CODE `internal/config/constants.go:15` — `defaultGeminiModel = "gemini-2.5-flash"`.
   CLASS: stale default

---

## docs/reference/DESIGN-SESSIONS-VIEW.md  — MISALIGNED: 3 — RESOLVED in docs-reference-audit-05

Re-verified and corrected: §2.1 now describes the list query as a bare SQL
`LIMIT ?` bound to the handler default of 20 (`repository.go:534`,
`webui.go:359`) — no literal `LIMIT 50` survives in code (the only `LIMIT 50`
string left is a stale comment in `ui/src/lib/sessionFilterSort.ts:22`, a code
file out of this documentation-only slice's scope), so §2.1 and §6.A ("default
20") now agree; the §6.B struct-comment excerpt no longer misquotes the rewritten
`webui.go:227` comment — it now describes the post-P0 both-surfaces (self-join,
not N+1) behavior that comment documents. All 3 entries below addressed; retained
for traceability.

1. DOC `:99,102` — "single capped top-N (`LIMIT 50`, no OFFSET/cursor,
   `internal/sessionmodel/repository.go:534`)."
   CODE `internal/sessionmodel/repository.go:533-534` — `LIMIT ?` bound to handler default
   `limit := 20` (`internal/api/webui.go:359`); no literal `LIMIT 50`.
   CLASS: stale default

2. DOC `:198-199` — "default 20 … `repository.go:534`, `:572`" while §2.1 says `LIMIT 50`.
   CODE: both cites are `LIMIT ?`; §6.A ("default 20") contradicts §2.1 ("LIMIT 50"); the wrong
   `LIMIT 50` literal is also echoed in `ui/src/lib/sessionFilterSort.ts:22`.
   CLASS: stale default

3. DOC `:227-229` — quotes the struct comment at `internal/api/webui.go:227` as "Set ONLY on the
   per-id GET (N+1 on the list projection)."
   CODE `internal/api/webui.go:227-231` — comment rewritten to describe the post-P0 both-surfaces
   behavior (self-join, not N+1); quoted text no longer at that line.
   CLASS: other (stale line/excerpt)

---

## docs/reference/joe-identity-design.md  — MISALIGNED: 2 — RESOLVED in docs-reference-audit-05

Re-verified and corrected per D-0011/D-0016, respecting the doc's AS-BUILT banner
(no drifted line numbers reintroduced, package path only): §2.9 now states that
zone provisioning runs over the admin REST API (`internal/api/admin.go`, the
single audited writer) — the design-era "CLI command only" plan was superseded
and the CLI provisioning surface removed (confirmed `cmd/joe/main.go`); §6 now
lists the admin HTTP endpoint as **shipped, not deferred** (`/api/v1/admin/zones`,
`/admin/component-zones`). All 2 entries below addressed; retained for traceability.

(AS-BUILT banner declares inline `file:line` citations historical — not flagged. These two
are as-built mechanism claims contradicted by D-0011/D-0016.)

1. DOC §2.9 `:141` — "Zone provisioning … CLI command only for v1 … Admin UI and admin HTTP
   endpoint are deferred behind this seam."
   CODE `cmd/joe/main.go:613-615` — RBAC zone/admin provisioning "is no longer a CLI surface — it
   runs over the admin REST API (`internal/api/admin.go`), the single audited writer" (D-0016).
   DISCREPANCY: the opposite — CLI removed, REST is the sole writer.
   CLASS: removed feature

2. DOC §6 `:199` — lists "Admin UI / admin HTTP endpoint for provisioning" as deferred behind a seam.
   CODE: admin REST surface is live (`internal/api/admin.go`: `/api/v1/admin/zones`,
   `/admin/component-zones`); shipped per D-0011/D-0016.
   CLASS: stale default (deferred feature was built)

---

## docs/reference/joe-skills-design.md  — MISALIGNED: 2 — RESOLVED in docs-reference-audit-05

Re-verified and corrected against `cmd/joe/main.go`: the `joe skills status`
subcommand was removed from the CLI-surface listing (the live switch is
install/list/remove/update/approve/reject/reload, with status folded into
`list`); the install invocation now shows the as-built shape
`joe skills install <repo-url> [--ref <branch|tag>] [--subdir <path>]` — a single
`<repo-url>` positional plus a `--subdir` flag for single-skill sparse checkout,
not the old inline `<git-url>/<path>`. All 2 entries below addressed; retained for
traceability.

1. DOC `:167` — CLI surface lists `joe skills status` as a distinct subcommand.
   CODE `cmd/joe/main.go:343-549` — switch has install/list/remove/update/approve/reject/reload only;
   no `status` case (usage at `:296` omits it). Status is folded into `list` (`:388-400`).
   CLASS: removed feature

2. DOC `:165` — `joe skills install <git-url>/<path>` (inline positional for single-skill install).
   CODE `cmd/joe/main.go:347-356` — `install` takes one `<repo-url>` positional and a `--subdir <path>`
   flag (and `--ref`), not an inline `<git-url>/<path>`.
   CLASS: other (CLI invocation-shape change)

---

## docs/reference/observability.md  — MISALIGNED: 2 — RESOLVED in docs-reference-audit-05

Re-verified against the live tree, where the audit's own code-side citations had
drifted since it ran: `internal/observability/llm_middleware.go` no longer exists,
and `NewInstrumentedAdapter` is now **wired into production** (the audit's "neither
is constructed in production" premise is reversed). Corrected to live truth: the
`llm.*` metric names are inline literals in `internal/llm/instrumented.go`
(`llm.requests`, `llm.errors`, `llm.tokens.input`, `llm.tokens.output`,
`llm.request.duration`) — not in `metric_names.go`, which holds the
`tools.*`/`adapters.*`/`graph.*`/`http.*`/`coreagent.*` + `joe.build.info`
families; the package tree dropped the deleted `llm_middleware.go`; the
architecture diagram, metric table, and Prometheus query examples now name the
`InstrumentedAdapter` and its live metric names; and the doc now states the
`InstrumentedAdapter` is the active path, wired as the outermost LLM-chain wrapper
by `internal/core/llmchain.go` `BuildLLMChain` (boot `cmd/joe/server.go` + both
model-swap handlers). The span/trace section (`llm.chat` attributes) was already
correct against `instrumented.go` and was preserved. All 2 entries below addressed;
retained for traceability.

1. DOC `:259` — package tree points the headline `llm.*` metric names at `metric_names.go`.
   CODE `internal/observability/metric_names.go:11-57` holds `tools.*`/`adapters.*`/`graph.*`/`http.*`/
   `coreagent.*` + `MetricBuildInfo`; the `llm.*` names are inline literals in
   `internal/observability/llm_middleware.go:42-76`.
   CLASS: other (file-contents mismatch)

2. DOC `:24,93-100,264` — implies the described LLM OTel instrumentation (`LLMMiddleware` emitting
   `llm.calls`/`llm.errors`/`llm.duration`/`llm.tokens`) is the active path and `instrumented.go` is the
   wrapper in use.
   CODE `internal/observability/llm_middleware.go:37` and `internal/llm/instrumented.go:53` — `NewLLMMiddleware`
   and `NewInstrumentedAdapter` are referenced only by tests; neither is constructed in `internal/llmfactory/`
   or `cmd/`. The two paths use divergent metric names and neither is wired into the running binary.
   CLASS: removed feature / stale (described instrumentation not wired in production)

---

## Clean docs (no MISALIGNED claims)

- **docs/reference/DESIGN-CHAT-SESSIONS.md** — VERIFIED 24, MISALIGNED 0, UNVERIFIABLE 2. The §1–§11
  history is correctly superseded by the line-3 banner; the normative §12/§13 as-built spec (migrations
  025–027, `internal/sessionmodel/`, `internal/sessionauthz/`, `internal/sessionarchive/`, the
  `/api/v1/sessions` + `/api/v1/admin/sessions` surface) matches live code.
- **docs/reference/security-architecture-direction.md** — VERIFIED 8, MISALIGNED 0, UNVERIFIABLE 14.
  Self-declared "DESIGN INTENT — not a description of the current implementation"; forward-looking targets
  (computed-decision, agent budget, egress chokepoint/verifier, capability-scoped interfaces) classified
  UNVERIFIABLE. Present-tense claims that touch today's code (binary read/mutate, deny-by-default-on-omission,
  native Claude+Gemini, floor>incident>RBAC, write-floor determinism) all VERIFIED.
- **docs/reference/learn-from-sessions-current-state.md** — VERIFIED 28, MISALIGNED 1, UNVERIFIABLE 2. The
  doc's thesis (the extractor in `internal/knowledge/learning/` is orphaned/unwired and self-contradicts on
  tier) is confirmed against live code. Its single misalignment was a 2-line off citation, **RESOLVED in
  docs-reference-audit-05** (corrected to `:42`; the orphaned-extractor thesis left untouched):
  - DOC `:38` — `type Services struct` at `internal/core/services.go:40`.
    CODE `internal/core/services.go:42`.
    CLASS: other (line drift)

---

## Notes for the actioning campaign

- The largest single fix surface is **`security-in-layers.md`** (the live mutation-surface section
  references a removed `internal/tools/local/` tree) and **`operational-modes-ui-status.md`** (whole-doc
  staleness against the D-0018 write-floor model). These are content rewrites, not line touch-ups.
- **`direct-http-mutation-surface.md`** and **`managed-system-egress-map.md`** were dated investigation
  snapshots whose central premise (a direct-HTTP managed-system mutation surface) was removed by `540f5e5`.
  Both were **retired (deleted) in `docs-reference-audit-03`** under a retire-and-absorb disposition: their
  still-true mutation invariants were absorbed into `security-in-layers.md` (see those sections above); their
  bypass verdicts and deleted-surface findings were dropped.
- **`accessor-promotion-state-axis.md`** was a dated investigation snapshot whose premise was overtaken by
  D-0028, D-0030, and CC-05. **Retired (deleted) in `docs-reference-audit-04`** under a retire-and-absorb
  disposition (D-0050): its still-true governance properties (inert component creation, the governed
  read-only→armed promotion that owns credential entry, and the accessor-governed/fail-closed refresh) were
  absorbed into `security-in-layers.md`; its overtaken verdict, bypass enumeration, and `file:line` citations
  were dropped.
- The remaining docs need targeted edits: a renamed path (`joe-architecture.md`), a stale default
  (`joe-architecture.md` gemini model; `DESIGN-SESSIONS-VIEW.md` LIMIT), a removed subcommand
  (`joe-skills-design.md` `status`), and as-built corrections (`joe-identity-design.md` §2.9/§6).
