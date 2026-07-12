package api

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/skills"
)

// Skills HTTP surface authorization (D-0075). The mutating routes — reload,
// approve, reject — are admin-gated via server.requireAdmin exactly as the RBAC
// and LLM admin surfaces are; GET /skills stays authenticated-only by design.
// These cases pin both halves: the gate on the mutators (non-admin 403 with the
// operation NOT performed; admin 200 with it performed; auth-disabled permits)
// and the deliberate list exemption (a non-admin can still read the roster).

// fakeSkillGit is a Git that synthesizes a single SKILL.md on Clone so the
// skills Manager can produce a quarantined install without a real git remote.
// The skill name is looked up per repo URL; Update is a no-op the seed path
// never exercises.
type fakeSkillGit struct {
	repoSkill map[string]string
}

func (g fakeSkillGit) Clone(_ context.Context, repo, _, _, dest string) (string, error) {
	name := g.repoSkill[repo]
	if name == "" {
		name = "unnamed"
	}
	content := fmt.Sprintf("---\nname: %s\ndescription: %s test skill\n---\nbody\n", name, name)
	if err := os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte(content), 0o600); err != nil {
		return "", err
	}
	return "deadbeef", nil
}

func (g fakeSkillGit) Update(_ context.Context, _, _ string) (string, error) {
	return "deadbeef", nil
}

type skillsAdminFixture struct {
	t       *testing.T
	manager *skills.Manager
	watcher *skills.Watcher
	server  *Server
	mux     *http.ServeMux
	rbac    rbac.Repository
}

// newSkillsAdminFixture wires a real store + RBAC repository and a skills
// Manager seeded with two quarantined installs ("pending-a" for the approve
// path, "pending-b" for the reject path) plus a live Watcher, all behind the
// registered routes. DefaultPolicy denies every source, so a seeded install
// lands in quarantine rather than active.
func newSkillsAdminFixture(t *testing.T, rbacEnabled bool) *skillsAdminFixture {
	t.Helper()
	s := newLLMAdminStore(t)
	rbacRepo := rbac.NewRepository(s.DB(), s.Driver())

	root := t.TempDir()
	git := fakeSkillGit{repoSkill: map[string]string{
		"https://example.com/pending-a.git": "pending-a",
		"https://example.com/pending-b.git": "pending-b",
	}}
	mgr := skills.NewManager(root, git).WithPolicy(skills.DefaultPolicy())
	for _, repo := range []string{
		"https://example.com/pending-a.git",
		"https://example.com/pending-b.git",
	} {
		in, err := mgr.Install(context.Background(), repo, "", "")
		if err != nil {
			t.Fatalf("seed install %s: %v", repo, err)
		}
		if !in.IsQuarantined() {
			t.Fatalf("seed install %s: expected quarantine, got status=%q", repo, in.Status)
		}
	}

	watcher, err := skills.NewWatcher(root, skills.NewAtomicRouter(nil))
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	t.Cleanup(func() { _ = watcher.Close() })

	cfg := &config.Config{}
	metrics := observability.NewMetrics()
	services := core.New(cfg, s, s.DB(), s.Driver(), nil, metrics)
	services.RBAC = rbacRepo
	services.RBACEnabled = rbacEnabled
	services.SkillsManager = mgr
	services.SkillsWatcher = watcher

	srv := New(services)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	return &skillsAdminFixture{
		t:       t,
		manager: mgr,
		watcher: watcher,
		server:  srv,
		mux:     mux,
		rbac:    rbacRepo,
	}
}

func (f *skillsAdminFixture) markAdmin(principal string) {
	f.t.Helper()
	if err := f.rbac.AddAdmin(context.Background(), rbac.Admin{
		Principal: principal, GrantedBy: "test", Reason: "test fixture",
	}, "test"); err != nil {
		f.t.Fatalf("AddAdmin %q: %v", principal, err)
	}
}

func (f *skillsAdminFixture) do(method, path, body string, principal rbac.Principal) *httptest.ResponseRecorder {
	f.t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	if principal != "" {
		req = req.WithContext(rbac.WithPrincipal(req.Context(), principal))
	}
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, req)
	return w
}

// installStatus reports the lockfile status of the install carrying `name`
// ("active"/"quarantined"), and whether it is still present at all.
func (f *skillsAdminFixture) installStatus(name string) (status string, found bool) {
	f.t.Helper()
	installs, err := f.manager.List()
	if err != nil {
		f.t.Fatalf("List: %v", err)
	}
	for _, in := range installs {
		for _, sk := range in.Skills {
			if sk.Name == name {
				st := in.Status
				if st == "" {
					st = skills.InstallStatusActive
				}
				return st, true
			}
		}
	}
	return "", false
}

