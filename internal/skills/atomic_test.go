package skills

import (
	"testing"
)

func TestAtomicRouter_NilSafe(t *testing.T) {
	var a *AtomicRouter
	if a.Snapshot() != nil {
		t.Error("nil AtomicRouter.Snapshot must return nil")
	}
	a.Set(NewRouter(NewRegistry())) // must not panic
}

func TestAtomicRouter_StoresAndSwaps(t *testing.T) {
	first := NewRouter(buildRegistry(t,
		mustParse(t, "alpha", "first skill"),
	))
	a := NewAtomicRouter(first)

	if got := a.Snapshot(); got != first {
		t.Errorf("Snapshot before swap = %v, want %v", got, first)
	}

	second := NewRouter(buildRegistry(t,
		mustParse(t, "beta", "second skill"),
	))
	a.Set(second)

	if got := a.Snapshot(); got != second {
		t.Errorf("Snapshot after swap = %v, want %v", got, second)
	}
}

func TestAtomicRouter_SnapshotIsStableForCaller(t *testing.T) {
	// A chain that captures a snapshot at the start of reasoning must
	// continue to see that snapshot even if the registry is swapped
	// mid-chain. This is the snapshot-per-reasoning-chain guarantee
	// from docs/reference/joe-skills-design.md.
	old := NewRouter(buildRegistry(t, mustParse(t, "alpha", "old description")))
	a := NewAtomicRouter(old)

	snap := a.Snapshot()
	a.Set(NewRouter(buildRegistry(t, mustParse(t, "beta", "new description"))))

	if snap != old {
		t.Errorf("captured snapshot drifted after Set; got %v, want %v", snap, old)
	}
	if got := snap.Registry().Get("alpha"); got == nil {
		t.Error("snapshot lost original skill after concurrent swap")
	}
}
