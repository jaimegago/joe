package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/graph"
)

// Component deletion cascades graph state transactionally: deleting a component
// removes every graph_nodes row carrying its component_id in the SAME transaction
// as the components row and the audit insert, and graph_edges dies with its
// endpoint via the migration-002 FK ON DELETE CASCADE. Cross-component edges
// dying with their dead endpoint is correct — a per-component reconcile never
// manages them (it requires both endpoints in the loaded node set), so
// endpoint-node death is their only cleanup. See
// docs/backlog/component-delete-graph-orphans.md.
//
// These tests exercise the real handler-to-store chain (DELETE
// /api/v1/components/{id} -> handleDeleteComponent -> mutateWithAudit ->
// Graph.DeleteNodesByComponentTx + Components.DeleteTx), never a hand-rolled
// delete. TestDeleteComponent_CascadesGraphState was the pre-fix orphan
// break-test, inverted into the post-fix regression pin; TestDeleteComponent_
// CascadeRollback pins the transactionality (a mid-transaction failure reverts
// the graph delete along with everything else).

// seedGraphNode writes one node under componentID directly through the production
// graph store (the same *sql.DB the delete transaction spans, on a pooled
// auto-commit connection — so the seed persists independently of the later tx).
func seedGraphNode(t *testing.T, f *llmadminFixture, id, componentID string) {
	t.Helper()
	if err := f.services.Graph.AddNode(context.Background(), graph.Node{
		ID:          id,
		Type:        "deployment",
		ComponentID: componentID,
	}); err != nil {
		t.Fatalf("seed graph node %q: %v", id, err)
	}
}

// seedGraphEdge writes one edge through the production graph store. An edge's
// component scope is implicit in its endpoints — graph_edges has no component_id
// column (graph.Edge.ComponentID is never persisted), so an edge is "owned" by a
// component only insofar as both its endpoints are.
func seedGraphEdge(t *testing.T, f *llmadminFixture, from, to, relation string) {
	t.Helper()
	if err := f.services.Graph.AddEdge(context.Background(), graph.Edge{
		From:     from,
		To:       to,
		Relation: relation,
	}); err != nil {
		t.Fatalf("seed graph edge %s->%s (%s): %v", from, to, relation, err)
	}
}

// graphNodeCountByComponent counts graph_nodes rows carrying componentID.
func graphNodeCountByComponent(t *testing.T, f *llmadminFixture, componentID string) int {
	t.Helper()
	var n int
	if err := f.store.DB().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM graph_nodes WHERE component_id = ?`, componentID).Scan(&n); err != nil {
		t.Fatalf("count graph_nodes for %q: %v", componentID, err)
	}
	return n
}

// graphEdgeExists reports whether the (from,to,relation) edge is present.
func graphEdgeExists(t *testing.T, f *llmadminFixture, from, to, relation string) bool {
	t.Helper()
	var n int
	if err := f.store.DB().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM graph_edges WHERE from_node = ? AND to_node = ? AND relation = ?`,
		from, to, relation).Scan(&n); err != nil {
		t.Fatalf("probe edge %s->%s (%s): %v", from, to, relation, err)
	}
	return n > 0
}

// seedTwoComponentGraph registers two components and lays a small graph over
// them:
//   - del-a: nodes a1, a2; intra-component edge a1->a2
//   - keep-b: nodes b1, b2; self-owned edge b1->b2
//   - cross-component edge a2->b1 (del-a -> keep-b)
//
// It marks user:alice admin and asserts the seed took before returning.
func seedTwoComponentGraph(t *testing.T, f *llmadminFixture) {
	t.Helper()
	f.services.Adapters = adapters.NewRegistry()
	f.markAdmin("user:alice")
	seedComponent(t, f, "del-a")
	seedComponent(t, f, "keep-b")
	seedGraphNode(t, f, "a1", "del-a")
	seedGraphNode(t, f, "a2", "del-a")
	seedGraphNode(t, f, "b1", "keep-b")
	seedGraphNode(t, f, "b2", "keep-b")
	seedGraphEdge(t, f, "a1", "a2", "exposes")    // intra-component (both under del-a)
	seedGraphEdge(t, f, "b1", "b2", "exposes")    // self-owned by keep-b (must survive)
	seedGraphEdge(t, f, "a2", "b1", "metrics_in") // cross-component (del-a -> keep-b)

	if got := graphNodeCountByComponent(t, f, "del-a"); got != 2 {
		t.Fatalf("seed: del-a node count = %d, want 2", got)
	}
	if got := graphNodeCountByComponent(t, f, "keep-b"); got != 2 {
		t.Fatalf("seed: keep-b node count = %d, want 2", got)
	}
	for _, e := range [][3]string{{"a1", "a2", "exposes"}, {"b1", "b2", "exposes"}, {"a2", "b1", "metrics_in"}} {
		if !graphEdgeExists(t, f, e[0], e[1], e[2]) {
			t.Fatalf("seed: edge %s->%s (%s) missing", e[0], e[1], e[2])
		}
	}
}

