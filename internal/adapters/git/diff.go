package git

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

const maxDiffSize = 1 << 20 // 1 MB

func (a *Adapter) Diff(_ context.Context, fromRef, toRef string) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if !a.connected {
		return "", fmt.Errorf("adapter not connected")
	}

	fromCommit, err := a.resolveCommit(fromRef)
	if err != nil {
		return "", fmt.Errorf("resolve from ref %q: %w", fromRef, err)
	}

	toCommit, err := a.resolveCommit(toRef)
	if err != nil {
		return "", fmt.Errorf("resolve to ref %q: %w", toRef, err)
	}

	fromTree, err := fromCommit.Tree()
	if err != nil {
		return "", fmt.Errorf("get from tree: %w", err)
	}

	toTree, err := toCommit.Tree()
	if err != nil {
		return "", fmt.Errorf("get to tree: %w", err)
	}

	changes, err := fromTree.Diff(toTree)
	if err != nil {
		return "", fmt.Errorf("compute diff: %w", err)
	}

	patch, err := changes.Patch()
	if err != nil {
		return "", fmt.Errorf("generate patch: %w", err)
	}

	diffStr := patch.String()
	if len(diffStr) > maxDiffSize {
		diffStr = diffStr[:maxDiffSize] + "\n... (truncated)"
	}

	return diffStr, nil
}

// resolveCommit resolves a ref string (branch name, tag, or commit hash) to a commit.
func (a *Adapter) resolveCommit(ref string) (*object.Commit, error) {
	// Try as HEAD
	if strings.EqualFold(ref, "HEAD") {
		headRef, err := a.repo.Head()
		if err != nil {
			return nil, fmt.Errorf("resolve HEAD: %w", err)
		}
		return a.repo.CommitObject(headRef.Hash())
	}

	// Try as branch reference
	branchRef, err := a.repo.Reference(plumbing.NewBranchReferenceName(ref), true)
	if err == nil {
		return a.repo.CommitObject(branchRef.Hash())
	}

	// Try as tag reference
	tagRef, err := a.repo.Reference(plumbing.NewTagReferenceName(ref), true)
	if err == nil {
		return a.repo.CommitObject(tagRef.Hash())
	}

	// Try as commit hash
	hash := plumbing.NewHash(ref)
	commit, err := a.repo.CommitObject(hash)
	if err != nil {
		return nil, fmt.Errorf("resolve ref %q: not a branch, tag, or commit hash", ref)
	}
	return commit, nil
}
