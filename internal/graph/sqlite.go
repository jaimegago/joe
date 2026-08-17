package graph

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/store"
)

// sqlGraphStore implements GraphStore using a SQL database.
type sqlGraphStore struct {
	db      *sql.DB
	driver  string
	metrics *observability.Metrics
}

// execContext is the subset of *sql.DB / *sql.Tx a write body needs, so a
// transactional path can share one SQL body against whichever executor it is
// handed — the same idiom the component store uses (internal/store/components.go).
type execContext interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// SQLiteStore is a backward-compatible alias kept for test helpers.
// New code should use NewSQLStore.
type SQLiteStore = sqlGraphStore

// NewSQLStore creates a SQL-backed graph store for the given driver.
// The provided db must have the graph_nodes and graph_edges tables (via migration 002).
func NewSQLStore(db *sql.DB, driver string, metrics *observability.Metrics) *sqlGraphStore {
	return &sqlGraphStore{db: db, driver: driver, metrics: observability.EnsureMetrics(metrics)}
}

// NewSQLiteStore is a backward-compatible constructor (equivalent to NewSQLStore with DriverSQLite).
func NewSQLiteStore(db *sql.DB, metrics *observability.Metrics) *sqlGraphStore {
	return NewSQLStore(db, store.DriverSQLite, metrics)
}

// AddNode performs an upsert: inserts a new node or updates an existing one.
// On conflict (same ID), it updates type, component_id, metadata, and last_seen
// while preserving the original first_seen timestamp.
func (s *sqlGraphStore) AddNode(ctx context.Context, node Node) (err error) {
	start := time.Now()
	defer func() { s.metrics.RecordGraphOperation(ctx, "add_node", time.Since(start), err) }()

	if node.Metadata == nil {
		node.Metadata = map[string]any{}
	}
	metadata, err := json.Marshal(node.Metadata)
	if err != nil {
		return fmt.Errorf("marshal node metadata: %w", err)
	}

	now := time.Now()
	if node.FirstSeen.IsZero() {
		node.FirstSeen = now
	}
	if node.LastSeen.IsZero() {
		node.LastSeen = now
	}

	query := store.Rebind(s.driver, `
		INSERT INTO graph_nodes (id, type, component_id, metadata, first_seen, last_seen)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			type = excluded.type,
			component_id = excluded.component_id,
			metadata = excluded.metadata,
			last_seen = excluded.last_seen
	`)
	_, err = s.db.ExecContext(ctx, query,
		node.ID, node.Type, node.ComponentID, string(metadata),
		node.FirstSeen, node.LastSeen,
	)
	if err != nil {
		return fmt.Errorf("upsert node %s: %w", node.ID, err)
	}
	return nil
}

func (s *sqlGraphStore) AddEdge(ctx context.Context, edge Edge) (err error) {
	start := time.Now()
	defer func() { s.metrics.RecordGraphOperation(ctx, "add_edge", time.Since(start), err) }()

	if edge.CreatedAt.IsZero() {
		edge.CreatedAt = time.Now()
	}

	query := store.Rebind(s.driver, `
		INSERT INTO graph_edges (from_node, to_node, relation, confidence, source, context, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(from_node, to_node, relation) DO UPDATE SET
			confidence = excluded.confidence,
			source = excluded.source,
			context = excluded.context
	`)
	_, err = s.db.ExecContext(ctx, query,
		edge.From, edge.To, edge.Relation,
		int(edge.Confidence), edge.Source, edge.Context, edge.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert edge %s->%s (%s): %w", edge.From, edge.To, edge.Relation, err)
	}
	return nil
}

