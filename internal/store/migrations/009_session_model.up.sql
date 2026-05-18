-- Phase 1 Change 1: Session model — durable session, system regime, and
-- captain binding tables. See docs/PHASE-0-SESSION-MODEL.md (§5b, §B, §R)
-- and docs/PHASE-1-DECOMPOSITION.md (Change 1, §6-C).
--
-- §6-C: FKs to agent_sessions(id) in this migration are ON DELETE CASCADE.
-- Rationale: incident-expunge per PHASE-0-SESSION-MODEL.md §5b-5. The
-- self-FK on agent_sessions(linked_incident_id) is what makes two-level
-- expunge (incident -> linked investigations) a pure schema property.
--
-- Postgres portability: no AUTOINCREMENT, no STRICT, no SQLite-only JSON1.
-- TEXT primary keys (UUIDs supplied by callers); RFC3339 TEXT timestamps;
-- CHECK constraints for enum-shaped TEXT columns.

-- agent_sessions: the durable session record. Type discriminates the small
-- set of behaviors: only 'incident' sessions have a lifecycle (§5b-1); all
-- others are persistent artifacts with no terminal state (§5b-2).
CREATE TABLE agent_sessions (
    id                 TEXT PRIMARY KEY,
    type               TEXT NOT NULL,
    incident_state     TEXT,
    created_at         TEXT NOT NULL,
    last_activity_at   TEXT NOT NULL,
    creator_principal  TEXT NOT NULL,
    linked_incident_id TEXT REFERENCES agent_sessions(id) ON DELETE CASCADE,
    retention_class    TEXT,
    CHECK (type IN ('incident', 'investigation', 'other')),
    CHECK (
        (type = 'incident' AND incident_state IS NOT NULL
            AND incident_state IN ('declared', 'being_worked', 'believed_mitigated', 'resolved', 'reviewed'))
        OR
        (type <> 'incident' AND incident_state IS NULL)
    ),
    CHECK (linked_incident_id IS NULL OR type <> 'incident')
);

CREATE INDEX idx_agent_sessions_type ON agent_sessions (type);
CREATE INDEX idx_agent_sessions_linked_incident ON agent_sessions (linked_incident_id);

-- system_regime: single-row table holding the current system regime.
-- Single-row pattern follows cluster_panic_state from migration 008.
CREATE TABLE system_regime (
    id                    INTEGER PRIMARY KEY DEFAULT 1,
    mode                  TEXT NOT NULL DEFAULT 'normal',
    declared_at           TEXT,
    declared_by_principal TEXT,
    declared_kind         TEXT,
    CHECK (id = 1),
    CHECK (mode IN ('normal', 'incident')),
    CHECK (declared_kind IS NULL OR declared_kind IN ('human', 'joe'))
);

-- Seed the single row so all subsequent operations are UPDATE-only.
INSERT INTO system_regime (id, mode) VALUES (1, 'normal')
ON CONFLICT (id) DO NOTHING;

-- session_captains: captain bindings over time. The "current captain" of a
-- session is the row with detached_at IS NULL. Captain transfer leaves the
-- old row in place (detached_at set) and inserts a new active row. Captain
-- exists only in incident regime (§B4).
CREATE TABLE session_captains (
    id                  TEXT PRIMARY KEY,
    session_id          TEXT NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
    captain_type        TEXT NOT NULL,
    principal           TEXT NOT NULL,
    attached_at         TEXT NOT NULL,
    detached_at         TEXT,
    transfer_state      TEXT,
    incoming_principal  TEXT,
    transfer_initiator  TEXT,
    CHECK (captain_type IN ('human', 'joe')),
    CHECK (transfer_state IS NULL OR transfer_state IN ('active', 'transfer_requested', 'transfer_confirmed')),
    CHECK (transfer_initiator IS NULL OR transfer_initiator IN ('outgoing', 'incoming'))
);

CREATE INDEX idx_session_captains_session_id ON session_captains (session_id);
