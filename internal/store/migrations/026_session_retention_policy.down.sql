-- B007a rollback: drop the single-row admin retention-policy configuration
-- table. The reverse of 026.up. Legacy tables are untouched (never referenced
-- here).
DROP TABLE session_retention_policy;
