package coreagent

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/adapters"
	datadogadapter "github.com/jaimegago/joe/internal/adapters/observability/datadog"
	jaegeradapter "github.com/jaimegago/joe/internal/adapters/observability/jaeger"
	lokiadapter "github.com/jaimegago/joe/internal/adapters/observability/loki"
	prometheusadapter "github.com/jaimegago/joe/internal/adapters/observability/prometheus"
	tempoadapter "github.com/jaimegago/joe/internal/adapters/observability/tempo"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
)

// fakePrometheusAdapter satisfies prometheusadapter.PrometheusAdapter.
type fakePrometheusAdapter struct {
	targets []prometheusadapter.Target
	result  *prometheusadapter.QueryResult
	err     error
}

func (f *fakePrometheusAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (f *fakePrometheusAdapter) Disconnect() error                                  { return nil }
func (f *fakePrometheusAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true}
}
func (f *fakePrometheusAdapter) Query(_ context.Context, _ string, _ time.Time) (*prometheusadapter.QueryResult, error) {
	return f.result, f.err
}
func (f *fakePrometheusAdapter) QueryRange(_ context.Context, _ string, _, _ time.Time, _ time.Duration) (*prometheusadapter.QueryResult, error) {
	return f.result, f.err
}
func (f *fakePrometheusAdapter) Targets(_ context.Context) ([]prometheusadapter.Target, error) {
	return f.targets, f.err
}

// fakeLokiAdapter satisfies lokiadapter.LokiAdapter.
type fakeLokiAdapter struct {
	result   *lokiadapter.QueryResult
	services []string
	err      error
}

func (f *fakeLokiAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (f *fakeLokiAdapter) Disconnect() error                                  { return nil }
func (f *fakeLokiAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true}
}
func (f *fakeLokiAdapter) Query(_ context.Context, _ string, _ int, _ time.Duration) (*lokiadapter.QueryResult, error) {
	return f.result, f.err
}
func (f *fakeLokiAdapter) QueryRange(_ context.Context, _ string, _, _ time.Time, _ int) (*lokiadapter.QueryResult, error) {
	return f.result, f.err
}
func (f *fakeLokiAdapter) ListServices(_ context.Context) ([]string, error) {
	return f.services, f.err
}

// fakeTempoAdapter satisfies tempoadapter.TempoAdapter.
type fakeTempoAdapter struct {
	searchResults []tempoadapter.TraceSearchResult
	trace         *tempoadapter.Trace
	services      []string
	err           error
}

func (f *fakeTempoAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (f *fakeTempoAdapter) Disconnect() error                                  { return nil }
func (f *fakeTempoAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true}
}
func (f *fakeTempoAdapter) Search(_ context.Context, _, _ string, _, _, _ int) ([]tempoadapter.TraceSearchResult, error) {
	return f.searchResults, f.err
}
func (f *fakeTempoAdapter) GetTrace(_ context.Context, _ string) (*tempoadapter.Trace, error) {
	return f.trace, f.err
}
func (f *fakeTempoAdapter) ListServices(_ context.Context) ([]string, error) {
	return f.services, f.err
}

// fakeJaegerAdapter satisfies jaegeradapter.JaegerAdapter.
type fakeJaegerAdapter struct {
	services []string
	traces   []jaegeradapter.TraceSearchResult
	trace    map[string]any
	err      error
}

func (f *fakeJaegerAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (f *fakeJaegerAdapter) Disconnect() error                                  { return nil }
func (f *fakeJaegerAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true}
}
func (f *fakeJaegerAdapter) ListServices(_ context.Context) ([]string, error) {
	return f.services, f.err
}
func (f *fakeJaegerAdapter) SearchTraces(_ context.Context, _, _ string, _ int) ([]jaegeradapter.TraceSearchResult, error) {
	return f.traces, f.err
}
func (f *fakeJaegerAdapter) GetTrace(_ context.Context, _ string) (map[string]any, error) {
	return f.trace, f.err
}

// fakeDatadogAdapter satisfies datadogadapter.DatadogAdapter.
type fakeDatadogAdapter struct {
	metricServices []string
	logServices    []string
	metricsResult  *datadogadapter.MetricsResult
	logsResult     *datadogadapter.LogsResult
	err            error
}

