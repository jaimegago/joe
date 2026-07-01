package search

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/config"
)

// TestNewProvider_NoSilentDefault pins the "web search is inert until
// configured" invariant: an empty provider yields a nil Provider and no error,
// exactly as the LLM adapter refuses to invent a provider. A nil Provider is
// what makes the web_search tool exposed-and-deny.
func TestNewProvider_NoSilentDefault(t *testing.T) {
	p, err := NewProvider(config.WebSearchConfig{})
	if err != nil {
		t.Fatalf("NewProvider(empty) error = %v, want nil", err)
	}
	if p != nil {
		t.Fatalf("NewProvider(empty) = %v, want nil (no silent default provider)", p)
	}
}

// TestNewProvider_SearXNG covers the one implemented backend and its required
// base_url.
func TestNewProvider_SearXNG(t *testing.T) {
	if _, err := NewProvider(config.WebSearchConfig{Provider: ProviderSearXNG}); err == nil {
		t.Fatal("NewProvider(searxng without base_url) returned nil error, want a base_url-required error")
	}

	p, err := NewProvider(config.WebSearchConfig{Provider: ProviderSearXNG, BaseURL: "https://searxng.example.com"})
	if err != nil {
		t.Fatalf("NewProvider(searxng with base_url) error = %v", err)
	}
	if p == nil {
		t.Fatal("NewProvider(searxng with base_url) = nil, want a live provider")
	}
}

// TestNewProvider_UnknownProviderFails pins that an unrecognized provider is a
// boot failure, not a silent fallthrough.
func TestNewProvider_UnknownProviderFails(t *testing.T) {
	if _, err := NewProvider(config.WebSearchConfig{Provider: "tavily", BaseURL: "https://api.tavily.com"}); err == nil {
		t.Fatal("NewProvider(unknown provider) returned nil error, want an unsupported-provider error")
	}
}

// TestSearchPackage_NoMutationOrSeamImports is the optional STRUCTURAL
// break-test: the search package deals only in HTTP GETs against a configured
// endpoint and must never reach a mutation, credential, adapter, or accessor
// seam. AST imports-only scoped to this package's files (the entra_exchange /
// transport_break precedent), never a tree-wide grep.
func TestSearchPackage_NoMutationOrSeamImports(t *testing.T) {
	files := []string{"provider.go", "factory.go", "searxng.go"}
	forbidden := []string{
		"internal/adapters",   // any infrastructure adapter
		"internal/access",     // the guarded accessor seam
		"internal/credential", // the credentials-as-references model
		"internal/store",      // persistence / mutation
		"internal/graph",      // the graph store
		"internal/tools",      // executor / registry
	}
	fset := token.NewFileSet()
	for _, name := range files {
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			for _, bad := range forbidden {
				if strings.Contains(path, bad) {
					t.Errorf("%s imports %q (matches forbidden %q) — the search package must not reach a mutation, credential, adapter, or accessor seam", name, path, bad)
				}
			}
		}
	}
}
