package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestCLI_NoLLMAdapterOrLoopInClosure is the Phase 2 structural guard for the
// acceptance criterion "no code path in cmd/joe instantiates an LLM adapter or
// runs an agentic loop". The CLI cannot construct an adapter or run the loop if
// the adapter-factory, provider-client, and agent-loop packages are absent from
// its build closure — so we assert exactly that.
//
// internal/llm (types like ParameterSchema) is intentionally allowed; it is the
// adapter *factory* and *provider* packages and the agentic-loop package that
// must not be linked into the CLI.
func TestCLI_NoLLMAdapterOrLoopInClosure(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Skipf("go list unavailable: %v", err)
	}

	forbidden := []string{
		"github.com/jaimegago/joe/internal/llmfactory",
		"github.com/jaimegago/joe/internal/llm/claude",
		"github.com/jaimegago/joe/internal/llm/gemini",
		"github.com/jaimegago/joe/internal/useragent", // the agentic loop (relocated in a later change)
		"github.com/jaimegago/joe/internal/agentloop", // future home of the loop; forbidden here too
	}

	deps := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		deps[strings.TrimSpace(line)] = true
	}

	for _, pkg := range forbidden {
		if deps[pkg] {
			t.Errorf("cmd/joe must not link %s — the CLI would be able to instantiate an LLM adapter or run a loop", pkg)
		}
	}
}
