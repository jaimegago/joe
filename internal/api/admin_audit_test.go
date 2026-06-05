package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jaimegago/joe/internal/audit"
)

// Regression tests for DECISIONS.md D-0013 (the admin-audit gap). D-0012
// admin-gated the RBAC admin surface (admin.go) but it wrote ZERO audit rows;
// D-0013 wires a KindAdminAccess row into every handler with Phase F's §4
// failure posture. These tests are RED without the recordAdminAudit wiring
// and GREEN with it. For the mutating actions they also assert fail-CLOSED:
// an injected audit-write failure aborts the mutation (no state change). For
// the gate denial they assert the "non-admin tried to escalate" row.
//
// They reuse the Stream G fixture (llmadminFixture, llmadmin_test.go): a real
// migrated store (so migration 020's widened kind CHECK is exercised — an
// admin_access INSERT would fail the CHECK without it), an RBAC repo with the
// admin gate enabled, markAdmin, and the do() principal-injecting helper.

// failingAudit is an audit.Repository whose every write fails, used to drive
// the fail-closed path.
type failingAudit struct{}

func (failingAudit) Insert(context.Context, audit.Event) error {
	return audit.ErrAuditWriteFailed
}
func (failingAudit) InsertTx(context.Context, *sql.Tx, audit.Event) error {
	return audit.ErrAuditWriteFailed
}

// swappableAudit is an audit.Repository whose underlying sink can be swapped at
// runtime. The fixture shares ONE instance between the RBAC repository (which
// writes admin-mutation rows in-transaction via InsertTx) and services.Audit
// (the handler path for reads and gate denials), so breakAudit() drives the
// fail-closed / fail-open paths uniformly across both layers without rebuilding
// the server.
type swappableAudit struct{ inner audit.Repository }

func (s *swappableAudit) Insert(ctx context.Context, e audit.Event) error {
	return s.inner.Insert(ctx, e)
}
func (s *swappableAudit) InsertTx(ctx context.Context, tx *sql.Tx, e audit.Event) error {
	return s.inner.InsertTx(ctx, tx, e)
}

// breakAudit makes every subsequent audit write fail, for both the in-handler
// read/denial path and the in-transaction repository-mutation path.
func (f *llmadminFixture) breakAudit() {
	f.audit.(*swappableAudit).inner = failingAudit{}
}

// latestAudit returns the most recent audit_log row for the given action,
// decoding the context blob into audit.Details. found is false when no row
// for the action exists.
func latestAudit(t *testing.T, f *llmadminFixture, action string) (principal, decision string, details audit.Details, found bool) {
	t.Helper()
	var ctxBlob string
	err := f.store.DB().QueryRowContext(context.Background(),
		`SELECT principal, decision, context FROM audit_log
		   WHERE action = ? ORDER BY id DESC LIMIT 1`, action).
		Scan(&principal, &decision, &ctxBlob)
	if err == sql.ErrNoRows {
		return "", "", audit.Details{}, false
	}
	if err != nil {
		t.Fatalf("query latest audit row for %q: %v", action, err)
	}
	if uerr := json.Unmarshal([]byte(ctxBlob), &details); uerr != nil {
		t.Fatalf("decode audit context for %q (%q): %v", action, ctxBlob, uerr)
	}
	return principal, decision, details, true
}

// --- Allow-path: each mutating action writes its row ---

func TestAdminAudit_ZoneCreate_WritesRow(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")

	w := f.do(http.MethodPost, "/api/v1/admin/zones",
		`{"id":"staging","name":"Staging","allowed_actions":["read","query"]}`, "user:alice")
	if w.Code != http.StatusCreated {
		t.Fatalf("admin create zone: status=%d body=%s; want 201", w.Code, w.Body.String())
	}

	principal, decision, d, found := latestAudit(t, f, audit.ActionAdminZoneCreate)
	if !found {
		t.Fatal("no zone.create audit row written")
	}
	if principal != "user:alice" || decision != string(audit.DecisionAllow) {
		t.Errorf("zone.create row principal=%q decision=%q; want user:alice/allow", principal, decision)
	}
	if d.Target != "zone:staging" || d.After == nil {
		t.Errorf("zone.create details=%+v; want target=zone:staging with after-state", d)
	}
}

