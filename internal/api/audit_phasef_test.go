package api_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/runmodel"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
)

// newRegimeServerWithAudit is newRegimeServer + an audit.Repository wired
// into services. Returns the test server, the repos, and the underlying
// SQL DB (the test queries audit_log directly because audit.Repository
// deliberately exposes no list method).
func newRegimeServerWithAudit(t *testing.T) (
	ts *httptest.Server,
	sessRepo sessionmodel.Repository,
	rbacRepo rbac.Repository,
	db *sql.DB,
) {
	t.Helper()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	sessRepo = sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	rbacRepo = rbac.NewRepository(s.DB(), store.DriverSQLite)
	auditRepo := audit.NewRepository(s.DB(), store.DriverSQLite)
	runRepo := runmodel.NewRepository(s.DB(), store.DriverSQLite)
	captainSvc := sessionmodel.NewCaptainService(sessRepo, runRepo, 90)

	svc := &core.Services{
		Store:        s,
		SessionModel: sessRepo,
		RBAC:         rbacRepo,
		Audit:        auditRepo,
		RunModel:     runRepo,
		CaptainSvc:   captainSvc,
	}
	srv := api.New(svc)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	handler := rbac.IdentityMiddleware(testPrincipalProvider{})(mux)
	ts = httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts, sessRepo, rbacRepo, s.DB()
}

// countAuditRows reads the audit_log row count by a free-form WHERE clause.
func countAuditRows(t *testing.T, db *sql.DB, where string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM audit_log WHERE "+where, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// TestPhaseF_Bug3_IncidentHistorySurvivesResolve is the bug #3 regression
// (joe-identity-design.md §0 bug #3): declare an incident, resolve it,
// then assert the audit table STILL contains a durable record of who
// declared it and when. This test would FAIL on pre-Phase-F code (where
// system_regime.declared_by_principal is nulled on resolve and there is
// no audit table); it PASSES because the durable record lives in audit_log
// and is independent of the mutable system_regime row.
func TestPhaseF_Bug3_IncidentHistorySurvivesResolve(t *testing.T) {
	ts, sessRepo, rbacRepo, db := newRegimeServerWithAudit(t)
	grantRegimeControl(t, rbacRepo, "alice")

	// 1. Declare (promote-in-place, §12.3).
	sid := createDefaultSession(t, sessRepo, "alice")
	rDeclare := doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/declare", "alice",
		map[string]string{"session_id": sid})
	if rDeclare.StatusCode != http.StatusCreated {
		t.Fatalf("declare status = %d, want 201", rDeclare.StatusCode)
	}
	rDeclare.Body.Close()

	// After declare: audit log holds one declare-allow row for alice.
	if n := countAuditRows(t, db,
		"action = ? AND principal = ? AND decision = 'allow'",
		audit.ActionDeclareIncident, "alice"); n != 1 {
		t.Fatalf("after declare: durable declare rows for alice = %d, want 1", n)
	}

	// Advance state to believed_mitigated then resolve.
	sessions, err := sessRepo.ListSessionsByType(context.Background(), sessionmodel.SessionTypeIncident)
	if err != nil {
		t.Fatalf("list incident sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 incident session, got %d", len(sessions))
	}
	if err := sessRepo.UpdateIncidentState(context.Background(), sessions[0].ID, sessionmodel.IncidentStateBelievedMitigated); err != nil {
		t.Fatalf("update state: %v", err)
	}

	// 2. Resolve.
	rResolve := doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/resolve", "alice", nil)
	if rResolve.StatusCode != http.StatusOK {
		t.Fatalf("resolve status = %d, want 200", rResolve.StatusCode)
	}
	rResolve.Body.Close()

	// 3. THE bug #3 assertion: after resolve, the mutable system_regime
	// row's declared_by_principal is null (this is the pre-Phase-F
	// behaviour preserved in the existing repo). The DURABLE trail must
	// still hold the declare record.
	var declaredBy sql.NullString
	if err := db.QueryRowContext(context.Background(),
		`SELECT declared_by_principal FROM system_regime WHERE id = 1`).
		Scan(&declaredBy); err != nil {
		t.Fatalf("read system_regime: %v", err)
	}
	if declaredBy.Valid {
		t.Errorf("system_regime.declared_by_principal = %q after resolve, want NULL — the test's premise is broken (resolve no longer nulls it)", declaredBy.String)
	}

	// THE Phase F durable-record assertion.
	if n := countAuditRows(t, db,
		"action = ? AND principal = ? AND decision = 'allow'",
		audit.ActionDeclareIncident, "alice"); n != 1 {
		t.Fatalf("after resolve: durable declare rows for alice = %d, want 1 — bug #3 has regressed (history erased)", n)
	}
	// And the resolve event is also durably recorded.
	if n := countAuditRows(t, db,
		"action = ? AND principal = ? AND decision = 'allow'",
		audit.ActionResolveIncident, "alice"); n != 1 {
		t.Fatalf("after resolve: durable resolve rows for alice = %d, want 1", n)
	}
}

