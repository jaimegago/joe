package slack

import (
	"testing"

	"github.com/jaimegago/joe/internal/graph"
)

func TestFormatter_StatusBlocks_Empty(t *testing.T) {
	f := NewFormatter()
	summary := &graph.GraphSummary{
		NodeCount: 0,
		EdgeCount: 0,
	}
	blocks := f.StatusBlocks(summary)
	if len(blocks) == 0 {
		t.Fatal("StatusBlocks() returned no blocks")
	}
}

func TestFormatter_StatusBlocks_WithData(t *testing.T) {
	f := NewFormatter()
	summary := &graph.GraphSummary{
		NodeCount: 50,
		EdgeCount: 20,
		NodesByType: map[string]int{
			"deployment": 10,
			"service":    40,
		},
		RecentlyAdded: []graph.Node{
			{ID: "k8s/src1/deployment/prod/api", Type: "deployment"},
		},
	}
	blocks := f.StatusBlocks(summary)
	if len(blocks) == 0 {
		t.Fatal("StatusBlocks() returned no blocks")
	}
}

func TestFormatter_StatusBlocks_RecentlyAddedTruncated(t *testing.T) {
	f := NewFormatter()
	summary := &graph.GraphSummary{
		NodeCount: 10,
		EdgeCount: 4,
		RecentlyAdded: []graph.Node{
			{ID: "svc-1", Type: "deployment"},
			{ID: "svc-2", Type: "deployment"},
			{ID: "svc-3", Type: "deployment"},
			{ID: "svc-4", Type: "deployment"},
			{ID: "svc-5", Type: "deployment"},
		},
	}
	blocks := f.StatusBlocks(summary)
	if len(blocks) == 0 {
		t.Fatal("StatusBlocks() returned no blocks")
	}
}

func TestFormatter_AskBlocks(t *testing.T) {
	f := NewFormatter()
	blocks := f.AskBlocks("show payment service", "Found 3 nodes")
	if len(blocks) == 0 {
		t.Fatal("AskBlocks() returned no blocks")
	}
}

func TestFormatter_ErrorBlock(t *testing.T) {
	f := NewFormatter()
	blocks := f.ErrorBlock("something went wrong")
	if len(blocks) == 0 {
		t.Fatal("ErrorBlock() returned no blocks")
	}
}

func TestFormatter_HelpBlocks(t *testing.T) {
	f := NewFormatter()
	blocks := f.HelpBlocks()
	if len(blocks) == 0 {
		t.Fatal("HelpBlocks() returned no blocks")
	}
}
