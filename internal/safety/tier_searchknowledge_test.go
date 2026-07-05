package safety

import "testing"

// TestClassifySearchKnowledgeIsRead is the action-class break-test for the
// search_knowledge core tool. It reads Joe's own knowledge store and mutates no
// managed system, so it must classify as ActionRead and pass the write floor
// unconditionally.
//
// The load-bearing invariant: search_knowledge is registered on the user task
// loop (internal/tools/default.go) and advertised to the model. ClassifyTool
// defaults UNKNOWN tools to ActionMutate (deny-by-default), so a missing or
// mis-typed toolRegistry row would make every search_knowledge call
// floor-blocked and policy-denied even though it only reads. This test catches
// that silent regression.
func TestClassifySearchKnowledgeIsRead(t *testing.T) {
	c := ClassifyTool("search_knowledge")
	if c.Class != ActionRead {
		t.Fatalf("ClassifyTool(\"search_knowledge\").Class = %v, want ActionRead — "+
			"a Mutate/default classification would floor-block and policy-gate the tool", c.Class)
	}

	if err := CheckAccess("search_knowledge", DefaultPolicy()); err != nil {
		t.Fatalf("CheckAccess(\"search_knowledge\", DefaultPolicy()) = %v, want nil (reads are always allowed)", err)
	}
}
