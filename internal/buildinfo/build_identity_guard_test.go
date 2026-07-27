package buildinfo_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// CLAUDE.md states that internal/buildinfo is the single source of build truth
// and that no other package declares build-identity vars. Until this file that
// claim was prose, and it had two live counterexamples: internal/mcp declared
// its own serverVersion and internal/observability its own serviceVersion, both
// frozen at "0.1.0" and both reported outward — in the MCP handshake and on the
// OpenTelemetry resource. They read correctly at v0.1.0 by coincidence and
// falsified themselves at v0.2.0.
//
// A hand-written version literal is exactly the kind of thing that re-enters,
// because writing one is easier than importing the package. This guard makes the
// claim structural.
//
// Scope discipline. The guard fires only on the intersection of two conditions:
// an identifier from a CLOSED set of names that denote THIS BINARY's own build
// identity, AND a value that looks like a build identity. Both halves matter.
// Names outside the set are untouched however version-shaped their value, which
// is what keeps protocol versions (mcp's protocol version, apiVersion = "v1"),
// schema versions, dependency pins, and semconv package versions out of it. The
// set is deliberately not a "contains Version" substring rule: "protocolVersion"
// and "appVersion" (a Helm chart's, in internal/adapters/packaging/helm) both
// end in Version and neither is Joe's build identity.

// buildIdentityNames is the closed set of identifiers that name this binary's
// own build identity, lowercased for comparison. Adding a name here widens what
// the guard forbids elsewhere; it is never the way to make a violation pass.
var buildIdentityNames = map[string]identityClass{
	"version":        classVersion,
	"serverversion":  classVersion,
	"serviceversion": classVersion,
	"appversion":     classVersion, // only when version-shaped; a chart's AppVersion is a struct field, not a ValueSpec
	"binaryversion":  classVersion,
	"buildversion":   classVersion,
	"daemonversion":  classVersion,
	"joeversion":     classVersion,
	"commit":         classOpaque,
	"gitcommit":      classOpaque,
	"commithash":     classOpaque,
	"buildcommit":    classOpaque,
	"buildtime":      classOpaque,
	"builddate":      classOpaque,
}

type identityClass int

const (
	// classVersion names carry a human version string, and other things are
	// legitimately called "version" too, so a value shape is also required.
	classVersion identityClass = iota
	// classOpaque names (commit, build time) have no meaning outside build
	// identity, so any constant string value under such a name is a violation.
	classOpaque
)

// versionShaped matches a literal semantic-version-like value: an optional "v",
// then at least major.minor. "v1", "1", "dev", "none" and "unknown" do not
// match — the last three are buildinfo's own uninjected defaults.
var versionShaped = regexp.MustCompile(`^v?\d+\.\d+`)

// buildIdentityOwner is the one package permitted to declare these. It is the
// ldflags -X injection target, addressed by full import path.
const buildIdentityOwner = "internal/buildinfo"

// TestGuard_BuildIdentityDeclaredOnlyInBuildinfo walks the repository and fails
// on any production const or var outside internal/buildinfo that declares a
// build-identity value as a literal.
//
// It mirrors the existing structural guards (the composition-root policy-engine
// guard, the admin_principals writer guard, the kubeconfig transport break-test):
// module-anchored root, nested modules and non-source trees skipped, _test.go
// excluded, and an explicit anti-vacuity check so a rename or a walk that
// silently found nothing fails loudly instead of passing while checking nothing.
func TestGuard_BuildIdentityDeclaredOnlyInBuildinfo(t *testing.T) {
	repoRoot := findRepoRootFromBuildinfo(t)

	type site struct {
		fileRel string
		line    int
		name    string
		value   string
	}
	var violations []site
	matched := 0

	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if path != repoRoot && skipBuildIdentityGuardDir(path, d.Name()) {
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
			decl, ok := n.(*ast.GenDecl)
			if !ok || (decl.Tok != token.CONST && decl.Tok != token.VAR) {
				return true
			}
			for _, spec := range decl.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, ident := range vs.Names {
					class, named := buildIdentityNames[strings.ToLower(ident.Name)]
					if !named || i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					value, unquoteErr := strconv.Unquote(lit.Value)
					if unquoteErr != nil {
						continue
					}
					matched++
					if class == classVersion && !versionShaped.MatchString(value) {
						continue
					}
					if strings.HasPrefix(rel, buildIdentityOwner+"/") {
						continue
					}
					violations = append(violations, site{
						fileRel: rel,
						line:    fset.Position(ident.Pos()).Line,
						name:    ident.Name,
						value:   value,
					})
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	// Anti-vacuity. buildinfo's own Version/Commit/BuildTime defaults are
	// build-identity-named string literals, so a working matcher always sees at
	// least those. Zero means the walk, the parse, or the name set stopped
	// working and the guard is checking nothing.
	if matched == 0 {
		t.Fatal("found no build-identity-named string constants anywhere in production code, " +
			"not even buildinfo's own Version/Commit/BuildTime defaults — the guard is not " +
			"exercising anything; were they renamed, or did the walk stop finding files?")
	}

	for _, v := range violations {
		t.Errorf("build-identity violation: %s = %q declared at %s:%d — %s is the single "+
			"source of build truth and the sole ldflags -X injection target. A literal here "+
			"cannot move with a release, so it reports a version the artifact never was (this "+
			"is what internal/mcp and internal/observability did, both frozen at 0.1.0). Read "+
			"buildinfo.Get() instead. If the value is NOT this binary's build identity — a "+
			"protocol version, a schema version, a dependency pin — give it a name that says "+
			"so rather than adding it to buildIdentityNames here.",
			v.name, v.value, v.fileRel, v.line, buildIdentityOwner)
	}
}

// findRepoRootFromBuildinfo walks up from this package until it finds the module
// root, so the guard covers the whole tree regardless of where `go test` ran.
func findRepoRootFromBuildinfo(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repository root (no go.mod found walking up from the test's working directory)")
		}
		dir = parent
	}
}

// skipBuildIdentityGuardDir excludes the usual non-source trees and, crucially,
// any nested module — a git worktree or vendored copy of this repo checked out
// inside it would otherwise be walked as if it were the tree under test, and its
// own (possibly stale) declarations reported as violations here.
func skipBuildIdentityGuardDir(path, name string) bool {
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
