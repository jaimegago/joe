package auth

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/config"
)

// recordingAudit is a thread-safe in-memory audit.Repository that captures
// every Event passed to Insert. It is the success-path double for the
// Stream H3 auth_login tests.
type recordingAudit struct {
	mu     sync.Mutex
	events []audit.Event
}

func (r *recordingAudit) Insert(_ context.Context, e audit.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
	return nil
}

func (r *recordingAudit) InsertTx(_ context.Context, _ *sql.Tx, e audit.Event) error {
	return r.Insert(context.Background(), e)
}

func (r *recordingAudit) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

func (r *recordingAudit) snapshot() []audit.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]audit.Event, len(r.events))
	copy(out, r.events)
	return out
}

// failingAudit always errors on Insert, counting the calls. It is the
// fail-open-but-loud double: the login/request must still complete and the
// loud failure log must fire.
type failingAudit struct {
	mu    sync.Mutex
	calls int
}

func (f *failingAudit) Insert(_ context.Context, _ audit.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return errors.New("audit store unavailable")
}

func (f *failingAudit) InsertTx(_ context.Context, _ *sql.Tx, e audit.Event) error {
	return f.Insert(context.Background(), e)
}

func (f *failingAudit) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// captureLogs redirects the default slog logger to a buffer for the duration
// of the test and returns the buffer plus a restore func. Used to assert the
// loud AUDIT WRITE FAILED line fires on the fail-open-but-loud paths.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

// newAuditHandlers wires Handlers with an explicit audit repository (the
// newTestHandlers helper in handlers_test.go wires none).
func newAuditHandlers(t *testing.T, prov Provider, a audit.Repository) (*Handlers, *SQLRepository, interface{ DB() *sql.DB }) {
	t.Helper()
	repo, s := newTestRepo(t)
	h := NewHandlers(HandlerConfig{
		Provider: prov,
		Sessions: NewSessionManager(repo, time.Hour),
		Repo:     repo,
		RBAC:     nil,
		Audit:    a,
	})
	return h, repo, s
}

// --- OIDC human-login audit -------------------------------------------------

// TestCallback_WritesExactlyOneAuthLoginRow is the OIDC acceptance: a
// successful login writes exactly one auth_login row carrying the user
// principal and the OIDC source, and the login still redirects.
func TestCallback_WritesExactlyOneAuthLoginRow(t *testing.T) {
	prov := &fakeProvider{claims: Claims{Email: "alice@example.com", EmailVerified: true}}
	rec := &recordingAudit{}
	h, _, _ := newAuditHandlers(t, prov, rec)

	state := runLogin(t, h)
	resp := runCallback(t, h, state)

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want 302 (login must still redirect)", resp.StatusCode)
	}
	if rec.count() != 1 {
		t.Fatalf("auth_login rows = %d, want exactly 1 per login", rec.count())
	}
	e := rec.snapshot()[0]
	if e.Kind != audit.KindAuthLogin {
		t.Errorf("kind = %q, want %q", e.Kind, audit.KindAuthLogin)
	}
	if e.Action != audit.ActionOIDCLogin {
		t.Errorf("action = %q, want %q", e.Action, audit.ActionOIDCLogin)
	}
	if e.ComponentID != auditSourceOIDC {
		t.Errorf("source = %q, want %q", e.ComponentID, auditSourceOIDC)
	}
	if e.Decision != audit.DecisionAllow {
		t.Errorf("decision = %q, want allow", e.Decision)
	}
	if e.Principal != "user:alice@example.com" {
		t.Errorf("principal = %q, want user:alice@example.com", e.Principal)
	}
	if !strings.Contains(e.Context, "alice@example.com") {
		t.Errorf("context %q must carry the verified email", e.Context)
	}
}

