package rbac_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jaimegago/joe/internal/rbac"
)

// fakePromote is a controllable rbac.PromoteReadsResolver for the
// auto_promote_reads dynamic admit predicate tests (A001-COREGOV CC-04). It
// maps componentID -> type and type -> ON, and can force lookup errors so the
// fail-closed branches are exercised.
type fakePromote struct {
	idToType    map[string]string
	promoted    map[string]bool
	typeErr     error
	promotedErr error
}

func (f *fakePromote) ComponentType(_ context.Context, componentID string) (string, error) {
	if f.typeErr != nil {
		return "", f.typeErr
	}
	return f.idToType[componentID], nil // "" for unknown id
}

func (f *fakePromote) IsPromoted(_ context.Context, componentType string) (bool, error) {
	if f.promotedErr != nil {
		return false, f.promotedErr
	}
	return f.promoted[componentType], nil
}

// agentCore returns the canonical svc:agent:core principal or fails the test.
func agentCore(t *testing.T) rbac.Principal {
	t.Helper()
	p, err := rbac.AgentCorePrincipal()
	if err != nil {
		t.Fatalf("AgentCorePrincipal: %v", err)
	}
	return p
}

// TestPromoteReads_AgentCoreRead_FlagOn_Permits: agent:core + ActionRead on a
// component whose type has the flag ON is permitted with NO grant row, via the
// auto_promote_read reason.
func TestPromoteReads_AgentCoreRead_FlagOn_Permits(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()
	// k8s-dev is unassigned (zone "unassigned" allows ["read"]); no grant.
	promote := &fakePromote{
		idToType: map[string]string{"k8s-dev": "kubernetes"},
		promoted: map[string]bool{"kubernetes": true},
	}
	engine := rbac.NewPolicyEngineWithPromote(repo, promote)

	dec := engine.Decide(ctx, rbac.NewPrincipalSet(agentCore(t)), "k8s-dev", rbac.ActionRead)
	if !dec.Allowed {
		t.Fatalf("agent:core read of promoted type should be permitted, got deny (reason %q)", dec.Reason)
	}
	if dec.Reason != rbac.ReasonAutoPromoteRead {
		t.Errorf("expected reason %q, got %q", rbac.ReasonAutoPromoteRead, dec.Reason)
	}
}

// TestPromoteReads_AgentCoreRead_FlagOff_Denies: same component with the flag
// OFF and no other grant is denied (no_grant).
func TestPromoteReads_AgentCoreRead_FlagOff_Denies(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()
	promote := &fakePromote{
		idToType: map[string]string{"k8s-dev": "kubernetes"},
		promoted: map[string]bool{"kubernetes": false},
	}
	engine := rbac.NewPolicyEngineWithPromote(repo, promote)

	dec := engine.Decide(ctx, rbac.NewPrincipalSet(agentCore(t)), "k8s-dev", rbac.ActionRead)
	if dec.Allowed {
		t.Fatalf("agent:core read of non-promoted type with no grant should be denied")
	}
	if dec.Reason != rbac.ReasonNoGrant {
		t.Errorf("expected reason %q, got %q", rbac.ReasonNoGrant, dec.Reason)
	}
}

// TestPromoteReads_LiveFlip_NoRestart: a type flipped ON makes its components
// readable on the NEXT decision with the SAME engine instance (engine reads
// live, no cache, no restart).
func TestPromoteReads_LiveFlip_NoRestart(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()
	promote := &fakePromote{
		idToType: map[string]string{"k8s-dev": "kubernetes"},
		promoted: map[string]bool{"kubernetes": false},
	}
	engine := rbac.NewPolicyEngineWithPromote(repo, promote)
	core := rbac.NewPrincipalSet(agentCore(t))

	if engine.Decide(ctx, core, "k8s-dev", rbac.ActionRead).Allowed {
		t.Fatal("precondition: flag OFF should deny")
	}
	// Flip ON (simulating an admin SetPromoted) — same engine instance.
	promote.promoted["kubernetes"] = true
	if !engine.Decide(ctx, core, "k8s-dev", rbac.ActionRead).Allowed {
		t.Fatal("after flip ON, the next decision on the SAME engine should permit (live read, no restart)")
	}
}