func (s *sqlGraphStore) GetNode(ctx context.Context, id string) (node *Node, err error) {
	start := time.Now()
	defer func() { s.metrics.RecordGraphOperation(ctx, "get_node", time.Since(start), err) }()

	query := store.Rebind(s.driver, `
		SELECT id, type, component_id, metadata, first_seen, last_seen
		FROM graph_nodes WHERE id = ?
	`)
	var result Node
	var metadataStr string
	var sourceID sql.NullString

	err = s.db.QueryRowContext(ctx, query, id).Scan(
		&result.ID, &result.Type, &sourceID, &metadataStr,
		&result.FirstSeen, &result.LastSeen,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNodeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query node %s: %w", id, err)
	}

	if sourceID.Valid {
		result.ComponentID = sourceID.String
	}

	if err := json.Unmarshal([]byte(metadataStr), &result.Metadata); err != nil {
		slog.Warn("failed to unmarshal node metadata", "node_id", result.ID, "error", err)
		result.Metadata = map[string]any{}
	}

	return &result, nil
}

func (s *sqlGraphStore) Query(ctx context.Context, query string) (nodes []Node, err error) {
	start := time.Now()
	defer func() { s.metrics.RecordGraphOperation(ctx, "query", time.Since(start), err) }()

	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	var sqlQuery string
	var args []any

	// Support "type:deployment" syntax for filtering by type
	if strings.HasPrefix(query, "type:") {
		nodeType := strings.TrimPrefix(query, "type:")
		sqlQuery = store.Rebind(s.driver, `
			SELECT id, type, component_id, metadata, first_seen, last_seen
			FROM graph_nodes WHERE type = ? ORDER BY id
		`)
		args = []any{nodeType}
	} else {
		// Search by ID pattern or metadata content
		pattern := "%" + query + "%"
		sqlQuery = store.Rebind(s.driver, `
			SELECT id, type, component_id, metadata, first_seen, last_seen
			FROM graph_nodes
			WHERE id LIKE ? OR type LIKE ? OR metadata LIKE ?
			ORDER BY id
		`)
		args = []any{pattern, pattern, pattern}
	}

	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("query graph nodes: %w", err)
	}
	defer rows.Close()

	return scanNodes(rows)
}

func (s *sqlGraphStore) Related(ctx context.Context, nodeID string, depth int) (subgraph *Subgraph, err error) {
	start := time.Now()
	defer func() { s.metrics.RecordGraphOperation(ctx, "related", time.Since(start), err) }()

	if depth <= 0 {
		// Just return the single node if it exists
		node, err := s.GetNode(ctx, nodeID)
		if err != nil {
			return nil, err
		}
		return &Subgraph{Nodes: []Node{*node}}, nil
	}

	// Bidirectional recursive CTE to find all connected nodes within depth
	nodesQuery := store.Rebind(s.driver, `
		WITH RECURSIVE connected(node_id, d) AS (
			SELECT ?, 0
			UNION
			SELECT CASE
				WHEN e.from_node = c.node_id THEN e.to_node
				ELSE e.from_node
			END, c.d + 1
			FROM graph_edges e
			JOIN connected c ON (e.from_node = c.node_id OR e.to_node = c.node_id)
			WHERE c.d < ?
		)
		SELECT DISTINCT n.id, n.type, n.component_id, n.metadata, n.first_seen, n.last_seen
		FROM graph_nodes n
		JOIN connected c ON n.id = c.node_id
		ORDER BY n.id
	`)

	rows, err := s.db.QueryContext(ctx, nodesQuery, nodeID, depth)
	if err != nil {
		return nil, fmt.Errorf("query related nodes: %w", err)
	}
	defer rows.Close()

	nodes, err := scanNodes(rows)
	if err != nil {
		return nil, err
	}

	if len(nodes) == 0 {
		return nil, ErrNodeNotFound
	}

	// Collect node IDs for edge query
	nodeIDs := make([]string, len(nodes))
	for i, n := range nodes {
		nodeIDs[i] = n.ID
	}

	edges, err := s.edgesBetween(ctx, nodeIDs)
	if err != nil {
		return nil, err
	}

	return &Subgraph{Nodes: nodes, Edges: edges}, nil
}

