-- D-0013: audit_log.kind CHECK widening for the admin-RBAC-mutation kind.
--
-- D-0012 admin-gated the RBAC admin HTTP API (internal/api/admin.go) but
-- confirmed it writes ZERO audit rows — the most authorization-critical
-- mutations in the system (mint a zone, grant/revoke a policy, assign a
-- source to a zone) went unrecorded, and gate denials left no trail. Phase F
-- (migration 015) modelled the guarded accessor's DECISION point; it never
-- modelled mutations of the authorization CONFIGURATION the accessor reads.
-- D-0013 closes that gap.
--
-- Admin-RBAC events have no semantically-correct home among the six kinds
-- the migration-018 CHECK admits (infra_access, regime_transition,
-- captain_transition, llm_settings_mutation, llm_limit_triggered,
-- auth_login). They are their own surface — the parallel of infra_access for
-- the accessor: one kind for every event on the admin RBAC surface
-- (zone/policy/source-zone reads, creates, grants, revokes, assignments, and
-- gate denials), discriminated by action + decision. This migration widens
-- the CHECK to add that kind, 'admin_access'. An INSERT with an unadmitted
-- kind fails the CHECK, and under Phase F's fail-closed posture that would
-- break every admin mutation outright, so this widening is REQUIRED to
-- record admin actions at all — not an optional polish.
--
-- SQLite cannot alter a CHECK constraint in place, so this follows the exact
-- rebuild sequence migrations 017 and 018 established:
--   (a) create a new table with the widened CHECK;
--   (b) copy every row across (column-named, id-preserving);
--   (c) drop the old table (drops its indexes and triggers);
--   (d) rename the new table into place;
--   (e) recreate the three indexes and the two append-only triggers verbatim.
-- Postgres deployments would instead `ALTER TABLE audit_log DROP CONSTRAINT
-- ...; ADD CONSTRAINT ... CHECK (...)` because Postgres can alter a CHECK in
-- place — no table rebuild needed.
--
-- All columns, defaults, nullability, and the decision CHECK are preserved
-- byte-for-byte from migration 015/017/018. Only the kind CHECK enum widens,
-- adding 'admin_access'.

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
        'auth_login',
        'admin_access'
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

-- Recreate the three indexes with the same names as migration 015/017/018 so
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
