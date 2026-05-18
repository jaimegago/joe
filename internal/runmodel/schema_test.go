package runmodel_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/runmodel"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
)

// newTestStore opens an in-memory SQLite, runs all migrations, and returns
// the ready store. Mirrors internal/sessionmodel/schema_test.go's helper.
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

// newTestSession creates an incident or investigation session and returns
// its ID. Tests that need a parent session for runs use this.
func newTestSession(t *testing.T, s *store.Store) string {
	t.Helper()
	sessRepo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	state := sessionmodel.IncidentStateDeclared
	sess := sessionmodel.AgentSession{
		ID:               uuid.NewString(),
		Type:             sessionmodel.SessionTypeIncident,
		IncidentState:    &state,
		CreatorPrincipal: "alice",
	}
	if _, err := sessRepo.CreateSession(context.Background(), sess); err != nil {
		t.Fatalf("create session: %v", err)
	}
	return sess.ID
}

// TestMigration010_SchemaSQLite asserts the new run-model tables exist and
// are queryable after migration 010 runs.
func TestMigration010_SchemaSQLite(t *testing.T) {
	s := newTestStore(t)
	db := s.DB()
	for _, table := range []string{
		"agent_runs", "run_steps", "run_solicitations",
		"run_world_handles", "tool_idempotency_keys", "action_ledger",
	} {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatalf("query %s: %v", table, err)
		}
	}
}

// TestSchema_D3_SingleRunningRunPerSession is the named structural guard
// for §D3 / Invariant 1. The partial unique index
// idx_agent_runs_one_running_per_session on (session_id) WHERE state =
// 'running' must reject a second running row for the same session.
//
// The test must fail loudly if the index is missing — that is the whole
// point of pinning the guard structurally.
func TestSchema_D3_SingleRunningRunPerSession(t *testing.T) {
	s := newTestStore(t)
	sessionID := newTestSession(t, s)
	repo := runmodel.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	// First running run — must succeed.
	first, err := repo.CreateRun(ctx, runmodel.Run{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		State:     runmodel.RunStateRunning,
	})
	if err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Second running run on the same session — must fail because of the
	// partial unique index.
	if _, err := repo.CreateRun(ctx, runmodel.Run{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		State:     runmodel.RunStateRunning,
	}); err == nil {
		t.Fatal("second running run on same session should have been rejected by " +
			"idx_agent_runs_one_running_per_session — guard is missing or broken")
	}

	// Transition the first run away from 'running' and now a new running
	// run is allowed. The partial index by definition doesn't apply to
	// non-'running' rows.
	endedAt := time.Now().UTC()
	if err := repo.UpdateRunState(ctx, first.ID, runmodel.RunStateCompleted, &endedAt); err != nil {
		t.Fatalf("update first to completed: %v", err)
	}
	if _, err := repo.CreateRun(ctx, runmodel.Run{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		State:     runmodel.RunStateRunning,
	}); err != nil {
		t.Fatalf("third running run after first completed: %v", err)
	}

	// A second session can have its own running run unaffected.
	otherSessionID := newTestSession(t, s)
	if _, err := repo.CreateRun(ctx, runmodel.Run{
		ID:        uuid.NewString(),
		SessionID: otherSessionID,
		State:     runmodel.RunStateRunning,
	}); err != nil {
		t.Fatalf("running run on separate session: %v", err)
	}
}

// TestSchema_RunState_CheckConstraint: unknown states are rejected.
func TestSchema_RunState_CheckConstraint(t *testing.T) {
	s := newTestStore(t)
	sessionID := newTestSession(t, s)
	db := s.DB()

	_, err := db.ExecContext(context.Background(), store.Rebind(store.DriverSQLite, `
		INSERT INTO agent_runs (id, session_id, state, started_at)
		VALUES (?, ?, ?, ?)`),
		uuid.NewString(), sessionID, "not_a_state", time.Now().UTC().Format(time.RFC3339))
	if err == nil {
		t.Fatal("inserting run with invalid state should fail CHECK constraint")
	}
}

// TestRepository_StepsAndLedger: end-to-end sanity for the step and ledger
// flows the §6-C cascade test depends on.
func TestRepository_StepsAndLedger(t *testing.T) {
	s := newTestStore(t)
	sessionID := newTestSession(t, s)
	repo := runmodel.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	run, err := repo.CreateRun(ctx, runmodel.Run{
		ID:        uuid.NewString(),
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	step, err := repo.AppendStep(ctx, runmodel.Step{
		ID:         uuid.NewString(),
		RunID:      run.ID,
		StepNumber: 1,
		Kind:       runmodel.StepKindReasoning,
		Payload:    `{"summary":"chose plan A"}`,
	})
	if err != nil {
		t.Fatalf("append step: %v", err)
	}
	if err := repo.SetLastStepID(ctx, run.ID, step.ID); err != nil {
		t.Fatalf("set last step: %v", err)
	}

	got, err := repo.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.LastStepID == nil || *got.LastStepID != step.ID {
		t.Errorf("last_step_id mismatch: %+v", got.LastStepID)
	}

	steps, err := repo.ListStepsForRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("list steps: %v", err)
	}
	if len(steps) != 1 {
		t.Errorf("steps count = %d, want 1", len(steps))
	}

	// Idempotency key + ledger entry — the §D8 attaching-SRE shape.
	key := "tool-call-" + uuid.NewString()
	if _, err := repo.RecordToolIntent(ctx, key, run.ID, "k8s_apply", "abc123"); err != nil {
		t.Fatalf("record tool intent: %v", err)
	}
	if err := repo.MarkToolCompleted(ctx, key, `{"ok":true}`); err != nil {
		t.Fatalf("mark completed: %v", err)
	}

	_, err = repo.AppendLedger(ctx, runmodel.LedgerEntry{
		ID:             uuid.NewString(),
		RunID:          run.ID,
		IdempotencyKey: key,
		ToolName:       "k8s_apply",
		Tier:           runmodel.TierT2,
		Principal:      "alice",
		Summary:        "rolled deployment",
		Status:         "completed",
	})
	if err != nil {
		t.Fatalf("append ledger: %v", err)
	}

	entries, err := repo.ListLedgerForRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("list ledger: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("ledger count = %d, want 1", len(entries))
	}
	if entries[0].Tier != runmodel.TierT2 {
		t.Errorf("ledger tier = %d, want 2", entries[0].Tier)
	}
}

// TestSchema_PartialIndexExists is a meta-guard: it asserts the partial
// unique index actually exists in the SQLite catalog. If a future
// contributor drops or renames it without noticing, this test fails
// immediately, in addition to the behavioral test above.
func TestSchema_PartialIndexExists(t *testing.T) {
	s := newTestStore(t)
	rows, err := s.DB().Query(`
		SELECT name, sql FROM sqlite_master
		WHERE type = 'index' AND name = 'idx_agent_runs_one_running_per_session'`)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("idx_agent_runs_one_running_per_session is missing")
	}
	var name, idxSQL sql.NullString
	if err := rows.Scan(&name, &idxSQL); err != nil {
		t.Fatalf("scan index: %v", err)
	}
	if !idxSQL.Valid || !strings.Contains(idxSQL.String, "WHERE state = 'running'") {
		t.Errorf("index is not partial-on-running: %v", idxSQL.String)
	}
}
