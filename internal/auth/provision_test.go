package auth

import (
	"context"
	"testing"

	"github.com/jaimegago/joe/internal/rbac"
)

// TestPhaseH_GrantAdminMarksDynamicCapability is the core Phase H
// behavioural contract: GrantAdmin marks the principal as a dynamic
// admin (admin_principals row) rather than spraying per-zone grants
// on rbac_policies. Pre-Phase-H this test would have FAILED — the call
// wrote one rbac_policies row per zone and no admin_principals row
// existed (the table did not exist either). After Phase H it PASSES.
func TestPhaseH_GrantAdminMarksDynamicCapability(t *testing.T) {
	_, s := newTestRepo(t)
	rbacRepo := rbac.NewRepository(s.DB(), s.Driver())
	prov := NewProvisioner(rbacRepo)

	ctx := context.Background()
	wasNew, err := prov.GrantAdmin(ctx, "user:admin@example.com")
	if err != nil {
		t.Fatalf("GrantAdmin: %v", err)
	}
	if !wasNew {
		t.Error("first GrantAdmin must report wasNew=true (a real escalation)")
	}

	isAdmin, err := rbacRepo.IsAdmin(ctx, "user:admin@example.com")
	if err != nil {
		t.Fatalf("IsAdmin: %v", err)
	}
	if !isAdmin {
		t.Fatal("GrantAdmin must mark the principal as dynamic admin")
	}

	admins, err := rbacRepo.ListAdmins(ctx)
	if err != nil {
		t.Fatalf("ListAdmins: %v", err)
	}
	if len(admins) != 1 {
		t.Fatalf("ListAdmins: got %d, want 1", len(admins))
	}
	if admins[0].Principal != "user:admin@example.com" {
		t.Errorf("admin principal = %q, want %q", admins[0].Principal, "user:admin@example.com")
	}
	if admins[0].GrantedBy != GrantedByBootstrapAdminEmail {
		t.Errorf("granted_by = %q, want %q", admins[0].GrantedBy, GrantedByBootstrapAdminEmail)
	}
	if admins[0].Reason != BootstrapAdminReason {
		t.Errorf("reason = %q, want %q", admins[0].Reason, BootstrapAdminReason)
	}
}

// TestPhaseH_GrantAdminDoesNotSnapshotZones asserts the structural
// invariant the prompt requires: there are no leftover bootstrap
// "grant on every existing zone" rows for the admin after GrantAdmin
// runs. Admin authority has exactly one source of truth — the
// admin_principals table.
func TestPhaseH_NoLeftoverSnapshotGrants(t *testing.T) {
	_, s := newTestRepo(t)
	rbacRepo := rbac.NewRepository(s.DB(), s.Driver())
	prov := NewProvisioner(rbacRepo)
	ctx := context.Background()

	// Pre-seed rbac_policies rows for the admin as if a pre-Phase-H
	// deployment had written snapshot grants on every zone. The Phase H
	// cleanup must remove these on first GrantAdmin call so the
	// "single source of truth" property holds structurally.
	zones, err := rbacRepo.ListZones(ctx)
	if err != nil {
		t.Fatalf("list zones: %v", err)
	}
	for _, z := range zones {
		if _, err := rbacRepo.CreatePolicy(ctx, rbac.Policy{
			Principal: "user:admin@example.com",
			ZoneID:    z.ID,
		}, "test"); err != nil {
			t.Fatalf("seed snapshot grant %q: %v", z.ID, err)
		}
	}

	if _, err := prov.GrantAdmin(ctx, "user:admin@example.com"); err != nil {
		t.Fatalf("GrantAdmin: %v", err)
	}

	grants, err := rbacRepo.ListPoliciesForPrincipal(ctx, "user:admin@example.com")
	if err != nil {
		t.Fatalf("list grants: %v", err)
	}
	if len(grants) != 0 {
		t.Errorf("Phase H: GrantAdmin must leave zero rbac_policies rows for the admin, found %d: %#v",
			len(grants), grants)
	}

	// And admin status still holds.
	isAdmin, _ := rbacRepo.IsAdmin(ctx, "user:admin@example.com")
	if !isAdmin {
		t.Error("admin status must hold after snapshot cleanup")
	}
}

// TestPhaseH_GrantAdminIsIdempotent guarantees safe re-running on every
// admin login: the second call does not error and does not produce
// extra rows.
func TestPhaseH_GrantAdminIsIdempotent(t *testing.T) {
	_, s := newTestRepo(t)
	rbacRepo := rbac.NewRepository(s.DB(), s.Driver())
	prov := NewProvisioner(rbacRepo)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		wasNew, err := prov.GrantAdmin(ctx, "user:admin@example.com")
		if err != nil {
			t.Fatalf("GrantAdmin call %d: %v", i+1, err)
		}
		// First call is the escalation; repeats must report wasNew=false so
		// the OIDC callback audits the grant exactly once.
		if want := i == 0; wasNew != want {
			t.Errorf("GrantAdmin call %d: wasNew = %v, want %v", i+1, wasNew, want)
		}
	}

	admins, _ := rbacRepo.ListAdmins(ctx)
	if len(admins) != 1 {
		t.Errorf("idempotent GrantAdmin must produce exactly one row, got %d", len(admins))
	}
}
