-- Revert autojoe-ro-run-build: drop the run record and the schedules. Data is
-- not recoverable by a down migration; indexes are dropped explicitly ahead of
-- their tables, as migration 033 does.
DROP INDEX IF EXISTS idx_auto_schedules_next_run_at;
DROP TABLE IF EXISTS auto_schedules;

DROP INDEX IF EXISTS idx_auto_runs_one_live_per_scope;
DROP INDEX IF EXISTS idx_auto_runs_started_at;
DROP INDEX IF EXISTS idx_auto_runs_scope_key;
DROP TABLE IF EXISTS auto_runs;
