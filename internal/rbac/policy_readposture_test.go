package rbac_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jaimegago/joe/internal/rbac"
)

// fakePosture is a controllable rbac.ReadPostureResolver for the read-posture
// admit tests (read-posture-latch). It returns a fixed posture and can force a
// resolve error so the fail-to-zoned branch is exercised.
type fakePosture struct {
	posture string
	err     error
}

func (f *fakePosture) ReadPosture(context.Context) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.posture, nil
}

// TestReadPosture_TeamFlat_UngrantedRead_Allowed is the break-test for the
// team_flat admit: an authenticated principal with NO grant is ALLOWED on a read
// of any component, via the team_flat_read reason. The component is "unassigned"
// (zone allows ["read"]); there is no policy grant and the principal is not
// admin — under the grant-based (zoned) decision this would be a no_grant deny.
func TestReadPosture_TeamFlat_UngrantedRead_Allowed(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()
	posture := &fakePosture{posture: rbac.PostureTeamFlat}
	engine := rbac.NewPolicyEngineWithGovernance(repo, nil, posture)

	dec := engine.Decide(ctx, rbac.NewPrincipalSet("user:alice@example.com"), "some-component", rbac.ActionRead)
	if !dec.Allowed {
		t.Fatalf("team_flat: ungranted authenticated read should be allowed, got deny (reason %q)", dec.Reason)
	}
	if dec.Reason != rbac.ReasonTeamFlatRead {
		t.Errorf("expected reason %q, got %q", rbac.ReasonTeamFlatRead, dec.Reason)
	}
}

// TestReadPosture_TeamFlat_NeverWidensMutate proves the admit is read-only: under
// team_flat, an ungranted mutate is STILL denied. The posture must never affect
// the mutate axis. The component is in prod-write (zone allows mutate) so the
// zone gate would not block; only the read-only scope of the admit keeps it from
// firing — the decision falls through to the grant logic and denies (no_grant).
func TestReadPosture_TeamFlat_NeverWidensMutate(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()
	if err := repo.UpsertAssignment(ctx, rbac.ComponentZoneAssignment{
		ComponentID: "k8s-prod", ZoneID: "prod-write", AssignedBy: "test",
	}, "test"); err != nil {
		t.Fatalf("UpsertAssignment: %v", err)
	}
	posture := &fakePosture{posture: rbac.PostureTeamFlat}
	engine := rbac.NewPolicyEngineWithGovernance(repo, nil, posture)

	dec := engine.Decide(ctx, rbac.NewPrincipalSet("user:alice@example.com"), "k8s-prod", rbac.ActionMutate)
	if dec.Allowed {
		t.Fatalf("team_flat must NEVER admit a mutate, even in a mutate-capable zone")
	}
	if dec.Reason == rbac.ReasonTeamFlatRead {
		t.Errorf("mutate denial must not carry the team_flat_read reason, got %q", dec.Reason)
	}
}

// TestReadPosture_TeamFlat_EmptyPrincipalSet_NotAdmitted proves the admit
// requires an authenticated caller: an empty principal set is not admitted even
// under team_flat. (Unauthenticated callers are rejected at the edge and never
// reach the engine; this guards the engine's own contract.)
func TestReadPosture_TeamFlat_EmptyPrincipalSet_NotAdmitted(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()
	posture := &fakePosture{posture: rbac.PostureTeamFlat}
	engine := rbac.NewPolicyEngineWithGovernance(repo, nil, posture)

	dec := engine.Decide(ctx, rbac.NewPrincipalSet(), "some-component", rbac.ActionRead)
	if dec.Allowed {
		t.Fatalf("team_flat must not admit an empty principal set; got allow (reason %q)", dec.Reason)
	}
	if dec.Reason == rbac.ReasonTeamFlatRead {
		t.Errorf("empty-set denial must not carry the team_flat_read reason, got %q", dec.Reason)
	}
}

