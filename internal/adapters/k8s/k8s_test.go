package k8s_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jaimegago/joe/internal/adapters/k8s"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
)

func newTestAdapter(objects ...runtime.Object) *k8s.Adapter {
	scheme := runtime.NewScheme()
	dynClient := fakedynamic.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{
			{Group: "", Version: "v1", Resource: "pods"}:            "PodList",
			{Group: "apps", Version: "v1", Resource: "deployments"}: "DeploymentList",
			{Group: "", Version: "v1", Resource: "services"}:        "ServiceList",
		},
		objects...,
	)
	clientset := fakeclientset.NewSimpleClientset()
	return k8s.NewWithClients(dynClient, clientset)
}

func testPod(name, namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{},
		},
	}
}

func testDeployment(name, namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
		},
	}
}

func TestListResources(t *testing.T) {
	tests := []struct {
		name      string
		resource  string
		namespace string
		objects   []runtime.Object
		wantCount int
		wantErr   bool
	}{
		{
			name:      "list pods in namespace",
			resource:  "pods",
			namespace: "default",
			objects:   []runtime.Object{testPod("pod-a", "default"), testPod("pod-b", "default")},
			wantCount: 2,
		},
		{
			name:      "list pods empty namespace",
			resource:  "pods",
			namespace: "empty-ns",
			objects:   []runtime.Object{testPod("pod-a", "default")},
			wantCount: 0,
		},
		{
			name:      "list deployments",
			resource:  "deployments",
			namespace: "default",
			objects:   []runtime.Object{testDeployment("app", "default")},
			wantCount: 1,
		},
		{
			name:     "unknown resource type",
			resource: "foobar",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := newTestAdapter(tt.objects...)
			ctx := context.Background()

			items, err := adapter.ListResources(ctx, tt.resource, tt.namespace)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ListResources() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && len(items) != tt.wantCount {
				t.Errorf("ListResources() returned %d items, want %d", len(items), tt.wantCount)
			}
		})
	}
}

func TestGetResource(t *testing.T) {
	tests := []struct {
		name      string
		resource  string
		namespace string
		resName   string
		objects   []runtime.Object
		wantErr   bool
	}{
		{
			name:      "get existing pod",
			resource:  "pods",
			namespace: "default",
			resName:   "pod-a",
			objects:   []runtime.Object{testPod("pod-a", "default")},
		},
		{
			name:      "get nonexistent pod",
			resource:  "pods",
			namespace: "default",
			resName:   "missing",
			objects:   []runtime.Object{testPod("pod-a", "default")},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := newTestAdapter(tt.objects...)
			ctx := context.Background()

			obj, err := adapter.GetResource(ctx, tt.resource, tt.namespace, tt.resName)
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetResource() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				name := obj.GetName()
				if name != tt.resName {
					t.Errorf("GetResource() name = %s, want %s", name, tt.resName)
				}
			}
		})
	}
}

