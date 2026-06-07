package sessiongate_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/safety"
	"github.com/jaimegago/joe/internal/sessiongate"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
)

// gateEnv holds a real SQLite store + repository for the matrix tests.
// No mocks: the gate logic depends on real data (regime row, session
// row, captain row), so the tests exercise the actual SQL path.
type gateEnv struct {
	store *store.Store
	repo  sessionmodel.Repository
	ctx   context.Context
}

func newGateEnv(t *testing.T) *gateEnv {
	t.Helper()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return &gateEnv{
		store: s,
		repo:  sessionmodel.NewRepository(s.DB(), store.DriverSQLite),
		ctx:   context.Background(),
	}
}

// --- Normal regime: §R1/§B4 — gate always allows ---

func TestCheck_NormalRegime_AllowsAllClasses(t *testing.T) {
	e := newGateEnv(t)
	// Make a plain investigation session (any non-incident kind works).
	sess := sessionmodel.AgentSession{
		ID: uuid.NewString(), Type: sessionmodel.SessionTypeInvestigation,
		CreatorPrincipal: "alice",
	}
	if _, err := e.repo.CreateSession(e.ctx, sess); err != nil {
		t.Fatalf("create session: %v", err)
	}

	for _, class := range []safety.ActionClass{safety.ActionRead, safety.ActionMutate} {
		for _, principal := range []string{"alice", "bob"} {
			t.Run(class.String()+"/"+principal, func(t *testing.T) {
				d, err := sessiongate.Check(e.ctx, e.repo, sess.ID, principal, class)
				if err != nil {
					t.Fatalf("Check: %v", err)
				}
				if !d.Allow {
					t.Errorf("normal regime should Allow all classes/principals; got refuse with redirect=%q", d.CaptainSessionID)
				}
			})
		}
	}
}

// --- Incident regime: full §C matrix ---

func TestCheck_IncidentRegime_Matrix(t *testing.T) {
	e := newGateEnv(t)

	// Declare an incident — alice is captain by R-CAP1.
	captainSessionID, _, err := e.repo.DeclareIncidentRegime(e.ctx, "alice", sessionmodel.RegimeKindHuman)
	if err != nil {
		t.Fatalf("declare: %v", err)
	}

	// A parallel investigation session (non-captain) under the same incident.
	linked := captainSessionID
	investigationID := uuid.NewString()
	if _, err := e.repo.CreateSession(e.ctx, sessionmodel.AgentSession{
		ID: investigationID, Type: sessionmodel.SessionTypeInvestigation,
		CreatorPrincipal: "bob", LinkedIncidentID: &linked,
	}); err != nil {
		t.Fatalf("create investigation: %v", err)
	}

	cases := []struct {
		name         string
		sessionID    string
		principal    string
		class        safety.ActionClass
		wantAllow    bool
		wantRedirect string // expected CaptainSessionID; "" means empty redirect
	}{
		// Reads bypass the gate regardless of session/principal — §A1/§C1.
		{"read captain-session captain-principal", captainSessionID, "alice", safety.ActionRead, true, ""},
		{"read captain-session other-principal", captainSessionID, "bob", safety.ActionRead, true, ""},
		{"read non-captain-session captain-principal", investigationID, "alice", safety.ActionRead, true, ""},
		{"read non-captain-session other-principal", investigationID, "bob", safety.ActionRead, true, ""},

		// Mutate from captain session with captain principal → Allow.
		// (Former T2 and T3 rows collapse to one Mutate row; the gate
		// treated them identically pre-collapse.)
		{"mutate captain-session captain-principal", captainSessionID, "alice", safety.ActionMutate, true, ""},

		// Mutate from captain session by non-captain principal → refuse,
		// redirect to captain session (observer trying to mutate).
		{"mutate captain-session other-principal", captainSessionID, "bob", safety.ActionMutate, false, captainSessionID},

		// Mutate from non-captain session, even by captain's principal →
		// refuse with redirect (positional gate per §C4: it doesn't
		// matter who you are if you're not in the captain session).
		{"mutate non-captain-session captain-principal", investigationID, "alice", safety.ActionMutate, false, captainSessionID},

		// Mutate from non-captain session by other principal → refuse with redirect.
		{"mutate non-captain-session other-principal", investigationID, "bob", safety.ActionMutate, false, captainSessionID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := sessiongate.Check(e.ctx, e.repo, tc.sessionID, tc.principal, tc.class)
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if d.Allow != tc.wantAllow {
				t.Errorf("Allow = %v, want %v", d.Allow, tc.wantAllow)
			}
			if d.CaptainSessionID != tc.wantRedirect {
				t.Errorf("CaptainSessionID = %q, want %q", d.CaptainSessionID, tc.wantRedirect)
			}
		})
	}
}

// --- §B2 pending_captain: empty redirect target ---

