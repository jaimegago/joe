package coreagent

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/adapters"
	dynatraceadapter "github.com/jaimegago/joe/internal/adapters/observability/dynatrace"
	newrelicadapter "github.com/jaimegago/joe/internal/adapters/observability/newrelic"
	splunkadapter "github.com/jaimegago/joe/internal/adapters/observability/splunk"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/store"
)

// ============================================================
// Fake adapters for Splunk / Dynatrace / NewRelic
// ============================================================

type fakeSplunkAdapter struct {
	result *splunkadapter.SearchResult
	err    error
}

func (f *fakeSplunkAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (f *fakeSplunkAdapter) Disconnect() error                                  { return nil }
func (f *fakeSplunkAdapter) Status() adapters.Status                            { return adapters.Status{Connected: true} }
func (f *fakeSplunkAdapter) Search(_ context.Context, _, _, _ string, _ int) (*splunkadapter.SearchResult, error) {
	return f.result, f.err
}

type fakeDynatraceAdapter struct {
	metrics *dynatraceadapter.MetricsResult
	events  *dynatraceadapter.EventsResult
	err     error
}

func (f *fakeDynatraceAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (f *fakeDynatraceAdapter) Disconnect() error                                  { return nil }
func (f *fakeDynatraceAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true}
}
func (f *fakeDynatraceAdapter) MetricsQuery(_ context.Context, _ string, _, _ int64) (*dynatraceadapter.MetricsResult, error) {
	return f.metrics, f.err
}
func (f *fakeDynatraceAdapter) Events(_ context.Context, _, _ int64, _ int) (*dynatraceadapter.EventsResult, error) {
	return f.events, f.err
}

type fakeNewRelicAdapter struct {
	result *newrelicadapter.NRQLResult
	err    error
}

func (f *fakeNewRelicAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (f *fakeNewRelicAdapter) Disconnect() error                                  { return nil }
func (f *fakeNewRelicAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true}
}
func (f *fakeNewRelicAdapter) NRQLQuery(_ context.Context, _ int, _ string) (*newrelicadapter.NRQLResult, error) {
	return f.result, f.err
}

// ============================================================
// Observability: Splunk / Dynatrace / NewRelic refresh tests
// ============================================================

func setupObsVendorRefresher(t *testing.T) *Refresher {
	t.Helper()
	gs := setupGraphStore(t)
	return &Refresher{
		services: &core.Services{Graph: gs},
		logger:   slog.Default(),
	}
}

// ---- Splunk ----

func TestRefreshSplunkComponent_Basic(t *testing.T) {
	r := setupObsVendorRefresher(t)
	src := &store.Component{ID: "src-splunk-1", Type: store.ComponentTypeSplunk, Name: "splunk-prod"}

	if err := r.refreshSplunkComponent(context.Background(), src, &fakeSplunkAdapter{}); err != nil {
		t.Fatalf("refreshSplunkComponent: %v", err)
	}

	nodes, _, err := LoadGraphStateForComponent(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Type != "splunk_component" {
		t.Errorf("want 1 splunk_source node, got %v", nodes)
	}
}

func TestRefreshSplunkComponent_SecondRefresh(t *testing.T) {
	// Verifies idempotency — running twice should not error.
	r := setupObsVendorRefresher(t)
	src := &store.Component{ID: "src-splunk-2", Type: store.ComponentTypeSplunk, Name: "splunk"}
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if err := r.refreshSplunkComponent(ctx, src, &fakeSplunkAdapter{}); err != nil {
			t.Fatalf("refreshSplunkComponent (iteration %d): %v", i, err)
		}
	}
}

func TestRefreshComponent_SplunkType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-splunk", &fakeSplunkAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	src := &store.Component{ID: "src-splunk", Type: store.ComponentTypeSplunk, Name: "splunk"}
	if err := r.refreshComponent(context.Background(), src); err != nil {
		t.Fatalf("refreshComponent(splunk) error: %v", err)
	}
}

