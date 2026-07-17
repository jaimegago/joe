package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/store"
)

// fakeBackupStore is an in-memory backupStore for the routing and error-path
// tests. It records whether ExecContext ran so a test can assert the command
// refused BEFORE reaching any SQL.
type fakeBackupStore struct {
	driver   string
	execErr  error
	execRan  bool
	lastArgs []any
	lastSQL  string
}

func (f *fakeBackupStore) Driver() string {
	if f.driver == "" {
		return store.DriverSQLite
	}
	return f.driver
}

func (f *fakeBackupStore) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	f.execRan = true
	f.lastSQL = query
	f.lastArgs = args
	return nil, f.execErr
}

// depsWithBackupStore wires deps.openBackupStore to fake and returns both.
func depsWithBackupStore(fake *fakeBackupStore) runDeps {
	deps := defaultRunDeps()
	deps.openBackupStore = func() (backupStore, func() error, error) {
		return fake, func() error { return nil }, nil
	}
	return deps
}

func TestRunDBCommand_NoSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runDBCommand(context.Background(), nil, &stdout, &stderr, defaultRunDeps())
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "Usage: joe db") {
		t.Errorf("stderr missing usage, got: %s", stderr.String())
	}
}

func TestRunDBCommand_UnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runDBCommand(context.Background(), []string{"restore"}, &stdout, &stderr, defaultRunDeps())
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	out := stderr.String()
	if !strings.Contains(out, "restore") || !strings.Contains(out, "Usage: joe db") {
		t.Errorf("stderr should name the bad subcommand and print usage, got: %s", out)
	}
}

func TestRunDBBackup_MissingDest(t *testing.T) {
	fake := &fakeBackupStore{}
	var stdout, stderr bytes.Buffer
	code := runDBCommand(context.Background(), []string{"backup"}, &stdout, &stderr, depsWithBackupStore(fake))
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "<dest>") {
		t.Errorf("stderr should name the missing argument, got: %s", stderr.String())
	}
	if fake.execRan {
		t.Error("no SQL should run when <dest> is missing")
	}
}

func TestRunDBBackup_OccupiedDestRefused(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "backup.db")
	if err := os.WriteFile(dest, []byte("a pre-existing file"), 0o600); err != nil {
		t.Fatal(err)
	}

	fake := &fakeBackupStore{}
	var stdout, stderr bytes.Buffer
	code := runDBCommand(context.Background(), []string{"backup", dest}, &stdout, &stderr, depsWithBackupStore(fake))
	if code == 0 {
		t.Error("exit code = 0, want non-zero for an occupied destination")
	}
	out := stderr.String()
	if !strings.Contains(out, dest) {
		t.Errorf("error should name the occupied path, got: %s", out)
	}
	if !strings.Contains(out, "--force") {
		t.Errorf("error should point at the recovery path, got: %s", out)
	}
	if fake.execRan {
		t.Error("no SQL should run when the destination is occupied")
	}
	// The refusal must not consume the file it refused to overwrite.
	if b, err := os.ReadFile(dest); err != nil || string(b) != "a pre-existing file" {
		t.Errorf("refused destination was modified: content=%q err=%v", b, err)
	}
}

// TestRunDBBackup_ZeroByteDestRefused pins the case the engine gets wrong. SQLite
// treats a 0-byte file as a freshly created database and silently overwrites it,
// so `touch dest.db` (or a `> dest.db` redirect) is clobbered with no error. The
// up-front stat is what closes that window; this test is its guard.
func TestRunDBBackup_ZeroByteDestRefused(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "backup.db")
	if err := os.WriteFile(dest, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	fake := &fakeBackupStore{}
	var stdout, stderr bytes.Buffer
	code := runDBCommand(context.Background(), []string{"backup", dest}, &stdout, &stderr, depsWithBackupStore(fake))
	if code == 0 {
		t.Error("exit code = 0, want non-zero for a zero-byte destination")
	}
	if !strings.Contains(stderr.String(), dest) {
		t.Errorf("error should name the occupied path, got: %s", stderr.String())
	}
	if fake.execRan {
		t.Error("no SQL should run for a zero-byte destination — the engine would clobber it silently")
	}
}

