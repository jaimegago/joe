package coreagent

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	envoypadapter "github.com/jaimegago/joe/internal/adapters/networking/envoy"
	nginxadapter "github.com/jaimegago/joe/internal/adapters/networking/nginx"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/store"
)

// ---- fake adapters ----

type fakeNginxAdapter struct {
	ingresses []nginxadapter.Ingress
	err       error
}

func (f *fakeNginxAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (f *fakeNginxAdapter) Disconnect() error                                  { return nil }
func (f *fakeNginxAdapter) Status() adapters.Status                            { return adapters.Status{Connected: true} }
func (f *fakeNginxAdapter) ListIngresses(_ context.Context, _ string) ([]nginxadapter.Ingress, error) {
	return f.ingresses, f.err
}
func (f *fakeNginxAdapter) GetNginxStatus(_ context.Context) (*nginxadapter.NginxStatus, error) {
	return nil, f.err
}
func (f *fakeNginxAdapter) ListConfigMaps(_ context.Context, _ string) ([]nginxadapter.ConfigMapSummary, error) {
	return nil, f.err
}

type fakeEnvoyAdapter struct {
	clusters []envoypadapter.ClusterStatus
	err      error
}

func (f *fakeEnvoyAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (f *fakeEnvoyAdapter) Disconnect() error                                  { return nil }
func (f *fakeEnvoyAdapter) Status() adapters.Status                            { return adapters.Status{Connected: true} }
func (f *fakeEnvoyAdapter) Clusters(_ context.Context) ([]envoypadapter.ClusterStatus, error) {
	return f.clusters, f.err
}
func (f *fakeEnvoyAdapter) ConfigDump(_ context.Context, _ string) (map[string]any, error) {
	return nil, f.err
}
func (f *fakeEnvoyAdapter) Stats(_ context.Context, _ string) ([]envoypadapter.Stat, error) {
	return nil, f.err
}

// ---- helper ----

func setupNetworkingRefresher(t *testing.T) *Refresher {
	t.Helper()
	gs := setupGraphStore(t)
	return &Refresher{
		services: &core.Services{Graph: gs},
		logger:   slog.Default(),
	}
}

// ---- NGINX ----

func TestRefreshNginxComponent_NoIngresses(t *testing.T) {
	r := setupNetworkingRefresher(t)
	src := &store.Component{ID: "src-nginx-1", Type: store.ComponentTypeNginx, Name: "nginx-ingress"}

	if err := r.refreshNginxComponent(context.Background(), src, &fakeNginxAdapter{}); err != nil {
		t.Fatalf("refreshNginxComponent: %v", err)
	}

	nodes, _, err := LoadGraphStateForComponent(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Type != "nginx_component" {
		t.Errorf("want 1 nginx_source node, got %v", nodes)
	}
}

func TestRefreshNginxComponent_IngressesError(t *testing.T) {
	r := setupNetworkingRefresher(t)
	src := &store.Component{ID: "src-nginx-2", Type: store.ComponentTypeNginx, Name: "nginx"}
	adapter := &fakeNginxAdapter{err: errors.New("k8s api error")}

	// Should still succeed (skips edge discovery).
	if err := r.refreshNginxComponent(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshNginxComponent should not error on ListIngresses failure, got: %v", err)
	}
}

func TestRefreshNginxComponent_WithIngresses(t *testing.T) {
	r := setupNetworkingRefresher(t)
	src := &store.Component{ID: "src-nginx-3", Type: store.ComponentTypeNginx, Name: "nginx"}
	adapter := &fakeNginxAdapter{
		ingresses: []nginxadapter.Ingress{
			{
				Name:      "api-ingress",
				Namespace: "default",
				Class:     "nginx",
				Rules: []nginxadapter.IngressRule{
					{
						Host: "api.example.com",
						Paths: []nginxadapter.IngressPath{
							{Path: "/", PathType: "Prefix", ServiceName: "api-service", ServicePort: "80"},
						},
					},
				},
			},
		},
	}

	if err := r.refreshNginxComponent(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshNginxComponent: %v", err)
	}

	nodes, _, err := LoadGraphStateForComponent(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent: %v", err)
	}
	// 1 source node + 1 ingress node.
	if len(nodes) != 2 {
		t.Errorf("want 2 nodes, got %d", len(nodes))
	}
}

// ---- Envoy ----

func TestRefreshEnvoyComponent_NoClusters(t *testing.T) {
	r := setupNetworkingRefresher(t)
	src := &store.Component{ID: "src-envoy-1", Type: store.ComponentTypeEnvoy, Name: "envoy"}

	if err := r.refreshEnvoyComponent(context.Background(), src, &fakeEnvoyAdapter{}); err != nil {
		t.Fatalf("refreshEnvoyComponent: %v", err)
	}

	nodes, _, err := LoadGraphStateForComponent(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Type != "envoy_component" {
		t.Errorf("want 1 envoy_source node, got %v", nodes)
	}
}

func TestRefreshEnvoyComponent_ClustersError(t *testing.T) {
	r := setupNetworkingRefresher(t)
	src := &store.Component{ID: "src-envoy-2", Type: store.ComponentTypeEnvoy, Name: "envoy"}
	adapter := &fakeEnvoyAdapter{err: errors.New("admin api unreachable")}

	if err := r.refreshEnvoyComponent(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshEnvoyComponent should not error on Clusters failure, got: %v", err)
	}
}

func TestRefreshEnvoyComponent_WithClusters(t *testing.T) {
	r := setupNetworkingRefresher(t)
	src := &store.Component{ID: "src-envoy-3", Type: store.ComponentTypeEnvoy, Name: "envoy"}
	adapter := &fakeEnvoyAdapter{
		clusters: []envoypadapter.ClusterStatus{
			{Name: "payment_80"},
			{Name: "outbound|80||api.default.svc.cluster.local"},
		},
	}

	if err := r.refreshEnvoyComponent(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshEnvoyComponent: %v", err)
	}
	// Just verify no error — edge creation is best-effort.
}

// ---- extractServiceNameFromCluster ----

func TestExtractServiceNameFromCluster(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"outbound|80||payment.default.svc.cluster.local", "payment"},
		{"inbound|8080||api.prod.svc.cluster.local", "api"},
		{"payment_80", "payment"},
		{"payment", "payment"},
		{"outbound|80||PassthroughCluster", ""},
		{"BlackHoleCluster", "BlackHoleCluster"},
		{"", ""},
	}

	for _, tt := range tests {
		got := extractServiceNameFromCluster(tt.input)
		if got != tt.want {
			t.Errorf("extractServiceNameFromCluster(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ---- isPortOrProto ----

func TestIsPortOrProto(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"80", true},
		{"443", true},
		{"8080", true},
		{"http", true},
		{"https", true},
		{"grpc", true},
		{"tcp", true},
		{"udp", true},
		{"payment", false},
		{"", false},
	}

	for _, tt := range tests {
		got := isPortOrProto(tt.input)
		if got != tt.want {
			t.Errorf("isPortOrProto(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// ---- networkingNodeID ----

func TestNetworkingNodeID(t *testing.T) {
	id := networkingNodeID("src1", "nginx-ingress")
	want := "networking/nginx-ingress/src1"
	if id != want {
		t.Errorf("networkingNodeID = %q, want %q", id, want)
	}
}

// ---- refreshComponent switch cases ----

func TestRefreshComponent_NginxType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-nginx", &fakeNginxAdapter{})

	r := withPermitAllAccessor(&Refresher{services: svc, logger: slog.Default()})
	src := &store.Component{ID: "src-nginx", Type: store.ComponentTypeNginx, Name: "nginx"}
	if err := r.refreshComponent(context.Background(), src); err != nil {
		t.Fatalf("refreshComponent(nginx) error: %v", err)
	}
}

func TestRefreshComponent_EnvoyType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-envoy", &fakeEnvoyAdapter{})

	r := withPermitAllAccessor(&Refresher{services: svc, logger: slog.Default()})
	src := &store.Component{ID: "src-envoy", Type: store.ComponentTypeEnvoy, Name: "envoy"}
	if err := r.refreshComponent(context.Background(), src); err != nil {
		t.Fatalf("refreshComponent(envoy) error: %v", err)
	}
}

func TestRefreshComponent_NginxWrongType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-nginx-bad", &fakeEnvoyAdapter{})

	r := withPermitAllAccessor(&Refresher{services: svc, logger: slog.Default()})
	src := &store.Component{ID: "src-nginx-bad", Type: store.ComponentTypeNginx, Name: "nginx"}
	if err := r.refreshComponent(context.Background(), src); err == nil {
		t.Error("expected error for wrong adapter type, got nil")
	}
}
