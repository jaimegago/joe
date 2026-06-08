package access

import (
	"context"
	"errors"
	"fmt"

	"github.com/jaimegago/joe/internal/adapters"
	githubadapter "github.com/jaimegago/joe/internal/adapters/github"
	gitlabadapter "github.com/jaimegago/joe/internal/adapters/gitlab"
	"github.com/jaimegago/joe/internal/rbac"
)

// GitHubWebhookSecret returns the configured webhook secret used to HMAC-
// validate inbound GitHub webhooks. This is adapter CONFIG, not a
// principal-gated infrastructure operation: webhook receivers run before any
// Joe principal exists (the sender is authenticated via HMAC, not the Joe
// API key), so no RBAC decision applies and the method takes no principal.
// It still resolves through the accessor so the adapter registry stays
// reachable from exactly one package. (The action-declaration static guard
// exempts accessor methods that take no rbac.Principal — these config
// resolvers — by design.)
func (a *Accessor) GitHubWebhookSecret(sourceID string) (string, error) {
	adapter, err := a.registry.Get(sourceID)
	if err != nil {
		if errors.Is(err, adapters.ErrAdapterNotFound) {
			return "", fmt.Errorf("%w: %s", ErrComponentNotFound, sourceID)
		}
		return "", err
	}
	gh, ok := adapter.(githubadapter.GitHubAdapter)
	if !ok {
		return "", fmt.Errorf("%w: github", ErrWrongAdapterType)
	}
	return gh.WebhookSecret(), nil
}

// GitLabWebhookSecret is the GitLab counterpart of GitHubWebhookSecret. Same
// rationale: pre-auth config read, no principal, no RBAC.
func (a *Accessor) GitLabWebhookSecret(sourceID string) (string, error) {
	adapter, err := a.registry.Get(sourceID)
	if err != nil {
		if errors.Is(err, adapters.ErrAdapterNotFound) {
			return "", fmt.Errorf("%w: %s", ErrComponentNotFound, sourceID)
		}
		return "", err
	}
	gl, ok := adapter.(gitlabadapter.GitLabAdapter)
	if !ok {
		return "", fmt.Errorf("%w: gitlab", ErrWrongAdapterType)
	}
	return gl.WebhookSecret(), nil
}

// --- GitHub ---

func (a *Accessor) GitHubGetPR(ctx context.Context, principal rbac.Principal, sourceID, owner, repo string, number int) (*githubadapter.PRInfo, error) {
	ad, err := guard[githubadapter.GitHubAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "github")
	if err != nil {
		return nil, err
	}
	return ad.GetPR(ctx, owner, repo, number)
}

func (a *Accessor) GitHubGetPRDiff(ctx context.Context, principal rbac.Principal, sourceID, owner, repo string, number int) (string, error) {
	ad, err := guard[githubadapter.GitHubAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "github")
	if err != nil {
		return "", err
	}
	return ad.GetPRDiff(ctx, owner, repo, number)
}

func (a *Accessor) GitHubListPRs(ctx context.Context, principal rbac.Principal, sourceID, owner, repo, state string) ([]*githubadapter.PRInfo, error) {
	ad, err := guard[githubadapter.GitHubAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "github")
	if err != nil {
		return nil, err
	}
	return ad.ListPRs(ctx, owner, repo, state)
}

func (a *Accessor) GitHubPostComment(ctx context.Context, principal rbac.Principal, sourceID, owner, repo string, number int, body string) error {
	ad, err := guard[githubadapter.GitHubAdapter](a, ctx, principal, sourceID, rbac.ActionMutate, "github")
	if err != nil {
		return err
	}
	return ad.PostComment(ctx, owner, repo, number, body)
}

func (a *Accessor) GitHubRequestChanges(ctx context.Context, principal rbac.Principal, sourceID, owner, repo string, number int, body string) error {
	ad, err := guard[githubadapter.GitHubAdapter](a, ctx, principal, sourceID, rbac.ActionMutate, "github")
	if err != nil {
		return err
	}
	return ad.RequestChanges(ctx, owner, repo, number, body)
}

// --- GitLab ---

func (a *Accessor) GitLabGetMR(ctx context.Context, principal rbac.Principal, sourceID, projectID string, iid int) (*gitlabadapter.MRInfo, error) {
	ad, err := guard[gitlabadapter.GitLabAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "gitlab")
	if err != nil {
		return nil, err
	}
	return ad.GetMR(ctx, projectID, iid)
}

func (a *Accessor) GitLabGetMRDiff(ctx context.Context, principal rbac.Principal, sourceID, projectID string, iid int) (string, error) {
	ad, err := guard[gitlabadapter.GitLabAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "gitlab")
	if err != nil {
		return "", err
	}
	return ad.GetMRDiff(ctx, projectID, iid)
}

func (a *Accessor) GitLabListMRs(ctx context.Context, principal rbac.Principal, sourceID, projectID, state string) ([]*gitlabadapter.MRInfo, error) {
	ad, err := guard[gitlabadapter.GitLabAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "gitlab")
	if err != nil {
		return nil, err
	}
	return ad.ListMRs(ctx, projectID, state)
}

func (a *Accessor) GitLabPostNote(ctx context.Context, principal rbac.Principal, sourceID, projectID string, iid int, body string) error {
	ad, err := guard[gitlabadapter.GitLabAdapter](a, ctx, principal, sourceID, rbac.ActionMutate, "gitlab")
	if err != nil {
		return err
	}
	return ad.PostNote(ctx, projectID, iid, body)
}

func (a *Accessor) GitLabRequestChanges(ctx context.Context, principal rbac.Principal, sourceID, projectID string, iid int, body string) error {
	ad, err := guard[gitlabadapter.GitLabAdapter](a, ctx, principal, sourceID, rbac.ActionMutate, "gitlab")
	if err != nil {
		return err
	}
	return ad.RequestChanges(ctx, projectID, iid, body)
}
