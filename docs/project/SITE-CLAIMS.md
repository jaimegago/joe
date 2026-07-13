# Joe — Site Claims Register

This register lists the **load-bearing claims published on joeagent.dev**, each mapped to
the **mechanism it rests on** and the **guard or break-test pinning it** where one exists.
Its purpose is drift detection: any change to a listed mechanism obligates the changing
session to **flag a joeagent.dev revision in its session report**. A claim whose mechanism
still passes its pinning test is not drift; a claim whose mechanism changed shape — or lost
its guard — is.

The obligation runs **both directions**: a session that changes a listed mechanism flags a
site revision, and a session that **publishes a new load-bearing claim to a joeagent.dev
publication source** adds the corresponding register entry (or entries) in the **same
session** — so the register does not fall behind the copy it exists to track.

**Conventions.**

- Entries carry **test names, never file:line coordinates** — per the D-0032 principle that
  volatile references do not belong in standing documentation. Test functions are stable
  handles; line numbers rot.
- Claims marked **launch-bound** or **mechanism-bound** are **planned revision points, not
  drift**: the published copy is deliberately pinned to a posture that is expected to change
  on a known trigger (a launch decision, a later mechanism landing). Reaching that trigger is
  the cue to revise the copy, not a regression to fix.
- Claim wording here is a short paraphrase for lookup; the **published copy is authoritative**
  for exact phrasing. This register is a derived pointer, not a source of truth for the site.
- The register-maintenance duty is **bidirectional**: a mechanism change flags a copy revision,
  and newly published load-bearing copy gets its register entry in the same session that
  publishes it. Neither half is optional; the register is kept level with the site by both.

Format per entry: **claim** (short form) · **page/section** · **mechanism** · **pinning
test(s)** · **binding note** (where applicable).

---

## Safety deep-dive — `/safety/`

### The write floor is boot-resolved and runtime-immutable, with observation as the day-one default

- **Claim.** The write floor is resolved once at boot and is immutable for the life of the
  process; nothing at runtime lowers it, recovery is a restart. An unconfigured Joe (`JOE_MODE`
  unset, or `=observation`) boots read-only in observation mode; `JOE_MODE=full` is refused at
  boot pending implementation; unrecognized values are refused fail-closed.
- **Mechanism.** `internal/safety/floor.go` (`ResolveWriteFloor`, no live down-transition) plus
  the boot-mode decision function `env.ResolveBootMode` mapping the raw `JOE_MODE` value ahead
  of floor resolution (`internal/env/keys.go`), called once by the boot caller.
- **Pinning tests.** `TestResolveWriteFloor_Precedence`, `TestResolveBootMode`.
- **Binding note. Launch-bound.** The *refusal of `full`* is explicitly pinned to the current
  pre-launch posture in the copy ("a governed full-capabilities mode is forthcoming"). When
  full mode is implemented (`docs/backlog/full-mode-rbac-track.md`, `observation-default.md`)
  the boot-branch copy revises. The observation *default* itself is stable.

### The floor cannot be lowered at runtime — repository-walk and executor guards

- **Claim.** No code path in the binary lowers the floor once it is up; the executor does not
  re-derive the floor from disk; panic state has a single home and no file concept.
- **Mechanism.** Absence-of-setter / no-runtime-lowering enforced structurally across the
  repository; the executor reads the sealed value, never a disk source.
- **Pinning tests.** `TestWriteFloor_NoRuntimeLoweringPath` (repository-walk guard),
  `TestWriteFloor_NotReDerivedFromDiskInExecutor`, `TestWriteFloor_NoSetter`,
  `TestPanicState_SingleHomeNoFileConcept`.

### Actions are a binary Read/Mutate axis; unknown tools default to Mutate

- **Claim.** Every tool is classified as either a read or a mutation; reads run
  unconditionally, mutations are denied by default, and an unrecognized tool defaults to
  Mutate (deny-by-default), never Read.
- **Mechanism.** `internal/safety/tier.go` — `ActionRead`/`ActionMutate`, the `toolRegistry`
  classification map, and `ClassifyTool`'s unknown-default; `CheckAccess` enforcing
  read-always / mutate-default-deny.
