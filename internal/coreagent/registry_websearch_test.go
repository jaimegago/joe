package coreagent

import (
	"io"
	"log/slog"
	"testing"

	"github.com/jaimegago/joe/internal/tools"
)

// TestCoreAgentRegistryExcludesWebSearch pins the invariant that the autonomous
// agent:core tool registry does NOT include the web_search shared tool
// (CLAUDE.md: web_search is registered on the user task loop only, via
// internal/tools/default.go). The autonomous refresh surface must not gain an
// external-egress tool by accident, so this guards against a future edit that
// adds web_search — or any shared egress tool — to registerCoreAgentTools.
func TestCoreAgentRegistryExcludesWebSearch(t *testing.T) {
	registry := tools.NewRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// services is only stored by the constructors, not dereferenced at
	// registration, so nil is safe for a name-set assertion.
	registerCoreAgentTools(registry, nil, logger)

	for _, tool := range registry.GetAll() {
		if tool.Name() == "web_search" {
			t.Fatal("agent:core registry must NOT include web_search — it is a user-task-loop-only tool; " +
				"the autonomous refresh surface must not gain external-egress tools")
		}
	}

	// Sanity: the expected model-maintenance tools ARE present, so a broken
	// registry that registers nothing wouldn't vacuously pass the check above.
	if len(registry.GetAll()) == 0 {
		t.Fatal("agent:core registry registered no tools")
	}
}
