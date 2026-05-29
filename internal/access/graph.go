package access

import (
	"context"

	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/rbac"
)

// Graph operations are gated as reads (rbac.ActionRead) against the reserved
// GraphSourceID. This matches the HTTP transport's prior behaviour, where
// graph paths were GET requests (verb → ActionRead) keyed on non-source path
// segments that resolved to the "unassigned" zone — the same decision the
// accessor reaches via GraphSourceID.

// GraphQuery searches for nodes matching the query string.
func (a *Accessor) GraphQuery(ctx context.Context, principal rbac.Principal, query string) ([]graph.Node, error) {
	if err := a.permit(ctx, principal, GraphSourceID, rbac.ActionRead); err != nil {
		return nil, err
	}
	if a.graph == nil {
		return nil, ErrGraphUnavailable
	}
	return a.graph.Query(ctx, query)
}

// GraphRelated finds nodes related to nodeID up to the given depth.
func (a *Accessor) GraphRelated(ctx context.Context, principal rbac.Principal, nodeID string, depth int) (*graph.Subgraph, error) {
	if err := a.permit(ctx, principal, GraphSourceID, rbac.ActionRead); err != nil {
		return nil, err
	}
	if a.graph == nil {
		return nil, ErrGraphUnavailable
	}
	return a.graph.Related(ctx, nodeID, depth)
}

// GraphSummary returns a summary of the graph for LLM context.
func (a *Accessor) GraphSummary(ctx context.Context, principal rbac.Principal) (graph.GraphSummary, error) {
	if err := a.permit(ctx, principal, GraphSourceID, rbac.ActionRead); err != nil {
		return graph.GraphSummary{}, err
	}
	if a.graph == nil {
		return graph.GraphSummary{}, ErrGraphUnavailable
	}
	return a.graph.Summary(ctx)
}

// GraphGetNode retrieves a node by ID.
func (a *Accessor) GraphGetNode(ctx context.Context, principal rbac.Principal, id string) (*graph.Node, error) {
	if err := a.permit(ctx, principal, GraphSourceID, rbac.ActionRead); err != nil {
		return nil, err
	}
	if a.graph == nil {
		return nil, ErrGraphUnavailable
	}
	return a.graph.GetNode(ctx, id)
}

// GraphListAll returns all nodes and edges in the graph (capped).
func (a *Accessor) GraphListAll(ctx context.Context, principal rbac.Principal) (*graph.Subgraph, error) {
	if err := a.permit(ctx, principal, GraphSourceID, rbac.ActionRead); err != nil {
		return nil, err
	}
	if a.graph == nil {
		return nil, ErrGraphUnavailable
	}
	return a.graph.ListAll(ctx)
}

// GraphAvailable reports whether a graph store is wired. Handlers that
// previously branched on services.Graph == nil use this to preserve their
// "graph not configured" response path without reaching the store directly.
func (a *Accessor) GraphAvailable() bool {
	return a.graph != nil
}
