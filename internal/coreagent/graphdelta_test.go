package coreagent

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/graph"
	_ "github.com/mattn/go-sqlite3"
)

func setupGraphStore(t *testing.T) graph.GraphStore {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=on")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	_, err = db.Exec(`
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

	t.Cleanup(func() { _ = db.Close() })
	return graph.NewSQLiteStore(db, nil)
}

func TestLoadGraphStateForSource(t *testing.T) {
	store := setupGraphStore(t)
	ctx := context.Background()

	nodes := []graph.Node{
		{ID: "k8s/src-1/deployment/default/api", Type: "deployment", SourceID: "src-1", Metadata: map[string]any{}},
		{ID: "k8s/src-1/service/default/api", Type: "service", SourceID: "src-1", Metadata: map[string]any{}},
		{ID: "k8s/src-2/deployment/default/other", Type: "deployment", SourceID: "src-2", Metadata: map[string]any{}},
	}
	for _, node := range nodes {
		if err := store.AddNode(ctx, node); err != nil {
			t.Fatalf("AddNode(%s): %v", node.ID, err)
		}
	}

	edges := []graph.Edge{
		{From: nodes[0].ID, To: nodes[1].ID, Relation: "routes_to", Confidence: graph.Explicit, Source: "k8s_api", SourceID: ""},
		{From: nodes[0].ID, To: nodes[2].ID, Relation: "calls", Confidence: graph.Explicit, Source: "k8s_api", SourceID: ""},
	}
	for _, edge := range edges {
		if err := store.AddEdge(ctx, edge); err != nil {
			t.Fatalf("AddEdge(%s->%s): %v", edge.From, edge.To, err)
		}
	}

	sourceNodes, sourceEdges, err := LoadGraphStateForSource(ctx, store, "src-1")
	if err != nil {
		t.Fatalf("LoadGraphStateForSource error: %v", err)
	}

	if len(sourceNodes) != 2 {
		t.Fatalf("nodes count = %d, want 2", len(sourceNodes))
	}
	if len(sourceEdges) != 1 {
		t.Fatalf("edges count = %d, want 1", len(sourceEdges))
	}

	if sourceEdges[0].From != nodes[0].ID || sourceEdges[0].To != nodes[1].ID {
		t.Fatalf("unexpected edge %s->%s", sourceEdges[0].From, sourceEdges[0].To)
	}
}

func TestBuildGraphDelta(t *testing.T) {
	now := time.Now()
	firstSeen := now.Add(-2 * time.Hour)
	existingNodes := []graph.Node{
		{ID: "k8s/src-1/deployment/default/api", Type: "deployment", SourceID: "src-1", Metadata: map[string]any{}, FirstSeen: firstSeen, LastSeen: now},
		{ID: "k8s/src-1/service/default/api", Type: "service", SourceID: "src-1", Metadata: map[string]any{}, LastSeen: now},
	}
	existingEdges := []graph.Edge{
		{From: existingNodes[1].ID, To: existingNodes[0].ID, Relation: "routes_to", Confidence: graph.Explicit, Source: "k8s_api", SourceID: ""},
	}

	desiredNodes := []graph.Node{
		{ID: "k8s/src-1/deployment/default/api", Type: "deployment", SourceID: "src-1", Metadata: map[string]any{"replicas": float64(2)}, LastSeen: now},
	}
	desiredEdges := []graph.Edge{}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, desiredEdges)

	if len(delta.NodesToUpsert) != 1 {
		t.Fatalf("NodesToUpsert = %d, want 1", len(delta.NodesToUpsert))
	}
	if len(delta.NodesToDelete) != 1 {
		t.Fatalf("NodesToDelete = %d, want 1", len(delta.NodesToDelete))
	}
	if len(delta.EdgesToUpsert) != 0 {
		t.Fatalf("EdgesToUpsert = %d, want 0", len(delta.EdgesToUpsert))
	}
	if len(delta.EdgesToDelete) != 0 {
		t.Fatalf("EdgesToDelete = %d, want 0", len(delta.EdgesToDelete))
	}

	if delta.NodesToUpsert[0].FirstSeen.IsZero() || !delta.NodesToUpsert[0].FirstSeen.Equal(firstSeen) {
		t.Fatalf("FirstSeen = %v, want %v", delta.NodesToUpsert[0].FirstSeen, firstSeen)
	}
}

