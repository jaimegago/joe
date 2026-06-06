package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Stream G structural guards for the /api/v1/llm/ admin surface.
//
// These are the siblings of admin_gate_guard_test.go and
// admin_audit_guard_test.go (D-0012 / D-0013), which parse admin.go and pin
// the gate+audit invariant for the RBAC admin surface under /api/v1/admin/.
// The Stream G structural-guard gap (DECISIONS.md D-0014) was the exact
// regression class those guards close, STILL OPEN for
// the LLM admin surface: the settings/usage mutators live on
// llmSettingsHandler / llmUsageHandler in llmsettings.go / llmusageapi.go,
// registered under /api/v1/llm/ — outside the parse scope of both admin
// guards. A future `POST /api/v1/llm/settings/*` mutator added without
// requireAdmin (or without routing through the audited MutationService) would
// fail NO structural test. These guards close that gap.
//
// Two invariants, mirroring the two admin guards:
//
//   - TestLLMAdminRoutes_MutatorsRequireAdminGate — every mutating route
//     (POST/PUT/DELETE/PATCH) registered under /api/v1/llm/ must admin-gate
//     via server.requireAdmin. GET reads are intentionally open per Stream G
//     design (settings/usage values are policy knobs, not credentials) — with
//     ONE exception: the per-principal usage breakdown IS required to
//     admin-gate (it exposes other principals' usage), so the guard treats any
//     route whose pattern contains "per-principal" as gate-required regardless
//     of verb.
//
//   - TestLLMAdminRoutes_MutatorsAudit — every mutating route must write an
//     audit row. Unlike the admin surface (which calls recordAdminAudit
//     directly in the handler), the Stream G mutators route through
//     services.LLMSettings (the MutationService), which persists the value AND
//     writes the audit row in one transaction via audit.Repository.InsertTx
//     with Kind=KindLLMSettingsMutation (DECISIONS.md D-0014,
//     internal/llmsettings/service.go). So the structural property is:
//     a mutating handler's body must invoke a method on
//     h.server.services.LLMSettings — the single audited mutation path. A
//     mutator that writes through any other path (or none) is flagged.

// llmRoute is one HandleFunc registration on the LLM surface.
type llmRoute struct {
	handler string // adapter method name, e.g. "handleSetCostLimit"
	verb    string // HTTP verb parsed from the pattern, e.g. "POST"
	pattern string // raw pattern literal, e.g. "POST %s/llm/settings/active-model"
}

// llmGuardFiles is the set of source files holding the LLM admin surface.
// Bare filenames parse because the test working dir IS this package's
// directory — the same locality trick admin_gate_guard_test.go uses.
var llmGuardFiles = []string{"llmsettings.go", "llmusageapi.go"}

func parseLLMGuardFiles(t *testing.T) []*ast.File {
	t.Helper()
	fset := token.NewFileSet()
	files := make([]*ast.File, 0, len(llmGuardFiles))
	for _, name := range llmGuardFiles {
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files = append(files, f)
	}
	return files
}

func TestLLMAdminRoutes_MutatorsRequireAdminGate(t *testing.T) {
	files := parseLLMGuardFiles(t)

	routes := registeredLLMRoutes(t, files)
	gated := gatedLLMHandlers(files)

	if len(routes) == 0 {
		t.Fatal("found no routes registered in the registerLLM*Routes functions — " +
			"the guard cannot be vacuously green; did the route-registration " +
			"shape in llmsettings.go / llmusageapi.go change?")
	}

	// Sanity: at least one route must be gate-required, else the guard is
	// vacuously green (e.g. if every route were misread as an open GET).
	var gateRequired int
	var ungated []string
	for _, rt := range routes {
		if !llmRouteRequiresAdmin(rt) {
			continue
		}
		gateRequired++
		if !gated[rt.handler] {
			ungated = append(ungated, rt.handler)
		}
	}
	if gateRequired == 0 {
		t.Fatal("found no gate-required LLM routes (mutations or per-principal) — " +
			"the guard cannot be vacuously green; did the route shapes change?")
	}
	sort.Strings(ungated)

	for _, name := range ungated {
		t.Errorf("LLM handler %q is registered under /api/v1/llm/ for a "+
			"mutating (or per-principal) route but its body never calls "+
			"requireAdmin — this re-opens the privilege-escalation class "+
			"D-0012 closed for the admin surface, left open for the Stream G "+
			"surface (DECISIONS.md D-0014). Add "+
			"`if _, gated := h.server.requireAdmin(w, r); gated { return }` at "+
			"the top of %s, the same gate the admin and existing LLM mutators "+
			"use (admingate.go). Do NOT route an LLM admin endpoint around the "+
			"gate.", name, name)
	}
}

func TestLLMAdminRoutes_MutatorsAudit(t *testing.T) {
	files := parseLLMGuardFiles(t)

	routes := registeredLLMRoutes(t, files)
	audited := auditedLLMHandlers(files)

	if len(routes) == 0 {
		t.Fatal("found no routes registered in the registerLLM*Routes functions — " +
			"the guard cannot be vacuously green; did the route-registration " +
			"shape in llmsettings.go / llmusageapi.go change?")
	}

	var mutators int
	var unaudited []string
	for _, rt := range routes {
		if !isMutatingVerb(rt.verb) {
			continue // reads do not mutate state, so nothing to audit
		}
		mutators++
		if !audited[rt.handler] {
			unaudited = append(unaudited, rt.handler)
		}
	}
	if mutators == 0 {
		t.Fatal("found no mutating LLM routes — the guard cannot be vacuously " +
			"green; did the route shapes change?")
	}
	sort.Strings(unaudited)

	for _, name := range unaudited {
		t.Errorf("LLM handler %q is registered under /api/v1/llm/ for a mutating "+
			"route but its body never invokes a method on "+
			"h.server.services.LLMSettings — the MutationService is the single "+
			"path that persists the change AND writes the audit row "+
			"(Kind=KindLLMSettingsMutation) in one transaction "+
			"(internal/llmsettings/service.go). Routing the mutation around it "+
			"re-opens the audit gap D-0013 closed for the admin surface, left "+
			"open for the Stream G surface (DECISIONS.md D-0014). "+
			"Persist the change through services.LLMSettings in %s; do NOT "+
			"write the value directly.", name, name)
	}
}

