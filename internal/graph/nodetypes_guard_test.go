package graph_test

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

const graphPkgPath = "github.com/jaimegago/joe/internal/graph"

// TestNodeTypeVocabulary is the break-test for promise 3 of the graph
// contract, for node types: the vocabulary is single-sourced in nodetypes.go,
// shared by writers and readers, and a reader can name only a type some
// writer emits.
//
// It type-checks every production package that imports internal/graph and
// asserts, in both directions:
//
//  1. No string literal stands where a node type belongs. The positions are:
//     the Type field of a graph.Node composite literal or assignment; any
//     struct field named NodeType or TargetTypes; a nodeType parameter; an
//     ==/!= comparison or switch case against any of those (or a nodeType
//     variable); and a graph Query string of the form "type:<literal>".
//     A literal there is either a copy of the vocabulary or a phantom.
//  2. Every NodeType* constant in nodetypes.go is written by some writer —
//     used as a node's Type, a spec's NodeType, or a nodeType argument — so
//     the set contains nothing no refresher emits.
//
// Together these make a phantom arm unrepresentable: a literal fails (1), and
// a constant nobody writes fails (2). Test files are exempt; fixtures may
// stage arbitrary types to exercise skip paths.
func TestNodeTypeVocabulary(t *testing.T) {
	root := findRepoRoot(t)

	// Pass 1: find the production packages that import internal/graph.
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedImports, Dir: root}
	all, err := packages.Load(cfg, "./...")
	if err != nil {
		t.Fatalf("load packages: %v", err)
	}
	patterns := []string{graphPkgPath}
	for _, p := range all {
		if _, ok := p.Imports[graphPkgPath]; ok {
			patterns = append(patterns, p.PkgPath)
		}
	}

	// Pass 2: type-check them.
	cfg = &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo,
		Dir: root,
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		t.Fatalf("load packages: %v", err)
	}
	if packages.PrintErrors(pkgs) > 0 {
		t.Fatal("packages failed to load")
	}

	var graphPkg *types.Package
	for _, p := range pkgs {
		if p.PkgPath == graphPkgPath {
			graphPkg = p.Types
		}
	}
	if graphPkg == nil {
		t.Fatal("internal/graph not loaded")
	}
	vocab := nodeTypeConsts(graphPkg)
	if len(vocab) == 0 {
		t.Fatal("nodetypes.go declares no NodeType* constants")
	}
	nodeTypeField := nodeStructTypeField(t, graphPkg)

	written := map[*types.Const]bool{}
	for _, p := range pkgs {
		c := &vocabChecker{
			t: t, fset: p.Fset, info: p.TypesInfo, root: root,
			nodeTypeField: nodeTypeField, vocab: vocab, written: written,
		}
		for _, f := range p.Syntax {
			name := p.Fset.Position(f.Pos()).Filename
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			ast.Inspect(f, c.visit)
		}
	}

	var unwritten []string
	for obj := range vocab {
		if !written[obj] {
			unwritten = append(unwritten, obj.Name())
		}
	}
	sort.Strings(unwritten)
	for _, name := range unwritten {
		t.Errorf("graph.%s is declared in nodetypes.go but no writer emits it — "+
			"a reader matching it would be matching a phantom. Remove the constant, "+
			"or make a refresher write it.", name)
	}
}

type vocabChecker struct {
	t             *testing.T
	fset          *token.FileSet
	info          *types.Info
	root          string
	nodeTypeField *types.Var
	vocab         map[*types.Const]bool
	written       map[*types.Const]bool
}

func (c *vocabChecker) visit(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.CompositeLit:
		c.checkComposite(n)
	case *ast.AssignStmt:
		for i, lhs := range n.Lhs {
			if i < len(n.Rhs) && c.isNodeTypeExpr(lhs) {
				c.checkValue(n.Rhs[i], "assignment to a node type")
			}
		}
	case *ast.BinaryExpr:
		if n.Op == token.EQL || n.Op == token.NEQ {
			if c.isNodeTypeExpr(n.X) {
				c.rejectLiteral(n.Y, "comparison against a node type")
			}
			if c.isNodeTypeExpr(n.Y) {
				c.rejectLiteral(n.X, "comparison against a node type")
			}
		}
	case *ast.SwitchStmt:
		if n.Tag != nil && c.isNodeTypeExpr(n.Tag) {
			for _, stmt := range n.Body.List {
				for _, e := range stmt.(*ast.CaseClause).List {
					c.rejectLiteral(e, "switch case on a node type")
				}
			}
		}
	case *ast.CallExpr:
		c.checkCall(n)
	case *ast.BasicLit:
		if s, ok := stringLit(n); ok && strings.HasPrefix(s, "type:") && len(s) > len("type:") {
			c.report(n, "graph query %q names a node type by literal; build it from a graph.NodeType* constant", s)
		}
	}
	return true
}

