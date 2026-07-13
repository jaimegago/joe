package coreagent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jaimegago/joe/internal/adapters/k8s"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// resourceSkip records that one resource type was omitted from a kubernetes
// refresh tick because the component's credential lacked list permission on it
// (a forbidden error). A tick that completes with skips is DEGRADED, not failed:
// it still applies the delta built from the types it could list, and the skips
// surface as the component's degraded status and last_error summary (D-0093).
type resourceSkip struct {
	// Type is the graph node type (core loop) or CRD short name (CRD loop) that
	// was skipped.
	Type string
	// Reason is a short, non-sensitive explanation of why it was skipped.
	Reason string
}

// forbiddenSkipReason is the uniform reason recorded for a resource type the
// credential may not list. Degradation here is forbidden-only by design.
const forbiddenSkipReason = "credential lacks list permission"

// summarizeSkips renders a skip set into a concise, human-readable last_error
// summary, e.g. "degraded: secrets forbidden — credential lacks list
// permission". The type names are sorted for a stable string so transition
// detection (D-0093 logging) does not fire on map-order churn. An empty skip set
// yields the empty string (a healthy tick clears last_error).
func summarizeSkips(skips []resourceSkip) string {
	if len(skips) == 0 {
		return ""
	}
	types := make([]string, 0, len(skips))
	for _, s := range skips {
		types = append(types, s.Type)
	}
	sort.Strings(types)
	return fmt.Sprintf("degraded: %s forbidden — %s", strings.Join(types, ", "), forbiddenSkipReason)
}

type k8sResourceSpec struct {
	Resource   string
	NodeType   string
	Namespaced bool
}

var k8sRefreshResources = []k8sResourceSpec{
	{Resource: "deployments", NodeType: "deployment", Namespaced: true},
	{Resource: "statefulsets", NodeType: "statefulset", Namespaced: true},
	{Resource: "daemonsets", NodeType: "daemonset", Namespaced: true},
	{Resource: "services", NodeType: "service", Namespaced: true},
	{Resource: "configmaps", NodeType: "configmap", Namespaced: true},
	{Resource: "secrets", NodeType: "secret", Namespaced: true},
	{Resource: "namespaces", NodeType: "namespace", Namespaced: false},
	{Resource: "nodes", NodeType: "node", Namespaced: false},
}

type workloadInfo struct {
	ID           string
	NodeType     string
	Namespace    string
	PodLabels    map[string]string
	ConfigMaps   map[string]struct{}
	Secrets      map[string]struct{}
	SelectorInfo map[string]string
}

type serviceInfo struct {
	ID        string
	Namespace string
	Selector  map[string]string
}