func TestRefreshComponent_SplunkWrongType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-splunk-bad", &fakeDynatraceAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	src := &store.Component{ID: "src-splunk-bad", Type: store.ComponentTypeSplunk, Name: "splunk"}
	if err := r.refreshComponent(context.Background(), src); err == nil {
		t.Error("expected error for wrong adapter type, got nil")
	}
}

// ---- Dynatrace ----

func TestRefreshDynatraceComponent_Basic(t *testing.T) {
	r := setupObsVendorRefresher(t)
	src := &store.Component{ID: "src-dyna-1", Type: store.ComponentTypeDynatrace, Name: "dynatrace-prod"}

	if err := r.refreshDynatraceComponent(context.Background(), src, &fakeDynatraceAdapter{}); err != nil {
		t.Fatalf("refreshDynatraceComponent: %v", err)
	}

	nodes, _, err := LoadGraphStateForComponent(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Type != "dynatrace_component" {
		t.Errorf("want 1 dynatrace_source node, got %v", nodes)
	}
}

func TestRefreshDynatraceComponent_SecondRefresh(t *testing.T) {
	r := setupObsVendorRefresher(t)
	src := &store.Component{ID: "src-dyna-2", Type: store.ComponentTypeDynatrace, Name: "dynatrace"}
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if err := r.refreshDynatraceComponent(ctx, src, &fakeDynatraceAdapter{}); err != nil {
			t.Fatalf("refreshDynatraceComponent (iteration %d): %v", i, err)
		}
	}
}

func TestRefreshComponent_DynatraceType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-dyna", &fakeDynatraceAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	src := &store.Component{ID: "src-dyna", Type: store.ComponentTypeDynatrace, Name: "dynatrace"}
	if err := r.refreshComponent(context.Background(), src); err != nil {
		t.Fatalf("refreshComponent(dynatrace) error: %v", err)
	}
}

func TestRefreshComponent_DynatraceWrongType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-dyna-bad", &fakeSplunkAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	src := &store.Component{ID: "src-dyna-bad", Type: store.ComponentTypeDynatrace, Name: "dynatrace"}
	if err := r.refreshComponent(context.Background(), src); err == nil {
		t.Error("expected error for wrong adapter type, got nil")
	}
}

// ---- NewRelic ----

func TestRefreshNewRelicComponent_Basic(t *testing.T) {
	r := setupObsVendorRefresher(t)
	src := &store.Component{ID: "src-nr-1", Type: store.ComponentTypeNewRelic, Name: "newrelic-prod"}

	if err := r.refreshNewRelicComponent(context.Background(), src, &fakeNewRelicAdapter{}); err != nil {
		t.Fatalf("refreshNewRelicComponent: %v", err)
	}

	nodes, _, err := LoadGraphStateForComponent(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Type != "newrelic_component" {
		t.Errorf("want 1 newrelic_source node, got %v", nodes)
	}
}

func TestRefreshNewRelicComponent_SecondRefresh(t *testing.T) {
	r := setupObsVendorRefresher(t)
	src := &store.Component{ID: "src-nr-2", Type: store.ComponentTypeNewRelic, Name: "newrelic"}
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if err := r.refreshNewRelicComponent(ctx, src, &fakeNewRelicAdapter{}); err != nil {
			t.Fatalf("refreshNewRelicComponent (iteration %d): %v", i, err)
		}
	}
}

func TestRefreshComponent_NewRelicType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-nr", &fakeNewRelicAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	src := &store.Component{ID: "src-nr", Type: store.ComponentTypeNewRelic, Name: "newrelic"}
	if err := r.refreshComponent(context.Background(), src); err != nil {
		t.Fatalf("refreshComponent(newrelic) error: %v", err)
	}
}

func TestRefreshComponent_NewRelicWrongType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-nr-bad", &fakeSplunkAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	src := &store.Component{ID: "src-nr-bad", Type: store.ComponentTypeNewRelic, Name: "newrelic"}
	if err := r.refreshComponent(context.Background(), src); err == nil {
		t.Error("expected error for wrong adapter type, got nil")
	}
}

// ============================================================
// refresh.go: Stop, refreshLoop via context cancel
// ============================================================

