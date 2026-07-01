package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// defaultSearXNGResults caps how many results are returned when the caller does
// not request a specific count. Ranked results are returned in the order the
// instance provides them.
const defaultSearXNGResults = 10

// maxResponseBytes bounds how much of the instance's JSON response is read, a
// defensive limit against a misbehaving or hostile endpoint.
const maxResponseBytes = 1 << 20 // 1 MiB

// searxngProvider queries a SearXNG instance's JSON search endpoint. SearXNG is
// self-hostable and keyless; base_url may point directly at the instance or at
// an operator-run egress gateway that fronts it, transparent to Joe.
type searxngProvider struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func newSearXNGProvider(baseURL, apiKey string) *searxngProvider {
	return &searxngProvider{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

// searxngResponse is the subset of the SearXNG JSON response Joe reads. Only the
// per-result title, url, and content (snippet) fields are extracted; page
// bodies are never fetched.
type searxngResponse struct {
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Content string `json:"content"`
	} `json:"results"`
}

func (p *searxngProvider) Search(ctx context.Context, query string, count int) ([]Result, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("web_search: query must not be empty")
	}
	if count <= 0 {
		count = defaultSearXNGResults
	}

	q := url.Values{}
	q.Set("q", query)
	q.Set("format", "json")
	endpoint := p.baseURL + "/search?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("web_search: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	// Send the optional key only when configured. SearXNG normally needs none;
	// a fronting gateway may expect a bearer token.
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("web_search: request to %s failed: %w", p.baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("web_search: %s returned status %d (is JSON output enabled on the instance?)", p.baseURL, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("web_search: read response: %w", err)
	}

	var parsed searxngResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("web_search: parse JSON from %s (enable JSON output in the instance settings): %w", p.baseURL, err)
	}

	results := make([]Result, 0, count)
	for _, r := range parsed.Results {
		if len(results) >= count {
			break
		}
		results = append(results, Result{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Content,
		})
	}
	return results, nil
}
