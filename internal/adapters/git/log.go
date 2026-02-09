package git

import (
	"context"
	"fmt"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

const (
	defaultLogLimit = 20
	maxLogLimit     = 500
)

func (a *Adapter) Log(_ context.Context, limit int) ([]CommitInfo, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if !a.connected {
		return nil, fmt.Errorf("adapter not connected")
	}

	if limit <= 0 {
		limit = defaultLogLimit
	}
	if limit > maxLogLimit {
		limit = maxLogLimit
	}

	iter, err := a.repo.Log(&gogit.LogOptions{})
	if err != nil {
		return nil, fmt.Errorf("get log: %w", err)
	}
	defer iter.Close()

	var commits []CommitInfo
	err = iter.ForEach(func(c *object.Commit) error {
		if len(commits) >= limit {
			return storer.ErrStop
		}
		commits = append(commits, CommitInfo{
			Hash:    c.Hash.String(),
			Author:  c.Author.Name,
			Date:    c.Author.When,
			Message: c.Message,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("iterate log: %w", err)
	}

	return commits, nil
}
