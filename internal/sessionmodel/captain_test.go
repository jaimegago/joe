package sessionmodel_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jaimegago/joe/internal/runmodel"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
)

// captainTestEnv assembles a captain-service test fixture: a real
// SQLite store, real sessionmodel + runmodel repositories, and a
// CaptainService with a small reachability threshold so the unreachable
// branch is easy to exercise.
type captainTestEnv struct {
	store   *store.Store
	sess    sessionmodel.Repository
	runRepo runmodel.Repository
	svc     *sessionmodel.CaptainService
	ctx     context.Context
}

// newCaptainEnv builds the fixture with a 2-second reachability window.
// Tests that exercise the unreachable branch backdate last_seen_at past
// that window via the real database column — there's no mock.
func newCaptainEnv(t *testing.T, thresholdSeconds int) *captainTestEnv {
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
	svc := sessionmodel.NewCaptainService(sessRepo, runRepo, thresholdSeconds)
	return &captainTestEnv{
		store:   s,
		sess:    sessRepo,
		runRepo: runRepo,
		svc:     svc,
		ctx:     context.Background(),
	}
}

// declareWithCaptain creates an incident session via DeclareIncidentRegime
// (Change 5's atomic path) and returns the session ID + initial captain
// principal. Used by every transfer test that needs a starting state.
func (e *captainTestEnv) declareWithCaptain(t *testing.T, principal string) string {
	t.Helper()
	sessionID, _, err := e.sess.DeclareIncidentRegime(e.ctx, principal, sessionmodel.RegimeKindHuman)
	if err != nil {
		t.Fatalf("declare: %v", err)
	}
	return sessionID
}

