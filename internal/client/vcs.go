package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	githubadapter "github.com/jaimegago/joe/internal/adapters/github"
	gitlabadapter "github.com/jaimegago/joe/internal/adapters/gitlab"
)

// =========================
// GitHub PR operations
// =========================

// GitHubGetPR fetches GitHub pull request metadata.
// Implements tools/core.GitHubPRGetClient.
func (c *Client) GitHubGetPR(ctx context.Context, sourceID, owner, repo string, number int) (*githubadapter.PRInfo, error) {
	u := fmt.Sprintf("%s%s/%s/pulls/%d?owner=%s&repo=%s",
		c.baseURL, apiGitHubBasePath, url.PathEscape(sourceID),
		number, url.QueryEscape(owner), url.QueryEscape(repo))
	var pr githubadapter.PRInfo
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &pr, "github get pr"); err != nil {
		return nil, err
	}
	return &pr, nil
}

// GitHubGetPRDiff fetches the diff for a GitHub pull request.
// Implements tools/core.GitHubPRDiffClient.
func (c *Client) GitHubGetPRDiff(ctx context.Context, sourceID, owner, repo string, number int) (string, error) {
	u := fmt.Sprintf("%s%s/%s/pulls/%d/diff?owner=%s&repo=%s",
		c.baseURL, apiGitHubBasePath, url.PathEscape(sourceID),
		number, url.QueryEscape(owner), url.QueryEscape(repo))
	var result struct {
		Diff string `json:"diff"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "github get pr diff"); err != nil {
		return "", err
	}
	return result.Diff, nil
}

// GitHubListPRs lists pull requests for a GitHub repository.
func (c *Client) GitHubListPRs(ctx context.Context, sourceID, owner, repo, state string) ([]*githubadapter.PRInfo, error) {
	params := url.Values{"owner": {owner}, "repo": {repo}}
	if state != "" {
		params.Set("state", state)
	}
	u := fmt.Sprintf("%s%s/%s/pulls?%s",
		c.baseURL, apiGitHubBasePath, url.PathEscape(sourceID), params.Encode())
	var result struct {
		PRs   []*githubadapter.PRInfo `json:"prs"`
		Count int                     `json:"count"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "github list prs"); err != nil {
		return nil, err
	}
	return result.PRs, nil
}

// GitHubPostComment posts a comment on a GitHub PR.
// Implements tools/core.GitHubCommentClient.
func (c *Client) GitHubPostComment(ctx context.Context, sourceID, owner, repo string, number int, body string) error {
	u := fmt.Sprintf("%s%s/%s/pulls/%d/comments",
		c.baseURL, apiGitHubBasePath, url.PathEscape(sourceID), number)
	payload, err := json.Marshal(map[string]string{"owner": owner, "repo": repo, "body": body})
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	return c.doJSON(ctx, http.MethodPost, u, bytes.NewReader(payload), http.StatusCreated, nil, "github post comment")
}

// GitHubRequestChanges submits a "request changes" review on a GitHub PR.
// Implements tools/core.GitHubRequestChangesClient.
func (c *Client) GitHubRequestChanges(ctx context.Context, sourceID, owner, repo string, number int, body string) error {
	u := fmt.Sprintf("%s%s/%s/pulls/%d/reviews",
		c.baseURL, apiGitHubBasePath, url.PathEscape(sourceID), number)
	payload, err := json.Marshal(map[string]string{"owner": owner, "repo": repo, "body": body})
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	return c.doJSON(ctx, http.MethodPost, u, bytes.NewReader(payload), http.StatusCreated, nil, "github request changes")
}

// =========================
// GitLab MR operations
// =========================

// GitLabGetMR fetches GitLab merge request metadata.
// Implements tools/core.GitLabMRGetClient.
func (c *Client) GitLabGetMR(ctx context.Context, sourceID, projectID string, iid int) (*gitlabadapter.MRInfo, error) {
	u := fmt.Sprintf("%s%s/%s/projects/%s/mrs/%d",
		c.baseURL, apiGitLabBasePath, url.PathEscape(sourceID), url.PathEscape(projectID), iid)
	var mr gitlabadapter.MRInfo
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &mr, "gitlab get mr"); err != nil {
		return nil, err
	}
	return &mr, nil
}

// GitLabGetMRDiff fetches the unified diff for a GitLab merge request.
// Implements tools/core.GitLabMRDiffClient.
func (c *Client) GitLabGetMRDiff(ctx context.Context, sourceID, projectID string, iid int) (string, error) {
	u := fmt.Sprintf("%s%s/%s/projects/%s/mrs/%d/diff",
		c.baseURL, apiGitLabBasePath, url.PathEscape(sourceID), url.PathEscape(projectID), iid)
	var result struct {
		Diff string `json:"diff"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "gitlab get mr diff"); err != nil {
		return "", err
	}
	return result.Diff, nil
}

// GitLabListMRs lists merge requests for a GitLab project.
func (c *Client) GitLabListMRs(ctx context.Context, sourceID, projectID, state string) ([]*gitlabadapter.MRInfo, error) {
	u := fmt.Sprintf("%s%s/%s/projects/%s/mrs",
		c.baseURL, apiGitLabBasePath, url.PathEscape(sourceID), url.PathEscape(projectID))
	if state != "" {
		u += "?state=" + url.QueryEscape(state)
	}
	var result struct {
		MRs   []*gitlabadapter.MRInfo `json:"mrs"`
		Count int                     `json:"count"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "gitlab list mrs"); err != nil {
		return nil, err
	}
	return result.MRs, nil
}

// GitLabPostNote posts a note on a GitLab MR.
// Implements tools/core.GitLabCommentClient.
func (c *Client) GitLabPostNote(ctx context.Context, sourceID, projectID string, iid int, body string) error {
	u := fmt.Sprintf("%s%s/%s/projects/%s/mrs/%d/notes",
		c.baseURL, apiGitLabBasePath, url.PathEscape(sourceID), url.PathEscape(projectID), iid)
	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	return c.doJSON(ctx, http.MethodPost, u, bytes.NewReader(payload), http.StatusCreated, nil, "gitlab post note")
}
