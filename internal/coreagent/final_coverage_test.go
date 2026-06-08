package coreagent

// final_coverage_test.go — targeted tests to push coverage above 90%.
// Covers remaining uncovered branches in buildMetricsInEdges, buildAlertsInEdges,
// selectorMatches, extractWorkloadInfo, executeJoeFileToolCalls, Loki/Tempo/Jaeger
// non-service-node paths, and applyRegistryDelta error paths.

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	alertmanageradapter "github.com/jaimegago/joe/internal/adapters/alerting/alertmanager"
	gitadapter "github.com/jaimegago/joe/internal/adapters/git"
	prometheusadapter "github.com/jaimegago/joe/internal/adapters/observability/prometheus"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/store"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ============================================================
// buildMetricsInEdges: non-service-node branch
// ============================================================

func TestBuildMetricsInEdges_SkipsNonServiceNode(t *testing.T) {
	gs := setupGraphStore(t)
	ctx := context.Background()

	// Add a node that is NOT a service or deployment — it should be skipped.
	if err := gs.AddNode(ctx, graph.Node{
		ID:       "k8snode/worker-1",
		Type:     "k8s_node",
		Metadata: map[string]any{"name": "worker-1"},
	}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	r := &Refresher{services: &core.Services{Graph: gs}, logger: slog.Default()}
	src := &store.Component{ID: "src-prom-skip", Type: store.ComponentTypePrometheus}

	edges, err := r.buildMetricsInEdges(ctx, src, "prom-node", []prometheusadapter.Target{
		{State: "active", Labels: map[string]string{"job": "worker-1"}},
	}, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("want 0 edges for non-service node, got %d", len(edges))
	}
}

func TestBuildMetricsInEdges_WithMatchingDeployment(t *testing.T) {
	gs := setupGraphStore(t)
	ctx := context.Background()

	if err := gs.AddNode(ctx, graph.Node{
		ID:       "deploy/api",
		Type:     "deployment",
		Metadata: map[string]any{"name": "api"},
	}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	r := &Refresher{services: &core.Services{Graph: gs}, logger: slog.Default()}
	src := &store.Component{ID: "src-prom-dep", Type: store.ComponentTypePrometheus}

	edges, err := r.buildMetricsInEdges(ctx, src, "prom-node", []prometheusadapter.Target{
		{State: "active", Labels: map[string]string{"job": "api"}},
	}, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edges) != 1 {
		t.Errorf("want 1 edge, got %d", len(edges))
	}
	if len(edges) > 0 && edges[0].Relation != graph.RelationMetricsIn {
		t.Errorf("edge relation = %v, want RelationMetricsIn", edges[0].Relation)
	}
}

// ============================================================
// buildAlertsInEdges: deduplication (seen[svcName] + seen[svcNode.ID])
// ============================================================

func TestBuildAlertsInEdges_Deduplication(t *testing.T) {
	gs := setupGraphStore(t)
	ctx := context.Background()

	// Add a deployment node that matches "payment".
	if err := gs.AddNode(ctx, graph.Node{
		ID:       "deploy/payment",
		Type:     "deployment",
		Metadata: map[string]any{"name": "payment"},
	}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	r := &Refresher{services: &core.Services{Graph: gs}, logger: slog.Default()}
	src := &store.Component{ID: "src-am-dedup", Type: store.ComponentTypeAlertmanager}

	// Two alerts with the same service name → only one edge expected.
	alerts := []alertmanageradapter.Alert{
		{
			Fingerprint: "fp-1",
			Status:      alertmanageradapter.AlertStatus{State: "active"},
			Labels:      map[string]string{"service": "payment"},
		},
		{
			Fingerprint: "fp-2",
			Status:      alertmanageradapter.AlertStatus{State: "active"},
			Labels:      map[string]string{"service": "payment"}, // duplicate svcName
		},
	}

	edges, err := r.buildAlertsInEdges(ctx, src, "am-node", alerts, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edges) != 1 {
		t.Errorf("want 1 edge (deduplicated), got %d", len(edges))
	}
}

func TestBuildAlertsInEdges_NonServiceNodeSkipped(t *testing.T) {
	gs := setupGraphStore(t)
	ctx := context.Background()

	// Add a k8s_node (not service/deployment) — should be skipped.
	if err := gs.AddNode(ctx, graph.Node{
		ID:       "k8snode/controller",
		Type:     "k8s_node",
		Metadata: map[string]any{"name": "controller"},
	}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	r := &Refresher{services: &core.Services{Graph: gs}, logger: slog.Default()}
	src := &store.Component{ID: "src-am-skip", Type: store.ComponentTypeAlertmanager}

	alerts := []alertmanageradapter.Alert{
		{
			Fingerprint: "fp-skip",
			Status:      alertmanageradapter.AlertStatus{State: "active"},
			Labels:      map[string]string{"service": "controller"},
		},
	}

	edges, err := r.buildAlertsInEdges(ctx, src, "am-node", alerts, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("want 0 edges for non-service/deployment node, got %d", len(edges))
	}
}

// ============================================================
// selectorMatches: partial match returns false
// ============================================================

func TestSelectorMatches_EmptySelector(t *testing.T) {
	if selectorMatches(map[string]string{}, map[string]string{"app": "foo"}) {
		t.Error("empty selector should return false")
	}
}

func TestSelectorMatches_FullMatch(t *testing.T) {
	selector := map[string]string{"app": "payment", "env": "prod"}
	labels := map[string]string{"app": "payment", "env": "prod", "extra": "ignore"}
	if !selectorMatches(selector, labels) {
		t.Error("full match should return true")
	}
}

func TestSelectorMatches_PartialMismatch(t *testing.T) {
	selector := map[string]string{"app": "payment", "env": "prod"}
	labels := map[string]string{"app": "payment", "env": "staging"}
	if selectorMatches(selector, labels) {
		t.Error("partial mismatch should return false")
	}
}

func TestSelectorMatches_MissingKey(t *testing.T) {
	selector := map[string]string{"app": "payment"}
	labels := map[string]string{"version": "v1"} // no "app" key
	if selectorMatches(selector, labels) {
		t.Error("missing key in labels should return false")
	}
}

// ============================================================
// extractWorkloadInfo: volumes with configMap/secret, envFrom paths
// ============================================================

func TestExtractWorkloadInfo_WithVolumesAndEnvFrom(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{"name": "api-server"},
			"spec": map[string]any{
				"selector": map[string]any{
					"matchLabels": map[string]any{"app": "api"},
				},
				"template": map[string]any{
					"metadata": map[string]any{
						"labels": map[string]any{"app": "api", "version": "v2"},
					},
					"spec": map[string]any{
						"volumes": []any{
							map[string]any{
								"name":      "config-vol",
								"configMap": map[string]any{"name": "app-config"},
							},
							map[string]any{
								"name":   "secret-vol",
								"secret": map[string]any{"secretName": "app-secret"},
							},
							// non-map entry — should be skipped gracefully
							"invalid-volume-entry",
						},
						"containers": []any{
							map[string]any{
								"name": "main",
								"envFrom": []any{
									map[string]any{
										"configMapRef": map[string]any{"name": "env-config"},
									},
									map[string]any{
										"secretRef": map[string]any{"name": "env-secret"},
									},
									// non-map envFrom entry — should be skipped
									"invalid",
								},
								"env": []any{
									map[string]any{
										"name": "DB_URL",
										"valueFrom": map[string]any{
											"secretKeyRef": map[string]any{"name": "db-secret", "key": "url"},
										},
									},
									map[string]any{
										"name": "LOG_LEVEL",
										"valueFrom": map[string]any{
											"configMapKeyRef": map[string]any{"name": "logging-config", "key": "level"},
										},
									},
									// env entry without valueFrom — should be skipped
									map[string]any{
										"name":  "STATIC",
										"value": "true",
									},
									// non-map env entry
									"invalid-env",
								},
							},
						},
						"initContainers": []any{
							map[string]any{
								"name": "init",
								"envFrom": []any{
									map[string]any{
										"configMapRef": map[string]any{"name": "init-config"},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	info := extractWorkloadInfo("src-k8s", "deployment", "production", obj)

	if info.ID == "" {
		t.Error("expected non-empty ID")
	}
	if info.NodeType != "deployment" {
		t.Errorf("NodeType = %q, want deployment", info.NodeType)
	}
	if info.Namespace != "production" {
		t.Errorf("Namespace = %q, want production", info.Namespace)
	}
	if len(info.PodLabels) == 0 {
		t.Error("expected PodLabels to be populated")
	}
	if _, ok := info.ConfigMaps["app-config"]; !ok {
		t.Error("expected ConfigMaps to contain app-config (from volume)")
	}
	if _, ok := info.Secrets["app-secret"]; !ok {
		t.Error("expected Secrets to contain app-secret (from volume)")
	}
	if _, ok := info.ConfigMaps["env-config"]; !ok {
		t.Error("expected ConfigMaps to contain env-config (from envFrom)")
	}
	if _, ok := info.Secrets["env-secret"]; !ok {
		t.Error("expected Secrets to contain env-secret (from envFrom)")
	}
	if _, ok := info.Secrets["db-secret"]; !ok {
		t.Error("expected Secrets to contain db-secret (from env.valueFrom.secretKeyRef)")
	}
	if _, ok := info.ConfigMaps["logging-config"]; !ok {
		t.Error("expected ConfigMaps to contain logging-config (from env.valueFrom.configMapKeyRef)")
	}
	if _, ok := info.ConfigMaps["init-config"]; !ok {
		t.Error("expected ConfigMaps to contain init-config (from initContainers envFrom)")
	}
}

// ============================================================
// executeJoeFileToolCalls: unknown tool, direct invocation
// ============================================================

func TestExecuteJoeFileToolCalls_AllBranches(t *testing.T) {
	gs := setupGraphStore(t)
	svc := makeTestServices(t)
	svc.Graph = gs

	r := &Refresher{
		services: svc,
		logger:   slog.Default(),
	}
	ctx := context.Background()

	toolCalls := []llm.ToolCall{
		{
			ID:   "call-1",
			Name: "graph_add_node",
			Args: map[string]any{
				"node_id":   "service/test-exec",
				"node_type": "service",
				"metadata":  map[string]any{"team": "platform"},
			},
		},
		{
			ID:   "call-2",
			Name: "graph_add_edge",
			Args: map[string]any{
				"from":     "service/test-exec",
				"to":       "database/orders",
				"relation": "uses",
			},
		},
		{
			ID:   "call-3",
			Name: "save_onboarding_fact",
			Args: map[string]any{
				"fact_type": "ownership",
				"subject":   "test-exec",
				"content":   "Owned by platform team",
			},
		},
		{
			ID:   "call-4",
			Name: "unknown_tool_xyz",
			Args: map[string]any{},
		},
	}

	if err := r.executeJoeFileToolCalls(ctx, toolCalls, "src-git-exec"); err != nil {
		t.Fatalf("executeJoeFileToolCalls error: %v", err)
	}

	// Verify node was added.
	node, err := gs.GetNode(ctx, "service/test-exec")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if node.Type != "service" {
		t.Errorf("node.Type = %q, want service", node.Type)
	}
}

func TestExecuteJoeFileToolCalls_AddNodeMissingArgs(t *testing.T) {
	gs := setupGraphStore(t)
	r := &Refresher{
		services: &core.Services{Graph: gs},
		logger:   slog.Default(),
	}
	ctx := context.Background()

	// Missing node_id and node_type — should log warning but not return error.
	toolCalls := []llm.ToolCall{
		{
			ID:   "call-bad",
			Name: "graph_add_node",
			Args: map[string]any{}, // missing required fields
		},
	}

	if err := r.executeJoeFileToolCalls(ctx, toolCalls, "src-git-bad"); err != nil {
		t.Fatalf("executeJoeFileToolCalls should not propagate error, got: %v", err)
	}
}

func TestExecuteJoeFileToolCalls_AddEdgeMissingArgs(t *testing.T) {
	gs := setupGraphStore(t)
	r := &Refresher{
		services: &core.Services{Graph: gs},
		logger:   slog.Default(),
	}
	ctx := context.Background()

	toolCalls := []llm.ToolCall{
		{
			ID:   "call-bad-edge",
			Name: "graph_add_edge",
			Args: map[string]any{"from": "a"}, // missing to and relation
		},
	}

	if err := r.executeJoeFileToolCalls(ctx, toolCalls, "src-git-bad-edge"); err != nil {
		t.Fatalf("executeJoeFileToolCalls should not propagate error, got: %v", err)
	}
}

func TestExecuteJoeFileToolCalls_SaveFactMissingArgs(t *testing.T) {
	gs := setupGraphStore(t)
	svc := makeTestServices(t)
	svc.Graph = gs

	r := &Refresher{
		services: svc,
		logger:   slog.Default(),
	}
	ctx := context.Background()

	toolCalls := []llm.ToolCall{
		{
			ID:   "call-bad-fact",
			Name: "save_onboarding_fact",
			Args: map[string]any{"fact_type": "ownership"}, // missing subject and content
		},
	}

	if err := r.executeJoeFileToolCalls(ctx, toolCalls, "src-git-bad-fact"); err != nil {
		t.Fatalf("executeJoeFileToolCalls should not propagate error, got: %v", err)
	}
}

// ============================================================
// refreshGitComponent: ProcessJoeFiles error branch (joe_dir_present = false)
// ============================================================

type errListGitAdapter struct {
	fakeGitAdapter
	listErr error
}

func (e *errListGitAdapter) ListFiles(_ context.Context, dir string) ([]gitadapter.FileInfo, error) {
	if dir == ".joe" {
		return nil, e.listErr
	}
	return nil, nil
}

func TestRefreshGitComponent_ProcessJoeFilesError(t *testing.T) {
	gs := setupGraphStore(t)
	cache := newFakeCache()
	fakeLLMInst := &fakeLLM{}
	joeFileService := NewJoeFileService(cache, fakeLLMInst, slog.Default(), nil)

	refresher := &Refresher{
		services:       &core.Services{Graph: gs},
		joeFileService: joeFileService,
		logger:         slog.Default(),
	}

	source := &store.Component{ID: "src-git-err", Type: store.ComponentTypeGit, Name: "err-repo"}
	adapter := &errListGitAdapter{
		listErr: errors.New("permission denied"),
	}

	// Should not return an error — the git refresh logs the warning and continues.
	if err := refresher.refreshGitComponent(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshGitComponent should not error on ProcessJoeFiles failure, got: %v", err)
	}

	nodes, _, err := LoadGraphStateForComponent(context.Background(), gs, source.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent error: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(nodes))
	}
	// joe_dir_present should be false on error.
	joePresent, _ := nodes[0].Metadata["joe_dir_present"].(bool)
	if joePresent {
		t.Error("joe_dir_present should be false when ProcessJoeFiles errors")
	}
}

// ============================================================
// Loki/Tempo/Jaeger: non-service-node skip branch
// ============================================================

func TestRefreshLokiComponent_SkipsNonServiceNode(t *testing.T) {
	gs := setupGraphStore(t)
	ctx := context.Background()

	// Add a non-service, non-deployment node.
	if err := gs.AddNode(ctx, graph.Node{
		ID:       "k8snode/loki-worker",
		Type:     "k8s_node",
		Metadata: map[string]any{"name": "loki-worker"},
	}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	r := &Refresher{services: &core.Services{Graph: gs}, logger: slog.Default()}
	src := &store.Component{ID: "src-loki-skip", Type: store.ComponentTypeLoki, Name: "test-loki"}
	adapter := &fakeLokiAdapter{services: []string{"loki-worker"}}

	if err := r.refreshLokiComponent(ctx, src, adapter); err != nil {
		t.Fatalf("refreshLokiComponent error: %v", err)
	}

	// The loki node itself is added, but no logs_in edge should exist.
	nodes, edges, err := LoadGraphStateForComponent(ctx, gs, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("want 1 node (loki source), got %d", len(nodes))
	}
	if len(edges) != 0 {
		t.Errorf("want 0 edges (non-service skipped), got %d", len(edges))
	}
}

func TestRefreshTempoComponent_SkipsNonServiceNode(t *testing.T) {
	gs := setupGraphStore(t)
	ctx := context.Background()

	if err := gs.AddNode(ctx, graph.Node{
		ID:       "k8snode/tempo-worker",
		Type:     "k8s_node",
		Metadata: map[string]any{"name": "tempo-worker"},
	}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	r := &Refresher{services: &core.Services{Graph: gs}, logger: slog.Default()}
	src := &store.Component{ID: "src-tempo-skip", Type: store.ComponentTypeTempo, Name: "test-tempo"}
	adapter := &fakeTempoAdapter{services: []string{"tempo-worker"}}

	if err := r.refreshTempoComponent(ctx, src, adapter); err != nil {
		t.Fatalf("refreshTempoComponent error: %v", err)
	}

	_, edges, err := LoadGraphStateForComponent(ctx, gs, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("want 0 edges for non-service node, got %d", len(edges))
	}
}

func TestRefreshJaegerComponent_SkipsNonServiceNode(t *testing.T) {
	gs := setupGraphStore(t)
	ctx := context.Background()

	if err := gs.AddNode(ctx, graph.Node{
		ID:       "k8snode/jaeger-worker",
		Type:     "k8s_node",
		Metadata: map[string]any{"name": "jaeger-worker"},
	}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	r := &Refresher{services: &core.Services{Graph: gs}, logger: slog.Default()}
	src := &store.Component{ID: "src-jaeger-skip", Type: store.ComponentTypeJaeger, Name: "test-jaeger"}
	adapter := &fakeJaegerAdapter{services: []string{"jaeger-worker"}}

	if err := r.refreshJaegerComponent(ctx, src, adapter); err != nil {
		t.Fatalf("refreshJaegerComponent error: %v", err)
	}

	_, edges, err := LoadGraphStateForComponent(ctx, gs, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("want 0 edges for non-service node, got %d", len(edges))
	}
}

// ============================================================
// buildMetricsInEdges: query error path (continue branch)
// ============================================================

func TestBuildMetricsInEdges_QueryError(t *testing.T) {
	gs := &errorGraphStore{queryErr: errors.New("graph query failed")}
	r := makeErrRefresher(t, gs)
	src := &store.Component{ID: "src-prom-qerr", Type: store.ComponentTypePrometheus}

	edges, err := r.buildMetricsInEdges(context.Background(), src, "prom-node", []prometheusadapter.Target{
		{State: "active", Labels: map[string]string{"job": "payment"}},
	}, time.Now())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("want 0 edges on query error, got %d", len(edges))
	}
}

// ============================================================
// applyRegistryDelta: error path from LoadGraphStateForComponent
// ============================================================

type errListNodesGraphStore struct {
	errorGraphStore
}

func (e *errListNodesGraphStore) ListNodesByComponent(_ context.Context, _ string) ([]graph.Node, error) {
	return nil, errors.New("db error listing nodes")
}

func TestApplyRegistryDelta_LoadGraphStateError(t *testing.T) {
	gs := &errListNodesGraphStore{}
	r := makeErrRefresher(t, gs)
	src := &store.Component{ID: "src-reg-err", Type: store.ComponentTypeOCIRegistry}

	err := r.applyRegistryDelta(context.Background(), src, []graph.Node{}, []graph.Edge{}, "oci")
	if err == nil {
		t.Error("expected error when LoadGraphStateForComponent fails")
	}
}

// ============================================================
// Splunk / Dynatrace / NewRelic: LoadGraphStateForComponent error paths
// ============================================================

func TestRefreshSplunkComponent_LoadGraphStateError(t *testing.T) {
	gs := &errListNodesGraphStore{}
	r := makeErrRefresher(t, gs)
	src := &store.Component{ID: "src-splunk-err", Type: store.ComponentTypeSplunk}

	err := r.refreshSplunkComponent(context.Background(), src, &fakeSplunkAdapter{})
	if err == nil {
		t.Error("expected error when LoadGraphStateForComponent fails")
	}
}

func TestRefreshDynatraceComponent_LoadGraphStateError(t *testing.T) {
	gs := &errListNodesGraphStore{}
	r := makeErrRefresher(t, gs)
	src := &store.Component{ID: "src-dyna-err", Type: store.ComponentTypeDynatrace}

	err := r.refreshDynatraceComponent(context.Background(), src, &fakeDynatraceAdapter{})
	if err == nil {
		t.Error("expected error when LoadGraphStateForComponent fails")
	}
}

func TestRefreshNewRelicComponent_LoadGraphStateError(t *testing.T) {
	gs := &errListNodesGraphStore{}
	r := makeErrRefresher(t, gs)
	src := &store.Component{ID: "src-nr-err", Type: store.ComponentTypeNewRelic}

	err := r.refreshNewRelicComponent(context.Background(), src, &fakeNewRelicAdapter{})
	if err == nil {
		t.Error("expected error when LoadGraphStateForComponent fails")
	}
}

func TestRefreshPrometheusComponent_LoadGraphStateError(t *testing.T) {
	gs := &errListNodesGraphStore{}
	r := makeErrRefresher(t, gs)
	src := &store.Component{ID: "src-prom-lgs-err", Type: store.ComponentTypePrometheus}

	err := r.refreshPrometheusComponent(context.Background(), src, &fakePrometheusAdapter{})
	if err == nil {
		t.Error("expected error when LoadGraphStateForComponent fails")
	}
}

func TestRefreshLokiComponent_LoadGraphStateError(t *testing.T) {
	gs := &errListNodesGraphStore{}
	r := makeErrRefresher(t, gs)
	src := &store.Component{ID: "src-loki-lgs-err", Type: store.ComponentTypeLoki}

	err := r.refreshLokiComponent(context.Background(), src, &fakeLokiAdapter{})
	if err == nil {
		t.Error("expected error when LoadGraphStateForComponent fails")
	}
}

func TestRefreshTempoComponent_LoadGraphStateError(t *testing.T) {
	gs := &errListNodesGraphStore{}
	r := makeErrRefresher(t, gs)
	src := &store.Component{ID: "src-tempo-lgs-err", Type: store.ComponentTypeTempo}

	err := r.refreshTempoComponent(context.Background(), src, &fakeTempoAdapter{})
	if err == nil {
		t.Error("expected error when LoadGraphStateForComponent fails")
	}
}

func TestRefreshJaegerComponent_LoadGraphStateError(t *testing.T) {
	gs := &errListNodesGraphStore{}
	r := makeErrRefresher(t, gs)
	src := &store.Component{ID: "src-jaeger-lgs-err", Type: store.ComponentTypeJaeger}

	err := r.refreshJaegerComponent(context.Background(), src, &fakeJaegerAdapter{})
	if err == nil {
		t.Error("expected error when LoadGraphStateForComponent fails")
	}
}

// ============================================================
// ProcessJoeFiles: cache unmarshal error → fall through to LLM
// ============================================================

// errCache is like fakeCache but returns errors on Get
type errSetCache struct {
	fakeCache
	setErr error
}

func (c *errSetCache) Set(_ context.Context, _ *store.JoeFileCache) error {
	return c.setErr
}

func TestProcessJoeFiles_CacheSetError(t *testing.T) {
	// cache.Set error should be non-fatal
	cache := &errSetCache{
		fakeCache: *newFakeCache(),
		setErr:    errors.New("disk full"),
	}
	fakeLLMInst := &fakeLLM{
		returnCalls: []llm.ToolCall{
			{ID: "t1", Name: "graph_add_node", Args: map[string]any{"node_id": "svc/x", "node_type": "service"}},
		},
	}
	service := NewJoeFileService(cache, fakeLLMInst, slog.Default(), nil)

	gitA := &fakeGitAdapterWithContent{
		files: []gitadapter.FileInfo{
			{Path: ".joe/manifest.yaml", IsDir: false},
		},
		fileContent: map[string]string{
			".joe/manifest.yaml": "joe_version: '1.0'",
		},
	}

	toolCalls, err := service.ProcessJoeFiles(context.Background(), gitA, "src-cache-err")
	if err != nil {
		t.Fatalf("ProcessJoeFiles error: %v", err)
	}
	// LLM call returned 1 tool call; cache Set errored but we still get results
	if len(toolCalls) != 1 {
		t.Errorf("want 1 tool call even with cache Set error, got %d", len(toolCalls))
	}
}

func TestProcessJoeFiles_CacheUnmarshalError_FallsBackToLLM(t *testing.T) {
	// Cache returns a hit (hash matches) but ToolCalls has invalid JSON.
	// ProcessJoeFiles should fall through to LLM interpretation.
	fileContent := "joe_version: '1.0'\nrepo:\n  name: api"
	hash := computeContentHash(fileContent)

	cache := newFakeCache()
	// Seed cache with WRONG hash to force hash mismatch → cache miss → LLM called
	// Actually we need the hash to match but JSON to be invalid.
	// The code checks: cached != nil && cached.ContentHash == hash && cached.ToolCalls != nil
	// Then does json.Unmarshal. If Unmarshal fails, toolCalls = nil (falls to LLM).
	_ = cache.Set(context.Background(), &store.JoeFileCache{
		FilePath:    ".joe/api.yaml",
		ContentHash: hash,
		ToolCalls:   []byte("{{bad json that cannot unmarshal}}"),
	})

	fakeLLMInst := &fakeLLM{
		returnCalls: []llm.ToolCall{
			{ID: "from-llm", Name: "graph_add_node", Args: map[string]any{"node_id": "svc/api", "node_type": "service"}},
		},
	}
	service := NewJoeFileService(cache, fakeLLMInst, slog.Default(), nil)

	gitA := &fakeGitAdapterWithContent{
		files: []gitadapter.FileInfo{
			{Path: ".joe/api.yaml", IsDir: false},
		},
		fileContent: map[string]string{
			".joe/api.yaml": fileContent,
		},
	}

	toolCalls, err := service.ProcessJoeFiles(context.Background(), gitA, "src-bad-cache")
	if err != nil {
		t.Fatalf("ProcessJoeFiles error: %v", err)
	}
	// LLM was called because cache unmarshal failed
	if fakeLLMInst.chatCalls == 0 {
		t.Error("expected LLM to be called after cache unmarshal failure")
	}
	if len(toolCalls) != 1 {
		t.Errorf("want 1 tool call from LLM fallback, got %d", len(toolCalls))
	}
}

// ============================================================
// refreshPrometheusComponent: buildMetricsInEdges error path
// NOTE: buildMetricsInEdges always returns nil error currently,
// so we focus on the refreshPrometheusComponent ApplyGraphDelta error.
// ============================================================

type addEdgeErrGraphStore struct {
	errorGraphStore
	nodeCount int
}

func (e *addEdgeErrGraphStore) AddNode(ctx context.Context, node graph.Node) error {
	e.nodeCount++
	return nil
}

func (e *addEdgeErrGraphStore) ListNodesByComponent(_ context.Context, _ string) ([]graph.Node, error) {
	return nil, nil
}

func (e *addEdgeErrGraphStore) ListEdgesForNodes(_ context.Context, _ []string) ([]graph.Edge, error) {
	return nil, nil
}

func (e *addEdgeErrGraphStore) AddEdge(_ context.Context, _ graph.Edge) error {
	return errors.New("failed to add edge")
}

func TestRefreshPrometheusComponent_ApplyDeltaError(t *testing.T) {
	// Seed a real service node so buildMetricsInEdges produces an edge,
	// then make AddEdge fail so ApplyGraphDelta returns an error.
	realGs := setupGraphStore(t)
	ctx := context.Background()

	if err := realGs.AddNode(ctx, graph.Node{
		ID:       "svc/billing",
		Type:     "service",
		Metadata: map[string]any{"name": "billing"},
	}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	// Use a graph store that allows AddNode (for the prom node) but fails AddEdge.
	gs := &addEdgeErrGraphStore{}
	r := &Refresher{
		services: &core.Services{Graph: gs},
		logger:   slog.Default(),
	}
	src := &store.Component{ID: "src-prom-applyerr", Type: store.ComponentTypePrometheus}

	// Use the real graph for the query in buildMetricsInEdges.
	r.services.Graph = realGs

	// Test that when AddEdge fails, the function returns an error.
	errGs := &errorGraphStore{addEdgeErr: errors.New("constraint violation"), underlying: realGs}
	r2 := &Refresher{
		services: &core.Services{Graph: errGs},
		logger:   slog.Default(),
	}

	adapter := &fakePrometheusAdapter{
		targets: []prometheusadapter.Target{
			{State: "active", Labels: map[string]string{"job": "billing"}},
		},
	}

	err := r2.refreshPrometheusComponent(ctx, src, adapter)
	if err == nil {
		t.Error("expected error when AddEdge fails during ApplyGraphDelta")
	}
}