// TestPromoteReads_RefusesMutate: agent:core + a mutate action on a promoted
// type is DENIED — auto_promote can never grant mutate, even when the zone
// allows mutate. The component is in prod-write (allows mutate), so the zone
// gate would not block; only the read-only predicate scope does.
func TestPromoteReads_RefusesMutate(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()
	_ = repo.UpsertAssignment(ctx, rbac.ComponentZoneAssignment{
		ComponentID: "k8s-prod", ZoneID: "prod-write", AssignedBy: "test",
	}, "test")
	promote := &fakePromote{
		idToType: map[string]string{"k8s-prod": "kubernetes"},
		promoted: map[string]bool{"kubernetes": true},
	}
	engine := rbac.NewPolicyEngineWithPromote(repo, promote)

	dec := engine.Decide(ctx, rbac.NewPrincipalSet(agentCore(t)), "k8s-prod", rbac.ActionMutate)
	if dec.Allowed {
		t.Fatalf("auto_promote must NEVER admit a mutate, even for a promoted type in a mutate-capable zone")
	}
	if dec.Reason == rbac.ReasonAutoPromoteRead {
		t.Errorf("mutate denial must not carry the auto_promote_read reason, got %q", dec.Reason)
	}
}

// TestPromoteReads_NonAgentCore_PredicateInert: a non-agent:core principal with
// the type ON does not fire the predicate — normal grant logic decides,
// unchanged. With no grant the principal is denied.
func TestPromoteReads_NonAgentCore_PredicateInert(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()
	promote := &fakePromote{
		idToType: map[string]string{"k8s-dev": "kubernetes"},
		promoted: map[string]bool{"kubernetes": true},
	}
	engine := rbac.NewPolicyEngineWithPromote(repo, promote)

	dec := engine.Decide(ctx, rbac.NewPrincipalSet("user:alice@example.com"), "k8s-dev", rbac.ActionRead)
	if dec.Allowed {
		t.Fatalf("non-agent:core principal must not be admitted by the promote predicate; got allow (reason %q)", dec.Reason)
	}
	if dec.Reason == rbac.ReasonAutoPromoteRead {
		t.Errorf("non-agent:core denial must not carry the auto_promote_read reason, got %q", dec.Reason)
	}
}

// TestPromoteReads_UnknownComponent_FailsClosed: agent:core + read on an
// unknown/missing componentID (empty resolved type) is denied — fail closed.
func TestPromoteReads_UnknownComponent_FailsClosed(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()
	promote := &fakePromote{
		idToType: map[string]string{}, // no mapping -> "" resolved type
		promoted: map[string]bool{"kubernetes": true},
	}
	engine := rbac.NewPolicyEngineWithPromote(repo, promote)

	dec := engine.Decide(ctx, rbac.NewPrincipalSet(agentCore(t)), "does-not-exist", rbac.ActionRead)
	if dec.Allowed {
		t.Fatalf("unknown componentID under agent:core+read must fail closed (deny)")
	}
}

// TestPromoteReads_LookupError_FailsClosed: a component-type resolve error
// under agent:core+read does not admit (fail closed).
func TestPromoteReads_LookupError_FailsClosed(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()
	promote := &fakePromote{
		typeErr: errors.New("boom"),
	}
	engine := rbac.NewPolicyEngineWithPromote(repo, promote)

	dec := engine.Decide(ctx, rbac.NewPrincipalSet(agentCore(t)), "k8s-dev", rbac.ActionRead)
	if dec.Allowed {
		t.Fatalf("component-type lookup error under agent:core+read must fail closed (deny)")
	}
}

// TestPromoteReads_NilResolver_BehaviourNeutral: an engine built WITHOUT a
// resolver (the default NewPolicyEngine) never fires the predicate — even for
// agent:core + read — so it behaves exactly as before CC-04. With no grant the
// agent:core principal is denied.
func TestPromoteReads_NilResolver_BehaviourNeutral(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()
	engine := rbac.NewPolicyEngine(repo) // no resolver

	dec := engine.Decide(ctx, rbac.NewPrincipalSet(agentCore(t)), "k8s-dev", rbac.ActionRead)
	if dec.Allowed {
		t.Fatalf("nil-resolver engine must not admit via the promote predicate; got allow (reason %q)", dec.Reason)
	}
	if dec.Reason == rbac.ReasonAutoPromoteRead {
		t.Errorf("nil-resolver engine must never produce the auto_promote_read reason, got %q", dec.Reason)
	}
}
