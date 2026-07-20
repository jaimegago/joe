package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/auth"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
)

// fakeAdminGrantStore is an in-memory adminGrantStore for the routing and
// refusal tests. It records whether the grant ran, so a test can assert the
// command refused BEFORE reaching the database — which is the point of every
// refusal here except the containment clause itself.
type fakeAdminGrantStore struct {
	granted   bool
	grantErr  error
	grantRan  bool
	principal rbac.Principal
}

func (f *fakeAdminGrantStore) GrantFirstAdmin(_ context.Context, p rbac.Principal) (bool, error) {
	f.grantRan = true
	f.principal = p
	return f.granted, f.grantErr
}

// depsWithAdminStore wires deps.openAdminStore to fake and deps.loadConfig to a
// config carrying the named service accounts. paths.JoeDirPath does NOT honour
// $HOME, so injection (not a temp home) is the only way these tests stay off the
// developer's real ~/.joe.
func depsWithAdminStore(fake *fakeAdminGrantStore, serviceAccounts ...string) runDeps {
	deps := defaultRunDeps()
	deps.openAdminStore = func() (adminGrantStore, func() error, error) {
		return fake, func() error { return nil }, nil
	}
	deps.loadConfig = func(string) (*config.Config, error) {
		cfg := &config.Config{}
		for _, name := range serviceAccounts {
			cfg.Server.ServiceAccounts = append(cfg.Server.ServiceAccounts,
				config.ServiceAccount{Name: name, Key: "key-" + name})
		}
		return cfg, nil
	}
	return deps
}

func TestRunAdminCommand_NoSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runAdminCommand(context.Background(), nil, &stdout, &stderr, defaultRunDeps())
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "Usage: joe admin") {
		t.Errorf("stderr missing usage, got: %s", stderr.String())
	}
}

func TestRunAdminCommand_UnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runAdminCommand(context.Background(), []string{"revoke"}, &stdout, &stderr, defaultRunDeps())
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	out := stderr.String()
	if !strings.Contains(out, "revoke") || !strings.Contains(out, "Usage: joe admin") {
		t.Errorf("stderr should name the bad subcommand and print usage, got: %s", out)
	}
}

func TestAdminBootstrap_ArityIsUsageError(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"none", nil},
		{"two", []string{"svc:a", "svc:b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeAdminGrantStore{granted: true}
			var stdout, stderr bytes.Buffer
			code := runAdminBootstrap(context.Background(), tc.args, &stdout, &stderr,
				depsWithAdminStore(fake, "ci"))
			if code != 2 {
				t.Errorf("exit code = %d, want 2 (usage error)", code)
			}
			if fake.grantRan {
				t.Error("the grant ran despite a usage error")
			}
		})
	}
}

// TestAdminBootstrap_GrantsConfiguredServiceAccount pins the accepted case and,
// with it, the principal STRING that reaches the database: it must be what
// rbac.ServicePrincipal mints, since that is what a presented key resolves to on
// the authenticating request path. A row carrying anything else would be an
// admin nobody can log in as.
func TestAdminBootstrap_GrantsConfiguredServiceAccount(t *testing.T) {
	for _, arg := range []string{"svc:ci", "ci"} {
		t.Run(arg, func(t *testing.T) {
			fake := &fakeAdminGrantStore{granted: true}
			var stdout, stderr bytes.Buffer
			code := runAdminBootstrap(context.Background(), []string{arg}, &stdout, &stderr,
				depsWithAdminStore(fake, "ci", "reporting"))
			if code != 0 {
				t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
			}
			want, err := rbac.ServicePrincipal("ci")
			if err != nil {
				t.Fatalf("ServicePrincipal: %v", err)
			}
			if fake.principal != want {
				t.Errorf("granted principal = %q, want %q", fake.principal, want)
			}
			if !strings.Contains(stdout.String(), "one-time") {
				t.Errorf("success narration should say the command is one-shot, got: %s", stdout.String())
			}
		})
	}
}

