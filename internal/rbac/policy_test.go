package rbac_test

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/rbac"
)

// openTestDB opens an in-memory SQLite database with the RBAC schema seeded.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE sources (
			id TEXT PRIMARY KEY,
			name TEXT
		);
		CREATE TABLE security_zones (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT,
			allowed_actions TEXT NOT NULL DEFAULT '["read"]',
			created_at TEXT NOT NULL
		);
		CREATE TABLE source_zone_assignments (
			source_id TEXT NOT NULL REFERENCES sources(id),
			zone_id TEXT NOT NULL REFERENCES security_zones(id),
			assigned_by TEXT NOT NULL,
			reason TEXT,
			assigned_at TEXT NOT NULL,
			PRIMARY KEY (source_id)
		);
		CREATE TABLE rbac_policies (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			principal TEXT NOT NULL,
			zone_id TEXT NOT NULL REFERENCES security_zones(id),
			created_at TEXT NOT NULL,
			UNIQUE (principal, zone_id)
		);

		-- seed zones
		INSERT INTO security_zones VALUES ('prod-readonly','Production Read-Only','',   '["read","query"]',                   '2026-01-01T00:00:00Z');
		INSERT INTO security_zones VALUES ('prod-write',   'Production Write',   '',   '["read","query","mutate"]',           '2026-01-01T00:00:00Z');
		INSERT INTO security_zones VALUES ('dev-full',     'Development Full',   '',   '["read","query","mutate","delete"]',  '2026-01-01T00:00:00Z');
		INSERT INTO security_zones VALUES ('unassigned',   'Unassigned',         '',   '["read"]',                           '2026-01-01T00:00:00Z');

		-- seed a source
		INSERT INTO sources VALUES ('k8s-prod', 'Production K8s');
		INSERT INTO sources VALUES ('k8s-dev',  'Dev K8s');
	`)
	if err != nil {
		t.Fatalf("seed schema: %v", err)
	}
	return db
}

func TestPolicyEngine_IsAllowed_ReadOnZone(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	// Assign k8s-prod to prod-readonly zone.
	_ = repo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{
		SourceID: "k8s-prod", ZoneID: "prod-readonly", AssignedBy: "test",
	})

	// Grant alice access to prod-readonly.
	_, _ = repo.CreatePolicy(ctx, rbac.Policy{Principal: "alice", ZoneID: "prod-readonly"})

	engine := rbac.NewPolicyEngine(repo)

	if !engine.IsAllowed(ctx, "alice", "k8s-prod", rbac.ActionRead) {
		t.Error("alice should be able to read prod-readonly source")
	}
	if !engine.IsAllowed(ctx, "alice", "k8s-prod", rbac.ActionQuery) {
		t.Error("alice should be able to query prod-readonly source")
	}
	if engine.IsAllowed(ctx, "alice", "k8s-prod", rbac.ActionMutate) {
		t.Error("alice should NOT be able to mutate prod-readonly source")
	}
}

func TestPolicyEngine_IsAllowed_WriteZone(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	_ = repo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{
		SourceID: "k8s-prod", ZoneID: "prod-write", AssignedBy: "test",
	})
	_, _ = repo.CreatePolicy(ctx, rbac.Policy{Principal: "bob", ZoneID: "prod-write"})

	engine := rbac.NewPolicyEngine(repo)

	if !engine.IsAllowed(ctx, "bob", "k8s-prod", rbac.ActionMutate) {
		t.Error("bob should be able to mutate prod-write source")
	}
	if engine.IsAllowed(ctx, "bob", "k8s-prod", rbac.ActionDelete) {
		t.Error("bob should NOT be able to delete from prod-write source")
	}
}

func TestPolicyEngine_IsAllowed_Unassigned(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	// k8s-dev has no zone assignment — defaults to "unassigned" (read only).
	_, _ = repo.CreatePolicy(ctx, rbac.Policy{Principal: "charlie", ZoneID: "unassigned"})

	engine := rbac.NewPolicyEngine(repo)

	if !engine.IsAllowed(ctx, "charlie", "k8s-dev", rbac.ActionRead) {
		t.Error("charlie should be able to read unassigned source")
	}
	if engine.IsAllowed(ctx, "charlie", "k8s-dev", rbac.ActionMutate) {
		t.Error("charlie should NOT be able to mutate unassigned source")
	}
}

func TestPolicyEngine_IsAllowed_NoPolicyDenied(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	_ = repo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{
		SourceID: "k8s-prod", ZoneID: "prod-readonly", AssignedBy: "test",
	})
	// No policy for dave.

	engine := rbac.NewPolicyEngine(repo)

	if engine.IsAllowed(ctx, "dave", "k8s-prod", rbac.ActionRead) {
		t.Error("dave has no policy and should be denied")
	}
}

func TestPolicyEngine_IsAllowed_DevFull(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	_ = repo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{
		SourceID: "k8s-dev", ZoneID: "dev-full", AssignedBy: "test",
	})
	_, _ = repo.CreatePolicy(ctx, rbac.Policy{Principal: "eve", ZoneID: "dev-full"})

	engine := rbac.NewPolicyEngine(repo)

	for _, action := range []rbac.Action{rbac.ActionRead, rbac.ActionQuery, rbac.ActionMutate, rbac.ActionDelete} {
		if !engine.IsAllowed(ctx, "eve", "k8s-dev", action) {
			t.Errorf("eve should be allowed action %q in dev-full zone", action)
		}
	}
}
