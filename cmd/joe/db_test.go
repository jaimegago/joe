package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
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
	// "vacuum" is deliberately not a real db subcommand; this test used to pass
	// "restore", which now is one.
	code := runDBCommand(context.Background(), []string{"vacuum"}, &stdout, &stderr, defaultRunDeps())
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	out := stderr.String()
	if !strings.Contains(out, "vacuum") || !strings.Contains(out, "Usage: joe db") {
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

// ---------------------------------------------------------------- restore

// restoreHolder is a helper process stand-in: it opens target and holds it, so a
// test can exercise the running-daemon refusal against a real lock.

// seedDatabase creates a joe-shaped database at path via the real store (so it
// carries joe's own pragmas) with a components table and marker rows.
func seedDatabase(t *testing.T, path string, marker string, encryptedConfig bool) {
	t.Helper()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: path}, nil)
	if err != nil {
		t.Fatalf("seed open %s: %v", path, err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("seed migrate %s: %v", path, err)
	}
	cfg := `{"api_server":"https://example"}`
	if encryptedConfig {
		// Mirror the at-rest shape: a JSON-encoded string carrying the enc: marker.
		cfg = `"enc:` + base64.StdEncoding.EncodeToString([]byte("ciphertext")) + `"`
	} else {
		cfg = `{"api_server":"https://example"}`
	}
	_, err = s.DB().Exec(
		`INSERT INTO components (id, type, name, config) VALUES (?, ?, ?, ?)`,
		marker+"-component", "kubernetes", marker, cfg)
	if err != nil {
		t.Fatalf("seed component: %v", err)
	}
	if _, err := s.DB().Exec(`CREATE TABLE ` + marker + `_marker (x int)`); err != nil {
		t.Fatalf("seed marker table: %v", err)
	}
}

// makeBackup produces a real `joe db backup`-shaped file (a VACUUM INTO output)
// from the database at src.
func makeBackup(t *testing.T, src, dest string) {
	t.Helper()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: src}, nil)
	if err != nil {
		t.Fatalf("backup open: %v", err)
	}
	defer s.Close()
	if _, err := s.DB().Exec("VACUUM INTO ?", dest); err != nil {
		t.Fatalf("VACUUM INTO: %v", err)
	}
}

