//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/access"
	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/test/mocks"
)

// These tests exercise the RBAC authorization decision at the guarded accessor
// (internal/access) — the chokepoint Phase E (D-0008) moved enforcement onto,
// reached by both the HTTP handlers and the in-process agent loop. They
// formerly drove a direct-HTTP managed-system route (GET /api/v1/k8s/{id}/
// resources) purely to reach the accessor; that route has been removed, so the
// decision is now asserted directly with constructed principals over a real
// SQLite-backed RBAC repository. The HTTP auth chain (EdgeAuth credential
// extraction → context principal → accessor → status mapping) is covered by the
// api_test suite via a test-only probe route, and by internal/auth.

// newRBACStore opens an in-memory SQLite store with migrations applied and
// returns it alongside an RBAC repository over the same database.
func newRBACStore(t *testing.T) (*store.Store, rbac.Repository) {
	t.Helper()
	testStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := testStore.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { testStore.Close() })
	return testStore, rbac.NewRepository(testStore.DB(), testStore.Driver())
}

// registryWithK8s registers a no-op Kubernetes adapter under componentID so an
// allowed read resolves to an adapter and returns nil rather than
// component-not-found.
func registryWithK8s(componentID string) *adapters.Registry {
	registry := adapters.NewRegistry()
	registry.Register(componentID, mocks.NewMockK8sAdapter())
	return registry
}

// TestIntegration_RBAC_NoAuth_SourceReadPassthrough checks that when no service
// account is configured the policy engine is nil and the accessor permits every
// decision (mirroring EnforcementMiddleware(nil) on the old transport) — a read
// is never denied by RBAC.
func TestIntegration_RBAC_NoAuth_SourceReadPassthrough(t *testing.T) {
	// nil engine ⇒ accessor permits all; the registered adapter makes the read
	// reach infrastructure and return nil.
	acc := access.New(registryWithK8s("local-k8s"), nil, nil, nil)

	if _, err := acc.K8sListResources(context.Background(), "anyone", "local-k8s", "pods", ""); err != nil {
		if errors.Is(err, access.ErrPermissionDenied) {
			t.Errorf("auth disabled (nil engine): read must not be denied by RBAC, got ErrPermissionDenied")
		}
		t.Errorf("auth disabled: read should reach the adapter, got: %v", err)
	}
}

// TestIntegration_RBAC_Auth_AllowsReadWithPolicy checks that a principal whose
// policy grants the source's zone is allowed by the accessor.
func TestIntegration_RBAC_Auth_AllowsReadWithPolicy(t *testing.T) {
	testStore, repo := newRBACStore(t)
	ctx := context.Background()
	const principal rbac.Principal = "svc:ops"

	if err := testStore.Components.Create(ctx, &store.Component{
		ID: "local-k8s", Type: store.ComponentTypeKubernetes, Name: "Local Kind", Config: []byte(`{}`),
	}); err != nil {
		t.Fatalf("create source: %v", err)
	}
	// Seed: local-k8s → prod-readonly; svc:ops → prod-readonly.
	if err := repo.UpsertAssignment(ctx, rbac.ComponentZoneAssignment{
		ComponentID: "local-k8s", ZoneID: "prod-readonly", AssignedBy: "test",
	}, "test"); err != nil {
		t.Fatalf("UpsertAssignment: %v", err)
	}
	if _, err := repo.CreatePolicy(ctx, rbac.Policy{
		Principal: string(principal), ZoneID: "prod-readonly",
	}, "test"); err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}

	acc := access.New(registryWithK8s("local-k8s"), nil, rbac.NewPolicyEngine(repo), nil)
	if _, err := acc.K8sListResources(ctx, principal, "local-k8s", "pods", ""); err != nil {
		t.Errorf("principal with a valid policy should be allowed, got: %v", err)
	}
}

