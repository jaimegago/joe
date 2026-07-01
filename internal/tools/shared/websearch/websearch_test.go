package websearch

import (
	"context"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/search"
)

// spyProvider records whether Search was invoked, so the exposed-and-deny test
// can prove that an unconfigured tool returns an error WITHOUT reaching a
// backend.
type spyProvider struct {
	called  bool
	results []search.Result
}

func (s *spyProvider) Search(_ context.Context, _ string, _ int) ([]search.Result, error) {
	s.called = true
	return s.results, nil
}

// TestExecute_NoBackendConfigured is the behavioral break-test that locks the
// exposed-and-deny contract, in the style of the httpreq method-scoping test
// that locks its read-only floor: with no backend configured (nil provider),
// Execute returns a tool-error rather than performing a request.
func TestExecute_NoBackendConfigured(t *testing.T) {
	tool := NewWebSearchTool(nil) // unconfigured

	// The tool is still fully constructed and advertised.
	if tool.Name() != "web_search" {
		t.Fatalf("Name() = %q, want web_search", tool.Name())
	}
	if _, ok := tool.Parameters().Properties["query"]; !ok {
		t.Fatal("Parameters() must advertise a query property")
	}

	out, err := tool.Execute(context.Background(), map[string]any{"query": "kubernetes crashloop"})
	if err == nil {
		t.Fatal("Execute with no backend configured returned nil error, want a no-backend tool-error")
	}
	if out != nil {
		t.Fatalf("Execute with no backend returned non-nil output %v, want nil", out)
	}
	if !strings.Contains(err.Error(), "no search backend configured") {
		t.Fatalf("error = %q, want it to mention 'no search backend configured'", err.Error())
	}
}

// TestExecute_ConfiguredCallsProvider confirms the counterpart: when a provider
// IS configured, Execute delegates to it and renders ranked title/url/snippet.
func TestExecute_ConfiguredCallsProvider(t *testing.T) {
	spy := &spyProvider{results: []search.Result{
		{Title: "Debugging CrashLoopBackOff", URL: "https://example.com/a", Snippet: "why pods restart"},
		{Title: "kubectl describe", URL: "https://example.com/b"},
	}}
	tool := NewWebSearchTool(spy)

	out, err := tool.Execute(context.Background(), map[string]any{"query": "crashloop"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !spy.called {
		t.Fatal("Execute did not call the configured provider")
	}
	text, ok := out.(string)
	if !ok {
		t.Fatalf("Execute output type = %T, want string", out)
	}
	for _, want := range []string{"Debugging CrashLoopBackOff", "https://example.com/a", "why pods restart", "kubectl describe"} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q\ngot:\n%s", want, text)
		}
	}
}

// TestExecute_MissingQuery guards the required parameter.
func TestExecute_MissingQuery(t *testing.T) {
	tool := NewWebSearchTool(&spyProvider{})
	if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
		t.Fatal("Execute without a query returned nil error, want a missing-parameter error")
	}
}
