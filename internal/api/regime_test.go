package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
)

// createDefaultSession inserts a 'default' session owned by creator and returns
// its id. Promote-in-place (§12.3) means incident declaration takes an existing
// session, so these tests create one first. creator may differ from the
// principal that later declares the incident (proving creator ≠ captain).
func createDefaultSession(t *testing.T, repo sessionmodel.Repository, creator string) string {
	t.Helper()
	sid := uuid.NewString()
	if _, err := repo.CreateSession(context.Background(), sessionmodel.AgentSession{
		ID: sid, Type: sessionmodel.SessionTypeDefault, CreatorPrincipal: creator,
	}); err != nil {
		t.Fatalf("create default session: %v", err)
	}
	return sid
}

// testPrincipalProvider reads X-Test-Principal from the request and uses it
// as the principal. Lets a single test server vary the caller per request.
// "" → rbac.Unknown (simulates an unauthenticated request).
type testPrincipalProvider struct{}

func (testPrincipalProvider) Identity(r *http.Request) rbac.Principal {
	p := r.Header.Get("X-Test-Principal")
	if p == "" {
		return rbac.Unknown
	}
	return rbac.Principal(p)
}

// newRegimeServer wires the full session-model + RBAC stack with the test
// principal middleware. Returns the test server, the session-model repo,
// and the RBAC repo (so tests can mutate policies).
func newRegimeServer(t *testing.T) (*httptest.Server, sessionmodel.Repository, rbac.Repository) {
	t.Helper()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	sessRepo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	rbacRepo := rbac.NewRepository(s.DB(), store.DriverSQLite)

	svc := &core.Services{
		Store:        s,
		SessionModel: sessRepo,
		RBAC:         rbacRepo,
	}
	// The regime-control zone check reads the injected engine (rbac-engine-split);
	// this stack has RBAC wired but no service accounts, so pass the same bare
	// engine the handler used to construct on demand — preserving the declare/
	// resolve authorization outcomes these tests assert.
	srv := api.New(svc, rbac.NewPolicyEngine(rbacRepo))
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// Wrap with IdentityMiddleware backed by the test principal provider.
	handler := rbac.IdentityMiddleware(testPrincipalProvider{})(mux)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts, sessRepo, rbacRepo
}

