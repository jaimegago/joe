package runcmd

import (
	"errors"
	"testing"
)

func TestValidateSubcommand_ReadOnlyCommand(t *testing.T) {
	// Commands not in subcommandAllowlists should pass with any args.
	tests := []struct {
		command string
		args    []string
	}{
		{"ls", []string{"-la"}},
		{"grep", []string{"-r", "pattern", "."}},
		{"cat", []string{"/etc/hosts"}},
		{"wc", []string{"-l"}},
		{"find", []string{".", "-name", "*.go"}},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			if err := ValidateSubcommand(tt.command, tt.args); err != nil {
				t.Errorf("ValidateSubcommand(%q, %v) = %v, want nil", tt.command, tt.args, err)
			}
		})
	}
}

func TestValidateSubcommand_KubectlAllowed(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"get pods", []string{"get", "pods"}},
		{"describe pod", []string{"describe", "pod", "my-pod"}},
		{"logs", []string{"logs", "my-pod", "-f"}},
		{"top nodes", []string{"top", "nodes"}},
		{"explain deployment", []string{"explain", "deployment"}},
		{"api-resources", []string{"api-resources"}},
		{"api-versions", []string{"api-versions"}},
		{"version", []string{"version"}},
		{"cluster-info", []string{"cluster-info"}},
		{"get with namespace flag", []string{"-n", "kube-system", "get", "pods"}},
		{"get with --namespace", []string{"--namespace", "default", "get", "svc"}},
		{"get with output flag", []string{"get", "pods", "-o", "json"}},
		{"config current-context", []string{"config", "current-context"}},
		{"auth can-i", []string{"auth", "can-i", "get", "pods"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateSubcommand("kubectl", tt.args); err != nil {
				t.Errorf("ValidateSubcommand(kubectl, %v) = %v, want nil", tt.args, err)
			}
		})
	}
}

func TestValidateSubcommand_KubectlDenied(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantSubcmd string
	}{
		{"delete pod", []string{"delete", "pod", "my-pod"}, "delete"},
		{"apply file", []string{"apply", "-f", "deploy.yaml"}, "apply"},
		{"patch", []string{"patch", "deployment", "my-deploy"}, "patch"},
		{"scale", []string{"scale", "--replicas=3", "deployment/my-deploy"}, "scale"},
		{"drain node", []string{"drain", "node-1"}, "drain"},
		{"cordon", []string{"cordon", "node-1"}, "cordon"},
		{"taint", []string{"taint", "node", "node-1"}, "taint"},
		{"edit", []string{"edit", "deployment", "my-deploy"}, "edit"},
		{"replace", []string{"replace", "-f", "deploy.yaml"}, "replace"},
		{"rollout restart", []string{"rollout", "restart", "deployment/my-deploy"}, "rollout"},
		{"exec", []string{"exec", "-it", "my-pod", "--", "bash"}, "exec"},
		{"run", []string{"run", "test-pod", "--image=nginx"}, "run"},
		{"create", []string{"create", "namespace", "test"}, "create"},
		{"delete with namespace flag", []string{"-n", "prod", "delete", "pod", "my-pod"}, "delete"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSubcommand("kubectl", tt.args)
			if err == nil {
				t.Fatalf("ValidateSubcommand(kubectl, %v) = nil, want error", tt.args)
			}

			var denied *SubcommandDeniedError
			if !errors.As(err, &denied) {
				t.Fatalf("expected SubcommandDeniedError, got %T: %v", err, err)
			}
			if denied.Subcommand != tt.wantSubcmd {
				t.Errorf("denied subcommand = %q, want %q", denied.Subcommand, tt.wantSubcmd)
			}
		})
	}
}

func TestValidateSubcommand_KubectlNoSubcommand(t *testing.T) {
	err := ValidateSubcommand("kubectl", []string{})
	if err == nil {
		t.Fatal("expected error for kubectl with no subcommand")
	}
}

func TestValidateSubcommand_KubectlOnlyFlags(t *testing.T) {
	err := ValidateSubcommand("kubectl", []string{"-n", "default"})
	if err == nil {
		t.Fatal("expected error for kubectl with only flags, no subcommand")
	}
}

