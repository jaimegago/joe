-- Chat sessions Phase 1 (ownership & isolation): move the Web UI chat onto the
-- new session model and close the cross-user chat data leak.
-- See docs/reference/DESIGN-CHAT-SESSIONS.md §10 (locked decisions) and §11 (Phase 1).
--
-- Two parts:
--  1. agent_sessions gains `title` (human-editable label, written in Phase 2)
--     and `visibility` (default 'private'). visibility ships now — before the
--     sharing feature exists — so Phase 3 needs no second migration to make a
--     session public. Validation of the value set is deferred to the app layer
--     (no CHECK), which keeps the column droppable in the down migration on
--     SQLite (a column used by a CHECK cannot be dropped).
--  2. chat_messages: the interim flat, owner-scoped message store keyed to
--     agent_sessions. Per the §10 storage decision this is explicitly interim —
--     the committed endgame is agent_runs->run_steps (a durable agentic trace in
--     history), which retires this table. It exists now to preserve the flat
--     user/assistant rendering the chat UI already does.
--
-- Postgres portability: no AUTOINCREMENT, no STRICT, no SQLite-only JSON1.
-- TEXT primary key (UUID supplied by callers); RFC3339 TEXT timestamps; a
-- per-session INTEGER `seq` gives deterministic ordering (mirrors
-- run_steps.step_number) without relying on lexical timestamp comparison.

ALTER TABLE agent_sessions ADD COLUMN title TEXT;
ALTER TABLE agent_sessions ADD COLUMN visibility TEXT NOT NULL DEFAULT 'private';

-- §6-C: the FK to agent_sessions(id) is ON DELETE CASCADE so deleting a session
-- expunges its messages (incident-expunge per the session-model design (Phase 0) §5b-5).
CREATE TABLE chat_messages (
    id          TEXT PRIMARY KEY,
    session_id  TEXT NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
    seq         INTEGER NOT NULL,
    role        TEXT NOT NULL,
    content     TEXT NOT NULL DEFAULT '',
    tool_name   TEXT,
    tool_args   TEXT,
    created_at  TEXT NOT NULL,
    UNIQUE (session_id, seq)
);

CREATE INDEX idx_chat_messages_session_id ON chat_messages (session_id);