func (f *fakeDatadogAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (f *fakeDatadogAdapter) Disconnect() error                                  { return nil }
func (f *fakeDatadogAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true}
}
func (f *fakeDatadogAdapter) MetricsQuery(_ context.Context, _ string, _, _ int64) (*datadogadapter.MetricsResult, error) {
	return f.metricsResult, f.err
}
func (f *fakeDatadogAdapter) LogsSearch(_ context.Context, _ string, _, _ int64, _ int) (*datadogadapter.LogsResult, error) {
	return f.logsResult, f.err
}
func (f *fakeDatadogAdapter) ListActiveServices(_ context.Context) ([]string, error) {
	return f.metricServices, f.err
}
func (f *fakeDatadogAdapter) ListLogServices(_ context.Context) ([]string, error) {
	return f.logServices, f.err
}

// setupObsTestServices creates a full services stack for refreshComponent tests.
func setupObsTestServices(t *testing.T) (*core.Services, *adapters.Registry) {
	t.Helper()
	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	reg := adapters.NewRegistry()
	cfg := &config.Config{}
	svc := core.New(cfg, sqlStore, sqlStore.DB(), sqlStore.Driver(), reg, nil)
	return svc, reg
}

// ---- obsNodeID ----

func TestObsNodeID(t *testing.T) {
	id := obsNodeID("src1", "prometheus")
	want := "obs/prometheus/src1"
	if id != want {
		t.Errorf("obsNodeID = %q, want %q", id, want)
	}
}

// ---- refreshPrometheusComponent ----

func TestRefreshPrometheusComponent_NoTargets(t *testing.T) {
	graphStore := setupGraphStore(t)
	r := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Component{ID: "src-prom-1", Type: store.ComponentTypePrometheus, Name: "test-prom"}
	adapter := &fakePrometheusAdapter{}

	if err := r.refreshPrometheusComponent(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshPrometheusComponent error: %v", err)
	}

	nodes, _, err := LoadGraphStateForComponent(context.Background(), graphStore, source.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent error: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("len(nodes) = %d, want 1", len(nodes))
	}
	if nodes[0].Type != "prometheus_component" {
		t.Errorf("node type = %q, want prometheus_component", nodes[0].Type)
	}
}

func TestRefreshPrometheusComponent_TargetsError(t *testing.T) {
	graphStore := setupGraphStore(t)
	r := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Component{ID: "src-prom-2", Type: store.ComponentTypePrometheus, Name: "test-prom"}
	adapter := &fakePrometheusAdapter{err: errors.New("scrape error")}

	// Should still succeed (skips edge discovery).
	if err := r.refreshPrometheusComponent(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshPrometheusComponent should not error on Targets failure, got: %v", err)
	}
}

func TestRefreshPrometheusComponent_ActiveTargetWithJobLabel(t *testing.T) {
	graphStore := setupGraphStore(t)

	svcNode := graph.Node{ID: "svc/payment", Type: "service", ComponentID: "src-k8s"}
	if err := graphStore.AddNode(context.Background(), svcNode); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	r := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Component{ID: "src-prom-3", Type: store.ComponentTypePrometheus, Name: "test-prom"}
	adapter := &fakePrometheusAdapter{
		targets: []prometheusadapter.Target{
			{State: "active", Labels: map[string]string{"job": "payment"}},
		},
	}

	// Cross-source edges are applied; just verify no error.
	if err := r.refreshPrometheusComponent(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshPrometheusComponent error: %v", err)
	}
}

func TestRefreshPrometheusComponent_InactiveTarget(t *testing.T) {
	graphStore := setupGraphStore(t)
	r := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Component{ID: "src-prom-4", Type: store.ComponentTypePrometheus, Name: "test-prom"}
	adapter := &fakePrometheusAdapter{
		targets: []prometheusadapter.Target{
			{State: "dropped", Labels: map[string]string{"job": "payment"}}, // not "active"
		},
	}

	if err := r.refreshPrometheusComponent(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshPrometheusComponent error: %v", err)
	}
}