// TestReadPosture_Zoned_ByteIdentical is the regression test: under the zoned
// posture the read decision is byte-identical to the pre-posture grant-based
// behaviour — granted allow, ungranted deny, in-zone and out-of-zone outcomes
// unchanged. Each case is checked against BOTH a zoned engine and a bare
// (nil-resolver) engine, asserting the two produce the SAME Decision.
func TestReadPosture_Zoned_ByteIdentical(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	// granted: alice holds a grant on prod-readonly; bob holds none.
	if err := repo.UpsertAssignment(ctx, rbac.ComponentZoneAssignment{
		ComponentID: "ro-comp", ZoneID: "prod-readonly", AssignedBy: "test",
	}, "test"); err != nil {
		t.Fatalf("UpsertAssignment ro-comp: %v", err)
	}
	if _, err := repo.CreatePolicy(ctx, rbac.Policy{Principal: "user:alice@example.com", ZoneID: "prod-readonly"}, "test"); err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}

	zoned := rbac.NewPolicyEngineWithGovernance(repo, nil, &fakePosture{posture: rbac.PostureZoned})
	bare := rbac.NewPolicyEngine(repo)

	cases := []struct {
		name       string
		principals rbac.PrincipalSet
		component  string
		action     rbac.Action
		wantAllow  bool
		wantReason string
	}{
		{"granted-in-zone-read", rbac.NewPrincipalSet("user:alice@example.com"), "ro-comp", rbac.ActionRead, true, rbac.ReasonPolicyAllow},
		{"ungranted-in-zone-read", rbac.NewPrincipalSet("user:bob@example.com"), "ro-comp", rbac.ActionRead, false, rbac.ReasonNoGrant},
		// out-of-zone: prod-readonly does not allow mutate at all.
		{"granted-out-of-zone-mutate", rbac.NewPrincipalSet("user:alice@example.com"), "ro-comp", rbac.ActionMutate, false, rbac.ReasonActionNotInZone},
		// unassigned default zone allows only read.
		{"ungranted-unassigned-read", rbac.NewPrincipalSet("user:bob@example.com"), "no-assignment", rbac.ActionRead, false, rbac.ReasonNoGrant},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			zd := zoned.Decide(ctx, tc.principals, tc.component, tc.action)
			bd := bare.Decide(ctx, tc.principals, tc.component, tc.action)
			if zd != bd {
				t.Fatalf("zoned decision %+v must be byte-identical to the bare (pre-posture) decision %+v", zd, bd)
			}
			if zd.Allowed != tc.wantAllow || zd.Reason != tc.wantReason {
				t.Fatalf("got {allow=%v reason=%q}, want {allow=%v reason=%q}", zd.Allowed, zd.Reason, tc.wantAllow, tc.wantReason)
			}
		})
	}
}

// TestReadPosture_LiveFlip_NoRestart: flipping the posture takes effect on the
// NEXT decision with the SAME engine instance (the engine reads live, no cache,
// no restart) — the migration property an operator relies on for the zoned flip.
func TestReadPosture_LiveFlip_NoRestart(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()
	posture := &fakePosture{posture: rbac.PostureTeamFlat}
	engine := rbac.NewPolicyEngineWithGovernance(repo, nil, posture)
	bob := rbac.NewPrincipalSet("user:bob@example.com")

	if !engine.Decide(ctx, bob, "some-component", rbac.ActionRead).Allowed {
		t.Fatal("precondition: team_flat should admit the ungranted read")
	}
	// Flip to zoned (simulating an admin SetPosture) — same engine instance.
	posture.posture = rbac.PostureZoned
	dec := engine.Decide(ctx, bob, "some-component", rbac.ActionRead)
	if dec.Allowed {
		t.Fatal("after flip to zoned, the ungranted read must deny on the next decision (live read, no restart)")
	}
	if dec.Reason != rbac.ReasonNoGrant {
		t.Errorf("expected no_grant after flip to zoned, got %q", dec.Reason)
	}
}

// TestReadPosture_ResolveError_FallsBackToZoned: a posture-resolve error does NOT
// admit — the decision falls through to the grant-based (zoned) logic, which
// denies an ungranted read. The narrower direction is the safe one.
func TestReadPosture_ResolveError_FallsBackToZoned(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()
	posture := &fakePosture{err: errors.New("boom")}
	engine := rbac.NewPolicyEngineWithGovernance(repo, nil, posture)

	dec := engine.Decide(ctx, rbac.NewPrincipalSet("user:bob@example.com"), "some-component", rbac.ActionRead)
	if dec.Allowed {
		t.Fatalf("a posture-resolve error must fall back to zoned and deny an ungranted read, got allow (reason %q)", dec.Reason)
	}
	if dec.Reason == rbac.ReasonTeamFlatRead {
		t.Errorf("error fallback must not carry the team_flat_read reason, got %q", dec.Reason)
	}
}

// TestReadPosture_NilResolver_BehaviourNeutral: an engine built WITHOUT a posture
// resolver (NewPolicyEngine / NewPolicyEngineWithPromote) never fires the admit,
// so it behaves exactly as the grant-based (zoned) decision did before the
// posture existed. With no grant the principal is denied.
func TestReadPosture_NilResolver_BehaviourNeutral(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()
	engine := rbac.NewPolicyEngine(repo) // no posture resolver

	dec := engine.Decide(ctx, rbac.NewPrincipalSet("user:bob@example.com"), "some-component", rbac.ActionRead)
	if dec.Allowed {
		t.Fatalf("nil-resolver engine must not admit via the posture admit; got allow (reason %q)", dec.Reason)
	}
	if dec.Reason == rbac.ReasonTeamFlatRead {
		t.Errorf("nil-resolver engine must never produce the team_flat_read reason, got %q", dec.Reason)
	}
}
