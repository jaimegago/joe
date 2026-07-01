package safety

import "testing"

// TestClassifyWebSearchIsRead is the action-class break-test for the web_search
// shared tool. web_search discovers URLs via the operator-configured search
// engine and mutates no managed system, so it must classify as ActionRead and
// pass the write floor unconditionally.
//
// The load-bearing invariant: ClassifyTool defaults UNKNOWN tools to
// ActionMutate (deny-by-default). If the explicit web_search row in
// toolRegistry is removed or mis-typed, this test fails — catching the silent
// regression where web_search would become floor-blocked and policy-gated.
func TestClassifyWebSearchIsRead(t *testing.T) {
	c := ClassifyTool("web_search")
	if c.Class != ActionRead {
		t.Fatalf("ClassifyTool(\"web_search\").Class = %v, want ActionRead — "+
			"a Mutate/default classification would floor-block and policy-gate the tool", c.Class)
	}

	// A Read passes CheckAccess unconditionally, even under the default policy
	// (which denies mutates by default). This confirms web_search is never
	// gated behind an act opt-in.
	if err := CheckAccess("web_search", DefaultPolicy()); err != nil {
		t.Fatalf("CheckAccess(\"web_search\", DefaultPolicy()) = %v, want nil (reads are always allowed)", err)
	}
}
