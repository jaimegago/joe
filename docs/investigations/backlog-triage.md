# Backlog triage — done / obsolete / open against landed reality

Read-only diagnosis (2026-06-24, HEAD `1fcc5b4`). For each pre-existing backlog
file directly under `docs/backlog/`, the verdict is derived from **landed
reality** — git log, `docs/DECISIONS.md`, and the current codebase — not from the
tone of the prose. No files were modified, moved, or archived.

**Scope.** 22 files sit directly under `docs/backlog/`. Four were recently
created as known-open and are **excluded** from triage per the task
(`launch-positioning-and-lgt-decoupling`, `oasis-relationship`, `session-doc-debt`,
`edge-type-literal-consolidation`). The remaining **18** are triaged below. There
is no `INDEX.md` or `done/` subdirectory present despite the CLAUDE.md pointer.

Verdicts are restricted to: `likely-done`, `likely-obsolete`, `still-open`,
`cannot-determine`.

## Summary table

| Slug | Verdict |
|------|---------|
| adapter-dispatch-consolidation | still-open |
| aws-credential-provider | still-open |
| azure-adapter-connect-skeleton | still-open |
| azure-credential-provider-and-connect | still-open |
| captain-write-consolidation | still-open |
| cross-incident-relink | still-open |
| datastore-uri-credential-provider | still-open |
| denial-feedback-popup | still-open |
| full-mode-rbac-track | still-open |
| governed-connectivity-check-surface | still-open |
| incident-view-filter-to-mine | still-open |
| learn-from-sessions-fate | still-open |
| posture-endpoint-grants-signal | still-open |
| promotion-requirements-single-source | still-open |
| registry-auth-pair-credential-provider | still-open |
| session-content-search | still-open |
| sessions-view-paging | still-open |
| tilde-expansion-helper-unification | still-open |

All 18 in-scope items are **still-open**. None landed in full; none was superseded
or decided against. Two items (`datastore-uri-credential-provider`,
`full-mode-rbac-track`) have landed *sub-parts* that are called out explicitly in
their entries below — but the item's load-bearing deliverable remains undone in
each case, so the verdict stays `still-open` rather than `likely-done`.

---

## Findings

### adapter-dispatch-consolidation
**Summary:** Fold the divergent type→adapter construction paths (API path vs boot
path) into one canonical function so coverage is the union and no valid type
returns nil.
**Verdict:** still-open
**Evidence:** Both construction paths are still distinct in the live tree. The API
path `newAdapterForType` remains at `internal/api/components.go:131` and is the one
`webui.go:880` calls; the boot path `connectSourcesDefault`
(`cmd/joe/server.go:1018`) still does its **own** per-type
`Components.ListByType(...) + adapter.New()` (11 such call sites in the function
body — `k8s.New()`, `gitadapter.New()`, `awsadapter.New()`, …) and does not route
through `newAdapterForType`. No single canonical construction function exists. The
D-0021 source→component rename (the only dependency the file names) did land
(commit history / `internal/api/components.go`), so the item is unblocked but not
done.

### aws-credential-provider
**Summary:** No AWS-credential-chain provider exists on the credential seam, so
`cloudwatch`/`ecr`/`aws` cannot be promoted under the credential-reference model.
**Verdict:** still-open
**Evidence:** `internal/credential/` contains only `static.go` and
`kubeconfig_exec.go` as concrete providers (plus `provider.go`, `wiring.go`,
`requirements.go`, etc.); no AWS-shaped provider file exists. Deferral recorded by
D-0026; nothing wired since. `ProviderForKind` (`internal/credential/provider.go:95`)
constructs only the static/kubeconfig-exec kinds.

### azure-adapter-connect-skeleton
**Summary:** The azure adapter `Connect` is a skeleton (no SDK client, no
connectivity check); deferred behind D-0026's ambient-workload-identity provider.
**Verdict:** still-open
**Evidence:** Deferral recorded by D-0026; the ambient-workload-identity provider
it ties to is still absent (no such provider in `internal/credential/`). No commit
since builds the azure SDK client. No landed signal contradicting the deferral.

### azure-credential-provider-and-connect
**Summary:** `azure` is blocked from promotion by two missing pieces — a real
`Connect` and an Azure-shaped credential provider; `azuremonitor`'s auth path is an
open question.
**Verdict:** still-open
**Evidence:** No Azure-shaped provider in `internal/credential/` (only
static + kubeconfig-exec). No `azuremonitor` adapter exists under
`internal/adapters/` (open question unresolved). Deferral recorded by D-0026.

