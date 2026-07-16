package coreagent

import (
	"io"
	"log/slog"
	"testing"

	"github.com/jaimegago/joe/internal/tools"
)

// TestSaveKnowledgeEntryToolIsParked pins that the autonomous agent:core tool
// registry does NOT include save_knowledge_entry (session
// knowledge-store-maturation, D-0081 parking pattern).
//
// Why this needs a pin rather than a comment: save_knowledge_entry is
// ActionRead-classed (internal/safety/tier.go — per D-0020 Joe's own
// model-maintenance tools are Reads), so it passes the write floor
// unconditionally, observation mode included. Registered here, it would give a
// future autonomous loop an unaudited, unstamped, un-admin-gated write into the
// knowledge store that observation mode would NOT stop. The parking is the only
// thing standing between that and a live writer, so it is asserted, not assumed.
func TestSaveKnowledgeEntryToolIsParked(t *testing.T) {
	registry := tools.NewRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// services is only stored by the constructors, not dereferenced at
	// registration, so nil is safe for a name-set assertion.
	registerCoreAgentTools(registry, nil, logger)

	for _, tool := range registry.GetAll() {
		if tool.Name() == "save_knowledge_entry" {
			t.Fatal("agent:core registry must NOT include save_knowledge_entry — it is parked " +
				"(session knowledge-store-maturation); it is Read-classed and would pass the " +
				"write floor with no audit, no principal stamping, and no admin gate")
		}
	}

	// Sanity: the registry still registers its live tools, so a registry that
	// registered nothing at all wouldn't vacuously pass the check above.
	if len(registry.GetAll()) == 0 {
		t.Fatal("agent:core registry registered no tools")
	}
}

// TestSaveKnowledgeEntryToolRetained pins the other half of the parking
// contract: the implementation is RETAINED, not deleted. Parking means removing
// exactly one registration call site, so re-enabling is restoring it. A future
// cleanup that deletes the tool would break this and should be a deliberate
// decision (see docs/backlog/knowledge-store-maturation.md), not a tidy-up.
func TestSaveKnowledgeEntryToolRetained(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tool := NewSaveKnowledgeEntryTool(nil, logger)

	if tool.Name() != "save_knowledge_entry" {
		t.Fatalf("Name() = %q, want save_knowledge_entry", tool.Name())
	}
}
