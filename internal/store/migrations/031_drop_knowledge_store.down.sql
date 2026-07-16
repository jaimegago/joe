-- Recreate the three tables as they stood before the prune: knowledge_entries and
-- knowledge_sources as created in migration 004 (023 deliberately left
-- knowledge_sources untouched, so 004's shape is current), and doc_proposals as it
-- stood after 005 plus the 008 partial unique index.
--
-- Data is not recoverable by a down migration; this restores shape only.

CREATE TABLE knowledge_entries (
    id TEXT PRIMARY KEY,
    tier TEXT NOT NULL CHECK(tier IN ('curated','synced','derived')),
    type TEXT NOT NULL,
    title TEXT NOT NULL,
    content TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    embedding BLOB,
    embedding_model TEXT,
    embedding_at TIMESTAMP,
    source_type TEXT,
    source_id TEXT,
    source_url TEXT,
    related_nodes TEXT,
    confidence REAL DEFAULT 1.0,
    created_by TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_synced_at TIMESTAMP
);

CREATE INDEX idx_knowledge_tier ON knowledge_entries(tier);
CREATE INDEX idx_knowledge_source ON knowledge_entries(source_type, source_id);
CREATE INDEX idx_knowledge_hash ON knowledge_entries(content_hash);

CREATE TABLE knowledge_sources (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    name TEXT NOT NULL,
    config TEXT NOT NULL,
    status TEXT DEFAULT 'active',
    sync_interval_minutes INTEGER DEFAULT 60,
    last_sync_at TIMESTAMP,
    last_error TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_knowledge_sources_type ON knowledge_sources(type);
CREATE INDEX idx_knowledge_sources_status ON knowledge_sources(status);

CREATE TABLE doc_proposals (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id TEXT NOT NULL,
    target_url TEXT,
    current_content TEXT,
    proposed_content TEXT NOT NULL,
    diff TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    context TEXT,
    knowledge_entry_ids TEXT,
    rejected_reason TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    approved_at TIMESTAMP,
    published_at TIMESTAMP
);

CREATE INDEX idx_proposals_status ON doc_proposals(status);
CREATE INDEX idx_proposals_target ON doc_proposals(target_type, target_id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_proposals_pending_unique_target
    ON doc_proposals (target_type, target_id)
    WHERE status = 'pending';
