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
	// Phase H: admin status. isAdminErr exists so a test can exercise
	// the warn-and-fallback branch in Decide/HasZoneAccess; isAdminResult
	// is the value returned when err is nil.
	isAdminErr    error
	isAdminResult bool
}

func (r *errRepository) ListZones(_ context.Context) ([]rbac.Zone, error) {
	return nil, nil
}
func (r *errRepository) GetZone(_ context.Context, _ string) (*rbac.Zone, error) {
	return r.getZoneResult, r.getZoneErr
}
func (r *errRepository) CreateZone(_ context.Context, z rbac.Zone, _ string) (*rbac.Zone, error) {
	return &z, nil
}
func (r *errRepository) UpdateZone(_ context.Context, z rbac.Zone, _ string) (*rbac.Zone, error) {
	return &z, nil
}
func (r *errRepository) DeleteZone(_ context.Context, _ string, _ string) error {
	return nil
}
func (r *errRepository) ListAssignments(_ context.Context) ([]rbac.SourceZoneAssignment, error) {
	return nil, nil
}
func (r *errRepository) GetAssignment(_ context.Context, _ string) (*rbac.SourceZoneAssignment, error) {
	return r.getAssignmentResult, r.getAssignmentErr
}
func (r *errRepository) UpsertAssignment(_ context.Context, _ rbac.SourceZoneAssignment, _ string) error {
	return nil
}
func (r *errRepository) DeleteAssignment(_ context.Context, _ string, _ string) (int64, error) {
	return 0, nil
}
func (r *errRepository) ListPolicies(_ context.Context) ([]rbac.Policy, error) {
	return nil, nil
}
func (r *errRepository) ListPoliciesForPrincipal(_ context.Context, _ string) ([]rbac.Policy, error) {
	return r.listPoliciesResult, r.listPoliciesForPrincipalErr
}
func (r *errRepository) CreatePolicy(_ context.Context, p rbac.Policy, _ string) (*rbac.Policy, error) {
	return &p, nil
}
func (r *errRepository) DeletePolicy(_ context.Context, _ int64, _ string) error {
	return nil
}
func (r *errRepository) DeletePolicyForPrincipalZone(_ context.Context, _, _ string, _ string) (int64, error) {
	return 0, nil
}
func (r *errRepository) ListUnassignedSourceIDs(_ context.Context) ([]string, error) {
	return nil, nil
}
func (r *errRepository) DeletePoliciesForPrincipal(_ context.Context, _ string) (int64, error) {
	return 0, nil
}

// --- Admin status (Phase H) ---

func (r *errRepository) IsAdmin(_ context.Context, _ string) (bool, error) {
	return r.isAdminResult, r.isAdminErr
}
func (r *errRepository) ListAdmins(_ context.Context) ([]rbac.Admin, error) {
	return nil, nil
}
func (r *errRepository) AddAdmin(_ context.Context, _ rbac.Admin, _ string) error {
	return nil
}
func (r *errRepository) RemoveAdmin(_ context.Context, _ string, _ string) (int64, error) {
	return 0, nil
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
		-- Phase H: admin_principals mirrors migration 016.
		CREATE TABLE admin_principals (
			principal  TEXT PRIMARY KEY,
			granted_at TEXT NOT NULL,
			granted_by TEXT NOT NULL DEFAULT '',
			reason     TEXT NOT NULL DEFAULT ''
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
	}, "test")

	// Grant alice access to prod-readonly.
	_, _ = repo.CreatePolicy(ctx, rbac.Policy{Principal: "alice", ZoneID: "prod-readonly"}, "test")

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
	}, "test")
	_, _ = repo.CreatePolicy(ctx, rbac.Policy{Principal: "bob", ZoneID: "prod-write"}, "test")

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
	_, _ = repo.CreatePolicy(ctx, rbac.Policy{Principal: "charlie", ZoneID: "unassigned"}, "test")

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
	}, "test")
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
	}, "test")
	_, _ = repo.CreatePolicy(ctx, rbac.Policy{Principal: "eve", ZoneID: "dev-full"}, "test")

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
	}, "test")
	_, _ = repo.CreatePolicy(ctx, rbac.Policy{Principal: "grace", ZoneID: "prod-readonly"}, "test")

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
	}, "test")
	_, _ = repo.CreatePolicy(ctx, rbac.Policy{Principal: "alice", ZoneID: "prod-readonly"}, "test")

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
	}, "test")
	// Only alice is granted; bob and mallory are not.
	_, _ = repo.CreatePolicy(ctx, rbac.Policy{Principal: "alice", ZoneID: "prod-readonly"}, "test")

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

