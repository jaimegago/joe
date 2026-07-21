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

### Every registered Mutate is denied unconditionally — the act opt-in reaches no tool

- **Claim.** The per-action operator opt-in is a structurally intact seam that is currently
  reachable by **no registered tool**, so every registered mutating tool is denied regardless
  of how the safety policy is configured — not merely "denied until an operator opts in."
- **Mechanism.** Disjointness between two hardcoded sets: `IsT3Allowed`
  (`internal/safety/policy.go`) switches on exactly `k8s_write`, `pagerduty_ack`,
  `alertmanager_silence`, `git_push` and otherwise returns its `default: false`; the three
  `ActionMutate` rows in `internal/safety/tier.go` declare `github_comment`, `gitlab_comment`,
  `github_request_changes`. No key is in both sets, so `CheckAccess`'s policy-allows branch is
  unreachable by any real tool name. `publish_doc_update_git` under `git_push` was the last
  tool reaching it and was deleted with the knowledge store (D-0113).
- **Pinning tests.** `TestRegisteredMutatesAreUngrantable` — derives the Mutate set from
  `toolRegistry` itself and asserts `IsT3Allowed` returns false for every such row's
  `PolicyKey`, and that `CheckAccess` denies it, under a policy with **every** `act` toggle
  enabled. It fails the moment a registered Mutate becomes grantable, which is the event that
  obligates a revision of this claim. `TestCheckAccess_MutateDefaultDeny` remains, pinning the
  weaker denial-under-`DefaultPolicy()` claim. The seam's future is tracked in
  `docs/backlog/act-policy-vestigial.md`; the residual thread in
  `docs/backlog/security-authority-claims.md`.

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
- **Mechanism.** Separated engine construction at the composition root — the transport policy
  engine (`NewPolicyEngineWithGovernance`, built in `cmd/joe/server.go`'s `buildHTTPHandler` and
  injected into `api.New`) carries the read-posture resolver; the refresh engine
  (`NewPolicyEngineWithPromote`, posture seam nil) never does; the autonomous read surface is
  governed by `auto_promote_read` plus grants. rbac-engine-split moved both constructions to the
  composition root and forbids engine construction elsewhere, so the two engines cannot share
  state by accident.
- **Pinning tests.** `TestReadPosture_AxisSeparation_RefreshEngineIgnoresPosture` (the refresh
  engine ignores the posture) and `TestGuard_PolicyEngineConstructedOnlyAtCompositionRoot` (no
  `rbac.NewPolicyEngine*` construction outside `cmd/joe`/tests).

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
- **Binding note.** The claim is scoped to the **running daemon's request paths**: every
  authenticated caller is gated. It has never covered an operator with filesystem access to
  the database, who can already replace the whole store (`joe db restore`). The offline
  first-admin CLI in *The set of admin-mint paths is closed* below is a writer of that same
  class — narrower and audited — and does not widen this claim; it is cross-referenced here
  so a future reader does not mistake it for a gap in the request-path guarantee.

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

### Demo clips — what the three recorded clips assert

The "What Joe does" section carries three screen-capture clips recorded against a live kind
cluster staged from `examples/demo-world/`. Their claims are unlike every other entry in this
register: the **mechanism is recorded footage**, not code, so no Go test can pin them and no
repository change can break them. What breaks them is **re-recording**. Each entry below therefore
states its invalidation trigger — the condition under which the footage and the copy stop agreeing
— in place of a pinning test.

The general rule: a caption may assert only what the footage actually shows. Where a fact is
visible in frame, the caption does not restate it; where it is not visible, the caption carries it
and becomes load-bearing.

#### The feature-chat clip and caption assert the recorded run was on `gemini-2.5-flash`

- **Claim.** The chat-and-streamed-agentic-loop clip was recorded on `gemini-2.5-flash`, named in
  the caption as "a budget-tier model" — a deliberate claim that Joe's agentic loop reaches a real
  diagnosis without a frontier model.
- **Mechanism.** The recorded footage of `static/media/feature-chat.{mp4,webm}` and the caption on
  the first feature row of `content/_index.md`. The model is **not** frame-visible; the caption is
  the sole carrier of the claim, which is what makes it load-bearing.
