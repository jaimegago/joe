package runcmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/jaimegago/joe/internal/llm"
)

type Tool struct {
	allowedCommands map[string]bool
	runner          Runner
}

// Runner executes a command and returns stdout, stderr, exit code, and error.
type Runner interface {
	Run(ctx context.Context, name string, args []string) (string, string, int, error)
}

type execRunner struct{}

func (r *execRunner) Run(ctx context.Context, name string, args []string) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	return stdout.String(), stderr.String(), exitCode, err
}

func New(allowed []string) *Tool {
	allowedMap := make(map[string]bool)
	for _, cmd := range allowed {
		allowedMap[cmd] = true
	}
	return &Tool{
		allowedCommands: allowedMap,
		runner:          &execRunner{},
	}
}

// NewWithRunner creates a run_command tool with a custom runner (useful for tests).
func NewWithRunner(allowed []string, runner Runner) *Tool {
	tool := New(allowed)
	if runner != nil {
		tool.runner = runner
	}
	return tool
}

func (t *Tool) Name() string {
	return "run_command"
}

func (t *Tool) Description() string {
	allowedList := make([]string, 0, len(t.allowedCommands))
	for cmd := range t.allowedCommands {
		allowedList = append(allowedList, cmd)
	}
	return fmt.Sprintf("Run a safe shell command (limited to: %s). Use this to inspect system state, list files, or run read-only commands.", strings.Join(allowedList, ", "))
}

func (t *Tool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"command": {
				Type:        "string",
				Description: "Command to run (must be in allowed list)",
			},
			"args": {
				Type:        "array",
				Description: "Command arguments as an array of strings (optional)",
				Items: &llm.Property{
					Type:        "string",
					Description: "A command argument",
				},
			},
		},
		Required: []string{"command"},
	}
}

func (t *Tool) Execute(ctx context.Context, args map[string]any) (any, error) {
	// Get command
	cmdName, ok := args["command"].(string)
	if !ok || cmdName == "" {
		return nil, fmt.Errorf("command parameter is required and must be a string")
	}

	// Check if command is allowed
	if !t.allowedCommands[cmdName] {
		allowedList := make([]string, 0, len(t.allowedCommands))
		for cmd := range t.allowedCommands {
			allowedList = append(allowedList, cmd)
		}
		return nil, fmt.Errorf("command '%s' is not allowed. Allowed: %s", cmdName, strings.Join(allowedList, ", "))
	}

	// Get arguments
	var cmdArgs []string
	if argsRaw, ok := args["args"]; ok && argsRaw != nil {
		if argsList, ok := argsRaw.([]any); ok {
			for _, arg := range argsList {
				if argStr, ok := arg.(string); ok {
					cmdArgs = append(cmdArgs, argStr)
				}
			}
		}
	}

	// Create context with timeout
	execCtx, cancel := context.WithTimeout(ctx, CommandTimeout)
	defer cancel()

	// Execute command (NOT through shell, direct execution)
	stdoutStr, stderrStr, exitCode, err := t.runner.Run(execCtx, cmdName, cmdArgs)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || execCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("command timed out after %v", CommandTimeout)
		}
		if exitCode == 0 {
			return nil, fmt.Errorf("failed to execute command: %w", err)
		}
	}

	// Truncate output if too large
	truncated := false

	truncateMsg := fmt.Sprintf("\n... (truncated at %dKB)", MaxOutputSize/1024)
	if len(stdoutStr) > MaxOutputSize {
		stdoutStr = stdoutStr[:MaxOutputSize] + truncateMsg
		truncated = true
	}
	if len(stderrStr) > MaxOutputSize {
		stderrStr = stderrStr[:MaxOutputSize] + truncateMsg
		truncated = true
	}

	result := map[string]any{
		"command":   cmdName,
		"args":      cmdArgs,
		"stdout":    stdoutStr,
		"stderr":    stderrStr,
		"exit_code": exitCode,
	}

	if truncated {
		result["truncated"] = true
	}

	return result, nil
}