func (s *sqlGraphStore) Path(ctx context.Context, from, to string) (edges []Edge, err error) {
	start := time.Now()
	defer func() { s.metrics.RecordGraphOperation(ctx, "path", time.Since(start), err) }()

	// Recursive CTE to find a path from source to target.
	// We track the path as a comma-separated string of node IDs to avoid cycles.

	pathQuery := store.Rebind(s.driver, `
		WITH RECURSIVE pathfinder(current, target, path, depth) AS (
			SELECT ?, ?, ',' || ? || ',', 0
			UNION ALL
			SELECT
				CASE WHEN e.from_node = p.current THEN e.to_node ELSE e.from_node END,
				p.target,
				p.path || CASE WHEN e.from_node = p.current THEN e.to_node ELSE e.from_node END || ',',
				p.depth + 1
			FROM graph_edges e
			JOIN pathfinder p ON (e.from_node = p.current OR e.to_node = p.current)
			WHERE p.depth < 10
				AND p.path NOT LIKE '%,' || CASE WHEN e.from_node = p.current THEN e.to_node ELSE e.from_node END || ',%'
				AND p.current != p.target
		)
		SELECT path FROM pathfinder
		WHERE current = target
		ORDER BY depth
		LIMIT 1
	`)

	var pathStr string
	err = s.db.QueryRowContext(ctx, pathQuery, from, to, from).Scan(&pathStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil // No path found
	}
	if err != nil {
		return nil, fmt.Errorf("find path %s->%s: %w", from, to, err)
	}

	// Parse path string into node sequence and fetch edges along the path.
	// The path format uses delimiter-wrapped IDs: ",from,node1,to," — trim edges.
	pathNodes := strings.Split(strings.Trim(pathStr, ","), ",")
	edges = make([]Edge, 0, len(pathNodes))
	for i := 0; i < len(pathNodes)-1; i++ {
		edge, err := s.getEdgeBetween(ctx, pathNodes[i], pathNodes[i+1])
		if err != nil {
			return nil, err
		}
		if edge != nil {
			edges = append(edges, *edge)
		}
	}

	return edges, nil
}