// TestIntegration_RBAC_Auth_DeniesReadWithoutPolicy checks that a principal with
// no policy for the source's zone is denied with ErrPermissionDenied. This is
// the security-load-bearing deny assertion (previously asserted as an HTTP 403).
func TestIntegration_RBAC_Auth_DeniesReadWithoutPolicy(t *testing.T) {
	testStore, repo := newRBACStore(t)
	ctx := context.Background()

	if err := testStore.Components.Create(ctx, &store.Component{
		ID: "local-k8s", Type: store.ComponentTypeKubernetes, Name: "Local Kind", Config: []byte(`{}`),
	}); err != nil {
		t.Fatalf("create source: %v", err)
	}
	// Assign the source to a zone but grant svc:nobody no policy for it.
	if err := repo.UpsertAssignment(ctx, rbac.ComponentZoneAssignment{
		ComponentID: "local-k8s", ZoneID: "prod-readonly", AssignedBy: "test",
	}, "test"); err != nil {
		t.Fatalf("UpsertAssignment: %v", err)
	}

	acc := access.New(registryWithK8s("local-k8s"), nil, rbac.NewPolicyEngine(repo), nil)
	if _, err := acc.K8sListResources(ctx, "svc:nobody", "local-k8s", "pods", ""); !errors.Is(err, access.ErrPermissionDenied) {
		t.Errorf("principal with no policy must be denied with ErrPermissionDenied, got: %v", err)
	}
}

// TestIntegration_RBAC_ComponentResolutionFiltersPerPrincipal is the
// component-resolution half of this suite. Resolution is the only accessor path
// whose ANSWER is filtered per principal rather than merely permitted or denied
// as a whole, so the assertion it needs is different in kind: not "was this call
// denied" but "did the result silently omit everything this principal may not
// see".
//
// Two omissions are asserted, and they are separate holes:
//
//   - a matched component the principal may not read is not a candidate; and
//   - a peer component the principal may not read is dropped from a PERMITTED
//     candidate's evidence, so the candidate cannot become a side channel
//     naming a component the grant does not cover.
//
// It runs against a real SQLite-backed RBAC repository and a real graph store,
// so the decision path, the zone resolution and the graph read are the
// production ones.
func TestIntegration_RBAC_ComponentResolutionFiltersPerPrincipal(t *testing.T) {
	testStore, repo := newRBACStore(t)
	ctx := context.Background()
	const principal rbac.Principal = "svc:ops"

	// Three components. The principal is granted prod-readonly, which holds the
	// app and its metrics backend; the log backend sits in dev-full, which the
	// principal holds no policy for. Both zones are migration-seeded, so the
	// decision runs against real zone rows rather than test-only ones.
	for _, c := range []struct{ id, typ, name, zone string }{
		{"c-checkout", store.ComponentTypeKubernetes, "checkout", "prod-readonly"},
		{"c-prom", store.ComponentTypePrometheus, "prom-prod", "prod-readonly"},
		{"c-loki", store.ComponentTypeLoki, "loki-prod", "dev-full"},
	} {
		if err := testStore.Components.Create(ctx, &store.Component{
			ID: c.id, Type: c.typ, Name: c.name, Config: []byte(`{}`),
		}); err != nil {
			t.Fatalf("create component %s: %v", c.id, err)
		}
		if err := repo.UpsertAssignment(ctx, rbac.ComponentZoneAssignment{
			ComponentID: c.id, ZoneID: c.zone, AssignedBy: "test",
		}, "test"); err != nil {
			t.Fatalf("assign %s: %v", c.id, err)
		}
	}
	// A fourth component matches the same phrase and is granted to nobody.
	if err := testStore.Components.Create(ctx, &store.Component{
		ID: "c-checkout-secret", Type: store.ComponentTypeKubernetes, Name: "checkout-secret", Config: []byte(`{}`),
	}); err != nil {
		t.Fatalf("create component c-checkout-secret: %v", err)
	}
	if err := repo.UpsertAssignment(ctx, rbac.ComponentZoneAssignment{
		ComponentID: "c-checkout-secret", ZoneID: "dev-full", AssignedBy: "test",
	}, "test"); err != nil {
		t.Fatalf("assign c-checkout-secret: %v", err)
	}
	if _, err := repo.CreatePolicy(ctx, rbac.Policy{
		Principal: string(principal), ZoneID: "prod-readonly",
	}, "test"); err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}

	// The graph: checkout is bound to both backends. Only the Prometheus
	// binding may survive.
	graphStore := graph.NewSQLStore(testStore.DB(), testStore.Driver(), nil)
	for _, n := range []graph.Node{
		{ID: "svc:checkout", Type: "service", ComponentID: "c-checkout"},
		{ID: "prom:root", Type: "metrics_backend", ComponentID: "c-prom"},
		{ID: "loki:root", Type: "log_backend", ComponentID: "c-loki"},
	} {
		if err := graphStore.AddNode(ctx, n); err != nil {
			t.Fatalf("AddNode(%s): %v", n.ID, err)
		}
	}
	for _, e := range []graph.Edge{
		{From: "svc:checkout", To: "prom:root", Relation: graph.RelationMetricsIn, Confidence: graph.Explicit, Source: "test"},
		{From: "svc:checkout", To: "loki:root", Relation: graph.RelationLogsIn, Confidence: graph.Explicit, Source: "test"},
	} {
		if err := graphStore.AddEdge(ctx, e); err != nil {
			t.Fatalf("AddEdge(%s->%s): %v", e.From, e.To, err)
		}
	}

	acc := access.New(adapters.NewRegistry(), graphStore, rbac.NewPolicyEngine(repo), nil)

	// Both Kubernetes components match a phrase naming "checkout"; the matcher
	// is exercised in its own package, so the ids are passed directly here to
	// keep this test about the decision rather than about string matching.
	res, err := acc.ComponentBindings(ctx, principal, access.ComponentResolveRequest{
		ComponentIDs: []string{"c-checkout", "c-checkout-secret"},
	})
	if err != nil {
		t.Fatalf("ComponentBindings: %v", err)
	}
	sets := res.Sets

	if len(sets) != 1 || sets[0].ComponentID != "c-checkout" {
		var ids []string
		for _, s := range sets {
			ids = append(ids, s.ComponentID)
		}
		t.Fatalf("candidates = %v, want only c-checkout — an ungranted component must not be a candidate", ids)
	}

	if len(sets[0].Bindings) != 1 {
		t.Fatalf("candidate carries %d bindings (%+v), want only the granted peer",
			len(sets[0].Bindings), sets[0].Bindings)
	}
	if got := sets[0].Bindings[0].PeerComponentID; got != "c-prom" {
		t.Errorf("surviving binding peer = %q, want c-prom", got)
	}
	for _, b := range sets[0].Bindings {
		if b.PeerComponentID == "c-loki" {
			t.Error("evidence disclosed a peer component the principal holds no grant on")
		}
	}
}

