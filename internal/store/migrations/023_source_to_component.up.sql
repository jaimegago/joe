-- D-0021: Rename the infrastructure "source" concept to "component".
--
-- Lexical rename of the `sources` entity, its RBAC zone assignments, the
-- provenance foreign keys that point at it, their indexes, the audit
-- component reference, and the graph node label idiom (<type>_source ->
-- <type>_component). Type values themselves are unchanged.
--
-- The knowledge_sources concept (human/confluence/notion/session) is a
-- different, unrelated entity and is deliberately left untouched, as are
-- the investigation "source session" columns on findings/joe_warnings.
--
-- SQL kept portable across SQLite (3.25+) and Postgres: RENAME TO /
-- RENAME COLUMN, DROP+CREATE INDEX (SQLite has no ALTER INDEX), and
-- substr/length/REPLACE for the label data fix.

-- Core entity table + indexes.
ALTER TABLE sources RENAME TO components;
DROP INDEX idx_sources_type;
DROP INDEX idx_sources_status;
CREATE INDEX idx_components_type ON components(type);
CREATE INDEX idx_components_status ON components(status);

-- RBAC zone assignments: column first, then the table.
ALTER TABLE source_zone_assignments RENAME COLUMN source_id TO component_id;
ALTER TABLE source_zone_assignments RENAME TO component_zone_assignments;

-- Provenance foreign keys -> component_id.
ALTER TABLE graph_nodes RENAME COLUMN source_id TO component_id;
DROP INDEX idx_graph_nodes_source;
CREATE INDEX idx_graph_nodes_component ON graph_nodes(component_id);

ALTER TABLE onboarding_facts RENAME COLUMN source_id TO component_id;
ALTER TABLE review_jobs RENAME COLUMN source_id TO component_id;
ALTER TABLE action_ledger RENAME COLUMN source_id TO component_id;

-- Audit component reference.
ALTER TABLE audit_log RENAME COLUMN source TO component_id;

-- Graph node label idiom: <type>_source -> <type>_component.
UPDATE graph_nodes
   SET type = substr(type, 1, length(type) - length('_source')) || '_component'
 WHERE type LIKE '%\_source' ESCAPE '\';

-- Refresh the seeded zone description that referenced "sources".
UPDATE security_zones
   SET description = 'Default zone for new components'
 WHERE id = 'unassigned' AND description = 'Default zone for new sources';