func TestRefreshPrometheusComponent_TargetNoJobLabel(t *testing.T) {
	graphStore := setupGraphStore(t)
	r := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Component{ID: "src-prom-5", Type: store.ComponentTypePrometheus, Name: "test-prom"}
	adapter := &fakePrometheusAdapter{
		targets: []prometheusadapter.Target{
			{State: "active", Labels: map[string]string{"instance": "localhost:9090"}}, // no "job"
		},
	}

	if err := r.refreshPrometheusComponent(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshPrometheusComponent error: %v", err)
	}
}

// ---- refreshLokiComponent ----

func TestRefreshLokiComponent(t *testing.T) {
	graphStore := setupGraphStore(t)
	r := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Component{ID: "src-loki-1", Type: store.ComponentTypeLoki, Name: "test-loki"}
	adapter := &fakeLokiAdapter{}

	if err := r.refreshLokiComponent(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshLokiComponent error: %v", err)
	}

	nodes, _, err := LoadGraphStateForComponent(context.Background(), graphStore, source.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent error: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("len(nodes) = %d, want 1", len(nodes))
	}
	if nodes[0].Type != "loki_component" {
		t.Errorf("node type = %q, want loki_component", nodes[0].Type)
	}
}

func TestRefreshLokiComponent_ListServicesError(t *testing.T) {
	graphStore := setupGraphStore(t)
	r := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Component{ID: "src-loki-2", Type: store.ComponentTypeLoki, Name: "test-loki"}
	adapter := &fakeLokiAdapter{err: errors.New("connection refused")}

	// Should still succeed (skips edge discovery).
	if err := r.refreshLokiComponent(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshLokiComponent should not error on ListServices failure, got: %v", err)
	}
}

func TestRefreshLokiComponent_WithMatchingService(t *testing.T) {
	graphStore := setupGraphStore(t)

	svcNode := graph.Node{ID: "svc/checkout", Type: "service", ComponentID: "src-k8s"}
	if err := graphStore.AddNode(context.Background(), svcNode); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	r := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Component{ID: "src-loki-3", Type: store.ComponentTypeLoki, Name: "test-loki"}
	adapter := &fakeLokiAdapter{services: []string{"checkout"}}

	if err := r.refreshLokiComponent(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshLokiComponent error: %v", err)
	}
}

// ---- refreshTempoComponent ----

func TestRefreshTempoComponent(t *testing.T) {
	graphStore := setupGraphStore(t)
	r := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Component{ID: "src-tempo-1", Type: store.ComponentTypeTempo, Name: "test-tempo"}
	adapter := &fakeTempoAdapter{}

	if err := r.refreshTempoComponent(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshTempoComponent error: %v", err)
	}

	nodes, _, err := LoadGraphStateForComponent(context.Background(), graphStore, source.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent error: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("len(nodes) = %d, want 1", len(nodes))
	}
	if nodes[0].Type != "tempo_component" {
		t.Errorf("node type = %q, want tempo_component", nodes[0].Type)
	}
}

func TestRefreshTempoComponent_ListServicesError(t *testing.T) {
	graphStore := setupGraphStore(t)
	r := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Component{ID: "src-tempo-2", Type: store.ComponentTypeTempo, Name: "test-tempo"}
	adapter := &fakeTempoAdapter{err: errors.New("connection refused")}

	// Should still succeed (skips edge discovery).
	if err := r.refreshTempoComponent(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshTempoComponent should not error on ListServices failure, got: %v", err)
	}
}

func TestRefreshTempoComponent_WithMatchingService(t *testing.T) {
	graphStore := setupGraphStore(t)

	svcNode := graph.Node{ID: "svc/shipping", Type: "deployment", ComponentID: "src-k8s"}
	if err := graphStore.AddNode(context.Background(), svcNode); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	r := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Component{ID: "src-tempo-3", Type: store.ComponentTypeTempo, Name: "test-tempo"}
	adapter := &fakeTempoAdapter{services: []string{"shipping"}}

	if err := r.refreshTempoComponent(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshTempoComponent error: %v", err)
	}
}

// ---- refreshJaegerComponent ----

