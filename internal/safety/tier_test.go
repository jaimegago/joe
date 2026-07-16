package safety

import (
	"errors"
	"testing"
)

func TestClassifyTool_KnownTools(t *testing.T) {
	tests := []struct {
		tool      string
		wantClass ActionClass
	}{
		// Read
		{"list_components", ActionRead},
		{"graph_query", ActionRead},
		{"graph_related", ActionRead},
		{"k8s_get", ActionRead},
		{"k8s_logs", ActionRead},
		{"git_read", ActionRead},
		{"git_log", ActionRead},
		{"git_diff", ActionRead},
		{"aws_ec2", ActionRead},
		{"aws_eks", ActionRead},
		{"aws_rds", ActionRead},
		{"aws_vpc", ActionRead},

		// Phase 6.3: Observability
		{"prometheus_query", ActionRead},
		{"loki_query", ActionRead},
		{"tempo_search", ActionRead},
		{"jaeger_traces", ActionRead},

		// Phase 6.4: Alerting & dashboards
		{"alertmanager_alerts", ActionRead},
		{"pagerduty_incidents", ActionRead},
		{"grafana_dashboards", ActionRead},

		// Joe's own model maintenance — read-class per D-0018/D-0019.
		// These record observed state into Joe's own graph/store; the
		// managed system is unchanged, so they are reads, not writes.
		{"graph_add_node", ActionRead},
		{"graph_add_edge", ActionRead},
		{"graph_update_node", ActionRead},
		{"register_component", ActionRead},
		{"save_onboarding_fact", ActionRead},
		{"registry_query", ActionRead},
		{"artifactory_query", ActionRead},
		{"ecr_query", ActionRead},

		// Mutate — managed-system mutations
		{"github_comment", ActionMutate},
		{"gitlab_comment", ActionMutate},
		{"github_request_changes", ActionMutate},
	}

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			c := ClassifyTool(tt.tool)
			if c.Class != tt.wantClass {
				t.Errorf("ClassifyTool(%q).Class = %v, want %v", tt.tool, c.Class, tt.wantClass)
			}
		})
	}
}

func TestClassifyTool_UnknownTool(t *testing.T) {
	c := ClassifyTool("evil_tool_from_prompt_injection")
	if c.Class != ActionMutate {
		t.Errorf("unknown tool class = %v, want ActionMutate (deny by default)", c.Class)
	}
}

// TestActionClass_IsBinary is the break-test for the tier collapse (D-0018/
// D-0019): the action classification has exactly two states, Read and Mutate.
// The former middle tier (Record) is gone from the type. If anyone re-adds a
// third class, this fails loudly.
func TestActionClass_IsBinary(t *testing.T) {
	// The only valid classes are ActionRead and ActionMutate, and they are
	// distinct. There is no third (former Record) constant.
	if ActionRead == ActionMutate {
		t.Fatal("ActionRead and ActionMutate must be distinct")
	}
	// Every classification ClassifyTool can return must be one of the two.
	for tool, c := range toolRegistry {
		if c.Class != ActionRead && c.Class != ActionMutate {
			t.Errorf("tool %q has class %v, which is neither ActionRead nor ActionMutate — the classification must be binary", tool, c.Class)
		}
	}
	// The unknown-tool default is also one of the two.
	def := ClassifyTool("definitely_not_a_registered_tool").Class
	if def != ActionRead && def != ActionMutate {
		t.Errorf("unknown-tool default %v is not a valid binary class", def)
	}
}

// TestClassifyTool_UnknownDefaultIsMutate guards the deny-by-default floor: an
// unregistered tool must classify as Mutate (the conservative side) so a
// prompt-injected or newly-added tool can never run unclassified as a read.
func TestClassifyTool_UnknownDefaultIsMutate(t *testing.T) {
	def := ClassifyTool("definitely_not_a_registered_tool").Class
	if def != ActionMutate {
		t.Errorf("unknown-tool default = %v, want ActionMutate (deny by default)", def)
	}
}

