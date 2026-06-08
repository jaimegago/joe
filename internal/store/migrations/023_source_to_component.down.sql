-- Reverts 023_source_to_component.up.sql.

-- Graph node label idiom back: <type>_component -> <type>_source.
UPDATE graph_nodes
   SET type = substr(type, 1, length(type) - length('_component')) || '_source'
 WHERE type LIKE '%\_component' ESCAPE '\';

UPDATE security_zones
   SET description = 'Default zone for new sources'
 WHERE id = 'unassigned' AND description = 'Default zone for new components';

ALTER TABLE audit_log RENAME COLUMN component_id TO source;

ALTER TABLE action_ledger RENAME COLUMN component_id TO source_id;
ALTER TABLE review_jobs RENAME COLUMN component_id TO source_id;
ALTER TABLE onboarding_facts RENAME COLUMN component_id TO source_id;

DROP INDEX idx_graph_nodes_component;
ALTER TABLE graph_nodes RENAME COLUMN component_id TO source_id;
CREATE INDEX idx_graph_nodes_source ON graph_nodes(source_id);

ALTER TABLE component_zone_assignments RENAME TO source_zone_assignments;
ALTER TABLE source_zone_assignments RENAME COLUMN component_id TO source_id;

DROP INDEX idx_components_type;
DROP INDEX idx_components_status;
CREATE INDEX idx_sources_type ON components(type);
CREATE INDEX idx_sources_status ON components(status);
ALTER TABLE components RENAME TO sources;
