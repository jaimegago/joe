package graph

import (
	"context"
	"errors"
	"time"
)

// ErrNodeNotFound is returned when a node does not exist in the graph.
var ErrNodeNotFound = errors.New("node not found")

// GraphStore is the interface for the graph database
type GraphStore interface {
	// AddNode adds or updates a node in the graph (upsert).
	// If a node with the same ID already exists, its type, component_id,
	// metadata, and last_seen fields are updated. The first_seen field
	// is preserved from the original insert.
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

	// ListNodesByComponent returns all nodes for a given component_id
	ListNodesByComponent(ctx context.Context, sourceID string) ([]Node, error)

	// ListEdgesForNodes returns edges where both endpoints are in nodeIDs
	ListEdgesForNodes(ctx context.Context, nodeIDs []string) ([]Edge, error)

	// ListAll returns all nodes and edges in the graph (capped at 5000 nodes).
	ListAll(ctx context.Context) (*Subgraph, error)
}

// Node represents a node in the infrastructure graph
type Node struct {
	ID          string
	Type        string
	ComponentID string
	Metadata    map[string]any
	FirstSeen   time.Time
	LastSeen    time.Time
}

// Edge represents a relationship between two nodes
type Edge struct {
	From        string
	To          string
	Relation    string
	Confidence  ConfidenceLevel
	Source      string
	ComponentID string
	Context     string
	CreatedAt   time.Time
}

// ConfidenceLevel represents how certain we are about an edge
type ConfidenceLevel int

// The levels are persisted as raw ints in graph_edges.confidence (migration
// 002, INTEGER DEFAULT 3). Explicit and UserConfirmed both shipped as 3 for a
// while (a duplicate-value bug), so pre-existing rows cannot distinguish the
// two: a stored 3 may be either. Renumbering Explicit to its intended 2 is
// forward-safe because nothing orders or branches on these values today — the
// only reads are serialization and the refresh delta's equality check
// (coreagent/graphdelta.go), which treats an old Explicit-as-3 row as changed
// and rewrites it to 2 on the next reconcile (self-healing for every
// refresh-managed edge). Ambiguous legacy rows outside a refresh loop keep 3
// and merely display as user-confirmed; no migration can recover which they
// meant, so none is attempted.
const (
	// Inferred means the edge was guessed by the LLM, not yet confirmed
	Inferred ConfidenceLevel = 1

	// Explicit means the edge was discovered from API or .joe/ file
	Explicit ConfidenceLevel = 2

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
