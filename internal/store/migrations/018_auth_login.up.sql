-- Stream H3: audit_log.kind CHECK widening for the auth_login kind.
--
-- Break-glass auth-login auditing (docs/reference/joe-identity-design.md §2.6) records
-- credential use — both the OIDC human login and the break-glass
-- service-account bearer path — as one row in the existing append-only
-- audit_log. Those rows carry a new kind value, 'auth_login', which the
-- CHECK constraint from migration 017 does not yet admit. This migration
-- widens that CHECK to add it.
--
-- SQLite cannot alter a CHECK constraint in place, so this follows the exact
-- rebuild-and-recreate sequence migration 017 established:
--   (a) create a new table with the widened CHECK;
--   (b) copy all rows across, preserving id values (AUTOINCREMENT sequence);
--   (c) drop the old table (which drops its indexes and triggers);
--   (d) rename the new table into place;
--   (e) recreate the three named indexes;
--   (f) recreate the two append-only triggers.
--
-- The two BEFORE UPDATE / BEFORE DELETE triggers do NOT fire on
-- `INSERT INTO ... SELECT` or on `DROP TABLE`, so this sequence is safe to
-- run on a non-empty audit_log without tripping the append-only abort.
--
-- All columns, defaults, nullability, and the decision CHECK are preserved
-- byte-for-byte from migration 015/017. Only the kind CHECK enum widens,
-- adding 'auth_login'. The Postgres equivalent would be a simple
-- `ALTER TABLE audit_log DROP CONSTRAINT ...; ADD CONSTRAINT ... CHECK (...)`
-- because Postgres can alter a CHECK in place — no table rebuild needed.

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
        'llm_limit_triggered',
        'auth_login'
    ))
);

-- Copy every row across explicitly. Naming the columns guards against a
-- future column order shift; preserving id values keeps the AUTOINCREMENT
-- sequence aligned with prior rows.
INSERT INTO audit_log_new (id, created_at, principal, action, zone, source, decision, reason, kind, context)
    SELECT id, created_at, principal, action, zone, source, decision, reason, kind, context
    FROM audit_log;

-- Drop the old table. This also drops its three indexes and its two
-- triggers (they hung off the dropped relation). DROP TABLE does NOT fire
-- the BEFORE DELETE trigger — triggers fire on DELETE statements only.
DROP TABLE audit_log;

-- Rename the new table into place. RENAME TO carries the AUTOINCREMENT
-- sequence; it does NOT carry triggers or indexes, so we recreate them below.
ALTER TABLE audit_log_new RENAME TO audit_log;

-- Recreate the three indexes with the same names as migration 015/017 so
-- queries written against those index names continue to work.
CREATE INDEX idx_audit_log_created_at ON audit_log (created_at);
CREATE INDEX idx_audit_log_principal  ON audit_log (principal);
CREATE INDEX idx_audit_log_kind       ON audit_log (kind);

-- Recreate the two append-only triggers verbatim from migration 015. The
-- database-level append-only guarantee is restored once these are in place.
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
