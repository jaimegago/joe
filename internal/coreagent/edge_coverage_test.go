package coreagent

// Targeted tests for edge-case branches that are still uncovered.

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ============================================================
// buildDDMetricsInEdges / buildDDLogsInEdges — with matching nodes
// ============================================================

func setupDDRefresher(t *testing.T) *Refresher {
	t.Helper()
	gs := setupGraphStore(t)
	return &Refresher{
		services: &core.Services{Graph: gs},
		logger:   slog.Default(),
	}
}

func TestBuildDDMetricsInEdges_WithMatchingService(t *testing.T) {
	r := setupDDRefresher(t)
	ctx := context.Background()
	src := &store.Component{ID: "src-dd-1", Type: store.ComponentTypeDatadog, Name: "datadog"}

	_ = r.services.Graph.AddNode(ctx, graph.Node{
		ID:          "svc/default/payment",
		Type:        "service",
		ComponentID: "src-k8s",
		Metadata:    map[string]any{"name": "payment"},
	})

	edges, err := r.buildDDMetricsInEdges(ctx, src, "dd-node-id", []string{"payment", "api"}, time.Now())
	if err != nil {
		t.Fatalf("buildDDMetricsInEdges error: %v", err)
	}
	if len(edges) == 0 {
		t.Error("want at least 1 metrics_in edge for matching service")
	}
	if edges[0].Relation != graph.RelationMetricsIn {
		t.Errorf("edge relation = %v, want RelationMetricsIn", edges[0].Relation)
	}
}

func TestBuildDDMetricsInEdges_SkipsNonServiceNodes(t *testing.T) {
	r := setupDDRefresher(t)
	ctx := context.Background()
	src := &store.Component{ID: "src-dd-2", Type: store.ComponentTypeDatadog, Name: "datadog"}

	_ = r.services.Graph.AddNode(ctx, graph.Node{
		ID:          "k8snode/payment-node",
		Type:        "k8s_node",
		ComponentID: "src-k8s",
		Metadata:    map[string]any{"name": "payment-node"},
	})

	edges, err := r.buildDDMetricsInEdges(ctx, src, "dd-node", []string{"payment-node"}, time.Now())
	if err != nil {
		t.Fatalf("buildDDMetricsInEdges error: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("want 0 edges for non-service node, got %d", len(edges))
	}
}

func TestBuildDDLogsInEdges_WithMatchingDeployment(t *testing.T) {
	r := setupDDRefresher(t)
	ctx := context.Background()
	src := &store.Component{ID: "src-dd-3", Type: store.ComponentTypeDatadog, Name: "datadog"}

	_ = r.services.Graph.AddNode(ctx, graph.Node{
		ID:          "deploy/default/api-gateway",
		Type:        "deployment",
		ComponentID: "src-k8s",
		Metadata:    map[string]any{"name": "api-gateway"},
	})

	edges, err := r.buildDDLogsInEdges(ctx, src, "dd-node-id", []string{"api-gateway"}, time.Now())
	if err != nil {
		t.Fatalf("buildDDLogsInEdges error: %v", err)
	}
	if len(edges) == 0 {
		t.Error("want at least 1 logs_in edge for matching deployment")
	}
	if edges[0].Relation != graph.RelationLogsIn {
		t.Errorf("edge relation = %v, want RelationLogsIn", edges[0].Relation)
	}
}

func TestBuildDDLogsInEdges_NoMatch(t *testing.T) {
	r := setupDDRefresher(t)
	ctx := context.Background()
	src := &store.Component{ID: "src-dd-4", Type: store.ComponentTypeDatadog, Name: "datadog"}

	edges, err := r.buildDDLogsInEdges(ctx, src, "dd-node", []string{"nonexistent"}, time.Now())
	if err != nil {
		t.Fatalf("buildDDLogsInEdges error: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("want 0 edges when no matching nodes, got %d", len(edges))
	}
}

// ============================================================
// buildK8sMetadata: exercise each node type branch
// ============================================================

func TestBuildK8sMetadata_Deployment(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": map[string]any{
				"replicas": int64(3),
				"selector": map[string]any{
					"matchLabels": map[string]any{"app": "payment"},
				},
			},
		},
	}
	obj.SetName("payment-deploy")
	obj.SetLabels(map[string]string{"app": "payment"})
	obj.SetAPIVersion("apps/v1")
	obj.SetKind("Deployment")

	meta := buildK8sMetadata(obj, "deployment", "default")
	if meta["name"] != "payment-deploy" {
		t.Errorf("name = %v", meta["name"])
	}
	if meta["namespace"] != "default" {
		t.Errorf("namespace = %v", meta["namespace"])
	}
	if meta["replicas"] != int64(3) {
		t.Errorf("replicas = %v", meta["replicas"])
	}
}

func TestBuildK8sMetadata_Service(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": map[string]any{
				"type": "ClusterIP",
				"selector": map[string]any{
					"app": "api",
				},
				"ports": []any{
					map[string]any{"name": "http", "port": int64(80), "protocol": "TCP"},
				},
			},
		},
	}
	obj.SetName("api-svc")

	meta := buildK8sMetadata(obj, "service", "default")
	if meta["type"] != "ClusterIP" {
		t.Errorf("service type = %v", meta["type"])
	}
	if meta["ports"] == nil {
		t.Error("ports should not be nil")
	}
}

