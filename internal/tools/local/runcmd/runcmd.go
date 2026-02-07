package runcmd

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/jaimegago/joe/internal/llm"
)

const (
	commandTimeout = 30 * time.Second
	maxOutputSize  = 100 * 1024 // 100KB
)

type Tool struct {
	allowedCommands map[string]bool
}

func New(allowed []string) *Tool {
	allowedMap := make(map[string]bool)
	for _, cmd := range allowed {
		allowedMap[cmd] = true
	}
	return &Tool{
		allowedCommands: allowedMap,
	}
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
	execCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	// Execute command (NOT through shell, direct execution)
	cmd := exec.CommandContext(execCtx, cmdName, cmdArgs...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if execCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("command timed out after 30s")
		} else {
			return nil, fmt.Errorf("failed to execute command: %w", err)
		}
	}

	// Truncate output if too large
	stdoutStr := stdout.String()
	stderrStr := stderr.String()
	truncated := false

	if len(stdoutStr) > maxOutputSize {
		stdoutStr = stdoutStr[:maxOutputSize] + "\n... (truncated at 100KB)"
		truncated = true
	}
	if len(stderrStr) > maxOutputSize {
		stderrStr = stderrStr[:maxOutputSize] + "\n... (truncated at 100KB)"
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