- **Pinning tests.** `TestClassifyTool_KnownTools`, `TestClassifyTool_UnknownDefaultIsMutate`,
  `TestActionClass_IsBinary`, `TestCheckAccess_ReadAlwaysAllowed`,
  `TestCheckAccess_MutateDefaultDeny`, `TestCheckAccess_UnknownToolDenied`.

### The layered pipeline checks the floor at every layer; mutating tools live only in the governed loops' registry

- **Claim.** A mutation passes through a fixed-order pipeline in which the write floor is
  checked ahead of the incident gate and RBAC; mutating tools exist only in the registries of
  the governed loops, so there is no ungoverned surface holding one.
- **Mechanism.** Executor gate order (`internal/tools/executor.go`) with denial precedence
  floor > incident > RBAC; tool-registry composition (`internal/tools/default.go`) restricting
  which loop advertises which tool (e.g. `web_search` on the user task loop only, absent from
  the `agent:core` registry).
- **Pinning tests.** `TestFloorPrecedesIncidentGate`, `TestFloorDownGateStillRefuses`,
  `TestFloorAllowsReadsThroughGate`.

### The graph-refresh read surface is a stated, tracked bypass

- **Claim.** The autonomous `agent:core` graph-refresh read surface is governed separately
  from human-facing transport reads and is named in the copy as a deliberate, tracked
  exception rather than a hidden gap.
- **Mechanism.** Separated engine construction — the read-posture resolver is wired into the
  transport policy engine only (`NewPolicyEngineWithGovernance`), never the refresh engine
  (`NewPolicyEngineWithPromote`, posture seam nil); the autonomous read surface is governed by
  `auto_promote_read` plus grants.
- **Pinning tests.** None (documented tracked exception; the separation is an architectural
  invariant, not a single break-test).

### The guarded accessor is the audit point, fail-closed on mutate and fail-open on read, over an insert-only repository

- **Claim.** Every read and mutation flows through one guarded accessor that writes an
  append-only audit record; an audit-store failure fails **closed** for mutations and **open**
  for reads; the audit repository API is insert-only, with the DB-level immutability caveat
  that it rests on SQLite triggers, not a structural impossibility.
- **Mechanism.** `internal/access` guarded accessor on both HTTP and agent paths; the
  append-only audit repository surface (`internal/audit`); migration-015 SQLite triggers
  blocking UPDATE/DELETE on the audit table.
- **Pinning tests.** `TestPhaseF_FailClosedOnMutate`, `TestPhaseF_FailOpenOnRead`,
  `TestPhaseF_AllowedAccessProducesOneAllowAuditRow`, `TestRepositoryAPISurface_AppendOnly`,
  `TestMigration015_TriggerBlocksUpdate`, `TestMigration015_TriggerBlocksDelete`.
- **Binding note.** The copy states the SQLite-trigger caveat explicitly; if the audit store
  moves off SQLite the immutability guarantee's basis (and its copy) changes.

### The incident gate is deny-only, adds captain semantics, and has a single shared implementation

- **Claim.** During a declared incident, non-captain mutations are refused by a gate that can
  only ever deny (never grant authority RBAC would refuse); the gate is a single shared
  implementation, not re-implemented per call site.
- **Mechanism.** `internal/captaingate` — the deny-only incident gate on the loop path, with
  the single-shared-implementation structural guard.
- **Pinning tests.** `TestPhaseG_GateIsDenyOnly_RBACAuthorityInvariance`,
  `TestPhaseG_SingleSharedCaptainGateImplementation`, `TestCaptainGate_EndToEnd`,
  `TestPhaseG_LoopPathNonCaptainMutationRefused`, `TestPhaseG_GateRefusalRecordedInAuditTrail`.

### Panic persists then exits; state is a sticky DB row; unlock is local-only with no HTTP endpoint

- **Claim.** Panic persists the safe-mode state and then exits the process; the state is a
  sticky DB row that survives restart; recovery (`joe unlock`) is a local-only operator act
  with **no** HTTP unlock endpoint; running under a supervisor is required for restart.
- **Mechanism.** `cluster_panic_state` single-row store (`internal/store/panic_store.go`);
  panic raises the floor and exits; the `joe unlock` CLI subcommand; the deliberate absence of
  an unlock route.
