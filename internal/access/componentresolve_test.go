package access_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/access"
	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/rbac"
)

// bindingGraph is a graph store that answers ListComponentBindings from a
// per-component table and records which components were asked about. Every
// other method is a no-op: ComponentBindings must reach the graph store through
// exactly one call per permitted component, and a fake that answered more
// broadly would hide a widening of that read.
type bindingGraph struct {
	byComponent map[string][]graph.ComponentBinding
	asked       []string
}

func (g *bindingGraph) ListComponentBindings(_ context.Context, componentID string, limit int) ([]graph.ComponentBinding, error) {
	g.asked = append(g.asked, componentID)
	rows := g.byComponent[componentID]
	if limit > 0 && len(rows) > limit {
		return rows[:limit], nil
	}
	return rows, nil
}

func (g *bindingGraph) AddNode(context.Context, graph.Node) error            { return nil }
func (g *bindingGraph) AddEdge(context.Context, graph.Edge) error            { return nil }
func (g *bindingGraph) GetNode(context.Context, string) (*graph.Node, error) { return nil, nil }
func (g *bindingGraph) Query(context.Context, string) ([]graph.Node, error)  { return nil, nil }
func (g *bindingGraph) Related(context.Context, string, int) (*graph.Subgraph, error) {
	return &graph.Subgraph{}, nil
}
func (g *bindingGraph) Path(context.Context, string, string) ([]graph.Edge, error) { return nil, nil }
func (g *bindingGraph) DeleteNode(context.Context, string) error                   { return nil }
func (g *bindingGraph) DeleteEdge(context.Context, string, string, string) error   { return nil }
func (g *bindingGraph) Summary(context.Context) (graph.GraphSummary, error) {
	return graph.GraphSummary{}, nil
}
func (g *bindingGraph) ListNodesByComponent(context.Context, string) ([]graph.Node, error) {
	return nil, nil
}
func (g *bindingGraph) ListEdgesForNodes(context.Context, []string) ([]graph.Edge, error) {
	return nil, nil
}
func (g *bindingGraph) ListAll(context.Context) (*graph.Subgraph, error) {
	return &graph.Subgraph{}, nil
}
func (g *bindingGraph) DeleteNodesByComponentTx(context.Context, *sql.Tx, string) error { return nil }

// binding builds one outbound binding from a component's node to a peer.
func binding(nodeID, relation, peerComponent string) graph.ComponentBinding {
	return graph.ComponentBinding{
		NodeID:          nodeID,
		NodeType:        "service",
		Relation:        relation,
		Direction:       graph.BindingOut,
		Confidence:      graph.Explicit,
		PeerNodeID:      peerComponent + ":root",
		PeerNodeType:    "metrics_backend",
		PeerComponentID: peerComponent,
	}
}

// resolveFixture wires an accessor over a policy engine that grants `allowed`
// the z-read zone, plus a recording audit repository. Components named in
// granted are assigned to z-read; every other component is left unassigned,
// which resolves to the "unassigned" zone that `allowed` holds no policy for —
// so it is denied.
func resolveFixture(t *testing.T, g graph.GraphStore, granted ...string) (*access.Accessor, *recordingAudit) {
	t.Helper()
	repo := newFakeRepo()
	repo.grant(allowed, "z-read")
	for _, id := range granted {
		repo.assign(id, "z-read")
	}
	rec := &recordingAudit{}
	return access.New(adapters.NewRegistry(), g, rbac.NewPolicyEngine(repo), rec), rec
}

// TestComponentBindings_DeniedCandidateIsNotACandidate is the load-bearing
// governance assertion: a component the principal may not read contributes
// nothing. It is not returned with empty evidence, it is not returned with a
// denial marker — it is absent, and absent in the same way a component that
// does not exist is absent.
func TestComponentBindings_DeniedCandidateIsNotACandidate(t *testing.T) {
	g := &bindingGraph{byComponent: map[string][]graph.ComponentBinding{
		"c-visible": {binding("svc:a", graph.RelationMetricsIn, "c-prom")},
		"c-hidden":  {binding("svc:b", graph.RelationMetricsIn, "c-prom")},
	}}
	acc, _ := resolveFixture(t, g, "c-visible", "c-prom")

	sets, err := acc.ComponentBindings(context.Background(), allowed,
		[]string{"c-visible", "c-hidden"}, 0)
	if err != nil {
		t.Fatalf("ComponentBindings: %v", err)
	}
	if len(sets) != 1 || sets[0].ComponentID != "c-visible" {
		t.Fatalf("got %d sets (%v), want only c-visible", len(sets), setIDs(sets))
	}

	// The denied component's graph context is never read at all: permit runs
	// before the store, so a denial costs no infrastructure access.
	for _, asked := range g.asked {
		if asked == "c-hidden" {
			t.Error("the graph store was read for a component the principal may not see")
		}
	}
}

