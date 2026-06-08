package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
)

// --- admin-bootstrap privilege-escalation audit ----------------------------
//
// These tests cover the H3 follow-up: a first-time admin bootstrap (a
// logging-in user promoted to admin via the auth.admin_email match) writes
// exactly one auth_login row with action admin_granted; a repeat admin login
// writes none. The grant's own fail-closed-500 posture is unchanged; only the
// added audit is fail-open-but-loud.

// newAdminGrantHandlers wires Handlers with the given audit repository, a real
// RBAC repository (so the Provisioner's IsAdmin/AddAdmin run against real
// SQL), and a configured admin email so the bootstrap path fires.
func newAdminGrantHandlers(t *testing.T, prov Provider, a audit.Repository, adminEmail string) (*Handlers, rbac.Repository, *store.Store) {
	t.Helper()
	repo, s := newTestRepo(t)
	rbacRepo := rbac.NewRepository(s.DB(), s.Driver())
	h := NewHandlers(HandlerConfig{
		Provider:   prov,
		Sessions:   NewSessionManager(repo, time.Hour),
		Repo:       repo,
		RBAC:       rbacRepo,
		AdminEmail: adminEmail,
		Audit:      a,
	})
	return h, rbacRepo, s
}

// countAction returns how many captured events carry the given action verb.
func countAction(events []audit.Event, action string) int {
	n := 0
	for _, e := range events {
		if e.Action == action {
			n++
		}
	}
	return n
}

// failingGrantRepo embeds a real RBAC repository but forces AddAdmin to error,
// driving GrantAdmin's fail-closed path. Every other method delegates to the
// real repo, so IsAdmin and the rest behave normally.
type failingGrantRepo struct {
	rbac.Repository
}

func (failingGrantRepo) AddAdmin(context.Context, rbac.Admin, string) error {
	return errors.New("rbac store unavailable")
}

// TestCallback_FirstAdminLoginWritesAdminGrantedRow: a first-time admin login
// (the principal was not previously an admin) writes exactly one admin_granted
// auth_login row carrying the user principal and the bootstrap source, plus the
// H3 oidc_login row, and the login still mints a session and redirects.
func TestCallback_FirstAdminLoginWritesAdminGrantedRow(t *testing.T) {
	const adminEmail = "admin@example.com"
	prov := &fakeProvider{claims: Claims{Email: adminEmail, EmailVerified: true}}
	rec := &recordingAudit{}
	h, rbacRepo, s := newAdminGrantHandlers(t, prov, rec, adminEmail)

	state := runLogin(t, h)
	resp := runCallback(t, h, state)

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want 302 (login must redirect)", resp.StatusCode)
	}
	if n := countSessions(t, s); n != 1 {
		t.Fatalf("session rows = %d, want 1 (login must mint a session)", n)
	}
	if isAdmin, _ := rbacRepo.IsAdmin(context.Background(), "user:"+adminEmail); !isAdmin {
		t.Fatal("admin email must hold admin status after first login")
	}

	events := rec.snapshot()
	if got := countAction(events, audit.ActionAdminGranted); got != 1 {
		t.Fatalf("admin_granted rows = %d, want exactly 1 on first escalation", got)
	}
	if got := countAction(events, audit.ActionOIDCLogin); got != 1 {
		t.Fatalf("oidc_login rows = %d, want 1 (H3 row still written)", got)
	}

	var grant audit.Event
	for _, e := range events {
		if e.Action == audit.ActionAdminGranted {
			grant = e
		}
	}
	if grant.Kind != audit.KindAuthLogin {
		t.Errorf("admin_granted kind = %q, want %q", grant.Kind, audit.KindAuthLogin)
	}
	if grant.ComponentID != auditSourceAdminBootstrap {
		t.Errorf("admin_granted source = %q, want %q", grant.ComponentID, auditSourceAdminBootstrap)
	}
	if grant.Decision != audit.DecisionAllow {
		t.Errorf("admin_granted decision = %q, want allow", grant.Decision)
	}
	if grant.Principal != "user:"+adminEmail {
		t.Errorf("admin_granted principal = %q, want user:%s", grant.Principal, adminEmail)
	}
	if !strings.Contains(grant.Context, adminEmail) {
		t.Errorf("admin_granted context %q must carry the verified email", grant.Context)
	}
}

