package coreagent

import (
	"context"
	"fmt"
	"time"

	azureadapter "github.com/jaimegago/joe/internal/adapters/azure"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
)

func (r *Refresher) refreshAzureSource(ctx context.Context, source *store.Source, adapter azureadapter.AzureAdapter) error {
	start := time.Now()
	r.logger.Info("refreshing azure source", "source_id", source.ID)

	now := time.Now()
	desiredNodes := make([]graph.Node, 0)
	desiredEdges := make([]graph.Edge, 0)
	vnetIndex := make(map[string]string)

	vnets, err := adapter.ListVNets(ctx)
	if err != nil {
		return fmt.Errorf("list vnets: %w", err)
	}
	for _, vnet := range vnets {
		vnetID := azureResourceID(vnet.ID, vnet.Name)
		nodeID := azureNodeID(source.ID, "vnet", vnetID)
		metadata := map[string]any{
			"id":       vnet.ID,
			"name":     vnet.Name,
			"address":  vnet.Address,
			"location": vnet.Location,
		}
		if len(vnet.Tags) > 0 {
			metadata["tags"] = vnet.Tags
		}
		desiredNodes = append(desiredNodes, graph.Node{
			ID:       nodeID,
			Type:     "vnet",
			SourceID: source.ID,
			Metadata: metadata,
			LastSeen: now,
		})
		vnetIndex[vnetID] = nodeID
	}

	vms, err := adapter.ListVMs(ctx)
	if err != nil {
		return fmt.Errorf("list vms: %w", err)
	}
	for _, vm := range vms {
		vmID := azureResourceID(vm.ID, vm.Name)
		nodeID := azureNodeID(source.ID, "vm", vmID)
		metadata := map[string]any{
			"id":        vm.ID,
			"name":      vm.Name,
			"size":      vm.Size,
			"state":     vm.State,
			"vnet_id":   vm.VNetID,
			"subnet_id": vm.SubnetID,
			"location":  vm.Location,
		}
		if len(vm.Tags) > 0 {
			metadata["tags"] = vm.Tags
		}
		desiredNodes = append(desiredNodes, graph.Node{
			ID:       nodeID,
			Type:     "vm",
			SourceID: source.ID,
			Metadata: metadata,
			LastSeen: now,
		})

		if vm.VNetID != "" {
			if vnetNodeID, ok := vnetIndex[vm.VNetID]; ok {
				desiredEdges = append(desiredEdges, graph.Edge{
					From:       nodeID,
					To:         vnetNodeID,
					Relation:   "in_vnet",
					Confidence: graph.Explicit,
					Source:     "azure_api",
					Context:    "vnet_id",
				})
			}
		}
	}

	aksClusters, err := adapter.ListAKSClusters(ctx)
	if err != nil {
		return fmt.Errorf("list aks clusters: %w", err)
	}
	for _, cluster := range aksClusters {
		clusterID := azureResourceID(cluster.ID, cluster.Name)
		nodeID := azureNodeID(source.ID, "aks", clusterID)
		metadata := map[string]any{
			"id":         cluster.ID,
			"name":       cluster.Name,
			"version":    cluster.Version,
			"status":     cluster.Status,
			"vnet_id":    cluster.VNetID,
			"subnet_ids": cluster.SubnetIDs,
			"location":   cluster.Location,
		}
		if len(cluster.Tags) > 0 {
			metadata["tags"] = cluster.Tags
		}
		desiredNodes = append(desiredNodes, graph.Node{
			ID:       nodeID,
			Type:     "aks_cluster",
			SourceID: source.ID,
			Metadata: metadata,
			LastSeen: now,
		})

		if cluster.VNetID != "" {
			if vnetNodeID, ok := vnetIndex[cluster.VNetID]; ok {
				desiredEdges = append(desiredEdges, graph.Edge{
					From:       nodeID,
					To:         vnetNodeID,
					Relation:   "in_vnet",
					Confidence: graph.Explicit,
					Source:     "azure_api",
					Context:    "vnet_id",
				})
			}
		}
	}

	sqlDatabases, err := adapter.ListSQLDatabases(ctx)
	if err != nil {
		return fmt.Errorf("list sql databases: %w", err)
	}
	for _, db := range sqlDatabases {
		dbID := azureResourceID(db.ID, db.Name)
		nodeID := azureNodeID(source.ID, "sql", dbID)
		metadata := map[string]any{
			"id":          db.ID,
			"name":        db.Name,
			"server_name": db.ServerName,
			"edition":     db.Edition,
			"status":      db.Status,
			"vnet_id":     db.VNetID,
			"location":    db.Location,
		}
		if len(db.Tags) > 0 {
			metadata["tags"] = db.Tags
		}
		desiredNodes = append(desiredNodes, graph.Node{
			ID:       nodeID,
			Type:     "sql_database",
			SourceID: source.ID,
			Metadata: metadata,
			LastSeen: now,
		})

		if db.VNetID != "" {
			if vnetNodeID, ok := vnetIndex[db.VNetID]; ok {
				desiredEdges = append(desiredEdges, graph.Edge{
					From:       nodeID,
					To:         vnetNodeID,
					Relation:   "in_vnet",
					Confidence: graph.Explicit,
					Source:     "azure_api",
					Context:    "vnet_id",
				})
			}
		}
	}

	// Cross-source: match Azure VMs to K8s nodes by VM name → is_k8s_node edges.
	desiredEdges = append(desiredEdges, r.buildIsK8sNodeEdgesFromVMs(ctx, source, vms, now)...)

	existingNodes, existingEdges, err := LoadGraphStateForSource(ctx, r.services.Graph, source.ID)
	if err != nil {
		return err
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, desiredEdges)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return err
	}

	r.logger.Info("azure refresh completed", "source_id", source.ID, "nodes", len(desiredNodes), "edges", len(desiredEdges), "duration_ms", time.Since(start).Milliseconds())
	return nil
}