// runOn creates a run for the given session so the transfer-via-
// solicitation path has somewhere to write the decision row.
func (e *captainTestEnv) runOn(t *testing.T, sessionID string) string {
	t.Helper()
	run, err := e.runRepo.CreateRun(e.ctx, runmodel.Run{
		ID: uuid.NewString(), SessionID: sessionID, State: runmodel.RunStateRunning,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	return run.ID
}

// backdateCaptainLastSeen writes session_captains.last_seen_at directly
// to a past timestamp. Used by §6-D unreachable-path tests to drive the
// real reachability column past its threshold without sleeping. This
// exercises the production path: IsCaptainReachable reads the column,
// compares against now() - threshold, and the unreachable branch fires.
func (e *captainTestEnv) backdateCaptainLastSeen(t *testing.T, sessionID string, past time.Duration) {
	t.Helper()
	old := time.Now().UTC().Add(-past).Format(time.RFC3339)
	_, err := e.store.DB().ExecContext(e.ctx, store.Rebind(store.DriverSQLite, `
		UPDATE session_captains SET last_seen_at = ?
		WHERE session_id = ? AND detached_at IS NULL`), old, sessionID)
	if err != nil {
		t.Fatalf("backdate last_seen_at: %v", err)
	}
}

// --- §B2: null-authority on pending_captain ---

func TestCaptain_B2_NullAuthorityOnPendingCaptain(t *testing.T) {
	e := newCaptainEnv(t, 60)
	// Create an incident session WITHOUT going through
	// DeclareIncidentRegime — DeclareIncidentRegime always attaches a
	// captain (R-CAP1). To exercise the §B2 pending_captain state we
	// have to construct a captain-less incident directly via the repo.
	state := sessionmodel.IncidentStateDeclared
	sess := sessionmodel.AgentSession{
		ID: uuid.NewString(), Type: sessionmodel.SessionTypeIncident,
		IncidentState: &state, CreatorPrincipal: "system",
	}
	if _, err := e.sess.CreateSession(e.ctx, sess); err != nil {
		t.Fatalf("create incident: %v", err)
	}

	principal, ok, err := e.sess.CurrentCaptainPrincipal(e.ctx, sess.ID)
	if err != nil {
		t.Fatalf("CurrentCaptainPrincipal: %v", err)
	}
	if ok {
		t.Errorf("expected (_, false) for pending_captain session, got (%q, true)", principal)
	}
}

// --- R-CAP2/R-CAP3: first human attach on pending_captain becomes captain ---

func TestCaptain_RCAP2_FirstHumanBecomesCaptain(t *testing.T) {
	e := newCaptainEnv(t, 60)
	state := sessionmodel.IncidentStateDeclared
	sess := sessionmodel.AgentSession{
		ID: uuid.NewString(), Type: sessionmodel.SessionTypeIncident,
		IncidentState: &state, CreatorPrincipal: "system",
	}
	if _, err := e.sess.CreateSession(e.ctx, sess); err != nil {
		t.Fatalf("create incident: %v", err)
	}

	res, err := e.svc.Attach(e.ctx, sess.ID, "alice", sessionmodel.CaptainTypeHuman)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if !res.BecameCaptain {
		t.Fatal("first human attach on pending_captain should become captain (R-CAP2)")
	}

	// §B1 principal-threading getter resolves to alice.
	got, ok, _ := e.sess.CurrentCaptainPrincipal(e.ctx, sess.ID)
	if !ok || got != "alice" {
		t.Errorf("CurrentCaptainPrincipal after attach = (%q, %v), want (alice, true)", got, ok)
	}

	// A second attach is informational (observer; §A3), captain stays alice.
	res2, err := e.svc.Attach(e.ctx, sess.ID, "bob", sessionmodel.CaptainTypeHuman)
	if err != nil {
		t.Fatalf("second attach: %v", err)
	}
	if res2.BecameCaptain {
		t.Error("second attach should NOT become captain (alice still holds it)")
	}
	got, _, _ = e.sess.CurrentCaptainPrincipal(e.ctx, sess.ID)
	if got != "alice" {
		t.Errorf("captain changed unexpectedly: %q", got)
	}
}

// --- §B4: attach is informational outside incident regime ---

func TestCaptain_B4_NonIncidentAttachIsInformational(t *testing.T) {
	e := newCaptainEnv(t, 60)
	sess := sessionmodel.AgentSession{
		ID: uuid.NewString(), Type: sessionmodel.SessionTypeDefault,
		CreatorPrincipal: "alice",
	}
	if _, err := e.sess.CreateSession(e.ctx, sess); err != nil {
		t.Fatalf("create investigation: %v", err)
	}
	res, err := e.svc.Attach(e.ctx, sess.ID, "alice", sessionmodel.CaptainTypeHuman)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if res.BecameCaptain {
		t.Error("attach on non-incident session should not produce captain semantics (§B4)")
	}
	_, ok, _ := e.sess.CurrentCaptainPrincipal(e.ctx, sess.ID)
	if ok {
		t.Error("CurrentCaptainPrincipal should be (_, false) outside incident regime")
	}
}

// --- §B BeginTransfer / Outgoing-initiated → solicitation + transfer_requested (B3) ---

func TestCaptain_B3_OutgoingInitiatedOpensDecisionSolicitation(t *testing.T) {
	e := newCaptainEnv(t, 60)
	sessionID := e.declareWithCaptain(t, "alice")
	runID := e.runOn(t, sessionID)

	res, err := e.svc.BeginTransfer(e.ctx, sessionID,
		sessionmodel.TransferInitiatorOutgoing,
		"alice", // requesting principal = current captain
		"bob",   // incoming candidate
		runID)
	if err != nil {
		t.Fatalf("BeginTransfer outgoing: %v", err)
	}
	if res.State != sessionmodel.TransferStateTransferRequested {
		t.Errorf("state = %q, want transfer_requested", res.State)
	}
	if res.SolicitationID == "" {
		t.Fatal("expected a decision solicitation to be opened (B3 finish-or-cancel)")
	}

	// The solicitation row exists on the run and is a decision-kind row.
	sol, err := e.runRepo.GetSolicitation(e.ctx, res.SolicitationID)
	if err != nil || sol == nil {
		t.Fatalf("solicitation not persisted: %v %v", err, sol)
	}
	if sol.Kind != runmodel.SolicitationKindDecision {
		t.Errorf("solicitation kind = %q, want decision", sol.Kind)
	}
	if sol.ResolvedAt != nil {
		t.Errorf("solicitation should be unresolved on creation, got resolved_at=%v", sol.ResolvedAt)
	}
	// Payload identifies it as a captain transfer (B3 finish-or-cancel).
	var payload map[string]string
	_ = json.Unmarshal([]byte(sol.Payload), &payload)
	if payload["kind"] != "captain_transfer" {
		t.Errorf("payload kind = %q, want captain_transfer", payload["kind"])
	}
	if payload["reason"] != "outgoing_finish_or_cancel" {
		t.Errorf("payload reason = %q, want outgoing_finish_or_cancel", payload["reason"])
	}

	// Active captain row now carries transfer_state=transfer_requested.
	cap, _ := e.sess.GetActiveCaptain(e.ctx, sessionID)
	if cap.TransferState == nil || *cap.TransferState != sessionmodel.TransferStateTransferRequested {
		t.Errorf("captain transfer_state = %+v, want transfer_requested", cap.TransferState)
	}
	if cap.IncomingPrincipal == nil || *cap.IncomingPrincipal != "bob" {
		t.Errorf("incoming_principal = %+v, want bob", cap.IncomingPrincipal)
	}
}

// --- §B Incoming-initiated when reachable → decision solicitation, state transfer_requested ---

func TestCaptain_IncomingInitiatedWhenReachableAsksOutgoing(t *testing.T) {
	e := newCaptainEnv(t, 60)
	sessionID := e.declareWithCaptain(t, "alice")
	runID := e.runOn(t, sessionID)

	// alice is reachable (just attached; last_seen_at = now).
	res, err := e.svc.BeginTransfer(e.ctx, sessionID,
		sessionmodel.TransferInitiatorIncoming,
		"bob", // requesting (incoming) principal
		"bob",
		runID)
	if err != nil {
		t.Fatalf("BeginTransfer incoming-reachable: %v", err)
	}
	if res.State != sessionmodel.TransferStateTransferRequested {
		t.Errorf("state = %q, want transfer_requested", res.State)
	}
	if res.SolicitationID == "" {
		t.Fatal("expected approve/decline decision solicitation when outgoing reachable")
	}
	sol, _ := e.runRepo.GetSolicitation(e.ctx, res.SolicitationID)
	var payload map[string]string
	_ = json.Unmarshal([]byte(sol.Payload), &payload)
	if payload["reason"] != "incoming_request_approve_decline" {
		t.Errorf("payload reason = %q, want incoming_request_approve_decline", payload["reason"])
	}

	// Cancel returns the state to active (decline-by-current-captain).
	if err := e.svc.CancelTransfer(e.ctx, sessionID, "alice"); err != nil {
		t.Fatalf("CancelTransfer: %v", err)
	}
	cap, _ := e.sess.GetActiveCaptain(e.ctx, sessionID)
	if cap.TransferState == nil || *cap.TransferState != sessionmodel.TransferStateActive {
		t.Errorf("captain transfer_state after cancel = %+v, want active", cap.TransferState)
	}
	// Captain principal unchanged — decline keeps alice in command.
	if cap.Principal != "alice" {
		t.Errorf("captain principal after cancel = %q, want alice", cap.Principal)
	}
}

// --- §6-D Incoming-initiated when UNREACHABLE → direct transfer_confirmed,
//        exercising the real reachability column (no mock) ---

func TestCaptain_6D_IncomingInitiatedWhenUnreachableDirectConfirm(t *testing.T) {
	// Use a 2-second window so the test can backdate last_seen_at into
	// the past and trigger the real unreachable path.
	e := newCaptainEnv(t, 2)
	sessionID := e.declareWithCaptain(t, "alice")
	runID := e.runOn(t, sessionID)

	// Backdate alice's last_seen_at to 10s ago — past the 2s threshold.
	// This writes to the REAL column the production reachability check
	// reads. No mock, no override.
	e.backdateCaptainLastSeen(t, sessionID, 10*time.Second)

	// Sanity: IsCaptainReachable says false.
	reachable, err := e.sess.IsCaptainReachable(e.ctx, sessionID, 2)
	if err != nil {
		t.Fatalf("IsCaptainReachable: %v", err)
	}
	if reachable {
		t.Fatal("captain should be unreachable after backdating last_seen_at — §6-D signal not wired")
	}

	res, err := e.svc.BeginTransfer(e.ctx, sessionID,
		sessionmodel.TransferInitiatorIncoming,
		"bob", "bob", runID)
	if err != nil {
		t.Fatalf("BeginTransfer incoming-unreachable: %v", err)
	}
	if res.State != sessionmodel.TransferStateTransferConfirmed {
		t.Errorf("state = %q, want transfer_confirmed (direct path on unreachable)", res.State)
	}
	if res.NewCaptainID == "" {
		t.Fatal("new captain id missing")
	}
	if res.SolicitationID != "" {
		t.Errorf("solicitation should NOT be created on direct-confirm path, got %q", res.SolicitationID)
	}

	// §B1 principal-threading: CurrentCaptainPrincipal now returns bob.
	p, ok, _ := e.sess.CurrentCaptainPrincipal(e.ctx, sessionID)
	if !ok || p != "bob" {
		t.Errorf("CurrentCaptainPrincipal after direct-confirm = (%q, %v), want (bob, true)", p, ok)
	}

	// alice's row is detached.
	all, _ := e.sess.ListCaptainsForSession(e.ctx, sessionID)
	if len(all) != 2 {
		t.Fatalf("captain rows = %d, want 2 (alice detached + bob active)", len(all))
	}
	var aliceRow, bobRow *sessionmodel.Captain
	for i := range all {
		c := &all[i]
		switch c.Principal {
		case "alice":
			aliceRow = c
		case "bob":
			bobRow = c
		}
	}
	if aliceRow == nil || aliceRow.DetachedAt == nil {
		t.Errorf("alice row should be detached: %+v", aliceRow)
	}
	if bobRow == nil || bobRow.DetachedAt != nil {
		t.Errorf("bob row should be active (no detached_at): %+v", bobRow)
	}
}

// --- ConfirmTransfer: §B1 principal-threading after transfer completes ---

func TestCaptain_B1_PrincipalThreadedAfterConfirm(t *testing.T) {
	e := newCaptainEnv(t, 60)
	sessionID := e.declareWithCaptain(t, "alice")
	runID := e.runOn(t, sessionID)

	// Outgoing-initiated: alice opens the finish-or-cancel solicitation.
	_, err := e.svc.BeginTransfer(e.ctx, sessionID,
		sessionmodel.TransferInitiatorOutgoing, "alice", "bob", runID)
	if err != nil {
		t.Fatalf("BeginTransfer: %v", err)
	}

	// Confirm transfer (outgoing chose "finish"); only the solicited
	// incoming principal (bob) is authorized to confirm.
	newID, err := e.svc.ConfirmTransfer(e.ctx, sessionID, "bob")
	if err != nil {
		t.Fatalf("ConfirmTransfer: %v", err)
	}
	if newID == "" {
		t.Fatal("new captain id missing")
	}

	// §B1: CurrentCaptainPrincipal returns the new principal.
	p, ok, _ := e.sess.CurrentCaptainPrincipal(e.ctx, sessionID)
	if !ok || p != "bob" {
		t.Errorf("CurrentCaptainPrincipal after confirm = (%q, %v), want (bob, true)", p, ok)
	}
}

// --- §B Cancel-by-current-captain (decline) leaves state active ---

func TestCaptain_CancelLeavesStateActive(t *testing.T) {
	e := newCaptainEnv(t, 60)
	sessionID := e.declareWithCaptain(t, "alice")
	runID := e.runOn(t, sessionID)

	if _, err := e.svc.BeginTransfer(e.ctx, sessionID,
		sessionmodel.TransferInitiatorOutgoing, "alice", "bob", runID); err != nil {
		t.Fatalf("BeginTransfer: %v", err)
	}
	if err := e.svc.CancelTransfer(e.ctx, sessionID, "alice"); err != nil {
		t.Fatalf("CancelTransfer: %v", err)
	}

	cap, _ := e.sess.GetActiveCaptain(e.ctx, sessionID)
	if cap.Principal != "alice" {
		t.Errorf("captain principal after cancel = %q, want alice", cap.Principal)
	}
	if cap.TransferState == nil || *cap.TransferState != sessionmodel.TransferStateActive {
		t.Errorf("transfer_state after cancel = %+v, want active", cap.TransferState)
	}
	if cap.IncomingPrincipal != nil {
		t.Errorf("incoming_principal should be cleared after cancel, got %+v", cap.IncomingPrincipal)
	}
}

// --- §6-D heartbeat round-trip: backdated captain becomes reachable again
//        after a real heartbeat write to the real column ---

func TestCaptain_6D_HeartbeatRestoresReachability(t *testing.T) {
	e := newCaptainEnv(t, 2)
	sessionID := e.declareWithCaptain(t, "alice")
	e.backdateCaptainLastSeen(t, sessionID, 10*time.Second)

	reachable, _ := e.sess.IsCaptainReachable(e.ctx, sessionID, 2)
	if reachable {
		t.Fatal("precondition: captain should be unreachable after backdating")
	}

	if err := e.sess.RecordCaptainHeartbeat(e.ctx, sessionID, "alice", time.Now().UTC()); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	reachable, _ = e.sess.IsCaptainReachable(e.ctx, sessionID, 2)
	if !reachable {
		t.Error("captain should be reachable after heartbeat — §6-D signal not refreshing")
	}
}

// --- Heartbeat refuses non-captain principal (captain-bound, not session-bound) ---

func TestCaptain_HeartbeatRejectsNonCaptain(t *testing.T) {
	e := newCaptainEnv(t, 60)
	sessionID := e.declareWithCaptain(t, "alice")

	err := e.sess.RecordCaptainHeartbeat(e.ctx, sessionID, "bob", time.Now().UTC())
	if err != sessionmodel.ErrCaptainPrincipalMismatch {
		t.Errorf("heartbeat from non-captain err = %v, want ErrCaptainPrincipalMismatch", err)
	}
}

// --- Transfer-already-in-flight rejection ---

func TestCaptain_BeginTransferRejectsConcurrent(t *testing.T) {
	e := newCaptainEnv(t, 60)
	sessionID := e.declareWithCaptain(t, "alice")
	runID := e.runOn(t, sessionID)

	if _, err := e.svc.BeginTransfer(e.ctx, sessionID,
		sessionmodel.TransferInitiatorOutgoing, "alice", "bob", runID); err != nil {
		t.Fatalf("first BeginTransfer: %v", err)
	}
	_, err := e.svc.BeginTransfer(e.ctx, sessionID,
		sessionmodel.TransferInitiatorIncoming, "carol", "carol", runID)
	if err != sessionmodel.ErrTransferAlreadyInFlight {
		t.Errorf("second BeginTransfer err = %v, want ErrTransferAlreadyInFlight", err)
	}
}

// --- ConfirmTransfer without in-flight transfer is rejected ---

func TestCaptain_ConfirmWithoutTransferRejected(t *testing.T) {
	e := newCaptainEnv(t, 60)
	sessionID := e.declareWithCaptain(t, "alice")

	_, err := e.svc.ConfirmTransfer(e.ctx, sessionID, "alice")
	if err != sessionmodel.ErrNoTransferInFlight {
		t.Errorf("ConfirmTransfer err = %v, want ErrNoTransferInFlight", err)
	}
}

// --- §B authorization binding: confirm/cancel are bound to the handshake
//        parties, not merely authenticated. Break test for the D-0017
//        authorization-bypass fix: it fails if the principal binding on
//        ConfirmTransfer or CancelTransfer is removed, because a non-party
//        would then complete/abort a transfer it is not part of. ---

// stillInFlight asserts the active captain row still holds an in-flight
// transfer to a named incoming principal — i.e. a rejected confirm/cancel
// did NOT mutate the handshake. If the binding is removed and the rejected
// call instead succeeds, this assertion fails (state would be active or the
// captain swapped).
func (e *captainTestEnv) stillInFlight(t *testing.T, sessionID, wantCaptain, wantIncoming string) {
	t.Helper()
	cap, err := e.sess.GetActiveCaptain(e.ctx, sessionID)
	if err != nil {
		t.Fatalf("GetActiveCaptain: %v", err)
	}
	if cap.Principal != wantCaptain {
		t.Fatalf("active captain principal = %q, want %q (a non-party call must not swap the captain)", cap.Principal, wantCaptain)
	}
	if cap.TransferState == nil || *cap.TransferState != sessionmodel.TransferStateTransferRequested {
		t.Fatalf("transfer_state = %+v, want transfer_requested (a non-party call must not resolve the transfer)", cap.TransferState)
	}
	if cap.IncomingPrincipal == nil || *cap.IncomingPrincipal != wantIncoming {
		t.Fatalf("incoming_principal = %+v, want %q", cap.IncomingPrincipal, wantIncoming)
	}
}

func TestCaptain_ConfirmBoundToSolicitedIncoming(t *testing.T) {
	e := newCaptainEnv(t, 60)
	sessionID := e.declareWithCaptain(t, "alice")
	runID := e.runOn(t, sessionID)

	// Outgoing-initiated: alice solicits bob as the incoming captain.
	if _, err := e.svc.BeginTransfer(e.ctx, sessionID,
		sessionmodel.TransferInitiatorOutgoing, "alice", "bob", runID); err != nil {
		t.Fatalf("BeginTransfer: %v", err)
	}

	// A third principal (carol), party to neither side, cannot confirm.
	if _, err := e.svc.ConfirmTransfer(e.ctx, sessionID, "carol"); err != sessionmodel.ErrNotSolicitedIncoming {
		t.Errorf("confirm by third party err = %v, want ErrNotSolicitedIncoming", err)
	}
	e.stillInFlight(t, sessionID, "alice", "bob")

	// The soliciting/outgoing captain cannot confirm in the incoming
	// principal's place — confirm is reserved to the solicited incoming.
	if _, err := e.svc.ConfirmTransfer(e.ctx, sessionID, "alice"); err != sessionmodel.ErrNotSolicitedIncoming {
		t.Errorf("confirm by outgoing captain err = %v, want ErrNotSolicitedIncoming", err)
	}
	e.stillInFlight(t, sessionID, "alice", "bob")

	// The solicited incoming principal (bob) is authorized; the swap proceeds.
	newID, err := e.svc.ConfirmTransfer(e.ctx, sessionID, "bob")
	if err != nil {
		t.Fatalf("confirm by solicited incoming: %v", err)
	}
	if newID == "" {
		t.Fatal("new captain id missing after authorized confirm")
	}
	p, ok, _ := e.sess.CurrentCaptainPrincipal(e.ctx, sessionID)
	if !ok || p != "bob" {
		t.Errorf("active captain after authorized confirm = (%q, %v), want (bob, true)", p, ok)
	}
}

func TestCaptain_CancelBoundToHandshakeParties(t *testing.T) {
	e := newCaptainEnv(t, 60)
	sessionID := e.declareWithCaptain(t, "alice")
	runID := e.runOn(t, sessionID)

	// Outgoing-initiated: alice → bob.
	if _, err := e.svc.BeginTransfer(e.ctx, sessionID,
		sessionmodel.TransferInitiatorOutgoing, "alice", "bob", runID); err != nil {
		t.Fatalf("BeginTransfer: %v", err)
	}

	// A third principal (carol) cannot cancel a transfer it is not part of.
	if err := e.svc.CancelTransfer(e.ctx, sessionID, "carol"); err != sessionmodel.ErrNotTransferParty {
		t.Errorf("cancel by third party err = %v, want ErrNotTransferParty", err)
	}
	e.stillInFlight(t, sessionID, "alice", "bob")

	// The solicited incoming principal (bob) is a party → may cancel.
	if err := e.svc.CancelTransfer(e.ctx, sessionID, "bob"); err != nil {
		t.Fatalf("cancel by incoming party (bob): %v", err)
	}
	cap, _ := e.sess.GetActiveCaptain(e.ctx, sessionID)
	if cap.Principal != "alice" || cap.TransferState == nil || *cap.TransferState != sessionmodel.TransferStateActive {
		t.Errorf("after bob cancel: captain=%q state=%+v, want alice/active", cap.Principal, cap.TransferState)
	}

	// Re-open, then the soliciting/outgoing captain (alice) is also a party.
	if _, err := e.svc.BeginTransfer(e.ctx, sessionID,
		sessionmodel.TransferInitiatorOutgoing, "alice", "bob", runID); err != nil {
		t.Fatalf("re-BeginTransfer: %v", err)
	}
	if err := e.svc.CancelTransfer(e.ctx, sessionID, "alice"); err != nil {
		t.Fatalf("cancel by outgoing party (alice): %v", err)
	}
}

// --- D-0024: resolve detaches the active captain atomically with the
// regime flip (resolve-half of the no-auto-lapse captaincy model) ---

// resolveActiveIncident drives the active incident to believed_mitigated and
// resolves it, returning the resolved session id. Mirrors the production
// resolve path used by the regime handler.
func (e *captainTestEnv) resolveActiveIncident(t *testing.T, sessionID, principal string) {
	t.Helper()
	if err := e.sess.UpdateIncidentState(e.ctx, sessionID,
		sessionmodel.IncidentStateBelievedMitigated); err != nil {
		t.Fatalf("advance to believed_mitigated: %v", err)
	}
	if _, err := e.sess.ResolveIncidentRegime(e.ctx, principal); err != nil {
		t.Fatalf("resolve: %v", err)
	}
}

// TestCaptain_ResolveDetachesActiveCaptain is the break test for D-0024:
// resolving an incident must detach its active captain. It fails if the
// detach UPDATE is removed from ResolveIncidentRegime's transaction —
// GetActiveCaptain would then keep returning the stale row.
func TestCaptain_ResolveDetachesActiveCaptain(t *testing.T) {
	e := newCaptainEnv(t, 60)
	sessionID := e.declareWithCaptain(t, "alice")

	// Sanity: declare attached alice as captain.
	if cap, err := e.sess.GetActiveCaptain(e.ctx, sessionID); err != nil || cap == nil {
		t.Fatalf("pre-resolve GetActiveCaptain = (%v, %v), want a live captain", cap, err)
	}

	e.resolveActiveIncident(t, sessionID, "alice")

	// Break assertion: no active captain remains after resolve.
	cap, err := e.sess.GetActiveCaptain(e.ctx, sessionID)
	if err != nil {
		t.Fatalf("post-resolve GetActiveCaptain: %v", err)
	}
	if cap != nil {
		t.Errorf("post-resolve GetActiveCaptain = %+v, want nil (captain must be detached on resolve)", cap)
	}
	if principal, ok, err := e.sess.CurrentCaptainPrincipal(e.ctx, sessionID); err != nil {
		t.Fatalf("post-resolve CurrentCaptainPrincipal: %v", err)
	} else if ok {
		t.Errorf("post-resolve CurrentCaptainPrincipal = (%q, true), want (_, false)", principal)
	}
}

// TestCaptain_ResolveAtomicRegimeAndCaptain asserts the atomicity intent of
// D-0024 as a single post-condition: after resolve the regime is normal AND
// no active captain exists. There must be no surviving state where the regime
// is normal but a captain is still active.
func TestCaptain_ResolveAtomicRegimeAndCaptain(t *testing.T) {
	e := newCaptainEnv(t, 60)
	sessionID := e.declareWithCaptain(t, "alice")
	e.resolveActiveIncident(t, sessionID, "alice")

	var mode string
	if err := e.store.DB().QueryRowContext(e.ctx,
		`SELECT mode FROM system_regime WHERE id = 1`).Scan(&mode); err != nil {
		t.Fatalf("read regime mode: %v", err)
	}
	cap, err := e.sess.GetActiveCaptain(e.ctx, sessionID)
	if err != nil {
		t.Fatalf("GetActiveCaptain: %v", err)
	}
	if mode != string(sessionmodel.RegimeModeNormal) || cap != nil {
		t.Errorf("post-resolve regime=%q activeCaptain=%+v, want regime=normal AND no active captain",
			mode, cap)
	}
}

// TestCaptain_DeclareAfterResolveAttachesCleanly verifies a fresh incident
// after a prior resolve attaches the new declarer cleanly, with no
// interference from the prior incident's (now detached) captain row.
func TestCaptain_DeclareAfterResolveAttachesCleanly(t *testing.T) {
	e := newCaptainEnv(t, 60)
	first := e.declareWithCaptain(t, "alice")
	e.resolveActiveIncident(t, first, "alice")

	second := e.declareWithCaptain(t, "bob")
	if first == second {
		t.Fatalf("expected a distinct new incident session, got same id %q", second)
	}
	cap, err := e.sess.GetActiveCaptain(e.ctx, second)
	if err != nil {
		t.Fatalf("GetActiveCaptain(second): %v", err)
	}
	if cap == nil || cap.Principal != "bob" {
		t.Fatalf("new incident captain = %+v, want bob attached cleanly", cap)
	}
	// The prior incident's captain row stays detached and inert.
	if old, err := e.sess.GetActiveCaptain(e.ctx, first); err != nil {
		t.Fatalf("GetActiveCaptain(first): %v", err)
	} else if old != nil {
		t.Errorf("prior incident still has active captain %+v after a new declare", old)
	}
}

// --- D-0025: transfer swap (detach old + attach new) is atomic ---

// forcedSwapFault is a hook that always errors. SwapCaptainWithHook runs it
// inside the swap transaction, after the detach UPDATE and before the attach
// INSERT, so it simulates the attach step failing after the detach would have
// run.
func forcedSwapFault(*sql.Tx) error { return errForcedSwapFault }

var errForcedSwapFault = errors.New("forced fault between detach and attach")

// TestCaptain_TransferSwapAtomicOnAttachFailure is a true rollback test for the
// D-0025 fix: it forces the attach step to fail after the detach would have run
// and asserts the swap rolled back as a unit. SwapCaptainWithHook returns the
// error, the original captain (alice) is still the active captain with
// detached_at still NULL, no incoming row was inserted, and there is no
// captain-less state. The fault fires between the two writes *inside the
// transaction*, so if the detach and attach are taken off the shared transaction
// the detach commits before the fault and this test fails (the session goes
// captain-less / alice is no longer active).
func TestCaptain_TransferSwapAtomicOnAttachFailure(t *testing.T) {
	e := newCaptainEnv(t, 60)
	sessionID := e.declareWithCaptain(t, "alice")

	repo, ok := e.sess.(*sessionmodel.SQLRepository)
	if !ok {
		t.Fatalf("expected *sessionmodel.SQLRepository, got %T", e.sess)
	}

	outgoing, err := repo.GetActiveCaptain(e.ctx, sessionID)
	if err != nil || outgoing == nil {
		t.Fatalf("pre-swap GetActiveCaptain = (%+v, %v), want alice active", outgoing, err)
	}

	now := time.Now().UTC()
	active := sessionmodel.TransferStateActive
	incoming := sessionmodel.Captain{
		ID:            uuid.NewString(),
		SessionID:     sessionID,
		CaptainType:   sessionmodel.CaptainTypeHuman,
		Principal:     "bob",
		AttachedAt:    now,
		TransferState: &active,
		LastSeenAt:    &now,
	}

	err = repo.SwapCaptainWithHook(e.ctx, outgoing.ID, incoming, now, forcedSwapFault)
	if !errors.Is(err, errForcedSwapFault) {
		t.Fatalf("SwapCaptainWithHook err = %v, want forced fault", err)
	}

	// The original captain must still be active — the detach rolled back.
	cap, err := repo.GetActiveCaptain(e.ctx, sessionID)
	if err != nil {
		t.Fatalf("post-fault GetActiveCaptain: %v", err)
	}
	if cap == nil {
		t.Fatal("post-fault session is captain-less — swap was not atomic (detach committed without attach)")
	}
	if cap.Principal != "alice" || cap.ID != outgoing.ID || cap.DetachedAt != nil {
		t.Errorf("post-fault active captain = %+v, want the original alice row with detached_at NULL", cap)
	}

	// The incoming row must not have been inserted.
	all, err := repo.ListCaptainsForSession(e.ctx, sessionID)
	if err != nil {
		t.Fatalf("ListCaptainsForSession: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("post-fault captain rows = %d, want 1 (incoming attach must have rolled back)", len(all))
	}
}