func (s *sqlGraphStore) DeleteNode(ctx context.Context, id string) (err error) {
	start := time.Now()
	defer func() { s.metrics.RecordGraphOperation(ctx, "delete_node", time.Since(start), err) }()

	result, err := s.db.ExecContext(ctx, store.Rebind(s.driver, "DELETE FROM graph_nodes WHERE id = ?"), id)
	if err != nil {
		return fmt.Errorf("delete node %s: %w", id, err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNodeNotFound
	}
	return nil
}

// DeleteNodesByComponentTx removes every graph_nodes row carrying componentID
// against the caller-supplied transaction. Component deletion cascades graph
// state through here — the component delete handler invokes it inside the same
// mutateWithAudit transaction as Components.DeleteTx and the audit insert, so
// the graph rows die atomically with the component row (or all three roll back
// together). graph_edges rows die with their endpoint nodes via the migration-002
// FK ON DELETE CASCADE, which fires on the transaction's connection because
// foreign_keys=1 is DSN-encoded on every pooled connection (internal/store);
// no explicit edge delete is needed. There is intentionally NO pooled
// (non-Tx) sibling: the only caller is the audited delete transaction.
func (s *sqlGraphStore) DeleteNodesByComponentTx(ctx context.Context, tx *sql.Tx, componentID string) (err error) {
	start := time.Now()
	defer func() { s.metrics.RecordGraphOperation(ctx, "delete_nodes_by_component_tx", time.Since(start), err) }()
	return s.deleteNodesByComponent(ctx, tx, componentID)
}

// deleteNodesByComponent is the shared body, taking an execContext so the SQL
// cannot drift from any future pooled variant.
func (s *sqlGraphStore) deleteNodesByComponent(ctx context.Context, exec execContext, componentID string) error {
	_, err := exec.ExecContext(ctx, store.Rebind(s.driver, "DELETE FROM graph_nodes WHERE component_id = ?"), componentID)
	if err != nil {
		return fmt.Errorf("delete graph nodes for component %s: %w", componentID, err)
	}
	return nil
}

func (s *sqlGraphStore) DeleteEdge(ctx context.Context, from, to, relation string) (err error) {
	start := time.Now()
	defer func() { s.metrics.RecordGraphOperation(ctx, "delete_edge", time.Since(start), err) }()

	_, err = s.db.ExecContext(ctx,
		store.Rebind(s.driver, "DELETE FROM graph_edges WHERE from_node = ? AND to_node = ? AND relation = ?"),
		from, to, relation,
	)
	if err != nil {
		return fmt.Errorf("delete edge %s->%s (%s): %w", from, to, relation, err)
	}
	return nil
}

func (s *sqlGraphStore) Summary(ctx context.Context) (summary GraphSummary, err error) {
	start := time.Now()
	defer func() { s.metrics.RecordGraphOperation(ctx, "summary", time.Since(start), err) }()

	// Node count
	err = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM graph_nodes").Scan(&summary.NodeCount)
	if err != nil {
		return summary, fmt.Errorf("count nodes: %w", err)
	}

	// Edge count
	err = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM graph_edges").Scan(&summary.EdgeCount)
	if err != nil {
		return summary, fmt.Errorf("count edges: %w", err)
	}

	// Nodes by type
	summary.NodesByType = map[string]int{}
	rows, err := s.db.QueryContext(ctx,
		"SELECT type, COUNT(*) FROM graph_nodes GROUP BY type ORDER BY COUNT(*) DESC",
	)
	if err != nil {
		return summary, fmt.Errorf("count nodes by type: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var nodeType string
		var count int
		if err := rows.Scan(&nodeType, &count); err != nil {
			return summary, fmt.Errorf("scan node type count: %w", err)
		}
		summary.NodesByType[nodeType] = count
	}
	if err := rows.Err(); err != nil {
		return summary, fmt.Errorf("iterate node type counts: %w", err)
	}

	// Recently added (by first_seen, last 10)
	recentRows, err := s.db.QueryContext(ctx, `
		SELECT id, type, component_id, metadata, first_seen, last_seen
		FROM graph_nodes ORDER BY first_seen DESC LIMIT 10
	`)
	if err != nil {
		return summary, fmt.Errorf("query recently added: %w", err)
	}
	defer recentRows.Close()

	summary.RecentlyAdded, err = scanNodes(recentRows)
	if err != nil {
		return summary, err
	}

	// Recently updated (by last_seen, last 10)
	updatedRows, err := s.db.QueryContext(ctx, `
		SELECT id, type, component_id, metadata, first_seen, last_seen
		FROM graph_nodes ORDER BY last_seen DESC LIMIT 10
	`)
	if err != nil {
		return summary, fmt.Errorf("query recently updated: %w", err)
	}
	defer updatedRows.Close()

	summary.RecentlyUpdated, err = scanNodes(updatedRows)
	if err != nil {
		return summary, err
	}

	return summary, nil
}

// ListNodesByComponent returns all nodes for a given component_id.
func (s *sqlGraphStore) ListNodesByComponent(ctx context.Context, sourceID string) (nodes []Node, err error) {
	start := time.Now()
	defer func() { s.metrics.RecordGraphOperation(ctx, "list_nodes_by_component", time.Since(start), err) }()

	query := store.Rebind(s.driver, `
		SELECT id, type, component_id, metadata, first_seen, last_seen
		FROM graph_nodes WHERE component_id = ? ORDER BY id
	`)
	rows, err := s.db.QueryContext(ctx, query, sourceID)
	if err != nil {
		return nil, fmt.Errorf("query nodes by component %s: %w", sourceID, err)
	}
	defer rows.Close()

	return scanNodes(rows)
}

// ListComponentBindings returns the edges binding componentID to other
// components. See the interface doc in store.go for the contract; the SQL below
// is the whole of it.
//
// The two arms of the UNION are the two directions. They are separate arms
// rather than one OR-ed query because each arm joins near/far to the opposite
// endpoint column, so the index on from_node and the index on to_node are each
// usable; an OR across both would not be.
func (s *sqlGraphStore) ListComponentBindings(ctx context.Context, componentID string, limit int) (bindings []ComponentBinding, err error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordGraphOperation(ctx, "list_component_bindings", time.Since(start), err)
	}()

	if componentID == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = DefaultComponentBindingLimit
	}

	query := store.Rebind(s.driver, `
		SELECT relation, confidence, direction, node_id, node_type,
		       peer_node_id, peer_node_type, peer_component_id
		FROM (
			SELECT e.relation AS relation, e.confidence AS confidence, 'out' AS direction,
			       near.id AS node_id, near.type AS node_type,
			       far.id AS peer_node_id, far.type AS peer_node_type,
			       far.component_id AS peer_component_id
			FROM graph_edges e
			JOIN graph_nodes near ON near.id = e.from_node
			JOIN graph_nodes far ON far.id = e.to_node
			WHERE near.component_id = ?
			  AND far.component_id IS NOT NULL
			  AND far.component_id <> ''
			  AND far.component_id <> ?
			UNION ALL
			SELECT e.relation AS relation, e.confidence AS confidence, 'in' AS direction,
			       near.id AS node_id, near.type AS node_type,
			       far.id AS peer_node_id, far.type AS peer_node_type,
			       far.component_id AS peer_component_id
			FROM graph_edges e
			JOIN graph_nodes near ON near.id = e.to_node
			JOIN graph_nodes far ON far.id = e.from_node
			WHERE near.component_id = ?
			  AND far.component_id IS NOT NULL
			  AND far.component_id <> ''
			  AND far.component_id <> ?
		) AS b
		ORDER BY relation, peer_component_id, peer_node_id, node_id, direction
		LIMIT ?
	`)

	rows, err := s.db.QueryContext(ctx, query,
		componentID, componentID, componentID, componentID, limit)
	if err != nil {
		return nil, fmt.Errorf("query component bindings for %s: %w", componentID, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			b          ComponentBinding
			confidence sql.NullInt64
		)
		if err := rows.Scan(
			&b.Relation, &confidence, &b.Direction, &b.NodeID, &b.NodeType,
			&b.PeerNodeID, &b.PeerNodeType, &b.PeerComponentID,
		); err != nil {
			return nil, fmt.Errorf("scan component binding: %w", err)
		}
		b.Confidence = ConfidenceLevel(confidence.Int64)
		bindings = append(bindings, b)
	}
	return bindings, rows.Err()
}

