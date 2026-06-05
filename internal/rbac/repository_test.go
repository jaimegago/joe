package rbac_test

import (
	"context"
	"testing"

	"github.com/jaimegago/joe/internal/rbac"
)

// Uses openTestDB from policy_test.go (same package, same test binary).

func TestSQLRepository_ListZones(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	zones, err := repo.ListZones(ctx)
	if err != nil {
		t.Fatalf("ListZones: %v", err)
	}
	if len(zones) != 4 {
		t.Errorf("got %d zones, want 4 (the 4 seeded defaults)", len(zones))
	}
}

func TestSQLRepository_GetZone(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	t.Run("existing zone", func(t *testing.T) {
		z, err := repo.GetZone(ctx, "prod-readonly")
		if err != nil {
			t.Fatalf("GetZone: %v", err)
		}
		if z == nil {
			t.Fatal("GetZone returned nil for existing zone")
		}
		if z.ID != "prod-readonly" {
			t.Errorf("ID = %q, want %q", z.ID, "prod-readonly")
		}
		if len(z.AllowedActions) == 0 {
			t.Error("AllowedActions should not be empty")
		}
	})

	t.Run("nonexistent zone returns nil", func(t *testing.T) {
		z, err := repo.GetZone(ctx, "does-not-exist")
		if err != nil {
			t.Fatalf("GetZone: %v", err)
		}
		if z != nil {
			t.Errorf("expected nil for nonexistent zone, got %+v", z)
		}
	})
}

func TestSQLRepository_CreateZone(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	newZone := rbac.Zone{
		ID:             "staging-readonly",
		Name:           "Staging Read-Only",
		Description:    "Staging environment, read access only",
		AllowedActions: []rbac.Action{rbac.ActionRead, rbac.ActionQuery},
	}

	created, err := repo.CreateZone(ctx, newZone, "test")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	if created.ID != newZone.ID {
		t.Errorf("ID = %q, want %q", created.ID, newZone.ID)
	}

	// Verify it's retrievable
	got, err := repo.GetZone(ctx, newZone.ID)
	if err != nil {
		t.Fatalf("GetZone after create: %v", err)
	}
	if got == nil {
		t.Fatal("zone not found after create")
	}
	if got.Name != newZone.Name {
		t.Errorf("Name = %q, want %q", got.Name, newZone.Name)
	}

	// Verify zones list grows
	zones, err := repo.ListZones(ctx)
	if err != nil {
		t.Fatalf("ListZones: %v", err)
	}
	if len(zones) != 5 {
		t.Errorf("got %d zones, want 5 after creating one", len(zones))
	}
}

func TestSQLRepository_UpsertAssignment(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	a := rbac.SourceZoneAssignment{
		SourceID:   "k8s-prod",
		ZoneID:     "prod-readonly",
		AssignedBy: "admin",
		Reason:     "initial assignment",
	}

	// First upsert
	if err := repo.UpsertAssignment(ctx, a, "test"); err != nil {
		t.Fatalf("UpsertAssignment: %v", err)
	}

	got, err := repo.GetAssignment(ctx, "k8s-prod")
	if err != nil {
		t.Fatalf("GetAssignment: %v", err)
	}
	if got == nil {
		t.Fatal("assignment not found after upsert")
	}
	if got.ZoneID != "prod-readonly" {
		t.Errorf("ZoneID = %q, want %q", got.ZoneID, "prod-readonly")
	}

	// Second upsert (update)
	a.ZoneID = "prod-write"
	a.Reason = "escalated"
	if err := repo.UpsertAssignment(ctx, a, "test"); err != nil {
		t.Fatalf("UpsertAssignment (update): %v", err)
	}
	got, err = repo.GetAssignment(ctx, "k8s-prod")
	if err != nil {
		t.Fatalf("GetAssignment after update: %v", err)
	}
	if got.ZoneID != "prod-write" {
		t.Errorf("ZoneID after update = %q, want %q", got.ZoneID, "prod-write")
	}
}

func TestSQLRepository_GetAssignment_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	got, err := repo.GetAssignment(ctx, "no-such-source")
	if err != nil {
		t.Fatalf("GetAssignment: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for unassigned source, got %+v", got)
	}
}

