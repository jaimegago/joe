package writefile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/tools/local"
)

type Tool struct{}

func New() *Tool {
	return &Tool{}
}

func (t *Tool) Name() string {
	return "write_file"
}

func (t *Tool) Description() string {
	return "Write content to a file on the local filesystem. Creates the file if it doesn't exist, overwrites if it does. Parent directories are created automatically."
}

func (t *Tool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"path": {
				Type:        "string",
				Description: "Path to file (absolute or relative to current directory, ~ expands to home directory)",
			},
			"content": {
				Type:        "string",
				Description: "Content to write to the file",
			},
		},
		Required: []string{"path", "content"},
	}
}

func (t *Tool) Execute(ctx context.Context, args map[string]any) (any, error) {
	pathArg, ok := args["path"].(string)
	if !ok || pathArg == "" {
		return nil, fmt.Errorf("path parameter is required and must be a string")
	}

	content, ok := args["content"].(string)
	if !ok {
		return nil, fmt.Errorf("content parameter is required and must be a string")
	}

	// Expand path
	absPath, err := local.ExpandPath(pathArg)
	if err != nil {
		return nil, fmt.Errorf("failed to expand path: %w", err)
	}

	// Check if file exists to determine if we're creating or overwriting
	_, err = os.Stat(absPath)
	created := os.IsNotExist(err)

	// Create parent directories if they don't exist
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create parent directories: %w", err)
	}

	// Write file
	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	return map[string]any{
		"path":          absPath,
		"bytes_written": len(content),
		"created":       created,
	}, nil
}
