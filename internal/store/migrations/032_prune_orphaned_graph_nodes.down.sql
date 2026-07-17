-- Irreversible data prune: the up migration deletes orphaned graph_nodes rows
-- (and their edges via FK cascade). Deleted rows are not recoverable by a down
-- migration, and the migration changes no schema, so its reversal is a documented
-- no-op. Following the 029/031 convention for data-affecting migrations, whose
-- down restores shape only — here there is no shape to restore.
SELECT 1;
