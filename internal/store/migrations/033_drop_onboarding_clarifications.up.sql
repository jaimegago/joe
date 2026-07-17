-- onboarding-feature-removal: drop the onboarding and clarifications tables.
--
-- The onboarding feature and the clarifications subsystem are deleted rather
-- than parked (superseding the D-0081 park), per the D-0074 rule that the tree
-- describes only what the binary ships. With the Go code gone — the discovery
-- engine, the ClarificationService, the store repositories, the
-- save_onboarding_fact tool, and the onboarding/clarifications HTTP routes —
-- these two tables have no reader or writer.
--
-- Index drops are explicit and precede their tables. No foreign keys reference
-- either table (graph provenance lives on graph_nodes.component_id, not here),
-- so the drops are unconditional and order-independent.
--
-- Scope note: review_jobs is NOT dropped here. It belongs to the Phase 10
-- code-review subsystem, not onboarding, and its disposition is separate — see
-- docs/backlog/review-jobs-orphaned-table.md.

DROP INDEX IF EXISTS idx_clarifications_status;
DROP INDEX IF EXISTS idx_clarifications_type;
DROP TABLE IF EXISTS clarifications;

DROP INDEX IF EXISTS idx_facts_subject;
DROP INDEX IF EXISTS idx_facts_type;
DROP TABLE IF EXISTS onboarding_facts;