// seedRegimeControlZone adds the sourceless regime-control zone (which
// migration 012 creates in production) to openTestDB's bare schema.
// The zone allows declare_incident and resolve_incident only — these
// are sourceless capabilities used by the regime/captain path that
// HasZoneAccess gates.
func seedRegimeControlZone(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO security_zones VALUES (
		'regime-control','Regime Control','sourceless declare/resolve',
		'["declare_incident","resolve_incident"]','2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("seed regime-control: %v", err)
	}
}

// TestPolicyEngine_HasZoneAccess_SetSingleMember is the Phase G size-1
// behavioural contract for the sourceless authorization path: a set
// whose single member holds the zone grant is allowed; a set whose
// single member lacks the grant is denied. This is the production case
// — incident declare/resolve build a one-element set from the caller's
// ctx principal — and the outcome must be identical to the
// pre-Phase-G single-principal call (D-0010).
func TestPolicyEngine_HasZoneAccess_SetSingleMember(t *testing.T) {
	db := openTestDB(t)
	seedRegimeControlZone(t, db)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	_, _ = repo.CreatePolicy(ctx, rbac.Policy{Principal: "alice", ZoneID: "regime-control"}, "test")

	engine := rbac.NewPolicyEngine(repo)

	if !engine.HasZoneAccess(ctx, rbac.NewPrincipalSet("alice"), "regime-control", rbac.ActionDeclareIncident) {
		t.Error("size-1 set whose member is granted should be allowed")
	}
	if engine.HasZoneAccess(ctx, rbac.NewPrincipalSet("mallory"), "regime-control", rbac.ActionDeclareIncident) {
		t.Error("size-1 set whose member lacks the grant should be denied")
	}
}

// TestPolicyEngine_HasZoneAccess_SetUnion is the Phase G forward-looking
// multi-member contract: HasZoneAccess permits if ANY member of the set
// holds the grant, denies if none do, denies the empty set, and stays
// bounded by the zone's allowed actions (no action_not_in_zone widening
// via union). Mirrors the equivalent test for IsAllowed so the
// sourceless path is on the same multi-principal footing as the
// source-keyed path (D-0010, §2.7 + §2.10).
func TestPolicyEngine_HasZoneAccess_SetUnion(t *testing.T) {
	db := openTestDB(t)
	seedRegimeControlZone(t, db)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	// Only alice is granted.
	_, _ = repo.CreatePolicy(ctx, rbac.Policy{Principal: "alice", ZoneID: "regime-control"}, "test")

	engine := rbac.NewPolicyEngine(repo)

	if !engine.HasZoneAccess(ctx, rbac.NewPrincipalSet("mallory", "alice", "bob"), "regime-control", rbac.ActionDeclareIncident) {
		t.Error("set with any granted member should be allowed (union of grants)")
	}
	if engine.HasZoneAccess(ctx, rbac.NewPrincipalSet("mallory", "bob"), "regime-control", rbac.ActionDeclareIncident) {
		t.Error("set with no granted member should be denied")
	}
	if engine.HasZoneAccess(ctx, rbac.NewPrincipalSet(), "regime-control", rbac.ActionDeclareIncident) {
		t.Error("empty principal set should be denied")
	}
	// Union must not exceed the zone's allowed_actions: regime-control
	// does not allow ActionRead, so even a granted member is denied.
	if engine.HasZoneAccess(ctx, rbac.NewPrincipalSet("alice"), "regime-control", rbac.ActionRead) {
		t.Error("union must not exceed the zone's allowed actions")
	}
}

// --- Phase H: dynamic admin capability (docs/DECISIONS.md D-0011) ---