func restoreDeps(t *testing.T, target string) runDeps {
	t.Helper()
	deps := defaultRunDeps()
	deps.resolveDatabaseConfig = func() (store.DatabaseConfig, error) {
		return store.DatabaseConfig{Driver: store.DriverSQLite, DSN: target}, nil
	}
	// A key that exists, so the missing-key gate is not what a test trips over
	// unless it means to.
	keyPath := filepath.Join(t.TempDir(), "encryption.key")
	if err := os.WriteFile(keyPath, make([]byte, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	deps.encryptionKeyPath = func() (string, error) { return keyPath, nil }
	return deps
}

func tableNames(t *testing.T, path string) string {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer db.Close()
	rows, err := db.Query("select name from sqlite_master where type='table' order by name")
	if err != nil {
		t.Fatalf("query %s: %v", path, err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		names = append(names, n)
	}
	return strings.Join(names, ",")
}

// TestRunDBRestore_UncleanStopTargetRestoresToBackupNotPriorDatabase pins the
// outcome an operator cares about: restoring over a target left in an
// unclean-stop state — main file plus live, uncheckpointed sidecars holding the
// previous database's committed data — yields the BACKUP, not the previous
// database.
//
// It is a guarantee pin, not a break-test for the sidecar deletion. Measured
// behaviour: with the copy done by VACUUM INTO the guarantee already holds
// without deleting the sidecars, because the engine treats the absent
// destination as a new database and resets the WAL instead of recovering it.
// The test earns its place by pinning the outcome against the copy mechanism
// changing: laid down by byte copy instead, the stale -wal replays and this
// assertion fails, which is exactly the manual-procedure hazard the Operations
// page now warns about.
func TestRunDBRestore_UncleanStopTargetRestoresToBackupNotPriorDatabase(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "joe.db")

	// Database A, with its data live in the -wal.
	liveA, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: target}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := liveA.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := liveA.DB().Exec(`CREATE TABLE only_in_a (x int)`); err != nil {
		t.Fatal(err)
	}
	if _, err := liveA.DB().Exec(
		`INSERT INTO components (id, type, name, config) VALUES (?,?,?,?)`,
		"a-component", "kubernetes", "from-A", `{"a":true}`); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target + "-wal"); err != nil {
		t.Fatalf("premise broken: expected a -wal beside the target: %v", err)
	}

	// Snapshot the unclean-stop state: main file plus live sidecars.
	stash := filepath.Join(dir, "stash")
	if err := os.MkdirAll(stash, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, suf := range []string{"", "-wal", "-shm"} {
		b, err := os.ReadFile(target + suf)
		if err != nil {
			t.Fatalf("snapshot %s: %v", suf, err)
		}
		if err := os.WriteFile(filepath.Join(stash, "joe.db"+suf), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	liveA.Close()

	// Backup B: a different database, VACUUM INTO-shaped like a real backup.
	bDir := t.TempDir()
	bSrc := filepath.Join(bDir, "b-source.db")
	seedDatabase(t, bSrc, "fromb", false)
	backup := filepath.Join(bDir, "backup.db")
	makeBackup(t, bSrc, backup)

	// Restore the stale-sidecar state onto the target.
	for _, suf := range []string{"", "-wal", "-shm"} {
		b, err := os.ReadFile(filepath.Join(stash, "joe.db"+suf))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target+suf, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	deps := restoreDeps(t, target)
	var stdout, stderr bytes.Buffer
	code := runDBCommand(context.Background(), []string{"restore", backup, "--force"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("restore exit = %d, want 0; stderr: %s", code, stderr.String())
	}

	// The restored database must be B, with no trace of A.
	names := tableNames(t, target)
	if !strings.Contains(names, "fromb_marker") {
		t.Errorf("restored target is missing B's marker table; tables=[%s]", names)
	}
	if strings.Contains(names, "only_in_a") {
		t.Errorf("A's schema survived the restore — a stale -wal replayed over it; tables=[%s]", names)
	}

	db, err := sql.Open("sqlite", "file:"+target+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var fromA int
	if err := db.QueryRow(`select count(*) from components where name='from-A'`).Scan(&fromA); err != nil {
		t.Fatalf("query restored target: %v", err)
	}
	if fromA != 0 {
		t.Errorf("A's component rows survived the restore (%d of them) — the previous database was resurrected", fromA)
	}
	var fromB int
	if err := db.QueryRow(`select count(*) from components where name='fromb'`).Scan(&fromB); err != nil {
		t.Fatalf("query restored target: %v", err)
	}
	if fromB != 1 {
		t.Errorf("B's component row is absent from the restored target (got %d)", fromB)
	}
	var integrity string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil {
		t.Fatal(err)
	}
	if integrity != "ok" {
		t.Errorf("integrity_check = %q, want ok", integrity)
	}
}

// TestRunDBRestore_StaleSidecarsRemoved pins the sidecar deletion directly.
//
// VACUUM INTO clears a stale -wal on its way past but leaves a stale -shm sitting
// beside the restored database, so this fails against an implementation that does
// not delete the sidecars explicitly. The deletion is defence in depth — see the
// comment on the deletion in db.go — and this is what holds it in place.
func TestRunDBRestore_StaleSidecarsRemoved(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "joe.db")

	live, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: target}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := live.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := live.DB().Exec(
		`INSERT INTO components (id, type, name, config) VALUES (?,?,?,?)`,
		"a", "kubernetes", "from-A", `{"a":true}`); err != nil {
		t.Fatal(err)
	}
	stash := map[string][]byte{}
	for _, suf := range []string{"", "-wal", "-shm"} {
		b, err := os.ReadFile(target + suf)
		if err != nil {
			t.Fatalf("premise broken: expected %s beside the target: %v", suf, err)
		}
		stash[suf] = b
	}
	live.Close()
	for suf, b := range stash {
		if err := os.WriteFile(target+suf, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	bDir := t.TempDir()
	bSrc := filepath.Join(bDir, "b.db")
	seedDatabase(t, bSrc, "fromb", false)
	backup := filepath.Join(bDir, "backup.db")
	makeBackup(t, bSrc, backup)

	deps := restoreDeps(t, target)
	var stdout, stderr bytes.Buffer
	if code := runDBCommand(context.Background(), []string{"restore", backup, "--force"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("restore exit = %d, want 0; stderr: %s", code, stderr.String())
	}

	for _, suf := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(target + suf); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("stale %s survived the restore (stat err = %v); restore must clear the target's sidecars", suf, err)
		}
	}
}

func TestRunDBRestore_MissingSrc(t *testing.T) {
	dir := t.TempDir()
	deps := restoreDeps(t, filepath.Join(dir, "joe.db"))
	var stdout, stderr bytes.Buffer
	code := runDBCommand(context.Background(), []string{"restore", filepath.Join(dir, "nope.db")}, &stdout, &stderr, deps)
	if code == 0 {
		t.Error("exit = 0, want non-zero for a missing SRC")
	}
	if !strings.Contains(stderr.String(), "does not exist") {
		t.Errorf("error should say SRC does not exist, got: %s", stderr.String())
	}
}

func TestRunDBRestore_NoSrcArgument(t *testing.T) {
	deps := restoreDeps(t, filepath.Join(t.TempDir(), "joe.db"))
	var stdout, stderr bytes.Buffer
	code := runDBCommand(context.Background(), []string{"restore"}, &stdout, &stderr, deps)
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "<src>") {
		t.Errorf("error should name the missing argument, got: %s", stderr.String())
	}
}

func TestRunDBRestore_NotAJoeDatabase(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "other.db")
	db, err := sql.Open("sqlite", src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("create table unrelated(x int)"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	deps := restoreDeps(t, filepath.Join(dir, "joe.db"))
	var stdout, stderr bytes.Buffer
	code := runDBCommand(context.Background(), []string{"restore", src}, &stdout, &stderr, deps)
	if code == 0 {
		t.Error("exit = 0, want non-zero for a non-Joe database")
	}
	if !strings.Contains(stderr.String(), "components table") {
		t.Errorf("error should name what was checked, got: %s", stderr.String())
	}
}

// TestRunDBRestore_MissingKeyGate exercises both sides of the --allow-missing-key
// gate against a SRC whose component config is encrypted.
func TestRunDBRestore_MissingKeyGate(t *testing.T) {
	newCase := func(t *testing.T) (runDeps, string, string) {
		t.Helper()
		dir := t.TempDir()
		src := filepath.Join(dir, "b.db")
		seedDatabase(t, src, "fromb", true) // encrypted config
		backup := filepath.Join(dir, "backup.db")
		makeBackup(t, src, backup)
		target := filepath.Join(dir, "joe.db")
		deps := restoreDeps(t, target)
		deps.encryptionKeyPath = func() (string, error) {
			return filepath.Join(dir, "absent", "encryption.key"), nil
		}
		return deps, backup, target
	}

	t.Run("refused without the flag", func(t *testing.T) {
		deps, backup, target := newCase(t)
		var stdout, stderr bytes.Buffer
		code := runDBCommand(context.Background(), []string{"restore", backup}, &stdout, &stderr, deps)
		if code == 0 {
			t.Error("exit = 0, want non-zero when encrypted configs meet a missing key")
		}
		out := stderr.String()
		if !strings.Contains(out, "encryption.key") {
			t.Errorf("error should name the key path, got: %s", out)
		}
		if !strings.Contains(out, "--allow-missing-key") {
			t.Errorf("error should name the override, got: %s", out)
		}
		if !strings.Contains(out, "reaches none of its components") {
			t.Errorf("error should state the consequence, got: %s", out)
		}
		if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
			t.Error("a refused restore must not have written the target")
		}
	})

	t.Run("proceeds with the flag", func(t *testing.T) {
		deps, backup, target := newCase(t)
		var stdout, stderr bytes.Buffer
		code := runDBCommand(context.Background(), []string{"restore", backup, "--allow-missing-key"}, &stdout, &stderr, deps)
		if code != 0 {
			t.Fatalf("exit = %d, want 0 with --allow-missing-key; stderr: %s", code, stderr.String())
		}
		if !strings.Contains(stdout.String(), "Warning") {
			t.Errorf("proceeding without a key should warn, got: %s", stdout.String())
		}
		if _, err := os.Stat(target); err != nil {
			t.Errorf("target should have been written: %v", err)
		}
	})

	t.Run("force does not imply allow-missing-key", func(t *testing.T) {
		deps, backup, _ := newCase(t)
		var stdout, stderr bytes.Buffer
		code := runDBCommand(context.Background(), []string{"restore", backup, "--force"}, &stdout, &stderr, deps)
		if code == 0 {
			t.Error("--force must not stand in for --allow-missing-key")
		}
	})
}

func TestRunDBRestore_OccupiedTargetWithoutForce(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "joe.db")
	seedDatabase(t, target, "existing", false)

	src := filepath.Join(dir, "b.db")
	seedDatabase(t, src, "fromb", false)
	backup := filepath.Join(dir, "backup.db")
	makeBackup(t, src, backup)

	before, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}

	deps := restoreDeps(t, target)
	var stdout, stderr bytes.Buffer
	code := runDBCommand(context.Background(), []string{"restore", backup}, &stdout, &stderr, deps)
	if code == 0 {
		t.Error("exit = 0, want non-zero for an existing database without --force")
	}
	out := stderr.String()
	if !strings.Contains(out, target) || !strings.Contains(out, "--force") {
		t.Errorf("error should name the path and the override, got: %s", out)
	}
	after, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("a refused restore must leave the existing database untouched")
	}
}

// TestRunDBRestore_RunningDaemonRefused asserts restore refuses while a process
// holds the target, with no override, and changes nothing.
func TestRunDBRestore_RunningDaemonRefused(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "joe.db")
	seedDatabase(t, target, "existing", false)

	src := filepath.Join(dir, "b.db")
	seedDatabase(t, src, "fromb", false)
	backup := filepath.Join(dir, "backup.db")
	makeBackup(t, src, backup)

	deps := restoreDeps(t, target)
	// Force the occupied answer: the real probe distinguishes PROCESSES, so a
	// same-process holder would not reproduce a daemon.
	deps.probeTargetOccupied = func(string) (bool, error) { return true, nil }

	before, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runDBCommand(context.Background(), []string{"restore", backup, "--force"}, &stdout, &stderr, deps)
	if code == 0 {
		t.Error("exit = 0, want non-zero while a process holds the target open")
	}
	if !strings.Contains(stderr.String(), "Stop Joe") {
		t.Errorf("error should tell the operator to stop Joe, got: %s", stderr.String())
	}
	after, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("a restore refused for occupancy must leave the target untouched")
	}
}

