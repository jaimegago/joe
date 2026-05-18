-- Phase 1 Change 2: Run model — the §D durable run substrate.
-- See docs/PHASE-0-SESSION-MODEL.md §D and docs/PHASE-1-DECOMPOSITION.md
-- (Change 2, §6-C).
--
-- §6-C: FKs to agent_sessions(id) and agent_runs(id) in this migration are
-- ON DELETE CASCADE. Rationale: incident-expunge per PHASE-0-SESSION-MODEL.md
-- §5b-5. The cascade test in internal/runmodel/cascade_schema_test.go proves
-- this end-to-end as a pure schema property.

-- agent_runs: the run state machine record. Single-threaded per §D3;
-- "running" is the unique active state per session, enforced by the partial
-- unique index below.
CREATE TABLE agent_runs (
    id           TEXT PRIMARY KEY,
    session_id   TEXT NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
    state        TEXT NOT NULL,
    started_at   TEXT NOT NULL,
    ended_at     TEXT,
    last_step_id TEXT,
    CHECK (state IN ('running', 'awaiting_input', 'awaiting_world', 'completed', 'failed', 'cancelled'))
);

CREATE INDEX idx_agent_runs_session_id ON agent_runs (session_id);

-- D3 / Invariant 1 named structural guard: at most one 'running' run per
-- session. Tested in internal/runmodel/schema_test.go by attempting to
-- insert a second running row and asserting a UNIQUE-constraint failure.
-- Partial-index syntax is portable to both SQLite (3.8+) and Postgres.
CREATE UNIQUE INDEX idx_agent_runs_one_running_per_session
    ON agent_runs (session_id) WHERE state = 'running';

-- run_steps: the durable unit per §D4. step_number is the strictly
-- increasing per-run sequence. payload carries kind-specific TEXT (JSON
-- encoded by callers — no SQLite-only JSON1).
CREATE TABLE run_steps (
    id           TEXT PRIMARY KEY,
    run_id       TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
    step_number  INTEGER NOT NULL,
    kind         TEXT NOT NULL,
    payload      TEXT NOT NULL DEFAULT '',
    persisted_at TEXT NOT NULL,
    CHECK (kind IN (
        'reasoning',
        'tool_call_intent',
        'tool_call_result',
        'solicitation_open',
        'solicitation_resolved',
        'world_handle_recorded',
        'world_handle_observed'
    )),
    UNIQUE (run_id, step_number)
);

CREATE INDEX idx_run_steps_run_id ON run_steps (run_id);

-- run_solicitations: one record per outstanding §D awaiting_input. The kind
-- enum is the §D taxonomy (decision / provide_data / confirm_close).
-- liveness_flag is meaningful only for kind = 'provide_data' per §D taxonomy.
CREATE TABLE run_solicitations (
    id                 TEXT PRIMARY KEY,
    run_id             TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
    kind               TEXT NOT NULL,
    payload            TEXT NOT NULL DEFAULT '',
    created_at         TEXT NOT NULL,
    resolved_at        TEXT,
    resolution_payload TEXT,
    liveness_flag      TEXT,
    CHECK (kind IN ('decision', 'provide_data', 'confirm_close')),
    CHECK (liveness_flag IS NULL OR liveness_flag IN ('attached_human_now', 'out_of_band_human_work'))
);

CREATE INDEX idx_run_solicitations_run_id ON run_solicitations (run_id);

-- run_world_handles: the §D6 reattachable handle. locator + query_meta tell
-- a resuming loop how to re-query the world's current state.
CREATE TABLE run_world_handles (
    id                  TEXT PRIMARY KEY,
    run_id              TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
    locator             TEXT NOT NULL,
    query_meta          TEXT NOT NULL DEFAULT '',
    recorded_at         TEXT NOT NULL,
    last_poll_at        TEXT,
    last_observed_state TEXT
);

CREATE INDEX idx_run_world_handles_run_id ON run_world_handles (run_id);

-- tool_idempotency_keys: §D5 invariant — every world-mutating tool call
-- carries a key persisted *before* the call is issued. Status transitions
-- 'issued' -> 'completed' | 'failed' (terminal). See the repo-API shape
-- test in internal/runmodel/repository_test.go for the no-overwrite rule.
-- step_id is nullable (the key is recorded as the intent is formed; a
-- corresponding step may not yet exist when the executor wraps the call).
CREATE TABLE tool_idempotency_keys (
    key          TEXT PRIMARY KEY,
    run_id       TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
    step_id      TEXT REFERENCES run_steps(id) ON DELETE SET NULL,
    tool_name    TEXT NOT NULL,
    args_hash    TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    completed_at TEXT,
    result       TEXT,
    status       TEXT NOT NULL DEFAULT 'issued',
    CHECK (status IN ('issued', 'completed', 'failed'))
);

CREATE INDEX idx_tool_idempotency_keys_run_id ON tool_idempotency_keys (run_id);

-- action_ledger: the §D8 attaching-SRE view. Tier is the T1/T2/T3 Safety
-- tier. principal is the captain's principal (incident regime) or the
-- request-time principal (normal regime), per §B1.
CREATE TABLE action_ledger (
    id              TEXT PRIMARY KEY,
    run_id          TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
    idempotency_key TEXT NOT NULL REFERENCES tool_idempotency_keys(key) ON DELETE CASCADE,
    tool_name       TEXT NOT NULL,
    tier            INTEGER NOT NULL,
    principal       TEXT NOT NULL,
    source_id       TEXT,
    summary         TEXT NOT NULL,
    recorded_at     TEXT NOT NULL,
    completed_at    TEXT,
    status          TEXT NOT NULL,
    CHECK (tier IN (1, 2, 3))
);

CREATE INDEX idx_action_ledger_run_id ON action_ledger (run_id);
