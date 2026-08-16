package git

import (
	"context"
	"fmt"
	"io"
	"strings"
)

const maxFileSize = 1 << 20 // 1 MB

// ReadResult is one file read, at one commit.
//
// The commit is part of the answer, on the same terms as SearchResult.Commit
// and for the same reason: repo_search advertises that a hit becomes citable
// only after a git_read re-read at the reported commit, and a caller cannot
// check that rule against a result that does not say where it was answered.
// Reporting it is not decoration on top of the Commit argument — without it a
// caller can pass a commit and never learn whether it was honoured, which is
// the same blindness in a new place.
type ReadResult struct {
	// Commit is the resolved full hash the read answered at, always reported,
	// including when the caller named no commit.
	Commit  string `json:"commit"`
	Content string `json:"content"`
}

// ListResult is one directory listing, at one commit. Commit carries the same
// contract as ReadResult.Commit.
type ListResult struct {
	Commit string     `json:"commit"`
	Files  []FileInfo `json:"files"`
}

// ReadFile returns one file's contents at a commit.
//
// An empty commit resolves the clone's current head; a named one answers at
// exactly that revision or fails. Resolution is resolvePinnedCommit, shared
// with Search rather than reimplemented here.
func (a *Adapter) ReadFile(_ context.Context, path, commit string) (*ReadResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if !a.connected {
		return nil, fmt.Errorf("adapter not connected")
	}

	c, err := a.resolvePinnedCommit(commit)
	if err != nil {
		return nil, err
	}

	tree, err := c.Tree()
	if err != nil {
		return nil, fmt.Errorf("get tree: %w", err)
	}

	file, err := tree.File(path)
	if err != nil {
		return nil, fmt.Errorf("file %q not found: %w", path, err)
	}

	if file.Size > maxFileSize {
		return nil, fmt.Errorf("file %q too large (%d bytes, max %d)", path, file.Size, maxFileSize)
	}

	reader, err := file.Reader()
	if err != nil {
		return nil, fmt.Errorf("open file %q: %w", path, err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read file %q: %w", path, err)
	}

	return &ReadResult{Commit: c.Hash.String(), Content: string(data)}, nil
}

// ListFiles returns the entries of one directory at a commit, on the same
// commit terms as ReadFile.
func (a *Adapter) ListFiles(_ context.Context, dir, commit string) (*ListResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if !a.connected {
		return nil, fmt.Errorf("adapter not connected")
	}

	c, err := a.resolvePinnedCommit(commit)
	if err != nil {
		return nil, err
	}

	tree, err := c.Tree()
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

	return &ListResult{Commit: c.Hash.String(), Files: files}, nil
}
