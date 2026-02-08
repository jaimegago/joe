-- Sources: registered infrastructure sources
CREATE TABLE sources (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    name TEXT NOT NULL,
    config TEXT NOT NULL,
    status TEXT DEFAULT 'active',
    last_sync_at TIMESTAMP,
    last_error TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_sources_type ON sources(type);
CREATE INDEX idx_sources_status ON sources(status);

-- Sessions: conversation sessions
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    started_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    ended_at TIMESTAMP,
    summary TEXT,
    metadata TEXT
);

CREATE INDEX idx_sessions_started ON sessions(started_at);

-- Session messages: individual messages in a session
CREATE TABLE session_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    tool_name TEXT,
    tool_args TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_messages_session ON session_messages(session_id);

-- Clarifications: Core Agent questions for humans
CREATE TABLE clarifications (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    context TEXT NOT NULL,
    question TEXT NOT NULL,
    options TEXT,
    status TEXT DEFAULT 'pending',
    answer TEXT,
    answered_by TEXT,
    answered_at TIMESTAMP,
    graph_operations TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    notified_at TIMESTAMP
);

CREATE INDEX idx_clarifications_status ON clarifications(status);
CREATE INDEX idx_clarifications_type ON clarifications(type);

-- Joe file cache: hash-based cache for .joe/ file processing
CREATE TABLE joe_file_cache (
    file_path TEXT PRIMARY KEY,
    content_hash TEXT NOT NULL,
    parsed_data TEXT NOT NULL,
    parsed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Onboarding facts: user-provided context
CREATE TABLE onboarding_facts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    fact_type TEXT NOT NULL,
    subject TEXT NOT NULL,
    content TEXT NOT NULL,
    source TEXT NOT NULL,
    source_id TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_facts_subject ON onboarding_facts(subject);
CREATE INDEX idx_facts_type ON onboarding_facts(fact_type);
