package runfindings

import (
	"context"
	"testing"

	"github.com/jaimegago/joe/internal/access"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
)

// testPrincipal is the principal the tests read as. The accessor under test
// has no policy engine (permit-all), so any principal is admitted; the real
// run passes agent:core.
const testPrincipal = rbac.Principal("svc:agent:core")

// newTestStore opens a migrated in-memory store for a test.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// testNode and testEdge describe a small graph for a test.
type testNode struct{ id, typ, component string }
type testEdge struct{ from, to, relation string }

// seedGraph loads nodes and edges into the store's graph tables and returns a
// guarded reader over them — an accessor with no policy engine, which permits
// every read, the same posture api tests use.
func seedGraph(t *testing.T, s *store.Store, nodes []testNode, edges []testEdge) GraphReader {
	t.Helper()
	g := graph.NewSQLiteStore(s.DB(), nil)
	ctx := context.Background()
	for _, n := range nodes {
		if err := g.AddNode(ctx, graph.Node{ID: n.id, Type: n.typ, ComponentID: n.component, Metadata: map[string]any{}}); err != nil {
			t.Fatalf("add node %s: %v", n.id, err)
		}
	}
	for _, e := range edges {
		if err := g.AddEdge(ctx, graph.Edge{From: e.from, To: e.to, Relation: e.relation, Confidence: graph.Explicit}); err != nil {
			t.Fatalf("add edge %s-%s->%s: %v", e.from, e.relation, e.to, err)
		}
	}
	return access.New(nil, g, nil, nil)
}
