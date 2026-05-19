package coreagent_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/agentctx"
	"github.com/jaimegago/joe/internal/coreagent"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/runmodel"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
)

// principalSpyExecutor wraps an inner executor and records the principal
// it sees in context for each call. Used by §B1 / C2 tests to verify
// what principal the downstream pipeline (i.e. anything calling
// rbac.PrincipalFromContext) would observe.
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

// gateEnv assembles a fully-real session-model + run-model stack and a
// wrapper instance, so gate tests exercise the actual SQL paths (no
// mocks for the gate's repo dependencies).
type gateEnv struct {
	store   *store.Store
	sess    sessionmodel.Repository
	run     runmodel.Repository
	wrapper *coreagent.DurableExecutor
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
	runRepo := runmodel.NewRepository(s.DB(), store.DriverSQLite)
	spy := &principalSpyExecutor{returnValue: map[string]any{"ok": true}}
	wrapper := coreagent.NewDurableExecutor(spy, runRepo, sessRepo)
	return &gateEnv{
		store: s, sess: sessRepo, run: runRepo, wrapper: wrapper, spy: spy,
		ctx: context.Background(),
	}
}

// declareWithCaptain declares an incident and returns the captain
// session ID. alice is the declaring captain by R-CAP1.
func (e *gateEnv) declareWithCaptain(t *testing.T, principal string) string {
	t.Helper()
	id, _, err := e.sess.DeclareIncidentRegime(e.ctx, principal, sessionmodel.RegimeKindHuman)
	if err != nil {
		t.Fatalf("declare: %v", err)
	}
	return id
}

// resolveIncident drives the active incident to believed_mitigated and
// then through resolve back to normal regime — for the end-to-end test
// that asserts "regime returned to normal → both sessions can mutate".
func (e *gateEnv) resolveIncident(t *testing.T, principal, sessionID string) {
	t.Helper()
	if err := e.sess.UpdateIncidentState(e.ctx, sessionID, sessionmodel.IncidentStateBelievedMitigated); err != nil {
		t.Fatalf("advance to mitigated: %v", err)
	}
	if _, err := e.sess.ResolveIncidentRegime(e.ctx, principal); err != nil {
		t.Fatalf("resolve regime: %v", err)
	}
}

// withCtx threads sessionID + principal into the request context the
// way HTTP middleware would.
func withCtx(ctx context.Context, sessionID, principal string) context.Context {
	c := agentctx.WithSessionID(ctx, sessionID)
	c = rbac.WithPrincipal(c, rbac.Principal(principal))
	return c
}

// withRunCtx adds a runID to the context (needed for §D5 persistence
// to engage; without a run the wrapper falls through after the gate
// check).
func withRunCtx(ctx context.Context, runID string) context.Context {
	return agentctx.WithRunID(ctx, runID)
}

