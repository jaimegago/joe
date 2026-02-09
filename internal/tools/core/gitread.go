package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/llm"
)

// GitReadTool reads files or lists directories from a Git repository source.
type GitReadTool struct {
	client *client.Client
}

// NewGitReadTool creates a new git_read tool.
func NewGitReadTool(c *client.Client) *GitReadTool {
	return &GitReadTool{client: c}
}

func (t *GitReadTool) Name() string { return "git_read" }

func (t *GitReadTool) Description() string {
	return "Read a file or list files in a directory from a connected Git repository source."
}

func (t *GitReadTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_id": {Type: "string", Description: "ID of the Git source."},
			"path":      {Type: "string", Description: "File path to read, or directory path to list."},
			"list":      {Type: "boolean", Description: "If true, list files in the directory instead of reading a file."},
		},
		Required: []string{"source_id", "path"},
	}
}

func (t *GitReadTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["source_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: source_id")
	}

	path, ok := args["path"].(string)
	if !ok || path == "" {
		return nil, fmt.Errorf("missing required parameter: path")
	}

	listMode, _ := args["list"].(bool)

	if listMode {
		files, err := t.client.GitListFiles(ctx, sourceID, path)
		if err != nil {
			return nil, fmt.Errorf("git list files failed: %w", err)
		}
		return map[string]any{
			"files":     files,
			"count":     len(files),
			"dir":       path,
			"source_id": sourceID,
		}, nil
	}

	content, err := t.client.GitReadFile(ctx, sourceID, path)
	if err != nil {
		return nil, fmt.Errorf("git read file failed: %w", err)
	}
	return map[string]any{
		"content":   content,
		"path":      path,
		"source_id": sourceID,
	}, nil
}
