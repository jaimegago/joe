package git

import (
	"fmt"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// resolvePinnedCommit pins the substrate for every commit-pinned verb on this
// adapter. An empty revision means the clone's current head; a named one
// answers at exactly that revision or fails, never silently at a different one.
//
// It lives in its own file, and is deliberately NOT per-verb, because Search,
// ReadFile and ListFiles have to agree about what a revision means for a
// repo_search hit to be checkable by a git_read at the reported commit. Two
// resolvers would be identical by resemblance and would drift; one is identical
// by construction. That is the same argument D-0152 (h) makes for routing the
// search through the accessor call shape a plain read already uses.
//
// NOT the only resolver in this package, and the name says which one this is.
// diff.go's resolveCommit is a different contract for a different verb: a ref
// lookup over HEAD, branch, tag and full hash, with no empty-means-head case
// and no gitrevisions syntax. Diff takes two refs by design and predates the
// pinning work, so it is left alone rather than folded in on the way past — but
// a later session unifying them should start here, and should treat that as a
// change to git_diff's contract rather than as a refactor.
func (a *Adapter) resolvePinnedCommit(rev string) (*object.Commit, error) {
	var hash plumbing.Hash
	if rev == "" {
		head, err := a.repo.Head()
		if err != nil {
			return nil, fmt.Errorf("get HEAD: %w", err)
		}
		hash = head.Hash()
	} else {
		resolved, err := a.repo.ResolveRevision(plumbing.Revision(rev))
		if err != nil {
			return nil, fmt.Errorf("resolve commit %q: %w", rev, err)
		}
		hash = *resolved
	}

	commit, err := a.repo.CommitObject(hash)
	if err != nil {
		return nil, fmt.Errorf("get commit %s: %w", hash.String(), err)
	}
	return commit, nil
}