- **Pinning tests.** `TestRegisterPanicRoutes` (asserts `POST /api/v1/unlock` returns 404 — no
  unlock endpoint exists), `TestPanicState_SingleHomeNoFileConcept`.

### Credential invariants: kubeconfig confined, no impersonation, exec provider deleted, two shipped auth methods, env-var uniqueness at promotion

- **Claim.** The Kubernetes transport is a hand-built `*rest.Config` with no kubeconfig
  ingestion, no impersonation, and no exec/auth provider; the kubeconfig-exec provider is
  deleted; `clientcmd` is confined to two named adapters and barred from the transport and
  credential paths; exactly two auth methods ship (`static-bearer`, `entra-exchange`); and an
  `env_var` credential locator is enforced unique across all components at promotion.
- **Mechanism.** `buildRESTConfig` (`internal/adapters/k8s/k8s.go`) sets only Host/CAData/
  BearerToken; `clientcmd` confinement break-tests; the two credential Kinds
  (`internal/credential`); `staticEnvVarConflict` at the promotion seam
  (`internal/api/components.go`).
- **Pinning tests.** `TestNoClientcmdOutsideAllowedAdapters`,
  `TestTransport_NoKubeconfigIngestion`, `TestCredentialPackage_NoKubeconfigIngestion`,
  `TestTransport_NoForbiddenAuthMechanisms`, `TestTransport_RESTFieldsSetOnlyInBuilder`,
  `TestEntraProvider_TransportAgnostic`, `TestPromote_StaticEnvVarUniqueness`.
- **Binding note. Mechanism-bound.** The operator-supplies-the-env-var model and the
  two-auth-method count are both current-mechanism claims: a future platform credential-broker
  integration (e.g. an AWS/Azure-native secret source, or a third auth method) would revise
  both the count and the "operator supplies a name-only locator" framing in the copy.

### Skills posture: mutating routes are admin-gated with an AST guard and denial audit; lifecycle-audit and load-time integrity are stated as open boundaries

- **Claim.** The three mutating skills HTTP routes (reload/approve/reject) are admin-gated,
  pinned by a structural AST guard, with denials producing audit rows; list stays
  authenticated-only. The copy states the current boundaries: no dedicated skill-lifecycle
  audit Kind and no load-time content-integrity verification yet.
- **Mechanism.** `server.requireAdmin` on the three mutators (`internal/api/skills.go`); the
  AST route guard; `requireAdmin` denial path writes a `KindAdminAccess`/deny row; `GET
  /api/v1/skills` deliberately exempt.
- **Pinning tests.** `TestSkillsRoutes_MutatorsRequireAdminGate` (AST guard),
  `TestSkillsMutators_NonAdminForbiddenAndInert`, `TestSkillsMutators_AdminSucceeds`,
  `TestSkillsList_NonAdminAllowed`.
- **Binding note.** The stated open boundaries (lifecycle-audit Kind, load-time integrity) are
  deferred work (`docs/backlog/skills-governance-hardening.md`, D-0075 items 2–3); the copy's
  boundary sentences revise when either lands.

### Joe speaks MCP in one direction only — server, never client

- **Claim.** Joe runs as an MCP server (`joe mcp`) exposing its own governed tools and
  deliberately does not act as an MCP client consuming external servers' tools, because the
  protocol carries no enforceable mutation classification. The copy carries a precision note
  that this stance is not yet pinned by a guard test.
- **Mechanism.** `joe mcp` is server-only; no MCP-client dispatch path exists in the tree
  (D-0067).
- **Pinning tests.** None yet.
- **Binding note. Mechanism-bound.** The "not yet test-pinned" sentence in the copy dies when
  the guard lands (`docs/backlog/mcp-client-absence-guard.md`); revise the copy to cite the new
  guard test at that point.

### OASIS: Joe is a validated pipeline stage, with a deliberate no-verdict stance

- **Claim.** Joe participates in the OASIS evaluation as a validated pipeline stage, and the
  copy deliberately states no evaluation verdict/score.
- **Mechanism.** External evaluation relationship, not a code mechanism; the no-verdict stance
  is an editorial posture.
