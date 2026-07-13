package coreagent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jaimegago/joe/internal/adapters/k8s"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// crdRefreshSpec describes a CRD resource type to discover and the relation to emit.
type crdRefreshSpec struct {
	// Resource is the fully-qualified resource identifier passed to
	// ListResources, in the resolver's "group/version/resource" form
	// (e.g. "keda.sh/v1alpha1/scaledobjects"). This is the ONLY shape
	// k8s.ResolveGVR accepts for a CRD — the version is required because the
	// dynamic client addresses "/apis/{group}/{version}/{resource}" directly
	// (there is no discovery-client lookup to infer it). The older
	// "resource.group" form silently never resolved (D-0094).
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
		Resource:    "keda.sh/v1alpha1/scaledobjects",
		NodeType:    "keda_scaledobject",
		Relation:    graph.RelationScaledBy,
		TargetField: "spec.scaleTargetRef.name",
		TargetTypes: []string{"deployment", "statefulset", "daemonset"},
	},
	{
		// cert-manager Certificates: secures from Certificate → service/ingress.
		Resource:    "cert-manager.io/v1/certificates",
		NodeType:    "certificate",
		Relation:    graph.RelationSecures,
		TargetField: "", // use cert name (matches the ingress/service it secures)
		TargetTypes: []string{"service", "deployment", "nginx_ingress"},
	},
	{
		// OPA ConstraintTemplates: policy_enforces from template → namespace/workload.
		Resource:    "templates.gatekeeper.sh/v1/constrainttemplates",
		NodeType:    "opa_constraint_template",
		Relation:    graph.RelationPolicyEnforces,
		TargetField: "",
		TargetTypes: []string{"namespace", "deployment"},
	},
	{
		// Cilium NetworkPolicies: policy_enforces from policy → namespace/workload.
		Resource:    "cilium.io/v2/ciliumnetworkpolicies",
		NodeType:    "cilium_network_policy",
		Relation:    graph.RelationPolicyEnforces,
		TargetField: "spec.endpointSelector.matchLabels.app",
		TargetTypes: []string{"namespace", "deployment"},
	},
	{
		// Istio VirtualServices: mesh_for from VirtualService → service.
		Resource:    "networking.istio.io/v1beta1/virtualservices",
		NodeType:    "istio_virtual_service",
		Relation:    graph.RelationMeshFor,
		TargetField: "", // use VS name which typically matches the service
		TargetTypes: []string{"service", "deployment"},
	},
	{
		// Crossplane Managed Resources: provisions from XR → cloud node.
		Resource:    "apiextensions.crossplane.io/v1/composites",
		NodeType:    "crossplane_resource",
		Relation:    graph.RelationProvisions,
		TargetField: "",
		TargetTypes: []string{"ec2_instance", "rds_instance", "azure_vm", "node"},
	},
}

// refreshK8sCRDs discovers CRD-based resources from a K8s source and returns
// the nodes and edges to add to the graph, plus the set of CRD types skipped
// because the credential could not list them (forbidden). It is called by
// refreshK8sComponent after the core resource refresh completes.
//
// A forbidden CRD list is DEGRADATION (surfaced as a skip); an uninstalled CRD
// (not-found) is a silent Debug skip — an operator absence, not a permission
// gap; any other list error keeps the existing tolerant Debug skip and never
// aborts the refresh (a CRD transport blip must not fail the whole tick).
func (r *Refresher) refreshK8sCRDs(ctx context.Context, source *store.Component, adapter k8s.KubernetesAdapter) ([]graph.Node, []graph.Edge, []resourceSkip) {
	now := time.Now()
	var nodes []graph.Node
	var edges []graph.Edge
	var skips []resourceSkip

	for _, spec := range crdRefreshSpecs {
		crdNodes, crdEdges, crdSkip := r.refreshCRDSpec(ctx, source, adapter, spec, now)
		nodes = append(nodes, crdNodes...)
		edges = append(edges, crdEdges...)
		if crdSkip != nil {
			skips = append(skips, *crdSkip)
		}
	}

	return nodes, edges, skips
}

// refreshCRDSpec discovers one CRD resource type, creates graph nodes, and
// attempts to build edges to matching workload/service nodes. It returns a
// non-nil skip only when the list was forbidden (degradation); every other
// error path returns a nil skip and a Debug log, unchanged.
func (r *Refresher) refreshCRDSpec(ctx context.Context, source *store.Component, adapter k8s.KubernetesAdapter, spec crdRefreshSpec, now time.Time) ([]graph.Node, []graph.Edge, *resourceSkip) {
	items, err := adapter.ListResources(ctx, spec.Resource, "")
	if err != nil {
		switch {
		case apierrors.IsForbidden(err):
			// The credential may not list this CRD type — degradation, not an
			// operator absence. Record a skip keyed by the CRD short name.
			r.logger.Debug("crd list forbidden (degraded)", "resource", spec.Resource, "component_id", source.ID)
			return nil, nil, &resourceSkip{Type: crdShortName(spec.Resource), Reason: forbiddenSkipReason}
		case apierrors.IsNotFound(err):
			// CRD not installed in this cluster — expected, silent, not degradation.
			r.logger.Debug("crd not installed (skipping)", "resource", spec.Resource, "component_id", source.ID)
			return nil, nil, nil
		default:
			// Any other error (transport blip, GVR resolution) — tolerant Debug
			// skip, unchanged; never aborts the refresh.
			r.logger.Debug("crd not available (skipping)", "resource", spec.Resource, "component_id", source.ID, "error", err)
			return nil, nil, nil
		}
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

	return nodes, edges, nil
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

// crdShortName extracts the plural resource name from a "group/version/resource"
// identifier for use in node IDs, skip keys, and edge context.
// "keda.sh/v1alpha1/scaledobjects" → "scaledobjects". A string with no "/" is
// returned unchanged.
func crdShortName(resource string) string {
	if idx := strings.LastIndexByte(resource, '/'); idx >= 0 {
		return resource[idx+1:]
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
