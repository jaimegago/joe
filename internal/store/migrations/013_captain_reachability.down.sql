-- SQLite supports DROP COLUMN since 3.35; modernc.org/sqlite shipped that.
-- Postgres has supported it since forever.
ALTER TABLE session_captains DROP COLUMN last_seen_at;