func TestSkillsMutators_NonAdminForbiddenAndInert(t *testing.T) {
	f := newSkillsAdminFixture(t, true)

	wReload := f.do(http.MethodPost, "/api/v1/skills/reload", "", "user:bob")
	if wReload.Code != http.StatusForbidden {
		t.Fatalf("reload non-admin: status=%d body=%s; want 403", wReload.Code, wReload.Body.String())
	}
	if !strings.Contains(wReload.Body.String(), `"forbidden"`) {
		t.Errorf("reload non-admin body missing forbidden error code: %s", wReload.Body.String())
	}
	if lr := f.watcher.LastReload(); lr.Trigger != "" {
		t.Errorf("reload ran for a forbidden caller: LastReload trigger=%q", lr.Trigger)
	}

	wApprove := f.do(http.MethodPost, "/api/v1/skills/approve", `{"name":"pending-a"}`, "user:bob")
	if wApprove.Code != http.StatusForbidden {
		t.Fatalf("approve non-admin: status=%d body=%s; want 403", wApprove.Code, wApprove.Body.String())
	}
	if st, found := f.installStatus("pending-a"); !found || st != skills.InstallStatusQuarantined {
		t.Errorf("approve forbidden but pending-a found=%v status=%q; want still quarantined", found, st)
	}

	wReject := f.do(http.MethodPost, "/api/v1/skills/reject", `{"name":"pending-b"}`, "user:bob")
	if wReject.Code != http.StatusForbidden {
		t.Fatalf("reject non-admin: status=%d body=%s; want 403", wReject.Code, wReject.Body.String())
	}
	if st, found := f.installStatus("pending-b"); !found || st != skills.InstallStatusQuarantined {
		t.Errorf("reject forbidden but pending-b found=%v status=%q; want still quarantined", found, st)
	}
}

func TestSkillsMutators_AdminSucceeds(t *testing.T) {
	f := newSkillsAdminFixture(t, true)
	f.markAdmin("user:alice")

	wReload := f.do(http.MethodPost, "/api/v1/skills/reload", "", "user:alice")
	if wReload.Code != http.StatusOK {
		t.Fatalf("reload admin: status=%d body=%s; want 200", wReload.Code, wReload.Body.String())
	}
	if lr := f.watcher.LastReload(); lr.Trigger == "" {
		t.Error("reload did not run for an admin caller")
	}

	wApprove := f.do(http.MethodPost, "/api/v1/skills/approve", `{"name":"pending-a"}`, "user:alice")
	if wApprove.Code != http.StatusOK {
		t.Fatalf("approve admin: status=%d body=%s; want 200", wApprove.Code, wApprove.Body.String())
	}
	if st, found := f.installStatus("pending-a"); !found || st != skills.InstallStatusActive {
		t.Errorf("after admin approve pending-a found=%v status=%q; want active", found, st)
	}

	wReject := f.do(http.MethodPost, "/api/v1/skills/reject", `{"name":"pending-b"}`, "user:alice")
	if wReject.Code != http.StatusOK {
		t.Fatalf("reject admin: status=%d body=%s; want 200", wReject.Code, wReject.Body.String())
	}
	if _, found := f.installStatus("pending-b"); found {
		t.Errorf("after admin reject pending-b still present; want removed")
	}
}

func TestSkillsMutators_AuthDisabledPermits(t *testing.T) {
	f := newSkillsAdminFixture(t, false)

	wReload := f.do(http.MethodPost, "/api/v1/skills/reload", "", "user:nobody")
	if wReload.Code != http.StatusOK {
		t.Fatalf("reload auth-disabled: status=%d body=%s; want 200", wReload.Code, wReload.Body.String())
	}
	wApprove := f.do(http.MethodPost, "/api/v1/skills/approve", `{"name":"pending-a"}`, "user:nobody")
	if wApprove.Code != http.StatusOK {
		t.Fatalf("approve auth-disabled: status=%d body=%s; want 200", wApprove.Code, wApprove.Body.String())
	}
}

func TestSkillsList_NonAdminAllowed(t *testing.T) {
	f := newSkillsAdminFixture(t, true)
	w := f.do(http.MethodGet, "/api/v1/skills", "", "user:bob")
	if w.Code != http.StatusOK {
		t.Fatalf("list non-admin: status=%d body=%s; want 200", w.Code, w.Body.String())
	}
	// The quarantine roster is visible to any authenticated teammate by design.
	if !strings.Contains(w.Body.String(), "pending-a") {
		t.Errorf("list response missing quarantined skill: %s", w.Body.String())
	}
}