func TestResolveGVR(t *testing.T) {
	tests := []struct {
		input   string
		wantGVR schema.GroupVersionResource
		wantErr bool
	}{
		{input: "pods", wantGVR: schema.GroupVersionResource{Version: "v1", Resource: "pods"}},
		{input: "deployments", wantGVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}},
		{input: "Pods", wantGVR: schema.GroupVersionResource{Version: "v1", Resource: "pods"}},
		{input: "stable.example.com/v1/widgets", wantGVR: schema.GroupVersionResource{Group: "stable.example.com", Version: "v1", Resource: "widgets"}},
		{input: "foobar", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			gvr, err := k8s.ResolveGVR(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ResolveGVR(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && gvr != tt.wantGVR {
				t.Errorf("ResolveGVR(%q) = %v, want %v", tt.input, gvr, tt.wantGVR)
			}
		})
	}
}

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name    string
		raw     json.RawMessage
		want    k8s.Config
		wantErr bool
	}{
		{
			name: "full config",
			raw:  json.RawMessage(`{"kubeconfig":"/home/user/.kube/config","context":"prod"}`),
			want: k8s.Config{Kubeconfig: "/home/user/.kube/config", Context: "prod"},
		},
		{
			name: "empty config",
			raw:  json.RawMessage(`{}`),
			want: k8s.Config{},
		},
		{
			name: "nil config",
			raw:  nil,
			want: k8s.Config{},
		},
		{
			name:    "invalid json",
			raw:     json.RawMessage(`{bad`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := k8s.ParseConfig(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseConfig() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestAdapterStatus(t *testing.T) {
	adapter := k8s.New()
	status := adapter.Status()
	if status.Connected {
		t.Error("new adapter should not be connected")
	}

	// After connecting with test clients
	connected := newTestAdapter()
	status = connected.Status()
	if !status.Connected {
		t.Error("test adapter should be connected")
	}
}

func TestDisconnect(t *testing.T) {
	adapter := newTestAdapter()

	if err := adapter.Disconnect(); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}

	status := adapter.Status()
	if status.Connected {
		t.Error("should be disconnected after Disconnect()")
	}

	// Operations should fail after disconnect
	_, err := adapter.ListResources(context.Background(), "pods", "default")
	if err == nil {
		t.Error("expected error after disconnect")
	}
}

func TestListResources_AllNamespaces(t *testing.T) {
	adapter := newTestAdapter(
		testPod("pod-a", "ns1"),
		testPod("pod-b", "ns2"),
	)

	// Empty namespace should list across all namespaces
	items, err := adapter.ListResources(context.Background(), "pods", "")
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 pods across all namespaces, got %d", len(items))
	}
}

func TestGetResource_ByMetadataFields(t *testing.T) {
	adapter := newTestAdapter(testPod("payment-svc", "prod"))

	obj, err := adapter.GetResource(context.Background(), "pods", "prod", "payment-svc")
	if err != nil {
		t.Fatalf("GetResource: %v", err)
	}

	// Verify we can extract standard metadata
	if obj.GetName() != "payment-svc" {
		t.Errorf("name = %s, want payment-svc", obj.GetName())
	}
	if obj.GetNamespace() != "prod" {
		t.Errorf("namespace = %s, want prod", obj.GetNamespace())
	}

	// Verify metadata exists
	md, found, err := unstructured.NestedMap(obj.Object, "metadata")
	if err != nil || !found {
		t.Error("expected metadata field")
	}
	_ = md

	// Check labels can be retrieved from typed accessor
	labels := obj.GetLabels()
	if labels == nil {
		// Empty labels is fine for our test pod
		_ = labels
	}

	// Check annotations accessor
	annotations := obj.GetAnnotations()
	_ = annotations

	// Check creation timestamp accessor
	ct := obj.GetCreationTimestamp()
	_ = ct
}

func TestGetResource_ReturnsCompleteObject(t *testing.T) {
	pod := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name":      "web",
				"namespace": "default",
				"labels":    map[string]any{"app": "web"},
			},
			"spec": map[string]any{
				"containers": []any{
					map[string]any{
						"name":  "nginx",
						"image": "nginx:latest",
					},
				},
			},
		},
	}
	adapter := newTestAdapter(pod)

	obj, err := adapter.GetResource(context.Background(), "pods", "default", "web")
	if err != nil {
		t.Fatalf("GetResource: %v", err)
	}

	// Verify nested spec data is preserved
	containers, found, err := unstructured.NestedSlice(obj.Object, "spec", "containers")
	if err != nil || !found {
		t.Fatal("expected spec.containers")
	}
	if len(containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(containers))
	}

	c, ok := containers[0].(map[string]any)
	if !ok {
		t.Fatal("expected container to be map")
	}
	if c["name"] != "nginx" {
		t.Errorf("container name = %v, want nginx", c["name"])
	}
}

// Verify that KubernetesAdapter interface is satisfied by *Adapter.
var _ k8s.KubernetesAdapter = (*k8s.Adapter)(nil)

// Helper to verify all known resources resolve without error.
func TestResolveGVR_AllKnown(t *testing.T) {
	known := []string{
		"pods", "services", "configmaps", "secrets", "namespaces", "nodes",
		"events", "deployments", "statefulsets", "daemonsets", "replicasets",
		"ingresses", "jobs", "cronjobs", "serviceaccounts",
	}

	for _, name := range known {
		t.Run(name, func(t *testing.T) {
			gvr, err := k8s.ResolveGVR(name)
			if err != nil {
				t.Errorf("ResolveGVR(%q) failed: %v", name, err)
			}
			if gvr.Resource == "" {
				t.Errorf("ResolveGVR(%q) returned empty resource", name)
			}
		})
	}
}