- **Pinning tests.** None possible — see the section preamble.
- **Binding note. Recording-bound.** Invalidated if the clip is re-recorded on a different model;
  the caption must change with it. The budget-tier framing is the point of the claim, so
  re-recording on a frontier model does not merely require a name swap — it removes the claim.

#### The feature-graph clip and caption assert the recorded diagnosis ran on `gemini-2.5-flash`

- **Claim.** The clip shows Joe's live map of the infrastructure and a proposed fix held behind the
  write floor, and the caption asserts the recorded diagnosis ran on `gemini-2.5-flash`, "a
  budget-tier model".
- **Mechanism.** The recorded footage of `static/media/feature-graph.{mp4,webm}` and the caption on
  the second feature row of `content/_index.md`. As with feature-chat, the model is carried by the
  caption rather than shown in frame. The write-floor half of the caption rests on the shipped
  mechanism recorded in the Safety deep-dive entries above; only the model half is footage-bound.
- **Pinning tests.** None possible for the footage half. The write-floor half inherits
  `TestResolveWriteFloor_Precedence` and the executor gate-order tests via the Safety entries.
- **Binding note. Recording-bound.** Invalidated if the clip is re-recorded on a different model;
  the caption must change with it.

#### The feature-mcp clip asserts a Claude Code session answered from Joe-served live prod state over MCP

- **Claim.** The recorded Claude Code session reads a diff, queries Joe over MCP, and answers from
  live prod state — the evidence path runs through Joe rather than through the agent's own cluster
  access.
- **Mechanism.** The recorded footage of `static/media/feature-mcp.{mp4,webm}` and the caption on
  the third feature row of `content/_index.md`. **The model (`claude-opus-4-8`) is frame-visible in
  the footage rather than claimed in the caption** — the inverse of the two clips above, and the
  reason this caption names no model. The claim the caption does carry is the **grounding path**:
  that the answer came from Joe-served state over MCP.
- **Pinning tests.** None possible for the footage. The underlying server-only MCP posture is
  covered by the Safety deep-dive entry "Joe speaks MCP in one direction only", which records that
  it too is not yet test-pinned.
- **Binding note. Recording-bound.** Invalidated if the clip is re-recorded such that either the
  frame-visible model or the Joe-grounded evidence path changes. Because the model here is shown
  rather than stated, a re-record on a different model invalidates the footage-to-claim mapping in
  this register even though the caption text would need no edit — which is precisely why it is
  recorded here.

#### Body-copy note — the third beat was reworded to match recorded footage

- The third feature row's body copy was reworded from "check the change against Joe's **live
  infrastructure graph**" to "check the change against Joe's **live view of prod**", and its
  caption from "queries Joe's live graph over MCP, and answers from prod state" to "queries Joe
  over MCP, and answers from live prod state". The recorded session reaches live prod state through
  Joe's MCP tool surface generally, not specifically through a rendered graph, so the prior wording
  asserted more than the footage shows. Recorded here because the change was made **to match
  footage**, which makes it a recording-bound editorial constraint rather than free copy: a future
  copy pass that restores "infrastructure graph" would re-introduce the mismatch.

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

### The database path and the encryption key path are both configurable, their documented defaults are resolved locations, and `~` is not expanded for either

- **Claim.** `database.dsn` (or `JOE_DATABASE_DSN`) relocates the database file, and
  `database.encryption_key_path` (or `JOE_DATABASE_ENCRYPTION_KEY_PATH`) relocates the
  component-config encryption key. The documented defaults are **resolved locations** —
  `joe.db` and `encryption.key` in the `.joe` directory under the home directory of the account
  running `joe` — not literal strings to paste. An explicit value of **either** must be an
  absolute path (or one relative to the working directory): Joe does **not** expand a leading
  `~` for either key, so `~/.joe/joe.db` is taken literally and creates a directory named `~`.
  Relocating the database no longer strands the key: the two are relocated by the same
  mechanism and are expected to move together. Restated on `/operations/persistence-and-backup/`.
