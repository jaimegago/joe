package coreagent

// Tests for error-return branches that require a failing graph store.

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
)

// ============================================================
// errorGraphStore: a mock graph.GraphStore that returns errors
// on demand, delegating to a real store for everything else.
// ============================================================

type errorGraphStore struct {
	addNodeErr error
	addEdgeErr error
	delEdgeErr error
	delNodeErr error
	queryErr   error
	underlying graph.GraphStore
}

func (e *errorGraphStore) AddNode(ctx context.Context, node graph.Node) error {
	if e.addNodeErr != nil {
		return e.addNodeErr
	}
	if e.underlying != nil {
		return e.underlying.AddNode(ctx, node)
	}
	return nil
}
func (e *errorGraphStore) AddEdge(ctx context.Context, edge graph.Edge) error {
	if e.addEdgeErr != nil {
		return e.addEdgeErr
	}
	if e.underlying != nil {
		return e.underlying.AddEdge(ctx, edge)
	}
	return nil
}
func (e *errorGraphStore) GetNode(ctx context.Context, id string) (*graph.Node, error) {
	if e.underlying != nil {
		return e.underlying.GetNode(ctx, id)
	}
	return nil, graph.ErrNodeNotFound
}
func (e *errorGraphStore) Query(ctx context.Context, query string) ([]graph.Node, error) {
	if e.queryErr != nil {
		return nil, e.queryErr
	}
	if e.underlying != nil {
		return e.underlying.Query(ctx, query)
	}
	return nil, nil
}
func (e *errorGraphStore) Related(ctx context.Context, nodeID string, depth int) (*graph.Subgraph, error) {
	if e.underlying != nil {
		return e.underlying.Related(ctx, nodeID, depth)
	}
	return &graph.Subgraph{}, nil
}
func (e *errorGraphStore) Path(ctx context.Context, from, to string) ([]graph.Edge, error) {
	if e.underlying != nil {
		return e.underlying.Path(ctx, from, to)
	}
	return nil, nil
}
func (e *errorGraphStore) DeleteNode(ctx context.Context, id string) error {
	if e.delNodeErr != nil {
		return e.delNodeErr
	}
	if e.underlying != nil {
		return e.underlying.DeleteNode(ctx, id)
	}
	return nil
}
func (e *errorGraphStore) DeleteEdge(ctx context.Context, from, to, relation string) error {
	if e.delEdgeErr != nil {
		return e.delEdgeErr
	}
	if e.underlying != nil {
		return e.underlying.DeleteEdge(ctx, from, to, relation)
	}
	return nil
}
func (e *errorGraphStore) Summary(ctx context.Context) (graph.GraphSummary, error) {
	if e.underlying != nil {
		return e.underlying.Summary(ctx)
	}
	return graph.GraphSummary{}, nil
}
func (e *errorGraphStore) ListNodesByComponent(ctx context.Context, sourceID string) ([]graph.Node, error) {
	if e.underlying != nil {
		return e.underlying.ListNodesByComponent(ctx, sourceID)
	}
	return nil, nil
}
func (e *errorGraphStore) ListEdgesForNodes(ctx context.Context, nodeIDs []string) ([]graph.Edge, error) {
	if e.underlying != nil {
		return e.underlying.ListEdgesForNodes(ctx, nodeIDs)
	}
	return nil, nil
}
func (e *errorGraphStore) ListAll(ctx context.Context) (*graph.Subgraph, error) {
	if e.underlying != nil {
		return e.underlying.ListAll(ctx)
	}
	return &graph.Subgraph{}, nil
}
func (e *errorGraphStore) DeleteNodesByComponentTx(ctx context.Context, tx *sql.Tx, componentID string) error {
	if e.underlying != nil {
		return e.underlying.DeleteNodesByComponentTx(ctx, tx, componentID)
	}
	return nil
}

// makeErrRefresher creates a Refresher backed by an errorGraphStore.
func makeErrRefresher(t *testing.T, gs graph.GraphStore) *Refresher {
	t.Helper()
	return &Refresher{
		services: &core.Services{Graph: gs},
		logger:   slog.Default(),
	}
}

// ============================================================
// ApplyGraphDelta error branch tests
// ============================================================

