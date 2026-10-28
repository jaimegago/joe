-- autojoe-ro-report-build: the typed finding record a read-only Auto Joe run
-- produces (docs/backlog/autojoe-ro.md § Incrementality and the finding
-- record). Owned by internal/runfindings.
--
-- Not the findings table (a human cross-session annotation, migration 011)
-- and not joe_warnings (no production writer): both match in name and not in
-- shape, and reusing either would make "who wrote this" ambiguous on a record
-- whose author is a principal. A run finding has a table of its own.
--
-- run_finding_sets is the report side's own index of "a run persisted its
-- findings": one row per run, written even when the run found nothing, so the
-- latest run on a scope is exact and a clean run reads as clean. The run
-- record proper — activator, resolved scope, ceilings, coverage, stop reason —
-- is the run side's and lives elsewhere; run_id here is a pointer to it and
-- carries no FK, so the two tables are independent of its shape.
--
-- §6-C: the FK to agent_sessions(id) is ON DELETE CASCADE, so expunging a
-- run's session expunges its findings; run_findings cascades from its set.
-- Portability: TEXT primary keys, RFC3339 TEXT timestamps, JSON as TEXT (no
-- SQLite-only JSON1), partial-index-free.

CREATE TABLE run_finding_sets (
    run_id        TEXT PRIMARY KEY,
    scope_key     TEXT NOT NULL,
    session_id    TEXT NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
    created_at    TEXT NOT NULL,
    finding_count INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_run_finding_sets_scope ON run_finding_sets (scope_key, created_at);
CREATE INDEX idx_run_finding_sets_session ON run_finding_sets (session_id);

-- One row per (run, finding). finding_key is the finding's identity ACROSS
-- runs — derived from type, subject node and claim, never from the run — so
-- the same finding established by two consecutive runs has one finding_key
-- and two rows. label is the row's standing against the previous completed
-- run on the same scope. derivation and proposal are JSON documents whose
-- shape internal/runfindings owns.
CREATE TABLE run_findings (
    id                   TEXT PRIMARY KEY,
    finding_key          TEXT NOT NULL,
    run_id               TEXT NOT NULL REFERENCES run_finding_sets(run_id) ON DELETE CASCADE,
    scope_key            TEXT NOT NULL,
    session_id           TEXT NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
    type                 TEXT NOT NULL,
    reference            TEXT NOT NULL,
    subject_node_id      TEXT NOT NULL,
    subject_component_id TEXT NOT NULL DEFAULT '',
    claim                TEXT NOT NULL,
    condition            TEXT NOT NULL DEFAULT '',
    rationale            TEXT NOT NULL DEFAULT '',
    consequence          TEXT NOT NULL DEFAULT '',
    band                 TEXT NOT NULL,
    reach_count          INTEGER NOT NULL DEFAULT 0,
    weight               REAL NOT NULL DEFAULT 1.0,
    derivation           TEXT NOT NULL,
    proposal             TEXT,
    label                TEXT NOT NULL,
    evidence_session_id  TEXT NOT NULL,
    evidence_message_id  TEXT,
    established_at       TEXT NOT NULL,
    created_at           TEXT NOT NULL,
    CHECK (type IN ('issue', 'risk', 'graph_update')),
    CHECK (band IN ('P0', 'P1', 'P2', 'P3')),
    CHECK (label IN ('new', 'standing', 'resolved', 'unknown')),
    UNIQUE (run_id, finding_key)
);

CREATE INDEX idx_run_findings_run ON run_findings (run_id);
CREATE INDEX idx_run_findings_key ON run_findings (finding_key);
CREATE INDEX idx_run_findings_session ON run_findings (session_id);
CREATE INDEX idx_run_findings_subject ON run_findings (subject_node_id);