- **Mechanism.** `config.Load` assigns `cfg.Database.DSN` and `cfg.Database.EncryptionKeyPath`
  **verbatim** from YAML and from their `JOE_DATABASE_*` overrides, with no expansion step;
  `paths.ExpandPath` (which *does* expand `~`) is applied only to the **config-file path**,
  never to either of these. The DSN default is computed by `paths.DatabasePath` →
  `paths.JoeDirPath` → `getSecureHomeDir`, which deliberately bypasses `$HOME`. The key path is
  resolved by the single helper `encryptionKeyPathFor` (`cmd/joe/db.go`) — configured value if
  set, else `paths.EncryptionKeyPath` — and **every** consumer goes through it: the daemon's
  boot key load (`cmd/joe/server.go`) and `joe db restore`'s missing-key pre-flight, via
  `resolveEncryptionKeyPath` on the `runDeps` seam. One resolver is what keeps the command that
  warns about a missing key and the process that would mint one naming the same file.
- **Pinning tests.** The key-path half is **guarded**: `TestEncryptionKeyPath_EnvOverride`,
  `TestEncryptionKeyPath_UnsetLeavesEmpty`, `TestEncryptionKeyPath_ParsesFromYAML`
  (`internal/config`), and `TestEncryptionKeyPathFor_ConfiguredWins` /
  `TestEncryptionKeyPathFor_DefaultsToJoeDir` (`cmd/joe`). The **no-`~`-expansion** property is
  still **unguarded for both keys**: no test asserts that a `~`-prefixed value is left
  unexpanded. It holds by the *absence* of an expansion call — a shape that a well-meant
  "expand these like we expand the config path" change would silently break, taking this copy
  with it. Recorded as unguarded rather than implied to be guarded.
- **Binding note. Mechanism-bound.** Revised by D-0120 (session `encryption-key-path`), which
  added the config key and retired the "the key does not follow the DSN" half. Revises again if
  `~` expansion is ever added — which, per the same decision, must be done to **both** keys
  together or to neither.

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
  this version and grow monotonically with use — per-model-call usage records and code-review
  jobs accumulate with no prune path. The legacy session/messages tables
  are frozen: nothing writes to them, they have no deletion path, and they are deliberately
  retained for a future feature. The infrastructure graph is **not** in this class — a live
  component's nodes and edges are added and removed as Joe reconciles its topology, and when a
  component is deleted its graph rows are removed with it, so the graph does not grow without
  bound.
- **Mechanism.** Absence of any production delete-site for the unbounded tables; the frozen
  legacy store (`internal/store/sessions.go`, its inserts unreachable by any live caller, one
  dormant reader); for the graph, two paths together — the per-component delta reconciler
  adding and removing a live component's nodes and edges, **and** the component-delete cascade
  (D-0117) removing a deleted component's `graph_nodes` in the same transaction as the
  `components` row, with `graph_edges` following by FK `ON DELETE CASCADE`. Described
  structurally, per D-0032 — the set of unbounded tables is named by class, not pinned to a
  fixed count. (Before D-0117 the reconciler alone did not bound the graph: it is
  per-component, and a deleted component never refreshes again, so its rows were orphaned
  permanently — the cascade closes that leak.)
- **Pinning tests.** `TestDeleteComponent_CascadesGraphState` and
  `TestDeleteComponent_CascadeRollback` (the delete-path cascade and its transactionality) and
  `TestMigration032_PruneOrphanedGraphNodes` (the one-time backfill of pre-existing orphans).
  The unbounded-tables and frozen-legacy halves remain a growth-posture / absence-of-delete
  property with no single break-test (the audit half carries the append-only guards above).
- **Binding note. Mechanism-bound.** Revises when any of the deferred retention work lands — an
  `llm_usage` retention/roll-up, a review-jobs disposition, or a DB-size
  operator signal (`docs/backlog/db-retention-story.md`) — or if the legacy-table disposition
  changes (`learn-from-sessions-fate`).

### The store runs in WAL journal mode, on every pooled connection — `/operations/persistence-and-backup/`

- **Claim.** Joe's SQLite store runs in WAL (write-ahead logging) mode, so committed data can
  live in the `-wal` sidecar rather than the main database file until a checkpoint folds it in.
  This is the load-bearing premise under both the backup command's existence and the
  copy-from-a-running-Joe warning below.