// TestClassifyTool_GraphMutationFamilyIsRead is the break-test for the
// D-0018/D-0019 reclassification: Joe's graph-mutation family maintains Joe's
// OWN model and must stay read-class. Re-promoting them to mutate would freeze
// Joe's model whenever safe mode or an incident captain gate is engaged. If
// someone bumps these to Mutate, this fails loudly.
func TestClassifyTool_GraphMutationFamilyIsRead(t *testing.T) {
	for _, tool := range []string{"graph_add_node", "graph_add_edge", "graph_update_node"} {
		if got := ClassifyTool(tool).Class; got != ActionRead {
			t.Errorf("ClassifyTool(%q).Class = %v, want ActionRead (Joe's own model maintenance)", tool, got)
		}
	}
}

// TestClassifyTool_ExternalCommentsAreMutate locks the comment tools at the
// write floor. Posting to a PR/MR mutates an external system, so it must not
// silently regress to read-class (which would skip the act-policy gate and the
// blocking pre-execution notification).
func TestClassifyTool_ExternalCommentsAreMutate(t *testing.T) {
	for _, tool := range []string{"github_comment", "gitlab_comment"} {
		if got := ClassifyTool(tool).Class; got != ActionMutate {
			t.Errorf("ClassifyTool(%q).Class = %v, want ActionMutate (external system write)", tool, got)
		}
	}
}

// TestClassifyTool_NonIdempotentCreatesNeedDurability pins the known
// non-idempotent creates/appends as NeedsDurability. Durability is opt-in and
// default OFF (D-0020 follow-up), so a regression that silently drops the
// declaration from one of these would re-open the casualty — an in-run retry
// or crash-resume would duplicate the row/comment. This fails loudly if any of
// them loses the property.
func TestClassifyTool_NonIdempotentCreatesNeedDurability(t *testing.T) {
	needs := []string{
		// Read-class creates with server-generated identity outside the args.
		"register_component", "save_onboarding_fact",
		// Mutate-class non-idempotent external appends.
		"github_comment", "gitlab_comment", "github_request_changes",
	}
	for _, tool := range needs {
		if !ClassifyTool(tool).NeedsDurability {
			t.Errorf("ClassifyTool(%q).NeedsDurability = false, want true — this is a non-idempotent create/append; dropping durability re-opens the per-run duplicate casualty", tool)
		}
	}
}

// TestClassifyTool_IdempotentToolsAreNotDurable pins that naturally
// idempotent operations do NOT carry NeedsDurability — durability on them is a
// wasted fsync/storage tax and risks serving a stale same-key result. Reads,
// graph upserts, and status-guarded publishes must stay OFF.
func TestClassifyTool_IdempotentToolsAreNotDurable(t *testing.T) {
	notDurable := []string{
		"graph_query", "list_components", // reads
		"graph_add_node", "graph_add_edge", "graph_update_node", // arg-keyed upserts
	}
	for _, tool := range notDurable {
		if ClassifyTool(tool).NeedsDurability {
			t.Errorf("ClassifyTool(%q).NeedsDurability = true, want false — this operation is naturally idempotent and must not pay the durability tax", tool)
		}
	}
}

// TestClassifyTool_UnknownToolNotDurable pins the default-OFF posture: an
// unregistered tool must not be wrapped for durability.
func TestClassifyTool_UnknownToolNotDurable(t *testing.T) {
	if ClassifyTool("does_not_exist").NeedsDurability {
		t.Error("unknown tool defaults to NeedsDurability=true, want false (durability is opt-in, default OFF)")
	}
}

func TestCheckAccess_ReadAlwaysAllowed(t *testing.T) {
	// Read tools should be allowed even with the most restrictive policy
	policy := &SafetyPolicy{Version: 1} // zero-value = all disabled

	readTools := []string{"list_components", "git_read", "graph_query", "k8s_get", "aws_ec2"}
	for _, tool := range readTools {
		t.Run(tool, func(t *testing.T) {
			err := CheckAccess(tool, policy)
			if err != nil {
				t.Errorf("CheckAccess(%q) = %v, want nil (read always allowed)", tool, err)
			}
		})
	}
}

