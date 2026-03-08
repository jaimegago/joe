package core

import (
	"context"
	"encoding/json"
	"fmt"
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

func (m *mockGraphStore) ListAll(ctx context.Context) (*graph.Subgraph, error) {
	return &graph.Subgraph{}, nil
}

func setupClarificationServiceTest(t *testing.T) (*ClarificationService, *mockGraphStore, *store.Store) {
	t.Helper()

	sqlStore, err := store.New(":memory:?_pragma=foreign_keys(1)", nil)
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

func TestApplyAnswer_NilStore(t *testing.T) {
	svc := NewClarificationService(&mockGraphStore{}, nil)
	err := svc.ApplyAnswer(context.Background(), "x", "y", "z")
	if err == nil {
		t.Error("expected error when store is nil")
	}
}

func TestApplyAnswer_ClarificationNotFound(t *testing.T) {
	svc, _, _ := setupClarificationServiceTest(t)
	err := svc.ApplyAnswer(context.Background(), "does-not-exist", "answer", "user")
	if err == nil {
		t.Error("expected error for missing clarification")
	}
}

func TestApplyAnswer_BadJSON(t *testing.T) {
	svc, _, storeInst := setupClarificationServiceTest(t)
	ctx := context.Background()

	c := &store.Clarification{
		ID:              "clar-bad-json",
		Type:            store.ClarificationNewService,
		Context:         json.RawMessage(`{}`),
		Question:        "What?",
		GraphOperations: []byte(`{not valid json`),
	}
	storeInst.Clarifications.Create(ctx, c)
	storeInst.Clarifications.Answer(ctx, "clar-bad-json", "answer", "user")

	err := svc.ApplyAnswer(ctx, "clar-bad-json", "answer", "user")
	if err == nil {
		t.Error("expected error for invalid JSON in GraphOperations")
	}
}

func TestApplyAnswer_OperationErrors_Aggregated(t *testing.T) {
	mockGraph := &mockGraphStore{err: fmt.Errorf("graph error")}
	sqlStore, err := store.New(":memory:?_pragma=foreign_keys(1)", nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	svc := NewClarificationService(mockGraph, sqlStore)
	ctx := context.Background()

	ops := GraphOperations{
		Operations: []GraphOperation{
			{Type: "add_node", Node: &graph.Node{ID: "n1", Type: "svc", SourceID: "s1"}},
			{Type: "add_node", Node: &graph.Node{ID: "n2", Type: "svc", SourceID: "s1"}},
		},
	}
	opsJSON, _ := json.Marshal(ops)

	c := &store.Clarification{
		ID:              "clar-err",
		Type:            store.ClarificationNewService,
		Context:         json.RawMessage(`{}`),
		Question:        "What?",
		GraphOperations: opsJSON,
	}
	sqlStore.Clarifications.Create(ctx, c)
	sqlStore.Clarifications.Answer(ctx, "clar-err", "answer", "user")

	applyErr := svc.ApplyAnswer(ctx, "clar-err", "answer", "user")
	if applyErr == nil {
		t.Error("expected aggregated error when graph operations fail")
	}
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

func TestApplyOperation_NilGraphStore(t *testing.T) {
	svc := &ClarificationService{graphStore: nil}
	err := svc.applyOperation(context.Background(), &GraphOperation{Type: "add_node", Node: &graph.Node{}}, &OperationProvenance{})
	if err == nil {
		t.Error("expected error when graphStore is nil")
	}
}

func TestApplyOperation_Branches(t *testing.T) {
	tests := []struct {
		name    string
		op      GraphOperation
		wantErr bool
	}{
		{
			name:    "add_node nil node",
			op:      GraphOperation{Type: "add_node", Node: nil},
			wantErr: true,
		},
		{
			name: "add_node success",
			op: GraphOperation{
				Type: "add_node",
				Node: &graph.Node{ID: "x", Type: "svc", SourceID: "s"},
			},
			wantErr: false,
		},
		{
			name:    "add_edge nil edge",
			op:      GraphOperation{Type: "add_edge", Edge: nil},
			wantErr: true,
		},
		{
			name: "add_edge success",
			op: GraphOperation{
				Type: "add_edge",
				Edge: &graph.Edge{From: "a", To: "b", Relation: "r"},
			},
			wantErr: false,
		},
		{
			name:    "delete_node missing node_id",
			op:      GraphOperation{Type: "delete_node", NodeID: ""},
			wantErr: true,
		},
		{
			name:    "delete_node success",
			op:      GraphOperation{Type: "delete_node", NodeID: "n1"},
			wantErr: false,
		},
		{
			name:    "delete_edge missing from",
			op:      GraphOperation{Type: "delete_edge", From: "", To: "b", Relation: "r"},
			wantErr: true,
		},
		{
			name:    "delete_edge missing to",
			op:      GraphOperation{Type: "delete_edge", From: "a", To: "", Relation: "r"},
			wantErr: true,
		},
		{
			name:    "delete_edge missing relation",
			op:      GraphOperation{Type: "delete_edge", From: "a", To: "b", Relation: ""},
			wantErr: true,
		},
		{
			name:    "delete_edge success",
			op:      GraphOperation{Type: "delete_edge", From: "a", To: "b", Relation: "r"},
			wantErr: false,
		},
		{
			name:    "unknown type",
			op:      GraphOperation{Type: "explode"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &ClarificationService{graphStore: &mockGraphStore{}}
			err := svc.applyOperation(context.Background(), &tt.op, &OperationProvenance{})
			if (err != nil) != tt.wantErr {
				t.Errorf("applyOperation() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
