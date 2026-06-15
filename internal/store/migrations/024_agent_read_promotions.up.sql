-- A001-COREGOV CC-04: per-component-type auto_promote_reads flag.
--
-- A boolean knob, keyed by component-type string, that the RBAC policy engine
-- consults as a DYNAMIC ADMIT PREDICATE for the agent:core principal on
-- ActionRead ONLY (see internal/rbac/policy.go and internal/promotereads).
-- When a type's flag is ON, agent:core may read any component of that type
-- without a materialized grant row — the same grant-less admit pattern the
-- Phase H admin short-circuit already uses, resolved live at decision time.
--
-- Default semantics: ABSENT row == OFF. This is the load-bearing design choice
-- the CC-04 spec calls out: the table is NOT seeded with one row per
-- component-type enum value (36 of them, internal/store/constants.go). Promote
-- == upsert a row with enabled=1; OFF == absent row OR a row with enabled=0.
-- A freshly migrated system therefore has EVERY type OFF with zero rows, the
-- conservative (deny) default. This mirrors the keyed-row, one-row-per-key
-- storage shape of llm_cost_limits (migration 017) but without the seed: there
-- is no neutral pre-seed value to write (absent already means OFF), and seeding
-- 36 rows would be churn with no behavioural meaning.
--
-- The component_type is NOT foreign-keyed to anything: components carry a type
-- but there is no component_types table, and the authoritative enum lives in Go
-- (internal/store/constants.go::IsValidComponentType). The admin setter
-- validates the key against that enum and rejects unknown types at the HTTP
-- boundary (4xx), so arbitrary keys never reach this table; a CHECK here would
-- duplicate that enum in SQL and drift when a new type is added in Go.
--
-- Portability: no STRICT, no SQLite-only types — matching the convention of the
-- llm_* tables (migration 017). enabled is an INTEGER 0/1 boolean (SQLite has
-- no native bool; Postgres accepts 0/1 into a boolean or this stays INTEGER).
-- last_modified is RFC3339 UTC TEXT, the same convention every timestamp column
-- in the schema uses (migrations 009/015/016/017).

CREATE TABLE agent_read_promotions (
    component_type  TEXT PRIMARY KEY,
    enabled         INTEGER NOT NULL DEFAULT 0,
    last_modified   TEXT NOT NULL DEFAULT '1970-01-01T00:00:00Z',
    CHECK (enabled IN (0, 1))
);
