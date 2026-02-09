package git

import (
	"context"
	"fmt"
	"io"
	"strings"
)

const maxFileSize = 1 << 20 // 1 MB

func (a *Adapter) ReadFile(_ context.Context, path string) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if !a.connected {
		return "", fmt.Errorf("adapter not connected")
	}

	head, err := a.repo.Head()
	if err != nil {
		return "", fmt.Errorf("get HEAD: %w", err)
	}

	commit, err := a.repo.CommitObject(head.Hash())
	if err != nil {
		return "", fmt.Errorf("get HEAD commit: %w", err)
	}

	tree, err := commit.Tree()
	if err != nil {
		return "", fmt.Errorf("get tree: %w", err)
	}

	file, err := tree.File(path)
	if err != nil {
		return "", fmt.Errorf("file %q not found: %w", path, err)
	}

	if file.Size > maxFileSize {
		return "", fmt.Errorf("file %q too large (%d bytes, max %d)", path, file.Size, maxFileSize)
	}

	reader, err := file.Reader()
	if err != nil {
		return "", fmt.Errorf("open file %q: %w", path, err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("read file %q: %w", path, err)
	}

	return string(data), nil
}

func (a *Adapter) ListFiles(_ context.Context, dir string) ([]FileInfo, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if !a.connected {
		return nil, fmt.Errorf("adapter not connected")
	}

	head, err := a.repo.Head()
	if err != nil {
		return nil, fmt.Errorf("get HEAD: %w", err)
	}

	commit, err := a.repo.CommitObject(head.Hash())
	if err != nil {
		return nil, fmt.Errorf("get HEAD commit: %w", err)
	}

	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("get tree: %w", err)
	}

	if dir != "" && dir != "." && dir != "/" {
		dir = strings.TrimPrefix(dir, "/")
		dir = strings.TrimSuffix(dir, "/")
		tree, err = tree.Tree(dir)
		if err != nil {
			return nil, fmt.Errorf("directory %q not found: %w", dir, err)
		}
	}

	var files []FileInfo
	for _, entry := range tree.Entries {
		fi := FileInfo{
			Path:  entry.Name,
			IsDir: !entry.Mode.IsFile(),
		}
		if !fi.IsDir {
			f, fErr := tree.TreeEntryFile(&entry)
			if fErr == nil {
				fi.Size = f.Size
			}
		}
		files = append(files, fi)
	}

	return files, nil
}
