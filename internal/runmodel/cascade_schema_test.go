package runmodel_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/jaimegago/joe/internal/runmodel"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
)

// TestCascadeSchema_IncidentDeleteSeversLinks_Runs extends the §12.4 severance
// guard (see internal/sessionmodel/cascade_schema_test.go) across the run-model
// child tables:
//
//	agent_runs, run_steps, run_solicitations, run_world_handles,
//	tool_idempotency_keys, action_ledger
//
// Layout:
//
//	incident I
//	├── linked session J1
//	│   └── run + step + solicitation + world handle + idempotency key
//	│       + action ledger entry
//	├── linked session J2
//	│   └── (same)
//	└── (same on I itself)
//
// After DELETE FROM agent_sessions WHERE id = I, only I's OWN run-chain is
// destroyed (its agent_runs row and all run-keyed children cascade). J1 and J2
// SURVIVE with linked_incident_id severed to NULL (§12.4: linked_incident_id is
// ON DELETE SET NULL, not CASCADE), and so do their entire run-chains. This
// REPLACES the as-built two-level-expunge guard.
func TestCascadeSchema_IncidentDeleteSeversLinks_Runs(t *testing.T) {
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
			Type:             sessionmodel.SessionTypeDefault,
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

	// 5. I is gone; J1 and J2 SURVIVE with their links severed to NULL.
	mustCount(`SELECT count(*) FROM agent_sessions WHERE id = ?`, 0, incidentID)
	mustCount(`SELECT count(*) FROM agent_sessions WHERE id IN (?,?)`, 2, j1ID, j2ID)
	mustCount(`SELECT count(*) FROM agent_sessions WHERE id IN (?,?) AND linked_incident_id IS NULL`, 2, j1ID, j2ID)

	// 6. I's OWN run-chain (runIDs[0]) is fully cascaded away...
	incRun := runIDs[0]
	mustCount(`SELECT count(*) FROM agent_runs WHERE id = ?`, 0, incRun)
	mustCount(`SELECT count(*) FROM run_steps WHERE run_id = ?`, 0, incRun)
	mustCount(`SELECT count(*) FROM run_solicitations WHERE run_id = ?`, 0, incRun)
	mustCount(`SELECT count(*) FROM run_world_handles WHERE run_id = ?`, 0, incRun)
	mustCount(`SELECT count(*) FROM tool_idempotency_keys WHERE run_id = ?`, 0, incRun)
	mustCount(`SELECT count(*) FROM action_ledger WHERE run_id = ?`, 0, incRun)

	// ...but J1's and J2's run-chains (runIDs[1], runIDs[2]) survive intact.
	j1Run, j2Run := runIDs[1], runIDs[2]
	mustCount(`SELECT count(*) FROM agent_runs WHERE id IN (?,?)`, 2, j1Run, j2Run)
	mustCount(`SELECT count(*) FROM run_steps WHERE run_id IN (?,?)`, 2, j1Run, j2Run)
	mustCount(`SELECT count(*) FROM run_solicitations WHERE run_id IN (?,?)`, 2, j1Run, j2Run)
	mustCount(`SELECT count(*) FROM run_world_handles WHERE run_id IN (?,?)`, 2, j1Run, j2Run)
	mustCount(`SELECT count(*) FROM tool_idempotency_keys WHERE run_id IN (?,?)`, 2, j1Run, j2Run)
	mustCount(`SELECT count(*) FROM action_ledger WHERE run_id IN (?,?)`, 2, j1Run, j2Run)
}
