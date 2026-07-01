// Package search provides Joe's web-search capability: a SearchProvider
// abstraction plus a boot-time factory that selects a provider implementation
// from configuration. It mirrors the LLM-adapter factory pattern in spirit — a
// provider name plus a base_url and an optional key select the backend, and
// there is no silent default (web search is inert until configured).
//
// This package deals only in discovering ranked results (title, url, snippet).
// It never fetches page bodies — retrieving a chosen URL is the http_request
// tool's job. Search and fetch stay separate and compose.
package search

import "context"

// Result is one ranked search hit. It carries only the metadata a provider
// returns for a result — never the page body.
type Result struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// Provider is the SearchProvider abstraction: a single search operation over a
// configured backend. Implementations are constructed once at boot by
// NewProvider and are safe for concurrent use.
type Provider interface {
	// Search runs the query against the backend and returns ranked results.
	// count is an optional cap on the number of results; a value <= 0 means the
	// provider's own default applies. Providers never fetch page contents.
	Search(ctx context.Context, query string, count int) ([]Result, error)
}