// TestCallback_AuditFailureDoesNotBlockLogin: an audit-write failure must not
// block the login — the redirect still fires, a session is still created, and
// the loud failure log fires.
func TestCallback_AuditFailureDoesNotBlockLogin(t *testing.T) {
	buf := captureLogs(t)
	prov := &fakeProvider{claims: Claims{Email: "alice@example.com", EmailVerified: true}}
	fail := &failingAudit{}
	h, _, s := newAuditHandlers(t, prov, fail)

	state := runLogin(t, h)
	resp := runCallback(t, h, state)

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want 302 (audit failure must not block login)", resp.StatusCode)
	}
	var n int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM auth_sessions`).Scan(&n); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if n != 1 {
		t.Fatalf("session rows = %d, want 1 (login must complete despite audit failure)", n)
	}
	if fail.callCount() != 1 {
		t.Fatalf("audit Insert calls = %d, want 1", fail.callCount())
	}
	if !strings.Contains(buf.String(), "AUDIT WRITE FAILED") {
		t.Errorf("expected a loud AUDIT WRITE FAILED log; got:\n%s", buf.String())
	}
}

// TestCallback_NoopAuditLeavesLoginUnchanged: a Noop audit repository (and the
// nil case, covered by the existing handlers tests) leaves the login behaving
// exactly as today — redirect, one session, no panic.
func TestCallback_NoopAuditLeavesLoginUnchanged(t *testing.T) {
	prov := &fakeProvider{claims: Claims{Email: "alice@example.com", EmailVerified: true}}
	h, _, s := newAuditHandlers(t, prov, audit.NewNoopRepository())

	state := runLogin(t, h)
	resp := runCallback(t, h, state)

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want 302", resp.StatusCode)
	}
	var n int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM auth_sessions`).Scan(&n); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if n != 1 {
		t.Fatalf("session rows = %d, want 1", n)
	}
}

// --- break-glass bearer audit ----------------------------------------------

// breakGlassRequest builds a GET on a protected path carrying the given bearer
// key and remote address.
func breakGlassRequest(key, remote string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/probe/s1/read", nil)
	r.Header.Set("Authorization", "Bearer "+key)
	r.RemoteAddr = remote
	return r
}

// TestEdgeAuth_BreakGlassAuditOncePerWindow: repeated bearer requests from the
// same service-account principal and source within the window write exactly
// one auth_login row, not one per request.
func TestEdgeAuth_BreakGlassAuditOncePerWindow(t *testing.T) {
	resolver := mustResolver(t, config.ServiceAccount{Name: "ci", Key: "secret"})
	rec := &recordingAudit{}
	mw := EdgeAuth(EdgeConfig{ServiceAccounts: resolver, Audit: rec, AuditDedupWindow: time.Hour})
	h := mw(http.HandlerFunc(principalEcho))

	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, breakGlassRequest("secret", "10.0.0.1:1234"))
		if w.Code != http.StatusOK || w.Body.String() != "svc:ci" {
			t.Fatalf("request %d resolved (%d, %q), want (200, svc:ci)", i, w.Code, w.Body.String())
		}
	}

	if rec.count() != 1 {
		t.Fatalf("auth_login rows = %d, want exactly 1 across the window", rec.count())
	}
	e := rec.snapshot()[0]
	if e.Kind != audit.KindAuthLogin || e.Action != audit.ActionBreakGlassUse {
		t.Errorf("row kind/action = %q/%q, want %q/%q", e.Kind, e.Action, audit.KindAuthLogin, audit.ActionBreakGlassUse)
	}
	if e.ComponentID != auditSourceBreakGlass {
		t.Errorf("source = %q, want %q", e.ComponentID, auditSourceBreakGlass)
	}
	if e.Principal != "svc:ci" {
		t.Errorf("principal = %q, want svc:ci", e.Principal)
	}
}

// TestEdgeAuth_BreakGlassAuditDifferentKeyWritesAnother: a request from a
// different source (remote addr) writes its own row.
func TestEdgeAuth_BreakGlassAuditDifferentKeyWritesAnother(t *testing.T) {
	resolver := mustResolver(t, config.ServiceAccount{Name: "ci", Key: "secret"})
	rec := &recordingAudit{}
	mw := EdgeAuth(EdgeConfig{ServiceAccounts: resolver, Audit: rec, AuditDedupWindow: time.Hour})
	h := mw(http.HandlerFunc(principalEcho))

	for _, remote := range []string{"10.0.0.1:1111", "10.0.0.2:2222"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, breakGlassRequest("secret", remote))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
	}
	if rec.count() != 2 {
		t.Fatalf("auth_login rows = %d, want 2 (one per distinct source)", rec.count())
	}
}

// TestEdgeAuth_BreakGlassAuditAfterWindowWritesAnother: a request after the
// window elapses writes another row.
func TestEdgeAuth_BreakGlassAuditAfterWindowWritesAnother(t *testing.T) {
	resolver := mustResolver(t, config.ServiceAccount{Name: "ci", Key: "secret"})
	rec := &recordingAudit{}
	mw := EdgeAuth(EdgeConfig{ServiceAccounts: resolver, Audit: rec, AuditDedupWindow: 20 * time.Millisecond})
	h := mw(http.HandlerFunc(principalEcho))

	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, breakGlassRequest("secret", "10.0.0.1:1234"))
	time.Sleep(40 * time.Millisecond)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, breakGlassRequest("secret", "10.0.0.1:1234"))

	if rec.count() != 2 {
		t.Fatalf("auth_login rows = %d, want 2 (one per window)", rec.count())
	}
}