// refreshK8sComponent refreshes one kubernetes component. It returns the set of
// resource types skipped this tick because the credential could not list them
// (forbidden), so the caller can surface a degraded status. A non-forbidden list
// error aborts the whole component refresh with the wrapped error and applies no
// delta — today's semantics, unchanged. A tick that completes with skips still
// builds and applies its delta from the types it did list; the reconcile
// consequence that a now-forbidden type's previously discovered nodes are diffed
// out is intended (the graph reflects what the credential can observe, D-0093).
func (r *Refresher) refreshK8sComponent(ctx context.Context, source *store.Component, adapter k8s.KubernetesAdapter) ([]resourceSkip, error) {
	start := time.Now()
	r.logger.Info("refreshing k8s component", "component_id", source.ID)

	desiredNodes := make([]graph.Node, 0)
	desiredEdges := make([]graph.Edge, 0)
	nodeIndex := make(map[string]graph.Node)

	workloads := make([]workloadInfo, 0)
	services := make([]serviceInfo, 0)

	var skips []resourceSkip

	now := time.Now()

	for _, spec := range k8sRefreshResources {
		items, err := adapter.ListResources(ctx, spec.Resource, "")
		if err != nil {
			// Forbidden: the credential may not list this type. Degrade — record
			// the skip and continue to the next type — rather than aborting the
			// whole component refresh (D-0093). The typed check unwraps the
			// adapter's %w-wrapped client-go error; it is not string matching.
			if apierrors.IsForbidden(err) {
				skips = append(skips, resourceSkip{Type: spec.NodeType, Reason: forbiddenSkipReason})
				continue
			}
			// Any other list error keeps today's semantics: abort with the
			// wrapped error, no delta applied.
			return nil, fmt.Errorf("refresh k8s resource %s: %w", spec.Resource, err)
		}

		for i := range items {
			obj := items[i]
			name := obj.GetName()
			namespace := obj.GetNamespace()
			if !spec.Namespaced {
				namespace = ""
			}

			nodeID := k8sNodeID(source.ID, spec.NodeType, namespace, name)
			metadata := buildK8sMetadata(&obj, spec.NodeType, namespace)

			node := graph.Node{
				ID:          nodeID,
				Type:        spec.NodeType,
				ComponentID: source.ID,
				Metadata:    metadata,
				LastSeen:    now,
			}
			desiredNodes = append(desiredNodes, node)
			nodeIndex[node.ID] = node

			switch spec.NodeType {
			case "deployment", "statefulset", "daemonset":
				workloads = append(workloads, extractWorkloadInfo(source.ID, spec.NodeType, namespace, &obj))
			case "service":
				services = append(services, extractServiceInfo(source.ID, namespace, &obj))
			}
		}
	}

	namespaceNodes := make(map[string]string)
	for _, node := range desiredNodes {
		if node.Type == "namespace" {
			name, _ := node.Metadata["name"].(string)
			if name != "" {
				namespaceNodes[name] = node.ID
			}
		}
	}

	addEdge := func(edge graph.Edge) {
		if _, ok := nodeIndex[edge.From]; !ok {
			return
		}
		if _, ok := nodeIndex[edge.To]; !ok {
			return
		}
		desiredEdges = append(desiredEdges, edge)
	}

	// Namespace contains edges.
	for _, node := range desiredNodes {
		if node.Type == "namespace" {
			continue
		}
		namespace, _ := node.Metadata["namespace"].(string)
		if namespace == "" {
			continue
		}
		if nsID, ok := namespaceNodes[namespace]; ok {
			addEdge(graph.Edge{
				From:       nsID,
				To:         node.ID,
				Relation:   "contains",
				Confidence: graph.Explicit,
				Source:     "k8s_api",
				Context:    "namespace",
			})
		}
	}

	// Service selector edges.
	for _, svc := range services {
		if len(svc.Selector) == 0 {
			continue
		}
		for _, wl := range workloads {
			if wl.Namespace != svc.Namespace {
				continue
			}
			if selectorMatches(svc.Selector, wl.PodLabels) {
				addEdge(graph.Edge{
					From:       svc.ID,
					To:         wl.ID,
					Relation:   "routes_to",
					Confidence: graph.Explicit,
					Source:     "k8s_api",
					Context:    "selector_match",
				})
			}
		}
	}

	// Workload -> configmap/secret references.
	for _, wl := range workloads {
		for name := range wl.ConfigMaps {
			cmID := k8sNodeID(source.ID, "configmap", wl.Namespace, name)
			addEdge(graph.Edge{
				From:       wl.ID,
				To:         cmID,
				Relation:   "references",
				Confidence: graph.Explicit,
				Source:     "k8s_api",
				Context:    "configmap_ref",
			})
		}
		for name := range wl.Secrets {
			secretID := k8sNodeID(source.ID, "secret", wl.Namespace, name)
			addEdge(graph.Edge{
				From:       wl.ID,
				To:         secretID,
				Relation:   "references",
				Confidence: graph.Explicit,
				Source:     "k8s_api",
				Context:    "secret_ref",
			})
		}
	}

	// Extend with CRD-based nodes and edges (KEDA, cert-manager, OPA, Cilium, Istio, Crossplane).
	// A CRD type the credential may not list joins the same forbidden skip set;
	// an uninstalled CRD is a silent skip, not degradation.
	crdNodes, crdEdges, crdSkips := r.refreshK8sCRDs(ctx, source, adapter)
	desiredNodes = append(desiredNodes, crdNodes...)
	desiredEdges = append(desiredEdges, crdEdges...)
	skips = append(skips, crdSkips...)

	existingNodes, existingEdges, err := LoadGraphStateForComponent(ctx, r.services.Graph, source.ID)
	if err != nil {
		return nil, err
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, desiredEdges)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return nil, err
	}

	r.logger.Info("k8s refresh completed", "component_id", source.ID, "nodes", len(desiredNodes), "edges", len(desiredEdges), "skipped", len(skips), "duration_ms", time.Since(start).Milliseconds())
	return skips, nil
}

