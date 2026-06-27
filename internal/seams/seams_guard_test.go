package seams_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestSeams_ConstNotVar is the named structural guard for the
// "compile-time const false" property of the four autonomy-seam flags
// declared in seams.go (the session-model design (Phase 0) "incremental-autonomy
// seam pattern" / the Phase 1 decomposition plan Change 12).
//
// The seams are not config-driven, not env-var-driven, not settings-
// backed. Flipping any of them is a compile-time edit in seams.go (or
// equivalently a build-tag rebuild via seams_enabled.go). This guard
// asserts that property in two pieces:
//
//  1. seams.go parses to const declarations only — no `var Joe*Enabled`
//     identifier appears in the file. Each Joe*Enabled identifier must
//     have an untyped-bool literal value (true or false).
//
//  2. Every production .go file in the repository (test files exempted)
//     is scanned for forbidden references that would route the seam
//     decision through runtime state instead of the compile-time
//     constant — `var Joe.*Enabled`, `os.Getenv("JOE_...ENABLED...")`,
//     and `cfg.Joe.*Enabled` patterns.
//
// If a future contributor introduces a runtime override path, this
// guard fails with an explanatory message pointing at the const-only
// design intent. The legitimate way to relax this is to update
// the session-model design's "incremental-autonomy seam pattern" and
// this guard in the same diff.
func TestSeams_ConstNotVar(t *testing.T) {
	repoRoot := findRepoRoot(t)
	seamsPath := filepath.Join(repoRoot, "internal", "seams", "seams.go")

	// (1) Parse seams.go and assert every exported Joe*Enabled
	// identifier is const, with a true/false bool literal.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, seamsPath, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", seamsPath, err)
	}

	idRe := regexp.MustCompile(`^Joe.*Enabled$`)
	seen := map[string]bool{}

	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		switch gen.Tok {
		case token.VAR:
			// A var Joe*Enabled declaration is a structural violation.
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range vs.Names {
					if idRe.MatchString(name.Name) {
						t.Errorf("seams.go declares %q as `var` — autonomy seams must be "+
							"compile-time `const` per the incremental-autonomy seam pattern. "+
							"Change the declaration to `const` or update the session-model "+
							"design (Phase 0) and this guard.", name.Name)
					}
				}
			}
		case token.CONST:
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if !idRe.MatchString(name.Name) {
						continue
					}
					seen[name.Name] = true
					// The const must have a literal value (no type
					// conversions, no function calls). An untyped bool
					// literal parses as an *ast.Ident whose name is
					// "true" or "false".
					if i >= len(vs.Values) {
						t.Errorf("seams.go const %q has no value expression — must be "+
							"a bool literal", name.Name)
						continue
					}
					val := vs.Values[i]
					ident, ok := val.(*ast.Ident)
					if !ok {
						t.Errorf("seams.go const %q has non-literal value %T — must be "+
							"an untyped bool literal (true/false). Computed seams open "+
							"the door to runtime configuration.", name.Name, val)
						continue
					}
					if ident.Name != "true" && ident.Name != "false" {
						t.Errorf("seams.go const %q value = %q — must be `true` or `false`",
							name.Name, ident.Name)
					}
				}
			}
		}
	}

	// Sanity: the four canonical seam flags from Change 12 must all be
	// present. A future contributor removing one without updating the
	// decomposition is a structural change that should fail loudly.
	required := []string{
		"JoeAutonomousDeclareEnabled",
		"JoeAutonomousResolveEnabled",
		"JoeConfirmCloseDispositionEnabled",
		"JoeCaptainTypeEnabled",
	}
	for _, name := range required {
		if !seen[name] {
			t.Errorf("seams.go is missing the required Change 12 seam flag %q. "+
				"The Phase 1 decomposition plan §Change 12 enumerates the four "+
				"flags — removing one is a design change.", name)
		}
	}

	// (2) Walk every production .go file in the repo and assert no
	// runtime override patterns reference the seam names.
	varJoeEnabled := regexp.MustCompile(`\bvar\s+Joe\w*Enabled\b`)
	// os.Getenv("JOE_...ENABLED..."): the seam env var names would
	// embed the substring ENABLED (e.g. JOE_AUTONOMOUS_DECLARE_ENABLED).
	// Match `os.Getenv("..ENABLED..")` and inspect the literal.
	osGetenvRe := regexp.MustCompile(`os\.Getenv\(\s*"([^"]+)"\s*\)`)
	// cfg.JoeXxxEnabled or .JoeXxxEnabled as a struct field reference.
	cfgFieldRe := regexp.MustCompile(`\bcfg\.Joe\w*Enabled\b`)

	skipDir := func(name string) bool {
		// Mirror the directory-skip list from regime_invariant_test.go.
		if name == ".git" || name == "node_modules" || name == "vendor" ||
			name == "dist" || name == "build" || name == ".joe" {
			return true
		}
		if strings.HasPrefix(name, ".") && name != "." {
			return true
		}
		return false
	}

	err = filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if skipDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil // production-code rule; test files exempt
		}
		// seams.go itself is allowed to declare consts that look like
		// the patterns we're scanning for — skip it.
		rel, _ := filepath.Rel(repoRoot, path)
		if rel == filepath.Join("internal", "seams", "seams.go") ||
			rel == filepath.Join("internal", "seams", "seams_enabled.go") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			return nil
		}
		content := string(data)

		// var Joe*Enabled — instant violation.
		if loc := varJoeEnabled.FindStringIndex(content); loc != nil {
			line := lineOf(content, loc[0])
			t.Errorf("%s:%d declares `var Joe...Enabled` — autonomy seams must be "+
				"compile-time `const` in internal/seams/seams.go, never `var` "+
				"in production code. Move the declaration to seams.go (and "+
				"flip the value via -tags=seam_enabled if needed for tests).",
				rel, line)
		}

		// cfg.Joe*Enabled — config-struct override path. Forbidden.
		if loc := cfgFieldRe.FindStringIndex(content); loc != nil {
			line := lineOf(content, loc[0])
			t.Errorf("%s:%d references `cfg.Joe...Enabled` — autonomy seams must "+
				"NOT be threaded through a config struct. The seam is a "+
				"compile-time constant in internal/seams/seams.go.",
				rel, line)
		}

		// os.Getenv with a JOE_*ENABLED* literal. We extract every
		// os.Getenv literal and inspect; this avoids false positives on
		// legitimate JOE_SERVER / JOE_API_KEY env vars.
		for _, m := range osGetenvRe.FindAllStringSubmatchIndex(content, -1) {
			// m[2], m[3] bound the captured literal.
			literal := content[m[2]:m[3]]
			if !strings.HasPrefix(literal, "JOE_") {
				continue
			}
			upper := strings.ToUpper(literal)
			if !strings.Contains(upper, "ENABLED") {
				continue
			}
			line := lineOf(content, m[0])
			t.Errorf("%s:%d calls os.Getenv(%q) — autonomy seams must not be "+
				"env-var-driven. The seam is a compile-time constant in "+
				"internal/seams/seams.go. Remove the env lookup or move the "+
				"flag into seams.go (and document why a runtime path is "+
				"needed by updating the session-model design (Phase 0)).",
				rel, line, literal)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
}

// lineOf returns the 1-indexed line number containing the given byte
// offset in s.
func lineOf(s string, offset int) int {
	if offset > len(s) {
		offset = len(s)
	}
	return 1 + strings.Count(s[:offset], "\n")
}

// findRepoRoot walks up from the working directory until it finds a
// go.mod. Mirrors the helper in internal/api/regime_invariant_test.go.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := wd
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not find go.mod walking up from %s", wd)
	return ""
}
