package tools

import "testing"

// TestWebSearchAdvertisedWhenUnconfigured is the exposed-and-deny advertisement
// break-test: even with NO web-search backend configured (nil provider passed
// to the shared-tool registration path), web_search must remain present in the
// registry's advertised tool definitions. The tool is never hidden; a call with
// no backend surfaces as a tool-error result (see the websearch package test),
// not as an absent tool.
func TestWebSearchAdvertisedWhenUnconfigured(t *testing.T) {
	registry := NewRegistry()
	registerSharedTools(registry, nil) // nil provider = web search unconfigured

	found := false
	for _, def := range registry.ToDefinitions() {
		if def.Name == "web_search" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("web_search missing from advertised definitions with no backend configured — it must stay exposed-and-deny, not hidden")
	}
}