// TestDeleteComponent_CascadesGraphState is the regression pin (the inverted
// orphan break-test): after an admin deletes del-a through the real handler,
// zero graph_nodes rows remain under its id, its intra-component edge is gone,
// the cross-component edge is gone (its del-a endpoint died), and the surviving
// component keep-b keeps both its nodes and its self-owned edge.
func TestDeleteComponent_CascadesGraphState(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	seedTwoComponentGraph(t, f)

	w := f.do(http.MethodDelete, "/api/v1/components/del-a", "", "user:alice")
	if w.Code != http.StatusNoContent {
		t.Fatalf("admin delete: status=%d body=%s; want 204", w.Code, w.Body.String())
	}
	if got := componentCount(t, f, "del-a"); got != 0 {
		t.Fatalf("component del-a still present after delete (count=%d)", got)
	}

	// The deleted component's graph rows are gone — cascaded, not orphaned.
	if got := graphNodeCountByComponent(t, f, "del-a"); got != 0 {
		t.Errorf("del-a graph nodes = %d, want 0 (cascade must remove them)", got)
	}
	if graphEdgeExists(t, f, "a1", "a2", "exposes") {
		t.Error("intra-component edge a1->a2 survived; must die with its endpoint nodes")
	}
	if graphEdgeExists(t, f, "a2", "b1", "metrics_in") {
		t.Error("cross-component edge a2->b1 survived; must die with its del-a endpoint via FK cascade")
	}

	// The surviving component is fully untouched — its nodes and its self-owned
	// edge remain.
	if got := graphNodeCountByComponent(t, f, "keep-b"); got != 2 {
		t.Errorf("keep-b graph nodes = %d, want 2 (surviving component untouched)", got)
	}
	if !graphEdgeExists(t, f, "b1", "b2", "exposes") {
		t.Error("keep-b self-owned edge b1->b2 removed; a foreign component's delete must not touch it")
	}
}

// TestDeleteComponent_CascadeRollback pins transactionality: forcing a failure
// inside the delete transaction AFTER the graph delete and BEFORE commit (the
// audit InsertTx, the last step before commit) must roll the ENTIRE transaction
// back — the components row, the deleted component's graph_nodes rows, its
// graph_edges (which the FK cascade removed on the tx connection), and any audit
// row all unchanged. This proves the graph cascade is genuinely inside the same
// transaction as the component delete and the audit insert, not a separate write
// that could commit on its own.
func TestDeleteComponent_CascadeRollback(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	seedTwoComponentGraph(t, f)
	f.breakAudit() // the in-transaction audit insert now fails -> mutateWithAudit rolls back

	w := f.do(http.MethodDelete, "/api/v1/components/del-a", "", "user:alice")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("delete with failing audit: status=%d body=%s; want 500 (fail-closed rollback)", w.Code, w.Body.String())
	}

	// Everything the transaction touched is intact.
	if got := componentCount(t, f, "del-a"); got != 1 {
		t.Errorf("del-a component count = %d after rollback; want 1 (the delete must not commit)", got)
	}
	if got := graphNodeCountByComponent(t, f, "del-a"); got != 2 {
		t.Errorf("del-a graph nodes = %d after rollback; want 2 (the graph delete must roll back with the tx)", got)
	}
	if !graphEdgeExists(t, f, "a1", "a2", "exposes") {
		t.Error("intra-component edge a1->a2 gone after rollback; the FK cascade must revert with the tx")
	}
	if !graphEdgeExists(t, f, "a2", "b1", "metrics_in") {
		t.Error("cross-component edge a2->b1 gone after rollback; the FK cascade must revert with the tx")
	}
	if n := f.countAudit(audit.ActionComponentDelete); n != 0 {
		t.Errorf("delete audit rows = %d after rollback; want 0 (no row commits when the tx rolls back)", n)
	}

	// The surviving component is likewise untouched.
	if got := graphNodeCountByComponent(t, f, "keep-b"); got != 2 {
		t.Errorf("keep-b graph nodes = %d after rollback; want 2", got)
	}
}