// TestAdminBootstrap_RefusesHumanIdentity pins the service-account-only
// restriction for the two human-shaped prefixes, and that the refusal names the
// admin_email path as the remedy. The database is never opened.
func TestAdminBootstrap_RefusesHumanIdentity(t *testing.T) {
	for _, arg := range []string{"user:alice@example.com", "USER:alice@example.com", "group:sre"} {
		t.Run(arg, func(t *testing.T) {
			fake := &fakeAdminGrantStore{granted: true}
			var stdout, stderr bytes.Buffer
			code := runAdminBootstrap(context.Background(), []string{arg}, &stdout, &stderr,
				depsWithAdminStore(fake, "ci"))
			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if fake.grantRan {
				t.Error("a human identity reached the grant — it must be refused before the database is opened")
			}
			if !strings.Contains(stderr.String(), "auth.admin_email") {
				t.Errorf("refusal should point at the admin_email bootstrap, got: %s", stderr.String())
			}
		})
	}
}

func TestAdminBootstrap_RefusesUnconfiguredServiceAccount(t *testing.T) {
	fake := &fakeAdminGrantStore{granted: true}
	var stdout, stderr bytes.Buffer
	code := runAdminBootstrap(context.Background(), []string{"svc:nope"}, &stdout, &stderr,
		depsWithAdminStore(fake, "ci", "reporting"))
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if fake.grantRan {
		t.Error("an unconfigured principal reached the grant")
	}
	out := stderr.String()
	// The refusal lists what IS configured, so the operator can correct a typo
	// without going to read the config file.
	if !strings.Contains(out, "svc:ci") || !strings.Contains(out, "svc:reporting") {
		t.Errorf("refusal should list the configured accounts, got: %s", out)
	}
}

func TestAdminBootstrap_RefusesWhenNoServiceAccountsConfigured(t *testing.T) {
	fake := &fakeAdminGrantStore{granted: true}
	var stdout, stderr bytes.Buffer
	code := runAdminBootstrap(context.Background(), []string{"svc:ci"}, &stdout, &stderr,
		depsWithAdminStore(fake))
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if fake.grantRan {
		t.Error("the grant ran with no service accounts configured")
	}
	// The dedicated-account guidance is the operationally load-bearing half of
	// this message: promoting the shared general-purpose key would put admin on
	// the bearer secret every caller already holds.
	if !strings.Contains(stderr.String(), "DEDICATED") {
		t.Errorf("refusal should steer the operator to a dedicated account, got: %s", stderr.String())
	}
}

// TestAdminBootstrap_RefusalWhenAdminExists is the CLI-level containment clause:
// a refused grant exits non-zero and says so. It is the shape the real
// repository produces (granted=false, err=nil), which the database-backed dual
// below proves.
func TestAdminBootstrap_RefusalWhenAdminExists(t *testing.T) {
	fake := &fakeAdminGrantStore{granted: false}
	var stdout, stderr bytes.Buffer
	code := runAdminBootstrap(context.Background(), []string{"svc:ci"}, &stdout, &stderr,
		depsWithAdminStore(fake, "ci"))
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !fake.grantRan {
		t.Error("the grant should have been attempted — the refusal is the store's answer, not the CLI's guess")
	}
	out := stderr.String()
	if !strings.Contains(out, "already has an admin") {
		t.Errorf("refusal should name the cause, got: %s", out)
	}
	if !strings.Contains(out, "/api/v1/admin/admins") {
		t.Errorf("refusal should name the surface that owns later grants, got: %s", out)
	}
}

func TestAdminBootstrap_StoreOpenFailure(t *testing.T) {
	deps := depsWithAdminStore(&fakeAdminGrantStore{}, "ci")
	deps.openAdminStore = func() (adminGrantStore, func() error, error) {
		return nil, nil, errors.New("boom")
	}
	var stdout, stderr bytes.Buffer
	code := runAdminBootstrap(context.Background(), []string{"svc:ci"}, &stdout, &stderr, deps)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "boom") {
		t.Errorf("stderr should surface the open error, got: %s", stderr.String())
	}
}

func TestAdminBootstrap_GrantFailure(t *testing.T) {
	fake := &fakeAdminGrantStore{grantErr: errors.New("db is angry")}
	var stdout, stderr bytes.Buffer
	code := runAdminBootstrap(context.Background(), []string{"svc:ci"}, &stdout, &stderr,
		depsWithAdminStore(fake, "ci"))
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "db is angry") {
		t.Errorf("stderr should surface the grant error, got: %s", stderr.String())
	}
}

