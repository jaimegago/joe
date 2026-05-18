package findings_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/jaimegago/joe/internal/findings"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
)

// TestCascadeSchema_TwoLevelExpunge_Change3_Findings extends the §6-C
// structural guard with the findings table:
//
//	incident I  -- target_session_id of finding_for_I
//	├── investigation J1  -- source_session_id of finding_for_I,
//	│                         and target_session_id of finding_for_J1
//	├── investigation J2  -- referenced_investigation_session_id of
//	│                         finding_for_I
//	└── (cascade)
//
// After DELETE FROM agent_sessions WHERE id = I, every finding row tied to
// I, J1, or J2 (via ANY of its three FKs) must be gone. This guards
// against a future schema change that drops ON DELETE CASCADE on any one
// of the three FKs.
func TestCascadeSchema_TwoLevelExpunge_Change3_Findings(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sessRepo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	repo := findings.NewRepository(s.DB(), store.DriverSQLite)

	state := sessionmodel.IncidentStateDeclared
	incidentID := uuid.NewString()
	if _, err := sessRepo.CreateSession(ctx, sessionmodel.AgentSession{
		ID: incidentID, Type: sessionmodel.SessionTypeIncident,
		IncidentState: &state, CreatorPrincipal: "alice",
	}); err != nil {
		t.Fatalf("create incident: %v", err)
	}
	j1ID := uuid.NewString()
	j2ID := uuid.NewString()
	for _, id := range []string{j1ID, j2ID} {
		linked := incidentID
		if _, err := sessRepo.CreateSession(ctx, sessionmodel.AgentSession{
			ID: id, Type: sessionmodel.SessionTypeInvestigation,
			CreatorPrincipal: "alice", LinkedIncidentID: &linked,
		}); err != nil {
			t.Fatalf("create investigation %s: %v", id, err)
		}
	}

	// finding_for_I: target = I, source = J1, referenced = J2.
	findingForI := findings.Finding{
		ID:                               uuid.NewString(),
		SourceSessionID:                  j1ID,
		TargetSessionID:                  incidentID,
		AuthorPrincipal:                  "alice",
		Body:                             "synthesis A",
		ReferencedInvestigationSessionID: &j2ID,
	}
	if _, err := repo.PostFinding(ctx, findingForI); err != nil {
		t.Fatalf("post finding_for_I: %v", err)
	}

	// finding_for_J1: target = J1, source = J2.
	findingForJ1 := findings.Finding{
		ID:              uuid.NewString(),
		SourceSessionID: j2ID,
		TargetSessionID: j1ID,
		AuthorPrincipal: "alice",
		Body:            "synthesis B",
	}
	if _, err := repo.PostFinding(ctx, findingForJ1); err != nil {
		t.Fatalf("post finding_for_J1: %v", err)
	}

	// Pre-delete: two findings exist.
	all, _ := repo.ListFindings(ctx)
	if len(all) != 2 {
		t.Fatalf("pre-delete findings count = %d, want 2", len(all))
	}

	// One SQL DELETE.
	if _, err := s.DB().ExecContext(ctx,
		`DELETE FROM agent_sessions WHERE id = ?`, incidentID); err != nil {
		t.Fatalf("delete incident: %v", err)
	}

	// Both findings gone: finding_for_I via target_session_id cascade,
	// finding_for_J1 via target_session_id (J1) cascade (J1 itself was
	// expunged via linked_incident_id two-level cascade).
	all, _ = repo.ListFindings(ctx)
	if len(all) != 0 {
		t.Errorf("post-delete findings count = %d, want 0 (cascade failed)", len(all))
		for _, f := range all {
			t.Logf("survived: %+v", f)
		}
	}
}

// TestCascadeSchema_FindingCascadeOnEachFK: an independent finding whose
// only tie to a deleted session is through a SINGLE FK must still be
// cascaded — once for each of the three FKs.
func TestCascadeSchema_FindingCascadeOnEachFK(t *testing.T) {
	cases := []struct {
		name   string
		setFKs func(t *testing.T, s *store.Store, doomedSession, otherSession string) findings.Finding
	}{
		{
			name: "source_session_id cascade",
			setFKs: func(t *testing.T, s *store.Store, doomed, other string) findings.Finding {
				return findings.Finding{
					ID:              uuid.NewString(),
					SourceSessionID: doomed,
					TargetSessionID: other,
					AuthorPrincipal: "alice",
					Body:            "src cascade",
				}
			},
		},
		{
			name: "target_session_id cascade",
			setFKs: func(t *testing.T, s *store.Store, doomed, other string) findings.Finding {
				return findings.Finding{
					ID:              uuid.NewString(),
					SourceSessionID: other,
					TargetSessionID: doomed,
					AuthorPrincipal: "alice",
					Body:            "tgt cascade",
				}
			},
		},
		{
			name: "referenced_investigation_session_id cascade",
			setFKs: func(t *testing.T, s *store.Store, doomed, other string) findings.Finding {
				return findings.Finding{
					ID:                               uuid.NewString(),
					SourceSessionID:                  other,
					TargetSessionID:                  other,
					AuthorPrincipal:                  "alice",
					Body:                             "ref cascade",
					ReferencedInvestigationSessionID: &doomed,
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			ctx := context.Background()
			repo := findings.NewRepository(s.DB(), store.DriverSQLite)
			doomed := newTestSession(t, s, sessionmodel.SessionTypeInvestigation)
			other := newTestSession(t, s, sessionmodel.SessionTypeInvestigation)

			f := tc.setFKs(t, s, doomed, other)
			if _, err := repo.PostFinding(ctx, f); err != nil {
				t.Fatalf("post: %v", err)
			}
			if _, err := s.DB().ExecContext(ctx,
				`DELETE FROM agent_sessions WHERE id = ?`, doomed); err != nil {
				t.Fatalf("delete doomed: %v", err)
			}
			got, _ := repo.GetFinding(ctx, f.ID)
			if got != nil {
				t.Errorf("finding survived %s deletion — FK is missing ON DELETE CASCADE", tc.name)
			}
		})
	}
}