func TestCheck_IncidentRegime_PendingCaptain_EmptyRedirect(t *testing.T) {
	e := newGateEnv(t)

	// Construct a pending_captain incident WITHOUT going through
	// DeclareIncidentRegime (which always attaches a captain).
	// 1. Create an incident session directly.
	state := sessionmodel.IncidentStateDeclared
	incident := sessionmodel.AgentSession{
		ID: uuid.NewString(), Type: sessionmodel.SessionTypeIncident,
		IncidentState: &state, CreatorPrincipal: "system",
	}
	if _, err := e.repo.CreateSession(e.ctx, incident); err != nil {
		t.Fatalf("create incident: %v", err)
	}
	// 2. Flip regime to incident (no captain attached — this simulates
	//    the R-CAP2 pending_captain state where Joe declared but no
	//    authorized human has attached yet).
	if err := e.repo.SetRegime(e.ctx, sessionmodel.Regime{Mode: sessionmodel.RegimeModeIncident}); err != nil {
		t.Fatalf("set regime: %v", err)
	}

	// Any mutation must refuse with empty CaptainSessionID, even
	// if the mutating session IS the incident session — the captain
	// doesn't exist yet.
	for _, class := range []safety.ActionClass{safety.ActionMutate} {
		t.Run(class.String(), func(t *testing.T) {
			d, err := sessiongate.Check(e.ctx, e.repo, incident.ID, "alice", class)
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if d.Allow {
				t.Error("pending_captain should refuse all mutations (§B2 null authority)")
			}
			if d.CaptainSessionID != "" {
				t.Errorf("pending_captain refusal must have empty CaptainSessionID, got %q", d.CaptainSessionID)
			}
		})
	}

	// Reads still allowed even in pending_captain (reads/discovery unaffected per §A1/§C1).
	d, err := sessiongate.Check(e.ctx, e.repo, incident.ID, "alice", safety.ActionRead)
	if err != nil {
		t.Fatalf("Check read: %v", err)
	}
	if !d.Allow {
		t.Error("reads must be allowed even in pending_captain (§A1/§C1)")
	}
}

// --- Defensive: regime incident but no active incident session ---

func TestCheck_IncidentRegime_NoActiveIncident_Refuses(t *testing.T) {
	e := newGateEnv(t)
	// Flip regime to incident without creating any session.
	if err := e.repo.SetRegime(e.ctx, sessionmodel.Regime{Mode: sessionmodel.RegimeModeIncident}); err != nil {
		t.Fatalf("set regime: %v", err)
	}

	d, err := sessiongate.Check(e.ctx, e.repo, "any-session", "alice", safety.ActionMutate)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if d.Allow {
		t.Error("inconsistent state (regime=incident but no active session) should fail closed")
	}
}

// --- §B1 principal-threading: after transfer, new principal allowed ---

func TestCheck_IncidentRegime_AfterCaptainTransfer_NewPrincipalAllowed(t *testing.T) {
	e := newGateEnv(t)
	// alice declares → captain.
	captainSessionID, _, err := e.repo.DeclareIncidentRegime(e.ctx, "alice", sessionmodel.RegimeKindHuman)
	if err != nil {
		t.Fatalf("declare: %v", err)
	}

	// alice can mutate.
	d, _ := sessiongate.Check(e.ctx, e.repo, captainSessionID, "alice", safety.ActionMutate)
	if !d.Allow {
		t.Fatal("precondition: alice should be allowed to mutate as captain")
	}

	// Manually transfer captaincy: detach alice, attach bob. The Change
	// 6 CaptainService.completeTransfer does exactly this; we drive the
	// repo directly so the gate test stays focused on Check.
	oldCap, _ := e.repo.GetActiveCaptain(e.ctx, captainSessionID)
	if err := e.repo.MarkCaptainDetached(e.ctx, oldCap.ID, oldCap.AttachedAt); err != nil {
		t.Fatalf("detach alice: %v", err)
	}
	active := sessionmodel.TransferStateActive
	if _, err := e.repo.AttachCaptain(e.ctx, sessionmodel.Captain{
		ID: uuid.NewString(), SessionID: captainSessionID,
		CaptainType: sessionmodel.CaptainTypeHuman, Principal: "bob",
		TransferState: &active,
	}); err != nil {
		t.Fatalf("attach bob: %v", err)
	}

	// alice (former captain) is now refused; bob is allowed.
	dAlice, _ := sessiongate.Check(e.ctx, e.repo, captainSessionID, "alice", safety.ActionMutate)
	if dAlice.Allow {
		t.Error("after transfer, alice should NOT be allowed (§B1 principal binding moved to bob)")
	}
	if dAlice.CaptainSessionID != captainSessionID {
		t.Errorf("refusal redirect = %q, want %q", dAlice.CaptainSessionID, captainSessionID)
	}

	dBob, _ := sessiongate.Check(e.ctx, e.repo, captainSessionID, "bob", safety.ActionMutate)
	if !dBob.Allow {
		t.Error("after transfer, bob should be allowed (§B1 principal binding)")
	}
}
