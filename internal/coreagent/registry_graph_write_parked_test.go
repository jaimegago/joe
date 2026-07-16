package coreagent

import (
	"io"
	"log/slog"
	"testing"

	"github.com/jaimegago/joe/internal/tools"
)

// parkedGraphWriteTools are the three onboarding-era graph-write tools parked
// out of the agent:core registry by session iac-graph-ingestion (D-0110).
var parkedGraphWriteTools = []string{
	"graph_add_node",
	"graph_add_edge",
	"graph_update_node",
}

// TestGraphWriteToolsAreParked pins that the autonomous agent:core tool registry
// does NOT include the three graph-write tools (session iac-graph-ingestion,
// D-0110, following the D-0081/D-0109 parking pattern).
//
// Why this needs a pin rather than a comment: all three are ActionRead-classed
// (internal/safety/tier.go — per D-0020 Joe's own model-maintenance tools are
// Reads), so they pass the write floor unconditionally, observation mode
// included. They also write via services.Graph.AddNode/AddEdge/UpdateNode
// DIRECTLY, bypassing the delta-reconcile seam every refresher writes through,
// so nothing reconciles away what they add. Registered here, they would give a
// future autonomous loop a way to write LLM-inferred nodes and edges into the
// infrastructure graph that observation mode would NOT stop — the exact thing
// D-0110 forbids. The parking is the only thing standing between that and a live
// writer, so it is asserted, not assumed.
func TestGraphWriteToolsAreParked(t *testing.T) {
	registry := tools.NewRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// services is only stored by the constructors, not dereferenced at
	// registration, so nil is safe for a name-set assertion.
	registerCoreAgentTools(registry, nil, logger)

	for _, tool := range registry.GetAll() {
		for _, parked := range parkedGraphWriteTools {
			if tool.Name() == parked {
				t.Fatalf("agent:core registry must NOT include %s — it is parked "+
					"(session iac-graph-ingestion, D-0110); it is Read-classed, so it passes the "+
					"write floor even in observation mode, and it bypasses the delta-reconcile "+
					"seam, so the graph would carry unreconciled LLM-authored structure", parked)
			}
		}
	}

	// Sanity: the registry still registers its live tools, so a registry that
	// registered nothing at all wouldn't vacuously pass the check above.
	if len(registry.GetAll()) == 0 {
		t.Fatal("agent:core registry registered no tools")
	}
}

// TestGraphWriteToolsRetained pins the other half of the parking contract: the
// implementations are RETAINED, not deleted. Parking means removing exactly
// three registration call sites, so re-enabling is restoring them. A future
// cleanup that deletes these tools would break this and should be a deliberate
// decision (see docs/backlog/iac-graph-ingestion.md), not a tidy-up.
func TestGraphWriteToolsRetained(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if got := NewGraphAddNodeTool(nil, logger).Name(); got != "graph_add_node" {
		t.Errorf("Name() = %q, want graph_add_node", got)
	}
	if got := NewGraphAddEdgeTool(nil, logger).Name(); got != "graph_add_edge" {
		t.Errorf("Name() = %q, want graph_add_edge", got)
	}
	if got := NewGraphUpdateNodeTool(nil, logger).Name(); got != "graph_update_node" {
		t.Errorf("Name() = %q, want graph_update_node", got)
	}
}
