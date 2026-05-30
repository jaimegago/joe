package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
	_ "modernc.org/sqlite"
)

// adminTestDeps wires `joe admin` against an in-memory RBAC repo backed by
// the real SQL schema (including migration 016's admin_principals table).
func adminTestDeps(t *testing.T) (runDeps, rbac.Repository) {
	t.Helper()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	repo := rbac.NewRepository(s.DB(), s.Driver())

	deps := defaultRunDeps()
	deps.loadConfig = func(string) (*config.Config, error) { return &config.Config{}, nil }
	deps.openRBACRepo = func(*config.Config) (rbacRepo, func() error, error) {
		return repo, func() error { return nil }, nil
	}
	return deps, repo
}

func runAdmin(t *testing.T, deps runDeps, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), append([]string{"admin"}, args...), &stdout, &stderr, deps)
	return code, stdout.String(), stderr.String()
}

// TestPhaseH_AdminGrantListRevoke is the Phase H CLI acceptance: an
// operator can mark a principal as admin, see them in `joe admin list`,
// and revoke the status — all without touching rbac_policies.
func TestPhaseH_AdminGrantListRevoke(t *testing.T) {
	deps, repo := adminTestDeps(t)
	ctx := context.Background()

	// Grant.
	if code, out, errOut := runAdmin(t, deps, "grant", "--principal", "user:root@example.com", "--reason", "incident commander"); code != 0 {
		t.Fatalf("admin grant exit = %d (stderr: %s)", code, errOut)
	} else if !strings.Contains(out, "Granted admin status") {
		t.Fatalf("grant output = %q", out)
	}
	if admin, _ := repo.IsAdmin(ctx, "user:root@example.com"); !admin {
		t.Fatal("after grant, principal should be admin")
	}

	// Idempotent: second grant updates rather than fails.
	if code, out, _ := runAdmin(t, deps, "grant", "--principal", "user:root@example.com"); code != 0 || !strings.Contains(out, "already admin") {
		t.Fatalf("idempotent admin grant: code=%d out=%q", code, out)
	}

	// List shows it.
	if code, out, _ := runAdmin(t, deps, "list"); code != 0 || !strings.Contains(out, "user:root@example.com") {
		t.Fatalf("admin list: code=%d out=%q", code, out)
	}

	// Revoke.
	if code, out, errOut := runAdmin(t, deps, "revoke", "--principal", "user:root@example.com"); code != 0 {
		t.Fatalf("admin revoke exit = %d (stderr: %s)", code, errOut)
	} else if !strings.Contains(out, "Revoked admin status") {
		t.Fatalf("revoke output = %q", out)
	}
	if admin, _ := repo.IsAdmin(ctx, "user:root@example.com"); admin {
		t.Fatal("after revoke, principal should no longer be admin")
	}

	// Revoke a non-existent admin is a clean no-op (exit 0).
	if code, out, _ := runAdmin(t, deps, "revoke", "--principal", "user:root@example.com"); code != 0 || !strings.Contains(out, "not admin") {
		t.Fatalf("revoke missing: code=%d out=%q", code, out)
	}
}

// TestPhaseH_AdminGrantCleansUpZoneSnapshots asserts the CLI surface
// enforces the same single-source-of-truth invariant the bootstrap
// path does: promoting a principal to admin removes any per-zone
// rbac_policies rows they held (which are now redundant). Matches the
// auth.Provisioner.GrantAdmin behaviour.
func TestPhaseH_AdminGrantCleansUpZoneSnapshots(t *testing.T) {
	deps, repo := adminTestDeps(t)
	ctx := context.Background()

	// Pre-seed a zone grant the way `joe zone grant` would.
	if _, err := repo.CreatePolicy(ctx, rbac.Policy{
		Principal: "user:operator@example.com",
		ZoneID:    "prod-readonly",
	}); err != nil {
		t.Fatalf("seed zone grant: %v", err)
	}

	if code, _, errOut := runAdmin(t, deps, "grant", "--principal", "user:operator@example.com"); code != 0 {
		t.Fatalf("admin grant exit = %d (stderr: %s)", code, errOut)
	}

	grants, _ := repo.ListPoliciesForPrincipal(ctx, "user:operator@example.com")
	if len(grants) != 0 {
		t.Errorf("Phase H CLI: promoting to admin must drop redundant rbac_policies, found %d: %#v",
			len(grants), grants)
	}
	if admin, _ := repo.IsAdmin(ctx, "user:operator@example.com"); !admin {
		t.Error("after promotion, principal should be admin")
	}
}

func TestPhaseH_AdminListEmpty(t *testing.T) {
	deps, _ := adminTestDeps(t)
	if code, out, _ := runAdmin(t, deps, "list"); code != 0 || !strings.Contains(out, "No admin") {
		t.Fatalf("empty list: code=%d out=%q", code, out)
	}
}

func TestPhaseH_AdminGrantUnprefixedPrincipalRejected(t *testing.T) {
	deps, _ := adminTestDeps(t)
	code, _, errOut := runAdmin(t, deps, "grant", "--principal", "operator")
	if code != 1 {
		t.Fatalf("grant unprefixed principal exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "reserved prefix") {
		t.Fatalf("expected reserved-prefix error, got %q", errOut)
	}
}
