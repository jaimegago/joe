package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/paths"
	"github.com/jaimegago/joe/internal/skills"
	"github.com/jaimegago/joe/internal/store"
)

// This file covers --config across every subcommand that carries it. Three
// properties per command, plus a coherence property for the commands that read
// more than one thing out of a config:
//
//  1. an explicitly named config governs what the command acts on,
//  2. an explicitly named path that does not resolve is an operational failure
//     (exit 1) naming the flag, rather than a silent fallback to defaults,
//  3. omitting the flag loads the default path, exactly as before it existed.
//
// Non-vacuity throughout comes from the named config carrying values no default
// config could supply — temp-directory paths and httptest addresses. If the flag
// were ignored, every honoring test would fail rather than pass by accident.

// writeConfigFile writes body to a temp config.yaml and returns its path.
func writeConfigFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "joe.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return p
}

// recordConfigPath wraps deps.loadConfig so a test can see which path was asked
// for while the REAL config.Load still runs.
func recordConfigPath(deps *runDeps, seen *[]string) {
	inner := deps.loadConfig
	deps.loadConfig = func(path string) (*config.Config, error) {
		*seen = append(*seen, path)
		return inner(path)
	}
}

// missingConfigPath names a file that does not exist, inside a directory that
// does — so the failure is unambiguously the file, not its parent.
func missingConfigPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "no-such-config.yaml")
}

// ------------------------------------------------------------------ joe unlock

// TestConfigFlag_UnlockHonorsExplicitPath pins that --config selects the
// database whose panic row is cleared. That is the whole of what configuration
// governs for this command, so redirecting it is coherent by construction.
func TestConfigFlag_UnlockHonorsExplicitPath(t *testing.T) {
	wantDSN := filepath.Join(t.TempDir(), "elsewhere.db")
	cfgPath := writeConfigFile(t, "database:\n  dsn: "+wantDSN+"\n")

	fake := &fakePanicRowStore{panicked: true}
	var gotDSN string
	deps := testDeps(t.TempDir()) // loadConfig stays REAL — the file is the subject.
	deps.openPanicStore = func(cfg *config.Config) (panicRowStore, func() error, error) {
		dbCfg, err := databaseConfigFor(cfg)
		if err != nil {
			return nil, nil, err
		}
		gotDSN = dbCfg.DSN
		return fake, func() error { return nil }, nil
	}

	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(),
		[]string{"unlock", "--config", cfgPath, "--reason", "test"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, stderr.String())
	}
	if gotDSN != wantDSN {
		t.Errorf("cleared the panic row in %q, want %q — the named config did not govern the database",
			gotDSN, wantDSN)
	}
	if !fake.cleared {
		t.Error("the panic row was not cleared")
	}
}

func TestConfigFlag_UnlockNonexistentPathIsOperationalFailure(t *testing.T) {
	opened := false
	deps := testDeps(t.TempDir())
	deps.openPanicStore = func(*config.Config) (panicRowStore, func() error, error) {
		opened = true
		return &fakePanicRowStore{}, func() error { return nil }, nil
	}
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(),
		[]string{"unlock", "--config", missingConfigPath(t)}, &stdout, &stderr, deps)
	if code != 1 {
		t.Errorf("exit = %d, want 1 (operational failure)", code)
	}
	if opened {
		t.Error("a database was opened for a config file that does not exist")
	}
	if !strings.Contains(stderr.String(), "--config") {
		t.Errorf("refusal should name the flag, got: %s", stderr.String())
	}
}

func TestConfigFlag_UnlockAbsentFlagUsesDefaultPath(t *testing.T) {
	deps := testDeps(t.TempDir())
	deps.openPanicStore = func(*config.Config) (panicRowStore, func() error, error) {
		return &fakePanicRowStore{}, func() error { return nil }, nil
	}
	var seen []string
	recordConfigPath(&deps, &seen)

	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"unlock"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, stderr.String())
	}
	if len(seen) != 1 || seen[0] != paths.DefaultConfigPath() {
		t.Errorf("loaded %v, want exactly one load of the default %q", seen, paths.DefaultConfigPath())
	}
}

// ------------------------------------------------------------- joe db backup

