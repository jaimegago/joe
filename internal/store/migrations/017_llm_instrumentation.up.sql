-- Stream G phase G1: LLM instrumentation schema.
-- See docs/reference/joe-identity-design.md (LLM cost/runaway gates) and the Stream G
-- prompt. This migration is purely a schema groundwork phase — no recorder,
-- enforcement, settings service, HTTP, or UI lands here. It only:
--
--   1. Adds four new tables: llm_usage, llm_settings, llm_cost_limits,
--      llm_runaway_limits.
--   2. Widens the audit_log.kind CHECK constraint to admit two new kinds:
--      'llm_settings_mutation' and 'llm_limit_triggered'.
--
-- The whole file relies on the golang-migrate SQLite driver's per-file
-- transaction wrapping for atomicity (no explicit BEGIN/COMMIT; no PRAGMA
-- foreign_keys — audit_log is a leaf table with no inbound foreign keys, so
-- the PRAGMA would be a no-op inside the wrapped transaction).
--
-- Portability note. Migration 015 (the original audit_log) already uses
-- SQLite-only syntax (AUTOINCREMENT, RAISE(ABORT)); the established
-- audit_log path is SQLite-first. The new tables in this file are written
-- with portability in mind (no STRICT, no JSON1, no SQLite-only types),
-- matching 009's convention. The audit_log rebuild is the SQLite path; the
-- equivalent on Postgres would be a simple `ALTER TABLE audit_log DROP
-- CONSTRAINT ... ; ADD CONSTRAINT ... CHECK (kind IN (...))` because
-- Postgres can alter a CHECK in place — no table rebuild needed there.

-- =========================================================================
-- 1. llm_usage — per-call usage record.
-- =========================================================================
-- One row per LLM call. Columns track principal (nullable for caller-less
-- system calls, matching audit_log.principal's nullable convention), the
-- model invoked, token counts, and an estimated cost stored as integer
-- nano-units of the row's currency (see estimated_cost_nano and currency
-- below). session_id and task_id are nullable because not every call
-- originates from a tracked session/task.
--
-- Why integer nano-units and not NUMERIC/REAL: integer SUM is EXACT on both
-- SQLite and Postgres, while REAL/NUMERIC accumulation can drift in either
-- engine when many small per-call costs are summed for a cost window. The
-- integer scale is fixed at 1e-9 of the row's currency (one nano-unit),
-- documented in the Go constant CostNanoUnitsPerUnit (internal/llm). Safe
-- to change to integer now because no caller writes this column yet — the
-- recorder lands in a later Stream G phase.
--
-- Why currency is a per-row column and not a single global setting:
-- provider prices are quoted in a provider-chosen currency (predominantly
-- USD), and self-hosted / open-weight models have no per-token provider
-- charge at all. The recorder (later phase) converts the source-currency
-- price into Joe's configured currency at record time and stamps that
-- currency here. Recording the currency on the ROW means a later change to
-- Joe's configured currency does not misdenominate the historical rows.
--
-- No CHECK enumerating allowed currencies on this column: the allowed set
-- is enforced in config validation (internal/config), and
-- over-constraining a historical-record column risks rejecting a
-- legitimately-written past value after the allowed set changes.
--
-- The created_at index is required this phase because later cost-window
-- aggregation queries filter on it; per-principal and per-model rollups have
-- their own indexes for the same reason.

CREATE TABLE llm_usage (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at            TEXT NOT NULL,
    principal             TEXT,
    model                 TEXT NOT NULL,
    input_tokens          INTEGER NOT NULL DEFAULT 0,
    output_tokens         INTEGER NOT NULL DEFAULT 0,
    -- estimated_cost_nano: integer nano-units (1e-9) of the currency named
    -- in this row's `currency` column. Provider prices are converted to the
    -- configured currency at record time by a later phase; a stored value
    -- of zero is VALID and is what self-hosted or unpriced models record.
    estimated_cost_nano   INTEGER NOT NULL DEFAULT 0,
    -- currency: the operator's configured currency at the time this row was
    -- written, in which estimated_cost_nano is denominated. Recorded per
    -- row so a later change to Joe's configured currency does not
    -- misdenominate historical rows. No CHECK on this column — see header
    -- comment.
    currency              TEXT NOT NULL,
    session_id            TEXT,
    task_id               TEXT
);

