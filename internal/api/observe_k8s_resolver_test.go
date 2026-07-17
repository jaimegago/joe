package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
	_ "modernc.org/sqlite"
)

// k8sResolverFixture stages the graph the kubernetes refresher actually writes:
// unprefixed node types (service/deployment — internal/coreagent/k8s_refresh.go
// nodeSpecs) with ComponentID stamped on every node, plus the component rows the
// nodes point at. It is deliberately built from the refresher's real vocabulary,
// not the "k8s_"-prefixed vocabulary no production writer emits.
type k8sResolverFixture struct {
	server   *Server
	sqlStore *store.Store
}

func newK8sResolverFixture(t *testing.T) *k8sResolverFixture {
	t.Helper()
	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })

	graphStore := graph.NewSQLiteStore(sqlStore.DB(), nil)
	server := New(&core.Services{
		Config:   &config.Config{},
		Store:    sqlStore,
		Graph:    graphStore,
		Adapters: adapters.NewRegistry(),
	}, nil)

	return &k8sResolverFixture{server: server, sqlStore: sqlStore}
}

// addComponent inserts a component row of the given type.
func (f *k8sResolverFixture) addComponent(t *testing.T, id, componentType string) {
	t.Helper()
	now := time.Now().UTC()
	if err := f.sqlStore.Components.Create(context.Background(), &store.Component{
		ID: id, Type: componentType, Name: id, Config: json.RawMessage(`{}`),
		Status: "connected", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create component %q: %v", id, err)
	}
}

// addNode inserts a graph node shaped like the refresher's output.
func (f *k8sResolverFixture) addNode(t *testing.T, id, nodeType, componentID string) {
	t.Helper()
	if err := f.server.services.Graph.AddNode(context.Background(), graph.Node{
		ID: id, Type: nodeType, ComponentID: componentID,
		Metadata: map[string]any{"name": id},
	}); err != nil {
		t.Fatalf("add node %q: %v", id, err)
	}
}

func (f *k8sResolverFixture) addEdge(t *testing.T, from, to, relation string) {
	t.Helper()
	if err := f.server.services.Graph.AddEdge(context.Background(), graph.Edge{
		From: from, To: to, Relation: relation,
		Confidence: graph.Explicit, Source: "k8s_api",
	}); err != nil {
		t.Fatalf("add edge %s->%s: %v", from, to, err)
	}
}

func (f *k8sResolverFixture) resolve(t *testing.T, service string) (string, error) {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/v1/observe/k8s", nil)
	return f.server.resolveK8sComponentForService(req, service)
}

// TestResolveK8sComponentForService_RefresherVocabulary is the break-test: a
// service the kubernetes refresher discovered, related to a deployment from the
// same cluster component, must resolve to that component. Every node carries the
// refresher's real type vocabulary ("service"/"deployment") and its ComponentID,
// which is the only shape a production graph ever contains.
func TestResolveK8sComponentForService_RefresherVocabulary(t *testing.T) {
	f := newK8sResolverFixture(t)
	f.addComponent(t, "k8s-prod", store.ComponentTypeKubernetes)

	svcID := "k8s/k8s-prod/service/default/checkout"
	deployID := "k8s/k8s-prod/deployment/default/checkout"
	f.addNode(t, svcID, "service", "k8s-prod")
	f.addNode(t, deployID, "deployment", "k8s-prod")
	f.addEdge(t, svcID, deployID, "routes_to")

	componentID, err := f.resolve(t, "checkout")
	if err != nil {
		t.Fatalf("resolve: unexpected error: %v", err)
	}
	if componentID != "k8s-prod" {
		t.Errorf("resolved component = %q, want %q", componentID, "k8s-prod")
	}
}

// TestResolveK8sComponentForService_NoKubernetesComponent covers the inverse: a
// service related only to non-kubernetes components resolves to an error, and the
// error is the honest one. The pre-fix message told the operator the service had no
// k8s nodes in the graph, which was a lie in the exact case that mattered — nodes
// were present and the resolver simply could not recognize them.
func TestResolveK8sComponentForService_NoKubernetesComponent(t *testing.T) {
	f := newK8sResolverFixture(t)
	f.addComponent(t, "prom-prod", store.ComponentTypePrometheus)

	svcID := "prom/prom-prod/service/checkout"
	targetID := "prom/prom-prod/target/checkout"
	f.addNode(t, svcID, "service", "prom-prod")
	f.addNode(t, targetID, "target", "prom-prod")
	f.addEdge(t, svcID, targetID, graph.RelationMetricsIn)

	componentID, err := f.resolve(t, "checkout")
	if err == nil {
		t.Fatalf("resolve: expected an error, got component %q", componentID)
	}
	if got := err.Error(); strings.Contains(got, "has k8s nodes") {
		t.Errorf("error message still claims the service lacks k8s nodes: %q", got)
	}
	if got := err.Error(); !strings.Contains(got, "no kubernetes component found") {
		t.Errorf("error message = %q, want it to report no kubernetes component found", got)
	}
}

// TestResolveK8sComponentForService_KubernetesWinsOverObservability stages a
// service whose subgraph spans two components — an observability one and a
// kubernetes one — and pins that the kubernetes component is the one resolved.
//
// The jaeger node is deliberately named so it sorts BEFORE the kubernetes nodes:
// the subgraph walk yields nodes ordered by node ID (graph/sqlite.go's Related
// does ORDER BY n.id), so the observability component is the first one the
// resolver encounters. That gives the test teeth against the obvious wrong fix —
// returning the first node with any non-empty ComponentID — which would resolve
// jaeger-prod here and send the k8s query to a tracing backend.
func TestResolveK8sComponentForService_KubernetesWinsOverObservability(t *testing.T) {
	f := newK8sResolverFixture(t)
	f.addComponent(t, "jaeger-prod", store.ComponentTypeJaeger)
	f.addComponent(t, "k8s-prod", store.ComponentTypeKubernetes)

	svcID := "k8s/k8s-prod/service/default/checkout"
	deployID := "k8s/k8s-prod/deployment/default/checkout"
	jaegerID := "jaeger/jaeger-prod/service/checkout"
	f.addNode(t, svcID, "service", "k8s-prod")
	f.addNode(t, deployID, "deployment", "k8s-prod")
	f.addNode(t, jaegerID, "service", "jaeger-prod")
	f.addEdge(t, svcID, deployID, "routes_to")
	f.addEdge(t, svcID, jaegerID, graph.RelationTracesIn)

	componentID, err := f.resolve(t, "checkout")
	if err != nil {
		t.Fatalf("resolve: unexpected error: %v", err)
	}
	if componentID != "k8s-prod" {
		t.Errorf("resolved component = %q, want the kubernetes component %q", componentID, "k8s-prod")
	}
}
