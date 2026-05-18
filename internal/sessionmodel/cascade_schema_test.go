package sessionmodel_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
)

// TestCascadeSchema_TwoLevelExpunge_Change1 is the §6-C named structural
// guard introduced by Change 1. It proves two-level expunge as a pure
// schema property — no application code, one SQL DELETE statement.
//
// Layout:
//
//	incident I
//	├── linked investigation J1 (linked_incident_id = I)
//	│   └── captain on J1
//	├── linked investigation J2 (linked_incident_id = I)
//	│   └── captain on J2
//	└── captain on I
//
// After DELETE FROM agent_sessions WHERE id = I:
//
//   - I's row is gone (direct).
//   - J1 and J2's rows are gone via linked_incident_id ON DELETE CASCADE
//     (the second level — the property that makes this a schema test, not
//     an application-code test).
//   - All captain rows tied to I/J1/J2 are gone via session_captains.session_id
//     ON DELETE CASCADE (downward cascade to child tables this change
//     introduces).
//
// Changes 2 and 3 will extend this test with their own child tables; see
// PHASE-1-DECOMPOSITION.md §6-C.
func TestCascadeSchema_TwoLevelExpunge_Change1(t *testing.T) {
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

	// 2. Insert linked investigations J1 and J2.
	j1ID := uuid.NewString()
	j2ID := uuid.NewString()
	for _, id := range []string{j1ID, j2ID} {
		linked := incidentID
		if _, err := repo.CreateSession(ctx, sessionmodel.AgentSession{
			ID:               id,
			Type:             sessionmodel.SessionTypeInvestigation,
			CreatorPrincipal: "alice",
			LinkedIncidentID: &linked,
		}); err != nil {
			t.Fatalf("create investigation %s: %v", id, err)
		}
	}

	// 3. Insert at least one child row per child table introduced by this
	//    change (just session_captains in Change 1) under each session.
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
	if n := countRows(t, s.DB(),
		`SELECT count(*) FROM session_captains WHERE session_id IN (?,?,?)`,
		incidentID, j1ID, j2ID); n != 3 {
		t.Fatalf("pre-delete captains count = %d, want 3", n)
	}

	// 4. Execute one raw SQL DELETE — no handler, no application code, no
	//    explicit transaction. This is the §6-C self-check: the cascade is
	//    structural.
	if _, err := s.DB().ExecContext(ctx,
		`DELETE FROM agent_sessions WHERE id = ?`, incidentID); err != nil {
		t.Fatalf("delete incident: %v", err)
	}

	// 5. Assert: I, J1, and J2 are all gone.
	if n := countRows(t, s.DB(),
		`SELECT count(*) FROM agent_sessions WHERE id IN (?,?,?)`,
		incidentID, j1ID, j2ID); n != 0 {
		t.Errorf("post-delete sessions count = %d, want 0 (two-level cascade failed)", n)
	}

	// 6. Assert: every child row tied to I/J1/J2 is gone.
	if n := countRows(t, s.DB(),
		`SELECT count(*) FROM session_captains WHERE session_id IN (?,?,?)`,
		incidentID, j1ID, j2ID); n != 0 {
		t.Errorf("post-delete captains count = %d, want 0 (child-table cascade failed)", n)
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
		Type:             sessionmodel.SessionTypeInvestigation,
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
		Type:             sessionmodel.SessionTypeInvestigation,
		CreatorPrincipal: "alice",
	}); err != nil {
		t.Fatalf("create solo investigation: %v", err)
	}
	otherID := uuid.NewString()
	if _, err := repo.CreateSession(ctx, sessionmodel.AgentSession{
		ID:               otherID,
		Type:             sessionmodel.SessionTypeOther,
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
