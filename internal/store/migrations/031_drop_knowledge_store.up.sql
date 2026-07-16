-- knowledge-store-prune: drop the knowledge store and the doc-proposals arm.
--
-- The knowledge subsystem (internal/knowledge, its HTTP routes, its four
-- registered tools, the MCP joe_knowledge_search tool, and the Confluence/Notion
-- sync arm) is deleted rather than parked, per the D-0074 rule that the tree
-- describes only what the binary ships. With the code gone these tables have no
-- reader or writer.
--
-- doc_proposals goes with them: drafts.Generator was the sole producer of its
-- rows (both the REST create route and generate_doc_draft funnelled through it),
-- and it read the knowledge store to compose a draft, so the proposals arm
-- cannot outlive the store it drafts from.
--
-- Index drops are explicit and precede their tables. idx_proposals_pending_unique_target
-- is named here because it was added later, by migration 008, not by 005 alongside
-- the table — dropping doc_proposals without naming it would leave the 008 index
-- unaccounted for in this migration's record even though SQLite/Postgres remove it
-- with the table.
--
-- No foreign keys reference any of these three tables. knowledge_entries.source_id
-- is a free-form provenance string, not a declared FK to knowledge_sources, and
-- doc_proposals.knowledge_entry_ids is a JSON array of entry IDs in a TEXT column.
-- The drops are therefore unconditional and order-independent.
--
-- Scope note: review_jobs is NOT dropped here. Despite migration 023 renaming its
-- source_id column alongside the knowledge-era tables, it belongs to the Phase 10
-- code-review subsystem, not the proposals arm. It has no Go reader or writer
-- either, but that is a separate disposition — see
-- docs/backlog/review-jobs-orphaned-table.md.

DROP INDEX IF EXISTS idx_proposals_pending_unique_target;
DROP INDEX IF EXISTS idx_proposals_target;
DROP INDEX IF EXISTS idx_proposals_status;
DROP TABLE IF EXISTS doc_proposals;

DROP INDEX IF EXISTS idx_knowledge_sources_status;
DROP INDEX IF EXISTS idx_knowledge_sources_type;
DROP TABLE IF EXISTS knowledge_sources;

DROP INDEX IF EXISTS idx_knowledge_hash;
DROP INDEX IF EXISTS idx_knowledge_source;
DROP INDEX IF EXISTS idx_knowledge_tier;
DROP TABLE IF EXISTS knowledge_entries;