func TestAdminAudit_PolicyGrant_WritesRow(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")

	w := f.do(http.MethodPost, "/api/v1/admin/policies",
		`{"principal":"user:carol","zone_id":"prod-write"}`, "user:alice")
	if w.Code != http.StatusCreated {
		t.Fatalf("admin create policy: status=%d body=%s; want 201", w.Code, w.Body.String())
	}

	principal, decision, d, found := latestAudit(t, f, audit.ActionAdminPolicyGrant)
	if !found {
		t.Fatal("no policy.grant audit row written")
	}
	if principal != "user:alice" || decision != string(audit.DecisionAllow) {
		t.Errorf("policy.grant row principal=%q decision=%q; want user:alice/allow", principal, decision)
	}
	if d.Target != "policy:user:carol@prod-write" || d.After == nil {
		t.Errorf("policy.grant details=%+v; want target=policy:user:carol@prod-write with after-state", d)
	}
}

func TestAdminAudit_PolicyRevoke_WritesRowWithBefore(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")

	// Create a policy, then find its id to revoke it.
	if w := f.do(http.MethodPost, "/api/v1/admin/policies",
		`{"principal":"user:dave","zone_id":"prod-write"}`, "user:alice"); w.Code != http.StatusCreated {
		t.Fatalf("seed policy: status=%d body=%s; want 201", w.Code, w.Body.String())
	}
	ps, err := f.rbac.ListPolicies(context.Background())
	if err != nil {
		t.Fatalf("ListPolicies: %v", err)
	}
	var id int64 = -1
	for _, p := range ps {
		if p.Principal == "user:dave" && p.ZoneID == "prod-write" {
			id = p.ID
			break
		}
	}
	if id < 0 {
		t.Fatal("seeded policy not found")
	}

	w := f.do(http.MethodDelete,
		"/api/v1/admin/policies/"+itoaInt64(id), "", "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("admin revoke policy: status=%d body=%s; want 200", w.Code, w.Body.String())
	}

	principal, decision, d, found := latestAudit(t, f, audit.ActionAdminPolicyRevoke)
	if !found {
		t.Fatal("no policy.revoke audit row written")
	}
	if principal != "user:alice" || decision != string(audit.DecisionAllow) {
		t.Errorf("policy.revoke row principal=%q decision=%q; want user:alice/allow", principal, decision)
	}
	// Before-state is the canonical D-0013 requirement for revokes.
	if d.Before == nil {
		t.Errorf("policy.revoke details=%+v; want before-state recorded for the revoked grant", d)
	}
}

func TestAdminAudit_SourceZoneAssign_WritesRow(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	// source_zone_assignments.source_id has a FOREIGN KEY to sources(id), so
	// the source must exist before the upsert runs (the gate regression tests
	// never reach the upsert — the gate refuses first — so they skip this).
	if _, err := f.store.DB().ExecContext(context.Background(),
		`INSERT INTO sources (id, type, name, config) VALUES ('src-1', 'k8s', 'Src One', '{}')`); err != nil {
		t.Fatalf("seed source: %v", err)
	}

	w := f.do(http.MethodPost, "/api/v1/admin/source-zones",
		`{"source_id":"src-1","zone_id":"prod-write","assigned_by":"user:alice"}`, "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("admin assign source-zone: status=%d body=%s; want 200", w.Code, w.Body.String())
	}

	principal, decision, d, found := latestAudit(t, f, audit.ActionAdminSourceZoneAssign)
	if !found {
		t.Fatal("no source_zone.assign audit row written")
	}
	if principal != "user:alice" || decision != string(audit.DecisionAllow) {
		t.Errorf("source_zone.assign row principal=%q decision=%q; want user:alice/allow", principal, decision)
	}
	if d.Target != "source_zone:src-1" || d.After == nil {
		t.Errorf("source_zone.assign details=%+v; want target=source_zone:src-1 with after-state", d)
	}
}

