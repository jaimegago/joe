// Package slack implements the Joe Slack bot server.
// It connects to Slack via Socket Mode (no public URL required)
// and routes commands to joecored via the HTTP client.
package slack

import (
	"context"
	"fmt"
	"strings"

	"github.com/jaimegago/joe/internal/graph"
)

// JoeClient is the subset of *client.Client used by the Slack bot Agent.
// Using an interface allows the tests to inject a mock.
type JoeClient interface {
	GraphQuery(ctx context.Context, query string) ([]graph.Node, error)
	GraphSummary(ctx context.Context) (*graph.GraphSummary, error)
}

// Agent queries joecored and returns plain-text answers for Slack messages.
type Agent struct {
	c JoeClient
}

// NewAgent creates a new Agent backed by the given JoeClient.
func NewAgent(c JoeClient) *Agent {
	return &Agent{c: c}
}

// Ask runs a freeform query against the graph and returns a human-readable
// summary suitable for a Slack message.
func (a *Agent) Ask(ctx context.Context, query string) (string, error) {
	nodes, err := a.c.GraphQuery(ctx, query)
	if err != nil {
		return "", fmt.Errorf("graph query: %w", err)
	}

	return buildAskResponse(query, nodes), nil
}

// Status returns the current graph summary from joecored.
func (a *Agent) Status(ctx context.Context) (*graph.GraphSummary, error) {
	return a.c.GraphSummary(ctx)
}

// buildAskResponse assembles a text answer from graph nodes.
func buildAskResponse(query string, nodes []graph.Node) string {
	var sb strings.Builder

	if len(nodes) == 0 {
		return fmt.Sprintf("I didn't find anything matching *%s* in the graph.", query)
	}

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

	return sb.String()
}