// --- the bounds, and where they sit relative to the permit ---
//
// The three tests below pin one defect in three consequences: a bound spent on a
// permission-blind ordering hands the principal a filtered prefix of everyone's
// answer rather than a prefix of their own. Every one of them needs a principal
// who does NOT hold every peer, which is the condition under which a pre-filter
// count and a post-filter count can disagree at all — and is exactly the
// condition the suite's first truncation test lacked.

// resolveBoundsFixture builds the shape all three need: one principal holding
// prod-readonly, one visible peer in that zone, and a wall of peers in dev-full
// the principal holds no policy for. It returns the accessor and the audit
// database behind it.
//
// The denied peers are bound by alerts_in and the visible peer by metrics_in,
// which puts every denied peer AHEAD of the visible one under the store's
// ordering (relation, peer component, peer node, near node, direction). That is
// an ordinary arrangement rather than a contrived one — the ordering is
// alphabetical on the relation and knows nothing about grants — and it is the
// arrangement under which a bound applied before the filter is worst.
func resolveBoundsFixture(t *testing.T, principal rbac.Principal, deniedPeers int) (*access.Accessor, *store.Store) {
	t.Helper()
	testStore, repo := newRBACStore(t)
	ctx := context.Background()

	component := func(id, zone string) {
		t.Helper()
		if err := testStore.Components.Create(ctx, &store.Component{
			ID: id, Type: store.ComponentTypeKubernetes, Name: id, Config: []byte(`{}`),
		}); err != nil {
			t.Fatalf("create component %s: %v", id, err)
		}
		if err := repo.UpsertAssignment(ctx, rbac.ComponentZoneAssignment{
			ComponentID: id, ZoneID: zone, AssignedBy: "test",
		}, "test"); err != nil {
			t.Fatalf("assign %s: %v", id, err)
		}
	}
	if _, err := repo.CreatePolicy(ctx, rbac.Policy{
		Principal: string(principal), ZoneID: "prod-readonly",
	}, "test"); err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}

	component("c-app", "prod-readonly")
	component("c-prom", "prod-readonly")

	graphStore := graph.NewSQLStore(testStore.DB(), testStore.Driver(), nil)
	addNode := func(id, typ, componentID string) {
		t.Helper()
		if err := graphStore.AddNode(ctx, graph.Node{ID: id, Type: typ, ComponentID: componentID}); err != nil {
			t.Fatalf("AddNode(%s): %v", id, err)
		}
	}
	addEdge := func(from, to, relation string) {
		t.Helper()
		if err := graphStore.AddEdge(ctx, graph.Edge{
			From: from, To: to, Relation: relation, Confidence: graph.Explicit, Source: "test",
		}); err != nil {
			t.Fatalf("AddEdge(%s->%s): %v", from, to, err)
		}
	}

	addNode("svc:app", "service", "c-app")
	addNode("prom:root", "metrics_backend", "c-prom")
	addEdge("svc:app", "prom:root", graph.RelationMetricsIn)

	for i := 0; i < deniedPeers; i++ {
		id := fmt.Sprintf("c-secret-%02d", i)
		component(id, "dev-full")
		addNode(id+":root", "alert_backend", id)
		addEdge("svc:app", id+":root", graph.RelationAlertsIn)
	}

	return access.New(adapters.NewRegistry(), graphStore,
		rbac.NewPolicyEngine(repo), audit.NewRepository(testStore.DB(), testStore.Driver())), testStore
}

