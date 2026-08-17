package graph_test

import (
	"context"
	"testing"

	"github.com/jaimegago/joe/internal/graph"
)

// seedBindingGraph builds a small cross-component topology:
//
//	k8s/prod  svc:checkout ──metrics_in──▶ prom:root      (obs/prom)
//	k8s/prod  svc:checkout ──logs_in─────▶ loki:root      (obs/loki)
//	k8s/prod  svc:checkout ──calls───────▶ svc:payments   (k8s/prod — SAME component)
//	k8s/prod  svc:checkout ──managed_by──▶ orphan:node    (no component at all)
//	argocd/x  app:checkout ──manages─────▶ svc:checkout   (INBOUND to k8s/prod)
func seedBindingGraph(t *testing.T, store graph.GraphStore) {
	t.Helper()
	ctx := context.Background()

	nodes := []graph.Node{
		{ID: "svc:checkout", Type: "service", ComponentID: "k8s/prod"},
		{ID: "svc:payments", Type: "service", ComponentID: "k8s/prod"},
		{ID: "prom:root", Type: "metrics_backend", ComponentID: "obs/prom"},
		{ID: "loki:root", Type: "log_backend", ComponentID: "obs/loki"},
		{ID: "app:checkout", Type: "argocd_app", ComponentID: "argocd/x"},
		{ID: "orphan:node", Type: "unknown"}, // no ComponentID
	}
	for _, n := range nodes {
		if err := store.AddNode(ctx, n); err != nil {
			t.Fatalf("AddNode(%s): %v", n.ID, err)
		}
	}

	edges := []graph.Edge{
		{From: "svc:checkout", To: "prom:root", Relation: graph.RelationMetricsIn, Confidence: graph.Explicit, Source: "prom_targets"},
		{From: "svc:checkout", To: "loki:root", Relation: graph.RelationLogsIn, Confidence: graph.Inferred, Source: "loki_labels"},
		{From: "svc:checkout", To: "svc:payments", Relation: "calls", Confidence: graph.Explicit, Source: "k8s_api"},
		{From: "svc:checkout", To: "orphan:node", Relation: graph.RelationManagedBy, Confidence: graph.Inferred, Source: "heuristic"},
		{From: "app:checkout", To: "svc:checkout", Relation: graph.RelationManagedBy, Confidence: graph.Explicit, Source: "argocd"},
	}
	for _, e := range edges {
		if err := store.AddEdge(ctx, e); err != nil {
			t.Fatalf("AddEdge(%s->%s): %v", e.From, e.To, err)
		}
	}
}

// TestListComponentBindings_ReturnsCrossComponentEdgesOnly is the shape of the
// answer: what a component is bound TO. An edge inside the component binds it
// to itself and says nothing about what it is attached to; an edge to a node
// carrying no component attribution cannot be authorized per component, so
// returning it would disclose a node no grant covers.
func TestListComponentBindings_ReturnsCrossComponentEdgesOnly(t *testing.T) {
	store := setupTestStore(t)
	seedBindingGraph(t, store)

	got, err := store.ListComponentBindings(context.Background(), "k8s/prod", 0)
	if err != nil {
		t.Fatalf("ListComponentBindings: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d bindings, want 3 (metrics_in, logs_in, inbound managed_by): %+v", len(got), got)
	}

	for _, b := range got {
		if b.PeerComponentID == "" {
			t.Errorf("binding %+v has an unattributed peer; those must not be returned", b)
		}
		if b.PeerComponentID == "k8s/prod" {
			t.Errorf("binding %+v is internal to the component; those are not bindings TO anything", b)
		}
		if b.Relation == "calls" {
			t.Errorf("the same-component 'calls' edge was returned: %+v", b)
		}
	}
}

// TestListComponentBindings_CarriesDirectionAndPeer asserts the fields a caller
// disambiguates on. Direction is load-bearing: these relations mean different
// things from each end, so a binding that dropped it would be ambiguous.
func TestListComponentBindings_CarriesDirectionAndPeer(t *testing.T) {
	store := setupTestStore(t)
	seedBindingGraph(t, store)

	got, err := store.ListComponentBindings(context.Background(), "k8s/prod", 0)
	if err != nil {
		t.Fatalf("ListComponentBindings: %v", err)
	}

	byRelation := map[string]graph.ComponentBinding{}
	for _, b := range got {
		byRelation[b.Relation] = b
	}

	metrics, ok := byRelation[graph.RelationMetricsIn]
	if !ok {
		t.Fatalf("no metrics_in binding in %+v", got)
	}
	if metrics.Direction != graph.BindingOut {
		t.Errorf("metrics_in direction = %q, want %q (the component's node is the from-node)",
			metrics.Direction, graph.BindingOut)
	}
	if metrics.NodeID != "svc:checkout" || metrics.NodeType != "service" {
		t.Errorf("metrics_in near endpoint = %s/%s, want svc:checkout/service", metrics.NodeID, metrics.NodeType)
	}
	if metrics.PeerComponentID != "obs/prom" || metrics.PeerNodeID != "prom:root" || metrics.PeerNodeType != "metrics_backend" {
		t.Errorf("metrics_in peer = %s %s/%s, want obs/prom prom:root/metrics_backend",
			metrics.PeerComponentID, metrics.PeerNodeID, metrics.PeerNodeType)
	}
	if metrics.Confidence != graph.Explicit {
		t.Errorf("metrics_in confidence = %v, want Explicit", metrics.Confidence)
	}

	managed, ok := byRelation[graph.RelationManagedBy]
	if !ok {
		t.Fatalf("no managed_by binding in %+v", got)
	}
	if managed.Direction != graph.BindingIn {
		t.Errorf("inbound managed_by direction = %q, want %q", managed.Direction, graph.BindingIn)
	}
	if managed.PeerComponentID != "argocd/x" {
		t.Errorf("inbound managed_by peer component = %q, want argocd/x", managed.PeerComponentID)
	}

	logs := byRelation[graph.RelationLogsIn]
	if logs.Confidence != graph.Inferred {
		t.Errorf("logs_in confidence = %v, want Inferred — a name-matched edge must not read as confirmed", logs.Confidence)
	}
}

