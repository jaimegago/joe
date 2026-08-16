package access

import (
	"context"

	gitadapter "github.com/jaimegago/joe/internal/adapters/git"
	"github.com/jaimegago/joe/internal/rbac"
)

// GitReadFile reads a file from the repository at a commit. An empty commit
// resolves the clone's current head; either way the result reports the commit
// the read answered at.
func (a *Accessor) GitReadFile(ctx context.Context, principal rbac.Principal, sourceID, path, commit string) (*gitadapter.ReadResult, error) {
	ad, err := guard[gitadapter.GitAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "git")
	if err != nil {
		return nil, err
	}
	return ad.ReadFile(ctx, path, commit)
}

// GitListFiles lists files under a directory in the repository, on the same
// commit terms as GitReadFile.
func (a *Accessor) GitListFiles(ctx context.Context, principal rbac.Principal, sourceID, dir, commit string) (*gitadapter.ListResult, error) {
	ad, err := guard[gitadapter.GitAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "git")
	if err != nil {
		return nil, err
	}
	return ad.ListFiles(ctx, dir, commit)
}

// GitLog returns up to limit recent commits.
func (a *Accessor) GitLog(ctx context.Context, principal rbac.Principal, sourceID string, limit int) ([]gitadapter.CommitInfo, error) {
	ad, err := guard[gitadapter.GitAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "git")
	if err != nil {
		return nil, err
	}
	return ad.Log(ctx, limit)
}

// GitSearch searches file contents at a pinned commit in the repository.
func (a *Accessor) GitSearch(ctx context.Context, principal rbac.Principal, sourceID string, opts gitadapter.SearchOptions) (*gitadapter.SearchResult, error) {
	ad, err := guard[gitadapter.GitAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "git")
	if err != nil {
		return nil, err
	}
	return ad.Search(ctx, opts)
}

// GitDiff returns the diff between two refs.
func (a *Accessor) GitDiff(ctx context.Context, principal rbac.Principal, sourceID, fromRef, toRef string) (string, error) {
	ad, err := guard[gitadapter.GitAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "git")
	if err != nil {
		return "", err
	}
	return ad.Diff(ctx, fromRef, toRef)
}
