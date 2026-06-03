-- Stream G context pass: LLM context-budget setting.
--
-- Adds one singleton table, llm_context_budget, holding the operator-tunable
-- fraction of the active model's context window the agentic loop fills with
-- input (history + prompt + tools) before pruning oldest messages. The rest
-- of the window is reserved for the output cap and the fixed prompt/tool
-- overhead.
--
-- Singleton-row pattern, identical in shape to llm_runaway_limits (migration
-- 017) and llm_settings: id INTEGER PRIMARY KEY DEFAULT 1 with CHECK (id = 1),
-- seeded once so all subsequent writes are UPDATEs (no concurrent-INSERT race
-- between joe instances).
--
-- budget_fraction = 0 is the "unset" sentinel, matching the 0-means-unset
-- convention the cost-limit and runaway-ceiling rows use: the storage-backed
-- ContextBudgetProvider reinterprets a stored zero as the hardcoded backstop
-- fraction (agentloop.DefaultContextBudgetFraction = 0.7), so a freshly
-- migrated system is budgeted by the safe default until an operator sets a
-- value through the settings API. NUMERIC column (not INTEGER) because the
-- fraction is a real in (0, 1]; validation that it stays in range lives in
-- the settings endpoint, not a CHECK, so a future widening of the allowed
-- range does not require a table rebuild.
--
-- No STRICT, no JSON1, no SQLite-only types — portable to Postgres, matching
-- the convention established by migrations 009 and 017. last_modified is
-- RFC3339 UTC TEXT seeded to the never-modified sentinel.

CREATE TABLE llm_context_budget (
    id              INTEGER PRIMARY KEY DEFAULT 1,
    budget_fraction NUMERIC NOT NULL DEFAULT 0,
    last_modified   TEXT NOT NULL DEFAULT '1970-01-01T00:00:00Z',
    CHECK (id = 1)
);

INSERT INTO llm_context_budget (id) VALUES (1)
ON CONFLICT (id) DO NOTHING;
