-- read-posture-latch: the install-wide read posture.
--
-- A single global scalar — NOT keyed by component or principal — that the RBAC
-- policy engine consults LIVE per read decision (see internal/rbac/policy.go and
-- internal/readposture). It selects WHO may perform a read the resolved zone
-- already permits; it never widens WHICH actions a zone allows, and it never
-- touches the mutate axis (the write floor and write-RBAC govern mutates
-- independently of this value).
--
-- Two values:
--   - 'team_flat' — any authenticated principal is admitted for a read action on
--     any component, regardless of grant. The launch default. This is the
--     team-public read model (DESIGN-CHAT-SESSIONS.md §12 team-wide read
--     amendment: privacy between teammates is a non-goal; the spine is integrity
--     and accountability, not secrecy).
--   - 'zoned'     — the grant-based read decision (the full-mode read path):
--     exactly the pre-existing zone+grant behaviour, byte-identical to before
--     this posture existed.
--
-- Default semantics. The row is SEEDED with 'team_flat' so BOTH a fresh install
-- AND an install upgraded from a build that predates this posture inherit
-- 'team_flat' and behave identically until an operator deliberately flips to
-- 'zoned' via the admin REST surface. Moving to 'zoned' is the single, audited
-- operator act that opts an install into grant-based read.
--
-- Singleton-row shape (id = 1, CHECK (id = 1)), mirroring llm_settings and
-- llm_runaway_limits (migration 017): a small, durable, admin-set global scalar.
-- The CHECK pins the two valid posture values so an out-of-band write cannot
-- store a meaningless posture; the admin setter validates at the HTTP boundary
-- too. Portability: no STRICT, no SQLite-only types — matching the llm_* tables.
-- last_modified is RFC3339 UTC TEXT, the schema-wide timestamp convention.

CREATE TABLE read_posture (
    id              INTEGER PRIMARY KEY DEFAULT 1,
    posture         TEXT NOT NULL DEFAULT 'team_flat',
    last_modified   TEXT NOT NULL DEFAULT '1970-01-01T00:00:00Z',
    CHECK (id = 1),
    CHECK (posture IN ('team_flat', 'zoned'))
);

INSERT INTO read_posture (id, posture) VALUES (1, 'team_flat')
ON CONFLICT (id) DO NOTHING;