- **Pinning tests.** None (external relationship; no code guard).
- **Binding note. Launch-bound.** The section is planned to be rewritten at the OASIS
  reference-evaluation re-score (`docs/backlog/oasis-relationship.md`, post-Phase-2); the
  rescore is the cue to revise, not drift.

---

## Landing page — `joeagent.dev/`

### Governed by construction — "Joe running implies Joe governed"

- **Claim.** If the daemon is up it is authenticated, authorized, and write-gated; there is no
  configuration that produces a running-but-ungoverned Joe. Boot refuses without an identity
  configuration.
- **Mechanism.** Boot refuses without OIDC issuer or a service account; the single guarded
  accessor (`internal/access`) evaluates every read and mutation on both the HTTP and agent
  paths and writes the decision to the append-only audit record.
- **Pinning tests.** `TestEveryDispatchMethodDeclaresAnAction` (structural: every
  principal-gated dispatch declares its action), `TestPhaseF_FailClosedOnMutate`,
  `TestPhaseF_FailOpenOnRead`.

### One governance seam — the guarded accessor is the only way to reach an adapter or the graph

- **Claim.** There is a single governed path to every system; the same seam serves the HTTP
  handlers and the in-process agent, with no privileged shortcut for Joe's own reasoning, and
  the seam cannot be bypassed because a structural test build-fails on direct adapter/graph
  access outside a narrow allowlist.
- **Mechanism.** `internal/access` guarded accessor as the sole adapter/graph reach; the
  build-failing structural guard over principal-gated dispatch methods.
- **Pinning tests.** `TestEveryDispatchMethodDeclaresAnAction`.

### Ships in observe mode

- **Claim.** Joe ships read-only: an unconfigured install comes up in observation mode with the
  write floor raised, seeing and reasoning about infrastructure without being able to change
  it.
- **Mechanism.** Observation is the day-one default — the write floor comes up when `JOE_MODE`
  is unset (`env.ResolveBootMode` + `ResolveWriteFloor`, D-0073).
- **Pinning tests.** `TestResolveWriteFloor_Precedence`, `TestResolveBootMode`.
- **Binding note. Launch-bound** in the same sense as the Safety deep-dive write-floor entry:
  the "observation is the only posture Joe boots into" framing revises when full mode lands.

---

## Configuration — `/configuration/`

### SQLite is the supported store; the `pgx` (PostgreSQL) value exists but is not operational

- **Claim.** SQLite is the supported database and the only functional driver; a `pgx`
  (PostgreSQL) value is present in the configuration surface — the store opens the configured
  driver, the repositories are dialect-aware, and the migration runner has a PostgreSQL branch
  — but it is **not yet operational**: setting `database.driver: "pgx"` fails at startup during
  the migration step, before the server serves, because the embedded migration set is written
  in SQLite dialect only (`AUTOINCREMENT`, SQLite-specific append-only trigger DDL).
- **Mechanism.** Driver-parameterized store construction (`store.New` → `sql.Open` on the
  configured driver) plus the migration runner's driver branch (`Store.Migrate`,
  `migratePostgres.WithInstance` vs `migrateSQLite.WithInstance`, `internal/store/store.go`),
  standing over a **SQLite-dialect-locked embedded migration set** — the gap between the wired
  driver seam and the un-portable SQL is exactly what makes the `pgx` value latent.
- **Pinning tests.** `TestMigration009_SchemaSQLite` (the SQLite half). The Postgres half,
  `TestMigration009_SchemaPostgres`, is **env-gated on `JOE_TEST_POSTGRES_DSN` and skips when
  unset**, so it does not run in CI and covers only migration 009's schema, not the full
  chain — there is no continuously-run guard proving the whole migration set is Postgres-valid
  (indeed it is not). Following the MCP-entry precedent, the latency is recorded here rather
  than claimed as guarded.
- **Binding note. Mechanism-bound.** The copy revises when
  `docs/backlog/postgres-backend-completion.md` lands — dialect-portable migration rewrites,
  PostgreSQL-native append-only enforcement, driver-value validation, and an un-gated CI
  Postgres migration run — at which point the "present but not operational" framing and the
  skipped-test posture both change.

---

## Operations — `/operations/`

### The session retention sweeper runs on by default, with trash-grace-purge on and inactivity-expiry off