func TestRunDBBackup_ForceOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "backup.db")
	if err := os.WriteFile(dest, []byte("stale backup"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The fake does not write a file, so re-create it as the "backup" the way a
	// real VACUUM INTO would, to exercise the success path's stat-back.
	fake := &fakeBackupStore{}
	deps := defaultRunDeps()
	deps.openBackupStore = func() (backupStore, func() error, error) {
		return fake, func() error { return nil }, nil
	}

	var stdout, stderr bytes.Buffer
	// --force after the positional, to pin the flag reordering the other
	// subcommands allow.
	code := runDBCommand(context.Background(), []string{"backup", dest, "--force"}, &stdout, &stderr, deps)

	if !fake.execRan {
		t.Fatal("--force should have reached the VACUUM INTO")
	}
	// The pre-existing file must be gone before the SQL runs: SQLite refuses a
	// destination it recognizes as a database, so --force can only work by
	// removing it first.
	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("--force should have removed the existing file before the copy, stat err = %v", err)
	}
	// With the fake writing nothing, the stat-back fails and the command reports
	// that honestly rather than claiming a backup exists.
	if code == 0 {
		t.Error("a destination that cannot be read back should not report success")
	}
	if !strings.Contains(fake.lastSQL, "VACUUM INTO") {
		t.Errorf("statement = %q, want a VACUUM INTO", fake.lastSQL)
	}
	// The destination must be bound, never interpolated into the statement.
	if strings.Contains(fake.lastSQL, dest) {
		t.Errorf("destination was interpolated into the SQL: %q", fake.lastSQL)
	}
	if len(fake.lastArgs) != 1 || fake.lastArgs[0] != dest {
		t.Errorf("bound args = %v, want exactly the destination %q", fake.lastArgs, dest)
	}
}

func TestRunDBBackup_MissingParentDirectory(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "no-such-dir")
	dest := filepath.Join(missing, "backup.db")

	fake := &fakeBackupStore{}
	var stdout, stderr bytes.Buffer
	code := runDBCommand(context.Background(), []string{"backup", dest}, &stdout, &stderr, depsWithBackupStore(fake))
	if code == 0 {
		t.Error("exit code = 0, want non-zero for a missing parent directory")
	}
	out := stderr.String()
	if !strings.Contains(out, missing) {
		t.Errorf("error should name the missing directory, got: %s", out)
	}
	if fake.execRan {
		t.Error("no SQL should run when the parent directory is missing")
	}
	// The command must not create the directory it named.
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("backup should not create the missing directory, stat err = %v", err)
	}
}

func TestRunDBBackup_NonSQLiteDriverRefused(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "backup.db")

	fake := &fakeBackupStore{driver: store.DriverPostgres}
	var stdout, stderr bytes.Buffer
	code := runDBCommand(context.Background(), []string{"backup", dest}, &stdout, &stderr, depsWithBackupStore(fake))
	if code == 0 {
		t.Error("exit code = 0, want non-zero for a non-SQLite driver")
	}
	out := stderr.String()
	if !strings.Contains(strings.ToLower(out), "sqlite") {
		t.Errorf("error should say backup is SQLite-only, got: %s", out)
	}
	if fake.execRan {
		t.Error("no SQL should run against a non-SQLite driver")
	}
}

func TestRunDBBackup_OpenStoreFailure(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "backup.db")

	deps := defaultRunDeps()
	deps.openBackupStore = func() (backupStore, func() error, error) {
		return nil, nil, errors.New("permission denied")
	}
	var stdout, stderr bytes.Buffer
	code := runDBCommand(context.Background(), []string{"backup", dest}, &stdout, &stderr, deps)
	if code == 0 {
		t.Error("exit code = 0, want non-zero when the database cannot be opened")
	}
	if !strings.Contains(stderr.String(), "permission denied") {
		t.Errorf("error should carry the underlying cause, got: %s", stderr.String())
	}
}