// TestListComponentBindings_OrderingIsDeterministic pins that the same graph
// answers the same way every time. The accessor detects truncation by asking
// for one row past the limit, which is only meaningful if the row order is
// stable — an unstable order would make a truncated prefix arbitrary.
func TestListComponentBindings_OrderingIsDeterministic(t *testing.T) {
	store := setupTestStore(t)
	seedBindingGraph(t, store)
	ctx := context.Background()

	first, err := store.ListComponentBindings(ctx, "k8s/prod", 0)
	if err != nil {
		t.Fatalf("ListComponentBindings: %v", err)
	}
	for i := 0; i < 3; i++ {
		again, err := store.ListComponentBindings(ctx, "k8s/prod", 0)
		if err != nil {
			t.Fatalf("ListComponentBindings: %v", err)
		}
		if len(again) != len(first) {
			t.Fatalf("run %d returned %d bindings, first run returned %d", i, len(again), len(first))
		}
		for j := range again {
			if again[j] != first[j] {
				t.Fatalf("run %d differs at %d: %+v vs %+v", i, j, again[j], first[j])
			}
		}
	}

	// Ordered by relation first: logs_in < managed_by < metrics_in.
	wantRelations := []string{graph.RelationLogsIn, graph.RelationManagedBy, graph.RelationMetricsIn}
	for i, want := range wantRelations {
		if first[i].Relation != want {
			t.Errorf("binding %d relation = %q, want %q (ordering is relation-first)", i, first[i].Relation, want)
		}
	}
}

// TestListComponentBindings_LimitAndEmptyCases covers the bound and the two
// ways nothing comes back.
func TestListComponentBindings_LimitAndEmptyCases(t *testing.T) {
	store := setupTestStore(t)
	seedBindingGraph(t, store)
	ctx := context.Background()

	limited, err := store.ListComponentBindings(ctx, "k8s/prod", 2)
	if err != nil {
		t.Fatalf("ListComponentBindings: %v", err)
	}
	if len(limited) != 2 {
		t.Errorf("got %d bindings under a limit of 2, want 2", len(limited))
	}

	// A component with nodes but no cross-component edges.
	if err := store.AddNode(ctx, graph.Node{ID: "lonely:node", Type: "service", ComponentID: "k8s/lonely"}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if got, err := store.ListComponentBindings(ctx, "k8s/lonely", 0); err != nil || len(got) != 0 {
		t.Errorf("ListComponentBindings(k8s/lonely) = %+v, %v; want an empty answer and no error", got, err)
	}

	// A component that does not exist at all answers the same way — the store
	// draws no distinction, which is what lets the layer above refuse to.
	if got, err := store.ListComponentBindings(ctx, "k8s/nonexistent", 0); err != nil || len(got) != 0 {
		t.Errorf("ListComponentBindings(k8s/nonexistent) = %+v, %v; want an empty answer and no error", got, err)
	}

	// An empty component id is a caller error shaped as an empty answer rather
	// than a query that would scan for the empty string.
	if got, err := store.ListComponentBindings(ctx, "", 0); err != nil || len(got) != 0 {
		t.Errorf("ListComponentBindings(\"\") = %+v, %v; want an empty answer and no error", got, err)
	}
}

// TestConfidenceLevel_String pins the rendering the tool payload uses, including
// the documented ambiguity: a stored 3 renders as user_confirmed even though a
// legacy row may have meant Explicit. The method renders what is stored.
func TestConfidenceLevel_String(t *testing.T) {
	cases := map[graph.ConfidenceLevel]string{
		graph.Inferred:           "inferred",
		graph.Explicit:           "explicit",
		graph.UserConfirmed:      "user_confirmed",
		graph.ConfidenceLevel(0): "unknown",
	}
	for level, want := range cases {
		if got := level.String(); got != want {
			t.Errorf("ConfidenceLevel(%d).String() = %q, want %q", int(level), got, want)
		}
	}
}
