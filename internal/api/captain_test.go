package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/runmodel"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
)

func newCaptainServer(t *testing.T, reachableThresholdSec int) (*httptest.Server, sessionmodel.Repository, runmodel.Repository) {
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
	runRepo := runmodel.NewRepository(s.DB(), store.DriverSQLite)
	captainSvc := sessionmodel.NewCaptainService(sessRepo, runRepo, reachableThresholdSec)

	svc := &core.Services{
		Store:        s,
		SessionModel: sessRepo,
		RunModel:     runRepo,
		CaptainSvc:   captainSvc,
	}
	srv := api.New(svc, api.TestingPolicyEngine(svc))
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	handler := rbac.IdentityMiddleware(testPrincipalProvider{})(mux)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts, sessRepo, runRepo
}

func TestCaptainAPI_AttachHeartbeatTransferConfirm(t *testing.T) {
	ts, sessRepo, runRepo := newCaptainServer(t, 60)
	ctx := context.Background()

	// alice declares an incident (promote-in-place, §12.3).
	sessionID := declareIncident(t, sessRepo, "alice")
	// Start a run for the transfer solicitation to live on.
	run, err := runRepo.CreateRun(ctx, runmodel.Run{
		ID: "run-1", SessionID: sessionID, State: runmodel.RunStateRunning,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	// Heartbeat as alice — should succeed.
	r1 := doRequest(t, http.MethodPost,
		ts.URL+"/api/v1/sessions/"+sessionID+"/captain/heartbeat",
		"alice", nil)
	if r1.StatusCode != http.StatusOK {
		t.Errorf("heartbeat status = %d, want 200", r1.StatusCode)
	}
	r1.Body.Close()

	// Heartbeat as bob — should fail (not the captain).
	r2 := doRequest(t, http.MethodPost,
		ts.URL+"/api/v1/sessions/"+sessionID+"/captain/heartbeat",
		"bob", nil)
	if r2.StatusCode != http.StatusForbidden {
		t.Errorf("non-captain heartbeat status = %d, want 403", r2.StatusCode)
	}
	r2.Body.Close()

	// Transfer begin (outgoing) — alice asks finish-or-cancel.
	r3 := doRequest(t, http.MethodPost,
		ts.URL+"/api/v1/sessions/"+sessionID+"/captain/transfer/begin",
		"alice",
		map[string]any{
			"initiator":          "outgoing",
			"incoming_principal": "bob",
			"run_id":             run.ID,
		})
	if r3.StatusCode != http.StatusOK {
		t.Fatalf("transfer/begin status = %d, want 200", r3.StatusCode)
	}
	var begin struct {
		State          string `json:"state"`
		SolicitationID string `json:"solicitation_id"`
	}
	json.NewDecoder(r3.Body).Decode(&begin)
	r3.Body.Close()
	if begin.State != string(sessionmodel.TransferStateTransferRequested) {
		t.Errorf("state = %q, want transfer_requested", begin.State)
	}
	if begin.SolicitationID == "" {
		t.Error("expected solicitation id on outgoing-initiated transfer")
	}

	// Confirm — authorized only to the solicited incoming principal (bob).
	// The outgoing captain (alice) cannot confirm the transfer in bob's
	// place; the binding is enforced at the service layer (D-0017).
	r4 := doRequest(t, http.MethodPost,
		ts.URL+"/api/v1/sessions/"+sessionID+"/captain/transfer/confirm",
		"bob", nil)
	if r4.StatusCode != http.StatusOK {
		t.Errorf("transfer/confirm status = %d, want 200", r4.StatusCode)
	}
	r4.Body.Close()

	// CurrentCaptainPrincipal now resolves to bob.
	p, ok, _ := sessRepo.CurrentCaptainPrincipal(ctx, sessionID)
	if !ok || p != "bob" {
		t.Errorf("CurrentCaptainPrincipal = (%q, %v), want (bob, true)", p, ok)
	}
}

func TestCaptainAPI_TransferUnreachableDirectConfirm(t *testing.T) {
	ts, sessRepo, runRepo := newCaptainServer(t, 2)
	ctx := context.Background()

	sessionID := declareIncident(t, sessRepo, "alice")
	run, _ := runRepo.CreateRun(ctx, runmodel.Run{
		ID: "run-1", SessionID: sessionID, State: runmodel.RunStateRunning,
	})

	// Backdate alice's last_seen_at via the store directly — exercises
	// the REAL §6-D column. Note we keep the store handle alive via the
	// test fixture; here we go through the test server's underlying
	// store using sessRepo's GetActiveCaptain to discover the row, then
	// a direct UPDATE on the db.
	// Simpler: just sleep 3s past the 2s window — too slow. Use the
	// repo's heartbeat with an old timestamp explicitly (but heartbeat
	// stamps now()). The clean path: directly UPDATE via the repo's
	// underlying connection. The api_test package doesn't own that, so
	// we use a small SQL helper through the http API: post a backdated
	// heartbeat? Heartbeat stamps server-now(). So we go via raw SQL.
	if sq, ok := sessRepo.(*sessionmodel.SQLRepository); ok {
		_ = sq
	}
	// Use the repo to fetch the active captain, then update via repo
	// (we don't expose a "set last_seen_at to past" method; instead
	// detach + re-attach with a stale LastSeenAt).
	old, _ := sessRepo.GetActiveCaptain(ctx, sessionID)
	if err := sessRepo.MarkCaptainDetached(ctx, old.ID, time.Now().UTC()); err != nil {
		t.Fatalf("detach old captain: %v", err)
	}
	stale := time.Now().UTC().Add(-10 * time.Second)
	active := sessionmodel.TransferStateActive
	if _, err := sessRepo.AttachCaptain(ctx, sessionmodel.Captain{
		ID:            "captain-2",
		SessionID:     sessionID,
		CaptainType:   sessionmodel.CaptainTypeHuman,
		Principal:     "alice",
		AttachedAt:    stale,
		TransferState: &active,
		LastSeenAt:    &stale,
	}); err != nil {
		t.Fatalf("attach stale captain: %v", err)
	}

	// bob requests command. alice is stale → direct transfer_confirmed.
	r := doRequest(t, http.MethodPost,
		ts.URL+"/api/v1/sessions/"+sessionID+"/captain/transfer/begin",
		"bob",
		map[string]any{
			"initiator": "incoming",
			"run_id":    run.ID,
		})
	if r.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", r.StatusCode)
	}
	var body struct {
		State        string `json:"state"`
		NewCaptainID string `json:"new_captain_id"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	r.Body.Close()
	if body.State != string(sessionmodel.TransferStateTransferConfirmed) {
		t.Errorf("state = %q, want transfer_confirmed", body.State)
	}
	if body.NewCaptainID == "" {
		t.Error("new_captain_id missing on direct-confirm path")
	}

	// §B1: principal threaded to bob.
	p, ok, _ := sessRepo.CurrentCaptainPrincipal(ctx, sessionID)
	if !ok || p != "bob" {
		t.Errorf("CurrentCaptainPrincipal = (%q, %v), want (bob, true)", p, ok)
	}
}

// TestCaptainAPI_TransferConfirmCancelBindToParties exercises the full
// wire path of the D-0017 authorization fix: the typed service errors
// (ErrNotSolicitedIncoming / ErrNotTransferParty) surface as HTTP 403, and
// a principal that is party to neither side of the handshake can neither
// confirm nor cancel an in-flight transfer.
func TestCaptainAPI_TransferConfirmCancelBindToParties(t *testing.T) {
	ts, sessRepo, runRepo := newCaptainServer(t, 60)
	ctx := context.Background()

	sessionID := declareIncident(t, sessRepo, "alice")
	run, err := runRepo.CreateRun(ctx, runmodel.Run{
		ID: "run-1", SessionID: sessionID, State: runmodel.RunStateRunning,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	// alice solicits bob (outgoing-initiated).
	rb := doRequest(t, http.MethodPost,
		ts.URL+"/api/v1/sessions/"+sessionID+"/captain/transfer/begin",
		"alice",
		map[string]any{"initiator": "outgoing", "incoming_principal": "bob", "run_id": run.ID})
	if rb.StatusCode != http.StatusOK {
		t.Fatalf("transfer/begin status = %d, want 200", rb.StatusCode)
	}
	rb.Body.Close()

	// carol — party to neither side — cannot confirm (403).
	rc := doRequest(t, http.MethodPost,
		ts.URL+"/api/v1/sessions/"+sessionID+"/captain/transfer/confirm",
		"carol", nil)
	if rc.StatusCode != http.StatusForbidden {
		t.Errorf("non-party confirm status = %d, want 403", rc.StatusCode)
	}
	rc.Body.Close()

	// carol cannot cancel either (403).
	rd := doRequest(t, http.MethodPost,
		ts.URL+"/api/v1/sessions/"+sessionID+"/captain/transfer/cancel",
		"carol", nil)
	if rd.StatusCode != http.StatusForbidden {
		t.Errorf("non-party cancel status = %d, want 403", rd.StatusCode)
	}
	rd.Body.Close()

	// The transfer is untouched: alice is still captain with bob in flight.
	cap, _ := sessRepo.GetActiveCaptain(ctx, sessionID)
	if cap.Principal != "alice" || cap.TransferState == nil ||
		*cap.TransferState != sessionmodel.TransferStateTransferRequested {
		t.Errorf("after non-party calls: captain=%q state=%+v, want alice/transfer_requested",
			cap.Principal, cap.TransferState)
	}
}

func TestCaptainAPI_AttachInformationalOutsideIncident(t *testing.T) {
	ts, sessRepo, _ := newCaptainServer(t, 60)
	ctx := context.Background()
	// Plain investigation session.
	sess := sessionmodel.AgentSession{
		ID: "sess-1", Type: sessionmodel.SessionTypeDefault, CreatorPrincipal: "alice",
	}
	if _, err := sessRepo.CreateSession(ctx, sess); err != nil {
		t.Fatalf("create session: %v", err)
	}
	r := doRequest(t, http.MethodPost,
		ts.URL+"/api/v1/sessions/"+sess.ID+"/captain/attach",
		"alice", nil)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("attach status = %d, want 200", r.StatusCode)
	}
	var body struct {
		BecameCaptain bool `json:"became_captain"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	r.Body.Close()
	if body.BecameCaptain {
		t.Error("attach on non-incident session should not become captain (§B4)")
	}
}
