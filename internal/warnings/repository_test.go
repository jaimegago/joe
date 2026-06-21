package warnings_test

import (
	"context"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/internal/warnings"
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

// TestWarningsRepository_AppendOnlyShape is the §E2 / R9 named structural
// guard. The warnings.Repository interface MUST expose exactly these three
// methods:
//
//	RaiseWarning, ListWarnings, MarkReviewed
//
// Anything else — Update*, Delete*, Get, queue/state methods — fails this
// test. The "warnings is not a queue with state" property is structurally
// enforced rather than relying on review.
//
// To add a method, the contributor must extend the `expected` set here in
// the same diff and justify it against §E2: the warnings surface is
// deliberately minimal — not a queue, not state-tracked, not self-
// escalating.
func TestWarningsRepository_AppendOnlyShape(t *testing.T) {
	expected := []string{"ListWarnings", "MarkReviewed", "RaiseWarning"}

	repoType := reflect.TypeOf((*warnings.Repository)(nil)).Elem()
	got := make([]string, 0, repoType.NumMethod())
	for i := 0; i < repoType.NumMethod(); i++ {
		got = append(got, repoType.Method(i).Name)
	}
	sort.Strings(got)
	sort.Strings(expected)

	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("warnings.Repository method set drifted.\n  got:      %v\n  expected: %v\n\n"+
			"This is the §E2 / R9 named structural guard. The warnings surface is "+
			"deliberately minimal — not a queue, not state-tracked, not self-"+
			"escalating. Adding a method requires updating this test in the same "+
			"diff with explicit justification.", got, expected)
	}
}

// TestRaiseWarning_AndList: end-to-end sanity for the append + read flow.
func TestRaiseWarning_AndList(t *testing.T) {
	s := newTestStore(t)
	repo := warnings.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	w := warnings.Warning{
		ID:              uuid.NewString(),
		SignalReference: "alert://prom/HighCPU/pod-x",
		Body:            "noisy alert pattern; not declaring incident",
	}
	if _, err := repo.RaiseWarning(ctx, w); err != nil {
		t.Fatalf("RaiseWarning: %v", err)
	}
	all, err := repo.ListWarnings(ctx)
	if err != nil {
		t.Fatalf("ListWarnings: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("count = %d, want 1", len(all))
	}
	if all[0].SignalReference != w.SignalReference {
		t.Errorf("signal mismatch: %q", all[0].SignalReference)
	}
	if all[0].ReviewedAt != nil {
		t.Errorf("ReviewedAt should be nil on raise, got %v", *all[0].ReviewedAt)
	}
}

// TestMarkReviewed_StampsReviewer: the one allowed mutation.
func TestMarkReviewed_StampsReviewer(t *testing.T) {
	s := newTestStore(t)
	repo := warnings.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	w := warnings.Warning{ID: uuid.NewString(), SignalReference: "x", Body: "y"}
	if _, err := repo.RaiseWarning(ctx, w); err != nil {
		t.Fatalf("raise: %v", err)
	}

	reviewedAt := time.Now().UTC().Truncate(time.Second)
	if err := repo.MarkReviewed(ctx, w.ID, "alice", reviewedAt); err != nil {
		t.Fatalf("MarkReviewed: %v", err)
	}

	all, _ := repo.ListWarnings(ctx)
	if all[0].ReviewedAt == nil || all[0].ReviewedByPrincipal == nil {
		t.Fatal("reviewed fields not stamped")
	}
	if *all[0].ReviewedByPrincipal != "alice" {
		t.Errorf("reviewer = %q, want alice", *all[0].ReviewedByPrincipal)
	}

	// Idempotent re-review: a second call against an already-reviewed row
	// is a no-op (does not overwrite the original reviewer/timestamp). The
	// WHERE reviewed_at IS NULL clause makes this structural.
	if err := repo.MarkReviewed(ctx, w.ID, "bob", time.Now().UTC()); err != nil {
		t.Fatalf("second MarkReviewed: %v", err)
	}
	all, _ = repo.ListWarnings(ctx)
	if *all[0].ReviewedByPrincipal != "alice" {
		t.Errorf("re-review overwrote reviewer to %q — must remain alice", *all[0].ReviewedByPrincipal)
	}
}

// TestWarning_WithSourceInvestigation: source_investigation_session_id is
// nullable and the cascade test asserts cascade behavior; here we just
// verify the FK accepts a real session ID.
func TestWarning_WithSourceInvestigation(t *testing.T) {
	s := newTestStore(t)
	repo := warnings.NewRepository(s.DB(), store.DriverSQLite)
	sessRepo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

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
		t.Fatalf("raise with source: %v", err)
	}

	// FK violation: source session that doesn't exist.
	if _, err := s.DB().ExecContext(ctx, store.Rebind(store.DriverSQLite, `
		INSERT INTO joe_warnings (id, raised_at, signal_reference, body, source_investigation_session_id, reviewed_at, reviewed_by_principal)
		VALUES (?, ?, ?, ?, ?, NULL, NULL)`),
		uuid.NewString(), time.Now().UTC().Format(time.RFC3339), "x", "y", "nonexistent-session"); err == nil {
		t.Fatal("insert with nonexistent source_investigation_session_id should fail FK")
	}
}