// doRequest is a helper for tests that need to set headers.
func doRequest(t *testing.T, method, url, principal string, body any) *http.Response {
	t.Helper()
	var bodyReader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		bodyReader = bytes.NewReader(b)
	} else {
		bodyReader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if principal != "" {
		req.Header.Set("X-Test-Principal", principal)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// grantRegimeControl gives the principal a policy entry for the
// regime-control zone (seeded by migration 012). The test helper exists
// because there is no admin API yet for this in Change 5; policies are
// managed through the rbac repo directly in tests.
func grantRegimeControl(t *testing.T, rbacRepo rbac.Repository, principal string) {
	t.Helper()
	if _, err := rbacRepo.CreatePolicy(context.Background(), rbac.Policy{
		Principal: principal,
		ZoneID:    "regime-control",
	}, "test"); err != nil {
		t.Fatalf("grant regime-control to %s: %v", principal, err)
	}
}

func TestRegimeDeclare_HappyPath(t *testing.T) {
	ts, sessRepo, rbacRepo := newRegimeServer(t)
	grantRegimeControl(t, rbacRepo, "alice")

	// Promote-in-place (§12.3): declaration promotes an existing 'default'
	// session designated by session_id.
	sid := createDefaultSession(t, sessRepo, "alice")
	resp := doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/declare", "alice",
		map[string]string{"session_id": sid})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var body struct {
		SessionID  string `json:"session_id"`
		CaptainID  string `json:"captain_id"`
		DeclaredBy string `json:"declared_by"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()

	if body.SessionID == "" || body.CaptainID == "" {
		t.Fatalf("declare returned empty ids: %+v", body)
	}
	// Promote-in-place: the returned session id is the SAME row we created.
	if body.SessionID != sid {
		t.Errorf("declare returned session_id = %q, want the promoted session %q (mint-fresh regression)", body.SessionID, sid)
	}
	if body.DeclaredBy != "alice" {
		t.Errorf("declared_by = %q, want alice", body.DeclaredBy)
	}

	// Regime is incident.
	reg, err := sessRepo.GetRegime(context.Background())
	if err != nil {
		t.Fatalf("GetRegime: %v", err)
	}
	if reg.Mode != sessionmodel.RegimeModeIncident {
		t.Errorf("regime.mode = %q, want incident", reg.Mode)
	}
	if reg.DeclaredByPrincipal == nil || *reg.DeclaredByPrincipal != "alice" {
		t.Errorf("declared_by_principal mismatch: %+v", reg.DeclaredByPrincipal)
	}
	if reg.DeclaredKind == nil || *reg.DeclaredKind != sessionmodel.RegimeKindHuman {
		t.Errorf("declared_kind = %+v, want human", reg.DeclaredKind)
	}

	// Session created in 'declared' state with alice as creator.
	sess, err := sessRepo.GetSession(context.Background(), body.SessionID)
	if err != nil || sess == nil {
		t.Fatalf("GetSession: %v %v", err, sess)
	}
	if sess.Type != sessionmodel.SessionTypeIncident {
		t.Errorf("session type = %q, want incident", sess.Type)
	}
	if sess.IncidentState == nil || *sess.IncidentState != sessionmodel.IncidentStateDeclared {
		t.Errorf("incident_state = %+v, want declared", sess.IncidentState)
	}
	if sess.CreatorPrincipal != "alice" {
		t.Errorf("creator_principal = %q, want alice", sess.CreatorPrincipal)
	}

	// Captain attached (R-CAP1).
	cap, err := sessRepo.GetActiveCaptain(context.Background(), body.SessionID)
	if err != nil || cap == nil {
		t.Fatalf("GetActiveCaptain: %v %v", err, cap)
	}
	if cap.Principal != "alice" {
		t.Errorf("captain principal = %q, want alice", cap.Principal)
	}
	if cap.CaptainType != sessionmodel.CaptainTypeHuman {
		t.Errorf("captain type = %q, want human", cap.CaptainType)
	}
}

func TestRegimeDeclare_Unauthorized(t *testing.T) {
	ts, _, _ := newRegimeServer(t)
	// alice was never granted regime-control.

	resp := doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/declare", "alice", nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestRegimeDeclare_AlreadyIncident(t *testing.T) {
	ts, sessRepo, rbacRepo := newRegimeServer(t)
	grantRegimeControl(t, rbacRepo, "alice")

	// First declare promotes session A. Second declare designates a fresh,
	// eligible session B — it must still be refused with 409 because the
	// global regime is already incident (the "no second concurrent incident"
	// rule, §12.3), NOT because session B is ineligible.
	sidA := createDefaultSession(t, sessRepo, "alice")
	resp1 := doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/declare", "alice",
		map[string]string{"session_id": sidA})
	if resp1.StatusCode != http.StatusCreated {
		t.Fatalf("first declare: %d", resp1.StatusCode)
	}
	resp1.Body.Close()

	sidB := createDefaultSession(t, sessRepo, "alice")
	resp2 := doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/declare", "alice",
		map[string]string{"session_id": sidB})
	if resp2.StatusCode != http.StatusConflict {
		t.Fatalf("second declare status = %d, want 409", resp2.StatusCode)
	}
	resp2.Body.Close()

	// Session B was untouched — it must still be a 'default' session.
	sessB, _ := sessRepo.GetSession(context.Background(), sidB)
	if sessB == nil || sessB.Type != sessionmodel.SessionTypeDefault {
		t.Errorf("session B = %+v, want untouched default after refused second declare", sessB)
	}
}

func TestRegimeResolve_HappyPath(t *testing.T) {
	ts, sessRepo, rbacRepo := newRegimeServer(t)
	grantRegimeControl(t, rbacRepo, "alice")

	// Declare first (promote-in-place, §12.3).
	sid := createDefaultSession(t, sessRepo, "alice")
	rDeclare := doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/declare", "alice",
		map[string]string{"session_id": sid})
	var declareBody struct {
		SessionID string `json:"session_id"`
	}
	json.NewDecoder(rDeclare.Body).Decode(&declareBody)
	rDeclare.Body.Close()

	// Manually advance session to 'believed_mitigated' (the believed_mitigated
	// transition flow lives in Change 6+; bypass the repo's CreateSession
	// API and reach for the underlying DB to update state directly).
	// In Change 6 this becomes a captain-driven transition; for now, the
	// test directly mutates via SQL.
	advanceIncidentToMitigated(t, sessRepo, declareBody.SessionID)

	rResolve := doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/resolve", "alice", nil)
	if rResolve.StatusCode != http.StatusOK {
		t.Fatalf("resolve status = %d, want 200", rResolve.StatusCode)
	}
	var resolveBody struct {
		SessionID string `json:"session_id"`
	}
	json.NewDecoder(rResolve.Body).Decode(&resolveBody)
	rResolve.Body.Close()
	if resolveBody.SessionID != declareBody.SessionID {
		t.Errorf("resolved session = %q, want %q", resolveBody.SessionID, declareBody.SessionID)
	}

	reg, _ := sessRepo.GetRegime(context.Background())
	if reg.Mode != sessionmodel.RegimeModeNormal {
		t.Errorf("regime after resolve = %q, want normal", reg.Mode)
	}
	sess, _ := sessRepo.GetSession(context.Background(), declareBody.SessionID)
	if sess.IncidentState == nil || *sess.IncidentState != sessionmodel.IncidentStateResolved {
		t.Errorf("session state after resolve = %+v, want resolved", sess.IncidentState)
	}
}

func TestRegimeResolve_Unauthorized(t *testing.T) {
	ts, sessRepo, rbacRepo := newRegimeServer(t)
	grantRegimeControl(t, rbacRepo, "alice")
	// Declare so there's an incident to resolve (promote-in-place, §12.3).
	sid := createDefaultSession(t, sessRepo, "alice")
	doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/declare", "alice",
		map[string]string{"session_id": sid}).Body.Close()

	// bob has no policy.
	resp := doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/resolve", "bob", nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestRegimeResolve_NotIncident(t *testing.T) {
	ts, _, rbacRepo := newRegimeServer(t)
	grantRegimeControl(t, rbacRepo, "alice")

	resp := doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/resolve", "alice", nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestRegimeResolve_NotMitigated(t *testing.T) {
	ts, sessRepo, rbacRepo := newRegimeServer(t)
	grantRegimeControl(t, rbacRepo, "alice")
	sid := createDefaultSession(t, sessRepo, "alice")
	doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/declare", "alice",
		map[string]string{"session_id": sid}).Body.Close()
	// Session is in 'declared' — not yet 'believed_mitigated'.

	resp := doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/resolve", "alice", nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (not yet believed_mitigated)", resp.StatusCode)
	}
	resp.Body.Close()
}

// forcedRollback is a hook that always returns an error, used to prove
// the single-transaction property in the two rollback tests below.
func forcedRollback(*sql.Tx) error { return errForced }

var errForced = errors.New("forced rollback for single-tx test")

// TestRegimeDeclare_SingleTransactionRollback proves the promote-in-place
// transition is atomic: forcing a rollback after the captain attach must also
// roll back the session promotion and the regime flip, leaving NO half-applied
// state (no promoted-but-no-captain, no regime-flipped-but-not-promoted). Uses
// the DeclareIncidentRegimeWithHook test seam on *SQLRepository.
func TestRegimeDeclare_SingleTransactionRollback(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	defer s.Close()
	repo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)

	pre, _ := repo.GetRegime(ctx)
	if pre.Mode != sessionmodel.RegimeModeNormal {
		t.Fatalf("precondition: regime = %q, want normal", pre.Mode)
	}

	// An existing 'default' session is the promote target.
	sid := uuid.NewString()
	if _, err := repo.CreateSession(ctx, sessionmodel.AgentSession{
		ID: sid, Type: sessionmodel.SessionTypeDefault, CreatorPrincipal: "alice",
	}); err != nil {
		t.Fatalf("create default session: %v", err)
	}

	_, _, err = repo.DeclareIncidentRegimeWithHook(ctx,
		"alice", sid, sessionmodel.RegimeKindHuman, forcedRollback)
	if !errors.Is(err, errForced) {
		t.Fatalf("expected forced rollback error, got %v", err)
	}

	// Regime must still be normal (regime flip rolled back).
	post, _ := repo.GetRegime(ctx)
	if post.Mode != sessionmodel.RegimeModeNormal {
		t.Errorf("post-rollback regime = %q, want normal — single-tx property failed", post.Mode)
	}
	// The session must still be 'default' (promotion rolled back), and there
	// must be NO incident session at all.
	sess, _ := repo.GetSession(ctx, sid)
	if sess == nil || sess.Type != sessionmodel.SessionTypeDefault || sess.IncidentState != nil {
		t.Errorf("post-rollback session = %+v, want untouched 'default' — promotion leaked", sess)
	}
	all, _ := repo.ListSessionsByType(ctx, sessionmodel.SessionTypeIncident)
	if len(all) != 0 {
		t.Errorf("post-rollback incident sessions = %d, want 0 — session promoted despite rollback", len(all))
	}
	// And no captain was left attached.
	if _, ok, _ := repo.CurrentCaptainPrincipal(ctx, sid); ok {
		t.Errorf("post-rollback captain attached — captain leaked past the rolled-back tx")
	}
}

// TestRegimeDeclare_PromotesInPlace is the headline B004 acceptance: declaring
// an incident on an existing 'default' session PROMOTES that same session — same
// id — to an incident in state 'declared', rather than minting a fresh row. It
// also proves the promoted session keeps its ORIGINAL creator while the captain
// is the DECLARER, so creator and captain may differ (§12.3).
func TestRegimeDeclare_PromotesInPlace(t *testing.T) {
	ctx := context.Background()
	ts, sessRepo, rbacRepo := newRegimeServer(t)
	grantRegimeControl(t, rbacRepo, "alice")

	// bob owns the pre-existing default session; alice declares the incident.
	sid := createDefaultSession(t, sessRepo, "bob")

	before, _ := sessRepo.ListSessions(ctx)
	countBefore := len(before)

	resp := doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/declare", "alice",
		map[string]string{"session_id": sid})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var body struct {
		SessionID string `json:"session_id"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()

	// No new row was minted — the count is unchanged and the promoted id is sid.
	after, _ := sessRepo.ListSessions(ctx)
	if len(after) != countBefore {
		t.Errorf("session count = %d, want %d — a fresh row was minted (mint-fresh regression)", len(after), countBefore)
	}
	if body.SessionID != sid {
		t.Errorf("declare promoted session_id = %q, want the original %q", body.SessionID, sid)
	}

	// The SAME session is now the incident in state 'declared'.
	sess, _ := sessRepo.GetSession(ctx, sid)
	if sess == nil {
		t.Fatalf("promoted session %q missing", sid)
	}
	if sess.Type != sessionmodel.SessionTypeIncident {
		t.Errorf("type = %q, want incident", sess.Type)
	}
	if sess.IncidentState == nil || *sess.IncidentState != sessionmodel.IncidentStateDeclared {
		t.Errorf("incident_state = %+v, want declared", sess.IncidentState)
	}
	// Creator is PRESERVED (bob), not overwritten by the declarer.
	if sess.CreatorPrincipal != "bob" {
		t.Errorf("creator_principal = %q, want bob (preserved across promote)", sess.CreatorPrincipal)
	}
	// Captain is the DECLARER (alice) — creator ≠ captain.
	cap, _ := sessRepo.GetActiveCaptain(ctx, sid)
	if cap == nil || cap.Principal != "alice" {
		t.Fatalf("captain = %+v, want principal alice (the declarer)", cap)
	}
	if cap.Principal == sess.CreatorPrincipal {
		t.Errorf("captain (%q) equals creator (%q) — the test must prove they may differ",
			cap.Principal, sess.CreatorPrincipal)
	}
}

// TestRegimeDeclare_ClearsLinkedIncidentOnPromote proves a session that was
// participating in a prior (resolved) incident sheds its linked_incident_id
// pointer when it is itself promoted — required by the migration-025 CHECK
// (linked_incident_id IS NULL OR type <> 'incident').
func TestRegimeDeclare_ClearsLinkedIncidentOnPromote(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	defer s.Close()
	repo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)

	// anchor is any existing session the FK can point at; target carries a
	// linked_incident_id to it and is then promoted.
	anchor := createDefaultSession(t, repo, "carol")
	targetID := uuid.NewString()
	link := anchor
	if _, err := repo.CreateSession(ctx, sessionmodel.AgentSession{
		ID: targetID, Type: sessionmodel.SessionTypeDefault, CreatorPrincipal: "carol",
		LinkedIncidentID: &link,
	}); err != nil {
		t.Fatalf("create linked session: %v", err)
	}

	if _, _, err := repo.DeclareIncidentRegime(ctx, "alice", targetID, sessionmodel.RegimeKindHuman); err != nil {
		t.Fatalf("promote linked session: %v", err)
	}

	sess, _ := repo.GetSession(ctx, targetID)
	if sess == nil || sess.Type != sessionmodel.SessionTypeIncident {
		t.Fatalf("promoted session = %+v, want incident", sess)
	}
	if sess.LinkedIncidentID != nil {
		t.Errorf("linked_incident_id = %v, want nil (cleared on promote, CHECK requires it)", *sess.LinkedIncidentID)
	}
}

// TestRegimeDeclare_RejectsIneligibleSession proves the promote-in-place
// preconditions: a missing session is rejected with ErrNotFound, and an
// already-incident session is rejected with ErrSessionAlreadyIncident rather
// than double-promoted.
func TestRegimeDeclare_RejectsIneligibleSession(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	defer s.Close()
	repo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)

	// (a) Missing session → ErrNotFound.
	if _, _, err := repo.DeclareIncidentRegime(ctx, "alice", "does-not-exist", sessionmodel.RegimeKindHuman); !errors.Is(err, sessionmodel.ErrNotFound) {
		t.Errorf("declare on missing session: err = %v, want ErrNotFound", err)
	}

	// (b) Promote a session, resolve the regime so a second declare can pass
	// the regime-normal precondition, then try to promote the SAME (now
	// incident/resolved) session again → ErrSessionAlreadyIncident.
	sid := createDefaultSession(t, repo, "alice")
	if _, _, err := repo.DeclareIncidentRegime(ctx, "alice", sid, sessionmodel.RegimeKindHuman); err != nil {
		t.Fatalf("first promote: %v", err)
	}
	if err := repo.UpdateIncidentState(ctx, sid, sessionmodel.IncidentStateBelievedMitigated); err != nil {
		t.Fatalf("advance to mitigated: %v", err)
	}
	if _, err := repo.ResolveIncidentRegime(ctx, "alice"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if _, _, err := repo.DeclareIncidentRegime(ctx, "alice", sid, sessionmodel.RegimeKindHuman); !errors.Is(err, sessionmodel.ErrSessionAlreadyIncident) {
		t.Errorf("re-promote already-incident session: err = %v, want ErrSessionAlreadyIncident", err)
	}
}

// TestRegimeDeclare_RequiresSessionID proves the HTTP declare handler refuses a
// declaration with no session_id (400), since promote-in-place always
// designates an existing session, while a denied caller still gets 403 first.
func TestRegimeDeclare_RequiresSessionID(t *testing.T) {
	ts, _, rbacRepo := newRegimeServer(t)
	grantRegimeControl(t, rbacRepo, "alice")

	// Authorized caller, no session_id → 400.
	resp := doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/declare", "alice", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing session_id: status = %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()

	// Unauthorized caller, no session_id → 403 (authz precedes the 400).
	resp2 := doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/declare", "mallory", nil)
	if resp2.StatusCode != http.StatusForbidden {
		t.Fatalf("unauthorized + missing session_id: status = %d, want 403", resp2.StatusCode)
	}
	resp2.Body.Close()
}

// TestRegimeResolve_SingleTransactionRollback: same property for resolve.
// After UPDATE session.state succeeds, forcing a failure before UPDATE
// regime.mode must roll back the session update too.
func TestRegimeResolve_SingleTransactionRollback(t *testing.T) {
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	defer s.Close()
	repo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)

	sessionID := createDefaultSession(t, repo, "alice")
	if _, _, err := repo.DeclareIncidentRegime(context.Background(), "alice", sessionID, sessionmodel.RegimeKindHuman); err != nil {
		t.Fatalf("declare: %v", err)
	}
	if err := repo.UpdateIncidentState(context.Background(), sessionID,
		sessionmodel.IncidentStateBelievedMitigated); err != nil {
		t.Fatalf("advance to mitigated: %v", err)
	}

	_, err = repo.ResolveIncidentRegimeWithHook(context.Background(), "alice", forcedRollback)
	if !errors.Is(err, errForced) {
		t.Fatalf("expected forced rollback error, got %v", err)
	}

	// Regime must still be incident.
	reg, _ := repo.GetRegime(context.Background())
	if reg.Mode != sessionmodel.RegimeModeIncident {
		t.Errorf("post-rollback regime = %q, want incident — single-tx property failed", reg.Mode)
	}
	// Session state must still be believed_mitigated, not resolved.
	sess, _ := repo.GetSession(context.Background(), sessionID)
	if sess.IncidentState == nil || *sess.IncidentState != sessionmodel.IncidentStateBelievedMitigated {
		t.Errorf("post-rollback session state = %+v, want believed_mitigated — UPDATE rolled forward",
			sess.IncidentState)
	}
}