// checkComposite handles graph.Node{Type: ...} and any struct literal with a
// NodeType or TargetTypes field.
func (c *vocabChecker) checkComposite(lit *ast.CompositeLit) {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		field, _ := c.info.Uses[key].(*types.Var)
		if field == nil || !field.IsField() {
			continue
		}
		switch {
		case field == c.nodeTypeField, field.Name() == "NodeType":
			c.checkValue(kv.Value, "node type field "+field.Name())
			c.markWritten(kv.Value)
		case field.Name() == "TargetTypes":
			if inner, ok := kv.Value.(*ast.CompositeLit); ok {
				for _, e := range inner.Elts {
					c.checkValue(e, "TargetTypes entry")
				}
			}
		}
	}
}

// checkCall treats an argument bound to a parameter named nodeType as a
// write position.
func (c *vocabChecker) checkCall(call *ast.CallExpr) {
	sig, ok := c.info.TypeOf(call.Fun).(*types.Signature)
	if !ok {
		return
	}
	params := sig.Params()
	for i, arg := range call.Args {
		if i >= params.Len() {
			break
		}
		if strings.EqualFold(params.At(i).Name(), "nodeType") {
			c.checkValue(arg, "nodeType argument")
			c.markWritten(arg)
		}
	}
}

// isNodeTypeExpr reports whether e denotes a node type: graph.Node's Type
// field, any NodeType field, or a variable named nodeType.
func (c *vocabChecker) isNodeTypeExpr(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.SelectorExpr:
		sel := c.info.Selections[e]
		if sel == nil || sel.Kind() != types.FieldVal {
			return false
		}
		f := sel.Obj().(*types.Var)
		return f == c.nodeTypeField || f.Name() == "NodeType"
	case *ast.Ident:
		v, ok := c.info.ObjectOf(e).(*types.Var)
		return ok && !v.IsField() && strings.EqualFold(v.Name(), "nodeType")
	}
	return false
}

// checkValue rejects a string literal, and any constant that is not a member
// of the vocabulary (e.g. a local const re-encoding a type).
func (c *vocabChecker) checkValue(e ast.Expr, where string) {
	if c.rejectLiteral(e, where) {
		return
	}
	if obj := c.constOf(e); obj != nil && !c.vocab[obj] {
		c.report(e, "%s uses constant %s, which is not a graph.NodeType* constant", where, obj.Name())
	}
}

func (c *vocabChecker) rejectLiteral(e ast.Expr, where string) bool {
	lit, ok := e.(*ast.BasicLit)
	if !ok {
		return false
	}
	s, ok := stringLit(lit)
	if !ok || s == "" {
		return false
	}
	c.report(e, "%s uses the literal %q; reference a graph.NodeType* constant from "+
		"internal/graph/nodetypes.go (a type no writer emits there is a phantom)", where, s)
	return true
}

func (c *vocabChecker) markWritten(e ast.Expr) {
	if obj := c.constOf(e); obj != nil && c.vocab[obj] {
		c.written[obj] = true
	}
}

func (c *vocabChecker) constOf(e ast.Expr) *types.Const {
	var id *ast.Ident
	switch e := e.(type) {
	case *ast.Ident:
		id = e
	case *ast.SelectorExpr:
		id = e.Sel
	default:
		return nil
	}
	obj, _ := c.info.ObjectOf(id).(*types.Const)
	return obj
}

func (c *vocabChecker) report(n ast.Node, format string, args ...any) {
	pos := c.fset.Position(n.Pos())
	rel, err := filepath.Rel(c.root, pos.Filename)
	if err != nil {
		rel = pos.Filename
	}
	c.t.Errorf("%s:%d: "+format, append([]any{rel, pos.Line}, args...)...)
}

func stringLit(lit *ast.BasicLit) (string, bool) {
	if lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

func nodeTypeConsts(pkg *types.Package) map[*types.Const]bool {
	out := map[*types.Const]bool{}
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		obj, ok := scope.Lookup(name).(*types.Const)
		if !ok || !strings.HasPrefix(name, "NodeType") || obj.Val().Kind() != constant.String {
			continue
		}
		out[obj] = true
	}
	return out
}

func nodeStructTypeField(t *testing.T, pkg *types.Package) *types.Var {
	t.Helper()
	obj := pkg.Scope().Lookup("Node")
	if obj == nil {
		t.Fatal("graph.Node not found")
	}
	st, ok := obj.Type().Underlying().(*types.Struct)
	if !ok {
		t.Fatal("graph.Node is not a struct")
	}
	for i := 0; i < st.NumFields(); i++ {
		if st.Field(i).Name() == "Type" {
			return st.Field(i)
		}
	}
	t.Fatal("graph.Node has no Type field")
	return nil
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