func k8sNodeID(sourceID, kind, namespace, name string) string {
	kind = strings.ToLower(kind)
	if namespace == "" {
		return fmt.Sprintf("k8s/%s/%s/%s", sourceID, kind, name)
	}
	return fmt.Sprintf("k8s/%s/%s/%s/%s", sourceID, kind, namespace, name)
}

func buildK8sMetadata(obj *unstructured.Unstructured, nodeType, namespace string) map[string]any {
	metadata := map[string]any{}

	metadata["name"] = obj.GetName()
	if namespace != "" {
		metadata["namespace"] = namespace
	}

	if labels := obj.GetLabels(); len(labels) > 0 {
		metadata["labels"] = labels
	}

	if apiVersion := obj.GetAPIVersion(); apiVersion != "" {
		metadata["api_version"] = apiVersion
	}
	if kind := obj.GetKind(); kind != "" {
		metadata["kind"] = strings.ToLower(kind)
	}
	if uid := string(obj.GetUID()); uid != "" {
		metadata["uid"] = uid
	}
	if ts := obj.GetCreationTimestamp(); !ts.IsZero() {
		metadata["creation_timestamp"] = ts.Time.UTC().Format(time.RFC3339)
	}

	switch nodeType {
	case "deployment", "statefulset", "daemonset":
		if replicas, found, _ := unstructured.NestedInt64(obj.Object, "spec", "replicas"); found {
			metadata["replicas"] = replicas
		}
		if selector, found, _ := unstructured.NestedStringMap(obj.Object, "spec", "selector", "matchLabels"); found {
			metadata["selector"] = selector
		}
	case "service":
		if svcType, found, _ := unstructured.NestedString(obj.Object, "spec", "type"); found {
			metadata["type"] = svcType
		}
		if selector, found, _ := unstructured.NestedStringMap(obj.Object, "spec", "selector"); found {
			metadata["selector"] = selector
		}
		if ports, found, _ := unstructured.NestedSlice(obj.Object, "spec", "ports"); found {
			metadata["ports"] = simplifyPorts(ports)
		}
	case "configmap", "secret":
		if data, found, _ := unstructured.NestedMap(obj.Object, "data"); found {
			metadata["data_keys"] = mapKeys(data)
		}
	case "node":
		if labels := obj.GetLabels(); len(labels) > 0 {
			metadata["labels"] = labels
		}
		if taints, found, _ := unstructured.NestedSlice(obj.Object, "spec", "taints"); found {
			metadata["taints"] = extractTaintKeys(taints)
		}
		if capacity, found, _ := unstructured.NestedMap(obj.Object, "status", "capacity"); found {
			metadata["capacity"] = capacitySummary(capacity)
		}
		if addresses, found, _ := unstructured.NestedSlice(obj.Object, "status", "addresses"); found {
			for _, addr := range addresses {
				addrMap, ok := addr.(map[string]any)
				if !ok {
					continue
				}
				addrType, _ := addrMap["type"].(string)
				addrValue, _ := addrMap["address"].(string)
				switch addrType {
				case "InternalIP":
					metadata["internal_ip"] = addrValue
				case "Hostname":
					metadata["hostname"] = addrValue
				}
			}
		}
	}

	return metadata
}

func mapKeys(input map[string]any) []string {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	return keys
}

func simplifyPorts(ports []any) []map[string]any {
	results := make([]map[string]any, 0, len(ports))
	for _, port := range ports {
		portMap, ok := port.(map[string]any)
		if !ok {
			continue
		}
		entry := map[string]any{}
		if name, ok := portMap["name"].(string); ok && name != "" {
			entry["name"] = name
		}
		if protocol, ok := portMap["protocol"].(string); ok && protocol != "" {
			entry["protocol"] = protocol
		}
		if portNumber, ok := portMap["port"]; ok {
			entry["port"] = portNumber
		}
		if targetPort, ok := portMap["targetPort"]; ok {
			entry["targetPort"] = targetPort
		}
		if len(entry) > 0 {
			results = append(results, entry)
		}
	}
	return results
}