// TestR7_NoUnwatchedAmbiguousIncident is the §R7 specific assertion: every
// code path that transitions regime to 'incident' must also produce an
// agent_sessions row in the same transaction. The declare path is the only
// one that exists in Phase 1; this test asserts that contract for it.
// When Change 12 adds the autonomous-declare seam (currently const false),
// the seam returns 403 before any transition; this test continues to apply
// to whatever future paths reach the declare repository method.
func TestR7_NoUnwatchedAmbiguousIncident(t *testing.T) {
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	defer s.Close()
	repo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)

	declareEntryPoints := []struct {
		name string
		fn   func() (string, error)
	}{
		{
			name: "human declare (promote-in-place)",
			fn: func() (string, error) {
				// Promote-in-place: create the 'default' session, then promote.
				sid := uuid.NewString()
				if _, err := repo.CreateSession(context.Background(), sessionmodel.AgentSession{
					ID: sid, Type: sessionmodel.SessionTypeDefault, CreatorPrincipal: "alice",
				}); err != nil {
					return "", err
				}
				_, _, err := repo.DeclareIncidentRegime(context.Background(), "alice", sid, sessionmodel.RegimeKindHuman)
				return sid, err
			},
		},
		// Change 12 adds autonomous declare; when it does, append the
		// entry point here so the assertion inherits.
	}

	for _, ep := range declareEntryPoints {
		t.Run(ep.name, func(t *testing.T) {
			// Reset regime so each subtest starts from normal.
			if err := repo.SetRegime(context.Background(), sessionmodel.Regime{Mode: sessionmodel.RegimeModeNormal}); err != nil {
				t.Fatalf("reset regime: %v", err)
			}
			// Clear any prior session by deleting all incident sessions.
			ids, _ := repo.ListSessionsByType(context.Background(), sessionmodel.SessionTypeIncident)
			for _, prev := range ids {
				_ = repo.DeleteSession(context.Background(), prev.ID)
			}

			sid, err := ep.fn()
			if err != nil {
				t.Fatalf("declare entry point error: %v", err)
			}
			// After regime transitions to incident, the matching session
			// MUST exist. If a future path flips regime without creating
			// a session, this assertion fails.
			reg, _ := repo.GetRegime(context.Background())
			if reg.Mode != sessionmodel.RegimeModeIncident {
				t.Errorf("regime not flipped to incident: %q", reg.Mode)
			}
			sess, err := repo.GetSession(context.Background(), sid)
			if err != nil || sess == nil {
				t.Fatalf("regime is incident but session %q is missing — §R7 violation", sid)
			}
		})
	}
}

