package prompts

import (
	"strings"
	"testing"
)

// TestTaskSystem_ResolveBeforeActing pins the ONE rule the resolve-component
// work put in the task system prompt.
//
// Why it belongs here rather than in the tool's own description: a tool
// description is read only once the model is already considering that tool, so
// it cannot establish ORDERING. "Resolve the phrase before you act on it" has
// to reach the model before it picks a tool, which means the system prompt or
// nothing. Everything else about the tool — how to read a candidate's
// bindings, what to do with several candidates, when to fall back — lives in
// the tool description, where it is co-located, locally changeable, and not
// competing for a contended global surface. This test asserts that split holds:
// the ordering rule is present, and the usage guidance has not crept in beside
// it.
func TestTaskSystem_ResolveBeforeActing(t *testing.T) {
	for _, required := range []string{
		"resolve_component",
		"before you use it as a component_id",
	} {
		if !strings.Contains(TaskSystem, required) {
			t.Errorf("TaskSystem must carry the resolve-first ordering rule (missing %q)", required)
		}
	}
}

// TestTaskSystem_ResolveRuleDoesNotConflateWithPosture is the D-0101 guard,
// applied to a new prompt section rather than to the posture strings.
//
// D-0101 was a live-cluster incident: wording that described what the model
// could offer INSTEAD of reading was followed literally, and the model deferred
// reads to the operator. The failure mode is a prompt clause that a weak model
// can read as permission to stop. A resolve-first rule is exposed to exactly
// that class of misreading — "resolve before acting" can be heard as "if you
// cannot resolve, do not act" — so the wording states that resolution is a read
// and always available, and that an empty result is not a stopping point.
//
// D-0104 is the other half and needs no assertion of its own: it declined to
// reword per-tool capability strings because a capability statement attached to
// one tool cannot express a system-wide posture. The rule pinned above is an
// ordering instruction with no capability or posture claim in it, which is why
// it can live in the shared prompt without reopening that axis.
func TestTaskSystem_ResolveRuleDoesNotConflateWithPosture(t *testing.T) {
	for _, required := range []string{
		"Resolution is a read and is always available to you",
		"An empty result is an answer, not a wall",
		"keep investigating with the tools you can reach",
	} {
		if !strings.Contains(TaskSystem, required) {
			t.Errorf("the resolve rule must not read as a restriction or a stopping point (missing %q)", required)
		}
	}

	// The rule must not tell the model to hand the task back on a failure to
	// resolve. This is the precise shape of the D-0101 clause that cost a live
	// investigation.
	lower := strings.ToLower(TaskSystem)
	for _, forbidden := range []string{
		"ask the operator which component",
		"stop and ask the operator",
	} {
		if strings.Contains(lower, forbidden) {
			t.Errorf("TaskSystem must not direct the model to defer to the operator on a failed resolution (found %q)", forbidden)
		}
	}
}
