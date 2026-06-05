package api

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/auth"
	"github.com/jaimegago/joe/internal/rbac"
)

// parseAdminFile parses admin.go in this package's directory so the Stage 3
// guard-coverage test can reuse the same AST walkers the structural guards use.
func parseAdminFile(t *testing.T) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "admin.go", nil, 0)
	if err != nil {
		t.Fatalf("parse admin.go: %v", err)
	}
	return f
}

// Stage 3 handler tests for the identity/RBAC admin REST surface. They reuse
// the llmadminFixture (a real migrated store with the admin gate, audit, and —
// since this stage — the Stage 3 dependencies wired: Provisioner,
// PrincipalAdmin, Principals). Every mutation test asserts EXACTLY ONE audit row
// is written for the action (the row is owned by the lower layer; the handler
// must not double-write).

// --- 1. Admin add wraps GrantAdmin (not AddAdmin), audits once ---

func TestAdminAddAdmin_ViaGrantAdmin(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice") // the acting admin

	before := f.countAudit(audit.ActionAdminGrant)
	w := f.do(http.MethodPost, "/api/v1/admin/admins",
		`{"principal":"user:newadmin@example.com","reason":"promote"}`, "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("add admin: status=%d body=%s; want 200", w.Code, w.Body.String())
	}

	var resp struct {
		Principal string `json:"principal"`
		Granted   bool   `json:"granted"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.Principal != "user:newadmin@example.com" || !resp.Granted {
		t.Errorf("resp=%+v; want principal=user:newadmin@example.com granted=true", resp)
	}

	// The grant landed durably (GrantAdmin → AddAdmin).
	if ok, err := f.rbac.IsAdmin(context.Background(), "user:newadmin@example.com"); err != nil || !ok {
		t.Errorf("IsAdmin(newadmin)=%v,%v; want true,nil — GrantAdmin must persist the row", ok, err)
	}
	// Exactly one admin.grant row for this mutation (single-write).
	if got := f.countAudit(audit.ActionAdminGrant); got != before+1 {
		t.Errorf("admin.grant audit rows = %d; want %d (exactly one per mutation)", got, before+1)
	}
}

func TestAdminAddAdmin_RejectsUnprefixedPrincipal(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")

	w := f.do(http.MethodPost, "/api/v1/admin/admins", `{"principal":"bob"}`, "user:alice")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("add admin with unprefixed principal: status=%d; want 400", w.Code)
	}
}

// --- 2. Admin remove refuses the configured bootstrap admin (409) ---

func TestAdminRemoveAdmin_BootstrapAdminConflict(t *testing.T) {
	f := newLLMAdminFixtureCfg(t, true, "Boss@Example.com")
	f.markAdmin("user:alice")            // acting admin
	f.markAdmin("user:boss@example.com") // bootstrap admin (case differs from config)

	// Case-insensitive match against user:<auth.admin_email> → 409.
	w := f.do(http.MethodDelete, "/api/v1/admin/admins/user:boss@example.com", "", "user:alice")
	if w.Code != http.StatusConflict {
		t.Fatalf("remove bootstrap admin: status=%d body=%s; want 409", w.Code, w.Body.String())
	}
	// Still an admin — the removal was refused before any mutation.
	if ok, _ := f.rbac.IsAdmin(context.Background(), "user:boss@example.com"); !ok {
		t.Error("bootstrap admin was removed despite the 409 guard")
	}
}

// --- 3. Admin remove refuses the last remaining admin (409) ---

func TestAdminRemoveAdmin_LastAdminConflict(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice") // the sole admin (also the caller)

	w := f.do(http.MethodDelete, "/api/v1/admin/admins/user:alice", "", "user:alice")
	if w.Code != http.StatusConflict {
		t.Fatalf("remove last admin: status=%d body=%s; want 409", w.Code, w.Body.String())
	}
	if ok, _ := f.rbac.IsAdmin(context.Background(), "user:alice"); !ok {
		t.Error("last admin was removed despite the 409 guard")
	}
}

func TestAdminRemoveAdmin_Succeeds(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	f.markAdmin("user:carol") // a second admin so carol is not the last one

	before := f.countAudit(audit.ActionAdminRevoke)
	w := f.do(http.MethodDelete, "/api/v1/admin/admins/user:carol", "", "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("remove admin: status=%d body=%s; want 200", w.Code, w.Body.String())
	}
	if ok, _ := f.rbac.IsAdmin(context.Background(), "user:carol"); ok {
		t.Error("carol still admin after successful removal")
	}
	if got := f.countAudit(audit.ActionAdminRevoke); got != before+1 {
		t.Errorf("admin.revoke audit rows = %d; want %d (exactly one)", got, before+1)
	}
}

// --- 4. Zone delete returns 409 when sources are still assigned ---

func TestAdminDeleteZone_InUseConflict(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")

	// Seed a source and a zone, then assign the source to the zone.
	if _, err := f.store.DB().ExecContext(context.Background(),
		`INSERT INTO sources (id, type, name, config) VALUES ('src-1','k8s','Src One','{}')`); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	if w := f.do(http.MethodPost, "/api/v1/admin/zones",
		`{"id":"z-inuse","name":"In Use","allowed_actions":["read"]}`, "user:alice"); w.Code != http.StatusCreated {
		t.Fatalf("seed zone: status=%d body=%s", w.Code, w.Body.String())
	}
	if w := f.do(http.MethodPost, "/api/v1/admin/source-zones",
		`{"source_id":"src-1","zone_id":"z-inuse","assigned_by":"user:alice"}`, "user:alice"); w.Code != http.StatusOK {
		t.Fatalf("seed assignment: status=%d body=%s", w.Code, w.Body.String())
	}

	w := f.do(http.MethodDelete, "/api/v1/admin/zones/z-inuse", "", "user:alice")
	if w.Code != http.StatusConflict {
		t.Fatalf("delete in-use zone: status=%d body=%s; want 409", w.Code, w.Body.String())
	}
	// The zone survives the refused delete.
	if z, _ := f.rbac.GetZone(context.Background(), "z-inuse"); z == nil {
		t.Error("zone was deleted despite the in-use 409 guard")
	}
}

func TestAdminDeleteZone_Succeeds(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	if w := f.do(http.MethodPost, "/api/v1/admin/zones",
		`{"id":"z-free","name":"Free","allowed_actions":["read"]}`, "user:alice"); w.Code != http.StatusCreated {
		t.Fatalf("seed zone: status=%d body=%s", w.Code, w.Body.String())
	}
	before := f.countAudit(audit.ActionAdminZoneDelete)
	w := f.do(http.MethodDelete, "/api/v1/admin/zones/z-free", "", "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("delete zone: status=%d body=%s; want 200", w.Code, w.Body.String())
	}
	if got := f.countAudit(audit.ActionAdminZoneDelete); got != before+1 {
		t.Errorf("zone.delete audit rows = %d; want %d (exactly one)", got, before+1)
	}
}

// --- 5. Zone edit (partial update) ---

func TestAdminUpdateZone_PartialAndNotFound(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	if w := f.do(http.MethodPost, "/api/v1/admin/zones",
		`{"id":"z-edit","name":"Old","description":"old desc","allowed_actions":["read"]}`, "user:alice"); w.Code != http.StatusCreated {
		t.Fatalf("seed zone: status=%d body=%s", w.Code, w.Body.String())
	}

	before := f.countAudit(audit.ActionAdminZoneUpdate)
	// Partial: only name; description and allowed_actions must be preserved.
	w := f.do(http.MethodPatch, "/api/v1/admin/zones/z-edit", `{"name":"New"}`, "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("update zone: status=%d body=%s; want 200", w.Code, w.Body.String())
	}
	z, _ := f.rbac.GetZone(context.Background(), "z-edit")
	if z == nil || z.Name != "New" || z.Description != "old desc" || len(z.AllowedActions) != 1 {
		t.Errorf("partial update result=%+v; want name=New, desc preserved, actions preserved", z)
	}
	if got := f.countAudit(audit.ActionAdminZoneUpdate); got != before+1 {
		t.Errorf("zone.update audit rows = %d; want %d (exactly one)", got, before+1)
	}

	// Missing zone → 404.
	if w := f.do(http.MethodPatch, "/api/v1/admin/zones/nope", `{"name":"x"}`, "user:alice"); w.Code != http.StatusNotFound {
		t.Fatalf("update missing zone: status=%d; want 404", w.Code)
	}
}

// --- 6. Policy revoke by principal + zone (natural key) ---

func TestAdminRevokePolicy_ByPrincipalAndZone(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	if _, err := f.rbac.CreatePolicy(context.Background(),
		rbac.Policy{Principal: "user:dave", ZoneID: "prod-write"}, "test"); err != nil {
		t.Fatalf("seed policy: %v", err)
	}

	before := f.countAudit(audit.ActionAdminPolicyRevoke)
	w := f.do(http.MethodPost, "/api/v1/admin/policies/revoke",
		`{"principal":"user:dave","zone_id":"prod-write"}`, "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("revoke policy by key: status=%d body=%s; want 200", w.Code, w.Body.String())
	}
	var resp struct {
		Removed int64 `json:"removed"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Removed != 1 {
		t.Errorf("removed=%d; want 1", resp.Removed)
	}
	ps, _ := f.rbac.ListPoliciesForPrincipal(context.Background(), "user:dave")
	if len(ps) != 0 {
		t.Errorf("policy still present after revoke-by-key: %+v", ps)
	}
	if got := f.countAudit(audit.ActionAdminPolicyRevoke); got != before+1 {
		t.Errorf("policy.revoke audit rows = %d; want %d (exactly one)", got, before+1)
	}
}

// --- 7. Source-zone unassign ---

func TestAdminUnassignSourceZone(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	if _, err := f.store.DB().ExecContext(context.Background(),
		`INSERT INTO sources (id, type, name, config) VALUES ('src-u','k8s','Src U','{}')`); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	if err := f.rbac.UpsertAssignment(context.Background(),
		rbac.SourceZoneAssignment{SourceID: "src-u", ZoneID: "prod-write", AssignedBy: "test"}, "test"); err != nil {
		t.Fatalf("seed assignment: %v", err)
	}
	before := f.countAudit(audit.ActionAdminSourceZoneUnassign)
	w := f.do(http.MethodDelete, "/api/v1/admin/source-zones/src-u", "", "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("unassign source-zone: status=%d body=%s; want 200", w.Code, w.Body.String())
	}
	if a, _ := f.rbac.GetAssignment(context.Background(), "src-u"); a != nil {
		t.Error("assignment still present after unassign")
	}
	if got := f.countAudit(audit.ActionAdminSourceZoneUnassign); got != before+1 {
		t.Errorf("source_zone.unassign audit rows = %d; want %d (exactly one)", got, before+1)
	}
}

// --- 8. Principals list (identity registry) ---

func TestAdminListPrincipals(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	now := time.Now().UTC()
	if err := f.services.Principals.UpsertPrincipal(context.Background(),
		rbac.PrincipalRecord{Principal: "user:dana", Status: rbac.PrincipalStatusActive, LastSeenAt: &now}); err != nil {
		t.Fatalf("seed principal: %v", err)
	}
	w := f.do(http.MethodGet, "/api/v1/admin/principals", "", "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("list principals: status=%d body=%s; want 200", w.Code, w.Body.String())
	}
	var resp struct {
		Count int `json:"count"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Count < 1 {
		t.Errorf("count=%d; want >=1 (the seeded principal)", resp.Count)
	}
	if f.countAudit(audit.ActionAdminPrincipalRead) == 0 {
		t.Error("list principals wrote no principal.read audit row")
	}
}

// --- 9. Principal disable revokes sessions, audits once; self-disable refused ---

func TestAdminDisablePrincipal_RevokesSessions(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	// Provision the target and seed two live sessions for it.
	if err := f.services.Principals.UpsertPrincipal(context.Background(),
		rbac.PrincipalRecord{Principal: "user:target", Status: rbac.PrincipalStatusActive}); err != nil {
		t.Fatalf("provision target: %v", err)
	}
	exp := time.Now().Add(time.Hour)
	for _, id := range []string{"sess-1", "sess-2"} {
		if err := f.sessions.CreateSession(context.Background(),
			auth.Session{ID: id, Principal: "user:target", ExpiresAt: exp}); err != nil {
			t.Fatalf("seed session %s: %v", id, err)
		}
	}

	before := f.countAudit(audit.ActionAdminPrincipalDisable)
	w := f.do(http.MethodPost, "/api/v1/admin/principals/user:target/disable", "", "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("disable principal: status=%d body=%s; want 200", w.Code, w.Body.String())
	}
	var resp struct {
		Status          string `json:"status"`
		SessionsRevoked int64  `json:"sessions_revoked"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Status != rbac.PrincipalStatusDisabled || resp.SessionsRevoked != 2 {
		t.Errorf("resp=%+v; want status=disabled sessions_revoked=2", resp)
	}
	// Sessions are gone (instant revocation).
	if s, _ := f.sessions.GetSession(context.Background(), "sess-1"); s != nil {
		t.Error("session sess-1 survived the disable")
	}
	// Registry row is disabled.
	rec, _ := f.services.Principals.GetPrincipal(context.Background(), "user:target")
	if rec == nil || rec.Status != rbac.PrincipalStatusDisabled {
		t.Errorf("registry status=%+v; want disabled", rec)
	}
	// Exactly one principal.disable row (single-write, lower layer owns it).
	if got := f.countAudit(audit.ActionAdminPrincipalDisable); got != before+1 {
		t.Errorf("principal.disable audit rows = %d; want %d (exactly one)", got, before+1)
	}
}

func TestAdminDisablePrincipal_SelfLockoutRefused(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	if err := f.services.Principals.UpsertPrincipal(context.Background(),
		rbac.PrincipalRecord{Principal: "user:alice", Status: rbac.PrincipalStatusActive}); err != nil {
		t.Fatalf("provision alice: %v", err)
	}
	before := f.countAudit(audit.ActionAdminPrincipalDisable)
	w := f.do(http.MethodPost, "/api/v1/admin/principals/user:alice/disable", "", "user:alice")
	if w.Code != http.StatusConflict {
		t.Fatalf("self-disable: status=%d body=%s; want 409", w.Code, w.Body.String())
	}
	// No mutation, no audit row.
	if rec, _ := f.services.Principals.GetPrincipal(context.Background(), "user:alice"); rec.Status != rbac.PrincipalStatusActive {
		t.Error("alice was disabled despite the self-lockout guard")
	}
	if got := f.countAudit(audit.ActionAdminPrincipalDisable); got != before {
		t.Errorf("principal.disable audit rows = %d; want %d (self-disable must not audit)", got, before)
	}
}

// --- 10. Principal enable restores status ---

func TestAdminEnablePrincipal(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	if err := f.services.Principals.UpsertPrincipal(context.Background(),
		rbac.PrincipalRecord{Principal: "user:target", Status: rbac.PrincipalStatusActive}); err != nil {
		t.Fatalf("provision target: %v", err)
	}
	if _, _, err := f.services.PrincipalAdmin.Disable(context.Background(), "user:target", "test"); err != nil {
		t.Fatalf("pre-disable: %v", err)
	}
	w := f.do(http.MethodPost, "/api/v1/admin/principals/user:target/enable", "", "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("enable principal: status=%d body=%s; want 200", w.Code, w.Body.String())
	}
	rec, _ := f.services.Principals.GetPrincipal(context.Background(), "user:target")
	if rec == nil || rec.Status != rbac.PrincipalStatusActive {
		t.Errorf("status=%+v; want active after enable", rec)
	}
}

// --- createPolicy prefix-validation regression ---

func TestAdminCreatePolicy_RejectsUnprefixedPrincipal(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	w := f.do(http.MethodPost, "/api/v1/admin/policies", `{"principal":"bob","zone_id":"prod-write"}`, "user:alice")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("create policy with unprefixed principal: status=%d; want 400", w.Code)
	}
}

// --- Structural guard coverage of the new Stage 3 routes ---

// TestAdminRoutes_Stage3RoutesGuarded asserts the new Stage 3 handlers are
// (a) actually registered, (b) recognised as gated by the gate guard, and
// (c) recognised as auditing by the audit guard (either via recordAdminAudit
// for reads or via an audited repo/seam mutation). This proves the new routes
// are covered by — not merely invisible to — the two structural guards.
func TestAdminRoutes_Stage3RoutesGuarded(t *testing.T) {
	f := parseAdminFile(t)
	registered := map[string]bool{}
	for _, n := range registeredAdminHandlers(t, f) {
		registered[n] = true
	}
	gated := gatedAdminHandlers(f)
	auditing := auditingAdminHandlers(f)

	newHandlers := []string{
		"updateZone", "deleteZone", "unassignSourceZone", "revokePolicy",
		"listAdmins", "addAdmin", "removeAdmin",
		"listPrincipals", "disablePrincipal", "enablePrincipal",
	}
	for _, name := range newHandlers {
		if !registered[name] {
			t.Errorf("Stage 3 handler %q is not registered in registerAdminRoutes", name)
		}
		if !gated[name] {
			t.Errorf("Stage 3 handler %q is not detected as admin-gated by the gate guard", name)
		}
		if !auditing[name] {
			t.Errorf("Stage 3 handler %q is not detected as auditing by the audit guard", name)
		}
	}
}