// ListEdgesForNodes returns edges where both endpoints are in nodeIDs.
func (s *sqlGraphStore) ListEdgesForNodes(ctx context.Context, nodeIDs []string) (edges []Edge, err error) {
	start := time.Now()
	defer func() { s.metrics.RecordGraphOperation(ctx, "list_edges_for_nodes", time.Since(start), err) }()

	return s.edgesBetween(ctx, nodeIDs)
}

// edgesBetween returns all edges where both endpoints are in the given node ID set.
func (s *sqlGraphStore) edgesBetween(ctx context.Context, nodeIDs []string) ([]Edge, error) {
	if len(nodeIDs) == 0 {
		return nil, nil
	}

	placeholders := strings.Repeat("?,", len(nodeIDs))
	placeholders = placeholders[:len(placeholders)-1] // trim trailing comma

	// We need nodeIDs twice: once for from_node IN (...) and once for to_node IN (...)
	args := make([]any, 0, len(nodeIDs)*2)
	for _, id := range nodeIDs {
		args = append(args, id)
	}
	for _, id := range nodeIDs {
		args = append(args, id)
	}

	query := store.Rebind(s.driver, fmt.Sprintf(`
		SELECT from_node, to_node, relation, confidence, source, context, created_at
		FROM graph_edges
		WHERE from_node IN (%s) AND to_node IN (%s)
		ORDER BY from_node, to_node
	`, placeholders, placeholders))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query edges between nodes: %w", err)
	}
	defer rows.Close()

	return scanEdges(rows)
}