- **Mechanism.** `sqlitePragmas` in `internal/store/store.go` carries `journal_mode(WAL)` (with
  `busy_timeout(5000)` and `foreign_keys(1)`), encoded into the DSN by `withSQLitePragmas` so
  the driver applies it to **every** connection the pool opens rather than to one arbitrary
  pooled connection.
- **Pinning tests.** `TestWithSQLitePragmas`, `TestNew_SQLitePragmasOnEveryPooledConnection`.
- **Binding note.** Stable. WAL is not a tuning choice the copy hedges on; if the journal mode
  ever changed, the backup page's entire rationale section revises with it.

### `joe db backup` is safe against a live Joe — `/operations/persistence-and-backup/`

- **Claim.** `joe db backup DEST` takes a consistent copy of committed data while Joe is
  running, leaves the source untouched (including its schema version), and produces a
  standalone file.
- **Mechanism.** `runDBBackup` (`cmd/joe/db.go`) executes SQLite's `VACUUM INTO` with the
  destination **bound as a parameter**, over a second independent open of the database file;
  concurrent access is safe by the same WAL-plus-`busy_timeout` property D-0018 established for
  `joe unlock`'s second open, generalized from a single row to the whole file. The command
  deliberately does **not** run migrations, so the source's schema version is not altered by a
  command that promises a copy. An up-front `os.Stat` refuses an occupied destination ahead of
  any SQL — the engine's own guard accepts a 0- or 1-byte destination as a fresh database and
  silently overwrites it.
- **Pinning tests.** `TestRunDBBackup_LiveDatabaseWithConcurrentWriter` (the live-safety test:
  a real file-backed store on joe's own pragma path, populated, held open by a first handle with
  an in-flight write transaction, backed up through the real command path via a second
  independent open; asserts the destination opens standalone, holds the committed rows, excludes
  the uncommitted one, and passes `integrity_check`). Error paths:
  `TestRunDBBackup_OccupiedDestRefused`, `TestRunDBBackup_ZeroByteDestRefused`,
  `TestRunDBBackup_ForceOverwritesExisting` (which also pins the bound-not-interpolated
  destination), `TestRunDBBackup_MissingParentDirectory`, `TestRunDBBackup_NonSQLiteDriverRefused`.
- **Binding note. Mechanism-bound.** The SQLite-only framing revises if the `pgx` driver ever
  becomes operational — `VACUUM INTO` has no cross-engine equivalent, and the command refuses a
  non-SQLite driver rather than emit something that is not a backup.

### Copying the database file from a running Joe can yield a backup missing recent or all data — `/operations/persistence-and-backup/`

- **Claim.** Copying `joe.db` alone out from under a running Joe can produce a valid SQLite file
  that opens without error and is missing recent data — or every table — because the committed
  rows are still in the uncopied `-wal` sidecar. Stop-then-copy is the safe dependency-free
  alternative: a clean shutdown checkpoints and removes the sidecars, leaving one complete file.
- **Mechanism.** No guard of its own — this is an **operator-procedure warning that rests
  entirely on the WAL claim above** and inherits its pinning tests. Nothing in the binary
  prevents an operator from running `cp`; the claim describes a consequence of WAL, and the
  binary's contribution is offering `joe db backup` as the alternative.
- **Pinning tests.** None specific to this warning. It inherits `TestWithSQLitePragmas` and
  `TestNew_SQLitePragmasOnEveryPooledConnection` via the WAL claim it rests on — stated here
  rather than implied, so the register does not suggest a guard that does not exist.
- **Binding note.** Revises with the WAL claim; it has no independent mechanism.

### The encryption key is required for a usable restore, and boot now fails closed without it — `/operations/persistence-and-backup/`

- **Claim.** A restored database is not a restored Joe without the matching `encryption.key`,
  and **Joe refuses to boot rather than starting without it**. Two rules, both fatal:
  a key file that is **absent** while the database holds encrypted component config is treated
  as a lost key, not a first run — Joe will not mint a replacement over it; and a key that is
  **present but cannot authenticate** any stored component config fails boot too, naming every
  component that failed. There is still no recovery, no key rotation, and no re-encrypt path —
  the change is that the loss is now loud and immediate instead of silent and permanent.
  A genuine first run (no key, nothing encrypted) still generates a key and boots normally.
