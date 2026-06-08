package graph_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/jaimegago/joe/internal/graph"
	_ "modernc.org/sqlite"
)

// setupTestStoreWithDB returns both the GraphStore and the raw *sql.DB,
// allowing tests to inject raw rows (e.g., invalid JSON) to cover error paths.
func setupTestStoreWithDB(t *testing.T) (graph.GraphStore, *sql.DB) {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE graph_nodes (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			component_id TEXT,
			metadata TEXT DEFAULT '{}',
			first_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			last_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX idx_graph_nodes_type ON graph_nodes(type);
		CREATE INDEX idx_graph_nodes_source ON graph_nodes(component_id);

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
	`)
	if err != nil {
		t.Fatalf("create tables: %v", err)
	}

	t.Cleanup(func() { db.Close() })
	return graph.NewSQLiteStore(db, nil), db
}

func setupTestStore(t *testing.T) graph.GraphStore {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	// Create graph tables (same as migration 002)
	_, err = db.Exec(`
		CREATE TABLE graph_nodes (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			component_id TEXT,
			metadata TEXT DEFAULT '{}',
			first_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			last_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX idx_graph_nodes_type ON graph_nodes(type);
		CREATE INDEX idx_graph_nodes_source ON graph_nodes(component_id);

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
	`)
	if err != nil {
		t.Fatalf("create tables: %v", err)
	}

	t.Cleanup(func() { db.Close() })
	return graph.NewSQLiteStore(db, nil)
}

// seedGraph creates a small test graph:
//
//	payment-svc --calls--> user-svc --depends_on--> postgres
//	payment-svc --depends_on--> redis
func seedGraph(t *testing.T, store graph.GraphStore) {
	t.Helper()
	ctx := context.Background()

	nodes := []graph.Node{
		{ID: "deployment/prod/payment-svc", Type: "deployment", ComponentID: "k8s/prod", Metadata: map[string]any{"replicas": float64(3)}},
		{ID: "deployment/prod/user-svc", Type: "deployment", ComponentID: "k8s/prod", Metadata: map[string]any{"replicas": float64(2)}},
		{ID: "statefulset/prod/postgres", Type: "statefulset", ComponentID: "k8s/prod", Metadata: map[string]any{"version": "15"}},
		{ID: "deployment/prod/redis", Type: "deployment", ComponentID: "k8s/prod", Metadata: map[string]any{}},
	}
	for _, n := range nodes {
		if err := store.AddNode(ctx, n); err != nil {
			t.Fatalf("AddNode(%s): %v", n.ID, err)
		}
	}

	edges := []graph.Edge{
		{From: "deployment/prod/payment-svc", To: "deployment/prod/user-svc", Relation: "calls", Confidence: graph.Explicit, Source: "k8s_api"},
		{From: "deployment/prod/user-svc", To: "statefulset/prod/postgres", Relation: "depends_on", Confidence: graph.Explicit, Source: "joe_file"},
		{From: "deployment/prod/payment-svc", To: "deployment/prod/redis", Relation: "depends_on", Confidence: graph.Inferred, Source: "llm"},
	}
	for _, e := range edges {
		if err := store.AddEdge(ctx, e); err != nil {
			t.Fatalf("AddEdge(%s->%s): %v", e.From, e.To, err)
		}
	}
}

func TestAddNode(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	t.Run("add new node", func(t *testing.T) {
		err := store.AddNode(ctx, graph.Node{
			ID:          "deployment/prod/api",
			Type:        "deployment",
			ComponentID: "k8s/prod",
			Metadata:    map[string]any{"replicas": float64(3)},
		})
		if err != nil {
			t.Fatalf("AddNode() error = %v", err)
		}

		node, err := store.GetNode(ctx, "deployment/prod/api")
		if err != nil {
			t.Fatalf("GetNode() error = %v", err)
		}
		if node.Type != "deployment" {
			t.Errorf("Type = %q, want %q", node.Type, "deployment")
		}
		if node.ComponentID != "k8s/prod" {
			t.Errorf("ComponentID = %q, want %q", node.ComponentID, "k8s/prod")
		}
		replicas, ok := node.Metadata["replicas"]
		if !ok || replicas != float64(3) {
			t.Errorf("Metadata[replicas] = %v, want 3", replicas)
		}
	})

	t.Run("upsert updates existing node", func(t *testing.T) {
		err := store.AddNode(ctx, graph.Node{
			ID:          "deployment/prod/api",
			Type:        "deployment",
			ComponentID: "k8s/prod",
			Metadata:    map[string]any{"replicas": float64(5)},
		})
		if err != nil {
			t.Fatalf("AddNode() upsert error = %v", err)
		}

		node, _ := store.GetNode(ctx, "deployment/prod/api")
		replicas := node.Metadata["replicas"]
		if replicas != float64(5) {
			t.Errorf("Metadata[replicas] after upsert = %v, want 5", replicas)
		}
	})

	t.Run("nil metadata defaults to empty", func(t *testing.T) {
		err := store.AddNode(ctx, graph.Node{
			ID:   "service/prod/ingress",
			Type: "service",
		})
		if err != nil {
			t.Fatalf("AddNode() error = %v", err)
		}

		node, _ := store.GetNode(ctx, "service/prod/ingress")
		if node.Metadata == nil {
			t.Error("Metadata should not be nil")
		}
	})
}

func TestAddEdge_NewRelations(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	nodes := []graph.Node{
		{ID: "service/prod/api", Type: "service", ComponentID: "k8s/prod", Metadata: map[string]any{}},
		{ID: "observability/prom", Type: "prometheus", ComponentID: "obs-1", Metadata: map[string]any{}},
	}
	for _, node := range nodes {
		if err := store.AddNode(ctx, node); err != nil {
			t.Fatalf("AddNode(%s): %v", node.ID, err)
		}
	}

	relations := []string{
		graph.RelationMetricsIn,
		graph.RelationLogsIn,
		graph.RelationTracesIn,
		graph.RelationAlertsIn,
		graph.RelationPagedVia,
		graph.RelationDashboardIn,
		graph.RelationIsK8sNode,
	}
	for _, relation := range relations {
		edge := graph.Edge{
			From:        nodes[0].ID,
			To:          nodes[1].ID,
			Relation:    relation,
			Confidence:  graph.Explicit,
			Source:      "test",
			ComponentID: "",
		}
		if err := store.AddEdge(ctx, edge); err != nil {
			t.Fatalf("AddEdge(%s): %v", relation, err)
		}
	}
}

func TestGetNode(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	t.Run("nonexistent returns ErrNodeNotFound", func(t *testing.T) {
		_, err := store.GetNode(ctx, "nonexistent")
		if !errors.Is(err, graph.ErrNodeNotFound) {
			t.Errorf("GetNode() error = %v, want ErrNodeNotFound", err)
		}
	})
}

func TestAddEdge(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Add nodes first
	store.AddNode(ctx, graph.Node{ID: "svc-a", Type: "service"})
	store.AddNode(ctx, graph.Node{ID: "svc-b", Type: "service"})

	t.Run("add edge", func(t *testing.T) {
		err := store.AddEdge(ctx, graph.Edge{
			From:        "svc-a",
			To:          "svc-b",
			Relation:    "calls",
			Confidence:  graph.Explicit,
			Source:      "k8s_api",
			ComponentID: "",
			Context:     "network traffic",
		})
		if err != nil {
			t.Fatalf("AddEdge() error = %v", err)
		}
	})

	t.Run("upsert edge updates confidence", func(t *testing.T) {
		err := store.AddEdge(ctx, graph.Edge{
			From:        "svc-a",
			To:          "svc-b",
			Relation:    "calls",
			Confidence:  graph.UserConfirmed,
			Source:      "user",
			ComponentID: "",
		})
		if err != nil {
			t.Fatalf("AddEdge() upsert error = %v", err)
		}
	})

	t.Run("foreign key enforced", func(t *testing.T) {
		err := store.AddEdge(ctx, graph.Edge{
			From:        "svc-a",
			To:          "nonexistent",
			Relation:    "calls",
			ComponentID: "",
		})
		if err == nil {
			t.Error("expected foreign key error, got nil")
		}
	})
}

func TestQuery(t *testing.T) {
	store := setupTestStore(t)
	seedGraph(t, store)
	ctx := context.Background()

	t.Run("query by type prefix", func(t *testing.T) {
		nodes, err := store.Query(ctx, "type:deployment")
		if err != nil {
			t.Fatalf("Query() error = %v", err)
		}
		if len(nodes) != 3 {
			t.Errorf("Query(type:deployment) returned %d nodes, want 3", len(nodes))
		}
	})

	t.Run("query by type prefix statefulset", func(t *testing.T) {
		nodes, err := store.Query(ctx, "type:statefulset")
		if err != nil {
			t.Fatalf("Query() error = %v", err)
		}
		if len(nodes) != 1 {
			t.Errorf("Query(type:statefulset) returned %d nodes, want 1", len(nodes))
		}
	})

	t.Run("query by id substring", func(t *testing.T) {
		nodes, err := store.Query(ctx, "payment")
		if err != nil {
			t.Fatalf("Query() error = %v", err)
		}
		if len(nodes) != 1 {
			t.Errorf("Query(payment) returned %d nodes, want 1", len(nodes))
		}
		if nodes[0].ID != "deployment/prod/payment-svc" {
			t.Errorf("ID = %q, want deployment/prod/payment-svc", nodes[0].ID)
		}
	})

	t.Run("query by metadata", func(t *testing.T) {
		nodes, err := store.Query(ctx, "15")
		if err != nil {
			t.Fatalf("Query() error = %v", err)
		}
		// postgres has version "15" in metadata
		found := false
		for _, n := range nodes {
			if n.ID == "statefulset/prod/postgres" {
				found = true
			}
		}
		if !found {
			t.Error("expected postgres node in results for query '15'")
		}
	})

	t.Run("empty query returns nil", func(t *testing.T) {
		nodes, err := store.Query(ctx, "")
		if err != nil {
			t.Fatalf("Query() error = %v", err)
		}
		if nodes != nil {
			t.Errorf("Query('') = %v, want nil", nodes)
		}
	})

	t.Run("no matches returns empty", func(t *testing.T) {
		nodes, err := store.Query(ctx, "zzz_no_match")
		if err != nil {
			t.Fatalf("Query() error = %v", err)
		}
		if len(nodes) != 0 {
			t.Errorf("Query(zzz_no_match) returned %d nodes, want 0", len(nodes))
		}
	})
}

func TestRelated(t *testing.T) {
	store := setupTestStore(t)
	seedGraph(t, store)
	ctx := context.Background()

	t.Run("depth 1 from payment-svc", func(t *testing.T) {
		sg, err := store.Related(ctx, "deployment/prod/payment-svc", 1)
		if err != nil {
			t.Fatalf("Related() error = %v", err)
		}
		// payment-svc + user-svc + redis = 3 nodes
		if len(sg.Nodes) != 3 {
			t.Errorf("Related() returned %d nodes, want 3", len(sg.Nodes))
			for _, n := range sg.Nodes {
				t.Logf("  node: %s", n.ID)
			}
		}
		if len(sg.Edges) < 2 {
			t.Errorf("Related() returned %d edges, want >= 2", len(sg.Edges))
		}
	})

	t.Run("depth 2 from payment-svc reaches postgres", func(t *testing.T) {
		sg, err := store.Related(ctx, "deployment/prod/payment-svc", 2)
		if err != nil {
			t.Fatalf("Related() error = %v", err)
		}
		// payment-svc + user-svc + redis + postgres = 4 nodes
		if len(sg.Nodes) != 4 {
			t.Errorf("Related() returned %d nodes, want 4", len(sg.Nodes))
			for _, n := range sg.Nodes {
				t.Logf("  node: %s", n.ID)
			}
		}
	})

	t.Run("depth 0 returns single node", func(t *testing.T) {
		sg, err := store.Related(ctx, "deployment/prod/payment-svc", 0)
		if err != nil {
			t.Fatalf("Related() error = %v", err)
		}
		if len(sg.Nodes) != 1 {
			t.Errorf("Related(depth=0) returned %d nodes, want 1", len(sg.Nodes))
		}
	})

	t.Run("nonexistent node returns error", func(t *testing.T) {
		_, err := store.Related(ctx, "nonexistent", 1)
		if !errors.Is(err, graph.ErrNodeNotFound) {
			t.Errorf("Related(nonexistent) error = %v, want ErrNodeNotFound", err)
		}
	})

	t.Run("bidirectional traversal", func(t *testing.T) {
		// Start from user-svc and traverse depth 1 — should find payment-svc (incoming edge) and postgres (outgoing)
		sg, err := store.Related(ctx, "deployment/prod/user-svc", 1)
		if err != nil {
			t.Fatalf("Related() error = %v", err)
		}
		// user-svc + payment-svc + postgres = 3 nodes
		if len(sg.Nodes) != 3 {
			t.Errorf("Related(user-svc, 1) returned %d nodes, want 3", len(sg.Nodes))
			for _, n := range sg.Nodes {
				t.Logf("  node: %s", n.ID)
			}
		}
	})
}

func TestPath(t *testing.T) {
	store := setupTestStore(t)
	seedGraph(t, store)
	ctx := context.Background()

	t.Run("direct path", func(t *testing.T) {
		edges, err := store.Path(ctx, "deployment/prod/payment-svc", "deployment/prod/user-svc")
		if err != nil {
			t.Fatalf("Path() error = %v", err)
		}
		if len(edges) != 1 {
			t.Errorf("Path() returned %d edges, want 1", len(edges))
		}
	})

	t.Run("two-hop path", func(t *testing.T) {
		edges, err := store.Path(ctx, "deployment/prod/payment-svc", "statefulset/prod/postgres")
		if err != nil {
			t.Fatalf("Path() error = %v", err)
		}
		if len(edges) != 2 {
			t.Errorf("Path() returned %d edges, want 2", len(edges))
		}
	})

	t.Run("no path returns nil", func(t *testing.T) {
		// Add a disconnected node
		store.AddNode(ctx, graph.Node{ID: "isolated", Type: "service"})

		edges, err := store.Path(ctx, "deployment/prod/payment-svc", "isolated")
		if err != nil {
			t.Fatalf("Path() error = %v", err)
		}
		if edges != nil {
			t.Errorf("Path() to disconnected node = %v, want nil", edges)
		}
	})
}

func TestPath_SubstringNodeIDs(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Create nodes where one ID is a substring of another: "pod" and "pod-abc"
	for _, n := range []graph.Node{
		{ID: "pod", Type: "service"},
		{ID: "pod-abc", Type: "service"},
		{ID: "target", Type: "service"},
	} {
		if err := store.AddNode(ctx, n); err != nil {
			t.Fatalf("AddNode(%s): %v", n.ID, err)
		}
	}

	// Create a path: pod -> pod-abc -> target
	for _, e := range []graph.Edge{
		{From: "pod", To: "pod-abc", Relation: "connects_to"},
		{From: "pod-abc", To: "target", Relation: "connects_to"},
	} {
		if err := store.AddEdge(ctx, e); err != nil {
			t.Fatalf("AddEdge(%s->%s): %v", e.From, e.To, err)
		}
	}

	// Path from "pod" to "target" should traverse through "pod-abc"
	// Without delimiter-based cycle detection, "pod-abc" would falsely match "pod" in the path
	edges, err := store.Path(ctx, "pod", "target")
	if err != nil {
		t.Fatalf("Path() error = %v", err)
	}
	if len(edges) != 2 {
		t.Errorf("Path() returned %d edges, want 2 (pod->pod-abc->target)", len(edges))
	}
}

func TestDeleteNode(t *testing.T) {
	store := setupTestStore(t)
	seedGraph(t, store)
	ctx := context.Background()

	t.Run("delete cascades edges", func(t *testing.T) {
		err := store.DeleteNode(ctx, "deployment/prod/user-svc")
		if err != nil {
			t.Fatalf("DeleteNode() error = %v", err)
		}

		_, err = store.GetNode(ctx, "deployment/prod/user-svc")
		if !errors.Is(err, graph.ErrNodeNotFound) {
			t.Errorf("GetNode after delete error = %v, want ErrNodeNotFound", err)
		}

		// Edges involving user-svc should be gone
		sg, _ := store.Related(ctx, "deployment/prod/payment-svc", 1)
		for _, e := range sg.Edges {
			if e.To == "deployment/prod/user-svc" || e.From == "deployment/prod/user-svc" {
				t.Error("found edge to deleted node")
			}
		}
	})

	t.Run("delete nonexistent returns error", func(t *testing.T) {
		err := store.DeleteNode(ctx, "nonexistent")
		if !errors.Is(err, graph.ErrNodeNotFound) {
			t.Errorf("DeleteNode(nonexistent) error = %v, want ErrNodeNotFound", err)
		}
	})
}

func TestDeleteEdge(t *testing.T) {
	store := setupTestStore(t)
	seedGraph(t, store)
	ctx := context.Background()

	t.Run("delete specific edge", func(t *testing.T) {
		err := store.DeleteEdge(ctx, "deployment/prod/payment-svc", "deployment/prod/redis", "depends_on")
		if err != nil {
			t.Fatalf("DeleteEdge() error = %v", err)
		}

		// payment-svc should only have 1 edge now (calls -> user-svc)
		sg, _ := store.Related(ctx, "deployment/prod/payment-svc", 1)
		// payment-svc + user-svc = 2 nodes (redis no longer connected)
		if len(sg.Nodes) != 2 {
			t.Errorf("After edge delete, Related() returned %d nodes, want 2", len(sg.Nodes))
			for _, n := range sg.Nodes {
				t.Logf("  node: %s", n.ID)
			}
		}
	})

	t.Run("delete nonexistent edge is no-op", func(t *testing.T) {
		err := store.DeleteEdge(ctx, "a", "b", "c")
		if err != nil {
			t.Errorf("DeleteEdge(nonexistent) error = %v, want nil", err)
		}
	})
}

func TestSummary(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	t.Run("empty graph", func(t *testing.T) {
		summary, err := store.Summary(ctx)
		if err != nil {
			t.Fatalf("Summary() error = %v", err)
		}
		if summary.NodeCount != 0 {
			t.Errorf("NodeCount = %d, want 0", summary.NodeCount)
		}
		if summary.EdgeCount != 0 {
			t.Errorf("EdgeCount = %d, want 0", summary.EdgeCount)
		}
	})

	seedGraph(t, store)

	t.Run("populated graph", func(t *testing.T) {
		summary, err := store.Summary(ctx)
		if err != nil {
			t.Fatalf("Summary() error = %v", err)
		}
		if summary.NodeCount != 4 {
			t.Errorf("NodeCount = %d, want 4", summary.NodeCount)
		}
		if summary.EdgeCount != 3 {
			t.Errorf("EdgeCount = %d, want 3", summary.EdgeCount)
		}
		if summary.NodesByType["deployment"] != 3 {
			t.Errorf("NodesByType[deployment] = %d, want 3", summary.NodesByType["deployment"])
		}
		if summary.NodesByType["statefulset"] != 1 {
			t.Errorf("NodesByType[statefulset] = %d, want 1", summary.NodesByType["statefulset"])
		}
		if len(summary.RecentlyAdded) == 0 {
			t.Error("RecentlyAdded should not be empty")
		}
		if len(summary.RecentlyUpdated) == 0 {
			t.Error("RecentlyUpdated should not be empty")
		}
	})
}

func TestScanNodes_InvalidJSON(t *testing.T) {
	// Insert a node with invalid JSON metadata directly via raw SQL to exercise
	// the json.Unmarshal fallback path in scanNodes.
	store, db := setupTestStoreWithDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		INSERT INTO graph_nodes (id, type, component_id, metadata, first_seen, last_seen)
		VALUES ('bad-json-node', 'service', 'src1', 'NOT_VALID_JSON', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`)
	if err != nil {
		t.Fatalf("insert raw node: %v", err)
	}

	t.Run("GetNode falls back to empty metadata on bad JSON", func(t *testing.T) {
		node, err := store.GetNode(ctx, "bad-json-node")
		if err != nil {
			t.Fatalf("GetNode() error = %v", err)
		}
		if node.Metadata == nil {
			t.Error("expected non-nil Metadata fallback, got nil")
		}
	})

	t.Run("Summary with bad JSON node succeeds", func(t *testing.T) {
		_, err := store.Summary(ctx)
		if err != nil {
			t.Fatalf("Summary() error = %v", err)
		}
	})

	t.Run("ListAll with bad JSON node succeeds", func(t *testing.T) {
		sg, err := store.ListAll(ctx)
		if err != nil {
			t.Fatalf("ListAll() error = %v", err)
		}
		if len(sg.Nodes) == 0 {
			t.Error("expected at least 1 node")
		}
	})

	t.Run("ListNodesByComponent with bad JSON node succeeds", func(t *testing.T) {
		nodes, err := store.ListNodesByComponent(ctx, "src1")
		if err != nil {
			t.Fatalf("ListNodesByComponent() error = %v", err)
		}
		if len(nodes) == 0 {
			t.Error("expected at least 1 node")
		}
	})
}

func TestClosedDB_ErrorPaths(t *testing.T) {
	// By closing the DB we can exercise error-return paths that are otherwise
	// unreachable in normal operation (query failures, scan failures, etc.).
	ctx := context.Background()

	newClosedStore := func(t *testing.T) graph.GraphStore {
		t.Helper()
		store, db := setupTestStoreWithDB(t)
		db.Close() // deliberately close so all queries fail
		return store
	}

	t.Run("AddNode error on closed db", func(t *testing.T) {
		s := newClosedStore(t)
		err := s.AddNode(ctx, graph.Node{ID: "x", Type: "service"})
		if err == nil {
			t.Fatal("expected error on closed db")
		}
	})

	t.Run("Summary error on closed db", func(t *testing.T) {
		s := newClosedStore(t)
		_, err := s.Summary(ctx)
		if err == nil {
			t.Fatal("expected error on closed db")
		}
	})

	t.Run("ListNodesByComponent error on closed db", func(t *testing.T) {
		s := newClosedStore(t)
		_, err := s.ListNodesByComponent(ctx, "src1")
		if err == nil {
			t.Fatal("expected error on closed db")
		}
	})

	t.Run("ListAll error on closed db", func(t *testing.T) {
		s := newClosedStore(t)
		_, err := s.ListAll(ctx)
		if err == nil {
			t.Fatal("expected error on closed db")
		}
	})

	t.Run("Related error on closed db (depth>0)", func(t *testing.T) {
		s := newClosedStore(t)
		_, err := s.Related(ctx, "x", 1)
		if err == nil {
			t.Fatal("expected error on closed db")
		}
	})

	t.Run("Related error on closed db (depth=0)", func(t *testing.T) {
		s := newClosedStore(t)
		_, err := s.Related(ctx, "x", 0)
		if err == nil {
			t.Fatal("expected error on closed db")
		}
	})

	t.Run("DeleteEdge error on closed db", func(t *testing.T) {
		s := newClosedStore(t)
		err := s.DeleteEdge(ctx, "a", "b", "calls")
		if err == nil {
			t.Fatal("expected error on closed db")
		}
	})

	t.Run("Query error on closed db", func(t *testing.T) {
		s := newClosedStore(t)
		_, err := s.Query(ctx, "payment")
		if err == nil {
			t.Fatal("expected error on closed db")
		}
	})
}

func TestListNodesByComponent(t *testing.T) {
	store := setupTestStore(t)
	seedGraph(t, store)
	ctx := context.Background()

	t.Run("returns nodes for source", func(t *testing.T) {
		nodes, err := store.ListNodesByComponent(ctx, "k8s/prod")
		if err != nil {
			t.Fatalf("ListNodesByComponent() error = %v", err)
		}
		if len(nodes) != 4 {
			t.Errorf("ListNodesByComponent(k8s/prod) returned %d nodes, want 4", len(nodes))
		}
	})

	t.Run("returns empty for unknown source", func(t *testing.T) {
		nodes, err := store.ListNodesByComponent(ctx, "unknown-source")
		if err != nil {
			t.Fatalf("ListNodesByComponent() error = %v", err)
		}
		if len(nodes) != 0 {
			t.Errorf("ListNodesByComponent(unknown) returned %d nodes, want 0", len(nodes))
		}
	})
}

func TestListEdgesForNodes(t *testing.T) {
	store := setupTestStore(t)
	seedGraph(t, store)
	ctx := context.Background()

	t.Run("returns edges between nodes", func(t *testing.T) {
		nodeIDs := []string{"deployment/prod/payment-svc", "deployment/prod/user-svc"}
		edges, err := store.ListEdgesForNodes(ctx, nodeIDs)
		if err != nil {
			t.Fatalf("ListEdgesForNodes() error = %v", err)
		}
		if len(edges) != 1 {
			t.Errorf("ListEdgesForNodes() returned %d edges, want 1", len(edges))
		}
	})

	t.Run("empty node IDs returns nil", func(t *testing.T) {
		edges, err := store.ListEdgesForNodes(ctx, []string{})
		if err != nil {
			t.Fatalf("ListEdgesForNodes([]) error = %v", err)
		}
		if edges != nil {
			t.Errorf("ListEdgesForNodes([]) = %v, want nil", edges)
		}
	})

	t.Run("single node returns no edges", func(t *testing.T) {
		edges, err := store.ListEdgesForNodes(ctx, []string{"deployment/prod/payment-svc"})
		if err != nil {
			t.Fatalf("ListEdgesForNodes() error = %v", err)
		}
		if len(edges) != 0 {
			t.Errorf("ListEdgesForNodes() returned %d edges, want 0 (no both-endpoints-in-set)", len(edges))
		}
	})
}

func TestListAll(t *testing.T) {
	ctx := context.Background()

	t.Run("empty graph returns empty subgraph", func(t *testing.T) {
		store := setupTestStore(t)
		sg, err := store.ListAll(ctx)
		if err != nil {
			t.Fatalf("ListAll() error = %v", err)
		}
		if sg == nil {
			t.Fatal("ListAll() returned nil")
		}
		if len(sg.Nodes) != 0 {
			t.Errorf("ListAll() empty graph nodes = %d, want 0", len(sg.Nodes))
		}
		if len(sg.Edges) != 0 {
			t.Errorf("ListAll() empty graph edges = %d, want 0", len(sg.Edges))
		}
	})

	t.Run("populated graph returns all nodes and edges", func(t *testing.T) {
		store := setupTestStore(t)
		seedGraph(t, store)
		sg, err := store.ListAll(ctx)
		if err != nil {
			t.Fatalf("ListAll() error = %v", err)
		}
		if len(sg.Nodes) != 4 {
			t.Errorf("ListAll() nodes = %d, want 4", len(sg.Nodes))
		}
		if len(sg.Edges) != 3 {
			t.Errorf("ListAll() edges = %d, want 3", len(sg.Edges))
		}
	})

	t.Run("nodes without edges", func(t *testing.T) {
		store := setupTestStore(t)
		store.AddNode(ctx, graph.Node{ID: "isolated-1", Type: "service"})
		store.AddNode(ctx, graph.Node{ID: "isolated-2", Type: "service"})
		sg, err := store.ListAll(ctx)
		if err != nil {
			t.Fatalf("ListAll() error = %v", err)
		}
		if len(sg.Nodes) != 2 {
			t.Errorf("ListAll() nodes = %d, want 2", len(sg.Nodes))
		}
		if len(sg.Edges) != 0 {
			t.Errorf("ListAll() edges = %d, want 0", len(sg.Edges))
		}
	})
}
