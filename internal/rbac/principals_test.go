package rbac_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
)

// migratedStore builds a fully-migrated in-memory store so the principals and
// audit_log tables (with the append-only triggers) exist — openTestDB's manual
// schema in policy_test.go predates both. Returned to rbac_test callers that
// exercise the identity registry and the transactional audit path.
func migratedStore(t *testing.T) *store.Store {
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

// failAudit is an audit.Repository whose every write fails — used to assert the
// status change rolls back when its in-transaction audit row cannot be written.
type failAudit struct{}

func (failAudit) Insert(context.Context, audit.Event) error { return audit.ErrAuditWriteFailed }
func (failAudit) InsertTx(context.Context, *sql.Tx, audit.Event) error {
	return audit.ErrAuditWriteFailed
}

func TestPrincipals_UpsertGetList(t *testing.T) {
	s := migratedStore(t)
	repo := rbac.NewRepository(s.DB(), s.Driver())
	ctx := context.Background()

	if err := repo.UpsertPrincipal(ctx, rbac.PrincipalRecord{Principal: "user:alice@example.com"}); err != nil {
		t.Fatalf("UpsertPrincipal: %v", err)
	}

	got, err := repo.GetPrincipal(ctx, "user:alice@example.com")
	if err != nil {
		t.Fatalf("GetPrincipal: %v", err)
	}
	if got == nil {
		t.Fatal("GetPrincipal returned nil for an upserted principal")
	}
	if got.Status != rbac.PrincipalStatusActive {
		t.Errorf("status = %q, want %q (default)", got.Status, rbac.PrincipalStatusActive)
	}
	if got.CreatedAt.IsZero() {
		t.Error("created_at must be stamped on upsert")
	}

	// Upsert again refreshes display_name without disturbing status/created_at.
	if err := repo.UpsertPrincipal(ctx, rbac.PrincipalRecord{
		Principal:   "user:alice@example.com",
		DisplayName: "Alice",
	}); err != nil {
		t.Fatalf("UpsertPrincipal (refresh): %v", err)
	}
	got2, _ := repo.GetPrincipal(ctx, "user:alice@example.com")
	if got2.DisplayName != "Alice" {
		t.Errorf("display_name = %q, want Alice after refresh", got2.DisplayName)
	}
	if !got2.CreatedAt.Equal(got.CreatedAt) {
		t.Errorf("created_at changed on refresh: %v -> %v", got.CreatedAt, got2.CreatedAt)
	}

	// Unknown principal → (nil, nil).
	missing, err := repo.GetPrincipal(ctx, "user:nobody@example.com")
	if err != nil {
		t.Fatalf("GetPrincipal(missing): %v", err)
	}
	if missing != nil {
		t.Errorf("expected nil for unknown principal, got %+v", missing)
	}

	if err := repo.UpsertPrincipal(ctx, rbac.PrincipalRecord{Principal: "svc:ci"}); err != nil {
		t.Fatalf("UpsertPrincipal svc: %v", err)
	}
	list, err := repo.ListPrincipals(ctx)
	if err != nil {
		t.Fatalf("ListPrincipals: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListPrincipals returned %d, want 2", len(list))
	}
}

func TestPrincipals_SetStatus_DisableEnable_WritesAuditInTx(t *testing.T) {
	s := migratedStore(t)
	repo := rbac.NewRepositoryWithAudit(s.DB(), s.Driver(), audit.NewRepository(s.DB(), s.Driver()))
	ctx := context.Background()

	if err := repo.UpsertPrincipal(ctx, rbac.PrincipalRecord{Principal: "user:bob@example.com"}); err != nil {
		t.Fatalf("UpsertPrincipal: %v", err)
	}

	// Disable.
	n, err := repo.SetPrincipalStatus(ctx, "user:bob@example.com", rbac.PrincipalStatusDisabled, "user:admin@example.com")
	if err != nil {
		t.Fatalf("SetPrincipalStatus disable: %v", err)
	}
	if n != 1 {
		t.Fatalf("disable changed %d rows, want 1", n)
	}
	got, _ := repo.GetPrincipal(ctx, "user:bob@example.com")
	if got.Status != rbac.PrincipalStatusDisabled {
		t.Errorf("status = %q, want disabled", got.Status)
	}
	if got.DisabledAt == nil {
		t.Error("disabled_at must be set on disable")
	}
	if got.DisabledBy != "user:admin@example.com" {
		t.Errorf("disabled_by = %q, want the acting principal", got.DisabledBy)
	}
	assertAuditRow(t, s.DB(), audit.ActionAdminPrincipalDisable, "user:admin@example.com")

	// Enable clears the disable provenance.
	if _, err := repo.SetPrincipalStatus(ctx, "user:bob@example.com", rbac.PrincipalStatusActive, "user:admin@example.com"); err != nil {
		t.Fatalf("SetPrincipalStatus enable: %v", err)
	}
	got, _ = repo.GetPrincipal(ctx, "user:bob@example.com")
	if got.Status != rbac.PrincipalStatusActive {
		t.Errorf("status = %q, want active", got.Status)
	}
	if got.DisabledAt != nil || got.DisabledBy != "" {
		t.Errorf("disable provenance not cleared on enable: at=%v by=%q", got.DisabledAt, got.DisabledBy)
	}
	assertAuditRow(t, s.DB(), audit.ActionAdminPrincipalEnable, "user:admin@example.com")
}

func TestPrincipals_SetStatus_AuditFailureRollsBack(t *testing.T) {
	s := migratedStore(t)
	repo := rbac.NewRepositoryWithAudit(s.DB(), s.Driver(), failAudit{})
	ctx := context.Background()

	if err := repo.UpsertPrincipal(ctx, rbac.PrincipalRecord{Principal: "user:carol@example.com"}); err != nil {
		t.Fatalf("UpsertPrincipal: %v", err)
	}

	_, err := repo.SetPrincipalStatus(ctx, "user:carol@example.com", rbac.PrincipalStatusDisabled, "user:admin@example.com")
	if err == nil {
		t.Fatal("SetPrincipalStatus must fail when its audit write fails")
	}
	if !errors.Is(err, audit.ErrAuditWriteFailed) {
		t.Errorf("error = %v, want it to wrap ErrAuditWriteFailed", err)
	}

	// The status change must have rolled back with the audit row.
	got, _ := repo.GetPrincipal(ctx, "user:carol@example.com")
	if got.Status != rbac.PrincipalStatusActive {
		t.Errorf("status = %q after failed audit; the change must NOT commit without its audit row", got.Status)
	}
}

// assertAuditRow asserts exactly one audit_log row exists for the action with
// the given acting principal — verifying single-write and the threaded actor.
func assertAuditRow(t *testing.T, db *sql.DB, action, wantPrincipal string) {
	t.Helper()
	var count int
	var principal string
	if err := db.QueryRow(
		`SELECT COUNT(*), COALESCE(MAX(principal),'') FROM audit_log WHERE action = ?`, action).
		Scan(&count, &principal); err != nil {
		t.Fatalf("query audit row for %q: %v", action, err)
	}
	if count != 1 {
		t.Errorf("audit rows for %q = %d, want exactly 1", action, count)
	}
	if principal != wantPrincipal {
		t.Errorf("audit %q principal = %q, want %q", action, principal, wantPrincipal)
	}
}