func TestApplyGraphDelta_AddNodeError(t *testing.T) {
	gs := &errorGraphStore{addNodeErr: errors.New("disk full")}
	delta := GraphDelta{
		NodesToUpsert: []graph.Node{{ID: "n1", Type: "test", Metadata: map[string]any{}}},
	}
	if err := ApplyGraphDelta(context.Background(), gs, delta); err == nil {
		t.Error("expected error when AddNode fails")
	}
}

func TestApplyGraphDelta_AddEdgeError(t *testing.T) {
	gs := &errorGraphStore{addEdgeErr: errors.New("constraint violation")}
	delta := GraphDelta{
		EdgesToUpsert: []graph.Edge{
			{From: "n1", To: "n2", Relation: "calls", CreatedAt: time.Now()},
		},
	}
	if err := ApplyGraphDelta(context.Background(), gs, delta); err == nil {
		t.Error("expected error when AddEdge fails")
	}
}

func TestApplyGraphDelta_DeleteEdgeError(t *testing.T) {
	gs := &errorGraphStore{delEdgeErr: errors.New("delete edge error")}
	delta := GraphDelta{
		EdgesToDelete: []graph.Edge{
			{From: "n1", To: "n2", Relation: "calls"},
		},
	}
	if err := ApplyGraphDelta(context.Background(), gs, delta); err == nil {
		t.Error("expected error when DeleteEdge fails")
	}
}

func TestApplyGraphDelta_DeleteNodeError(t *testing.T) {
	gs := &errorGraphStore{delNodeErr: errors.New("permission denied")}
	delta := GraphDelta{
		NodesToDelete: []graph.Node{{ID: "n1", Type: "test"}},
	}
	if err := ApplyGraphDelta(context.Background(), gs, delta); err == nil {
		t.Error("expected error when DeleteNode fails with non-ErrNodeNotFound")
	}
}

func TestApplyGraphDelta_DeleteNodeErrNotFound_IsSwallowed(t *testing.T) {
	gs := &errorGraphStore{delNodeErr: graph.ErrNodeNotFound}
	delta := GraphDelta{
		NodesToDelete: []graph.Node{{ID: "n1", Type: "test"}},
	}
	if err := ApplyGraphDelta(context.Background(), gs, delta); err != nil {
		t.Errorf("ErrNodeNotFound should be swallowed, got: %v", err)
	}
}

// ============================================================
// buildImageStoredInEdges: graph query error → returns empty
// ============================================================

func TestBuildImageStoredInEdges_QueryError(t *testing.T) {
	gs := &errorGraphStore{queryErr: errors.New("graph query failed")}
	r := makeErrRefresher(t, gs)
	src := &store.Component{ID: "src-img-err", Type: store.ComponentTypeOCIRegistry}

	edges := r.buildImageStoredInEdges(context.Background(), src, "repo-node", "payment", time.Now())
	if len(edges) != 0 {
		t.Errorf("want 0 edges on query error, got %d", len(edges))
	}
}

// ============================================================
// buildIngressForEdges: graph query error
// ============================================================

func TestBuildIngressForEdges_QueryError(t *testing.T) {
	gs := &errorGraphStore{queryErr: errors.New("graph query failed")}
	r := makeErrRefresher(t, gs)
	src := &store.Component{ID: "src-nginx-err", Type: store.ComponentTypeNginx}

	edges := r.buildIngressForEdges(context.Background(), src, "ing-node", "api-svc", "default", "api.example.com", time.Now())
	if len(edges) != 0 {
		t.Errorf("want 0 edges on query error, got %d", len(edges))
	}
}

// ============================================================
// buildProxiesEdges: graph query error
// ============================================================

func TestBuildProxiesEdges_QueryError(t *testing.T) {
	gs := &errorGraphStore{queryErr: errors.New("graph query failed")}
	r := makeErrRefresher(t, gs)
	src := &store.Component{ID: "src-envoy-err", Type: store.ComponentTypeEnvoy}

	edges := r.buildProxiesEdges(context.Background(), src, "envoy-node", "payment", "payment_80", time.Now())
	if len(edges) != 0 {
		t.Errorf("want 0 edges on query error, got %d", len(edges))
	}
}

// ============================================================
// buildManagedByEdges: graph query error
// ============================================================

