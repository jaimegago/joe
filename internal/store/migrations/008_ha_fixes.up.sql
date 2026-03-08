-- HA safety fixes: cluster-wide panic state + proposal deduplication.

-- cluster_panic_state holds exactly one row (id = 1) shared by all joecored
-- instances that point at the same database file.  When panicked = 1 every
-- instance boots in safe mode (T1-only) regardless of its local panic.state
-- file.  A single-row design avoids concurrent INSERT races; use UPDATE only.
CREATE TABLE IF NOT EXISTS cluster_panic_state (
    id             INTEGER PRIMARY KEY DEFAULT 1,
    panicked       INTEGER NOT NULL DEFAULT 0,  -- 1 = emergency shutdown active
    triggered_at   TEXT,
    trigger_source TEXT,
    trigger_reason TEXT,
    CHECK (id = 1)
);

-- Seed the single row so all subsequent operations can use UPDATE.
INSERT OR IGNORE INTO cluster_panic_state (id, panicked) VALUES (1, 0);

-- Prevent two pending proposals for the same target from being created
-- concurrently by two instances.  Once a proposal is approved/rejected a new
-- one can be created for the same target.
CREATE UNIQUE INDEX IF NOT EXISTS idx_proposals_pending_unique_target
    ON doc_proposals (target_type, target_id)
    WHERE status = 'pending';
