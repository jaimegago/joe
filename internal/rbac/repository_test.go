package rbac_test

import (
	"context"
	"testing"

	"github.com/jaimegago/joe/internal/rbac"
)

// Uses openTestDB from policy_test.go (same package, same test binary).

func TestSQLRepository_ListZones(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db)
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
	repo := rbac.NewRepository(db)
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
	repo := rbac.NewRepository(db)
	ctx := context.Background()

	newZone := rbac.Zone{
		ID:             "staging-readonly",
		Name:           "Staging Read-Only",
		Description:    "Staging environment, read access only",
		AllowedActions: []rbac.Action{rbac.ActionRead, rbac.ActionQuery},
	}

	created, err := repo.CreateZone(ctx, newZone)
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
	repo := rbac.NewRepository(db)
	ctx := context.Background()

	a := rbac.SourceZoneAssignment{
		SourceID:   "k8s-prod",
		ZoneID:     "prod-readonly",
		AssignedBy: "admin",
		Reason:     "initial assignment",
	}

	// First upsert
	if err := repo.UpsertAssignment(ctx, a); err != nil {
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
	if err := repo.UpsertAssignment(ctx, a); err != nil {
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
	repo := rbac.NewRepository(db)
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
	repo := rbac.NewRepository(db)
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
	}); err != nil {
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
	repo := rbac.NewRepository(db)
	ctx := context.Background()

	p := rbac.Policy{
		Principal: "alice",
		ZoneID:    "prod-readonly",
	}

	created, err := repo.CreatePolicy(ctx, p)
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
	repo := rbac.NewRepository(db)
	ctx := context.Background()

	// Create two policies
	for _, principal := range []string{"alice", "bob"} {
		if _, err := repo.CreatePolicy(ctx, rbac.Policy{
			Principal: principal, ZoneID: "prod-readonly",
		}); err != nil {
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
	repo := rbac.NewRepository(db)
	ctx := context.Background()

	// alice gets two zones, bob gets one
	for _, zone := range []string{"prod-readonly", "dev-full"} {
		if _, err := repo.CreatePolicy(ctx, rbac.Policy{
			Principal: "alice", ZoneID: zone,
		}); err != nil {
			t.Fatalf("CreatePolicy alice/%s: %v", zone, err)
		}
	}
	if _, err := repo.CreatePolicy(ctx, rbac.Policy{
		Principal: "bob", ZoneID: "dev-full",
	}); err != nil {
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
	repo := rbac.NewRepository(db)
	ctx := context.Background()

	created, err := repo.CreatePolicy(ctx, rbac.Policy{
		Principal: "carol", ZoneID: "prod-readonly",
	})
	if err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}

	if err := repo.DeletePolicy(ctx, created.ID); err != nil {
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
	repo := rbac.NewRepository(db)
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
	}); err != nil {
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
