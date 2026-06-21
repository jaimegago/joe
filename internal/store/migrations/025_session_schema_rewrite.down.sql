-- Reverts 025_session_schema_rewrite.up.sql: rebuild agent_sessions back to its
-- pre-025 (migration 022-era) shape — type domain {incident, investigation,
-- other}, linked_incident_id ON DELETE CASCADE, the title + visibility columns,
-- and none of the lifecycle timestamp columns. The 'default' literal maps back
-- to 'other'. visibility is restored as NOT NULL DEFAULT 'private'.

CREATE TABLE agent_sessions_old (
    id                 TEXT PRIMARY KEY,
    type               TEXT NOT NULL,
    incident_state     TEXT,
    created_at         TEXT NOT NULL,
    last_activity_at   TEXT NOT NULL,
    creator_principal  TEXT NOT NULL,
    linked_incident_id TEXT REFERENCES agent_sessions_old(id) ON DELETE CASCADE,
    retention_class    TEXT,
    title              TEXT,
    visibility         TEXT NOT NULL DEFAULT 'private',
    CHECK (type IN ('incident', 'investigation', 'other')),
    CHECK (
        (type = 'incident' AND incident_state IS NOT NULL
            AND incident_state IN ('declared', 'being_worked', 'believed_mitigated', 'resolved', 'reviewed'))
        OR
        (type <> 'incident' AND incident_state IS NULL)
    ),
    CHECK (linked_incident_id IS NULL OR type <> 'incident')
);

INSERT INTO agent_sessions_old
    (id, type, incident_state, created_at, last_activity_at,
     creator_principal, linked_incident_id, retention_class, title, visibility)
SELECT id,
       CASE WHEN type = 'incident' THEN 'incident' ELSE 'other' END,
       incident_state, created_at, last_activity_at,
       creator_principal, linked_incident_id, retention_class, title, 'private'
FROM agent_sessions
WHERE linked_incident_id IS NULL;

INSERT INTO agent_sessions_old
    (id, type, incident_state, created_at, last_activity_at,
     creator_principal, linked_incident_id, retention_class, title, visibility)
SELECT id,
       CASE WHEN type = 'incident' THEN 'incident' ELSE 'other' END,
       incident_state, created_at, last_activity_at,
       creator_principal, linked_incident_id, retention_class, title, 'private'
FROM agent_sessions
WHERE linked_incident_id IS NOT NULL;

DROP TABLE agent_sessions;
ALTER TABLE agent_sessions_old RENAME TO agent_sessions;

CREATE INDEX idx_agent_sessions_type ON agent_sessions (type);
CREATE INDEX idx_agent_sessions_linked_incident ON agent_sessions (linked_incident_id);
