package runmodel_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/jaimegago/joe/internal/runmodel"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
)

// TestCascadeSchema_TwoLevelExpunge_Change2 extends the §6-C structural
// guard introduced in internal/sessionmodel/cascade_schema_test.go to cover
// the child tables added by Change 2:
//
//	agent_runs, run_steps, run_solicitations, run_world_handles,
//	tool_idempotency_keys, action_ledger
//
// Layout:
//
//	incident I
//	├── linked investigation J1
//	│   └── run + step + solicitation + world handle + idempotency key
//	│       + action ledger entry
//	├── linked investigation J2
//	│   └── (same)
//	└── (same on I itself)
//
// After DELETE FROM agent_sessions WHERE id = I, every row across every
// child table tied to I, J1, or J2 must be gone. The cascade is one SQL
// statement — the schema does the work.
func TestCascadeSchema_TwoLevelExpunge_Change2(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sessRepo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	repo := runmodel.NewRepository(s.DB(), store.DriverSQLite)

	declared := sessionmodel.IncidentStateDeclared

	// 1. Insert incident I.
	incidentID := uuid.NewString()
	if _, err := sessRepo.CreateSession(ctx, sessionmodel.AgentSession{
		ID:               incidentID,
		Type:             sessionmodel.SessionTypeIncident,
		IncidentState:    &declared,
		CreatorPrincipal: "alice",
	}); err != nil {
		t.Fatalf("create incident: %v", err)
	}

	// 2. Linked investigations J1, J2.
	j1ID := uuid.NewString()
	j2ID := uuid.NewString()
	for _, id := range []string{j1ID, j2ID} {
		linked := incidentID
		if _, err := sessRepo.CreateSession(ctx, sessionmodel.AgentSession{
			ID:               id,
			Type:             sessionmodel.SessionTypeInvestigation,
			CreatorPrincipal: "alice",
			LinkedIncidentID: &linked,
		}); err != nil {
			t.Fatalf("create investigation %s: %v", id, err)
		}
	}

	// 3. For each session, populate one row in every Change-2 child table.
	runIDs := make([]string, 0, 3)
	for _, sessID := range []string{incidentID, j1ID, j2ID} {
		run, err := repo.CreateRun(ctx, runmodel.Run{
			ID: uuid.NewString(), SessionID: sessID, State: runmodel.RunStateAwaitingInput,
		})
		if err != nil {
			t.Fatalf("create run for %s: %v", sessID, err)
		}
		runIDs = append(runIDs, run.ID)

		step, err := repo.AppendStep(ctx, runmodel.Step{
			ID: uuid.NewString(), RunID: run.ID, StepNumber: 1,
			Kind: runmodel.StepKindReasoning, Payload: `{"x":1}`,
		})
		if err != nil {
			t.Fatalf("append step: %v", err)
		}
		_ = step

		if _, err := repo.OpenSolicitation(ctx, runmodel.Solicitation{
			ID: uuid.NewString(), RunID: run.ID,
			Kind: runmodel.SolicitationKindDecision, Payload: `{"q":"go?"}`,
		}); err != nil {
			t.Fatalf("open solicitation: %v", err)
		}

		if _, err := repo.RecordWorldHandle(ctx, runmodel.WorldHandle{
			ID: uuid.NewString(), RunID: run.ID,
			Locator: "k8s://deploy/x", QueryMeta: `{"v":1}`,
		}); err != nil {
			t.Fatalf("record world handle: %v", err)
		}

		key := "k-" + uuid.NewString()
		if _, err := repo.RecordToolIntent(ctx, key, run.ID, "k8s_apply", "h"); err != nil {
			t.Fatalf("record tool intent: %v", err)
		}
		if _, err := repo.AppendLedger(ctx, runmodel.LedgerEntry{
			ID: uuid.NewString(), RunID: run.ID, IdempotencyKey: key,
			ToolName: "k8s_apply", Tier: runmodel.TierT2,
			Principal: "alice", Summary: "rolled", Status: "issued",
		}); err != nil {
			t.Fatalf("append ledger: %v", err)
		}
	}

	// Sanity: pre-delete counts. Each child table should have exactly 3 rows
	// tied to the three sessions / their runs.
	mustCount := func(query string, want int, args ...any) {
		t.Helper()
		var n int
		if err := s.DB().QueryRow(query, args...).Scan(&n); err != nil {
			t.Fatalf("count %q: %v", query, err)
		}
		if n != want {
			t.Fatalf("pre-delete count for %q = %d, want %d", query, n, want)
		}
	}
	mustCount(`SELECT count(*) FROM agent_sessions WHERE id IN (?,?,?)`, 3, incidentID, j1ID, j2ID)
	mustCount(`SELECT count(*) FROM agent_runs WHERE id IN (?,?,?)`, 3, runIDs[0], runIDs[1], runIDs[2])
	mustCount(`SELECT count(*) FROM run_steps WHERE run_id IN (?,?,?)`, 3, runIDs[0], runIDs[1], runIDs[2])
	mustCount(`SELECT count(*) FROM run_solicitations WHERE run_id IN (?,?,?)`, 3, runIDs[0], runIDs[1], runIDs[2])
	mustCount(`SELECT count(*) FROM run_world_handles WHERE run_id IN (?,?,?)`, 3, runIDs[0], runIDs[1], runIDs[2])
	mustCount(`SELECT count(*) FROM tool_idempotency_keys WHERE run_id IN (?,?,?)`, 3, runIDs[0], runIDs[1], runIDs[2])
	mustCount(`SELECT count(*) FROM action_ledger WHERE run_id IN (?,?,?)`, 3, runIDs[0], runIDs[1], runIDs[2])

	// 4. ONE SQL DELETE. No handler. No application code. No transaction
	//    wrapper beyond the implicit single-statement transaction.
	if _, err := s.DB().ExecContext(ctx,
		`DELETE FROM agent_sessions WHERE id = ?`, incidentID); err != nil {
		t.Fatalf("delete incident: %v", err)
	}

	// 5. Every session row tied to I / J1 / J2 is gone (two-level cascade).
	mustCount(`SELECT count(*) FROM agent_sessions WHERE id IN (?,?,?)`, 0, incidentID, j1ID, j2ID)

	// 6. Every child row from every Change-2 table is gone.
	mustCount(`SELECT count(*) FROM agent_runs WHERE id IN (?,?,?)`, 0, runIDs[0], runIDs[1], runIDs[2])
	mustCount(`SELECT count(*) FROM run_steps WHERE run_id IN (?,?,?)`, 0, runIDs[0], runIDs[1], runIDs[2])
	mustCount(`SELECT count(*) FROM run_solicitations WHERE run_id IN (?,?,?)`, 0, runIDs[0], runIDs[1], runIDs[2])
	mustCount(`SELECT count(*) FROM run_world_handles WHERE run_id IN (?,?,?)`, 0, runIDs[0], runIDs[1], runIDs[2])
	mustCount(`SELECT count(*) FROM tool_idempotency_keys WHERE run_id IN (?,?,?)`, 0, runIDs[0], runIDs[1], runIDs[2])
	mustCount(`SELECT count(*) FROM action_ledger WHERE run_id IN (?,?,?)`, 0, runIDs[0], runIDs[1], runIDs[2])
}
