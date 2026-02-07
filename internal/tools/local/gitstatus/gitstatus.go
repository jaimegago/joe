package gitstatus

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/tools/local"
)

type Tool struct{}

type FileStatus struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

func New() *Tool {
	return &Tool{}
}

func (t *Tool) Name() string {
	return "local_git_status"
}

func (t *Tool) Description() string {
	return "Get git status of the current working directory or a specified path. Shows current branch, staged changes, unstaged changes, and untracked files."
}

func (t *Tool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"path": {
				Type:        "string",
				Description: "Directory path (defaults to current working directory, ~ expands to home directory)",
			},
		},
		Required: []string{},
	}
}

func (t *Tool) Execute(ctx context.Context, args map[string]any) (any, error) {
	// Get path, default to CWD
	dir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current directory: %w", err)
	}

	if pathArg, ok := args["path"].(string); ok && pathArg != "" {
		dir, err = local.ExpandPath(pathArg)
		if err != nil {
			return nil, fmt.Errorf("failed to expand path: %w", err)
		}
	}

	// Get current branch
	branch, err := local.RunGit(ctx, dir, "branch", "--show-current")
	if err != nil {
		return nil, err
	}
	branch = strings.TrimSpace(branch)

	// Get status in porcelain format
	statusOutput, err := local.RunGit(ctx, dir, "status", "--porcelain")
	if err != nil {
		return nil, err
	}

	// Parse status
	var staged, unstaged, untracked []FileStatus

	lines := strings.Split(strings.TrimSpace(statusOutput), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}

		if len(line) < 4 {
			continue
		}

		stagedStatus := line[0]
		unstagedStatus := line[1]
		path := strings.TrimSpace(line[3:])

		// Handle staged changes
		if stagedStatus != ' ' && stagedStatus != '?' {
			status := parseStatusCode(stagedStatus)
			staged = append(staged, FileStatus{
				Path:   path,
				Status: status,
			})
		}

		// Handle unstaged changes
		if unstagedStatus != ' ' && unstagedStatus != '?' {
			status := parseStatusCode(unstagedStatus)
			unstaged = append(unstaged, FileStatus{
				Path:   path,
				Status: status,
			})
		}

		// Handle untracked files
		if stagedStatus == '?' && unstagedStatus == '?' {
			untracked = append(untracked, FileStatus{
				Path:   path,
				Status: "untracked",
			})
		}
	}

	isClean := len(staged) == 0 && len(unstaged) == 0 && len(untracked) == 0

	return map[string]any{
		"branch":    branch,
		"is_clean":  isClean,
		"staged":    staged,
		"unstaged":  unstaged,
		"untracked": untracked,
	}, nil
}

func parseStatusCode(code byte) string {
	switch code {
	case 'M':
		return "modified"
	case 'A':
		return "added"
	case 'D':
		return "deleted"
	case 'R':
		return "renamed"
	case 'C':
		return "copied"
	default:
		return string(code)
	}
}