// TestEdgeAuth_BreakGlassAuditConcurrentSingleRow fires many simultaneous
// bearer requests for the same key and asserts exactly one row is recorded —
// the dedup compare-and-set must let exactly one racing first-use through.
func TestEdgeAuth_BreakGlassAuditConcurrentSingleRow(t *testing.T) {
	resolver := mustResolver(t, config.ServiceAccount{Name: "ci", Key: "secret"})
	rec := &recordingAudit{}
	mw := EdgeAuth(EdgeConfig{ServiceAccounts: resolver, Audit: rec, AuditDedupWindow: time.Hour})
	h := mw(http.HandlerFunc(principalEcho))

	const n = 32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			w := httptest.NewRecorder()
			h.ServeHTTP(w, breakGlassRequest("secret", "10.0.0.1:1234"))
		}()
	}
	close(start)
	wg.Wait()

	if rec.count() != 1 {
		t.Fatalf("auth_login rows = %d, want exactly 1 under concurrency", rec.count())
	}
}

// TestEdgeAuth_BreakGlassAuditFailureDoesNotBlock: an audit-write failure must
// not block the request — the principal still resolves (200) and the loud
// failure log fires.
func TestEdgeAuth_BreakGlassAuditFailureDoesNotBlock(t *testing.T) {
	buf := captureLogs(t)
	resolver := mustResolver(t, config.ServiceAccount{Name: "ci", Key: "secret"})
	fail := &failingAudit{}
	mw := EdgeAuth(EdgeConfig{ServiceAccounts: resolver, Audit: fail, AuditDedupWindow: time.Hour})
	h := mw(http.HandlerFunc(principalEcho))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, breakGlassRequest("secret", "10.0.0.1:1234"))

	if w.Code != http.StatusOK || w.Body.String() != "svc:ci" {
		t.Fatalf("request resolved (%d, %q), want (200, svc:ci) despite audit failure", w.Code, w.Body.String())
	}
	if fail.callCount() != 1 {
		t.Fatalf("audit Insert calls = %d, want 1", fail.callCount())
	}
	if !strings.Contains(buf.String(), "AUDIT WRITE FAILED") {
		t.Errorf("expected a loud AUDIT WRITE FAILED log; got:\n%s", buf.String())
	}
}

// TestEdgeAuth_BreakGlassNilAuditUnchanged: a nil audit repository leaves the
// bearer path behaving exactly as today — the principal resolves, no panic.
func TestEdgeAuth_BreakGlassNilAuditUnchanged(t *testing.T) {
	resolver := mustResolver(t, config.ServiceAccount{Name: "ci", Key: "secret"})
	mw := EdgeAuth(EdgeConfig{ServiceAccounts: resolver}) // no Audit wired
	h := mw(http.HandlerFunc(principalEcho))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, breakGlassRequest("secret", "10.0.0.1:1234"))
	if w.Code != http.StatusOK || w.Body.String() != "svc:ci" {
		t.Fatalf("nil-audit bearer resolved (%d, %q), want (200, svc:ci)", w.Code, w.Body.String())
	}
}

// --- dedup unit tests -------------------------------------------------------

// TestLoginDedup_WindowAndKeys exercises the windowing and per-key isolation
// of the dedup with an injected clock.
func TestLoginDedup_WindowAndKeys(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	clk := base
	d := newLoginDedup(time.Hour)
	d.now = func() time.Time { return clk }

	if !d.shouldRecord("k") {
		t.Fatal("first use must record")
	}
	if d.shouldRecord("k") {
		t.Fatal("second use within the window must suppress")
	}
	if !d.shouldRecord("other") {
		t.Fatal("a different key is independent and must record")
	}
	clk = base.Add(2 * time.Hour)
	if !d.shouldRecord("k") {
		t.Fatal("a use after the window elapses must record again")
	}
}

// TestLoginDedup_ConcurrentExactlyOneWins asserts the compare-and-set
// guarantee directly: among many racing first-uses of one key, exactly one
// shouldRecord returns true.
func TestLoginDedup_ConcurrentExactlyOneWins(t *testing.T) {
	d := newLoginDedup(time.Hour)
	var wins int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if d.shouldRecord("k") {
				atomic.AddInt64(&wins, 1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if wins != 1 {
		t.Fatalf("dedup winners = %d, want exactly 1 (compare-and-set must serialize)", wins)
	}
}
