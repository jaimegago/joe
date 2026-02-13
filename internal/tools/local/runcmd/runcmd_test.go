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

func TestExecute_CommandNotAllowed(t *testing.T) {
	tool := NewWithRunner([]string{"echo"}, &mockRunner{})

	_, err := tool.Execute(context.Background(), map[string]any{
		"command": "rm",
	})
	if err == nil {
		t.Fatal("expected error for disallowed command")
	}
}

func TestExecute_RunnerSuccess(t *testing.T) {
	runner := &mockRunner{stdout: "ok"}
	tool := NewWithRunner([]string{"echo"}, runner)

	resultRaw, err := tool.Execute(context.Background(), map[string]any{
		"command": "echo",
		"args":    []any{"hello"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, ok := resultRaw.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", resultRaw)
	}

	if result["stdout"] != "ok" {
		t.Errorf("stdout = %v, want %v", result["stdout"], "ok")
	}
	if result["exit_code"] != 0 {
		t.Errorf("exit_code = %v, want %d", result["exit_code"], 0)
	}
	if runner.seenName != "echo" {
		t.Errorf("runner name = %q, want %q", runner.seenName, "echo")
	}
	if len(runner.seenArgs) != 1 || runner.seenArgs[0] != "hello" {
		t.Errorf("runner args = %v, want %v", runner.seenArgs, []string{"hello"})
	}
}

func TestExecute_RunnerNonZeroExit(t *testing.T) {
	runner := &mockRunner{stdout: "oops", exitCode: 2, err: errors.New("exit 2")}
	tool := NewWithRunner([]string{"cmd"}, runner)

	resultRaw, err := tool.Execute(context.Background(), map[string]any{
		"command": "cmd",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := resultRaw.(map[string]any)
	if result["exit_code"] != 2 {
		t.Errorf("exit_code = %v, want %d", result["exit_code"], 2)
	}
}

func TestExecute_RunnerTimeout(t *testing.T) {
	runner := &mockRunner{err: context.DeadlineExceeded}
	tool := NewWithRunner([]string{"cmd"}, runner)

	_, err := tool.Execute(context.Background(), map[string]any{
		"command": "cmd",
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestExecute_TruncatesOutput(t *testing.T) {
	long := strings.Repeat("a", MaxOutputSize+10)
	runner := &mockRunner{stdout: long, stderr: long}
	tool := NewWithRunner([]string{"cmd"}, runner)

	resultRaw, err := tool.Execute(context.Background(), map[string]any{
		"command": "cmd",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := resultRaw.(map[string]any)
	if result["truncated"] != true {
		t.Fatalf("expected truncated=true")
	}
	stdout := result["stdout"].(string)
	if !strings.Contains(stdout, "truncated") {
		t.Errorf("stdout missing truncation message")
	}
}
