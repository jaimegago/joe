package sessionsweeper_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/auth"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/sessionsweeper"
	"github.com/jaimegago/joe/internal/store"
)

// fixedNow is the deterministic clock all sweep tests drive expiry against.
var fixedNow = time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)

type env struct {
	t       *testing.T
	db      *sql.DB
	repo    *sessionmodel.SQLRepository
	auth    *auth.SQLRepository
	auditRp audit.Repository
	princ   rbac.Principal
}

func newEnv(t *testing.T) *env {
	t.Helper()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	princ, err := rbac.SessionSweeperPrincipal()
	if err != nil {
		t.Fatalf("SessionSweeperPrincipal: %v", err)
	}
	return &env{
		t:       t,
		db:      s.DB(),
		repo:    sessionmodel.NewRepository(s.DB(), store.DriverSQLite),
		auth:    auth.NewRepository(s.DB(), store.DriverSQLite),
		auditRp: audit.NewRepository(s.DB(), store.DriverSQLite),
		princ:   princ,
	}
}

// newSweeper builds a sweeper over this env with an optional store/audit override
// (pass nil to use the real ones) so the fail-safe / rollback tests can inject
// failing collaborators.
func (e *env) newSweeper(sessions sessionsweeper.SessionStore, auditSink sessionsweeper.AuditSink) *sessionsweeper.Sweeper {
	if sessions == nil {
		sessions = e.repo
	}
	var sink sessionsweeper.AuditSink = e.auditRp
	if auditSink != nil {
		sink = auditSink
	}
	return sessionsweeper.New(sessionsweeper.Config{
		DB:        e.db,
		Sessions:  sessions,
		Flows:     e.auth,
		Audit:     sink,
		Principal: e.princ,
		Now:       func() time.Time { return fixedNow },
	})
}

func (e *env) setPolicy(inactivityDays *int, trashGraceDays int, terminal sessionmodel.TerminalAction) {
	e.t.Helper()
	var inactivity any
	if inactivityDays != nil {
		inactivity = *inactivityDays
	}
	if _, err := e.db.Exec(
		`UPDATE session_retention_policy SET inactivity_days=?, trash_grace_days=?, terminal_action=? WHERE id=1`,
		inactivity, trashGraceDays, string(terminal)); err != nil {
		e.t.Fatalf("setPolicy: %v", err)
	}
}

// createSession inserts a session with an explicit last_activity_at and optional
// trash state, used to place candidates on either side of an expiry boundary.
func (e *env) createSession(id, creator string, lastActivity time.Time, trashedAt, purgeAfter *time.Time) {
	e.t.Helper()
	s := sessionmodel.AgentSession{
		ID:               id,
		Type:             sessionmodel.SessionTypeDefault,
		CreatorPrincipal: creator,
		CreatedAt:        lastActivity,
		LastActivityAt:   lastActivity,
	}
	if trashedAt != nil {
		s.TrashedAt = trashedAt
		by := "user:" + creator
		s.TrashedBy = &by
		s.PurgeAfter = purgeAfter
	}
	if _, err := e.repo.CreateSession(context.Background(), s); err != nil {
		e.t.Fatalf("createSession %s: %v", id, err)
	}
}

func (e *env) auditCount(action string) int {
	e.t.Helper()
	var n int
	if err := e.db.QueryRow(`SELECT count(*) FROM audit_log WHERE action=?`, action).Scan(&n); err != nil {
		e.t.Fatalf("auditCount %s: %v", action, err)
	}
	return n
}

func (e *env) sessionExists(id string) bool {
	e.t.Helper()
	sess, err := e.repo.GetSession(context.Background(), id)
	if err != nil {
		e.t.Fatalf("GetSession %s: %v", id, err)
	}
	return sess != nil
}

func intPtr(i int) *int { return &i }

// --- Inactivity expiry ---

// TestSweep_InactivityOffByDefault proves the §12.5 default-OFF posture: with no
// inactivity window configured (the migration-026 seed), a long-idle session is
// NOT trashed and no lifecycle audit row is written.
func TestSweep_InactivityOffByDefault(t *testing.T) {
	e := newEnv(t)
	// Default policy: inactivity OFF. Session idle for a year.
	e.createSession("s-old", "alice", fixedNow.Add(-365*24*time.Hour), nil, nil)

	e.newSweeper(nil, nil).Sweep(context.Background())

	if !e.sessionExists("s-old") {
		t.Fatal("session removed while inactivity window is OFF — must not auto-expire")
	}
	sess, _ := e.repo.GetSession(context.Background(), "s-old")
	if sess.TrashedAt != nil {
		t.Error("session trashed while inactivity window is OFF")
	}
	if got := e.auditCount(audit.ActionSessionTrash); got != 0 {
		t.Errorf("trash audit rows = %d, want 0 (window OFF)", got)
	}
}