- **Claim.** A background retention sweeper runs from boot (on by default, not opt-in),
  applying the single install-wide session policy on a fixed interval. Trash-grace auto-purge
  is **on by default** (a trashed session is stamped a purge deadline of trashed-time plus the
  trash-grace window and hard-purged past it, its transcript removed by cascade); inactivity
  expiry is **off by default**; the terminal action is **trash-then-purge (default) or
  archive** (archive moves the session to the archive directory as a versioned artifact, and
  with no archive directory wired it leaves the session active and logs rather than falsely
  archiving); every sweep effect is written to the audit log in the same transaction. A
  **zero** trash-grace (or an unresolvable policy at trash time) leaves the session with no
  purge deadline, so it persists in trash indefinitely until an admin purges it by hand —
  intended "zero grace = never auto-purge" semantics.
- **Mechanism.** The seeded single-row retention policy from migration 026 (`inactivity_days`
  NULL/OFF, `trash_grace_days` 30, `terminal_action` `trash_then_purge`) plus the boot-started
  sweeper (`internal/sessionsweeper`, wired in `cmd/joe/server.go` under the
  `svc:sweeper:sessions` principal); the `chat_messages` `ON DELETE CASCADE`; the
  `purge_after IS NOT NULL` selection predicate that implements the zero-grace footgun.
- **Pinning tests.** The **seed defaults** are pinned by
  `TestMigration026_027_RetentionAndAuditKind` (asserts inactivity NULL/OFF, trash-grace 30,
  terminal `trash_then_purge`, single-row CHECK). The **sweeper behaviors** are pinned by the
  `TestSweep_*` family — `TestSweep_InactivityOffByDefault`, `TestSweep_TrashGracePurge`,
  `TestSweep_InactivityTrashThenPurge`, `TestSweep_ArchiveTerminalNoProvider`,
  `TestSweep_ArchiveTerminalLive`, `TestSweep_LoginFlowDrainIsDistinct`,
  `TestSweep_NeverTouchesLegacyTables`, `TestSweep_AuditFailureRollsBack`. The **1h default
  interval cadence** is a code default (`internal/sessionsweeper/sweeper.go`) exercised on a
  fixed clock by the behavior tests, not directly asserted by its own test — **none yet** for
  the cadence value itself.
- **Binding note. Mechanism-bound.** The copy revises if the seed defaults or the sweep cadence
  change, or when retention v2 lands (`docs/backlog/db-retention-story.md`).

### The audit log grows unbounded by design — append-only, no rotation in v1

- **Claim.** The audit log grows without bound by design: it is append-only (inserts only,
  enforced in application code and by database triggers rejecting update/delete), so there is
  deliberately no rotation or pruning in this version; the only sanctioned space-reclamation is
  dropping and recreating the table wholesale, discarding the entire history.
- **Mechanism.** The insert-only audit repository surface (`internal/audit`) plus migration-015
  SQLite triggers blocking UPDATE/DELETE on the audit table; the absence of any production
  delete path.
- **Pinning tests.** `TestRepositoryAPISurface_AppendOnly`, `TestMigration015_TriggerBlocksUpdate`,
  `TestMigration015_TriggerBlocksDelete`.
- **Binding note. Mechanism-bound.** The "no rotation in v1" framing revises when audit
  rotation v2 (the insert-rotate-only repository extension named by D-0009) lands, tracked in
  `docs/backlog/db-retention-story.md`.

### Several other tables grow unbounded with no prune path; the graph self-reconciles and the legacy session tables are frozen

- **Claim.** Beyond the audit log, several table classes have no automatic deletion path in
  this version and grow monotonically with use — per-model-call usage records, code-review
  jobs, and clarifications accumulate with no prune path. The legacy session/messages tables
  are frozen: nothing writes to them, they have no deletion path, and they are deliberately
  retained for a future feature. The infrastructure graph is **not** in this class — its edges
  are added and removed as Joe reconciles the live topology, so it does not grow without bound.
- **Mechanism.** Absence of any production delete-site for the unbounded tables; the frozen
  legacy store (`internal/store/sessions.go`, its inserts unreachable by any live caller, one
  dormant reader); the graph reconciler adding and removing edges. Described structurally, per
  D-0032 — the set of unbounded tables is named by class, not pinned to a fixed count.
