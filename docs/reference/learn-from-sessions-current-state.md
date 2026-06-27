# Current state — the learn-from-sessions (knowledge extraction) feature

Status: **dormant / orphaned.** This record is the ground-truth current-state
reference for the learn-from-sessions feature. It supersedes the stale "shipped"
claims reconciled in the B001-LEARNING docs pass (see "Stale claims this record
supersedes" at the end). Every claim below was re-derived from the live tree on
2026-06-20; citations are `file:line` against the working tree at that time.

## What the feature is

A single extractor that reads a completed session transcript, asks the LLM to
distil reusable "learnings," and persists each as a knowledge entry.

- Lives entirely in `internal/knowledge/learning/extractor.go` (the package's
  only non-test file; `extractor_test.go` is the only other file in the
  directory).
- Type `Extractor` and constructor `learning.New` —
  [extractor.go:22](../../internal/knowledge/learning/extractor.go) (struct),
  [extractor.go:30](../../internal/knowledge/learning/extractor.go) (`New`).
- Entry point `Extractor.ExtractFromSession(ctx, sessionID)` —
  [extractor.go:43](../../internal/knowledge/learning/extractor.go).

## Verified facts

### 1. It is orphaned — nothing constructs or calls it

- `Extractor.ExtractFromSession` and `learning.New` have **no non-test caller.**
  A tree-wide search finds `ExtractFromSession` only in
  `internal/knowledge/learning/extractor_test.go` and its own definition; and
  `learning.New` only at its definition site
  ([extractor.go:30](../../internal/knowledge/learning/extractor.go)) — no
  production call site at all.
- No HTTP route, subcommand, background ticker, signal handler, agentloop hook,
  or core-agent path reaches it. Searches for `learning` / `Extractor` /
  `ExtractFrom` across `internal/config/`, `cmd/`, `internal/core/`,
  `internal/agentloop/`, and `internal/coreagent/` return nothing.
- `core.Services` has no `Extractor` field —
  [internal/core/services.go:42](../../internal/core/services.go) (`type
  Services struct`) contains no learning/extractor member.
- `cmd/joe/server.go` never constructs it — no `learning` import or `learning.New`
  call anywhere in the server entrypoint.

Net: the package compiles and its tests pass, but no running code path can ever
invoke it.

### 2. It reads the legacy session store, not the live one

- `ExtractFromSession` loads messages via `e.store.Sessions.GetMessages` —
  [extractor.go:45](../../internal/knowledge/learning/extractor.go).
- That method is `sqlSessionRepository.GetMessages`
  ([internal/store/sessions.go:106](../../internal/store/sessions.go)), which
  `SELECT ... FROM session_messages`
  ([sessions.go:112](../../internal/store/sessions.go)).
- `session_messages` is the **legacy** table from migration 001 —
  [001_initial.up.sql:29](../../internal/store/migrations/001_initial.up.sql)
  (`CREATE TABLE session_messages`).
- The **live** conversation store is `agent_sessions` (migration 009,
  [009_session_model.up.sql:17](../../internal/store/migrations/009_session_model.up.sql))
  plus the interim `chat_messages` table (migration 022,
  [022_chat_sessions.up.sql:28](../../internal/store/migrations/022_chat_sessions.up.sql)).
- Consequence: even if it were wired, the extractor would never see a live
  conversation — those are written to `chat_messages`/`agent_sessions`, never to
  `session_messages`.

### 3. Its write path is ungoverned

It persists each learning with `e.svc.UpsertSynced` —
[extractor.go:85](../../internal/knowledge/learning/extractor.go), defined at
[internal/knowledge/service.go:112](../../internal/knowledge/service.go).

- `UpsertSynced(ctx, e *Entry)` threads **no principal** — there is no principal
  argument in the signature.
- It consults **no accessor or RBAC seam** — the method body
  ([service.go:112-146](../../internal/knowledge/service.go)) hashes content,
  looks up by source, and calls the repository directly.
- It writes **no audit row** — neither `service.go` nor the extractor emits an
  audit record for the write.
- `CreatedBy` is **left unset for new entries.** It is copied only on the
  *update* branch ([service.go:126](../../internal/knowledge/service.go)); the
  new-entry branch ([service.go:135-145](../../internal/knowledge/service.go))
  never sets it, and the extractor itself does not populate `entry.CreatedBy`
  ([extractor.go:72-79](../../internal/knowledge/learning/extractor.go)).

A knowledge entry is a **Mutate**-class write in the binary Read/Mutate model.
Here that Mutate is performed with no principal and no audit.

### 4. Internal tier contradiction

- The package doc and the struct doc both say the entries are **Tier 3
  (derived)**:
  [extractor.go:1-2](../../internal/knowledge/learning/extractor.go) ("stores
  them as Tier 3 (derived) knowledge entries") and
  [extractor.go:21](../../internal/knowledge/learning/extractor.go) ("derives
  Tier 3 knowledge entries").
- The extractor sets `Tier: knowledge.TierDerived` on the entry —
  [extractor.go:73](../../internal/knowledge/learning/extractor.go). `TierDerived`
  is the Tier-3 / lowest-trust knowledge tier
  ([knowledge.go:16-17](../../internal/knowledge/knowledge.go)).
- But `UpsertSynced` **overwrites** it to Tier 2: its first statement is
  `e.Tier = TierSynced`
  ([service.go:113](../../internal/knowledge/service.go)), where `TierSynced` is
  the Tier-2 / external-source tier
  ([knowledge.go:14-15](../../internal/knowledge/knowledge.go)).
- So the stored tier is always `synced` (Tier 2), contradicting the code's own
  stated intent of `derived` (Tier 3). The existing test acknowledges this:
  [extractor_test.go:119](../../internal/knowledge/learning/extractor_test.go)
  ("uses UpsertSynced which forces Tier = TierSynced").

> Note: these are **knowledge** tiers (`curated`/`synced`/`derived`,
> [knowledge.go:12-17](../../internal/knowledge/knowledge.go)), a distinct concept
> from the deleted T1/T2/T3 *safety* model. See
> [JOE_PROJECT_KNOWLEDGE.md](../../JOE_PROJECT_KNOWLEDGE.md) §10 item 4.

### 5. No configuration governs it

No config field, feature flag, or env var enables, disables, or tunes the
feature — a search across `internal/config/` and the rest of the tree finds no
reference to the extractor. Because nothing instantiates it (fact 1), it is
effectively **always-off**: there is no switch to turn it on.

### 6. Mechanism — for reference if the feature is revived

(Behaviour as written, not a recommendation.)

- One LLM call per session, system prompt `prompts.ExtractionSystem`
  ([extractor.go:108-110](../../internal/knowledge/learning/extractor.go);
  prompt at
  [internal/prompts/knowledge.go:24](../../internal/prompts/knowledge.go)),
  `MaxTokens: 2048`.
- The LLM is expected to return a **JSON array of learning objects** (`type`,
  `title`, `description`, `related_nodes`, `confidence`); the response is parsed
  after stripping ` ```json ` fences
  ([extractor.go:106-134](../../internal/knowledge/learning/extractor.go)).
- One `knowledge.Entry` is created per learning
  ([extractor.go:71-84](../../internal/knowledge/learning/extractor.go)).
- Dedup key is `source_type=session`
  ([extractor.go:77](../../internal/knowledge/learning/extractor.go),
  `SourceTypeSession` =
  [knowledge.go:39](../../internal/knowledge/knowledge.go)) plus a `SourceID` of
  `sessionID` + `"/"` + a sanitized title
  ([extractor.go:78](../../internal/knowledge/learning/extractor.go);
  `sanitizeTitle` lowercases, replaces separators, truncates to 80 chars).
- Tool-role messages are skipped from the transcript: `buildTranscript` drops any
  message where `Role == "tool"` or `ToolName != ""`
  ([extractor.go:140-141](../../internal/knowledge/learning/extractor.go)).

## Not determinable from the tree

Two questions cannot be answered by reading the code; they need product/history
context:

1. **Was it ever wired, or never wired?** The tree shows it is *currently*
   orphaned, but cannot tell us whether a call site (route, ticker, hook) once
   existed and was removed, versus the extractor having been written and never
   connected. Git archaeology or design history would be needed to decide.
2. **What tier was intended?** The code disagrees with itself (fact 4): the
   comments and the extractor say `derived` (Tier 3); `UpsertSynced` forces
   `synced` (Tier 2). Which one reflects the design intent is a decision, not a
   fact in the tree.

## Decided fate

The feature's fate is **decided — deferred to a future feature, not retired** —
recorded in
[docs/backlog/learn-from-sessions-fate.md](../backlog/learn-from-sessions-fate.md).
When the future feature is built, the extractor will be rewired (repointed at the
live `agent_sessions`/`chat_messages` store) and governed (real principal, audit
row, resolved tier, explicit invocation path, config switch defaulting off)
rather than deleted.

Because those legacy tables are this feature's only data source, retaining them is
now a **hard constraint** on the B001 sessions consolidation: the legacy
`sessions` / `session_messages` tables **must NOT be dropped** until the rewire
repoints the extractor at the live store, or until the feature is explicitly
re-killed. This replaces the earlier framing in which the fate was an open
decision coupled to those tables' retirement.

## Stale claims this record supersedes

The following older statements describe the feature as a shipped, working
capability; they are stale and are corrected to point here:

- The completed-milestones log, Phase 7.4 / 7.5 (LLM-derived insights "DONE",
  "session learning wired").
- `docs/reference/joe-architecture.md` KNOWLEDGE TIERS Tier 3 ("Patterns extracted from
  sessions") and the Phase 7 checklist ("LLM-derived insights from sessions").
- Any memory/chat asserting the learn-from-sessions feature is complete (see
  `JOE_PROJECT_KNOWLEDGE.md` §10).