// buildIsK8sNodeEdgesFromVMs creates is_k8s_node edges by matching Azure VM names
// against K8s node names or hostnames already in the graph. AKS node names typically
// match the VM name.
func (r *Refresher) buildIsK8sNodeEdgesFromVMs(ctx context.Context, source *store.Source, vms []azureadapter.VM, now time.Time) []graph.Edge {
	// Build a lookup index: node name (and hostname) → K8s node graph ID.
	k8sNodes, err := r.services.Graph.Query(ctx, "type:node")
	if err != nil || len(k8sNodes) == 0 {
		return nil
	}
	nameToK8sNodeID := make(map[string]string, len(k8sNodes))
	for _, n := range k8sNodes {
		if name, ok := n.Metadata["name"].(string); ok && name != "" {
			nameToK8sNodeID[name] = n.ID
		}
		if hostname, ok := n.Metadata["hostname"].(string); ok && hostname != "" {
			nameToK8sNodeID[hostname] = n.ID
		}
	}

	var edges []graph.Edge
	for _, vm := range vms {
		if vm.Name == "" {
			continue
		}
		k8sNodeID, ok := nameToK8sNodeID[vm.Name]
		if !ok {
			continue
		}
		vmID := azureResourceID(vm.ID, vm.Name)
		edges = append(edges, graph.Edge{
			From:       azureNodeID(source.ID, "vm", vmID),
			To:         k8sNodeID,
			Relation:   graph.RelationIsK8sNode,
			Confidence: graph.Inferred,
			Source:     "azure_k8s_name_match",
			SourceID:   source.ID,
			Context:    "vm_name=" + vm.Name,
			CreatedAt:  now,
		})
	}
	return edges
}

func azureNodeID(sourceID, service, resourceID string) string {
	return fmt.Sprintf("azure/%s/%s/%s", sourceID, service, resourceID)
}

func azureResourceID(id, name string) string {
	if id != "" {
		return id
	}
	return name
}
