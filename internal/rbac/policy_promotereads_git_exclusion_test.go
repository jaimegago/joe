package rbac_test

import (
	"context"
	"testing"

	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
)

// The auto_promote_reads exclusion for git (D-0150).
//
// A git read is not a read in the sense the auto_promote_reads bargain was made
// about. Every other promotable type answers a read by querying a backend the
// operator already pointed Joe at; the git adapter answers one by cloning or
// pulling the repository — an OUTBOUND FETCH plus a DISK WRITE under Joe's home
// directory, on the autonomous refresher's schedule with no human in the loop.
// Granting agent:core reads of repositories therefore stays a deliberate,
// per-component grant.
//
// The exclusion is enforced at the PREDICATE, not only at the admin setter,
// because the flag resolver queries agent_read_promotions by type string and
// never consults the registrable-type enum: a row written by any other means
// would otherwise keep admitting.

// TestPromoteReads_GitExcluded_EvenWithFlagOn is the pin. It sets up exactly the
// state that admits for every non-excluded type — agent:core, ActionRead, an
// unassigned component, and the type's flag ON — and asserts git is denied
// anyway. The companion assertion in the same test runs the identical setup
// against kubernetes and shows it DOES admit, so a failure here means the
// exclusion stopped working rather than that the predicate broke generally.
func TestPromoteReads_GitExcluded_EvenWithFlagOn(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	promote := &fakePromote{
		idToType: map[string]string{
			"repo-pub": store.ComponentTypeGit,
			"k8s-dev":  store.ComponentTypeKubernetes,
		},
		promoted: map[string]bool{
			store.ComponentTypeGit:        true,
			store.ComponentTypeKubernetes: true,
		},
	}
	engine := rbac.NewPolicyEngineWithPromote(repo, promote)

	// The shape being prevented: without the exclusion this ON row would admit,
	// exactly as the kubernetes row below does.
	dec := engine.Decide(ctx, rbac.NewPrincipalSet(agentCore(t)), "repo-pub", rbac.ActionRead)
	if dec.Allowed {
		t.Fatalf("agent:core was admitted to read a git component via auto_promote_reads (reason %q); "+
			"a git read performs an outbound fetch and a disk write, so it must require a deliberate grant", dec.Reason)
	}
	if dec.Reason != rbac.ReasonNoGrant {
		t.Errorf("excluded type should fall through to normal grant logic (reason %q), got %q",
			rbac.ReasonNoGrant, dec.Reason)
	}

	// Control: the identical setup on a non-excluded type admits, proving the
	// denial above is the exclusion and not a broken predicate.
	if dec := engine.Decide(ctx, rbac.NewPrincipalSet(agentCore(t)), "k8s-dev", rbac.ActionRead); !dec.Allowed {
		t.Fatalf("control: kubernetes with the flag ON should still be admitted, got deny (reason %q)", dec.Reason)
	}
}

// TestAutoPromotableReadType_Declaration pins the shared declaration both the
// engine and the admin surface read, so the two cannot disagree about which types
// are excluded.
func TestAutoPromotableReadType_Declaration(t *testing.T) {
	if rbac.AutoPromotableReadType(store.ComponentTypeGit) {
		t.Error("git must be excluded from auto-promoted reads")
	}
	for _, ct := range []string{
		store.ComponentTypeKubernetes,
		store.ComponentTypePrometheus,
		store.ComponentTypeGitHub,
		store.ComponentTypeGitLab,
	} {
		if !rbac.AutoPromotableReadType(ct) {
			t.Errorf("%q must remain auto-promotable; the exclusion is git-specific", ct)
		}
	}
}
