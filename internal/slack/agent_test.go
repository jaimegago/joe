package slack

import (
	"context"
	"errors"
	"testing"

	"github.com/jaimegago/joe/internal/graph"
)

// mockJoeClient is a test double for JoeClient.
type mockJoeClient struct {
	nodes    []graph.Node
	summary  *graph.GraphSummary
	queryErr error
	sumErr   error
}

func (m *mockJoeClient) GraphQuery(_ context.Context, _ string) ([]graph.Node, error) {
	return m.nodes, m.queryErr
}

func (m *mockJoeClient) GraphSummary(_ context.Context) (*graph.GraphSummary, error) {
	return m.summary, m.sumErr
}

func TestAgent_Ask(t *testing.T) {
	tests := []struct {
		name        string
		client      *mockJoeClient
		query       string
		wantContain string
		wantErr     bool
	}{
		{
			name: "returns graph nodes in response",
			client: &mockJoeClient{
				nodes: []graph.Node{
					{ID: "k8s/src1/deployment/prod/payment-svc", Type: "deployment"},
					{ID: "k8s/src1/service/prod/payment-svc", Type: "service"},
				},
			},
			query:       "payment",
			wantContain: "payment-svc",
		},
		{
			name: "returns no-results message when nothing found",
			client: &mockJoeClient{
				nodes: []graph.Node{},
			},
			query:       "unknown-xyz",
			wantContain: "didn't find anything",
		},
		{
			name: "returns error on graph query failure",
			client: &mockJoeClient{
				queryErr: errors.New("connection refused"),
			},
			query:   "test",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewAgent(tt.client)
			got, err := a.Ask(context.Background(), tt.query)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Ask() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && tt.wantContain != "" && !contains(got, tt.wantContain) {
				t.Errorf("Ask() = %q, want it to contain %q", got, tt.wantContain)
			}
		})
	}
}

func TestAgent_Status(t *testing.T) {
	want := &graph.GraphSummary{
		NodeCount: 42,
		EdgeCount: 18,
		NodesByType: map[string]int{
			"deployment": 10,
			"service":    32,
		},
	}

	a := NewAgent(&mockJoeClient{summary: want})
	got, err := a.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if got.NodeCount != want.NodeCount {
		t.Errorf("NodeCount = %d, want %d", got.NodeCount, want.NodeCount)
	}
	if got.EdgeCount != want.EdgeCount {
		t.Errorf("EdgeCount = %d, want %d", got.EdgeCount, want.EdgeCount)
	}
}

func TestAgent_Status_Error(t *testing.T) {
	a := NewAgent(&mockJoeClient{sumErr: errors.New("unreachable")})
	_, err := a.Status(context.Background())
	if err == nil {
		t.Fatal("Status() expected error, got nil")
	}
}

// contains checks if s contains sub.
func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