// TestConfigFlag_DBBackupHonorsExplicitPath is end-to-end through the REAL
// defaultOpenBackupStore: the named config's database.dsn is the database that
// gets copied. This is the case D-0131 called out as the sharpest — without the
// flag the command reports a successful backup of a database the operator was
// not backing up.
//
// Non-vacuity: the copy is checked for a marker table that exists only in the
// seeded source, so a backup of any other database fails the assertion.
func TestConfigFlag_DBBackupHonorsExplicitPath(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "named.db")
	seedDatabase(t, src, "named", false)
	cfgPath := writeConfigFile(t, "database:\n  dsn: "+src+"\n")
	dest := filepath.Join(dir, "copy.db")

	deps := testDeps(t.TempDir()) // openBackupStore stays REAL.
	var stdout, stderr bytes.Buffer
	// Flag AFTER the positional: the order an operator writes naturally, and the
	// one the stdlib flag package rejects without reorderFlagsFirst.
	code := runWithDeps(context.Background(),
		[]string{"db", "backup", dest, "--config", cfgPath}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, stderr.String())
	}
	if names := tableNames(t, dest); !strings.Contains(names, "named_marker") {
		t.Errorf("backup tables = %q, want the named config's database (named_marker)", names)
	}
}

func TestConfigFlag_DBBackupNonexistentPathIsOperationalFailure(t *testing.T) {
	fake := &fakeBackupStore{}
	deps := depsWithBackupStore(fake)
	dest := filepath.Join(t.TempDir(), "copy.db")
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(),
		[]string{"db", "backup", dest, "--config", missingConfigPath(t)}, &stdout, &stderr, deps)
	if code != 1 {
		t.Errorf("exit = %d, want 1 (operational failure)", code)
	}
	if fake.execRan {
		t.Error("a backup ran against a config file that does not exist")
	}
	if !strings.Contains(stderr.String(), "--config") {
		t.Errorf("refusal should name the flag, got: %s", stderr.String())
	}
}

func TestConfigFlag_DBBackupAbsentFlagUsesDefaultPath(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.db")
	seedDatabase(t, src, "def", false)
	deps := depsWithBackupStore(&fakeBackupStore{})
	var seen []string
	recordConfigPath(&deps, &seen)

	var stdout, stderr bytes.Buffer
	// The fake store makes no file, so the command fails on the read-back; the
	// subject here is only which config path was asked for.
	runWithDeps(context.Background(),
		[]string{"db", "backup", filepath.Join(dir, "copy.db")}, &stdout, &stderr, deps)
	if len(seen) != 1 || seen[0] != paths.DefaultConfigPath() {
		t.Errorf("loaded %v, want exactly one load of the default %q", seen, paths.DefaultConfigPath())
	}
}

// ------------------------------------------------------------ joe db restore

// TestConfigFlag_DBRestoreRedirectsDatabaseAndKeyCoherently is the coherence
// test for the one command with two config-governed uses: the database it
// replaces, and the encryption key it checks for beside it.
//
// It asserts the redirect is coherent by construction, not merely in effect —
// both uses receive the SAME *config.Config, so a partial redirect is not
// representable. It then checks both effects: the restored file lands at the
// named config's dsn, and the key checked is the one at the named config's
// encryption_key_path.
//
// This is the pairing that matters: a flag that moved the database but resolved
// the key from the default config would check for a key belonging to a different
// install, pass the missing-key gate on the strength of it, and hand back a
// database nothing can decrypt.
func TestConfigFlag_DBRestoreRedirectsDatabaseAndKeyCoherently(t *testing.T) {
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin.db")
	seedDatabase(t, origin, "restored", true) // encrypted config → the key gate fires
	src := filepath.Join(dir, "backup.db")
	makeBackup(t, origin, src)

	wantTarget := filepath.Join(dir, "target.db")
	wantKey := filepath.Join(dir, "named-encryption.key")
	if err := os.WriteFile(wantKey, make([]byte, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeConfigFile(t,
		"database:\n  dsn: "+wantTarget+"\n  encryption_key_path: "+wantKey+"\n")

	var cfgForDB, cfgForKey *config.Config
	var gotKey string
	deps := testDeps(t.TempDir())
	deps.resolveDatabaseConfig = func(cfg *config.Config) (store.DatabaseConfig, error) {
		cfgForDB = cfg
		return databaseConfigFor(cfg)
	}
	deps.encryptionKeyPath = func(cfg *config.Config) (string, error) {
		cfgForKey = cfg
		p, err := encryptionKeyPathFor(cfg)
		gotKey = p
		return p, err
	}

	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(),
		[]string{"db", "restore", src, "--config", cfgPath}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, stderr.String())
	}

	if cfgForDB == nil || cfgForKey == nil {
		t.Fatalf("both config-governed uses must run; db=%v key=%v", cfgForDB != nil, cfgForKey != nil)
	}
	if cfgForDB != cfgForKey {
		t.Error("the database target and the encryption key were resolved from different config objects; " +
			"a partial redirect is possible")
	}
	if gotKey != wantKey {
		t.Errorf("checked key %q, want %q — the named config did not govern the key path", gotKey, wantKey)
	}
	if names := tableNames(t, wantTarget); !strings.Contains(names, "restored_marker") {
		t.Errorf("target tables = %q, want the backup restored to the named config's dsn", names)
	}
}

