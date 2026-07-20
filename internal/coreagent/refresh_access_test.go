package coreagent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/access"
	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// These tests exercise the A001-COREGOV CC-05 refresh read floor END-TO-END
// through the REAL refresh accessor: a *access.Accessor built over the
// promote-aware engine (rbac.NewPolicyEngineWithPromote) and consumed by the
// Refresher under the agent:core principal carried on the refresh context. They
// prove the refresh path is gated (not a registry bypass), that an
// ungranted/unpromoted component is denied before its adapter — and credential —
// is resolved, and that flipping auto_promote_reads ON takes effect live.

// emptyRBACRepo is a minimal rbac.Repository for the refresh access tests. Every
// component resolves to the seeded "unassigned" zone (which allows only read),
// there are no policy grants, and no admins. Under this repo the agent:core
// principal is DENIED on a read unless the promote predicate admits it — which
// is exactly the steady state the refresh floor governs.
type emptyRBACRepo struct{}

func (emptyRBACRepo) GetAssignment(context.Context, string) (*rbac.ComponentZoneAssignment, error) {
	return nil, nil // unassigned -> zone "unassigned"
}

func (emptyRBACRepo) GetZone(_ context.Context, id string) (*rbac.Zone, error) {
	if id == "unassigned" {
		return &rbac.Zone{ID: "unassigned", AllowedActions: []rbac.Action{rbac.ActionRead}}, nil
	}
	return nil, nil
}

func (emptyRBACRepo) ListPoliciesForPrincipal(context.Context, string) ([]rbac.Policy, error) {
	return nil, nil
}
func (emptyRBACRepo) IsAdmin(context.Context, string) (bool, error) { return false, nil }

// Interface completeness (unused by PolicyEngine.Decide).
func (emptyRBACRepo) ListZones(context.Context) ([]rbac.Zone, error) { return nil, nil }
func (emptyRBACRepo) CreateZone(context.Context, rbac.Zone, string) (*rbac.Zone, error) {
	return nil, nil
}
func (emptyRBACRepo) UpdateZone(context.Context, rbac.Zone, string) (*rbac.Zone, error) {
	return nil, nil
}
func (emptyRBACRepo) DeleteZone(context.Context, string, string) error { return nil }
func (emptyRBACRepo) ListAssignments(context.Context) ([]rbac.ComponentZoneAssignment, error) {
	return nil, nil
}
func (emptyRBACRepo) UpsertAssignment(context.Context, rbac.ComponentZoneAssignment, string) error {
	return nil
}
func (emptyRBACRepo) DeleteAssignment(context.Context, string, string) (int64, error) { return 0, nil }
func (emptyRBACRepo) ListPolicies(context.Context) ([]rbac.Policy, error)             { return nil, nil }
func (emptyRBACRepo) CreatePolicy(context.Context, rbac.Policy, string) (*rbac.Policy, error) {
	return nil, nil
}
func (emptyRBACRepo) DeletePolicy(context.Context, int64, string) error { return nil }
func (emptyRBACRepo) DeletePolicyForPrincipalZone(context.Context, string, string, string) (int64, error) {
	return 0, nil
}
func (emptyRBACRepo) DeletePoliciesForPrincipal(context.Context, string) (int64, error) {
	return 0, nil
}
func (emptyRBACRepo) ListUnassignedComponentIDs(context.Context) ([]string, error) { return nil, nil }
func (emptyRBACRepo) ListAdmins(context.Context) ([]rbac.Admin, error)             { return nil, nil }
func (emptyRBACRepo) AddAdmin(context.Context, rbac.Admin, string) error           { return nil }
func (emptyRBACRepo) AddFirstAdmin(context.Context, rbac.Admin, string) (bool, error) {
	return false, nil
}
func (emptyRBACRepo) RemoveAdmin(context.Context, string, string) (int64, error) { return 0, nil }

// fakePromote is a controllable rbac.PromoteReadsResolver: it maps componentID
// to type and type to its auto_promote_reads flag. The map is mutated live to
// simulate an admin flip with no restart.
type fakePromote struct {
	idToType map[string]string
	promoted map[string]bool
}

func (f *fakePromote) ComponentType(_ context.Context, componentID string) (string, error) {
	return f.idToType[componentID], nil
}
func (f *fakePromote) IsPromoted(_ context.Context, componentType string) (bool, error) {
	return f.promoted[componentType], nil
}

// markerAdapter is a registered adapter whose only purpose is to be the handle
// returned on a permitted resolve. resolveAdapter returns nil on a denied
// decision, so "did the credential-bearing handle escape?" is answered by
// whether the returned adapter is this instance (permit) or nil (deny).
type markerAdapter struct{ id string }

func (markerAdapter) Connect(context.Context, store.Component) error { return nil }
func (markerAdapter) Disconnect() error                              { return nil }
func (markerAdapter) Status() adapters.Status                        { return adapters.Status{Connected: true} }

