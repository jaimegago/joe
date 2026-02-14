-- Add tool_calls and processed_at columns to joe_file_cache for MVP caching
ALTER TABLE joe_file_cache ADD COLUMN tool_calls TEXT;
ALTER TABLE joe_file_cache ADD COLUMN processed_at TIMESTAMP;

