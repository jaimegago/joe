package k8s

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// transport_break_test.go pins the agent-identity-doc-02 stance STRUCTURALLY: the
// kubernetes transport ingests no kubeconfig (no clientcmd, no rest.InClusterConfig),
// sets the three REST fields only via our hand-built builder, reads no external
// kubeconfig, and never sets an exec provider, auth provider, or impersonation.
//
// clientcmd absence is asserted three ways, strengthened in agent-identity-doc-04
// once the dead kubeconfig-exec provider (which itself imported clientcmd) was
// deleted from internal/credential:
//   - THIS package (the live kubernetes transport) imports no clientcmd;
//   - internal/credential (credential resolution) imports no clientcmd — newly
//     true post-deletion, previously impossible while kubeconfig_exec.go lived there;
//   - repo-wide, the ONLY production packages that may import clientcmd are the two
//     legitimately kubeconfig-shaped adapters, helm and nginx-ingress. clientcmd
//     remains a legitimate module dependency (it is NOT removed) — this guard just
//     confines it, catching any new clientcmd creep into the transport/credential path.

// parseProdFilesIn returns the parsed non-test .go files of the given directory.
func parseProdFilesIn(t *testing.T, dir string) (*token.FileSet, []*ast.File) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", name, perr)
		}
		files = append(files, f)
	}
	return fset, files
}

// packageProdFiles returns the parsed non-test .go files of this package.
func packageProdFiles(t *testing.T) (*token.FileSet, []*ast.File) {
	t.Helper()
	fset, files := parseProdFilesIn(t, ".")
	if len(files) == 0 {
		t.Fatal("no production .go files found in the k8s package")
	}
	return fset, files
}

// importsClientcmd reports whether any of the parsed files imports the client-go
// kubeconfig loader (clientcmd).
func importsClientcmd(files []*ast.File) bool {
	for _, f := range files {
		for _, imp := range f.Imports {
			if strings.Contains(strings.Trim(imp.Path.Value, `"`), "client-go/tools/clientcmd") {
				return true
			}
		}
	}
	return false
}

// TestTransport_NoKubeconfigIngestion asserts the package imports no clientcmd —
// the kubernetes transport never ingests a kubeconfig.
func TestTransport_NoKubeconfigIngestion(t *testing.T) {
	_, files := packageProdFiles(t)
	if importsClientcmd(files) {
		t.Error("the k8s transport package imports clientcmd — it must build *rest.Config by hand, never ingest a kubeconfig")
	}
}

// TestCredentialPackage_NoKubeconfigIngestion asserts internal/credential imports
// no clientcmd. This became assertable in agent-identity-doc-04: the kubeconfig-exec
// provider that lived here was the only clientcmd importer in the package and has
// been deleted. Credential resolution now mints/reads bearer tokens only — never a
// kubeconfig.
func TestCredentialPackage_NoKubeconfigIngestion(t *testing.T) {
	_, files := parseProdFilesIn(t, filepath.Join("..", "..", "credential"))
	if len(files) == 0 {
		t.Fatal("no production .go files found in internal/credential")
	}
	if importsClientcmd(files) {
		t.Error("internal/credential imports clientcmd — credential resolution must not ingest a kubeconfig")
	}
}

// TestNoClientcmdOutsideAllowedAdapters is the repo-wide confinement guard: the
// ONLY production packages permitted to import clientcmd are the two kubeconfig-
// shaped adapters (helm, nginx). Any other production file importing it fails here.
// clientcmd stays a legitimate dependency (helm/nginx use it); this only prevents
// it re-entering the transport or credential path.
func TestNoClientcmdOutsideAllowedAdapters(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	allowed := map[string]bool{
		filepath.Join("internal", "adapters", "packaging", "helm"):   true,
		filepath.Join("internal", "adapters", "networking", "nginx"): true,
	}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip node_modules and any hidden dir (.git, and .claude/worktrees,
			// which holds sibling git worktrees whose stale copies would be scanned).
			if d.Name() == "node_modules" || (len(d.Name()) > 1 && strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if perr != nil {
			t.Fatalf("parse %s: %v", path, perr)
		}
		if !importsClientcmd([]*ast.File{f}) {
			return nil
		}
		rel, _ := filepath.Rel(root, filepath.Dir(path))
		if !allowed[rel] {
			t.Errorf("%s imports clientcmd — only helm and nginx-ingress may; clientcmd must not re-enter the transport/credential path", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
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