// newGuardedRefresher builds a Refresher wired with a REAL access accessor over
// the promote-aware engine, plus a registry whose single adapter records when it
// is resolved. The registry is also exposed as the Refresher's fallback services
// so the test would fail loudly if resolveAdapter ever bypassed the accessor.
func newGuardedRefresher(t *testing.T, reg *adapters.Registry, promote *fakePromote) *Refresher {
	t.Helper()
	engine := rbac.NewPolicyEngineWithPromote(emptyRBACRepo{}, promote)
	accessor := access.New(reg, nil, engine, nil)
	return &Refresher{
		services: &core.Services{Adapters: reg},
		logger:   slog.Default(),
		accessor: accessor,
	}
}

// agentCoreCtx returns a context carrying the canonical agent:core principal,
// exactly as cmd/joe/server.go stamps it onto the Start ctx (CC-02).
func agentCoreCtx(t *testing.T) context.Context {
	t.Helper()
	p, err := rbac.AgentCorePrincipal()
	if err != nil {
		t.Fatalf("AgentCorePrincipal: %v", err)
	}
	return rbac.WithPrincipal(context.Background(), p)
}

// TestRefreshResolve_Ungranted_Denied: an ungranted + unpromoted component is
// DENIED — the adapter (and its credential) is never resolved.
func TestRefreshResolve_Ungranted_Denied(t *testing.T) {
	reg := adapters.NewRegistry()
	reg.Register("k8s-dev", markerAdapter{id: "k8s-dev"})
	promote := &fakePromote{
		idToType: map[string]string{"k8s-dev": "kubernetes"},
		promoted: map[string]bool{"kubernetes": false}, // OFF
	}
	r := newGuardedRefresher(t, reg, promote)

	adapter, err := r.resolveAdapter(agentCoreCtx(t), &store.Component{ID: "k8s-dev", Type: store.ComponentTypeKubernetes})
	if !errors.Is(err, access.ErrPermissionDenied) {
		t.Fatalf("ungranted+unpromoted component should be denied with ErrPermissionDenied, got: %v", err)
	}
	if adapter != nil {
		t.Fatal("adapter handle escaped despite a denied decision — credential resolution must not occur on deny")
	}
}

// TestRefreshResolve_PromoteOn_Resolved exercises the real refresh accessor
// end-to-end and proves it uses the promote-aware engine AND the agent:core
// principal from ctx: a component whose type has auto_promote_reads ON is
// resolved with NO grant, purely via the promote predicate.
func TestRefreshResolve_PromoteOn_Resolved(t *testing.T) {
	reg := adapters.NewRegistry()
	want := markerAdapter{id: "k8s-dev"}
	reg.Register("k8s-dev", want)
	promote := &fakePromote{
		idToType: map[string]string{"k8s-dev": "kubernetes"},
		promoted: map[string]bool{"kubernetes": true}, // ON
	}
	r := newGuardedRefresher(t, reg, promote)

	adapter, err := r.resolveAdapter(agentCoreCtx(t), &store.Component{ID: "k8s-dev", Type: store.ComponentTypeKubernetes})
	if err != nil {
		t.Fatalf("promoted type should be resolved for agent:core, got error: %v", err)
	}
	if got, ok := adapter.(markerAdapter); !ok || got.id != want.id {
		t.Fatalf("expected the registered adapter to be resolved for the promoted component, got: %#v", adapter)
	}
}

// TestRefreshResolve_LiveFlip_NoRestart: a type OFF is denied, then flipping it
// ON (same engine instance, no restart) admits the NEXT resolve.
func TestRefreshResolve_LiveFlip_NoRestart(t *testing.T) {
	reg := adapters.NewRegistry()
	reg.Register("k8s-dev", markerAdapter{id: "k8s-dev"})
	promote := &fakePromote{
		idToType: map[string]string{"k8s-dev": "kubernetes"},
		promoted: map[string]bool{"kubernetes": false}, // OFF
	}
	r := newGuardedRefresher(t, reg, promote)
	comp := &store.Component{ID: "k8s-dev", Type: store.ComponentTypeKubernetes}
	ctx := agentCoreCtx(t)

	if _, err := r.resolveAdapter(ctx, comp); !errors.Is(err, access.ErrPermissionDenied) {
		t.Fatalf("precondition: flag OFF should deny, got: %v", err)
	}
	// Flip ON live (simulating an admin SetPromoted) — same engine instance.
	promote.promoted["kubernetes"] = true
	if _, err := r.resolveAdapter(ctx, comp); err != nil {
		t.Fatalf("after live flip ON, the next resolve should be admitted, got: %v", err)
	}
}

