package coreagent

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/adapters/git"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
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

// --- the hosting edge derived from the declared provider sibling (D-0150) ---

// gitHostingRefresher builds a refresher over a real migrated store (so component
// lookups resolve) sharing that store's graph, and registers the components the
// case needs.
func gitHostingRefresher(t *testing.T, comps ...*store.Component) *Refresher {
	t.Helper()
	svc := makeTestServices(t)
	for _, c := range comps {
		if c.Config == nil {
			c.Config = json.RawMessage(`{}`)
		}
		if err := svc.Store.Components.Create(context.Background(), c); err != nil {
			t.Fatalf("seed component %s: %v", c.ID, err)
		}
	}
	return &Refresher{services: svc, logger: slog.Default()}
}

// TestRefreshGitComponent_EmitsHostingEdge proves the refresher derives one
// deterministic hosting edge from the git component's DECLARED
// provider_component_id when it resolves to a real github or gitlab component.
// Nothing is inferred: the edge exists because an operator said so, and its
// metadata carries the named component so a reader can find the provider's PR/MR
// surface beside the repository.
func TestRefreshGitComponent_EmitsHostingEdge(t *testing.T) {
	r := gitHostingRefresher(t,
		&store.Component{ID: "gh-main", Type: store.ComponentTypeGitHub, Name: "corp github"},
		&store.Component{ID: "repo-a", Type: store.ComponentTypeGit, Name: "repo a",
			Config: json.RawMessage(`{"url":"https://example.com/r.git","provider_component_id":"gh-main"}`)},
	)
	source, err := r.services.Store.Components.Get(context.Background(), "repo-a")
	if err != nil {
		t.Fatalf("get seeded component: %v", err)
	}

	if err := r.refreshGitComponent(context.Background(), source, &fakeGitAdapter{}); err != nil {
		t.Fatalf("refreshGitComponent: %v", err)
	}

	_, edges, err := LoadGraphStateForComponent(context.Background(), r.services.Graph, "repo-a")
	if err != nil {
		t.Fatalf("load graph state: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("edges = %d, want exactly 1 hosting edge", len(edges))
	}
	e := edges[0]
	if e.Relation != graph.RelationHostedBy {
		t.Errorf("relation = %q, want %q", e.Relation, graph.RelationHostedBy)
	}
	if e.From != "git/repo-a/repo" {
		t.Errorf("edge From = %q, want the repository anchor node", e.From)
	}
	if e.To != "git/repo-a/provider" {
		t.Errorf("edge To = %q, want this component's host node", e.To)
	}
	if e.Confidence != graph.Explicit {
		t.Errorf("confidence = %v, want Explicit — the edge restates an operator declaration, it does not guess", e.Confidence)
	}
}

// TestRefreshGitComponent_DanglingProviderSkipped proves a declaration naming no
// existing component yields NO node and NO edge rather than an error or a false
// hosting claim. The reference is shape-validated at registration only, so this
// state is legal and expected — the named component may be registered later or
// deleted afterwards.
func TestRefreshGitComponent_DanglingProviderSkipped(t *testing.T) {
	r := gitHostingRefresher(t,
		&store.Component{ID: "repo-b", Type: store.ComponentTypeGit, Name: "repo b",
			Config: json.RawMessage(`{"url":"https://example.com/r.git","provider_component_id":"gh-never-registered"}`)},
	)
	source, err := r.services.Store.Components.Get(context.Background(), "repo-b")
	if err != nil {
		t.Fatalf("get seeded component: %v", err)
	}

	if err := r.refreshGitComponent(context.Background(), source, &fakeGitAdapter{}); err != nil {
		t.Fatalf("refreshGitComponent with a dangling reference must not fail the tick: %v", err)
	}
	nodes, edges, err := LoadGraphStateForComponent(context.Background(), r.services.Graph, "repo-b")
	if err != nil {
		t.Fatalf("load graph state: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("edges = %d, want 0 — a dangling declaration must not produce a hosting claim", len(edges))
	}
	if len(nodes) != 1 {
		t.Errorf("nodes = %d, want only the repository anchor", len(nodes))
	}
}

// TestRefreshGitComponent_NonProviderTypeSkipped proves the declaration is only
// honoured when it names a github or gitlab component. Pointing it at some other
// registered component is not a hosting relationship and produces no edge.
func TestRefreshGitComponent_NonProviderTypeSkipped(t *testing.T) {
	r := gitHostingRefresher(t,
		&store.Component{ID: "k8s-prod", Type: store.ComponentTypeKubernetes, Name: "prod"},
		&store.Component{ID: "repo-c", Type: store.ComponentTypeGit, Name: "repo c",
			Config: json.RawMessage(`{"url":"https://example.com/r.git","provider_component_id":"k8s-prod"}`)},
	)
	source, err := r.services.Store.Components.Get(context.Background(), "repo-c")
	if err != nil {
		t.Fatalf("get seeded component: %v", err)
	}

	if err := r.refreshGitComponent(context.Background(), source, &fakeGitAdapter{}); err != nil {
		t.Fatalf("refreshGitComponent: %v", err)
	}
	_, edges, err := LoadGraphStateForComponent(context.Background(), r.services.Graph, "repo-c")
	if err != nil {
		t.Fatalf("load graph state: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("edges = %d, want 0 — only a github or gitlab component hosts a repository", len(edges))
	}
}

// TestRefreshGitComponent_HostingEdgeReconcilesAway proves the edge follows the
// declaration: clearing provider_component_id removes the edge on the next tick,
// because the refresher reconciles its full desired set rather than accumulating.
func TestRefreshGitComponent_HostingEdgeReconcilesAway(t *testing.T) {
	r := gitHostingRefresher(t,
		&store.Component{ID: "gh-main", Type: store.ComponentTypeGitHub, Name: "corp github"},
		&store.Component{ID: "repo-d", Type: store.ComponentTypeGit, Name: "repo d",
			Config: json.RawMessage(`{"url":"https://example.com/r.git","provider_component_id":"gh-main"}`)},
	)
	ctx := context.Background()
	source, err := r.services.Store.Components.Get(ctx, "repo-d")
	if err != nil {
		t.Fatalf("get seeded component: %v", err)
	}
	if err := r.refreshGitComponent(ctx, source, &fakeGitAdapter{}); err != nil {
		t.Fatalf("first refresh: %v", err)
	}

	source.Config = json.RawMessage(`{"url":"https://example.com/r.git"}`)
	if err := r.refreshGitComponent(ctx, source, &fakeGitAdapter{}); err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	nodes, edges, err := LoadGraphStateForComponent(ctx, r.services.Graph, "repo-d")
	if err != nil {
		t.Fatalf("load graph state: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("edges = %d after the declaration was cleared, want 0", len(edges))
	}
	if len(nodes) != 1 {
		t.Errorf("nodes = %d after the declaration was cleared, want only the repository anchor", len(nodes))
	}
}