// TestComponentBindings_DeniedPeerIsDroppedFromEvidence closes the side channel
// the candidate filter alone would leave open: a candidate the principal MAY
// read must not disclose, through its evidence, the existence of a peer
// component they may not.
func TestComponentBindings_DeniedPeerIsDroppedFromEvidence(t *testing.T) {
	g := &bindingGraph{byComponent: map[string][]graph.ComponentBinding{
		"c-visible": {
			binding("svc:a", graph.RelationMetricsIn, "c-prom"),
			binding("svc:a", graph.RelationLogsIn, "c-secret-loki"),
		},
	}}
	acc, _ := resolveFixture(t, g, "c-visible", "c-prom")

	sets, err := acc.ComponentBindings(context.Background(), allowed, []string{"c-visible"}, 0)
	if err != nil {
		t.Fatalf("ComponentBindings: %v", err)
	}
	if len(sets) != 1 {
		t.Fatalf("got %d sets, want 1", len(sets))
	}
	if len(sets[0].Bindings) != 1 {
		t.Fatalf("got %d bindings (%v), want only the permitted peer",
			len(sets[0].Bindings), sets[0].Bindings)
	}
	if got := sets[0].Bindings[0].PeerComponentID; got != "c-prom" {
		t.Errorf("surviving binding names peer %q, want c-prom", got)
	}
	for _, b := range sets[0].Bindings {
		if b.PeerComponentID == "c-secret-loki" {
			t.Error("a binding disclosed a peer component the principal may not read")
		}
	}
}

// TestComponentBindings_PermitIsTakenOncePerComponent asserts the memoisation.
// Two candidates commonly share a backend; a permit per EDGE would write an
// audit row per edge and turn one resolve call into an unreadable burst of
// identical decisions.
func TestComponentBindings_PermitIsTakenOncePerComponent(t *testing.T) {
	g := &bindingGraph{byComponent: map[string][]graph.ComponentBinding{
		"c-one": {
			binding("svc:a", graph.RelationMetricsIn, "c-prom"),
			binding("svc:a", graph.RelationAlertsIn, "c-prom"),
		},
		"c-two": {binding("svc:b", graph.RelationMetricsIn, "c-prom")},
	}}
	acc, rec := resolveFixture(t, g, "c-one", "c-two", "c-prom")

	if _, err := acc.ComponentBindings(context.Background(), allowed, []string{"c-one", "c-two"}, 0); err != nil {
		t.Fatalf("ComponentBindings: %v", err)
	}

	perComponent := map[string]int{}
	for _, row := range rec.snapshot() {
		if row.ComponentID != "" {
			perComponent[row.ComponentID]++
		}
	}
	for component, n := range perComponent {
		if n != 1 {
			t.Errorf("component %s took %d permits in one call, want exactly 1", component, n)
		}
	}
	if perComponent["c-prom"] != 1 {
		t.Errorf("the shared peer took %d permits, want 1 across three bindings", perComponent["c-prom"])
	}
}

