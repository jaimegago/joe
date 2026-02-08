-- Graph nodes: infrastructure topology nodes
CREATE TABLE graph_nodes (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    source_id TEXT,
    metadata TEXT DEFAULT '{}',
    first_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_graph_nodes_type ON graph_nodes(type);
CREATE INDEX idx_graph_nodes_source ON graph_nodes(source_id);

-- Graph edges: relationships between nodes
CREATE TABLE graph_edges (
    from_node TEXT NOT NULL REFERENCES graph_nodes(id) ON DELETE CASCADE,
    to_node TEXT NOT NULL REFERENCES graph_nodes(id) ON DELETE CASCADE,
    relation TEXT NOT NULL,
    confidence INTEGER DEFAULT 3,
    source TEXT,
    context TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (from_node, to_node, relation)
);

CREATE INDEX idx_graph_edges_from ON graph_edges(from_node);
CREATE INDEX idx_graph_edges_to ON graph_edges(to_node);
CREATE INDEX idx_graph_edges_relation ON graph_edges(relation);
