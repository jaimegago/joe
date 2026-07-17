-- Prune graph_nodes rows orphaned by past component deletions (one-time backfill).
--
-- Before session component-delete-graph-orphans, deleting a component removed
-- only its components row and stranded every graph_nodes row carrying its
-- component_id: nothing reconciles those rows away, since a deleted component
-- never refreshes again. This migration deletes the accumulated orphans in
-- existing installs. The delete-path cascade added in the same session prevents
-- NEW orphans, so this is a one-time backfill, NOT a recurring startup sweep — a
-- boot-time sweep would mask a future regression of the cascade invariant instead
-- of letting it surface.
--
-- Predicate is NOT EXISTS over components, deleting a graph_nodes row when no
-- component carries its component_id. NOT EXISTS is deliberate over a bare
-- `component_id NOT IN (SELECT id FROM components)` for the two degenerate ids
-- that name no component and can never be reconciled:
--   * NULL component_id  — `c.id = NULL` is never true, so NOT EXISTS holds -> deleted.
--   * empty string ''    — no component id is empty, so NOT EXISTS holds -> deleted.
-- The IN form would instead leave NULL rows untouched (`NULL NOT IN (...)`
-- evaluates to NULL, never true). Sweeping NULL and '' alike is intended: neither
-- can name a live component. Stating the disposition here, not leaving it to
-- predicate accident.
--
-- graph_edges rows die with their endpoint nodes via the migration-002 FK
-- ON DELETE CASCADE, which fires because foreign_keys=1 is DSN-encoded on every
-- pooled SQLite connection (internal/store), the migrator's included.
DELETE FROM graph_nodes
 WHERE NOT EXISTS (
   SELECT 1 FROM components c WHERE c.id = graph_nodes.component_id
 );
