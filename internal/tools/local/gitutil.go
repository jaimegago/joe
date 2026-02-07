package local

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// RunGit runs a git command in the specified directory
func RunGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Check if it's "not a git repo" error
		stderrStr := stderr.String()
		if strings.Contains(stderrStr, "not a git repository") {
			return "", fmt.Errorf("not a git repository (or any of the parent directories)")
		}
		if strings.Contains(stderrStr, "command not found") || strings.Contains(err.Error(), "executable file not found") {
			return "", fmt.Errorf("git command not found")
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), stderrStr)
	}

	return stdout.String(), nil
}