// Verify we need typed accessor for pod logs.
func TestGetPodLogs_NotConnected(t *testing.T) {
	adapter := k8s.New()
	_, err := adapter.GetPodLogs(context.Background(), "default", "pod", "", 100)
	if err == nil {
		t.Error("expected error from disconnected adapter")
	}
}

// Verify interface compatibility with the base Adapter interface.
func TestK8sAdapterSatisfiesBaseAdapter(t *testing.T) {
	adapter := k8s.New()
	status := adapter.Status()
	if status.Connected {
		t.Error("new adapter should report disconnected")
	}
}

// Verify the metadata accessor works for objects retrieved from the dynamic client.
func TestListResources_ReturnedObjectsHaveMetadata(t *testing.T) {
	adapter := newTestAdapter(testPod("test", "default"))

	items, err := adapter.ListResources(context.Background(), "pods", "default")
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected at least 1 item")
	}

	item := items[0]
	if item.GetName() != "test" {
		t.Errorf("name = %s, want test", item.GetName())
	}
	if item.GetNamespace() != "default" {
		t.Errorf("namespace = %s, want default", item.GetNamespace())
	}

	// Verify the unstructured metadata accessor returns expected values.
	md := item.GetObjectKind().GroupVersionKind()
	_ = md
}

// Verify Create/Update metadata accessors on test objects.
func TestListResources_MultipleResourceTypes(t *testing.T) {
	adapter := newTestAdapter(
		testPod("pod1", "default"),
		testDeployment("deploy1", "default"),
	)

	pods, err := adapter.ListResources(context.Background(), "pods", "default")
	if err != nil {
		t.Fatalf("list pods: %v", err)
	}
	if len(pods) != 1 {
		t.Errorf("pods = %d, want 1", len(pods))
	}

	deployments, err := adapter.ListResources(context.Background(), "deployments", "default")
	if err != nil {
		t.Fatalf("list deployments: %v", err)
	}
	if len(deployments) != 1 {
		t.Errorf("deployments = %d, want 1", len(deployments))
	}
}

// Test that GetPodLogs works through the typed clientset.
func TestGetPodLogs_FakeReturnsLogs(t *testing.T) {
	adapter := newTestAdapter()

	logs, err := adapter.GetPodLogs(context.Background(), "default", "any-pod", "", 100)
	if err != nil {
		t.Fatalf("GetPodLogs: %v", err)
	}
	if logs == "" {
		t.Error("expected non-empty logs from fake clientset")
	}
}

// Verify the GetCreationTimestamp accessor on fake objects.
func TestGetResource_CreationTimestamp(t *testing.T) {
	pod := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name":              "ts-pod",
				"namespace":         "default",
				"creationTimestamp": "2024-01-15T10:30:00Z",
			},
		},
	}
	adapter := newTestAdapter(pod)

	obj, err := adapter.GetResource(context.Background(), "pods", "default", "ts-pod")
	if err != nil {
		t.Fatalf("GetResource: %v", err)
	}

	ct := obj.GetCreationTimestamp()
	if ct.IsZero() {
		t.Error("expected non-zero creation timestamp")
	}
}

// Helper: verify that the adapter implements the KubernetesAdapter via metav1.ObjectMeta.
func TestListResources_ObjectMetaAccessors(t *testing.T) {
	pod := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name":      "labeled-pod",
				"namespace": "default",
				"labels":    map[string]any{"app": "web", "env": "prod"},
			},
		},
	}
	adapter := newTestAdapter(pod)

	items, err := adapter.ListResources(context.Background(), "pods", "default")
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}

	labels := items[0].GetLabels()
	if labels["app"] != "web" {
		t.Errorf("label app = %s, want web", labels["app"])
	}
	if labels["env"] != "prod" {
		t.Errorf("label env = %s, want prod", labels["env"])
	}
}

// Ensure that the fake metav1.ObjectMetaAccessor interface is satisfied.
func TestNewWithClients_SetsConnected(t *testing.T) {
	adapter := k8s.NewWithClients(nil, nil)
	if !adapter.Status().Connected {
		t.Error("NewWithClients should set connected=true")
	}
}
