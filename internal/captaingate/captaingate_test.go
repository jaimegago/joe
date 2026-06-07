package captaingate_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/agentctx"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/captaingate"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/internal/tools"
)

// principalSpyExecutor wraps an inner executor and records the principal
// it sees in context for each call. Used by the §B1 substitution and
// §C2 ordering tests to verify what principal the downstream pipeline
// (anything calling rbac.PrincipalFromContext) would observe.
type principalSpyExecutor struct {
	calls         atomic.Int64
	lastPrincipal atomic.Value // rbac.Principal
	returnValue   any
	returnErr     error
}

func (s *principalSpyExecutor) Execute(ctx context.Context, name string, args map[string]any) (any, error) {
	s.calls.Add(1)
	s.lastPrincipal.Store(rbac.PrincipalFromContext(ctx))
	if s.returnErr != nil {
		return nil, s.returnErr
	}
	return s.returnValue, nil
}

// gateEnv assembles a fully-real session-model + audit stack and a
// wrapper instance, so gate tests exercise the actual SQL paths (no
// mocks for the gate's repo dependencies).
type gateEnv struct {
	store   *store.Store
	sess    sessionmodel.Repository
	audit   audit.Repository
	wrapper *captaingate.Wrapper
	spy     *principalSpyExecutor
	ctx     context.Context
}

func newGateEnv(t *testing.T) *gateEnv {
	t.Helper()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	sessRepo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	auditRepo := audit.NewRepository(s.DB(), store.DriverSQLite)
	spy := &principalSpyExecutor{returnValue: map[string]any{"ok": true}}
	wrapper := captaingate.New(spy, sessRepo, auditRepo)
	return &gateEnv{
		store: s, sess: sessRepo, audit: auditRepo, wrapper: wrapper, spy: spy,
		ctx: context.Background(),
	}
}

func (e *gateEnv) declareWithCaptain(t *testing.T, principal string) string {
	t.Helper()
	id, _, err := e.sess.DeclareIncidentRegime(e.ctx, principal, sessionmodel.RegimeKindHuman)
	if err != nil {
		t.Fatalf("declare: %v", err)
	}
	return id
}

func (e *gateEnv) resolveIncident(t *testing.T, principal, sessionID string) {
	t.Helper()
	if err := e.sess.UpdateIncidentState(e.ctx, sessionID, sessionmodel.IncidentStateBelievedMitigated); err != nil {
		t.Fatalf("advance to mitigated: %v", err)
	}
	if _, err := e.sess.ResolveIncidentRegime(e.ctx, principal); err != nil {
		t.Fatalf("resolve regime: %v", err)
	}
}

func withCtx(ctx context.Context, sessionID, principal string) context.Context {
	c := agentctx.WithSessionID(ctx, sessionID)
	c = rbac.WithPrincipal(c, rbac.Principal(principal))
	return c
}

// countAuditRows tallies audit_log rows whose action equals the given
// verb. Used by TestPhaseG_GateRefusalRecordedInAuditTrail to assert
// the refusal landed durably.
func countAuditRows(t *testing.T, s *store.Store, action string) int {
	t.Helper()
	row := s.DB().QueryRow("SELECT COUNT(*) FROM audit_log WHERE action = ?", action)
	var n int
	if err := row.Scan(&n); err != nil {
		t.Fatalf("count audit_log: %v", err)
	}
	return n
}

// --- Migrated from coreagent/executor_gate_test.go (Phase 1 Change 10).
//     Behaviour is unchanged; what changed is WHERE the gate lives —
//     now the shared captaingate.Wrapper instead of DurableExecutor. ---

