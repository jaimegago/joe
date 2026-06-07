package safety

import (
	"errors"
	"testing"
)

func TestClassifyTool_KnownTools(t *testing.T) {
	tests := []struct {
		tool     string
		wantTier ActionTier
	}{
		// T1: Observe
		{"read_file", TierObserve},
		{"local_git_status", TierObserve},
		{"local_git_diff", TierObserve},
		{"ask_user", TierObserve},
		{"list_sources", TierObserve},
		{"graph_query", TierObserve},
		{"graph_related", TierObserve},
		{"k8s_get", TierObserve},
		{"k8s_logs", TierObserve},
		{"git_read", TierObserve},
		{"git_log", TierObserve},
		{"git_diff", TierObserve},
		{"aws_ec2", TierObserve},
		{"aws_eks", TierObserve},
		{"aws_rds", TierObserve},
		{"aws_vpc", TierObserve},

		// Phase 6.3: Observability
		{"prometheus_query", TierObserve},
		{"loki_query", TierObserve},
		{"tempo_search", TierObserve},
		{"jaeger_traces", TierObserve},

		// Phase 6.4: Alerting & dashboards
		{"alertmanager_alerts", TierObserve},
		{"pagerduty_incidents", TierObserve},
		{"grafana_dashboards", TierObserve},

		// Joe's own model maintenance — observe-tier per D-0018/D-0019.
		// These record observed state into Joe's own graph/store; the
		// managed system is unchanged, so they are reads, not writes.
		{"graph_add_node", TierObserve},
		{"graph_add_edge", TierObserve},
		{"graph_update_node", TierObserve},
		{"register_source", TierObserve},
		{"save_onboarding_fact", TierObserve},
		{"save_knowledge_entry", TierObserve},
		{"generate_doc_draft", TierObserve},
		{"registry_query", TierObserve},
		{"artifactory_query", TierObserve},
		{"ecr_query", TierObserve},

		// T3: Act — managed-system mutations
		{"write_file", TierAct},
		{"run_command", TierAct},
		{"github_comment", TierAct},
		{"gitlab_comment", TierAct},
		{"github_request_changes", TierAct},
	}

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			c := ClassifyTool(tt.tool)
			if c.Tier != tt.wantTier {
				t.Errorf("ClassifyTool(%q).Tier = %v, want %v", tt.tool, c.Tier, tt.wantTier)
			}
		})
	}
}

func TestClassifyTool_UnknownTool(t *testing.T) {
	c := ClassifyTool("evil_tool_from_prompt_injection")
	if c.Tier != TierAct {
		t.Errorf("unknown tool tier = %v, want TierAct (deny by default)", c.Tier)
	}
}

// TestClassifyTool_UnknownDefaultIsMostConservative guards the deny-by-default
// floor: an unregistered tool must classify at the highest (most restrictive)
// tier so a prompt-injected or newly-added tool can never run unclassified.
// If a tier higher than TierAct is ever added, this fails until the default
// is bumped to match.
func TestClassifyTool_UnknownDefaultIsMostConservative(t *testing.T) {
	def := ClassifyTool("definitely_not_a_registered_tool").Tier

	for _, known := range []ActionTier{TierObserve, TierRecord, TierAct} {
		if def < known {
			t.Errorf("unknown-tool default tier %v is less conservative than %v; default must be the most conservative tier", def, known)
		}
	}
	if def != TierAct {
		t.Errorf("unknown-tool default = %v, want TierAct", def)
	}
}

// TestClassifyTool_GraphMutationFamilyIsObserve is the break-test for the
// D-0018/D-0019 reclassification: Joe's graph-mutation family maintains Joe's
// OWN model and must stay observe-tier. Re-promoting them to a write tier
// would freeze Joe's model whenever safe mode or an incident captain gate is
// engaged. If someone bumps these back to Record/Act, this fails loudly.
func TestClassifyTool_GraphMutationFamilyIsObserve(t *testing.T) {
	for _, tool := range []string{"graph_add_node", "graph_add_edge", "graph_update_node"} {
		if got := ClassifyTool(tool).Tier; got != TierObserve {
			t.Errorf("ClassifyTool(%q).Tier = %v, want TierObserve (Joe's own model maintenance)", tool, got)
		}
	}
}

