package audit_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/store"
)

// freshStore opens an in-memory SQLite with the full migration chain
// applied. Used by both the SQL Insert test and the migration trigger test.
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

// TestSQLRepository_Insert_RoundTrip writes one row and reads it back via
// raw SELECT (audit.Repository deliberately exposes no list method).
func TestSQLRepository_Insert_RoundTrip(t *testing.T) {
	s := freshStore(t)
	repo := audit.NewRepository(s.DB(), store.DriverSQLite)

	if err := repo.Insert(context.Background(), audit.Event{
		Principal:   "user:alice",
		Action:      "read",
		Zone:        "prod-readonly",
		ComponentID: "k8s-prod",
		Decision:    audit.DecisionAllow,
		Reason:      "policy_allow",
		Kind:        audit.KindInfraAccess,
		Context:     `{"note":"k8s list"}`,
	}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	var (
		principal, action, zone, component, decision, reason, kind, ctxJSON sql.NullString
	)
	err := s.DB().QueryRowContext(context.Background(),
		`SELECT principal, action, zone, component_id, decision, reason, kind, context FROM audit_log LIMIT 1`).
		Scan(&principal, &action, &zone, &component, &decision, &reason, &kind, &ctxJSON)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if principal.String != "user:alice" {
		t.Errorf("principal = %q, want %q", principal.String, "user:alice")
	}
	if action.String != "read" || zone.String != "prod-readonly" || component.String != "k8s-prod" {
		t.Errorf("action/zone/component = %q/%q/%q", action.String, zone.String, component.String)
	}
	if decision.String != "allow" || reason.String != "policy_allow" || kind.String != "infra_access" {
		t.Errorf("decision/reason/kind = %q/%q/%q", decision.String, reason.String, kind.String)
	}
	if !strings.Contains(ctxJSON.String, "k8s list") {
		t.Errorf("context = %q, want substring %q", ctxJSON.String, "k8s list")
	}
}

// TestSQLRepository_Insert_NullableColumns verifies that empty
// principal/zone/component values are stored as SQL NULL (sourceless rows
// such as captain transitions take this path).
func TestSQLRepository_Insert_NullableColumns(t *testing.T) {
	s := freshStore(t)
	repo := audit.NewRepository(s.DB(), store.DriverSQLite)

	if err := repo.Insert(context.Background(), audit.Event{
		Principal: "user:alice",
		Action:    audit.ActionCaptainAttach,
		Decision:  audit.DecisionAllow,
		Reason:    "transition_recorded",
		Kind:      audit.KindCaptainTransition,
		// Zone and Source intentionally empty.
	}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	var zone, component sql.NullString
	err := s.DB().QueryRowContext(context.Background(),
		`SELECT zone, component_id FROM audit_log WHERE action = ? LIMIT 1`, audit.ActionCaptainAttach).
		Scan(&zone, &component)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if zone.Valid {
		t.Errorf("zone = %q, want NULL for transition row", zone.String)
	}
	if component.Valid {
		t.Errorf("component = %q, want NULL for transition row", component.String)
	}
}

// TestSQLRepository_InsertTx_RollbackLeavesNoRow proves InsertTx honors
// the CALLER'S transaction rather than opening its own. The caller
// begins a transaction, writes one row through InsertTx, then rolls
// back; the audit_log must be empty afterwards. If the implementation
// silently fell back to the database handle (or committed internally),
// the row would survive the rollback.
//
// Stream G phase G4: the load-bearing test for the settings service's
// atomic-mutation guarantee. A settings change AND its audit row must
// commit together or roll back together; this test exercises the
// roll-back-together half against the audit half alone.
func TestSQLRepository_InsertTx_RollbackLeavesNoRow(t *testing.T) {
	s := freshStore(t)
	repo := audit.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	concrete, ok := repo.(interface {
		InsertTx(ctx context.Context, tx *sql.Tx, e audit.Event) error
	})
	if !ok {
		t.Fatalf("audit.Repository must expose InsertTx (Stream G phase G4)")
	}

	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := concrete.InsertTx(ctx, tx, audit.Event{
		Principal: "user:operator",
		Action:    audit.ActionLLMSetActiveModel,
		Decision:  audit.DecisionAllow,
		Reason:    "settings_change",
		Kind:      audit.KindLLMSettingsMutation,
		Context:   `{"target":"active_model","before":"","after":"claude-opus-4-7"}`,
	}); err != nil {
		t.Fatalf("InsertTx: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	var count int
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_log`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("audit_log count after rolled-back InsertTx = %d, want 0; InsertTx must honor the caller's transaction", count)
	}
}

// TestSQLRepository_InsertTx_NilTx returns an error rather than silently
// falling back to the database handle. The nil-fallback would defeat the
// point of InsertTx (no shared transaction, no atomicity), so the
// implementation refuses the call.
func TestSQLRepository_InsertTx_NilTx(t *testing.T) {
	s := freshStore(t)
	repo := audit.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	concrete, ok := repo.(interface {
		InsertTx(ctx context.Context, tx *sql.Tx, e audit.Event) error
	})
	if !ok {
		t.Fatalf("audit.Repository must expose InsertTx (Stream G phase G4)")
	}

	err := concrete.InsertTx(ctx, nil, audit.Event{
		Principal: "user:operator",
		Action:    audit.ActionLLMSetActiveModel,
		Decision:  audit.DecisionAllow,
		Reason:    "settings_change",
		Kind:      audit.KindLLMSettingsMutation,
	})
	if err == nil {
		t.Fatalf("InsertTx(nil tx) returned nil; expected ErrAuditWriteFailed")
	}
}

// TestSQLRepository_InsertTx_AndInsert_ProduceIdenticalRows proves the
// shared SQL body in sql.go: the two paths run through the same
// insertOn helper, so for the same Event input they MUST produce
// byte-identical rows in audit_log. The test inserts the same Event
// twice — once via Insert (database handle), once via InsertTx + commit
// — then compares every column.
//
// This is the regression guard against the two insert paths drifting
// apart in a future edit (a copy-paste in InsertTx that adds a different
// default, a different empty-to-null mapping, or a different SQL
// statement would be caught here).
func TestSQLRepository_InsertTx_AndInsert_ProduceIdenticalRows(t *testing.T) {
	s := freshStore(t)
	repo := audit.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	concrete, ok := repo.(interface {
		InsertTx(ctx context.Context, tx *sql.Tx, e audit.Event) error
	})
	if !ok {
		t.Fatalf("audit.Repository must expose InsertTx (Stream G phase G4)")
	}

	// Use a fixed timestamp so we don't depend on time.Now between the
	// two inserts.
	ts := time.Date(2026, 6, 1, 12, 34, 56, 789, time.UTC)
	ev := audit.Event{
		Timestamp: ts,
		Principal: "user:operator",
		Action:    audit.ActionLLMSetCostLimit,
		Decision:  audit.DecisionAllow,
		Reason:    "settings_change",
		Kind:      audit.KindLLMSettingsMutation,
		Context:   `{"target":"cost_limit:hourly","before":0,"after":100000000000}`,
		// Leave Zone and Source empty to exercise the empty-to-null map
		// through both paths.
	}

	// Path A: non-transactional Insert.
	if err := repo.Insert(ctx, ev); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	// Path B: InsertTx + commit.
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := concrete.InsertTx(ctx, tx, ev); err != nil {
		t.Fatalf("InsertTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Both rows present. Pull them in deterministic id order and
	// compare every column.
	rows, err := s.DB().QueryContext(ctx, `
		SELECT created_at, principal, action, zone, component_id, decision, reason, kind, context
		  FROM audit_log
		 ORDER BY id ASC`)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	defer rows.Close()

	type rowSnap struct {
		createdAt, action, decision, reason, kind, ctxJSON string
		principal, zone, component                         sql.NullString
	}
	var got []rowSnap
	for rows.Next() {
		var rs rowSnap
		if err := rows.Scan(&rs.createdAt, &rs.principal, &rs.action, &rs.zone, &rs.component, &rs.decision, &rs.reason, &rs.kind, &rs.ctxJSON); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, rs)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("audit_log row count = %d, want 2", len(got))
	}
	a, b := got[0], got[1]
	if a.createdAt != b.createdAt ||
		a.principal != b.principal ||
		a.action != b.action ||
		a.zone != b.zone ||
		a.component != b.component ||
		a.decision != b.decision ||
		a.reason != b.reason ||
		a.kind != b.kind ||
		a.ctxJSON != b.ctxJSON {
		t.Fatalf("Insert and InsertTx produced DIFFERENT rows for the same event:\n  Insert: %+v\nInsertTx: %+v\nThe two paths must share one SQL body so the row shape cannot diverge.", a, b)
	}
}

// TestMigration015_TriggerBlocksUpdate is the database-level half of the
// append-only enforcement (Phase F req 2b, design §2.6). The trigger
// must RAISE/ABORT on any UPDATE against audit_log.
func TestMigration015_TriggerBlocksUpdate(t *testing.T) {
	s := freshStore(t)
	repo := audit.NewRepository(s.DB(), store.DriverSQLite)

	if err := repo.Insert(context.Background(), audit.Event{
		Principal:   "user:alice",
		Action:      "read",
		Decision:    audit.DecisionAllow,
		Reason:      "policy_allow",
		Kind:        audit.KindInfraAccess,
		ComponentID: "k8s-prod",
	}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	_, err := s.DB().ExecContext(context.Background(),
		`UPDATE audit_log SET reason = 'tampered' WHERE id = 1`)
	if err == nil {
		t.Fatal("UPDATE on audit_log returned nil; the trigger must abort UPDATEs")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "append-only") &&
		!strings.Contains(strings.ToLower(err.Error()), "update is not permitted") {
		t.Errorf("UPDATE error = %v; expected an append-only abort message", err)
	}
}

// TestMigration015_TriggerBlocksDelete — DELETE counterpart of the
// trigger test.
func TestMigration015_TriggerBlocksDelete(t *testing.T) {
	s := freshStore(t)
	repo := audit.NewRepository(s.DB(), store.DriverSQLite)

	if err := repo.Insert(context.Background(), audit.Event{
		Principal:   "user:alice",
		Action:      "read",
		Decision:    audit.DecisionAllow,
		Reason:      "policy_allow",
		Kind:        audit.KindInfraAccess,
		ComponentID: "k8s-prod",
	}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	_, err := s.DB().ExecContext(context.Background(),
		`DELETE FROM audit_log WHERE id = 1`)
	if err == nil {
		t.Fatal("DELETE on audit_log returned nil; the trigger must abort DELETEs")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "append-only") &&
		!strings.Contains(strings.ToLower(err.Error()), "delete is not permitted") {
		t.Errorf("DELETE error = %v; expected an append-only abort message", err)
	}

	// And confirm the row is still there.
	var count int
	if err := s.DB().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM audit_log`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("audit_log count after blocked DELETE = %d, want 1", count)
	}
}
