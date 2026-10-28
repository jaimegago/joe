package runfindings

import (
	"context"
	"reflect"
	"testing"

	"github.com/jaimegago/joe/internal/graph"
)

// A small topology: two services store in one database; an ingress fronts one
// of the services; a certificate secures the other; a Prometheus scrapes the
// database (an observability edge, which is not a dependency).
//
//	ingress  --ingress_for-->  svc-a  --stores_in-->  db  <--stores_in--  svc-b  <--secures--  cert
//	db  --metrics_in-->  prom
func reachTopology(t *testing.T) GraphReader {
	t.Helper()
	s := newTestStore(t)
	return seedGraph(t, s,
		[]testNode{
			{"ingress", "ingress", "k8s"},
			{"svc-a", "service", "k8s"},
			{"svc-b", "service", "k8s-b"},
			{"db", "database", "pg"},
			{"cert", "certificate", "k8s-b"},
			{"prom", "prometheus", "mon"},
			{"island", "service", "k8s"},
		},
		[]testEdge{
			{"ingress", "svc-a", graph.RelationIngressFor},
			{"svc-a", "db", graph.RelationStoresIn},
			{"svc-b", "db", graph.RelationStoresIn},
			{"cert", "svc-b", graph.RelationSecures},
			{"db", "prom", graph.RelationMetricsIn},
		},
	)
}

func TestComputeReach_WalksDependenceTransitively(t *testing.T) {
	g := reachTopology(t)
	r, err := ComputeReach(context.Background(), g, testPrincipal, "db")
	if err != nil {
		t.Fatal(err)
	}
	// Everything that depends on the database, transitively: both services,
	// and the ingress through svc-a. The certificate does not depend on the
	// database (svc-b depends on the certificate, not the reverse) and
	// Prometheus is an observability edge.
	if want := []string{"ingress", "svc-a", "svc-b"}; !reflect.DeepEqual(r.Dependents, want) {
		t.Errorf("dependents = %v, want %v", r.Dependents, want)
	}
	if r.Count != 3 {
		t.Errorf("count = %d, want 3", r.Count)
	}
	if want := []string{"k8s", "k8s-b"}; !reflect.DeepEqual(r.Components, want) {
		t.Errorf("components = %v, want %v", r.Components, want)
	}
}

func TestComputeReach_DirectionOfDependence(t *testing.T) {
	g := reachTopology(t)
	ctx := context.Background()

	// The certificate: svc-b depends on it (secures is walked to→from).
	r, err := ComputeReach(ctx, g, testPrincipal, "cert")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"svc-b"}; !reflect.DeepEqual(r.Dependents, want) {
		t.Errorf("cert dependents = %v, want %v", r.Dependents, want)
	}

	// The ingress: nothing depends on it — it is the dependent end of its
	// only edge.
	r, err = ComputeReach(ctx, g, testPrincipal, "ingress")
	if err != nil {
		t.Fatal(err)
	}
	if r.Count != 0 || len(r.Dependents) != 0 {
		t.Errorf("ingress reach = %+v, want none", r)
	}
}

func TestComputeReach_SubjectNothingDependsOnHasNoReach(t *testing.T) {
	g := reachTopology(t)
	r, err := ComputeReach(context.Background(), g, testPrincipal, "island")
	if err != nil {
		t.Fatal(err)
	}
	if r.Count != 0 {
		t.Errorf("island reach = %d, want 0", r.Count)
	}
	// Prometheus is reached only by an observability edge, which is not a
	// dependency, so nothing depends on it either.
	r, err = ComputeReach(context.Background(), g, testPrincipal, "prom")
	if err != nil {
		t.Fatal(err)
	}
	if r.Count != 0 {
		t.Errorf("prom reach = %d, want 0", r.Count)
	}
}

func TestComputeReach_UnknownSubjectIsAnError(t *testing.T) {
	g := reachTopology(t)
	if _, err := ComputeReach(context.Background(), g, testPrincipal, "nope"); err == nil {
		t.Fatal("unknown subject accepted, want error")
	}
}

func TestComputeReach_CycleTerminates(t *testing.T) {
	s := newTestStore(t)
	g := seedGraph(t, s,
		[]testNode{{"a", "service", "c"}, {"b", "service", "c"}},
		[]testEdge{{"a", "b", graph.RelationStoresIn}, {"b", "a", graph.RelationStoresIn}},
	)
	r, err := ComputeReach(context.Background(), g, testPrincipal, "a")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"b"}; !reflect.DeepEqual(r.Dependents, want) {
		t.Errorf("dependents = %v, want %v", r.Dependents, want)
	}
}

// TestDependencyRelations_AreGraphRelations pins that every relation reach
// walks is one the graph declares, so a renamed constant breaks here rather
// than silently emptying reach.
func TestDependencyRelations_AreGraphRelations(t *testing.T) {
	declared := map[string]struct{}{
		graph.RelationStoresIn: {}, graph.RelationQueuesIn: {}, graph.RelationImageStoredIn: {},
		graph.RelationIngressFor: {}, graph.RelationProxies: {}, graph.RelationManagedBy: {},
		graph.RelationSecures: {},
	}
	for _, r := range DependencyRelations() {
		if _, ok := declared[r]; !ok {
			t.Errorf("reach walks %q, which is not a declared graph relation", r)
		}
	}
}