### captain-write-consolidation
**Summary:** Introduce a single tx-aware detach/attach seam so the three
`session_captains` write patterns share one detach SQL.
**Verdict:** still-open
**Evidence:** The shared **attach** core `attachCaptainExec` exists
(`internal/sessionmodel/repository.go:935`), but the proposed shared **detach**
(`detachCaptainExec`) does **not** — no such symbol exists. The detach SQL is still
duplicated: the inline `SET` in `regime_transitions.go:259` ("Mirrors
MarkCaptainDetached's SET but keyed by session_id"), the standalone
`MarkCaptainDetached` (`repository.go:1116`), and `swapCaptainWithHook`'s inline
detach (`repository.go:1151`). Deferral recorded by D-0025; D-0024/D-0025 are the
landed work this refactor sits on top of.

### cross-incident-relink
**Summary:** Post-launch structural feature to let a resolved incident master be
attached as a participant of a different active incident (or a dedicated
related-incidents pointer).
**Verdict:** still-open
**Evidence:** Created as a deliberate deferral by commit `e770291`
("docs(backlog): defer cross-incident relink of former incident master"). The
structural blockers it names are still live: the CHECK
`(linked_incident_id IS NULL OR type <> 'incident')`
(`internal/store/migrations/025_session_schema_rewrite.up.sql`) and
`handleLinkIncident`'s 409 refusal of incident-type sessions (`internal/api/webui.go`)
both remain. No related-incidents relation was added.

### datastore-uri-credential-provider
**Summary:** No URI-shaped credential provider exists, so
`postgresql`/`mysql`/`mongodb`/`kafka`/`elasticsearch` cannot be promoted, and the
URI-redaction boundary is unowned.
**Verdict:** still-open
**Evidence:** No URI/datastore provider in `internal/credential/` — the seam is
still static + kubeconfig-exec only, so the promotion blocker stands (deferral
recorded by D-0026). **Partial landing, noted but not sufficient for done:** the
two concrete mongodb leak sites the file flags were independently fixed by commit
`753a9b0` ("fix(adapters): redact credentials from MongoDB URIs…") — `mongodb.go:106`
and `:135` now wrap the URI in `adapters.RedactURI(...)` (added in
`internal/adapters/redact.go`). That closes the verified leak but not the item's
load-bearing deliverable (the provider that gates promotion and owns redaction
across the URI datastores).

### denial-feedback-popup
**Summary:** A distinct pop-up/toast surface (not the inline transcript notice)
that fires when a user-initiated action is denied by any of the floor/incident/RBAC
layers.
**Verdict:** still-open
**Evidence:** Denials still render **inline only**: `writeFailureCode` is consumed
solely by the `write-failure-notice` `<div>` in
`ui/src/components/chat/AssistantTurnView.tsx:99` via `writeFailureMessage`
(`ui/src/hooks/useChat.ts`). A Sonner `<Toaster>` is mounted
(`ui/src/App.tsx:4,56`) and `toast(...)` is used by admin components
(SettingsTab, PoliciesTable, AdminsTable, …) — but **nothing wires denial codes to
it**. No toast/modal consumes `writeFailureCode`. The backend code vocabulary the
file says already exists (`internal/api/writefailure.go`, `constants.go`) is indeed
present, confirming this is the unbuilt frontend presentation task.

### full-mode-rbac-track
**Summary:** Implement the full-capabilities posture: full mode must require a live
policy engine (no fail-open nil engine), fail closed at zero grants, and introduce
a dedicated autonomous principal.
**Verdict:** still-open
**Evidence:** Two of the four named obstacles have **landed** (called out for
honesty, but the track as scoped is not complete): obstacle 4 (no autonomous
principal) is closed — `CoreAgentServiceName = "agent:core"` and
`AgentCorePrincipal` now exist (`internal/rbac/identity.go:87`), introduced with
the auto_promote_reads predicate (D-0028); obstacle 1 (nil-engine fail-open) is
addressed by D-0027 ("Refuse to start without a usable identity configuration —
engine-nil-at-runtime made unreachable"). **But** the central deliverable is not in
the tree: no "full mode" boot posture/config exists (no `full_mode`/`FullMode`
match in `cmd/joe/server.go` or `internal/safety/`), the nil-engine allow-all
branch still physically exists at `internal/access/access.go:140`
(`reason = "rbac_disabled"`), and the autonomous-path seam routing the file says it
unblocks is still not done. Deferral design of record is D-0019.

### governed-connectivity-check-surface
**Summary:** Collapse the two "does this component work?" paths into one governed
check; repoint the UI off the un-gated legacy `/components/{id}/test` route.
**Verdict:** still-open
**Evidence:** The legacy route is still registered ungated on the webUI group —
`handleTestComponent` (`internal/api/webui.go:857`), registered at
`webui.go:944` with no `requireAdmin`. The frontend still points at it:
`testComponent` posts to `/api/v1/components/${id}/test`
(`ui/src/api/components.ts:56-58`), not the admin credential-status Probe. Neither
the deprecate/admin-gate decision nor the repoint has happened. Standard it
diverges from (D-0029/D-0030) is landed.

### incident-view-filter-to-mine
**Summary:** A "filter to mine" toggle for the incident view, blocked on the
unresolved "what makes a cluster mine" rule (master-ownership vs any-participant).
**Verdict:** still-open
**Evidence:** Explicitly "ships only if explicitly chosen." The open question is
undecided and no such filter exists in the sessions views (the P1/P2 work that
landed — commits `d88f342`, `5f0815c`, `17f1491` — added the two-view split and
client-side title filter+sort, not a cross-owner "mine" toggle). UI-only feature,
no schema signal to find.

### learn-from-sessions-fate
**Summary:** Records the decision that the dormant learn-from-sessions extractor is
deferred to a future feature (not retired), and the legacy `sessions`/
`session_messages` tables must be retained.
**Verdict:** still-open
**Evidence:** This is a decision record whose *work* (rewire the extractor at the
live store, route through the governed seam, add invocation + config switch) is
future. `internal/knowledge/learning/extractor.go` still exists, still dormant; the
fate-recording commits are `3102c2b` and `0a0f28b`. No rewire commit since. The
hard constraint (don't drop legacy tables) is an active, open dependency on the
B001 consolidation — not completed work.

### posture-endpoint-grants-signal
**Summary:** Add a coarse "do any write grants exist" boolean to a posture read
endpoint, shipping with the full-mode/RBAC track.
**Verdict:** still-open
**Evidence:** The posture endpoint it extends still does **not** exist — no posture
handler/route/response struct in `internal/api/` (every "posture" hit is unrelated
prose like `audit.FailurePosture` / "failure posture"). It ships *with*
`full-mode-rbac-track`, which is itself still-open. Two layers of unbuilt
prerequisite; no landed signal.

### promotion-requirements-single-source
**Summary:** Refactor `buildArmedConfig` to validate **from** the
`promotionRequirements` table so the describe table and the enforcement branching
stop being two declarations of the same per-Kind rules.
**Verdict:** still-open
**Evidence:** `buildArmedConfig` (`internal/api/components.go:758`) still enforces
via its own inline `switch kind` — `KindStatic` (env_var required, inline value
rejected) and `KindKubeconfigExec` (in_cluster-or-kubeconfig either-or) branches are
hand-coded in the function, not driven from `promotionRequirements`. The deliberate
drift surface the file describes is intact. Reference decision D-0030 is landed; the
no-drift refactor is not.

### registry-auth-pair-credential-provider
**Summary:** No registry-auth (username+password / bimodal artifactory) provider
exists, so `oci_registry`/`dockerhub`/`artifactory` cannot be promoted.
**Verdict:** still-open
**Evidence:** No registry/pair provider in `internal/credential/`. The
"Deliberately ABSENT" comment in `internal/credential/wiring.go:32-36` still lists
oci_registry/dockerhub/artifactory as not wired. Confirms the prior landed history
the file cites — artifactory was wired then reverted (commits `66c9253` then
`cc9289e` "back artifactory out of the W2 static-token wired set"). Deferral
recorded by D-0026 / D-0031 §DEFERRED.

### session-content-search
**Summary:** A conversational retrieval tool that searches `chat_messages`
transcript bodies (needs a net-new content/FTS/embedding index), distinct from the
P2 title-only list filter.
**Verdict:** still-open
**Evidence:** Created as a post-launch deferral by commit `374de72`
("docs(sessions): P2 client-side interim note + P3/content-search backlog"). No
content index over `chat_messages` and no such retrieval tool/skill exists. The P2
filter that did land is title-only over the list projection (`webUISession`,
`internal/api/webui.go`), exactly as the file distinguishes.

### sessions-view-paging
**Summary:** P3 of the sessions-view split — introduce paging and move the
client-side keyword-filter+sort server-side; the open decision is the paging unit
(flat rows vs atomic clusters).
**Verdict:** still-open
**Evidence:** The list query is still an unpaged capped top-N — `LIMIT ?` with no
`OFFSET`/cursor at `internal/sessionmodel/repository.go:534` (and the parallel
queries at `:572`, `:613`). P2's filter/sort are still the client-side pure
functions in `ui/src/lib/sessionFilterSort.ts` (landed via `17f1491`/`5f0815c`),
not moved server-side. Paging not built; open architecture decision undecided.

### tilde-expansion-helper-unification
**Summary:** Converge the three `~`-expansion helpers (two byte-identical
hand-copies + the security-sensitive `paths.ExpandPath`) onto one shared helper,
once the import cycle and the `os.UserHomeDir`-vs-`getSecureHomeDir`/`Abs` semantic
difference are reconciled.
**Verdict:** still-open
**Evidence:** All three helpers still exist separately: `expandPath`
(`internal/adapters/k8s/k8s.go:172`), `expandKubeconfigPath`
(`internal/credential/kubeconfig_exec.go:190`), and `ExpandPath`
(`internal/paths/defaults.go:61`). No shared leaf-package helper was introduced. The
`internal/credential/tildeguard` guard (the file's deferral-safety net) is present,
consistent with "deferring is safe." Deferral recorded by D-0026.