// TestCaptainGate_EndToEnd: declare → captain write allowed → non-captain
// write refused → resolve → both allowed. The gate keys on tier > observe;
// write_file (T3) is the representative managed-system mutation.
func TestCaptainGate_EndToEnd(t *testing.T) {
	e := newGateEnv(t)
	captainSess := e.declareWithCaptain(t, "alice")

	investigation := sessionmodel.AgentSession{
		ID:               uuid.NewString(),
		Type:             sessionmodel.SessionTypeInvestigation,
		CreatorPrincipal: "bob",
		LinkedIncidentID: &captainSess,
	}
	if _, err := e.sess.CreateSession(e.ctx, investigation); err != nil {
		t.Fatalf("create investigation: %v", err)
	}

	// 1. Write from captain session BY captain principal → ALLOWED.
	ctx := withCtx(e.ctx, captainSess, "alice")
	if _, err := e.wrapper.Execute(ctx, "write_file", map[string]any{"id": "x"}); err != nil {
		t.Errorf("captain mutation should be allowed: %v", err)
	}

	// 2. SAME write from non-captain session → REFUSED with redirect.
	ctx2 := withCtx(e.ctx, investigation.ID, "bob")
	_, err := e.wrapper.Execute(ctx2, "write_file", map[string]any{"id": "y"})
	if err == nil {
		t.Fatal("non-captain mutation should be refused by the §C gate")
	}
	var refusal *captaingate.GateRefusalError
	if !errors.As(err, &refusal) {
		t.Fatalf("expected *GateRefusalError, got %T: %v", err, err)
	}
	if refusal.CaptainSessionID != captainSess {
		t.Errorf("refusal redirect = %q, want %q", refusal.CaptainSessionID, captainSess)
	}

	// 3. Resolve → regime returns to normal → both can mutate.
	e.resolveIncident(t, "alice", captainSess)
	if _, err := e.wrapper.Execute(ctx, "write_file", map[string]any{"id": "x2"}); err != nil {
		t.Errorf("captain mutation after resolve: %v", err)
	}
	if _, err := e.wrapper.Execute(ctx2, "write_file", map[string]any{"id": "y2"}); err != nil {
		t.Errorf("non-captain mutation after resolve: %v", err)
	}
}

// TestCaptainGate_RefusalNeverCallsInner: §C2 / Invariant 5 ordering.
// On the refusal path, the inner Execute is never invoked.
func TestCaptainGate_RefusalNeverCallsInner(t *testing.T) {
	e := newGateEnv(t)
	captainSess := e.declareWithCaptain(t, "alice")
	investigation := sessionmodel.AgentSession{
		ID:               uuid.NewString(),
		Type:             sessionmodel.SessionTypeInvestigation,
		CreatorPrincipal: "bob",
		LinkedIncidentID: &captainSess,
	}
	if _, err := e.sess.CreateSession(e.ctx, investigation); err != nil {
		t.Fatalf("create investigation: %v", err)
	}

	ctx := withCtx(e.ctx, investigation.ID, "bob")
	_, err := e.wrapper.Execute(ctx, "write_file", map[string]any{"id": "y"})
	if err == nil {
		t.Fatal("expected refusal")
	}
	if got := e.spy.calls.Load(); got != 0 {
		t.Errorf("inner executor called %d times on refusal path, want 0", got)
	}
}

