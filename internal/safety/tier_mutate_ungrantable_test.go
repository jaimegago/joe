package safety

import (
	"reflect"
	"testing"
)

// allActionsEnabledPolicy returns a policy with EVERY act toggle turned on —
// the most permissive configuration the policy shape can express.
//
// The toggles are set by reflection over ActPolicy rather than field by field
// so that a toggle added later is enabled automatically. A hand-written literal
// would silently leave a new field false, which is the *restrictive* direction
// and would weaken the assertion below into a tautology.
func allActionsEnabledPolicy(t *testing.T) *SafetyPolicy {
	t.Helper()

	p := DefaultPolicy()
	act := reflect.ValueOf(&p.Act).Elem()

	enabled := 0
	for i := 0; i < act.NumField(); i++ {
		f := act.Field(i)
		if f.Type() != reflect.TypeFor[ActionToggle]() {
			continue
		}
		f.Set(reflect.ValueOf(ActionToggle{Enabled: true}))
		enabled++
	}

	if enabled == 0 {
		t.Fatal("ActPolicy carries no ActionToggle fields — this helper enabled nothing, " +
			"so any assertion built on it is vacuous")
	}
	return p
}

// TestRegisteredMutatesAreUngrantable pins the property the security authority
// asserts in docs/reference/security-in-layers.md §3.2: every registered Mutate
// tool is denied *unconditionally*, not merely denied until an operator opts in.
//
// The mechanism is disjointness between two hardcoded sets. IsT3Allowed
// (policy.go) switches on k8s_write / pagerduty_ack / alertmanager_silence /
// git_push; the ActionMutate rows in toolRegistry declare github_comment /
// gitlab_comment / github_request_changes. No key is in both, so every lookup
// falls to `default: return false` and CheckAccess's allow branch is reachable
// by no real tool name.
//
// TestCheckAccess_MutateDefaultDeny pins the weaker claim — denial under
// DefaultPolicy() — which would still pass if a key were grantable but merely
// defaulted off. This test closes that gap by granting everything the policy
// shape can grant and asserting denial anyway.
//
// A failure here is NOT a safety regression. The property is a consequence of
// what D-0113 deleted (publish_doc_update_git under git_push was the last tool
// reaching the allow branch), not a designed invariant. If this fails, someone
// has shipped a tool with a live policy key — which is legitimate work, and the
// correct response is to revise the doc claim and this test together, not to
// revert the tool. See docs/backlog/act-policy-vestigial.md.
//
// The Mutate set is derived from toolRegistry, never listed here: a hand-kept
// list would have to be edited by the same change this test exists to catch
// (D-0032 — do not hardcode a count or a roster that the tree already owns).
func TestRegisteredMutatesAreUngrantable(t *testing.T) {
	policy := allActionsEnabledPolicy(t)

	mutates := 0
	for name, c := range toolRegistry {
		if c.Class != ActionMutate {
			continue
		}
		mutates++

		if policy.IsT3Allowed(c.PolicyKey) {
			t.Errorf("IsT3Allowed(%q) = true for registered Mutate tool %q under a policy "+
				"with every act toggle enabled — the tool is now grantable, so the "+
				"\"denied unconditionally, regardless of configuration\" claim in "+
				"docs/reference/security-in-layers.md §3.2 and in docs/project/SITE-CLAIMS.md "+
				"is no longer true and must be revised", c.PolicyKey, name)
		}

		if err := CheckAccess(name, policy); err == nil {
			t.Errorf("CheckAccess(%q) = nil under a policy with every act toggle enabled, "+
				"want denied — CheckAccess's allow branch is now reachable by a real tool name", name)
		}
	}

	// Sanity: a registry that classified nothing as Mutate would pass the loop
	// above vacuously and hide the very regression this guards.
	if mutates == 0 {
		t.Fatal("toolRegistry contains no ActionMutate rows — the assertion above is vacuous")
	}
}
