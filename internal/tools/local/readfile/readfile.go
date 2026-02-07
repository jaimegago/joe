package readfile

import (
	"context"
	"fmt"
	"os"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/tools/local"
)

const maxFileSize = 1 * 1024 * 1024 // 1MB

type Tool struct{}

func New() *Tool {
	return &Tool{}
}

func (t *Tool) Name() string {
	return "read_file"
}

func (t *Tool) Description() string {
	return "Read contents of a file from the local filesystem. Use this to read configuration files, source code, or any text files the user asks about."
}

func (t *Tool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"path": {
				Type:        "string",
				Description: "Path to file (absolute or relative to current directory, ~ expands to home directory)",
			},
		},
		Required: []string{"path"},
	}
}

func (t *Tool) Execute(ctx context.Context, args map[string]any) (any, error) {
	pathArg, ok := args["path"].(string)
	if !ok || pathArg == "" {
		return nil, fmt.Errorf("path parameter is required and must be a string")
	}

	// Expand path
	absPath, err := local.ExpandPath(pathArg)
	if err != nil {
		return nil, fmt.Errorf("failed to expand path: %w", err)
	}

	// Check if file exists
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found: %s", absPath)
		}
		if os.IsPermission(err) {
			return nil, fmt.Errorf("permission denied: %s", absPath)
		}
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	// Check if it's a directory
	if info.IsDir() {
		return nil, fmt.Errorf("path is a directory, not a file: %s", absPath)
	}

	// Check file size
	if info.Size() > maxFileSize {
		sizeMB := float64(info.Size()) / (1024 * 1024)
		return nil, fmt.Errorf("file too large (%.1fMB), max 1MB supported", sizeMB)
	}

	// Read file
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Check if binary
	if isBinary(data) {
		return nil, fmt.Errorf("file appears to be binary, not text: %s", absPath)
	}

	return map[string]any{
		"path":       absPath,
		"content":    string(data),
		"size_bytes": len(data),
	}, nil
}

// isBinary checks if data appears to be binary by looking for null bytes
func isBinary(data []byte) bool {
	// Check first 512 bytes for null bytes
	checkLen := 512
	if len(data) < checkLen {
		checkLen = len(data)
	}
	for i := 0; i < checkLen; i++ {
		if data[i] == 0 {
			return true
		}
	}
	return false
}
