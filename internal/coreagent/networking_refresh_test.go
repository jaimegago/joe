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

func (f *fakeNginxAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (f *fakeNginxAdapter) Disconnect() error                               { return nil }
func (f *fakeNginxAdapter) Status() adapters.Status                         { return adapters.Status{Connected: true} }
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

func (f *fakeEnvoyAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (f *fakeEnvoyAdapter) Disconnect() error                               { return nil }
func (f *fakeEnvoyAdapter) Status() adapters.Status                         { return adapters.Status{Connected: true} }
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

func TestRefreshNginxSource_NoIngresses(t *testing.T) {
	r := setupNetworkingRefresher(t)
	src := &store.Source{ID: "src-nginx-1", Type: store.SourceTypeNginx, Name: "nginx-ingress"}

	if err := r.refreshNginxSource(context.Background(), src, &fakeNginxAdapter{}); err != nil {
		t.Fatalf("refreshNginxSource: %v", err)
	}

	nodes, _, err := LoadGraphStateForSource(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForSource: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Type != "nginx_source" {
		t.Errorf("want 1 nginx_source node, got %v", nodes)
	}
}

func TestRefreshNginxSource_IngressesError(t *testing.T) {
	r := setupNetworkingRefresher(t)
	src := &store.Source{ID: "src-nginx-2", Type: store.SourceTypeNginx, Name: "nginx"}
	adapter := &fakeNginxAdapter{err: errors.New("k8s api error")}

	// Should still succeed (skips edge discovery).
	if err := r.refreshNginxSource(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshNginxSource should not error on ListIngresses failure, got: %v", err)
	}
}

func TestRefreshNginxSource_WithIngresses(t *testing.T) {
	r := setupNetworkingRefresher(t)
	src := &store.Source{ID: "src-nginx-3", Type: store.SourceTypeNginx, Name: "nginx"}
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

	if err := r.refreshNginxSource(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshNginxSource: %v", err)
	}

	nodes, _, err := LoadGraphStateForSource(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForSource: %v", err)
	}
	// 1 source node + 1 ingress node.
	if len(nodes) != 2 {
		t.Errorf("want 2 nodes, got %d", len(nodes))
	}
}

// ---- Envoy ----

func TestRefreshEnvoySource_NoClusters(t *testing.T) {
	r := setupNetworkingRefresher(t)
	src := &store.Source{ID: "src-envoy-1", Type: store.SourceTypeEnvoy, Name: "envoy"}

	if err := r.refreshEnvoySource(context.Background(), src, &fakeEnvoyAdapter{}); err != nil {
		t.Fatalf("refreshEnvoySource: %v", err)
	}

	nodes, _, err := LoadGraphStateForSource(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForSource: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Type != "envoy_source" {
		t.Errorf("want 1 envoy_source node, got %v", nodes)
	}
}

func TestRefreshEnvoySource_ClustersError(t *testing.T) {
	r := setupNetworkingRefresher(t)
	src := &store.Source{ID: "src-envoy-2", Type: store.SourceTypeEnvoy, Name: "envoy"}
	adapter := &fakeEnvoyAdapter{err: errors.New("admin api unreachable")}

	if err := r.refreshEnvoySource(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshEnvoySource should not error on Clusters failure, got: %v", err)
	}
}

func TestRefreshEnvoySource_WithClusters(t *testing.T) {
	r := setupNetworkingRefresher(t)
	src := &store.Source{ID: "src-envoy-3", Type: store.SourceTypeEnvoy, Name: "envoy"}
	adapter := &fakeEnvoyAdapter{
		clusters: []envoypadapter.ClusterStatus{
			{Name: "payment_80"},
			{Name: "outbound|80||api.default.svc.cluster.local"},
		},
	}

	if err := r.refreshEnvoySource(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshEnvoySource: %v", err)
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

// ---- refreshSource switch cases ----

func TestRefreshSource_NginxType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-nginx", &fakeNginxAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	src := &store.Source{ID: "src-nginx", Type: store.SourceTypeNginx, Name: "nginx"}
	if err := r.refreshSource(context.Background(), src); err != nil {
		t.Fatalf("refreshSource(nginx) error: %v", err)
	}
}

func TestRefreshSource_EnvoyType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-envoy", &fakeEnvoyAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	src := &store.Source{ID: "src-envoy", Type: store.SourceTypeEnvoy, Name: "envoy"}
	if err := r.refreshSource(context.Background(), src); err != nil {
		t.Fatalf("refreshSource(envoy) error: %v", err)
	}
}

func TestRefreshSource_NginxWrongType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-nginx-bad", &fakeEnvoyAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	src := &store.Source{ID: "src-nginx-bad", Type: store.SourceTypeNginx, Name: "nginx"}
	if err := r.refreshSource(context.Background(), src); err == nil {
		t.Error("expected error for wrong adapter type, got nil")
	}
}