func TestBuildK8sMetadata_ConfigMap(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"data": map[string]any{
				"config.yaml": "key: value",
				"other.yaml":  "other: val",
			},
		},
	}
	obj.SetName("app-config")

	meta := buildK8sMetadata(obj, "configmap", "")
	// namespace empty — should not be set.
	if _, ok := meta["namespace"]; ok {
		t.Error("namespace should not be set when empty")
	}
	if meta["data_keys"] == nil {
		t.Error("data_keys should not be nil for configmap")
	}
}

func TestBuildK8sMetadata_Node(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": map[string]any{
				"taints": []any{
					map[string]any{"key": "node-role.kubernetes.io/master", "effect": "NoSchedule"},
				},
			},
			"status": map[string]any{
				"capacity": map[string]any{"cpu": "4", "memory": "16Gi"},
				"addresses": []any{
					map[string]any{"type": "InternalIP", "address": "10.0.0.1"},
					map[string]any{"type": "Hostname", "address": "node-1"},
				},
			},
		},
	}
	obj.SetName("node-1")
	obj.SetLabels(map[string]string{"kubernetes.io/hostname": "node-1"})

	meta := buildK8sMetadata(obj, "node", "")
	if meta["internal_ip"] != "10.0.0.1" {
		t.Errorf("internal_ip = %v", meta["internal_ip"])
	}
	if meta["hostname"] != "node-1" {
		t.Errorf("hostname = %v", meta["hostname"])
	}
	if meta["taints"] == nil {
		t.Error("taints should not be nil")
	}
	if meta["capacity"] == nil {
		t.Error("capacity should not be nil")
	}
}

func TestBuildK8sMetadata_UnknownType(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.SetName("some-resource")
	meta := buildK8sMetadata(obj, "unknown_type", "default")
	if meta["name"] != "some-resource" {
		t.Errorf("name = %v", meta["name"])
	}
}

// ============================================================
// extractWorkloadInfo — verify struct fields via sourceID/nodeType
// ============================================================

func TestExtractWorkloadInfo_StatefulSet(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": map[string]any{
				"replicas":    int64(2),
				"serviceName": "payment-sts",
				"selector": map[string]any{
					"matchLabels": map[string]any{"app": "payment"},
				},
				"template": map[string]any{
					"spec": map[string]any{
						"containers": []any{
							map[string]any{"name": "payment", "image": "payment:v1"},
						},
					},
				},
			},
		},
	}
	obj.SetName("payment-sts")
	obj.SetNamespace("default")
	obj.SetKind("StatefulSet")

	info := extractWorkloadInfo("src-k8s", "statefulset", "default", obj)
	if info.ID == "" {
		t.Error("ID should not be empty")
	}
	if info.NodeType != "statefulset" {
		t.Errorf("NodeType = %q, want statefulset", info.NodeType)
	}
}

func TestExtractWorkloadInfo_DaemonSet(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": map[string]any{
				"selector": map[string]any{
					"matchLabels": map[string]any{"app": "fluentd"},
				},
				"template": map[string]any{
					"spec": map[string]any{
						"containers": []any{
							map[string]any{"name": "fluentd", "image": "fluentd:v1"},
						},
					},
				},
			},
		},
	}
	obj.SetName("fluentd-ds")
	obj.SetNamespace("kube-system")

	info := extractWorkloadInfo("src-k8s", "daemonset", "kube-system", obj)
	if info.NodeType != "daemonset" {
		t.Errorf("NodeType = %q, want daemonset", info.NodeType)
	}
}

// ============================================================
// buildProxiesEdges: with matching deployment node
// ============================================================

func TestBuildProxiesEdges_WithMatchingDeployment(t *testing.T) {
	r := setupNetworkingRefresher(t)
	ctx := context.Background()
	src := &store.Component{ID: "src-envoy-deploy", Type: store.ComponentTypeEnvoy, Name: "envoy"}

	_ = r.services.Graph.AddNode(ctx, graph.Node{
		ID:          "deploy/default/orders",
		Type:        "deployment",
		ComponentID: "src-k8s",
		Metadata:    map[string]any{"name": "orders"},
	})

	now := time.Now()
	edges := r.buildProxiesEdges(ctx, src, "envoy-node", "orders", "orders_8080", now)
	if len(edges) == 0 {
		t.Error("want at least 1 proxies edge for matching deployment")
	}
}

// ============================================================
// buildQueuesInEdges: duplicate topic name deduplication
// ============================================================