// TestSweep_InactivityTrashThenPurge proves the §12.5 inactivity expiry under
// trash_then_purge: a session older than the window is trashed (by the service
// principal, with purge_after = now + grace), a fresh session is untouched, and
// EXACTLY ONE session.trash audit row is written in the effect transaction,
// attributed to the boot-minted service principal under KindSessionLifecycle.
func TestSweep_InactivityTrashThenPurge(t *testing.T) {
	e := newEnv(t)
	e.setPolicy(intPtr(7), 30, sessionmodel.TerminalActionTrashThenPurge)
	e.createSession("s-stale", "alice", fixedNow.Add(-30*24*time.Hour), nil, nil)
	e.createSession("s-fresh", "bob", fixedNow.Add(-1*time.Hour), nil, nil)

	e.newSweeper(nil, nil).Sweep(context.Background())

	stale, _ := e.repo.GetSession(context.Background(), "s-stale")
	if stale == nil || stale.TrashedAt == nil {
		t.Fatal("stale session not trashed by inactivity expiry")
	}
	if stale.TrashedBy == nil || *stale.TrashedBy != string(e.princ) {
		t.Errorf("trashed_by = %v, want service principal %q", stale.TrashedBy, e.princ)
	}
	if stale.PurgeAfter == nil {
		t.Error("purge_after not stamped on sweeper trash (trash_then_purge)")
	} else {
		want := fixedNow.Add(30 * 24 * time.Hour)
		if !stale.PurgeAfter.Equal(want) {
			t.Errorf("purge_after = %v, want now+grace %v", stale.PurgeAfter, want)
		}
	}
	fresh, _ := e.repo.GetSession(context.Background(), "s-fresh")
	if fresh.TrashedAt != nil {
		t.Error("fresh session trashed — only inactivity-expired sessions should be")
	}
	if got := e.auditCount(audit.ActionSessionTrash); got != 1 {
		t.Errorf("trash audit rows = %d, want exactly 1", got)
	}
	// The single row names the service principal under the lifecycle kind.
	var principal, kind string
	if err := e.db.QueryRow(`SELECT principal, kind FROM audit_log WHERE action=?`, audit.ActionSessionTrash).
		Scan(&principal, &kind); err != nil {
		t.Fatalf("read trash audit row: %v", err)
	}
	if principal != string(e.princ) {
		t.Errorf("audit principal = %q, want %q", principal, e.princ)
	}
	if kind != string(audit.KindSessionLifecycle) {
		t.Errorf("audit kind = %q, want %q", kind, audit.KindSessionLifecycle)
	}
}

// TestSweep_ArchiveTerminalDeferred proves the soft-coupling honest seam: under
// the archive terminal action (provider deferred to B007c), an inactivity-expired
// session is left ACTIVE — never trashed, never falsely marked archived — and no
// lifecycle audit row is written for it.
func TestSweep_ArchiveTerminalDeferred(t *testing.T) {
	e := newEnv(t)
	e.setPolicy(intPtr(7), 30, sessionmodel.TerminalActionArchive)
	e.createSession("s-stale", "alice", fixedNow.Add(-30*24*time.Hour), nil, nil)

	e.newSweeper(nil, nil).Sweep(context.Background())

	sess, _ := e.repo.GetSession(context.Background(), "s-stale")
	if sess == nil {
		t.Fatal("archive-policy session destroyed — must be left active under the deferred seam")
	}
	if sess.TrashedAt != nil {
		t.Error("archive-policy session trashed — would route to purge (data loss)")
	}
	if sess.ArchivedAt != nil || sess.ArchiveRef != nil {
		t.Error("archive-policy session falsely marked archived without a real provider artifact")
	}
	if e.auditCount(audit.ActionSessionTrash) != 0 || e.auditCount(audit.ActionSessionArchive) != 0 {
		t.Error("a lifecycle audit row was written for a deferred archive action")
	}
}