// resolveOutcomeReason reads back the single per-call resolve-outcome row. The
// audit repository is write-only by design, so this reads the table the real
// insert wrote to — which also proves the new reason survives migration 015's
// constraints rather than only surviving a fake.
func resolveOutcomeReason(t *testing.T, testStore *store.Store) (reason, blob string) {
	t.Helper()
	rows, err := testStore.DB().Query(
		`SELECT reason, context FROM audit_log WHERE reason LIKE 'component_resolve%' ORDER BY id`)
	if err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	defer rows.Close()

	var found int
	for rows.Next() {
		if err := rows.Scan(&reason, &blob); err != nil {
			t.Fatalf("scan audit row: %v", err)
		}
		found++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("audit rows: %v", err)
	}
	if found != 1 {
		t.Fatalf("found %d resolve-outcome rows, want exactly 1 — one call, one outcome row", found)
	}
	return reason, blob
}

// TestIntegration_RBAC_ComponentResolutionEvidenceBoundFollowsThePermit pins the
// two consequences that share the evidence path, in opposite directions.
//
// The bound used to be spent by the store, before the peer filter ran. That made
// the truncation flag a count of rows the principal may NOT see — disclosed
// beside a post-filter binding count, which is a cardinality channel on the
// governed side — and it made the cut land on the denied prefix, withholding
// evidence the principal IS entitled to.
func TestIntegration_RBAC_ComponentResolutionEvidenceBoundFollowsThePermit(t *testing.T) {
	const principal rbac.Principal = "svc:ops"
	acc, _ := resolveBoundsFixture(t, principal, 6)

	// A limit of two, against six denied peers sorting ahead of one visible
	// one. Spent before the filter this returns nothing and reports truncation;
	// spent after it, it returns the one binding and reports none.
	res, err := acc.ComponentBindings(context.Background(), principal, access.ComponentResolveRequest{
		ComponentIDs: []string{"c-app"}, PerComponentLimit: 2,
	})
	if err != nil {
		t.Fatalf("ComponentBindings: %v", err)
	}
	if len(res.Sets) != 1 {
		t.Fatalf("got %d candidates, want 1", len(res.Sets))
	}
	set := res.Sets[0]

	if len(set.Bindings) != 1 {
		t.Fatalf("candidate carries %d bindings (%+v), want the one permitted peer that sorts "+
			"after the denied ones — a bound spent before the peer filter withholds evidence "+
			"the principal is entitled to", len(set.Bindings), set.Bindings)
	}
	if got := set.Bindings[0].PeerComponentID; got != "c-prom" {
		t.Errorf("surviving binding peer = %q, want c-prom", got)
	}
	if set.Truncated {
		t.Errorf("Truncated is set beside a binding count of %d under a limit of 2: derived from "+
			"the raw rows, the pair discloses how many edges from a component the principal may "+
			"read reach components they may not", len(set.Bindings))
	}
}