CREATE INDEX idx_llm_usage_created_at ON llm_usage (created_at);
CREATE INDEX idx_llm_usage_principal  ON llm_usage (principal);
CREATE INDEX idx_llm_usage_model      ON llm_usage (model);

-- =========================================================================
-- 2. llm_settings — singleton operator-editable settings (active model).
-- =========================================================================
-- Singleton-row pattern matching system_regime (migration 009) and
-- cluster_panic_state (migration 008): id INTEGER PRIMARY KEY DEFAULT 1
-- with CHECK (id = 1). The seed INSERT below means all subsequent writes
-- are UPDATE — no concurrent INSERT race between joe-core instances.
--
-- last_modified is RFC3339 UTC TEXT, the same convention used by every
-- timestamp column added in migrations 009, 010, 011, 014, 015. The seed
-- value '1970-01-01T00:00:00Z' is a clear unset/never-modified sentinel
-- (the settings service writing later phases will overwrite it with the
-- real RFC3339Nano stamp on first mutation).

CREATE TABLE llm_settings (
    id              INTEGER PRIMARY KEY DEFAULT 1,
    active_model    TEXT NOT NULL DEFAULT '',
    last_modified   TEXT NOT NULL DEFAULT '1970-01-01T00:00:00Z',
    CHECK (id = 1)
);

INSERT INTO llm_settings (id) VALUES (1)
ON CONFLICT (id) DO NOTHING;

-- =========================================================================
-- 3. llm_cost_limits — per-window cost thresholds.
-- =========================================================================
-- Keyed-row shape (one row per window) so each threshold can be read and
-- updated independently. CHECK pins the three valid window names. Window
-- column is `window_name` (not `window`) because WINDOW is reserved in
-- Postgres; the underscore form is unambiguous in both drivers.
--
-- The three rows are seeded at install time with threshold = 0, which the
-- later cost-window service treats as "no limit configured" — operators
-- enable enforcement by setting a non-zero threshold via the (later)
-- settings API.

CREATE TABLE llm_cost_limits (
    window_name     TEXT PRIMARY KEY,
    threshold       NUMERIC NOT NULL DEFAULT 0,
    last_modified   TEXT NOT NULL DEFAULT '1970-01-01T00:00:00Z',
    CHECK (window_name IN ('hourly', 'daily', 'monthly'))
);

INSERT INTO llm_cost_limits (window_name) VALUES ('hourly')  ON CONFLICT (window_name) DO NOTHING;
INSERT INTO llm_cost_limits (window_name) VALUES ('daily')   ON CONFLICT (window_name) DO NOTHING;
INSERT INTO llm_cost_limits (window_name) VALUES ('monthly') ON CONFLICT (window_name) DO NOTHING;

-- =========================================================================
-- 4. llm_runaway_limits — session token ceiling.
-- =========================================================================
-- Singleton-row pattern (same as llm_settings). session_token_ceiling = 0
-- is the "no ceiling configured" sentinel; the runaway gate refuses to
-- terminate sessions when ceiling is 0, matching the cost-limit "0 = off"
-- convention above.

CREATE TABLE llm_runaway_limits (
    id                      INTEGER PRIMARY KEY DEFAULT 1,
    session_token_ceiling   INTEGER NOT NULL DEFAULT 0,
    last_modified           TEXT NOT NULL DEFAULT '1970-01-01T00:00:00Z',
    CHECK (id = 1)
);

INSERT INTO llm_runaway_limits (id) VALUES (1)
ON CONFLICT (id) DO NOTHING;

-- =========================================================================
-- 5. audit_log.kind CHECK widening — rebuild-table sequence.
-- =========================================================================
-- SQLite cannot alter a CHECK constraint in place. The standard pattern is:
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
-- byte-for-byte from migration 015. Only the kind CHECK enum widens, adding
-- 'llm_settings_mutation' and 'llm_limit_triggered'. These two new kinds
-- are written by later Stream G phases — no caller writes them yet.

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
-- sequence; it does NOT carry triggers or indexes (the new table never had
-- any defined on it), so we recreate them below.
ALTER TABLE audit_log_new RENAME TO audit_log;

-- Recreate the three indexes with the same names as migration 015 so
-- queries written against the original index names continue to work.
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