// TestPhaseH_AdminAllowedOnZoneCreatedAfterDesignation is the bug-fix
// demonstration. Pre-Phase-H, admin authority was a snapshot of grants
// captured at bootstrap; any zone created AFTER bootstrap was silently
// uncovered (a day-100 correctness gap). Phase H evaluates admin
// capability dynamically at decision time, so a zone created later is
// covered automatically without a re-snapshot.
//
// Pre-Phase-H this test would FAIL (the admin holds no rbac_policies row
// for the new zone, so IsAllowed returns false). Post-Phase-H it PASSES
// because Decide short-circuits to ReasonAdminCapability the moment it
// sees IsAdmin=true, regardless of when the zone was created.
func TestPhaseH_AdminAllowedOnZoneCreatedAfterDesignation(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	// Designate alice as dynamic admin BEFORE the new zone exists. No
	// rbac_policies row is created — admin status is the sole basis.
	if err := repo.AddAdmin(ctx, rbac.Admin{Principal: "alice", GrantedBy: "test"}, "test"); err != nil {
		t.Fatalf("add admin: %v", err)
	}

	// NOW create a brand-new zone. This is the day-100 case the bug
	// described: an operator adds a zone months after admin was
	// designated.
	if _, err := repo.CreateZone(ctx, rbac.Zone{
		ID:             "post-bootstrap-zone",
		Name:           "Late Zone",
		AllowedActions: []rbac.Action{rbac.ActionRead, rbac.ActionMutate},
	}, "test"); err != nil {
		t.Fatalf("create zone: %v", err)
	}
	if err := repo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{
		SourceID: "k8s-prod", ZoneID: "post-bootstrap-zone", AssignedBy: "test",
	}, "test"); err != nil {
		t.Fatalf("upsert assignment: %v", err)
	}

	engine := rbac.NewPolicyEngine(repo)

	// Admin must be allowed on the new zone without any per-zone grant.
	for _, action := range []rbac.Action{rbac.ActionRead, rbac.ActionMutate} {
		d := engine.Decide(ctx, rbac.NewPrincipalSet("alice"), "k8s-prod", action)
		if !d.Allowed {
			t.Errorf("Phase H bug fix: admin must be allowed action %q on zone %q created after designation; got deny reason=%q",
				action, d.Zone, d.Reason)
		}
		if d.Reason != rbac.ReasonAdminCapability {
			t.Errorf("Phase H: admin allow on new zone should record reason=%q, got %q",
				rbac.ReasonAdminCapability, d.Reason)
		}
	}

	// Confirm via the repository that admin still holds ZERO per-zone
	// grants — admin authority has one source of truth.
	grants, _ := repo.ListPoliciesForPrincipal(ctx, "alice")
	if len(grants) != 0 {
		t.Errorf("Phase H: admin authority must come from admin_principals only, but found %d rbac_policies rows", len(grants))
	}
}

// TestPhaseH_AdminAllowedAcrossMultipleZonesWithoutGrants asserts the
// admin capability spans every existing zone+action the zones themselves
// permit, with NO rbac_policies rows present for the admin. Differs from
// the bug-fix test in that all four seeded zones exist before
// designation; the focus here is the breadth (every zone) rather than
// the temporal gap.
func TestPhaseH_AdminAllowedAcrossMultipleZonesWithoutGrants(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	// Pre-seed every source onto a distinct zone (all four are seeded by
	// openTestDB).
	_ = repo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{SourceID: "k8s-prod", ZoneID: "prod-write", AssignedBy: "test"}, "test")
	_ = repo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{SourceID: "k8s-dev", ZoneID: "dev-full", AssignedBy: "test"}, "test")

	if err := repo.AddAdmin(ctx, rbac.Admin{Principal: "alice", GrantedBy: "test"}, "test"); err != nil {
		t.Fatalf("add admin: %v", err)
	}

	engine := rbac.NewPolicyEngine(repo)

	cases := []struct {
		source string
		action rbac.Action
		want   bool // bounded by zone's allowed_actions
	}{
		// prod-write allows read/query/mutate but not delete.
		{"k8s-prod", rbac.ActionRead, true},
		{"k8s-prod", rbac.ActionQuery, true},
		{"k8s-prod", rbac.ActionMutate, true},
		{"k8s-prod", rbac.ActionDelete, false}, // admin does NOT bypass allowed_actions
		// dev-full allows everything.
		{"k8s-dev", rbac.ActionRead, true},
		{"k8s-dev", rbac.ActionMutate, true},
		{"k8s-dev", rbac.ActionDelete, true},
	}

	for _, c := range cases {
		d := engine.Decide(ctx, rbac.NewPrincipalSet("alice"), c.source, c.action)
		if d.Allowed != c.want {
			t.Errorf("Phase H admin on (%s, %s): got allowed=%v reason=%q, want allowed=%v",
				c.source, c.action, d.Allowed, d.Reason, c.want)
		}
		if c.want && d.Reason != rbac.ReasonAdminCapability {
			t.Errorf("Phase H admin allow on (%s, %s): reason=%q, want %q",
				c.source, c.action, d.Reason, rbac.ReasonAdminCapability)
		}
		if !c.want && d.Reason != rbac.ReasonActionNotInZone {
			t.Errorf("Phase H admin deny on (%s, %s): reason=%q, want %q (admin must NOT widen allowed_actions)",
				c.source, c.action, d.Reason, rbac.ReasonActionNotInZone)
		}
	}

	// No rbac_policies grants exist for the admin — single source of
	// truth.
	grants, _ := repo.ListPoliciesForPrincipal(ctx, "alice")
	if len(grants) != 0 {
		t.Errorf("Phase H: admin must hold zero rbac_policies rows, got %d", len(grants))
	}
}