// --- Trash-grace purge ---

// TestSweep_TrashGracePurge proves the §12.5 unconditional second pass: a trashed
// session past purge_after is purged (one session.purge audit row by the service
// principal); a trashed session whose deadline is still in the future survives.
func TestSweep_TrashGracePurge(t *testing.T) {
	e := newEnv(t)
	past := fixedNow.Add(-1 * time.Hour)
	future := fixedNow.Add(48 * time.Hour)
	trashedAt := fixedNow.Add(-31 * 24 * time.Hour)
	e.createSession("s-due", "alice", trashedAt, &trashedAt, &past)
	e.createSession("s-waiting", "bob", trashedAt, &trashedAt, &future)

	e.newSweeper(nil, nil).Sweep(context.Background())

	if e.sessionExists("s-due") {
		t.Error("trashed session past purge_after not purged")
	}
	if !e.sessionExists("s-waiting") {
		t.Error("trashed session before purge_after wrongly purged")
	}
	if got := e.auditCount(audit.ActionSessionPurge); got != 1 {
		t.Errorf("purge audit rows = %d, want exactly 1", got)
	}
	var principal string
	_ = e.db.QueryRow(`SELECT principal FROM audit_log WHERE action=?`, audit.ActionSessionPurge).Scan(&principal)
	if principal != string(e.princ) {
		t.Errorf("purge audit principal = %q, want service principal %q", principal, e.princ)
	}
}

// --- Same-tx rollback (effect↔audit coupling holds outside HTTP) ---

type failingAudit struct{}

func (failingAudit) InsertTx(_ context.Context, _ *sql.Tx, _ audit.Event) error {
	return errors.New("forced audit failure")
}

// TestSweep_AuditFailureRollsBack proves the effect↔audit same-transaction
// coupling holds on the autonomous path: a forced audit-write failure rolls the
// trash effect back, leaving the session active and unrecorded.
func TestSweep_AuditFailureRollsBack(t *testing.T) {
	e := newEnv(t)
	e.setPolicy(intPtr(7), 30, sessionmodel.TerminalActionTrashThenPurge)
	e.createSession("s-stale", "alice", fixedNow.Add(-30*24*time.Hour), nil, nil)

	e.newSweeper(nil, failingAudit{}).Sweep(context.Background())

	sess, _ := e.repo.GetSession(context.Background(), "s-stale")
	if sess == nil {
		t.Fatal("session vanished — the failed transaction must roll back, not delete")
	}
	if sess.TrashedAt != nil {
		t.Error("trash effect persisted despite audit failure — same-tx coupling broken")
	}
	if got := e.auditCount(audit.ActionSessionTrash); got != 0 {
		t.Errorf("trash audit rows = %d, want 0 (rolled back)", got)
	}
}

// --- Fail-safe (one session's error does not abort the batch) ---

type failPurgeStore struct {
	sessionsweeper.SessionStore
	failID string
}

func (f failPurgeStore) PurgeSessionTx(ctx context.Context, tx *sql.Tx, id string) error {
	if id == f.failID {
		return errors.New("forced purge failure for " + id)
	}
	return f.SessionStore.PurgeSessionTx(ctx, tx, id)
}

// TestSweep_FailSafePerSession proves a forced error on one session is logged and
// skipped without aborting the others and without leaving partial state: the two
// healthy sessions purge, the failing one survives intact.
func TestSweep_FailSafePerSession(t *testing.T) {
	e := newEnv(t)
	trashedAt := fixedNow.Add(-31 * 24 * time.Hour)
	past := fixedNow.Add(-1 * time.Hour)
	for _, id := range []string{"s-1", "s-bad", "s-3"} {
		e.createSession(id, "alice", trashedAt, &trashedAt, &past)
	}
	store := failPurgeStore{SessionStore: e.repo, failID: "s-bad"}

	e.newSweeper(store, nil).Sweep(context.Background())

	if e.sessionExists("s-1") || e.sessionExists("s-3") {
		t.Error("a healthy session was not purged — the batch aborted on the failing one")
	}
	if !e.sessionExists("s-bad") {
		t.Error("the failing session was purged — its transaction must roll back cleanly")
	}
	if got := e.auditCount(audit.ActionSessionPurge); got != 2 {
		t.Errorf("purge audit rows = %d, want 2 (only the healthy sessions)", got)
	}
}