// TestComponentBindings_TruncationIsDetectedByOverfetch asserts the limit
// contract: the accessor asks for limit+1 and reports Truncated, so the store
// stays a plain ordered read with no truncation flag of its own.
func TestComponentBindings_TruncationIsDetectedByOverfetch(t *testing.T) {
	rows := []graph.ComponentBinding{
		binding("svc:a", graph.RelationMetricsIn, "c-prom"),
		binding("svc:a", graph.RelationLogsIn, "c-prom"),
		binding("svc:a", graph.RelationTracesIn, "c-prom"),
	}
	g := &bindingGraph{byComponent: map[string][]graph.ComponentBinding{"c-one": rows}}
	acc, _ := resolveFixture(t, g, "c-one", "c-prom")

	sets, err := acc.ComponentBindings(context.Background(), allowed, []string{"c-one"}, 2)
	if err != nil {
		t.Fatalf("ComponentBindings: %v", err)
	}
	if len(sets) != 1 {
		t.Fatalf("got %d sets, want 1", len(sets))
	}
	if !sets[0].Truncated {
		t.Error("a component with more bindings than the limit must report Truncated")
	}
	if len(sets[0].Bindings) != 2 {
		t.Errorf("got %d bindings, want the limit of 2", len(sets[0].Bindings))
	}

	full, err := acc.ComponentBindings(context.Background(), allowed, []string{"c-one"}, 10)
	if err != nil {
		t.Fatalf("ComponentBindings: %v", err)
	}
	if full[0].Truncated {
		t.Error("a component inside the limit must not report Truncated")
	}
}

// TestComponentBindings_AuditRecordsWhichEmptyCaseOccurred is the assertion the
// unhappy-path decision rests on. The CALLER cannot tell "nothing matched" from
// "nothing you may see matched" — both are an empty result. The OPERATOR can,
// because the two calls leave different rows.
func TestComponentBindings_AuditRecordsWhichEmptyCaseOccurred(t *testing.T) {
	g := &bindingGraph{byComponent: map[string][]graph.ComponentBinding{
		"c-hidden": {binding("svc:b", graph.RelationMetricsIn, "c-prom")},
	}}

	t.Run("nothing matched", func(t *testing.T) {
		acc, rec := resolveFixture(t, g)
		sets, err := acc.ComponentBindings(context.Background(), allowed, nil, 0)
		if err != nil {
			t.Fatalf("ComponentBindings: %v", err)
		}
		if len(sets) != 0 {
			t.Fatalf("got %d sets, want none", len(sets))
		}
		row := outcomeRow(t, rec)
		if row.Reason != access.ReasonComponentResolveNoMatch {
			t.Errorf("outcome reason = %q, want %q", row.Reason, access.ReasonComponentResolveNoMatch)
		}
		assertOutcomeCounts(t, row, 0, 0)
	})

	t.Run("matched but not permitted", func(t *testing.T) {
		acc, rec := resolveFixture(t, g) // c-hidden is granted to nobody
		sets, err := acc.ComponentBindings(context.Background(), allowed, []string{"c-hidden"}, 0)
		if err != nil {
			t.Fatalf("ComponentBindings: %v", err)
		}
		if len(sets) != 0 {
			t.Fatalf("got %d sets, want none", len(sets))
		}
		row := outcomeRow(t, rec)
		if row.Reason != access.ReasonComponentResolveNoPermittedMatch {
			t.Errorf("outcome reason = %q, want %q", row.Reason, access.ReasonComponentResolveNoPermittedMatch)
		}
		assertOutcomeCounts(t, row, 1, 0)

		// And the withheld component is named, on its own deny row — this is
		// what an operator debugging a missing grant actually acts on.
		var denied bool
		for _, r := range rec.snapshot() {
			if r.ComponentID == "c-hidden" && r.Decision == audit.DecisionDeny {
				denied = true
			}
		}
		if !denied {
			t.Error("no deny row names the withheld component; the operator has nothing to act on")
		}
	})

	t.Run("permitted match", func(t *testing.T) {
		acc, rec := resolveFixture(t, g, "c-hidden", "c-prom")
		if _, err := acc.ComponentBindings(context.Background(), allowed, []string{"c-hidden"}, 0); err != nil {
			t.Fatalf("ComponentBindings: %v", err)
		}
		row := outcomeRow(t, rec)
		if row.Reason != access.ReasonComponentResolveMatch {
			t.Errorf("outcome reason = %q, want %q", row.Reason, access.ReasonComponentResolveMatch)
		}
		assertOutcomeCounts(t, row, 1, 1)
	})
}