// TestCheckAccess_ModelMaintenanceAlwaysAllowed asserts Joe's own graph/model
// maintenance is read-class, so it is always allowed regardless of policy —
// even with the most restrictive (all-disabled) policy. This guards against a
// regression that would gate Joe's model behind a write policy and freeze it
// in safe mode / incident regimes. (This is the graph/model-maintenance read
// tool that must pass the floor, per the break-test requirement.)
func TestCheckAccess_ModelMaintenanceAlwaysAllowed(t *testing.T) {
	policy := &SafetyPolicy{Version: 1} // zero-value = every gated category disabled

	for _, tool := range []string{
		"graph_add_node", "graph_add_edge", "graph_update_node",
		"register_component", "save_onboarding_fact",
	} {
		if err := CheckAccess(tool, policy); err != nil {
			t.Errorf("CheckAccess(%q) = %v, want nil (read-class, always allowed)", tool, err)
		}
	}
}

// TestCheckAccess_ExternalCommentDeniedByDefault confirms the comment tools,
// now mutating external writes, are denied under the default policy (their
// policy keys are not enabled), preserving deny-by-default for external writes.
func TestCheckAccess_ExternalCommentDeniedByDefault(t *testing.T) {
	policy := DefaultPolicy()

	err := CheckAccess("github_comment", policy)
	if err == nil {
		t.Fatal("expected github_comment to be denied by default (mutating external write)")
	}
	var denied *AccessDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("error type = %T, want *AccessDeniedError", err)
	}
	if denied.Class != ActionMutate {
		t.Errorf("denied.Class = %v, want ActionMutate", denied.Class)
	}
}

func TestCheckAccess_MutateDefaultDeny(t *testing.T) {
	policy := DefaultPolicy() // no act toggle is enabled by default

	err := CheckAccess("github_comment", policy)
	if err == nil {
		t.Fatal("expected error for disabled github_comment, got nil")
	}
	var denied *AccessDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("error type = %T, want *AccessDeniedError", err)
	}
	if denied.Class != ActionMutate {
		t.Errorf("denied.Class = %v, want ActionMutate", denied.Class)
	}
}

// The former TestCheckAccess_MutateEnabled is deleted, not migrated
// (knowledge-store-prune). It exercised CheckAccess's policy-allows branch via
// publish_doc_update_git under the git_push toggle — the only registered tool
// whose PolicyKey resolved to a live ActPolicy field. With the doc-publish arm
// gone, every remaining Mutate tool carries a PolicyKey that IsT3Allowed has no
// case for, so the allow branch is unreachable by any real tool name and has no
// replacement fixture. IsT3Allowed's own true branch stays covered directly in
// policy_test.go. Reconstitute this coverage when full mode ships a tool with a
// real opt-in — see docs/backlog/act-policy-vestigial.md.

func TestCheckAccess_UnknownToolDenied(t *testing.T) {
	policy := DefaultPolicy()

	err := CheckAccess("injected_dangerous_tool", policy)
	if err == nil {
		t.Fatal("expected error for unknown tool, got nil")
	}
}

func TestActionClass_String(t *testing.T) {
	tests := []struct {
		class ActionClass
		want  string
	}{
		{ActionRead, "read"},
		{ActionMutate, "mutate"},
		{ActionClass(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.class.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAccessDeniedError_Message(t *testing.T) {
	err := &AccessDeniedError{
		ToolName: "github_comment",
		Class:    ActionMutate,
		Reason:   "mutating action 'github_comment' is disabled in safety policy",
	}

	msg := err.Error()
	if msg != "safety: access denied for tool 'github_comment' (mutate): mutating action 'github_comment' is disabled in safety policy" {
		t.Errorf("unexpected error message: %s", msg)
	}
}
