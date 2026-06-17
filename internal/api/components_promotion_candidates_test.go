package api

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/credential"
	"github.com/jaimegago/joe/internal/rbac"
)

// A002 promotion-candidates surface. The candidates endpoint answers the LIVE
// question — "which references can the admin choose right now?" — sibling to the
// cacheable promotion-requirements SHAPE endpoint. These tests pin: names-only,
// type-prefix-scoped enumeration with zero value leakage; the unwired and
// kubeconfig-exec honest shapes; admin-gating; and the STRUCTURAL invariant that
// env enumeration lives in the provider seam, never the handler.

type candidate struct {
	Label      string `json:"label"`
	EnvVarName string `json:"env_var_name"`
}

type promotionCandidatesBody struct {
	Type         string      `json:"type"`
	Wired        bool        `json:"wired"`
	Kind         string      `json:"kind"`
	Prefix       string      `json:"prefix"`
	Applicable   bool        `json:"applicable"`
	Candidates   []candidate `json:"candidates"`
	ArmableTypes []string    `json:"armable_types"`
}

func getPromotionCandidates(t *testing.T, f *llmadminFixture, id string, principal rbac.Principal) (*promotionCandidatesBody, string) {
	t.Helper()
	w := f.do(http.MethodGet, "/api/v1/components/"+id+"/promotion-candidates", "", principal)
	if w.Code != http.StatusOK {
		t.Fatalf("GET promotion-candidates %s: status=%d body=%s; want 200", id, w.Code, w.Body.String())
	}
	var body promotionCandidatesBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode promotion-candidates body: %v (raw=%s)", err, w.Body.String())
	}
	return &body, w.Body.String()
}

// TestPromotionCandidates_StaticScopedNoValueLeakage is the SAFETY test: with the
// process env seeded with two in-prefix vars, an in-Joe-namespace different-type
// var, and an unrelated var, a github component's candidates are EXACTLY {PROD,
// FOOBAR} (and their composed names) — and the response contains NEITHER the
// unrelated var name NOR any value.
func TestPromotionCandidates_StaticScopedNoValueLeakage(t *testing.T) {
	// Seed the real process env — the production NewStaticProvider reads os.Environ,
	// so this exercises the real enumeration path. t.Setenv auto-restores.
	t.Setenv("JOE_GITHUB_PROD", "ghp_prodsecretvalue")
	t.Setenv("JOE_GITHUB_FOOBAR", "ghp_foobarsecretvalue")
	t.Setenv("JOE_PROMETHEUS_MAIN", "prom_other_type_value")
	t.Setenv("SECRET_DB_PASSWORD", "totally_unrelated_secret")

	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	registerComponent(t, f, "c-gh", "github", `{}`)

	body, raw := getPromotionCandidates(t, f, "c-gh", "user:alice")

	if !body.Wired || body.Kind != "static" || body.Type != "github" {
		t.Fatalf("static shape: wired=%v kind=%q type=%q; want true/static/github", body.Wired, body.Kind, body.Type)
	}
	if !body.Applicable || body.Prefix != "JOE_GITHUB_" {
		t.Fatalf("applicable=%v prefix=%q; want true/JOE_GITHUB_", body.Applicable, body.Prefix)
	}

	gotLabels := []string{}
	for _, c := range body.Candidates {
		gotLabels = append(gotLabels, c.Label)
		if want := "JOE_GITHUB_" + c.Label; c.EnvVarName != want {
			t.Errorf("candidate %+v: env_var_name=%q; want %q", c, c.EnvVarName, want)
		}
	}
	sort.Strings(gotLabels)
	if strings.Join(gotLabels, ",") != "FOOBAR,PROD" {
		t.Fatalf("labels=%v; want exactly [FOOBAR PROD]", gotLabels)
	}

	// No value, and no foreign/unrelated name, anywhere in the raw response.
	for _, banned := range []string{
		"ghp_prodsecretvalue", "ghp_foobarsecretvalue", "prom_other_type_value",
		"totally_unrelated_secret",                  // value
		"JOE_PROMETHEUS_MAIN", "SECRET_DB_PASSWORD", // foreign names
	} {
		if strings.Contains(raw, banned) {
			t.Errorf("promotion-candidates leaked %q in response:\n%s", banned, raw)
		}
	}
}