// TestRefreshResolve_NonAgentCorePrincipal_Denied: the admit comes from the
// agent:core principal ON THE CTX, not a bypass. A context carrying a different
// principal is denied even when the type is promoted (the promote predicate is
// agent:core-only).
func TestRefreshResolve_NonAgentCorePrincipal_Denied(t *testing.T) {
	reg := adapters.NewRegistry()
	reg.Register("k8s-dev", markerAdapter{id: "k8s-dev"})
	promote := &fakePromote{
		idToType: map[string]string{"k8s-dev": "kubernetes"},
		promoted: map[string]bool{"kubernetes": true}, // ON
	}
	r := newGuardedRefresher(t, reg, promote)
	ctx := rbac.WithPrincipal(context.Background(), rbac.Principal("user:alice@example.com"))

	adapter, err := r.resolveAdapter(ctx, &store.Component{ID: "k8s-dev", Type: store.ComponentTypeKubernetes})
	if !errors.Is(err, access.ErrPermissionDenied) {
		t.Fatalf("a non-agent:core principal must be denied even for a promoted type (predicate is agent:core-only), got: %v", err)
	}
	if adapter != nil {
		t.Fatal("adapter handle escaped for a non-agent:core principal despite denial")
	}
}

// TestRefreshCycle_DenialDoesNotAbortCycle exercises the FULL refresh cycle end
// to end through a real store: one component's type is OFF (denied) and another
// is ON (promoted). The denied component must be skipped — not error out and not
// abort the cycle — and the promoted component must still be refreshed (its
// adapter resolved). This is the graceful-degradation contract: denial is an
// expected steady state, not a cycle-killing failure.
func TestRefreshCycle_DenialDoesNotAbortCycle(t *testing.T) {
	svc := makeTestServices(t)
	ctx := agentCoreCtx(t)

	// Two kubernetes components: one stays OFF (denied), one is flipped ON.
	denyComp := &store.Component{ID: "k8s-deny", Name: "deny", Type: store.ComponentTypeKubernetes, Config: jsonRaw()}
	allowComp := &store.Component{ID: "k8s-allow", Name: "allow", Type: store.ComponentTypeKubernetes, Config: jsonRaw()}
	if err := svc.Store.Components.Create(ctx, denyComp); err != nil {
		t.Fatalf("create deny component: %v", err)
	}
	if err := svc.Store.Components.Create(ctx, allowComp); err != nil {
		t.Fatalf("create allow component: %v", err)
	}

	// The denied component gets a credential-tracking adapter; it is denied
	// before the type switch, so its concrete type is irrelevant. The promoted
	// component gets a real k8s-shaped adapter (empty lists) so refreshComponent
	// runs all the way through its type switch and graph write.
	svc.Adapters.Register("k8s-deny", markerAdapter{id: "k8s-deny"})
	svc.Adapters.Register("k8s-allow", &fakeK8sAdapter{items: map[string][]unstructured.Unstructured{}})

	// Only k8s-allow's type ("kubernetes") is promoted; k8s-deny maps to an empty
	// type so the predicate fails closed (deny).
	promote := &fakePromote{
		idToType: map[string]string{"k8s-allow": "kubernetes"},
		promoted: map[string]bool{"kubernetes": true},
	}

	engine := rbac.NewPolicyEngineWithPromote(emptyRBACRepo{}, promote)
	accessor := access.New(svc.Adapters, svc.Graph, engine, nil)

	r := NewRefresher(svc, &mockLLMAdapter{}, slog.Default(), nil)
	r.SetAccessor(accessor)

	if err := r.refresh(ctx); err != nil {
		t.Fatalf("refresh cycle should not fail because one component is denied, got: %v", err)
	}

	// The denied component must NOT have a sync error stamped (it was skipped,
	// not failed). The allowed component must have synced (LastSyncAt set, no
	// error), proving the cycle proceeded past the denial and refreshed it.
	denied, err := svc.Store.Components.Get(ctx, "k8s-deny")
	if err != nil {
		t.Fatalf("get deny component: %v", err)
	}
	if denied.LastError != "" {
		t.Fatalf("denied component should be skipped quietly, not stamped with an error; got LastError=%q", denied.LastError)
	}
	allowed, err := svc.Store.Components.Get(ctx, "k8s-allow")
	if err != nil {
		t.Fatalf("get allow component: %v", err)
	}
	if allowed.LastError != "" {
		t.Fatalf("promoted component should refresh cleanly; got LastError=%q", allowed.LastError)
	}
	if allowed.LastSyncAt == nil {
		t.Fatal("promoted component should have been refreshed (LastSyncAt set) — the cycle must proceed past the denied one")
	}
}

func jsonRaw() json.RawMessage { return json.RawMessage(`{}`) }

// ── A001-COREGOV CC-08: fail-closed accessor seam ───────────────────────────