func TestConfigFlag_DBRestoreNonexistentPathIsOperationalFailure(t *testing.T) {
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin.db")
	seedDatabase(t, origin, "x", false)
	src := filepath.Join(dir, "backup.db")
	makeBackup(t, origin, src)

	resolved := false
	deps := testDeps(t.TempDir())
	deps.resolveDatabaseConfig = func(*config.Config) (store.DatabaseConfig, error) {
		resolved = true
		return store.DatabaseConfig{Driver: store.DriverSQLite, DSN: filepath.Join(dir, "t.db")}, nil
	}
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(),
		[]string{"db", "restore", src, "--config", missingConfigPath(t)}, &stdout, &stderr, deps)
	if code != 1 {
		t.Errorf("exit = %d, want 1 (operational failure)", code)
	}
	if resolved {
		t.Error("a restore target was resolved from a config file that does not exist")
	}
	if !strings.Contains(stderr.String(), "--config") {
		t.Errorf("refusal should name the flag, got: %s", stderr.String())
	}
}

func TestConfigFlag_DBRestoreAbsentFlagUsesDefaultPath(t *testing.T) {
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin.db")
	seedDatabase(t, origin, "y", false)
	src := filepath.Join(dir, "backup.db")
	makeBackup(t, origin, src)

	deps := restoreDeps(t, filepath.Join(dir, "target.db"))
	var seen []string
	recordConfigPath(&deps, &seen)

	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"db", "restore", src}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, stderr.String())
	}
	if len(seen) != 1 || seen[0] != paths.DefaultConfigPath() {
		t.Errorf("loaded %v, want exactly one load of the default %q", seen, paths.DefaultConfigPath())
	}
}

// -------------------------------------------------------------- joe incident

// keyedRegimeServer is a stand-in joe server for the incident tests. It answers
// GET /api/v1/regime only for a request bearing wantKey, so a test passes only
// when the address AND the key both came from the same named config.
func keyedRegimeServer(t *testing.T, wantKey string) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+wantKey {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"regime": "normal"})
	}))
	t.Cleanup(ts.Close)
	return ts
}

// TestConfigFlag_IncidentRedirectsAddressAndKeyCoherently is the coherence test
// for `joe incident`: one load feeds both the server address it contacts and the
// key it presents. The stand-in server rejects a request carrying the wrong key,
// so reaching exit 0 proves both came from the file the flag named — neither is
// obtainable from a default config.
func TestConfigFlag_IncidentRedirectsAddressAndKeyCoherently(t *testing.T) {
	const key = "named-config-key"
	ts := keyedRegimeServer(t, key)
	addr := strings.TrimPrefix(ts.URL, "http://")
	cfgPath := writeConfigFile(t,
		"server:\n  address: "+addr+"\n  service_accounts:\n    - name: server\n      key: "+key+"\n")

	deps := testDeps(t.TempDir())
	deps.newClient = client.New

	var stdout, stderr bytes.Buffer
	// Flag after the sub-subcommand word — splitConfigFlag accepts it anywhere.
	code := runWithDeps(context.Background(),
		[]string{"incident", "status", "--config", cfgPath}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "operating normally") {
		t.Errorf("stdout = %q, want the regime read back from the named config's server", stdout.String())
	}
}

// TestConfigFlag_IncidentAcceptsFlagBeforeSubcommand pins the other position, so
// an operator's invocation shape is not load-bearing.
func TestConfigFlag_IncidentAcceptsFlagBeforeSubcommand(t *testing.T) {
	const key = "named-config-key"
	ts := keyedRegimeServer(t, key)
	addr := strings.TrimPrefix(ts.URL, "http://")
	cfgPath := writeConfigFile(t,
		"server:\n  address: "+addr+"\n  service_accounts:\n    - name: server\n      key: "+key+"\n")

	deps := testDeps(t.TempDir())
	deps.newClient = client.New

	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(),
		[]string{"incident", "--config", cfgPath, "status"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, stderr.String())
	}
}

