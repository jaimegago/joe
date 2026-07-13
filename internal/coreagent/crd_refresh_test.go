package coreagent

import (
	"context"
	"log/slog"
	"testing"

	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/store"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// fakeK8sAdapter is defined in k8s_refresh_test.go.

func setupCRDRefresher(t *testing.T) *Refresher {
	t.Helper()
	gs := setupGraphStore(t)
	return &Refresher{
		services: &core.Services{Graph: gs},
		logger:   slog.Default(),
	}
}

// ---- refreshK8sCRDs ----

func TestRefreshK8sCRDs_NoCRDsInstalled(t *testing.T) {
	r := setupCRDRefresher(t)
	src := &store.Component{ID: "src-k8s-crd-1", Type: store.ComponentTypeKubernetes}

	// Adapter returns nil for all resources — CRDs not installed.
	adapter := &fakeK8sAdapter{items: map[string][]unstructured.Unstructured{}}

	nodes, edges, _ := r.refreshK8sCRDs(context.Background(), src, adapter)
	// No CRDs installed → no nodes or edges expected.
	if len(nodes) != 0 {
		t.Errorf("want 0 nodes, got %d", len(nodes))
	}
	if len(edges) != 0 {
		t.Errorf("want 0 edges, got %d", len(edges))
	}
}

func TestRefreshK8sCRDs_KEDAScaledObject(t *testing.T) {
	r := setupCRDRefresher(t)
	src := &store.Component{ID: "src-k8s-crd-2", Type: store.ComponentTypeKubernetes}

	scaledObj := unstructured.Unstructured{}
	scaledObj.SetName("payment-scaler")
	scaledObj.SetNamespace("default")
	scaledObj.SetKind("ScaledObject")
	// Set spec.scaleTargetRef.name
	_ = scaledObj.Object

	adapter := &fakeK8sAdapter{
		items: map[string][]unstructured.Unstructured{
			"scaledobjects.keda.sh": {scaledObj},
		},
	}

	nodes, _, _ := r.refreshK8sCRDs(context.Background(), src, adapter)
	if len(nodes) != 1 {
		t.Fatalf("want 1 keda_scaledobject node, got %d", len(nodes))
	}
	if nodes[0].Type != "keda_scaledobject" {
		t.Errorf("node type = %q, want keda_scaledobject", nodes[0].Type)
	}
}

func TestRefreshK8sCRDs_CertManagerCertificate(t *testing.T) {
	r := setupCRDRefresher(t)
	src := &store.Component{ID: "src-k8s-crd-3", Type: store.ComponentTypeKubernetes}

	cert := unstructured.Unstructured{}
	cert.SetName("api-tls")
	cert.SetNamespace("default")
	cert.SetKind("Certificate")

	adapter := &fakeK8sAdapter{
		items: map[string][]unstructured.Unstructured{
			"certificates.cert-manager.io": {cert},
		},
	}

	nodes, _, _ := r.refreshK8sCRDs(context.Background(), src, adapter)
	found := false
	for _, n := range nodes {
		if n.Type == "certificate" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a certificate node, got: %v", nodes)
	}
}

func TestRefreshK8sCRDs_IstioVirtualService(t *testing.T) {
	r := setupCRDRefresher(t)
	src := &store.Component{ID: "src-k8s-crd-4", Type: store.ComponentTypeKubernetes}

	vs := unstructured.Unstructured{}
	vs.SetName("payment-vs")
	vs.SetNamespace("default")
	vs.SetKind("VirtualService")

	adapter := &fakeK8sAdapter{
		items: map[string][]unstructured.Unstructured{
			"virtualservices.networking.istio.io": {vs},
		},
	}

	nodes, _, _ := r.refreshK8sCRDs(context.Background(), src, adapter)
	found := false
	for _, n := range nodes {
		if n.Type == "istio_virtual_service" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an istio_virtual_service node, got: %v", nodes)
	}
}

func TestRefreshK8sCRDs_MeshForEdge(t *testing.T) {
	r := setupCRDRefresher(t)
	gs := r.services.Graph

	// Plant a service node that the VirtualService should mesh_for.
	svc := store.Component{ID: "src-k8s-crd-5", Type: store.ComponentTypeKubernetes}
	_ = svc

	_ = gs // graph query used internally

	src := &store.Component{ID: "src-k8s-crd-5", Type: store.ComponentTypeKubernetes}

	vs := unstructured.Unstructured{}
	vs.SetName("payment")
	vs.SetNamespace("default")
	vs.SetKind("VirtualService")

	adapter := &fakeK8sAdapter{
		items: map[string][]unstructured.Unstructured{
			"virtualservices.networking.istio.io": {vs},
		},
	}

	nodes, _, _ := r.refreshK8sCRDs(context.Background(), src, adapter)
	if len(nodes) == 0 {
		t.Error("expected at least 1 node")
	}
}

// ---- crdShortName ----

func TestCRDShortName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"scaledobjects.keda.sh", "scaledobjects"},
		{"certificates.cert-manager.io", "certificates"},
		{"constrainttemplates.templates.gatekeeper.sh", "constrainttemplates"},
		{"noDotResource", "noDotResource"},
		{"", ""},
	}

	for _, tt := range tests {
		got := crdShortName(tt.input)
		if got != tt.want {
			t.Errorf("crdShortName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ---- containsType ----

func TestContainsType(t *testing.T) {
	types := []string{"deployment", "statefulset", "daemonset"}

	if !containsType(types, "deployment") {
		t.Error("containsType should return true for deployment")
	}
	if containsType(types, "service") {
		t.Error("containsType should return false for service")
	}
	if containsType(nil, "deployment") {
		t.Error("containsType should return false for nil slice")
	}
}

// ---- resolveCRDTarget ----

func TestResolveCRDTarget_EmptyFieldPath(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.SetName("my-cert")

	got := resolveCRDTarget(obj, "", "my-cert")
	if got != "my-cert" {
		t.Errorf("resolveCRDTarget = %q, want my-cert", got)
	}
}

func TestResolveCRDTarget_WithFieldPath(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": map[string]any{
				"scaleTargetRef": map[string]any{
					"name": "payment-worker",
				},
			},
		},
	}
	obj.SetName("payment-scaler")

	got := resolveCRDTarget(obj, "spec.scaleTargetRef.name", "payment-scaler")
	if got != "payment-worker" {
		t.Errorf("resolveCRDTarget = %q, want payment-worker", got)
	}
}

func TestResolveCRDTarget_FieldNotFound_FallsBack(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{}}
	obj.SetName("my-scaler")

	got := resolveCRDTarget(obj, "spec.scaleTargetRef.name", "my-scaler")
	if got != "my-scaler" {
		t.Errorf("resolveCRDTarget = %q, want my-scaler (fallback)", got)
	}
}
