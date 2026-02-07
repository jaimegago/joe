package gitdiff

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/tools/local"
)

const maxDiffSize = 100 * 1024 // 100KB

type Tool struct{}

func New() *Tool {
	return &Tool{}
}

func (t *Tool) Name() string {
	return "local_git_diff"
}

func (t *Tool) Description() string {
	return "Get git diff of uncommitted changes. Shows the actual code changes line-by-line. Can show unstaged or staged changes, and can filter to a specific file."
}

func (t *Tool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"path": {
				Type:        "string",
				Description: "Specific file path to diff (optional, defaults to all changes)",
			},
			"staged": {
				Type:        "boolean",
				Description: "If true, show staged changes (git diff --staged), otherwise show unstaged changes",
			},
		},
		Required: []string{},
	}
}

func (t *Tool) Execute(ctx context.Context, args map[string]any) (any, error) {
	// Get directory
	dir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current directory: %w", err)
	}

	// Build git diff arguments
	gitArgs := []string{"diff"}

	// Add --staged if requested
	if staged, ok := args["staged"].(bool); ok && staged {
		gitArgs = append(gitArgs, "--staged")
	}

	// Add specific path if provided
	if pathArg, ok := args["path"].(string); ok && pathArg != "" {
		// Expand path
		absPath, err := local.ExpandPath(pathArg)
		if err != nil {
			return nil, fmt.Errorf("failed to expand path: %w", err)
		}
		gitArgs = append(gitArgs, "--", absPath)
	}

	// Run git diff
	diffOutput, err := local.RunGit(ctx, dir, gitArgs...)
	if err != nil {
		return nil, err
	}

	// Count files changed
	filesChanged := countFilesInDiff(diffOutput)

	// Check if output is too large and truncate if needed
	truncated := false
	truncatedMessage := ""
	if len(diffOutput) > maxDiffSize {
		truncated = true
		diffOutput = diffOutput[:maxDiffSize]
		truncatedMessage = "Output truncated at 100KB. Use path parameter to diff specific files."
	}

	result := map[string]any{
		"diff":          diffOutput,
		"truncated":     truncated,
		"files_changed": filesChanged,
	}

	if truncated {
		result["truncated_message"] = truncatedMessage
	}

	return result, nil
}

// countFilesInDiff counts the number of files in a diff output
func countFilesInDiff(diff string) int {
	count := 0
	lines := strings.Split(diff, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git") {
			count++
		}
	}
	return count
}
