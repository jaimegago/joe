package core

import (
	"context"
	"fmt"

	gitadapter "github.com/jaimegago/joe/internal/adapters/git"
	"github.com/jaimegago/joe/internal/llm"
)

// GitReadClient defines the subset of client.Client needed for GitReadTool.
type GitReadClient interface {
	GitReadFile(ctx context.Context, sourceID, path, commit string) (*gitadapter.ReadResult, error)
	GitListFiles(ctx context.Context, sourceID, dir, commit string) (*gitadapter.ListResult, error)
}

// GitReadTool reads files or lists directories from a Git repository component.
type GitReadTool struct {
	client GitReadClient
}

// NewGitReadTool creates a new git_read tool.
func NewGitReadTool(c GitReadClient) *GitReadTool {
	return &GitReadTool{client: c}
}

func (t *GitReadTool) Name() string { return "git_read" }

// Description states the commit contract, because the loop knows only what the
// tool advertises — the same reason repo_search carries its caveats. repo_search
// tells the loop to re-read at the reported commit before citing anything, and
// that instruction is only performable if this tool says it takes a commit and
// says which one it answered at.
func (t *GitReadTool) Description() string {
	return "Read a file or list files in a directory from a connected Git repository component, at a pinned commit. " +
		"The commit is optional: omit it to answer at the clone's current head, or name one to answer at exactly that commit or fail — never silently at a different one. " +
		"Every result reports the full hash of the commit it answered at, including when no commit was given, " +
		"so a repo_search hit can be re-read here at the commit that search reported and the two answers compared."
}

func (t *GitReadTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {Type: "string", Description: "ID of the Git component."},
			"path":         {Type: "string", Description: "File path to read, or directory path to list."},
			"list":         {Type: "boolean", Description: "If true, list files in the directory instead of reading a file."},
			"commit":       {Type: "string", Description: "Commit to read at. Omit to read the clone's current head, which is reported back. If given, the read answers at exactly that commit or fails."},
		},
		Required: []string{"component_id", "path"},
	}
}

func (t *GitReadTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}

	path, ok := args["path"].(string)
	if !ok || path == "" {
		return nil, fmt.Errorf("missing required parameter: path")
	}

	listMode, _ := args["list"].(bool)
	commit, _ := args["commit"].(string)

	// List mode honours the commit on the same terms as read mode. A tool that
	// accepts a commit and, in one of its two modes, silently answers at a
	// different one is precisely the fiction the pinned-commit design exists to
	// prevent.
	if listMode {
		res, err := t.client.GitListFiles(ctx, sourceID, path, commit)
		if err != nil {
			return nil, fmt.Errorf("git list files failed: %w", err)
		}
		return map[string]any{
			"files":        res.Files,
			"count":        len(res.Files),
			"dir":          path,
			"commit":       res.Commit,
			"component_id": sourceID,
		}, nil
	}

	res, err := t.client.GitReadFile(ctx, sourceID, path, commit)
	if err != nil {
		return nil, fmt.Errorf("git read file failed: %w", err)
	}
	return map[string]any{
		"content":      res.Content,
		"path":         path,
		"commit":       res.Commit,
		"component_id": sourceID,
	}, nil
}