// registeredLLMRoutes returns every HandleFunc registration found inside the
// registerLLM*Routes functions across the parsed files. Each registration is
// `mux.HandleFunc(fmt.Sprintf("<VERB> %s/llm/...", prefix), h.<method>)`; we
// pull the verb+pattern from the format-string literal and the handler name
// from the last argument selector (h.<method>).
func registeredLLMRoutes(t *testing.T, files []*ast.File) []llmRoute {
	t.Helper()
	var routes []llmRoute
	var foundRegistrar bool

	for _, f := range files {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			// The route registrars are methods on *Server named
			// registerLLM<...>Routes (registerLLMSettingsRoutes /
			// registerLLMUsageRoutes). Match by name shape so a new LLM
			// registrar file is covered automatically once added to
			// llmGuardFiles.
			if !strings.HasPrefix(fd.Name.Name, "registerLLM") || !strings.HasSuffix(fd.Name.Name, "Routes") {
				continue
			}
			foundRegistrar = true
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "HandleFunc" || len(call.Args) < 2 {
					return true
				}
				verb, pattern, ok := patternFromHandleFuncArg(call.Args[0])
				if !ok {
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
				routes = append(routes, llmRoute{handler: hsel.Sel.Name, verb: verb, pattern: pattern})
				return true
			})
		}
	}

	if !foundRegistrar {
		t.Fatal("no registerLLM*Routes function found in llmsettings.go / " +
			"llmusageapi.go — the guard relies on those being the single LLM " +
			"route-registration sites")
	}
	return routes
}

// patternFromHandleFuncArg extracts the HTTP verb and the raw pattern literal
// from the first HandleFunc argument, which is
// `fmt.Sprintf("<VERB> %s/...", prefix)`. Returns ok=false if the argument is
// not a Sprintf over a string literal in the expected shape.
func patternFromHandleFuncArg(arg ast.Expr) (verb, pattern string, ok bool) {
	call, ok := arg.(*ast.CallExpr)
	if !ok || len(call.Args) == 0 {
		return "", "", false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", "", false
	}
	unq, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", "", false
	}
	fields := strings.Fields(unq)
	if len(fields) == 0 {
		return "", "", false
	}
	return fields[0], unq, true
}

func isMutatingVerb(verb string) bool {
	switch verb {
	case "POST", "PUT", "DELETE", "PATCH":
		return true
	default:
		return false
	}
}

// llmRouteRequiresAdmin reports whether a route must admin-gate: every
// mutating verb, plus the per-principal usage breakdown (a GET that exposes
// other principals' usage and so is admin-only by Stream G design,
// DECISIONS.md D-0014).
func llmRouteRequiresAdmin(rt llmRoute) bool {
	if isMutatingVerb(rt.verb) {
		return true
	}
	return strings.Contains(rt.pattern, "per-principal")
}

// gatedLLMHandlers returns the set of LLM handler method names whose body
// contains a call to a selector named requireAdmin. Mirrors
// gatedAdminHandlers in admin_gate_guard_test.go.
func gatedLLMHandlers(files []*ast.File) map[string]bool {
	gated := map[string]bool{}
	forEachLLMHandlerMethod(files, func(fd *ast.FuncDecl) {
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
	})
	return gated
}

// auditedLLMHandlers returns the set of LLM handler method names whose body
// invokes a method on h.server.services.LLMSettings — the single audited
// mutation path (MutationService). The structural shape detected is a call
// `<...>.LLMSettings.<Method>(...)`: a CallExpr whose Fun is a SelectorExpr
// whose receiver (.X) is itself a SelectorExpr selecting the field
// "LLMSettings". A bare nil-check (`... .LLMSettings == nil`) is a BinaryExpr,
// not a method CallExpr, so it does not falsely satisfy the invariant.
func auditedLLMHandlers(files []*ast.File) map[string]bool {
	audited := map[string]bool{}
	forEachLLMHandlerMethod(files, func(fd *ast.FuncDecl) {
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fun, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			recv, ok := fun.X.(*ast.SelectorExpr)
			if ok && recv.Sel.Name == "LLMSettings" {
				audited[fd.Name.Name] = true
				return false
			}
			return true
		})
	})
	return audited
}

// forEachLLMHandlerMethod calls fn for every method whose receiver is
// *llmSettingsHandler or *llmUsageHandler and which has a body.
func forEachLLMHandlerMethod(files []*ast.File, fn func(*ast.FuncDecl)) {
	for _, f := range files {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || fd.Body == nil {
				continue
			}
			if isLLMHandlerReceiver(fd.Recv) {
				fn(fd)
			}
		}
	}
}

// isLLMHandlerReceiver reports whether the receiver is `(h *llmSettingsHandler)`
// or `(h *llmUsageHandler)`.
func isLLMHandlerReceiver(recv *ast.FieldList) bool {
	if recv == nil || len(recv.List) != 1 {
		return false
	}
	star, ok := recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	ident, ok := star.X.(*ast.Ident)
	if !ok {
		return false
	}
	return ident.Name == "llmSettingsHandler" || ident.Name == "llmUsageHandler"
}
