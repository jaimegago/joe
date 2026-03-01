package slack

import (
	"testing"

	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/knowledge"
)

func TestStripMention(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"<@U12345> show status", " show status"},
		{"<@UABC> ", " "},
		{"no mention here", "no mention here"},
		{"", ""},
		{"<@U123>", ""},
	}

	for _, tt := range tests {
		got := stripMention(tt.input)
		if got != tt.want {
			t.Errorf("stripMention(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBuildAskResponse_NoResults(t *testing.T) {
	got := buildAskResponse("unknown", nil, nil)
	if !contains(got, "didn't find anything") {
		t.Errorf("expected no-results message, got: %q", got)
	}
}

func TestBuildAskResponse_NodesTruncatedAt5(t *testing.T) {
	nodes := make([]graph.Node, 8)
	for i := range nodes {
		nodes[i] = graph.Node{ID: "node", Type: "deployment"}
	}
	got := buildAskResponse("test", nodes, nil)
	if !contains(got, "and 3 more") {
		t.Errorf("expected truncation note, got: %q", got)
	}
}

func TestBuildAskResponse_IncludesKnowledgeEntries(t *testing.T) {
	results := []knowledge.SearchResult{
		{Entry: knowledge.Entry{Title: "DB Runbook", Content: "How to restart the database"}},
	}
	got := buildAskResponse("database", nil, results)
	if !contains(got, "DB Runbook") {
		t.Errorf("expected knowledge entry title, got: %q", got)
	}
}
