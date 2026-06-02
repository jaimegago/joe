-- Stream H3 rollback: narrow the audit_log.kind CHECK back to the
-- migration-017 enum, removing 'auth_login'.
--
-- This is the symmetric reverse of 018.up: it rebuilds audit_log with the
-- five-kind CHECK that 017 left in place (infra_access, regime_transition,
-- captain_transition, llm_settings_mutation, llm_limit_triggered) and
-- recreates the indexes and append-only triggers.
--
-- CAVEAT (same shape as any CHECK-narrowing reverse): if any 'auth_login'
-- rows were written while 018 was applied, the row-copy below will fail
-- against the narrower CHECK rather than silently drop forensic rows — the
-- audit_log append-only invariant (migration 015 §, internal/audit) forbids
-- deleting them. On a fresh DB or a DB with no auth_login rows (the
-- up/down/up round-trip case) the reverse is clean. An operator who has
-- written auth_login rows and genuinely needs to roll back must first
-- export/quarantine those rows; this is the intended, loud failure mode,
-- not a bug.
--
-- Same rebuild sequence and trigger/INSERT-SELECT safety notes as 018.up.

CREATE TABLE audit_log_new (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at TEXT NOT NULL,
    principal  TEXT,
    action     TEXT NOT NULL,
    zone       TEXT,
    source     TEXT,
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
        'llm_limit_triggered'
    ))
);

INSERT INTO audit_log_new (id, created_at, principal, action, zone, source, decision, reason, kind, context)
    SELECT id, created_at, principal, action, zone, source, decision, reason, kind, context
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
