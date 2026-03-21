package runcmd

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type mockRunner struct {
	stdout   string
	stderr   string
	exitCode int
	err      error
	seenName string
	seenArgs []string
}

func (r *mockRunner) Run(ctx context.Context, name string, args []string) (string, string, int, error) {
	r.seenName = name
	r.seenArgs = append([]string{}, args...)
	return r.stdout, r.stderr, r.exitCode, r.err
}

func TestTool_Metadata(t *testing.T) {
	tool := New([]string{"ls", "cat"})
	if got := tool.Name(); got != "run_command" {
		t.Errorf("Name() = %q, want %q", got, "run_command")
	}
	if tool.Description() == "" {
		t.Fatal("Description() should not be empty")
	}
	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("Parameters().Type = %q, want %q", params.Type, "object")
	}
	if _, ok := params.Properties["command"]; !ok {
		t.Error("Parameters() missing 'command' property")
	}
}

func TestExecute(t *testing.T) {
	tests := []struct {
		name        string
		allowedCmds []string
		runner      *mockRunner
		args        map[string]any
		wantErr     bool
		errContains string
		validate    func(t *testing.T, result any, runner *mockRunner)
	}{
		// Allowlist enforcement
		{
			name:        "command not in allowed list",
			allowedCmds: []string{"echo"},
			runner:      &mockRunner{},
			args:        map[string]any{"command": "rm"},
			wantErr:     true,
		},
		// Self-protection
		{
			name:        "blocks joe command",
			allowedCmds: []string{"joe"},
			runner:      &mockRunner{stdout: "ok"},
			args:        map[string]any{"command": "joe"},
			wantErr:     true,
			errContains: "self-protection",
		},
		{
			name:        "blocks joe-core command",
			allowedCmds: []string{"joe-core"},
			runner:      &mockRunner{stdout: "ok"},
			args:        map[string]any{"command": "joe-core"},
			wantErr:     true,
			errContains: "self-protection",
		},
		{
			name:        "blocks kill command",
			allowedCmds: []string{"kill"},
			runner:      &mockRunner{stdout: "ok"},
			args:        map[string]any{"command": "kill"},
			wantErr:     true,
			errContains: "self-protection",
		},
		{
			name:        "blocks pkill command",
			allowedCmds: []string{"pkill"},
			runner:      &mockRunner{stdout: "ok"},
			args:        map[string]any{"command": "pkill"},
			wantErr:     true,
			errContains: "self-protection",
		},
		{
			name:        "blocks killall command",
			allowedCmds: []string{"killall"},
			runner:      &mockRunner{stdout: "ok"},
			args:        map[string]any{"command": "killall"},
			wantErr:     true,
			errContains: "self-protection",
		},
		// kubectl subcommand validation
		{
			name:        "kubectl get allowed",
			allowedCmds: []string{"kubectl"},
			runner:      &mockRunner{stdout: "NAME  READY  STATUS\nmy-pod  1/1  Running"},
			args:        map[string]any{"command": "kubectl", "args": []any{"get", "pods"}},
			validate: func(t *testing.T, result any, runner *mockRunner) {
				if result.(map[string]any)["stdout"] != runner.stdout {
					t.Errorf("stdout = %v, want %q", result.(map[string]any)["stdout"], runner.stdout)
				}
			},
		},
		{
			name:        "kubectl delete blocked",
			allowedCmds: []string{"kubectl"},
			runner:      &mockRunner{stdout: "ok"},
			args:        map[string]any{"command": "kubectl", "args": []any{"delete", "pod", "my-pod"}},
			wantErr:     true,
			errContains: "not allowed",
		},
		{
			name:        "kubectl apply blocked",
			allowedCmds: []string{"kubectl"},
			runner:      &mockRunner{stdout: "ok"},
			args:        map[string]any{"command": "kubectl", "args": []any{"apply", "-f", "deploy.yaml"}},
			wantErr:     true,
			errContains: "not allowed",
		},
		{
			name:        "kubectl with no subcommand blocked",
			allowedCmds: []string{"kubectl"},
			runner:      &mockRunner{stdout: "ok"},
			args:        map[string]any{"command": "kubectl", "args": []any{}},
			wantErr:     true,
		},
		{
			name:        "helm install blocked",
			allowedCmds: []string{"helm"},
			runner:      &mockRunner{stdout: "ok"},
			args:        map[string]any{"command": "helm", "args": []any{"install", "my-release", "my-chart"}},
			wantErr:     true,
		},
		{
			name:        "argocd app sync blocked",
			allowedCmds: []string{"argocd"},
			runner:      &mockRunner{stdout: "ok"},
			args:        map[string]any{"command": "argocd", "args": []any{"app", "sync", "my-app"}},
			wantErr:     true,
		},
		// Runner behavior
		{
			name:        "runner success",
			allowedCmds: []string{"echo"},
			runner:      &mockRunner{stdout: "ok"},
			args:        map[string]any{"command": "echo", "args": []any{"hello"}},
			validate: func(t *testing.T, result any, runner *mockRunner) {
				res := result.(map[string]any)
				if res["stdout"] != "ok" {
					t.Errorf("stdout = %v, want ok", res["stdout"])
				}
				if res["exit_code"] != 0 {
					t.Errorf("exit_code = %v, want 0", res["exit_code"])
				}
				if runner.seenName != "echo" {
					t.Errorf("seenName = %q, want echo", runner.seenName)
				}
				if len(runner.seenArgs) != 1 || runner.seenArgs[0] != "hello" {
					t.Errorf("seenArgs = %v, want [hello]", runner.seenArgs)
				}
			},
		},
		{
			name:        "non-zero exit code returned without error",
			allowedCmds: []string{"cmd"},
			runner:      &mockRunner{stdout: "oops", exitCode: 2, err: errors.New("exit 2")},
			args:        map[string]any{"command": "cmd"},
			validate: func(t *testing.T, result any, _ *mockRunner) {
				if result.(map[string]any)["exit_code"] != 2 {
					t.Errorf("exit_code = %v, want 2", result.(map[string]any)["exit_code"])
				}
			},
		},
		{
			name:        "deadline exceeded returns error",
			allowedCmds: []string{"cmd"},
			runner:      &mockRunner{err: context.DeadlineExceeded},
			args:        map[string]any{"command": "cmd"},
			wantErr:     true,
		},
		{
			name:        "long output is truncated",
			allowedCmds: []string{"cmd"},
			runner:      &mockRunner{stdout: strings.Repeat("a", MaxOutputSize+10), stderr: strings.Repeat("a", MaxOutputSize+10)},
			args:        map[string]any{"command": "cmd"},
			validate: func(t *testing.T, result any, _ *mockRunner) {
				res := result.(map[string]any)
				if res["truncated"] != true {
					t.Error("want truncated=true")
				}
				if !strings.Contains(res["stdout"].(string), "truncated") {
					t.Error("stdout missing truncation message")
				}
			},
		},
		{
			name:        "read-only command skips subcommand check",
			allowedCmds: []string{"ls"},
			runner:      &mockRunner{stdout: "file1.go\nfile2.go"},
			args:        map[string]any{"command": "ls", "args": []any{"-la", "/tmp"}},
			validate: func(t *testing.T, result any, runner *mockRunner) {
				if result.(map[string]any)["stdout"] != runner.stdout {
					t.Errorf("stdout = %v, want %q", result.(map[string]any)["stdout"], runner.stdout)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := NewWithRunner(tt.allowedCmds, tt.runner)
			got, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.errContains != "" && (err == nil || !strings.Contains(err.Error(), tt.errContains)) {
				t.Errorf("Execute() error = %v, want error containing %q", err, tt.errContains)
			}
			if tt.validate != nil && err == nil {
				tt.validate(t, got, tt.runner)
			}
		})
	}
}

// TestExecute_MissingCommand verifies the error path when command is not a string.
func TestExecute_MissingCommand(t *testing.T) {
	tool := NewWithRunner([]string{"echo"}, &mockRunner{})
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing command")
	}
	if !strings.Contains(err.Error(), "command parameter") {
		t.Errorf("error = %v, want 'command parameter' message", err)
	}
}