func extractTaintKeys(taints []any) []string {
	keys := make([]string, 0, len(taints))
	for _, taint := range taints {
		item, ok := taint.(map[string]any)
		if !ok {
			continue
		}
		if key, ok := item["key"].(string); ok && key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

func capacitySummary(capacity map[string]any) map[string]any {
	summary := map[string]any{}
	if cpu, ok := capacity["cpu"]; ok {
		summary["cpu"] = cpu
	}
	if mem, ok := capacity["memory"]; ok {
		summary["memory"] = mem
	}
	if pods, ok := capacity["pods"]; ok {
		summary["pods"] = pods
	}
	return summary
}

func extractWorkloadInfo(sourceID, nodeType, namespace string, obj *unstructured.Unstructured) workloadInfo {
	info := workloadInfo{
		ID:         k8sNodeID(sourceID, nodeType, namespace, obj.GetName()),
		NodeType:   nodeType,
		Namespace:  namespace,
		PodLabels:  map[string]string{},
		ConfigMaps: map[string]struct{}{},
		Secrets:    map[string]struct{}{},
	}

	if labels, found, _ := unstructured.NestedStringMap(obj.Object, "spec", "template", "metadata", "labels"); found {
		info.PodLabels = labels
	}
	if selector, found, _ := unstructured.NestedStringMap(obj.Object, "spec", "selector", "matchLabels"); found {
		info.SelectorInfo = selector
	}

	volumes, _, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "volumes")
	for _, vol := range volumes {
		volMap, ok := vol.(map[string]any)
		if !ok {
			continue
		}
		if cfg, ok := volMap["configMap"].(map[string]any); ok {
			if name, ok := cfg["name"].(string); ok && name != "" {
				info.ConfigMaps[name] = struct{}{}
			}
		}
		if sec, ok := volMap["secret"].(map[string]any); ok {
			if name, ok := sec["secretName"].(string); ok && name != "" {
				info.Secrets[name] = struct{}{}
			}
		}
	}

	collectFromContainers := func(path ...string) {
		containers, _, _ := unstructured.NestedSlice(obj.Object, path...)
		for _, c := range containers {
			cMap, ok := c.(map[string]any)
			if !ok {
				continue
			}
			envFrom, ok := cMap["envFrom"].([]any)
			if ok {
				for _, envFromItem := range envFrom {
					item, ok := envFromItem.(map[string]any)
					if !ok {
						continue
					}
					if cfg, ok := item["configMapRef"].(map[string]any); ok {
						if name, ok := cfg["name"].(string); ok && name != "" {
							info.ConfigMaps[name] = struct{}{}
						}
					}
					if sec, ok := item["secretRef"].(map[string]any); ok {
						if name, ok := sec["name"].(string); ok && name != "" {
							info.Secrets[name] = struct{}{}
						}
					}
				}
			}

			env, ok := cMap["env"].([]any)
			if ok {
				for _, envItem := range env {
					item, ok := envItem.(map[string]any)
					if !ok {
						continue
					}
					valueFrom, ok := item["valueFrom"].(map[string]any)
					if !ok {
						continue
					}
					if cfg, ok := valueFrom["configMapKeyRef"].(map[string]any); ok {
						if name, ok := cfg["name"].(string); ok && name != "" {
							info.ConfigMaps[name] = struct{}{}
						}
					}
					if sec, ok := valueFrom["secretKeyRef"].(map[string]any); ok {
						if name, ok := sec["name"].(string); ok && name != "" {
							info.Secrets[name] = struct{}{}
						}
					}
				}
			}
		}
	}

	collectFromContainers("spec", "template", "spec", "containers")
	collectFromContainers("spec", "template", "spec", "initContainers")

	return info
}

func extractServiceInfo(sourceID, namespace string, obj *unstructured.Unstructured) serviceInfo {
	info := serviceInfo{
		ID:        k8sNodeID(sourceID, "service", namespace, obj.GetName()),
		Namespace: namespace,
		Selector:  map[string]string{},
	}

	if selector, found, _ := unstructured.NestedStringMap(obj.Object, "spec", "selector"); found {
		info.Selector = selector
	}
	return info
}

func selectorMatches(selector, labels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for key, value := range selector {
		if labels[key] != value {
			return false
		}
	}
	return true
}