// TestProbeTargetOccupied_RealLock exercises the real probe, not a fake: it must
// report false for a database nobody holds, and must not mutate it.
func TestProbeTargetOccupied_RealLock(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "joe.db")
	seedDatabase(t, target, "seed", false)

	busy, err := defaultProbeTargetOccupied(target)
	if err != nil {
		t.Fatalf("probe err: %v", err)
	}
	if busy {
		t.Error("probe reported busy for a database no process holds open")
	}
	db, err := sql.Open("sqlite", "file:"+target+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("select count(*) from components").Scan(&n); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if n != 1 {
		t.Errorf("component rows = %d, want the single seeded row", n)
	}
}

// TestRunDBRestore_SrcWithLiveWALRestoresFully is the probe-2b class: a SRC that
// still carries a live, uncheckpointed -wal must restore with ALL its committed
// data. VACUUM INTO from the read-only handle reads through the WAL; a plain byte
// copy of the SRC main file would silently drop everything not yet checkpointed.
func TestRunDBRestore_SrcWithLiveWALRestoresFully(t *testing.T) {
	dir := t.TempDir()
	liveSrc := filepath.Join(dir, "live.db")
	live, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: liveSrc}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := live.Migrate(); err != nil {
		t.Fatal(err)
	}
	const seeded = 9
	for i := range seeded {
		if _, err := live.DB().Exec(
			`INSERT INTO components (id, type, name, config) VALUES (?,?,?,?)`,
			fmt.Sprintf("c-%d", i), "kubernetes", "in-wal", `{"x":true}`); err != nil {
			t.Fatal(err)
		}
	}
	src := filepath.Join(dir, "snapshot.db")
	for _, suf := range []string{"", "-wal", "-shm"} {
		b, err := os.ReadFile(liveSrc + suf)
		if err != nil {
			t.Fatalf("premise broken: expected %s beside the live source: %v", suf, err)
		}
		if err := os.WriteFile(src+suf, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	live.Close()

	target := filepath.Join(dir, "joe.db")
	deps := restoreDeps(t, target)
	var stdout, stderr bytes.Buffer
	if code := runDBCommand(context.Background(), []string{"restore", src}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("restore exit = %d, want 0; stderr: %s", code, stderr.String())
	}

	db, err := sql.Open("sqlite", "file:"+target+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var got int
	if err := db.QueryRow(`select count(*) from components where name='in-wal'`).Scan(&got); err != nil {
		t.Fatalf("read restored target: %v", err)
	}
	if got != seeded {
		t.Errorf("restored target holds %d rows, want %d — data living in the SRC's -wal was dropped", got, seeded)
	}
}

// TestRunDBRestore_SrcUnchangedByPreflight hashes SRC around a full run: restore
// reads it through a read-only handle and must never write to it.
func TestRunDBRestore_SrcUnchangedByPreflight(t *testing.T) {
	dir := t.TempDir()
	srcDB := filepath.Join(dir, "b.db")
	seedDatabase(t, srcDB, "fromb", true)
	backup := filepath.Join(dir, "backup.db")
	makeBackup(t, srcDB, backup)

	hash := func(p string) string {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("hash %s: %v", p, err)
		}
		sum := sha256.Sum256(b)
		return fmt.Sprintf("%x", sum)
	}
	before := hash(backup)

	target := filepath.Join(dir, "joe.db")
	deps := restoreDeps(t, target)
	var stdout, stderr bytes.Buffer
	if code := runDBCommand(context.Background(), []string{"restore", backup}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("restore exit = %d, want 0; stderr: %s", code, stderr.String())
	}
	if after := hash(backup); before != after {
		t.Error("restore modified SRC; the source is opened read-only and must be byte-identical afterwards")
	}
}

// TestRunDBRestore_ForceOverCleanTarget covers the ordinary replace path.
func TestRunDBRestore_ForceOverCleanTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "joe.db")
	seedDatabase(t, target, "old", false)
	for _, suf := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(target + suf); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("premise broken: %s exists beside a cleanly closed database", suf)
		}
	}

	src := filepath.Join(dir, "b.db")
	seedDatabase(t, src, "fromb", false)
	backup := filepath.Join(dir, "backup.db")
	makeBackup(t, src, backup)

	deps := restoreDeps(t, target)
	var stdout, stderr bytes.Buffer
	code := runDBCommand(context.Background(), []string{"restore", backup, "--force"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("restore exit = %d, want 0; stderr: %s", code, stderr.String())
	}
	names := tableNames(t, target)
	if !strings.Contains(names, "fromb_marker") {
		t.Errorf("target is not the backup; tables=[%s]", names)
	}
	if strings.Contains(names, "old_marker") {
		t.Errorf("the replaced database survived --force; tables=[%s]", names)
	}
	out := stdout.String()
	if !strings.Contains(out, target) {
		t.Errorf("success line should name the restored path, got: %s", out)
	}
	if !strings.Contains(out, "encryption.key") {
		t.Errorf("success line should carry the key reminder, got: %s", out)
	}
}

// TestRunDBRestore_SameFileRefused guards the degenerate case: restoring the
// configured database onto itself would delete the sidecars holding the very data
// the copy is about to read.
func TestRunDBRestore_SameFileRefused(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "joe.db")
	seedDatabase(t, target, "existing", false)

	deps := restoreDeps(t, target)
	var stdout, stderr bytes.Buffer
	code := runDBCommand(context.Background(), []string{"restore", target, "--force"}, &stdout, &stderr, deps)
	if code == 0 {
		t.Error("exit = 0, want non-zero when SRC is the configured database itself")
	}
	if !strings.Contains(stderr.String(), "onto itself") {
		t.Errorf("error should explain the self-restore, got: %s", stderr.String())
	}
}
