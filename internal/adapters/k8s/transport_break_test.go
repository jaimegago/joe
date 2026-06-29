package k8s

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// transport_break_test.go pins the agent-identity-doc-02 stance STRUCTURALLY,
// scoped to the kubernetes resolution-and-transport path — the non-test .go files
// of THIS package. It is deliberately NOT a tree-wide grep: the dead
// kubeconfig-exec provider in internal/credential still imports clientcmd and is
// kept until slice D, so a whole-tree scan would match it. Scoping to the k8s
// package asserts the live transport, not the dead provider.
//
// The stance: the kubernetes transport ingests no kubeconfig (no clientcmd, no
// rest.InClusterConfig), sets the three REST fields only via our hand-built
// builder, reads no external kubeconfig, and never sets an exec provider, auth
// provider, or impersonation.

// packageProdFiles returns the parsed non-test .go files of this package.
func packageProdFiles(t *testing.T) (*token.FileSet, []*ast.File) {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, name, nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", name, perr)
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		t.Fatal("no production .go files found in the k8s package")
	}
	return fset, files
}

// TestTransport_NoKubeconfigIngestion asserts the package imports no clientcmd —
// the kubernetes transport never ingests a kubeconfig.
func TestTransport_NoKubeconfigIngestion(t *testing.T) {
	_, files := packageProdFiles(t)
	for _, f := range files {
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(path, "client-go/tools/clientcmd") {
				t.Errorf("%s imports %q — the kubernetes transport must build *rest.Config by hand, never ingest a kubeconfig via clientcmd", f.Name.Name, path)
			}
		}
	}
}

// TestTransport_NoForbiddenAuthMechanisms asserts the package never references
// rest.InClusterConfig (the full-config helper that would own host/CA/token), nor
// the ExecProvider / AuthProvider / Impersonate fields. The bearer token's
// in_cluster source reads the mounted token directly (in internal/credential),
// not via InClusterConfig.
func TestTransport_NoForbiddenAuthMechanisms(t *testing.T) {
	forbidden := map[string]string{
		"InClusterConfig": "rest.InClusterConfig owns host, CA, and bearer token — defeats the hand-built-config stance; the in_cluster token is read directly instead",
		"ExecProvider":    "an exec credential plugin must never be set — Joe authenticates only as its own non-human identity",
		"AuthProvider":    "an auth provider (OIDC, etc.) must never be set",
		"Impersonate":     "Joe never impersonates another identity",
	}
	_, files := packageProdFiles(t)
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if reason, bad := forbidden[sel.Sel.Name]; bad {
				t.Errorf("%s references %q — %s", f.Name.Name, sel.Sel.Name, reason)
			}
			return true
		})
	}
}

// TestTransport_RESTFieldsSetOnlyInBuilder asserts that the three REST credential
// fields (Host, BearerToken, and TLSClientConfig CA data) are set ONLY inside
// buildRESTConfig. Any other function assigning them — via a composite-literal key
// or a field assignment — fails this guard, so the three fields cannot be set by
// anything other than our builder.
func TestTransport_RESTFieldsSetOnlyInBuilder(t *testing.T) {
	restFields := map[string]bool{"Host": true, "BearerToken": true, "CAData": true}
	_, files := packageProdFiles(t)
	for _, f := range files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			inBuilder := fn.Name.Name == "buildRESTConfig"
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch e := n.(type) {
				case *ast.KeyValueExpr:
					if id, ok := e.Key.(*ast.Ident); ok && restFields[id.Name] && !inBuilder {
						t.Errorf("%s sets REST field %q outside buildRESTConfig — the three fields must be set only by our builder", fn.Name.Name, id.Name)
					}
				case *ast.AssignStmt:
					for _, lhs := range e.Lhs {
						if sel, ok := lhs.(*ast.SelectorExpr); ok && restFields[sel.Sel.Name] && !inBuilder {
							t.Errorf("%s assigns REST field %q outside buildRESTConfig", fn.Name.Name, sel.Sel.Name)
						}
					}
				}
				return true
			})
		}
	}
}
