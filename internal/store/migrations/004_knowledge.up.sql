-- Knowledge entries: unified store for all three knowledge tiers
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

-- Knowledge sources: external sync targets (Confluence spaces, Notion databases)
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