func TestSQLRepository_ListAssignments(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	// Initially no assignments
	initial, err := repo.ListAssignments(ctx)
	if err != nil {
		t.Fatalf("ListAssignments: %v", err)
	}
	initialCount := len(initial)

	// Assign k8s-prod
	if err := repo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{
		SourceID: "k8s-prod", ZoneID: "prod-readonly", AssignedBy: "admin",
	}, "test"); err != nil {
		t.Fatalf("UpsertAssignment: %v", err)
	}

	assignments, err := repo.ListAssignments(ctx)
	if err != nil {
		t.Fatalf("ListAssignments: %v", err)
	}
	if len(assignments) != initialCount+1 {
		t.Errorf("got %d assignments, want %d", len(assignments), initialCount+1)
	}
}

func TestSQLRepository_CreatePolicy(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	p := rbac.Policy{
		Principal: "alice",
		ZoneID:    "prod-readonly",
	}

	created, err := repo.CreatePolicy(ctx, p, "test")
	if err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}
	if created.ID == 0 {
		t.Error("CreatePolicy should set the auto-incremented ID")
	}
	if created.Principal != p.Principal {
		t.Errorf("Principal = %q, want %q", created.Principal, p.Principal)
	}
}

func TestSQLRepository_ListPolicies(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	// Create two policies
	for _, principal := range []string{"alice", "bob"} {
		if _, err := repo.CreatePolicy(ctx, rbac.Policy{
			Principal: principal, ZoneID: "prod-readonly",
		}, "test"); err != nil {
			t.Fatalf("CreatePolicy %s: %v", principal, err)
		}
	}

	policies, err := repo.ListPolicies(ctx)
	if err != nil {
		t.Fatalf("ListPolicies: %v", err)
	}
	if len(policies) != 2 {
		t.Errorf("got %d policies, want 2", len(policies))
	}
}

func TestSQLRepository_ListPoliciesForPrincipal(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	// alice gets two zones, bob gets one
	for _, zone := range []string{"prod-readonly", "dev-full"} {
		if _, err := repo.CreatePolicy(ctx, rbac.Policy{
			Principal: "alice", ZoneID: zone,
		}, "test"); err != nil {
			t.Fatalf("CreatePolicy alice/%s: %v", zone, err)
		}
	}
	if _, err := repo.CreatePolicy(ctx, rbac.Policy{
		Principal: "bob", ZoneID: "dev-full",
	}, "test"); err != nil {
		t.Fatalf("CreatePolicy bob: %v", err)
	}

	alice, err := repo.ListPoliciesForPrincipal(ctx, "alice")
	if err != nil {
		t.Fatalf("ListPoliciesForPrincipal(alice): %v", err)
	}
	if len(alice) != 2 {
		t.Errorf("alice policies = %d, want 2", len(alice))
	}

	bob, err := repo.ListPoliciesForPrincipal(ctx, "bob")
	if err != nil {
		t.Fatalf("ListPoliciesForPrincipal(bob): %v", err)
	}
	if len(bob) != 1 {
		t.Errorf("bob policies = %d, want 1", len(bob))
	}

	nobody, err := repo.ListPoliciesForPrincipal(ctx, "nobody")
	if err != nil {
		t.Fatalf("ListPoliciesForPrincipal(nobody): %v", err)
	}
	if len(nobody) != 0 {
		t.Errorf("nobody policies = %d, want 0", len(nobody))
	}
}

func TestSQLRepository_DeletePolicy(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	created, err := repo.CreatePolicy(ctx, rbac.Policy{
		Principal: "carol", ZoneID: "prod-readonly",
	}, "test")
	if err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}

	if err := repo.DeletePolicy(ctx, created.ID, "test"); err != nil {
		t.Fatalf("DeletePolicy: %v", err)
	}

	policies, err := repo.ListPolicies(ctx)
	if err != nil {
		t.Fatalf("ListPolicies: %v", err)
	}
	for _, p := range policies {
		if p.ID == created.ID {
			t.Error("policy still present after delete")
		}
	}
}