// TestRegime_6B_NoIncidentalSourceWidening is the §6-B specific assertion:
// granting can_declare_incident (i.e. a policy entry for the regime-control
// zone) MUST NOT incidentally widen any principal's source authority. The
// pre-grant allow/deny matrix on a sample source must equal the post-grant
// matrix.
//
// The encoding chosen in migration 012 lives in a dedicated 'regime-control'
// zone separate from 'unassigned', 'prod-readonly', etc. — so adding a
// principal to regime-control cannot grant them any source action. This
// test asserts that property end-to-end against IsAllowed.
func TestRegime_6B_NoIncidentalSourceWidening(t *testing.T) {
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	defer s.Close()
	rbacRepo := rbac.NewRepository(s.DB(), store.DriverSQLite)
	engine := rbac.NewPolicyEngine(rbacRepo)

	// Add a source so IsAllowed has something to evaluate. The source
	// is left unassigned to a zone (defaults to 'unassigned').
	ctx := context.Background()
	if _, err := s.DB().ExecContext(ctx, store.Rebind(store.DriverSQLite, `
		INSERT INTO components (id, type, name, config, status, created_at, updated_at)
		VALUES (?, 'kubernetes', 'sample', '{}', 'active', ?, ?)`),
		"sample-src", time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("insert sample source: %v", err)
	}

	actions := []rbac.Action{rbac.ActionRead, rbac.ActionQuery, rbac.ActionMutate, rbac.ActionDelete}

	// Pre-grant matrix.
	pre := map[rbac.Action]bool{}
	for _, a := range actions {
		pre[a] = engine.IsAllowed(ctx, rbac.NewPrincipalSet("alice"), "sample-src", a)
	}

	// Grant regime-control.
	if _, err := rbacRepo.CreatePolicy(ctx, rbac.Policy{
		Principal: "alice", ZoneID: "regime-control",
	}, "test"); err != nil {
		t.Fatalf("grant regime-control: %v", err)
	}

	// Post-grant matrix — must be identical.
	for _, a := range actions {
		got := engine.IsAllowed(ctx, rbac.NewPrincipalSet("alice"), "sample-src", a)
		if got != pre[a] {
			t.Errorf("granting regime-control widened source authority: "+
				"IsAllowed(alice, sample-src, %q) was %v, now %v — §6-B violation",
				a, pre[a], got)
		}
	}

	// And alice now DOES have regime-control access. Phase G:
	// HasZoneAccess is set-shaped — every caller builds a size-1 set
	// from the principal in ctx, exactly matching how IsAllowed is
	// called everywhere else.
	if !engine.HasZoneAccess(ctx, rbac.NewPrincipalSet("alice"), "regime-control", rbac.ActionDeclareIncident) {
		t.Error("alice should hold declare_incident on regime-control after grant")
	}
	if !engine.HasZoneAccess(ctx, rbac.NewPrincipalSet("alice"), "regime-control", rbac.ActionResolveIncident) {
		t.Error("alice should hold resolve_incident on regime-control after grant")
	}

	// Sanity: a principal without the grant has no regime-control access.
	if engine.HasZoneAccess(ctx, rbac.NewPrincipalSet("bob"), "regime-control", rbac.ActionDeclareIncident) {
		t.Error("bob should NOT hold declare_incident without a policy grant")
	}
}

// advanceIncidentToMitigated bumps a declared incident session to
// 'believed_mitigated' so resolve can run. In Change 6+ this transition
// becomes captain-driven; for Change 5 testing we go through the public
// Repository.UpdateIncidentState method.
func advanceIncidentToMitigated(t *testing.T, repo sessionmodel.Repository, sessionID string) {
	t.Helper()
	if err := repo.UpdateIncidentState(context.Background(), sessionID,
		sessionmodel.IncidentStateBelievedMitigated); err != nil {
		t.Fatalf("advance to mitigated: %v", err)
	}
}
