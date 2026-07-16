# Knowledge store: prune the subsystem from the tree

Status: open
Priority: now

Disposition decided: the knowledge subsystem is **deleted, not parked**. v2 is a ground-up
design and the current code is not a head start. The precedent is **D-0074**, which deleted
the two-binary-era safety residue rather than retaining it, on the rule that the tree
describes only what the binary ships — a subsystem retained-but-inert is a standing claim
that it works.

This item is the filing of that decision. No route, gate, tool, or table was changed in the
filing session (`knowledge-graph-guide-fixes`), which touched public documentation only.

## Verified facts grounding the decision

Verified against the live tree during the `knowledge-graph-guide-fixes` Phase-1 pass. Each
is tagged; one item as originally stated did not survive verification and is restated.

1. **The tables start empty and no autonomous producer fills them.** VERIFIED.
   `004_knowledge.up.sql` carries no seed rows. Of the three entry-producer paths, two are
   closed: `SaveKnowledgeEntryTool` (`internal/coreagent/agent.go`) is parked out of the
   agent:core registry per D-0109, and Confluence/Notion sync is dormant behind
   `knowledge.sync_enabled`, which defaults `false` (`internal/config/config.go`).
2. **The third producer path is open, not closed.** VERIFIED, and it **corrects** the
   framing that all three producers are closed. `POST /api/v1/knowledge/entries` is live:
   `registerKnowledgeRoutes` is called from `api.Server.RegisterRoutes`
   (`internal/api/server.go`), sitting directly beneath the D-0081 parked block, so the
   parking pattern was available at that call site and deliberately not applied. It is
   **authenticated-only** — no admin gate, no audit row, no principal stamping, and no
   component RBAC (the guarded accessor is component-scoped; these paths carry no
   componentID). Any authenticated principal may create an entry at the **curated** tier,
   which is permanently immutable, by naming `"tier": "curated"` explicitly. So the store is
   empty on a stock install because nothing has *chosen* to write to it, not because the
   write path is shut. This strengthens the prune case rather than weakening it: the one
   open producer is the least governed write surface in the binary.
3. **Search hard-errors on the two native adapters.** VERIFIED. `Service.Search`
   (`internal/knowledge/search.go`) embeds the query before matching, and `Embed` returns
   `embeddings not yet implemented` on both `claude` (`internal/llm/claude/claude.go`) and
   `gemini` (`internal/llm/gemini/gemini.go`). Scope note: `openai-compat`
   (`internal/llm/openaicompat/openaicompat.go`) implements `Embed` against `/v1/embeddings`,
   so search is functional there against an embeddings-capable endpoint. The feature is
   adapter-conditional, not uniformly dead.
4. **The sync-trigger route reports queued work it never queues.** VERIFIED.
   `handleTriggerSync` (`internal/api/knowledge.go`) validates that the source exists, then
   returns `202` with `status: sync_queued` and "sync will be performed by the background
   coordinator". It checks for no coordinator and enqueues nothing; the polling coordinator
   is the only real trigger, and it only runs when `sync_enabled` is on.
5. **Embedding-less entries are permanently invisible to search.** VERIFIED, and the
   mechanism is worse than "EmbedAll is uncalled". `Service.Create`
   (`internal/knowledge/service.go`) treats an embed failure as **non-fatal** — it logs a
   Warn and stores the row without an embedding. `Search` skips any entry whose embedding is
   empty. `EmbedAll` exists as the backfill and has **zero production callers** (its
   definition and tests only). So on a claude or gemini install every entry written through
   the live route is stored unsearchable by construction, with no path that ever repairs it.

## Scope of the prune

- The `internal/knowledge` tree, including the sync and drift arms.
- The knowledge and drift HTTP routes and their `RegisterRoutes` call sites.
- The `search_knowledge`, `detect_doc_drift`, and `generate_doc_draft` tools, their
  registrations in `internal/tools/default.go`, and their `internal/safety/tier.go`
  classifier rows.
- The parked `SaveKnowledgeEntryTool` and its parking guards.
- The MCP `joe_knowledge_search` tool.
- The Slack related-knowledge snippet.
- The `SyncEnabled` config surface and its boot wiring.
- A drop migration for both tables.

## Coupled surfaces the prune must also settle

Not scope creep — these break the moment the code goes, and a prune that leaves them
claiming a live feature reintroduces the exact drift D-0074 was about.

- **`publish_doc_update`** is registered beside the doc-copilot trio in
  `internal/tools/default.go` and is the one Mutate-classed member. Deleting the proposals
  arm without deciding its fate leaves a registered mutating tool over a deleted store.
- **Published documentation.** `/guides/knowledge-graph/` carries a "Curated versus derived
  knowledge" section and `/concepts/knowledge-graph/` carries the authority model; both
  describe the store as a shipped feature, accurately as of today. `/guides/doc-proposals/`
  is the proposals arm end to end. `/api-reference/` documents the endpoints. All are
  joeagent.dev publication sources and all need revision in the prune session.
- **`docs/project/SITE-CLAIMS.md`** carries a `Knowledge` section with three entries, two
  tagged launch-bound. They are pointers to published copy and must be removed with it.
- **`CLAUDE.md`** carries the knowledge-store invariant paragraph.
- **`docs/backlog/knowledge-store-maturation.md`** is superseded wholesale — it is a plan to
  mature what this item deletes. Close it as superseded rather than leaving two live items
  pointing opposite directions. Its § 6 records the one real forward dependency: future
  IaC-repo LLM-derived inferences were intended to land in the derived tier, and D-0110 pins
  that such inferences may never enter the graph. The v2 design inherits that constraint, so
  the prune should record where those inferences are expected to go, or explicitly leave it
  open for v2.
- **`docs/backlog/learn-from-sessions-fate.md`** is marked `decided` against a store that
  will not exist; revisit or close it.

## Not addressed here

Whether v2 is built at all, and on what design. This item removes the current
implementation; it makes no commitment about a successor.