func TestSQLRepository_ListUnassignedSourceIDs(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	// Both seeded sources (k8s-prod, k8s-dev) are initially unassigned
	unassigned, err := repo.ListUnassignedSourceIDs(ctx)
	if err != nil {
		t.Fatalf("ListUnassignedSourceIDs: %v", err)
	}
	if len(unassigned) != 2 {
		t.Errorf("got %d unassigned sources, want 2", len(unassigned))
	}

	// Assign k8s-prod → should drop to 1
	if err := repo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{
		SourceID: "k8s-prod", ZoneID: "prod-readonly", AssignedBy: "admin",
	}, "test"); err != nil {
		t.Fatalf("UpsertAssignment: %v", err)
	}

	unassigned, err = repo.ListUnassignedSourceIDs(ctx)
	if err != nil {
		t.Fatalf("ListUnassignedSourceIDs after assign: %v", err)
	}
	if len(unassigned) != 1 {
		t.Errorf("got %d unassigned sources after assigning one, want 1", len(unassigned))
	}
	if len(unassigned) > 0 && unassigned[0] != "k8s-dev" {
		t.Errorf("remaining unassigned = %q, want %q", unassigned[0], "k8s-dev")
	}
}

// TestSQLRepository_ListZones_ScanRows verifies that all rows returned by
// ListZones are scanned correctly (exercises the scan-in-loop body).
func TestSQLRepository_ListZones_ScanRows(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	zones, err := repo.ListZones(ctx)
	if err != nil {
		t.Fatalf("ListZones: %v", err)
	}
	// Verify that allowed_actions were unmarshalled correctly for every row.
	for _, z := range zones {
		if len(z.AllowedActions) == 0 {
			t.Errorf("zone %q has no allowed_actions after scan", z.ID)
		}
		if z.CreatedAt.IsZero() {
			t.Errorf("zone %q has zero CreatedAt after scan", z.ID)
		}
	}
}

// TestSQLRepository_CreateZone_VerifyAllowedActions ensures the allowed_actions
// JSON round-trips through CreateZone → GetZone correctly (covers the JSON
// unmarshal branch in CreateZone / GetZone).
func TestSQLRepository_CreateZone_VerifyAllowedActions(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	want := []rbac.Action{rbac.ActionRead, rbac.ActionQuery, rbac.ActionMutate, rbac.ActionDelete}
	_, err := repo.CreateZone(ctx, rbac.Zone{
		ID:             "full-custom",
		Name:           "Full Custom Zone",
		AllowedActions: want,
	}, "test")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}

	got, err := repo.GetZone(ctx, "full-custom")
	if err != nil {
		t.Fatalf("GetZone: %v", err)
	}
	if got == nil {
		t.Fatal("GetZone returned nil")
	}
	if len(got.AllowedActions) != len(want) {
		t.Errorf("AllowedActions len = %d, want %d", len(got.AllowedActions), len(want))
	}
}

// TestSQLRepository_UpsertAssignment_TimestampSet verifies that UpsertAssignment
// auto-populates AssignedAt when it is zero (covers the IsZero branch).
func TestSQLRepository_UpsertAssignment_TimestampSet(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	// AssignedAt is intentionally left zero so the auto-populate branch fires.
	a := rbac.SourceZoneAssignment{
		SourceID:   "k8s-dev",
		ZoneID:     "dev-full",
		AssignedBy: "auto-test",
	}
	if err := repo.UpsertAssignment(ctx, a, "test"); err != nil {
		t.Fatalf("UpsertAssignment: %v", err)
	}

	got, err := repo.GetAssignment(ctx, "k8s-dev")
	if err != nil {
		t.Fatalf("GetAssignment: %v", err)
	}
	if got == nil {
		t.Fatal("assignment not found")
	}
	if got.AssignedAt.IsZero() {
		t.Error("AssignedAt should have been auto-set, but is still zero")
	}
}