func TestValidateSubcommand_HelmAllowed(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"list", []string{"list"}},
		{"list all ns", []string{"list", "--all-namespaces"}},
		{"status", []string{"status", "my-release"}},
		{"get values", []string{"get", "values", "my-release"}},
		{"history", []string{"history", "my-release"}},
		{"show chart", []string{"show", "chart", "my-chart"}},
		{"search repo", []string{"search", "repo", "nginx"}},
		{"version", []string{"version"}},
		{"env", []string{"env"}},
		{"template", []string{"template", "my-release", "my-chart"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateSubcommand("helm", tt.args); err != nil {
				t.Errorf("ValidateSubcommand(helm, %v) = %v, want nil", tt.args, err)
			}
		})
	}
}

func TestValidateSubcommand_HelmDenied(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantSubcmd string
	}{
		{"install", []string{"install", "my-release", "my-chart"}, "install"},
		{"upgrade", []string{"upgrade", "my-release", "my-chart"}, "upgrade"},
		{"uninstall", []string{"uninstall", "my-release"}, "uninstall"},
		{"delete", []string{"delete", "my-release"}, "delete"},
		{"rollback", []string{"rollback", "my-release", "1"}, "rollback"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSubcommand("helm", tt.args)
			if err == nil {
				t.Fatalf("ValidateSubcommand(helm, %v) = nil, want error", tt.args)
			}

			var denied *SubcommandDeniedError
			if !errors.As(err, &denied) {
				t.Fatalf("expected SubcommandDeniedError, got %T: %v", err, err)
			}
			if denied.Subcommand != tt.wantSubcmd {
				t.Errorf("denied subcommand = %q, want %q", denied.Subcommand, tt.wantSubcmd)
			}
		})
	}
}

func TestValidateSubcommand_ArgocdAllowed(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"app list", []string{"app", "list"}},
		{"app get", []string{"app", "get", "my-app"}},
		{"app diff", []string{"app", "diff", "my-app"}},
		{"app logs", []string{"app", "logs", "my-app"}},
		{"app history", []string{"app", "history", "my-app"}},
		{"app manifests", []string{"app", "manifests", "my-app"}},
		{"app resources", []string{"app", "resources", "my-app"}},
		{"cluster list", []string{"cluster", "list"}},
		{"repo list", []string{"repo", "list"}},
		{"version", []string{"version"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateSubcommand("argocd", tt.args); err != nil {
				t.Errorf("ValidateSubcommand(argocd, %v) = %v, want nil", tt.args, err)
			}
		})
	}
}

func TestValidateSubcommand_ArgocdDenied(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantSubcmd string
	}{
		{"app sync", []string{"app", "sync", "my-app"}, "sync"},
		{"app delete", []string{"app", "delete", "my-app"}, "delete"},
		{"app rollback", []string{"app", "rollback", "my-app"}, "rollback"},
		{"app set", []string{"app", "set", "my-app"}, "set"},
		{"app create", []string{"app", "create", "my-app"}, "create"},
		{"app patch", []string{"app", "patch", "my-app"}, "patch"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSubcommand("argocd", tt.args)
			if err == nil {
				t.Fatalf("ValidateSubcommand(argocd, %v) = nil, want error", tt.args)
			}

			var denied *SubcommandDeniedError
			if !errors.As(err, &denied) {
				t.Fatalf("expected SubcommandDeniedError, got %T: %v", err, err)
			}
		})
	}
}