func TestRefreshJaegerComponent_NoServices(t *testing.T) {
	graphStore := setupGraphStore(t)
	r := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Component{ID: "src-jaeger-1", Type: store.ComponentTypeJaeger, Name: "test-jaeger"}
	adapter := &fakeJaegerAdapter{}

	if err := r.refreshJaegerComponent(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshJaegerComponent error: %v", err)
	}

	nodes, _, err := LoadGraphStateForComponent(context.Background(), graphStore, source.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent error: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("len(nodes) = %d, want 1", len(nodes))
	}
	if nodes[0].Type != "jaeger_component" {
		t.Errorf("node type = %q, want jaeger_component", nodes[0].Type)
	}
}

func TestRefreshJaegerComponent_ListServicesError(t *testing.T) {
	graphStore := setupGraphStore(t)
	r := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Component{ID: "src-jaeger-2", Type: store.ComponentTypeJaeger, Name: "test-jaeger"}
	adapter := &fakeJaegerAdapter{err: errors.New("connection refused")}

	// Should still succeed (skips edge discovery).
	if err := r.refreshJaegerComponent(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshJaegerComponent should not error on ListServices failure, got: %v", err)
	}
}

func TestRefreshJaegerComponent_WithMatchingService(t *testing.T) {
	graphStore := setupGraphStore(t)

	svcNode := graph.Node{ID: "svc/frontend", Type: "deployment", ComponentID: "src-k8s"}
	if err := graphStore.AddNode(context.Background(), svcNode); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	r := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Component{ID: "src-jaeger-3", Type: store.ComponentTypeJaeger, Name: "test-jaeger"}
	adapter := &fakeJaegerAdapter{services: []string{"frontend"}}

	// Cross-source edges are applied; just verify no error.
	if err := r.refreshJaegerComponent(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshJaegerComponent error: %v", err)
	}
}

// ---- refreshDatadogComponent ----

func TestRefreshDatadogComponent_NoServices(t *testing.T) {
	graphStore := setupGraphStore(t)
	r := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Component{ID: "src-dd-1", Type: store.ComponentTypeDatadog, Name: "test-dd"}
	adapter := &fakeDatadogAdapter{}

	if err := r.refreshDatadogComponent(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshDatadogComponent error: %v", err)
	}

	nodes, _, err := LoadGraphStateForComponent(context.Background(), graphStore, source.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent error: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("len(nodes) = %d, want 1", len(nodes))
	}
	if nodes[0].Type != "datadog_component" {
		t.Errorf("node type = %q, want datadog_component", nodes[0].Type)
	}
}

func TestRefreshDatadogComponent_ServiceDiscoveryError(t *testing.T) {
	graphStore := setupGraphStore(t)
	r := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Component{ID: "src-dd-2", Type: store.ComponentTypeDatadog, Name: "test-dd"}
	adapter := &fakeDatadogAdapter{err: errors.New("api error")}

	// Should still succeed (skips edge discovery).
	if err := r.refreshDatadogComponent(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshDatadogComponent should not error on discovery failure, got: %v", err)
	}
}

func TestRefreshDatadogComponent_MetricsInEdge(t *testing.T) {
	graphStore := setupGraphStore(t)

	svcNode := graph.Node{ID: "svc/payment", Type: "service", ComponentID: "src-k8s"}
	if err := graphStore.AddNode(context.Background(), svcNode); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	r := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Component{ID: "src-dd-3", Type: store.ComponentTypeDatadog, Name: "test-dd"}
	adapter := &fakeDatadogAdapter{metricServices: []string{"payment"}}

	if err := r.refreshDatadogComponent(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshDatadogComponent error: %v", err)
	}
}

func TestRefreshDatadogComponent_LogsInEdge(t *testing.T) {
	graphStore := setupGraphStore(t)

	svcNode := graph.Node{ID: "svc/frontend", Type: "deployment", ComponentID: "src-k8s"}
	if err := graphStore.AddNode(context.Background(), svcNode); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	r := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Component{ID: "src-dd-4", Type: store.ComponentTypeDatadog, Name: "test-dd"}
	adapter := &fakeDatadogAdapter{logServices: []string{"frontend"}}

	if err := r.refreshDatadogComponent(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshDatadogComponent error: %v", err)
	}
}

// ---- refreshComponent switch cases for observability types ----

func TestRefreshComponent_PrometheusType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-prom", &fakePrometheusAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	source := &store.Component{ID: "src-prom", Type: store.ComponentTypePrometheus, Name: "prom"}
	if err := r.refreshComponent(context.Background(), source); err != nil {
		t.Fatalf("refreshComponent(prometheus) error: %v", err)
	}
}