// TestSkillsRoutes_MutatorsRequireAdminGate is the structural guard analogous to
// TestAdminRoutes_AllRequireAdminGate (admin_gate_guard_test.go), scoped to the
// skillsHandler receiver: every mutating route (POST reload/approve/reject) must
// call requireAdmin in its body, and handleList is the single explicit exemption
// (GET, authenticated-only by design — D-0075). A future mutating skills route
// registered without the gate fails the build here.
func TestSkillsRoutes_MutatorsRequireAdminGate(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "skills.go", nil, 0)
	if err != nil {
		t.Fatalf("parse skills.go: %v", err)
	}

	mutating, reads := registeredSkillsHandlersByMethod(t, f)
	gated := gatedSkillsHandlers(f)

	if len(mutating) == 0 {
		t.Fatal("found no mutating skills routes registered in registerSkillsRoutes — " +
			"the guard cannot be vacuously green; did the route-registration shape change?")
	}

	var ungated []string
	for _, name := range mutating {
		if !gated[name] {
			ungated = append(ungated, name)
		}
	}
	sort.Strings(ungated)
	for _, name := range ungated {
		t.Errorf("mutating skills handler %q is registered but its body never calls "+
			"requireAdmin — add `if _, gated := h.server.requireAdmin(w, r); gated { return }` "+
			"at the top of %s, the same gate the admin surface uses (D-0075).", name, name)
	}

	// handleList is the sole deliberate exemption: read-only, authenticated-only.
	if len(reads) != 1 || reads[0] != "handleList" {
		t.Errorf("non-mutating skills routes = %v; want exactly [handleList] as the single exemption", reads)
	}
	if gated["handleList"] {
		t.Error("handleList must NOT be admin-gated — GET /skills is authenticated-only by design (D-0075)")
	}
}

// registeredSkillsHandlersByMethod returns the skillsHandler method names wired
// into the mux by registerSkillsRoutes, partitioned by the HTTP method that
// leads each route pattern. The handler argument is always the last arg and has
// the form `h.<method>`; the pattern is the first arg, either a bare string
// literal or an fmt.Sprintf whose format string carries the method.
func registeredSkillsHandlersByMethod(t *testing.T, f *ast.File) (mutating, reads []string) {
	t.Helper()
	var reg *ast.FuncDecl
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if ok && fd.Name.Name == "registerSkillsRoutes" {
			reg = fd
			break
		}
	}
	if reg == nil {
		t.Fatal("registerSkillsRoutes not found in skills.go — the guard relies on it " +
			"being the single skills-route registration site")
	}

	ast.Inspect(reg.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "HandleFunc" || len(call.Args) < 2 {
			return true
		}
		hsel, ok := call.Args[len(call.Args)-1].(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := hsel.X.(*ast.Ident)
		if !ok || ident.Name != "h" {
			return true
		}
		switch methodFromPattern(call.Args[0]) {
		case "GET":
			reads = append(reads, hsel.Sel.Name)
		case "POST", "PUT", "PATCH", "DELETE":
			mutating = append(mutating, hsel.Sel.Name)
		}
		return true
	})
	sort.Strings(mutating)
	sort.Strings(reads)
	return mutating, reads
}

// methodFromPattern extracts the leading HTTP method token from a route-pattern
// argument — either a string literal ("GET /x") or an fmt.Sprintf whose first
// argument is the format string ("GET %s/x").
func methodFromPattern(arg ast.Expr) string {
	var lit *ast.BasicLit
	switch a := arg.(type) {
	case *ast.BasicLit:
		lit = a
	case *ast.CallExpr:
		if len(a.Args) > 0 {
			if bl, ok := a.Args[0].(*ast.BasicLit); ok {
				lit = bl
			}
		}
	}
	if lit == nil || lit.Kind != token.STRING {
		return ""
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return ""
	}
	if i := strings.IndexByte(s, ' '); i > 0 {
		return s[:i]
	}
	return ""
}

// gatedSkillsHandlers returns the set of skillsHandler method names whose body
// contains a call to a selector named requireAdmin.
func gatedSkillsHandlers(f *ast.File) map[string]bool {
	gated := map[string]bool{}
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || fd.Body == nil {
			continue
		}
		if !isSkillsHandlerReceiver(fd.Recv) {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "requireAdmin" {
				gated[fd.Name.Name] = true
				return false
			}
			return true
		})
	}
	return gated
}

// isSkillsHandlerReceiver reports whether the receiver list is `(h *skillsHandler)`.
func isSkillsHandlerReceiver(recv *ast.FieldList) bool {
	if recv == nil || len(recv.List) != 1 {
		return false
	}
	star, ok := recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	ident, ok := star.X.(*ast.Ident)
	return ok && ident.Name == "skillsHandler"
}