// TestPhaseF_CaptainTransitionsSurviveResolve — companion test: durable
// captain-attach record survives the cascade-delete on session removal
// (session_captains has ON DELETE CASCADE — migration 009:62) and a
// regime resolve. The captain row in session_captains is created
// atomically by DeclareIncidentRegime (R-CAP1); after delete, the only
// trace is the audit row.
func TestPhaseF_CaptainTransitionsSurviveResolve(t *testing.T) {
	ts, sessRepo, rbacRepo, db := newRegimeServerWithAudit(t)
	grantRegimeControl(t, rbacRepo, "alice")

	// Declare → R-CAP1 atomically attaches alice as captain in the repo
	// (no HTTP /captain/attach hit, no audit row yet — R-CAP1 is internal
	// to the declare path; the declare audit row covers "who took
	// command" via Reason=transition_recorded).
	sid := createDefaultSession(t, sessRepo, "alice")
	rDeclare := doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/declare", "alice",
		map[string]string{"session_id": sid})
	rDeclare.Body.Close()
	sessions, err := sessRepo.ListSessionsByType(context.Background(), sessionmodel.SessionTypeIncident)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	sessionID := sessions[0].ID

	// Drive the resolve and delete the session — cascade-delete removes
	// session_captains.
	if err := sessRepo.UpdateIncidentState(context.Background(), sessionID, sessionmodel.IncidentStateBelievedMitigated); err != nil {
		t.Fatalf("update state: %v", err)
	}
	rResolve := doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/resolve", "alice", nil)
	rResolve.Body.Close()
	if err := sessRepo.DeleteSession(context.Background(), sessionID); err != nil {
		t.Fatalf("delete session: %v", err)
	}

	// Mutable session_captains rows are gone (cascade-delete).
	var capCount int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM session_captains WHERE session_id = ?`, sessionID).
		Scan(&capCount); err != nil {
		t.Fatalf("count session_captains: %v", err)
	}
	if capCount != 0 {
		t.Fatalf("session_captains rows for deleted session = %d, want 0 — test premise broken", capCount)
	}

	// Durable trail still has both regime transitions.
	if n := countAuditRows(t, db,
		"action IN (?, ?) AND principal = ? AND decision = 'allow'",
		audit.ActionDeclareIncident, audit.ActionResolveIncident, "alice"); n != 2 {
		t.Fatalf("durable declare+resolve rows for alice after session delete = %d, want 2", n)
	}
}

// TestPhaseF_CaptainAttachWritesAuditRow — explicit captain HTTP attach
// produces a durable captain_attach row that survives the session being
// deleted afterwards. (The endpoint is informational outside incident
// regime per §B4; the audit row is written regardless because the
// transition event happened.)
func TestPhaseF_CaptainAttachWritesAuditRow(t *testing.T) {
	ts, sessRepo, rbacRepo, db := newRegimeServerWithAudit(t)
	grantRegimeControl(t, rbacRepo, "alice")

	// Declare an incident so the captain endpoints have a session.
	sid := createDefaultSession(t, sessRepo, "alice")
	rDeclare := doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/declare", "alice",
		map[string]string{"session_id": sid})
	rDeclare.Body.Close()
	sessions, err := sessRepo.ListSessionsByType(context.Background(), sessionmodel.SessionTypeIncident)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 incident session, got %d", len(sessions))
	}
	sessionID := sessions[0].ID

	// Second human (bob) attaches as observer (§A3); audit row is written
	// regardless of whether they became captain.
	rAttach := doRequest(t, http.MethodPost,
		ts.URL+"/api/v1/agent-sessions/"+sessionID+"/captain/attach", "bob", nil)
	if rAttach.StatusCode != http.StatusOK {
		t.Fatalf("attach status = %d, want 200; body=%v", rAttach.StatusCode, rAttach.Body)
	}
	rAttach.Body.Close()

	if n := countAuditRows(t, db,
		"action = ? AND principal = ? AND decision = 'allow'",
		audit.ActionCaptainAttach, "bob"); n != 1 {
		t.Fatalf("captain_attach rows for bob = %d, want 1", n)
	}

	// Delete the session and prove the audit row still exists.
	if err := sessRepo.DeleteSession(context.Background(), sessionID); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	if n := countAuditRows(t, db,
		"action = ? AND principal = ? AND decision = 'allow'",
		audit.ActionCaptainAttach, "bob"); n != 1 {
		t.Fatalf("after session delete: captain_attach rows for bob = %d, want 1 — cascade leaked into audit", n)
	}
}

// TestPhaseF_DeclareDenialWritesAuditRow — a denied declare (no policy)
// produces a deny audit row. Important: even rejected transitions are
// durably recorded.
func TestPhaseF_DeclareDenialWritesAuditRow(t *testing.T) {
	ts, _, _, db := newRegimeServerWithAudit(t)
	// Note: NO grant for mallory.

	resp := doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/declare", "mallory", nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("declare status = %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()

	if n := countAuditRows(t, db,
		"action = ? AND principal = ? AND decision = 'deny'",
		audit.ActionDeclareIncident, "mallory"); n != 1 {
		t.Fatalf("declare deny rows for mallory = %d, want 1", n)
	}
}