func TestRefreshComponent_MimirType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-mimir", &fakePrometheusAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	source := &store.Component{ID: "src-mimir", Type: store.ComponentTypeMimir, Name: "mimir"}
	if err := r.refreshComponent(context.Background(), source); err != nil {
		t.Fatalf("refreshComponent(mimir) error: %v", err)
	}
}

func TestRefreshComponent_LokiType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-loki", &fakeLokiAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	source := &store.Component{ID: "src-loki", Type: store.ComponentTypeLoki, Name: "loki"}
	if err := r.refreshComponent(context.Background(), source); err != nil {
		t.Fatalf("refreshComponent(loki) error: %v", err)
	}
}

func TestRefreshComponent_TempoType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-tempo", &fakeTempoAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	source := &store.Component{ID: "src-tempo", Type: store.ComponentTypeTempo, Name: "tempo"}
	if err := r.refreshComponent(context.Background(), source); err != nil {
		t.Fatalf("refreshComponent(tempo) error: %v", err)
	}
}

func TestRefreshComponent_JaegerType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-jaeger", &fakeJaegerAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	source := &store.Component{ID: "src-jaeger", Type: store.ComponentTypeJaeger, Name: "jaeger"}
	if err := r.refreshComponent(context.Background(), source); err != nil {
		t.Fatalf("refreshComponent(jaeger) error: %v", err)
	}
}

func TestRefreshComponent_DatadogType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-dd", &fakeDatadogAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	source := &store.Component{ID: "src-dd", Type: store.ComponentTypeDatadog, Name: "datadog"}
	if err := r.refreshComponent(context.Background(), source); err != nil {
		t.Fatalf("refreshComponent(datadog) error: %v", err)
	}
}

func TestRefreshComponent_UnknownType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	// Register some adapter so the Get lookup succeeds.
	reg.Register("src-unknown", &fakePrometheusAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	source := &store.Component{ID: "src-unknown", Type: "unsupported_type", Name: "unknown"}
	// Default case: returns nil (logs a debug message, continues).
	if err := r.refreshComponent(context.Background(), source); err != nil {
		t.Fatalf("refreshComponent(unknown) should return nil, got: %v", err)
	}
}

func TestRefreshComponent_AdapterNotFound(t *testing.T) {
	svc, _ := setupObsTestServices(t)
	// Do NOT register any adapter for this source.

	r := &Refresher{services: svc, logger: slog.Default()}
	source := &store.Component{ID: "src-missing", Type: store.ComponentTypePrometheus, Name: "missing"}
	if err := r.refreshComponent(context.Background(), source); err == nil {
		t.Error("expected error when adapter not found, got nil")
	}
}

func TestRefreshComponent_PrometheusWrongType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-prom-bad", &fakeLokiAdapter{}) // wrong type

	r := &Refresher{services: svc, logger: slog.Default()}
	source := &store.Component{ID: "src-prom-bad", Type: store.ComponentTypePrometheus, Name: "prom"}
	if err := r.refreshComponent(context.Background(), source); err == nil {
		t.Error("expected error for wrong adapter type, got nil")
	}
}

func TestRefreshComponent_LokiWrongType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-loki-bad", &fakePrometheusAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	source := &store.Component{ID: "src-loki-bad", Type: store.ComponentTypeLoki, Name: "loki"}
	if err := r.refreshComponent(context.Background(), source); err == nil {
		t.Error("expected error for wrong adapter type, got nil")
	}
}

func TestRefreshComponent_TempoWrongType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-tempo-bad", &fakePrometheusAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	source := &store.Component{ID: "src-tempo-bad", Type: store.ComponentTypeTempo, Name: "tempo"}
	if err := r.refreshComponent(context.Background(), source); err == nil {
		t.Error("expected error for wrong adapter type, got nil")
	}
}

func TestRefreshComponent_JaegerWrongType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-jaeger-bad", &fakePrometheusAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	source := &store.Component{ID: "src-jaeger-bad", Type: store.ComponentTypeJaeger, Name: "jaeger"}
	if err := r.refreshComponent(context.Background(), source); err == nil {
		t.Error("expected error for wrong adapter type, got nil")
	}
}
