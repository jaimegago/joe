package graph

import (
	"context"
	"database/sql"
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

	// ListComponentBindings returns the edges that BIND componentID to some
	// other component: every edge with one endpoint carrying componentID and
	// the other endpoint carrying a different, non-empty component_id. Edges
	// wholly inside the component are excluded — they bind it to itself and
	// say nothing about what it is attached to — and so are edges whose far
	// endpoint carries no component attribution, because a node belonging to
	// no component cannot be authorized per component and must not be
	// disclosed on the strength of a grant that does not cover it.
	//
	// The result is ordered deterministically (relation, peer component, peer
	// node, near node, direction) and capped at limit rows. A non-positive
	// limit means DefaultComponentBindingLimit. Truncation is not signalled
	// here: the caller detects it by asking for limit+1 and seeing limit+1
	// rows come back, which keeps the store's contract a plain ordered read.
	ListComponentBindings(ctx context.Context, componentID string, limit int) ([]ComponentBinding, error)

	// ListAll returns all nodes and edges in the graph (capped at 5000 nodes).
	ListAll(ctx context.Context) (*Subgraph, error)

	// DeleteNodesByComponentTx removes every graph_nodes row carrying
	// componentID against the caller-supplied transaction, so component
	// deletion cascades graph state atomically with the components row and its
	// audit insert. Edges die with their endpoint nodes via the migration-002
	// graph_edges FK ON DELETE CASCADE. The store never commits or rolls back;
	// the caller owns the transaction.
	DeleteNodesByComponentTx(ctx context.Context, tx *sql.Tx, componentID string) error
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

// Confidence records how strongly a DETERMINISTICALLY-DERIVED edge is
// established — it is a heuristic-strength marker, not an authority tier. Every
// edge in the graph is produced by Joe-authored adapter and refresher code
// through the delta-reconcile seam; no LLM authors graph structure (D-0110).
// The levels distinguish how the deriving code reached the relationship, not who
// or what asserted it.
const (
	// Inferred means the edge was derived by a heuristic match rather than a
	// confirmed identifier — e.g. the gitops/terraform refreshers' name (plus
	// namespace, where known) matching in buildManagedByEdges/buildProvidesEdges
	// (internal/coreagent/gitops_refresh.go). Deterministic, but a same-named
	// resource in scope can produce a wrong edge.
	Inferred ConfidenceLevel = 1

	// Explicit means the edge rests on an identifier the source system confirmed,
	// or on a relationship an API reported directly — e.g. an owner reference or
	// a resource ID carried in the payload, not a name guessed to match.
	Explicit ConfidenceLevel = 2

	// UserConfirmed means the user explicitly confirmed this edge
	UserConfirmed ConfidenceLevel = 3
)

// String renders a confidence level for a reader — a payload handed to the
// agent loop, a log line — rather than for storage, which uses the raw int.
//
// A stored 3 renders as "user_confirmed". Per the note above that is not always
// what the writer meant: Explicit and UserConfirmed both shipped as 3 for a
// while, so an ambiguous legacy row outside a refresh loop displays as
// user-confirmed. No migration can recover which it meant, and this method does
// not pretend otherwise — it renders what is stored.
func (c ConfidenceLevel) String() string {
	switch c {
	case Inferred:
		return "inferred"
	case Explicit:
		return "explicit"
	case UserConfirmed:
		return "user_confirmed"
	default:
		return "unknown"
	}
}

// Binding direction, as seen from the component asked about: BindingOut means
// the component's own node is the edge's from-node, BindingIn means it is the
// to-node. Relations in this graph are directional and their meaning depends on
// which end you are standing on — metrics_in from a service names the backend
// that scrapes it, metrics_in into a Prometheus component names a service it
// scrapes — so a binding that dropped the direction would be ambiguous.
const (
	BindingOut = "out"
	BindingIn  = "in"
)

// DefaultComponentBindingLimit bounds ListComponentBindings when the caller
// names no limit. It is a bound on evidence returned for ONE component, not a
// bound on the graph: a Kubernetes cluster can carry thousands of nodes, and a
// caller reading a candidate list wants enough relations to tell two candidates
// apart, not an inventory.
const DefaultComponentBindingLimit = 50

// ComponentBinding is one graph edge seen from a component's point of view: the
// endpoint that belongs to that component, the relation, and the far endpoint
// together with the component it belongs to.
//
// It is deliberately NOT an Edge. An Edge names two node IDs and leaves the
// reader to look up what component each belongs to; the question this type
// answers — what is this component bound to, and via which relation — is the
// component-level one, and answering it from Edge would take a second read per
// endpoint.
type ComponentBinding struct {
	// NodeID and NodeType are the endpoint belonging to the component asked
	// about.
	NodeID   string
	NodeType string

	// Relation is the graph relation constant (see relations.go).
	Relation string

	// Direction is BindingOut or BindingIn, from the asked-about component's
	// point of view.
	Direction string

	// Confidence is the edge's confidence level, carried so a reader can tell
	// a name-matched heuristic edge from one resting on a confirmed
	// identifier.
	Confidence ConfidenceLevel

	// PeerNodeID, PeerNodeType and PeerComponentID are the far endpoint.
	// PeerComponentID is never empty: an unattributed far endpoint is not
	// returned at all.
	PeerNodeID      string
	PeerNodeType    string
	PeerComponentID string
}

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
