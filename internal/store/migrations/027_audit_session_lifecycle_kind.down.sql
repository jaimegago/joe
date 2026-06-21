-- B007a rollback: narrow the audit_log.kind CHECK back to the migration-020
-- enum, removing 'session_lifecycle'. Symmetric reverse of 027.up.
--
-- CAVEAT (same shape as the 020.down narrowing): if any 'session_lifecycle' rows
-- were written while 027 was applied, the row-copy below fails against the
-- narrower CHECK rather than silently dropping forensic rows — the append-only
-- invariant forbids deleting them. On a fresh DB or one with no session_lifecycle
-- rows (the up/down/up round-trip case) the reverse is clean. An operator who has
-- written such rows and genuinely needs to roll back must first export/quarantine
-- them; this is the intended, loud failure mode, not a bug.

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
        'admin_access'
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
