-- Reverts 030_chat_message_stop_reason.up.sql. stop_reason is not indexed and
-- carries no constraint, so it is droppable on SQLite (3.35+) and Postgres.
ALTER TABLE chat_messages DROP COLUMN stop_reason;