func TestBuildGraphDelta_EdgeUpsertChanges(t *testing.T) {
	existingEdges := []graph.Edge{
		{From: "a", To: "b", Relation: "calls", Confidence: graph.Explicit, Source: "k8s_api", SourceID: "", Context: "selector"},
	}

	t.Run("no change", func(t *testing.T) {
		desiredEdges := []graph.Edge{
			{From: "a", To: "b", Relation: "calls", Confidence: graph.Explicit, Source: "k8s_api", SourceID: "", Context: "selector"},
		}

		delta := BuildGraphDelta(nil, existingEdges, nil, desiredEdges)
		if len(delta.EdgesToUpsert) != 0 {
			t.Fatalf("EdgesToUpsert = %d, want 0", len(delta.EdgesToUpsert))
		}
	})

	t.Run("context changed", func(t *testing.T) {
		desiredEdges := []graph.Edge{
			{From: "a", To: "b", Relation: "calls", Confidence: graph.Explicit, Source: "k8s_api", SourceID: "", Context: "label_match"},
		}

		delta := BuildGraphDelta(nil, existingEdges, nil, desiredEdges)
		if len(delta.EdgesToUpsert) != 1 {
			t.Fatalf("EdgesToUpsert = %d, want 1", len(delta.EdgesToUpsert))
		}
	})

	t.Run("dedupe desired", func(t *testing.T) {
		desiredEdges := []graph.Edge{
			{From: "a", To: "b", Relation: "calls", Confidence: graph.Explicit, Source: "k8s_api", SourceID: "", Context: "selector"},
			{From: "a", To: "b", Relation: "calls", Confidence: graph.Explicit, Source: "k8s_api", SourceID: "", Context: "selector"},
		}

		delta := BuildGraphDelta(nil, nil, nil, desiredEdges)
		if len(delta.EdgesToUpsert) != 1 {
			t.Fatalf("EdgesToUpsert = %d, want 1", len(delta.EdgesToUpsert))
		}
	})
}

// TestApplyGraphDelta_EdgeDeletePath covers the EdgesToDelete loop body (was 0% before).
func TestApplyGraphDelta_EdgeDeletePath(t *testing.T) {
	gs := setupGraphStore(t)
	ctx := context.Background()

	nodeA := graph.Node{ID: "a", Type: "test", SourceID: "src", Metadata: map[string]any{}}
	nodeB := graph.Node{ID: "b", Type: "test", SourceID: "src", Metadata: map[string]any{}}
	for _, n := range []graph.Node{nodeA, nodeB} {
		if err := gs.AddNode(ctx, n); err != nil {
			t.Fatalf("AddNode(%s): %v", n.ID, err)
		}
	}
	edge := graph.Edge{From: "a", To: "b", Relation: "depends_on", Confidence: graph.Explicit, Source: "test"}
	if err := gs.AddEdge(ctx, edge); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	// Delta that keeps both nodes but removes the edge between them.
	delta := GraphDelta{
		NodesToUpsert: []graph.Node{nodeA, nodeB},
		EdgesToDelete: []graph.Edge{edge},
	}
	if err := ApplyGraphDelta(ctx, gs, delta); err != nil {
		t.Errorf("ApplyGraphDelta() error = %v", err)
	}
}

// TestApplyGraphDelta_DeleteNonExistentNode covers the ErrNodeNotFound swallow path.
func TestApplyGraphDelta_DeleteNonExistentNode(t *testing.T) {
	gs := setupGraphStore(t)
	ctx := context.Background()

	// Deleting a node that doesn't exist should not return an error.
	delta := GraphDelta{
		NodesToDelete: []graph.Node{{ID: "does-not-exist"}},
	}
	if err := ApplyGraphDelta(ctx, gs, delta); err != nil {
		t.Errorf("ApplyGraphDelta() should swallow ErrNodeNotFound: %v", err)
	}
}

func TestApplyGraphDelta(t *testing.T) {
	store := setupGraphStore(t)
	ctx := context.Background()

	existingNodes := []graph.Node{
		{ID: "k8s/src-1/deployment/default/api", Type: "deployment", SourceID: "src-1", Metadata: map[string]any{}},
		{ID: "k8s/src-1/service/default/api", Type: "service", SourceID: "src-1", Metadata: map[string]any{}},
	}
	for _, node := range existingNodes {
		if err := store.AddNode(ctx, node); err != nil {
			t.Fatalf("AddNode(%s): %v", node.ID, err)
		}
	}

	existingEdge := graph.Edge{From: existingNodes[1].ID, To: existingNodes[0].ID, Relation: "routes_to", Confidence: graph.Explicit, Source: "k8s_api", SourceID: ""}
	if err := store.AddEdge(ctx, existingEdge); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	desiredNodes := []graph.Node{
		{ID: "k8s/src-1/deployment/default/api", Type: "deployment", SourceID: "src-1", Metadata: map[string]any{"replicas": float64(3)}},
		{ID: "k8s/src-1/configmap/default/api-config", Type: "configmap", SourceID: "src-1", Metadata: map[string]any{"data_keys": []string{"FOO"}}},
	}
	desiredEdges := []graph.Edge{
		{From: desiredNodes[0].ID, To: desiredNodes[1].ID, Relation: "references", Confidence: graph.Explicit, Source: "k8s_api", Context: "envFrom"},
	}

	delta := BuildGraphDelta(existingNodes, []graph.Edge{existingEdge}, desiredNodes, desiredEdges)
	if err := ApplyGraphDelta(ctx, store, delta); err != nil {
		t.Fatalf("ApplyGraphDelta error: %v", err)
	}

	gotNodes, gotEdges, err := LoadGraphStateForSource(ctx, store, "src-1")
	if err != nil {
		t.Fatalf("LoadGraphStateForSource error: %v", err)
	}

	if len(gotNodes) != 2 {
		t.Fatalf("nodes count = %d, want 2", len(gotNodes))
	}
	if len(gotEdges) != 1 {
		t.Fatalf("edges count = %d, want 1", len(gotEdges))
	}

	if gotEdges[0].Relation != "references" {
		t.Fatalf("edge relation = %q, want references", gotEdges[0].Relation)
	}
}
