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

// zoneTestDeps wires the `joe zone` command against an in-memory RBAC repo so
// the test exercises the real provisioning SQL without touching a real DB.
func zoneTestDeps(t *testing.T) (runDeps, rbac.Repository) {
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

func runZone(t *testing.T, deps runDeps, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), append([]string{"zone"}, args...), &stdout, &stderr, deps)
	return code, stdout.String(), stderr.String()
}

func TestZoneGrantRevokeList(t *testing.T) {
	deps, repo := zoneTestDeps(t)
	ctx := context.Background()

	// Grant.
	if code, out, errOut := runZone(t, deps, "grant", "--principal", "user:alice@example.com", "--zone", "prod-readonly"); code != 0 {
		t.Fatalf("grant exit = %d (stderr: %s)", code, errOut)
	} else if !strings.Contains(out, "Granted") {
		t.Fatalf("grant output = %q", out)
	}
	grants, _ := repo.ListPoliciesForPrincipal(ctx, "user:alice@example.com")
	if len(grants) != 1 || grants[0].ZoneID != "prod-readonly" {
		t.Fatalf("after grant, policies = %+v", grants)
	}

	// Grant again is idempotent (no error, no duplicate).
	if code, out, _ := runZone(t, deps, "grant", "--principal", "user:alice@example.com", "--zone", "prod-readonly"); code != 0 || !strings.Contains(out, "already") {
		t.Fatalf("idempotent grant: code=%d out=%q", code, out)
	}

	// List shows it.
	if code, out, _ := runZone(t, deps, "list"); code != 0 || !strings.Contains(out, "user:alice@example.com") {
		t.Fatalf("list: code=%d out=%q", code, out)
	}

	// Revoke.
	if code, out, errOut := runZone(t, deps, "revoke", "--principal", "user:alice@example.com", "--zone", "prod-readonly"); code != 0 {
		t.Fatalf("revoke exit = %d (stderr: %s)", code, errOut)
	} else if !strings.Contains(out, "Revoked") {
		t.Fatalf("revoke output = %q", out)
	}
	grants, _ = repo.ListPoliciesForPrincipal(ctx, "user:alice@example.com")
	if len(grants) != 0 {
		t.Fatalf("after revoke, policies = %+v", grants)
	}

	// Revoke a non-existent grant is a clean no-op (exit 0).
	if code, out, _ := runZone(t, deps, "revoke", "--principal", "user:alice@example.com", "--zone", "prod-readonly"); code != 0 || !strings.Contains(out, "No grant") {
		t.Fatalf("revoke missing: code=%d out=%q", code, out)
	}
}

func TestZoneGrantUnknownZoneRejected(t *testing.T) {
	deps, _ := zoneTestDeps(t)
	code, _, errOut := runZone(t, deps, "grant", "--principal", "user:alice@example.com", "--zone", "nonexistent")
	if code != 1 {
		t.Fatalf("grant to unknown zone exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "does not exist") {
		t.Fatalf("expected unknown-zone error, got %q", errOut)
	}
}

func TestZoneGrantUnprefixedPrincipalRejected(t *testing.T) {
	deps, _ := zoneTestDeps(t)
	code, _, errOut := runZone(t, deps, "grant", "--principal", "alice", "--zone", "prod-readonly")
	if code != 1 {
		t.Fatalf("grant to unprefixed principal exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "reserved prefix") {
		t.Fatalf("expected reserved-prefix error, got %q", errOut)
	}
}

func TestZoneSvcPrincipalAccepted(t *testing.T) {
	deps, repo := zoneTestDeps(t)
	if code, _, errOut := runZone(t, deps, "grant", "--principal", "svc:ci-bot", "--zone", "dev-full"); code != 0 {
		t.Fatalf("grant to svc principal exit = %d (stderr: %s)", code, errOut)
	}
	grants, _ := repo.ListPoliciesForPrincipal(context.Background(), "svc:ci-bot")
	if len(grants) != 1 {
		t.Fatalf("svc grant policies = %+v", grants)
	}
}