// startRun creates an agent_runs row anchored on sessionID. Returns runID.
func (e *gateEnv) startRun(t *testing.T, sessionID string) string {
	t.Helper()
	run, err := e.run.CreateRun(e.ctx, runmodel.Run{
		ID: uuid.NewString(), SessionID: sessionID, State: runmodel.RunStateRunning,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	return run.ID
}

// --- §6-A end-to-end: declare → captain T2 allowed → non-captain T2
//     refused → resolve → both allowed ---

func TestDurableExecutor_GateEndToEnd(t *testing.T) {
	e := newGateEnv(t)
	captainSess := e.declareWithCaptain(t, "alice")

	// A parallel investigation session also linked to the incident.
	investigation := sessionmodel.AgentSession{
		ID:               uuid.NewString(),
		Type:             sessionmodel.SessionTypeInvestigation,
		CreatorPrincipal: "bob",
		LinkedIncidentID: &captainSess,
	}
	if _, err := e.sess.CreateSession(e.ctx, investigation); err != nil {
		t.Fatalf("create investigation: %v", err)
	}

	captainRun := e.startRun(t, captainSess)
	investigationRun := e.startRun(t, investigation.ID)

	// 1. T2 from captain session BY captain principal → ALLOWED.
	ctx := withRunCtx(withCtx(e.ctx, captainSess, "alice"), captainRun)
	if _, err := e.wrapper.Execute(ctx, "graph_add_node", map[string]any{"id": "x"}); err != nil {
		t.Errorf("captain mutation should be allowed: %v", err)
	}

	// 2. SAME T2 from non-captain session → REFUSED with redirect to
	//    captain session ID (§A4 finding path).
	ctx2 := withRunCtx(withCtx(e.ctx, investigation.ID, "bob"), investigationRun)
	_, err := e.wrapper.Execute(ctx2, "graph_add_node", map[string]any{"id": "y"})
	if err == nil {
		t.Fatal("non-captain mutation should be refused by the §C gate")
	}
	var refusal *coreagent.GateRefusalError
	if !errors.As(err, &refusal) {
		t.Fatalf("expected *GateRefusalError, got %T: %v", err, err)
	}
	if refusal.CaptainSessionID != captainSess {
		t.Errorf("refusal redirect = %q, want %q", refusal.CaptainSessionID, captainSess)
	}
	if refusal.SessionID != investigation.ID {
		t.Errorf("refusal session_id = %q, want %q", refusal.SessionID, investigation.ID)
	}

	// 3. Resolve the incident → regime returns to normal.
	e.resolveIncident(t, "alice", captainSess)

	// 4. Both sessions can mutate again (normal regime → gate allows).
	if _, err := e.wrapper.Execute(ctx, "graph_add_node", map[string]any{"id": "x2"}); err != nil {
		t.Errorf("captain mutation after resolve: %v", err)
	}
	if _, err := e.wrapper.Execute(ctx2, "graph_add_node", map[string]any{"id": "y2"}); err != nil {
		t.Errorf("non-captain mutation after resolve: %v", err)
	}
}

// --- Invariant 5 / §C2 ordering: refusal short-circuits before inner. ---

func TestDurableExecutor_GateRefusalNeverCallsInner(t *testing.T) {
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
	investigationRun := e.startRun(t, investigation.ID)

	ctx := withRunCtx(withCtx(e.ctx, investigation.ID, "bob"), investigationRun)
	_, err := e.wrapper.Execute(ctx, "graph_add_node", map[string]any{"id": "y"})
	if err == nil {
		t.Fatal("expected refusal")
	}

	// The spy executor stands in for ANY downstream consumer — RBAC
	// IsAllowed, the inner tools.Executor's safety check, the tool
	// itself. On the refusal path, NONE of them are reached. Asserts
	// the §C2 ordering: gate first, everything else after.
	if got := e.spy.calls.Load(); got != 0 {
		t.Errorf("inner executor called %d times on refusal path, want 0 — "+
			"§C2 / Invariant 5 ordering violated", got)
	}
}

// --- §B1 principal substitution: incident → ctx.Principal = captain's;
//     normal → unchanged. ---

func TestDurableExecutor_B1_PrincipalSubstitution(t *testing.T) {
	e := newGateEnv(t)
	captainSess := e.declareWithCaptain(t, "alice")
	captainRun := e.startRun(t, captainSess)

	// Incident regime, mutating from the captain session.
	// The request-time principal MUST equal the captain (otherwise the
	// gate refuses), but the §B1 substitution still matters: the
	// downstream sees `alice` via PrincipalFromContext regardless of
	// what principal the caller passes — even if it was already alice,
	// the substitution path is exercised and the captain principal is
	// what the spy observes.
	ctx := withRunCtx(withCtx(e.ctx, captainSess, "alice"), captainRun)
	if _, err := e.wrapper.Execute(ctx, "graph_add_node", map[string]any{"id": "x"}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := e.spy.lastPrincipal.Load().(rbac.Principal)
	if got != "alice" {
		t.Errorf("incident regime: spy principal = %q, want alice (§B1 substitution)", got)
	}

	// Now transfer captaincy to bob and re-run. The substitution should
	// reflect bob, not alice, even if the request-time context still
	// names alice (the substitution overrides the request-time value).
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
	// Caller still claims alice, but bob is now captain.
	if _, err := e.wrapper.Execute(ctx, "graph_add_node", map[string]any{"id": "x2"}); err == nil {
		t.Fatal("alice should be refused after captaincy transfers to bob")
	}
	// Now call as bob. Substitution should still pass bob to the inner.
	ctxBob := withRunCtx(withCtx(e.ctx, captainSess, "bob"), captainRun)
	if _, err := e.wrapper.Execute(ctxBob, "graph_add_node", map[string]any{"id": "x3"}); err != nil {
		t.Fatalf("bob mutation: %v", err)
	}
	got = e.spy.lastPrincipal.Load().(rbac.Principal)
	if got != "bob" {
		t.Errorf("after captain transfer: spy principal = %q, want bob (§B1)", got)
	}

	// Now resolve and verify normal-regime path leaves principal
	// unchanged: caller is "carol" (not a captain), substitution is
	// skipped, downstream sees "carol".
	e.resolveIncident(t, "bob", captainSess)
	carolCtx := withRunCtx(withCtx(e.ctx, captainSess, "carol"), captainRun)
	if _, err := e.wrapper.Execute(carolCtx, "graph_add_node", map[string]any{"id": "x4"}); err != nil {
		t.Fatalf("carol mutation in normal regime: %v", err)
	}
	got = e.spy.lastPrincipal.Load().(rbac.Principal)
	if got != "carol" {
		t.Errorf("normal regime: spy principal = %q, want carol (no substitution outside incident)", got)
	}
}

// --- §C5 non-configurable floor: enumerate config permutations,
//     all refuse non-captain mutation. ---

// configPermutation is a config-shaped snapshot used by the permutation
// test. The wrapper takes no config parameters, but we exhaustively
// vary the inputs that could plausibly carry config through context or
// through repo state — and assert refusal across all of them.
type configPermutation struct {
	name              string
	sessionIDInCtx    string // empty = unset
	principalInCtx    string // empty = unset (resolves to rbac.Unknown)
	idempotencyKeyCtx string // empty = unset; non-empty exercises caller-supplied key
	runIDInCtx        string // empty = unset (gate still runs; §D5 falls through)
	extraToolArgs     map[string]any
}

func TestDurableExecutor_C5_NonConfigurableFloor(t *testing.T) {
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
	investigationRun := e.startRun(t, investigation.ID)

	// Every permutation MUST result in refusal because the gate is not
	// configurable. The wrapper constructor takes (inner, runRepo,
	// sessRepo) — no config. There is no env var, config flag, or
	// feature toggle that flips the refusal to an allow. This test
	// enumerates plausible varying inputs and asserts the property
	// holds in all of them.
	permutations := []configPermutation{
		// Vary principal: known captain principal, but mutating from
		// the WRONG session — still refused, even though the principal
		// matches.
		{"captain principal but wrong session", investigation.ID, "alice", "", investigationRun, nil},
		// Vary principal: unrelated principal.
		{"unrelated principal in non-captain session", investigation.ID, "bob", "", investigationRun, nil},
		// Vary principal: empty.
		{"empty principal in non-captain session", investigation.ID, "", "", investigationRun, nil},
		// Vary session: empty session ID, any principal.
		{"empty session id, alice principal", "", "alice", "", investigationRun, nil},
		{"empty session id, bob principal", "", "bob", "", investigationRun, nil},
		// Vary idempotency-key: caller supplies one — doesn't change refusal.
		{"caller-supplied idempotency key", investigation.ID, "bob", "caller-key-xyz", investigationRun, nil},
		// Vary runID: empty — the gate still runs upstream.
		{"empty run id", investigation.ID, "bob", "", "", nil},
		// Vary tool args: with source_id field.
		{"tool args carry source_id", investigation.ID, "bob", "", investigationRun, map[string]any{"source_id": "prod-cluster", "id": "x"}},
	}
	for _, p := range permutations {
		t.Run(p.name, func(t *testing.T) {
			ctx := e.ctx
			if p.sessionIDInCtx != "" {
				ctx = agentctx.WithSessionID(ctx, p.sessionIDInCtx)
			}
			if p.principalInCtx != "" {
				ctx = rbac.WithPrincipal(ctx, rbac.Principal(p.principalInCtx))
			}
			if p.idempotencyKeyCtx != "" {
				ctx = agentctx.WithIdempotencyKey(ctx, p.idempotencyKeyCtx)
			}
			if p.runIDInCtx != "" {
				ctx = agentctx.WithRunID(ctx, p.runIDInCtx)
			}
			args := map[string]any{"id": "y"}
			for k, v := range p.extraToolArgs {
				args[k] = v
			}
			_, err := e.wrapper.Execute(ctx, "graph_add_node", args)
			if err == nil {
				t.Errorf("§C5 violation: permutation %q allowed a non-captain mutation in "+
					"incident regime. The gate must refuse under ALL configurations.", p.name)
				return
			}
			var refusal *coreagent.GateRefusalError
			if !errors.As(err, &refusal) {
				t.Errorf("§C5: expected *GateRefusalError, got %T: %v", err, err)
			}
		})
	}

	// Sanity: same wrapper, same permutations of ctx flags, but mutating
	// from the CAPTAIN session by the captain principal → allowed. The
	// test isn't asserting "all permutations refuse" universally — it
	// asserts "all permutations refuse THE NON-CAPTAIN MUTATION".
	captainRun := e.startRun(t, captainSess)
	ctxCaptain := withRunCtx(withCtx(e.ctx, captainSess, "alice"), captainRun)
	if _, err := e.wrapper.Execute(ctxCaptain, "graph_add_node", map[string]any{"id": "ok"}); err != nil {
		t.Errorf("captain-session mutation should be allowed: %v", err)
	}
}

// --- T1 reads bypass the gate too (gate's T1 branch + wrapper's T1
//     bypass align). ---

func TestDurableExecutor_GateAllowsT1ReadsInIncidentRegime(t *testing.T) {
	e := newGateEnv(t)
	captainSess := e.declareWithCaptain(t, "alice")
	// Any T1 tool from any session in incident regime should pass.
	ctx := withCtx(e.ctx, captainSess, "bob") // bob is not captain
	if _, err := e.wrapper.Execute(ctx, "read_file", map[string]any{"path": "/etc/hosts"}); err != nil {
		t.Errorf("T1 read should always pass: %v", err)
	}
}