// TestPhaseH_NonAdminOutcomesUnchanged is the post-Phase-G regression
// guarantee: introducing admin capability must not change ANY non-admin
// allow/deny outcome. A non-admin still goes through the per-zone grant
// path with reason policy_allow / no_grant / action_not_in_zone exactly
// as pre-Phase-H.
func TestPhaseH_NonAdminOutcomesUnchanged(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	_ = repo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{SourceID: "k8s-prod", ZoneID: "prod-readonly", AssignedBy: "test"}, "test")
	_, _ = repo.CreatePolicy(ctx, rbac.Policy{Principal: "alice", ZoneID: "prod-readonly"}, "test")

	// An admin also exists in the same DB — its existence must not
	// affect non-admin decisions either way.
	if err := repo.AddAdmin(ctx, rbac.Admin{Principal: "root", GrantedBy: "test"}, "test"); err != nil {
		t.Fatalf("add admin: %v", err)
	}

	engine := rbac.NewPolicyEngine(repo)

	// alice is granted (non-admin); the reason MUST be policy_allow,
	// not admin_capability.
	d := engine.Decide(ctx, rbac.NewPrincipalSet("alice"), "k8s-prod", rbac.ActionRead)
	if !d.Allowed || d.Reason != rbac.ReasonPolicyAllow {
		t.Errorf("non-admin allow: got allowed=%v reason=%q, want true/%q",
			d.Allowed, d.Reason, rbac.ReasonPolicyAllow)
	}

	// mallory is ungranted (non-admin); deny with no_grant.
	d = engine.Decide(ctx, rbac.NewPrincipalSet("mallory"), "k8s-prod", rbac.ActionRead)
	if d.Allowed || d.Reason != rbac.ReasonNoGrant {
		t.Errorf("non-admin ungranted deny: got allowed=%v reason=%q, want false/%q",
			d.Allowed, d.Reason, rbac.ReasonNoGrant)
	}

	// alice attempts a denied action in-zone; deny with action_not_in_zone.
	d = engine.Decide(ctx, rbac.NewPrincipalSet("alice"), "k8s-prod", rbac.ActionDelete)
	if d.Allowed || d.Reason != rbac.ReasonActionNotInZone {
		t.Errorf("non-admin action-not-in-zone deny: got allowed=%v reason=%q, want false/%q",
			d.Allowed, d.Reason, rbac.ReasonActionNotInZone)
	}
}

