package access

import (
	"context"

	gitadapter "github.com/jaimegago/joe/internal/adapters/git"
	"github.com/jaimegago/joe/internal/rbac"
)

// GitReadFile reads a file from the repository.
func (a *Accessor) GitReadFile(ctx context.Context, principal rbac.Principal, sourceID, path string) (string, error) {
	ad, err := guard[gitadapter.GitAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "git")
	if err != nil {
		return "", err
	}
	return ad.ReadFile(ctx, path)
}

// GitListFiles lists files under a directory in the repository.
func (a *Accessor) GitListFiles(ctx context.Context, principal rbac.Principal, sourceID, dir string) ([]gitadapter.FileInfo, error) {
	ad, err := guard[gitadapter.GitAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "git")
	if err != nil {
		return nil, err
	}
	return ad.ListFiles(ctx, dir)
}

// GitLog returns up to limit recent commits.
func (a *Accessor) GitLog(ctx context.Context, principal rbac.Principal, sourceID string, limit int) ([]gitadapter.CommitInfo, error) {
	ad, err := guard[gitadapter.GitAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "git")
	if err != nil {
		return nil, err
	}
	return ad.Log(ctx, limit)
}

// GitDiff returns the diff between two refs.
func (a *Accessor) GitDiff(ctx context.Context, principal rbac.Principal, sourceID, fromRef, toRef string) (string, error) {
	ad, err := guard[gitadapter.GitAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "git")
	if err != nil {
		return "", err
	}
	return ad.Diff(ctx, fromRef, toRef)
}