- **Mechanism.** `crypto.LoadOrCreateKey` (`internal/crypto/crypto.go`) takes an
  `EncryptedDataProbe` and its generate-on-absence branch is **conditional** on that probe
  reporting no encrypted data; the composition root supplies it from
  `store.ScanComponentConfigs`, which counts rows whose config carries the `enc:` marker
  (unmarshal-then-test, mirroring the production read path). A nil probe and a probe error both
  refuse — fail-closed, since minting is the irreversible direction. Rule B is
  `EncryptedComponentRepository.VerifyConfigs` (`internal/store/encrypted_components.go`), run
  once at boot immediately after wiring: it decrypts every stored config, collects **all**
  authentication failures rather than stopping at the first, and returns them joined. The
  authentication class is a typed seam — `crypto.ErrAuthentication` wrapped by
  `store.ConfigAuthError{ComponentID}` — so the boot path branches on the class rather than on
  message text, and only that class refuses; transient store errors keep their prior posture.
  The `component credential encryption enabled` log now prints **after** verification, so it
  asserts something proven rather than something assumed.
- **Pinning tests.** **Guarded on both rules, including the two gaps this register previously
  recorded as missing.** Rule A: `TestLoadOrCreateKey_AbsentKeyWithEncryptedDataRefuses` (which
  also asserts no key file is written on the way out — a key minted during the refusal would
  turn the next boot into the very disaster it prevented),
  `TestLoadOrCreateKey_ProbeNotConsultedWhenKeyPresent`,
  `TestLoadOrCreateKey_ProbeErrorFailsClosed`, `TestLoadOrCreateKey_NilProbeRefused`, and
  `TestScanComponentConfigs_PlaintextRowsAreNotEncryptedData` (the boundary: rows are not
  encrypted data, so a backward-compat plaintext install is still a first run). Rule B:
  `TestVerifyConfigs_WrongKeyFailsEveryComponent` — **this is the "wrong key fails to decrypt a
  component through the repository" test the register recorded as absent** — plus
  `TestVerifyConfigs_NamesOnlyTheDamagedRow` and `TestVerifyConfigs_PassesWhenKeyMatches`. The
  typed seam is pinned by `TestDecrypt_WrongKeyIsAuthenticationClass`, which also asserts a
  base64 decode failure does **not** carry the sentinel. The end-to-end boot behaviour was
  verified against the **real binary** in session `encryption-key-path` (relocated state dir,
  seeded encrypted rows, key deleted then replaced with a random one: exit 1 both times, no key
  minted, both components enumerated) but is **not** pinned by an automated test — that half
  still rests on the unit guards plus a recorded manual run.
- **Binding note. Mechanism-bound.** Revised by D-0120 (session `encryption-key-path`), which
  landed both halves of `docs/backlog/encryption-key-path.md` and **invalidated the prior claim
  outright** — the published copy's "Joe boots cleanly and breaks silently" and "nothing at boot
  guards this" sentences are now false and were rewritten with this entry. Revises again if
  full mode changes the trade-off, or if an escape hatch for a deliberate start-over is added
  (deliberately not built: `joe db restore` and deleting the database are the existing paths).
- **Prior amendment, retained as trail (D-0115, session `db-persistence-backup-02`).** `joe db
  restore` refuses a backup carrying encrypted component config when no key file is present
  (overridable by `--allow-missing-key`, pinned by `TestRunDBRestore_MissingKeyGate`). That was
  a guard on the **restore route only**, and this register recorded precisely that the boot path
  carried no guard. As of D-0120 the boot path carries one, so the restore-side gate is no
  longer the only thing that checks — it is now the **earlier, better-diagnosed** of two gates.
  Note the consequence for `--allow-missing-key`: it still lets the restore proceed, but the
  resulting Joe now **refuses to boot** instead of coming up reaching nothing, which is the
  outcome that flag's warning text describes and should be reread against this entry.

### `joe db restore` pre-flights a backup and clears stale WAL sidecars — `/operations/persistence-and-backup/`

