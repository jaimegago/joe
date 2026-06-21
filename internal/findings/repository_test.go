package findings_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/findings"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func newTestSession(t *testing.T, s *store.Store, typ sessionmodel.SessionType) string {
	t.Helper()
	repo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	sess := sessionmodel.AgentSession{
		ID:               uuid.NewString(),
		Type:             typ,
		CreatorPrincipal: "alice",
	}
	if typ == sessionmodel.SessionTypeIncident {
		state := sessionmodel.IncidentStateDeclared
		sess.IncidentState = &state
	}
	if _, err := repo.CreateSession(context.Background(), sess); err != nil {
		t.Fatalf("create session: %v", err)
	}
	return sess.ID
}

func TestFindings_PostAndList(t *testing.T) {
	s := newTestStore(t)
	repo := findings.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	src := newTestSession(t, s, sessionmodel.SessionTypeDefault)
	tgt := newTestSession(t, s, sessionmodel.SessionTypeIncident)
	investigation := newTestSession(t, s, sessionmodel.SessionTypeDefault)

	f := findings.Finding{
		ID:                               uuid.NewString(),
		SourceSessionID:                  src,
		TargetSessionID:                  tgt,
		AuthorPrincipal:                  "alice",
		Body:                             "checked pod logs — culprit is X",
		ReferencedInvestigationSessionID: &investigation,
	}
	if _, err := repo.PostFinding(ctx, f); err != nil {
		t.Fatalf("PostFinding: %v", err)
	}

	got, err := repo.GetFinding(ctx, f.ID)
	if err != nil || got == nil {
		t.Fatalf("GetFinding: %v %v", err, got)
	}
	if got.Body != f.Body {
		t.Errorf("body mismatch")
	}
	if got.ReferencedInvestigationSessionID == nil || *got.ReferencedInvestigationSessionID != investigation {
		t.Errorf("referenced investigation mismatch: %+v", got.ReferencedInvestigationSessionID)
	}

	all, _ := repo.ListFindings(ctx)
	if len(all) != 1 {
		t.Errorf("ListFindings = %d, want 1", len(all))
	}
	forTarget, _ := repo.ListFindingsForTarget(ctx, tgt)
	if len(forTarget) != 1 {
		t.Errorf("ListFindingsForTarget = %d, want 1", len(forTarget))
	}

	otherIncident := newTestSession(t, s, sessionmodel.SessionTypeIncident)
	forOther, _ := repo.ListFindingsForTarget(ctx, otherIncident)
	if len(forOther) != 0 {
		t.Errorf("ListFindingsForTarget for unrelated target = %d, want 0", len(forOther))
	}
}

func TestFindings_FKEnforced(t *testing.T) {
	s := newTestStore(t)
	repo := findings.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	// source_session_id pointing at a nonexistent session must fail FK.
	if _, err := repo.PostFinding(ctx, findings.Finding{
		ID:              uuid.NewString(),
		SourceSessionID: "missing-source",
		TargetSessionID: "missing-target",
		AuthorPrincipal: "alice",
		Body:            "x",
	}); err == nil {
		t.Fatal("PostFinding with missing FK sessions should fail")
	}
}
