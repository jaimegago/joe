package readposture_test

import (
	"context"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/readposture"
	"github.com/jaimegago/joe/internal/store"
)

// freshStore opens an in-memory SQLite with the full migration chain applied.
// Migration 028 creates read_posture seeded with the singleton 'team_flat' row.
func freshStore(t *testing.T) *store.Store {
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

func countAuditRows(t *testing.T, s *store.Store) int {
	t.Helper()
	var n int
	if err := s.DB().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM audit_log`).Scan(&n); err != nil {
		t.Fatalf("count audit_log: %v", err)
	}
	return n
}

// TestDefaultPosture_IsTeamFlat proves the default posture on an install with no
// posture explicitly set is team_flat — the migration-028 seed. This is the
// migration property: a fresh install (and an install upgraded from a
// pre-posture build, which runs migration 028 on upgrade) inherits team_flat.
func TestDefaultPosture_IsTeamFlat(t *testing.T) {
	s := freshStore(t)
	repo := readposture.NewRepository(s.DB(), store.DriverSQLite)
	got, err := repo.ReadPosture(context.Background())
	if err != nil {
		t.Fatalf("ReadPosture: %v", err)
	}
	if got != readposture.PostureTeamFlat {
		t.Fatalf("default posture = %q, want %q (the launch default on an unset install)", got, readposture.PostureTeamFlat)
	}
}

// TestSetPosture_AtomicWithAudit: a flip to zoned persists the new posture AND
// writes exactly one admin_access audit row carrying the read_posture.set action;
// ReadPosture then reports the new value (live).
func TestSetPosture_AtomicWithAudit(t *testing.T) {
	s := freshStore(t)
	repo := readposture.NewRepository(s.DB(), store.DriverSQLite)
	auditRepo := audit.NewRepository(s.DB(), store.DriverSQLite)
	svc := readposture.NewMutationService(repo, auditRepo)
	ctx := rbac.WithPrincipal(context.Background(), "user:admin@example.com")

	before := countAuditRows(t, s)
	if err := svc.SetPosture(ctx, readposture.PostureZoned); err != nil {
		t.Fatalf("SetPosture: %v", err)
	}
	if after := countAuditRows(t, s); after != before+1 {
		t.Fatalf("expected exactly one audit row written, got %d new", after-before)
	}

	got, err := repo.ReadPosture(ctx)
	if err != nil {
		t.Fatalf("ReadPosture: %v", err)
	}
	if got != readposture.PostureZoned {
		t.Fatalf("after SetPosture(zoned), ReadPosture = %q, want %q", got, readposture.PostureZoned)
	}

	// The row carries the read_posture.set action under the admin_access kind.
	var action, kind string
	if err := s.DB().QueryRowContext(ctx,
		`SELECT action, kind FROM audit_log ORDER BY id DESC LIMIT 1`).Scan(&action, &kind); err != nil {
		t.Fatalf("read latest audit row: %v", err)
	}
	if action != audit.ActionAdminReadPostureSet {
		t.Errorf("audit action = %q, want %q", action, audit.ActionAdminReadPostureSet)
	}
	if kind != string(audit.KindAdminAccess) {
		t.Errorf("audit kind = %q, want %q", kind, audit.KindAdminAccess)
	}
}

// TestSetPosture_RoundTrip proves a flip back to team_flat is also live and
// audited — the latch moves both ways.
func TestSetPosture_RoundTrip(t *testing.T) {
	s := freshStore(t)
	repo := readposture.NewRepository(s.DB(), store.DriverSQLite)
	auditRepo := audit.NewRepository(s.DB(), store.DriverSQLite)
	svc := readposture.NewMutationService(repo, auditRepo)
	ctx := rbac.WithPrincipal(context.Background(), "user:admin@example.com")

	if err := svc.SetPosture(ctx, readposture.PostureZoned); err != nil {
		t.Fatalf("SetPosture(zoned): %v", err)
	}
	if err := svc.SetPosture(ctx, readposture.PostureTeamFlat); err != nil {
		t.Fatalf("SetPosture(team_flat): %v", err)
	}
	got, err := repo.ReadPosture(ctx)
	if err != nil {
		t.Fatalf("ReadPosture: %v", err)
	}
	if got != readposture.PostureTeamFlat {
		t.Fatalf("after round-trip, ReadPosture = %q, want %q", got, readposture.PostureTeamFlat)
	}
}

// TestPostureConstants_InSyncWithRBAC asserts the readposture posture strings are
// byte-identical to the literals the rbac engine compares against. The engine
// keeps its own copies to avoid an rbac→readposture import cycle; this guard
// fails the build if the two ever drift (the same convention audit/rbac use for
// their shared string vocabulary).
func TestPostureConstants_InSyncWithRBAC(t *testing.T) {
	if readposture.PostureTeamFlat != rbac.PostureTeamFlat {
		t.Errorf("team_flat literal drift: readposture=%q rbac=%q", readposture.PostureTeamFlat, rbac.PostureTeamFlat)
	}
	if readposture.PostureZoned != rbac.PostureZoned {
		t.Errorf("zoned literal drift: readposture=%q rbac=%q", readposture.PostureZoned, rbac.PostureZoned)
	}
}

// TestIsValidPosture guards the HTTP-boundary validation predicate.
func TestIsValidPosture(t *testing.T) {
	for _, ok := range []string{readposture.PostureTeamFlat, readposture.PostureZoned} {
		if !readposture.IsValidPosture(ok) {
			t.Errorf("IsValidPosture(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "flat", "team-flat", "ZONED", "open"} {
		if readposture.IsValidPosture(bad) {
			t.Errorf("IsValidPosture(%q) = true, want false", bad)
		}
	}
}