// TestCaptainGate_B1_PrincipalSubstitution: in incident regime the
// downstream sees the captain's principal regardless of what principal
// the caller passes; in normal regime the request principal is
// untouched.
func TestCaptainGate_B1_PrincipalSubstitution(t *testing.T) {
	e := newGateEnv(t)
	captainSess := e.declareWithCaptain(t, "alice")

	ctx := withCtx(e.ctx, captainSess, "alice")
	if _, err := e.wrapper.Execute(ctx, "write_file", map[string]any{"id": "x"}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := e.spy.lastPrincipal.Load().(rbac.Principal); got != "alice" {
		t.Errorf("incident regime: spy principal = %q, want alice (§B1)", got)
	}

	// Transfer captaincy to bob. Caller still claims alice → refused.
	oldCap, _ := e.sess.GetActiveCaptain(e.ctx, captainSess)
	if err := e.sess.MarkCaptainDetached(e.ctx, oldCap.ID, oldCap.AttachedAt); err != nil {
		t.Fatalf("detach alice: %v", err)
	}
	active := sessionmodel.TransferStateActive
	if _, err := e.sess.AttachCaptain(e.ctx, sessionmodel.Captain{
		ID: uuid.NewString(), SessionID: captainSess,
		CaptainType: sessionmodel.CaptainTypeHuman, Principal: "bob",
		TransferState: &active,
	}); err != nil {
		t.Fatalf("attach bob: %v", err)
	}
	if _, err := e.wrapper.Execute(ctx, "write_file", map[string]any{"id": "x2"}); err == nil {
		t.Fatal("alice should be refused after captaincy transfers to bob")
	}
	ctxBob := withCtx(e.ctx, captainSess, "bob")
	if _, err := e.wrapper.Execute(ctxBob, "write_file", map[string]any{"id": "x3"}); err != nil {
		t.Fatalf("bob mutation: %v", err)
	}
	if got := e.spy.lastPrincipal.Load().(rbac.Principal); got != "bob" {
		t.Errorf("after transfer: spy principal = %q, want bob (§B1)", got)
	}

	// Resolve → normal regime: no substitution; carol seen as carol.
	e.resolveIncident(t, "bob", captainSess)
	carolCtx := withCtx(e.ctx, captainSess, "carol")
	if _, err := e.wrapper.Execute(carolCtx, "write_file", map[string]any{"id": "x4"}); err != nil {
		t.Fatalf("carol mutation in normal regime: %v", err)
	}
	if got := e.spy.lastPrincipal.Load().(rbac.Principal); got != "carol" {
		t.Errorf("normal regime: spy principal = %q, want carol (no substitution outside incident)", got)
	}
}

// TestCaptainGate_AllowsT1ReadsInIncident: T1 reads/discovery bypass
// the gate even when the caller is not the captain in incident regime.
// This is the loop-path equivalent of the read-paths-still-work
// requirement: investigators can still read during an incident.
func TestCaptainGate_AllowsT1ReadsInIncident(t *testing.T) {
	e := newGateEnv(t)
	captainSess := e.declareWithCaptain(t, "alice")
	// bob is not captain; any T1 read should pass.
	ctx := withCtx(e.ctx, captainSess, "bob")
	if _, err := e.wrapper.Execute(ctx, "read_file", map[string]any{"path": "/etc/hosts"}); err != nil {
		t.Errorf("T1 read should always pass: %v", err)
	}
	if got := e.spy.calls.Load(); got != 1 {
		t.Errorf("inner Execute should run for T1: calls = %d, want 1", got)
	}
}

// --- New Phase G tests. ---

// TestPhaseG_LoopPathNonCaptainMutationRefused proves the gate fix on
// the agentloop's executor path (ExecuteBatch, not Execute). Pre-Phase
// G this would have SUCCEEDED — the user task loop used a naked
// *tools.Executor with no §C gate — so this test is the concrete
// signal Phase G fixed the bug it set out to fix
// (docs/joe-identity-design.md §0 bug #2).
//
// We do NOT spin up the full agentic LLM loop because that's an
// integration concern; we drive the wrapper's ExecuteBatch directly
// with crafted ToolCallRequests, which is exactly what
// agentloop.Agent.Run does each iteration. The wrapper is the same
// object both code paths get.
func TestPhaseG_LoopPathNonCaptainMutationRefused(t *testing.T) {
	e := newGateEnv(t)
	captainSess := e.declareWithCaptain(t, "alice")
	investigation := sessionmodel.AgentSession{
		ID:               uuid.NewString(),
		Type:             sessionmodel.SessionTypeInvestigation,
		CreatorPrincipal: "bob",
		LinkedIncidentID: &captainSess,
	}
	if _, err := e.sess.CreateSession(e.ctx, investigation); err != nil {
		t.Fatalf("create investigation: %v", err)
	}

	// Pre-Phase-G this path was the naked tools.Executor — no gate, no
	// refusal. Phase G installs the same wrapper here, so the loop now
	// enforces §C.

	// 1. Captain session, captain principal: mutation proceeds.
	captainCtx := withCtx(e.ctx, captainSess, "alice")
	calls := []tools.ToolCallRequest{{ID: "ok-1", Name: "write_file", Args: map[string]any{"id": "x"}}}
	results, err := e.wrapper.ExecuteBatch(captainCtx, calls)
	if err != nil {
		t.Fatalf("captain batch: %v", err)
	}
	if results[0].Error != nil {
		t.Errorf("captain mutation refused unexpectedly: %v", results[0].Error)
	}

	// 2. Non-captain session: mutation refused, no inner.Execute.
	innerBefore := e.spy.calls.Load()
	investigationCtx := withCtx(e.ctx, investigation.ID, "bob")
	calls = []tools.ToolCallRequest{{ID: "block-1", Name: "write_file", Args: map[string]any{"id": "y"}}}
	results, err = e.wrapper.ExecuteBatch(investigationCtx, calls)
	// ExecuteBatch returns ErrAllToolsFailed when every call errored.
	if err == nil {
		t.Fatal("expected ErrAllToolsFailed for an all-refused batch")
	}
	if results[0].Error == nil {
		t.Fatal("non-captain mutation should be refused on the LOOP path — pre-Phase-G this would have succeeded")
	}
	var refusal *captaingate.GateRefusalError
	if !errors.As(results[0].Error, &refusal) {
		t.Errorf("expected *GateRefusalError, got %T: %v", results[0].Error, results[0].Error)
	}
	if e.spy.calls.Load() != innerBefore {
		t.Errorf("inner Execute called on refused loop-path mutation — gate bypassed")
	}
}

// TestPhaseG_LoopPathNonCaptainReadsStillSucceed proves the gate does
// not constrain reads/investigations on the loop path. A non-captain
// session can still read in incident regime; only mutations are gated.
// This is the "read/investigation tool calls still succeed" acceptance
// criterion.
func TestPhaseG_LoopPathNonCaptainReadsStillSucceed(t *testing.T) {
	e := newGateEnv(t)
	captainSess := e.declareWithCaptain(t, "alice")
	investigation := sessionmodel.AgentSession{
		ID:               uuid.NewString(),
		Type:             sessionmodel.SessionTypeInvestigation,
		CreatorPrincipal: "bob",
		LinkedIncidentID: &captainSess,
	}
	if _, err := e.sess.CreateSession(e.ctx, investigation); err != nil {
		t.Fatalf("create investigation: %v", err)
	}

	investigationCtx := withCtx(e.ctx, investigation.ID, "bob")
	reads := []tools.ToolCallRequest{{ID: "investigate-1", Name: "read_file", Args: map[string]any{"path": "/etc/hosts"}}}
	results, err := e.wrapper.ExecuteBatch(investigationCtx, reads)
	if err != nil {
		t.Fatalf("non-captain READ on loop should succeed in incident: %v", err)
	}
	if results[0].Error != nil {
		t.Errorf("read refused on the loop path: %v", results[0].Error)
	}
}

// TestPhaseG_GateRefusalRecordedInAuditTrail: a loop-path captain-gate
// refusal lands as one captain_transition row with action
// captain_gate_refused and decision deny. This satisfies the Phase G
// requirement that loop-path refusals are observable in the audit
// trail, consistent with Phase F.
func TestPhaseG_GateRefusalRecordedInAuditTrail(t *testing.T) {
	e := newGateEnv(t)
	captainSess := e.declareWithCaptain(t, "alice")
	investigation := sessionmodel.AgentSession{
		ID:               uuid.NewString(),
		Type:             sessionmodel.SessionTypeInvestigation,
		CreatorPrincipal: "bob",
		LinkedIncidentID: &captainSess,
	}
	if _, err := e.sess.CreateSession(e.ctx, investigation); err != nil {
		t.Fatalf("create investigation: %v", err)
	}

	before := countAuditRows(t, e.store, audit.ActionCaptainGateRefused)
	ctx := withCtx(e.ctx, investigation.ID, "bob")
	_, err := e.wrapper.Execute(ctx, "write_file", map[string]any{"id": "y"})
	if err == nil {
		t.Fatal("expected refusal")
	}
	after := countAuditRows(t, e.store, audit.ActionCaptainGateRefused)
	if after != before+1 {
		t.Errorf("captain_gate_refused audit rows = %d, want %d (one row per refusal)", after, before+1)
	}

	// Verify the row's shape: kind=captain_transition, decision=deny,
	// principal=bob, context carries the captain redirect target.
	row := e.store.DB().QueryRow(`SELECT kind, decision, principal, context FROM audit_log
	    WHERE action = ? ORDER BY id DESC LIMIT 1`, audit.ActionCaptainGateRefused)
	var kind, decision, principal, ctxBlob string
	if err := row.Scan(&kind, &decision, &principal, &ctxBlob); err != nil {
		t.Fatalf("scan refusal row: %v", err)
	}
	if kind != string(audit.KindCaptainTransition) {
		t.Errorf("kind = %q, want %q", kind, string(audit.KindCaptainTransition))
	}
	if decision != string(audit.DecisionDeny) {
		t.Errorf("decision = %q, want %q", decision, string(audit.DecisionDeny))
	}
	if principal != "bob" {
		t.Errorf("principal = %q, want bob", principal)
	}
	if !strings.Contains(ctxBlob, captainSess) {
		t.Errorf("context blob %q does not name the captain session %q", ctxBlob, captainSess)
	}
}

// TestPhaseG_GateIsDenyOnly_RBACAuthorityInvariance is the central
// safety property of Phase G: the §C gate can REFUSE but never WIDEN
// authority. For a fixed principal/action/zone, the underlying RBAC
// decision is identical whether the regime is incident or not.
//
// We don't need to run the accessor end-to-end here — its decision is
// a pure function of (principal, sourceID, action) and the policy
// tables. We assert IsAllowed (the function the accessor calls) is
// blind to regime by:
//
//	(a) computing the allow/deny outcome for a fixed principal/source/
//	    action under normal regime, then
//	(b) declaring incident, recomputing, and asserting the outcome is
//	    identical.
//
// This protects against any future change that might be tempted to
// route incident-regime context into IsAllowed and use it to widen or
// narrow authority — the design's settled invariant says it never
// should.
func TestPhaseG_GateIsDenyOnly_RBACAuthorityInvariance(t *testing.T) {
	e := newGateEnv(t)
	rbacRepo := rbac.NewRepository(e.store.DB(), store.DriverSQLite)
	engine := rbac.NewPolicyEngine(rbacRepo)

	// Seed a source/zone/policy combo so we have a non-trivial allow.
	if err := e.store.Sources.Create(e.ctx, &store.Source{
		ID: "src-prod", Type: "k8s", Name: "prod cluster", Config: []byte(`{}`),
	}); err != nil {
		t.Fatalf("create source: %v", err)
	}
	if err := rbacRepo.UpsertAssignment(e.ctx, rbac.SourceZoneAssignment{
		SourceID: "src-prod", ZoneID: "prod-readonly", AssignedBy: "test",
	}, "test"); err != nil {
		t.Fatalf("assign zone: %v", err)
	}
	if _, err := rbacRepo.CreatePolicy(e.ctx, rbac.Policy{Principal: "alice", ZoneID: "prod-readonly"}, "test"); err != nil {
		t.Fatalf("create policy: %v", err)
	}

	aliceSet := rbac.NewPrincipalSet("alice")
	carolSet := rbac.NewPrincipalSet("carol") // no policy

	// (a) Normal regime baseline.
	allowAliceNormal := engine.IsAllowed(e.ctx, aliceSet, "src-prod", rbac.ActionRead)
	denyCarolNormal := engine.IsAllowed(e.ctx, carolSet, "src-prod", rbac.ActionRead)

	// Declare incident under alice (she has regime-control granted
	// implicitly only if we wire it — we don't; we drive the regime
	// transition directly through the repo so the test exercises the
	// invariance under arbitrary regime states).
	if _, _, err := e.sess.DeclareIncidentRegime(e.ctx, "alice", sessionmodel.RegimeKindHuman); err != nil {
		t.Fatalf("declare incident: %v", err)
	}

	// (b) Same calls under incident regime → must produce identical
	// outcomes. If they differ, IsAllowed is leaking regime state into
	// the decision — which would violate the design's invariant.
	allowAliceIncident := engine.IsAllowed(e.ctx, aliceSet, "src-prod", rbac.ActionRead)
	denyCarolIncident := engine.IsAllowed(e.ctx, carolSet, "src-prod", rbac.ActionRead)

	if allowAliceNormal != allowAliceIncident {
		t.Errorf("alice's RBAC outcome changed across regimes: normal=%v incident=%v "+
			"— §2.10 invariant violated: incident must never alter authority",
			allowAliceNormal, allowAliceIncident)
	}
	if denyCarolNormal != denyCarolIncident {
		t.Errorf("carol's RBAC outcome changed across regimes: normal=%v incident=%v "+
			"— §2.10 invariant violated", denyCarolNormal, denyCarolIncident)
	}
	if !allowAliceNormal {
		t.Errorf("alice should be allowed read on prod-readonly (fixture sanity check)")
	}
	if denyCarolNormal {
		t.Errorf("carol should be denied (fixture sanity check)")
	}
}
