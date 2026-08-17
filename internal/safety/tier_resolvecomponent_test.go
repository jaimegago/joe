package safety

import "testing"

// TestClassifyResolveComponentIsRead is the action-class break-test for the
// resolve_component core tool. resolve_component reads the component registry
// and, through the governed read accessor, each permitted candidate's graph
// bindings. It mutates no managed system — so it must classify as ActionRead
// and pass the write floor unconditionally.
//
// The load-bearing invariant: ClassifyTool defaults UNKNOWN tools to
// ActionMutate (deny-by-default). If the explicit resolve_component row in
// toolRegistry is removed or mis-typed, this test fails — catching the silent
// regression where the NAMING hop becomes floor-blocked and policy-gated. That
// regression is worse than it is for most reads: with resolution unavailable in
// observation mode, the loop does not stop, it carries on choosing component_id
// values by guesswork, and nothing else in the suite goes red.
func TestClassifyResolveComponentIsRead(t *testing.T) {
	c := ClassifyTool("resolve_component")
	if c.Class != ActionRead {
		t.Fatalf("ClassifyTool(\"resolve_component\").Class = %v, want ActionRead — "+
			"a Mutate/default classification would floor-block and policy-gate the tool", c.Class)
	}

	// A Read passes CheckAccess unconditionally, even under the default policy
	// (which denies mutates by default).
	if err := CheckAccess("resolve_component", DefaultPolicy()); err != nil {
		t.Fatalf("CheckAccess(\"resolve_component\", DefaultPolicy()) = %v, want nil (reads are always allowed)", err)
	}

	// Resolution is naturally re-runnable: the same phrase against the same
	// registry and graph produces the same candidates, so an in-run retry
	// cannot duplicate anything. It must not opt into durability (D-0020).
	if c.NeedsDurability {
		t.Error("resolve_component declares NeedsDurability; a deterministic read needs no idempotency key")
	}
}
