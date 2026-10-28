-- Reverse of 035: drop the run findings record. Data is not recoverable by a
-- down migration; this restores shape only. Children before parents.

DROP INDEX IF EXISTS idx_run_findings_subject;
DROP INDEX IF EXISTS idx_run_findings_session;
DROP INDEX IF EXISTS idx_run_findings_key;
DROP INDEX IF EXISTS idx_run_findings_run;
DROP TABLE IF EXISTS run_findings;

DROP INDEX IF EXISTS idx_run_finding_sets_session;
DROP INDEX IF EXISTS idx_run_finding_sets_scope;
DROP TABLE IF EXISTS run_finding_sets;
