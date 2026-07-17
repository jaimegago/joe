package store

import (
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migrateSQLite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/jaimegago/joe/internal/observability"
)

// TestMigration032_PruneOrphanedGraphNodes asserts the one-time orphan sweep:
// a database seeded at the 031 boundary with a mix of live and orphaned
// graph_nodes (dead component_id, NULL, and empty-string), plus edges spanning
// them, has exactly the orphans and their edges removed when 032 applies, while
// the live component's nodes and the fully-live edge are untouched. The NULL and
// empty-string cases pin the NOT EXISTS predicate (a bare NOT IN would leave the
// NULL row behind).
func TestMigration032_PruneOrphanedGraphNodes(t *testing.T) {
	s, err := New(DatabaseConfig{Driver: DriverSQLite, DSN: ":memory:"}, (*observability.Metrics)(nil))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	source, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		t.Fatalf("iofs.New: %v", err)
	}
	driver, err := migrateSQLite.WithInstance(s.db, &migrateSQLite.Config{})
	if err != nil {
		t.Fatalf("WithInstance: %v", err)
	}
	m, err := migrate.NewWithInstance("iofs", source, DriverSQLite, driver)
	if err != nil {
		t.Fatalf("NewWithInstance: %v", err)
	}

	// 1) Up to the 031 boundary: graph_nodes/graph_edges (002) and components
	//    (001+023) all exist, but the 032 sweep has not yet run.
	if err := m.Migrate(31); err != nil {
		t.Fatalf("migrate to 31: %v", err)
	}

	// 2) Seed a pre-sweep graph. One live component with two nodes; orphan nodes
	//    carrying a dead component_id, a NULL, and an empty string. Edges: one
	//    fully-live (must survive), and two each touching an orphan node (must
	//    die with that node via FK cascade). graph_nodes has no FK to components,
	//    so orphan component_ids insert freely; graph_edges endpoints must exist
	//    as nodes at insert time (foreign_keys is ON), and all do.
	seed := [][]any{
		{`INSERT INTO components (id, type, name, config, status, created_at, updated_at)
		    VALUES ('live-a','kubernetes','Live A','{}','active',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`},
		// live nodes under live-a
		{`INSERT INTO graph_nodes (id, type, component_id) VALUES ('n-a1','deployment','live-a')`},
		{`INSERT INTO graph_nodes (id, type, component_id) VALUES ('n-a2','service','live-a')`},
		// orphans: dead component_id, NULL, and empty string
		{`INSERT INTO graph_nodes (id, type, component_id) VALUES ('o-dead','deployment','dead-comp')`},
		{`INSERT INTO graph_nodes (id, type, component_id) VALUES ('o-null','deployment',NULL)`},
		{`INSERT INTO graph_nodes (id, type, component_id) VALUES ('o-empty','deployment','')`},
		// fully-live edge (both endpoints under live-a) — must survive
		{`INSERT INTO graph_edges (from_node, to_node, relation) VALUES ('n-a1','n-a2','exposes')`},
		// edges touching an orphan node — must die when that node is swept
		{`INSERT INTO graph_edges (from_node, to_node, relation) VALUES ('o-dead','n-a1','points_at')`},
		{`INSERT INTO graph_edges (from_node, to_node, relation) VALUES ('n-a2','o-null','points_at')`},
	}
	for _, stmt := range seed {
		if _, err := s.db.Exec(stmt[0].(string)); err != nil {
			t.Fatalf("seed %q: %v", stmt[0], err)
		}
	}

	// 3) Apply 032.
	if err := m.Steps(1); err != nil {
		t.Fatalf("apply 032: %v", err)
	}

	// 4a) Every orphan node is gone; both live nodes remain.
	for _, id := range []string{"o-dead", "o-null", "o-empty"} {
		if graphNodeIDExists(t, s, id) {
			t.Errorf("orphan node %q survived the sweep; must be pruned", id)
		}
	}
	for _, id := range []string{"n-a1", "n-a2"} {
		if !graphNodeIDExists(t, s, id) {
			t.Errorf("live node %q was pruned; a live component's nodes must be untouched", id)
		}
	}

	// 4b) Orphan-touching edges are gone (FK cascade); the fully-live edge remains.
	if graphEdgeRowExists(t, s, "o-dead", "n-a1", "points_at") {
		t.Error("edge o-dead->n-a1 survived; must die with its swept orphan endpoint")
	}
	if graphEdgeRowExists(t, s, "n-a2", "o-null", "points_at") {
		t.Error("edge n-a2->o-null survived; must die with its swept orphan endpoint")
	}
	if !graphEdgeRowExists(t, s, "n-a1", "n-a2", "exposes") {
		t.Error("fully-live edge n-a1->n-a2 was removed; live topology must be untouched")
	}

	// 4c) The live component row itself is untouched.
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM components WHERE id = 'live-a'`).Scan(&n); err != nil {
		t.Fatalf("count live component: %v", err)
	}
	if n != 1 {
		t.Errorf("live component count = %d, want 1", n)
	}
}

func graphNodeIDExists(t *testing.T, s *Store, id string) bool {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM graph_nodes WHERE id = ?`, id).Scan(&n); err != nil {
		t.Fatalf("count graph_nodes %q: %v", id, err)
	}
	return n > 0
}

func graphEdgeRowExists(t *testing.T, s *Store, from, to, relation string) bool {
	t.Helper()
	var n int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM graph_edges WHERE from_node = ? AND to_node = ? AND relation = ?`,
		from, to, relation).Scan(&n); err != nil {
		t.Fatalf("count graph_edges %s->%s (%s): %v", from, to, relation, err)
	}
	return n > 0
}