func TestBuildQueuesInEdges_Deduplication(t *testing.T) {
	r := setupDatastoreRefresher(t)
	ctx := context.Background()
	src := &store.Component{ID: "src-kafka-dedup", Type: store.ComponentTypeKafka, Name: "kafka"}

	_ = r.services.Graph.AddNode(ctx, graph.Node{
		ID:          "svc/default/orders",
		Type:        "service",
		ComponentID: "src-k8s",
		Metadata:    map[string]any{"name": "orders"},
	})

	// "orders" appears 3 times — deduplication should produce only 1 edge.
	edges := r.buildQueuesInEdges(ctx, src, "kafka-node", []string{"orders", "orders", "orders"}, time.Now())
	ordersEdges := 0
	for _, e := range edges {
		if e.Relation == graph.RelationQueuesIn {
			ordersEdges++
		}
	}
	if ordersEdges != 1 {
		t.Errorf("want exactly 1 deduped queues_in edge, got %d", ordersEdges)
	}
}

func TestBuildQueuesInEdges_SkipsNonServiceNodes(t *testing.T) {
	r := setupDatastoreRefresher(t)
	ctx := context.Background()
	src := &store.Component{ID: "src-kafka-skip", Type: store.ComponentTypeKafka, Name: "kafka"}

	_ = r.services.Graph.AddNode(ctx, graph.Node{
		ID:          "k8snode/orders-node",
		Type:        "k8s_node",
		ComponentID: "src-k8s",
		Metadata:    map[string]any{"name": "orders-node"},
	})

	edges := r.buildQueuesInEdges(ctx, src, "kafka-node", []string{"orders-node"}, time.Now())
	if len(edges) != 0 {
		t.Errorf("want 0 edges for non-service node, got %d", len(edges))
	}
}

// ============================================================
// applyRegistryDelta: success path
// ============================================================

func TestApplyRegistryDelta_Success(t *testing.T) {
	r := setupRegistryRefresher(t)
	ctx := context.Background()
	src := &store.Component{ID: "src-reg-delta", Type: store.ComponentTypeOCIRegistry, Name: "reg"}

	desiredNodes := []graph.Node{
		{
			ID:          registryNodeID(src.ID, src.Type),
			Type:        "artifact_registry",
			ComponentID: src.ID,
			Metadata:    registryMetadata(src),
			LastSeen:    time.Now(),
		},
	}

	if err := r.applyRegistryDelta(ctx, src, desiredNodes, nil, "oci"); err != nil {
		t.Fatalf("applyRegistryDelta error: %v", err)
	}
}

// ============================================================
// refreshComponent: wrong-type branches not covered elsewhere
// ============================================================

func TestRefreshComponent_PostgresWrongType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-pg-bad", &fakeSplunkAdapter{})

	r := withPermitAllAccessor(&Refresher{services: svc, logger: slog.Default()})
	src := &store.Component{ID: "src-pg-bad", Type: store.ComponentTypePostgreSQL, Name: "pg"}
	if err := r.refreshComponent(context.Background(), src); err == nil {
		t.Error("expected error for wrong adapter type, got nil")
	}
}

func TestRefreshComponent_HelmWrongType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-helm-bad2", &fakeSplunkAdapter{})

	r := withPermitAllAccessor(&Refresher{services: svc, logger: slog.Default()})
	src := &store.Component{ID: "src-helm-bad2", Type: store.ComponentTypeHelm, Name: "helm"}
	if err := r.refreshComponent(context.Background(), src); err == nil {
		t.Error("expected error for wrong adapter type, got nil")
	}
}

func TestRefreshComponent_TerraformWrongType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-tf-bad2", &fakeSplunkAdapter{})

	r := withPermitAllAccessor(&Refresher{services: svc, logger: slog.Default()})
	src := &store.Component{ID: "src-tf-bad2", Type: store.ComponentTypeTerraform, Name: "tf"}
	if err := r.refreshComponent(context.Background(), src); err == nil {
		t.Error("expected error for wrong adapter type, got nil")
	}
}

// ============================================================
// simplifyPorts: various port entries
// ============================================================

func TestSimplifyPorts_WithValidPort(t *testing.T) {
	ports := []any{
		map[string]any{
			"name":       "http",
			"port":       int64(80),
			"protocol":   "TCP",
			"targetPort": int64(8080),
		},
		map[string]any{
			"name":     "https",
			"port":     int64(443),
			"protocol": "TCP",
		},
		"invalid-port-entry", // should be skipped
	}

	result := simplifyPorts(ports)
	if len(result) != 2 {
		t.Errorf("want 2 simplified ports, got %d", len(result))
	}
}

// ============================================================
// extractTaintKeys: extract taint key names
// ============================================================

func TestExtractTaintKeys_WithTaints(t *testing.T) {
	taints := []any{
		map[string]any{"key": "node-role.kubernetes.io/master", "effect": "NoSchedule"},
		map[string]any{"key": "node.kubernetes.io/not-ready", "effect": "NoExecute"},
		"invalid-taint", // should be skipped
	}

	keys := extractTaintKeys(taints)
	if len(keys) != 2 {
		t.Errorf("want 2 taint keys, got %d: %v", len(keys), keys)
	}
}

func TestExtractTaintKeys_Empty(t *testing.T) {
	keys := extractTaintKeys(nil)
	if len(keys) != 0 {
		t.Errorf("want 0 taint keys for nil input, got %d", len(keys))
	}
}