// TestSQLRepository_ListAssignments_ScanRows exercises the scan body in
// ListAssignments by verifying field values after inserting two assignments.
func TestSQLRepository_ListAssignments_ScanRows(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	sources := []struct {
		sourceID string
		zoneID   string
	}{
		{"k8s-prod", "prod-readonly"},
		{"k8s-dev", "dev-full"},
	}
	for _, s := range sources {
		if err := repo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{
			SourceID:   s.sourceID,
			ZoneID:     s.zoneID,
			AssignedBy: "admin",
			Reason:     "test",
		}, "test"); err != nil {
			t.Fatalf("UpsertAssignment %s: %v", s.sourceID, err)
		}
	}

	assignments, err := repo.ListAssignments(ctx)
	if err != nil {
		t.Fatalf("ListAssignments: %v", err)
	}
	if len(assignments) != 2 {
		t.Fatalf("got %d assignments, want 2", len(assignments))
	}
	for _, a := range assignments {
		if a.SourceID == "" {
			t.Error("scanned assignment has empty SourceID")
		}
		if a.ZoneID == "" {
			t.Error("scanned assignment has empty ZoneID")
		}
	}
}

// TestSQLRepository_ListPolicies_ScanRows exercises the scan body in
// ListPolicies by verifying that multiple rows are scanned into valid structs.
func TestSQLRepository_ListPolicies_ScanRows(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	principals := []string{"alice", "bob", "carol"}
	for _, p := range principals {
		if _, err := repo.CreatePolicy(ctx, rbac.Policy{
			Principal: p, ZoneID: "prod-readonly",
		}, "test"); err != nil {
			t.Fatalf("CreatePolicy %s: %v", p, err)
		}
	}

	policies, err := repo.ListPolicies(ctx)
	if err != nil {
		t.Fatalf("ListPolicies: %v", err)
	}
	if len(policies) != len(principals) {
		t.Errorf("got %d policies, want %d", len(policies), len(principals))
	}
	for _, p := range policies {
		if p.ID == 0 {
			t.Error("scanned policy has zero ID")
		}
		if p.Principal == "" {
			t.Error("scanned policy has empty Principal")
		}
		if p.CreatedAt.IsZero() {
			t.Error("scanned policy has zero CreatedAt")
		}
	}
}

// TestSQLRepository_ListPoliciesForPrincipal_ScanRows exercises the scan body
// in ListPoliciesForPrincipal when the principal has multiple zone grants.
func TestSQLRepository_ListPoliciesForPrincipal_ScanRows(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	zones := []string{"prod-readonly", "dev-full"}
	for _, z := range zones {
		if _, err := repo.CreatePolicy(ctx, rbac.Policy{
			Principal: "multi-zone-user", ZoneID: z,
		}, "test"); err != nil {
			t.Fatalf("CreatePolicy multi-zone-user/%s: %v", z, err)
		}
	}

	policies, err := repo.ListPoliciesForPrincipal(ctx, "multi-zone-user")
	if err != nil {
		t.Fatalf("ListPoliciesForPrincipal: %v", err)
	}
	if len(policies) != 2 {
		t.Fatalf("got %d policies, want 2", len(policies))
	}
	for _, p := range policies {
		if p.Principal != "multi-zone-user" {
			t.Errorf("unexpected principal %q in result", p.Principal)
		}
		if p.CreatedAt.IsZero() {
			t.Error("scanned policy has zero CreatedAt")
		}
	}
}

// TestSQLRepository_CreatePolicy_TimestampSet verifies the auto-populate branch
// in CreatePolicy when CreatedAt is zero.
func TestSQLRepository_CreatePolicy_TimestampSet(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	// CreatedAt intentionally left zero.
	created, err := repo.CreatePolicy(ctx, rbac.Policy{
		Principal: "timestamp-test-user",
		ZoneID:    "prod-readonly",
	}, "test")
	if err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}
	if created.CreatedAt.IsZero() {
		t.Error("CreatePolicy should auto-set CreatedAt when it is zero")
	}
}

// TestSQLRepository_DeletePolicy_NonExistentID verifies that deleting a policy
// that does not exist returns no error (SQL DELETE is a no-op on missing rows).
func TestSQLRepository_DeletePolicy_NonExistentID(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	// ID 9999 does not exist; ExecContext should succeed (0 rows affected).
	if err := repo.DeletePolicy(ctx, 9999, "test"); err != nil {
		t.Errorf("DeletePolicy for non-existent ID should not error, got: %v", err)
	}
}