// --- Login-flow drain (distinct subsystem) ---

func (e *env) insertFlow(state string, expiresAt time.Time) {
	e.t.Helper()
	if err := e.auth.CreateFlow(context.Background(), auth.LoginFlow{
		State:        state,
		CodeVerifier: "v-" + state,
		Nonce:        "n-" + state,
		CreatedAt:    expiresAt.Add(-10 * time.Minute),
		ExpiresAt:    expiresAt,
	}); err != nil {
		e.t.Fatalf("insertFlow %s: %v", state, err)
	}
}

func (e *env) flowExists(state string) bool {
	e.t.Helper()
	f, err := e.auth.GetFlow(context.Background(), state)
	if err != nil {
		e.t.Fatalf("GetFlow %s: %v", state, err)
	}
	return f != nil
}

// TestSweep_LoginFlowDrainIsDistinct proves the drain removes ONLY abandoned
// (expired) auth_login_flows and touches NO agent_sessions row — the two
// responsibilities stay distinct even in one worker. It runs with inactivity OFF
// so the only session activity that could occur would be erroneous.
func TestSweep_LoginFlowDrainIsDistinct(t *testing.T) {
	e := newEnv(t)
	e.insertFlow("abandoned", fixedNow.Add(-1*time.Hour))  // expired
	e.insertFlow("inflight", fixedNow.Add(10*time.Minute)) // still valid
	e.createSession("s-keep", "alice", fixedNow.Add(-365*24*time.Hour), nil, nil)

	e.newSweeper(nil, nil).Sweep(context.Background())

	if e.flowExists("abandoned") {
		t.Error("abandoned login flow not drained")
	}
	if !e.flowExists("inflight") {
		t.Error("in-flight login flow wrongly drained")
	}
	if !e.sessionExists("s-keep") {
		t.Error("login-flow drain touched a chat session — responsibilities conflated")
	}
}

// --- Legacy-table guard (the sweep never reaches the legacy tables) ---

// TestSweep_NeverTouchesLegacyTables proves the §13 hard constraint by effect:
// rows seeded into the legacy migration-001 sessions / session_messages tables
// survive a full sweep (inactivity expiry + trash-grace purge + login drain)
// untouched. The sweep's scans target only agent_sessions and auth_login_flows.
func TestSweep_NeverTouchesLegacyTables(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	// Seed the legacy tables directly (they are NOT the redesigned tables).
	if _, err := e.db.Exec(
		`INSERT INTO sessions (id, summary) VALUES ('legacy-1', 'legacy row')`); err != nil {
		t.Fatalf("seed legacy sessions: %v", err)
	}
	if _, err := e.db.Exec(
		`INSERT INTO session_messages (session_id, role, content, created_at) VALUES ('legacy-1', 'user', 'hi', ?)`,
		fixedNow.Format(time.RFC3339)); err != nil {
		t.Fatalf("seed legacy session_messages: %v", err)
	}

	// Make the sweep do real work across all three steps.
	e.setPolicy(intPtr(7), 30, sessionmodel.TerminalActionTrashThenPurge)
	e.createSession("s-stale", "alice", fixedNow.Add(-30*24*time.Hour), nil, nil)
	trashedAt := fixedNow.Add(-31 * 24 * time.Hour)
	past := fixedNow.Add(-1 * time.Hour)
	e.createSession("s-due", "bob", trashedAt, &trashedAt, &past)
	e.insertFlow("abandoned", fixedNow.Add(-1*time.Hour))

	e.newSweeper(nil, nil).Sweep(ctx)

	var legacySessions, legacyMessages int
	if err := e.db.QueryRow(`SELECT count(*) FROM sessions`).Scan(&legacySessions); err != nil {
		t.Fatalf("count legacy sessions: %v", err)
	}
	if err := e.db.QueryRow(`SELECT count(*) FROM session_messages`).Scan(&legacyMessages); err != nil {
		t.Fatalf("count legacy session_messages: %v", err)
	}
	if legacySessions != 1 || legacyMessages != 1 {
		t.Errorf("legacy rows changed by sweep: sessions=%d messages=%d, want 1/1", legacySessions, legacyMessages)
	}
	// Sanity: the sweep DID act on the redesigned tables.
	if e.sessionExists("s-due") {
		t.Error("sweep did not purge the due session — test would not exercise the purge path")
	}
}
