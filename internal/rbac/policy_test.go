package rbac_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/rbac"
)

// errRepository is a mock rbac.Repository that returns errors on demand.
type errRepository struct {
	getAssignmentErr            error
	getAssignmentResult         *rbac.SourceZoneAssignment
	getZoneErr                  error
	getZoneResult               *rbac.Zone
	listPoliciesForPrincipalErr error
	listPoliciesResult          []rbac.Policy
}

func (r *errRepository) ListZones(_ context.Context) ([]rbac.Zone, error) {
	return nil, nil
}
func (r *errRepository) GetZone(_ context.Context, _ string) (*rbac.Zone, error) {
	return r.getZoneResult, r.getZoneErr
}
func (r *errRepository) CreateZone(_ context.Context, z rbac.Zone) (*rbac.Zone, error) {
	return &z, nil
}
func (r *errRepository) ListAssignments(_ context.Context) ([]rbac.SourceZoneAssignment, error) {
	return nil, nil
}
func (r *errRepository) GetAssignment(_ context.Context, _ string) (*rbac.SourceZoneAssignment, error) {
	return r.getAssignmentResult, r.getAssignmentErr
}
func (r *errRepository) UpsertAssignment(_ context.Context, _ rbac.SourceZoneAssignment) error {
	return nil
}
func (r *errRepository) ListPolicies(_ context.Context) ([]rbac.Policy, error) {
	return nil, nil
}
func (r *errRepository) ListPoliciesForPrincipal(_ context.Context, _ string) ([]rbac.Policy, error) {
	return r.listPoliciesResult, r.listPoliciesForPrincipalErr
}
func (r *errRepository) CreatePolicy(_ context.Context, p rbac.Policy) (*rbac.Policy, error) {
	return &p, nil
}
func (r *errRepository) DeletePolicy(_ context.Context, _ int64) error {
	return nil
}
func (r *errRepository) ListUnassignedSourceIDs(_ context.Context) ([]string, error) {
	return nil, nil
}

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

	if !engine.IsAllowed(ctx, rbac.NewPrincipalSet("alice"), "k8s-prod", rbac.ActionRead) {
		t.Error("alice should be able to read prod-readonly source")
	}
	if !engine.IsAllowed(ctx, rbac.NewPrincipalSet("alice"), "k8s-prod", rbac.ActionQuery) {
		t.Error("alice should be able to query prod-readonly source")
	}
	if engine.IsAllowed(ctx, rbac.NewPrincipalSet("alice"), "k8s-prod", rbac.ActionMutate) {
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

	if !engine.IsAllowed(ctx, rbac.NewPrincipalSet("bob"), "k8s-prod", rbac.ActionMutate) {
		t.Error("bob should be able to mutate prod-write source")
	}
	if engine.IsAllowed(ctx, rbac.NewPrincipalSet("bob"), "k8s-prod", rbac.ActionDelete) {
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

	if !engine.IsAllowed(ctx, rbac.NewPrincipalSet("charlie"), "k8s-dev", rbac.ActionRead) {
		t.Error("charlie should be able to read unassigned source")
	}
	if engine.IsAllowed(ctx, rbac.NewPrincipalSet("charlie"), "k8s-dev", rbac.ActionMutate) {
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

	if engine.IsAllowed(ctx, rbac.NewPrincipalSet("dave"), "k8s-prod", rbac.ActionRead) {
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
		if !engine.IsAllowed(ctx, rbac.NewPrincipalSet("eve"), "k8s-dev", action) {
			t.Errorf("eve should be allowed action %q in dev-full zone", action)
		}
	}
}

// TestPolicyEngine_IsAllowed_ZoneNotFound covers the path where a source is
// assigned to a zone ID that does not exist in the security_zones table. The
// engine must deny access (zone == nil branch in IsAllowed).
func TestPolicyEngine_IsAllowed_ZoneNotFound(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	// Insert a source and a direct assignment to a zone that does not exist.
	_, err := db.Exec(`INSERT INTO sources VALUES ('orphan-src', 'Orphan Source')`)
	if err != nil {
		t.Fatalf("insert source: %v", err)
	}
	// Bypass the FK constraint (SQLite does not enforce FKs by default) to put
	// the source into a non-existent zone.
	_, err = db.Exec(`
		INSERT INTO source_zone_assignments (source_id, zone_id, assigned_by, reason, assigned_at)
		VALUES ('orphan-src', 'ghost-zone', 'test', '', '2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert assignment: %v", err)
	}
	// Give the principal a policy for the ghost zone (also bypassing FK).
	_, err = db.Exec(`
		INSERT INTO rbac_policies (principal, zone_id, created_at)
		VALUES ('frank', 'ghost-zone', '2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert policy: %v", err)
	}

	engine := rbac.NewPolicyEngine(repo)

	// Even though frank has a policy for ghost-zone, the zone doesn't exist so
	// GetZone returns nil and IsAllowed must deny.
	if engine.IsAllowed(ctx, rbac.NewPrincipalSet("frank"), "orphan-src", rbac.ActionRead) {
		t.Error("expected denial when source zone does not exist in security_zones")
	}
}

// TestPolicyEngine_IsAllowed_PrincipalHasPolicyButZoneActionDenied ensures the
// action-check branch is exercised: principal has a valid policy for the zone
// but the zone does not allow the requested action.
func TestPolicyEngine_IsAllowed_ActionNotInZone(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	_ = repo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{
		SourceID: "k8s-prod", ZoneID: "prod-readonly", AssignedBy: "test",
	})
	_, _ = repo.CreatePolicy(ctx, rbac.Policy{Principal: "grace", ZoneID: "prod-readonly"})

	engine := rbac.NewPolicyEngine(repo)

	// prod-readonly only allows read/query; delete is denied at the zone level.
	if engine.IsAllowed(ctx, rbac.NewPrincipalSet("grace"), "k8s-prod", rbac.ActionDelete) {
		t.Error("grace should NOT be able to delete in prod-readonly zone")
	}
}

// TestPolicyEngine_IsAllowed_GetAssignmentError covers the branch in IsAllowed
// where GetAssignment returns an error (defaults to "unassigned" zone).
func TestPolicyEngine_IsAllowed_GetAssignmentError(t *testing.T) {
	ctx := context.Background()

	repo := &errRepository{
		getAssignmentErr: errors.New("db connection lost"),
		// When assignment lookup fails, engine defaults to "unassigned" zone.
		// GetZone will then be called with "unassigned" — return nil to deny.
		getZoneResult: nil,
	}

	engine := rbac.NewPolicyEngine(repo)

	// GetAssignment errors → engine logs warning + falls back to unassigned zone.
	// GetZone("unassigned") returns nil → deny.
	if engine.IsAllowed(ctx, rbac.NewPrincipalSet("alice"), "some-source", rbac.ActionRead) {
		t.Error("expected denial when GetAssignment errors and zone lookup returns nil")
	}
}

// TestPolicyEngine_IsAllowed_ListPoliciesError covers the branch in IsAllowed
// where ListPoliciesForPrincipal returns an error → must deny.
func TestPolicyEngine_IsAllowed_ListPoliciesError(t *testing.T) {
	ctx := context.Background()

	repo := &errRepository{
		// No assignment error — source defaults to unassigned.
		getAssignmentResult: nil,
		// Zone exists and allows the action.
		getZoneResult: &rbac.Zone{
			ID:             "unassigned",
			Name:           "Unassigned",
			AllowedActions: []rbac.Action{rbac.ActionRead},
		},
		// But listing policies fails.
		listPoliciesForPrincipalErr: errors.New("policy table locked"),
	}

	engine := rbac.NewPolicyEngine(repo)

	if engine.IsAllowed(ctx, rbac.NewPrincipalSet("alice"), "some-source", rbac.ActionRead) {
		t.Error("expected denial when ListPoliciesForPrincipal errors")
	}
}

// TestPolicyEngine_IsAllowed_SetSingleMember is the Phase B behavioural
// contract for the size-1 case actually used in production: a set whose single
// member has a matching grant is allowed; a set whose single member lacks the
// grant is denied (docs/joe-identity-design.md §2.7).
func TestPolicyEngine_IsAllowed_SetSingleMember(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	_ = repo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{
		SourceID: "k8s-prod", ZoneID: "prod-readonly", AssignedBy: "test",
	})
	_, _ = repo.CreatePolicy(ctx, rbac.Policy{Principal: "alice", ZoneID: "prod-readonly"})

	engine := rbac.NewPolicyEngine(repo)

	if !engine.IsAllowed(ctx, rbac.NewPrincipalSet("alice"), "k8s-prod", rbac.ActionRead) {
		t.Error("size-1 set whose member is granted should be allowed")
	}
	if engine.IsAllowed(ctx, rbac.NewPrincipalSet("mallory"), "k8s-prod", rbac.ActionRead) {
		t.Error("size-1 set whose member lacks the grant should be denied")
	}
}

// TestPolicyEngine_IsAllowed_SetUnion proves the union-of-grants semantics that
// production does not yet exercise (size 1 at launch) but the design builds now
// so group: members drop in later (§2.7, §6): a multi-member set is permitted
// if ANY member holds the grant, denied if none do, and the empty set is denied.
func TestPolicyEngine_IsAllowed_SetUnion(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	_ = repo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{
		SourceID: "k8s-prod", ZoneID: "prod-readonly", AssignedBy: "test",
	})
	// Only alice is granted; bob and mallory are not.
	_, _ = repo.CreatePolicy(ctx, rbac.Policy{Principal: "alice", ZoneID: "prod-readonly"})

	engine := rbac.NewPolicyEngine(repo)

	// ANY granted member ⇒ allow (alice is granted, position in the set is
	// irrelevant to the union decision).
	if !engine.IsAllowed(ctx, rbac.NewPrincipalSet("mallory", "alice", "bob"), "k8s-prod", rbac.ActionRead) {
		t.Error("set with any granted member should be allowed (union of grants)")
	}
	// No member granted ⇒ deny.
	if engine.IsAllowed(ctx, rbac.NewPrincipalSet("mallory", "bob"), "k8s-prod", rbac.ActionRead) {
		t.Error("set with no granted member should be denied")
	}
	// Empty subject ⇒ deny (no member can satisfy a grant).
	if engine.IsAllowed(ctx, rbac.NewPrincipalSet(), "k8s-prod", rbac.ActionRead) {
		t.Error("empty principal set should be denied")
	}
	// Union is still bounded by the zone's allowed actions: prod-readonly
	// forbids mutate for everyone, granted or not.
	if engine.IsAllowed(ctx, rbac.NewPrincipalSet("alice", "bob"), "k8s-prod", rbac.ActionMutate) {
		t.Error("union must not exceed the zone's allowed actions")
	}
}