// TestPromotionCandidates_ArmedComponentStillNamesOnly proves that even after a
// component is armed with a real env_var reference, the candidates endpoint still
// returns only names scoped to the prefix and never a value.
func TestPromotionCandidates_ArmedComponentStillNamesOnly(t *testing.T) {
	t.Setenv("JOE_GITHUB_PROD", "ghp_armedsecret")

	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	registerComponent(t, f, "c-gh", "github", `{}`)

	// Arm it with the env_var reference.
	if w := f.do(http.MethodPost, "/api/v1/components/c-gh/promote",
		`{"env_var":"JOE_GITHUB_PROD"}`, "user:alice"); w.Code != http.StatusOK {
		t.Fatalf("arm c-gh: status=%d body=%s", w.Code, w.Body.String())
	}

	body, raw := getPromotionCandidates(t, f, "c-gh", "user:alice")
	if strings.Contains(raw, "ghp_armedsecret") {
		t.Errorf("candidates leaked the credential value for an armed component:\n%s", raw)
	}
	found := false
	for _, c := range body.Candidates {
		if c.EnvVarName == "JOE_GITHUB_PROD" && c.Label == "PROD" {
			found = true
		}
	}
	if !found {
		t.Errorf("candidates=%+v; want JOE_GITHUB_PROD/PROD present", body.Candidates)
	}
}

// TestPromotionCandidates_KubeconfigExecNotApplicable proves a kubeconfig-exec
// wired component answers honestly: applicable=false, no candidates, no prefix.
func TestPromotionCandidates_KubeconfigExecNotApplicable(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	registerComponent(t, f, "c-k8s", "kubernetes", `{}`)

	body, _ := getPromotionCandidates(t, f, "c-k8s", "user:alice")
	if !body.Wired || body.Kind != "kubeconfig-exec" {
		t.Fatalf("kexec shape: wired=%v kind=%q; want true/kubeconfig-exec", body.Wired, body.Kind)
	}
	if body.Applicable {
		t.Errorf("kubeconfig-exec applicable=true; want false (its reference is a file path, not an enumerable set)")
	}
	if len(body.Candidates) != 0 {
		t.Errorf("kexec candidates=%+v; want empty", body.Candidates)
	}
	if body.Prefix != "" {
		t.Errorf("kexec prefix=%q; want empty", body.Prefix)
	}
}

// TestPromotionCandidates_UnwiredSorted proves an unwired type mirrors
// promotion-requirements: 200, wired:false, sorted armable_types equal to the
// wired-type registry.
func TestPromotionCandidates_UnwiredSorted(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	registerComponent(t, f, "c-dd", "datadog", `{"site":"datadoghq.com"}`)

	body, _ := getPromotionCandidates(t, f, "c-dd", "user:alice")
	if body.Wired || body.Type != "datadog" {
		t.Fatalf("unwired shape: wired=%v type=%q; want false/datadog", body.Wired, body.Type)
	}
	if !sort.StringsAreSorted(body.ArmableTypes) {
		t.Errorf("armable_types not sorted: %v", body.ArmableTypes)
	}
	want := credential.WiredTypes()
	sort.Strings(want)
	if strings.Join(body.ArmableTypes, ",") != strings.Join(want, ",") {
		t.Errorf("armable_types=%v; want %v", body.ArmableTypes, want)
	}
}

// TestPromotionCandidates_AdminGated proves a non-admin caller is refused (403),
// matching the promote and promotion-requirements handlers.
func TestPromotionCandidates_AdminGated(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	registerComponent(t, f, "c-gh", "github", `{}`)

	w := f.do(http.MethodGet, "/api/v1/components/c-gh/promotion-candidates", "", "user:bob")
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin promotion-candidates: status=%d body=%s; want 403", w.Code, w.Body.String())
	}
}

// TestPromotionCandidates_NotFound proves an unknown component is a 404.
func TestPromotionCandidates_NotFound(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	w := f.do(http.MethodGet, "/api/v1/components/does-not-exist/promotion-candidates", "", "user:alice")
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing component: status=%d body=%s; want 404", w.Code, w.Body.String())
	}
}

// TestPromotionCandidates_SeamHeldNoEnvInHandler is the STRUCTURAL guard that env
// enumeration lives in the static provider, NOT the API handler:
// handleComponentPromotionCandidates' body must contain no call to os.LookupEnv or
// os.Environ. The enumeration must be delegated through the provider seam
// (credential.Provider.AvailableReferences). A reviewer can be fooled; the AST
// cannot.
func TestPromotionCandidates_SeamHeldNoEnvInHandler(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "components.go", nil, 0)
	if err != nil {
		t.Fatalf("parse components.go: %v", err)
	}
	var handler *ast.FuncDecl
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if ok && fd.Name.Name == "handleComponentPromotionCandidates" {
			handler = fd
			break
		}
	}
	if handler == nil {
		t.Fatal("handleComponentPromotionCandidates not found in components.go")
	}
	var offenders []string
	ast.Inspect(handler.Body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "os" {
			return true
		}
		if sel.Sel.Name == "LookupEnv" || sel.Sel.Name == "Environ" || sel.Sel.Name == "Getenv" {
			offenders = append(offenders, "os."+sel.Sel.Name)
		}
		return true
	})
	if len(offenders) != 0 {
		t.Errorf("handleComponentPromotionCandidates calls %v — env enumeration must live in the static "+
			"provider (internal/credential), delegated via Provider.AvailableReferences, NOT in the handler.", offenders)
	}
}
