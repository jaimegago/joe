-- autojoe-ro-run-build: the Auto Joe run record and the standing triggers that
-- start a run (joe-pm queue/autojoe-ro-run.md § Activation, § Budget; the
-- build order docs/backlog/autojoe-ro.md § The run record).
--
-- auto_runs is a SIBLING of agent_runs, not an extension of it. agent_runs is
-- the §D interactive substrate — its state machine pauses on solicitations and
-- world handles, and its structural guard is one 'running' run per SESSION. An
-- Auto Joe run has no human turn, so those states are unreachable, and its
-- guard is one live run per SCOPE. Giving it its own table keeps agent_runs
-- written only by the sessions API, so a /tasks call still creates no row in
-- either, and keeps every column here the run record the design named:
-- activator, resolved component set, both ceilings, coverage, stop reason, and
-- the session the run wrote.
--
-- selection, resolved_components, covered and not_covered are JSON-encoded
-- TEXT (callers encode; no SQLite-only JSON1), the same posture run_steps takes
-- for its payload.
--
-- session_id is ON DELETE SET NULL: the run record outlives its session. A
-- purged session severs the pointer and never erases the record of what was
-- covered and why the run stopped.
CREATE TABLE auto_runs (
    id                    TEXT PRIMARY KEY,
    activator             TEXT NOT NULL,
    principal             TEXT NOT NULL,
    selection             TEXT NOT NULL,
    scope_key             TEXT NOT NULL,
    resolved_components   TEXT NOT NULL,
    token_ceiling         INTEGER NOT NULL,
    wall_clock_seconds    INTEGER NOT NULL,
    report_reserve_tokens INTEGER NOT NULL,
    state                 TEXT NOT NULL,
    stop_reason           TEXT NOT NULL DEFAULT '',
    covered               TEXT NOT NULL DEFAULT '[]',
    not_covered           TEXT NOT NULL DEFAULT '[]',
    tokens_used           INTEGER NOT NULL DEFAULT 0,
    report_tokens         INTEGER NOT NULL DEFAULT 0,
    session_id            TEXT REFERENCES agent_sessions(id) ON DELETE SET NULL,
    error                 TEXT NOT NULL DEFAULT '',
    started_at            TEXT NOT NULL,
    ended_at              TEXT,
    CHECK (state IN ('running', 'completed', 'failed'))
);

CREATE INDEX idx_auto_runs_scope_key ON auto_runs (scope_key);
CREATE INDEX idx_auto_runs_started_at ON auto_runs (started_at);

-- At most one live run per scope (§ Activation). A second trigger on a scope
-- with a running row fails this index and is refused, never queued. Partial
-- index syntax is portable to SQLite (3.8+) and Postgres, as in migration 010.
CREATE UNIQUE INDEX idx_auto_runs_one_live_per_scope
    ON auto_runs (scope_key) WHERE state = 'running';

-- auto_schedules: a schedule is a standing trigger the operator installs over a
-- scope. The ceilings are copied from the deployment defaults at install time,
-- so a schedule's cost reads as runs times ceiling from what the operator set.
CREATE TABLE auto_schedules (
    id                    TEXT PRIMARY KEY,
    selection             TEXT NOT NULL,
    scope_key             TEXT NOT NULL,
    interval_seconds      INTEGER NOT NULL,
    token_ceiling         INTEGER NOT NULL,
    wall_clock_seconds    INTEGER NOT NULL,
    report_reserve_tokens INTEGER NOT NULL,
    installed_by          TEXT NOT NULL,
    installed_at          TEXT NOT NULL,
    next_run_at           TEXT NOT NULL,
    last_run_at           TEXT,
    last_outcome          TEXT NOT NULL DEFAULT '',
    CHECK (interval_seconds > 0)
);

CREATE INDEX idx_auto_schedules_next_run_at ON auto_schedules (next_run_at);