func TestValidateSubcommand_ArgocdAppWithFlags(t *testing.T) {
	// argocd --server foo app get my-app
	err := ValidateSubcommand("argocd", []string{"--server", "foo.example.com", "app", "get", "my-app"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateSubcommand_ArgocdAppSyncWithFlags(t *testing.T) {
	// argocd --server foo app sync my-app (should be denied)
	err := ValidateSubcommand("argocd", []string{"--server", "foo.example.com", "app", "sync", "my-app"})
	if err == nil {
		t.Fatal("expected error for argocd app sync with flags")
	}
}

func TestValidateSubcommand_ArgocdAppNoAction(t *testing.T) {
	err := ValidateSubcommand("argocd", []string{"app"})
	if err == nil {
		t.Fatal("expected error for 'argocd app' with no action")
	}
}

func TestIsMutationCapable(t *testing.T) {
	tests := []struct {
		command string
		want    bool
	}{
		{"kubectl", true},
		{"helm", true},
		{"argocd", true},
		{"ls", false},
		{"grep", false},
		{"cat", false},
		{"unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			if got := IsMutationCapable(tt.command); got != tt.want {
				t.Errorf("IsMutationCapable(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}

func TestSubcommandDeniedError(t *testing.T) {
	err := &SubcommandDeniedError{
		Command:    "kubectl",
		Subcommand: "delete",
		Allowed:    []string{"get", "describe"},
	}

	msg := err.Error()
	if msg == "" {
		t.Fatal("error message should not be empty")
	}
}

// TestIsFlagWithValue exercises the isFlagWithValue function directly via
// ValidateSubcommand paths that rely on flag skipping.
func TestIsFlagWithValue_AllFlagsThatTakeValues(t *testing.T) {
	// Each of these flags should cause the next token to be consumed as a value.
	// We test via ValidateSubcommand("kubectl", ...) since isFlagWithValue is unexported.
	// Pattern: <flag> <value> <subcommand> — if isFlagWithValue works, subcommand is found.
	valueFlagCases := []struct {
		name string
		args []string
	}{
		{"short -n", []string{"-n", "default", "get", "pods"}},
		{"short -l", []string{"-l", "app=foo", "get", "pods"}},
		{"short -o", []string{"-o", "json", "get", "pods"}},
		{"short -f", []string{"-f", "file.yaml", "get", "pods"}},
		{"short -c", []string{"-c", "myctx", "get", "pods"}},
		{"short -s", []string{"-s", "https://server", "get", "pods"}},
		{"--namespace", []string{"--namespace", "kube-system", "get", "pods"}},
		{"--context", []string{"--context", "prod", "get", "pods"}},
		{"--kubeconfig", []string{"--kubeconfig", "/tmp/kube", "get", "pods"}},
		{"--output", []string{"--output", "yaml", "get", "pods"}},
		{"--selector", []string{"--selector", "app=foo", "get", "pods"}},
		{"--field-selector", []string{"--field-selector", "status=Running", "get", "pods"}},
		{"--server", []string{"--server", "https://k8s", "get", "pods"}},
	}

	for _, tt := range valueFlagCases {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateSubcommand("kubectl", tt.args); err != nil {
				t.Errorf("ValidateSubcommand(kubectl, %v) = %v, want nil", tt.args, err)
			}
		})
	}
}

// TestValidateSubcommand_ArgocdAppWithFlagsBeforeApp covers the argocd path where
// a value-taking flag (--grpc-web-root-path) appears before "app".
func TestValidateSubcommand_ArgocdGRPCFlag(t *testing.T) {
	// --grpc-web-root-path takes a value, so the next token is consumed.
	err := ValidateSubcommand("argocd", []string{"--grpc-web-root-path", "/argocd", "app", "list"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestValidateSubcommand_ArgocdAppFlagsBeforeAction covers validateArgocdApp
// when flags appear between "app" and the action token.
func TestValidateSubcommand_ArgocdAppFlagsBeforeAllowedAction(t *testing.T) {
	// --server takes a value; after it the action is "get" (allowed).
	err := ValidateSubcommand("argocd", []string{"app", "--server", "https://argocd", "get", "my-app"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestValidateSubcommand_ArgocdAppFlagsOnlyAfterApp exercises the path in
// validateArgocdApp where only flags follow "app" with no action token.
func TestValidateSubcommand_ArgocdAppOnlyFlagsAfterApp(t *testing.T) {
	err := ValidateSubcommand("argocd", []string{"app", "--server", "https://argocd"})
	if err == nil {
		t.Fatal("expected error when no action follows 'argocd app'")
	}
}
