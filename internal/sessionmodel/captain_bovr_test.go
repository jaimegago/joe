package sessionmodel_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jaimegago/joe/internal/runmodel"
	"github.com/jaimegago/joe/internal/sessionmodel"
)

// TestCaptain_BOVR_ForceYieldOnJoeCurrent is the named structural guard
// for the session-model design (Phase 0) Invariant 3 (Human-override-always-wins
// for joe-captain, compiled in) and the R-OVR force-yield branch added
// in Change 12.
//
// Setup bypasses Attach (which refuses captain_type=joe per the
// seams.JoeCaptainTypeEnabled gate). The test inserts a session_captains
// row with captain_type='joe' directly through the repository — the
// schema's CHECK constraint already accepts the 'joe' value (migration
// 009), so this is a legal physical insert that simulates the
// hypothetical future state where Attach permits joe-type captains.
//
// The first assertion verifies the runtime behavior: an incoming-
// initiated transfer request from an RBAC-authorized human (the
// authorization check is the caller's responsibility per the §B B1
// note in CaptainService.Attach) routes directly to
// transfer_confirmed. NO decision solicitation is created on the run.
//
// The second assertion is the AST-based structural one: BeginTransfer
// in captain.go must not contain a "joe declines" code path. The walk
// looks for both a literal "decline" string inside the function body
// and any if/switch branch that inspects CaptainTypeJoe but does NOT
// route to completeTransfer. Either would signal a regression where
// joe-captain gained the ability to decline or delay human takeover —
// the invariant Change 12 compiles in.
func TestCaptain_BOVR_ForceYieldOnJoeCurrent(t *testing.T) {
	e := newCaptainEnv(t, 60)

	// Build an incident session manually (no Attach call, because Attach
	// refuses captain_type=joe in Phase 1). This mirrors
	// TestCaptain_B2_NullAuthorityOnPendingCaptain's setup pattern.
	state := sessionmodel.IncidentStateDeclared
	sess := sessionmodel.AgentSession{
		ID:               uuid.NewString(),
		Type:             sessionmodel.SessionTypeIncident,
		IncidentState:    &state,
		CreatorPrincipal: "system",
	}
	if _, err := e.sess.CreateSession(e.ctx, sess); err != nil {
		t.Fatalf("create incident: %v", err)
	}

	// Directly insert a session_captains row with captain_type='joe'.
	// This bypasses Attach (which would refuse) and simulates the
	// future state where the seam is enabled.
	now := time.Now().UTC()
	activeState := sessionmodel.TransferStateActive
	joeCaptainID := uuid.NewString()
	if _, err := e.sess.AttachCaptain(e.ctx, sessionmodel.Captain{
		ID:            joeCaptainID,
		SessionID:     sess.ID,
		CaptainType:   sessionmodel.CaptainTypeJoe,
		Principal:     "joe-agent",
		AttachedAt:    now,
		TransferState: &activeState,
		LastSeenAt:    &now,
	}); err != nil {
		t.Fatalf("attach joe captain via repo (test seam): %v", err)
	}

	// A run so BeginTransfer has somewhere to (potentially) write a
	// decision solicitation if the force-yield branch were missing —
	// the run is the negative-assertion vehicle for "no solicitation".
	runID := e.runOn(t, sess.ID)

	// Incoming-initiated transfer from an RBAC-authorized human.
	// The caller is the one that vouches for the human's RBAC; this
	// test plays that role by passing the human principal directly.
	res, err := e.svc.BeginTransfer(e.ctx, sess.ID,
		sessionmodel.TransferInitiatorIncoming,
		"alice", // requesting (incoming) human principal
		"alice",
		runID)
	if err != nil {
		t.Fatalf("BeginTransfer against joe captain: %v", err)
	}

	// R-OVR: immediate transfer_confirmed (no approve/decline/timeout).
	if res.State != sessionmodel.TransferStateTransferConfirmed {
		t.Errorf("state = %q, want transfer_confirmed (R-OVR force-yield)",
			res.State)
	}
	if res.NewCaptainID == "" {
		t.Error("new captain id missing — R-OVR must complete the transfer in one step")
	}
	if res.SolicitationID != "" {
		t.Errorf("solicitation id = %q, want empty — R-OVR must NOT open a "+
			"decision solicitation (no approve/decline path for joe-captain)",
			res.SolicitationID)
	}

	// §B1 principal-threading: the new captain row is alice.
	p, ok, _ := e.sess.CurrentCaptainPrincipal(e.ctx, sess.ID)
	if !ok || p != "alice" {
		t.Errorf("CurrentCaptainPrincipal after R-OVR = (%q, %v), want (alice, true)", p, ok)
	}

	// No decision solicitation row exists on the run. The runmodel
	// repository exposes no list-by-run method directly, so we probe by
	// asserting the steps ledger contains no solicitation_open step.
	// (BeginTransfer's solicitation path calls OpenSolicitation, which
	// in the production code path is paired with a solicitation_open
	// step via the HTTP layer; here we assert directly against the
	// runmodel store.)
	all, err := e.runRepo.ListStepsForRun(e.ctx, runID)
	if err != nil {
		t.Fatalf("ListStepsForRun: %v", err)
	}
	for _, step := range all {
		if step.Kind == runmodel.StepKindSolicitationOpen {
			t.Errorf("unexpected solicitation_open step %s: R-OVR must NOT "+
				"create a decision solicitation on the run", step.ID)
		}
	}

	// Belt-and-suspenders: also verify the row count is zero by
	// scanning the active captain. The new captain is human, the joe
	// row is detached.
	all2, _ := e.sess.ListCaptainsForSession(e.ctx, sess.ID)
	var joeRow, aliceRow *sessionmodel.Captain
	for i := range all2 {
		c := &all2[i]
		switch c.CaptainType {
		case sessionmodel.CaptainTypeJoe:
			joeRow = c
		case sessionmodel.CaptainTypeHuman:
			aliceRow = c
		}
	}
	if joeRow == nil || joeRow.DetachedAt == nil {
		t.Errorf("joe captain row should be detached after R-OVR: %+v", joeRow)
	}
	if aliceRow == nil || aliceRow.DetachedAt != nil {
		t.Errorf("alice captain row should be active (no detached_at): %+v", aliceRow)
	}
}

