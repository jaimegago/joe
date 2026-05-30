-- Stream G phase G1 rollback. Drops the four new LLM-instrumentation
-- tables and their indexes.
--
-- ASYMMETRY: this down does NOT narrow the audit_log.kind CHECK back to
-- the migration-015 enum. Two reasons:
--
--   1. Once a Stream G phase has written 'llm_settings_mutation' or
--      'llm_limit_triggered' rows, narrowing the CHECK would either fail
--      against those existing rows or require deleting them. The audit_log
--      append-only invariant (migration 015 §, internal/audit) forbids
--      deletion — DELETE is trigger-aborted; truncating the table to "fix"
--      a rollback would defeat the entire forensic guarantee.
--
--   2. A widened-but-unused enum value is harmless. The Go side
--      (internal/audit) only declares the Kind constants the codebase
--      writes; nothing in the database itself produces those values
--      spontaneously, so leaving the broader CHECK in place after a
--      rollback has no operational effect.
--
-- If a true narrowing is ever needed (e.g. for a Postgres install with no
-- such rows ever written), it should be a forward migration that drops the
-- constraint and re-adds it with the narrower enum, not a reversal of this
-- one. SQLite would still need a full rebuild for that.

DROP INDEX IF EXISTS idx_llm_usage_model;
DROP INDEX IF EXISTS idx_llm_usage_principal;
DROP INDEX IF EXISTS idx_llm_usage_created_at;
DROP TABLE IF EXISTS llm_usage;

DROP TABLE IF EXISTS llm_runaway_limits;
DROP TABLE IF EXISTS llm_cost_limits;
DROP TABLE IF EXISTS llm_settings;
