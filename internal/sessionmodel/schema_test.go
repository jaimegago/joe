package sessionmodel_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
)

// newTestStore opens an in-memory SQLite, runs migrations, and returns the
// fully-wired store. Used by tests that need real schema + FK cascades.
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

// TestMigration009_SchemaSQLite asserts the schema runs cleanly on SQLite
// (the always-on half of the cross-driver guard called out in
// PHASE-1-DECOMPOSITION.md Change 1 / Invariant 6).
func TestMigration009_SchemaSQLite(t *testing.T) {
	s := newTestStore(t)
	db := s.DB()

	// The three tables exist and are queryable.
	for _, table := range []string{"agent_sessions", "system_regime", "session_captains"} {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatalf("query %s: %v", table, err)
		}
	}

	// system_regime is seeded with the single normal row.
	repo := sessionmodel.NewRepository(db, store.DriverSQLite)
	reg, err := repo.GetRegime(context.Background())
	if err != nil {
		t.Fatalf("GetRegime: %v", err)
	}
	if reg.Mode != sessionmodel.RegimeModeNormal {
		t.Errorf("seeded regime mode = %q, want %q", reg.Mode, sessionmodel.RegimeModeNormal)
	}
}

// TestMigration009_SchemaPostgres runs the same migrations against a
// Postgres instance when one is reachable. CI without Postgres skips it;
// the SQLite half plus the Postgres-portable-SQL rule (no AUTOINCREMENT,
// no STRICT, no SQLite-only JSON1) is the residual guard. PHASE-1-
// DECOMPOSITION.md §6 residual risks records this.
//
// To enable locally: set JOE_TEST_POSTGRES_DSN to a libpq-style DSN
// pointing at a disposable Postgres database (e.g.
//
//	postgres://joe:joe@localhost:5432/joe_test?sslmode=disable
//
// ).
func TestMigration009_SchemaPostgres(t *testing.T) {
	dsn := os.Getenv("JOE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("JOE_TEST_POSTGRES_DSN unset — skipping cross-driver Postgres half")
	}

	s, err := store.New(store.DatabaseConfig{Driver: store.DriverPostgres, DSN: dsn}, nil)
	if err != nil {
		t.Fatalf("store.New(postgres): %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate(postgres): %v", err)
	}

	repo := sessionmodel.NewRepository(s.DB(), store.DriverPostgres)
	reg, err := repo.GetRegime(context.Background())
	if err != nil {
		t.Fatalf("GetRegime(postgres): %v", err)
	}
	if reg.Mode != sessionmodel.RegimeModeNormal {
		t.Errorf("seeded regime mode = %q, want %q", reg.Mode, sessionmodel.RegimeModeNormal)
	}
}

// TestRepository_Sessions exercises CRUD on agent_sessions and validates the
// CHECK-constraint shape: incident_state is non-null only when type =
// 'incident'.
func TestRepository_Sessions(t *testing.T) {
	s := newTestStore(t)
	repo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	t.Run("create + get investigation (no incident_state)", func(t *testing.T) {
		sess := sessionmodel.AgentSession{
			ID:               uuid.NewString(),
			Type:             sessionmodel.SessionTypeDefault,
			CreatorPrincipal: "alice",
		}
		if _, err := repo.CreateSession(ctx, sess); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		got, err := repo.GetSession(ctx, sess.ID)
		if err != nil {
			t.Fatalf("GetSession: %v", err)
		}
		if got == nil {
			t.Fatal("GetSession returned nil")
		}
		if got.Type != sessionmodel.SessionTypeDefault {
			t.Errorf("type = %q, want investigation", got.Type)
		}
		if got.IncidentState != nil {
			t.Errorf("non-incident session has incident_state = %v, want nil", *got.IncidentState)
		}
	})

	t.Run("create incident with state", func(t *testing.T) {
		state := sessionmodel.IncidentStateDeclared
		sess := sessionmodel.AgentSession{
			ID:               uuid.NewString(),
			Type:             sessionmodel.SessionTypeIncident,
			IncidentState:    &state,
			CreatorPrincipal: "alice",
		}
		if _, err := repo.CreateSession(ctx, sess); err != nil {
			t.Fatalf("CreateSession incident: %v", err)
		}
	})

	t.Run("create incident without state is rejected", func(t *testing.T) {
		sess := sessionmodel.AgentSession{
			ID:               uuid.NewString(),
			Type:             sessionmodel.SessionTypeIncident,
			CreatorPrincipal: "alice",
		}
		if _, err := repo.CreateSession(ctx, sess); err == nil {
			t.Fatal("CreateSession incident without state should fail CHECK constraint")
		}
	})

	t.Run("create investigation with incident_state is rejected", func(t *testing.T) {
		state := sessionmodel.IncidentStateDeclared
		sess := sessionmodel.AgentSession{
			ID:               uuid.NewString(),
			Type:             sessionmodel.SessionTypeDefault,
			IncidentState:    &state,
			CreatorPrincipal: "alice",
		}
		if _, err := repo.CreateSession(ctx, sess); err == nil {
			t.Fatal("non-incident session with incident_state should fail CHECK constraint")
		}
	})

	t.Run("list and list-by-type", func(t *testing.T) {
		all, err := repo.ListSessions(ctx)
		if err != nil {
			t.Fatalf("ListSessions: %v", err)
		}
		if len(all) == 0 {
			t.Fatal("ListSessions empty")
		}

		incidents, err := repo.ListSessionsByType(ctx, sessionmodel.SessionTypeIncident)
		if err != nil {
			t.Fatalf("ListSessionsByType: %v", err)
		}
		for _, sess := range incidents {
			if sess.Type != sessionmodel.SessionTypeIncident {
				t.Errorf("filter leaked non-incident: %+v", sess)
			}
		}
	})
}