// TestIntegration_RBAC_ComponentResolutionCandidateBoundFollowsThePermit pins
// the candidate half. The bound must count components the principal MAY SEE:
// spent on the match ordering it takes a prefix of everyone's ranking, so a
// principal whose own components sort below it receives the empty answer having
// never been evaluated.
func TestIntegration_RBAC_ComponentResolutionCandidateBoundFollowsThePermit(t *testing.T) {
	const principal rbac.Principal = "svc:ops"
	acc, testStore := resolveBoundsFixture(t, principal, 2)
	ctx := context.Background()
	repo := rbac.NewRepository(testStore.DB(), testStore.Driver())

	// Ungranted components rank ahead of every component the principal holds.
	var ids []string
	for i := 0; i < 5; i++ {
		ids = append(ids, fmt.Sprintf("c-secret-ranked-%02d", i))
	}
	var mine []string
	for i := 0; i <= access.MaxResolveCandidates; i++ {
		mine = append(mine, fmt.Sprintf("c-mine-%02d", i))
	}
	for _, spec := range []struct {
		ids  []string
		zone string
	}{{ids, "dev-full"}, {mine, "prod-readonly"}} {
		for _, id := range spec.ids {
			if err := testStore.Components.Create(ctx, &store.Component{
				ID: id, Type: store.ComponentTypeKubernetes, Name: id, Config: []byte(`{}`),
			}); err != nil {
				t.Fatalf("create component %s: %v", id, err)
			}
			if err := repo.UpsertAssignment(ctx, rbac.ComponentZoneAssignment{
				ComponentID: id, ZoneID: spec.zone, AssignedBy: "test",
			}, "test"); err != nil {
				t.Fatalf("assign %s: %v", id, err)
			}
		}
	}

	res, err := acc.ComponentBindings(ctx, principal, access.ComponentResolveRequest{
		ComponentIDs: append(ids, mine...),
	})
	if err != nil {
		t.Fatalf("ComponentBindings: %v", err)
	}

	if len(res.Sets) != access.MaxResolveCandidates {
		t.Fatalf("got %d candidates, want the bound of %d — ungranted matches ranked above the "+
			"principal's own must not consume the candidate budget",
			len(res.Sets), access.MaxResolveCandidates)
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

	// The total evidence budget is bounded and reported: the per-candidate share
	// is what the caller needs to read a truncation flag against.
	if res.TotalBindingBudget != access.MaxResolveBindings {
		t.Errorf("TotalBindingBudget = %d, want %d", res.TotalBindingBudget, access.MaxResolveBindings)
	}
	if want := access.MaxResolveBindings / access.MaxResolveCandidates; res.PerComponentLimit != want {
		t.Errorf("PerComponentLimit = %d, want the budget shared across %d candidates (%d)",
			res.PerComponentLimit, len(res.Sets), want)
	}
}

// TestIntegration_RBAC_ComponentResolutionAuditDoesNotBlameTheGrant pins the
// third consequence, and it is the one the whole unhappy-path design rests on:
// the caller cannot tell "nothing matched" from "nothing you may see matched" by
// construction, so the audit row is the SOLE carrier of the distinction.
//
// A call whose match prefix was cut may never have evaluated permission on the
// principal's own components. Recording it as "matched, none permitted" states a
// permission cause for a bound effect, to the one reader whose job is to work
// out whether a grant is missing.
func TestIntegration_RBAC_ComponentResolutionAuditDoesNotBlameTheGrant(t *testing.T) {
	const principal rbac.Principal = "svc:ops"

	t.Run("the match prefix was cut", func(t *testing.T) {
		acc, testStore := resolveBoundsFixture(t, principal, 1)
		if _, err := acc.ComponentBindings(context.Background(), principal, access.ComponentResolveRequest{
			ComponentIDs:   []string{"c-secret-00"},
			MatchesBounded: true,
		}); err != nil {
			t.Fatalf("ComponentBindings: %v", err)
		}

		reason, blob := resolveOutcomeReason(t, testStore)
		if reason == access.ReasonComponentResolveNoPermittedMatch {
			t.Fatal("the outcome row claims a permission cause for a call whose match prefix was " +
				"cut: components this principal may read could have matched and sorted below the " +
				"bound, in which case permission on them was never evaluated")
		}
		if reason != access.ReasonComponentResolveBoundedNoPermittedMatch {
			t.Errorf("outcome reason = %q, want %q", reason,
				access.ReasonComponentResolveBoundedNoPermittedMatch)
		}
		if !strings.Contains(blob, `"matches_bounded":true`) {
			t.Errorf("outcome context %q must record that the match step was bounded", blob)
		}
	})

	t.Run("the match prefix was complete", func(t *testing.T) {
		acc, testStore := resolveBoundsFixture(t, principal, 1)
		if _, err := acc.ComponentBindings(context.Background(), principal, access.ComponentResolveRequest{
			ComponentIDs: []string{"c-secret-00"},
		}); err != nil {
			t.Fatalf("ComponentBindings: %v", err)
		}

		// Every match WAS evaluated, so the permission claim is sound and must
		// survive: the fix separates the two cases rather than retiring the row
		// an operator debugging a missing grant is looking for.
		reason, _ := resolveOutcomeReason(t, testStore)
		if reason != access.ReasonComponentResolveNoPermittedMatch {
			t.Errorf("outcome reason = %q, want %q", reason,
				access.ReasonComponentResolveNoPermittedMatch)
		}
	})
}
