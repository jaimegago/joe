-- B002: Storage-schema rewrite for the redesigned session subsystem.
-- See docs/reference/DESIGN-CHAT-SESSIONS.md §12.4 (clean-room storage) and §13 (B002
-- node). §12 wins over earlier sections of that document where they conflict.
--
-- This migration rebuilds agent_sessions to the §12.4 target:
--   * type domain collapses from {incident, investigation, other} to exactly
--     {default, incident}. The as-built 'other' literal becomes 'default' and
--     'investigation' is removed (incident participation is the
--     linked_incident_id pointer, a fact — not a type — per §12.3).
--   * linked_incident_id changes from ON DELETE CASCADE to ON DELETE SET NULL:
--     purging an incident SEVERS the link (linked sessions revert to plain
--     'default' conversations) and never destroys those independent sessions
--     (§12.4). Cascade-as-a-unit applies only to a session's OWN dependent rows
--     (its transcript + captain bindings), which keep ON DELETE CASCADE.
--   * lifecycle is expressed by timestamp/reference columns, not a state enum:
--     trashed_at / trashed_by / purge_after / archived_at / archived_by /
--     archive_ref. Active = all six null (§12.4).
--   * the visibility column is dropped: team-wide read makes per-session
--     visibility inert (§12.4, §12.9 amended 2026-06-21).
--   * retention_class is kept (redefined as the per-session resolution of the
--     active admin retention policy — §12.4). title is kept (human-editable).
--   * the incident_state ⇔ type=incident CHECK is preserved (B002 acceptance).
--
-- The transcript table (chat_messages, migration 022) is the PERMANENT,
-- first-class system of record (§12.4) and is intentionally left untouched; its
-- FK to agent_sessions(id) stays ON DELETE CASCADE and resolves to the rebuilt
-- table by name after the swap below.
--
-- HARD CONSTRAINT: the legacy migration-001 `sessions` / `session_messages`
-- tables are the future learn-from-sessions feature's data source and are NOT
-- touched here (§13, docs/backlog/learn-from-sessions-fate.md).
--
-- Portability: a CHECK-domain change and an FK on-delete change cannot be done
-- by ALTER on SQLite, so the table is rebuilt with the portable
-- CREATE/INSERT/DROP/RENAME idiom. The INSERT copies parents (null link) before
-- children (non-null link) so the self-FK holds even with foreign_keys ON. The
-- ledger records nothing is deployed, so no data is expected; the copy is kept
-- for migration discipline and any dev rows.

CREATE TABLE agent_sessions_new (
    id                 TEXT PRIMARY KEY,
    type               TEXT NOT NULL,
    incident_state     TEXT,
    created_at         TEXT NOT NULL,
    last_activity_at   TEXT NOT NULL,
    creator_principal  TEXT NOT NULL,
    linked_incident_id TEXT REFERENCES agent_sessions_new(id) ON DELETE SET NULL,
    retention_class    TEXT,
    title              TEXT,
    trashed_at         TEXT,
    trashed_by         TEXT,
    purge_after        TEXT,
    archived_at        TEXT,
    archived_by        TEXT,
    archive_ref        TEXT,
    CHECK (type IN ('default', 'incident')),
    CHECK (
        (type = 'incident' AND incident_state IS NOT NULL
            AND incident_state IN ('declared', 'being_worked', 'believed_mitigated', 'resolved', 'reviewed'))
        OR
        (type <> 'incident' AND incident_state IS NULL)
    ),
    CHECK (linked_incident_id IS NULL OR type <> 'incident')
);

-- Parents first (linked_incident_id IS NULL), then children, so the self-FK is
-- satisfied row-by-row even under immediate FK enforcement. type 'incident'
-- stays; everything else ('other' and the removed 'investigation') maps to
-- 'default'.
INSERT INTO agent_sessions_new
    (id, type, incident_state, created_at, last_activity_at,
     creator_principal, linked_incident_id, retention_class, title)
SELECT id,
       CASE WHEN type = 'incident' THEN 'incident' ELSE 'default' END,
       incident_state, created_at, last_activity_at,
       creator_principal, linked_incident_id, retention_class, title
FROM agent_sessions
WHERE linked_incident_id IS NULL;

INSERT INTO agent_sessions_new
    (id, type, incident_state, created_at, last_activity_at,
     creator_principal, linked_incident_id, retention_class, title)
SELECT id,
       CASE WHEN type = 'incident' THEN 'incident' ELSE 'default' END,
       incident_state, created_at, last_activity_at,
       creator_principal, linked_incident_id, retention_class, title
FROM agent_sessions
WHERE linked_incident_id IS NOT NULL;

DROP TABLE agent_sessions;
ALTER TABLE agent_sessions_new RENAME TO agent_sessions;

CREATE INDEX idx_agent_sessions_type ON agent_sessions (type);
CREATE INDEX idx_agent_sessions_linked_incident ON agent_sessions (linked_incident_id);
