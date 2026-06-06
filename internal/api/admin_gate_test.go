package api

import (
	"context"
	"net/http"
	"testing"
)

// Regression tests for DECISIONS.md D-0012 (privilege escalation): the RBAC
// admin endpoints in admin.go were not admin-gated, so any authenticated
// principal — including a brand-new zero-zone OIDC user — could grant
// itself a policy or create a zone with arbitrary allowed-actions, fully
// escalating its own access. These tests are RED without the requireAdmin
// gate on the admin handlers and GREEN with it.
//
// They reuse the Stream G fixture (llmadminFixture, llmadmin_test.go): a
// real migrated store, an RBAC repository with the admin-gate enabled
// (rbacEnabled=true), markAdmin to grant admin authority, and the do()
// helper that injects the caller principal into request context exactly as
// the edge middleware does. This is the same RBAC test pattern the identity
// refactor used for the LLM settings/usage gate (TestRequireAdmin_*,
// TestSetActiveModel_NonAdminForbidden*).

func countPolicies(t *testing.T, f *llmadminFixture) int {
	t.Helper()
	ps, err := f.rbac.ListPolicies(context.Background())
	if err != nil {
		t.Fatalf("ListPolicies: %v", err)
	}
	return len(ps)
}

func countZones(t *testing.T, f *llmadminFixture) int {
	t.Helper()
	zs, err := f.rbac.ListZones(context.Background())
	if err != nil {
		t.Fatalf("ListZones: %v", err)
	}
	return len(zs)
}

// (3a) Non-admin POST /api/v1/admin/policies → 403, policy not created.
func TestAdminPolicies_NonAdminForbidden(t *testing.T) {
	f := newLLMAdminFixture(t, true)

	before := countPolicies(t, f)
	// user:bob tries to grant himself the prod-write zone.
	w := f.do(http.MethodPost, "/api/v1/admin/policies",
		`{"principal":"user:bob","zone_id":"prod-write"}`, "user:bob")

	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin create policy: status=%d body=%s; want 403", w.Code, w.Body.String())
	}
	if got := countPolicies(t, f); got != before {
		t.Errorf("policy count = %d after forbidden create; want %d — non-admin must not create a policy", got, before)
	}
}

// (3b) Non-admin POST /api/v1/admin/zones → 403, zone not created.
func TestAdminZones_NonAdminForbidden(t *testing.T) {
	f := newLLMAdminFixture(t, true)

	before := countZones(t, f)
	// user:bob tries to mint a zone that permits every action.
	w := f.do(http.MethodPost, "/api/v1/admin/zones",
		`{"id":"bob-superzone","name":"Bob Superzone","allowed_actions":["read","query","mutate","delete"]}`, "user:bob")

	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin create zone: status=%d body=%s; want 403", w.Code, w.Body.String())
	}
	if got := countZones(t, f); got != before {
		t.Errorf("zone count = %d after forbidden create; want %d — non-admin must not create a zone", got, before)
	}
}

// Non-admin POST /api/v1/admin/source-zones → 403, assignment not created.
func TestAdminSourceZones_NonAdminForbidden(t *testing.T) {
	f := newLLMAdminFixture(t, true)

	before, err := f.rbac.ListAssignments(context.Background())
	if err != nil {
		t.Fatalf("ListAssignments: %v", err)
	}
	w := f.do(http.MethodPost, "/api/v1/admin/source-zones",
		`{"source_id":"src-1","zone_id":"prod-write","assigned_by":"user:bob"}`, "user:bob")

	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin assign source-zone: status=%d body=%s; want 403", w.Code, w.Body.String())
	}
	after, err := f.rbac.ListAssignments(context.Background())
	if err != nil {
		t.Fatalf("ListAssignments: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("assignment count = %d after forbidden assign; want %d", len(after), len(before))
	}
}

// (3c) Admin POST both → 201 and the resource is created.
func TestAdminPolicies_AdminAllowedAndCreated(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")

	before := countPolicies(t, f)
	w := f.do(http.MethodPost, "/api/v1/admin/policies",
		`{"principal":"user:carol","zone_id":"prod-write"}`, "user:alice")

	if w.Code != http.StatusCreated {
		t.Fatalf("admin create policy: status=%d body=%s; want 201", w.Code, w.Body.String())
	}
	if got := countPolicies(t, f); got != before+1 {
		t.Errorf("policy count = %d after admin create; want %d", got, before+1)
	}
}

func TestAdminZones_AdminAllowedAndCreated(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")

	before := countZones(t, f)
	w := f.do(http.MethodPost, "/api/v1/admin/zones",
		`{"id":"staging","name":"Staging","allowed_actions":["read","query"]}`, "user:alice")

	if w.Code != http.StatusCreated {
		t.Fatalf("admin create zone: status=%d body=%s; want 201", w.Code, w.Body.String())
	}
	if got := countZones(t, f); got != before+1 {
		t.Errorf("zone count = %d after admin create; want %d", got, before+1)
	}
}

// Blocker 2: a non-admin cannot escalate into the regime-control zone via the
// admin policy endpoint. This is the concrete privilege-escalation-into-
// incident-control path the audit flagged as a consequence of Blocker 1;
// confirm the gate closes it. (The regime-control zone is seeded by
// migration 012.)
func TestAdminPolicies_NonAdminCannotGrantRegimeControl(t *testing.T) {
	f := newLLMAdminFixture(t, true)

	w := f.do(http.MethodPost, "/api/v1/admin/policies",
		`{"principal":"user:bob","zone_id":"regime-control"}`, "user:bob")
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin self-grant regime-control: status=%d body=%s; want 403", w.Code, w.Body.String())
	}

	// Verify no regime-control policy now exists for user:bob.
	ps, err := f.rbac.ListPolicies(context.Background())
	if err != nil {
		t.Fatalf("ListPolicies: %v", err)
	}
	for _, p := range ps {
		if p.Principal == "user:bob" && p.ZoneID == "regime-control" {
			t.Fatalf("non-admin escalated into regime-control: %+v", p)
		}
	}
}

// The read endpoints leak the full authorization map (who has which zone,
// what each zone permits, the admin-visible source roster), so they are
// gated too — confirm a non-admin is refused the policy listing.
func TestAdminListPolicies_NonAdminForbidden(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	w := f.do(http.MethodGet, "/api/v1/admin/policies", "", "user:bob")
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin list policies: status=%d body=%s; want 403", w.Code, w.Body.String())
	}
}

// Sanity: the admin gate honours the auth-disabled permit convention, so
// local/dev runs (rbacEnabled=false) are not blocked by the new gate —
// otherwise the fix would break every keyless deployment. Mirrors
// TestRequireAdmin_AuthDisabledPermits.
func TestAdminPolicies_AuthDisabledPermits(t *testing.T) {
	f := newLLMAdminFixture(t, false)
	w := f.do(http.MethodPost, "/api/v1/admin/policies",
		`{"principal":"user:nobody","zone_id":"unassigned"}`, "user:nobody")
	if w.Code != http.StatusCreated {
		t.Fatalf("auth-disabled create policy: status=%d body=%s; want 201", w.Code, w.Body.String())
	}
}
