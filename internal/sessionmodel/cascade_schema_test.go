package sessionmodel_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
)

// TestCascadeSchema_IncidentDeleteSeversLinks is the §12.4 named structural
// guard: deleting an incident SEVERS its linked sessions (ON DELETE SET NULL)
// rather than cascade-destroying them. It is a pure schema property — no
// application code, one SQL DELETE statement. This REPLACES the as-built
// two-level-expunge guard, which relied on linked_incident_id ON DELETE CASCADE
// (now SET NULL per §12.4: linked sessions are independent conversations and
// must never be destroyed by an incident purge).
//
// Layout:
//
//	incident I
//	├── linked session J1 (linked_incident_id = I)
//	│   └── captain on J1
//	├── linked session J2 (linked_incident_id = I)
//	│   └── captain on J2
//	└── captain on I
//
// After DELETE FROM agent_sessions WHERE id = I:
//
//   - I's row is gone (direct), and I's captain is gone (session_captains
//     ON DELETE CASCADE — a session's OWN dependent rows still cascade).
//   - J1 and J2 SURVIVE, with linked_incident_id set to NULL (the link is
//     severed, the second-level property reversed from the old CASCADE).
//   - J1's and J2's captains SURVIVE (their parent session survives).
func TestCascadeSchema_IncidentDeleteSeversLinks(t *testing.T) {
	s := newTestStore(t)
	repo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	incidentState := sessionmodel.IncidentStateDeclared

	// 1. Insert incident I.
	incidentID := uuid.NewString()
	if _, err := repo.CreateSession(ctx, sessionmodel.AgentSession{
		ID:               incidentID,
		Type:             sessionmodel.SessionTypeIncident,
		IncidentState:    &incidentState,
		CreatorPrincipal: "alice",
	}); err != nil {
		t.Fatalf("create incident I: %v", err)
	}

	// 2. Insert linked sessions J1 and J2 (plain 'default' conversations that
	//    point at the incident via linked_incident_id).
	j1ID := uuid.NewString()
	j2ID := uuid.NewString()
	for _, id := range []string{j1ID, j2ID} {
		linked := incidentID
		if _, err := repo.CreateSession(ctx, sessionmodel.AgentSession{
			ID:               id,
			Type:             sessionmodel.SessionTypeDefault,
			CreatorPrincipal: "alice",
			LinkedIncidentID: &linked,
		}); err != nil {
			t.Fatalf("create linked session %s: %v", id, err)
		}
	}

	// 3. Attach a captain under each session.
	for _, sessID := range []string{incidentID, j1ID, j2ID} {
		if _, err := repo.AttachCaptain(ctx, sessionmodel.Captain{
			ID:          uuid.NewString(),
			SessionID:   sessID,
			CaptainType: sessionmodel.CaptainTypeHuman,
			Principal:   "alice",
		}); err != nil {
			t.Fatalf("attach captain to %s: %v", sessID, err)
		}
	}

	// Sanity: counts before deletion.
	if n := countRows(t, s.DB(),
		`SELECT count(*) FROM agent_sessions WHERE id IN (?,?,?)`,
		incidentID, j1ID, j2ID); n != 3 {
		t.Fatalf("pre-delete sessions count = %d, want 3", n)
	}

	// 4. Execute one raw SQL DELETE — no handler, no application code, no
	//    explicit transaction. This is the schema self-check: the SET NULL
	//    severance is structural.
	if _, err := s.DB().ExecContext(ctx,
		`DELETE FROM agent_sessions WHERE id = ?`, incidentID); err != nil {
		t.Fatalf("delete incident: %v", err)
	}

	// 5. Assert: I is gone; J1 and J2 SURVIVE.
	if n := countRows(t, s.DB(),
		`SELECT count(*) FROM agent_sessions WHERE id = ?`, incidentID); n != 0 {
		t.Errorf("incident post-delete count = %d, want 0", n)
	}
	if n := countRows(t, s.DB(),
		`SELECT count(*) FROM agent_sessions WHERE id IN (?,?)`,
		j1ID, j2ID); n != 2 {
		t.Errorf("linked sessions post-delete count = %d, want 2 (must survive — link severed, not cascaded)", n)
	}

	// 6. Assert: the severed links are now NULL on the surviving sessions.
	if n := countRows(t, s.DB(),
		`SELECT count(*) FROM agent_sessions WHERE id IN (?,?) AND linked_incident_id IS NULL`,
		j1ID, j2ID); n != 2 {
		t.Errorf("linked sessions with NULL link = %d, want 2 (ON DELETE SET NULL did not fire)", n)
	}

	// 7. Assert: I's captain is gone (own-row cascade); J1/J2 captains survive.
	if n := countRows(t, s.DB(),
		`SELECT count(*) FROM session_captains WHERE session_id = ?`, incidentID); n != 0 {
		t.Errorf("incident captain post-delete count = %d, want 0 (own-row cascade failed)", n)
	}
	if n := countRows(t, s.DB(),
		`SELECT count(*) FROM session_captains WHERE session_id IN (?,?)`,
		j1ID, j2ID); n != 2 {
		t.Errorf("linked-session captains post-delete count = %d, want 2 (must survive with their session)", n)
	}
}

