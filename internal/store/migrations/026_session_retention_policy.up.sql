-- B007a: the admin retention-policy store for the redesigned session subsystem.
-- See docs/reference/DESIGN-CHAT-SESSIONS.md §12.5 (retention/lifecycle pipeline) and §13
-- (B007 node). §12 wins over earlier sections where they conflict.
--
-- §12.5 specifies ONE admin-configured retention policy (not a per-class table):
-- an inactivity window, a trash-grace window, and a terminal action chosen per
-- deployment (trash_then_purge | archive). This is a single-row configuration
-- surface, modelled like system_regime / cluster_panic_state (one row, id=1).
--
--   * inactivity_days  — the §12.5 inactivity window the sweeper (B007b) measures
--     against last_activity_at. NULL = OFF / effectively infinite: nothing
--     auto-expires until an admin opts in (§12.5 "default OFF for the regulated
--     posture"). Seeded NULL.
--   * trash_grace_days  — how long a trashed session waits before purge under
--     trash_then_purge. §12.5 default 30 days. The per-user soft-delete stamps
--     purge_after = trashed_at + trash_grace_days from this value.
--   * terminal_action   — the §12.5 terminal-action selector. Seeded
--     'trash_then_purge'.
--
-- retention_class on a session (migration 025) is the PER-SESSION RESOLUTION of
-- this policy (§12.4) — resolved in the store (ResolveRetention), not a second
-- table. The sweeper that ACTS on the policy is B007b; this migration only adds
-- the configuration store the admin retention-policy routes read and write.
--
-- HARD CONSTRAINT (§13): the legacy migration-001 `sessions` / `session_messages`
-- tables are the future learn-from-sessions feature's data source and are NOT
-- touched here. This migration adds exactly one new table and touches nothing
-- else.

CREATE TABLE session_retention_policy (
    id               INTEGER PRIMARY KEY CHECK (id = 1),
    inactivity_days  INTEGER,
    trash_grace_days INTEGER NOT NULL DEFAULT 30,
    terminal_action  TEXT NOT NULL DEFAULT 'trash_then_purge'
        CHECK (terminal_action IN ('trash_then_purge', 'archive')),
    updated_at       TEXT,
    updated_by       TEXT
);

-- Seed the single policy row with the §12.5 defaults: inactivity OFF (NULL),
-- trash-grace 30 days, terminal action trash_then_purge.
INSERT INTO session_retention_policy (id, inactivity_days, trash_grace_days, terminal_action)
VALUES (1, NULL, 30, 'trash_then_purge');