// getEdgeBetween returns the first edge connecting two adjacent nodes (in either direction).
func (s *sqlGraphStore) getEdgeBetween(ctx context.Context, a, b string) (*Edge, error) {
	query := store.Rebind(s.driver, `
		SELECT from_node, to_node, relation, confidence, source, context, created_at
		FROM graph_edges
		WHERE (from_node = ? AND to_node = ?) OR (from_node = ? AND to_node = ?)
		LIMIT 1
	`)
	var edge Edge
	var confidence int
	var source, edgeContext sql.NullString

	err := s.db.QueryRowContext(ctx, query, a, b, b, a).Scan(
		&edge.From, &edge.To, &edge.Relation,
		&confidence, &source, &edgeContext, &edge.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query edge between %s and %s: %w", a, b, err)
	}

	edge.Confidence = ConfidenceLevel(confidence)
	if source.Valid {
		edge.Source = source.String
	}
	if edgeContext.Valid {
		edge.Context = edgeContext.String
	}

	return &edge, nil
}

// ListAll returns all nodes and edges in the graph.
func (s *sqlGraphStore) ListAll(ctx context.Context) (result *Subgraph, err error) {
	start := time.Now()
	defer func() { s.metrics.RecordGraphOperation(ctx, "list_all", time.Since(start), err) }()

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, component_id, metadata, first_seen, last_seen
		FROM graph_nodes
		ORDER BY id
		LIMIT 5000
	`)
	if err != nil {
		return nil, fmt.Errorf("list all nodes: %w", err)
	}
	defer rows.Close()

	nodes, err := scanNodes(rows)
	if err != nil {
		return nil, err
	}

	if len(nodes) == 0 {
		return &Subgraph{Nodes: []Node{}, Edges: []Edge{}}, nil
	}

	nodeIDs := make([]string, len(nodes))
	for i, n := range nodes {
		nodeIDs[i] = n.ID
	}

	edges, err := s.edgesBetween(ctx, nodeIDs)
	if err != nil {
		return nil, err
	}
	if edges == nil {
		edges = []Edge{}
	}

	return &Subgraph{Nodes: nodes, Edges: edges}, nil
}

func scanNodes(rows *sql.Rows) ([]Node, error) {
	var nodes []Node
	for rows.Next() {
		var node Node
		var metadataStr string
		var sourceID sql.NullString

		if err := rows.Scan(
			&node.ID, &node.Type, &sourceID, &metadataStr,
			&node.FirstSeen, &node.LastSeen,
		); err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}

		if sourceID.Valid {
			node.ComponentID = sourceID.String
		}

		if err := json.Unmarshal([]byte(metadataStr), &node.Metadata); err != nil {
			slog.Warn("failed to unmarshal node metadata", "node_id", node.ID, "error", err)
			node.Metadata = map[string]any{}
		}

		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

func scanEdges(rows *sql.Rows) ([]Edge, error) {
	var edges []Edge
	for rows.Next() {
		var edge Edge
		var confidence int
		var source, edgeContext sql.NullString

		if err := rows.Scan(
			&edge.From, &edge.To, &edge.Relation,
			&confidence, &source, &edgeContext, &edge.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan edge: %w", err)
		}

		edge.Confidence = ConfidenceLevel(confidence)
		if source.Valid {
			edge.Source = source.String
		}
		if edgeContext.Valid {
			edge.Context = edgeContext.String
		}

		edges = append(edges, edge)
	}
	return edges, rows.Err()
}
