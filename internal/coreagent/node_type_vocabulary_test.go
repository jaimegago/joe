package coreagent

import (
	"context"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
)

// TestBuildProvidesEdges_ReachesAzureTypes pins the Azure half of the
// declared-to-live bridge. The provides-matcher once selected azure_vm,
// azure_aks and azure_sql — types the azure refresher never writes — so a
// terraform resource could never produce a provisions edge to any Azure node.
// Each case stages the type azure_refresh.go actually writes.
func TestBuildProvidesEdges_ReachesAzureTypes(t *testing.T) {
	for _, nodeType := range []string{graph.NodeTypeVM, graph.NodeTypeAKSCluster, graph.NodeTypeSQLDatabase} {
		t.Run(nodeType, func(t *testing.T) {
			r := setupGitOpsRefresher(t)
			ctx := context.Background()
			src := &store.Component{ID: "src-tf-azure", Type: store.ComponentTypeTerraform}

			_ = r.services.Graph.AddNode(ctx, graph.Node{
				ID:          "azure/" + nodeType + "/web",
				Type:        nodeType,
				ComponentID: "src-azure",
				Metadata:    map[string]any{"name": "web"},
			})

			edges := r.buildProvidesEdges(ctx, src, "tf-resource-node", "web", "azurerm_resource", time.Now())
			if len(edges) != 1 {
				t.Fatalf("want 1 provisions edge to a %s node, got %d", nodeType, len(edges))
			}
			if edges[0].Relation != graph.RelationProvisions {
				t.Errorf("edge relation = %q, want %q", edges[0].Relation, graph.RelationProvisions)
			}
		})
	}
}

// TestCrossplaneTargetsAzureVM pins the Crossplane spec's Azure target to the
// type the azure refresher writes (vm), not the azure_vm it once listed.
func TestCrossplaneTargetsAzureVM(t *testing.T) {
	for _, spec := range crdRefreshSpecs {
		if spec.NodeType != graph.NodeTypeCrossplaneResource {
			continue
		}
		if !containsType(spec.TargetTypes, graph.NodeTypeVM) {
			t.Errorf("crossplane TargetTypes = %v, want it to include %q", spec.TargetTypes, graph.NodeTypeVM)
		}
		return
	}
	t.Fatal("no crossplane spec in crdRefreshSpecs")
}
