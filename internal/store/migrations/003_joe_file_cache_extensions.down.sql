-- Revert joe_file_cache extensions
ALTER TABLE joe_file_cache DROP COLUMN tool_calls;
ALTER TABLE joe_file_cache DROP COLUMN processed_at;

