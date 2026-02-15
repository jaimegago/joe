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
		{"echo", TierObserve},
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

		// T2: Record
		{"graph_add_node", TierRecord},
		{"graph_add_edge", TierRecord},
		{"graph_update_node", TierRecord},
		{"register_source", TierRecord},
		{"save_onboarding_fact", TierRecord},

		// T3: Act
		{"write_file", TierAct},
		{"run_command", TierAct},
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

func TestCheckAccess_T1AlwaysAllowed(t *testing.T) {
	// T1 tools should be allowed even with the most restrictive policy
	policy := &SafetyPolicy{Version: 1} // zero-value = all disabled

	t1Tools := []string{"read_file", "echo", "graph_query", "k8s_get", "aws_ec2"}
	for _, tool := range t1Tools {
		t.Run(tool, func(t *testing.T) {
			err := CheckAccess(tool, policy)
			if err != nil {
				t.Errorf("CheckAccess(%q) = %v, want nil (T1 always allowed)", tool, err)
			}
		})
	}
}

func TestCheckAccess_T2WithPolicy(t *testing.T) {
	policy := DefaultPolicy()
	policy.Record.GraphMutations = false // disable graph mutations

	// graph_add_node should be denied
	err := CheckAccess("graph_add_node", policy)
	if err == nil {
		t.Fatal("expected error for disabled graph_mutations, got nil")
	}
	var denied *AccessDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("error type = %T, want *AccessDeniedError", err)
	}
	if denied.Tier != TierRecord {
		t.Errorf("denied.Tier = %v, want TierRecord", denied.Tier)
	}

	// save_onboarding_fact should still be allowed
	err = CheckAccess("save_onboarding_fact", policy)
	if err != nil {
		t.Errorf("CheckAccess(save_onboarding_fact) = %v, want nil", err)
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