func TestConfigFlag_IncidentNonexistentPathIsOperationalFailure(t *testing.T) {
	contacted := false
	deps := testDeps(t.TempDir())
	deps.newClient = func(string, ...client.ClientOption) *client.Client {
		contacted = true
		return client.New("http://127.0.0.1:1")
	}
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(),
		[]string{"incident", "status", "--config", missingConfigPath(t)}, &stdout, &stderr, deps)
	if code != 1 {
		t.Errorf("exit = %d, want 1 (operational failure)", code)
	}
	if contacted {
		t.Error("a client was built from a config file that does not exist")
	}
	if !strings.Contains(stderr.String(), "--config") {
		t.Errorf("refusal should name the flag, got: %s", stderr.String())
	}
}

func TestConfigFlag_IncidentAbsentFlagUsesDefaultPath(t *testing.T) {
	deps := testDeps(t.TempDir())
	deps.newClient = func(string, ...client.ClientOption) *client.Client {
		return client.New("http://127.0.0.1:1")
	}
	var seen []string
	recordConfigPath(&deps, &seen)

	var stdout, stderr bytes.Buffer
	// The status read fails (nothing is listening); the subject is the path.
	runWithDeps(context.Background(), []string{"incident", "status"}, &stdout, &stderr, deps)
	if len(seen) != 1 || seen[0] != paths.DefaultConfigPath() {
		t.Errorf("loaded %v, want exactly one load of the default %q", seen, paths.DefaultConfigPath())
	}
}

// ---------------------------------------------------------------- joe skills

// TestConfigFlag_SkillsRedirectsTrustedSourcesAndReloadTarget is the coherence
// test for `joe skills`. Its two config-governed uses live in different
// sub-subcommands, so the property to pin is that ONE load serves whichever runs:
// each invocation reads the named file exactly once, and the value that
// invocation depends on comes from it.
func TestConfigFlag_SkillsRedirectsTrustedSourcesAndReloadTarget(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok", "trigger": "manual", "before": 1, "after": 1,
		})
	}))
	t.Cleanup(ts.Close)
	addr := strings.TrimPrefix(ts.URL, "http://")
	cfgPath := writeConfigFile(t,
		"server:\n  address: "+addr+"\nskills:\n  trusted_sources:\n    - github.com/named-org\n")

	mgr := &fakeSkillManager{installResp: &skills.Install{Skills: []skills.SkillRecord{{Name: "x"}}}}
	deps := skillsDeps(t, t.TempDir(), mgr)
	deps.newClient = client.New
	var observedTrusted []string
	deps.newSkillManager = func(_ string, trusted []string, _ *skills.Policy) skillManager {
		observedTrusted = trusted
		return mgr
	}
	var seen []string
	recordConfigPath(&deps, &seen)

	// Use 1: the trusted-source allowlist install enforces.
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(),
		[]string{"skills", "install", "https://github.com/named-org/s.git", "--config", cfgPath},
		&stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("install exit = %d, want 0; stderr=%s", code, stderr.String())
	}
	if len(observedTrusted) != 1 || observedTrusted[0] != "github.com/named-org" {
		t.Errorf("trusted sources = %v, want the named config's allowlist", observedTrusted)
	}

	// Use 2: the server reload contacts.
	stdout.Reset()
	stderr.Reset()
	code = runWithDeps(context.Background(),
		[]string{"skills", "reload", "--config", cfgPath}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("reload exit = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Reload ok") {
		t.Errorf("reload stdout = %q, want the named config's server contacted", stdout.String())
	}

	// One load per invocation, both of the named file: neither use can be
	// redirected without the other.
	if len(seen) != 2 || seen[0] != cfgPath || seen[1] != cfgPath {
		t.Errorf("config loads = %v, want exactly one load of %q per invocation", seen, cfgPath)
	}
}

func TestConfigFlag_SkillsNonexistentPathIsOperationalFailure(t *testing.T) {
	mgr := &fakeSkillManager{}
	deps := skillsDeps(t, t.TempDir(), mgr)
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(),
		[]string{"skills", "install", "https://github.com/o/s.git", "--config", missingConfigPath(t)},
		&stdout, &stderr, deps)
	if code != 1 {
		t.Errorf("exit = %d, want 1 (operational failure)", code)
	}
	if mgr.installArgs.repo != "" {
		t.Error("the skills manager ran against a config file that does not exist")
	}
	if !strings.Contains(stderr.String(), "--config") {
		t.Errorf("refusal should name the flag, got: %s", stderr.String())
	}
}

