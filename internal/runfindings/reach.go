package runfindings

import (
	"context"
	"fmt"
	"sort"

	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/rbac"
)

// GraphReader is the guarded graph read this package uses. It is the shape of
// the accessor's graph reads (internal/access, GraphGetNode and GraphRelated):
// every read carries the run's principal and is permitted at the accessor, so
// a run sees exactly what the refresher sees and nothing here reaches the
// graph store directly (the Phase A invariant,
// internal/api/access_guard_test.go). *access.Accessor satisfies it.
type GraphReader interface {
	GraphGetNode(ctx context.Context, principal rbac.Principal, id string) (*graph.Node, error)
	GraphRelated(ctx context.Context, principal rbac.Principal, nodeID string, depth int) (*graph.Subgraph, error)
}

// Dependency edges. Reach is read from the graph's dependency edges walked in
// the direction of dependence, and every relation in the graph is directional
// with a meaning that depends on which end you stand on (graph.ComponentBinding
// says the same). This table is the whole of what "dependency edge" means
// here: a relation absent from it — the observability relations (metrics_in,
// logs_in, traces_in, alerts_in, paged_via, dashboard_in), the provenance
// relations (publishes_to, hosted_by, provisions) and the policy relations —
// does not carry availability and is not walked. The set is the build's cell,
// not the design's bound; a relation added to the graph is added here only if
// its from-end stops working when its to-end does, or the reverse.
type dependence int

const (
	// fromDependsOnTo: the edge's from-node depends on its to-node. A service
	// stores_in a database; the service is the dependent.
	fromDependsOnTo dependence = iota
	// toDependsOnFrom: the edge's to-node depends on its from-node. A
	// certificate secures a service; the service is the dependent.
	toDependsOnFrom
)

var dependencyRelations = map[string]dependence{
	graph.RelationStoresIn:      fromDependsOnTo,
	graph.RelationQueuesIn:      fromDependsOnTo,
	graph.RelationImageStoredIn: fromDependsOnTo,
	graph.RelationIngressFor:    fromDependsOnTo,
	graph.RelationProxies:       fromDependsOnTo,
	graph.RelationManagedBy:     fromDependsOnTo,
	graph.RelationSecures:       toDependsOnFrom,
}

// DependencyRelations lists the relations reach walks, sorted, for a reader or
// a test. The direction each is walked in is not exported: a caller has no use
// for it, and ComputeReach is the one consumer.
func DependencyRelations() []string {
	out := make([]string, 0, len(dependencyRelations))
	for r := range dependencyRelations {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// reachDepth bounds the transitive walk. Dependency chains in this graph are
// short — workload → datastore, ingress → service → image — and a bound keeps
// the subgraph read finite on a cycle-bearing graph.
const reachDepth = 8

// ComputeReach walks the graph's dependency edges from subjectNodeID in the
// direction of dependence and returns the nodes that depend on it,
// transitively. A subject nothing depends on has no reach. The walk is code
// over a guarded read as principal; the model never asserts reach.
func ComputeReach(ctx context.Context, reader GraphReader, principal rbac.Principal, subjectNodeID string) (Reach, error) {
	if reader == nil {
		return Reach{}, fmt.Errorf("compute reach: graph reader is nil")
	}
	sub, err := reader.GraphRelated(ctx, principal, subjectNodeID, reachDepth)
	if err != nil {
		return Reach{}, fmt.Errorf("compute reach for %s: %w", subjectNodeID, err)
	}
	componentOf := make(map[string]string, len(sub.Nodes))
	for _, n := range sub.Nodes {
		componentOf[n.ID] = n.ComponentID
	}
	// dependentsOf[x] = nodes that depend on x, one hop.
	dependentsOf := make(map[string][]string)
	for _, e := range sub.Edges {
		dir, ok := dependencyRelations[e.Relation]
		if !ok {
			continue
		}
		dependent, dependency := e.From, e.To
		if dir == toDependsOnFrom {
			dependent, dependency = e.To, e.From
		}
		dependentsOf[dependency] = append(dependentsOf[dependency], dependent)
	}
	seen := map[string]struct{}{subjectNodeID: {}}
	queue := []string{subjectNodeID}
	var dependents []string
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, d := range dependentsOf[cur] {
			if _, dup := seen[d]; dup {
				continue
			}
			seen[d] = struct{}{}
			dependents = append(dependents, d)
			queue = append(queue, d)
		}
	}
	sort.Strings(dependents)
	compSet := make(map[string]struct{})
	for _, d := range dependents {
		if c := componentOf[d]; c != "" {
			compSet[c] = struct{}{}
		}
	}
	comps := make([]string, 0, len(compSet))
	for c := range compSet {
		comps = append(comps, c)
	}
	sort.Strings(comps)
	r := Reach{Dependents: dependents, Components: comps, Count: len(dependents)}
	if len(r.Dependents) > MaxDerivationDependents {
		r.Dependents = r.Dependents[:MaxDerivationDependents]
	}
	if r.Dependents == nil {
		r.Dependents = []string{}
	}
	if r.Components == nil {
		r.Components = []string{}
	}
	return r, nil
}
