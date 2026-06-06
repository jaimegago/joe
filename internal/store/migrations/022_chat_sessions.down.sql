-- Reverts 022_chat_sessions.up.sql. Drop the child table first, then the
-- agent_sessions columns. Neither `title` nor `visibility` is indexed or used in
-- a constraint, so both are droppable on SQLite (3.35+) and Postgres.

DROP INDEX IF EXISTS idx_chat_messages_session_id;
DROP TABLE IF EXISTS chat_messages;

ALTER TABLE agent_sessions DROP COLUMN visibility;
ALTER TABLE agent_sessions DROP COLUMN title;
