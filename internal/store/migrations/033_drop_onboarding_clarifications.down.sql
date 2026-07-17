-- Recreate the two tables as they stood before the drop: clarifications as
-- created in migration 001 (023 left it untouched), and onboarding_facts as it
-- stood after migration 023 renamed its source_id column to component_id.
--
-- Data is not recoverable by a down migration; this restores shape only.

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

CREATE TABLE onboarding_facts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    fact_type TEXT NOT NULL,
    subject TEXT NOT NULL,
    content TEXT NOT NULL,
    source TEXT NOT NULL,
    component_id TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_facts_subject ON onboarding_facts(subject);
CREATE INDEX idx_facts_type ON onboarding_facts(fact_type);
