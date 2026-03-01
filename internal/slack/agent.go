// Package slack implements the Joe Slack bot server.
// It connects to Slack via Socket Mode (no public URL required)
// and routes commands to joecored via the HTTP client.
package slack

import (
	"context"
	"fmt"
	"strings"

	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/knowledge"
)

// JoeClient is the subset of *client.Client used by the Slack bot Agent.
// Using an interface allows the tests to inject a mock.
type JoeClient interface {
	GraphQuery(ctx context.Context, query string) ([]graph.Node, error)
	GraphSummary(ctx context.Context) (*graph.GraphSummary, error)
	SearchKnowledge(ctx context.Context, query string, topK int, tiers []knowledge.Tier) ([]knowledge.SearchResult, error)
}

// Agent queries joecored and returns plain-text answers for Slack messages.
type Agent struct {
	c JoeClient
}

// NewAgent creates a new Agent backed by the given JoeClient.
func NewAgent(c JoeClient) *Agent {
	return &Agent{c: c}
}

// Ask runs a freeform query against the graph and knowledge store and returns
// a human-readable summary suitable for a Slack message.
func (a *Agent) Ask(ctx context.Context, query string) (string, error) {
	nodes, err := a.c.GraphQuery(ctx, query)
	if err != nil {
		return "", fmt.Errorf("graph query: %w", err)
	}

	results, err := a.c.SearchKnowledge(ctx, query, 3, nil)
	if err != nil {
		// Knowledge search is best-effort; continue without it.
		results = nil
	}

	return buildAskResponse(query, nodes, results), nil
}

// Status returns the current graph summary from joecored.
func (a *Agent) Status(ctx context.Context) (*graph.GraphSummary, error) {
	return a.c.GraphSummary(ctx)
}

// buildAskResponse assembles a text answer from graph nodes and knowledge search results.
func buildAskResponse(query string, nodes []graph.Node, results []knowledge.SearchResult) string {
	var sb strings.Builder

	if len(nodes) == 0 && len(results) == 0 {
		return fmt.Sprintf("I didn't find anything matching *%s* in the graph or knowledge store.", query)
	}

	if len(nodes) > 0 {
		sb.WriteString(fmt.Sprintf("*Graph nodes matching \"%s\":*\n", query))
		limit := len(nodes)
		if limit > 5 {
			limit = 5
		}
		for _, n := range nodes[:limit] {
			sb.WriteString(fmt.Sprintf("• `%s` (%s)\n", n.ID, n.Type))
		}
		if len(nodes) > 5 {
			sb.WriteString(fmt.Sprintf("  _…and %d more_\n", len(nodes)-5))
		}
	}

	if len(results) > 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("*Related knowledge:*\n")
		for _, r := range results {
			sb.WriteString(fmt.Sprintf("• *%s*: %s\n", r.Entry.Title, truncate(r.Entry.Content, 120)))
		}
	}

	return sb.String()
}

// truncate shortens s to at most n characters, appending "…" if truncated.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