// TestClassifyTool_ExternalCommentsAreAct locks the comment tools at the write
// floor. Posting to a PR/MR mutates an external system, so it must not silently
// regress to a sub-Act tier (which would skip the act-policy gate and the
// blocking pre-execution notification).
func TestClassifyTool_ExternalCommentsAreAct(t *testing.T) {
	for _, tool := range []string{"github_comment", "gitlab_comment"} {
		if got := ClassifyTool(tool).Tier; got != TierAct {
			t.Errorf("ClassifyTool(%q).Tier = %v, want TierAct (external system write)", tool, got)
		}
	}
}

func TestCheckAccess_T1AlwaysAllowed(t *testing.T) {
	// T1 tools should be allowed even with the most restrictive policy
	policy := &SafetyPolicy{Version: 1} // zero-value = all disabled

	t1Tools := []string{"read_file", "ask_user", "graph_query", "k8s_get", "aws_ec2"}
	for _, tool := range t1Tools {
		t.Run(tool, func(t *testing.T) {
			err := CheckAccess(tool, policy)
			if err != nil {
				t.Errorf("CheckAccess(%q) = %v, want nil (T1 always allowed)", tool, err)
			}
		})
	}
}

// TestCheckAccess_ModelMaintenanceAlwaysAllowed replaces the former
// TestCheckAccess_T2WithPolicy. Under D-0018/D-0019, Joe's own graph/model
// maintenance is observe-tier, so it is always allowed regardless of policy —
// even with the most restrictive (all-disabled) policy. This guards against a
// regression that would gate Joe's model behind a write policy and freeze it
// in safe mode / incident regimes.
func TestCheckAccess_ModelMaintenanceAlwaysAllowed(t *testing.T) {
	policy := &SafetyPolicy{Version: 1} // zero-value = every gated category disabled

	for _, tool := range []string{
		"graph_add_node", "graph_add_edge", "graph_update_node",
		"register_source", "save_onboarding_fact", "save_knowledge_entry",
		"generate_doc_draft",
	} {
		if err := CheckAccess(tool, policy); err != nil {
			t.Errorf("CheckAccess(%q) = %v, want nil (observe-tier, always allowed)", tool, err)
		}
	}
}

// TestCheckAccess_ExternalCommentDeniedByDefault confirms the comment tools,
// now act-tier external writes, are denied under the default policy (their
// policy keys are not enabled), preserving deny-by-default for external writes.
func TestCheckAccess_ExternalCommentDeniedByDefault(t *testing.T) {
	policy := DefaultPolicy()

	err := CheckAccess("github_comment", policy)
	if err == nil {
		t.Fatal("expected github_comment to be denied by default (act-tier external write)")
	}
	var denied *AccessDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("error type = %T, want *AccessDeniedError", err)
	}
	if denied.Tier != TierAct {
		t.Errorf("denied.Tier = %v, want TierAct", denied.Tier)
	}
}

func TestCheckAccess_T3DefaultDeny(t *testing.T) {
	policy := DefaultPolicy() // write_file is disabled by default

	err := CheckAccess("write_file", policy)
	if err == nil {
		t.Fatal("expected error for disabled write_file, got nil")
	}
	var denied *AccessDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("error type = %T, want *AccessDeniedError", err)
	}
	if denied.Tier != TierAct {
		t.Errorf("denied.Tier = %v, want TierAct", denied.Tier)
	}
}

func TestCheckAccess_T3Enabled(t *testing.T) {
	policy := DefaultPolicy()
	policy.Act.WriteFile.Enabled = true

	err := CheckAccess("write_file", policy)
	if err != nil {
		t.Errorf("CheckAccess(write_file) = %v, want nil (enabled)", err)
	}
}

func TestCheckAccess_UnknownToolDenied(t *testing.T) {
	policy := DefaultPolicy()

	err := CheckAccess("injected_dangerous_tool", policy)
	if err == nil {
		t.Fatal("expected error for unknown tool, got nil")
	}
}

func TestActionTier_String(t *testing.T) {
	tests := []struct {
		tier ActionTier
		want string
	}{
		{TierObserve, "T1:Observe"},
		{TierRecord, "T2:Record"},
		{TierAct, "T3:Act"},
		{ActionTier(99), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.tier.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAccessDeniedError_Message(t *testing.T) {
	err := &AccessDeniedError{
		ToolName: "write_file",
		Tier:     TierAct,
		Reason:   "T3 action 'write_file' is disabled in safety policy",
	}

	msg := err.Error()
	if msg != "safety: access denied for tool 'write_file' (T3:Act): T3 action 'write_file' is disabled in safety policy" {
		t.Errorf("unexpected error message: %s", msg)
	}
}