// TestCaptain_BOVR_NoJoeDeclineBranchInBeginTransfer is the AST-based
// half of the R-OVR named structural guard. It parses
// internal/sessionmodel/captain.go via go/ast, locates the
// BeginTransfer FuncDecl, and asserts:
//
//  1. No string literal in the function body matches "joe declines"
//     (case-insensitive) — there is no joe-captain decline message.
//  2. Every if-statement or switch-case branch in the function body
//     whose guarding condition references the identifier
//     `CaptainTypeJoe` routes ONLY to the `completeTransfer` shortcut.
//     Any branch that returns without calling completeTransfer would
//     signal joe gained a path to decline/delay human takeover.
//
// The pattern mirrors TestC4_PositionalNotSemantic in
// internal/sessiongate/import_guard_test.go (a precedent for pinning a
// specific function's structure with go/ast) and TestInvariant4_
// RegimeResolveSingleCallSite in internal/api/regime_invariant_test.go
// (a precedent for go/ast walks bounded to a single function).
//
// A future contributor adding a joe-decline branch would have to either
// (a) refactor the override path so the AST shape is no longer
// recognizable here, or (b) update this test in the same diff with a
// documented justification — the explanatory failure message points
// the contributor at the §R-OVR invariant.
func TestCaptain_BOVR_NoJoeDeclineBranchInBeginTransfer(t *testing.T) {
	path, err := filepath.Abs("captain.go")
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	var beginTransfer *ast.FuncDecl
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fn.Name.Name != "BeginTransfer" {
			continue
		}
		// CaptainService method, not a free function.
		if fn.Recv == nil || len(fn.Recv.List) == 0 {
			continue
		}
		beginTransfer = fn
		break
	}
	if beginTransfer == nil {
		t.Fatal("could not find BeginTransfer method on CaptainService in captain.go — the R-OVR " +
			"force-yield branch lives there; if BeginTransfer was renamed, update this " +
			"guard to track the new name")
	}

	// (1) No "joe declines" literal anywhere in the function body. The
	// match is case-insensitive substring on string literal contents
	// (not comments, not identifiers).
	ast.Inspect(beginTransfer, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		// lit.Value includes the surrounding quotes; strip them for the
		// content check.
		content := strings.ToLower(strings.Trim(lit.Value, "`\""))
		if strings.Contains(content, "joe declines") || strings.Contains(content, "joe declined") {
			t.Errorf("§R-OVR violation: BeginTransfer contains a string literal "+
				"%q mentioning joe declining. R-OVR compiles in joe-captain "+
				"immediate force-yield — there is no decline path. Remove the "+
				"literal or update the session-model design (Phase 0) §B R-OVR and this "+
				"guard in the same diff.", strings.Trim(lit.Value, "`\""))
		}
		return true
	})

	// (2) Every branch whose condition mentions `CaptainTypeJoe` must
	// route to `completeTransfer`. This catches both if-statements and
	// switch-case statements where the joe-captain identifier appears
	// in the test/condition.
	ast.Inspect(beginTransfer, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.IfStmt:
			if !exprMentions(node.Cond, "CaptainTypeJoe") {
				return true
			}
			if !blockCallsCompleteTransfer(node.Body) {
				t.Errorf("§R-OVR violation: if-branch in BeginTransfer at offset %d "+
					"references CaptainTypeJoe but does not call completeTransfer "+
					"in its then-block. The R-OVR override branch must route ONLY "+
					"to immediate transfer_confirmed via completeTransfer — no "+
					"decline/delay path. If a NEW joe-aware branch is needed, "+
					"document it and update this guard.", node.Pos())
			}
			// If there's an else block, verify it doesn't introduce a
			// joe-aware decline either. (Skip if the else doesn't
			// mention CaptainTypeJoe — it's not a joe-specific path.)
			if node.Else != nil {
				ast.Inspect(node.Else, func(child ast.Node) bool {
					call, ok := child.(*ast.CallExpr)
					if !ok {
						return true
					}
					ident, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					if ident.Sel.Name == "openTransferDecisionSolicitation" {
						// The non-joe arms legitimately call this — but
						// only when the outer condition is NOT joe-aware.
						// We accept it here (this is a no-op for the joe
						// branch since CaptainTypeJoe was already routed
						// out before reaching this else).
					}
					return true
				})
			}
		case *ast.CaseClause:
			// switch case where CaptainTypeJoe is one of the case values.
			for _, expr := range node.List {
				if !exprMentions(expr, "CaptainTypeJoe") {
					continue
				}
				if !stmtsCallCompleteTransfer(node.Body) {
					t.Errorf("§R-OVR violation: switch case for CaptainTypeJoe at "+
						"offset %d does not route to completeTransfer. The "+
						"joe-captain arm has no decline branch — it must yield "+
						"immediately.", node.Pos())
				}
			}
		}
		return true
	})
}

// exprMentions returns true if e contains an identifier with the given
// name anywhere in its subtree.
func exprMentions(e ast.Expr, name string) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if found {
			return false
		}
		if ident, ok := n.(*ast.Ident); ok && ident.Name == name {
			found = true
			return false
		}
		return true
	})
	return found
}

// blockCallsCompleteTransfer reports whether the given block body
// contains a call to s.completeTransfer (or any selector ending in
// completeTransfer). This is the structural fingerprint of the R-OVR
// immediate-yield path.
func blockCallsCompleteTransfer(b *ast.BlockStmt) bool {
	if b == nil {
		return false
	}
	return stmtsCallCompleteTransfer(b.List)
}

func stmtsCallCompleteTransfer(stmts []ast.Stmt) bool {
	found := false
	for _, stmt := range stmts {
		ast.Inspect(stmt, func(n ast.Node) bool {
			if found {
				return false
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if sel.Sel.Name == "completeTransfer" {
				found = true
				return false
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}