// TestAdminBootstrap_ContainmentAgainstRealDatabase is the DUAL of
// internal/api/admin_stage3_test.go::TestAdminRemoveAdmin_LastAdminConflict.
// Read as a pair they state one closed rule about the admin roster:
//
//	zero admins -> one     ONLY through this command, ONLY once (here)
//	one admin   -> zero    NEVER (the last-admin 409 guard, there)
//
// This half runs against a real migrated store and the real audited repository,
// so it pins the whole path the CLI actually takes: the first grant lands with
// its in-transaction audit row, and the second is refused with the roster and
// the audit log both unchanged. The second grant deliberately names a DIFFERENT
// principal — a repeat of the first would be refused by the primary key, which
// would make the assertion vacuous.
func TestAdminBootstrap_ContainmentAgainstRealDatabase(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = s.Close() }()
	if err := s.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	auditRepo := audit.NewRepository(s.DB(), s.Driver())
	repo := rbac.NewRepositoryWithAudit(s.DB(), s.Driver(), auditRepo)
	prov := auth.NewProvisioner(repo)

	countAdmins := func() int {
		var n int
		if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_principals`).Scan(&n); err != nil {
			t.Fatalf("count admins: %v", err)
		}
		return n
	}
	countGrantAudit := func() int {
		var n int
		if err := s.DB().QueryRowContext(ctx,
			`SELECT COUNT(*) FROM audit_log WHERE action = ?`, audit.ActionAdminGrant).Scan(&n); err != nil {
			t.Fatalf("count audit: %v", err)
		}
		return n
	}

	// Zero -> one. This is the ONLY transition out of the absorbing state.
	granted, err := prov.GrantFirstAdmin(ctx, "svc:ci")
	if err != nil || !granted {
		t.Fatalf("first bootstrap: granted=%v err=%v; want true,nil", granted, err)
	}
	if got := countAdmins(); got != 1 {
		t.Fatalf("admins after first bootstrap = %d, want 1", got)
	}
	if got := countGrantAudit(); got != 1 {
		t.Fatalf("admin.grant audit rows = %d, want 1 (written in the grant's own transaction)", got)
	}

	// Provenance: the reserved CLI value, distinguishable from the OIDC
	// bootstrap path without parsing the reason (migration 016's rationale).
	var grantedBy, reason string
	if err := s.DB().QueryRowContext(ctx,
		`SELECT granted_by, reason FROM admin_principals WHERE principal = ?`, "svc:ci").
		Scan(&grantedBy, &reason); err != nil {
		t.Fatalf("read provenance: %v", err)
	}
	if grantedBy != auth.GrantedByCLI {
		t.Errorf("granted_by = %q, want %q", grantedBy, auth.GrantedByCLI)
	}
	if grantedBy == auth.GrantedByBootstrapAdminEmail {
		t.Error("the CLI grant is labelled as the admin_email bootstrap — the two paths must stay distinguishable")
	}
	if reason != auth.CLIBootstrapReason {
		t.Errorf("reason = %q, want %q", reason, auth.CLIBootstrapReason)
	}

	// The audit row names the mechanism, not the granted principal: the service
	// account performed no action and must not be recorded as having escalated
	// itself.
	var actor string
	if err := s.DB().QueryRowContext(ctx,
		`SELECT principal FROM audit_log WHERE action = ?`, audit.ActionAdminGrant).Scan(&actor); err != nil {
		t.Fatalf("read audit actor: %v", err)
	}
	if actor != auth.ActorCLIBootstrap {
		t.Errorf("audit actor = %q, want %q", actor, auth.ActorCLIBootstrap)
	}

	// One -> refused. A different principal, so the primary key is not what
	// stops it.
	granted, err = prov.GrantFirstAdmin(ctx, "svc:reporting")
	if err != nil {
		t.Fatalf("second bootstrap returned an error; a refusal is (false, nil): %v", err)
	}
	if granted {
		t.Fatal("the second bootstrap granted admin — the containment clause did not hold")
	}
	if got := countAdmins(); got != 1 {
		t.Errorf("admins after refused bootstrap = %d, want 1", got)
	}
	if got := countGrantAudit(); got != 1 {
		t.Errorf("admin.grant audit rows after refused bootstrap = %d, want 1 (a refusal changes nothing)", got)
	}
}
