package safety

import "testing"

// TestClassifyRepoSearchIsRead is the action-class break-test for the
// repo_search core tool. repo_search reads file contents out of a git
// component's local clone at a pinned commit, through the same governed read
// accessor as git_read, and mutates no managed system — so it must classify as
// ActionRead and pass the write floor unconditionally.
//
// The load-bearing invariant: ClassifyTool defaults UNKNOWN tools to
// ActionMutate (deny-by-default). If the explicit repo_search row in
// toolRegistry is removed or mis-typed, this test fails — catching the silent
// regression where a read tool would become floor-blocked and policy-gated, and
// would stop running in observation mode with nothing else failing.
func TestClassifyRepoSearchIsRead(t *testing.T) {
	c := ClassifyTool("repo_search")
	if c.Class != ActionRead {
		t.Fatalf("ClassifyTool(\"repo_search\").Class = %v, want ActionRead — "+
			"a Mutate/default classification would floor-block and policy-gate the tool", c.Class)
	}

	// A Read passes CheckAccess unconditionally, even under the default policy
	// (which denies mutates by default).
	if err := CheckAccess("repo_search", DefaultPolicy()); err != nil {
		t.Fatalf("CheckAccess(\"repo_search\", DefaultPolicy()) = %v, want nil (reads are always allowed)", err)
	}

	// repo_search is naturally re-runnable: the same arguments against the same
	// commit produce the same answer, so an in-run retry cannot duplicate
	// anything. It must not opt into durability (D-0020).
	if c.NeedsDurability {
		t.Error("repo_search declares NeedsDurability; a deterministic read needs no idempotency key")
	}
}