// TestCascadeSchema_ResolvedIncidentPostmortemProperty is the "linked
// investigations survive incident *resolution*" guard from Change 11's
// acceptance, asserted at the schema level here because the rule is
// structural: only DELETE cascades; UPDATE incident_state does not.
// Resolution is an UPDATE, not a DELETE, so linked investigations remain.
func TestCascadeSchema_ResolvedIncidentPostmortemProperty(t *testing.T) {
	s := newTestStore(t)
	db := s.DB()
	repo := sessionmodel.NewRepository(db, store.DriverSQLite)
	ctx := context.Background()

	declared := sessionmodel.IncidentStateDeclared
	incidentID := uuid.NewString()
	if _, err := repo.CreateSession(ctx, sessionmodel.AgentSession{
		ID:               incidentID,
		Type:             sessionmodel.SessionTypeIncident,
		IncidentState:    &declared,
		CreatorPrincipal: "alice",
	}); err != nil {
		t.Fatalf("create incident: %v", err)
	}
	linked := incidentID
	jID := uuid.NewString()
	if _, err := repo.CreateSession(ctx, sessionmodel.AgentSession{
		ID:               jID,
		Type:             sessionmodel.SessionTypeDefault,
		CreatorPrincipal: "alice",
		LinkedIncidentID: &linked,
	}); err != nil {
		t.Fatalf("create investigation: %v", err)
	}

	// Resolve = UPDATE incident_state. Linked investigations must remain.
	if _, err := db.ExecContext(ctx, store.Rebind(store.DriverSQLite, `
		UPDATE agent_sessions SET incident_state = ? WHERE id = ?`),
		string(sessionmodel.IncidentStateResolved), incidentID); err != nil {
		t.Fatalf("update incident_state: %v", err)
	}

	if n := countRows(t, db, `SELECT count(*) FROM agent_sessions WHERE id = ?`, jID); n != 1 {
		t.Errorf("linked investigation count after resolve = %d, want 1 (postmortem property failed)", n)
	}
}

// TestCascadeSchema_NonIncidentDeleteIndependent: deleting a free-standing
// investigation (no linked_incident_id) removes only that one row.
func TestCascadeSchema_NonIncidentDeleteIndependent(t *testing.T) {
	s := newTestStore(t)
	repo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	soloID := uuid.NewString()
	if _, err := repo.CreateSession(ctx, sessionmodel.AgentSession{
		ID:               soloID,
		Type:             sessionmodel.SessionTypeDefault,
		CreatorPrincipal: "alice",
	}); err != nil {
		t.Fatalf("create solo investigation: %v", err)
	}
	otherID := uuid.NewString()
	if _, err := repo.CreateSession(ctx, sessionmodel.AgentSession{
		ID:               otherID,
		Type:             sessionmodel.SessionTypeDefault,
		CreatorPrincipal: "alice",
	}); err != nil {
		t.Fatalf("create other: %v", err)
	}

	if err := repo.DeleteSession(ctx, soloID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if n := countRows(t, s.DB(), `SELECT count(*) FROM agent_sessions WHERE id = ?`, soloID); n != 0 {
		t.Errorf("solo session not deleted")
	}
	if n := countRows(t, s.DB(), `SELECT count(*) FROM agent_sessions WHERE id = ?`, otherID); n != 1 {
		t.Errorf("other session was incorrectly affected: count = %d, want 1", n)
	}
}
