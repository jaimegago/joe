-- joe-audit-trail (D-0171): a session's audit entries are readable by session.
--
-- An act the agent loop takes against a managed system leaves one
-- infra_access row, and that row now names the agent-loop session it belongs
-- to. session_id is a column, not only a key inside the context blob, because
-- the one read this table serves — a session's entries, in the order they were
-- written — selects on it and orders by id, and an index can carry both.
--
-- The column is NULL on every row written outside an agent-loop session: a
-- direct HTTP read, the autonomous refresh, every transition and admin row,
-- and every row that predates this migration. NULL reads as "belongs to no
-- session's trail", never as an empty session.
--
-- ADD COLUMN leaves the table, its CHECK constraints, its indexes and the two
-- append-only triggers (migration 015, carried through 017/018/020/027) in
-- place: no rebuild, no row is rewritten, and the UPDATE/DELETE aborts still
-- fire.

ALTER TABLE audit_log ADD COLUMN session_id TEXT;

CREATE INDEX idx_audit_log_session ON audit_log (session_id, id);
