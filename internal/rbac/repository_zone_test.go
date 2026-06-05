package rbac_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/rbac"
)

func TestZoneLifecycle_AuditedInTransaction(t *testing.T) {
	s := migratedStore(t)
	db := s.DB()
	repo := rbac.NewRepositoryWithAudit(db, s.Driver(), audit.NewRepository(db, s.Driver()))
	ctx := context.Background()

	// Create.
	if _, err := repo.CreateZone(ctx, rbac.Zone{
		ID: "staging", Name: "Staging", AllowedActions: []rbac.Action{rbac.ActionRead},
	}, "user:admin@example.com"); err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	assertAuditRow(t, db, audit.ActionAdminZoneCreate, "user:admin@example.com")

	// Update.
	updated, err := repo.UpdateZone(ctx, rbac.Zone{
		ID: "staging", Name: "Staging 2", Description: "edited",
		AllowedActions: []rbac.Action{rbac.ActionRead, rbac.ActionQuery},
	}, "user:admin@example.com")
	if err != nil {
		t.Fatalf("UpdateZone: %v", err)
	}
	if updated == nil || updated.Name != "Staging 2" {
		t.Fatalf("UpdateZone returned %+v, want name Staging 2", updated)
	}
	assertAuditRow(t, db, audit.ActionAdminZoneUpdate, "user:admin@example.com")

	// Delete (no assignments) cascades policies and writes the row.
	if _, err := repo.CreatePolicy(ctx, rbac.Policy{Principal: "user:x", ZoneID: "staging"}, "user:admin@example.com"); err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}
	if err := repo.DeleteZone(ctx, "staging", "user:admin@example.com"); err != nil {
		t.Fatalf("DeleteZone: %v", err)
	}
	assertAuditRow(t, db, audit.ActionAdminZoneDelete, "user:admin@example.com")
	if z, _ := repo.GetZone(ctx, "staging"); z != nil {
		t.Error("zone must be gone after DeleteZone")
	}
	var policyCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM rbac_policies WHERE zone_id = 'staging'`).Scan(&policyCount); err != nil {
		t.Fatalf("count policies: %v", err)
	}
	if policyCount != 0 {
		t.Errorf("rbac_policies for the deleted zone = %d, want 0 (ON DELETE CASCADE)", policyCount)
	}
}

func TestDeleteZone_RestrictWhenAssigned(t *testing.T) {
	s := migratedStore(t)
	db := s.DB()
	repo := rbac.NewRepositoryWithAudit(db, s.Driver(), audit.NewRepository(db, s.Driver()))
	ctx := context.Background()

	if _, err := repo.CreateZone(ctx, rbac.Zone{ID: "z1", Name: "Z1", AllowedActions: []rbac.Action{rbac.ActionRead}}, "a"); err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO sources (id, type, name, config) VALUES ('src-1','k8s','Src','{}')`); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	if err := repo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{
		SourceID: "src-1", ZoneID: "z1", AssignedBy: "a",
	}, "a"); err != nil {
		t.Fatalf("UpsertAssignment: %v", err)
	}

	err := repo.DeleteZone(ctx, "z1", "a")
	if !errors.Is(err, rbac.ErrZoneInUse) {
		t.Fatalf("DeleteZone of an assigned zone = %v, want ErrZoneInUse", err)
	}
	// The zone must still exist and no zone.delete row may have been written.
	if z, _ := repo.GetZone(ctx, "z1"); z == nil {
		t.Error("zone must survive a refused (RESTRICT) delete")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action = ?`, audit.ActionAdminZoneDelete).Scan(&n); err != nil {
		t.Fatalf("count delete audit: %v", err)
	}
	if n != 0 {
		t.Errorf("zone.delete audit rows = %d after a refused delete, want 0", n)
	}

	// After unassigning, the delete succeeds and writes its row.
	if _, err := repo.DeleteAssignment(ctx, "src-1", "a"); err != nil {
		t.Fatalf("DeleteAssignment: %v", err)
	}
	assertAuditRow(t, db, audit.ActionAdminSourceZoneUnassign, "a")
	if err := repo.DeleteZone(ctx, "z1", "a"); err != nil {
		t.Fatalf("DeleteZone after unassign: %v", err)
	}
}
