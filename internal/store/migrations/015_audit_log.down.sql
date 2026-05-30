-- Identity Phase F rollback. Removing the audit log is irreversible loss of
-- history (the trigger refuses DELETE for exactly that reason); dropping the
-- whole table is the only way out of the append-only contract. Triggers are
-- dropped first so the implicit DROP TABLE does not trip them.

DROP TRIGGER IF EXISTS audit_log_no_delete;
DROP TRIGGER IF EXISTS audit_log_no_update;
DROP INDEX IF EXISTS idx_audit_log_kind;
DROP INDEX IF EXISTS idx_audit_log_principal;
DROP INDEX IF EXISTS idx_audit_log_created_at;
DROP TABLE IF EXISTS audit_log;
