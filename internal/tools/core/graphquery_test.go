package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jaimegago/joe/internal/graph"
	core "github.com/jaimegago/joe/internal/tools/core"
)

type fakeGraphQueryClient struct {
	graphQueryFunc func(ctx context.Context, query string) ([]graph.Node, error)
}

func (f *fakeGraphQueryClient) GraphQuery(ctx context.Context, query string) ([]graph.Node, error) {
	return f.graphQueryFunc(ctx, query)
}

func TestGraphQueryTool(t *testing.T) {
	fake := &fakeGraphQueryClient{
		graphQueryFunc: func(ctx context.Context, query string) ([]graph.Node, error) {
			if query == "type:service" {
				return []graph.Node{
					{ID: "svc-1", Type: "service"},
					{ID: "svc-2", Type: "service"},
				}, nil
			}
			return nil, errors.New("query failed")
		},
	}
	tool := core.NewGraphQueryTool(fake)

	t.Run("name and metadata", func(t *testing.T) {
		if tool.Name() != "graph_query" {
			t.Errorf("Name() = %q, want %q", tool.Name(), "graph_query")
		}
		if tool.Description() == "" {
			t.Error("Description() should not be empty")
		}
		params := tool.Parameters()
		if params.Type != "object" {
			t.Errorf("Parameters().Type = %q, want %q", params.Type, "object")
		}
	})

	t.Run("missing query param", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{})
		if err == nil {
			t.Error("expected error for missing query param")
		}
	})

	t.Run("empty query param", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{"query": ""})
		if err == nil {
			t.Error("expected error for empty query param")
		}
	})

	t.Run("success", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{"query": "type:service"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		nodes := m["nodes"].([]graph.Node)
		if len(nodes) != 2 {
			t.Errorf("expected 2 nodes, got %d", len(nodes))
		}
		if m["count"].(int) != 2 {
			t.Errorf("expected count 2, got %v", m["count"])
		}
	})

	t.Run("client error", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{"query": "bad"})
		if err == nil {
			t.Error("expected error from client")
		}
	})
}