// TestCallback_RepeatAdminLoginWritesNoAdminGrantedRow is the core property:
// escalation is audited once. A second admin login by an already-admin
// principal writes NO new admin_granted row, while the H3 oidc_login row is
// still written each time and the login still succeeds.
func TestCallback_RepeatAdminLoginWritesNoAdminGrantedRow(t *testing.T) {
	const adminEmail = "admin@example.com"
	prov := &fakeProvider{claims: Claims{Email: adminEmail, EmailVerified: true}}
	rec := &recordingAudit{}
	h, _, s := newAdminGrantHandlers(t, prov, rec, adminEmail)

	// First login: the escalation.
	if resp := runCallback(t, h, runLogin(t, h)); resp.StatusCode != http.StatusFound {
		t.Fatalf("first callback status = %d, want 302", resp.StatusCode)
	}
	// Second login: a repeat — the principal is already admin.
	if resp := runCallback(t, h, runLogin(t, h)); resp.StatusCode != http.StatusFound {
		t.Fatalf("second callback status = %d, want 302 (repeat login must still succeed)", resp.StatusCode)
	}

	if n := countSessions(t, s); n != 2 {
		t.Fatalf("session rows = %d, want 2 (both logins mint a session)", n)
	}

	events := rec.snapshot()
	if got := countAction(events, audit.ActionAdminGranted); got != 1 {
		t.Fatalf("admin_granted rows = %d, want exactly 1 across two logins (escalation audited once, repeats silent)", got)
	}
	if got := countAction(events, audit.ActionOIDCLogin); got != 2 {
		t.Fatalf("oidc_login rows = %d, want 2 (one per login, unchanged by H3 follow-up)", got)
	}
}

// TestCallback_AdminGrantAuditFailureDoesNotBlockLogin: a grant-audit write
// failure must not block the login — the redirect still fires, a session is
// still created, and the loud failure log fires. The grant itself still
// succeeds.
func TestCallback_AdminGrantAuditFailureDoesNotBlockLogin(t *testing.T) {
	const adminEmail = "admin@example.com"
	buf := captureLogs(t)
	prov := &fakeProvider{claims: Claims{Email: adminEmail, EmailVerified: true}}
	fail := &failingAudit{}
	h, rbacRepo, s := newAdminGrantHandlers(t, prov, fail, adminEmail)

	resp := runCallback(t, h, runLogin(t, h))

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want 302 (audit failure must not block login)", resp.StatusCode)
	}
	if n := countSessions(t, s); n != 1 {
		t.Fatalf("session rows = %d, want 1 (login must complete despite audit failure)", n)
	}
	if isAdmin, _ := rbacRepo.IsAdmin(context.Background(), "user:"+adminEmail); !isAdmin {
		t.Fatal("grant must still succeed even when the grant audit write fails")
	}
	// Both the oidc_login and the admin_granted writes are attempted and fail.
	if fail.callCount() != 2 {
		t.Fatalf("audit Insert calls = %d, want 2 (oidc_login + admin_granted)", fail.callCount())
	}
	if !strings.Contains(buf.String(), "AUDIT WRITE FAILED") {
		t.Errorf("expected a loud AUDIT WRITE FAILED log; got:\n%s", buf.String())
	}
}

// TestCallback_NilAuditAdminPathUnchanged: a nil audit repository leaves the
// admin-bootstrap path behaving exactly as today — the grant happens, the
// login redirects and mints a session, no panic.
func TestCallback_NilAuditAdminPathUnchanged(t *testing.T) {
	const adminEmail = "admin@example.com"
	prov := &fakeProvider{claims: Claims{Email: adminEmail, EmailVerified: true}}
	h, rbacRepo, s := newAdminGrantHandlers(t, prov, nil, adminEmail)

	resp := runCallback(t, h, runLogin(t, h))

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want 302", resp.StatusCode)
	}
	if n := countSessions(t, s); n != 1 {
		t.Fatalf("session rows = %d, want 1", n)
	}
	if isAdmin, _ := rbacRepo.IsAdmin(context.Background(), "user:"+adminEmail); !isAdmin {
		t.Fatal("admin grant must still happen with a nil audit repository")
	}
}

// TestCallback_GrantFailureFailsLoginClosed asserts the grant's own posture is
// UNCHANGED by the signature change: a GrantAdmin error aborts the login with
// HTTP 500 and mints no session — fail-closed and loud, as before.
func TestCallback_GrantFailureFailsLoginClosed(t *testing.T) {
	const adminEmail = "admin@example.com"
	prov := &fakeProvider{claims: Claims{Email: adminEmail, EmailVerified: true}}
	repo, s := newTestRepo(t)
	h := NewHandlers(HandlerConfig{
		Provider:   prov,
		Sessions:   NewSessionManager(repo, time.Hour),
		Repo:       repo,
		RBAC:       failingGrantRepo{rbac.NewRepository(s.DB(), s.Driver())},
		AdminEmail: adminEmail,
	})

	resp := runCallback(t, h, runLogin(t, h))

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("callback status = %d, want 500 (grant failure must fail the login closed)", resp.StatusCode)
	}
	if n := countSessions(t, s); n != 0 {
		t.Fatalf("session rows = %d, want 0 (a failed grant must mint no session)", n)
	}
}