// TestRepository_Regime asserts the single-row regime table accepts
// transitions through SetRegime.
func TestRepository_Regime(t *testing.T) {
	s := newTestStore(t)
	repo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	principal := "alice"
	kind := sessionmodel.RegimeKindHuman
	if err := repo.SetRegime(ctx, sessionmodel.Regime{
		Mode:                sessionmodel.RegimeModeIncident,
		DeclaredAt:          &now,
		DeclaredByPrincipal: &principal,
		DeclaredKind:        &kind,
	}); err != nil {
		t.Fatalf("SetRegime incident: %v", err)
	}

	reg, err := repo.GetRegime(ctx)
	if err != nil {
		t.Fatalf("GetRegime: %v", err)
	}
	if reg.Mode != sessionmodel.RegimeModeIncident {
		t.Errorf("mode = %q, want incident", reg.Mode)
	}
	if reg.DeclaredByPrincipal == nil || *reg.DeclaredByPrincipal != "alice" {
		t.Errorf("declared_by_principal mismatch: %+v", reg.DeclaredByPrincipal)
	}
	if reg.DeclaredKind == nil || *reg.DeclaredKind != sessionmodel.RegimeKindHuman {
		t.Errorf("declared_kind mismatch: %+v", reg.DeclaredKind)
	}

	// Return to normal.
	if err := repo.SetRegime(ctx, sessionmodel.Regime{Mode: sessionmodel.RegimeModeNormal}); err != nil {
		t.Fatalf("SetRegime normal: %v", err)
	}
	reg, err = repo.GetRegime(ctx)
	if err != nil {
		t.Fatalf("GetRegime: %v", err)
	}
	if reg.Mode != sessionmodel.RegimeModeNormal {
		t.Errorf("mode = %q, want normal", reg.Mode)
	}
	if reg.DeclaredByPrincipal != nil {
		t.Errorf("declared_by_principal should be NULL after normal, got %v", *reg.DeclaredByPrincipal)
	}
}

// TestRepository_Captains exercises captain attach + active-captain lookup.
// The full §B state machine is the responsibility of Change 6.
func TestRepository_Captains(t *testing.T) {
	s := newTestStore(t)
	repo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	// Set up an incident session.
	state := sessionmodel.IncidentStateDeclared
	sess := sessionmodel.AgentSession{
		ID:               uuid.NewString(),
		Type:             sessionmodel.SessionTypeIncident,
		IncidentState:    &state,
		CreatorPrincipal: "alice",
	}
	if _, err := repo.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	activeState := sessionmodel.TransferStateActive
	cap := sessionmodel.Captain{
		ID:            uuid.NewString(),
		SessionID:     sess.ID,
		CaptainType:   sessionmodel.CaptainTypeHuman,
		Principal:     "alice",
		TransferState: &activeState,
	}
	if _, err := repo.AttachCaptain(ctx, cap); err != nil {
		t.Fatalf("AttachCaptain: %v", err)
	}

	got, err := repo.GetActiveCaptain(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetActiveCaptain: %v", err)
	}
	if got == nil {
		t.Fatal("GetActiveCaptain returned nil")
	}
	if got.Principal != "alice" {
		t.Errorf("principal = %q, want alice", got.Principal)
	}
	if got.TransferState == nil || *got.TransferState != sessionmodel.TransferStateActive {
		t.Errorf("transfer_state = %v, want active", got.TransferState)
	}

	// AttachCaptain with an invalid CaptainType fails the CHECK constraint.
	bad := sessionmodel.Captain{
		ID:          uuid.NewString(),
		SessionID:   sess.ID,
		CaptainType: "robot",
		Principal:   "bob",
	}
	if _, err := repo.AttachCaptain(ctx, bad); err == nil {
		t.Fatal("AttachCaptain with invalid type should fail CHECK constraint")
	}

	// All captains for the session — at least one row.
	all, err := repo.ListCaptainsForSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ListCaptainsForSession: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("ListCaptainsForSession returned no rows")
	}
}

// helper: assert a single column count equals expected.
func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("count query %q: %v", query, err)
	}
	return n
}