func TestBuildManagedByEdges_QueryError(t *testing.T) {
	gs := &errorGraphStore{queryErr: errors.New("graph query failed")}
	r := makeErrRefresher(t, gs)
	src := &store.Component{ID: "src-argocd-err", Type: store.ComponentTypeArgoCd}

	edges := r.buildManagedByEdges(context.Background(), src, "manager-node", "payment", "default", time.Now())
	if len(edges) != 0 {
		t.Errorf("want 0 edges on query error, got %d", len(edges))
	}
}

// ============================================================
// buildProvidesEdges: graph query error
// ============================================================

func TestBuildProvidesEdges_QueryError(t *testing.T) {
	gs := &errorGraphStore{queryErr: errors.New("graph query failed")}
	r := makeErrRefresher(t, gs)
	src := &store.Component{ID: "src-tf-err", Type: store.ComponentTypeTerraform}

	edges := r.buildProvidesEdges(context.Background(), src, "tf-node", "web-server", "aws_instance", time.Now())
	if len(edges) != 0 {
		t.Errorf("want 0 edges on query error, got %d", len(edges))
	}
}

// ============================================================
// buildDDMetricsInEdges / buildDDLogsInEdges: query error paths
// ============================================================

func TestBuildDDMetricsInEdges_QueryError(t *testing.T) {
	gs := &errorGraphStore{queryErr: errors.New("graph query failed")}
	r := makeErrRefresher(t, gs)
	src := &store.Component{ID: "src-dd-err", Type: store.ComponentTypeDatadog}

	edges, err := r.buildDDMetricsInEdges(context.Background(), src, "dd-node", []string{"payment"}, time.Now())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("want 0 edges on query error, got %d", len(edges))
	}
}

func TestBuildDDLogsInEdges_QueryError(t *testing.T) {
	gs := &errorGraphStore{queryErr: errors.New("graph query failed")}
	r := makeErrRefresher(t, gs)
	src := &store.Component{ID: "src-dd-log-err", Type: store.ComponentTypeDatadog}

	edges, err := r.buildDDLogsInEdges(context.Background(), src, "dd-node", []string{"payment"}, time.Now())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("want 0 edges on query error, got %d", len(edges))
	}
}

func TestBuildDDLogsInEdges_SkipsNonServiceNodes2(t *testing.T) {
	realStore := setupGraphStore(t)
	ctx := context.Background()

	_ = realStore.AddNode(ctx, graph.Node{
		ID:       "k8snode/api-node",
		Type:     "k8s_node",
		Metadata: map[string]any{"name": "api-node"},
	})

	gs := &errorGraphStore{underlying: realStore}
	r := makeErrRefresher(t, gs)
	src := &store.Component{ID: "src-dd-log-skip", Type: store.ComponentTypeDatadog}

	edges, err := r.buildDDLogsInEdges(ctx, src, "dd-node", []string{"api-node"}, time.Now())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("want 0 edges for non-service node, got %d", len(edges))
	}
}

// ============================================================
// buildStoresInEdgesByName: query error path
// ============================================================

func TestBuildStoresInEdgesByName_QueryError(t *testing.T) {
	gs := &errorGraphStore{queryErr: errors.New("graph query failed")}
	r := makeErrRefresher(t, gs)
	src := &store.Component{ID: "src-ds-err", Name: "orders-db", Type: store.ComponentTypePostgreSQL}

	edges := r.buildStoresInEdgesByName(context.Background(), src, "ds-node", "stores_in", "postgres", time.Now())
	if len(edges) != 0 {
		t.Errorf("want 0 edges on query error, got %d", len(edges))
	}
}

// ============================================================
// buildQueuesInEdges: query error path
// ============================================================

func TestBuildQueuesInEdges_QueryError(t *testing.T) {
	gs := &errorGraphStore{queryErr: errors.New("graph query failed")}
	r := makeErrRefresher(t, gs)
	src := &store.Component{ID: "src-kafka-err", Type: store.ComponentTypeKafka}

	edges := r.buildQueuesInEdges(context.Background(), src, "kafka-node", []string{"orders"}, time.Now())
	if len(edges) != 0 {
		t.Errorf("want 0 edges on query error, got %d", len(edges))
	}
}
