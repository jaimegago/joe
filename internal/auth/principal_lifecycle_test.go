package auth

import (
	"context"
	"database/sql"
	"net/http"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
)

// newAuditedHandlers mirrors newTestHandlers but wires the identity registry,
// the audit sink, and an audit-recording RBAC repository, returning the
// concrete *rbac.SQLRepository so a test can read/seed the principals table and
// the auth repo so it can mint sessions directly.
func newAuditedHandlers(t *testing.T, prov Provider, adminEmail string) (*Handlers, *SQLRepository, *rbac.SQLRepository, audit.Repository, *store.Store) {
	t.Helper()
	repo, s := newTestRepo(t)
	auditRepo := audit.NewRepository(s.DB(), s.Driver())
	rbacRepo := rbac.NewRepositoryWithAudit(s.DB(), s.Driver(), auditRepo)
	h := NewHandlers(HandlerConfig{
		Provider:   prov,
		Sessions:   NewSessionManager(repo, time.Hour),
		Repo:       repo,
		RBAC:       rbacRepo,
		Principals: rbacRepo,
		AdminEmail: adminEmail,
		Audit:      auditRepo,
	})
	return h, repo, rbacRepo, auditRepo, s
}

func countPrincipals(t *testing.T, s *store.Store) int {
	t.Helper()
	var n int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM principals`).Scan(&n); err != nil {
		t.Fatalf("count principals: %v", err)
	}
	return n
}

// principalRow reads status + last_seen_at for one principal directly from the
// registry. exists is false when no row is present.
func principalRow(t *testing.T, s *store.Store, principal string) (status string, lastSeen sql.NullString, exists bool) {
	t.Helper()
	err := s.DB().QueryRow(
		`SELECT status, last_seen_at FROM principals WHERE principal = ?`, principal).
		Scan(&status, &lastSeen)
	if err == sql.ErrNoRows {
		return "", sql.NullString{}, false
	}
	if err != nil {
		t.Fatalf("read principal row: %v", err)
	}
	return status, lastSeen, true
}

func countAuditRows(t *testing.T, s *store.Store, action string, decision audit.Decision) int {
	t.Helper()
	var n int
	if err := s.DB().QueryRow(
		`SELECT COUNT(*) FROM audit_log WHERE action = ? AND decision = ?`, action, string(decision)).
		Scan(&n); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	return n
}

// TestCallback_FirstLoginCreatesPrincipalRow: a brand-new principal's first
// login provisions exactly one active registry row carrying a last_seen.
func TestCallback_FirstLoginCreatesPrincipalRow(t *testing.T) {
	prov := &fakeProvider{claims: Claims{Email: "newbie@example.com", EmailVerified: true}}
	h, _, _, _, s := newAuditedHandlers(t, prov, "")

	state := runLogin(t, h)
	if resp := runCallback(t, h, state); resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want 302", resp.StatusCode)
	}

	if n := countPrincipals(t, s); n != 1 {
		t.Fatalf("first login must create exactly one principals row, got %d", n)
	}
	status, lastSeen, exists := principalRow(t, s, "user:newbie@example.com")
	if !exists {
		t.Fatal("first login must create the principal's registry row")
	}
	if status != rbac.PrincipalStatusActive {
		t.Fatalf("new principal status = %q, want %q", status, rbac.PrincipalStatusActive)
	}
	if !lastSeen.Valid || lastSeen.String == "" {
		t.Fatal("first login must stamp last_seen_at")
	}
}

// TestCallback_RepeatLoginUpdatesLastSeen: a second login advances last_seen_at
// and does NOT duplicate the registry row.
func TestCallback_RepeatLoginUpdatesLastSeen(t *testing.T) {
	prov := &fakeProvider{claims: Claims{Email: "repeat@example.com", EmailVerified: true}}
	h, _, _, _, s := newAuditedHandlers(t, prov, "")

	clock := time.Now().UTC()
	h.now = func() time.Time { return clock }

	state := runLogin(t, h)
	if resp := runCallback(t, h, state); resp.StatusCode != http.StatusFound {
		t.Fatalf("first callback status = %d, want 302", resp.StatusCode)
	}
	_, firstSeen, exists := principalRow(t, s, "user:repeat@example.com")
	if !exists || !firstSeen.Valid {
		t.Fatal("first login must stamp last_seen_at")
	}

	clock = clock.Add(2 * time.Hour)
	state2 := runLogin(t, h)
	if resp := runCallback(t, h, state2); resp.StatusCode != http.StatusFound {
		t.Fatalf("second callback status = %d, want 302", resp.StatusCode)
	}

	if n := countPrincipals(t, s); n != 1 {
		t.Fatalf("repeat login must not duplicate the registry row, got %d rows", n)
	}
	_, secondSeen, _ := principalRow(t, s, "user:repeat@example.com")
	if secondSeen.String == firstSeen.String {
		t.Fatalf("repeat login must advance last_seen_at (was %q, still %q)", firstSeen.String, secondSeen.String)
	}
}

// TestCallback_DisabledRejectedAtMint: a disabled principal is rejected before
// a session is minted, and the rejected attempt is audited (oidc_login / deny).
func TestCallback_DisabledRejectedAtMint(t *testing.T) {
	prov := &fakeProvider{claims: Claims{Email: "bob@example.com", EmailVerified: true}}
	h, _, rbacRepo, _, s := newAuditedHandlers(t, prov, "")

	const principal = "user:bob@example.com"
	if err := rbacRepo.UpsertPrincipal(context.Background(), rbac.PrincipalRecord{Principal: principal}); err != nil {
		t.Fatalf("seed principal: %v", err)
	}
	if _, err := rbacRepo.SetPrincipalStatus(context.Background(), principal, rbac.PrincipalStatusDisabled, "admin"); err != nil {
		t.Fatalf("disable principal: %v", err)
	}

	state := runLogin(t, h)
	resp := runCallback(t, h, state)

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("disabled callback status = %d, want 403", resp.StatusCode)
	}
	if sc := cookieByName(resp, SessionCookieName); sc != nil && sc.Value != "" {
		t.Fatal("a disabled principal must not be issued a session cookie")
	}
	if n := countSessions(t, s); n != 0 {
		t.Fatalf("a disabled login must mint no session, got %d", n)
	}
	if n := countAuditRows(t, s, audit.ActionOIDCLogin, audit.DecisionDeny); n != 1 {
		t.Fatalf("a rejected disabled login must write one oidc_login/deny audit row, got %d", n)
	}
}

// TestCallback_DisabledOverridesBootstrap: a disabled principal whose email
// matches auth.admin_email is rejected and is NOT (re-)granted admin — the
// status check sits ahead of the bootstrap grant.
func TestCallback_DisabledOverridesBootstrap(t *testing.T) {
	const adminEmail = "admin@example.com"
	const principal = "user:admin@example.com"
	prov := &fakeProvider{claims: Claims{Email: adminEmail, EmailVerified: true}}
	h, _, rbacRepo, _, s := newAuditedHandlers(t, prov, adminEmail)

	if err := rbacRepo.UpsertPrincipal(context.Background(), rbac.PrincipalRecord{Principal: principal}); err != nil {
		t.Fatalf("seed principal: %v", err)
	}
	if _, err := rbacRepo.SetPrincipalStatus(context.Background(), principal, rbac.PrincipalStatusDisabled, "admin"); err != nil {
		t.Fatalf("disable principal: %v", err)
	}

	state := runLogin(t, h)
	if resp := runCallback(t, h, state); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("disabled admin callback status = %d, want 403", resp.StatusCode)
	}

	isAdmin, err := rbacRepo.IsAdmin(context.Background(), principal)
	if err != nil {
		t.Fatalf("IsAdmin: %v", err)
	}
	if isAdmin {
		t.Fatal("a disabled principal matching admin_email must NOT be granted admin")
	}
	if n := countSessions(t, s); n != 0 {
		t.Fatalf("a disabled admin login must mint no session, got %d", n)
	}
}

// TestPrincipalAdmin_DisablePurgesSessions: Disable flips status to disabled and
// deletes the principal's sessions (instant revocation) while leaving other
// principals' sessions intact; Enable restores active status without resurrecting
// sessions.
func TestPrincipalAdmin_DisablePurgesSessions(t *testing.T) {
	authRepo, s := newTestRepo(t)
	auditRepo := audit.NewRepository(s.DB(), s.Driver())
	rbacRepo := rbac.NewRepositoryWithAudit(s.DB(), s.Driver(), auditRepo)
	admin := NewPrincipalAdmin(rbacRepo, authRepo)
	ctx := context.Background()

	const principal = "user:victim@example.com"
	const other = "user:bystander@example.com"
	if err := rbacRepo.UpsertPrincipal(ctx, rbac.PrincipalRecord{Principal: principal}); err != nil {
		t.Fatalf("seed principal: %v", err)
	}

	now := time.Now().UTC()
	mk := func(id, p string) {
		if err := authRepo.CreateSession(ctx, Session{ID: id, Principal: p, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
			t.Fatalf("create session: %v", err)
		}
	}
	mk("s1", principal)
	mk("s2", principal)
	mk("s3", other)

	changed, err := admin.Disable(ctx, principal, "operator")
	if err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if changed != 1 {
		t.Fatalf("Disable changed = %d, want 1", changed)
	}

	status, _, _ := principalRow(t, s, principal)
	if status != rbac.PrincipalStatusDisabled {
		t.Fatalf("status after Disable = %q, want %q", status, rbac.PrincipalStatusDisabled)
	}
	for _, id := range []string{"s1", "s2"} {
		got, _ := authRepo.GetSession(ctx, id)
		if got != nil {
			t.Fatalf("Disable must delete the principal's session %q", id)
		}
	}
	if got, _ := authRepo.GetSession(ctx, "s3"); got == nil {
		t.Fatal("Disable must NOT delete a different principal's session")
	}

	if _, err := admin.Enable(ctx, principal, "operator"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	status, _, _ = principalRow(t, s, principal)
	if status != rbac.PrincipalStatusActive {
		t.Fatalf("status after Enable = %q, want %q", status, rbac.PrincipalStatusActive)
	}
	if n := countSessionsFor(t, s, principal); n != 0 {
		t.Fatalf("Enable must not resurrect sessions, got %d for %q", n, principal)
	}
}

func countSessionsFor(t *testing.T, s *store.Store, principal string) int {
	t.Helper()
	var n int
	if err := s.DB().QueryRow(
		`SELECT COUNT(*) FROM auth_sessions WHERE principal = ?`, principal).Scan(&n); err != nil {
		t.Fatalf("count sessions for principal: %v", err)
	}
	return n
}
