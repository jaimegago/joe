package coreagent

import (
	"context"
	"log/slog"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/adapters/git"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/store"
	_ "github.com/mattn/go-sqlite3"
)

type fakeGitAdapter struct {
	files []git.FileInfo
	logs  []git.CommitInfo
}

func (f *fakeGitAdapter) Connect(_ context.Context, _ store.Source) error { return nil }

func (f *fakeGitAdapter) Disconnect() error { return nil }

func (f *fakeGitAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true, Message: "connected"}
}

func (f *fakeGitAdapter) ReadFile(_ context.Context, path string) (string, error) {
	return "", nil
}

func (f *fakeGitAdapter) ListFiles(_ context.Context, dir string) ([]git.FileInfo, error) {
	if dir == ".joe" {
		return f.files, nil
	}
	return nil, nil
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

func TestRefreshGitSourceBasic(t *testing.T) {
	graphStore := setupGraphStore(t)
	cache := newFakeCache()
	fakeLLM := &fakeLLM{}
	joeFileService := NewJoeFileService(cache, fakeLLM, slog.Default(), nil)

	refresher := &Refresher{
		services:       &core.Services{Graph: graphStore},
		joeFileService: joeFileService,
		logger:         slog.Default(),
	}

	source := &store.Source{ID: "src-git-1", Type: store.SourceTypeGit, Name: "test-repo"}
	adapter := &fakeGitAdapter{
		files: []git.FileInfo{
			{Path: ".joe/manifest.yaml", IsDir: false},
			{Path: ".joe/sources.yaml", IsDir: false},
		},
		logs: []git.CommitInfo{
			{Hash: "abc123", Author: "alice", Message: "fix: update deployment"},
		},
	}

	if err := refresher.refreshGitSource(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshGitSource error: %v", err)
	}

	nodes, edges, err := LoadGraphStateForSource(context.Background(), graphStore, source.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForSource error: %v", err)
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

	if joePresent, ok := nodes[0].Metadata["joe_dir_present"].(bool); !ok || !joePresent {
		t.Fatalf("joe_dir_present = %v, want true", joePresent)
	}

	if headCommit, ok := nodes[0].Metadata["head_commit"].(string); !ok || headCommit != "abc123" {
		t.Fatalf("head_commit = %q, want abc123", headCommit)
	}

	if len(edges) != 0 {
		t.Fatalf("edges count = %d, want 0", len(edges))
	}
}

func TestRefreshGitSourceNoJoeFiles(t *testing.T) {
	graphStore := setupGraphStore(t)
	cache := newFakeCache()
	fakeLLM := &fakeLLM{}
	joeFileService := NewJoeFileService(cache, fakeLLM, slog.Default(), nil)

	refresher := &Refresher{
		services:       &core.Services{Graph: graphStore},
		joeFileService: joeFileService,
		logger:         slog.Default(),
	}

	source := &store.Source{ID: "src-git-2", Type: store.SourceTypeGit, Name: "test-repo"}
	adapter := &fakeGitAdapter{
		files: []git.FileInfo{
			{Path: "README.md", IsDir: false},
		},
		logs: []git.CommitInfo{
			{Hash: "def456", Author: "bob", Message: "docs: update readme"},
		},
	}

	if err := refresher.refreshGitSource(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshGitSource error: %v", err)
	}

	nodes, _, err := LoadGraphStateForSource(context.Background(), graphStore, source.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForSource error: %v", err)
	}

	if joePresent, ok := nodes[0].Metadata["joe_dir_present"].(bool); !ok || joePresent {
		t.Fatalf("joe_dir_present = %v, want false", joePresent)
	}
}

var _ git.GitAdapter = (*fakeGitAdapter)(nil)