// TestRefreshResolve_NilAccessor_FailsClosed proves the CC-08 call-site change:
// with no accessor wired, resolveAdapter returns access.ErrPermissionDenied and
// resolves NO adapter — even though the registry holds a live one. Before CC-08
// this path fell open to the raw registry (returning the handle); it now fails
// closed so an absent guarded seam reads nothing.
func TestRefreshResolve_NilAccessor_FailsClosed(t *testing.T) {
	reg := adapters.NewRegistry()
	// A resolvable adapter is registered: if the old fail-open fallback were
	// still present, resolveAdapter would return THIS handle. Fail-closed means
	// it must not.
	reg.Register("k8s-dev", markerAdapter{id: "k8s-dev"})

	r := &Refresher{
		services: &core.Services{Adapters: reg},
		logger:   slog.Default(),
		// accessor deliberately nil — the unwired seam.
	}

	adapter, err := r.resolveAdapter(agentCoreCtx(t), &store.Component{ID: "k8s-dev", Type: store.ComponentTypeKubernetes})
	if !errors.Is(err, access.ErrPermissionDenied) {
		t.Fatalf("nil accessor must fail closed with ErrPermissionDenied, got: %v", err)
	}
	if adapter != nil {
		t.Fatal("nil accessor must resolve NO adapter (no raw registry.Get) — a handle escaped, meaning the fail-open fallback was reintroduced")
	}
}

// TestRefreshComponent_NilAccessor_SkipsQuietly proves the fail-closed denial
// flows into the existing skip-quietly path: refreshComponent returns nil (the
// component is skipped, not errored) when the seam is unwired, so a misconfigured
// binary degrades safely rather than panicking or stamping a sync error.
func TestRefreshComponent_NilAccessor_SkipsQuietly(t *testing.T) {
	// Use a full services stack so the deferred UpdateSyncStatus has a real
	// store; the refresher's accessor is deliberately left nil (the unwired seam).
	svc := makeTestServices(t)
	ctx := agentCoreCtx(t)
	comp := &store.Component{ID: "k8s-dev", Name: "dev", Type: store.ComponentTypeKubernetes, Config: jsonRaw()}
	if err := svc.Store.Components.Create(ctx, comp); err != nil {
		t.Fatalf("create component: %v", err)
	}
	svc.Adapters.Register("k8s-dev", markerAdapter{id: "k8s-dev"})
	r := &Refresher{
		services: svc,
		logger:   slog.Default(),
		// accessor deliberately nil.
	}

	if err := r.refreshComponent(ctx, comp); err != nil {
		t.Fatalf("nil accessor denial must be skipped quietly (nil error), got: %v", err)
	}

	// Skipped quietly means no sync error stamped (the denial is steady state,
	// not a failure).
	got, err := svc.Store.Components.Get(ctx, "k8s-dev")
	if err != nil {
		t.Fatalf("get component: %v", err)
	}
	if got.LastError != "" {
		t.Fatalf("denied component should be skipped without a sync error; got LastError=%q", got.LastError)
	}
}

// TestRefresherStart_RefusesWithoutAccessor proves the CC-08 refuse-to-start
// guard (mirroring D-0027): Refresher.Start returns a FATAL error — not a panic
// — when the refresh accessor was never wired. cmd/joe/server.go propagates this
// as a clean boot failure (return 1). The happy path (accessor wired) is covered
// by the existing Stop/loop tests in supplemental_coverage_test.go.
func TestRefresherStart_RefusesWithoutAccessor(t *testing.T) {
	r := &Refresher{
		services: &core.Services{Adapters: adapters.NewRegistry()},
		logger:   slog.Default(),
		doneCh:   make(chan struct{}),
		// accessor deliberately nil.
	}

	err := r.Start(context.Background())
	if err == nil {
		t.Fatal("Start must refuse to boot the background refresh when the accessor is unwired (fail-closed, mirroring D-0027)")
	}
	// The refusal must be a returned error guiding the operator/maintainer, not
	// a bare sentinel; it names the wiring contract so a maintainer can fix it.
	for _, want := range []string{"refusing to start", "access seam not wired", "ungoverned"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal message missing %q; got: %s", want, err.Error())
		}
	}
}

// TestAgentStart_RefusesWithoutRefreshAccessor proves the refusal propagates
// through Agent.Start (the production boot seam cmd/joe/server.go calls), not
// just the inner Refresher: an Agent created without SetRefreshAccessor refuses
// to start.
func TestAgentStart_RefusesWithoutRefreshAccessor(t *testing.T) {
	svc := makeTestServices(t)
	agent := New(svc, &mockLLMAdapter{}, nil)
	// Deliberately do NOT call agent.SetRefreshAccessor.

	if err := agent.Start(agentCoreCtx(t)); err == nil {
		t.Fatal("Agent.Start must propagate the refresher's refuse-to-start error when no refresh accessor was wired")
	}
}
