package promotereads_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/promotereads"
	"github.com/jaimegago/joe/internal/store"
)

// freshStore opens an in-memory SQLite with the full migration chain applied.
// Migration 024 creates agent_read_promotions (unseeded — absent == OFF).
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

func mustReadEnabled(t *testing.T, s *store.Store, componentType string) (int, bool) {
	t.Helper()
	var v int
	err := s.DB().QueryRowContext(context.Background(),
		`SELECT enabled FROM agent_read_promotions WHERE component_type = ?`, componentType).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false
	}
	if err != nil {
		t.Fatalf("read enabled %q: %v", componentType, err)
	}
	return v, true
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

// TestRepository_IsPromoted_AbsentIsOff: an absent row reports OFF.
func TestRepository_IsPromoted_AbsentIsOff(t *testing.T) {
	s := freshStore(t)
	repo := promotereads.NewRepository(s.DB(), store.DriverSQLite)
	on, err := repo.IsPromoted(context.Background(), "kubernetes")
	if err != nil {
		t.Fatalf("IsPromoted: %v", err)
	}
	if on {
		t.Fatal("absent row must report OFF")
	}
}

// TestService_SetPromoted_AtomicWithAudit: the happy path writes BOTH the flag
// row and one admin_access audit row; IsPromoted then reports the new value.
func TestService_SetPromoted_AtomicWithAudit(t *testing.T) {
	s := freshStore(t)
	repo := promotereads.NewRepository(s.DB(), store.DriverSQLite)
	auditRepo := audit.NewRepository(s.DB(), store.DriverSQLite)
	svc := promotereads.NewMutationService(repo, auditRepo)
	ctx := context.Background()

	if err := svc.SetPromoted(ctx, "kubernetes", true); err != nil {
		t.Fatalf("SetPromoted: %v", err)
	}
	if v, ok := mustReadEnabled(t, s, "kubernetes"); !ok || v != 1 {
		t.Fatalf("enabled row = (%d, %v), want (1, true)", v, ok)
	}
	if got := countAuditRows(t, s); got != 1 {
		t.Fatalf("audit rows = %d, want 1", got)
	}
	on, err := repo.IsPromoted(ctx, "kubernetes")
	if err != nil || !on {
		t.Fatalf("IsPromoted after set = (%v, %v), want (true, nil)", on, err)
	}

	// Flip OFF via upsert — same row, no second flag row, second audit row.
	if err := svc.SetPromoted(ctx, "kubernetes", false); err != nil {
		t.Fatalf("SetPromoted off: %v", err)
	}
	if v, ok := mustReadEnabled(t, s, "kubernetes"); !ok || v != 0 {
		t.Fatalf("enabled row after off = (%d, %v), want (0, true)", v, ok)
	}
	if got := countAuditRows(t, s); got != 2 {
		t.Fatalf("audit rows after off = %d, want 2", got)
	}
}

// failingAudit forces InsertTx to fail so the rollback path is exercised: the
// flag write already happened inside the tx, and the audit failure must roll
// BOTH back (fail-closed — neither persists).
type failingAudit struct{}

func (failingAudit) Insert(_ context.Context, _ audit.Event) error { return nil }
func (failingAudit) InsertTx(_ context.Context, _ *sql.Tx, _ audit.Event) error {
	return errors.New("forced audit failure")
}

// TestService_SetPromoted_FailClosedOnAuditFailure: when the audit insert
// fails, the transaction rolls back and NEITHER the flag row NOR an audit row
// persists.
func TestService_SetPromoted_FailClosedOnAuditFailure(t *testing.T) {
	s := freshStore(t)
	repo := promotereads.NewRepository(s.DB(), store.DriverSQLite)
	svc := promotereads.NewMutationService(repo, failingAudit{})
	ctx := context.Background()

	err := svc.SetPromoted(ctx, "kubernetes", true)
	if err == nil {
		t.Fatal("SetPromoted should fail when the audit insert fails")
	}
	if !errors.Is(err, promotereads.ErrWriteFailed) {
		t.Fatalf("error should wrap ErrWriteFailed, got %v", err)
	}
	// Flag row must NOT exist (rolled back).
	if _, ok := mustReadEnabled(t, s, "kubernetes"); ok {
		t.Fatal("flag row must not persist after audit failure (rollback)")
	}
	if got := countAuditRows(t, s); got != 0 {
		t.Fatalf("audit rows = %d, want 0 (rollback)", got)
	}
}

// TestRepository_ComponentType resolves a component's type and returns "" for a
// missing id (the engine's fail-closed signal).
func TestRepository_ComponentType(t *testing.T) {
	s := freshStore(t)
	if _, err := s.DB().ExecContext(context.Background(),
		`INSERT INTO components (id, type, name, config, status, created_at, updated_at)
		 VALUES ('c1', 'kubernetes', 'k8s', '{}', 'active', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed component: %v", err)
	}
	repo := promotereads.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	got, err := repo.ComponentType(ctx, "c1")
	if err != nil || got != "kubernetes" {
		t.Fatalf("ComponentType(c1) = (%q, %v), want (kubernetes, nil)", got, err)
	}
	missing, err := repo.ComponentType(ctx, "nope")
	if err != nil || missing != "" {
		t.Fatalf("ComponentType(nope) = (%q, %v), want (\"\", nil)", missing, err)
	}
}
