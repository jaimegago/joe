-- B007a: audit_log.kind CHECK widening for the session-lifecycle kind.
--
-- §12.5 requires EVERY session lifecycle transition (trash, restore, purge,
-- archive, unarchive, incident link-sever) to write an audit row naming actor
-- and session. B006 already records the ADMIN govern verbs (purge / archive /
-- unarchive / configure_retention) under the existing 'admin_access' kind — they
-- live on the admin HTTP surface. B007a adds the PER-USER owner transitions
-- (session.trash, session.restore), which are NOT admin-surface events and have
-- no semantically-correct home among the seven kinds the migration-020 CHECK
-- admits. Following the migration-020 precedent (which added 'admin_access'
-- rather than overloading an existing kind), this adds a dedicated
-- 'session_lifecycle' kind. The B007b sweeper's automated transitions reuse it.
--
-- Under the fail-closed audit posture an INSERT with an unadmitted kind fails the
-- CHECK and would break the governed transition outright, so this widening is
-- REQUIRED to record the per-user lifecycle transitions at all — not optional
-- polish.
--
-- SQLite cannot alter a CHECK in place, so this follows the exact rebuild
-- sequence migrations 017/018/020 established: create the widened table, copy
-- every row (column-named, id-preserving), drop the old table (drops its indexes
-- and triggers), rename into place, recreate the three indexes and the two
-- append-only triggers verbatim. Postgres would ALTER the CHECK in place.
--
-- All columns, defaults, nullability, and the decision CHECK are preserved
-- byte-for-byte from migration 020. Only the kind CHECK enum widens, adding
-- 'session_lifecycle'.

CREATE TABLE audit_log_new (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at TEXT NOT NULL,
    principal  TEXT,
    action     TEXT NOT NULL,
    zone       TEXT,
    component_id TEXT,
    decision   TEXT NOT NULL,
    reason     TEXT NOT NULL DEFAULT '',
    kind       TEXT NOT NULL,
    context    TEXT NOT NULL DEFAULT '{}',
    CHECK (decision IN ('allow', 'deny')),
    CHECK (kind IN (
        'infra_access',
        'regime_transition',
        'captain_transition',
        'llm_settings_mutation',
        'llm_limit_triggered',
        'auth_login',
        'admin_access',
        'session_lifecycle'
    ))
);

INSERT INTO audit_log_new (id, created_at, principal, action, zone, component_id, decision, reason, kind, context)
    SELECT id, created_at, principal, action, zone, component_id, decision, reason, kind, context
    FROM audit_log;

DROP TABLE audit_log;

ALTER TABLE audit_log_new RENAME TO audit_log;

CREATE INDEX idx_audit_log_created_at ON audit_log (created_at);
CREATE INDEX idx_audit_log_principal  ON audit_log (principal);
CREATE INDEX idx_audit_log_kind       ON audit_log (kind);

CREATE TRIGGER audit_log_no_update
BEFORE UPDATE ON audit_log
BEGIN
    SELECT RAISE(ABORT, 'audit_log is append-only: UPDATE is not permitted');
END;

CREATE TRIGGER audit_log_no_delete
BEFORE DELETE ON audit_log
BEGIN
    SELECT RAISE(ABORT, 'audit_log is append-only: DELETE is not permitted');
END;