// TestExecute_RunnerErrorWithExitZero covers the branch where err != nil but exitCode == 0.
func TestExecute_RunnerErrorWithExitZero(t *testing.T) {
	runner := &mockRunner{stdout: "", stderr: "bad", exitCode: 0, err: errors.New("exec failed")}
	tool := NewWithRunner([]string{"cmd"}, runner)
	_, err := tool.Execute(context.Background(), map[string]any{"command": "cmd"})
	if err == nil {
		t.Fatal("expected error when runner returns err with exit code 0")
	}
	if !strings.Contains(err.Error(), "failed to execute command") {
		t.Errorf("error = %v, want 'failed to execute command'", err)
	}
}

// TestExecRunner_Run exercises the real execRunner with an actual command.
func TestExecRunner_Run(t *testing.T) {
	r := &execRunner{}
	stdout, stderr, code, err := r.Run(context.Background(), "echo", []string{"hello"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "hello") {
		t.Errorf("stdout = %q, want 'hello'", stdout)
	}
	_ = stderr
}

// TestExecRunner_Run_NonZeroExit covers the exitErr branch in execRunner.Run.
func TestExecRunner_Run_NonZeroExit(t *testing.T) {
	r := &execRunner{}
	// "false" always exits with code 1.
	_, _, code, err := r.Run(context.Background(), "false", nil)
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
	if code == 0 {
		t.Error("exit code should be non-zero")
	}
}
