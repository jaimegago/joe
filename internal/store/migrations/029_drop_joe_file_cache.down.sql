-- Recreate joe_file_cache as it stood after migrations 001 + 003 (the full
-- shape: the 001 base columns plus the 003 tool_calls / processed_at columns).
CREATE TABLE joe_file_cache (
    file_path TEXT PRIMARY KEY,
    content_hash TEXT NOT NULL,
    parsed_data TEXT NOT NULL,
    parsed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    tool_calls TEXT,
    processed_at TIMESTAMP
);
