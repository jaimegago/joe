-- Reverse of 036: drop the session column and its index. Rows keep every other
-- column; the session an act belonged to stays in the row's context blob.

DROP INDEX IF EXISTS idx_audit_log_session;

ALTER TABLE audit_log DROP COLUMN session_id;