// TestRunDBBackup_LiveDatabaseWithConcurrentWriter is the load-bearing test for
// the command's central promise: that a backup taken while Joe is running is
// consistent and complete. It uses no fakes.
//
// It opens a real file-backed store through joe's own store.New (so the WAL,
// busy_timeout, and foreign_keys pragmas are exactly the daemon's), populates it,
// and holds a FIRST handle open with an uncommitted write transaction in flight —
// standing in for a live daemon mid-write. A SECOND, independent open then takes
// the backup through the real command path, and the destination is verified to be
// a standalone, readable, internally consistent database.
//
// The WAL is what makes this non-obvious: committed rows can live in the -wal
// sidecar rather than joe.db, so a naive file copy of joe.db alone could miss
// them. VACUUM INTO reads through a real transaction and therefore captures them.
func TestRunDBBackup_LiveDatabaseWithConcurrentWriter(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "joe.db")

	// First handle: the stand-in for the running daemon.
	live, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: srcPath}, nil)
	if err != nil {
		t.Fatalf("open live store: %v", err)
	}
	defer live.Close()
	if err := live.Migrate(); err != nil {
		t.Fatalf("migrate live store: %v", err)
	}

	// Populate a table with a committed, known row set. audit_log is a good
	// choice: it is append-only, so nothing can quietly remove rows underneath
	// the assertion.
	ctx := context.Background()
	const seeded = 12
	for i := range seeded {
		_, err := live.DB().ExecContext(ctx,
			`INSERT INTO audit_log (created_at, principal, action, component_id, decision, reason, kind)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			time.Now().UTC().Format(time.RFC3339Nano), "svc:test", "backup_probe",
			fmt.Sprintf("component-%d", i), "allow", "seeded by the live-backup test", "infra_access")
		if err != nil {
			t.Fatalf("seed audit_log row %d: %v", i, err)
		}
	}

	// Confirm the WAL sidecar is real: this test's premise is that committed data
	// can live outside joe.db, which is why a file copy is not a backup.
	if _, err := os.Stat(srcPath + "-wal"); err != nil {
		t.Fatalf("expected a -wal sidecar beside the database (the whole reason this command exists): %v", err)
	}

	// Hold an in-flight, uncommitted write transaction on the live handle for the
	// duration of the backup. Its row must NOT appear in the copy, and its
	// presence must not block the backup.
	tx, err := live.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin in-flight tx: %v", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO audit_log (created_at, principal, action, component_id, decision, reason, kind)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		time.Now().UTC().Format(time.RFC3339Nano), "svc:test", "backup_probe",
		"uncommitted", "allow", "must not reach the backup", "infra_access"); err != nil {
		t.Fatalf("write in-flight row: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Second, independent open — a separate connection pool, as the real command
	// has when it runs beside a live daemon.
	backupSrc, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: srcPath}, nil)
	if err != nil {
		t.Fatalf("second independent open: %v", err)
	}
	defer backupSrc.Close()

	deps := defaultRunDeps()
	deps.openBackupStore = func() (backupStore, func() error, error) {
		return storeBackupHandle{backupSrc}, func() error { return nil }, nil
	}

	dest := filepath.Join(dir, "backup.db")
	var stdout, stderr bytes.Buffer
	if code := runDBCommand(ctx, []string{"backup", dest}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("backup exit code = %d, want 0; stderr: %s", code, stderr.String())
	}

	// The live writer's transaction must still be usable: the backup must not
	// have broken it.
	if err := tx.Rollback(); err != nil {
		t.Errorf("live writer's transaction did not survive the backup: %v", err)
	}

	// The success line names the destination and reminds about the key.
	if !strings.Contains(stdout.String(), dest) {
		t.Errorf("stdout should name the destination, got: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "encryption.key") {
		t.Errorf("stdout should point at the encryption key, got: %s", stdout.String())
	}

	// The destination must stand alone: open it fresh, with no sidecars carried
	// over from the source.
	restored, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: dest}, nil)
	if err != nil {
		t.Fatalf("open the backup standalone: %v", err)
	}
	defer restored.Close()

	var got int
	if err := restored.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_log WHERE action = ?`, "backup_probe").Scan(&got); err != nil {
		t.Fatalf("read the backup: %v", err)
	}
	// Compare against what the source actually holds rather than a hardcoded
	// number, so the assertion tracks the seeding above (D-0032).
	if got != seeded {
		t.Errorf("backup holds %d seeded rows, want %d — committed data was lost", got, seeded)
	}
	// The uncommitted row must not have been captured.
	var leaked int
	if err := restored.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_log WHERE component_id = ?`, "uncommitted").Scan(&leaked); err != nil {
		t.Fatalf("check for the uncommitted row: %v", err)
	}
	if leaked != 0 {
		t.Errorf("backup captured %d uncommitted row(s); a backup must hold committed data only", leaked)
	}

	// And it must be internally consistent, not merely readable.
	var integrity string
	if err := restored.DB().QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		t.Fatalf("integrity_check: %v", err)
	}
	if integrity != "ok" {
		t.Errorf("integrity_check = %q, want \"ok\"", integrity)
	}
}