// TestPhaseH_AdminDecisionReasonIsDistinct asserts the audit-trail
// distinguishability the prompt requires (Phase H req 5): an admin allow
// records reason=admin_capability; an ordinary zone-grant allow records
// reason=policy_allow. The two are distinguishable for any downstream
// audit query.
func TestPhaseH_AdminDecisionReasonIsDistinct(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	_ = repo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{SourceID: "k8s-prod", ZoneID: "prod-readonly", AssignedBy: "test"}, "test")
	_, _ = repo.CreatePolicy(ctx, rbac.Policy{Principal: "alice", ZoneID: "prod-readonly"}, "test")
	if err := repo.AddAdmin(ctx, rbac.Admin{Principal: "root", GrantedBy: "test"}, "test"); err != nil {
		t.Fatalf("add admin: %v", err)
	}

	engine := rbac.NewPolicyEngine(repo)

	adminD := engine.Decide(ctx, rbac.NewPrincipalSet("root"), "k8s-prod", rbac.ActionRead)
	zoneD := engine.Decide(ctx, rbac.NewPrincipalSet("alice"), "k8s-prod", rbac.ActionRead)

	if !adminD.Allowed || adminD.Reason != rbac.ReasonAdminCapability {
		t.Errorf("admin allow: got allowed=%v reason=%q, want true/%q",
			adminD.Allowed, adminD.Reason, rbac.ReasonAdminCapability)
	}
	if !zoneD.Allowed || zoneD.Reason != rbac.ReasonPolicyAllow {
		t.Errorf("zone-grant allow: got allowed=%v reason=%q, want true/%q",
			zoneD.Allowed, zoneD.Reason, rbac.ReasonPolicyAllow)
	}
	if adminD.Reason == zoneD.Reason {
		t.Error("admin reason must differ from zone-grant reason for audit-trail distinguishability")
	}
}

// TestPhaseH_HasZoneAccessAdminCapability covers the sourceless path:
// the admin short-circuit applies to HasZoneAccess too, so an admin can
// declare/resolve incidents without a regime-control grant.
func TestPhaseH_HasZoneAccessAdminCapability(t *testing.T) {
	db := openTestDB(t)
	seedRegimeControlZone(t, db)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	if err := repo.AddAdmin(ctx, rbac.Admin{Principal: "root", GrantedBy: "test"}, "test"); err != nil {
		t.Fatalf("add admin: %v", err)
	}
	engine := rbac.NewPolicyEngine(repo)

	if !engine.HasZoneAccess(ctx, rbac.NewPrincipalSet("root"), "regime-control", rbac.ActionDeclareIncident) {
		t.Error("admin should be allowed regime-control declare without a per-zone grant")
	}
	// Still bounded by zone's allowed_actions: regime-control does not
	// list "read", so even an admin is denied that.
	if engine.HasZoneAccess(ctx, rbac.NewPrincipalSet("root"), "regime-control", rbac.ActionRead) {
		t.Error("admin must NOT bypass the zone's allowed_actions list")
	}
	// Non-admin without a grant is still denied.
	if engine.HasZoneAccess(ctx, rbac.NewPrincipalSet("mallory"), "regime-control", rbac.ActionDeclareIncident) {
		t.Error("non-admin without a grant must remain denied")
	}
}

// TestPhaseH_AdminIsAdminErrorFallsBackToGrant covers the warn-and-fall-
// through branch in Decide. If the admin lookup errors for a principal,
// the engine continues to the per-zone grant path rather than denying
// outright — preserving availability of normal RBAC when the admin
// store is degraded.
func TestPhaseH_AdminIsAdminErrorFallsBackToGrant(t *testing.T) {
	ctx := context.Background()

	repo := &errRepository{
		getAssignmentResult: nil, // defaults to unassigned
		getZoneResult: &rbac.Zone{
			ID:             "unassigned",
			AllowedActions: []rbac.Action{rbac.ActionRead},
		},
		isAdminErr: errors.New("admin store unhealthy"),
		listPoliciesResult: []rbac.Policy{
			{Principal: "alice", ZoneID: "unassigned"},
		},
	}

	engine := rbac.NewPolicyEngine(repo)

	// alice would have been allowed via her grant either way, but the
	// reason must be policy_allow — not admin_capability — because the
	// admin lookup errored and was skipped.
	d := engine.Decide(ctx, rbac.NewPrincipalSet("alice"), "k8s-prod", rbac.ActionRead)
	if !d.Allowed || d.Reason != rbac.ReasonPolicyAllow {
		t.Errorf("fallback path: got allowed=%v reason=%q, want true/%q",
			d.Allowed, d.Reason, rbac.ReasonPolicyAllow)
	}
}