// TestComponentBindings_AuditOutcomeCarriesNoPhrase pins the hygiene decision:
// the outcome row carries counts, never the task prose that produced them. The
// audit log is the governance trail, not a query log.
func TestComponentBindings_AuditOutcomeCarriesNoPhrase(t *testing.T) {
	g := &bindingGraph{byComponent: map[string][]graph.ComponentBinding{}}
	acc, rec := resolveFixture(t, g)

	if _, err := acc.ComponentBindings(context.Background(), allowed, nil, 0); err != nil {
		t.Fatalf("ComponentBindings: %v", err)
	}

	row := outcomeRow(t, rec)
	var blob map[string]any
	if err := json.Unmarshal([]byte(row.Context), &blob); err != nil {
		t.Fatalf("outcome context is not JSON: %v (%q)", err, row.Context)
	}
	for key := range blob {
		switch key {
		case "subkind", "matched", "permitted":
		default:
			t.Errorf("outcome context carries unexpected key %q — counts only, no query text", key)
		}
	}
	if row.ComponentID != "" {
		t.Errorf("outcome row names component %q; it is a per-call row, not a per-component decision", row.ComponentID)
	}
}

// TestComponentBindings_EmptyIsNeverAnError restates the contract at the
// accessor boundary: the error return is for a broken graph store, never for an
// answer the caller did not like.
func TestComponentBindings_EmptyIsNeverAnError(t *testing.T) {
	g := &bindingGraph{byComponent: map[string][]graph.ComponentBinding{}}
	acc, _ := resolveFixture(t, g, "c-known")

	sets, err := acc.ComponentBindings(context.Background(), allowed, []string{"c-known"}, 0)
	if err != nil {
		t.Fatalf("a component with no bindings must be a normal answer, got error: %v", err)
	}
	if len(sets) != 1 {
		t.Fatalf("got %d sets, want the component itself with empty evidence", len(sets))
	}
	if len(sets[0].Bindings) != 0 {
		t.Errorf("got %d bindings, want none", len(sets[0].Bindings))
	}
}

// TestComponentBindings_GraphUnavailableIsAnError separates the two: no graph
// store wired is a broken deployment, and it must not present as "this
// component has no relations".
func TestComponentBindings_GraphUnavailableIsAnError(t *testing.T) {
	acc, _ := resolveFixture(t, nil, "c-known")

	if _, err := acc.ComponentBindings(context.Background(), allowed, []string{"c-known"}, 0); err == nil {
		t.Fatal("a missing graph store must be an error, not an empty answer")
	} else if !strings.Contains(err.Error(), "graph store not available") {
		t.Errorf("unexpected error for a missing graph store: %v", err)
	}
}

// --- helpers ---

func setIDs(sets []access.ComponentBindingSet) []string {
	out := make([]string, 0, len(sets))
	for _, s := range sets {
		out = append(out, s.ComponentID)
	}
	return out
}

// outcomeRow returns the single per-call resolve-outcome row, failing if there
// is not exactly one — one call, one outcome row is the contract.
func outcomeRow(t *testing.T, rec *recordingAudit) audit.Event {
	t.Helper()
	var found []audit.Event
	for _, row := range rec.snapshot() {
		if strings.HasPrefix(row.Reason, "component_resolve_") {
			found = append(found, row)
		}
	}
	if len(found) != 1 {
		t.Fatalf("found %d resolve-outcome rows, want exactly 1", len(found))
	}
	if found[0].Kind != audit.KindInfraAccess {
		t.Errorf("outcome row kind = %q, want %q", found[0].Kind, audit.KindInfraAccess)
	}
	return found[0]
}

func assertOutcomeCounts(t *testing.T, row audit.Event, matched, permitted int) {
	t.Helper()
	var blob struct {
		Subkind   string `json:"subkind"`
		Matched   int    `json:"matched"`
		Permitted int    `json:"permitted"`
	}
	if err := json.Unmarshal([]byte(row.Context), &blob); err != nil {
		t.Fatalf("outcome context is not JSON: %v (%q)", err, row.Context)
	}
	if blob.Subkind != "component_resolve" {
		t.Errorf("outcome subkind = %q, want component_resolve", blob.Subkind)
	}
	if blob.Matched != matched || blob.Permitted != permitted {
		t.Errorf("outcome counts = matched %d / permitted %d, want %d / %d",
			blob.Matched, blob.Permitted, matched, permitted)
	}
}
