package rbac_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
)

// admin_principals is the highest-privilege table in the system: a row in it is
// authority on every zone, now and later. Until this file, the rule about who
// may write it existed only as prose in migration 016 and in a doc comment on
// auth.Provisioner — while several lesser seams (the composition-root engine
// rule, the accessor gate, the kubeconfig confinement) were machine-checked.
// The two guards below make the writer set structural.
//
// The set is stated as two layers, because closing only one leaves the other
// open. Layer 1 says nothing writes the table except the shared grant helper.
// Layer 2 says nothing reaches the shared grant helper except the sanctioned
// writers. Together they enumerate every path from a caller to a row.

// adminRepoWriters are the rbac.Repository methods that INSERT into
// admin_principals. RemoveAdmin is deliberately absent: this guard is about how
// authority is CREATED, and removal is already fenced by the last-admin and
// bootstrap-admin guards on the one surface that offers it.
var adminRepoWriters = map[string]bool{
	"AddAdmin":      true,
	"AddFirstAdmin": true,
}

// adminRepoWriterAllowlist is the set of production files that may call an
// adminRepoWriters method. It is exactly one: the shared grant helper. Routing
// every grant through it is what keeps the redundant-policy cleanup from being
// re-implemented (or forgotten) by a caller — the reason internal/api/admin.go
// wraps GrantAdmin rather than calling AddAdmin directly.
var adminRepoWriterAllowlist = map[string]bool{
	"internal/auth/provision.go": true,
}

// adminGrantHelpers are the Provisioner methods that perform a grant.
var adminGrantHelpers = map[string]bool{
	"GrantAdmin":      true,
	"GrantFirstAdmin": true,
}

// adminGrantHelperAllowlist is the set of production files that may call a
// grant helper — the sanctioned writers to the admin roster, named
// structurally rather than counted (D-0032):
//
//   - internal/auth/handlers.go — the OIDC callback's admin_email bootstrap,
//     which is registered ONLY when an identity provider is configured.
//   - internal/api/admin.go     — POST /api/v1/admin/admins, behind requireAdmin.
//   - cmd/joe/admin.go          — `joe admin bootstrap`, offline, service-accounts
//     only, refused once any admin exists.
//
// Adding a fourth is a deliberate act that must fail this test first.
var adminGrantHelperAllowlist = map[string]bool{
	"internal/auth/handlers.go": true,
	"internal/api/admin.go":     true,
	"cmd/joe/admin.go":          true,
}

