package graph

import (
	"context"
	"time"
)

// GraphStore is the interface for the graph database
type GraphStore interface {
	// AddNode adds a node to the graph
	AddNode(ctx context.Context, node Node) error

	// AddEdge adds an edge to the graph
	AddEdge(ctx context.Context, edge Edge) error

	// GetNode retrieves a node by ID
	GetNode(ctx context.Context, id string) (*Node, error)

	// Query searches for nodes matching a query
	Query(ctx context.Context, query string) ([]Node, error)

	// Related finds nodes related to the given node
	Related(ctx context.Context, nodeID string, depth int) (*Subgraph, error)

	// Path finds the path between two nodes
	Path(ctx context.Context, from, to string) ([]Edge, error)

	// DeleteNode removes a node from the graph
	DeleteNode(ctx context.Context, id string) error

	// DeleteEdge removes an edge from the graph
	DeleteEdge(ctx context.Context, from, to, relation string) error

	// Summary returns a summary of the graph for LLM context
	Summary(ctx context.Context) (GraphSummary, error)
}

// Node represents a node in the infrastructure graph
type Node struct {
	ID        string
	Type      string
	SourceID  string
	Metadata  map[string]any
	FirstSeen time.Time
	LastSeen  time.Time
}

// Edge represents a relationship between two nodes
type Edge struct {
	From       string
	To         string
	Relation   string
	Confidence ConfidenceLevel
	Source     string
	Context    string
	CreatedAt  time.Time
}

// ConfidenceLevel represents how certain we are about an edge
type ConfidenceLevel int

const (
	// Inferred means the edge was guessed by the LLM, not yet confirmed
	Inferred ConfidenceLevel = 1

	// Explicit means the edge was discovered from API or .joe/ file
	Explicit ConfidenceLevel = 3

	// UserConfirmed means the user explicitly confirmed this edge
	UserConfirmed ConfidenceLevel = 3
)

// Subgraph represents a subset of the graph
type Subgraph struct {
	Nodes []Node
	Edges []Edge
}

// GraphSummary provides a high-level view of the graph
type GraphSummary struct {
	NodeCount       int
	EdgeCount       int
	NodesByType     map[string]int
	RecentlyAdded   []Node
	RecentlyUpdated []Node
}