- **Pinning tests.** None (a growth posture / absence-of-delete-path property, not a single
  break-test; the audit half of it carries the append-only guards above).
- **Binding note. Mechanism-bound.** Revises when any of the deferred retention work lands — an
  `llm_usage` retention/roll-up, a review-jobs/clarifications disposition, or a DB-size
  operator signal (`docs/backlog/db-retention-story.md`) — or if the legacy-table disposition
  changes (`learn-from-sessions-fate`).

---

## Install and Build — `/install-and-build/`

### Distribution posture: build-from-source today, release pipeline armed to publish on tag

- **Claim.** Joe is distributed build-from-source only today — no published release binaries
  exist. The release pipeline is **armed to publish**: a `v`-prefixed tag push triggers a
  dedicated release workflow that runs `goreleaser release --clean`, publishing a GitHub Release
  with the built archives and a checksums file. Until an operator pushes such a tag, no release
  exists and building from source is the only way to obtain `joe`.
- **Mechanism.** `.goreleaser.yaml` (`release.disable` unset/false; `before.hooks` stage the real
  web UI via `scripts/stage-ui-for-release.sh` — the same source/dest the Makefile's `build-ui`
  target uses — before every goreleaser build or release, so a published binary cannot ship the
  placeholder embed); the tag-triggered `.github/workflows/release.yml` (`contents: write`,
  triggers only on `refs/tags/v*`); the unchanged `goreleaser-build` snapshot job in
  `.github/workflows/tests.yml`, which validates the same config on every push/PR and never
  publishes.
- **Pinning tests.** None (no Go test guards this; the CI snapshot job's `ui_digest`-based
  verification step is a CI-level guard proving the staged UI is real, not a named Go test).
- **Binding note. Launch-bound.** The "no published release binaries" half of this claim is
  expected to flip the moment the `v0.1.0` tag is pushed and cut (`release-pipeline-02`,
  `docs/backlog/release-pipeline.md`) — revise this entry and the Install and Build page's
  copy at that point; reaching that trigger is the cue to revise, not drift.

---

## Guides — `/guides/register-kubernetes/`

### The graph records only secret key names and object metadata, never secret values

- **Claim.** Even when Joe is granted `list` on secrets, the graph records only each secret's
  **key names** and object metadata (name, namespace, labels) — it **never** records secret
  values.
- **Mechanism.** The kubernetes metadata builder extracts only the data map's KEY NAMES for a
  secret node (`buildK8sMetadata` in `internal/coreagent/k8s_refresh.go`, secret case →
  `data_keys` via `mapKeys`); the value bytes are never copied into the node, and no `data` map
  is stored.
- **Pinning tests.** `TestSecretNodeMetadataOnlyKeyNames`.

### Joe works with the built-in `view` role; missing list permission degrades, it does not fail

- **Claim.** Joe works with Kubernetes' built-in read-only `view` ClusterRole. With `view` the
  graph populates fully except secret nodes (which `view` excludes), and the component shows a
  **degraded** status naming what was skipped rather than failing the refresh. Granting `list`
  on secrets completes the graph. The secrets grant is an explicit opt-in, not a requirement.
- **Mechanism.** The kubernetes refresher degrades per-resource-type on a **forbidden** list
  error only — it records a skip and continues instead of aborting, then writes status
  `degraded` with a summary; a non-forbidden error still aborts (`refreshK8sComponent` +
  `summarizeSkips` in `internal/coreagent/k8s_refresh.go`, forbidden detection via the
  apimachinery typed-error helper through error unwrapping; the third `degraded` status written
  via `store.UpdateSyncState`). D-0093.
- **Pinning tests.** `TestRefreshK8sComponent_ForbiddenSkipsAndContinues`,
  `TestRefreshK8sComponent_NonForbiddenAborts`,
  `TestRefreshComponent_DegradedStatusWrittenAndCleared`, `TestRefreshCRDSpec_ForbiddenVsMissing`.
- **Binding note.** The specific excluded-by-`view` set is described as a delta from the `view`
  role, not enumerated (D-0032); the copy points at `view` rather than listing resource types.