// --- Allow-path: reads write their (fail-open) rows ---

func TestAdminAudit_Reads_WriteRows(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")

	cases := []struct {
		path   string
		action string
	}{
		{"/api/v1/admin/zones", audit.ActionAdminZoneRead},
		{"/api/v1/admin/policies", audit.ActionAdminPolicyRead},
		{"/api/v1/admin/source-zones", audit.ActionAdminSourceZoneRead},
		{"/api/v1/admin/unassigned", audit.ActionAdminSourceZoneRead},
	}
	for _, c := range cases {
		if w := f.do(http.MethodGet, c.path, "", "user:alice"); w.Code != http.StatusOK {
			t.Fatalf("admin GET %s: status=%d body=%s; want 200", c.path, w.Code, w.Body.String())
		}
		if n := f.countAudit(c.action); n == 0 {
			t.Errorf("GET %s wrote no %q audit row", c.path, c.action)
		}
		principal, decision, _, _ := latestAudit(t, f, c.action)
		if principal != "user:alice" || decision != string(audit.DecisionAllow) {
			t.Errorf("%s row principal=%q decision=%q; want user:alice/allow", c.action, principal, decision)
		}
	}
}

// --- Fail-closed: a mutating action aborts when the audit write fails ---

func TestAdminAudit_ZoneCreate_FailClosed(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	f.breakAudit()

	before := countZones(t, f)
	w := f.do(http.MethodPost, "/api/v1/admin/zones",
		`{"id":"never","name":"Never","allowed_actions":["read"]}`, "user:alice")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("zone create with failing audit: status=%d body=%s; want 500 (fail-closed)", w.Code, w.Body.String())
	}
	if got := countZones(t, f); got != before {
		t.Errorf("zone count=%d after fail-closed create; want %d — the mutation must NOT commit when its audit row fails", got, before)
	}
}

func TestAdminAudit_PolicyGrant_FailClosed(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	f.breakAudit()

	before := countPolicies(t, f)
	w := f.do(http.MethodPost, "/api/v1/admin/policies",
		`{"principal":"user:eve","zone_id":"prod-write"}`, "user:alice")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("policy grant with failing audit: status=%d body=%s; want 500 (fail-closed)", w.Code, w.Body.String())
	}
	if got := countPolicies(t, f); got != before {
		t.Errorf("policy count=%d after fail-closed grant; want %d — the grant must NOT commit when its audit row fails", got, before)
	}
}

// --- Fail-open: a read proceeds even when the audit write fails ---

func TestAdminAudit_Read_FailOpen(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	f.breakAudit()

	w := f.do(http.MethodGet, "/api/v1/admin/zones", "", "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("zone list with failing audit: status=%d body=%s; want 200 (fail-open)", w.Code, w.Body.String())
	}
}

// --- Denial: a non-admin attempt is recorded as the escalation trail ---

func TestAdminAudit_Denial_WritesRow(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	// user:bob is NOT marked admin.

	w := f.do(http.MethodPost, "/api/v1/admin/zones",
		`{"id":"bob-superzone","name":"Bob Superzone","allowed_actions":["read","query","mutate","delete"]}`, "user:bob")
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin create zone: status=%d body=%s; want 403", w.Code, w.Body.String())
	}

	principal, decision, d, found := latestAudit(t, f, audit.ActionAdminAccessDenied)
	if !found {
		t.Fatal("no admin.access_denied audit row written for the refused escalation attempt")
	}
	if principal != "user:bob" || decision != string(audit.DecisionDeny) {
		t.Errorf("denial row principal=%q decision=%q; want user:bob/deny", principal, decision)
	}
	if d.Target != "POST /api/v1/admin/zones" {
		t.Errorf("denial details target=%q; want %q", d.Target, "POST /api/v1/admin/zones")
	}
}

// itoaInt64 renders an int64 path segment without dragging in strconv at the
// top of the file — mirrors the inlined helper the structural guards use.
func itoaInt64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