// TestGuard_AdminPrincipalsWriterSetIsClosed walks the repository and fails on
// any production call site of a grant path outside its allowlist.
//
// It mirrors TestGuard_PolicyEngineConstructedOnlyAtCompositionRoot: same
// module-anchored root, same qualified-and-unqualified callee matching (so a
// call from inside package rbac or package auth is caught too), same exclusion
// of _test.go files, which legitimately grant admin to set up fixtures.
func TestGuard_AdminPrincipalsWriterSetIsClosed(t *testing.T) {
	repoRoot := findRepoRootFromRBAC(t)

	type site struct {
		fileRel string
		line    int
		fn      string
	}
	var repoWriterSites, grantHelperSites []site

	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if path != repoRoot && skipGuardDir(path, d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(repoRoot, path)
		rel = filepath.ToSlash(rel)

		fset := token.NewFileSet()
		f, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			t.Errorf("parse %s: %v", path, parseErr)
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := calleeName(call.Fun)
			line := fset.Position(call.Pos()).Line
			if adminRepoWriters[name] {
				repoWriterSites = append(repoWriterSites, site{rel, line, name})
			}
			if adminGrantHelpers[name] {
				grantHelperSites = append(grantHelperSites, site{rel, line, name})
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	// Anti-vacuity, both layers. A rename, a refactor that inlines the helper,
	// or a walk that silently stopped finding files would otherwise leave this
	// test green while checking nothing at all — the failure mode the existing
	// guards in internal/api and internal/safety all defend against explicitly.
	if len(repoWriterSites) == 0 {
		t.Fatal("found no call sites of AddAdmin/AddFirstAdmin anywhere in production code — " +
			"the guard is not exercising anything; were the repository writers renamed?")
	}
	if len(grantHelperSites) == 0 {
		t.Fatal("found no call sites of GrantAdmin/GrantFirstAdmin anywhere in production code — " +
			"the guard is not exercising anything; was the shared grant helper renamed or inlined?")
	}

	for _, s := range repoWriterSites {
		if adminRepoWriterAllowlist[s.fileRel] {
			continue
		}
		t.Errorf("admin-principals writer violation: %s called at %s:%d — the repository's "+
			"admin_principals writers may be called ONLY from the shared grant helper "+
			"(internal/auth/provision.go). Calling one directly bypasses the redundant-policy "+
			"cleanup that keeps admin authority in exactly one table. Route through "+
			"Provisioner.GrantAdmin or GrantFirstAdmin.", s.fn, s.fileRel, s.line)
	}
	for _, s := range grantHelperSites {
		if adminGrantHelperAllowlist[s.fileRel] {
			continue
		}
		t.Errorf("admin-principals writer violation: %s called at %s:%d — admin_principals is "+
			"the highest-privilege table in the system, and its writer set is closed: the OIDC "+
			"admin_email bootstrap, the requireAdmin-gated REST surface, and the offline "+
			"first-admin CLI. Adding a writer is a deliberate decision: record it in "+
			"docs/project/DECISIONS.md and add the file to adminGrantHelperAllowlist here.",
			s.fn, s.fileRel, s.line)
	}
}

// TestGuard_AdminPrincipalsHasNoRawSQLWriter closes the hole a call-site guard
// cannot see: writing the table with hand-rolled SQL instead of calling a
// repository method. Only the repository itself may form a statement that
// mutates admin_principals.
func TestGuard_AdminPrincipalsHasNoRawSQLWriter(t *testing.T) {
	repoRoot := findRepoRootFromRBAC(t)
	const table = "admin_principals"
	mutations := []string{"INSERT INTO " + table, "UPDATE " + table, "DELETE FROM " + table}

	var found int
	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if path != repoRoot && skipGuardDir(path, d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(repoRoot, path)
		rel = filepath.ToSlash(rel)
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Errorf("read %s: %v", path, readErr)
			return nil
		}
		text := string(body)
		for _, m := range mutations {
			if !strings.Contains(text, m) {
				continue
			}
			found++
			if rel == "internal/rbac/repository.go" {
				continue
			}
			t.Errorf("admin-principals writer violation: %s contains raw SQL %q — only "+
				"internal/rbac/repository.go may form a statement that mutates the admin "+
				"roster, so that every write carries its audit row in the same transaction.",
				rel, m)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if found == 0 {
		t.Fatal("found no admin_principals mutation SQL anywhere in production code — " +
			"the guard is not exercising anything; was the table renamed?")
	}
}

// skipGuardDir reports whether the walk should skip a directory. It excludes the
// usual non-source trees and, crucially, any nested module — a git worktree or a
// vendored copy of this repo checked out inside it would otherwise be walked as
// if it were the tree under test, and its own (possibly stale) call sites
// reported as violations here.
func skipGuardDir(path, name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "dist", "build", ".joe", "testdata":
		return true
	}
	if strings.HasPrefix(name, ".") && name != "." {
		return true
	}
	if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
		return true // nested module: a worktree or vendored copy, not this tree
	}
	return false
}

// TestAddFirstAdmin_ConcurrentInvocationsGrantExactlyOne exercises the property
// the separate repository method exists for: the emptiness test and the write
// are one atomic act.
//
// The two goroutines name DIFFERENT principals on purpose. A repeat of one
// principal would be stopped by the primary key, which would make this pass
// without the NOT EXISTS predicate doing anything — the assertion has to fail
// against a check-then-AddAdmin implementation to be worth anything, and with
// distinct principals it does (both would observe an empty roster and both
// would insert).
//
// The database is file-backed, not ":memory:": store.New pins an unshared
// in-memory DSN to a one-connection pool, which would serialize the two calls
// in the pool and make the test prove nothing about the statement.
//
// Honest limits: this demonstrates that the two calls cannot BOTH grant, under
// real concurrent connections with SQLite's busy_timeout serializing the write
// lock. It does not prove the goroutines interleaved at any particular point on
// any particular run.
func TestAddFirstAdmin_ConcurrentInvocationsGrantExactlyOne(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "joe.db")
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: dsn}, nil)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = s.Close() }()
	if err := s.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := rbac.NewRepositoryWithAudit(s.DB(), s.Driver(),
		audit.NewRepository(s.DB(), s.Driver()))

	principals := []string{"svc:racer-a", "svc:racer-b"}
	results := make([]bool, len(principals))
	errs := make([]error, len(principals))

	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	for i, p := range principals {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait() // release both goroutines as close to together as possible
			results[i], errs[i] = repo.AddFirstAdmin(ctx,
				rbac.Admin{Principal: p, GrantedBy: "cli", Reason: "race"}, "cli:admin-bootstrap")
		}()
	}
	start.Done()
	done.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("AddFirstAdmin(%s): %v", principals[i], err)
		}
	}
	granted := 0
	for _, ok := range results {
		if ok {
			granted++
		}
	}
	if granted != 1 {
		t.Errorf("concurrent AddFirstAdmin granted %d times, want exactly 1", granted)
	}

	var admins, auditRows int
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_principals`).Scan(&admins); err != nil {
		t.Fatalf("count admins: %v", err)
	}
	if admins != 1 {
		t.Errorf("admin_principals holds %d rows after the race, want 1", admins)
	}
	if err := s.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_log WHERE action = ?`, audit.ActionAdminGrant).Scan(&auditRows); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if auditRows != 1 {
		t.Errorf("admin.grant audit rows = %d after the race, want 1 — a refused grant writes none", auditRows)
	}
}
