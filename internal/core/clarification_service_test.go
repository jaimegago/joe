package core

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
)

// mockGraphStore is a test double for graph.GraphStore
type mockGraphStore struct {
	addedNodes   []*graph.Node
	addedEdges   []*graph.Edge
	deletedNodes []string
	err          error
}

func (m *mockGraphStore) AddNode(ctx context.Context, node graph.Node) error {
	if m.err != nil {
		return m.err
	}
	m.addedNodes = append(m.addedNodes, &node)
	return nil
}

func (m *mockGraphStore) AddEdge(ctx context.Context, edge graph.Edge) error {
	if m.err != nil {
		return m.err
	}
	m.addedEdges = append(m.addedEdges, &edge)
	return nil
}

func (m *mockGraphStore) GetNode(ctx context.Context, id string) (*graph.Node, error) {
	return nil, nil
}

func (m *mockGraphStore) Query(ctx context.Context, query string) ([]graph.Node, error) {
	return nil, nil
}

func (m *mockGraphStore) Related(ctx context.Context, nodeID string, depth int) (*graph.Subgraph, error) {
	return nil, nil
}

func (m *mockGraphStore) Path(ctx context.Context, from, to string) ([]graph.Edge, error) {
	return nil, nil
}

func (m *mockGraphStore) DeleteNode(ctx context.Context, id string) error {
	if m.err != nil {
		return m.err
	}
	m.deletedNodes = append(m.deletedNodes, id)
	return nil
}

func (m *mockGraphStore) DeleteEdge(ctx context.Context, from, to, relation string) error {
	return nil
}

func (m *mockGraphStore) Summary(ctx context.Context) (graph.GraphSummary, error) {
	return graph.GraphSummary{}, nil
}

func (m *mockGraphStore) ListNodesBySource(ctx context.Context, sourceID string) ([]graph.Node, error) {
	return nil, nil
}

func (m *mockGraphStore) ListEdgesForNodes(ctx context.Context, nodeIDs []string) ([]graph.Edge, error) {
	return nil, nil
}

func setupClarificationServiceTest(t *testing.T) (*ClarificationService, *mockGraphStore, *store.Store) {
	t.Helper()

	sqlStore, err := store.New(":memory:?_foreign_keys=on", nil)
	if err != nil {
		t.Fatalf("Failed to create test store: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })

	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("Failed to migrate test store: %v", err)
	}

	mockGraph := &mockGraphStore{}
	svc := NewClarificationService(mockGraph, sqlStore)

	return svc, mockGraph, sqlStore
}

func TestClarificationService_ApplyAnswer(t *testing.T) {
	svc, mockGraph, storeInst := setupClarificationServiceTest(t)
	ctx := context.Background()

	t.Run("apply answer with add_node operation", func(t *testing.T) {
		nodeOp := GraphOperation{
			Type: "add_node",
			Node: &graph.Node{
				ID:       "deployment/prod/api",
				Type:     "deployment",
				SourceID: "k8s-1",
			},
		}

		opsData := GraphOperations{
			Operations: []GraphOperation{nodeOp},
		}
		opsJSON, _ := json.Marshal(opsData)

		c := &store.Clarification{
			ID:              "clar-1",
			Type:            store.ClarificationNewService,
			Context:         json.RawMessage(`{}`),
			Question:        "What is this?",
			Status:          store.ClarificationPending,
			GraphOperations: opsJSON,
		}

		storeInst.Clarifications.Create(ctx, c)
		storeInst.Clarifications.Answer(ctx, "clar-1", "API", "admin")

		if err := svc.ApplyAnswer(ctx, "clar-1", "API", "admin"); err != nil {
			t.Fatalf("ApplyAnswer error: %v", err)
		}

		if len(mockGraph.addedNodes) != 1 {
			t.Errorf("Expected 1 node added, got %d", len(mockGraph.addedNodes))
		}

		if mockGraph.addedNodes[0].ID != "deployment/prod/api" {
			t.Errorf("Node ID = %q, want %q", mockGraph.addedNodes[0].ID, "deployment/prod/api")
		}
	})

	t.Run("apply answer with add_edge operation", func(t *testing.T) {
		edgeOp := GraphOperation{
			Type: "add_edge",
			Edge: &graph.Edge{
				From:       "dep1",
				To:         "dep2",
				Relation:   "routes_to",
				Confidence: graph.Inferred,
			},
		}

		opsData := GraphOperations{
			Operations: []GraphOperation{edgeOp},
		}
		opsJSON, _ := json.Marshal(opsData)

		c := &store.Clarification{
			ID:              "clar-2",
			Type:            store.ClarificationEdgeConfirm,
			Context:         json.RawMessage(`{}`),
			Question:        "Does dep1 route to dep2?",
			GraphOperations: opsJSON,
		}

		storeInst.Clarifications.Create(ctx, c)
		storeInst.Clarifications.Answer(ctx, "clar-2", "Yes", "user")

		if err := svc.ApplyAnswer(ctx, "clar-2", "Yes", "user"); err != nil {
			t.Fatalf("ApplyAnswer error: %v", err)
		}

		if len(mockGraph.addedEdges) != 1 {
			t.Errorf("Expected 1 edge added, got %d", len(mockGraph.addedEdges))
		}

		if mockGraph.addedEdges[0].Confidence != graph.UserConfirmed {
			t.Errorf("Confidence = %v, want %v", mockGraph.addedEdges[0].Confidence, graph.UserConfirmed)
		}
	})

	t.Run("apply answer with no operations", func(t *testing.T) {
		c := &store.Clarification{
			ID:       "clar-3",
			Type:     store.ClarificationServicePurpose,
			Context:  json.RawMessage(`{}`),
			Question: "Purpose?",
		}

		storeInst.Clarifications.Create(ctx, c)
		storeInst.Clarifications.Answer(ctx, "clar-3", "Docs", "user")

		if err := svc.ApplyAnswer(ctx, "clar-3", "Docs", "user"); err != nil {
			t.Fatalf("ApplyAnswer error: %v", err)
		}

		if len(mockGraph.addedNodes) != 1 {
			t.Errorf("Expected 1 node total, got %d", len(mockGraph.addedNodes))
		}
	})
}
