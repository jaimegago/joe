package warnings_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/internal/warnings"
)

// TestCascadeSchema_WarningWithSourceInvestigation_Cascades extends the
// §6-C structural guard for joe_warnings: when a warning is tied to an
// investigation session and that session is deleted, the warning row
// cascades away. (This is the "investigation context is part of the
// warning's identity" rule expressed by the ON DELETE CASCADE FK in
// migration 011.)
func TestCascadeSchema_WarningWithSourceInvestigation_Cascades(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sessRepo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	repo := warnings.NewRepository(s.DB(), store.DriverSQLite)

	sess := sessionmodel.AgentSession{
		ID: uuid.NewString(), Type: sessionmodel.SessionTypeDefault,
		CreatorPrincipal: "alice",
	}
	if _, err := sessRepo.CreateSession(ctx, sess); err != nil {
		t.Fatalf("create session: %v", err)
	}
	src := sess.ID

	w := warnings.Warning{
		ID:                           uuid.NewString(),
		SignalReference:              "x",
		Body:                         "y",
		SourceInvestigationSessionID: &src,
	}
	if _, err := repo.RaiseWarning(ctx, w); err != nil {
		t.Fatalf("raise: %v", err)
	}

	all, _ := repo.ListWarnings(ctx)
	if len(all) != 1 {
		t.Fatalf("pre-delete count = %d, want 1", len(all))
	}

	if _, err := s.DB().ExecContext(ctx,
		`DELETE FROM agent_sessions WHERE id = ?`, sess.ID); err != nil {
		t.Fatalf("delete session: %v", err)
	}

	all, _ = repo.ListWarnings(ctx)
	if len(all) != 0 {
		t.Errorf("post-delete count = %d, want 0 (FK cascade missing)", len(all))
	}
}

// TestCascadeSchema_WarningWithoutSourceInvestigation_Independent: a
// warning with NULL source_investigation_session_id is not tied to any
// session and is not affected by session deletes — the §E warnings
// surface outlives the investigation that produced it (or never had one).
func TestCascadeSchema_WarningWithoutSourceInvestigation_Independent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sessRepo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	repo := warnings.NewRepository(s.DB(), store.DriverSQLite)

	w := warnings.Warning{
		ID:              uuid.NewString(),
		SignalReference: "x",
		Body:            "free-standing warning",
	}
	if _, err := repo.RaiseWarning(ctx, w); err != nil {
		t.Fatalf("raise: %v", err)
	}

	// Create and delete an unrelated session.
	sess := sessionmodel.AgentSession{
		ID: uuid.NewString(), Type: sessionmodel.SessionTypeDefault,
		CreatorPrincipal: "alice",
	}
	if _, err := sessRepo.CreateSession(ctx, sess); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx,
		`DELETE FROM agent_sessions WHERE id = ?`, sess.ID); err != nil {
		t.Fatalf("delete unrelated session: %v", err)
	}

	all, _ := repo.ListWarnings(ctx)
	if len(all) != 1 {
		t.Errorf("standalone warning count = %d, want 1 (unrelated session delete should not affect)", len(all))
	}
}
