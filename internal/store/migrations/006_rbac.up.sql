-- Phase 9.3: RBAC — security zones, source assignments, and policies.

CREATE TABLE security_zones (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT,
    -- JSON array of allowed actions: e.g. '["read","query","mutate","delete"]'
    allowed_actions TEXT NOT NULL DEFAULT '["read"]',
    created_at  TEXT NOT NULL
);

CREATE TABLE source_zone_assignments (
    source_id   TEXT NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    zone_id     TEXT NOT NULL REFERENCES security_zones(id) ON DELETE RESTRICT,
    assigned_by TEXT NOT NULL,
    reason      TEXT,
    assigned_at TEXT NOT NULL,
    PRIMARY KEY (source_id)
);

CREATE TABLE rbac_policies (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    principal   TEXT NOT NULL,
    zone_id     TEXT NOT NULL REFERENCES security_zones(id) ON DELETE CASCADE,
    created_at  TEXT NOT NULL,
    UNIQUE (principal, zone_id)
);

-- Seed default zones
INSERT INTO security_zones (id, name, description, allowed_actions, created_at) VALUES
    ('prod-readonly', 'Production Read-Only', 'Read and query operations only', '["read","query"]',          CURRENT_TIMESTAMP),
    ('prod-write',    'Production Write',     'Read, query, and mutate',         '["read","query","mutate"]', CURRENT_TIMESTAMP),
    ('dev-full',      'Development Full',     'All operations',                  '["read","query","mutate","delete"]', CURRENT_TIMESTAMP),
    ('unassigned',    'Unassigned',           'Default zone for new sources',    '["read"]',                 CURRENT_TIMESTAMP);
