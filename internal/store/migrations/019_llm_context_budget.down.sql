-- Reverse of 019_llm_context_budget.up.sql: drop the singleton settings
-- table. No other object references it, so a plain DROP is sufficient.
DROP TABLE IF EXISTS llm_context_budget;
