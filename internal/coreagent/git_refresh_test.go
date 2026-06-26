package coreagent

import (
	"context"
	"log/slog"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/adapters/git"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/store"
	_ "modernc.org/sqlite"
)

type fakeGitAdapter struct {
	files []git.FileInfo
	logs  []git.CommitInfo
}

func (f *fakeGitAdapter) Connect(_ context.Context, _ store.Component) error { return nil }

func (f *fakeGitAdapter) Disconnect() error { return nil }

func (f *fakeGitAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true, Message: "connected"}
}

func (f *fakeGitAdapter) ReadFile(_ context.Context, path string) (string, error) {
	return "", nil
}

func (f *fakeGitAdapter) ListFiles(_ context.Context, dir string) ([]git.FileInfo, error) {
	return f.files, nil
}

func (f *fakeGitAdapter) Log(_ context.Context, limit int) ([]git.CommitInfo, error) {
	if len(f.logs) > limit && limit > 0 {
		return f.logs[:limit], nil
	}
	return f.logs, nil
}

func (f *fakeGitAdapter) Diff(_ context.Context, _, _ string) (string, error) {
	return "", nil
}

// TestRefreshGitComponentBasic verifies the git_repo node is built from HEAD
// commit identity (hash, date, author) on every refresh.
func TestRefreshGitComponentBasic(t *testing.T) {
	graphStore := setupGraphStore(t)

	refresher := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}

	source := &store.Component{ID: "src-git-1", Type: store.ComponentTypeGit, Name: "test-repo"}
	adapter := &fakeGitAdapter{
		logs: []git.CommitInfo{
			{Hash: "abc123", Author: "alice", Message: "fix: update deployment"},
		},
	}

	if err := refresher.refreshGitComponent(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshGitComponent error: %v", err)
	}

	nodes, edges, err := LoadGraphStateForComponent(context.Background(), graphStore, source.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent error: %v", err)
	}

	if len(nodes) != 1 {
		t.Fatalf("nodes count = %d, want 1", len(nodes))
	}

	if nodes[0].Type != "git_repo" {
		t.Fatalf("node type = %q, want git_repo", nodes[0].Type)
	}

	if nodes[0].ID != "git/src-git-1/repo" {
		t.Fatalf("node ID = %q, want git/src-git-1/repo", nodes[0].ID)
	}

	if headCommit, ok := nodes[0].Metadata["head_commit"].(string); !ok || headCommit != "abc123" {
		t.Fatalf("head_commit = %q, want abc123", headCommit)
	}

	if author, ok := nodes[0].Metadata["latest_author"].(string); !ok || author != "alice" {
		t.Fatalf("latest_author = %q, want alice", author)
	}

	if len(edges) != 0 {
		t.Fatalf("edges count = %d, want 0", len(edges))
	}
}

var _ git.GitAdapter = (*fakeGitAdapter)(nil)
