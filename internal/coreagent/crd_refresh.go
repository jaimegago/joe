package coreagent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jaimegago/joe/internal/adapters/k8s"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// crdRefreshSpec describes a CRD resource type to discover and the relation to emit.
type crdRefreshSpec struct {
	// Resource is the plural resource name as passed to ListResources.
	// Format: "resource.group" (e.g. "scaledobjects.keda.sh").
	Resource string
	// NodeType is the graph node type to assign to discovered CRD objects.
	NodeType string
	// Relation is the graph relation to emit from the CRD node to the target.
	Relation string
	// TargetField is a dot-separated path inside spec to find the target name.
	// When empty the object name is used for matching.
	TargetField string
	// TargetTypes are the node types accepted as edge targets.
	TargetTypes []string
}

// crdRefreshSpecs is the list of CRD resource types refreshed from K8s components.
var crdRefreshSpecs = []crdRefreshSpec{
	{
		// KEDA ScaledObjects: scaled_by from ScaledObject → workload.
		Resource:    "scaledobjects.keda.sh",
		NodeType:    "keda_scaledobject",
		Relation:    graph.RelationScaledBy,
		TargetField: "spec.scaleTargetRef.name",
		TargetTypes: []string{"deployment", "statefulset", "daemonset"},
	},
	{
		// cert-manager Certificates: secures from Certificate → service/ingress.
		Resource:    "certificates.cert-manager.io",
		NodeType:    "certificate",
		Relation:    graph.RelationSecures,
		TargetField: "", // use cert name (matches the ingress/service it secures)
		TargetTypes: []string{"service", "deployment", "nginx_ingress"},
	},
	{
		// OPA ConstraintTemplates: policy_enforces from template → namespace/workload.
		Resource:    "constrainttemplates.templates.gatekeeper.sh",
		NodeType:    "opa_constraint_template",
		Relation:    graph.RelationPolicyEnforces,
		TargetField: "",
		TargetTypes: []string{"namespace", "deployment"},
	},
	{
		// Cilium NetworkPolicies: policy_enforces from policy → namespace/workload.
		Resource:    "ciliumnetworkpolicies.cilium.io",
		NodeType:    "cilium_network_policy",
		Relation:    graph.RelationPolicyEnforces,
		TargetField: "spec.endpointSelector.matchLabels.app",
		TargetTypes: []string{"namespace", "deployment"},
	},
	{
		// Istio VirtualServices: mesh_for from VirtualService → service.
		Resource:    "virtualservices.networking.istio.io",
		NodeType:    "istio_virtual_service",
		Relation:    graph.RelationMeshFor,
		TargetField: "", // use VS name which typically matches the service
		TargetTypes: []string{"service", "deployment"},
	},
	{
		// Crossplane Managed Resources: provisions from XR → cloud node.
		Resource:    "composites.apiextensions.crossplane.io",
		NodeType:    "crossplane_resource",
		Relation:    graph.RelationProvisions,
		TargetField: "",
		TargetTypes: []string{"ec2_instance", "rds_instance", "azure_vm", "node"},
	},
}

// refreshK8sCRDs discovers CRD-based resources from a K8s source and returns
// the nodes and edges to add to the graph. It is called by refreshK8sComponent
// after the core resource refresh completes.
//
// Errors from individual CRD list calls are logged and skipped — missing CRDs
// are expected in clusters that don't have those operators installed.
func (r *Refresher) refreshK8sCRDs(ctx context.Context, source *store.Component, adapter k8s.KubernetesAdapter) ([]graph.Node, []graph.Edge) {
	now := time.Now()
	var nodes []graph.Node
	var edges []graph.Edge

	for _, spec := range crdRefreshSpecs {
		crdNodes, crdEdges := r.refreshCRDSpec(ctx, source, adapter, spec, now)
		nodes = append(nodes, crdNodes...)
		edges = append(edges, crdEdges...)
	}

	return nodes, edges
}

// refreshCRDSpec discovers one CRD resource type, creates graph nodes, and
// attempts to build edges to matching workload/service nodes.
func (r *Refresher) refreshCRDSpec(ctx context.Context, source *store.Component, adapter k8s.KubernetesAdapter, spec crdRefreshSpec, now time.Time) ([]graph.Node, []graph.Edge) {
	items, err := adapter.ListResources(ctx, spec.Resource, "")
	if err != nil {
		// CRD not installed in this cluster — not an error condition.
		r.logger.Debug("crd not available (skipping)", "resource", spec.Resource, "component_id", source.ID, "error", err)
		return nil, nil
	}

	var nodes []graph.Node
	var edges []graph.Edge

	for i := range items {
		obj := items[i]
		name := obj.GetName()
		namespace := obj.GetNamespace()

		nodeID := fmt.Sprintf("crd/%s/%s/%s/%s", source.ID, crdShortName(spec.Resource), namespace, name)
		node := graph.Node{
			ID:          nodeID,
			Type:        spec.NodeType,
			ComponentID: source.ID,
			Metadata: map[string]any{
				"name":      name,
				"namespace": namespace,
				"kind":      obj.GetKind(),
			},
			LastSeen: now,
		}
		nodes = append(nodes, node)

		// Determine the target name for edge discovery.
		targetName := resolveCRDTarget(&obj, spec.TargetField, name)
		if targetName == "" {
			continue
		}

		matchingNodes, queryErr := r.services.Graph.Query(ctx, targetName)
		if queryErr != nil {
			r.logger.Debug("graph query failed for crd edge", "resource", spec.Resource, "target", targetName, "error", queryErr)
			continue
		}

		for _, target := range matchingNodes {
			if !containsType(spec.TargetTypes, target.Type) {
				continue
			}
			// Prefer same-namespace matches.
			if namespace != "" {
				targetNS, _ := target.Metadata["namespace"].(string)
				if targetNS != "" && targetNS != namespace {
					continue
				}
			}
			edges = append(edges, graph.Edge{
				From:        nodeID,
				To:          target.ID,
				Relation:    spec.Relation,
				Confidence:  graph.Inferred,
				Source:      "k8s_crd",
				ComponentID: source.ID,
				Context:     crdShortName(spec.Resource) + "=" + name,
				CreatedAt:   now,
			})
		}
	}

	return nodes, edges
}

// resolveCRDTarget extracts the target name from a CRD object.
// fieldPath is a dot-separated path through the object spec
// (e.g. "spec.scaleTargetRef.name"). When empty, the object name is returned.
func resolveCRDTarget(obj *unstructured.Unstructured, fieldPath, fallback string) string {
	if fieldPath == "" {
		return fallback
	}

	parts := strings.Split(fieldPath, ".")
	value, found, err := unstructured.NestedString(obj.Object, parts...)
	if err != nil || !found || value == "" {
		return fallback
	}
	return value
}

// crdShortName extracts a short identifier from a fully-qualified resource name.
// "scaledobjects.keda.sh" → "scaledobjects"
func crdShortName(resource string) string {
	if idx := strings.IndexByte(resource, '.'); idx > 0 {
		return resource[:idx]
	}
	return resource
}

// containsType reports whether target is in the allowed types slice.
func containsType(types []string, target string) bool {
	for _, t := range types {
		if t == target {
			return true
		}
	}
	return false
}