- **Claim.** `joe db restore SRC` puts a backup back at the configured database path after
  checking, before writing anything, that: the backup passes an integrity check; it is
  recognizably a Joe database; a key is present if its component config is encrypted
  (overridable by `--allow-missing-key`); and no process holds the target open. An existing
  database is replaced only under `--force`. Restoring over a database left by an unclean stop
  is safe — the stale `-wal`/`-shm` are cleared. SRC is never written to.
- **Mechanism.** `runDBRestore` (`cmd/joe/db.go`). SRC is opened through `defaultOpenSourceDB`
  with the `file:` URI and `mode=ro` — enforced at open, not a flippable pragma — and
  deliberately **not** `immutable=1`, which reads only the main file and silently ignores a
  `-wal`, misreading a WAL-carrying source as empty. The copy is `VACUUM INTO` executed from
  that read-only handle, with the destination bound; it reads through a transaction, so a SRC
  still carrying a live `-wal` is normalized into one complete file. Encrypted-config detection
  mirrors the production read path (`internal/store/encrypted_components.go`): the config column
  is JSON-unmarshalled to a string and only then tested with the exported `crypto.IsEncrypted`;
  an unmarshal failure means plaintext, not an error. SQL `LIKE` detection is not used and would
  not work — the value is JSON-quoted *and* lands with BLOB storage class despite the TEXT
  declaration. Restore never opens the target through `store.New`, which would create the very
  sidecars it removes, and runs no migrations (the D-0114 rationale).
- **Sidecar deletion — state its role precisely.** The stale-WAL substitution hazard is real and
  measured: a `-wal` left beside the target replays over a newly placed file, resurrects the
  prior database wholesale, passes `integrity_check` (the result is a coherent old database, not
  a corrupt one), then checkpoints itself into the main file and deletes its own evidence.
  **But that hazard bites the byte-copy shape — the manual procedure — not this command.** With
  the copy done by `VACUUM INTO`, the engine finds no destination file, treats it as new, and
  resets the WAL rather than recovering it, so the guarantee already holds without the deletion
  (measured both ways). The explicit deletion is **defence in depth**: the engine's reset is
  observed behaviour rather than a documented contract, and it keeps the outcome independent of
  the copy mechanism. Do not write copy claiming the deletion is what saves the operator here;
  what it does save is the **hand-restore**, which the published page now warns about at length.
