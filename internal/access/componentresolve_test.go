package access_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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

// resolve is the plain call every test that does not exercise a bound makes:
// these ids, production bounds.
func resolve(t *testing.T, acc *access.Accessor, ids ...string) access.ComponentResolveResult {
	t.Helper()
	return resolveWith(t, acc, access.ComponentResolveRequest{ComponentIDs: ids})
}

// resolveWith is the same call with the request spelled out, for the tests that
// lower a bound to reach it with a readable fixture.
func resolveWith(t *testing.T, acc *access.Accessor, req access.ComponentResolveRequest) access.ComponentResolveResult {
	t.Helper()
	res, err := acc.ComponentBindings(context.Background(), allowed, req)
	if err != nil {
		t.Fatalf("ComponentBindings: %v", err)
	}
	return res
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

	sets := resolve(t, acc, "c-visible", "c-hidden").Sets
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

	sets := resolve(t, acc, "c-visible").Sets
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

	resolve(t, acc, "c-one", "c-two")

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
// contract: the accessor over-fetches and reports Truncated, so the store stays
// a plain ordered read with no truncation flag of its own.
func TestComponentBindings_TruncationIsDetectedByOverfetch(t *testing.T) {
	rows := []graph.ComponentBinding{
		binding("svc:a", graph.RelationMetricsIn, "c-prom"),
		binding("svc:a", graph.RelationLogsIn, "c-prom"),
		binding("svc:a", graph.RelationTracesIn, "c-prom"),
	}
	g := &bindingGraph{byComponent: map[string][]graph.ComponentBinding{"c-one": rows}}
	acc, _ := resolveFixture(t, g, "c-one", "c-prom")

	sets := resolveWith(t, acc, access.ComponentResolveRequest{
		ComponentIDs: []string{"c-one"}, PerComponentLimit: 2,
	}).Sets
	if len(sets) != 1 {
		t.Fatalf("got %d sets, want 1", len(sets))
	}
	if !sets[0].Truncated {
		t.Error("a component with more visible bindings than the limit must report Truncated")
	}
	if len(sets[0].Bindings) != 2 {
		t.Errorf("got %d bindings, want the limit of 2", len(sets[0].Bindings))
	}

	full := resolveWith(t, acc, access.ComponentResolveRequest{
		ComponentIDs: []string{"c-one"}, PerComponentLimit: 10,
	}).Sets
	if full[0].Truncated {
		t.Error("a component inside the limit must not report Truncated")
	}
}

// TestComponentBindings_TruncationIsDerivedAfterThePeerFilter is the disclosure
// assertion, and it needs a principal who does NOT hold every peer — the
// condition under which the two counts can disagree at all.
//
// Truncated is read by the caller BESIDE a post-filter binding count. Derived
// from the raw row count, the pair is a cardinality channel on the governed
// side: "you were shown 1 of a prefix that was full" says at least limit-1 edges
// from a component you may read reach components you may not, which is the
// existence disclosure the peer filter exists to prevent. Derived from the
// visible rows, the pair says only how much of the caller's OWN evidence was
// shown.
func TestComponentBindings_TruncationIsDerivedAfterThePeerFilter(t *testing.T) {
	// Six edges, one visible peer. The old derivation capped the raw rows at
	// the limit, saw a full prefix, and reported Truncated beside a count of 1.
	rows := []graph.ComponentBinding{
		binding("svc:a", graph.RelationMetricsIn, "c-prom"),
		binding("svc:a", graph.RelationLogsIn, "c-secret-1"),
		binding("svc:a", graph.RelationLogsIn, "c-secret-2"),
		binding("svc:a", graph.RelationTracesIn, "c-secret-3"),
		binding("svc:a", graph.RelationTracesIn, "c-secret-4"),
		binding("svc:a", graph.RelationAlertsIn, "c-secret-5"),
	}
	g := &bindingGraph{byComponent: map[string][]graph.ComponentBinding{"c-one": rows}}
	acc, _ := resolveFixture(t, g, "c-one", "c-prom")

	sets := resolveWith(t, acc, access.ComponentResolveRequest{
		ComponentIDs: []string{"c-one"}, PerComponentLimit: 3,
	}).Sets
	if len(sets) != 1 {
		t.Fatalf("got %d sets, want 1", len(sets))
	}
	if n := len(sets[0].Bindings); n != 1 {
		t.Fatalf("got %d visible bindings, want only the permitted peer", n)
	}
	if sets[0].Truncated {
		t.Error("Truncated is set with 1 of 3 visible bindings returned: the flag is derived " +
			"before the peer filter, so it discloses how many edges reach components outside the grant")
	}
}

// TestComponentBindings_EvidenceIsDrawnPastTheDeniedPrefix is the mirror case,
// and it is the direction the accessor's own doc comment used to call honest:
// evidence the principal IS entitled to, withheld because the store's cut landed
// before the filter did.
//
// It needs a principal who does not hold every peer, and it needs the denied
// peers to sort FIRST — which the store's ordering (relation, peer component,
// peer node, near node, direction) makes an ordinary arrangement rather than a
// contrived one.
func TestComponentBindings_EvidenceIsDrawnPastTheDeniedPrefix(t *testing.T) {
	rows := []graph.ComponentBinding{
		binding("svc:a", graph.RelationAlertsIn, "c-secret-1"),
		binding("svc:a", graph.RelationAlertsIn, "c-secret-2"),
		binding("svc:a", graph.RelationLogsIn, "c-secret-3"),
		binding("svc:a", graph.RelationLogsIn, "c-secret-4"),
		binding("svc:a", graph.RelationMetricsIn, "c-prom"),
	}
	g := &bindingGraph{byComponent: map[string][]graph.ComponentBinding{"c-one": rows}}
	acc, _ := resolveFixture(t, g, "c-one", "c-prom")

	sets := resolveWith(t, acc, access.ComponentResolveRequest{
		ComponentIDs: []string{"c-one"}, PerComponentLimit: 2,
	}).Sets
	if len(sets) != 1 {
		t.Fatalf("got %d sets, want 1", len(sets))
	}
	if len(sets[0].Bindings) != 1 {
		t.Fatalf("got %d bindings (%v), want the permitted peer that sorts after the denied ones — "+
			"a bound spent before the filter withholds evidence the principal is entitled to",
			len(sets[0].Bindings), sets[0].Bindings)
	}
	if got := sets[0].Bindings[0].PeerComponentID; got != "c-prom" {
		t.Errorf("surviving binding names peer %q, want c-prom", got)
	}
}

// TestComponentBindings_CandidateBoundIsSpentOnPermittedComponents is the
// candidate half. The bound must count components the principal MAY SEE, so
// denied matches ranked above them cannot consume it — otherwise a principal
// whose own components sort below the cut receives the empty answer having never
// been evaluated.
func TestComponentBindings_CandidateBoundIsSpentOnPermittedComponents(t *testing.T) {
	byComponent := map[string][]graph.ComponentBinding{}
	var ids []string
	// Denied matches rank first, and there are more of them than the bound.
	for i := 0; i < access.MaxResolveCandidates+5; i++ {
		id := fmt.Sprintf("c-denied-%02d", i)
		ids = append(ids, id)
		byComponent[id] = []graph.ComponentBinding{binding("svc:d", graph.RelationMetricsIn, "c-prom")}
	}
	var granted []string
	for i := 0; i < access.MaxResolveCandidates+1; i++ {
		id := fmt.Sprintf("c-mine-%02d", i)
		ids = append(ids, id)
		granted = append(granted, id)
		byComponent[id] = []graph.ComponentBinding{binding("svc:m", graph.RelationMetricsIn, "c-prom")}
	}
	g := &bindingGraph{byComponent: byComponent}
	acc, _ := resolveFixture(t, g, append(granted, "c-prom")...)

	res := resolve(t, acc, ids...)
	if len(res.Sets) != access.MaxResolveCandidates {
		t.Fatalf("got %d candidates, want the bound of %d — denied matches ranked above them "+
			"must not consume the candidate budget", len(res.Sets), access.MaxResolveCandidates)
	}
	for i, s := range res.Sets {
		if want := fmt.Sprintf("c-mine-%02d", i); s.ComponentID != want {
			t.Fatalf("candidate %d = %q, want %q: the answer must be a prefix of the ranking "+
				"the principal is entitled to", i, s.ComponentID, want)
		}
	}
	if !res.CandidatesTruncated {
		t.Error("CandidatesTruncated must report that more components the principal may see matched")
	}
	if res.MaxCandidates != access.MaxResolveCandidates {
		t.Errorf("MaxCandidates = %d, want the bound in force (%d)", res.MaxCandidates, access.MaxResolveCandidates)
	}
}

// TestComponentBindings_TotalBindingBudgetIsBoundedAndReported closes the
// composition. A per-candidate bound and a candidate bound multiply, and a
// product that is neither bounded nor reported is not a contract — it is two
// numbers that happen to be small.
func TestComponentBindings_TotalBindingBudgetIsBoundedAndReported(t *testing.T) {
	byComponent := map[string][]graph.ComponentBinding{}
	var ids []string
	for i := 0; i < access.MaxResolveCandidates; i++ {
		id := fmt.Sprintf("c-%02d", i)
		ids = append(ids, id)
		var rows []graph.ComponentBinding
		for j := 0; j <= graph.DefaultComponentBindingLimit*2; j++ {
			rows = append(rows, binding(fmt.Sprintf("svc:%02d-%03d", i, j), graph.RelationMetricsIn, "c-prom"))
		}
		byComponent[id] = rows
	}
	g := &bindingGraph{byComponent: byComponent}
	acc, _ := resolveFixture(t, g, append(append([]string{}, ids...), "c-prom")...)

	res := resolve(t, acc, ids...)
	total := 0
	for _, s := range res.Sets {
		total += len(s.Bindings)
		if !s.Truncated {
			t.Errorf("candidate %s carries %d of many bindings without reporting Truncated",
				s.ComponentID, len(s.Bindings))
		}
	}
	if total > access.MaxResolveBindings {
		t.Errorf("one call returned %d bindings, over the total budget of %d",
			total, access.MaxResolveBindings)
	}
	if res.TotalBindingBudget != access.MaxResolveBindings {
		t.Errorf("TotalBindingBudget = %d, want the budget in force (%d)",
			res.TotalBindingBudget, access.MaxResolveBindings)
	}
	if res.PerComponentLimit != access.MaxResolveBindings/access.MaxResolveCandidates {
		t.Errorf("PerComponentLimit = %d, want the budget shared across %d candidates",
			res.PerComponentLimit, len(res.Sets))
	}

	// The share is a share, not a new ceiling: the ordinary few-candidate call
	// still carries the full per-component evidence.
	narrow := resolve(t, acc, ids[0])
	if narrow.PerComponentLimit != graph.DefaultComponentBindingLimit {
		t.Errorf("a single-candidate call got a share of %d, want the per-component ceiling of %d",
			narrow.PerComponentLimit, graph.DefaultComponentBindingLimit)
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
		if sets := resolve(t, acc).Sets; len(sets) != 0 {
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
		if sets := resolve(t, acc, "c-hidden").Sets; len(sets) != 0 {
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
		resolve(t, acc, "c-hidden")
		row := outcomeRow(t, rec)
		if row.Reason != access.ReasonComponentResolveMatch {
			t.Errorf("outcome reason = %q, want %q", row.Reason, access.ReasonComponentResolveMatch)
		}
		assertOutcomeCounts(t, row, 1, 1)
	})
}

// TestComponentBindings_AuditDoesNotBlameTheGrantForABound is the audit half of
// the candidate bound, and the reason it is load-bearing is that the audit row
// is the SOLE carrier of the distinction: the caller gets the same empty answer
// either way, by design.
//
// A match step that stopped at its own bound may have cut components this
// principal can read. Recording that call as "matched, none permitted" states a
// permission cause for a bound effect, and it states it to the one reader whose
// job is to work out whether a grant is missing.
func TestComponentBindings_AuditDoesNotBlameTheGrantForABound(t *testing.T) {
	g := &bindingGraph{byComponent: map[string][]graph.ComponentBinding{
		"c-hidden": {binding("svc:b", graph.RelationMetricsIn, "c-prom")},
	}}

	t.Run("the match prefix was cut", func(t *testing.T) {
		acc, rec := resolveFixture(t, g) // nothing is granted
		resolveWith(t, acc, access.ComponentResolveRequest{
			ComponentIDs: []string{"c-hidden"}, MatchesBounded: true,
		})

		row := outcomeRow(t, rec)
		if row.Reason == access.ReasonComponentResolveNoPermittedMatch {
			t.Fatal("the outcome row claims a permission cause for a call whose match prefix was cut: " +
				"components this principal may read could have matched and sorted below the bound, " +
				"in which case permission on them was never evaluated")
		}
		if row.Reason != access.ReasonComponentResolveBoundedNoPermittedMatch {
			t.Errorf("outcome reason = %q, want %q", row.Reason,
				access.ReasonComponentResolveBoundedNoPermittedMatch)
		}
		if !outcomeBlob(t, row).MatchesBounded {
			t.Error("the outcome row must record that the match step was bounded")
		}
	})

	t.Run("the match prefix was complete", func(t *testing.T) {
		acc, rec := resolveFixture(t, g)
		resolve(t, acc, "c-hidden")

		// Every match WAS evaluated here, so the permission claim is sound and
		// must survive: the fix separates the two cases rather than retiring the
		// row an operator debugging a missing grant is looking for.
		row := outcomeRow(t, rec)
		if row.Reason != access.ReasonComponentResolveNoPermittedMatch {
			t.Errorf("outcome reason = %q, want %q", row.Reason,
				access.ReasonComponentResolveNoPermittedMatch)
		}
	})

	t.Run("bounded but permitted", func(t *testing.T) {
		acc, rec := resolveFixture(t, g, "c-hidden", "c-prom")
		resolveWith(t, acc, access.ComponentResolveRequest{
			ComponentIDs: []string{"c-hidden"}, MatchesBounded: true,
		})
		if row := outcomeRow(t, rec); row.Reason != access.ReasonComponentResolveMatch {
			t.Errorf("outcome reason = %q, want %q", row.Reason, access.ReasonComponentResolveMatch)
		}
	})
}

// TestComponentBindings_AuditCarriesTheBoundsTheCallerIsNotTold is the other
// side of the disclosure rule. Two bounds cannot be reported to the caller — the
// match work bound and the per-candidate evidence work bound both derive from
// rows counted before the peer filter, so reporting either beside a post-filter
// count discloses the difference. They are not therefore unrecorded: the
// operator reads them here, which is where every other resolve distinction the
// caller may not have already lives.
func TestComponentBindings_AuditCarriesTheBoundsTheCallerIsNotTold(t *testing.T) {
	// Two visible bindings sit behind a denied prefix longer than the scan the
	// lowered limit buys, so the evidence work bound bites without the output
	// bound biting.
	var rows []graph.ComponentBinding
	for i := 0; i < 12; i++ {
		rows = append(rows, binding("svc:a", graph.RelationAlertsIn, fmt.Sprintf("c-secret-%02d", i)))
	}
	rows = append(rows, binding("svc:a", graph.RelationMetricsIn, "c-prom"))
	g := &bindingGraph{byComponent: map[string][]graph.ComponentBinding{"c-one": rows}}
	acc, rec := resolveFixture(t, g, "c-one", "c-prom")

	res := resolveWith(t, acc, access.ComponentResolveRequest{
		ComponentIDs: []string{"c-one"}, PerComponentLimit: 2,
	})
	if res.Sets[0].Truncated {
		t.Fatal("the output bound bit; this fixture is meant to exercise the WORK bound alone")
	}

	blob := outcomeBlob(t, outcomeRow(t, rec))
	if blob.EvidenceBounded != 1 {
		t.Errorf("evidence_bounded = %d, want 1: the operator has no other record that this "+
			"candidate's evidence may be short because of the work bound", blob.EvidenceBounded)
	}
}

// TestComponentBindings_AuditOutcomeCarriesNoPhrase pins the hygiene decision:
// the outcome row carries counts, never the task prose that produced them. The
// audit log is the governance trail, not a query log.
func TestComponentBindings_AuditOutcomeCarriesNoPhrase(t *testing.T) {
	g := &bindingGraph{byComponent: map[string][]graph.ComponentBinding{}}
	acc, rec := resolveFixture(t, g)

	resolve(t, acc)

	row := outcomeRow(t, rec)
	var blob map[string]any
	if err := json.Unmarshal([]byte(row.Context), &blob); err != nil {
		t.Fatalf("outcome context is not JSON: %v (%q)", err, row.Context)
	}
	for key := range blob {
		switch key {
		case "subkind", "matched", "permitted",
			"matches_bounded", "candidates_bounded", "evidence_bounded":
		default:
			t.Errorf("outcome context carries unexpected key %q — counts and bound flags only, no query text", key)
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

	res, err := acc.ComponentBindings(context.Background(), allowed,
		access.ComponentResolveRequest{ComponentIDs: []string{"c-known"}})
	if err != nil {
		t.Fatalf("a component with no bindings must be a normal answer, got error: %v", err)
	}
	sets := res.Sets
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

	_, err := acc.ComponentBindings(context.Background(), allowed,
		access.ComponentResolveRequest{ComponentIDs: []string{"c-known"}})
	if err == nil {
		t.Fatal("a missing graph store must be an error, not an empty answer")
	} else if !strings.Contains(err.Error(), "graph store not available") {
		t.Errorf("unexpected error for a missing graph store: %v", err)
	}

	// Unchanged by the two-pass split, and worth pinning because the graph check
	// moved: a principal permitted on nothing still gets the benign empty answer
	// rather than the error, so the graph-less deployment's failure stays an
	// availability one for grant-holders and not a disclosure one for anybody.
	denied, _ := resolveFixture(t, nil)
	if _, err := denied.ComponentBindings(context.Background(), allowed,
		access.ComponentResolveRequest{ComponentIDs: []string{"c-known"}}); err != nil {
		t.Errorf("a call permitting no candidate must not reach the graph store at all, got: %v", err)
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

// resolveOutcomeBlob mirrors the outcome row's context JSON.
type resolveOutcomeBlob struct {
	Subkind           string `json:"subkind"`
	Matched           int    `json:"matched"`
	Permitted         int    `json:"permitted"`
	MatchesBounded    bool   `json:"matches_bounded"`
	CandidatesBounded bool   `json:"candidates_bounded"`
	EvidenceBounded   int    `json:"evidence_bounded"`
}

func outcomeBlob(t *testing.T, row audit.Event) resolveOutcomeBlob {
	t.Helper()
	var blob resolveOutcomeBlob
	if err := json.Unmarshal([]byte(row.Context), &blob); err != nil {
		t.Fatalf("outcome context is not JSON: %v (%q)", err, row.Context)
	}
	return blob
}

func assertOutcomeCounts(t *testing.T, row audit.Event, matched, permitted int) {
	t.Helper()
	blob := outcomeBlob(t, row)
	if blob.Subkind != "component_resolve" {
		t.Errorf("outcome subkind = %q, want component_resolve", blob.Subkind)
	}
	if blob.Matched != matched || blob.Permitted != permitted {
		t.Errorf("outcome counts = matched %d / permitted %d, want %d / %d",
			blob.Matched, blob.Permitted, matched, permitted)
	}
}