func TestConfigFlag_SkillsAbsentFlagUsesDefaultPath(t *testing.T) {
	mgr := &fakeSkillManager{}
	deps := skillsDeps(t, t.TempDir(), mgr)
	var seen []string
	recordConfigPath(&deps, &seen)

	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"skills", "list"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, stderr.String())
	}
	if len(seen) != 1 || seen[0] != paths.DefaultConfigPath() {
		t.Errorf("loaded %v, want exactly one load of the default %q", seen, paths.DefaultConfigPath())
	}
}

// ----------------------------------------------------------------- joe panic

// TestConfigFlag_PanicNonexistentPathIsOperationalFailure pins the posture this
// command gained: it already carried --config, but with the default path as the
// flag's default value it could not tell "not passed" from "passed", so a path
// that did not resolve fell through to defaults and triggered a shutdown against
// whichever server the default config names.
func TestConfigFlag_PanicNonexistentPathIsOperationalFailure(t *testing.T) {
	contacted := false
	deps := testDeps(t.TempDir())
	deps.newClient = func(string, ...client.ClientOption) *client.Client {
		contacted = true
		return client.New("http://127.0.0.1:1")
	}
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(),
		[]string{"panic", "--config", missingConfigPath(t)}, &stdout, &stderr, deps)
	if code != 1 {
		t.Errorf("exit = %d, want 1 (operational failure)", code)
	}
	if contacted {
		t.Error("a shutdown was addressed to a server named by a config file that does not exist")
	}
	if !strings.Contains(stderr.String(), "--config") {
		t.Errorf("refusal should name the flag, got: %s", stderr.String())
	}
}

func TestConfigFlag_PanicAbsentFlagUsesDefaultPath(t *testing.T) {
	deps := testDeps(t.TempDir())
	deps.newClient = func(string, ...client.ClientOption) *client.Client {
		return client.New("http://127.0.0.1:1")
	}
	var seen []string
	recordConfigPath(&deps, &seen)

	var stdout, stderr bytes.Buffer
	runWithDeps(context.Background(), []string{"panic"}, &stdout, &stderr, deps)
	if len(seen) != 1 || seen[0] != paths.DefaultConfigPath() {
		t.Errorf("loaded %v, want exactly one load of the default %q", seen, paths.DefaultConfigPath())
	}
}

// --------------------------------------------------------------- the helpers

func TestSplitConfigFlag(t *testing.T) {
	cases := []struct {
		name      string
		args      []string
		wantValue string
		wantRest  []string
	}{
		{"absent", []string{"status"}, "", []string{"status"}},
		{"long separate", []string{"status", "--config", "p"}, "p", []string{"status"}},
		{"short separate", []string{"status", "-config", "p"}, "p", []string{"status"}},
		{"long inline", []string{"--config=p", "status"}, "p", []string{"status"}},
		{"short inline", []string{"-config=p", "status"}, "p", []string{"status"}},
		{"before the word", []string{"--config", "p", "declare", "--session", "s"}, "p",
			[]string{"declare", "--session", "s"}},
		{"other flags untouched", []string{"install", "r", "--ref", "v1", "--config", "p"}, "p",
			[]string{"install", "r", "--ref", "v1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, rest, err := splitConfigFlag(tc.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantValue {
				t.Errorf("value = %q, want %q", got, tc.wantValue)
			}
			if strings.Join(rest, " ") != strings.Join(tc.wantRest, " ") {
				t.Errorf("rest = %v, want %v", rest, tc.wantRest)
			}
		})
	}
}

func TestSplitConfigFlag_TrailingFlagNeedsAValue(t *testing.T) {
	if _, _, err := splitConfigFlag([]string{"status", "--config"}); err == nil {
		t.Error("a --config with no value must be reported as malformed")
	}
}

// TestResolveConfigFlag_EmptyValueIsTheDefaultPath pins the other half of the
// asymmetry directly: not passing the flag never fails, whatever is on disk.
func TestResolveConfigFlag_EmptyValueIsTheDefaultPath(t *testing.T) {
	var stderr bytes.Buffer
	got, ok := resolveConfigFlag("", &stderr)
	if !ok {
		t.Fatalf("an absent flag must resolve; stderr=%s", stderr.String())
	}
	if got != paths.DefaultConfigPath() {
		t.Errorf("path = %q, want %q", got, paths.DefaultConfigPath())
	}
}
