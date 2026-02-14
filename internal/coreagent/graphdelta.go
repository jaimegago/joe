package coreagent

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/graph"
)

// GraphDelta captures changes needed to reconcile graph state.
type GraphDelta struct {
	NodesToUpsert []graph.Node
	NodesToDelete []graph.Node
	EdgesToUpsert []graph.Edge
	EdgesToDelete []graph.Edge
}

// LoadGraphStateForSource returns current nodes and edges for a source.
func LoadGraphStateForSource(ctx context.Context, store graph.GraphStore, sourceID string) ([]graph.Node, []graph.Edge, error) {
	nodes, err := store.ListNodesBySource(ctx, sourceID)
	if err != nil {
		return nil, nil, fmt.Errorf("list nodes for source %s: %w", sourceID, err)
	}

	if len(nodes) == 0 {
		return nodes, []graph.Edge{}, nil
	}

	nodeIDs := make([]string, 0, len(nodes))
	for _, node := range nodes {
		nodeIDs = append(nodeIDs, node.ID)
	}

	edges, err := store.ListEdgesForNodes(ctx, nodeIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("list edges for source %s: %w", sourceID, err)
	}

	return nodes, edges, nil
}

// BuildGraphDelta computes the delta between existing and desired state.
// Nodes are always upserted to refresh last_seen.
func BuildGraphDelta(existingNodes []graph.Node, existingEdges []graph.Edge, desiredNodes []graph.Node, desiredEdges []graph.Edge) GraphDelta {
	existingNodeIDs := make(map[string]graph.Node, len(existingNodes))
	for _, node := range existingNodes {
		existingNodeIDs[node.ID] = node
	}

	desiredNodeIDs := make(map[string]graph.Node, len(desiredNodes))
	for i := range desiredNodes {
		node := desiredNodes[i]
		if existing, ok := existingNodeIDs[node.ID]; ok {
			if node.FirstSeen.IsZero() {
				node.FirstSeen = existing.FirstSeen
			}
		}
		desiredNodes[i] = node
		desiredNodeIDs[node.ID] = node
	}

	var nodesToDelete []graph.Node
	deletedNodeIDs := make(map[string]struct{})
	for id, node := range existingNodeIDs {
		if _, ok := desiredNodeIDs[id]; !ok {
			nodesToDelete = append(nodesToDelete, node)
			deletedNodeIDs[id] = struct{}{}
		}
	}

	existingEdgeKeys := make(map[string]graph.Edge, len(existingEdges))
	for _, edge := range existingEdges {
		existingEdgeKeys[edgeKey(edge)] = edge
	}

	desiredEdgeKeys := make(map[string]graph.Edge, len(desiredEdges))
	uniqueDesiredEdges := make([]graph.Edge, 0, len(desiredEdges))
	for _, edge := range desiredEdges {
		key := edgeKey(edge)
		if _, ok := desiredEdgeKeys[key]; ok {
			continue
		}
		desiredEdgeKeys[key] = edge
		uniqueDesiredEdges = append(uniqueDesiredEdges, edge)
	}

	var edgesToDelete []graph.Edge
	for key, edge := range existingEdgeKeys {
		if _, ok := desiredEdgeKeys[key]; !ok {
			if _, removingFrom := deletedNodeIDs[edge.From]; removingFrom {
				continue
			}
			if _, removingTo := deletedNodeIDs[edge.To]; removingTo {
				continue
			}
			edgesToDelete = append(edgesToDelete, edge)
		}
	}

	var edgesToUpsert []graph.Edge
	for _, edge := range uniqueDesiredEdges {
		if existing, ok := existingEdgeKeys[edgeKey(edge)]; ok {
			if edgeEqual(existing, edge) {
				continue
			}
		}
		edgesToUpsert = append(edgesToUpsert, edge)
	}

	return GraphDelta{
		NodesToUpsert: desiredNodes,
		NodesToDelete: nodesToDelete,
		EdgesToUpsert: edgesToUpsert,
		EdgesToDelete: edgesToDelete,
	}
}

// ApplyGraphDelta applies the delta to the graph store.
func ApplyGraphDelta(ctx context.Context, store graph.GraphStore, delta GraphDelta) error {
	for _, node := range delta.NodesToUpsert {
		if err := store.AddNode(ctx, node); err != nil {
			return fmt.Errorf("upsert node %s: %w", node.ID, err)
		}
	}

	for _, edge := range delta.EdgesToUpsert {
		if err := store.AddEdge(ctx, edge); err != nil {
			return fmt.Errorf("upsert edge %s->%s (%s): %w", edge.From, edge.To, edge.Relation, err)
		}
	}

	for _, edge := range delta.EdgesToDelete {
		if err := store.DeleteEdge(ctx, edge.From, edge.To, edge.Relation); err != nil {
			return fmt.Errorf("delete edge %s->%s (%s): %w", edge.From, edge.To, edge.Relation, err)
		}
	}

	for _, node := range delta.NodesToDelete {
		if err := store.DeleteNode(ctx, node.ID); err != nil && err != graph.ErrNodeNotFound {
			return fmt.Errorf("delete node %s: %w", node.ID, err)
		}
	}

	return nil
}

func edgeKey(edge graph.Edge) string {
	return fmt.Sprintf("%s|%s|%s", edge.From, edge.To, edge.Relation)
}

func edgeEqual(existing, desired graph.Edge) bool {
	return existing.Confidence == desired.Confidence && existing.Source == desired.Source && existing.Context == desired.Context
}