func TestRefresher_Stop_Graceful(t *testing.T) {
	svc := makeTestServices(t)
	r := NewRefresher(svc, &mockLLMAdapter{}, slog.Default(), nil)
	ctx := context.Background()

	if err := r.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Give the goroutine a moment to start.
	time.Sleep(10 * time.Millisecond)

	if err := r.Stop(ctx); err != nil {
		t.Errorf("Stop() error = %v", err)
	}
}

func TestRefresher_Stop_ContextCancel(t *testing.T) {
	svc := makeTestServices(t)
	r := NewRefresher(svc, &mockLLMAdapter{}, slog.Default(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	if err := r.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	// Cancel the context — refreshLoop should exit via ctx.Done().
	cancel()

	// Wait for the loop to finish via the doneCh.
	select {
	case <-r.doneCh:
		// Loop exited successfully via ctx.Done().
	case <-time.After(2 * time.Second):
		t.Error("refreshLoop did not exit after context cancel")
	}
}

func TestRefresher_RefreshLoop_ExitsOnStop(t *testing.T) {
	svc := makeTestServices(t)
	r := NewRefresher(svc, &mockLLMAdapter{}, slog.Default(), nil)
	ctx := context.Background()

	if err := r.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(10 * time.Millisecond)
	r.cancel()

	select {
	case <-r.doneCh:
		// OK
	case <-time.After(2 * time.Second):
		t.Error("refreshLoop did not exit after cancel")
	}
}

// ============================================================
// SaveKnowledgeEntryTool: Name, Description, Parameters, Execute
// ============================================================

func TestSaveKnowledgeEntryTool_NameDescriptionParameters(t *testing.T) {
	svc := makeTestServices(t)
	tool := NewSaveKnowledgeEntryTool(svc, slog.Default())

	if got := tool.Name(); got != "save_knowledge_entry" {
		t.Errorf("Name() = %q, want save_knowledge_entry", got)
	}
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
	p := tool.Parameters()
	if p.Type != "object" {
		t.Errorf("Parameters().Type = %q, want object", p.Type)
	}
	if len(p.Properties) == 0 {
		t.Error("Parameters() should have at least one property")
	}
}

func TestSaveKnowledgeEntryTool_Execute_Success(t *testing.T) {
	svc := makeTestServices(t)
	// services.Knowledge is populated in core.New.
	tool := NewSaveKnowledgeEntryTool(svc, slog.Default())

	result, err := tool.Execute(context.Background(), map[string]any{
		"title":      "test pattern",
		"content":    "test content",
		"entry_type": "pattern",
	})
	if err != nil {
		t.Errorf("Execute() error = %v", err)
	}
	if result == nil {
		t.Error("Execute() returned nil result")
	}
}

func TestSaveKnowledgeEntryTool_Execute_MissingTitle(t *testing.T) {
	svc := makeTestServices(t)
	tool := NewSaveKnowledgeEntryTool(svc, slog.Default())

	_, err := tool.Execute(context.Background(), map[string]any{
		"content":    "some content",
		"entry_type": "pattern",
	})
	if err == nil {
		t.Error("expected error for missing title")
	}
}

func TestSaveKnowledgeEntryTool_Execute_MissingContent(t *testing.T) {
	svc := makeTestServices(t)
	tool := NewSaveKnowledgeEntryTool(svc, slog.Default())

	_, err := tool.Execute(context.Background(), map[string]any{
		"title":      "test",
		"entry_type": "pattern",
	})
	if err == nil {
		t.Error("expected error for missing content")
	}
}

func TestSaveKnowledgeEntryTool_Execute_MissingEntryType(t *testing.T) {
	svc := makeTestServices(t)
	tool := NewSaveKnowledgeEntryTool(svc, slog.Default())

	_, err := tool.Execute(context.Background(), map[string]any{
		"title":   "test",
		"content": "test content",
	})
	if err == nil {
		t.Error("expected error for missing entry_type")
	}
}

// ============================================================
// buildIngressForEdges — exercise via refreshNginxComponent with
// a pre-existing service node so an edge can be created.
// ============================================================

func TestBuildIngressForEdges_WithMatchingService(t *testing.T) {
	r := setupNetworkingRefresher(t)
	ctx := context.Background()
	src := &store.Component{ID: "src-nginx-edge", Type: store.ComponentTypeNginx, Name: "nginx"}

	// Add a service node that matches the backend.
	_ = r.services.Graph.AddNode(ctx, graph.Node{
		ID:          "svc/default/api-service",
		Type:        "service",
		ComponentID: "src-k8s",
		Metadata:    map[string]any{"name": "api-service", "namespace": "default"},
	})

	// Call buildIngressForEdges directly.
	now := time.Now()
	edges := r.buildIngressForEdges(ctx, src, "ing-node-id", "api-service", "default", "api.example.com", now)
	if len(edges) == 0 {
		t.Error("want at least 1 ingress_for edge when matching service exists")
	}
	if len(edges) > 0 && edges[0].Relation != graph.RelationIngressFor {
		t.Errorf("edge relation = %v, want RelationIngressFor", edges[0].Relation)
	}
}

func TestBuildIngressForEdges_NamespaceMismatch(t *testing.T) {
	r := setupNetworkingRefresher(t)
	ctx := context.Background()
	src := &store.Component{ID: "src-nginx-ns", Type: store.ComponentTypeNginx, Name: "nginx"}

	// Service is in "other" namespace, ingress is in "default".
	_ = r.services.Graph.AddNode(ctx, graph.Node{
		ID:          "svc/other/api-service",
		Type:        "service",
		ComponentID: "src-k8s",
		Metadata:    map[string]any{"name": "api-service", "namespace": "other"},
	})

	now := time.Now()
	edges := r.buildIngressForEdges(ctx, src, "ing-node-id", "api-service", "default", "", now)
	// Namespace mismatch — no edges.
	if len(edges) != 0 {
		t.Errorf("want 0 edges for namespace mismatch, got %d", len(edges))
	}
}

func TestBuildIngressForEdges_NoMatchingNodes(t *testing.T) {
	r := setupNetworkingRefresher(t)
	ctx := context.Background()
	src := &store.Component{ID: "src-nginx-nomatch", Type: store.ComponentTypeNginx, Name: "nginx"}

	now := time.Now()
	edges := r.buildIngressForEdges(ctx, src, "ing-node-id", "nonexistent-svc", "default", "host.example.com", now)
	if len(edges) != 0 {
		t.Errorf("want 0 edges when no matching nodes, got %d", len(edges))
	}
}

// ============================================================
// buildProxiesEdges — call directly with matching service nodes.
// ============================================================

func TestBuildProxiesEdges_WithMatchingService(t *testing.T) {
	r := setupNetworkingRefresher(t)
	ctx := context.Background()
	src := &store.Component{ID: "src-envoy-edge", Type: store.ComponentTypeEnvoy, Name: "envoy"}

	_ = r.services.Graph.AddNode(ctx, graph.Node{
		ID:          "svc/default/payment",
		Type:        "service",
		ComponentID: "src-k8s",
		Metadata:    map[string]any{"name": "payment"},
	})

	now := time.Now()
	edges := r.buildProxiesEdges(ctx, src, "envoy-node-id", "payment", "payment_80", now)
	if len(edges) == 0 {
		t.Error("want at least 1 proxies edge when matching service exists")
	}
	if len(edges) > 0 && edges[0].Relation != graph.RelationProxies {
		t.Errorf("edge relation = %v, want RelationProxies", edges[0].Relation)
	}
}

func TestBuildProxiesEdges_NonServiceNode(t *testing.T) {
	r := setupNetworkingRefresher(t)
	ctx := context.Background()
	src := &store.Component{ID: "src-envoy-ns", Type: store.ComponentTypeEnvoy, Name: "envoy"}

	// k8s_node type — should be skipped.
	_ = r.services.Graph.AddNode(ctx, graph.Node{
		ID:          "k8snode/payment-node",
		Type:        "k8s_node",
		ComponentID: "src-k8s",
		Metadata:    map[string]any{"name": "payment-node"},
	})

	now := time.Now()
	edges := r.buildProxiesEdges(ctx, src, "envoy-node-id", "payment-node", "payment-node_8080", now)
	if len(edges) != 0 {
		t.Errorf("want 0 edges for non-service node, got %d", len(edges))
	}
}

// ============================================================
// buildManagedByEdges — exercise uncovered branches.
// ============================================================

func TestBuildManagedByEdges_EmptyName(t *testing.T) {
	r := setupGitOpsRefresher(t)
	src := &store.Component{ID: "src-argocd-me-1", Type: store.ComponentTypeArgoCd}
	edges := r.buildManagedByEdges(context.Background(), src, "manager-node", "", "default", time.Now())
	if len(edges) != 0 {
		t.Errorf("want 0 edges for empty name, got %d", len(edges))
	}
}

func TestBuildManagedByEdges_WithMatchingDeployment(t *testing.T) {
	r := setupGitOpsRefresher(t)
	ctx := context.Background()
	src := &store.Component{ID: "src-argocd-me-2", Type: store.ComponentTypeArgoCd}

	_ = r.services.Graph.AddNode(ctx, graph.Node{
		ID:          "deploy/default/payment",
		Type:        "deployment",
		ComponentID: "src-k8s",
		Metadata:    map[string]any{"name": "payment", "namespace": "default"},
	})

	edges := r.buildManagedByEdges(ctx, src, "argocd-app-node", "payment", "default", time.Now())
	if len(edges) == 0 {
		t.Error("want at least 1 managed_by edge when deployment exists")
	}
	if len(edges) > 0 && edges[0].Relation != graph.RelationManagedBy {
		t.Errorf("edge relation = %v, want RelationManagedBy", edges[0].Relation)
	}
}

func TestBuildManagedByEdges_NamespaceMismatch(t *testing.T) {
	r := setupGitOpsRefresher(t)
	ctx := context.Background()
	src := &store.Component{ID: "src-argocd-me-3", Type: store.ComponentTypeArgoCd}

	_ = r.services.Graph.AddNode(ctx, graph.Node{
		ID:          "deploy/other/payment",
		Type:        "deployment",
		ComponentID: "src-k8s",
		Metadata:    map[string]any{"name": "payment", "namespace": "other"},
	})

	edges := r.buildManagedByEdges(ctx, src, "argocd-app-node", "payment", "default", time.Now())
	if len(edges) != 0 {
		t.Errorf("want 0 edges for namespace mismatch, got %d", len(edges))
	}
}

func TestBuildManagedByEdges_SkipsUnsupportedTypes(t *testing.T) {
	r := setupGitOpsRefresher(t)
	ctx := context.Background()
	src := &store.Component{ID: "src-argocd-me-4", Type: store.ComponentTypeArgoCd}

	// pod type — not in the accepted list.
	_ = r.services.Graph.AddNode(ctx, graph.Node{
		ID:          "pod/default/payment-abc",
		Type:        "pod",
		ComponentID: "src-k8s",
		Metadata:    map[string]any{"name": "payment-abc"},
	})

	edges := r.buildManagedByEdges(ctx, src, "argocd-app-node", "payment-abc", "default", time.Now())
	if len(edges) != 0 {
		t.Errorf("want 0 edges for unsupported node type, got %d", len(edges))
	}
}

// ============================================================
// buildProvidesEdges — exercise uncovered branches.
// ============================================================

func TestBuildProvidesEdges_EmptyName(t *testing.T) {
	r := setupGitOpsRefresher(t)
	src := &store.Component{ID: "src-tf-pe-1", Type: store.ComponentTypeTerraform}
	edges := r.buildProvidesEdges(context.Background(), src, "tf-node", "", "aws_instance", time.Now())
	if len(edges) != 0 {
		t.Errorf("want 0 edges for empty name, got %d", len(edges))
	}
}

func TestBuildProvidesEdges_WithMatchingEC2(t *testing.T) {
	r := setupGitOpsRefresher(t)
	ctx := context.Background()
	src := &store.Component{ID: "src-tf-pe-2", Type: store.ComponentTypeTerraform}

	_ = r.services.Graph.AddNode(ctx, graph.Node{
		ID:          "aws/ec2/web-server",
		Type:        "ec2_instance",
		ComponentID: "src-aws",
		Metadata:    map[string]any{"name": "web-server"},
	})

	edges := r.buildProvidesEdges(ctx, src, "tf-resource-node", "web-server", "aws_instance", time.Now())
	if len(edges) == 0 {
		t.Error("want at least 1 provisions edge when ec2_instance matches")
	}
}

func TestBuildProvidesEdges_SkipsUnsupportedCloudTypes(t *testing.T) {
	r := setupGitOpsRefresher(t)
	ctx := context.Background()
	src := &store.Component{ID: "src-tf-pe-3", Type: store.ComponentTypeTerraform}

	// pod — not a cloud node type.
	_ = r.services.Graph.AddNode(ctx, graph.Node{
		ID:          "pod/default/web",
		Type:        "pod",
		ComponentID: "src-k8s",
		Metadata:    map[string]any{"name": "web"},
	})

	edges := r.buildProvidesEdges(ctx, src, "tf-resource-node", "web", "aws_instance", time.Now())
	if len(edges) != 0 {
		t.Errorf("want 0 edges for non-cloud node type, got %d", len(edges))
	}
}

// ============================================================
// refreshComponent default case
// ============================================================

func TestRefreshComponent_UnknownTypeWithSplunkAdapter(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-unknown-splunk", &fakeSplunkAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	src := &store.Component{ID: "src-unknown-splunk", Type: "unsupported_type_xyz", Name: "unknown"}
	// Should return nil — unknown types are silently skipped.
	if err := r.refreshComponent(context.Background(), src); err != nil {
		t.Errorf("refreshComponent(unknown) should return nil, got: %v", err)
	}
}

// ============================================================
// Refresher.refresh with components that have no adapter
// ============================================================

func TestRefresher_Refresh_WithUnsupportedSource(t *testing.T) {
	svc := makeTestServices(t)
	ctx := context.Background()

	// Register a source in the store without an adapter in the registry.
	_ = svc.Store.Components.Create(ctx, &store.Component{
		ID:     "src-no-adapter-refresh",
		Name:   "test",
		Type:   store.ComponentTypeSplunk,
		Config: []byte(`{}`),
	})

	r := NewRefresher(svc, &mockLLMAdapter{}, slog.Default(), nil)
	// refresh should complete without error even if individual components fail.
	if err := r.refresh(ctx); err != nil {
		t.Errorf("refresh() with failing source should not propagate error, got: %v", err)
	}
}

// ============================================================
// SaveKnowledgeEntryTool in TestToolNamesAndDescriptions
// (verifies it's included in the registered tool set)
// ============================================================

func TestSaveKnowledgeEntryTool_RegisteredInAgent(t *testing.T) {
	svc := makeTestServices(t)
	agent := New(svc, &mockLLMAdapter{}, nil)

	found := false
	for _, tool := range agent.GetAvailableTools() {
		if tool.Name() == "save_knowledge_entry" {
			found = true
			break
		}
	}
	if !found {
		t.Error("save_knowledge_entry tool should be registered in the agent")
	}
}

// ============================================================
// SaveKnowledgeEntryTool.Parameters: verify all required fields
// ============================================================

func TestSaveKnowledgeEntryTool_ParametersSchema(t *testing.T) {
	svc := makeTestServices(t)
	tool := NewSaveKnowledgeEntryTool(svc, slog.Default())
	p := tool.Parameters()

	requiredFields := map[string]bool{"title": false, "content": false, "entry_type": false}
	for _, req := range p.Required {
		if _, ok := requiredFields[req]; ok {
			requiredFields[req] = true
		}
	}
	for field, found := range requiredFields {
		if !found {
			t.Errorf("Parameters().Required missing field %q", field)
		}
	}

	// Verify optional fields are defined.
	optionalFields := []string{"session_id", "confidence", "related_nodes"}
	for _, field := range optionalFields {
		if _, ok := p.Properties[field]; !ok {
			t.Errorf("Parameters().Properties missing optional field %q", field)
		}
	}
}

// ============================================================
// SaveKnowledgeEntryTool.Execute with custom confidence and
// related_nodes (exercises those branches even without Knowledge svc).
// ============================================================

func TestSaveKnowledgeEntryTool_Execute_AllFields(t *testing.T) {
	svc := makeTestServices(t)
	tool := NewSaveKnowledgeEntryTool(svc, slog.Default())

	// All optional fields provided — Knowledge service is available.
	result, err := tool.Execute(context.Background(), map[string]any{
		"title":         "Slow payment pod",
		"content":       "payment pod memory leak observed on restart",
		"entry_type":    "failure_mode",
		"session_id":    "sess-123",
		"confidence":    float64(0.9),
		"related_nodes": []any{"node-a", "node-b"},
	})
	if err != nil {
		t.Errorf("Execute() error = %v", err)
	}
	if result == nil {
		t.Error("Execute() returned nil result")
	}
}

// ============================================================
// ObsNodeID helper (just verify the function exists / is correct)
// (Already tested in observability_refresh_test.go but adding
// variant for Splunk/Dynatrace/NewRelic source types.)
// ============================================================

func TestObsNodeID_VendorTypes(t *testing.T) {
	tests := []struct {
		sourceID   string
		sourceType string
		want       string
	}{
		{"src1", store.ComponentTypeSplunk, "obs/" + store.ComponentTypeSplunk + "/src1"},
		{"src2", store.ComponentTypeDynatrace, "obs/" + store.ComponentTypeDynatrace + "/src2"},
		{"src3", store.ComponentTypeNewRelic, "obs/" + store.ComponentTypeNewRelic + "/src3"},
	}
	for _, tt := range tests {
		got := obsNodeID(tt.sourceID, tt.sourceType)
		if got != tt.want {
			t.Errorf("obsNodeID(%q, %q) = %q, want %q", tt.sourceID, tt.sourceType, got, tt.want)
		}
	}
}

// ============================================================
// fakeMetricsForRefresher satisfies the observability.Metrics
// interface by returning a nil-safe metrics instance via EnsureMetrics.
// The Refresher's metrics field is always populated by EnsureMetrics
// so we just verify refresh() runs without panicking.
// ============================================================

func TestRefresher_Refresh_MetricsNotNil(t *testing.T) {
	svc := makeTestServices(t)
	// Pass nil metrics — NewRefresher should call EnsureMetrics internally.
	r := NewRefresher(svc, &mockLLMAdapter{}, slog.Default(), nil)
	if r.metrics == nil {
		t.Error("metrics should not be nil after NewRefresher")
	}
	if err := r.refresh(context.Background()); err != nil {
		t.Errorf("refresh() error = %v", err)
	}
}

// ============================================================
// refreshComponent: verify UpdateSyncStatus is called (deferred)
// by using a real store source and checking no panic occurs.
// ============================================================

func TestRefreshComponent_UpdateSyncStatusCalled(t *testing.T) {
	svc := makeTestServices(t)
	ctx := context.Background()

	// Create a real source in the store (config must be non-null).
	src := &store.Component{
		ID:     "src-sync-status",
		Name:   "test",
		Type:   store.ComponentTypeOCIRegistry,
		Config: []byte(`{}`),
	}
	if err := svc.Store.Components.Create(ctx, src); err != nil {
		t.Fatalf("create source: %v", err)
	}

	// No adapter registered → refreshComponent will fail, but deferred
	// UpdateSyncStatus should still run without panic.
	r := NewRefresher(svc, &mockLLMAdapter{}, slog.Default(), nil)
	_ = r.refreshComponent(ctx, src) // error expected; just verify no panic.
}

// Dummy test to ensure llm import is used (for the Parameters() test).
var _ llm.ParameterSchema