// TestSQLRepository_ListUnassignedSourceIDs_AllAssigned verifies the empty-result
// path of ListUnassignedSourceIDs when every source has been assigned.
func TestSQLRepository_ListUnassignedSourceIDs_AllAssigned(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	// Assign both seeded sources.
	for _, s := range []struct{ id, zone string }{
		{"k8s-prod", "prod-readonly"},
		{"k8s-dev", "dev-full"},
	} {
		if err := repo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{
			SourceID: s.id, ZoneID: s.zone, AssignedBy: "admin",
		}, "test"); err != nil {
			t.Fatalf("UpsertAssignment %s: %v", s.id, err)
		}
	}

	unassigned, err := repo.ListUnassignedSourceIDs(ctx)
	if err != nil {
		t.Fatalf("ListUnassignedSourceIDs: %v", err)
	}
	if len(unassigned) != 0 {
		t.Errorf("expected 0 unassigned sources when all are assigned, got %d: %v", len(unassigned), unassigned)
	}
}

// --- Error path tests (closed DB forces query errors) ---

func TestSQLRepository_ListZones_DBError(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	db.Close() // force all subsequent calls to fail
	_, err := repo.ListZones(context.Background())
	if err == nil {
		t.Error("expected error from ListZones on closed DB")
	}
}

func TestSQLRepository_GetZone_DBError(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	db.Close()
	_, err := repo.GetZone(context.Background(), "prod-readonly")
	if err == nil {
		t.Error("expected error from GetZone on closed DB")
	}
}

func TestSQLRepository_CreateZone_DBError(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	db.Close()
	_, err := repo.CreateZone(context.Background(), rbac.Zone{
		ID:             "new-zone",
		Name:           "New Zone",
		AllowedActions: []rbac.Action{rbac.ActionRead},
	}, "test")
	if err == nil {
		t.Error("expected error from CreateZone on closed DB")
	}
}

func TestSQLRepository_ListAssignments_DBError(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	db.Close()
	_, err := repo.ListAssignments(context.Background())
	if err == nil {
		t.Error("expected error from ListAssignments on closed DB")
	}
}

func TestSQLRepository_GetAssignment_DBError(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	db.Close()
	_, err := repo.GetAssignment(context.Background(), "k8s-prod")
	if err == nil {
		t.Error("expected error from GetAssignment on closed DB")
	}
}

func TestSQLRepository_UpsertAssignment_DBError(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	db.Close()
	err := repo.UpsertAssignment(context.Background(), rbac.SourceZoneAssignment{
		SourceID: "k8s-prod", ZoneID: "prod-readonly", AssignedBy: "admin",
	}, "test")
	if err == nil {
		t.Error("expected error from UpsertAssignment on closed DB")
	}
}

func TestSQLRepository_ListPolicies_DBError(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	db.Close()
	_, err := repo.ListPolicies(context.Background())
	if err == nil {
		t.Error("expected error from ListPolicies on closed DB")
	}
}

func TestSQLRepository_ListPoliciesForPrincipal_DBError(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	db.Close()
	_, err := repo.ListPoliciesForPrincipal(context.Background(), "alice")
	if err == nil {
		t.Error("expected error from ListPoliciesForPrincipal on closed DB")
	}
}

func TestSQLRepository_CreatePolicy_DBError(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	db.Close()
	_, err := repo.CreatePolicy(context.Background(), rbac.Policy{
		Principal: "alice", ZoneID: "prod-readonly",
	}, "test")
	if err == nil {
		t.Error("expected error from CreatePolicy on closed DB")
	}
}

func TestSQLRepository_DeletePolicy_DBError(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	db.Close()
	err := repo.DeletePolicy(context.Background(), 1, "test")
	if err == nil {
		t.Error("expected error from DeletePolicy on closed DB")
	}
}

func TestSQLRepository_ListUnassignedSourceIDs_DBError(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	db.Close()
	_, err := repo.ListUnassignedSourceIDs(context.Background())
	if err == nil {
		t.Error("expected error from ListUnassignedSourceIDs on closed DB")
	}
}