- **Three-way occupancy matrix, as documented.** Sidecars absent + lock acquirable → clean
  shutdown, proceed. Sidecars present + lock BUSY → a process holds it open, refuse ("stop Joe
  first"), **no override by design** — a restore under a live daemon races writes it cannot see,
  and the daemon would still hold the pre-restore database in memory. Sidecars present + lock
  acquirable → unclean shutdown, proceed. Sidecar presence alone cannot separate the last two
  from the second; `defaultProbeTargetOccupied` is the discriminator, using a write-free
  `BEGIN IMMEDIATE` under `locking_mode=exclusive` on a short-lived connection with the pool
  **pinned to one connection** (a second pooled connection contends with the first and reports
  the caller's own probe as busy). It classifies the driver's typed error by SQLITE_BUSY code,
  never by message text.
- **The direction that holds, and the one that does not — measured end-to-end.** BUSY ⇒ someone
  holds the database: sound, and the refusal rests on it. The converse does **not** hold. What the
  probe observes is a **held connection that has actually accessed the database**, not the
  existence of a process. Measured against the real binary across processes: a holder that had
  connected *and read* was detected and the restore refused; a holder that had only **connected
  without reading** was **not** detected and the restore proceeded. A real Joe reads at boot
  (migrations, component load), so the realistic running daemon is caught — but "lock acquirable"
  is a *safety net having found nothing*, not proof that no daemon exists. The published copy
  therefore leads with "stop Joe first" and presents the check as a refusal that can fire, never
  as a guarantee that makes stopping Joe unnecessary.
- **Stated limits — published, not buried.** The probe rests on **POSIX advisory locks**: it is
  unreliable on NFS and across some container boundaries (**UNVERIFIED** — not probed, and the
  copy claims nothing there). It is **point-in-time**: a daemon starting immediately after the
  probe is not prevented. With the connected-but-unread gap above, it is a guard against the
  common mistake, not mutual exclusion.
- **Pinning tests.** `TestRunDBRestore_StaleSidecarsRemoved` (fails without the explicit
  deletion — the stale `-shm` survives `VACUUM INTO`),
  `TestRunDBRestore_UncleanStopTargetRestoresToBackupNotPriorDatabase` (the outcome guarantee;
  passes with or without the deletion today, and is held to catch the copy mechanism changing),
  `TestRunDBRestore_SrcWithLiveWALRestoresFully`, `TestRunDBRestore_SrcUnchangedByPreflight`,
  `TestRunDBRestore_MissingKeyGate`, `TestRunDBRestore_RunningDaemonRefused`,
  `TestProbeTargetOccupied_RealLock`, `TestRunDBRestore_NotAJoeDatabase`,
  `TestRunDBRestore_OccupiedTargetWithoutForce`, `TestRunDBRestore_ForceOverCleanTarget`,
  `TestRunDBRestore_SameFileRefused`.
- **Binding note. Mechanism-bound.** The SQLite-only framing revises if the `pgx` value becomes
  operational. The sidecar-deletion paragraph revises if the copy mechanism ever changes from
  `VACUUM INTO` to a byte copy — at which point the deletion stops being defensive and becomes
  the only thing preventing substitution.

### The set of admin-mint paths is closed: cold start by OIDC admin-email or the offline CLI, the admin API thereafter — `/operations/`, `/install-and-build/`, `/guides/web-ui/`, `/configuration/`

- **Claim.** Admin is minted only on a named set of paths. **Cold start** — creating the
  *first* admin, with no admin to authorize it — is either the OIDC admin-email bootstrap
  (`auth.admin_email`) or `joe admin bootstrap`, an offline command that grants admin to a
  **configured service account** on a database that has no admin yet. That command refuses
  human identities, is refused the moment any admin exists with no override flag, contacts
  no daemon, and audits the grant in the same transaction as the roster row. Every grant
  after the first goes through the admin REST surface. An install with service accounts and
  no identity provider therefore **has** a cold-start path and still has **no
  self-escalation path** — no running principal grants itself admin. The copy steers the
  operator to a **dedicated** administration account rather than the shared
  general-purpose key. The published copy further states that the CLI's `--config` mirrors
  the daemon's flag and that the one named file supplies **both** the service accounts the
  principal is checked against and the database the grant lands in, that omitting it uses
  the default `~/.joe/config.yaml`, and that naming a file that does not exist fails rather
  than falling back.
- **Mechanism.** `auth.Provisioner` (`internal/auth/provision.go`) is the sole caller of the
  repository's `AddAdmin` / `AddFirstAdmin`, reached from exactly the sanctioned writers,
  named structurally: the OIDC callback's `admin_email` bootstrap, the `requireAdmin`-gated
  `POST /api/v1/admin/admins`, and `joe admin bootstrap` (`cmd/joe/admin.go`). The one-shot
  containment rides inside the INSERT's own `NOT EXISTS` predicate in
  `rbac.SQLRepository.AddFirstAdmin` rather than a check-then-write. Service-account-only
  acceptance resolves the argument against `server.service_accounts` and mints it through
  `rbac.ServicePrincipal`, the same single point the authenticating request path uses
  (D-0129). The config the command reads is the default path unless `--config` names
  another (`runAdminBootstrap`, `cmd/joe/admin.go`); the **single** loaded config is
  threaded into the store seam (`deps.openAdminStore(cfg)` → `databaseConfigFor`), which is
  what makes the redirect coherent rather than partial, and an explicitly-named path that
  does not resolve exits 1 instead of falling back to defaults (D-0131).
- **Pinning tests.** `TestGuard_AdminPrincipalsWriterSetIsClosed` and
  `TestGuard_AdminPrincipalsHasNoRawSQLWriter` (the closed writer set, in two call-site
  layers plus a raw-SQL check — these fail if a further writer appears);
  `TestAddFirstAdmin_ConcurrentInvocationsGrantExactlyOne` (one-shot under a race);
  `TestAdminBootstrap_RefusalWhenAdminExists`,
  `TestAdminBootstrap_ContainmentAgainstRealDatabase` (the refusal, against a real
  database); `TestAdminBootstrap_RefusesHumanIdentity`,
  `TestAdminBootstrap_RefusesUnconfiguredServiceAccount`,
  `TestAdminBootstrap_GrantsConfiguredServiceAccount`;
  `TestAdminBootstrap_ExplicitConfigPathRedirectsBothUses` (the coherence claim — one named
  config governs both the service-account set and the database DSN),
  `TestAdminBootstrap_NonexistentExplicitConfigPathIsOperationalFailure`,
  `TestAdminBootstrap_AbsentFlagUsesDefaultConfigPath`.
- **Binding note. Mechanism-bound.** The copy names the mint paths as a set rather than
  counting them, so a further path landing falsifies no numeral — but it does obligate a
  revision of every page listed above, and the writer-set guard is the trip-wire: it fails
  on any new writer, and that failure is the cue to revise the site, not only the guard.
  The **no-self-escalation** clause is bound to the absence of a running-principal
  escalation route, not to the absence of a CLI; it survived the correction that added the
  CLI and revises only if such a route appears. See also *Governed by construction* on the
  landing page, whose request-path scope this entry is cross-referenced from.

---

## Install and Build — `/install-and-build/`

### Distribution posture: published release binaries plus build-from-source; archives and checksums only, no signing

- **Claim.** Joe is distributed two ways: as **published release binaries** for the platforms in
  the `.goreleaser.yaml` build matrix, and as **source** anyone can build. A release is published
  by a `v`-prefixed tag push, which triggers a dedicated release workflow running
  `goreleaser release --clean`; the resulting GitHub Release carries the built archives and a
  `checksums.txt` file. The published artifact set is **exactly that** — the pipeline does **not**
  sign archives, and ships no install script, Homebrew tap, Scoop bucket, or distribution package.
  Build-from-source remains a first-class peer path, not a fallback.
- **Mechanism.** `.goreleaser.yaml` (`release.disable` unset/false; **no `signs:` block — signing
  is unconfigured, which is what makes the "no signing" half of the claim structural rather than
  editorial**; `archives.formats: tar.gz`; `checksum.name_template: checksums.txt`;
  `builds.goos` = linux/darwin and `builds.goarch` = amd64/arm64 define the supported platform
  set; `before.hooks` stage the real
  web UI via `scripts/stage-ui-for-release.sh` — the same source/dest the Makefile's `build-ui`
  target uses — before every goreleaser build or release, so a published binary cannot ship the
  placeholder embed); the tag-triggered `.github/workflows/release.yml` (`contents: write`,
  triggers only on `refs/tags/v*`); the unchanged `goreleaser-build` snapshot job in
  `.github/workflows/tests.yml`, which validates the same config on every push/PR and never
  publishes.
- **Pinning tests.** None (no Go test guards this; the CI snapshot job's `ui_digest`-based
  verification step is a CI-level guard proving the staged UI is real, not a named Go test).
- **Binding note.** No longer launch-bound: the `release-pipeline-02` doc sweep rewrote this
  entry and the Install and Build page to the release-exists state on the same commit the
  operator tags `v0.1.0`, discharging the previous launch-bound note. The claim is now
  **mechanism-bound in two directions**: adding a `signs:` block (or any tap, bucket, installer,
  or package target) to `.goreleaser.yaml` obliges a revision of the "archives and checksums
  only, no signing" half, and changing `builds.goos`/`builds.goarch` obliges re-checking the
  supported-platform-set half. The published page states no archive filename and no restated
  platform list, so a matrix change does not silently falsify page copy (D-0032).
  **Two pages now rest on this one mechanism.** Since D-0125, `/quickstart/` Step 1 also
  states the obtain-the-binary claim — fetch the archive plus `checksums.txt` from the
  Releases asset list, verify with `sha256sum`/`shasum --ignore-missing --check
  checksums.txt`, extract — inlined rather than delegated, and it too names no archive
  filename and no platform list. A revision obliged by this entry must sweep **both**
  `/install-and-build/` and `/quickstart/`. The `checksums.txt` filename is safe to state on
  both because `checksum.name_template` configures it explicitly; the archive filenames are
  not, because `archives:` carries no `name_template`. That Quickstart *leads* with the
  download rather than the build is an editorial choice about a tutorial (D-0125) and is
  **not** part of this claim — the peer posture above is what binds, and it is unchanged.

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
