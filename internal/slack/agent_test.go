package slack

import (
	"context"
	"errors"
	"testing"

	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/knowledge"
)

// mockJoeClient is a test double for JoeClient.
type mockJoeClient struct {
	nodes     []graph.Node
	summary   *graph.GraphSummary
	results   []knowledge.SearchResult
	queryErr  error
	sumErr    error
	searchErr error
}

func (m *mockJoeClient) GraphQuery(_ context.Context, _ string) ([]graph.Node, error) {
	return m.nodes, m.queryErr
}

func (m *mockJoeClient) GraphSummary(_ context.Context) (*graph.GraphSummary, error) {
	return m.summary, m.sumErr
}

func (m *mockJoeClient) SearchKnowledge(_ context.Context, _ string, _ int, _ []knowledge.Tier) ([]knowledge.SearchResult, error) {
	return m.results, m.searchErr
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
			name: "returns knowledge entries in response",
			client: &mockJoeClient{
				nodes: []graph.Node{},
				results: []knowledge.SearchResult{
					{Entry: knowledge.Entry{Title: "Payment Runbook", Content: "Steps to restart payment service"}},
				},
			},
			query:       "payment runbook",
			wantContain: "Payment Runbook",
		},
		{
			name: "returns no-results message when nothing found",
			client: &mockJoeClient{
				nodes:   []graph.Node{},
				results: nil,
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
		{
			name: "continues if knowledge search fails",
			client: &mockJoeClient{
				nodes:     []graph.Node{{ID: "aws/src1/ec2/i-123", Type: "ec2_instance"}},
				searchErr: errors.New("knowledge store unavailable"),
			},
			query:       "ec2",
			wantContain: "i-123",
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

func TestTruncate(t *testing.T) {
	tests := []struct {
		s    string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello…"},
		{"", 5, ""},
	}
	for _, tt := range tests {
		if got := truncate(tt.s, tt.n); got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
		}
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
