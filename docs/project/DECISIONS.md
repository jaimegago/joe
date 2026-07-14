# Joe — Decisions

Append-only project decision log. Newest entries at the top. Each entry
records what was decided, the basis (verifiable source, not assertion), and
what it supersedes. This file is normative: where a decision here conflicts
with prose elsewhere, this file states the project's position and the
conflicting prose is stale.

Format per entry: ID, date, decision, basis, supersedes, status.

---

## D-0100 — Loop-budget exhaustion no longer hard-fails: the agentic loop makes one forced-synthesis call before returning ErrMaxIterations

- Date: 2026-07-14
- Session: loop-budget-exhaustion
- Decision: when `Agent.Run` (`internal/agentloop/agent.go`) exhausts its iteration
  budget without the model producing a tool-call-free answer, it no longer returns
  `ErrMaxIterations` immediately. It makes **exactly one additional `Chat` call with
  no tools offered**, reusing the accumulated session messages plus an appended
  user-role instruction (`prompts.MaxIterationsSynthesis`) directing the model to
  answer now from the evidence already gathered — stating explicitly what it
  verified and what remains unverified because the step budget was reached. Content
  extraction reuses the normal no-tool-call completion branch's logic (the raw
  `resp.Content` string). If synthesis returns **non-empty** content the run is a
  **success** carrying that answer; if it **errors or returns empty content** the run
  falls through to the existing `ErrMaxIterations` return, unchanged. In-budget runs
  are byte-identical to prior behaviour — the synthesis call happens only after the
  loop has fully exhausted. The synthesis call is **observer-silent** (no `OnStep`),
  so the reported iteration count still counts loop steps only, and it is built from
  a local copy of the message slice writing nothing back into session history, so the
  turn's pruning/truncation counters are unperturbed; its token usage IS recorded.
- Basis: `internal/agentloop/agent.go` (post-loop synthesis seam + `synthesizeFinalAnswer`),
  `internal/prompts/prompts.go` (`MaxIterationsSynthesis`), `TestMaxIterations_ForcedSynthesisSucceeds` /
  `TestMaxIterations_SynthesisFailureFallsThrough` (`internal/agentloop/synthesis_test.go`),
  `TestTaskEndpoint_MaxIterations_ForcedSynthesisCompletes` (`internal/api/tasks_test.go`).
- Supersedes: the prior unconditional hard-fail at the loop's iteration cap. The
  `ErrMaxIterations` sentinel and its `taskStatus` classification are retained for the
  synthesis-failure path (see D-0098).
- Status: accepted.

---

## D-0099 — A successful forced synthesis completes; an additive `stop_reason` field carries the truncation marker across the wire and into storage

- Date: 2026-07-14
- Session: loop-budget-exhaustion
- Decision: a run that answers via forced synthesis (D-0100) reports terminal status
  **`completed`**, not `max_iterations_reached`. A new **`stop_reason`** field —
  additive, `omitempty`, a short string enum whose first value is **`max_iterations`**
  — is introduced generically so the token-ceiling and context-overflow paths can
  adopt sibling values later. It is surfaced on the non-streaming task response and
  the SSE final event (`taskResponse.StopReason` in `internal/api/tasks.go`,
  `stop_reason` in `FinalEventSchema`), carried off the session via
  `Session.StopReason()` (`internal/agentloop/session.go`, set only on the synthesis
  path), and **persisted on the assistant `chat_messages` row** via migration 030
  (`stop_reason TEXT`, nullable). The persistence chain threads through
  `sessionmodel.ChatMessage` (struct + `AddChatMessage` INSERT + `ListChatMessages`
  SELECT/Scan + `InsertChatMessageTx` archive-restore), `persistTaskMessages`,
  `webUIMessage`/`messageToWebUI`, `ChatMessageSchema`, and `historyToItems`, so a
  reloaded session can still render the truncation marker. `max_iterations_reached`
  remains a terminal **status** only for the synthesis-failure path.
- Basis: `internal/store/migrations/030_chat_message_stop_reason.up.sql`,
  `internal/sessionmodel/{types.go,repository.go,lifecycle.go}`,
  `internal/api/{tasks.go,tasks_stream.go,webui.go}`,
  `ui/src/api/{taskStream.ts,schemas.ts}`, `ui/src/hooks/useChat.ts`.
- Supersedes: nothing; additive to the task response, SSE final event, and
  `chat_messages` schema. No audit-schema migration is needed — see D-0097.
- Status: accepted.

---

## D-0098 — The UI renders an amber truncation notice for a completed max_iterations turn, distinct from the red failure banner

- Date: 2026-07-14
- Session: loop-budget-exhaustion
- Decision: when a **completed** turn carries `stop_reason` `max_iterations`,
  `AssistantTurnView` renders a visible **amber/warning** truncation notice
  (`data-testid="max-iterations-notice"`, reusing the existing amber write-failure
  notice styling) below the answer, stating the step budget was reached and the answer
  synthesizes the evidence gathered so far. This is deliberately distinct from the
  **destructive red failure banner**, which stays exactly as-is and is reserved for
  the `max_iterations_reached` **status** (the synthesis-failure path). The marker is
  set on both a live turn (from the SSE final event's `stop_reason`) and a reloaded
  turn (from the persisted `chat_messages.stop_reason` via `historyToItems`).
- Basis: `ui/src/components/chat/AssistantTurnView.tsx`, `ui/src/hooks/useChat.ts`
  (`AssistantTurn.stopReason`, `onFinal`, `historyToItems`),
  `ui/src/components/chat/AssistantTurnView.test.tsx`.
- Supersedes: nothing; the red-banner path for `max_iterations_reached` is unchanged.
- Status: accepted.

---

## D-0097 — Cap hits are audited: one llm_max_iterations_reached row per hit, written agentloop-side whether or not synthesis succeeds

- Date: 2026-07-14
- Session: loop-budget-exhaustion
- Decision: a new audit action **`ActionLLMMaxIterationsReached`
  (`llm_max_iterations_reached`)** under the existing **`KindLLMLimitTriggered`** kind
  records each iteration-cap hit. The writer (`writeMaxIterationsAudit`) lives
  **agentloop-side on `a.auditRepo`**, mirroring `writeRunawayAudit` exactly: same
  nil-tolerance (no repo wired → skip silently), same **fail-open-but-loud** posture
  via `audit.FailurePosture(..., FailOpen)`, decision `deny`. The **Reason** value is
  **`max_iterations_exhausted`**, a sibling to `session_token_ceiling_exceeded` and
  `context_window_exceeded`. Exactly **one row per cap hit** is written whether or not
  synthesis succeeded; the blob records the iteration count and a `synthesized`
  boolean. No audit-schema migration is required — the `audit_log.kind` CHECK already
  admits `KindLLMLimitTriggered` (migration 017); the CHECK enumerates kinds, not
  actions.
- Basis: `internal/audit/audit.go` (`ActionLLMMaxIterationsReached`),
  `internal/agentloop/agent.go` (`writeMaxIterationsAudit`),
  `internal/agentloop/synthesis_test.go`, `internal/api/tasks_test.go` (audit-row
  assertions on both the success and failure paths).
- Supersedes: nothing; extends the LLM-limit audit family
  (D-0097 companion to `ActionLLMRunawayTerminated` / `ActionLLMContextOverflow`).
- Status: accepted.

---

## D-0096 — DefaultMaxIterations raised from 10 to 20; the duplicated resolution is consolidated; errored tool calls still count full price

- Date: 2026-07-14
- Session: loop-budget-exhaustion
- Decision: `agentloop.DefaultMaxIterations` is raised from **10 to 20**, giving
  multi-step investigations more room before the cap fires; per-request override via
  `taskConfig.MaxIterations` is unchanged. With the cap no longer a hard-fail
  (D-0100) it now bounds tool spend, not the ability to answer. The two byte-identical
  copies of the max-iterations resolution+validation (`internal/api/tasks.go` and
  `internal/api/tasks_stream.go`) are consolidated into a single helper
  `resolveMaxIterations`. **Errored tool calls continue to count full price against
  the iteration budget** — a failed tool call consumes an iteration exactly as a
  successful one does; this is recorded as an explicit decision, with no code change
  (the loop already increments per iteration regardless of per-tool outcome).
- Basis: `internal/agentloop/constants.go` (`DefaultMaxIterations = 20`),
  `internal/api/tasks.go` (`resolveMaxIterations`), `internal/api/tasks_stream.go`,
  `internal/agentloop/agent_test.go` (cap-behaviour test pinned to an explicit cap
  rather than the default).
- Supersedes: the prior `DefaultMaxIterations = 10` and the duplicated inline
  resolution in the two task handlers.
- Status: accepted.

---

## D-0095 — The backlog file convention gains two optional lines, `Priority:` and `Blocked-by:`, and INDEX.md gains a blank-when-absent Priority column; a fuller relationship model is explicitly rejected

- Date: 2026-07-13
- Session: backlog-priority-field
- Decision: the per-file backlog format (`docs/backlog/<slug>.md`) is extended with
  **two optional lines placed immediately after the `Status:` line**:
  - **`Priority:`** — exactly one of `now`, `next`, `later`. `now` = on the current
    working horizon, picked from first; `next` = queued immediately behind the
    current horizon; `later` = no horizon. **An absent `Priority:` line means
    untriaged** — it is never defaulted to a value.
  - **`Blocked-by:`** — names either a backlog slug or the form
    `external (short note)`. It is a **one-directional edge**: there is deliberately
    **no `Blocks` field, no `Related` field, and no reverse-edge maintenance**.
    `Blocked-by:` is the canonical dependency line going forward; pre-existing
    informal variants (e.g. `Depends on:` in older files) are left as-is and
    normalized during the triage pass, not opportunistically.
  `docs/backlog/INDEX.md` gains a **Priority column after Status**, carrying the
  item's `Priority:` value; an item with no `Priority:` line **renders as a blank
  cell, never a default**. `Blocked-by:` is **not** surfaced in INDEX — it is
  detail-pass information read from the item file.
- Rejected alternative: a fuller relationship model — bidirectional
  `Blocks`/`Blocked-by` edges, a `Related` field, or a dependency graph across
  backlog items — was considered and **rejected**. The backlog is a flat worklist,
  not a graph; reverse-edge maintenance is manual toil with no picking-time payoff,
  and a single forward `Blocked-by:` edge captures the only dependency signal that
  affects what can be picked. The one-directional edge is the whole of it.
- Mechanics: INDEX has **no regeneration script** — regeneration is by-hand per the
  instructions in the INDEX header, so those header instructions were updated to
  specify the new column and its blank-when-absent rule (rather than a script). The
  format is described identically in `docs/project/pm-convention.md` and
  `docs/project/claude_joe_project_instructions.md` (kept in lockstep). `CLAUDE.md`
  was **not** touched: it only points at `docs/backlog/INDEX.md` and does not
  describe the backlog file format, so there was nothing to keep in sync.
- Scope: this session added the convention and regenerated INDEX under it; **every
  pre-existing item renders a blank Priority cell** and no existing backlog body or
  status line was edited. The deferred, maintainer-dictated triage pass that assigns
  actual priorities (and normalizes stray `Depends on:` lines to `Blocked-by:`) is
  recorded as `docs/backlog/backlog-priority-triage.md`.
- Basis: this session's read of `docs/backlog/INDEX.md` (header derivation prose,
  no script under `scripts/` or the Makefile references it), the per-file header
  survey across `docs/backlog/`, `docs/project/pm-convention.md`, and
  `docs/project/claude_joe_project_instructions.md`.
- Supersedes: nothing; extends the backlog-format convention recorded in
  `pm-convention.md` without changing existing status semantics.
- Status: accepted.

---

## D-0094 — CRD discovery in the kubernetes refresher: spec strings are the group/version/resource form the resolver requires; uninstalled CRDs manifest as a list-time 404, not a resolution error

- Date: 2026-07-13
- Status: accepted
- Session: crd-gvr-resolution
- Decision: the six `crdRefreshSpecs` (`internal/coreagent/crd_refresh.go`) are
  rewritten from the never-resolving `resource.group` form (e.g.
  `scaledobjects.keda.sh`) to the **`group/version/resource`** form
  `k8s.ResolveGVR` requires (`keda.sh/v1alpha1/scaledobjects`,
  `cert-manager.io/v1/certificates`,
  `templates.gatekeeper.sh/v1/constrainttemplates`,
  `cilium.io/v2/ciliumnetworkpolicies`,
  `networking.istio.io/v1beta1/virtualservices`,
  `apiextensions.crossplane.io/v1/composites`). **Bug**: `ResolveGVR`
  (`internal/adapters/k8s/resolve.go:32`) accepts a known short name or a 3-part
  `group/version/resource` split; a `resource.group` string is neither, so every
  CRD list failed at resolution with a plain (non-typed) `fmt.Errorf`, which the
  D-0093 taxonomy's `default` branch swallowed as a tolerant Debug skip
  (`crd_refresh.go`). CRD discovery had therefore **never produced a node** since
  it was written — the unit tests missed it because `fakeK8sAdapter` keys its
  items map by the raw spec string and bypasses `ResolveGVR` entirely.
- Side fixed and why: the **spec strings**, not the resolver. `resource.group`
  carries no version, and the dynamic client addresses
  `/apis/{group}/{version}/{resource}` directly with **no discovery-client
  lookup** to infer one (`internal/adapters/k8s/resources.go:26`), so
  `resource.group` is structurally insufficient — it can never be made to resolve
  statically. `group/version/resource` is the only form carrying enough
  information and is the resolver's documented, tested contract
  (`TestResolveGVR`). `crdShortName` is changed to take the resource segment
  after the final `/` (was: substring before the first `.`), preserving the
  existing node-ID / skip-key / edge-context short names (`scaledobjects`, …)
  unchanged. The set of CRD kinds is unchanged.
- Uninstalled-CRD manifestation and where the silent-skip branch lives: because
  `ResolveGVR` is static string parsing, an uninstalled CRD does **not** fail at
  resolution. It fails at **list time**: the dynamic client hits the unserved
  `/apis/{group}/{version}/{resource}` path, the API server returns a **404
  `StatusError`** (often with no typed reason), and the adapter wraps it `list
  %s: %w`. `apierrors.IsNotFound` recognizes a bare 404 code as not-found (its
  `code == http.StatusNotFound` fallback, unwrapping via `errors.As`), so the
  **existing `case apierrors.IsNotFound(err)` branch** in `refreshCRDSpec` is
  where the silent Debug skip fires — no new branch was needed. Even were a 404
  to arrive without a not-found reason, the `default` branch gives identical
  silent, no-skip, no-abort behavior. **Forbidden** on a CRD list continues to
  join the skip set as degradation, unchanged from D-0093.
- Tests: `TestCRDRefreshSpecsResolveGVR` (new, `crd_refresh_test.go`) pins that
  every `crdRefreshSpecs` entry resolves through `k8s.ResolveGVR` with non-empty
  group/version/resource and that `crdShortName` matches the resolved resource —
  a future spec added in `resource.group` or version-less form fails tests
  instead of silently never listing. `TestRefreshCRDSpec_ForbiddenVsMissing`
  (extended) adds the real unserved-GVR manifestation: a wrapped bare-404
  `StatusError` routes to no-skip. The existing CRD unit tests' fake-adapter keys
  were updated to the new g/v/r strings.
- Scope / deferred: the **core CRD tools** (`internal/tools/core/`:
  `keda_tools.go`, `cilium_tools.go`, `certmanager_tools.go`, `opa_tools.go`,
  `istio_tools.go`, `crossplane_tools.go`, `flux_tools.go`) pass the identical
  `resource.group` form (e.g. `KEDACRDTypes["ScaledObject"] =
  "scaledobjects.keda.sh"`) through `K8sListResources` → `ResolveGVR` and carry
  the **same latent bug** — their on-demand CRD listings have likewise never
  resolved. Fixing that on-demand tool surface (many kinds across seven tool
  files) is out of scope here and deferred to
  `docs/backlog/crd-gvr-resolution.md`.
- Basis: this session's read of `internal/adapters/k8s/resolve.go`,
  `internal/adapters/k8s/resources.go`, `internal/coreagent/crd_refresh.go`,
  `internal/coreagent/k8s_refresh.go` (confirmed `refreshK8sCRDs` is wired into
  the live refresh at line 245), the fake-adapter test harness
  (`k8s_refresh_test.go`), and `k8s.io/apimachinery` v0.35.2 `api/errors`
  (`IsNotFound` returns true for reason `NotFound` OR a non-known-reason 404
  code, unwrapping via `errors.As`). No public-docs claim names CRD-discovered
  kinds (KEDA/cert-manager/gatekeeper/cilium/istio/crossplane), so no docs edit
  was required (D-0052).
- Supersedes: nothing; fixes a latent defect in the CRD arm of D-0093's taxonomy.
  The D-0093 forbidden→degraded / not-found→silent / other→tolerant semantics are
  unchanged; this makes the resolution path they sit on actually reachable.
- Status: accepted.

---

## D-0093 — The kubernetes graph refresher degrades per-resource-type on forbidden, surfacing a third `degraded` component status instead of hard-failing the tick

- Date: 2026-07-13
- Status: accepted
- Session: refresher-rbac-degradation
- Decision: the kubernetes refresher's core-resource loop
  (`internal/coreagent/k8s_refresh.go`) no longer aborts the whole component
  refresh on the first list error. A **forbidden** list error (detected via the
  apimachinery typed-error helper `apierrors.IsForbidden` through error
  unwrapping — never string matching) records a **skip** for that resource type
  and continues to the next; the tick then builds and applies its delta from the
  types it could list. **Forbidden is the only degradation trigger**: any
  non-forbidden list error retains today's semantics exactly — abort the
  component refresh with the wrapped error, apply no delta. The **reconcile
  consequence is accepted and intended**: a now-forbidden type's
  previously-discovered nodes are diffed out, so the graph reflects what the
  credential can currently observe. The CRD path (`refreshCRDSpec`) gains the
  same forbidden→skip behavior while keeping an uninstalled CRD (typed
  `IsNotFound`) a silent Debug skip and every other CRD list error a tolerant
  Debug skip (a CRD transport blip must not fail the tick). A tick that completes
  with a non-empty skip set writes a **new third component status, `degraded`**,
  with `last_error` carrying a concise summary (e.g. `degraded: secrets forbidden
  — credential lacks list permission`); a clean tick writes the healthy status
  and clears `last_error` as before; a genuine abort writes `error` as before.
  The third state reuses the existing `status`/`last_error` columns (no
  migration) via a new `store.UpdateSyncState(status, ...)` seam alongside the
  existing derive-from-error `UpdateSyncStatus`. Per-tick logging for this class
  is **transition-based**: Warn when the skip summary differs from the previously
  persisted `last_error` (first appearance, change, or recovery to clean), Debug
  when unchanged; aborted (non-forbidden) ticks keep their loud ERROR. The
  connectivity-test ("Test Connection") success copy is amended to state its
  scope — it verifies reachability and authentication only; resource-level list
  permissions are exercised by the background refresher, directing the user to
  the component's sync status after the first refresh interval. No permission
  preflight is added in this session. The `register-kubernetes` public guide
  gains a "What permissions Joe needs" section stating as shipped truth that Joe
  works with the built-in `view` ClusterRole (graph fully populated except secret
  nodes, which `view` excludes → degraded status naming the skip), that granting
  `list` on secrets is an explicit opt-in that completes the graph, and that the
  graph records only secret **key names** and object metadata, never secret
  values (per D-0032 the excluded set is described as a delta from `view`, not
  enumerated).
- Scope: **kubernetes refresher only**. The aws, azure, and git refreshers share
  the hard-fail-on-first-error pattern and are **explicitly deferred**
  (`docs/backlog/refresher-rbac-degradation.md`), as are a
  SelfSubjectAccessReview permission preflight in the connectivity test (gated on
  the open `governed-connectivity-check-surface` item), the repo-wide
  source→component terminology sweep of `internal/coreagent` log strings and
  parameter names (D-0021 residue), and a structured per-resource-type skip field
  should the UI later need per-type affordances.
- Basis: this session's read of `internal/coreagent/{k8s_refresh,crd_refresh,refresh}.go`,
  `internal/store/components.go`, `internal/adapters/k8s/resources.go` (confirmed
  `ListResources` wraps with `%w`, preserving the typed error for `errors.As`
  unwrapping — no adapter change needed), migration `001_initial`/`023` (confirmed
  `components.status` is unconstrained `TEXT`, no CHECK), and the UI
  (`ui/src/api/schemas.ts` `status: z.string()` open; `ui/src/lib/constants.ts`
  `STATUS_CONFIG` already carries a `degraded` entry). `k8s.io/apimachinery`
  v0.35.2 is a direct dependency and its `api/errors` provides `IsForbidden`/`IsNotFound`,
  both unwrapping via `errors.As`.
- Supersedes: nothing structurally; refines the graph-refresh read surface's
  partial-tolerance semantics for kubernetes (the refresher previously hard-failed
  any list error).
- Status: accepted.

---

## D-0092 — The operator release procedure is captured as a runbook at `docs/RELEASING.md`

- Date: 2026-07-13
- Status: accepted
- Session: releasing-runbook
- Decision: `docs/RELEASING.md` is added as the maintainer-facing runbook for
  cutting a Joe release — a checklist for a human operator, not a skill or
  agent task, since the load-bearing step (pushing the release tag) is the
  irreversible publish trigger and must stay a deliberate human action. It is
  placed at `docs/` rather than `docs/public` per D-0052 (procedure/how-to
  content is barred from the published surface). Its content is derived
  strictly from the pipeline as committed by `release-pipeline-01`
  (`.goreleaser.yaml`, `.github/workflows/release.yml`, the `goreleaser-build`
  guard in `.github/workflows/tests.yml`) and generalizes the tag-cut
  procedure `release-pipeline-02` already records in
  `docs/backlog/release-pipeline.md`, cross-referencing rather than
  duplicating its update-at-tag-time site list. Volatile specifics (the
  `goos`/`goarch` build matrix, archive/checksum naming) are expressed as
  pointers into `.goreleaser.yaml`, not restated fixed lists, per D-0032.
- Basis: this session's read of `.goreleaser.yaml`, `.github/workflows/release.yml`,
  `.github/workflows/tests.yml`, and `docs/backlog/release-pipeline.md` as
  committed at `16de344` (`release-pipeline-01`); confirmed via `git tag -l`
  (empty) and `release.yml`'s file history (one commit) that no tag has ever
  been pushed and the workflow has never run.
- First-run correction obligation: the runbook is written pre-flight, before
  `release.yml` has ever fired. It carries its own "correct this after the
  first real release" section and must be revisited once `v0.1.0` actually
  publishes, to replace any UNVERIFIED operator-judgment claims (e.g. GitHub/
  goreleaser behavior on partial failure) with what was actually observed.
- Supersedes: nothing — first runbook for the release procedure.
- Status: accepted, pending first-run correction.

---

## D-0091 — GoReleaser is flipped from scaffold-only to publish-on-tag, with a goreleaser-level `before.hooks` guarantee that every invocation stages the real web UI, not the committed placeholder

- Date: 2026-07-13
- Status: accepted
- Session: release-pipeline-01
- Decision: the publish flip D-0036 reserved is taken. `.goreleaser.yaml`'s
  `release.disable: true` is removed (`release.github: {owner: jaimegago, name:
  joe}` set explicitly); a new tag-triggered workflow
  (`.github/workflows/release.yml`) runs `goreleaser release --clean` only on
  a `v`-prefixed tag push (`on.push.tags: ['v*']`), with `permissions:
  contents: write` and a full-history checkout (`fetch-depth: 0`), pinned to
  the same goreleaser version (`~> v2`) as the existing snapshot job. The
  ldflags injection into `internal/buildinfo` (D-0036) is untouched. Semver
  and **v0.1.0 as the launch version** stand, with the operator performing the
  tag push (no CLI-tooling change) and CI doing the publish. The work is split
  across two sessions, as reserved: this session (`release-pipeline-01`) arms
  the pipeline and proves it; a second session (`release-pipeline-02`) sweeps
  the update-at-tag-time copy sites and cuts the `v0.1.0` tag on its own
  commit — tracked in `docs/backlog/release-pipeline.md`. This session creates
  no tag and publishes no release.

  **UI-staging guarantee.** The real web UI must be embedded in every
  published binary, not the committed placeholder (`internal/webui/dist/
  .gitkeep`). Rather than making this a property of one workflow step, it is
  made a structural property of `.goreleaser.yaml` itself: a new
  `before.hooks` entry, `scripts/stage-ui-for-release.sh`, runs `npm ci && npm
  run build` in `ui/` and copies the output into `internal/webui/dist` — the
  same source/dest the Makefile's `build-ui` target uses, keeping
  `internal/webui/contract_test.go`'s `TestEmbedSourceMatchesViteOutDir`
  invariant intact. Because this hook runs before every goreleaser build,
  it applies uniformly to the tag-triggered release, a local `goreleaser
  release`/`build`, and the CI snapshot job (`goreleaser-build` in
  `.github/workflows/tests.yml`) — with no separate workflow-only staging
  step to drift out of sync. A consequence taken deliberately: the
  `goreleaser-build` snapshot job now needs Node (added `actions/setup-node`)
  and is no longer part of the Node-free/placeholder-compiling set; the unit,
  integration, and lint jobs remain Node-free and placeholder-compiling as
  before, since faithful release-path validation is exactly this job's
  purpose.

  **Proof.** The snapshot build was run through this `before.hooks` path in
  CI; the booted binary's `GET /api/v1/version` `ui_digest` was compared
  against an independently computed `buildinfo.Compute` digest over the
  staged `internal/webui/dist` (via the new `scripts/verify-ui-digest`
  command) and against a digest computed over a placeholder-only directory
  (just `.gitkeep`) — the booted digest must equal the former and differ from
  the latter. This assertion is wired as a permanent CI guard step in the
  `goreleaser-build` job, so a regression to placeholder-embedding fails CI
  rather than only being caught by manual inspection.

  **Docs.** The must-update-now posture sites identified in this session's
  Phase 1 (CLAUDE.md License posture and Build sections, `.goreleaser.yaml`'s
  own header comments, the `goreleaser-build` CI job comment,
  `docs/backlog/build-version-instrumentation.md`'s flip item,
  `docs/backlog/launch-positioning-and-employer-decoupling.md`, and
  `docs/backlog/public-docs-feature-inventory.md`'s install/build section)
  are updated to the now-true state: pipeline armed, publishes on tag, no
  release tagged yet. `docs/public/install-and-build/_index.md` is edited only
  for the "deliberately configured not to publish" clause that becomes false
  once armed — the "no published release binaries" factual statement itself
  is untouched, staying true until the tag (release-pipeline-02's edit,
  tracked in `docs/backlog/release-pipeline.md`). A new Install and Build /
  Distribution entry is added to `docs/project/SITE-CLAIMS.md` in this same
  session per the D-0077/D-0088 bidirectional register obligation, since this
  session publishes new load-bearing copy to that page.

  **Doc-footer version stamping** stays deferred per D-0052; its re-open
  condition (the first post-launch release) is unchanged and restated in
  `docs/backlog/release-pipeline.md` — it becomes actionable once
  `release-pipeline-02` cuts `v0.1.0`, not before.
- Basis: `.goreleaser.yaml` (`before.hooks`, `release` block);
  `scripts/stage-ui-for-release.sh`; `scripts/verify-ui-digest/main.go`;
  `.github/workflows/release.yml`; the `goreleaser-build` job changes and its
  new UI-digest CI guard step in `.github/workflows/tests.yml`; the doc edits
  enumerated above; all under the `release-pipeline-01` commit.
- Supersedes: nothing new — this is the flip D-0036 explicitly reserved as a
  future posture change with its own decision entry; D-0036 itself stands
  unchanged (the build-truth/ldflags/`ui_digest` design it recorded is
  untouched).
- Status: active. `release-pipeline-02` (tag cut + update-at-tag-time doc
  sweep) remains open, tracked in `docs/backlog/release-pipeline.md`.

---

## D-0090 — A second git-history rewrite scrubbed the former-employer email address and the employer acronym from object *content* (commit messages, blobs, and one tree path), correcting the residual leak that D-0089's identity-only pass left behind; the operator performs the force-push

- Date: 2026-07-13
- Status: accepted (implemented)
- Session: history-scrub-02
- Decision: D-0089 rewrote only author/committer *identity metadata*, but that same session's own documentation reintroduced the association in object *content* — the former-employer email address and the three-letter employer acronym appeared in a commit message, in this log's D-0089 entry, and in a tracked backlog file whose filename itself carried the acronym. This second pass removes every such content occurrence across all of history. A single `git-filter-repo` invocation combined `--replace-text` and `--replace-message` (one identical four-rule expression set) with a `--path-rename`, run with `--prune-empty never --prune-degenerate never` so no commit was dropped and the history stays unsquashed. The four rules, applied in order: (1) a targeted regex fixing the one sentence where bare-token substitution would read redundantly; (2) the former-employer email address → a neutral descriptive phrase; (3) the old backlog slug → `launch-positioning-and-employer-decoupling`, kept in sync with the path rename so in-content links stay valid; (4) a word-boundary, case-sensitive regex over the uppercase three-letter employer acronym → `former-employer`, proven not to touch the classified false positives (the code-review approval-keyword literals and two `go.sum` module-hash substrings, where the acronym is embedded rather than standalone). The renamed file is `docs/backlog/launch-positioning-and-employer-decoupling.md`. The **operator performs the force-push**; this session performed no push. A fresh full mirror backup was taken before the rewrite and the tool's default `origin` removal was reversed (remote re-added, not pushed).
- Basis: `history-scrub-02` session, two-phase (read-only classified audit with a hard stop, then approved execution); all figures are measured command output. **Backup:** `git clone --mirror` at `/Users/jaimegago/joe-launch-archive/joe-prescrub-02-backup.git` (4.8M, `refs/heads/main` = `2c1e962`, main count 488, all-refs 488) — taken before any rewrite. A `--dry-run` was validated before the real run and produced the same numbers. **Before/after evidence:** HEAD `2c1e962` → `1c91b3b`; **main commit count 488 → 488** (unchanged, unsquashed); **all-refs 488 → 488** (no divergent auxiliary ref this pass). Post-rewrite verification across all refs: the former-employer email address **0** occurrences in blobs, in commit messages, and in author/committer metadata; the standalone employer acronym **0** occurrences in blobs, messages, tree paths, and ref names; the old backlog slug **0**. Every remaining case-insensitive hit of that three-letter sequence (23 distinct lines) was matched byte-for-byte to a Phase-1-classified false positive: the code-review approval-keyword literals (79 occurrences, unchanged) and the two `go.sum` module-hash substrings (15 + 15 occurrences, unchanged).
- Supersedes: nothing. **Completes** D-0089 by closing the content-side residual its identity-only pass left behind; D-0089 remains the record of the first (metadata + binary-blob) pass and its own figures are unchanged. No security/safety invariant is touched — this session rewrote git message, blob, and path content only; no code, migration, or product behavior changed, and no `docs/project/SITE-CLAIMS.md` mechanism was affected. Note that the **first** backup `/Users/jaimegago/joe-launch-archive/joe-prescrub-backup.git` still holds the pre-D-0089 identity metadata; it is a separate, unreachable repository, and its retention or destruction is an operator decision. Post-push coordination (re-sync of every other working copy and of the claude.ai project-knowledge files) is owned by the operator and is not a code change.

---

## D-0089 — The pre-launch git-history scrub was executed: the three former-employer-email commits were rewritten to the personal email and the old compiled-binary blobs were purged from history, with the full commit history preserved unsquashed; the operator performs the force-push

- Date: 2026-07-13
- Status: accepted (implemented)
- Session: history-scrub
- Decision: Joe's git history was rewritten once, in place, to remove the two pre-publish blockers named in `docs/backlog/launch-positioning-and-employer-decoupling.md`: (1) the **three 2026-02-12 commits** whose author **and** committer email was a former-employer email address (`512de26` "fix: improve http api", `f52df74` "fix: tests", `8bdf832` "fix: lint, tests") were rewritten to `gagojaime@gmail.com` under the same display name; and (2) the **old compiled-binary blobs** `joe`/`joecored` were purged from every historical tree. Under **Option 1** (scoped): only the former-employer email was rewritten — the GitHub `noreply@github.com` committer identity on merge/web commits and the `dependabot[bot]` authorship were deliberately **left untouched**, since they are not a former-employer leak and rewriting them would misrepresent provenance. The rewrite used **`git-filter-repo`** with a single invocation combining `--mailmap` (former-employer→personal only), `--strip-blobs-with-ids` over the four verified binary blob SHAs, and `--prune-empty never --prune-degenerate never` so **no commit was dropped and the history was not squashed** — including the commit whose only content was the binary removal (`chore: remove binary`), which is retained as an empty commit. The **operator performs the force-push**; this session performed no push. A full mirror backup was taken before the rewrite and the tool's default `origin`-removal was reversed (remote re-added, not pushed).
- Basis: `history-scrub` session, two-phase (read-only verification with a hard stop, then approved execution), all figures below are measured command output from this session. **git-filter-repo** version `a40bce548d2c`; its fresh-clone-abort, default `origin` removal, `--mailmap` format, `--strip-blobs-with-ids`, and `--prune-empty`/`--prune-degenerate` defaults were confirmed from its own `--help` before use. **Backup:** `git clone --mirror` at `/Users/jaimegago/joe-launch-archive/joe-prescrub-backup.git` (64M, `refs/heads/main` = `984f1d9`, main count 487, all-refs 488, three former-employer commits present) — taken before any rewrite. **Before/after evidence:** HEAD `984f1d9` → `03b430d`; **HEAD tree hash unchanged** at `badecc92b97103730a47f159c3b5057b846de164` (author-email rewrites and binary purges touch no tree the working set reads); **main commit count 487 → 487** (unchanged, unsquashed); commits with a former-employer email address in author or committer across all refs **3 → 0**; the four binary blobs (`31b5139`, `738eba8` = `joe`; `864ab46` = `joecored`; `96ce679` = `joe`; 74,248,952 bytes ≈ 70.8 MiB uncompressed) **absent from every historical tree** after; `.git` on disk **64M → 5.0M** (pack 35.82 MiB → 4.50 MiB). The all-refs count moved 488 → 487 (**−1**): this is confined to a stale auxiliary branch, not `main` — pre-rewrite `refs/heads/feature/phase-2-single-agentic-runtime` (`bdef59f`) and `refs/remotes/origin/feature/phase-2-single-agentic-runtime` (`784d8bd`) had diverged by one commit each, and git-filter-repo's default remap of `refs/remotes/origin/*` onto `refs/heads/*` collapsed the two same-named branches onto one head, dropping the divergent tip `784d8bd` ("refactor(phase-2): relocate loop to internal/agentloop; record D-0003; mark Phase 2 complete — Change 7"); that commit is not on `main`, is not a former-employer commit or a binary, and survives both on GitHub `origin` and in the mirror backup.
- Supersedes: nothing. **Closes** the "History scrub" launch blocker tracked in `docs/backlog/launch-positioning-and-employer-decoupling.md` (that file is updated to mark the item resolved and stays open for its remaining OASIS/positioning items). No security/safety invariant is touched — this session rewrote git metadata and historical blobs only; no code, migration, or product behavior changed, and no `docs/project/SITE-CLAIMS.md` mechanism was affected. Post-push coordination (re-sync of claude.ai project-knowledge files, discard/re-clone of every other working copy, and re-pinning the Joe source reference in `joe-oasis-e2e`) is owned by the operator and is not a code change.

---

## D-0088 — The Site Claims Register gains Configuration and Operations sections for the D-0085 and D-0087 published claims, and the D-0077 register-maintenance obligation is extended to be bidirectional (mechanism changes flag copy; newly published load-bearing claims add register entries)

- Date: 2026-07-13
- Status: accepted (implemented)
- Session: site-claims-refresh
- Decision: This session **changes no mechanism and touches no code, migration, or UI file** — it closes a register gap and a convention gap. (1) `docs/project/SITE-CLAIMS.md` gains two new sections carrying load-bearing joeagent.dev copy that two prior sessions published but the register did not track. A **Configuration** section adds one entry: SQLite is the supported store and the `pgx` (PostgreSQL) value is present but not operational (mechanism: driver-parameterized `store.New`/`sql.Open` plus the `Store.Migrate` driver branch, standing over a SQLite-dialect-locked embedded migration set; pinned on the SQLite side by `TestMigration009_SchemaSQLite`, with the Postgres half `TestMigration009_SchemaPostgres` recorded as env-gated on `JOE_TEST_POSTGRES_DSN` and CI-skipped — an unpinned latency recorded per the MCP-entry precedent; mechanism-bound, revises when `docs/backlog/postgres-backend-completion.md` lands). An **Operations** section adds three entries: the session retention sweeper's defaults (on-by-default, trash-grace-purge on, inactivity-expiry off, terminal action trash-then-purge/archive, zero-grace footgun — seed row pinned by `TestMigration026_027_RetentionAndAuditKind`, behaviors by the `TestSweep_*` family, with the 1h default interval cadence recorded as **none-yet** since it is a code default exercised on a fixed clock, not directly asserted); the audit-log-grows-unbounded-by-design claim (pinned by `TestRepositoryAPISurface_AppendOnly`, `TestMigration015_TriggerBlocksUpdate`, `TestMigration015_TriggerBlocksDelete`); and the broader unbounded-tables growth posture, described structurally per D-0032 without pinning a fixed table count as authoritative (no single break-test; None). (2) The **D-0077 register-maintenance obligation is extended to run both directions**: in addition to the existing duty that a session changing a listed mechanism flag a site revision, a session that publishes a new load-bearing claim to a joeagent.dev publication source now adds the corresponding register entry in the same session. The amendment is recorded where the obligation lives — `CLAUDE.md`'s Reference Documents entry for SITE-CLAIMS.md — and mirrored in the register's own header intro and Conventions block. D-0077 itself is **left unedited** as append-only history.
- Basis: `site-claims-refresh` session, Phase-1 read-only verification against the live tree before any edit: the Configuration claim exists as described in `docs/public/configuration/_index.md` (the `database.driver` row and the "PostgreSQL is not yet functional" note) and the Operations "Retention and growth" section exists in `docs/public/operations/_index.md`, both as published by D-0085 and D-0087; the env-gated Postgres migration handle is `TestMigration009_SchemaPostgres` in `internal/sessionmodel/schema_test.go`, `t.Skip`-ing when `JOE_TEST_POSTGRES_DSN` is unset (SQLite side `TestMigration009_SchemaSQLite`); the migration runner's driver branch is `migratePostgres.WithInstance`/`migrateSQLite.WithInstance` in `internal/store/store.go`; the retention seed row and its defaults are asserted by `TestMigration026_027_RetentionAndAuditKind` (`internal/store/migrations_026_027_test.go`: inactivity NULL/OFF, trash-grace 30, terminal `trash_then_purge`, single-row CHECK); the sweeper behaviors are pinned by the `TestSweep_*` family in `internal/sessionsweeper/sweeper_test.go` while the 1h interval is a `sweeper.go` code default not directly test-asserted; the audit append-only handles are `TestRepositoryAPISurface_AppendOnly` and `TestMigration015_TriggerBlocksUpdate`/`Delete`; and the D-0077 obligation wording lives in `CLAUDE.md`'s Reference Documents section. `go build ./...` unaffected (no code touched).
- Supersedes: nothing. It **extends** the D-0077 register-maintenance obligation from one-directional (mechanism change → flag copy) to bidirectional (also: new published claim → add entry), without editing the append-only D-0077 record. No security/safety invariant is touched — this session documents and cross-references shipped posture only. The register additions are derived pointers to already-published copy, not new site claims, so this session triggers no further joeagent.dev revision of its own.

---

## D-0087 — Joe's data-retention and unbounded-growth posture is ratified as shipped: session retention is sweeper-enforced with trash-grace-on / inactivity-off defaults, and the audit log, LLM usage, review jobs, and clarifications grow unbounded in v1 by design; the whole-DB retention story is deferred to a backlog item

- Date: 2026-07-13
- Status: accepted (implemented)
- Session: db-retention-story
- Decision: This session **documents shipped behavior and changes none of it** — no code, migration, or UI file is touched. It ratifies Joe's data lifecycle posture and makes it operator-facing. (1) **Session retention is sweeper-enforced and on by default.** A boot-started retention sweeper (`internal/sessionsweeper`, wired in `cmd/joe/server.go` under the boot-minted service principal `svc:sweeper:sessions`) runs on a fixed interval (default 1h) whenever the session store is wired, applying the single install-wide retention policy (migration 026, one row, seeded: inactivity `NULL`/OFF, trash-grace 30 days, terminal action `trash_then_purge`) and draining abandoned `auth_login_flows`. **Trash-grace auto-purge is ON by default**: a trashed session is stamped `purge_after = trashed_at + trash-grace` and hard-purged once past it, with `chat_messages` removed by `ON DELETE CASCADE`. **Inactivity expiry is OFF by default** (opt-in, regulated-posture default). The **archive** terminal action moves an expired session into the `~/.joe/session-archive` directory as a versioned artifact; with no archive directory wired it leaves the session active and logs, never falsely archived. Every sweep effect is coupled to its audit row in one transaction. (2) A **stated footgun**: `trash_grace_days = 0` (or a nil/unresolvable policy at trash time) leaves `purge_after` NULL, and since `ListPurgeableSessions` only selects rows with `purge_after IS NOT NULL`, such sessions persist in trash indefinitely until an admin purges them manually — intended semantics (zero grace = never auto-purge), now documented as something an operator must know. (3) **Four table classes grow unbounded in v1 by design**: `audit_log` (deletion structurally forbidden — insert-only repository plus DB `BEFORE UPDATE`/`BEFORE DELETE` triggers; the only sanctioned space-reclamation is drop-and-remigrate via the Phase F down migration per D-0009, which discards all history), and `llm_usage` (one row per LLM call), `review_jobs`, and `clarifications` (no prune path). The legacy migration-001 `sessions`/`session_messages` tables are **frozen** (no live writer, no delete path, one dormant reader); `graph_edges` self-reconciles and is not unbounded. (4) The whole-DB retention work — audit rotation v2 (the insert-rotate-only repository extension named in D-0009), an `llm_usage` retention/roll-up, a `review_jobs`/`clarifications` disposition, and a DB-size observability signal for operators — is **deferred to `docs/backlog/db-retention-story.md`**, cross-referencing `learn-from-sessions-fate` (decided: the legacy tables must be retained, not dropped) for the legacy-table disposition. The operator-facing posture is now documented in a new "Retention and growth" section of `docs/public/operations/_index.md`, and its "State on disk" section is amended to note that Joe deletes some data on a timer by default (backups are the only recovery for a purged session) and that the session-archive directory is written by the sweeper's archive terminal action. Explanation-only tone and no restated numeric defaults on the public page, per D-0052 and the page's own convention; no volatile counts committed, per D-0032.
- Basis: `db-retention-story` session, Phase-1 read-only re-verification against the live tree before any edit, every core claim held: the sweeper's boot wiring and `svc:sweeper:sessions` principal (`cmd/joe/server.go`), its 1h default interval and the three policy defaults (`internal/store/migrations/026_*.up.sql` seed row: `inactivity_days NULL`, `trash_grace_days 30`, `terminal_action 'trash_then_purge'`); the footgun path (`trashGraceDeadline` returning nil for `TrashGraceDays <= 0` in both `internal/sessionsweeper/sweeper.go` and `internal/api/webui.go`, and `ListPurgeableSessions`' `purge_after IS NOT NULL` predicate in `internal/sessionmodel/lifecycle.go`); the `chat_messages` cascade (migration 022 `ON DELETE CASCADE` to `agent_sessions`); zero production delete-sites for `audit_log`/`llm_usage`/`review_jobs`/`clarifications` (grep of `internal/`, excluding tests and migrations) and the `audit_log` append-only triggers (migration 015); the frozen legacy tables (`internal/store/sessions.go` holds the only inserts, wired into `store.Store.Sessions` but with no live caller of `Create`/`AddMessage`; one dormant reader in the orphaned learn-from-sessions extractor); the "State on disk" content living only in `docs/public/operations/_index.md` (not in `docs/operations.md`); the audit no-retention-in-v1 posture recorded by **D-0009** (Identity Phase F, "No retention/rotation in v1 … drop … via the Phase F down migration … insert-rotate-only repository is a clean v2 extension"); and INDEX.md carrying no prior whole-DB retention item while `learn-from-sessions-fate` (decided) governs the legacy tables' disposition. Post-write `go build ./...` unaffected (no code touched). CLAUDE.md was checked and carries **no** retention/sweeper/growth claim, so it needed no edit.
- Supersedes: nothing. It **records** the posture that D-0009's "no retention/rotation in v1" fence established for the audit log and generalizes it to the other unbounded tables, and it defers the extension work D-0009 already named (the insert-rotate-only v2 repository) to a tracked backlog item. No security/safety invariant is changed — the `audit_log` append-only invariant, the write floor (D-0018), and denial precedence (D-0022) are documentation subjects here, unchanged in substance. Because `docs/public` is the joeagent.dev publication source, the new operations content is a site-revision flag for a separate site-side republish.

---

## D-0086 — External contributions are accepted via fork-and-PR, with no CLA/DCO, inline conduct expectations, and a repo-root CONTRIBUTING.md whose safety-invariants section is the enforceable contract

- Date: 2026-07-13
- Status: accepted (implemented)
- Session: contributing-guide
- Decision: Joe now accepts external contributions through the standard fork-and-pull-request flow against `main`. No CLA and no DCO sign-off are required; license terms are inbound-equals-outbound under the existing Apache-2.0 license. Conduct expectations are stated inline in `CONTRIBUTING.md` rather than in a separate `CODE_OF_CONDUCT.md` file. The project-management spine recorded in `docs/project/pm-convention.md` (chat/Claude-Code/commit/decision-log slug convention) stays maintainer-side and is explicitly not imposed on contributors — a PR need not follow it. The new repo-root `CONTRIBUTING.md` states a "before you start" norm (open an issue before non-trivial work; `docs/project/DECISIONS.md` is normative context to read, not to PR against or append to — the log stays maintainer-only) and names the five safety invariants a contribution must not violate: the boot-resolved/runtime-immutable write floor with the observation-mode default (`internal/safety/floor.go`, D-0018, D-0073), the fixed floor-then-incident-then-RBAC denial precedence (`internal/tools/executor.go`, D-0022), the binary Read/Mutate author-time tool classification with fail-closed unknown-tool defaulting (`internal/safety/tier.go`, pinned by `TestClassifyTool_UnknownDefaultIsMutate`), the no-kubeconfig-ingestion Kubernetes transport invariant (`internal/adapters/k8s/`, pinned by `TestTransport_NoKubeconfigIngestion`, `TestCredentialPackage_NoKubeconfigIngestion`, `TestNoClientcmdOutsideAllowedAdapters`, `TestTransport_NoForbiddenAuthMechanisms`), and the MCP-server-only / no-MCP-client stance (D-0067) — the last one named honestly as a recorded decision without an automated guard test yet, since `docs/backlog/mcp-client-absence-guard.md` remains open. The document also states the toolchain version, the three-step frontend rebuild discipline forced by the `go:embed` UI (build UI, rebuild binary, restart process — a stale-running-process trap, not just a stale-file one), and the verified test commands, all re-derived from the live tree rather than copied from memory. `README.md` gained a single "Contributing" section linking to the new file, placed directly after the existing "Development and testing" section, without restating any of its content. No product code changed.
- Basis: `contributing-guide` session, Phase-1 read-only re-derivation against the live tree before any file was written: confirmed absence of any existing `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, or `.github/` PR/issue-template file; `LICENSE:1-2` (Apache-2.0); `go.mod:3` (`go 1.25.0`); `Makefile:41-56` (`build` → `build-joe` → `build-ui`, the `internal/webui/dist` embed staging target); `Makefile:58-59` and `README.md:174,179` (`go test ./...`, `go test -tags=integration ./...`); `test/README.md` (exists, defers to CLAUDE.md as test authority); `internal/safety/floor.go` + D-0073 (write-floor boot resolution and observation default, pinning tests `TestResolveWriteFloor_Precedence`, `TestWriteFloor_NoRuntimeLoweringPath`); `internal/tools/executor.go:201-216` + D-0022 (denial precedence, floor checked first); `internal/safety/tier.go` + `internal/safety/tier_test.go:101` (`TestClassifyTool_UnknownDefaultIsMutate`); `internal/adapters/k8s/transport_break_test.go` (the four named break tests); D-0067 (MCP-client categorical rejection) cross-checked against `docs/backlog/mcp-client-absence-guard.md` (status: open — no pinning test exists, confirmed by a repo-wide grep finding no MCP-client-absence test) and against a grep of `go.mod`/test files finding no such guard; grep of `README.md` and `CLAUDE.md` for "contribut" (no existing mentions, so nothing was duplicated) and a full read of `docs/project/pm-convention.md` (silent on external contributions, entirely maintainer-workflow-scoped). No mechanism listed in `docs/project/SITE-CLAIMS.md` was touched — this session added no `joeagent.dev`-published claim.
- Supersedes: nothing. No security/safety invariant is changed in substance — the write floor (D-0018), denial precedence (D-0022), the observation boot default (D-0073), the no-kubeconfig invariant (D-0062/D-0064), and the MCP-client rejection (D-0067) are all documentation subjects here, restated for a new audience, not altered.

---

## D-0085 — Joe's PostgreSQL (pgx) backend is LATENT, not shipped; the public and reference docs that presented it as supported are corrected, and completion is deferred to a backlog item rather than fixing the migration set now

- Date: 2026-07-13
- Status: accepted (implemented)
- Session: postgres-backend-truth
- Decision: PostgreSQL backend support is **latent, not functional**. The configuration surface accepts `database.driver: "pgx"`, `pgx` is a direct dependency, `store.New` opens the configured driver via a parameterized `sql.Open`, the repositories are dialect-aware through the placeholder rewriter, and the migration runner (`internal/store/store.go`, `Store.Migrate`) has a PostgreSQL branch (`migratePostgres.WithInstance`) — but the embedded migration SQL is **SQLite-dialect-locked**, so setting the driver to `pgx` fails at `Store.Migrate()` before serving. This session records the honest state and **walks back the public claims**, choosing the honest-wording fix over completing the migrations because it removes the shipped-truth violation **without touching the live migration chain or the break-tested `audit_log` append-only invariant**. Three corrections landed: (1) `docs/public/configuration/_index.md` now states SQLite is the supported database and that the `pgx` value is present but not yet operational — the driver/dsn rows are **annotated, not deleted**, and a note names the two blockers (SQLite-only `AUTOINCREMENT`; SQLite-specific append-only trigger DDL) and that startup fails at the migration step, with a single planned-support sentence per the D-0052 explanation-only tone; (2) `docs/reference/joe-architecture.md`'s dual-driver migration claim is rewritten to describe the actual state — runner branch and dialect-aware repositories present, migration SQL SQLite-locked — naming the six blocking migrations (`001`, `006`, `015`, `018`, `020`, `027`) and the two blockers; (3) completion is captured in `docs/backlog/postgres-backend-completion.md` (dialect-portable rewrites of the six migrations, PostgreSQL-native append-only enforcement preserving the dual-enforcement invariant, driver-value validation in `internal/config/validation.go` — currently absent, any string reaches `sql.Open` — a decision on whether a `JOE_DATABASE_DRIVER` override should exist since only the DSN has one today, and a CI job running the migration set against real PostgreSQL to un-gate the `JOE_TEST_POSTGRES_DSN`-gated cross-driver test). No CLAUDE.md change was needed (it carries no PostgreSQL-backend claim). **No code or migration files were modified.**
- Basis: `postgres-backend-truth` session, Phase-1 read-only re-derivation against the live tree, all checks held before any edit: the `pgx` claim in `docs/public/configuration/_index.md` (lines ~96, ~164) and the "supporting both SQLite and PostgreSQL drivers" claim in `docs/reference/joe-architecture.md` (~line 401) existed as described; `INTEGER PRIMARY KEY AUTOINCREMENT` is present in migration `001` (and `006`, `015`, `018`, `020`, `027`) and the SQLite append-only trigger syntax (`CREATE TRIGGER … BEGIN SELECT RAISE(ABORT, …); END`) is present in `015_audit_log.up.sql`; the cross-driver Postgres migration test is env-gated on `JOE_TEST_POSTGRES_DSN` and `t.Skip`s when unset (`internal/sessionmodel/schema_test.go`); the migration runner's PostgreSQL branch is `migratePostgres.WithInstance` in `internal/store/store.go`; and a grep of CLAUDE.md and the rest of `docs/public` surfaced no other Joe-storage-backend PostgreSQL overclaim (the remaining `postgres`/`PostgreSQL` hits — `docs/public/api-reference/_index.md`, `docs/public/components/connectable-systems.md`, and the `postgresql` adapter mentions in `joe-architecture.md`) describe PostgreSQL as a **connectable component Joe manages**, not Joe's own datastore, and are accurate.
- Supersedes: nothing structurally. **Corrects** the prior public and reference prose that presented the `pgx` backend as supported; no decision is reversed. No security/safety invariant is touched — the write floor (D-0018), denial precedence (D-0022), the observation boot default (D-0073), and the `audit_log` dual-enforcement append-only invariant are all documentation subjects here, unchanged in substance. Because `docs/public` is the joeagent.dev publication source, the reworded configuration page is a site-revision flag for a separate site-side republish.

---

## D-0084 — The root README.md was rewritten from scratch, grounded in a claim-by-claim audit of the live tree, and re-anchored on the self-hosted open-source AI-agent framing

- Date: 2026-07-12
- Status: accepted (implemented)
- Session: readme-rewrite
- Decision: The repo-root `README.md` is **replaced wholesale** with a twelve-section landing page (hero; what Joe does; governed by construction; quick start; interfaces; access control; skills; components; knowledge store; development and testing; documentation; license), every command, flag, env var, route, and path re-verified against the live tree before writing. The **structural choices**: the old **project-structure directory tree**, the **three-tier safety tool table**, and the **adapter catalog** are **dropped entirely** rather than updated, per the D-0032 no-volatile-counts rule (a directory tree, a tier count, and an enumerated adapter list are all growth-driven volatile references); the README now points at [`internal/adapters/`](../../internal/adapters/) for the supported set instead of enumerating it. **Tier vocabulary is removed**: the retired T1/T2/T3 / Observe/Record/Act scheme (collapsed by D-0020) appears nowhere; the safety section is rewritten on the binary Read/Mutate action model (classified at author time, fail-closed on unknown tools, denial precedence write floor → incident → RBAC). **Component terminology** replaces "source" throughout, per D-0021. The **observation-default** boot posture (`JOE_MODE` unset → write floor up, read-only; `JOE_MODE=full` refused; unknown values refused fail-closed) is stated per D-0073, and the **no-kubeconfig** Kubernetes credential model (`api_server` + inline `ca_data` + `auth_method`) per D-0062. The hero adopts the D-0070 self-hosted open-source AI-agent framing; "copilot" and "HTTP daemon" wording are gone. The RBAC section is rewritten to the write-RBAC-plus-read-posture model with the admin surface described as admin-gated and audited — replacing the stale "all admin endpoints require Bearer auth" line D-0013 flagged.
- Basis: `readme-rewrite` session, Phase-2 write pass grounded in the Phase-1 audit and re-verified against the live tree: `go.mod` (`go 1.25.0`); `internal/env/keys.go` (`ResolveBootMode`, `JOE_MODE` observation/full/fail-closed); `internal/config/validation.go` (one-key-required, both-present-defaults-to-Claude, `JOE_LLM_PROVIDER`/`JOE_LLM_MODEL`); `cmd/joe/main.go` (subcommand dispatch: `mcp`, `slack`, `skills`, `incident`, `panic`, `unlock` — no `review`; skills sub-verbs `install`/`list`/`remove`/`update`/`approve`/`reject`/`reload`); `internal/api/components.go` + `internal/api/server.go` (`POST /api/v1/components` credential-less registration body `{id,type,name,config}`, `POST /api/v1/components/{id}/promote` admin-gated arm); `internal/readposture/readposture.go` (`team_flat` launch default, `zoned` opt-in); `Makefile` (`make build` = UI build + embed + `ldflags -X`); and the presence of every linked doc under `docs/` and `test/README.md`. CLAUDE.md was checked and carries **no** reference to README content (it needed no edit).
- Supersedes: nothing structurally, but **closes the stale-README follow-up D-0013 tracked as deferred** ("flagged for the later combined README rewrite pass, not edited here") — the RBAC-and-audit README copy D-0013 called stale is now rewritten. No security/safety invariant is touched — the write floor (D-0018), denial precedence (D-0022), and the observation boot default (D-0073) are documentation subjects here, unchanged in substance.

---

## D-0083 — The component read-model invariant now covers every component serialization path, including the POST create 201 echo; the create handler projects through componentView rather than echoing the raw store.Component

- Date: 2026-07-12
- Status: accepted (implemented)
- Session: create-echo-readmodel
- Decision: The A002 read-model invariant — that no authenticated caller receives a component's raw `store.Component` `Config` blob or any credential locator on a component-returning endpoint — is extended to the **last serialization path that still echoed the raw struct**: the `POST /api/v1/components` `201` create response. `handleCreateComponent` (`internal/api/components.go`) now serializes `newComponentView(source)` — the **same** `componentView` projection the list and get read endpoints already return — instead of `writeJSON(w, http.StatusCreated, source)` over the raw `*store.Component`. The echoed 201 body therefore carries only the read-model shape (`id`, `type`, `name`, `status`, timestamps, and the server-derived `armed`/`provider` projection) and never the `Config` blob or any credential-locator key. Because a registration is credential-less by construction, the echoed component is inert (`armed=false`, no provider), but any non-credential routing config an operator submitted still lives in `source.Config` and is now omitted from the echo exactly as it is from a read. No other handler behavior changes: registration remains admin-gated, probe-free, and credential-less; only the response projection changed. The read-model pin (`internal/api/components_readmodel_test.go`) is extended so the existing forbidden-key list and `assertNoLocatorKeys` helper that cover GET-by-id and GET-list now also cover the create 201 body, registering a component whose submitted config carries locator-shaped keys that pass the credential-less-at-registration guard and asserting none appear in the echo.
- Basis: `create-echo-readmodel` session, Phase-1 read-only verification against the live tree. Before the fix, `handleCreateComponent` serialized the raw `*store.Component` at `internal/api/components.go:287` (`writeJSON(w, http.StatusCreated, source)`), and `store.Component` carries the `Config` `json.RawMessage`, so the 201 echoed the raw blob — the one component serialization path not yet projecting. `componentView` (`components.go:56-67`) and `newComponentView` (`components.go:73-90`) already existed and omit `Config`, deriving `armed`/`provider` from the in-hand config via `armedState`; both read endpoints already consumed them (`handleListComponents` `components.go:101`, `handleGetComponent` `components.go:361`). The pre-existing pin asserted the forbidden-key absence for GET-by-id and GET-list only (`components_readmodel_test.go`, GET at line 61, LIST at line 86) with no create-response assertion. Implementation basis: the one-line projection change at the create handler's echo and the added `TestComponentReadModel_CreateEchoHidesLocators` reusing the existing `forbiddenReadKeys`/`assertNoLocatorKeys`. `go build ./...`, `go vet ./...`, `gofmt`, and `go test ./...` pass.
- Supersedes: nothing. **Completes** the A002 read-model closure by bringing the create echo under the same invariant the list and get endpoints already satisfied — it does not revise the read-model shape, `componentView`, or `armedState`, and touches no security/safety invariant (the write floor D-0018, denial precedence D-0022, and observation boot default D-0073 are unchanged). CLAUDE.md documents no component read-model invariant (verified: no matching prose), so it was left untouched. A separate residual verified in the same session — that the name-based `RejectCredentialFields` guard does not cover datastore secret field names (`uri`/`password`/`api_key`), so those register into the encrypted `config` blob — was **not** fixed here; it was rehomed into `docs/backlog/datastore-uri-credential-provider.md` (gated on that item's URI-shaped provider), which is now the deleted GitHub issue's sole home for the finding.

---

## D-0082 — The external-collaboration Mutate tools stay registered at launch; deregistering to an empty Mutate set was considered and rejected because the write floor's end-to-end denial of live, classified-Mutate tools is a demonstrated gate proof

- Date: 2026-07-12
- Status: accepted (implemented)
- Session: observation-wording-triage
- Decision: The shipped external-collaboration mutation surface (commenting on a GitHub pull request or GitLab merge request, submitting a GitHub request-changes review, and publishing an already-approved documentation proposal to Confluence, Notion, or a Git repository — the D-0069 inventory) **remains registered on the human-facing task loop at launch**, even though every currently bootable configuration has the write floor up and therefore denies the entire Mutate class. The alternative — **deregistering these tools to achieve a zero-registered-Mutates absence claim** and re-registering them when full mode is built — was considered and **rejected**. Rationale: with real, classified-`ActionMutate` tools registered, the boot-resolved write floor's denial is a **demonstrated gate proof** rather than an assertion — OASIS and the E2E suite can drive an actual mutation attempt into the executor and observe the floor deny it, exercising the **classified-Mutate deny path** end-to-end rather than only the unknown-tool fail-closed default (`ClassifyTool` mapping an unrecognized name to Mutate-and-deny). An empty Mutate set proves nothing about the gate. The registered surface is also the **working seam full mode will open**, so deregistration would be pure churn with no launch-safety gain: since observation is the boot default and `JOE_MODE=full` is refused at boot (D-0073), every bootable configuration already has the floor up, so the tools being registered changes nothing an operator can reach today. Consequence: the action-model Concepts page (`docs/public/concepts/action-model.md`) mutation-surface paragraph is reworded from "a real mutation surface exists and is reachable today" to state that the surface exists, is registered and governed, but is **denied by the write floor in every configuration Joe currently boots** — aligning that paragraph with the same page's observation-mode section (the "reachable today" phrasing was the sole stale line; the surface-boundary and observation-mode passages were verified accurate and left untouched).
- Basis: `observation-wording-triage` session, Phase-1 read-only verification against the live tree. The write floor is boot-resolved and denied-by-default for the Mutate class with observation as the day-one default and `JOE_MODE=full` refused at boot (D-0073, `env.ResolveBootMode` → `ResolveWriteFloor`); the executor checks the floor ahead of incident and RBAC (D-0022 denial precedence); tool classification is authored at write time with an unrecognized name defaulting to `ActionMutate` (`ClassifyTool`, `internal/safety/tier.go`), so a registered-and-classified Mutate tool exercises a **different** executor path than the unknown-tool default. The mutation surface inventory and boundaries are those recorded by D-0069 and unchanged here.
- Supersedes: nothing. **Refines the "reachable today" framing of D-0069** — the mutation surface exists and is governed, but is **not reachable in any currently bootable configuration** because the write floor is up — **without changing D-0069's surface inventory or its two boundaries** (mutations registered only on the human-facing task loop; no live-infrastructure mutation registered on any surface). Leaves the write floor (D-0018), denial precedence (D-0022), and the observation boot default (D-0073) unchanged; no code, test, or classifier change. Because `docs/public` is the joeagent.dev publication source, the reworded action-model paragraph is a site-revision flag for a separate site-side republish.

---

## D-0081 — The onboarding, clarifications, and manual-refresh HTTP routes are unregistered for launch as reachable-but-orphaned surfaces; code and schema retained; the findings routes and the autonomous Refresher are unaffected

- Date: 2026-07-12
- Status: accepted (implemented)
- Session: discovery-clarifications-pipeline
- Decision: The three-route clarifications group (`GET /api/v1/clarifications`, `POST /api/v1/clarifications/{id}/answer`, `POST /api/v1/clarifications/{id}/dismiss`) and the two-route control group (`POST /api/v1/onboarding`, `POST /api/v1/refresh`) are **parked for launch** — their `RegisterRoutes` call sites in `internal/api/server.go` are removed, so all five paths now return `404`. This is a **route-level park only**: the `registerClarificationRoutes`/`registerControlRoutes` functions, the clarification and control handlers, `ClarificationService`, `ClarificationRepository`, `discovery.ProcessInput`, the facts repository, the `save_onboarding_fact` tool, and **all tables and migrations** are retained untouched (removing the call sites rather than the functions makes re-enabling a one-line change per group). The surfaces are parked because the pipeline behind them is incomplete (no producer enqueues clarifications, `onboarding_facts` has no reader, `discovery.ProcessInput` is a stub, and there is no UI/MCP consumer — tracked in [`discovery-clarifications-pipeline`](../backlog/discovery-clarifications-pipeline.md)), so exposing the routes would advertise HTTP endpoints that do not deliver a working feature. Explicitly **out of scope and unchanged**: the autonomous `Refresher` engine (launched from `Agent.Start`, independent of the `/refresh` HTTP route, so the background refresh loop is unaffected) and the session-scoped findings routes (`registerFindingsRoutes`, `POST`/`GET /api/v1/sessions/{id}/findings`). Route-level tests that asserted these five paths respond were inverted to assert the parked contract (each returns `404`, and the clarifications group 404s regardless of `Store` presence); every handler-level, service-level, store-level, and refresh-engine test — and the B005 findings registration guard — was left untouched.
- Basis: `discovery-clarifications-pipeline` session, Phase-1 read-only verification against the live tree confirmed the anchors before any edit: `registerClarificationRoutes`/`registerControlRoutes` existed and were invoked from `RegisterRoutes` (`internal/api/server.go`), registering exactly the five routes above; the `Refresher` is launched from `Agent.Start` (`internal/coreagent/agent.go` `a.refresher.Start(ctx)`) with no dependency on the `/refresh` route; and the findings routes are registered separately via `registerFindingsRoutes` (`internal/api/findings.go`). Implementation basis: the two call-site removals in `RegisterRoutes`, and the parked-contract tests in `internal/api/routes_test.go`, `internal/api/control_test.go`, and `internal/api/clarifications_test.go` asserting `404` on all five paths under an authenticated test client. `go build ./...`, `go vet ./...`, and `go test ./...` pass.
- Supersedes: nothing. No security/safety invariant is touched — the write floor (D-0018), denial precedence (D-0022), and the observation boot default (D-0073) are unchanged; this is a launch-scoping park of orphaned HTTP surfaces, not a capability change. Because `docs/public` is the joeagent.dev publication source, the api-reference and agent-loop Concepts edits marking these routes as parked are a site-revision flag for a separate site-side republish.

---

## D-0080 — The knowledge graph's nodes are components-as-anchors plus refresher-discovered resources, not components alone; the "curated versus derived" authority distinction is an enforced property of the knowledge store, not of the graph

- Date: 2026-07-12
- Status: accepted (implemented)
- Session: graph-node-taxonomy
- Decision: Two published claims on the knowledge-graph Concepts page (`docs/public/concepts/knowledge-graph.md`) are corrected against the live tree. **(1) Node taxonomy.** The graph's nodes are **not** components alone. Registered components are the graph's **anchor nodes** (their node `Type` carries the `<component-type>_component` idiom — e.g. a Prometheus component's anchor is typed `prometheus_component`), and the autonomous `agent:core` **refreshers** expand the graph by discovering the resources each promoted component exposes and adding **each discovered resource as a node in its own right**, linked to its anchor and its peers by typed edges. Discovered-resource nodes carry a **bare kind** as their `Type` (a Kubernetes cluster contributes workload, service, and namespace nodes; a cloud account contributes network and instance nodes; and so on — named as representative examples, not an enumerated set, per D-0032). Both anchor and discovered nodes are written through the same delta-reconcile seam (`BuildGraphDelta`/`ApplyGraphDelta` → the graph store's `AddNode`). The Concepts page, the Components section index (`docs/public/components/_index.md`), and the component-lifecycle Concepts page (`docs/public/concepts/component-lifecycle.md`) are reworded from "components are the nodes" to "components **anchor** the graph, and the resources Joe discovers inside them become further nodes." This **refines the D-0078 wording** ("components are the nodes of the knowledge graph") **without changing its structure** — components remain the objects zones and RBAC grant access to, and the D-0078 section layout is untouched. **(2) Curated versus derived knowledge.** The prior "Curated versus derived knowledge" section claimed the authority distinction was a property of "knowledge in the graph." That conflates two subsystems. The curated/synced/derived tier taxonomy is a property of the **knowledge store** (`internal/knowledge`, the `Entry.Tier` field), **not** the infrastructure graph — the graph's `Node` model carries no tier or authority field. The Part-B verdict is **enforced in code**: `knowledge.Service.Update` and `knowledge.Service.Delete` reject any mutation of a `TierCurated` entry, pinned by `TestTierCuratedImmutable`; and no knowledge-**mutating** tool is registered on any agent surface (the agent's only knowledge tool is the `knowledge_search` Read), so the autonomous agent cannot overwrite curated facts through a tool at all. Phase 2 took the **enforced-in-code branch**: the section is kept but reworded to name the knowledge store as the correct layer, state that curated entries are immutable once written and that the immutability is enforced in the store, and explicitly disclaim the distinction as a graph property. The parallel `docs/public/guides/knowledge-graph.md` "Curated versus derived knowledge" section was already accurate (it names the knowledge store, lists all three tiers, and warns against bare tier numbers) and is left unchanged.
- Basis: `graph-node-taxonomy` session, Phase-1 read-only verification against the live tree. Node taxonomy: refreshers under `internal/coreagent` build discovered-resource nodes (e.g. `k8s_refresh.go` lists deployments/statefulsets/daemonsets/services/configmaps/secrets/namespaces/nodes and, via `crd_refresh.go`, KEDA/cert-manager/OPA/Cilium/Istio/Crossplane kinds; `aws_refresh.go`, `azure_refresh.go`, `registry_refresh.go`, `git_refresh.go` add cloud, registry, and repo resource nodes) and upsert them through `BuildGraphDelta`/`ApplyGraphDelta` → `graph.GraphStore.AddNode` (`graphdelta.go`); component anchor nodes are typed `source.Type + "_component"` (`observability_refresh.go`) and its hardcoded siblings (`alerting_refresh.go`, `gitops_refresh.go`, `networking_refresh.go`, `datastore_refresh.go`); `graph.Node` (`internal/graph/store.go`) has fields `Type`/`ComponentID`/`Metadata` and **no** tier or authority field. Curated/derived: the tier constants (`TierCurated`/`TierSynced`/`TierDerived`) live on the `Entry` model in `internal/knowledge/knowledge.go`; the immutability guard is in `internal/knowledge/service.go` (`Update`/`Delete` refuse `TierCurated`), pinned by `TestTierCuratedImmutable` (`internal/knowledge/knowledge_test.go`); the only knowledge tools registered for the agent are `knowledge_search` and `detect_doc_drift` (`internal/tools/core`), neither a knowledge mutation. `docs/project/SITE-CLAIMS.md` was checked and carries **neither** claim (its sole graph entry is the graph-refresh read-surface bypass, unrelated). The CLAUDE.md architectural-invariant line "LLM can create/update Tier 3 knowledge, but cannot touch Tier 1 (curated)" was evaluated as **directionally true but using the discouraged bare-tier-number vocabulary** — it is **not** action-safety-tier residue (the knowledge tiers are live, enforced, and tested), so it was corrected to the named-tier vocabulary rather than removed, per Phase 2 step 4.
- Supersedes: nothing. Refines the D-0078 knowledge-graph node wording without altering its structure; honors the D-0032 no-volatile-counts rule (node kinds and refresher counts are named as examples, never enumerated) and the D-0052 explanation-only discipline (no `file:line` on any public page). No security/safety invariant is touched — the write floor (D-0018), denial precedence (D-0022), and the observation boot default (D-0073) are unchanged. Because `docs/public` is the joeagent.dev publication source, the corrected node-taxonomy and curated/derived copy is a site-revision flag for a separate site-side republish; no separate site repo file is edited here.

---

## D-0079 — The Slack bot is a first-party client of the REST API surface, not a fourth input surface; the three-surface enumeration (Web UI, MCP, REST API) stands

- Date: 2026-07-12
- Status: accepted (implemented)
- Session: slack-client-stance
- Decision: The `joe slack` Socket Mode bot is classified as a **first-party client of the REST API surface**, not as a distinct fourth input surface. The published enumeration of Joe's input surfaces — **Web UI, MCP, REST API** — stands unchanged. Four points fix this stance: **(a)** the bot authenticates at the **same edge as any external caller** — it presents `JOE_API_KEY` as an `Authorization: Bearer <key>` header (`internal/client` `WithAPIKey`/`setAuth`) which the daemon resolves through `auth.EdgeAuth` to a `svc:<name>` service-account principal; there is **no daemon-side Slack-specific auth path, middleware, or bypass** anywhere in `internal/api`, `internal/auth`, or `internal/rbac`, so the single-governance-seam claim covers the bot **by construction**, not by amendment. **(b)** Surface status in the enumeration is earned by **capability shaping**: MCP registers its own tool set and speaks a distinct protocol, and the Web UI is the human OIDC/session entry genre — each shapes the interaction. The Slack bot **shapes nothing**; it forwards user text to existing read endpoints. Promoting it to a surface would **dilute the enumeration** and imply that every future thin client (a CI script, a `curl` caller, the next chat integration) grows the list. **(c)** The bot issuing only graph-query and knowledge-search calls is **client behavior, not an enforced property**. The enforced bound is its `svc:` principal's grants plus the write floor; nothing in the daemon constrains the bot to reads by virtue of it being the Slack bot. Accordingly, **no documentation may claim the Slack bot is read-only by construction** — that would be an operator assertion dressed as a machine-checkable guarantee, the same error D-0067 rejects for MCP-client absence. **(d)** Consequence: **no launch change** to the safety-page or landing-page surface enumerations. The optional demonstration framing — the Slack bot as a first-party client with no privileged daemon path, offered as *evidence* of the single governance seam — **may ride the existing safety-page workstream** but is not required by this decision.
- Basis: `slack-client-stance` session, Phase-1 verification against the live tree. Subcommand and Socket Mode client: dispatch at `cmd/joe/main.go` (`case "slack"`), `runSlackCommand` requires `SLACK_BOT_TOKEN` and `SLACK_APP_TOKEN`, reads `JOE_SERVER` (default `http://localhost:7777`) and optional `JOE_API_KEY`; the bot (`internal/slack/server.go`) binds only the outbound Socket Mode websocket and **no listening port of its own**. Same-edge bearer auth: `internal/client/client.go` `WithAPIKey`→`setAuth` emits `Authorization: Bearer <key>`; `auth.EdgeAuth` (`internal/auth/middleware.go`) resolves it to an `svc:<name>` principal (`internal/auth/serviceaccount.go`). A case-insensitive search for "slack" across `internal/api`, `internal/auth`, and `internal/rbac` returns **zero matches** — no Slack-specific path exists in the daemon. Read-only call pattern: the bot's `JoeClient` interface is exactly `{GraphQuery, GraphSummary, SearchKnowledge}` (`internal/slack/agent.go`); the incoming-message path (`internal/slack/handler.go` `handleAsk` → `Agent.Ask`) invokes only `GraphQuery` (`GET /api/v1/graph/query`) and `SearchKnowledge` (`POST /api/v1/knowledge/search`, a search read), with `GraphSummary` (`GET /api/v1/graph/summary`) on the status path — **no mutating endpoint is reachable**. Existing published coverage confirmed present: `docs/public/guides/slack.md`, a "Slack bot" section in `docs/public/components/_index.md`, both tokens in the `docs/public/configuration/_index.md` env table, and machine-client mentions in `docs/public/install-and-build/_index.md` and `docs/public/concepts/principals-and-identity.md`.
- Supersedes: nothing. Applies the D-0067 operator-assertion-vs-machine-guarantee principle (no read-only-by-construction claim for a client whose restraint is not enforced) to the Slack bot, and leaves the write floor (D-0018), denial precedence (D-0022), and the observation boot default (D-0073) untouched. No security/safety invariant changes; no product code changes.

---

## D-0078 — The Components section index opens with a definitional intro before the lifecycle spine; the per-type routing tables and the not-yet-supported list move to a `connectable-systems` child page

- Date: 2026-07-12
- Status: accepted (implemented)
- Session: components-page-restructure
- Decision: The published Components section index (`docs/public/components/_index.md`) is restructured so it opens with a **definitional intro** — what a component is (Joe's name for a managed system: a Kubernetes cluster, a Prometheus, a GitHub org, a Grafana), that components are the nodes of the knowledge graph and the objects zones and RBAC grant access to, and that Joe is near-useless without them — placed **ahead of the register → promote → arm → activate lifecycle spine**. The per-type **routing tables** (the six category tables mapping each connectable system to its credential mechanism and its runtime-vs-boot activation path, plus the Kubernetes web-UI guide cross-link) and the **"Not yet supported"** type list are **relocated verbatim** from the section index into a new child page, `docs/public/components/connectable-systems.md` (title *Connectable systems*, weight 10). The section index now **routes readers to that child page** (in the opening paragraph, in the activate step, and as the first "Where to go next" bullet) instead of carrying the tables inline. The **Front-end integrations** section (MCP server, Slack bot) stays on the section index unchanged; whether it belongs there at all is deferred (`docs/backlog/components-page-restructure.md`). This change **preserves the D-0055/D-0056 routing discipline** — the runtime-vs-boot activation split and the per-type routing remain the single documented source, now on one dedicated page — and **preserves the D-0032 no-volatile-counts rule**: no hardcoded count of component types or categories is introduced in the new or edited prose. The links in the moved content are re-based for the child page's one-level-deeper depth (`../guides/...`/`../concepts/...` → `../../...`).
- Basis: `components-page-restructure` session against the live tree. Phase-1 verification confirmed **zero anchored inbound links** to the removed in-page anchors (`#connectable-systems-and-their-credential-mechanism`, `#not-yet-supported`) from anywhere in `docs/public`, so **no published URL dies** and **no new alias is added** — the section index and the new child page both resolve at stable paths. The one relative link inside the moved content (the Kubernetes guide cross-link) was re-based to `../../guides/register-kubernetes/` and verified to resolve against the actual `docs/public` tree. The new intro's `knowledge graph` link targets `../concepts/knowledge-graph/`, which exists.
- Supersedes: nothing. Refines the presentation of the Components section established by earlier docs work (**D-0055**/**D-0056** routing discipline, unchanged in substance) and continues to honor the **D-0032** no-volatile-counts rule. No security/safety invariant is touched. Partially discharges the framing point tracked in `docs/backlog/registered-components-required-framing.md` for the Components section only; that item stays open for the Overview and Quickstart pass.

---

## D-0077 — A standing site-claims register (`docs/project/SITE-CLAIMS.md`) maps each load-bearing joeagent.dev claim to its mechanism and pinning test, and sessions changing a listed mechanism must flag a site revision in their report

- Date: 2026-07-12
- Status: accepted (implemented)
- Session: safety-page-03
- Decision: A new internal register, `docs/project/SITE-CLAIMS.md`, is established as the standing drift-detection surface between the code and the claims published on joeagent.dev. It lists each **load-bearing published claim** and maps it to the **mechanism it rests on** and the **guard or break-test pinning it** where one exists. The paired obligation, recorded in CLAUDE.md's Reference Documents section, is that **any session changing a mechanism listed in the register must state the joeagent.dev revision impact in its session report** — so that a mechanism change routinely surfaces the site copy it invalidates, rather than the site silently drifting from the binary. The register follows two conventions: entries carry **test names, never file:line coordinates** (per the D-0032 principle that volatile references do not belong in standing documentation), and claims are tagged **launch-bound** or **mechanism-bound** where the published posture is a **planned revision point** (a launch decision or a later mechanism landing) rather than drift. It is seeded with two sections: the Safety deep-dive at `/safety/` (twelve entries — write floor + `JOE_MODE` boot branch, floor no-runtime-lowering guards, binary Read/Mutate classification, layered-pipeline floor precedence, the graph-refresh tracked bypass, the guarded-accessor audit point with fail-closed/fail-open and insert-only repository, the deny-only incident gate, panic persist-then-exit with no-unlock-endpoint, the credential/`clientcmd`-confinement invariants, the admin-gated skills posture, the MCP server-only stance, and the OASIS validated-pipeline no-verdict stance) and the landing page (three entries — governed-by-construction, the one-governance-seam hero sentence, and ships-in-observe-mode). The register is a **derived pointer, not a source of truth** for the site; the published copy remains authoritative for exact wording.
- Basis: `safety-page-03` session against the live tree. Phase-1 re-derivation confirmed `docs/project/` held `DECISIONS.md` with no pre-existing claims register or similarly purposed file (repo-wide search for "claims register"/"SITE-CLAIMS"/`*claim*` returned nothing under `docs/`), and that CLAUDE.md's Reference Documents section is the placement matching the existing pointer-plus-obligation convention. Every pinning test name in the register was verified to exist in the tree at authoring time (e.g. `TestResolveWriteFloor_Precedence`, `TestResolveBootMode`, `TestWriteFloor_NoRuntimeLoweringPath`, `TestClassifyTool_UnknownDefaultIsMutate`, `TestClassifyWebSearchIsRead`, `TestPhaseF_FailClosedOnMutate`/`FailOpenOnRead`, `TestRepositoryAPISurface_AppendOnly`, `TestMigration015_TriggerBlocksUpdate`/`Delete`, `TestPhaseG_GateIsDenyOnly_RBACAuthorityInvariance`, `TestPhaseG_SingleSharedCaptainGateImplementation`, `TestRegisterPanicRoutes`, `TestNoClientcmdOutsideAllowedAdapters`, `TestTransport_NoForbiddenAuthMechanisms`, `TestPromote_StaticEnvVarUniqueness`, `TestEntraProvider_TransportAgnostic`, `TestSkillsRoutes_MutatorsRequireAdminGate`, `TestEveryDispatchMethodDeclaresAnAction`). The MCP server-only claim is recorded with **no pinning test** by design — the guard is deferred to `docs/backlog/mcp-client-absence-guard.md`, and the register's mechanism-bound note records that the copy's "not yet test-pinned" sentence dies when that guard lands.
- Supersedes: nothing. Establishes a new internal convention surface (the site-claims register) and a new session-report obligation; adopts the D-0032 no-file:line principle for its entries. Does not alter any security/safety invariant — the write floor (**D-0018**), denial precedence (**D-0022**), and the observation boot default (**D-0073**) are unchanged. The published copy and the joeagent.dev repo are external to this repo and untouched.

---

## D-0076 — The landing-page demo world is a committed public example under `examples/demo-world/`, staged with plain Kubernetes manifests on any local cluster, deliberately not petri-provisioned

- Date: 2026-07-12
- Status: accepted (implemented)
- Session: feature-clips
- Decision: The fictional "shop" demo world used to record the joeagent.dev landing-page clips is committed to this repo as **plain Kubernetes manifests** under `examples/demo-world/` (a new top-level `examples/` directory), applied to **any local cluster** (a throwaway kind cluster is the documented path) with `kubectl apply -f examples/demo-world/`. It stages one namespace (`shop`) with three Deployments + a Service each, engineered so each service exhibits a **real** cluster symptom rather than a scripted event: `orders` (1 replica, 128Mi memory limit) holds a resident baseline near the limit and periodically bursts past it so the kernel OOM-kills it (exit 137 / `OOMKilled` last state, climbing restart count); `payments` (2 replicas) runs `nginx` whose readiness probe hits a missing `/healthz` and gets a genuine 404, so both pods go NotReady while the Deployment stays up; `checkout` (2 replicas) is fully healthy and carries `PAYMENTS_URL` / `ORDERS_URL` env vars pointing at the in-cluster Service DNS names of the other two, making the dependency edges discoverable from the pod spec. Images are public, unauthenticated Docker Hub images only (`python:3.12-slim`, `nginx:1.27-alpine`) — no custom builds, no private registry; generic image names surfacing in diagnostic output is accepted as honest since the world is openly fictional. A namespace manifest carries a `00-` prefix so a single directory apply orders the namespace ahead of the workloads. **The world is deliberately NOT petri-provisioned:** petri's workload renderer cannot express these exact symptoms (a real limit-driven OOM excursion loop, a probe-404 NotReady-but-alive posture) without extending petri's core, and a marketing/demo deliverable is the wrong driver for a change to that shared provisioning path — plain manifests keep the demo self-contained, visitor-reproducible, and decoupled from petri's roadmap.
- Basis: `feature-clips` session. Validated on a local kind cluster (`joe-demo-world`): within ~2 minutes of staging, `orders` showed `lastState.terminated.reason=OOMKilled` (exit 137) with restart count ≥ 1 and continued running near the limit between kills; both `payments` pods reported `Ready=False` with `Warning Unhealthy … Readiness probe failed: HTTP probe failed with statuscode: 404` events while the `payments` Deployment object stayed up; both `checkout` pods were `1/1 Ready` with the two dependency env vars present in the spec. A namespace-delete-then-reapply reset reproduced all three symptoms identically. The README documents purpose, prerequisites, stage/reset commands, per-symptom verification commands (adjusted for kind/containerd, which records the OOM on the pod `lastState` and as `Back-off` events rather than a namespace-scoped `OOMKilling` event), expected time-to-symptom, and the fictional-world / real-symptom framing.
- Supersedes: nothing. Establishes `examples/` as a new committed-example surface. Independent of the security/safety invariants (write floor, denial precedence, read posture) — the demo world is external cluster state Joe observes, not a change to Joe's own runtime.

---

## D-0075 — The skills HTTP surface authorization posture: the three mutating endpoints (reload/approve/reject) are admin-gated via `requireAdmin`; list stays authenticated-only; a dedicated skill-lifecycle audit Kind is deferred

- Date: 2026-07-12
- Status: accepted (implemented)
- Session: skills-governance-hardening
- Decision: The skills REST surface (`internal/api/skills.go`) is brought in line with the RBAC and LLM admin surfaces' authorization posture. The three **mutating** endpoints — `POST /api/v1/skills/reload`, `.../approve`, `.../reject` — are now **admin-gated via `server.requireAdmin`**, applied at the very top of each handler **ahead of** the nil-watcher / nil-manager guards, the same in-tree gate pattern every admin handler uses (`admingate.go`). Before this change all four routes sat behind edge auth only, so **any authenticated principal could approve a quarantined skill** (promoting untrusted content into the LLM's decision-time context) or force a registry reload. `GET /api/v1/skills` **remains authenticated-only by design** — it is read-only, exposes no on-disk paths or credentials, and its quarantine-roster visibility to authenticated teammates is consistent with the team-public session model; it is the single deliberate exemption from the gate. The gate's allow path writes **no audit row of its own** (matching the admin surface: `requireAdmin` is admit/deny only — only the denial path writes a `KindAdminAccess`/deny row via `recordAdminDenial`; allow-path rows come from downstream mutation layers, which the skills manager does not yet have). Skill-lifecycle events therefore still emit **`slog` lines only** (`auditSkillEvent`). The **dedicated skill-lifecycle audit Kind is explicitly deferred**: it is migration-bearing work (a new audit Kind plus a CHECK-constraint-widening migration) whose principal value covers the **CLI** lifecycle operations that live outside this HTTP gate's reach, so it belongs with that larger item rather than this gate fix. Load-time content-integrity verification (lockfile-hash checking at `LoadDir`/`Reload`) also remains deferred. Two stale doc comments are corrected in the same change: the `skills.go` header (it claimed the endpoints were already gated and every state change audited via the Manager, and described two endpoints where four routes exist) and the `internal/skills/skill.go` package comment (it still claimed a phase-1 "no CLI, no hot reload, no quarantine" scope the tree has long since outgrown).
- Basis: `skills-governance-hardening` session against the live tree at 8acfe6f. Verified: `requireAdmin` (`internal/api/admingate.go`) is admit/deny with an audited denial path and no allow-path row; admin mutating handlers (`internal/api/admin.go`) likewise write no allow-path row from the gate (downstream repo/mutation-service transactions do). The skills `Manager`/`Watcher` lifecycle paths (`internal/skills/install.go`, `watcher.go`) emit `slog` `skill_audit` lines only — no audit-store Kind exists. New behavioral tests (`internal/api/skills_admin_test.go`, fixture pattern mirroring `llmadmin_test.go`): non-admin 403 with the operation inert on all three mutators; admin 200 with the operation performed; auth-disabled permits; non-admin GET `/skills` still returns the roster. New structural guard `TestSkillsRoutes_MutatorsRequireAdminGate` parses `skills.go` and asserts every mutating route calls `requireAdmin` with `handleList` as the single exemption (the skills-scoped analogue of `TestAdminRoutes_AllRequireAdminGate`). Full `go build ./...`, `go vet`, `gofmt`, and `go test ./...` are green. This discharges items 1 and 4 of `docs/backlog/skills-governance-hardening.md`; items 2 (audit Kind + migration) and 3 (load-time integrity) remain open. CLAUDE.md's stale "RBAC enforcement middleware fires only on paths with a componentID" line — false since the Phase-E demotion (D-0008) made `EnforcementMiddleware` a pass-through and moved enforcement into the guarded accessor (`internal/access`) — is corrected in the same commit.
- Supersedes: nothing. Discharges part of the `skills-governance-hardening` backlog item (items 1 and 4); items 2 and 3 stay open. Adopts the `requireAdmin` gate established by **D-0012** and its allow/deny audit posture (**D-0013**) for the skills surface. The write floor (**D-0018**) and denial precedence (**D-0022**) are unchanged — this gate sits alongside them, not in front.

---

## D-0074 — The two-binary-era local-tool residue is deleted from the safety layer: the six orphaned classification rows, the `write_file` / `run_command` policy surface, and the latent self-protection guards

- Date: 2026-07-09
- Status: accepted (implemented)
- Session: orphaned-tool-registration-cleanup
- Decision: The safety layer's residue for the removed `internal/tools/local/` tree is deleted so the safety surface describes only what the binary ships. Three removals: (1) the six orphaned **classification rows** in `internal/safety/tier.go` — `read_file` / `local_git_status` / `local_git_diff` / `ask_user` (were Read) and `write_file` / `run_command` (were Mutate). No tool registers under any of these names, so `ClassifyTool` now returns the **unknown-tool default (Mutate, deny-by-default)** for them — the deny-by-default floor for unknown tools is unchanged and no reachable path passes these names. (2) The **`write_file` / `run_command` policy surface** in `internal/safety/policy.go` — the `ActPolicy.WriteFile` / `ActPolicy.RunCommand` fields, the `WriteFilePolicy` / `RunCommandPolicy` structs, their `DefaultPolicy()` values, and their `IsT3Allowed` cases. This **resolves the long-standing "default-deny vs `DefaultPolicy()` shipping `run_command.enabled: true`" tension by deletion** — the relic toggle gated no registered tool. The typed fields were **not** retained as a deserialization shim: there is **no runtime decode of `SafetyPolicy`** anywhere (the on-disk loader was removed earlier — the runtime policy is `DefaultPolicy()` modulated per request), so no unknown-key tolerance is needed. (3) The **latent self-protection guards** in `internal/safety/invariants.go` — `IsPathAllowed`, `IsCommandAllowed`, `IsWritePathInAllowedDir` — plus their helpers and the now-orphaned `safety.PolicyFileName`. Phase-1 re-derivation confirmed **zero live non-test callers** for all three guards (the only references were their own definitions and `invariants_test.go`; `internal/skills/policy.go` mentions the no-file-tool posture in prose only, not the guard symbols), so per the backlog's delete-if-no-caller rule the whole file and its test were removed. The self-protection guarantee (invariant #2 in `security-in-layers.md`) is preserved **structurally**: Joe registers no file or command tool on any surface, so there is no LLM-invokable path to guard; a guard of that shape would return with any reintroduced file/command tool. All test fixtures that used the six names to exercise the Read/Mutate axis were migrated to live, currently-classified tool names (Read → `list_components` etc.; Mutate-denied → `github_comment`; Mutate-allowed-by-policy → `publish_doc_update_git` under `git_push`).
- Basis: `orphaned-tool-registration-cleanup` session against the live tree at c74d81e. Verified: `internal/tools/local/` is absent; no production `Name()` returns any of the six names; `internal/tools/default.go` registers only `shared/` and `core/` tools. The three guards had no live caller (grep of the whole tree; only definitions + `invariants_test.go`). No `LoadPolicy` / `yaml.Unmarshal` of `SafetyPolicy` exists, confirming the no-shim disposition. Full `go build ./...`, `go vet ./...`, `gofmt`, and `go test ./...` are green after the migration. `docs/reference/joe-architecture.md` and `docs/reference/security-in-layers.md` were reconciled (no reference doc describes `write_file` / `run_command` as live tools or the deleted guards as compiled-in). This completes the `docs/backlog/orphaned-tool-registration-cleanup.md` item.
- Supersedes: nothing; it completes cleanup that **D-0018/D-0019/D-0020** (the binary Read/Mutate collapse and the removal of the local-tool tree) left as residue, and closes the doc tension flagged in `docs-reference-audit-02`. **D-0003** (the original run_command allowlist decision) remains unedited as append-only history; this entry records that the surface it described no longer exists. The write floor (**D-0018**), denial precedence (**D-0022**), and the boot posture (**D-0073**) are unchanged.

---

## D-0073 — The boot write-floor default is inverted: an unconfigured Joe boots observation (read-only), `JOE_MODE=full` is refused at boot as not-yet-implemented, and unrecognized values are refused fail-closed

- Date: 2026-07-05
- Status: accepted (implemented)
- Session: observation-default
- Decision: Joe's day-one boot posture is inverted so an **unconfigured Joe (`JOE_MODE` unset) boots in observation mode** — the write floor comes up with reason observation, read-only below RBAC — rather than booting write-capable. This lands the day-one-observation posture that **D-0019 point 1** stated as the design of record but which was previously **pending** (the live parse site treated unset as writable). A new pure, unit-testable decision function `env.ResolveBootMode(raw string) (observation bool, err error)` (`internal/env/keys.go`) maps the raw `JOE_MODE` value to the observation input for `safety.ResolveWriteFloor`: **unset or the literal `observation` → observation posture** (observation input true); **`full` → error** whose message states full mode is not yet implemented and that Joe currently runs in observation mode only — refused at boot rather than enabling writes or silently downgrading; **any other non-empty value → error** naming the unrecognized value and the accepted set (`observation`, `full`), refused fail-closed. The boot caller (`cmd/joe/server.go`) logs the error and exits non-zero; the process-exit shim is the only untested part. The decision runs **before** floor resolution, so full/unknown refusal fires **regardless of panic state**; panic/safe-mode precedence within the floor is unchanged (panic still wins over observation). `safety.ResolveWriteFloor` and its break-tests (`TestResolveWriteFloor_Precedence`, `TestWriteFloor_NoRuntimeLoweringPath`) are **unchanged**, and its writable resolution path (observation input false) is **retained as the seam** for full mode when it is implemented. Full-mode auth fail-closed (`full-mode-rbac-track`) remains deferred and out of scope for this session.
- Basis: `observation-default` session against the live tree at 2b4c467. Verified: the sole production caller of `ResolveWriteFloor` is `cmd/joe/server.go` (every other call is in `_test.go`), so inverting the parse-site default is complete; `ResolveWriteFloor` (`internal/safety/floor.go`) returns a down floor only when neither panic nor observation is set, and panic wins. New: `env.ResolveBootMode` + `ModeFull` constant (`internal/env/keys.go`), unit test `TestResolveBootMode` covering all four cases (`internal/env/keys_test.go`). Full `go build`/`go vet`/`go test ./...` green; the floor precedence test and the no-runtime-lowering repo-walk guard pass unchanged. CLAUDE.md's write-floor invariant is updated to record the day-one observation default and the full/unknown boot refusal.
- Supersedes: resolves the **pending** day-one-observation posture of **D-0019 point 1** (design of record; implementation was pending) for the boot-default half. Does not supersede **D-0018** (the write floor's lifecycle/immutability — unchanged) or **D-0022** (denial precedence — unchanged). The full-mode write-capable posture and its fail-closed-empty-RBAC guarantee (`docs/backlog/full-mode-rbac-track.md`, tracked further by `docs/backlog/observation-default.md`) remain deferred.

---

## D-0072 — The recorded "hide the zoned-era admin UI" plan is scope-corrected: only the Policies page is inert under `team_flat` and is now client-side posture-gated in the UI; Zones and component-zone assignment stay visible; the REST surface is unchanged

- Date: 2026-07-05
- Status: accepted (implemented)
- Session: read-posture-latch
- Decision: The `read-posture-latch` backlog item's "Hide the zoned-era admin UI" deferred work recorded a **three-surface** scope — Zones, Policies, and component-zone assignment — as inert under the `team_flat` launch posture. That scope was **wrong** and is corrected here. The zone-allows-action gate (`zone.Allows`, `internal/rbac/policy.go`) runs **ahead** of the `team_flat` read admit, so **Zones and component-zone assignment still shape which reads are permitted under `team_flat`** — they are not inert and **stay visible**. Only the **Policies** page (grant rows) is truly inert under `team_flat`: the boot-resolved write floor denies every Mutate below RBAC (`internal/tools/executor.go`), and the `team_flat` admit widens read to every authenticated principal ahead of the read-grant logic (`internal/rbac/policy.go`), so grants admit nothing in either direction. The Policies sidebar nav entry and the `/admin/policies` route are therefore **posture-gated in the UI**: both render only when the live posture is `zoned`; under `team_flat` the nav entry is hidden and the route redirects to the index. The gate is **client-side only** — the backing `/api/v1/admin/policies` REST endpoints stay registered, admin-gated, and functional so an operator can manage grants over REST in either posture. The posture is fetched via a `useReadPosture` React Query hook against the existing GET `/api/v1/admin/read-posture`; **no backend endpoint was added**. The v2 zoned-flip admin UI (flipping `team_flat`↔`zoned` from the UI) remains deferred.
- Basis: `read-posture-latch` session against the live tree at b77cb86. Verified: the `team_flat` admit sits at `internal/rbac/policy.go` after the `zone.Allows` gate (the gate at the top of `Decide` returns `ReasonActionNotInZone` before the posture block); the write floor denies the Mutate class in `internal/tools/executor.go`; the posture read endpoint is GET `/api/v1/admin/read-posture` returning `{"posture": "team_flat"|"zoned"}` (`internal/api/admin.go`, `getReadPosture`). New UI: `ui/src/hooks/useReadPosture.ts`, `ui/src/auth/RequireZonedPosture.tsx`, posture constants + `fetchReadPosture` in `ui/src/api/security.ts`; route gate wired in `ui/src/App.tsx`; nav gate in `ui/src/components/layout/Sidebar.tsx`. Tests: `RequireZonedPosture.test.tsx` (route present under `zoned`, absent/redirected under `team_flat`, loading holds) and Sidebar nav cases; full UI lint+test and Go build/vet/test green. CLAUDE.md unchanged — a UI visibility gate over an unchanged REST surface adds no architectural invariant or convention.
- Supersedes: corrects the recorded scope of the "Hide the zoned-era admin UI" deferred item in `docs/backlog/read-posture-latch.md` (that file is amended in the same session to record the correction and mark the Policies gate done). Does not supersede **D-0041** (the posture mechanism) or **D-0043** (the transport-only scope of the posture) — it builds on both. The write floor (**D-0018**) and denial precedence (**D-0022**) are unchanged.

---

## D-0071 — A renamed docs/public page whose old URL was published on joeagent.dev carries a Hugo `aliases` front-matter entry for the old URL; aliases live upstream in docs/public

- Date: 2026-07-05
- Status: accepted (implemented)
- Session: docs-public-refit-02
- Decision: When a `docs/public` page or section is renamed after its URL has been published on joeagent.dev, the new page carries a Hugo `aliases` front-matter entry for the old published URL. The rationale is mechanical: joeagent.dev is hosted on GitHub Pages, which has no server-side redirects, so Hugo front-matter aliases are the only redirect mechanism available; and because the joeagent.dev build syncs a copy of the `docs/public` tree, the aliases must live upstream in `docs/public` itself so they travel with the content and survive site-side re-seeds rather than being hand-placed site-side. Applied here to the two published URLs killed by D-0070's renames: `docs/public/components/_index.md` gains `aliases: [/docs/integrations/]` and `docs/public/concepts/component-lifecycle.md` gains `aliases: [/docs/concepts/components-and-promotion/]`. No alias is added for the action-model page — the old capabilities URL was never published in the live sitemap.
- Basis: `docs-public-refit-02` session. The live joeagent.dev sitemap lists `/docs/integrations/` and `/docs/concepts/components-and-promotion/`, both of which the D-0070 renames (commit 411feea) would otherwise turn into 404s. GitHub Pages offers no redirect layer; Hugo's `aliases` front-matter field is its standard client-side redirect mechanism. CLAUDE.md carries no mention of aliases or the published-URL convention and needed no edit.
- Supersedes: nothing; extends **D-0070**'s rename with the redirect layer it left unaddressed. D-0070 remains unedited as append-only history.

---

## D-0070 — The public taxonomy section "Integrations" is renamed "Components", the "Components and promotion" Concepts page is retitled "The component lifecycle", and the copilot-for-platform-engineers / HTTP-daemon copy is replaced with the self-hosted open-source AI-agent framing

- Date: 2026-07-02
- Status: accepted (implemented)
- Session: docs-public-refit
- Decision: In `docs/public`, the published taxonomy section **`integrations/` is renamed `components/`** (directory rename via `git mv`; `_index.md` front-matter `title` and H1 changed from "Integrations" to "Components", weight unchanged at 60), and **every inbound relative link and section-name prose reference across `docs/public` is retargeted** from the `integrations/` path and "Integrations" text to `components/` and "Components" (overview, quickstart, configuration, install-and-build, api-reference, the concepts pages, the guides index and the mcp/knowledge-graph/register-kubernetes guides). Separately, to avoid a name collision with the new section, the Concepts page **`concepts/components-and-promotion.md` is retitled "The component lifecycle" and renamed `component-lifecycle.md`** (weight unchanged at 50), with all nine inbound links retargeted in path and display text; page body is otherwise unchanged. Finally, the launch positioning copy is corrected: the front page (`_index.md`) and Overview drop "AI-powered infrastructure copilot for platform engineers" for the locked line **"Joe is a self-hosted, open-source AI agent for your infrastructure"** (the "Joe Operates Everything" expansion and "governed by construction" phrasing preserved); Overview's "runs an HTTP daemon" becomes "runs as a long-lived daemon" (the HTTP server is one surface the daemon exposes, not what it is); and the Quickstart "governed copilot" becomes "governed agent". A `docs/public`-wide sweep confirms zero remaining "copilot", "platform engineers", or "HTTP daemon" occurrences.
- Basis: `docs-public-refit` session against the live tree at 94c76db. Directory and file renames done with `git mv` (history preserved). Post-edit `grep -rn` across `docs/public` returns zero hits for the old `integrations/` path, `[Integrations]` text, `components-and-promotion`, the old `concepts/capabilities` path, `copilot`, `platform engineers`, and `HTTP daemon`. CLAUDE.md carries no reference to the old section name or page and needed no edit.
- Supersedes: amends the **D-0052** nine-section published-taxonomy naming for this one section only (Integrations → Components) and the Concepts page title; it does **not** add or remove a section. The **D-0055** documentable-set gate and the **D-0056** runtime-versus-boot-config activation-routing split are content policies unchanged by the rename and continue to govern the section under its new name. Elevating RBAC to a first-class published section is explicitly **deferred** to the post-launch RBAC work (`docs/backlog/read-posture-latch.md` / `rbac-v2.md`), not taken here.

---

## D-0069 — The "Capabilities" Concepts page is reworked into "The action model": every action is classified on the binary Read/Mutate axis and governed accordingly, and the shipped external-collaboration mutation surface is documented with its as-built approval path

- Date: 2026-07-02
- Status: accepted (implemented)
- Session: docs-public-refit
- Decision: The Concepts page shipped by D-0066 (`concepts/capabilities.md`, title "Capabilities", spine "everything Joe does out of the box is a Read") is **reframed and renamed** to `concepts/action-model.md`, title **"The action model"** (weight unchanged at 100). The new spine is that **every action Joe takes is classified on a binary Read/Mutate axis and governed accordingly**: the classification is authored by Joe's authors at write time (not inferred at runtime), an unrecognized tool name defaults to Mutate-and-deny, Reads pass the write floor unconditionally, and Mutates are denied by default. The page now describes the **real, shipped mutation surface** in its exact scope: external-collaboration writes only — commenting on a GitHub pull request or GitLab merge request, submitting a GitHub request-changes review, and publishing an already-approved documentation proposal to Confluence, Notion, or Git. It states the two boundaries (mutations run only on the human-facing task loop; the autonomous core agent and the MCP server register no mutating tools; no tool mutating live infrastructure — Kubernetes, cloud, database, file, shell — is registered on any surface), the executor gate chain each mutate passes (write floor → incident captain gate → zone/namespace scope → safety-policy per-class opt-in), and the **as-built** approval path (operator boot-time policy opt-in, plus a human approval step in the proposal workflow for doc publishing, plus the captain-only session restriction during an incident) — explicitly **not** an interactive per-action confirm prompt, which the server flow does not have. Per D-0032 no mutating-tool count (or any volatile count) is stated; operations are named structurally. Per D-0052 the page stays explanation-only (no config tables, no how-to steps, no internal `file:line` citations).
- Basis: `docs-public-refit` session; mutation-surface claims re-derived and confirmed at 94c76db (external-collaboration writes reachable on the human task loop only; no infrastructure-mutating tool registered on any surface; executor gate order floor > incident > RBAC/zone > policy opt-in; approval is policy opt-in + proposal-workflow human approval + captain-only, with no server-side per-action confirm). The two inbound links to the page (Configuration web-search cross-link per D-0066, Concepts index) are retargeted to `action-model/`; the Concepts index one-liner is rewritten off the old "why all of it is reading" framing.
- Supersedes: supersedes the **"everything is a Read" spine framing of D-0066** — Joe is no longer presented as a categorically read-only agent — while **preserving D-0066's read-surface inventory** (the three read classes: built-in component-independent diagnostics with the compose-not-overlap fetch/search pair, the component-backed observe surface, and MCP as read-as-context), its **web-search narrative placement** in Concepts with keys left in Configuration, and its **observation-mode available-not-default** stance. D-0066 remains unedited as append-only history; this entry states the current position where its spine now reads stale.

---

## D-0068 — The `system_info` shared tool is removed; Joe's shared diagnostic tools are outward network probes only, and remote-host OS stats are deferred to a future component type

- Date: 2026-07-02
- Status: accepted (implemented)
- Session: sysinfo-tool-removal
- Decision: The `system_info` shared tool and its package are **deleted entirely** — the `internal/tools/shared/sysinfo` package (platform readers plus test), its import and registration in `internal/tools/default.go`, its classifier row in `internal/safety/tier.go`, and its entry in the registration pin-test in `internal/tools/default_test.go` (the shared set now pins to six tools: `tcp_connect`, `port_scan`, `dns_lookup`, `http_request`, `trace_route`, `web_search`). `system_info` was the one shared diagnostic that inspected the **host Joe itself runs on** (disk, memory, load, OS) rather than probing outward from Joe's network position; it had **no target parameter** and could only ever report on the daemon's own machine, which contradicts Joe's capability story — Joe inspects the *managed systems it is pointed at*, not the box it is deployed on. The remaining shared tools are therefore **outward network diagnostics only** (connectivity, port, DNS, path) plus the compose-with HTTP fetch and web search; none reads the local host. The legitimate want it gestured at — OS-level stats of a *remote managed host* — is **deferred to a future component type** (SSH-reachable host or node agent), recorded in `docs/backlog/remote-host-diagnostics.md`, with the constraints that a shell transport is Mutate-capable by construction and would need a severely constrained read-only surface to classify honestly as a Read, and that Kubernetes node stats already have a governed path through the `kubernetes` component. Not a shared-tool retrofit.
- Basis: Phase-1 re-derivation against the live tree (`sysinfo-tool-removal` session): package `internal/tools/shared/sysinfo/` (5 files including `sysinfo.go` and `sysinfo_test.go`); import at `internal/tools/default.go:10` and registration at `internal/tools/default.go:30`; classifier row `"system_info"` at `internal/safety/tier.go:110`; expected-registered-set entry at `internal/tools/default_test.go:66`; capabilities prose ("report local system stats (disk, memory, load, OS)") at `docs/public/concepts/capabilities.md:30`; shared-tree listing at `docs/reference/joe-architecture.md:313-314`. No reference to the tool name or package survives anywhere in Go, prompts, UI, or config after removal (only DECISIONS.md history and the new backlog file name it). `golang.org/x/sys` reclassified direct→indirect by `go mod tidy` since only the sysinfo readers used it. `go build`, `go vet`, `gofmt -l`, and full `go test ./...` all pass post-removal.
- Supersedes: narratively updates the D-0064 sibling list (which named `sysinfo` alongside `dnsquery`/`httpreq`/`netcheck`/`traceroute` as Go-native shared tools) and the D-0066 capabilities-page prose (which described built-in tools as reporting local system stats). Both entries remain unedited as append-only history; this entry states the current position where they now read stale.

---

## D-0067 — Joe will not act as an MCP client (consume external MCP servers' tools); the protocol's lack of an enforceable tool mutation classification is incompatible with the observe-mode guarantee

- Date: 2026-07-01
- Status: accepted
- Session: mcp-client-rejection
- Decision: Joe will NOT support MCP *client* connections — Joe will not connect out to external MCP servers and make their tools callable from the agent loop. This is a categorical stance, not a deferral. It sits alongside Joe's other by-construction refusals (never a human authentication path, never kubeconfig ingestion, never Kubernetes impersonation headers): a capability the ecosystem normalizes but which cannot be governed by construction, so Joe declines it.

  Core reasoning. Joe's central guarantee is machine-checkable: in observation mode the boot-resolved write floor denies the Mutate class at the executor, so "Joe will not change anything" is a proof about Joe's own code, not an operator's promise. That proof depends on Joe knowing, by construction, whether a tool mutates the managed system. For native tools that knowledge is authored — the compile-time classification in the tool tier map, written by Joe's authors, IS the proof. MCP tools carry no equivalent. The protocol's only mutation signal is the optional annotation set (readOnlyHint, destructiveHint), which the MCP specification itself designates untrusted unless the server is trusted, precisely because a destructive tool can advertise itself read-only (tool poisoning). An external MCP tool is therefore code Joe never sees, with no trustworthy statement of its effect. Making it callable means one of two things: deny it (the tool is useless), or run it on an operator's assertion that it is read-only — which downgrades the observe-mode guarantee from a machine-checkable proof to an operator promise. The whole value of the guarantee is that it is not a promise. Consuming MCP cannot be done without weakening the one property Joe exists to hold, so the direction is rejected.

  Native adapters strictly dominate, which is why the rejection costs little. For any given managed system, a native Joe adapter beats an MCP binding on every axis Joe cares about: it yields a provable read (works in observation mode), governs at operation granularity, presents a curated bounded surface, and is classified at compile time. An MCP binding wins on exactly one axis — zero adapter code — and that advantage is spent the moment a native adapter for that system exists. The only niche left for an MCP client is full-mode reach to a long-tail system that has an MCP server and no native Joe adapter, with the operator accepting the weaker guarantee for coverage. That niche is real but narrow, and it does not outweigh keeping the guarantee categorical. Any system worth showcasing earns a native adapter — a native Confluence adapter, for example, reaches the same company docs in observation mode because the read is Joe's own code, so the motivating "search company docs" case is served natively, not via MCP.

  Asymmetry — this does not touch `joe mcp` (Joe as MCP *server*). Exposing Joe's own governed tools to an external agent over MCP remains sound and supported: Joe authored and classified those tools, the consuming agent inherits Joe's gate, and the trust direction runs the safe way. The rejection is specifically about *ingesting* external, unclassifiable mutation capability into Joe's own agent loop — not about MCP the protocol.

  Revisit trigger (named so this is parked, not merely closed). This decision is reopenable on one specific condition: if MCP standardizes an *enforceable* tool mutation classification — a server-side contract a client can rely on, as opposed to the untrusted readOnlyHint — the governance objection dissolves. At that point the design settled and discarded in this session is the ready-made implementation, recorded here so it is not re-derived:
  - external tools inherit Joe's existing unknown-tool default (ActionMutate then deny) by construction; nothing external is callable until promoted;
  - an external tool becomes callable only through an explicit, admin-gated, audited per-tool promotion, at Read/Mutate parity with the native promotion model — the operator asserts the class, the server never does; annotations are advisory input to that human decision only;
  - the write-floor exemption predicate is (ActionRead AND native-provenance), so an external tool never passes the floor regardless of promotion (an external "Read" is an operator assertion about remote code, not a proof), and promotion is gated to full mode so the trust act happens in the warned, capable posture;
  - an external MCP server folds into the existing component model as a single component with a generic mcp type (routing discriminator only; safety stays type-blind), the server's own auth riding the credentials-as-references seam, with per-tool classification stored below the component keyed on (componentID, toolName).

- Basis: the MCP specification's tool-annotation trust model — annotations are untrusted unless the server is trusted, and unannotated tools default to destructive/open-world — confirmed against the published spec and current MCP annotation guidance during the design session. No code changes: this is a stance decision recorded ahead of, and instead of, any implementation. Grounded against the synced CLAUDE.md / DECISIONS.md / INDEX.md snapshot; the live-tree coordinates for the parked design (tool tier map, write floor, component and promotion seams) are the ones cross-referenced from the decision log this session and are to be re-derived if the revisit trigger ever fires.
- Supersedes: nothing. Closes the MCP-client direction; does not affect joe mcp (server direction) or any credential or component decision.

---

## D-0066 — Joe's capability surface is documented as a single Concepts explanation page ("Capabilities") with an "everything is a Read" spine; observation mode is presented as available-not-default; the web_search narrative moves from Configuration to Concepts

- Date: 2026-07-01
- Status: accepted (implemented)
- Session: capability-map
- Decision: The full capability surface of a built-from-source `joe` gets **one new
  published Concepts page**, `docs/public/concepts/capabilities.md` (title "Capabilities",
  weight 100 — the next free ascending-by-ten slot after the knowledge-graph page at 90,
  per D-0052). It is **explanation only** (D-0052 Concepts discipline): no config tables,
  no how-to steps, no API request/response listings, and no internal `file:line`
  citations; where a reader must act it links onward (Configuration for keys, Integrations
  for the per-type component set, Guides for registration, the safety Concepts pages for
  governance).
  - **Spine — "every capability is a Read."** The page organizes the whole surface around
    the invariant that everything Joe does out of the box inspects the managed system's
    state without changing it, so it classifies `ActionRead` and passes the write floor
    unconditionally. This is re-derived truth, not assertion: the shared-tool registration
    site registers a set of Read-class Go-native tools (`registerSharedTools`,
    `internal/tools/default.go`) — network/system diagnostics, an HTTP fetch restricted to
    `GET`/`HEAD` (`httpreq` `allowedMethods`), and a web search that returns ranked
    title/URL/snippet only and never fetches page bodies (D-0064) — each carrying an
    explicit `ActionRead` row in `toolRegistry` (`internal/safety/tier.go`). The MCP
    surface (`joe mcp`) registers only read tools (graph/k8s/metrics/logs/traces/alerts/
    knowledge). No volatile count is stated anywhere: the shared-tool set, component-type
    set, and edge types are expressed **structurally or as pointers** (D-0032).
  - **Three capability classes** are named: (1) built-in component-independent read tools,
    with fetch and search framed as **separate-and-composing** (search discovers a URL,
    fetch retrieves a URL already held); (2) the **component-backed observe surface** Joe
    opens once a component is promoted, governed by RBAC/zones/read posture, pointed to
    Integrations for the per-type set; (3) **MCP as read-as-context** — a coding agent
    reads Joe's live graph/state as context while it writes infrastructure-as-code
    (D-0034), explicitly *not* a code-driven change pipeline.
  - **Observation mode is available, not the boot default.** The page (and the corrected
    Overview and observation-mode Concepts pages) state Joe **can be run read-only** via
    `JOE_MODE=observation`, never that it ships/boots read-only. This tracks the
    implementation truth: `ResolveWriteFloor(panicStatePresent, observationEnvSet)`
    (`internal/safety/floor.go`) returns a floor that is **DOWN** when neither input is set
    — a normally started Joe boots writable and governance decides each mutation (D-0018
    implementation note). The default-observation posture (D-0019) remains **pending/not
    wired**, so the wording is "can run," not "by default."
  - **web_search narrative relocated.** The "what web search does / discovers-vs-fetches /
    compose" narrative moves from the Configuration page into the new Concepts page;
    Configuration keeps the **keys only** (`web_search.provider` / `base_url` / `api_key`
    and the `JOE_WEBSEARCH_*` overrides) plus a one-line cross-link to Capabilities. This
    applies the D-0032 structural framing and D-0052 discipline consistently.
- Basis: Re-derived from the live tree (read-only Phase 1): shared-tool registration
  (`internal/tools/default.go` `registerSharedTools`), the `ActionRead` classification of
  every shared tool and the `GET`/`HEAD` restriction (`internal/safety/tier.go`,
  `internal/tools/shared/httpreq/httpreq.go`), the web-search return shape
  (`internal/tools/shared/websearch/websearch.go`), the write-floor default
  (`internal/safety/floor.go` `ResolveWriteFloor`), the MCP read-only tool set
  (`internal/mcp/server.go`), and the shipped-truth gate
  (`docs/backlog/public-docs-feature-inventory.md`). No PARTIAL or unwired feature is
  described as realized.
- Supersedes: the Overview "Observation mode is the day-one default" section and the
  observation-mode Concepts page's "day-one default / freshly installed" phrasing — both
  corrected to "can run read-only via `JOE_MODE=observation`." Does not supersede D-0018
  (floor mechanism), D-0019 (default-observation, still pending), D-0032, D-0034, D-0052,
  or D-0064.

---

## D-0065 — The kubeconfig-exec credential provider is retired and deleted; clientcmd is confined by a strengthened repo-wide break-test (not removed)

- Date: 2026-07-01
- Status: accepted (implemented)
- Session: agent-identity-doc-04
- Decision: The **kubeconfig-exec credential provider is retired and deleted in full** now that
  both live kubernetes auth methods route through the per-component `auth_method`→Kind seam
  (D-0062 static-bearer, D-0063 entra-exchange). Deleted: the whole `internal/credential/kubeconfig_exec.go`
  (`KubeconfigExecProvider`, its constructor, `parseKubeconfigExecConfig`, `kubeconfigExecConfig`,
  `kubeProber`/`kubeProbeResult`/`defaultKubeProbe` and its `clientcmd` path, `expandKubeconfigPath`,
  `ExpandKubeconfigPathForTest`); the `KindKubeconfigExec` enum entry; the `ProviderForKind` case; the
  `promotionRequirements` map entry and its `kindConfigStruct` case; the `kubeconfigExecConfig` entry in
  `credentialConfigStructs` (`fields.go`); the compile-forced cascade — the `KubeSelection()` accessor,
  the now-unused `KubeSelection` struct and `Credential.kube` field; and the dead `buildArmedConfig`
  `KindKubeconfigExec` case. The `internal/credential/tildeguard` package (which existed solely to pin
  the now-deleted duplicate tilde helper against the k8s adapter's) is deleted, and the k8s adapter's
  `expandPath`/`ExpandPathForTest` — dead once the transport stopped ingesting kubeconfigs and the guard
  was gone — are pruned with it.
  - **Proof of unreachability** (established before deletion): no `wiredTypes` entry resolves to
    `KindKubeconfigExec` (kubernetes → `KindStaticBearer`); the promotion handler's effective Kind is
    `WiredProvider` (never it) overridden for kubernetes by `k8s.KindForAuthMethod` ∈ {static-bearer,
    entra-exchange} only, and a supplied `credential_provider` must equal that effective Kind or 400 —
    so no promotion can write the Kind and the `buildArmedConfig` case was dead; the adapter selects it
    via neither path (`kindForAuthMethod`). The one runtime construction path was `credential.Select`
    over a **stored** config (admin credential-status/probe only, never the adapter Connect); post-deletion
    `Select` returns an unknown-kind error for such a config, handled gracefully (`listCredentialStatus`
    reports a per-entry error; the probe returns 400) — no crash.
  - **Migration disposition — documented breaking change, NO migration.** Recon decrypted the sole
    kubernetes component in the live dev DB (`petri-test`) and confirmed it is armed pre-B with
    `credential_provider: "kubeconfig-exec"` + `in_cluster`, no `auth_method`. Such a row is **already
    un-connectable post-B** (the adapter errors on the empty `auth_method`); deletion only changes its
    admin-probe error text. A SQL migration **cannot** re-shape it — the config blob is AES-256-GCM
    encrypted at rest (the same reason the env-var uniqueness guard is application-level), and no
    automatic translation exists from the kubeconfig-exec locators to static-bearer/entra-exchange
    coordinates. With Joe distributed build-from-source only (no released binaries, no fleet of DBs) and
    the sole real instance a developer test row, the disposition is a documented breaking change:
    a pre-B kubeconfig-exec kubernetes component must be re-promoted with an `auth_method`, or deleted.
  - **Break-test strengthened, dependency confined not removed.** `clientcmd` remains a legitimate
    module dependency — the helm and nginx-ingress adapters (kubeconfig-shaped) still import it — so
    `go mod tidy` drops nothing and `go.mod`/`go.sum` are unchanged. `transport_break_test.go` is
    upgraded from a k8s-package-scoped assertion to assert `clientcmd` absence three ways: from the k8s
    transport package, from `internal/credential` (newly assertable once the dead provider left), and
    repo-wide from every production package except the accepted-importer set
    `{internal/adapters/packaging/helm, internal/adapters/networking/nginx}` — catching any new
    `clientcmd` creep into the transport or credential path.
  - **Vestigial-field prune and deferred stderr teardown.** The `promoteComponentRequest.Kubeconfig`
    and `Context` fields are pruned (no provider reads them; both static and static-bearer already
    rejected them). Dropping `kubeconfigExecConfig` removes `kubeconfig`/`context` from
    `CredentialBearingFields`, so **componentgov registration no longer rejects those two fields** —
    harmless, because nothing reads them (registration still rejects every live credential field).
    The `Credential.stderr` / `Resolution.CapturedStderr()` surface is now **vestigial** (kubeconfig-exec
    was its only producer): it always returns `""`, and it plus its admin endpoint and the
    `CredentialStatusTable` UI are **left in place** pending a separate teardown tracked in
    `docs/backlog/credential-stderr-surface-teardown.md`.
- Basis: Re-derived from the live tree (read-only recon, then implementation). Pre-B kubernetes wiring
  to `KindKubeconfigExec` confirmed at `a520898^:internal/credential/wiring.go:46`; the current wiring
  (`internal/credential/wiring.go`) maps kubernetes → `KindStaticBearer`. The `petri-test` config shape
  was read by decrypting only its top-level key names + the non-secret `credential_provider` discriminator
  (no secret values). `clientcmd`'s live importers (`internal/adapters/packaging/helm/helm.go`,
  `internal/adapters/networking/nginx/nginx.go`) verified present, so the dependency stays. `go build ./...`,
  `go vet ./...`, `gofmt -s`, and the full `go test ./...` suite (including the strengthened break-test)
  pass for this change.
- Supersedes: the deletion half of **D-0062**'s "kubeconfig-exec provider left dead-but-present for the
  slice-D removal" — that removal is now done. Builds on **D-0062** (hand-built transport, static-bearer)
  and **D-0063** (entra-exchange), which established the two live methods this deletion relies on. The
  historical **D-0026** ADR (kubeconfig-exec as the kubernetes transport) and **D-0059**/**D-0060** notes
  remain as history; this entry is the current position.

---

## D-0064 — Web search ships as a Go-native shared Read tool behind a SearchProvider abstraction, SearXNG-first, boot-only, exposed-and-deny, user-loop-only

- Date: 2026-07-01
- Status: accepted (implemented)
- Session: web-search-tool
- Decision: Joe gains a **web-search capability** as a **Go-native shared tool**
  (`internal/tools/shared/websearch`, tool `Name()` exactly `web_search`), a sibling of
  `dnsquery`/`httpreq`/`netcheck`/`sysinfo`/`traceroute` that satisfies the same
  `tools.Tool` interface and **never reaches the coretools client or accessor seam**. It is
  **distinct from `http_request`**: web search **discovers URLs**, `http_request` **fetches a
  URL the model already holds**; the two **stay separate and compose** — `web_search` returns
  **ranked title/url/snippet only** and **never fetches page bodies**. It is classified
  **`ActionRead`** via an **explicit row in the `toolRegistry` map**
  (`internal/safety/tier.go`), so it **passes the write floor unconditionally**; without that
  row `ClassifyTool` would default it to `ActionMutate` (deny-by-default) and the tool would
  be floor-blocked and policy-gated — the load-bearing invariant is pinned by
  `TestClassifyWebSearchIsRead`. The backend sits behind a **`SearchProvider` abstraction**
  (`internal/search`: interface `Provider.Search(ctx, query, count)` returning
  `[]search.Result{Title,URL,Snippet}`) with a **boot-time factory** (`search.NewProvider`)
  that selects an implementation from configuration, **mirroring the LLM-adapter factory
  pattern in spirit** — provider name plus `base_url` plus an optional key, analogous to the
  `openai-compat` model config. There is **no silent default provider**: exactly as the LLM
  adapter requires an operator to configure a provider, **web search is inert until
  configured** (empty provider → `NewProvider` returns a nil `Provider`). **SearXNG** is the
  one provider implemented — the self-hostable, **keyless** backend: a GET against the
  configured `base_url` `/search` endpoint requesting `format=json`, extracting only the
  per-result `title`/`url`/`content` fields; the optional key rides an `Authorization: Bearer`
  header when set, otherwise none is sent. **Keyed providers (Tavily, Brave) are designed-for
  and deferred** to the backlog — the abstraction leaves them a clean additive extension.
  Configuration is **boot-only**: `config.WebSearchConfig` (`web_search.provider` /
  `base_url` / `api_key`) resolved once at boot with the `JOE_WEBSEARCH_PROVIDER` /
  `JOE_WEBSEARCH_BASE_URL` / `JOE_WEBSEARCH_API_KEY` env overrides (the same JOE_-prefixed
  convention the LLM config uses), built in `cmd/joe/server.go` into `search.NewProvider`,
  sealed onto `core.Services.WebSearch`, and threaded into the tool constructor at the
  shared-tool registration point (`NewCoreRegistry` → `registerSharedTools`). A misconfigured
  backend (unknown provider, or SearXNG without a `base_url`) is **fatal at boot** as an LLM
  misconfiguration is; **there is no runtime swap handler, admin endpoint, or new audit
  vocabulary — changing the backend requires a restart**. Credentials, when a keyed provider
  is later added, ride **plain config/env like the LLM providers**, not the
  credentials-as-references model. **Exposed-and-deny**: when no backend is configured the
  tool stays **registered and advertised** and its call returns a `no search backend
  configured` **tool-error result** (it is never hidden), mirroring how denials surface as
  tool-results elsewhere — pinned by the behavioral break-test `TestExecute_NoBackendConfigured`
  and the advertisement break-test `TestWebSearchAdvertisedWhenUnconfigured`. Registration is
  **user-task-loop-only** (`internal/tools/default.go`); the **autonomous `agent:core` registry
  (`internal/coreagent`) registers its own tool set and does NOT include `web_search`** —
  agent:core registration is deferred. **Egress** is to the **single operator-configured
  `base_url`** (strictly narrower than `http_request`'s any-URL GET); **no URL allow-list, no
  rate limit, and no audit row** are added — matching the existing shared-tool posture (shared
  tools do not reach the accessor-point audit row), with external-network egress **deliberately
  not a gate dimension** (operator-run egress gateways are deployment substrate, and `base_url`
  may point at the provider directly or at such a gateway, transparent to Joe).
- Basis: Re-derived from the live tree (read-only recon). The shared-tool split is real —
  `internal/tools/shared/{dnsquery,httpreq,netcheck,sysinfo,traceroute}` are Go-native tools
  registered by `registerSharedTools` in `internal/tools/default.go`, separate from the
  accessor-backed `registerCoreTools`. The classifier map and its default are authoritative:
  `toolRegistry` in `internal/safety/tier.go` explicitly lists every read tool and
  `ClassifyTool` returns `ActionMutate` for any unlisted name, and `CheckAccess` allows reads
  unconditionally while gating mutates behind the act policy. The floor/consumer branch is
  real: `internal/api/tasks.go` injects `WithWriteFloor` into the user-task executor, which
  denies the Mutate class when the floor is up — a Read is never floor-blocked. The
  exposed-and-deny registry behavior matches the existing pattern where a tool always advertises
  via `Registry.ToDefinitions()` and a denial surfaces as a tool-result rather than a hidden
  tool. The LLM-adapter pattern this mirrors is `internal/llmfactory/factory.go` (provider
  switch) + `internal/config` (`ModelConfig` provider/base_url, `applyEnvOverrides` JOE_-prefix
  convention) + the openai-compat "optional key, base_url required" shape. `agent:core` builds a
  disjoint registry in `internal/coreagent/agent.go` (`registerCoreAgentTools`), confirming
  user-loop-only registration leaves the autonomous surface unchanged. `go build ./...`,
  `go vet ./...`, `gofmt`, and the full `go test ./...` suite (including the new break-tests)
  pass for this change, verified against a clean checkout of the commit.
- Supersedes: nothing. Additive. Establishes the `internal/search` SearchProvider abstraction
  and the `web_search` shared tool; the keyed hosted providers, `agent:core` registration, a
  runtime swap surface, and any egress rate-limit/allow-list/per-search log line remain open
  in `docs/backlog/web-search-tool.md`.

---

## D-0063 — Entra-exchange is the second Kubernetes auth method: a transport-agnostic credential Kind minting a short-lived bearer token via an Azure Entra OAuth2 client-credentials exchange

- Date: 2026-06-30
- Status: accepted (implemented)
- Session: agent-identity-doc-03
- Decision: **`KindEntraExchange`** (`internal/credential/credential.go`) is added as the
  **second Kubernetes authentication method** alongside `static-bearer`, exercising the
  per-component `auth_method`→Kind selection seam D-0062 established (decision #1 / D-0060)
  with a real second value. Its provider (`internal/credential/entra_exchange.go`)
  **mints a short-lived bearer token** via an **Azure Entra OAuth2 client-credentials
  grant**, performed with the **already-vendored `golang.org/x/oauth2/clientcredentials`**
  — **no new dependency**, and deliberately **not** the Azure identity SDK. tenant id,
  client id, audience/scope, and the **client-secret reference** are **all per-resolution
  values read from config**: the provider **hardcodes no audience and no tenant**, imports
  **no kubernetes symbol and no Azure-SDK symbol**, and applies the token nowhere — it is
  **transport-agnostic** so the deferred Azure credential track can reuse it. The minted
  token returns through the existing **non-serializable credential half**; **audience and
  expiry** surface on the **serializable diagnostic half** from the token response;
  Resolve reaches `StageMintSucceeded` and the provider's **own** Probe advances to
  `StageConnectivityProbed`. The grant type is **client-credentials with a client secret
  resolved by reference**; **federated workload-identity assertion** is **designed-for as
  an additive second source** (the `federated_token_file` field is reserved and the
  requirements at-least-one-of constraint ranges over `{client_secret_env_var,
  federated_token_file}`) but is **not built** in this slice. The client secret is resolved
  by the **call-time name-only `lookupEnv`** the static providers use, under a **DISTINCT
  field `client_secret_env_var`** (not the static-bearer `env_var`). That distinct field is
  **intentionally exempt from the D-0061/D-0062 env-var uniqueness guard** (which keys on the
  literal `env_var` field), because **one Azure app registration legitimately fronts many
  clusters** — two components sharing one `client_secret_env_var` is a valid shared-app
  case, not a collision. The **`BearerToken()` accessor is generalized** from a
  `KindStaticBearer`-only gate to a **bearer-Kind set** (`isBearer`, covering
  static-bearer and entra-exchange), so the Entra-minted token rides the **identical
  adapter consume-seam** (`resolveBearerToken` → `BearerToken()` → `buildRESTConfig`) with
  **no adapter change** — recon confirmed the adapter and builder were already
  Kind-neutral. The **promotion boundary is taught `auth_method`→Kind dispatch** for
  kubernetes (`internal/api/components.go`): the wired default stays `KindStaticBearer`, but
  for a kubernetes component the stored `auth_method` selects the effective Kind via the
  exported `k8s.KindForAuthMethod`, so the discriminator written, the shape validated, and
  the audit row match what the adapter selects at Connect; `buildArmedConfig` branches the
  kubernetes handling into the static-bearer and entra-exchange sub-shapes over the shared
  cluster coordinates, accepting `entra-exchange` as the second valid `auth_method` value
  and rejecting a mixed or incomplete shape. **Audience is required for entra-exchange** and
  is enforced in `buildArmedConfig` (the live authority); the requirements table declares it
  required and the `ValidateReference` always-permitted special-case for `audience` is
  relaxed to a per-Kind one so the describe-table↔enforcement guard test still agrees. The
  transport-agnostic invariant and the audience-from-config invariant are pinned by a
  **structural break-test scoped to the provider file** (`entra_exchange_test.go`: AST
  imports-only assertion that the provider imports no `k8s.io`/`azure`/`microsoft`/k8s-adapter
  symbol) plus behavioral coverage (two distinct audiences yield two distinct minted tokens
  and diagnostic audiences; the diagnostic surfaces audience and expiry). The Entra
  promotion UI rides this slice (`ui/src/components/admin/PromoteComponentForm.tsx`: an
  authentication-method selector revealing the tenant/client/audience/client-secret fields)
  and the full both-methods public-docs polish lands in lock-step.
- Basis: Phase-1 recon re-derived from the live tree (read-only) confirmed the gating
  finding — the adapter consume-seam was already Kind-neutral except for the
  `KindStaticBearer`-gated `BearerToken()` accessor, so generalizing it is the only adapter
  change; the four Kind-registration sites; the requirements/fields/wiring seams; the
  promotion handler deriving Kind from the single-valued `wiredTypes` map (the second seam
  the entra-exchange method exercises); and that `golang.org/x/oauth2/clientcredentials` is
  vendored and reachable (azidentity is not), so no dependency is required. `go build ./...`,
  `go vet ./...`, `gofmt`, the full `go test ./...` suite, and `ui` lint + vitest pass.
- Supersedes: nothing. Builds on **D-0062** (the static-bearer transport and the
  per-component `auth_method`→Kind seam this slice exercises with a second value) and on
  **D-0061** (the env-var uniqueness guard this slice deliberately exempts the distinct
  `client_secret_env_var` field from). It is the Entra-exchange slice (slice C) of the
  agent-identity design-of-record **D-0060** / decision #1. The remaining campaign slices —
  the kubeconfig-exec package retirement (slice D), the federated workload-identity
  assertion source, and the provenance assertion — remain open and unchanged by this entry.

---

## D-0062 — The Kubernetes transport is a hand-built rest.Config with no kubeconfig ingestion; static-bearer is its own credential Kind with env-var and in-cluster sources

- Date: 2026-06-29
- Status: accepted (implemented)
- Session: agent-identity-doc-02
- Decision: The Kubernetes adapter now constructs its `*rest.Config` **by hand** and
  **never ingests a kubeconfig**. `buildRESTConfig` (`internal/adapters/k8s/k8s.go`) sets
  exactly three fields — `Host` from the component's `api_server` coordinate,
  `TLSClientConfig.CAData` from the stored inline `ca_data` bundle, and `BearerToken` from
  the resolved credential; the `clientcmd` ingestion branch and the
  `rest.InClusterConfig()` branch are **deleted from the transport**, and an exec provider,
  auth provider, and impersonation are **never set** (Joe authenticates only as its own
  non-human identity). **CA is stored inline as `CAData`**, leaning on a self-contained
  record for a remote fleet rather than an on-disk CA path. The Kubernetes component
  `Config` gains the cluster coordinates `api_server` / `ca_data` / `namespace` and an
  `auth_method` discriminator (`internal/adapters/k8s/config.go`). **`static-bearer` is its
  own credential Kind** (`KindStaticBearer`, `internal/credential/credential.go`), distinct
  from the generic `KindStatic` so its in-cluster source stays contained to the Kubernetes
  transport and never leaks onto single-token HTTP backends. It resolves a bearer token
  from one of two locator sources (`internal/credential/static_bearer.go`): an **env-var
  source** reusing the existing call-time `lookupEnv` (only a name is stored, never the
  value) and an **in-cluster source** that **reads the pod-mounted service-account token
  directly** via `os.ReadFile` of `/var/run/secrets/kubernetes.io/serviceaccount/token` —
  re-homed out of the kubeconfig-exec provider and deliberately **not** via
  `rest.InClusterConfig()`, which would itself own host, CA, and token and defeat the
  hand-built stance. Kubernetes resolution is now **per-component**: the adapter reads
  `auth_method` and maps it to the Kind (`kindForAuthMethod`), establishing the
  per-component Kind-selection seam decision #1 and D-0060 call for; `static-bearer` is the
  only value today and is validated at promotion, with slice C adding a second value with
  no field migration. Kubernetes is **un-wired from `KindKubeconfigExec`** in
  `wiredTypes` (now `KindStaticBearer`); the **kubeconfig-exec provider package and its
  probe path are intentionally left dead-but-present** for the slice-D removal — only the
  routing changed. The **D-0061 uniqueness guard is generalized**: its already
  Kind-agnostic, process-global peer scan is unchanged, but the call-site gate now fires
  for **any promotion writing an `env_var` locator** (the generic static Kind OR the
  static-bearer env-var source), with the in-cluster source carrying no env var and exempt.
  The stance is **break-tested structurally**, scoped to the Kubernetes
  resolution-and-transport path (the k8s package's own files, never a tree-wide
  `clientcmd` grep that would match the still-present dead provider): the test fails if the
  package imports `clientcmd`, calls `rest.InClusterConfig`, references `ExecProvider` /
  `AuthProvider` / `Impersonate`, or sets `Host` / `BearerToken` / `CAData` outside
  `buildRESTConfig` — plus behavioral coverage that static-bearer resolves a token from
  each of its two sources and the adapter applies it as the bearer token
  (`internal/adapters/k8s/transport_break_test.go`, `internal/credential/static_bearer_test.go`).
  The promotion UI form (`ui/src/components/admin/PromoteComponentForm.tsx`) and a minimal
  public-docs accuracy fix land in this slice; the fuller both-methods docs polish is
  deferred to slice C (tracked in `docs/backlog/agent-identity-doc.md`).
- Basis: Phase-1 recon re-derived from the live tree (read-only) confirmed the single
  clientcmd ingestion site, the kubeconfig-shaped Config carrying none of the coordinates,
  the kubernetes-only kubeconfig-exec wiring, the in-cluster `rest.InClusterConfig()` call
  inside the provider, the promotion locator branch, the `KindStatic`-gated uniqueness
  call-site over a Kind-agnostic scan, and the vendored client-go (v0.35.2) mechanics
  showing `rest.InClusterConfig()` sets host/CA/token itself. Implementation adds the new
  Kind, provider, requirements entry, and per-component seam; rewrites the transport; and
  generalizes the guard gate. `go build ./...`, `go vet ./...`, `gofmt`, the full
  `go test ./...` suite, and `ui` lint + vitest pass; the release-shaped `make build`
  embeds the new form and the binary boots and serves the fresh UI.
- Supersedes: the Kubernetes half of **D-0026** (kubeconfig-exec as the kubernetes
  transport) and the current-state note in **D-0059** — the kubeconfig-exec provider
  remains in the tree but no longer governs the kubernetes path. Builds on **D-0061** (the
  unique per-component env-var locator the static-bearer env-var source depends on) and is
  the transport-rewrite slice of the agent-identity design-of-record **D-0060** / decision
  #1. The remaining campaign slices — the Entra-exchange method (slice C), the
  kubeconfig-exec package retirement (slice D), and the provenance assertion — remain open
  and are unchanged by this entry.

## D-0061 — Static credential environment-variable names are enforced unique per component at the promotion seam (operator-supplied, stored verbatim, never computed)

- Date: 2026-06-29
- Status: accepted (implemented)
- Session: agent-identity-doc-01
- Decision: A static credential's locator is an **operator-supplied environment-variable
  name, stored verbatim and enforced unique across the whole component set**. Promotion
  rejects an `env_var` already in use by another component. The keying is deliberately
  **not** computed and **not** componentID-derived: recon confirmed names are already
  operator-chosen and persisted as-is (`internal/api/components.go` `buildArmedConfig`
  `set("env_var", req.EnvVar)`) and read back verbatim at resolution
  (`internal/credential/static.go` `Resolve` → `lookupEnv(cfg.EnvVar)`), with the
  `ComposeEnvVarName` convention helper (`internal/credential/references.go`) dead outside
  tests — so there was nothing to switch, only a missing uniqueness check to add. The
  guard is an **application-level decrypt-and-scan at the promotion seam**
  (`staticEnvVarConflict` in `internal/api/components.go`): it lists peers, compares the
  decrypted `env_var`, excludes the component being promoted (so a re-promote to its own
  name is not a self-conflict), and returns 409 on a match. A **database UNIQUE constraint
  is impossible** because the component `config` blob is encrypted at rest
  (`internal/store/encrypted_components.go`), so the locator is opaque ciphertext at the
  SQL layer. The **scope is the whole component set, not per-type**, because environment
  variables are process-global — two components sharing a name would resolve to the same
  secret with no distinction. The **blast radius is every static-wired type**, expressed
  structurally as the `KindStatic` subset of `wiredTypes`
  (`internal/credential/wiring.go`); kubernetes is unaffected (kubeconfig-exec, no env
  segment). The **indirection-only posture is preserved**: an inline `value` is still
  refused and an empty `env_var` is still refused. **Pre-existing collisions are unhealable
  by code and out of scope**: the names are operator-chosen and the operator has already
  set those variables in the environment, so rewriting a stored locator would break
  resolution rather than fix anything — the guard is forward prevention only, with no
  startup scan and no data migration. The invariant is **break-tested**, not inspected
  (`TestPromote_StaticEnvVarUniqueness`): it drives the real HTTP promotion guard and the
  real `StaticProvider.Resolve`, failing if two distinct components can reach one variable
  or if two distinct names do not resolve to their own values.
- Basis: Phase-1 recon re-derived from the live tree (read-only), confirming verbatim
  store at `buildArmedConfig` and verbatim read at `static.go` `Resolve`, the dead
  `ComposeEnvVarName` helper (only `references_test.go` callers), the absence of any
  uniqueness guard in the promotion path, store layer, or migrations, and config
  encryption at rest. Implementation adds `staticEnvVarConflict` and a 409 reject in
  `handlePromoteComponent`, plus the break test. `go build ./...`, `go vet`, and the
  `internal/api` + `internal/credential` suites pass.
- Supersedes: nothing. Builds on **D-0026** (the credential-provider abstraction that owns
  the provider config structs and the static/kubeconfig-exec seam) and keys the references
  declared in `internal/credential/references.go`. This is the **first implementation slice
  of the agent-identity design-of-record D-0060**: a unique per-component static token
  variable is the precondition for the later static-bearer Kubernetes method. The later
  campaign slices — Kubernetes transport rewrite, the Entra-exchange provider,
  kubeconfig-exec retirement, and the provenance assertion — remain open and are unchanged
  by this entry.

## D-0060 — Joe's agent-identity and authentication stance is settled as design-of-record (captured in a held draft) — DESIGN, NOT YET IMPLEMENTED

- Date: 2026-06-29
- Status: accepted (design-of-record; implementation deferred)
- Session: agent-identity-doc
- Decision: Joe's agent-identity and authentication stance is settled as
  design-of-record and captured in a non-published held draft at
  `docs/drafts/agent-identity.md`. The stance comprises: (1) the **third-identity-class**
  framing — an agent is neither a human (authenticates assuming presence) nor a service
  (authenticates on fixed deployment scope), so its safety must come from a mediation
  layer it enforces on itself; (2) **provenance** as the authority an action traces back
  to, in two modes — *delegated* (a human originated it; originator = human, actor = Joe)
  and *autonomous* (Joe originated it; originator and actor both Joe, characteristic of
  discovery/observation) — with Joe always the actor on the wire, only the originator
  varying, and provenance orthogonal to read-vs-mutate; (3) **authenticate only as a
  non-human identity**, never the human authentication path; (4) **never ingest a human's
  kubeconfig** and **never impersonate** (never assume another identity through identity
  replacement); (5) the **provenance assertion held Joe-internal** — when a human is the
  originator, that human is recorded only inside Joe (originator, actor, action, derived
  from the authenticated session's creator principal) and never transmitted to the managed
  system, which sees only Joe's own service identity; (6) three never-collapsed planes —
  identity (who Joe may be on a system), provenance (on whose authority, Joe-internal),
  governance (the floor: what Joe may do now), with the invariant that a valid credential
  never implies a permitted action; (7) a **two-method Kubernetes target** — a static
  bearer method (long-lived bearer token as an `Authorization: Bearer` header, for
  OpenShift and self-managed/local clusters via a ServiceAccount token) plus a native
  **Entra exchange** method (Joe performs an Azure Entra OAuth2 token exchange to mint a
  short-lived bearer token for AKS); and (8) **client-certificate authentication
  permanently excluded** as a matter of stance, because it is a human authentication path.
  The draft is held in `docs/drafts/` — a new non-published staging directory deliberately
  outside `docs/public` (the sole published surface, D-0052), because no generator config
  exists that could exclude a subdirectory from single-sourcing, so anything under
  `docs/public` risks publication. The intended eventual home is the Concepts section as a
  single explanation page; that and the implementation work are tracked in
  `docs/backlog/agent-identity-doc.md`.
- **DESIGN, NOT YET IMPLEMENTED.** This entry records a settled stance, not shipped
  behaviour. The **current shipped Kubernetes credential path remains the
  kubeconfig-or-in-cluster locator** — the `kubeconfig-exec` provider
  (`KindKubeconfigExec`, `internal/credential/credential.go:40`) wired to `kubernetes` at
  `internal/credential/wiring.go:46`, resolving an in-cluster service account or a
  kubeconfig file (`internal/credential/kubeconfig_exec.go:147-178`, in-cluster fallback at
  `:150-151`) and consumed by the adapter at `internal/adapters/k8s/k8s.go:131`. The two
  credential-provider kinds that exist today are `KindStatic` (`:36`) and
  `KindKubeconfigExec` (`:40`); kubernetes uses the latter. This stance will be realized by
  a future ADR and the code change that retires the kubeconfig-exec locator for kubernetes
  in favour of the static-bearer and Entra-exchange methods; until then this is direction,
  not truth on the wire.
- Basis: re-derived from the live tree this session (read-only, no code changed). Credential
  wiring confirmed at `internal/credential/wiring.go:46` (`store.ComponentTypeKubernetes:
  KindKubeconfigExec`) and `internal/credential/credential.go:36,40`; the kubeconfig/in-cluster
  locator at `internal/credential/kubeconfig_exec.go`; adapter integration at
  `internal/adapters/k8s/k8s.go:131`. Staging-location safety confirmed against D-0052: no
  Hugo/Hextra/Netlify/Vercel generator config exists in the tree, everything under
  `docs/public/` publishes, and `docs/drafts/` did not previously exist — so the draft sits
  outside the published surface. The stance's foundational pattern follows RFC 8693
  (delegation vs. impersonation; the composite-actor pattern) and references Kubernetes
  impersonation as the native mechanism Joe declines to use; emerging agent-identity
  standardization work is pointed to by shape only (no Internet-Draft names/numbers/versions,
  per the volatility of that work).
- Supersedes: nothing yet — this is design-of-record that will eventually supersede the
  current-state credential decisions once the implementing ADR and code land. It records the
  intent to supersede **D-0026** (the credential-provider abstraction that established the
  kubeconfig-exec launch model for kubernetes) and the kubernetes-credential current-state
  confirmed by **D-0059** (kubernetes wired to the kubeconfig-exec provider). No code,
  CLAUDE.md authentication invariant, or public page is changed by this entry; CLAUDE.md is
  touched only to record the new `docs/drafts` staging convention.

## D-0059 — The component-registration how-to lives in Guides (Kubernetes first), and Quickstart includes registering one Kubernetes component

- Date: 2026-06-28
- Status: accepted
- Session: component-registration-guide
- Decision: The task-oriented, web-UI walkthrough for bringing a component under Joe's
  management is a **Guides** page, not new prose in Integrations or Concepts. The first
  such page, `docs/public/guides/register-kubernetes.md` (`weight: 15`, slotted directly
  after the web-UI login guide), covers **Kubernetes only**: register the component (it
  lands inert), assign it a zone, promote it with a **kubeconfig-exec** reference
  (in-cluster identity or kubeconfig path — indirection-only, no inline secret), then run
  the UI **Test Connection** affordance, which constructs and registers the live adapter
  with no restart. Other component types — including the boot-config-only types that come
  live only at a daemon restart per D-0056 — are deferred to further sections or sibling
  Guides pages and are *not* covered here. The page links to Concepts for the "why"
  (inert registration, governed promotion, credentials-as-references) rather than
  re-explaining, and cross-references Integrations instead of duplicating its per-type
  routing table (D-0055 / D-0056): Integrations' Kubernetes entry links forward to the
  Guide and the Guide links back. Separately, **Quickstart now includes registering and
  promoting one Kubernetes component** as a first-class step (a new Step 5 / Step 6),
  pointing at the Guide for the click-by-click detail and ending on an agentic ask that
  reads the live cluster — establishing the policy that Quickstart demonstrates a real
  registered component rather than ending on an empty graph.
- Basis: re-derived from the live tree this session. The web UI exposes the affordances
  the Guide documents and only those: the Components page renders an admin-gated
  **+ Register Component** form (type selector populated from the backend type enum;
  fields id/type/name; no credential field), an admin-gated **Promote** action whose
  Kubernetes form offers a kubeconfig path, an optional context, and an in-cluster
  checkbox with no secret field, and a **Test Connection** button available to any
  authenticated principal; zone assignment for a freshly registered component is the
  admin-gated unassigned-components control on the Zones admin page. The server accepts
  this exactly: `kubernetes` is wired to the kubeconfig-exec provider, promotion writes a
  reference and refuses an inline `value`, and the connectivity-test handler builds the
  adapter, connects, and registers it live in-process (no restart). The Test affordance is
  real and load-bearing for the runtime-registerable Kubernetes path, so the Guide's
  happy path ends on a Test Connection click; the open `governed-connectivity-check-surface`
  backlog item (the test route is not admin-gated and an inert component has nothing to
  probe) is noted as deferred and does not block documenting the affordance that ships.
  Claims were gated against `docs/backlog/public-docs-feature-inventory.md` per the
  D-0052 / D-0055 shipped-truth rule; no internal `file:line` citations appear on any
  public page; and per D-0032 no volatile count (number of component types or credential
  providers) is hardcoded on a public page — the Guide points to Integrations for the
  per-type set.
- Supersedes: nothing — adds a Guides page and a Quickstart step; refines the
  D-0055 / D-0056 Integrations-as-routing-index split by placing the UI walkthrough in
  Guides and cross-linking the two.

## D-0058 — Six dead-on-arrival component types are removed from the registrable set at the single authoritative seam so they are unregistrable through every surface

- Date: 2026-06-28
- Status: accepted
- Session: trim-deadonarrival-component-types
- Decision: Six component types that are non-functional at runtime regardless of
  config — `oci_registry`, `dockerhub`, `artifactory`, `ecr`, `cloudwatch`,
  `azuremonitor` — are removed from the authoritative registrable-type set
  (`store.AllowedComponentTypes` / `store.IsValidComponentType`,
  `internal/store/constants.go`). Because every registration surface consults that
  single seam — the HTTP create endpoint (`handleCreateComponent`,
  `internal/api/components.go`), the `register_component` LLM tool
  (`RegisterComponentTool.Execute`, `internal/coreagent/agent.go`), and the web
  registration form (which populates its type selector from
  `handleListComponentTypes` → `AllowedComponentTypes`) — all three now uniformly
  reject the six with exactly the invalid-type response a wholly unknown type
  already takes. No surface is special-cased. The four registry constants
  (`ComponentTypeOCIRegistry/DockerHub/Artifactory/ECR`) remain DEFINED because the
  coreagent refresh type-switch (`internal/coreagent/refresh.go`) still names them;
  a dead-on-arrival comment at the constant block records that they are
  unregistrable and why. The two with no adapter code at all
  (`ComponentTypeCloudWatch/AzureMonitor`) are deleted entirely — nothing outside
  the registrable lists referenced them. Read paths are type-agnostic (GET, list,
  and Test handlers read and serialize without validating the stored type), so a
  previously-stored row of a removed type still lists/reads; no read-path tolerance
  was needed and none was added. This closes a launch credibility gap: a type the
  operator could register but that could never function, with the runtime Test
  control reporting a misleading "no connection to test" success.
- Basis: re-derived from the live tree this session. Neither construction map
  contains any of the six — not the boot path (`connectSourcesDefault`,
  `cmd/joe/server.go`, which builds kubernetes/git/aws/azure/falco/datadog/splunk/
  dynatrace/newrelic/github/gitlab) nor the runtime path (`newAdapterForType`,
  `internal/api/components.go`) — so no adapter is ever constructed for them and the
  refresh cases for the four registry types can never be reached (dead but
  harmless). `cloudwatch`/`azuremonitor` had no adapter package at all; their
  constants were referenced only by the two registrable lists. Verified by tests:
  `TestHandleCreateComponent_DeadOnArrivalTypesRejected` (HTTP, all six →
  400/`invalid_component`), `TestRegisterComponentTool_DeadOnArrivalTypesRejected`
  (tool path, all six plus an unknown-type baseline → error, nothing persisted),
  and added negative cases in `TestIsValidComponentType`. The existing
  `TestHandleCreateComponent_FallthroughTypes` was updated to drop
  oci_registry/artifactory/ecr while keeping the boot-only group
  (github/gitlab/datadog/splunk/dynatrace/newrelic) and the empty-config-Connect
  group (helm/nginx-ingress) unchanged.
- Rejected alternatives: (1) gating the type only in the UI dropdown — rejected
  because the backend would still accept the six through the HTTP create endpoint
  and the `register_component` tool, leaving the dead-on-arrival hole open on every
  non-UI surface. (2) wiring the four adapter-bearing types into a construction map
  and building the two missing adapters now — rejected as a feature change unfit for
  launch; that work is deferred as separate post-launch items
  (`docs/backlog/trim-deadonarrival-component-types.md`).
- Supersedes: nothing — narrows the registrable set established incrementally
  across the Phase 6.13 / Phase 10 type additions.

---

## D-0057 — A config-less registration is made to persist by normalizing an absent/empty config to an empty JSON object at the shared registration seam, before encryption

- Date: 2026-06-28
- Status: accepted
- Session: register-component-config-default
- Decision: Both registration paths — the HTTP create handler
  (`handleCreateComponent`, `internal/api/components.go`) and the
  `register_component` LLM tool (`RegisterComponentTool.Execute`,
  `internal/coreagent/agent.go`) — now normalize an absent or empty registration
  config to an empty JSON object (`"{}"`) at a single shared seam,
  `componentgov.NormalizeRegistrationConfig`, mirroring the single-source
  `componentgov.RejectCredentialFields` credential-rejection seam so the two
  surfaces cannot drift. Normalization runs BEFORE config encryption, so the
  defaulted object flows through the normal encrypt-at-rest path
  (`encryptedComponentRepository.encryptConfig`) and is stored encrypted like any
  other config — never a plaintext value that bypasses encryption. The
  components.config column stays `TEXT NOT NULL` with no default, and the
  encryption-at-rest invariant is preserved. This honors the D-0029 design that a
  registration writes type + name + non-credential routing config only and lands
  inert: a config-less registration must succeed. All other behavior is unchanged
  — credential-bearing fields are still rejected (the empty object trivially
  passes `RejectCredentialFields`), the no-Connect-probe structural guard stays
  green, and the credential-less, inert-on-registration posture is intact.
- Basis: re-derived from the live tree this session. The UI registration payload
  omits `config`; the handler treats `createComponentRequest.Config`
  (`json.RawMessage`) as optional, but the `components.config` column is
  `NOT NULL` with no default (`internal/store/migrations/001_initial.up.sql`,
  preserved verbatim by the 023 source->component table rename) and nothing
  defaulted an absent config, so a nil blob reached the INSERT
  (`sqlComponentRepository.create`), tripped the NOT NULL constraint, and
  surfaced as a generic HTTP 500. The encrypt path short-circuits a zero-length
  config (`encryptConfig` returns it unchanged when `len == 0`), so only a
  non-empty `"{}"` both satisfies the column and round-trips through encrypt/
  decrypt — verified by store-, HTTP-handler-, and tool-path regression tests
  (`TestEncryptedComponentRepository_EmptyObjectRoundTrip`,
  `TestCreateComponent_AbsentConfigPersistsInert`,
  `TestRegisterComponentTool_AbsentConfigPersistsInert`). The suite previously
  stayed green only because every create-path helper always supplied a config
  (`TestRegisterComponentTool_Execute`'s "missing config" case even pinned the
  old reject-absent behavior, now corrected to expect inert success).
- Rejected alternative: relaxing the schema — making `components.config` nullable
  or giving it a column default. Rejected because it would let a component carry a
  NULL or unencrypted-default config, weakening the "config is a non-null
  encrypted JSON object" invariant and spreading the default across the schema
  rather than concentrating it behind one application-level normalization seam the
  way the credential-rejection rule is concentrated. Defaulting at the shared seam
  keeps the column constraint and the encrypt-at-rest path untouched.
- Supersedes: nothing — restores the D-0029 config-less-registration invariant
  that had regressed undetected.

---

## D-0056 — The Integrations section routes each documentable type by its actual activation path: runtime-registerable vs boot-config-only

- Date: 2026-06-28
- Status: accepted
- Session: docs-public-establishment-pass-04
- Decision: A refinement of the D-0055 gate. Passing the D-0055 Connect-AND-armable gate
  proves a type *can* be brought up, but not *how*. The 18 documentable types split into two
  bring-up paths that are not interchangeable, and the Integrations page must route each type
  to the one that actually works for it:
  - **Runtime-registerable (13):** `kubernetes`, `prometheus`, `mimir`, `loki`, `tempo`,
    `jaeger`, `alertmanager`, `pagerduty`, `grafana`, `argocd`, `falco`, `terraform`,
    `envoy`. The connectivity test (`POST /api/v1/components/{id}/test`) constructs the
    adapter, authenticates, and registers the live connection immediately — no restart. This
    is the only lifecycle step that constructs through the runtime type→adapter switch.
  - **Boot-config-only (5):** `github`, `gitlab`, `splunk`, `dynatrace`, `newrelic`. These
    are armable (credential-wired, so registration and promotion both succeed at runtime) but
    have **no runtime construction path**. Their connectivity test cannot build an adapter —
    it returns `"ok": true` with a "type … has no connection to test" message and registers
    nothing. They come live only at the next daemon **restart**, when the boot connect pass
    loads every armed component of these types from the store and opens its connection. The
    bring-up is therefore register + promote (runtime) + static credential env var + restart.
  Routing a boot-config-only type down the runtime register-promote-arm-test spine dead-ends
  at the construction step — the same walked-to-a-step-that-cannot-succeed failure the D-0055
  gate exists to prevent, moved one station downstream. The page must make the split explicit
  and per-type, and must never send a boot-only type down the runtime spine.
- Basis: re-derived from the live tree this session. `newAdapterForType` (the runtime
  type→adapter switch, `internal/api/components.go:131`) has cases for the 13 runtime types
  but **no** case for `github`, `gitlab`, `splunk`, `dynatrace`, `newrelic` (they hit the
  `default: return nil`). Of the four lifecycle steps, only the connectivity test
  (`handleTestComponent`, `internal/api/webui.go:880`) calls `newAdapterForType`; register
  (`handleCreateComponent`, `internal/api/components.go:200`) constructs nothing by design,
  and promote (`handlePromoteComponent`, `internal/api/components.go:632`) validates against
  the credential wiring registry (`credential.WiredProvider`) and writes the reference — it
  too never constructs an adapter. The boot connect pass `connectSourcesDefault`
  (`cmd/joe/server.go:1056`) directly constructs and connects `kubernetes`, `git`, `aws`,
  `azure`, `falco`, `datadog`, `splunk`, `dynatrace`, `newrelic`, `github`, `gitlab` from the
  store at startup — which is the only way the five boot-only documentable types ever go
  live. All five remain credential-wired (`internal/credential/wiring.go:44,45,52,53,54`,
  `KindStatic`) with declared env segments (`internal/credential/references.go:42,43,49,50,51`),
  so promotion succeeds; only construction does not.
- Supersedes: nothing — refines D-0055 (which established the documentable set but recorded
  one uniform register-promote-arm-test spine for all 18 types). D-0055's documentable set is
  unchanged; this entry only corrects how that set is routed within the page.

---

## D-0055 — The public Integrations section documents only component types that pass the real-Connect-AND-armable (or credential-less) gate

- Date: 2026-06-27
- Status: accepted
- Session: docs-public-establishment-pass-03
- Decision: A documentation policy governs what `docs/public/integrations/` may present as
  a working integration. A component type is documentable as a connectable system **only**
  when both hold: (a) its adapter's `Connect` is a real implementation that establishes or
  probes a live client (not a skeleton/stub that merely parses config and marks itself
  connected), and (b) the type is **armable** through the governed promotion path — either
  a credential provider exists that can supply its credential shape, or the type is
  credential-less-but-functional. The launch credential mechanisms that define armability
  are exactly two provider kinds plus the credential-less case: **static** (an env-var
  indirection named `JOE_<SEGMENT>_<LABEL>`, used by every wired single-token backend),
  **kubeconfig-exec** (an in-cluster or kubeconfig-path locator, used by `kubernetes`), and
  **none** (credential-less types that function as registered: `terraform`, `envoy`).
  Applying the gate to the live tree, the documentable set is the 16 credential-wired
  types — `kubernetes` (kubeconfig-exec) plus the 15 static types `github`, `gitlab`,
  `prometheus`, `mimir`, `loki`, `tempo`, `jaeger`, `splunk`, `dynatrace`, `newrelic`,
  `alertmanager`, `pagerduty`, `grafana`, `falco`, `argocd` — plus the two credential-less
  types `terraform` and `envoy`. Types that fail the gate are **never** given a working
  procedure: `azure`, `helm`, and `nginx-ingress` have skeleton `Connect`; `git`, `aws`,
  `datadog`, and the datastore types (`postgresql`, `mysql`, `redis`, `mongodb`, `kafka`,
  `elasticsearch`) have a real `Connect` but no governed credential path; `cloudwatch` and
  `azuremonitor` have no adapter package; and `oci_registry`, `dockerhub`, `artifactory`,
  `ecr` resolve to an adapter-not-found error when registered. For these, a single honest
  "not yet supported" line is the most the Integrations section may say — never a procedure
  that walks a reader to a promotion step that cannot accept their credential.
- Basis: the credential-provider surface is the authoritative armability signal —
  `internal/credential/wiring.go` (the wired-type registry, 16 entries; the promotion
  endpoint's reject-unwired authority) and `internal/credential/provider.go`
  (`ProviderForKind` ships exactly `KindStatic` and `KindKubeconfigExec`), with the
  per-type env segments in `internal/credential/references.go`. `Connect` verdicts and the
  full component-type enum (`internal/store/constants.go`, `AllowedComponentTypes`) were
  re-derived from the live adapter tree this session. Extends the shipped-truth gate of
  D-0052 (and the inventory `docs/backlog/public-docs-feature-inventory.md`) with the
  specific rule for the Integrations section. D-0052 establishes the public surface and the
  general shipped-truth gate but does not record this Connect-AND-armable rule, so this
  entry is not a duplicate.
- Supersedes: nothing — refines D-0052 for the Integrations section.

---

## D-0054 — Return-path routing conventions reverted: no title-level identifier; the slug in commits and the decision log is the sole join

- Date: 2026-06-27
- Status: accepted
- Session: revert-return-path
- Decision: The return-path routing conventions added by D-0053 are **reverted**. The
  spot-code and output-echo scheme — Claude Code echoing the full slug as its first output
  line, and chat/session titles and pins leading with a derived uppercase spot-code — added
  **no improvement over ad-hoc throwaway session titles** for live routing of a Claude Code
  output back to the chat that issued it. Durable archaeology is already served by the
  **slug in commit messages and the decision log**, so **no title-level identifier is
  needed**. The "Return-path routing" section is removed from
  `docs/project/pm-convention.md`, and the output-echo acceptance criterion and the
  spot-code title paragraph are removed from
  `docs/project/claude_joe_project_instructions.md`, restoring the build-prompt acceptance
  criteria and the per-chat title instruction to their prior wording. Session titles revert
  to ad-hoc, throwaway navigation aids with no machine meaning.
- Basis: `docs/project/pm-convention.md` (the "Return-path routing" section deleted) and
  `docs/project/claude_joe_project_instructions.md` (the output-echo criterion and spot-code
  paragraph removed), both restored to their pre-D-0053 state in this session; both files
  now diff clean against the commit preceding `4c02f60`.
- Supersedes: D-0053 (the only committed return-path entry; the `return-path-conventions`
  and `return-path-titles-fix` sessions both landed their changes under it).

---

## D-0053 — Return-path routing: Claude Code echoes the full slug as its first output line; titles and pins lead with a derived uppercase spot-code, while the full slug stays authoritative in commits and the log

- Date: 2026-06-27
- Status: accepted
- Session: return-path-conventions
- Decision: Two return-path routing conventions are added to the slug join so a Claude
  Code output can be routed back to the chat session that issued it when several sessions
  run in parallel. First, every Claude Code build prompt instructs Claude Code to **begin
  its final output with the full slug on its own line**, making the returned output
  self-labeling so the destination chat is read from line one rather than matched by hand.
  Second, **chat titles, Claude Code session titles, and pinned items lead with a short
  uppercase spot-code derived mechanically from the slug** — the repo initial uppercased,
  plus the first three alphabetic characters of the slug topic uppercased, plus the
  two-digit thread suffix (e.g. `joe/ledger-03` → `JLED03`, `oasis-spec/backlog-triage-01`
  → `OBAC01`) — for fast visual spotting in a crowded list. The spot-code is **derived,
  never stored**, may collide on repo initial or slug body because uniqueness lives in the
  full slug, and **never appears in commit messages or the decision log**, where the full
  slug remains authoritative.
- Basis: `docs/project/pm-convention.md` (new "Return-path routing" section) and
  `docs/project/claude_joe_project_instructions.md` (the output-echo acceptance criterion
  and the spot-code title paragraph), both updated in this session.
- Supersedes: none.

---

## D-0052 — `docs/public` is the sole published documentation surface for joeagent.dev; `docs/reference` stays internal-only; a nine-section Diataxis-disciplined taxonomy

- Date: 2026-06-27
- Status: accepted
- Session: docs-public-establishment-pass-01
- Decision: A new `docs/public/` tree is established as the **only** documentation
  surface published to joeagent.dev. `docs/reference/` remains **internal system-truth**
  and is **not published**: reference docs are never moved, copied, or linked into the
  public tree, and internal `file:line` citations never appear on a public page. The
  public tree is organized as a **nine-section, Diataxis-disciplined taxonomy**, one
  section per nav entry, in this fixed order: **Overview, Quickstart, Concepts, Install
  and Build, Configuration, Integrations, Guides, Operations, API Reference**. No site
  generator config exists yet anywhere in the repo (no Hugo/Hextra/Netlify/Vercel
  config), so this pass **chooses** the structure rather than matching an existing one:
  `docs/public/` is the content root; each section is a directory with an `_index.md`
  carrying `title` + `weight` front-matter; nav order is the `weight` ascending in
  increments of ten; Concepts holds one explanation page per concept. Forward links use
  directory-style relative paths (e.g. `../quickstart/`) so later slices fill placeholder
  sections in place without rewiring links. Per Diataxis, **Concepts is explanation
  only** — no install steps, config tables, API request/response listings, or procedural
  how-to; where a reader must act, the page links onward. This pass fully writes Overview
  and Concepts; the other seven sections land as under-construction placeholders. Every
  claim is gated by `docs/backlog/public-docs-feature-inventory.md` (shipped-truth spec);
  no PARTIAL or PRESENT-BUT-UNWIRED feature is described as fully realized.
  **Doc-version stamping is deferred**: rather than a versioned doc tree, versioning will
  be a footer commit-and-`ui_digest` stamp applied at release-cadence time (a later
  slice), consistent with the build-truth single source in `internal/buildinfo`.
- Basis: no generator config in the tree (`find` for hugo/hextra/netlify/vercel/config.toml
  returns nothing; the only `joeagent.dev` mention is in the feature inventory). The
  shipped-truth gate is `docs/backlog/public-docs-feature-inventory.md`. The five
  operator-facing root docs named as inputs by `docs/backlog/docs-public-establishment-pass.md`
  (`configuration.md`, `integrations.md`, `operations.md`, `web-ui.md`,
  `break-glass-access.md`) remain in place as internal source material for later slices.
- Supersedes: nothing — first decision establishing a published-docs surface. Sets the
  frame the remaining slices (-02..-05) fill in, tracked in
  `docs/backlog/docs-public-establishment-pass.md`.

---

## D-0051 — Remove the dead `observability.LLMMiddleware` metrics path; `llm.NewInstrumentedAdapter` wired into `BuildLLMChain` as the single LLM-instrumentation site

- Date: 2026-06-27
- Status: accepted
- Session: llm-instrumentation-rewire
- Decision: The redundant `observability.LLMMiddleware` (`internal/observability/llm_middleware.go`,
  emitting the `llm.calls` / `llm.errors` / `llm.duration` / `llm.tokens` metric set) is
  **deleted** along with its test, having had no production caller. LLM OpenTelemetry
  instrumentation is now applied by `llm.NewInstrumentedAdapter`
  (`internal/llm/instrumented.go`), wrapped as the **outermost** decorator in
  `core.Services.BuildLLMChain` (`internal/core/llmchain.go`) — the single chain
  construction site shared by boot (`cmd/joe/server.go`) and both model-swap handlers
  (`internal/api/models.go`, `internal/api/llmsettings.go`), so every live adapter (boot
  or hot-swapped) emits identical LLM metrics and spans. The live `llm.*` metric set is
  **not frozen here**: it is whatever `NewInstrumentedAdapter` declares as inline literals
  in `internal/llm/instrumented.go` (today `llm.requests` / `llm.errors` /
  `llm.tokens.input` / `llm.tokens.output` / `llm.request.duration`); that declaration is
  authoritative and `docs/reference/observability.md` mirrors it. This commit also drops
  the unrelated dead **cache** metrics (`cache.lookups` / `hits` / `misses` / `errors` /
  `duration`, `Metrics.RecordCacheLookup`, `AttrCacheName`) from
  `internal/observability/metrics.go` + `metric_names.go`, which likewise had no non-test
  caller. This closes the doc-ahead-of-code split: `observability.md` already described
  this exact state on `origin/main` and needed no further edit.
- Basis: post-deletion tree has zero references to `LLMMiddleware` / `NewLLMMiddleware`
  and to the `llm.calls` / `llm.duration` / `llm.tokens` literals (the surviving
  `llm.errors` hits belong to the live `internal/llm/instrumented.go` path); zero
  references to `RecordCacheLookup` / `MetricCache*` / `cacheMetrics`. `BuildLLMChain` is
  the single construction site, enforced by
  `internal/llmusage/wrap_once_guard_test.go`. Live metric names at
  `internal/llm/instrumented.go:62,70,78,86,94`; doc table at
  `docs/reference/observability.md:105-109`. `go build ./...`, `go vet`, and
  `go test ./internal/observability/ ./internal/core/ ./internal/llm/ ./internal/llmusage/`
  pass with the dead path removed; integration tests (`-tags=integration`) compile.
- Supersedes: the `observability.LLMMiddleware` instrumentation path and its
  `llm.calls`/`llm.errors`/`llm.duration`/`llm.tokens` metric names (deleted), and the
  dead `cache.*` observability metrics (deleted). Backlog/investigation findings that
  recorded this dead code (`docs/backlog/public-docs-feature-inventory.md`,
  `docs/backlog/investigations/llm-egress-chokepoint-and-provenance-feasibility.md`,
  `docs/backlog/done/docs-reference-audit.md`) are resolved by this removal; they remain
  as historical findings under their own campaigns.

---

## D-0050 — Retire-and-absorb the dated `accessor-promotion-state-axis.md` investigation; inert-create + governed-promotion + accessor-governed-refresh absorbed into `security-in-layers.md`

- Date: 2026-06-27
- Status: accepted
- Session: docs-reference-audit-04
- Decision: `docs/reference/accessor-promotion-state-axis.md` is **retired (deleted)**
  under a retire-and-absorb disposition. It was a dated (2026-06-14) one-shot
  investigation whose central verdict — "the only component-state axis the permit
  decision reads is the component's zone assignment; there is NO promotion/read-only/
  lifecycle field, only the presence/absence of a zone-assignment row" — was overtaken
  by D-0028 (`auto_promote_reads`, the per-component-type dynamic read-admit predicate),
  D-0030 (the read-only→armed promotion transition that owns credential entry), and the
  A001-COREGOV CC-05/CC-08 change that routes the autonomous refresh read through the
  access seam. A read-only survivor check re-derived the live model from code and
  partitioned every present-tense claim before deletion.
  - **Absorbed into `security-in-layers.md`**:
    - Part 2, new "Component lifecycle: inert registration → governed promotion"
      subsection — `POST /api/v1/components` lands the component **inert**
      (credential-less by construction, no adapter connected, resolving to the
      read-only `unassigned` zone; `internal/api/components.go:192-199,247-252`), and
      credential entry is owned by the single admin-gated, audited read-only→armed
      **promotion** transition (`POST /api/v1/components/{id}/promote` →
      `handlePromoteComponent`, `internal/api/components.go:607-721`), which writes a
      credential **reference** (never an inline secret) in one fail-closed transaction
      and performs no Connect/Resolve/Probe. The stale Part 2 endpoint-table row
      ("Creates a component + registers its adapter") was corrected to the inert
      landing in the same edit, and the create/delete/promote rows re-labelled
      admin-gated + audited.
    - §3.5 — the autonomous `agent:core` background refresh resolves every component's
      adapter **through** `access.ResolveAdapter` under the `agent:core` principal at
      `ActionRead` (`internal/coreagent/refresh.go:194-216`,
      `internal/access/access.go:196-231`), so an ungranted/unpromoted component is
      denied before its adapter — and thus its credential — is resolved; the seam
      **fails closed** when unwired (refresh refuses to start / denies; CC-08,
      `internal/coreagent/refresh.go:106-118`), and it is the sole governed adapter
      path, enforced by a build-failing structural test
      (`TestInvariant_NoUngovernedAdapterOrGraphAccess`).
  - **Dropped** (died with the doc): the overtaken "single zone-assignment axis / no
    promotion field" verdict; the now-false "component create → `adapter.Connect` then
    `Adapters.Register`, bypasses the seam" and "Core Agent refresh → `Adapters.Get`,
    bypasses the seam" claims (refresh is governed through the accessor, CC-05); the
    "`rbac.Action` enum is four constants" claim (the live set is six —
    `read`/`query`/`mutate`/`delete` + `declare_incident`/`resolve_incident`,
    `internal/rbac/zones.go:10-33` — already correctly stated in `security-in-layers.md`
    §8.1); the entire Q5 dispatch-bypass enumeration; and every `file:line` citation.
  - **Not absorbed (judged still-true findings/caveats, out of scope)**: the RBAC-
    disabled (nil-engine) permit-all fallback (`reason=rbac_disabled` when neither
    service accounts nor OIDC are configured, `internal/access/access.go:140-143`) — a
    permissive dev-mode caveat overlapping `docs/backlog/full-mode-rbac-track.md`, not a
    protective invariant; and the permit-precedes-backend / no-infra-call-on-denial
    property, substantially covered by Part 2's accessor-as-sole-gate description. No
    claim was left unresolved — every present-tense claim was settled from code.
  - **Retirement mechanism** follows D-0047: the doc was moved out of the repo to
    `~/joe-launch-archive/accessor-promotion-state-axis.md` (19376 bytes survive
    privately; git records a deletion); no in-repo graveyard.
- Basis: live re-derivation against the tree — promotion route at
  `internal/api/server.go:211`; inert create + governed promote at
  `internal/api/components.go:192-199,247-252,607-721`; `auto_promote_reads` predicate
  at `internal/rbac/policy.go:285-330` over migration
  `internal/store/migrations/024_agent_read_promotions.up.sql`; accessor-governed
  refresh at `internal/coreagent/refresh.go:194-216` /
  `internal/access/access.go:196-231` with engine separation at
  `cmd/joe/server.go:722` (`NewPolicyEngineWithPromote`, posture nil) vs `:856`
  (`NewPolicyEngineWithGovernance`); six-constant action enum at
  `internal/rbac/zones.go:10-33`.
- Supersedes: `docs/reference/accessor-promotion-state-axis.md` in full (deleted). Its
  open audit findings (9) are marked RETIRED in
  `docs/backlog/docs-reference-audit.md`, the remaining-open MISALIGNED total
  recomputed to 13; that campaign stays open.

---

## D-0049 — Retire-and-absorb two dated direct-HTTP-mutation investigation docs; their still-true invariants absorbed into `security-in-layers.md`

- Date: 2026-06-27
- Status: accepted
- Session: docs-reference-audit-03
- Decision: `docs/reference/direct-http-mutation-surface.md` and
  `docs/reference/managed-system-egress-map.md` are **retired (deleted)** under a
  retire-and-absorb disposition. Both were dated (2026-06-09) one-shot
  investigations whose central premise — a vestigial direct-HTTP managed-system
  mutation surface that bypasses the write floor — was removed by `540f5e5`
  (`internal/api/vcs.go`, `internal/api/gitops.go`, `internal/client/vcs.go`, the
  `/knowledge/proposals/{id}/publish` route, and the orphan
  `accessor.GitLabRequestChanges` no longer exist). A read-only survivor check
  partitioned every present-tense claim before deletion.
  - **Absorbed into `security-in-layers.md`** (Part 2, "Managed-system mutations"):
    the surviving managed-system mutation path is a single in-process seam — tool
    executor → in-process core client → RBAC accessor → vendor adapter — with the
    two enforcement seams at distinct layers: the write floor is checked **only** in
    the executor (`internal/tools/executor.go:215`) and the accessor carries no
    floor check (`internal/access/` references no write floor), while RBAC + the
    audit row are written **only** by the accessor (`internal/access/access.go:132`),
    which is the sole RBAC gate because the HTTP transport's
    `rbac.EnforcementMiddleware` is a pass-through (`internal/rbac/middleware.go:81`).
    There is no HTTP route that mutates a managed system post-`540f5e5`.
  - **Dropped** (died with the docs): all bypass verdicts; the deleted-route,
    handler, `registerVCSRoutes`, `internal/client/vcs.go`, and orphan-accessor
    findings; the now-false "publish_doc_update gated under `confluence_publish` for
    ALL targets" claim (the tool selects its policy key per target,
    `internal/safety/tier.go:229-233`); the now-false "Core Agent refresh bypasses
    the accessor" claim (refresh is governed through the accessor, CC-05); and every
    `file:line` citation. Two still-true read-side properties (SELECT-only
    datastore-query enforcement inside the adapter; the doc-drift detector's own
    `http.Client` read egress) were judged findings/caveats rather than protective
    mutation invariants and out of this retirement's scope, so they were **not**
    absorbed.
  - **Retirement mechanism** follows D-0047: the two docs were moved out of the repo
    to `~/joe-launch-archive` (bytes survive privately; git records a deletion); no
    in-repo graveyard.
- Basis: `git show --stat 540f5e5` (route/handler/client removal); confirmed absence
  of `internal/api/vcs.go`, `internal/api/gitops.go`, `internal/client/vcs.go`, the
  `/publish` route in `internal/api/proposals.go`, and `GitLabRequestChanges`
  anywhere in `internal/`; surviving mechanism verified at
  `internal/tools/executor.go:215`, `internal/access/access.go:132` (+ no
  `WriteFloor` reference in `internal/access/`), `internal/rbac/middleware.go:81`,
  `internal/api/inproc_client.go:646,651,666`, `internal/api/publish.go:19`,
  `internal/safety/tier.go:229-233`.
- Supersedes: `docs/reference/direct-http-mutation-surface.md` and
  `docs/reference/managed-system-egress-map.md` in full (deleted). Their open
  audit findings (11 + 11) are marked RETIRED in
  `docs/backlog/docs-reference-audit.md`; that campaign stays open.

---

## D-0048 — Generic OpenAI-compatible adapter (`openai-compat`); provider set is config-driven via `base_url`; native `claude`/`gemini` retained

- Date: 2026-06-27
- Status: accepted
- Session: openai-compat-adapter
- Decision: Joe gains a third LLM provider, `openai-compat`, a generic adapter
  that speaks the OpenAI Chat Completions protocol against **any** compatible
  server (OpenAI, vLLM, llama.cpp, Ollama, LocalAI, …) at a configurable
  `base_url`. Specifics:
  - **New adapter, native clients untouched.** `internal/llm/openaicompat/`
    implements `llm.LLMAdapter` (`Chat` + `Embed`) as a small, dependency-free
    HTTP client against `/v1/chat/completions` and `/v1/embeddings`. The native
    vendor-SDK `claude` and `gemini` clients are neither removed nor rerouted;
    the factory switch (`internal/llmfactory/factory.go`) adds an explicit
    `openai-compat` case ahead of the gemini default.
  - **Config-driven provider set.** `ModelConfig` gains a `BaseURL` field
    (`base_url`), consumed only by `openai-compat` and ignored by the native
    clients. The validated set lives in two authoritative places — the factory
    switch and the validation allow-list (`internal/config/validation.go`,
    both `ValidateAPIKeys` and `ValidateAPIKeysWithUserMessage`). For
    `openai-compat`, validation gates on `BaseURL` presence and accepts an
    **empty** `OPENAI_API_KEY` (keyless local endpoints); unknown providers are
    still rejected by the default case. `AutoSelectProvider` is unchanged — it
    stays key-presence-based over the native providers, and `openai-compat` is
    chosen explicitly via provider + `base_url`, never auto-selected.
  - **Wire-shape control is why this is hand-rolled, not SDK-backed.** A stable
    Go SDK exists (`github.com/openai/openai-go` v1.12.0, base URL via
    `option.WithBaseURL`), but it now defaults to `max_completion_tokens`, which
    many compatible servers reject; owning the request lets us emit `max_tokens`
    (what generic servers accept) and an optional Authorization header. Anthropic
    also ships an OpenAI-compatibility shim, but it is **test-only** and does not
    carry native `tool_use`; routing Claude through an OpenAI path would lose
    tool-calling parity. Native `tool_use` is required, so the native clients
    stay; the generic adapter is additive.
  - **Governance is unchanged.** The adapter emits only neutral `llm.ToolCall`
    values and has no execution authority; execution flows solely through the
    tool executor, whose gate order (floor > incident > RBAC) is untouched. A
    governance break-test proves an adapter-produced mutate tool call is denied
    by the write floor in observation mode exactly as for the native providers.
- Basis: `internal/llm/openaicompat/openaicompat.go` (Chat/Embed, request and
  both-direction tool mapping, optional key); `internal/llmfactory/factory.go`
  (switch case + `HasProviderAPIKey`); `internal/config/validation.go` and
  `internal/config/config.go` (`BaseURL`, allow-list, BaseURL gate);
  `internal/env/keys.go` (`OPENAI_API_KEY`); tests in
  `internal/llm/openaicompat/{openaicompat_test.go,governance_test.go}` and
  `internal/config/validation_openaicompat_test.go`. Go module probe confirmed
  `github.com/openai/openai-go` resolves to v1.12.0.
- Supersedes: the fixed-count "exactly two providers / no OpenAI adapter"
  framing in `CLAUDE.md`, `docs/reference/joe-architecture.md`, and
  `docs/configuration.md` (reworded structurally per D-0032 — no hardcoded
  provider count). Deferred work tracked in
  `docs/backlog/openai-compat-adapter.md` (streaming, Azure-style auth/url
  variant, embeddings capability negotiation beyond a clear error).

---

## D-0047 — Docs tree restructured (`docs/project`, `docs/reference`, `docs/backlog/investigations`); wholly-superseded process-exhaust archived OUT of the repo — no in-repo `docs/archive`

- Date: 2026-06-27
- Status: accepted
- Session: docs-tree-restructure
- Decision: the flat `docs/` tree was reorganized into three homes, and the
  retirement mechanism for dead docs was settled.
  - **No in-repo `docs/archive`.** Wholly-superseded **process-exhaust** —
    point-in-time plans, phase prompts, completed-milestone logs, and
    one-shot investigations that nothing live points to — moves to the
    **external** `~/joe-launch-archive` directory, **outside** the repo (a
    plain `mv`; git records a deletion and the bytes survive privately). The
    repo does not carry a graveyard. Files leaving this way include the
    `PHASE-0/1/2` session-model docs, `PLAN-OF-RECORD-RECONCILED.md`, the
    `joe-identity-phase-*` prompts + plan, `milestones-completed.md`,
    `may_16th_refactor_plan.txt`, `case-study-kiro-incident.md`,
    `security-findings-punchlist.md`, the `safety-reasoning-articulation`
    prompt, and nine credential/governance investigations. Where source
    comments cited a now-archived design doc (the `PHASE-0/1/2` cluster, cited
    by 22 source files incl. guard tests and migrations), the citations were
    **de-pathed in place** — the section anchors kept, the dangling filename
    dropped — so nothing in the tree points at an archived path.
  - **Archive-versus-banner primacy.** A **banner** marks a *partially*-
    superseded doc that **stays and relocates in-repo** (its as-built core is
    still normative; e.g. the `DESIGN-CHAT-SESSIONS.md` §12 banner, the
    `joe-identity-design.md` as-built banner). The **external archive** takes a
    *wholly*-superseded doc **out of the repo**. The two are mutually exclusive
    — a doc is either banner-and-kept or archived-and-removed, never both.
    `docs/backlog/done/` is unaffected: it remains the **completed-work
    archive** of finished backlog threads, distinct from both the in-repo
    banner mechanism and the external process-exhaust archive.
  - **New tree shape.** `docs/project/` holds build-meta (`DECISIONS.md`,
    `pm-convention.md`, `claude_joe_project_instructions.md`) plus the
    `adr/` annex (`D-0026…`) and its `adr/evidence/` sub-annex (the three
    investigations D-0026 names as its verification basis). `docs/reference/`
    holds system truth — the architecture/security/identity/skills/session
    design docs **plus the five flattened standing-reference current-state
    maps** (accessor-promotion, direct-http-mutation, learn-from-sessions,
    managed-system-egress, operational-modes), now peers of the architecture
    docs. `docs/backlog/investigations/` holds the live, open findings
    (agentic-path RBAC read, backlog-triage, edge-type-count, jpk-migration,
    llm-egress) below `INDEX.md`'s depth-one scope.
  - **Operator-facing root docs stay put.** `configuration.md`,
    `integrations.md`, `operations.md`, `web-ui.md`, and
    `break-glass-access.md` remain directly under `docs/` as the agreed
    operator-facing basis and the handoff to a separate, later `docs/public`
    establishment pass (tracked in `docs/backlog/docs-public-establishment-pass.md`).
    Their cross-links were repaired this session; only their final placement is
    deferred.
- Basis: executed against the live tree — every source path verified before its
  move; all inbound navigational references repaired (CLAUDE.md, README.md, the
  repo-root `DECISIONS.md` pointer, source/test comments, active backlog files,
  and intra-`reference`/root cross-links) and proven by a tree-wide grep that
  returns zero references to any moved-from or removed path; `go build ./...` and
  `go vet ./...` clean after the source-comment repointing. Historical-record
  citations (this log, the ADR + evidence annex, anything under
  `docs/backlog/done/`) were left intact as point-in-time record even where they
  name a now-removed file.
- Supersedes: establishes the docs tree layout and the external-archive
  retirement mechanism; supersedes the flat `docs/` convention. Does not
  re-decide any retired-doc content decision (D-0044/D-0045/D-0046) — those
  remain the authority for *why* their docs left; this entry records *where the
  tree now puts things*.

---

## D-0046 — `docs/JOE_SECURITY.md` and `docs/JOE_RBAC_IMPLEMENTATION.md` deleted, not relabeled; `docs/security-in-layers.md` is the sole security authority

- Date: 2026-06-27
- Status: accepted
- Session: docs-reconcile-security-consolidation
- Decision: both `docs/JOE_SECURITY.md` and `docs/JOE_RBAC_IMPLEMENTATION.md` are
  **deleted**, not relabeled or rewritten. `docs/security-in-layers.md` is the
  single authoritative security doc. The inbound navigational pointers were
  repaired to point there or removed: `README.md` (the "for the spec" line plus
  two documentation-index bullets), `CLAUDE.md` (two Reference-Documents bullets
  removed, the `security-in-layers.md` bullet broadened to name it the sole
  authority), `docs/operations.md` (two pointers), `docs/break-glass-access.md`
  (two pointers), `docs/case-study-kiro-incident.md` (a §3.3 cross-ref and two
  References entries), `docs/security-in-layers.md` (its own two back-pointers),
  and `docs/backlog/read-posture-latch.md` (the audit-target list). The
  point-in-time audit records in `docs/backlog/docs-reconcile-sweep.md` that note
  the docs existed are left intact.
- Basis: `JOE_SECURITY.md` was stale present-tense prose for an architecture the
  build diverged from — a separate `joe-security` binary, embedded/remote security
  modes, a `writeProtectedTables`/`CanWriteTable` compiled table guard, and a T3
  dry-run+countdown mutation path — none of which exist in the live tree (verified
  this session: `cmd/` holds only `joe`; no `internal/security{,svc}/` package; the
  table-guard symbols, the `security.mode` config key, and any dry-run/countdown
  mutation path all grep to zero; the action axis is the binary Read/Mutate of
  `internal/safety/tier.go` per D-0020). `JOE_RBAC_IMPLEMENTATION.md` was a
  pre-implementation RFC for an unbuilt two-binary (`joecored`/`joe-local`),
  middleware-chain RBAC with `internal/rbac/auth`+`authz` subtrees, eight identity
  providers (LDAP/Entra/AWS-IAM/GCP/mTLS/…), and a role/group Constraint engine
  (TimeWindow/IPWhitelist/MFA) — none of which exist (verified: `internal/rbac/` is
  flat; the providers, the constraint engine, and `RBACToolMiddleware` all grep to
  zero). The audit found no unique-and-true content in either doc that
  `security-in-layers.md` lacks, so nothing was folded forward — a clean delete plus
  reference repair, not a content migration. Recorded in the docs-reconcile sweep as
  §5 item 1 (DELETE-vs-relabel; DELETE chosen).
- Supersedes: retires `docs/JOE_SECURITY.md` and `docs/JOE_RBAC_IMPLEMENTATION.md`;
  `docs/security-in-layers.md` (D-0018/D-0020/D-0022/D-0041/D-0043) already
  supersedes their accurate content.

---

## D-0045 — `docs/testing-strategy.md` deleted, not rewritten; the as-built `test/` tree plus CLAUDE.md "Build / Test / Lint" are the testing authority

- Date: 2026-06-27
- Status: accepted
- Session: docs-reconcile-testing-strategy
- Decision: `docs/testing-strategy.md` is **deleted**, not rewritten. The
  authority for how Joe is tested is the real `test/` tree — `test/mocks/`,
  `test/integration/`, `test/e2e/` (plus `test/fixtures/`) — together with the
  **Build / Test / Lint** section of `CLAUDE.md`. The three inbound navigational
  pointers were repaired to point there: `README.md` (the "See … for the full
  strategy" line and the documentation-index bullet) and the References entry in
  `test/README.md` now redirect to the `test/` harness and CLAUDE.md instead of
  the deleted file.
- Basis: the doc's code samples would not compile against the live tree — it
  referenced a nonexistent `internal/useragent` (the loop is `internal/agentloop/`),
  `internal/tools/local` (real: `internal/tools/core` + `internal/tools/shared`),
  a `joecored` binary (there is one `joe` binary), and wrong constructors
  (`api.New()`/`NewWithStore` vs the as-built `api.New(services)`), all on a
  two-binary harness that no longer exists. Its structure was superseded by the
  as-built `test/{mocks,integration,e2e}` harness, verified present in-tree this
  session. Recorded in the docs-reconcile sweep as §5 item 4 (REWRITE-or-DELETE;
  DELETE chosen).
- Supersedes: retires `docs/testing-strategy.md` (no prior decision governed it).

---

## D-0044 — Migration round-trip tests derive their down/up step count from the live migration set at runtime, never as a hardcoded distance-from-top literal (extends D-0032 into test code)

- Date: 2026-06-26
- Status: accepted
- Session: post-joefile-cleanup
- Decision: the per-migration up/down/up round-trip tests in `internal/store`
  (`TestMigration017..026_027_*RoundTrip`) MUST NOT encode the number of `Steps`
  to walk down from HEAD as a fixed integer literal. The count is derived at
  runtime from the embedded migration set via two shared test helpers in
  `internal/store/migrations_steps_test.go`: `headVersion(t)` reads the highest
  version present in `migrationsFS` (the same `embed.FS` the migrator runs
  against), and `stepsDownTo(t, anchor)` returns `-(headVersion - anchor)` — the
  negative step count that reverts every migration strictly above a **fixed
  anchor version** the test names. The anchor is the version the schema must land
  AT (one below the migration under test, or that migration's own boundary for
  the 022 case, which then takes a relative `Steps(-1)` to revert itself). Because
  the anchor is a fixed known version that never shifts and HEAD is derived,
  adding a future migration on top changes the head and re-bumps every step
  automatically: **zero edits to these tests**. No round-trip test may carry a raw
  distance-from-top step literal, in code or in its narrating comments.
- Basis: this is the D-0032 principle — "volatile, growth-driven counts are
  expressed structurally, never as fixed figures" — carried from `CLAUDE.md` prose
  into test code. Before this change eight tests hardcoded the literal distance
  from HEAD: `017` `Steps(-13)`, `018` `Steps(-12)`, `019` `Steps(-11)`, `020`
  `Steps(-10)`, `021` `Steps(-9)`, `022` `Steps(-7)`, `025` `Steps(-5)`,
  `026_027` `Steps(-4)` — each a count of how many migrations sit above the target,
  which the `read-posture-latch` (028) and `joefile-removal` (029) sessions had to
  hand-bump migration-by-migration, and whose narrating comments had already
  drifted (e.g. `017` said "Stepping -8 lands the schema just below 017" while the
  literal was `-13`, and named `024` as "the head" when HEAD was `029`). The fix
  replaces each literal with `m.Steps(stepsDownTo(t, <anchor>))` over the fixed
  anchors 16/17/18/19/20/22/24/25 and rewrites the drifted comments to be
  version-agnostic. Verified: `go test ./internal/store/` green; and a temporary
  hypothetical migration `030` added on top kept every round-trip test green with
  **no edit** to any step count, demonstrating the zero-edit property directly.
  The `022` boundary test keeps its trailing relative `Steps(-1)` (revert 022 from
  its own boundary) — that is anchored, not distance-from-top, so it does not move.
- Supersedes: none — extends D-0032 (and D-0035, which operationalized it) from
  `CLAUDE.md` prose into the migration round-trip test code; no prior decision
  governed these test step counts.

---

## D-0043 — The read posture governs human-facing transport reads only; the agent:core autonomous read surface stays a separate axis governed solely by auto_promote_read + grants (corrects a coupling introduced by D-0041)

- Date: 2026-06-26
- Status: accepted
- Session: read-posture-latch-02
- Decision: the install-wide **read posture** (D-0041) is a **human read-sharing
  posture**. It governs the **human-facing transport read decision** only. The
  **autonomous `agent:core` read surface** — the background refresh path that
  resolves each component's adapter to read it — is a **separate axis**, governed
  **solely by `auto_promote_read` (per-component-type promotion) plus grants**, as
  it was before D-0041. The two axes are separated **at engine construction**: the
  agent:core refresh engine is built **without** the read-posture resolver
  (`rbac.NewPolicyEngineWithPromote(rbacRepo, promoteReadsRepo)`), so the
  `team_flat` admit is **structurally unreachable** on the refresh path — it is
  not merely disabled by a principal-type exclusion branch inside the admit. The
  transport engine continues to carry the resolver
  (`rbac.NewPolicyEngineWithGovernance(rbacRepo, promoteReadsRepo, readPostureRepo)`),
  unchanged from D-0041. **Consequence:** flipping the install read posture
  between `team_flat` and `zoned` **cannot change what `agent:core` can read** —
  the operator-controlled promotion surface is the sole authority for the
  autonomous read surface, in every posture.
- Basis: D-0041 wired `NewPolicyEngineWithGovernance` (with the read-posture
  resolver) into **both** engine-build sites in `cmd/joe/server.go` — the
  transport engine and the agent:core refresh engine. Verified live before the
  fix: the refresh path stamps the `svc:agent:core` principal on the Start ctx and
  resolves each component via `access.ResolveAdapter(..., rbac.ActionRead)`
  (`internal/coreagent/refresh.go` `resolveAdapter`), reaching the engine's
  `Decide` with a **non-empty principal set on ActionRead**; the `team_flat` admit
  (`internal/rbac/policy.go`, `if e.posture != nil && action == ActionRead &&
  len(principals) > 0`) sits **before** the `auto_promote_read` branch and admits
  any non-empty principal set with **no svc-principal exclusion** — so under the
  launch-default `team_flat` posture the refresh engine admitted `agent:core` to
  read **every** component **before** `auto_promote_read` was consulted, silently
  overriding the operator-controlled promotion surface. The fix swaps the refresh
  site to `NewPolicyEngineWithPromote` — the pre-existing constructor that sets the
  posture seam nil, yielding **byte-identical grant-plus-promote behaviour**
  (`internal/rbac/policy.go`); nothing other than the refresh engine consumed the
  posture resolver (the `refreshEngine` local feeds only its `access.New`
  accessor), so removing it changes nothing beyond the autonomous read surface it
  was wrongly widening. Tests: the axis-separation break-test
  `TestReadPosture_AxisSeparation_RefreshEngineIgnoresPosture` in
  `internal/rbac/policy_readposture_test.go` proves a `team_flat`-carrying
  transport engine admits `agent:core` on a non-promoted component
  (`team_flat_read`) while the promote-only refresh engine **denies** it
  (`no_grant`), that flipping the posture `team_flat`↔`zoned` does not change the
  refresh decision, and that a promoted type is admitted via `auto_promote_read`
  with no posture involvement. The prior-build transport `team_flat` tests, the
  `zoned` byte-identical regression, the nil-resolver behaviour-neutral test, and
  the floor-denies-mutate-independent-of-posture break-test all remain green.
- Supersedes: corrects D-0041 — it does not revert the read posture, it bounds its
  scope to human-facing transport reads and re-establishes the D-0041-era
  separation between the human read posture and the A001-COREGOV `auto_promote_read`
  surface. Closes a launch-relevant coupling under the fix-before-launch
  discipline: a default-`team_flat` install must not silently widen the autonomous
  read surface past what the operator promoted. A deferred follow-on (whether
  `team_flat` on the transport engine should admit `user:` principals only and
  leave all `svc:` named API-key principals grant-based — it currently admits any
  non-empty principal set, `svc:` included) is recorded in
  `docs/backlog/read-posture-latch.md`.

---

## D-0042 — Delete the `.joe/` repository-ingestion path (runtime no-op, stale two-binary docs, Phase-1 residue)

- Date: 2026-06-26
- Status: accepted
- Session: joefile-removal
- Decision: remove the `.joe/` repository-ingestion feature entirely — Joe no
  longer reads, interprets, or ingests any repo-authored `.joe/` directory or
  files. Deleted: the `JoeFileService` (`internal/coreagent/joefile_service.go`)
  and its interpretation system prompt (`internal/prompts/joefile.go`); the
  `.joe`-specific behavior in the git refresh path
  (`internal/coreagent/git_refresh.go`) — the `ProcessJoeFiles` call, the
  tool-call executor and its `graph_add_node` / `graph_add_edge` /
  `save_onboarding_fact` dispatchers, and the `joe_dir_present` node-metadata
  write; the `JoeFileService` field and construction on the `Refresher`; and the
  now-orphaned file cache (`internal/store/cache.go`, the `JoeFileCache` model,
  the `joe_file_cache` table dropped by migration `029_drop_joe_file_cache`).
  **Preserved, untouched:** the `git_repo` node is still built from HEAD commit
  identity (hash, date, author) on every refresh — only its `.joe`-derived
  metadata stops being written; the general onboarding/Facts store and its
  `POST /api/v1/onboarding` route (user-typed input, unrelated to `.joe`); and
  the graph `AddNode`/`AddEdge` store primitives (only the `.joe`-sourced callers
  were removed). Already-persisted `joe_dir_present` metadata in live databases
  is left in place — the write simply stops; no historical scrub.
- Basis: the path was a **runtime no-op**. The git adapter's `ListFiles(ctx, ".joe")`
  descends into the `.joe` subtree and returns bare basenames
  (`entry.Name`, e.g. `manifest.yaml`) — `internal/adapters/git/read.go:91-93` —
  but `ProcessJoeFiles` fed those basenames straight to `ReadFile`, which
  resolves `tree.File(path)` from the **repository root**
  (`internal/adapters/git/read.go:30-35`); `tree.File("manifest.yaml")` never
  matches `.joe/manifest.yaml`, so every read failed and the interpreter was
  never reached. The only tests that "passed" used a fake git adapter that
  returned full paths from `ListFiles` and keyed `ReadFile` by that same full
  path, masking the mismatch. The feature's supporting docs
  (`docs/joe-dataflow.md`, `docs/joe-prompt.md`, now deleted) described the
  retired two-binary `joecored` world (superseded by D-0033) and the pre-rename
  "sources" model (superseded by D-0021), confirming the path as unmaintained
  Phase-1 residue.
- Supersedes: none — the `.joe/` ingestion model predates this decision log (it
  is Phase-1 work with no introducing D-entry).

---

## D-0041 — Install-wide read posture: team_flat is the launch default, zoned is the full-mode read path, orthogonal to the write floor, flipped by an audited admin act

- Date: 2026-06-26
- Status: accepted
- Session: read-posture-latch
- Decision: a single persisted, install-wide **read posture** governs the RBAC
  read decision, with two values:
  - **`team_flat`** — any authenticated principal is ALLOWED for a read action on
    any component, regardless of grant. This is the **launch default**: a fresh
    install and an install upgraded from a build predating this posture both
    inherit `team_flat` (the migration seeds it) and behave identically until an
    operator deliberately opts in to `zoned`. It is the team-public read model
    already settled for sessions (DESIGN-CHAT-SESSIONS.md §12 team-wide read
    amendment: privacy between teammates is a non-goal; the spine is integrity and
    accountability, not secrecy).
  - **`zoned`** — the grant-based read decision (the **full-mode read path**):
    exactly the pre-existing zone+grant behaviour, byte-identical to before this
    posture existed. Naming the existing behaviour, not changing it.
  - The `team_flat` admit sits **after** the zone-allows-action gate, so a zone
    that forbids the read action still denies — the posture widens **WHO** may
    perform a read the zone already permits, never **WHICH** actions a zone allows
    (the same stricter interpretation D-0011 applies to admin). It fires for
    **ActionRead only** and requires a non-empty (authenticated) principal set;
    unauthenticated callers are rejected at the edge and never reach the engine.
  - The posture is **orthogonal to the write floor**. The write floor and
    write-RBAC govern mutates exactly as before, independent of read posture: a
    `team_flat` install with the floor up still denies every mutate. The posture
    has no input to the executor's floor check by construction.
  - The posture is read **live per request** from durable storage (no boot cache),
    so a change takes effect on the next decision with no restart.
  - **Flipping the posture is an admin-gated, audited operator act** on the admin
    REST surface (`GET`/`POST /api/v1/admin/read-posture`), reusing the existing
    admin-audit machinery and §4 failure posture, with a new action verb
    (`read_posture.set`, mutating/fail-closed; `read_posture.read`, read-class/
    fail-open) added to the existing `admin_access` vocabulary. The flip is the
    single deliberate operator act that moves an install from flat read to zoned
    read.
  - **Migration property:** an upgraded install inherits `team_flat` and behaves
    identically to today until an operator opts in to `zoned`.
- Basis: the read posture is modelled on the existing grant-less read-admit
  branches (the Phase-H admin short-circuit and the CC-04 auto_promote read
  admit). New code: `internal/readposture/` (the durable singleton scalar +
  sole audited write path, mirroring `internal/promotereads`), migration
  `internal/store/migrations/028_read_posture.up.sql` (singleton row seeded
  `team_flat`, mirroring the `llm_settings` shape), the `team_flat` admit and
  `ReasonTeamFlatRead`/`PostureTeamFlat`/`PostureZoned`/`ReadPostureResolver` in
  `internal/rbac/policy.go` (live resolver seam, no cache), the
  `NewPolicyEngineWithGovernance` constructor wired at both engine-build sites in
  `cmd/joe/server.go`, the `read_posture.set`/`read_posture.read` verbs in
  `internal/audit/audit.go`, and the admin handlers in `internal/api/admin.go`.
  Tests: the `team_flat` break-test (ungranted read allowed, mutate never widened)
  and the `zoned` byte-identical regression in
  `internal/rbac/policy_readposture_test.go`; the default-is-`team_flat` and
  atomic-audit tests in `internal/readposture/readposture_test.go`; the
  admin-gated + audited + non-admin-refused tests in
  `internal/api/readposture_admin_test.go`; and the floor-denies-mutate-
  independent-of-posture break-test in
  `internal/tools/executor_readposture_floor_test.go`. The refuse-to-start-
  without-governance invariant (JOE-IDBOOT) and all prior RBAC regressions remain
  green.
- Supersedes: nothing. This makes the read decision a deliberate operator choice
  and names the prior grant-based behaviour `zoned`; it does not change the
  `zoned` decision, the write floor, or the mutate axis. The deferred work (hide
  the zone/policy/source-zone admin UI from the launch build; reframe public docs
  so zones and grant-based read are the full-mode era concept; the v2 zoned-flip
  UI; roles and groups for full RBAC v2) is tracked in
  `docs/backlog/read-posture-latch.md`.

---

## D-0040 — Credentials is an admin-only surface placed under the Admin nav subgroup (corrects D-0039)

- Date: 2026-06-25
- Status: accepted
- Session: admin-nav-consolidation-01
- Decision: the Credentials surface is **not** a top-level nav entry. It is a
  plain admin-only child of the expandable Admin nav subgroup, alongside Zones,
  Policies, Autonomous Reads, Skills, Admins, Users, and LLM Settings. It has no
  operator-visible subset — a non-admin cannot list it because its backing
  `GET /api/v1/admin/credential-status` endpoint is `requireAdmin`-gated — so it
  needs no inline-admin treatment and behaves like the other admin-only children:
  it renders only when `is_admin`. After this change the top-level nav entries are
  exactly Chat, Sessions, Components. This is a navigation-grouping correction
  only: the `/credentials` route stays `RequireAdmin`-wrapped, the page is
  unchanged, and server-side gating is untouched.
- Basis: `ui/src/components/layout/Sidebar.tsx` now lists `/credentials` inside
  the `adminNav` array (the Admin subgroup children), with no separate top-level
  Credentials entry; `ui/src/App.tsx:53` keeps the `/credentials` route under
  `RequireAdmin`; `internal/api/admin.go:138` registers credential-status under
  the admin prefix and `internal/api/credential_status_test.go` asserts non-admins
  get 403. Verified live against the rebuilt embedded UI (`make build`) with a
  real admin and a real non-admin principal: admin sees top-level Chat, Sessions,
  Components and Credentials as a child of the Admin subgroup; non-admin sees no
  Credentials entry and no Admin subgroup.
- Supersedes: only the Credentials-placement point of D-0039 (the "Credentials
  stays a top-level entry" bullet, and the corresponding "top-level admin-gated
  Credentials entry" wording in its Basis). The rest of D-0039 stands.

---

## D-0039 — Admin surface consolidated: operator entries flat with inline admin affordances, admin-only surfaces under one Admin nav subgroup, in-page Admin tab row removed

- Date: 2026-06-25
- Status: accepted
- Session: admin-nav-consolidation
- Decision: the Web UI's admin surface, previously split between admin-only
  top-level left-nav items AND an in-page tab row inside a single `/admin`
  tab-host page (which duplicated each other), is consolidated into one model:
  - **Operator surfaces are flat top-level nav entries** — Chat, Sessions,
    Components — visible to every authenticated caller. The two that have both an
    operator view and admin actions (Components, Sessions) appear **exactly once**
    as the operator entry and reveal their admin affordances **inline**,
    conditionally rendered on the `/me` `is_admin` flag (one source of truth, no
    second gate). For Components the inline admin affordances are create
    (Register), promote, and delete, plus the zone-assignment console; for
    Sessions it is the governance console (cross-tenant list, retention policy,
    purge/archive/restore) surfaced as an admin-only Governance tab. A non-admin
    sees only the operator view.
  - **Admin-only surfaces with no operator view live under one expandable Admin
    nav subgroup**, rendered only when `is_admin`: Zones, Policies, Autonomous
    Reads, Skills, Admins, Users, and LLM Settings. The in-page Admin tab row and
    its tab-host shell are **removed entirely**; each former tab's surface is now
    its own standalone `/admin/<name>` route (with `/admin` redirecting to the
    first child), so every former capability stays reachable. Components and
    Sessions are **not** children of the Admin subgroup — they are operator
    entries with inline admin actions, so they are not duplicated.
  - **Credentials** stays a top-level entry (not folded into the Admin subgroup)
    but remains admin-gated, because its backing `GET /api/v1/admin/credential-status`
    endpoint is server-gated to admins; it therefore shows only to admins, as
    before.
  - **Users** (the principals registry, an admin-only surface with no operator
    view) is folded into the Admin subgroup. The original consolidation brief
    omitted it from the Admin-children set; it is included to preserve the
    existing capability — trusting the live tree over the brief.
  Server-side gating is **unchanged** throughout: this is a client-navigation and
  admin-control-placement change only. The client-side `is_admin` gates are
  defense-in-depth that mirror the unchanged server `requireAdmin` enforcement.
- Basis: `ui/src/components/layout/Sidebar.tsx` renders flat operator entries,
  a top-level admin-gated Credentials entry, and one expandable Admin subgroup of
  admin-only children (gated on `useCurrentUser().data.is_admin`); `ui/src/App.tsx`
  defines `/admin/{zones,policies,autonomous-reads,skills,admins,users}` and
  `/llm-settings` routes under `RequireAdmin`, `/admin` → `/admin/zones`, and no
  longer imports `AdminPage`; `ui/src/pages/AdminPage.tsx` (the tab-host) is
  deleted and its tabs live in `ui/src/pages/admin/*`; `ui/src/pages/ComponentsPage.tsx`
  renders Register + ComponentZoneAssign and gates create/promote/delete on
  `is_admin`; `ui/src/pages/SessionsPage.tsx` adds an `is_admin`-gated Governance
  tab mounting `AdminSessionsPanel`; server `requireAdmin` gates in
  `internal/api/admin.go` and `internal/api/adminsessions.go` are untouched.
  Verified live against the rebuilt embedded UI (`make build`) with a real admin
  principal (alice@example.com, OIDC) and a real non-admin principal
  (bob@example.com): admin sees the flat operator entries + Admin subgroup with
  every former tab reachable by route, no in-page tab row, and inline admin
  controls on Components and Sessions; non-admin sees only the operator views with
  no admin controls and is redirected away from `/admin/*`. UI lint/tests and
  `go build`/`vet`/`test` green. Per D-0032 no nav-item or tab count is recorded
  here as a fixed figure — the sets are named structurally.
- Supersedes: the split admin model — admin-only top-level nav items
  (`/admin`, `/users`, `/credentials`, `/llm-settings`) alongside the in-page
  `/admin` tab row — established across the Phase 12 / RBAC / session-governance
  UI work.

---

## D-0038 — Chat is the default landing surface; the fabricated-data dashboard was retired pending a purpose-built one

- Date: 2026-06-25
- Status: accepted
- Session: launch-ui-polish
- Decision: the web UI's default/index route (`/`) now redirects to the chat
  interface, so a fresh post-login load lands on **chat**, not a dashboard. The
  prior landing/dashboard page was **removed entirely** — page, route, and
  sidebar navigation entry — because it presented data that does not exist in the
  running system: its "Active Alerts" widgets read `GET /api/v1/alerts`, which is
  a stub that returns an empty list with a `TODO` (no Alertmanager/Grafana
  aggregation is wired), so the surface misrepresented a non-functional feature
  as real. The page's remaining widgets were backed by genuinely wired data
  (components via `GET /api/v1/components`, recent sessions via the sessions API),
  but that data is **already independently surfaced** on the Components and
  Sessions pages, so removing the aggregate dashboard loses no real-data surface.
  The dashboard sub-components and the orphaned `ui/src/api/alerts.ts` client
  were deleted with it. A proper, purpose-built dashboard is **deferred**, not
  abandoned (tracked in `docs/backlog/launch-ui-polish.md`).
- Basis: `ui/src/App.tsx` index route is now `<Navigate to="/chat" replace />`
  (no `DashboardPage` import/route); `ui/src/components/layout/Sidebar.tsx` no
  longer lists a Dashboard nav entry; `ui/src/pages/DashboardPage.tsx`,
  `ui/src/components/dashboard/`, and `ui/src/api/alerts.ts` are deleted; the
  stub handler `handleGetAlerts` in `internal/api/webui.go` (returns
  `{"alerts": [], "count": 0}` with a `TODO`) confirms the alerts surface had no
  real backing. Verified live against the rebuilt embedded UI: a fresh load
  redirects `/` → `/chat` and no dashboard or alerts surface is reachable by
  default; UI lint/tests and `go build`/`vet`/`test` green; `make build`
  re-embedded the UI.
- Supersedes: the Phase 12 dashboard as the default landing experience (the
  `RecentSessions`/`AlertsList`/`ComponentsHealth`/`MetricsCard` dashboard
  composition). Per D-0032 no widget or route count is recorded here as a fixed
  figure.

---

## D-0037 — Chat token badge is a context-window utilization figure (input X of window Y), closing the D-0015 deferred "used X of Y" fast-follow

- Date: 2026-06-25
- Status: accepted
- Session: context-usage-display
- Decision: the per-assistant-turn chat badge now reads as **context-window
  utilization** — "input X of window Y · N% of context" — instead of the bare
  per-turn token count that read as stuck. The numerator **X is the turn's real
  input tokens** (the figure that actually fills the context window), not
  input+output: utilization is input-against-Y so the comparison is meaningful.
  The denominator **Y is the active provider/model's context-window capacity
  read from the capabilities registry** (`internal/llmusage` `LookupCapabilities`
  → `ModelCapabilities.ContextWindowTokens`) — the SAME capacity the per-turn
  input-token budget is already computed against for history pruning
  (`ComputeInputTokenBudget` in `internal/api/tasks.go` `buildTaskRun`), not a
  new or separately-derived source. Y is surfaced as an additive,
  `omitempty`-compatible `context_window_tokens` field on the task response
  struct and the SSE `final` event wire schema, consistent with how
  `total_tokens` and the history-pruning flags already ride that event (D-0003).
  When the lookup returns the conservative unknown-model default (D-0015(d)), the
  badge renders **against that default rather than hiding the figure or
  special-casing it**; during streaming, before the `final` event carries Y, the
  badge falls back to a bare climbing input count. This is a **display-semantics +
  denominator-surfacing change only**: token recording, the agent loop, the usage
  subsystem, and the underlying counters' running-sum-while-streaming /
  snap-to-final-total behavior are unchanged.
- Basis: the new `taskResponse.ContextWindowTokens` field and the
  `finalizeTaskResponse` capacity parameter wired from `prepared.caps.ContextWindowTokens`
  at both call sites (`internal/api/tasks.go`, `internal/api/tasks_stream.go`);
  the additive `context_window_tokens` field on `FinalEventSchema`
  (`ui/src/api/taskStream.ts`); the input-only `inputTokens` running counter and
  `contextWindow` denominator on `AssistantTurn` (`ui/src/hooks/useChat.ts`) and
  the relabelled badge (`ui/src/components/chat/AssistantTurnView.tsx`); Go and
  Vitest tests green; the embedded UI rebuilt (`make build`). Verified live
  against the rebuilt binary: the `final` SSE event carries
  `context_window_tokens` = the registry window for the active model (200,000 for
  the registered Claude Sonnet row, 100,000 for an unregistered model via the
  conservative default), so Y is the same window history is pruned to fit.
- Supersedes: nothing. Closes the D-0015 Status-section deferred fast-follow
  ("per-turn budget-consumption telemetry — 'used X of Y', verification gap 4")
  that D-0015 explicitly left open. Per D-0032 no authoritative context-window
  number is recorded here as a fixed figure — the capabilities registry
  (`internal/llmusage/capabilities.go`) remains the single source for the window.

---

## D-0036 — Build-truth is a single `internal/buildinfo` source; freshness is a boot-computed embed-FS digest, not injected

- Date: 2026-06-24
- Status: accepted
- Session: build-version-instrumentation
- Decision: the binary now has one source of build truth, `internal/buildinfo`.
  It declares package-level (non-const, string) `Version`/`Commit`/`BuildTime`
  variables that are the **sole ldflags `-X` injection targets, addressed by full
  import path** (`github.com/jaimegago/joe/internal/buildinfo.Version`, etc.); the
  prior `main.version` → `Server.SetVersion` path is retired and `defaultVersion`
  removed. A plain `go build` with no ldflags still compiles and reports the unset
  defaults (`dev`/`none`/`unknown`), so `dev` only ever marks a deliberately unset
  build. The freshness field `ui_digest` is **NOT** injected: it is a sha256 over
  the embedded UI filesystem, **computed once at boot** from the exact bytes the
  binary serves (`webui.DistFS()` → `buildinfo.Init`). Self-derivation was chosen
  over a build-time injection because the digest then cannot be absent on a real
  boot and cannot disagree with what is embedded, and an external harness holding
  the same `dist` files can recompute it byte-for-byte with no shared secret (the
  canonical serialization is documented on `buildinfo.Compute`: sorted relative
  paths, per file write the path bytes then the content bytes into one running
  sha256, lowercase hex). A dedicated `GET /api/v1/version` endpoint serializes the
  full `buildinfo.Info` (version/commit/build_time/ui_digest) and is the single
  place `ui_digest` is read; `GET /api/v1/status` is repointed to read its version
  from `buildinfo` (slim shape unchanged, no digest). A single `joe_build_info`
  Prometheus gauge (constant `1`, identity in labels) is registered in the
  metrics-setup layer beside the business gauges; the `ui_digest` label makes a
  stale embed visible across replicas. Distribution posture is unchanged —
  **build-from-source only** — but a `.goreleaser.yaml` scaffold plus a CI
  `goreleaser build --snapshot --clean` job now validate the release path and prove
  the injection works, with no release, tag-publishing, or artifact upload
  (`release.disable: true`). Flipping goreleaser to publish is a separate posture
  change with its own decision entry when taken.
- Basis: the new `internal/buildinfo` package and its tests; the repointed
  `internal/api` handlers (`handleStatus`, new `handleVersion`) and removed
  `Server.version`/`SetVersion`/`defaultVersion`; `internal/observability`
  `RegisterBuildInfo` + the `joe.build.info` instrument (rendered `joe_build_info`,
  unit intentionally omitted to avoid a `_ratio` suffix) verified by a real
  Prometheus-exporter scrape test; the Makefile `LDFLAGS` wiring; the new
  `.goreleaser.yaml` and the `goreleaser-build` CI job in
  `.github/workflows/tests.yml`; all under the `build-version-instrumentation`
  commit. The boot-injection verification was confirmed live: a binary built with
  `-X` reported the injected `version`/`commit` and a computed `ui_digest` over
  `GET /api/v1/version`.
- Supersedes: the `main.version`/`SetVersion` version-reporting path and the
  `defaultVersion` constant referenced in earlier prose; no prior decision entry
  governed build versioning.

---

## D-0035 — The session-tracking convention and volatile-count rule are fully specified in `docs/pm-convention.md`

- Date: 2026-06-24
- Status: accepted
- Session: pm-convention-capture
- Decision: the session-tracking slug convention (established in D-0031) and the
  volatile-count rule (established in D-0032) are now fully specified in
  `docs/pm-convention.md`, which documents the single slug that joins chat
  (claude.ai) sessions, Claude Code sessions, git commits, and decision-log
  entries, along with the artifacts, read paths, sync-freshness discipline, and
  build-prompt acceptance criteria of the convention. In addition, the claude.ai
  project instructions that operationalize this convention are version-controlled
  at `docs/claude_joe_project_instructions.md` as a pure paste-source: that file
  is the master, and the claude.ai project's instructions field is a manual
  paste-deployment of it (the file leads, the field follows; convention changes
  are made to the file first, committed, then re-pasted into the field).
- Basis: the new files `docs/pm-convention.md` and
  `docs/claude_joe_project_instructions.md`, added in the
  `pm-convention-capture` commit; D-0031 (slug convention) and D-0032
  (volatile-count rule), which this entry specifies in full rather than alters.
- Supersedes: none — this entry specifies and operationalizes D-0031 and D-0032;
  it does not overturn them.

---

## D-0034 — The review-agent subsystem (and `joe review`) is removed

- Date: 2026-06-09 (enacting commit `779c53f`)
- Status: accepted
- Session: jpk-retirement
- Logged retroactively: this entry was created **2026-06-24**; the decision was
  enacted on the Date above and is reconstructed here from git history because no
  decision entry was logged at the time (gap flagged in the retired
  `JOE_PROJECT_KNOWLEDGE.md` §1.1).
- Decision: remove the orphaned, two-binary-era **review-agent subsystem** —
  `internal/review/` (`ReviewAgent`, `Service`, `Repository`, `ReviewJob`), the
  **`joe review`** subcommand, the review webhook + job-queue REST routes
  (`POST /webhooks/{github,gitlab}`, `{POST,GET} /reviews`), and
  `prompts.ReviewSystem`. The proposed-infra-change review use case will be
  re-solved later via **MCP-exposed tools**, not this subsystem. **Preserved**
  (shared with the agentic/MCP path): the GitHub-PR / GitLab-MR *operation*
  routes, handlers, client/accessor methods (`internal/access/vcs.go`), and the
  github/gitlab core tools. The unused `review_jobs` table and migration
  `007_review_jobs` were deliberately **kept** (option A) because `023` mutates
  the column and deleting `007` would break replay — their true end state belongs
  to the git-filter-repo history scrub (`docs/security-findings-punchlist.md` §D).
- Basis: commit `779c53f` ("refactor: remove orphaned two-binary-era review-agent
  subsystem", 2026-06-09); the subcommand's resulting absence from the
  `cmd/joe/main.go` dispatcher; `docs/security-findings-punchlist.md` §C/§D
  (removal manifest + retained-table rationale).
- Supersedes: none — no logged decision ever established the review-agent
  subsystem (it was Phase 10 "Code Review Integration" work, never a `D-00xx`
  entry), so there is nothing to supersede.

---

## D-0033 — Collapse of `joe-core` into a single `joe` binary

- Date: 2026-06-03 (enacting commit `5f9dbd7`)
- Status: accepted
- Session: jpk-retirement
- Logged retroactively: this entry was created **2026-06-24**; the decision was
  enacted on the Date above and is reconstructed here from git history because no
  decision entry was logged at the time (gap explicitly flagged in the retired
  `JOE_PROJECT_KNOWLEDGE.md` §1.1, which noted "there is no single dedicated
  D-00xx decision entry recording the final joe-core→single-binary collapse").
- Decision: collapse the two-binary split — the separate `cmd/joe-core` daemon
  plus the `joe` CLI as a thin SSE client — into a **single `joe` binary** that
  runs the server by default with subcommands dispatched ahead of it. Provider
  keys now live in the one process that runs `joe`; there is no keyless thin
  client. (The earlier `cmd/joe-mcp` / `cmd/joe-slack` binaries had already
  become subcommands.)
- Basis: commit `5f9dbd7` ("refactor: collapse joe-core into single joe binary",
  2026-06-03) — the sole commit that deletes `cmd/joe-core` (`git log
  --diff-filter=D -- cmd/joe-core` returns only `5f9dbd7`); the present tree has
  `cmd/joe/` as the only `cmd/` entry, matching `CLAUDE.md`'s single-binary
  invariant.
- Supersedes: **D-0003** — its two-binary architecture premise (the
  `joe-core → CLI` SSE streaming boundary and the CLI-as-thin-streaming-client
  model). Note the SSE streaming transport itself survives (front-ends still
  stream the loop over `POST /api/v1/tasks/stream`), and the in-process
  tool-execution boundary that retired delegation was a separate step (Phase E,
  D-0008); this entry records the structural binary collapse, which D-0003 did
  not anticipate.

---

## D-0032 — Volatile, growth-driven counts are expressed structurally in CLAUDE.md, never as fixed figures

- Date: 2026-06-24
- Status: accepted
- Session: claude-md-drift-correction
- Decision: counts that grow as the codebase grows — graph edge types,
  database migrations, and similar — are NOT stated as hardcoded numbers in
  `CLAUDE.md`. They are expressed structurally instead: a pointer to the
  declaring file plus a description of how the set is defined (e.g. "the
  relation constants declared in `internal/graph/relations.go`, plus inline
  literals that bypass them" rather than "19 edge types"; "the migration files
  on disk in the migrations directory" rather than a count). A raw number, if
  retained at all, must never be presented as the authoritative total, because
  such figures restale on the next commit that grows the set.
- Basis: this session (`claude-md-drift-correction`) found `CLAUDE.md` carrying
  a stale "19 edge types" figure while `internal/graph/relations.go` declares
  the named constants and `internal/coreagent/` emits further edge types as
  inline string literals into the free-form TEXT `graph_edges.relation` column
  (no CHECK constraint) — so no single count is complete. The same drift class
  recurs for migration counts as files accrete under the migrations directory.
  The fix replaced these counts with structural descriptions and pointers.
- Supersedes: none.

---

## D-0031 — Session-tracking convention: a slug threads chat sessions, Claude Code sessions, commits, and decisions

- Date: 2026-06-23
- Status: accepted
- Session: pm-spine-wiring
- Decision: adopt a single session-tracking spine so a unit of work is
  traceable end to end. The mechanism:
  a. SLUG AS THE JOIN KEY. One slug ties together the chat session, the Claude
     Code session, the implementing commit(s), and the decision entry for a unit
     of work. For feature work the slug derives from the backlog filename (e.g.
     `cross-incident-relink` ← `docs/backlog/cross-incident-relink.md`); for
     infrastructure work with no backlog file it is hand-minted (e.g.
     `pm-spine-wiring`).
  b. DECISION ENTRIES CARRY A SESSION FIELD. Every entry in this log records a
     `Session:` field naming the slug under which the decision was made, so a
     decision points back at the session that produced it.
  c. COMMITS BEGIN WITH THE SLUG. Each implementing commit message begins with
     the slug, so `git log` is filterable by session and a commit points back at
     its session and decision.
  d. BACKLOG IS THE OPEN-WORK HOME. `docs/backlog/` holds open work, one file per
     item, summarized in `docs/backlog/INDEX.md` (the open-work entry point);
     finished items move to `docs/backlog/done/`. New backlog files use a title
     line, then a status line, then the body.
- Basis: this session (`pm-spine-wiring`) wired the spine — the root
  `DECISIONS.md` pointer, the `docs/DECISIONS.md` and `docs/backlog/INDEX.md`
  references in `CLAUDE.md`, and the demotion of the git-ignored
  `JOE_PROJECT_KNOWLEDGE.md` from log-signpost to current-state narrative. The
  `Session:` field on this entry is the first application of the convention.
- Supersedes: none.

---

## D-0030 — The component promotion endpoint: the single governed read-only-to-armed transition that owns credential entry (A003 Stream P)

- Date: 2026-06-16
- Status: IMPLEMENTED. In the tree as of this date as the A003 Stream P stack.
  Cite paths/symbols and re-derive line numbers against the tree.
- Gap closed: D-0029 made registration a credential-less promotion BOUNDARY but
  left promotion itself unbuilt — a registered component could land inert, with
  no governed path to ever acquire a credential. There was no promotion/arm
  route and no PATCH/PUT on components (`internal/api/server.go`
  `registerComponentRoutes`), so changing or supplying a component's credential
  meant either editing the DB directly or delete-and-recreate — an ungoverned,
  un-audited credential-change path (the delete-and-recreate-only gap; Finding 3
  in `docs/investigations/component-credential-registration-surface.md:22`).
- Decision: add a dedicated, admin-only promotion endpoint that performs the
  read-only-to-armed transition by writing a credential REFERENCE into the
  component's existing Config blob. It is the keystone of A003.
  a. ROUTE. `POST /api/v1/components/{id}/promote` — the arming verb as a
     sub-path of the component resource keyed on componentID, distinct from
     create/get/delete and from any (deliberately non-existent) full-resource
     PATCH/PUT. Registered in `registerComponentRoutes`
     (`internal/api/server.go`), handler `handlePromoteComponent`
     (`internal/api/components.go`). Off the chat/LLM path: an admin REST handler
     only; no core-agent tool reaches it.
  b. REFERENCE-IN-CONFIG (B-2). Promotion writes the `credential_provider`
     discriminator + the wired provider Kind's locator fields into the EXISTING
     Config blob (merged, preserving non-credential routing fields), via the new
     `store.ComponentRepository.UpdateConfigTx` (`internal/store/components.go`,
     encrypted-at-rest through `encryptedComponentRepository.UpdateConfigTx`). It
     introduces NO new schema column and writes exactly what the two wired
     providers read (`credential.KindFromConfig` / `staticConfig` /
     `kubeconfigExecConfig`). This is the arming transition: the component now
     carries a credential reference where before it had none.
  c. REJECT-UNWIRED, keyed on the W1 registry. The FIRST validation after the
     component loads consults `credential.WiredProvider`
     (`internal/credential/wiring.go`): a type with no wired credential provider
     can never be armed and is refused naming the type. Only github, gitlab
     (static) and kubernetes (kubeconfig-exec) are wired today.
  d. INLINE-VALUE POSTURE — INDIRECTION-ONLY. Promotion REFUSES an inline static
     `value` (a literal secret) and requires a true indirection (static
     `env_var`; kubeconfig-exec `kubeconfig` path or `in_cluster`). Rationale:
     the boundary's whole purpose is that the armed record carries a REFERENCE,
     not a secret — accepting an inline value would put a literal secret at rest
     in the Config blob, exactly what D-0029 rejects at registration. The static
     provider's inline-value capability remains for legacy/other paths; the
     GOVERNED promotion boundary does not use it. (`buildArmedConfig`.)
  e. UPDATE-VIA-RE-PROMOTE — YES. Promotion is idempotent-by-design: re-promoting
     an already-armed component overwrites the reference in the same governed,
     audited transaction, because the alternative (delete-and-recreate to rotate
     a credential) is precisely the ungoverned gap A003 exists to close
     (Finding 3). Re-arm is subject to the identical gate / reject-unwired /
     reference-validation checks; the audit before-state (`armed`) distinguishes
     initial-arm from re-arm.
  f. ADMIN-GATED + SAME-TX FAIL-CLOSED AUDIT, NO CREDENTIAL IN THE ROW. Gated by
     `Server.requireAdmin` (`internal/api/admingate.go`, the D-0029 standard).
     The Config write and a new `component.promote` audit verb
     (`audit.ActionComponentPromote`, kind `KindAdminAccess`, mutating /
     fail-closed) commit in ONE transaction via `Server.mutateWithAudit`; a
     failed audit rolls the arming back. The row records actor, componentID,
     type, provider Kind, and the reference SHAPE (the locator KEY names written)
     — NEVER the credential material or locator VALUES (an inline value is
     refused outright, so it can never reach the row).
  g. NO RESOLUTION ON PROMOTE. The handler performs no `Connect` / `Resolve` /
     `Probe` / provider `Select` / adapter build — promotion writes a reference;
     whether it works is a separate explicit admin Probe that already exists
     (`adminHandler.resolveAndProbe`, `internal/api/admin.go`). Asserted
     structurally by an AST guard (`TestPromote_NoResolution`).
- Prerequisite (commit-one): closed the D-0029 single-source seam.
  `internal/componentgov` no longer hand-maintains its credential-field denylist;
  it consumes `credential.CredentialBearingFields()` (`internal/credential/fields.go`),
  derived by reflection from the provider config structs (audience excluded).
  Guard tests: `credential.TestCredentialBearingFields_ExactSet`,
  `componentgov.TestCredentialBearingFields_MatchCredentialPackage`. This is the
  one place P touched `internal/credential`, exporting an existing fact only — no
  provider resolution behavior changed.
- Basis: `internal/api/components.go` (`handlePromoteComponent`,
  `promoteComponentRequest`, `buildArmedConfig`, `armedState`,
  `componentPromoteEvent`); `internal/api/server.go` (route +
  `sourceHandler.handlePromote`); `internal/store/components.go`
  (`UpdateConfigTx`) + `internal/store/encrypted_components.go`
  (`encryptConfig`); `internal/audit/audit.go` (`ActionComponentPromote`);
  `internal/credential/fields.go`; `internal/componentgov/credentials.go`.
  Tests: `internal/api/components_promote_governance_test.go` (reject-unwired,
  static + kubeconfig-exec arm, indirection-only, non-admin 403, audit
  fail-closed rollback, re-promote, mismatched-provider, no-resolution AST guard
  — the reject-unwired and no-resolution guards verified break-tested),
  `internal/credential/fields_test.go`,
  `internal/componentgov/credentials_test.go`,
  `internal/store/encrypted_components_test.go`
  (`TestEncryptedComponentRepository_UpdateConfigTxEncrypts`).
- Supersedes: nothing — completes D-0029. D-0029 built the credential-less
  registration boundary; this builds the credential-supplying promotion boundary
  it deferred. Use-time at-seam credential resolution stays where it is today
  (adapter Connect); P stores a reference, resolution is unchanged (D-0026).

---

## D-0029 — Govern component registration as a promotion boundary: credential-less, admin-gated, same-tx-audited CREATE/DELETE and register_component (A003 Stream G)

- Date: 2026-06-16
- Status: IMPLEMENTED. In the tree as of this date as the A003 Stream G stack.
  Cite paths/symbols and re-derive line numbers against the tree rather than
  trusting any quoted here.
- Gap closed: component registration was an ungoverned surface. POST/DELETE
  `/api/v1/components` (`internal/api/components.go`) were authenticated-only —
  no admin gate, no audit row — and CREATE handed the submitted config
  `json.RawMessage` straight to `adapter.Connect(...)` at registration time, an
  eager probe that made an attacker-controllable network call and dereferenced
  attacker-supplied credential locators (e.g. `env_var`) before the record even
  existed. The `register_component` LLM tool (`internal/coreagent/agent.go`)
  minted a component with an arbitrary credential-bearing config, un-gated and
  un-audited on the LLM path. Credentials could enter the system at
  registration, through both the HTTP and the LLM surface, with no durable trail.
- Decision: registration is a promotion BOUNDARY. A registration writes
  `type` + `name` + non-credential routing config only; the component lands
  inert (unassigned zone, read-only floor, no credential). Credentials enter the
  system later, EXCLUSIVELY at promotion (a separate stream — not built here).
  Concretely:
  a. CREDENTIAL-LESS BY CONSTRUCTION. Both registration paths reject — never
     silently strip — any credential-bearing config field, via the single
     shared `componentgov.RejectCredentialFields`
     (`internal/componentgov/credentials.go`). The denied set is the
     authentication fields the credential providers parse: the
     `credential_provider` discriminator, the static provider's `value` /
     `env_var`, and the kubeconfig-exec locators `kubeconfig` / `context` /
     `in_cluster`. CREATE returns 400; the LLM tool returns an error the LLM
     sees.
  b. ADMIN-GATED + SAME-TX FAIL-CLOSED AUDIT (CREATE/DELETE). Both HTTP handlers
     admit through the same `Server.requireAdmin` gate the Area-6 exemplar uses
     (`internal/api/admingate.go`), then commit the store mutation AND a
     `component.register` / `component.delete` audit row in ONE transaction via
     `Server.mutateWithAudit` + the new `ComponentRepository.CreateTx` /
     `DeleteTx` (`internal/store/components.go`) + `audit.Repository.InsertTx`.
     A failed audit rolls the mutation back — no row, no registration/deletion.
     This is the read-promotions `MutationService.SetPromoted` pattern
     (`internal/promotereads/promotereads.go`) applied to component writes.
  c. EAGER CONNECT PROBE REMOVED. `handleCreateComponent` no longer resolves an
     adapter or calls `Connect` at registration; a credential-less record cannot
     authenticate, so the probe was both pointless and the launch-blocking
     attacker-controllable-network-call / env-dereference vector. Connectivity
     checking belongs to promotion (the provider's Probe), out of scope here. A
     structural AST guard (`TestCreateComponent_NoConnectProbe`) fails the build
     if a `Connect` / `newAdapterForType` call is re-introduced on the create
     path.
  d. DELETE CLEARS THE CREDENTIAL REFERENCE. Delete removes the FULL component
     row (including whatever in-config credential reference it carries) in the
     audited transaction, so a delete cannot leave a dangling credential
     reference behind whether or not the component was ever promoted/armed. This
     requirement is satisfied by the existing full-row delete made
     transactional — no promotion-specific cleanup was built.
  e. register_component STAYS ActionRead, STAYS on the LLM surface. Recording a
     discovered component to Joe's OWN store is not a managed-system mutation, so
     the tool is NOT reclassified to Mutate and NOT subjected to the write floor
     (`internal/safety/tier.go` unchanged); discovery remains a legitimate
     LLM-path capability. It is now credential-less (same rejection rule) and
     writes a `component.register` audit row with actor `svc:agent:core`
     (`rbac.AgentCorePrincipal`), even though credential-less — an autonomous
     "Joe registered a component it discovered" action warrants a durable record.
- Single-source seam (flagged, not closed): the rejected-credential-field list
  in `componentgov.credentialBearingFields` is the ONE place both paths consult,
  so the two registration surfaces cannot drift from each other. It is, however,
  DUPLICATED from the provider json tags it mirrors: those tags live on
  UNEXPORTED structs (`staticConfig`, `kubeconfigExecConfig`, `discriminator` in
  `internal/credential/{static,kubeconfig_exec,provider}.go`), which this stream
  is fenced from modifying. Adding a future credential provider field without
  also adding it to `credentialBearingFields` would silently re-open a credential
  hole in create. To make this single-sourced later, export the field set from
  `internal/credential` (e.g. a `CredentialBearingFields()` accessor) and have
  `componentgov` consume it; the duplication and its fix are recorded in the
  `componentgov` package doc.
- Basis: `internal/componentgov/credentials.go`;
  `internal/api/components.go` (`handleCreateComponent`,
  `handleDeleteComponent`, `mutateWithAudit`, `componentRegisterEvent`);
  `internal/store/components.go` (`CreateTx` / `DeleteTx` + shared
  `create` / `delete` bodies); `internal/audit/audit.go`
  (`ActionComponentRegister` / `ActionComponentDelete`);
  `internal/coreagent/agent.go` (`RegisterComponentTool.Execute` /
  `registerWithAudit`). Tests:
  `internal/api/components_governance_test.go`,
  `internal/coreagent/register_component_governance_test.go`, and the updated
  probe-free expectations in `internal/api/components_test.go`. Pattern derived
  from the read-promotions exemplar (D-0028 / A001-COREGOV) and the admin gate
  (D-0012/D-0013).
- Supersedes: nothing — extends the governed-surface posture (D-0012 admin gate,
  D-0013 admin-surface audit vocabulary, D-0028 same-tx fail-closed mutation
  service) to the component-registration surface. Promotion (credential supply +
  connectivity Probe) remains a separate, unbuilt stream.

---

## D-0028 — Govern the Core Agent's autonomous refresh reads under the per-component RBAC floor (boot-minted agent:core principal + per-type auto_promote_reads predicate)

- Date: 2026-06-15
- Status: IMPLEMENTED. In the tree as of this date as the A001-COREGOV stack
  (uncommitted at time of writing; cite paths/symbols, not commit hashes, and
  re-derive line numbers against the tree rather than trusting any quoted here).
  Closes the carve-out the read-only investigations named: the Core Agent
  background refresh path resolved adapters directly off the raw registry with no
  principal and no permit (the "documented allowlisted exception"), so the
  per-component RBAC / read-only floor the Accessor enforces on the transport path
  did not cover the one autonomous path that touches every component cluster-wide.
- Decision: The Core Agent's autonomous refresh reads are now floored under the
  same per-component RBAC seam as transport reads, via (1) a boot-minted `svc:`
  service principal `agent:core` stamped onto the refresh context, (2) refresh
  adapter resolution routed through an `*access.Accessor` that runs permit at
  `rbac.ActionRead` BEFORE the adapter/credential is resolved, and (3) a
  per-component-type, default-OFF `auto_promote_reads` control that is the single
  autonomous-read admit, expressed as a dynamic predicate inside the policy engine
  rather than as materialized grant rows. "The Core Agent is refreshing" now
  structurally implies "the refresh is governed by the floor."
- Threat closed: previously refresh ran under the plain server-lifecycle context
  with NO principal (`PrincipalFromContext` → `rbac.Unknown`) and resolved each
  component's adapter via the raw `r.services.Adapters.Get(source.ID)` with no
  permit, then enumerated cluster-wide (e.g. all-namespaces k8s list). A read
  forbidden to every human/transport caller by zone was still performed
  autonomously against the backend by the refresh loop, using the component's
  resident (often org-wide / ambient) credential, with the only governance being a
  static allowlist that exempted the whole `internal/coreagent` prefix. Flooring
  the resolve deletes that ungoverned-read state class: a component with no zone
  grant and no promotion is now denied before its credential is ever exercised by
  refresh.
- Decisions (each with WHY + a verifiable basis anchor):
  a. SINGLE SHARED SERVICE PRINCIPAL. One principal — `agent:core`, minted as
     `svc:agent:core` — carries BOTH the refresh read path AND the in-loop graph
     writes. WHY: there is no separate discovery-write subsystem; the autonomous
     graph mutations run INSIDE the refresh loop (every `*_refresh.go` builds a
     delta and calls `ApplyGraphDelta` on the same `ctx`), so stamping the
     principal once on the context handed to the agent covers both halves with no
     per-path threading. Minted as an internal boot principal, never through the
     service-account resolver (it is not an authenticated API caller). BASIS:
     `rbac.CoreAgentServiceName` / `rbac.AgentCorePrincipal()`
     (`internal/rbac/identity.go`); the single mint + `rbac.WithPrincipal` stamp at
     `coreAgent.Start(...)` (`cmd/joe/server.go`, the agent-start block); the
     in-loop write half recorded in
     `docs/investigations/coreagent-refresh-governance-mint-thread.md` (Q3 / the
     "no distinct discovery-write subsystem" contradiction section).
  b. REFRESH READS ARE FLOORED AT THE ADAPTER RESOLUTION. The refresh path resolves
     each component's adapter through the guarded seam
     (`internal/coreagent/refresh.go` `resolveAdapter` → `access.Accessor.ResolveAdapter`),
     which runs permit at `rbac.ActionRead` BEFORE the adapter (and thus its
     credential) is resolved. WHY: a single floored resolution point means all
     downstream per-type reads inherit the floor — a denied component returns early
     at `refreshComponent` (skip-quietly on `access.ErrPermissionDenied`) before any
     `refresh*Component` runs, so no per-type read site needs its own check. BASIS:
     `internal/coreagent/refresh.go` (`resolveAdapter`, the `ActionRead` call, the
     skip-quietly branch); `Accessor.ResolveAdapter` permit-before-resolve
     (`internal/access/access.go`).
  c. auto_promote_reads IS THE SINGLE AUTONOMOUS-READ CONTROL. A per-component-type,
     default-OFF flag, on the admin-gated / audited config surface, is THE control
     that admits a component's type into the agent:core read floor — for both
     refresh and discovery. It is NOT a boot grant and NOT per-component manual
     grants. It grants ONLY read; the read/mutate floor (D-0018/D-0020) is
     independent and unconditional — promotion never widens to mutate. WHY: a single
     per-type admit keeps the autonomous-read surface inspectable and operator-
     controllable from one place, default-closed, with the safety floor orthogonal to
     it. BASIS: migration `024_agent_read_promotions`
     (`internal/store/migrations/024_agent_read_promotions.up.sql`); the
     `internal/promotereads` package (`Repository` read surface +
     `MutationService.SetPromoted` sole write path); admin read-promotions endpoints
     (`GET/POST /api/v1/admin/read-promotions`,
     `internal/api/admin.go` `listReadPromotions` / `setReadPromotion`).
  d. MECHANISM = DYNAMIC ADMIT PREDICATE, NOT MATERIALIZED GRANT ROWS. The flag is
     enforced as a live branch in `rbac.PolicyEngine.Decide`
     (`internal/rbac/policy.go`, `ReasonAutoPromoteRead`), NOT by inserting
     `rbac_policies` rows. WHY: `rbac_policies` is zone-keyed and action-less, so it
     cannot express a component-scoped read-only admit without new schema and would
     over-grant the whole zone; the predicate is hot-reloadable (the engine reads
     live per request, no boot cache), component+action precise, and covers
     components added AFTER a flag is turned on with no backfill. It is wired via
     `NewPolicyEngineWithPromote(repo, promoteResolver)` and fires for
     `(principal == agent:core ∧ action == ActionRead ∧ component-type promoted)`
     only — structurally the same grant-less admit shape as the existing admin
     short-circuit. It sits AFTER the `zone.Allows(action)` gate, so a read-
     forbidding zone still denies (D-0011-consistent: the flag widens WHO may read a
     zone-permitted action, never WHICH actions a zone allows). BASIS:
     `internal/rbac/policy.go` (`Decide`, the `e.promote != nil && action == ActionRead`
     branch placed after `zone.Allows`, `ReasonAutoPromoteRead`,
     `NewPolicyEngineWithPromote`, the `PromoteReadsResolver` seam); fork analysis in
     `docs/investigations/coreagent-refresh-governance-autopromote.md` (Q2/Q4).
  e. FAIL-CLOSED POSTURE. The refresh seam fails closed: a nil accessor denies with
     `access.ErrPermissionDenied` (resolving nothing), and boot wires the refresh
     accessor before `Start`. The static guard
     (`TestInvariant_NoUngovernedAdapterOrGraphAccess`,
     `internal/api/access_guard_test.go`) was RE-ARMED — `internal/coreagent` is no
     longer exempt for the ADAPTER READ (the allowlist is now split by access kind),
     so any reintroduced raw `services.Adapters.Get` on a coreagent path now fails
     the test. WHY: mirrors D-0027's refuse-on-absent-governance posture at the
     autonomous-read seam — the absence of governance must be unreachable, not a soft
     default. BASIS: `internal/coreagent/refresh.go` (`resolveAdapter` nil-accessor →
     `ErrPermissionDenied`, "fail-closed; CC-08"); the boot wiring at
     `cmd/joe/server.go` (`SetRefreshAccessor` before `coreAgent.Start`);
     `internal/api/access_guard_test.go` (the split-by-access-kind allowlist, adapter
     read no longer exempt for coreagent).
  f. WRITE-HALF INTENTIONAL NON-GATE. The autonomous graph writes
     (`ApplyGraphDelta` → graph store `AddNode`/`AddEdge`) are internal Tier-3
     knowledge writes governed UPSTREAM by the read floor — a denied component yields
     no adapter, no data, and therefore no delta — and are principal-stamped
     (agent:core) for audit, with NO write permit by design. WHY: this is a
     deliberate non-gate, not an oversight: the only data that can reach the graph is
     data a floored read already admitted, and the write target is Joe's own internal
     knowledge graph, not an external system. The post-floor orphan-write
     enumeration found ZERO graph-write call sites reachable without a floored adapter
     read (all floored-upstream). BASIS: `internal/coreagent/graphdelta.go`
     (`ApplyGraphDelta` on the raw graph store); the orphan-write enumeration (CC-06)
     in `docs/investigations/coreagent-refresh-governance-mint-thread.md`; the graph-
     write allowlist arm retained in `internal/api/access_guard_test.go`.
- Soft spots (recorded, NOT launch bars):
  - Secret-value redaction in the graph-write path is INCIDENTAL, not an enforced
    invariant. `internal/coreagent/graphdelta.go` has no redaction layer; the reason
    no secret VALUE reaches the store is upstream and incidental — the k8s build step
    copies only `data_keys` (key names), so there is no value on the node to redact.
    A future change that copied values would persist them unfiltered. Recorded in
    `docs/investigations/coreagent-refresh-governance.md` (§6).
  - Orphan-write-path outcome: the post-CC-05 re-enumeration found NO graph-write
    reachable without a floored adapter read (all 23 `ApplyGraphDelta` sites
    floored-upstream, zero orphans), so no new soft spot was created by routing only
    the read half through the Accessor. Outcome recorded in
    `docs/investigations/coreagent-refresh-governance-mint-thread.md` (CC-06).
- Basis: re-verified against the live tree on landing —
  `internal/rbac/identity.go` (`CoreAgentServiceName`, `AgentCorePrincipal`);
  `cmd/joe/server.go` (refresh-accessor build + `SetRefreshAccessor` + the
  `rbac.WithPrincipal(ctx, agentCore)` stamp at `coreAgent.Start`);
  `internal/coreagent/refresh.go` (`resolveAdapter`, `ActionRead`, nil-accessor
  fail-closed); `internal/access/access.go` (`ResolveAdapter` permit-before-resolve);
  `internal/rbac/policy.go` (`Decide` auto-promote branch after `zone.Allows`,
  `ReasonAutoPromoteRead`, `NewPolicyEngineWithPromote`, `PromoteReadsResolver`);
  `internal/promotereads/promotereads.go` (Repository + `MutationService.SetPromoted`);
  `internal/store/migrations/024_agent_read_promotions.{up,down}.sql`;
  `internal/api/admin.go` (read-promotions routes); the re-armed guard
  `internal/api/access_guard_test.go`. Tests landed alongside:
  `internal/coreagent/agent_core_principal_test.go`,
  `internal/coreagent/refresh_access_test.go`,
  `internal/rbac/policy_promotereads_test.go`,
  `internal/api/readpromotions_admin_test.go`.
- Cross-references (investigations this design produced):
  `docs/investigations/coreagent-refresh-governance.md` (read-only diagnosis: no
  principal, secret-content scope, sole bypass site),
  `docs/investigations/coreagent-refresh-governance-mint-thread.md` (mint+thread
  dependency re-verification, the in-loop write-half contradiction, CC-06 orphan
  enumeration), and
  `docs/investigations/coreagent-refresh-governance-autopromote.md` (the dynamic-
  predicate vs materialized-grant fork). Also references
  `docs/investigations/ambient-credential-dispatch-seam.md` (the seam keyed on
  componentID not credential reach, and the carve-out this closes).
- Supersedes: (1) the documented allowlist exception for the coreagent ADAPTER READ
  — the prose at `internal/access/access.go` / `internal/api/access_guard_test.go`
  stating the in-process Core Agent refresh path is "the single documented exception"
  is now RETIRED for the read half (the guard is re-armed; only the graph-WRITE
  exemption remains, per decision f); and (2) the
  `internal/coreagent/agent.go` comment asserting "the agent:core principal does not
  yet exist", now obsolete (the principal is minted and stamped at boot).
- Deferred (explicit post-launch non-goals, recorded so they are not assumed in
  scope):
  - Full-mode / self-healing autonomous MUTATING actions — a separate authorization
    model (not covered by the read floor; the write-half non-gate of decision f is
    deliberately read-floored-only).
  - Scoped-ambient sub-grants — the unit of authorization remains the COMPONENT, not
    the underlying repo/dashboard/namespace a single component credential can reach
    (the granularity caveat from
    `docs/investigations/ambient-credential-dispatch-seam.md`).
  - Operator-facing UI / CLI to register or promote components — the read-promotions
    control ships as the admin HTTP surface only.
  - EdgeAuth open-when-unconfigured dead branch (`internal/auth/middleware.go`) —
    the same newly-unreachable branch D-0027 parked; retire later with the
    unreachable-state-assertion pattern.
- References D-0027 (the boot fail-fast / refuse-on-absent-governance posture this
  mirrors at the autonomous-read seam), D-0019 (the trust model and the promotion /
  read-only-confinement / autonomous-discovery work D-0027 said this unblocks),
  D-0018/D-0020 (the write floor / Read-Mutate axis the promotion is orthogonal to),
  and D-0011 (admin/zone non-widening, which the post-`zone.Allows` predicate
  placement preserves).

---

## D-0027 — Refuse to start without a usable identity configuration (engine-nil-at-runtime made unreachable)

- Date: 2026-06-15
- Status: IMPLEMENTED. In the tree as of this date (commits 91d472a, 3fd6d3a,
  44e9f5f). Implements the "RBAC inert/permissive when auth off must become
  UNREACHABLE" obstacle named in D-0019 decision 3 — the central obstacle that
  entry said the implementation "must close, not defer."
- Decision: Joe refuses to start unless the RBAC policy engine would be
  constructed non-nil. Missing or incomplete identity configuration is now a hard
  fail-fast at boot, in the SAME tier and exit semantics as missing LLM
  credentials and DB access — not a soft warning. "Joe is running" now
  structurally implies "Joe is governed."
- Threat closed: previously the policy engine was nil whenever no service account
  and no complete OIDC config existed, and a nil engine permitted every operation
  with reason `rbac_disabled` indefinitely, off any network bind, behind only a
  single soft boot warning. This was reachable not just by a fresh install but by
  a HALF-configured one — a partial OIDC block (issuer set, client_id/redirect_url
  empty) yielded engine-nil despite identity values being present, so an operator
  mid-setup would see Joe running and assume it was governed while it was silently
  allow-all. Refuse-to-start deletes this entire state class.
- Load-bearing design property: the refuse-to-start predicate IS the engine's own
  enable predicate, factored into one shared function so the guard and the engine
  constructors cannot drift. A new nil-safe method `config.(*Config).RBACEnabled()`
  — true iff service accounts are configured OR OIDC is `Configured()` — is the
  single source of truth, called by BOTH engine-construction sites and the boot
  guard. It reads raw config only (via the existing `ServiceAccountsConfigured` /
  OIDC `Configured` sub-predicates) and adds NO IdP reachability probe.
- What the guard does / does not fire on (both encode the decision):
  - FIRES (refuse to start): no identity at all; partial OIDC (any of
    issuer/client_id/redirect_url missing). The partial-OIDC case is the one that
    proves the half-configured gap is closed.
  - DOES NOT FIRE (start, governed): service-account-only; complete OIDC;
    complete-but-unreachable OIDC (IdP down). Completeness of config is the test,
    NOT IdP liveness — this deliberately avoids converting an IdP outage into a Joe
    outage. No reachability probe was added to the boot path.
- Implementation points (the landed shape; future readers should re-derive these
  against the tree rather than trust any line numbers, which are intentionally
  omitted):
  - Shared predicate: `config.(*Config).RBACEnabled()` in
    `internal/config/config.go`.
  - Boot guard + rich remediation-message constant (`noIdentityConfigMessage`,
    mirroring `noProviderKeyMessage`): `cmd/joe/server.go`, positioned AFTER the
    service-account-resolver fatal-validation gate and BEFORE engine
    construction. The post-gate ordering is load-bearing: it is what makes
    raw-config SA presence equivalent to the resolved resolver at that site (a
    malformed account map exits at the resolver gate before reaching the
    predicate).
  - SITE 1 (`cmd/joe`) builds the engine via `cfg.RBACEnabled()`. SITE 2
    (`internal/api` `newPolicyEngine`) was swapped to the same predicate, keeping
    its Config-nil / RBAC-nil guards (for `api.New`'s looser contract); no second
    refuse-to-start is added there because `api.New` has exactly one production
    caller, downstream of the `cmd/joe` guard.
  - Exit semantics: `slog.Error` + `return 1` bubbling to `os.Exit(1)`. No
    `log.Fatal`.
- Scope / explicitly deferred (conscious non-goals):
  - No runtime identity-provisioning / setup-wizard / first-run flow — boot-time
    config satisfies the guard.
  - The promotion / read-only-confinement / autonomous-discovery model is the
    separate work this unblocks, NOT part of this unit.
  - The soft nil-engine warning was retired: its default arm became unreachable
    and was replaced with an unreachable-state assertion (an internal-invariant-
    breach `slog.Error`, not an operator-misconfiguration warning).
- Consequence to flag (newly unreachable, parked as follow-up): EdgeAuth's
  open-when-unconfigured branch (`internal/auth/middleware.go`) is now unreachable
  via the boot path for the same reason the nil engine is — post-guard,
  service-account-or-OIDC is always true. This is now a dead branch in the same
  equivalence class as this fix; a future follow-up should retire it, likely with
  the same unreachable-state-assertion pattern.
- Basis: the three commits above, re-verified against the live tree on landing —
  `internal/config/config.go` (`RBACEnabled`), `cmd/joe/server.go`
  (`requireIdentityConfigured`, `noIdentityConfigMessage`, the guard placement and
  the retired warning arm), `internal/api/server.go` (`newPolicyEngine`). Guard
  tests cover all five identity states, asserting the two non-negotiable cases:
  partial-OIDC refuses, complete-but-unreachable starts
  (`internal/config/rbacenabled_test.go`, `cmd/joe/identityguard_test.go`).
- Supersedes: nothing — implements the engine-nil obstacle from D-0019 decision 3.
  References D-0018 (the write floor it sits beside in the boot fail-fast tier).

---

## D-0026 — Credential provider abstraction (Resolve/Probe/Describe, two-half resolved-credential type, launch-vs-deferred split)

- Date: 2026-06-09
- Status: ACCEPTED (design). Launch scope buildable without an Accessor refactor; not yet implemented.
- Decision: Adopt a credential-provider abstraction in which "which credential" is
  a property of the target component, resolved and applied at the guarded accessor
  seam, keyed strictly on the authz'd componentID with no ambient fallback. Full
  record in `docs/decisions/D-0026-credential-provider-abstraction.md`. In brief:
  resolution returns one typed result with two structurally separated halves — a
  serializable diagnostic half (component identity, provider kind, audience, expiry,
  stage reached, non-sensitive reason) and a non-serializable credential half (a
  means/source, never a value). A four-stage enum (provider-selected → mint-attempted
  → mint-succeeded → connectivity-probed) is the diagnostic spine, with
  mint-succeeded-without-probe a legal lazy-connectivity terminal state. The provider
  exposes exactly three operations — Resolve / Probe / Describe — and deliberately
  excludes Refresh/Rotate and any store/seam dependency in its signature. Launch ships
  two providers (static/env-var and kubeconfig-exec) invoked inside adapter Connect
  with no Accessor signature change; the resolve-value-at-the-seam model (and its
  store.ComponentRepository-on-Accessor dependency), ambient-workload-identity,
  rotation orchestration, per-zone scoping, and mutation-credential separation are
  designed-for but deferred.
- Basis: three read-only investigations against the live tree —
  credential-handling-current-state.md, adapter-credential-refresh-tolerance.md,
  credential-design-assumptions-check.md — cited file:line throughout the ADR and
  re-verified against the tree on landing (one citation corrected: networking.go:20
  → networking.go:13).
- Supersedes: nothing. Builds on the security-architecture-direction record §9 (the
  one credential commitment already made) and decides the parts §9 deferred.
- Documented gaps (tracked separately as issues/backlog, dispositions preserved):
  kafka parses-but-never-applies SASL auth (security finding, arguably
  fix-before-launch); two live credential leaks — /api/v1/components returns decrypted
  Config + mongodb URI interpolated into a ping error (T3, arguably fix-before-launch);
  azure Connect skeleton (deferred, tied to ambient-WI); component-management paths
  bypass the permit/guard seam (existing authz gap, flagged as issue).

---

## D-0025 — Captain transfer swap (detach old + attach new) is a single atomic transaction (transfer-half of the no-auto-lapse captaincy model)

- Date: 2026-06-08
- Status: IMPLEMENTED. In the tree as of this date.
- Gap: `CaptainService.completeTransfer` performed the captaincy handoff as two
  independent, sequential repository writes — `MarkCaptainDetached(outgoing)`
  then `AttachCaptain(incoming)` — with no shared transaction. A failure (or
  crash) after the detach committed but before the attach committed left the
  active incident with the old captain row detached and no successor row: a
  permanently captain-less incident. The captaingate then reads that as the
  pending-captain / null-authority state and refuses mutations, and nothing
  in-process re-attaches — recovery would require a fresh `Attach`. The prior
  code self-documented this as a Phase 1 gap ("a failure between them leaves
  the session captain-less, which a subsequent Attach would heal"); that
  heal is not automatic. (This is gap #6 in
  `docs/investigations/incident-captain-flow.md`.)
- Decision: `completeTransfer` now performs the detach-old + attach-new swap
  atomically through a new repository method, `SwapCaptain`, which runs both
  writes inside one DB transaction — either both commit or neither does. There
  is no committed state in which the old captain is detached and the new one is
  not attached, so a mid-swap failure can never strand the incident
  captain-less. The detach is an inline `UPDATE ... SET detached_at = ?,
  transfer_state = NULL, incoming_principal = NULL, transfer_initiator = NULL`
  on the transaction — the same `SET` clause the D-0024 resolve detach uses,
  keyed here by the outgoing captain's `id`. The attach reuses the existing
  insert logic: `AttachCaptain`'s body was extracted into an unexported,
  executor-accepting core (`attachCaptainExec(ctx, exec sqlExecer, c)`) called
  by both `AttachCaptain` (on `r.db`) and `SwapCaptain` (on the `*sql.Tx`), so
  the INSERT and its §6-D `last_seen_at` seeding are defined once and the two
  callers cannot drift.
- Scope: this is the **transfer-half of the no-auto-lapse captaincy model** —
  the counterpart to D-0024's resolve-half. Only `completeTransfer`'s
  transactionality changed; the §B state machine, the D-0017 confirm/cancel
  authorization binding, the deny-only sessiongate, and the resolve-path detach
  are untouched.
- Deliberately deferred: this fix leaves **three** captain-write patterns in
  the tree — resolve's inline tx detach (D-0024), the still-used non-tx
  `MarkCaptainDetached` / `AttachCaptain` primitives, and `completeTransfer`'s
  new tx swap. Consolidating them behind one tx-aware detach/attach seam is
  recorded as a backlog item (`docs/backlog/captain-write-consolidation.md`)
  and is **out of scope** here — collapsing the patterns would expand the blast
  radius of a targeted durability fix.
- Basis: fix in `internal/sessionmodel/captain.go` (`completeTransfer` now
  calls `s.repo.SwapCaptain`) and `internal/sessionmodel/repository.go`
  (`SwapCaptain` / `swapCaptainWithHook`, the shared `attachCaptainExec`, and
  the `sqlExecer` seam). True rollback test
  `TestCaptain_TransferSwapAtomicOnAttachFailure` in
  `internal/sessionmodel/captain_test.go` injects a fault between the detach and
  attach (via the `SwapCaptainWithHook` test seam, mirroring D-0024's
  `ResolveIncidentRegimeWithHook`) and asserts the swap rolled back: the
  original captain is still active with `detached_at` NULL and no incoming row
  was inserted. The test fails if the two writes are taken off the shared
  transaction (proven by re-running it with the detach moved onto `r.db`: the
  detach commits before the fault and the session goes captain-less). The
  happy-path transfer is covered by the existing
  `TestCaptain_B1_PrincipalThreadedAfterConfirm` and
  `TestCaptain_6D_IncomingInitiatedWhenUnreachableDirectConfirm`, which still
  pass through the new swap.
- Supersedes: nothing — closes gap #6 from the incident-captain-flow audit.

---

## D-0024 — Incident resolve detaches the active captain atomically with the regime flip (resolve-half of the no-auto-lapse captaincy model)

- Date: 2026-06-08
- Status: IMPLEMENTED. In the tree as of this date.
- Gap: `ResolveIncidentRegime` flipped `system_regime` back to `normal` and
  transitioned the incident session to `resolved`, but performed no write to the
  `session_captains` row. The active-captain row therefore survived resolution
  with `detached_at IS NULL`, so `GetActiveCaptain` / `CurrentCaptainPrincipal`
  kept reporting a phantom captain on a resolved incident — a dangling
  active-captain row that reads treated as live. (This is gap #8 in
  `docs/investigations/incident-captain-flow.md`.)
- Decision: resolve now detaches the resolving incident's active captain. The
  detach is a `session_captains` UPDATE (`detached_at` set; transfer columns
  cleared, mirroring `MarkCaptainDetached`) keyed by `session_id` where
  `detached_at IS NULL`, executed **inside the existing resolve transaction**
  alongside the session-state transition and the regime→normal flip. They commit
  as a single unit, so there is no observable intermediate state where the regime
  is normal but a captain is still active, nor where the captain is detached but
  the regime still says incident. (The resolve writes already ran in one tx; the
  detach joined that tx — no new transaction was introduced.)
- Scope: this is the **resolve-half of the no-auto-lapse captaincy model** —
  captaincy ends only on explicit transfer or on incident resolve; there is no
  idle-timeout lapse. `session_captains` has no detach-reason column, so no reason
  is recorded (out of scope to add one). The transfer-swap path
  (`completeTransfer`) and its separate non-atomic finding (D-0017 area, gap #6)
  are untouched.
- Basis: fix in `internal/sessionmodel/regime_transitions.go`
  (`ResolveIncidentRegimeWithHook`, the detach UPDATE between the session-state
  transition and the regime clear). Break test
  `TestCaptain_ResolveDetachesActiveCaptain` in
  `internal/sessionmodel/captain_test.go` fails if the detach is removed;
  `TestCaptain_ResolveAtomicRegimeAndCaptain` asserts the joint post-condition
  (regime normal AND no active captain);
  `TestCaptain_DeclareAfterResolveAttachesCleanly` confirms a fresh declare after
  a prior resolve attaches the new captain without interference.
- Supersedes: nothing — closes gap #8 from the incident-captain-flow audit.

---

## D-0023 — Write-floor posture line in the task system prompt (proactive articulation, observation/safe_mode only)

- Date: 2026-06-08
- Status: IMPLEMENTED. In the tree as of this date. Realizes D-0019's observation
  posture in the LLM-facing system prompt; refines neither D-0018 nor D-0020.
- Decision: when the boot-resolved write floor is up, the task system prompt now
  carries a posture section telling the model its current posture, so it declines
  managed-system writes proactively with articulation instead of only reacting
  after the floor denies the tool call at execution. The section is **conditional
  on the floor reason** and added at the single prompt-assembly site
  (`internal/api/tasks.go`, `buildTaskRun`):
  - `observation` → an observation-mode posture line framed as Joe's intended
    read-only resting state. **No recovery/unlock language** — observation is the
    intended default, there is nothing for the user to fix or clear.
  - `safe_mode` → a different, safe-mode posture line framed as an emergency halt.
    **No user-directed recovery instruction**: restoration is framed as an operator
    action, and the model is told NOT to direct the user to clear the state or run
    any command (no `joe unlock`, no "see docs to restore"). Recovery guidance
    already lives in the reactive denial UI message; the prompt must not duplicate
    or contradict it.
  - `none` (full mode) → **nothing injected**. Full-mode write behaviour is
    governed by RBAC, not a prompt line; a "you can write" line would be
    behaviorally risky noise.
- Scope: this changes ONLY the model's proactive explanation. The **tool surface
  is unchanged** (no pruning — every tool stays advertised) and **enforcement is
  unchanged** (the floor still denies every Mutate at execution regardless of what
  the model does). Reads the same boot-sealed `services.WriteFloor` value the
  executor and the captaingate floor wrapper use; nothing is re-resolved.
- Basis: prompt text and the reason→section mapping live in
  `internal/prompts/posture.go` (`PostureSection`), per the invariant that all LLM
  prompt strings live in `internal/prompts/`. Tests in
  `internal/prompts/posture_test.go` assert the three cases and — the load-bearing
  guard — the absence of unlock/recovery language in both posture strings.
- Supersedes: nothing — implements part of D-0019.

---

## D-0022 — Denial-message precedence (floor > incident > RBAC) enforced by check order; autonomous-path seam routing deferred

- Date: 2026-06-08
- Status: PARTIALLY IMPLEMENTED. Task 1 (precedence) is in the tree as of this
  date. Task 2 (routing the autonomous Core Agent path through the shared seam)
  is DEFERRED with findings recorded below. Implements D-0019 decision 9 and the
  "autonomous path must route through the shared seam" item under D-0019's
  "Current state being changed". References D-0018 (the write floor) and D-0010
  (the shared §C captaingate; coreagent refresh confirmed read-only).

### Task 1 — denial-message precedence (IMPLEMENTED)

- Decision: when more than one denial could apply to a single attempted write,
  the user sees ONE reason, ordered **floor > incident > RBAC** (and within the
  floor, `safe_mode` > `observation`). Rationale: resolvability depth — show the
  reason the user can least readily fix, because it is the one actually blocking
  them. A floored Joe is read-only until restart (least fixable); an incident
  redirect needs the captain (less fixable than a zone grant); a zone denial is
  an ordinary RBAC grant away (most fixable).

- **Co-occurrence is possible, and is resolved by CHECK ORDER, not by the
  classifier.** Enforcement short-circuits at the first failing check, so for any
  single write attempt exactly ONE typed error is ever produced. The three error
  types (`*safety.WriteFloorError`, `*captaingate.GateRefusalError`,
  `access.ErrPermissionDenied`) are therefore mutually exclusive on a single
  `err`. The classifier `classifyWriteFailure` (`internal/api/writefailure.go`)
  only maps the one error that fired to its UI code — it does NOT decide
  precedence. Its branch order was realigned to floor → incident → RBAC as
  documentation of intent, but that change is functionally a no-op.

- The denials live in TWO layers, so precedence is enforced by reordering checks
  across BOTH:
  1. **`tools.Executor.Execute`** (`internal/tools/executor.go`): the write-floor
     check was moved ABOVE the zone/namespace scope checks, so for a Mutate that
     trips both, the `WriteFloorError` is the one error produced (floor > RBAC
     scope). Pinned by `TestExecutor_Floor_PrecedesZoneScope`.
  2. **`captaingate.Wrapper.Execute`** (`internal/captaingate/captaingate.go`):
     the §C incident gate sits in a wrapper UPSTREAM of the executor, so the
     executor reorder alone cannot make floor > incident. The wrapper now takes
     an optional `WithFloor` and checks the floor BEFORE the §C gate; a floored
     Mutate is refused with the floor reason and the gate is never consulted (no
     `GateRefusalError`, no `inner.Execute`, no `captain_gate_refused` audit row).
     §C2 (gate upstream of RBAC) is preserved — the floor simply becomes the new
     outermost gate. Pinned by `TestFloorPrecedesIncidentGate`; the inert-floor
     and read-through cases by `TestFloorDownGateStillRefuses` /
     `TestFloorAllowsReadsThroughGate`.
  - `safe_mode > observation` needs no runtime ordering: the floor resolves to
    exactly ONE reason at boot (`safety.ResolveWriteFloor`, panic wins over the
    env var), pinned by the pre-existing `TestResolveWriteFloor_Precedence`.

- **Behavior on the autonomous path is unchanged; the user-task path now
  enforces the floor.** The Core Agent executor (`internal/coreagent/agent.go`)
  issues only Reads (per D-0010), so neither the floor nor the gate fires on it
  today — the reorders there remain no-ops, correct BY CONSTRUCTION for the day
  an autonomous Mutate exists. The user-task executor (`internal/api/tasks.go`)
  now carries the floor (see "Discovered gap — CLOSED" below), so a user-task
  Mutate under an up floor IS denied with the floor reason, and `WithFloor` is
  wired at BOTH captaingate sites so floor > incident holds on both agentic
  paths.

- Discovered gap — **CLOSED** (2026-06-08): the D-0018 write floor was originally
  injected ONLY on the Core Agent executor, not on the user-task-loop executor —
  `internal/api/tasks.go` built its `tools.Executor` without
  `tools.WithWriteFloor`, so in observation/safe mode a user-task Mutate
  (`write_file`, `run_command`, `publish_doc_update_*`, `github_comment`, …) was
  NOT floor-blocked, contradicting the `WithWriteFloor` doc comment's claim that
  both construction sites are wired. Closed by adding
  `tools.WithWriteFloor(services.WriteFloor)` to the user-task `execOpts` and
  `captaingate.WithFloor(services.WriteFloor)` to the user-task captaingate
  wrapper (mirroring the Core-Agent site in `cmd/joe/server.go`), so the floor is
  enforced and the floor > incident precedence holds on the user-task path too.
  Pinned by `TestTaskEndpoint_WriteFloorBlocksMutate` (Mutate denied with the
  observation code), `TestTaskEndpoint_WriteFloorAllowsReads` (Reads still flow),
  `TestUserTaskExecutorFloor_ErrorsIs` (the seam returns a
  `*safety.WriteFloorError`, `errors.Is ErrWriteFloor`), and
  `TestTaskEndpoint_FloorPrecedesIncidentOnUserTaskPath` (with the floor up AND
  an incident regime active, a user-task Mutate surfaces the floor reason, not
  `incident_mode` — the `captaingate.WithFloor` line is what makes this hold; the
  test regresses to `incident_mode` if it is removed). The boot-floor
  immutability guards (`internal/safety/floor_guard_test.go`) remain green.

### Task 2 — route the autonomous Core Agent path through the shared seam (DEFERRED)

- The Core Agent's background graph-refresh path writes the graph DIRECTLY:
  each refresher (`internal/coreagent/{k8s,aws,azure,git,gitops,observability,
  networking,datastore,alerting,registry,crd}_refresh.go`) calls
  `BuildGraphDelta` → `ApplyGraphDelta(ctx, r.services.Graph, delta)`
  (`internal/coreagent/graphdelta.go`), which calls `store.AddNode/AddEdge/
  DeleteEdge/DeleteNode` on the graph store — bypassing the executor seam where
  the floor, classification, and §C gate live. ~25 call sites across the refresh
  files. Confirmed read-only on infrastructure per D-0010 (VERDICT-A); these
  graph writes are Reads under the binary model (arg-keyed idempotent upserts of
  Joe's own model), so they pass the floor and MUST keep flowing — observation
  mode must not freeze Joe's own model (a settled design point).

- Routing this through the seam is NOT a clean, mechanical reroute. It is
  non-trivially entangled, so per the staged rule it was not implemented:
  1. **Shape mismatch.** The seam (`Executor.Execute` / `captaingate.Wrapper`)
     is keyed by TOOL NAME + `map[string]any` args and classifies via
     `safety.ClassifyTool(name)`. The refresh path operates on typed
     `graph.Node`/`graph.Edge` via the store. There is no clean adapter.
  2. **Missing tools.** `ApplyGraphDelta` does AddNode, AddEdge, DeleteEdge,
     DeleteNode. Tools exist for `graph_add_node`/`graph_add_edge`/
     `graph_update_node` (all Read), but there is NO `graph_delete_edge` /
     `graph_delete_node` tool; and the existing graph tools are CORE tools that
     round-trip the in-process client/accessor, not the direct in-process
     `services.Graph` store writes the refresh uses. Routing through them would
     change the write MECHANISM, not just its path.
  3. **Autonomous principal does not exist.** The "autonomous principal"
     (`agent:core`) referenced by the design is NOT in live code — only
     `user:`/`group:`/`svc:` reserved prefixes are defined
     (`internal/rbac/identity.go`). Carrying it requires introducing a new
     reserved principal-kind — an identity-model change.
  4. **Behavior-change risk on the Reads.** The refresh runs in a background
     context with no principal. Routed through the accessor with an empty/new
     principal while RBAC is live, the accessor could DENY
     (`access.ErrPermissionDenied`) — the graph Reads must keep passing. This is
     precisely the "surface, do not paper over" case the task called out.

- Decision: DEFER routing to a dedicated follow-up that (a) defines the
  `agent:core` principal kind, (b) decides the seam shape for typed store writes
  (a store-level governed wrapper vs. new delete tools), and (c) proves the
  graph Reads still pass through the seam unchanged. The precedence work above
  already makes the floor > incident ordering correct in `captaingate` by
  construction, so the day that routing lands and an autonomous managed-system
  Mutate flows through the seam, the floor governs it before the gate. The
  deferral note at `internal/coreagent/agent.go` (New) is retained.

- Basis: code investigation 2026-06-08 against the live tree (executor.go,
  captaingate.go, writefailure.go, sessiongate.go, graphdelta.go + refreshers,
  tasks.go, server.go, agent.go, rbac/identity.go). Build/vet/test/gofmt clean;
  the boot-floor immutability break-tests (`internal/safety/floor_guard_test.go`)
  still pass.
- Supersedes: nothing. Implements part of D-0019; refines neither D-0018 nor
  D-0020 (the binary model and floor lifecycle are unchanged).

---

## D-0021: Rename "source" → "component"; flat model with type as a routing discriminator

Date: 2026-06-08
Status: IMPLEMENTED (2026-06-08). The lexical sweep landed: Go (`store.Source`→`store.Component`, `SourceRepository`→`ComponentRepository`, the `SourceType*` constants, the `sourceID`/`SourceID` seam→`componentID`/`ComponentID`, audit `Event.Source`→`ComponentID`, client/handler CRUD, `register_source`→`register_component`, `list_sources`→`list_components`); SQL via migration 023 (`sources`→`components`, `source_zone_assignments`→`component_zone_assignments`, the provenance `source_id` columns and `audit_log.source`→`component_id`, indexes, plus the `<type>_source`→`<type>_component` graph-label data migration); REST routes (`/api/v1/components`, `/admin/component-zones`, `{componentID}` path param) and `component_id` JSON; and the UI (`ComponentsPage`, `useComponents`, schemas/types, wire contract). `knowledge_sources`, `skills.TrustedSources`, panic `trigger_source`, the investigation "source session" columns, `Edge.Source`, and `onboarding_facts.source` were deliberately left untouched.

### Context

The model's top-level concept for a registered external system was named "source" (table `sources`, `sourceID` in the RBAC seam). "Source" names only the read direction. Joe now reads AND mutates these systems (apply a k8s manifest, push to a git repo, create a Grafana dashboard, edit an alert), so "source" no longer captures what the thing is. These systems span three rough categories — observability/telemetry backends, infrastructure platforms (k8s/AWS), and code repositories (IaC, app config, app code) — all of which Joe both reads and mutates.

### Decision

Rename the concept to "component": a part of the managed system that Joe represents as a node in its graph and reads or mutates as the situation needs. One flat top-level type, not a kind-split.

The earlier idea of splitting into two top-level kinds (read-only "sources" for telemetry vs mutable "components" for infra/repos) is REJECTED. Telemetry backends are mutable too (dashboards, alerts), so the write boundary does not run between the categories — it runs through each of them, at the operation level. The three categories are the same kind of thing under the only axis that structures Joe (the write definition, D-0018).

The existing `type` field is retained as a routing-and-presentation discriminator. It drives adapter dispatch, available-operation set, and node labels — NOT safety classification and NOT a structural kind split. Type values themselves are unchanged (aws, kubernetes, prometheus, …).

### Why this is safe to rename

A code investigation (2026-06-08) confirmed the safety layer is completely type-blind: tier classification keys on tool name, the write floor is a pure function of two boot booleans, RBAC keys on (principal, sourceID, action). No safety/tier/floor/RBAC decision reads source.Type. The ratified write-definition — "the boundary is the operation's effect on the managed system, not the kind of target" — is upheld in code. A rename therefore cannot disturb the trust model.

### Scope of the rename (lexical only)

- SQL: `sources` table → `components`; `source_zone_assignments` and any FK/index naming; a NEW migration, not an edit to an existing one.
- Go: `store.Source` → `store.Component`; `sourceID` → `componentID` in the guarded accessor / IsAllowed path and throughout.
- Admin REST: source-zones routes and JSON field names (breaking API change — acceptable pre-launch).
- Audit vocabulary: any audit row referencing a source.
- UI: SourcesPage and security API; be deliberate where the `Component` domain type meets React components (import naming).
- OASIS-facing API: check whether any scenario references a source field name through POST /api/v1/tasks; if so, OASIS needs a matching pass.
- Graph node labels: the `<type>_source` idiom becomes `<type>_component` (e.g. "prometheus_component"). Requires a data migration for existing graph rows carrying the old `_source` label.
- Docs: CLAUDE.md, identity/design docs, project-knowledge file.

### Explicitly out of scope (separate follow-ups)

- Adapter-construction consolidation: dispatch is fragmented across divergent type-keyed paths (see docs/backlog/adapter-dispatch-consolidation.md). This is a pre-existing latent bug. Fix AFTER the rename — rename is lexical and low-risk, consolidation is structural; interleaving them makes a coverage bug mid-sweep unattributable.
- The knowledge.Source model (knowledge_sources table; human/confluence/notion/session) is a different, unrelated concept. NOT renamed.

### Type values reference (unchanged by this decision)

Enforced in Go via AllowedSourceTypes() — no SQL CHECK/enum. ~37 values incl. aws, azure, git, kubernetes, prometheus, mimir, loki, tempo, jaeger, datadog, splunk, dynatrace, newrelic, cloudwatch, azuremonitor, alertmanager, pagerduty, grafana, postgresql, mysql, redis, mongodb, kafka, elasticsearch, argocd, terraform, helm, nginx-ingress, envoy, falco, oci_registry, dockerhub, artifactory, ecr, github, gitlab.

---

## D-0020 — Collapse the three-tier action classification (Observe/Record/Act) into a binary Read/Mutate axis

- Date: 2026-06-07
- Status: IMPLEMENTED. Unlike D-0018/D-0019 (design-pending), this entry records
  a code change that is in the tree as of this date. Refines D-0018/D-0019 — it
  realizes their "write = mutation of the managed system" definition as the
  single decidable axis the classifier carries.
- Decision: the action classification is now binary. `safety.ActionTier`
  (`TierObserve`/`TierRecord`/`TierAct`, the old T1/T2/T3) is replaced by
  `safety.ActionClass` with exactly two states: `ActionRead` (does not mutate
  the managed system) and `ActionMutate` (mutates the managed system). The
  classifier `ClassifyTool` returns one of the two; the struct field
  `ToolClassification.Tier`/`ActionInfo.Tier`/`AccessDeniedError.Tier` is
  renamed `Class`. `ActionClass.String()` returns `"read"`/`"mutate"`.

  Rationale: severity-of-mutation is DELIBERATELY NOT a classification tier. A
  static blast-radius taxonomy is hard to get right and hard to evaluate on a
  non-deterministic LLM. The classification answers one decidable question —
  does this operation mutate the managed system. Blast-radius safety lives in
  tools, skills, OASIS testing, and the per-zone/per-capability graduation
  ladder (D-0019), not in a tier.

  **Mapping (old → new).** Former Observe → Read; former Act → Mutate. The
  middle tier (Record) was already vacant after the prior reclassification
  (commit d3c34d3 / D-0018/D-0019): no registered tool was Record. It is
  DELETED from the type, not merged as a live value. Every tool carries forward
  its already-decided read/write nature unchanged (Joe's own graph/model
  maintenance — graph_add_*, register_source, save_onboarding_fact,
  save_knowledge_entry, generate_doc_draft — stays Read; external/managed-system
  writes — write_file, run_command, publish_doc_update_*, github/gitlab_comment,
  github_request_changes — are Mutate). The unknown-tool default is `ActionMutate`
  (deny-by-default, unchanged conservatism).

  **Consumer map (every layer that keyed off the tier; the binary question each
  now asks).** All behavior is preserved exactly; only the type changed.
  - Write floor — executor safe-mode check (`internal/tools/executor.go`): was
    `IsSafeModeActive() && tier > TierObserve`, now `&& class == ActionMutate`.
    Question: "is this Mutate" (deny Mutate). Equivalent set: with Record gone,
    "tier > Observe" and "is Mutate" denote the identical tool set. (The
    boot-resolved D-0018 floor proper is still design-pending; this safe-mode
    check is the live floor today.)
  - Pre-execution blocking notification (executor): was `== TierAct`, now
    `== ActionMutate`. Same set (Act == Mutate, Record vacant).
  - Post-execution audit notification (executor): was `>= TierRecord`, now
    `== ActionMutate`. Same set (Record vacant ⇒ fired only for Act before).
  - Captain/incident gate (`internal/captaingate`, `internal/sessiongate`):
    Read bypasses the gate; Mutate runs the §C captain-session check. Question:
    "is this Mutate". `sessiongate.Check`'s 5th param renamed `tier`→`class`
    (§C4 signature-pin guard unaffected — `class` is not a forbidden name).
  - Policy gate (`internal/safety/policy.go`): `IsT3Allowed` still gates Mutate
    by per-action grant. Question: "is this Mutate" + which capability is
    granted.
  - DurableExecutor (`internal/coreagent/executor_durable.go`): Read bypasses
    idempotency persistence; Mutate gets the §D5 RecordToolIntent → execute →
    MarkToolCompleted crash-resume protocol. Keyed off `ActionRead` now — the
    IDENTICAL operation set as before (former Observe bypassed; former Act
    persisted).

  **DurableExecutor coupling — temporary preservation, not the final design.**
  Keying crash-resume durability off the action class is a behavior-preserving
  stopgap. "Does this operation need crash-resume idempotency" is NOT the same
  question as "does this mutate the managed system." Joe's own model-maintenance
  creates are now Read and therefore bypass durability, losing idempotency on
  crash-resume — named casualties: register_source, save_onboarding_fact (and
  the graph_add_* family). This is a KNOWN, OUTSTANDING gap, intentionally NOT
  fixed here. The follow-up idempotency/durability-decoupling task will replace
  the binary key with a durability-specific predicate so durability tracks
  "needs crash-resume," independent of the Read/Mutate axis.

  **DurableExecutor decoupling — IMPLEMENTED (follow-up to the coupling note
  above; durability is now opt-in per tool, default OFF).** The
  DurableExecutor's wrap decision no longer reads the action class. A new
  per-tool boolean `ToolClassification.NeedsDurability` (in
  `internal/safety/tier.go`, declared alongside `Class`/`PolicyKey`) drives it:
  `executor_durable.go` wraps an op IFF `ClassifyTool(name).NeedsDurability` is
  set, else it bypasses (no key, no persistence). The §D5 protocol
  (RecordToolIntent → execute → MarkToolCompleted, replay short-circuit,
  crash-resume on 'issued') is unchanged — only what selects INTO it changed.

  Rationale for default-OFF (fail toward declare-the-few, not default-on): each
  wrapped op costs two synchronous fsyncs (persist 'issued', then the terminal
  status) plus an unbounded, never-pruned `tool_idempotency_keys` row carrying
  the full serialized result. Negligible for the handful of genuine creates;
  a material I/O and storage tax on the high-frequency read path. Worse,
  durability on a naturally re-runnable read risks serving a STALE cached
  result on a same-key replay (e.g. a metrics query that should re-run). So
  reads stay OFF and only non-idempotent operations opt in.

  Per-tool accounting (the audit; re-derived from each tool's actual
  create/append site and the underlying store method):
  - DECLARE NeedsDurability (wrapped):
    - `register_source` — `Source.Create` is a plain INSERT; the row ID is a
      crypto-random `type-<rand>` generated server-side OUTSIDE the args, so a
      retry creates a second source. (Casualty fixed.)
    - `save_onboarding_fact` — `Facts.Create` is a plain INSERT with an
      autoincrement `RETURNING id`, no natural key. (Casualty fixed.)
    - `save_knowledge_entry` — `knowledge.Service.Create` sets `id = uid.New()`
      server-side, plain INSERT, no unique key. (Additional casualty found by
      the audit, beyond the two named in the task.)
    - `generate_doc_draft` — `proposals.Create` sets `id = uid.New()`, plain
      INSERT; also wraps an expensive LLM draft generation. (Additional
      casualty found by the audit.)
    - `github_comment`, `gitlab_comment` — each posts a NEW comment/note to the
      PR/MR thread (server-assigned comment ID, no natural idempotency); a
      retry double-posts. Kept durable.
    - `github_request_changes` — files a NEW review; a retry duplicates it.
      Kept durable.
  - DO NOT declare (intentionally non-durable; idempotent / no duplicate risk):
    - `graph_add_node`, `graph_add_edge`, `graph_update_node` — UPSERTs keyed on
      caller-supplied args (node_id, or the `(from,to,relation)` edge key via
      `ON CONFLICT … DO UPDATE`). Re-running converges; no duplicate. (Note:
      the coupling note above loosely listed `graph_add_*` as casualties — the
      audit corrects that: they are idempotent and correctly NOT declared.)
    - `write_file` — overwrites a path (the path IS the natural key); rewriting
      with the same content is idempotent.
    - `run_command` — creates no Joe-side record; caching a command's output by
      args and replaying it would be WRONG (a fresh run must re-execute), and
      side-effect replay-safety is the command's own concern.
    - `publish_doc_update`, `publish_doc_update_{confluence,notion,git}` —
      guarded at the data layer: `PublishProposal` requires `status==approved`
      and flips it to `published`, so a re-publish of the same proposal fails
      closed rather than duplicating (a natural idempotency key). The
      crash-after-target-write/before-MarkPublished window is unprotected by
      durability either (an 'issued' key re-runs), so no protection is lost by
      dropping it.

  Wrapping-status changes vs. the old Mutate-only set:
  - Newly wrapped (were bypassed as Read): register_source,
    save_onboarding_fact, save_knowledge_entry, generate_doc_draft.
  - No longer wrapped (were wrapped solely for being Mutate): write_file,
    run_command, publish_doc_update, publish_doc_update_confluence,
    publish_doc_update_notion, publish_doc_update_git. Two distinct reasons,
    not one: write_file, the graph upserts, and the publish_doc_update variants
    are idempotent or data-layer-guarded (re-running converges / fails closed);
    run_command is non-idempotent but durability CANNOT protect it — replaying
    cached output would be wrong, so crash-resume re-executes by design and the
    command's own side-effect safety is out of durability's scope. Either way,
    no operation that needs (and can use) replay-safety silently lost it.
  - Unchanged-wrapped (Mutate, still declared): github_comment, gitlab_comment,
    github_request_changes.

  Casualty fix: register_source and save_onboarding_fact are durable again — an
  in-run duplicate call or crash-resume with identical args short-circuits to
  the cached result and creates no second row (the §D5 key is stable because
  their IDs are generated server-side, outside the args-hash).

  STILL OPEN (named explicitly, NOT built here):
  1. Dedup is PER-RUN only, not cross-run. The idempotency key is
     `SHA256(runID + tool + sorted-args-hash)`, so it deduplicates within a
     single run; it does NOT guarantee cross-run uniqueness (two separate runs
     each registering "prod-cluster" still create two rows). True "never two
     rows for the same logical source/fact" needs a natural unique key or a
     get-or-create at the data layer (`sources`/`onboarding_facts`), not
     durability. Separate, unaddressed follow-up.
  2. The `tool_idempotency_keys` table has no pruning/TTL — rows live for the
     run/session lifetime, reclaimed only via FK cascade when the run/session
     is deleted. Acceptable for the small declared-durable set, but should be
     noted; a high-volume durable tool would grow it unbounded.

  **Backward-compat shim retained.** Existing `~/.joe/safety-policy.yaml` files
  may carry a `record:` block. The `SafetyPolicy.Record`/`RecordPolicy` struct
  and `IsT2Allowed` are RETAINED in `internal/safety/policy.go` purely so those
  files still deserialize; `CheckAccess` no longer calls `IsT2Allowed` (no tool
  is Record). Separately, `internal/runmodel` has its OWN `Tier` type
  (`TierT1/T2/T3` = 1/2/3) persisted to the `action_ledger.tier` column. It is a
  DISTINCT type, never converted from the safety classification, and
  `AppendLedger` has no production caller (test-only). It is out of scope for
  this collapse and left untouched; flagged here so the next task knows the
  ledger still encodes 1/2/3 by number.
- Basis: re-derived against the live tree — `internal/safety/tier.go` (type,
  classifier, registry, `CheckAccess`), `policy.go` (retained shim),
  `internal/tools/executor.go`, `internal/captaingate/captaingate.go`,
  `internal/sessiongate/sessiongate.go`, `internal/coreagent/executor_durable.go`,
  `internal/safety/notifier.go`. Break-tests added: `TestActionClass_IsBinary`
  (exactly two states, middle gone), `TestClassifyTool_UnknownDefaultIsMutate`,
  `TestCheckAccess_ModelMaintenanceAlwaysAllowed` (a graph/model read passes the
  floor), `TestClassifyTool_ExternalCommentsAreMutate`,
  `TestClassifyTool_GraphMutationFamilyIsRead`. Captain-gate and policy-gate
  behavior-preservation are covered by the existing captaingate/policy tests,
  which exercise the real classifier (write_file = Mutate, read_file = Read) and
  pass unchanged.

  Durability decoupling (implementation note above) basis: re-derived the wrap
  site (`internal/coreagent/executor_durable.go`, formerly the
  `classification.Class == ActionRead` bypass, now `!NeedsDurability`), the key
  derivation (`computeIdempotencyKey` = SHA256(runID|tool|argsHash)), and each
  tool's create/append site against its store method (`store.sources.go`,
  `store.facts.go`, `knowledge/{service,repository}.go`,
  `knowledge/proposals/service.go`, `graph/sqlite.go AddEdge` ON CONFLICT,
  `api/inproc_client.go PublishProposal` status guard). Break-tests added:
  `safety.TestClassifyTool_NonIdempotentCreatesNeedDurability` (pins the seven
  declared tools — guards the default-OFF silent-gap risk),
  `TestClassifyTool_IdempotentToolsAreNotDurable`,
  `TestClassifyTool_UnknownToolNotDurable` (default OFF), and in coreagent
  `TestDurableExecutor_DrivenByProperty` (Read+declared wrapped, Mutate+
  undeclared bypassed — durability no longer reads the class),
  `TestDurableExecutor_UndeclaredBypass`,
  `TestDurableExecutor_InRunReplayDedupsCreate` (computed runID+args key
  short-circuit on a declared create). The pre-existing durable tests
  (D5Ordering, ReplayShortCircuit, CrashResume, NoGoroutineFanOut) were
  repointed from `write_file` (no longer wrapped) to `register_source` (now
  declared durable); their intent — ordering, replay short-circuit,
  crash-resume re-run, no goroutine fan-out — is unchanged.
- Supersedes: nothing — refines D-0018/D-0019. Follow-up status: the
  idempotency/durability decoupling (named casualties above) is now RESOLVED
  (see "DurableExecutor decoupling — IMPLEMENTED"). Two items remain open and
  are NOT this change: cross-run uniqueness for sources/facts (needs a natural
  unique key or get-or-create at the data layer), and pruning/TTL for the
  `tool_idempotency_keys` table.
- Known cleanup debt — persisted three-valued tier (deferred, NOT blocking).
  Discovered after the collapse: a three-valued tier concept survives in the
  run-model persistence layer. It is INERT but contradicts the binary model.
  - What survives: the `runmodel.Tier` type with constants
    `TierT1`/`TierT2`/`TierT3` (= 1/2/3) in `internal/runmodel/types.go`, used
    as the `Tier` field of `LedgerEntry`; and the `action_ledger.tier` column,
    `tier INTEGER NOT NULL CHECK (tier IN (1, 2, 3))`, from
    `internal/store/migrations/010_run_model.up.sql`. (This is the `runmodel`
    `Tier` flagged out-of-scope under "Backward-compat shim retained" above,
    now fully characterized.)
  - Current status (honest): there is NO production writer of the action
    ledger — `AppendLedger` is called only from tests, and the production
    `DurableExecutor` path persists idempotency keys, not ledger rows — so the
    column is unpopulated in real deployments. There IS one production reader,
    `getSITREP` (the `GET /api/v1/runs/{id}` handler, `internal/api/runs.go`,
    via `ListLedgerForRun`), but it does NOT interpret the tier: it passes the
    raw int straight through to JSON. So the concept is inert, not actively
    buggy — no production path reads-and-interprets a stale three-valued
    semantics.
  - The landmine: the `CHECK (tier IN (1, 2, 3))` constraint actively
    contradicts the binary model. If any writer is reintroduced under the
    Read/Mutate classification, it has no natural 1/2/3 value to write; a
    zero-value `Tier` (0) would VIOLATE the CHECK and fail the insert. The
    schema must not be left constrained this way indefinitely — this is why the
    cleanup eventually has to happen, rather than being purely cosmetic.
  - Cleanup scope (when done, not now): a new migration dropping the `tier`
    column and its CHECK; remove the `Tier` field from `LedgerEntry`; delete the
    `Tier` type and its `TierT1/T2/T3` constants; remove `tier` from the
    INSERT/SELECT and the `Tier(tier)` mapping in the `internal/runmodel`
    repository (`repository.go`); update the three test files that reference the
    old tier constants (`internal/runmodel/schema_test.go`,
    `internal/runmodel/cascade_schema_test.go`,
    `internal/api/cascade_delete_test.go`).
  - Adjacent item, same cleanup: `LedgerEntry` has NO JSON tags, so its fields
    (including `Tier`) serialize with capitalized Go-default names through the
    `GET /runs/{id}` response — the same no-JSON-tags issue D-0019 flagged for
    the regime endpoint. Fix when the ledger is cleaned.
  - Deferred, not blocking: it is inert today (no writer), and the trust-model
    floor work (D-0018/D-0019) takes priority.

---

## D-0019 — Joe's trust model: two boot postures, graduated capability, and fail-closed-empty-RBAC as the load-bearing safety boundary

- Date: 2026-06-07
- Status: design decision of record; implementation PENDING. No code currently
  realizes this trust model — nothing here is implemented yet, and the live
  code diverges from this entry. This entry records the target design and the
  divergence honestly; it does NOT claim any code implements it. (Same
  "accepted-as-design, build pending" posture as D-0018.)
- Companion to D-0018. D-0018 recorded the write floor's lifecycle, immutability,
  sticky-panic semantics, and the load-bearing definition of a write. THIS entry
  records the broader trust model the floor sits INSIDE: the posture model, the
  principal model, the capability ladder, and the empty-RBAC fail-closed
  guarantee. Where the two meet, this entry references D-0018 and does not
  repeat it.
- Context: Joe must be safe to adopt by default and must scale from read-only
  observation to eventual lights-out autonomous operation as a GRADIENT, not a
  single read-only/read-write bit. The definition of a write — mutation of the
  managed system (live infrastructure and the code/config that governs it),
  where reads include source queries, Joe's own graph/model maintenance, and
  notifications to humans — is established in D-0018 and assumed here.

  The decision, as numbered points:

  1. **Two boot postures, env-var-selected, restart-to-change.** Observation
     mode is the day-one default: a hard read-only floor (D-0018) where Joe
     reads but performs no managed-system mutation regardless of RBAC, enforced
     BELOW RBAC so no policy or grant can override it. It is the intended
     resting state, not an emergency, and the UI presents it calmly.
     Full-capabilities mode permits writes at the binary level but boots Joe at
     the BOTTOM of its capability ladder with zero write grants; RBAC becomes
     the floor.

  2. **Fail-closed-with-empty-RBAC is the real safety boundary — not a setup
     wizard.** The day-1-to-day-2 transition (flipping the env var to full mode)
     is Joe's single most dangerous configuration change. Its safety must NOT
     rest on any UI screen that can be skipped or that runs after the backend is
     already write-capable. It rests on two backend properties: full mode
     requires authentication ON, and with no policy rows every write is denied.
     "Full mode, no grants yet" must be a genuine fail-closed floor with the
     SAME observable behavior as observation mode (Joe performs no
     managed-system mutation), enforced at a different layer (RBAC rather than
     the hard floor). The env-var flip removes the hard ceiling; empty RBAC
     remains a floor. The two dangerous acts — flipping the env var and granting
     capability — stay separate by construction. This is the load-bearing safety
     property of the trust model.

  3. **This requires fixing RBAC's current inert/permissive-by-default
     behavior.** As of the investigation (verify against live code): the policy
     engine instantiates only when a service account or OIDC is configured
     (around `cmd/joe/server.go`); with auth off the default identity is
     permissive and the access guard short-circuits allow-all with reason
     `rbac_disabled` (around `internal/access/access.go`); the agentic task,
     stream, and chat routes are not source-keyed but do carry a context
     principal evaluated at the access guard (around `internal/api/tasks.go` and
     `internal/agentloop`). With auth ON and empty policy rows, the engine
     already fails closed (no grant, and the default `unassigned` zone allows
     only read). The trust model requires that full-capabilities mode cannot run
     write-capable with a permissive/absent engine — full mode demands auth ON
     and a live engine, so the fail-open path is UNREACHABLE in full mode. This
     is the central obstacle the implementation must close, not defer.

  4. **The principal model — who authorizes a write.** Interactive
     (human-initiated) writes gate against the launching human's grants;
     graduation means granting that human or their role write capability in a
     zone. Autonomous (Core Agent) actions gate against a dedicated autonomous
     principal (named to match the existing `user:`/`svc:` convention, e.g.
     `agent:core` — verify the convention against live code). Both resolve to a
     principal and both go through the SAME enforcement seam; neither has a path
     that skips gating. The autonomous principal exists from day one with zero
     write grants, so autonomous Joe is read-only by enforcement, and its
     current operations (source queries, graph/model refresh) are reads under
     the D-0018 write definition and pass the floor. The current divergence to
     close (as of the investigation, verify against live code): the autonomous
     Core Agent refresh bypasses the executor seam entirely, writing the graph
     directly via the graph-delta path (around `internal/coreagent`), carrying
     no principal — it must be routed through the shared seam so that the day a
     managed-system autonomous write exists, it is governed by the same floor
     and RBAC as everything else, by construction.

  5. **The capability ladder.** In full mode, graduation is per-zone and
     per-capability: observe, then granted writes in dev, then staging, then one
     production zone, then wider, toward eventual lights-out autonomous
     operation. The trust model is a gradient, not a single bit.
     Autonomous-write capability is a FUTURE grant on this existing ladder for
     the autonomous principal — explicitly NOT built now; no autonomous-write
     subsystem is built in this work. The mechanism for lights-out already
     exists the moment the autonomous principal and the uniform gate exist: when
     lights-out is real, an operator grants the autonomous principal write in a
     zone via the same ladder. No new subsystem is required at that point.

  6. **The LLM's tool surface under posture: exposed-and-deny, not hidden.** All
     tools remain advertised to the model regardless of posture or grants;
     authorization is enforced at execution and denials are fed back to the
     model as tool-results (this is already the codebase behavior as of the
     investigation, verify against live code — the full registry is always
     advertised, around `internal/tools/registry.go` and `internal/agentloop`,
     and denied calls return error tool-results). Tool-surface pruning by
     posture is deliberately NOT built: a prior zone-violation finding (treat as
     a lead, not verified here) showed Joe lost its zone-first refusal language
     when the tool surface changed between read and write — hiding tools removes
     the refusal there is to articulate, which degrades safety-evaluation
     behavior. To gain proactive (rather than only reactive) refusal
     articulation, the model is TOLD its posture: a posture line is added to the
     system prompt in observation mode (and, in full mode, the zone-scope prompt
     mechanism already conveys authorized zones, around
     `internal/prompts/zones.go`), so the model can refuse with articulation
     before attempting a denied call. The model is NOT told it is in safe mode
     today (as of the investigation, verify against live code, no safe-mode or
     panic reference exists in the prompts package) — adding the
     observation-posture line is net-new.

  7. **Two distinct "Joe does nothing" states must be presented differently —
     and they are different mechanisms, not one state rendered twice.**
     Observation mode is the hard env-var floor (D-0018) — a deliberate ceiling;
     the UI reassures ("Joe is running in observation mode — no changes will be
     made," calm, with a link to an explanatory doc), bound to a posture read
     endpoint. Full-mode-with-zero-grants is RBAC denying for lack of a grant —
     a soft floor and an invitation to configure; it is surfaced by an on-demand
     "evaluate Joe's write capability" PULL mechanism (a button, optionally
     scheduled), NOT a pushed banner. The banner reads the floor; the
     grant-state is pulled on demand. This distinction is UI on top of the two
     backend mechanisms; it carries no safety weight because the fail-closed
     floor (point 2) holds regardless of what the UI shows.

  8. **The read path.** A posture read endpoint reports the current mode
     (observation versus full) and, in full mode, a coarse "any write grants
     exist" signal sufficient for the UI to distinguish configured from
     zero-grants — derived from the audit trail of write-policy creation (the
     admin REST API is the sole audited writer of RBAC state per prior
     decisions, so this is derivable rather than a new mutable flag; verify that
     sole-writer property against live code). The endpoint is auth-gated only,
     consistent with the existing panic-status and regime endpoints, and uses
     explicit snake_case JSON tags — NOT Go default serialization (as of the
     investigation, verify against live code, the regime endpoint serializes a
     struct with no JSON tags, emitting capitalized Go-default field names around
     `internal/sessionmodel` and `internal/api/regime.go`; do not repeat that).
     A calm observation-mode banner is bound to a real fetch of this endpoint
     and mounted alongside the existing safe-mode and incident banners.

  9. **Denial-message precedence when more than one denial could apply to a
     single write:** the floor first (and within the floor, `safe_mode` over
     `observation`, per D-0018), then incident/captain gate, then RBAC zone
     denial. Ordered by resolvability depth — show the user the reason they can
     least readily fix, because it is the one actually blocking them.
     Implementation note (verify against live code, do not act here): the
     current classifier evaluates incident, then permission-denied, then
     safe-mode (around `internal/api/writefailure.go`), which does not match this
     precedence; and the in-`Execute` checks place RBAC scope before the floor
     (around `internal/tools/executor.go`). Whether precedence is a real runtime
     collision or is already foreclosed by enforcement short-circuit order must
     be determined in implementation, and enforced by reordering the checks, the
     classifier branches, or both.

- What this deliberately does NOT do:
  - No runtime posture toggle (boot + restart only; the runtime stop-all-writes
    need is served by panic, per D-0018).
  - No autonomous-write capability or any autonomous-write subsystem (a future
    grant on the existing ladder).
  - No tool-surface pruning by posture (exposed-and-deny is retained
    deliberately).
  - Does NOT rest the day-2 safety on any setup wizard or first-login UI: the
    fail-closed empty-RBAC floor is the boundary; any setup or awareness UI is
    advisory UX on top of a hard backend floor, not the floor itself.
  - Does NOT finalize the first-login full-mode setup/awareness flow or a
    write-configuration latch (parked — their shape depends on enumerating what
    full mode requires configured beyond the first grant, which is deferred;
    whether such a latch is its own concept or merely the setup-step completion
    state is unresolved and downstream of that flow design).

- Relationship to other decisions:
  - References D-0018 for the floor lifecycle, immutability, sticky panic, and
    the write definition.
  - The principal model and the empty-RBAC fail-closed work are the
    implementation track that FOLLOWS this entry.
  - The RBAC sole-writer and audit-trail properties this entry relies on come
    from the prior identity-stage decisions — D-0016 (the admin REST API as the
    sole RBAC/identity writer; the audited admin surface) building on D-0012 (the
    admin gate) and D-0013 (admin-mutation audit). If the sole-writer/audit-trail
    mapping needs finer confirmation against those entries, treat that
    cross-reference as to-be-confirmed.

- Current state being changed (target diverging from live code; every item is
  "as of the investigation, verify against live code" and is NOT acted on here):
  - RBAC inert/permissive when auth off must become UNREACHABLE in full mode
    (full mode requires auth on and a live engine).
  - The autonomous Core Agent path must be routed through the shared enforcement
    seam and carry the autonomous principal.
  - A posture read endpoint with snake_case tags is net-new.
  - An observation-posture system-prompt line is net-new.
  - The observation-mode banner and the on-demand write-capability evaluation
    are net-new UI.
  - The denial precedence may require reordering enforcement and/or
    classification.

- Basis: a prior trust-model / safe-mode investigation (the file:line
  coordinates above are from that investigation and are marked
  verify-against-live-code; they were not re-verified for this entry, which is
  documentation-only). This entry records a DESIGN decision; no code change
  accompanies it, and the live behavior described under "Current state being
  changed" is what the design supersedes once implemented.
- Supersedes: nothing yet — the design is not yet implemented. Companion to
  D-0018 (the write floor's lifecycle and immutability), which this entry
  surrounds with the broader trust model. Builds on the identity-stage
  decisions D-0016/D-0013/D-0012 for the RBAC sole-writer and audit-trail
  properties it relies on. Adjacent pending decisions: the
  floor-vs-other-gate precedence (point 9) and the first-login full-mode flow
  (parked, above).

---

## D-0018 — The read-only write floor as a boot-resolved, runtime-immutable security boundary; safe mode is absorbed as one reason the floor is up, not a separate mechanism

- Date: 2026-06-07
- Status: design decision of record; the floor proper (points 1–5, 8) is now
  IMPLEMENTED — see the dated "boot-resolved, runtime-immutable write floor
  landed" implementation note at the end of this entry. The design narrative
  below was written before the build and is preserved as-is; where it says
  "PENDING"/"diverges from live code", read it against that note, which records
  what landed and what remains deferred (autonomous-path seam routing, the
  posture read endpoint/observation banner, and floor-vs-other-gate precedence).
- Decision: Joe's trust model has two boot postures — observation mode (a hard
  read-only floor, the day-one default) and full-capabilities mode (RBAC is the
  floor) — which, together with the pre-existing panic/safe mode, are unified
  into a single notion: "Joe cannot mutate the managed system right now." This
  entry covers only the floor's lifecycle and security guarantees. It does NOT
  cover full-mode graduation or empty-RBAC fail-closed behavior; those are
  separate pending decisions. The floor is treated as a SECURITY BOUNDARY, not
  a feature flag.

  **Threat model (what the floor must withstand).** (i) A fully compromised or
  erratic LLM emitting arbitrary tool calls. (ii) A human attacker reaching
  Joe's API as an authenticated caller. It explicitly does NOT defend against
  an attacker who controls the deployment substrate — host, environment, state
  file, supervisor — because such an attacker owns Joe regardless. That is the
  boundary's honest edge.

  **Definition of a write (load-bearing).** A write is an operation that
  mutates the state of the MANAGED SYSTEM — the live infrastructure Joe
  operates and the code/config that governs it (the sources). The test is
  whether the managed system is in a different state after the operation.
  Everything else is a read, even when it mutates local storage or emits
  something a human receives:
  - querying sources is a read;
  - recording observed state into Joe's own graph DB (the Core Agent refresh)
    is a read, because the graph is Joe's MODEL of the system, not the system;
  - emitting notifications or alerts to humans is a read, because it changes
    what humans know and Joe's own state, not the managed system.
  Consequence: graph-mutation operations are read-tier (T1) under this
  definition, NOT T2, and the floor governs only managed-system mutations.

  The decision, as numbered points:

  1. **One floor, two reasons, one error.** A single write floor denies all
     managed-system mutations (T2/T3) for every principal, including the
     autonomous one. It is up at boot if EITHER the observation env var is set
     OR a sticky panic state file is present. The reason rides out of the
     single denial as data — one write-floor error carrying a `reason` field,
     `reason` being either `observation` or `safe_mode` — not two code paths.
     Presentation differs per reason; enforcement is identical and singular.

  2. **Boot-resolved, runtime-immutable.** The floor is resolved exactly once
     at boot from its inputs into a sealed read-only value and is never
     re-derived from disk during the process lifetime. Within a process
     lifetime it is immutable — no endpoint, tool, LLM output, or operator
     action short of a restart can lower it. The live binary contains no
     down-transition capability; any deactivate or reset is removed from
     production and is test-only at most. Immutability is guaranteed by the
     lowering operation NOT EXISTING in the running program, not by permission
     guards.

  3. **One way down: change boot inputs and restart.** No live-clear path
     exists. Leaving observation mode and recovering from panic are the same
     operation: set the writable env var, ensure no sticky panic, restart.

  4. **The unlock HTTP endpoint and the unlock HTTP client command are
     deleted.** Panic recovery is a deploy-access operation, not an API
     operation. Clearing the sticky panic reason is done by a purely local
     host CLI that edits the panic state file while Joe is down, records an
     acknowledgment reason for audit, and never contacts or signals a live
     process. There is NO authorized API surface for clearing the floor.

  5. **Panic is sticky and wins over the env var.** When boot inputs disagree
     — env var writable but panic state present — the floor stays up. A panic
     must be explicitly acknowledged by clearing the panic state; flipping the
     env var alone is insufficient. This keeps acknowledging-the-panic and
     arming-writes as distinct deliberate steps, neither sufficient alone.

  6. **Protection A — no self-API reach.** No tool may call Joe's own
     control-plane endpoints or issue arbitrary HTTP to Joe's own API. If
     nothing in the tool registry routes to a floor, panic, or admin control,
     the LLM cannot reach the floor. Asserted by break-test over the registry.

  7. **Protection B — no self-restart influence.** Joe can stop itself (panic
     via process exit) but cannot start itself or influence the terms of its
     own restart. Restart authority is external — a supervisor with a restart
     policy, or a human. Panic uses process exit, never self-re-exec, because
     re-exec would place restart authority inside Joe and let a compromised Joe
     choose to return writable. Joe must have no ability to modify its own boot
     inputs (env var, panic state file location, supervisor config); those live
     outside Joe's writable surface.

  8. **Within-floor reason precedence.** `safe_mode` outranks `observation`
     when surfacing the reason. Precedence between the floor and OTHER denial
     sources (incident/captain gate, RBAC zone denial) is deferred to a
     separate precedence decision.

- Recovery process (the focused human work, ordered): the human triggers
  panic; Joe exits and writes the panic state; Joe is down. The human
  investigates by hand. A naive supervised restart brings Joe back read-only
  because the panic state is sticky, with a startup message stating the panic
  was not cleared and how to resume — Joe observes but cannot write. When the
  human decides it is safe, they clear the panic state via the local host CLI
  (recording a reason), set the env var to the intended posture, and restart.
  On boot: panic cleared + env writable yields a writable Joe; panic cleared +
  env read-only yields observation mode. All failure modes are safe-by-default:
  doing nothing leaves Joe down (most inert); restarting without clearing panic
  yields a read-only Joe; clearing panic but leaving the read-only env var
  yields an observation Joe. No careless path yields a writable Joe.

- What this deliberately does NOT do:
  - No runtime toggle into or out of observation mode (boot + restart only; a
    runtime stop-all-writes need is served by panic, not a second control).
  - No API path to clear panic or lower the floor (the surface is ELIMINATED,
    not protected).
  - No defense against a deployment-substrate attacker (whoever controls env
    vars, the panic state file, and restart authority controls the floor —
    this is the boundary's honest edge).
  - Panic-recovery semantics REQUIRE a supervisor: bare Joe with no restart
    policy means panic equals halt and Joe stays down (a safe fail). So "panic
    puts Joe in safe mode" precisely means "panic stops Joe; a supervised Joe
    reboots into safe mode" — running under a supervisor with a restart policy
    is a documented deployment requirement.
  - No autonomous-write capability: the Core Agent path is routed through the
    shared seam and classified read-tier, with managed-system write capability
    deferred to a future graduation step.

- Current state being changed (target diverging from live code). Every
  coordinate below is from a prior investigation, may be STALE, and is NOT to
  be acted on as part of recording this decision — each is "as of the
  investigation, verify against live code":
  - Safe mode is currently boot-set but live-clearable, with the unlock path
    calling an in-process deactivate with no restart (as of the investigation,
    around `internal/safety/unlock.go` and `internal/api/panic.go` — verify
    against live code). The live down-transition must be removed and
    reset/deactivate become test-only. Note the reset function's own comment
    already claims it is restart-only for testing, which the live call
    contradicts.
  - The floor is currently a mutable process-global atomic boolean with a
    public setter (as of the investigation, around
    `internal/safety/safemode.go` — verify against live code) and becomes a
    resolve-once read-only value.
  - The single denial branch currently keys on a plain safe-mode sentinel
    error with no reason field (as of the investigation, around
    `internal/tools/executor.go` and `internal/safety/safemode.go` — verify
    against live code) and is subsumed into the write-floor error with a
    `reason` field. The break-set that must continue to satisfy `errors.Is`
    against the floor sentinel: the classifier in
    `internal/api/writefailure.go`, the executor safe-mode test, and two
    assertions in the writefailure test.
  - The autonomous Core Agent refresh currently bypasses the executor seam
    entirely, writing the graph directly via the graph-delta path (as of the
    investigation, around `internal/coreagent` — verify against live code),
    carrying no principal and no tier check. It must be routed through the
    shared seam so future managed-system writes are governed by construction,
    while its current graph mutations classify as read-tier and pass the floor
    so observation Joe's graph stays live.
  - Graph-mutation tools are currently classified T2 (as of the investigation,
    around `internal/safety/tier.go` — verify against live code) and must be
    reclassified to T1 per the write definition. The full tier map must be
    audited against the managed-system-mutation test, with the dangerous
    direction being any UNDER-classified real infrastructure mutation rather
    than the over-classified graph ops.
  - The unlock CLI command is currently an HTTP client call (as of the
    investigation, around `cmd/joe/main.go` — verify against live code) and is
    replaced by a local file-only operation, with the endpoint and HTTP-client
    paths deleted.

- Invariants to assert (the break-tests the implementation must add):
  - The production binary contains no runtime down-transition of the floor (no
    production caller of any deactivate or reset).
  - The floor value is read from a single boot-resolved source and never
    re-derived from disk mid-process.
  - No registered tool at any tier routes to a floor, panic, or admin control,
    or can issue HTTP to Joe's own control plane.
  - A write-floor error satisfies `errors.Is` against the floor sentinel
    (preserving the existing dependents).
  - With the panic state present, the floor boots up regardless of the env var.
  - Panic uses process exit rather than self-re-exec, with no production path
    letting Joe alter its own boot inputs.

- Basis: a prior trust-model / safe-mode investigation (the file:line
  coordinates above are from that investigation and are marked
  verify-against-live-code; they were not re-verified for this entry, which is
  documentation-only). This entry records a DESIGN decision; no code change
  accompanies it, and the live behavior described under "Current state being
  changed" is what the design supersedes once implemented.
- Supersedes: nothing yet — the previous standalone safe-mode lifecycle is
  absorbed into the unified floor by this design, but that supersession takes
  effect only when the implementation lands. Relates to the existing
  panic/safe-mode mechanism (`internal/safety/`), which this design unifies
  with the observation-mode floor. Adjacent pending decisions (NOT covered
  here): full-mode graduation, empty-RBAC fail-closed, and floor-vs-other-gate
  precedence.

- Implementation note (2026-06-07) — tier-map reclassification landed. This
  note records the partial implementation of the write definition above
  (D-0018) and its trust-model application (D-0019) in the tool tier
  classification only. The floor's lifecycle/immutability (points 1–8) remains
  PENDING; only the classifier in `internal/safety/tier.go` was changed.
  - Classifier confirmed: `ClassifyTool` + `toolRegistry` in
    `internal/safety/tier.go`; tiers `TierObserve`=1 < `TierRecord`=2 <
    `TierAct`=3; unknown tools default to `TierAct` (the most conservative
    tier) — left unchanged, now guarded by a test.
  - Reclassified Joe-own-model maintenance from T2 (Record) to T1 (Observe),
    per the write definition (these mutate Joe's model, not the managed
    system): `graph_add_node`, `graph_add_edge`, `graph_update_node`,
    `register_source`, `save_onboarding_fact`, `generate_doc_draft`. This
    realizes the "graph-mutation operations are read-tier (T1), NOT T2"
    consequence stated above.
  - Added four registered tools that were MISSING from the tier map and so
    fell through to the unknown→TierAct default (permanently denied — a
    safe-direction but functionally broken state): `save_knowledge_entry`
    (Joe-own knowledge store) and the read-only `registry_query`,
    `artifactory_query`, `ecr_query`. All added at T1 (Observe).
  - `github_comment` / `gitlab_comment` were T2 (Record). Per a deliberate
    decision (posting to a PR/MR mutates an external system and is not
    idempotent on retry), they are reclassified UP to T3 (Act) as
    managed-system writes — not down to observe. They were already
    deny-by-default (their policy keys are unrecognized by `IsT3Allowed`/
    `IsT2Allowed`, same pre-existing gap as `github_request_changes` and
    `publish_doc_update*`); the change corrects the label and routes them
    through the T3 blocking pre-execution notification.
  - Latent floor hole found and closed (the dangerous under-classified
    direction this audit was meant to catch): `http_request` was T1 but
    accepted POST/PUT/PATCH/DELETE with a body to any URL — a write-capable
    tool classified read-only, always allowed and ungated. It is a live
    registered tool. Resolved by restricting the tool to GET/HEAD in
    `internal/tools/shared/httpreq/httpreq.go` (mutating verbs now rejected
    before any request), making its T1 classification correct rather than
    bumping it to T3 (which would have broken legitimate probing and, lacking
    a policy key, denied it permanently).
  - No entries were left unclear: every tool's managed-system effect was
    determinable from its implementation. The two genuinely consequential
    judgments (comment-tool direction; http_request remediation) were taken as
    explicit human decisions rather than guessed.
  - Consequence on the Record tier: T2 is now VACANT — no registered tool is
    record-tier. The tier constant and its policy/enforcement plumbing
    (`RecordPolicy`, `IsT2Allowed`) are retained for forward compatibility but
    are currently vestigial.
  - Enforcement-behavior changes surfaced (the demotions un-gate where the
    T1 bypass applies; conscious, consistent with the intent that Joe's model
    stays live in safe mode / incident regimes): the reclassified
    model-maintenance tools no longer consult the safety policy, no longer fire
    the after-action audit notification, and bypass the safe-mode block, the
    captain/session incident gates, and the DurableExecutor crash-resume
    idempotency wrapper. The last point means `register_source` (random-ID
    create) and `save_onboarding_fact` lose retry de-duplication — flagged for
    a conscious follow-up if idempotency is desired for Joe-own writes; not
    changed here.
  - Break-tests added/updated (`internal/safety/tier_test.go`): graph-mutation
    family asserted T1; unknown-tool default asserted most-conservative;
    comment tools asserted T3. Gate/executor/durability tests that used
    `graph_add_node` as their representative write were repointed to a real
    managed-system write (`write_file`).

- Implementation note (2026-06-07) — IMPLEMENTED: the boot-resolved,
  runtime-immutable write floor (points 1–5, 8) landed. This realizes the
  floor's lifecycle and immutability; the items in "What this deliberately does
  NOT do" that are out of THIS task's scope are enumerated as deferred below.
  Phase 1 re-verified the divergence coordinates against the live tree before
  any change; all matched as described.
  - Sentinel subsumed into a reason-carrying floor error. The former plain
    safe-mode sentinel (`var ErrSafeModeActive` in the now-deleted
    `internal/safety/safemode.go`) is replaced by `*safety.WriteFloorError`
    (`internal/safety/floor.go`) carrying a `Reason` of `observation` or
    `safe_mode`. errors.Is compatibility is preserved by a new floor-identity
    sentinel `safety.ErrWriteFloor` plus a `WriteFloorError.Is` method that
    matches it, so the FOUR pre-existing dependents (verified: the classifier in
    `internal/api/writefailure.go`; the executor floor test; and two assertions
    in `internal/api/writefailure_test.go`) keep matching via
    `errors.Is(err, ErrWriteFloor)`. The executor's single write-denial branch
    (`internal/tools/executor.go`) now returns this error.
  - Mutable boolean + public setter replaced by a resolve-once read-only value.
    The process-global `atomic.Bool` and `ActivateSafeMode/DeactivateSafeMode/
    IsSafeModeActive` are deleted. The floor is `safety.WriteFloor`, a value type
    exposing only `Up()/Reason()`. It is computed once at boot by the PURE
    `safety.ResolveWriteFloor(panicStatePresent, observationEnvSet)` and sealed
    into `core.Services.WriteFloor` — the single process-wide source, injected
    into both tool executors via `tools.WithWriteFloor` and read by the panic
    status handler. It is never re-derived from disk mid-process.
  - Live deactivate removed from the production binary (THE immutability
    guarantee). `internal/safety/unlock.go` (which called the in-process
    `DeactivateSafeMode()` + panic-flag `Reset()`) and `Reset()` itself are
    deleted. No production code transitions the floor up→down at runtime; the
    lowering operation does not exist in the running program. Enforced
    structurally by a repo-walk guard (`internal/safety/floor_guard_test.go`,
    `TestWriteFloor_NoRuntimeLoweringPath`) that fails if any of `safeModeActive
    / ActivateSafeMode / DeactivateSafeMode / IsSafeModeActive / ErrSafeModeActive
    / func Reset( / safety.Reset(` reappears in production code.
  - Observation env var added as a boot input with its own code/message distinct
    from safe mode. `JOE_MODE=observation` (`internal/env/keys.go`,
    read once in `cmd/joe/server.go` consistent with the other `JOE_*` boot env
    vars) raises the floor with reason `observation`. New write-failure code
    `errorCodeObservation` ("observation") with its own classifier branch and a
    CALM frontend message in `ui/src/hooks/useChat.ts` ("…intended read-only
    posture") that does NOT mention unlock or safe mode. Note: per this task's
    contract the floor is DOWN when neither input is set (writable, RBAC
    governs); D-0019's "observation is the day-one default" posture is a
    separate graduation decision, not implemented here.
  - Panic made sticky with `safe_mode`-wins precedence. `ResolveWriteFloor`
    resolves panic-present → `safe_mode` REGARDLESS of the observation env var;
    observation-only → `observation`; neither → down. All four combinations are
    pinned by `TestResolveWriteFloor_Precedence`
    (`internal/safety/floor_test.go`).
  - Unlock endpoint/CLI replaced by a local-file-only clear with restart-required
    semantics. The `POST /api/v1/unlock` endpoint, its `*client.Client.Unlock`
    HTTP-client method, and `safety.Unlock` are deleted. `joe unlock --reason`
    now calls `safety.AcknowledgePanic(joeDir, reason)` — clears the persisted
    `panic.state` file locally (recording the reason to the audit log), contacts
    no process, references no floor, and prints "panic state cleared — restart
    joe to resume writes." Recovery is now: clear panic state (this CLI) + set
    `JOE_MODE` to the intended posture + restart.
  - Break-tests (all passing): precedence over all four input combinations incl.
    both-set; `WriteFloorError` satisfies `errors.Is(ErrWriteFloor)` and carries
    the right reason incl. when wrapped; the no-runtime-lowering repo-walk guard;
    floor-not-re-derived-from-disk (an executor with an up floor still denies
    after `ClearPanicState` removes the file mid-process) plus a guard asserting
    `executor.go` references neither `ReadPanicState` nor the panic-state file;
    distinct presentation (a denied Mutate under observation → `observation`
    code/message, under safe mode → `safe_mode`, single enforcement branch),
    asserted in Go (`writefailure_test.go`) and TS (`writeFailureMessage.test.ts`).
  - Sticky-panic recovery rests on the panic→exit→restart mechanism, which Phase
    1 re-verified holds: all three trigger paths exit the process via
    `os.Exit(2)` (API handler in `internal/api/panic.go`; SIGUSR1 handler and the
    panic CLI's server-side trigger in `cmd/joe/server.go`) and never flip the
    floor in-process; boot re-reads the persisted panic state in
    `cmd/joe/server.go`. No path was found that lowers the floor in-process
    without exiting, so the guarantee holds (no prerequisite gap).
  - DEFERRED to following tasks (explicitly NOT in this change): routing the
    autonomous Core Agent graph-refresh path through the executor seam (the Core
    Agent's LLM-tool executor IS floor-governed here, but its direct graph-delta
    writes still bypass the seam); the posture read endpoint and the observation
    banner (only the write-failure code/message landed so a denied write surfaces
    correctly); and denial precedence between the floor and other denial sources
    (incident/captain gate, RBAC zone denial) — the classifier ordering is left
    unchanged. Cluster note: the local-file-only `joe unlock` clears only the
    local `panic.state`; in a clustered deployment where boot also reads the
    shared `cluster_panic_state` row, that row must be cleared separately — the
    former live cluster-clear rode on the now-deleted `safety.Unlock`.

- Implementation note (2026-06-08) — IMPLEMENTED: panic state consolidated to a
  single store (the DB row); the `panic.state` file deleted entirely. This
  closes the clustered-recovery divergence flagged in the prior note above —
  by REMOVING the second store, not by patching a cluster-clear into the CLI.
  Phase 1 re-verified every coordinate against the live tree before any change.
  - Single home. Panic state had TWO stores — a local `panic.state` file AND the
    shared `cluster_panic_state` DB row — and boot OR'd both. The file-only
    acknowledge (`safety.AcknowledgePanic`) cleared only the file, leaving the
    row set: a recovery hole. There is no clustered Joe today, so the split was
    unnecessary. Panic state now has ONE home, the DB row — the same single-store
    principle this entry applied to the write floor itself (one boot-resolved
    value, no drift between sources).
  - Panic entry writes ONLY the row. All three entry paths now persist via
    `safety.Trigger` → the boot-registered cluster store
    (`store.sqlPanicStore.SetPanicked`, extended to record source/reason/
    triggered_at into the existing migration-008 columns), then `os.Exit(2)`:
    the API handler (`internal/api/panic.go`), the SIGUSR1 handler
    (`cmd/joe/server.go`), and the panic CLI (an HTTP call to the API handler).
    The `safety.WritePanicState` file write was removed from every path.
  - Boot reads ONLY the row. `cmd/joe/server.go` resolves the floor from
    `clusterPanicStore.IsPanicked(ctx)` alone (file read deleted). Boot-order was
    VERIFIED SAFE in Phase 1: the panic read already sat AFTER store init/migrate
    and BEFORE the floor is sealed into `Services` and any tool executor is wired,
    so moving to DB-only required no reordering — nothing in boot needs panic
    state before the store is available. Floor resolution logic
    (`ResolveWriteFloor`: panic→`safe_mode` regardless of env var; observation
    env→`observation`; neither→down) is UNCHANGED — only its panic-state INPUT
    moved from file-or-DB to DB-only.
  - File deleted as a concept. `internal/safety/panic_state.go` (the
    `panic.state` writer `WritePanicState`, reader `ReadPanicState`, clearer
    `ClearPanicState`, the `panicStateFile` constant, the file-serialization
    `PanicState` struct, and `AcknowledgePanic`) and its test are deleted. The
    in-memory `safety.PanicInfo` struct + `ClusterPanicStore.PanicInfo` carry
    who/when/why for boot logging and the status endpoint, sourced from the row.
  - Acknowledge CLI rewritten to the DB row. `joe unlock` opens the store
    DIRECTLY (config + `store.New` + `Migrate`, the daemon's own pattern) and
    NEVER contacts or signals a running process. It is read-then-report-
    conditionally and idempotent: reads the row first, and only when a panic is
    present clears it ("panic state was present and has been cleared; restart to
    resume writes if no other read-only posture is set"); when no panic is
    present it clears nothing and says so ("Joe is not in a panicked state;
    nothing to clear") — neither message asserts the daemon's live state nor
    promises writes resume unconditionally (observation mode may independently
    hold the floor up). The two functional cases both exit 0; only a genuine
    store-access failure exits non-zero, so the report never lies. `--reason` is
    now optional (logged when given), not required.
  - Single-node contention handled. Panic entry exits the process, so during
    recovery the daemon is down and the CLI opening the SQLite store does not
    contend. In the not-panicked case (operator runs `joe unlock` on a healthy
    running Joe) the CLI only READS the row — no write — and SQLite WAL +
    `busy_timeout(5000)` make the short-lived second open non-disruptive.
  - Immutability guarantees intact. No runtime lowering path, no live setter,
    floor not re-derived mid-process — all preserved. Clearing the DB row affects
    only the NEXT boot; a running process's sealed floor is unchanged by the CLI.
  - Break-tests (all passing): `TestPanicState_SingleHomeNoFileConcept`
    (repo-walk guard that fails if any `panic.state` file writer/reader/clearer/
    constant reappears in production code, analogous to the no-lowering guard);
    `TestExecutor_Floor_NotReDerivedFromDBRow` (an executor with an up floor still
    denies after the panic row is cleared mid-process — the not-re-derived
    guarantee re-expressed against the DB row); the executor source-scan guard
    `TestWriteFloor_NotReDerivedFromDiskInExecutor` extended to also forbid the
    DB-row readers (`PanicStore`/`IsPanicked`/`PanicInfo`/`cluster_panic_state`)
    in `executor.go`; `TestRunUnlockCommand_PanicPresent` /
    `TestRunUnlockCommand_NoPanic` (conditional clear + exit-0 in both cases);
    store `PanicInfo` round-trip in `TestPanicStore_StateTransitions`. The
    pre-existing `TestWriteFloor_NoRuntimeLoweringPath` and
    `TestResolveWriteFloor_Precedence` still pass unchanged.

---

## D-0017 — The captaincy transfer handshake authenticated confirm/cancel but never bound the caller to the transfer; any authenticated principal could complete or abort a transfer it was not part of

- Date: 2026-06-07
- Decision: This is a defect entry, not a polish entry — an authorization
  bypass in the captaincy control plane, the same family as D-0012 (a
  control that authenticated the caller but never checked that the caller
  was entitled to the specific action). The §B captain transfer handshake
  (`internal/sessionmodel/captain.go`, exposed over HTTP at
  `internal/api/captain.go`) is a three-step state machine: `transfer/begin`
  opens an in-flight solicitation, then `transfer/confirm` completes the
  captaincy swap or `transfer/cancel` aborts it. EdgeAuth resolves the
  caller's principal into request context for all three. **`begin` already
  persisted both parties of the handshake** — the in-flight record lives on
  the active `session_captains` row (scoped to the active incident session
  via `detached_at IS NULL`), whose `principal` column is the
  soliciting/outgoing captain and whose `incoming_principal` column is the
  solicited incoming principal. But `confirm` and `cancel` **read the
  caller's principal only to write their audit row and never compared it to
  either party.** `CaptainService.ConfirmTransfer(ctx, sessionID)` and
  `CancelTransfer(ctx, sessionID)` took no caller principal at all:
  - **(a) The gap.** Any authenticated principal — including one who is
    neither the outgoing captain nor the solicited incoming principal —
    could `POST .../transfer/confirm` and finalize the swap to the recorded
    `incoming_principal`, or `POST .../transfer/cancel` and abort a transfer
    it had no part in. The caller was authenticated; it was never
    *authorized* against the handshake. (The two handlers also did not even
    reject the `rbac.Unknown` principal the way `attach`/`heartbeat`/`begin`
    do.) The binding data existed in the row the whole time — only the check
    was missing.
  - **(b) The binding model now enforced.** The caller principal is threaded
    into both service methods. **Confirm** is authorized to exactly one
    principal: the solicited `incoming_principal` named in the in-flight
    record. The outgoing captain cannot confirm in the incoming principal's
    place; a third principal cannot confirm at all → `ErrNotSolicitedIncoming`.
    **Cancel** is authorized to *either* party — the soliciting/outgoing
    captain (`principal`) or the solicited incoming (`incoming_principal`);
    any third principal → `ErrNotTransferParty`. A confirm/cancel with no
    matching in-flight solicitation is still rejected first with
    `ErrNoTransferInFlight` (the binding is checked against a real record, or
    there is nothing to act on). No new persistence was added — `begin`
    already recorded both parties; this fix is enforcement, not schema.
  - **(c) The authorization-failure convention used.** Both new sentinel
    errors map at the HTTP layer to `403` `"forbidden"`, matching the
    existing captain-control surface convention — the same shape
    `heartbeat` uses for `ErrCaptainPrincipalMismatch` (typed sentinel in
    `sessionmodel`, matched via `errors.Is`, rendered as a stable 403). No
    new error-code vocabulary was invented.
  - **(d) The break-tested invariant.** `TestCaptain_ConfirmBoundToSolicitedIncoming`
    and `TestCaptain_CancelBoundToHandshakeParties`
    (`internal/sessionmodel/captain_test.go`) assert the negative cases
    structurally: a non-party confirm/cancel returns the typed forbidden
    error *and* leaves the in-flight transfer untouched (captain unchanged,
    state still `transfer_requested`), and confirm by the outgoing captain in
    the incoming principal's place is rejected. `TestCaptainAPI_TransferConfirmCancelBindToParties`
    (`internal/api/captain_test.go`) pins the 403 wire mapping. All three
    were break-tested: neutralizing either binding in `captain.go` turns the
    rejections into successes and fails the suite (confirm-by-third-party
    returns nil/200 and swaps the captain; cancel-by-third-party resolves the
    transfer), confirming the tests fail if the principal binding is removed.
- Scope held: the transactionality of the captaincy swap itself
  (`completeTransfer` — two sequential repo writes, no shared tx; call site
  unchanged at `ConfirmTransfer` and the `begin` shortcut paths) and the
  resolve-path dangling-row behavior are SEPARATE findings and were not
  touched here. The non-transactional swap is noted, not fixed.
- Basis: `internal/sessionmodel/captain.go` (`ConfirmTransfer`/`CancelTransfer`
  now take and check `callerPrincipal`; `ErrNotSolicitedIncoming`/
  `ErrNotTransferParty` added) and `internal/api/captain.go` (handlers thread
  `string(principal)`, reject `rbac.Unknown`, map the typed errors to 403),
  verified by `go build ./...`, `go vet ./...`, `gofmt -s -w .`, and
  `go test ./...` green, plus the break-test described in (d). The pre-state
  is the captain/incident investigation under `docs/investigations/`, read
  against current code and confirmed.
- Supersedes: nothing — it closes a defect, it does not revise a prior
  decision. Same family as D-0012 (authenticate-without-authorize); builds on
  D-0010 (the shared §C captain gate) and D-0009 (captain-transition audit),
  neither of which is changed.
- Status: active. Authorization bypass closed; binding break-tested. The
  non-transactional `completeTransfer` swap and the resolve-path dangling-row
  behavior remain open as separate findings.

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
