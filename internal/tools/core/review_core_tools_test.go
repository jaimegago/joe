package core_test

import (
	"context"
	"errors"
	"testing"

	githubadapter "github.com/jaimegago/joe/internal/adapters/github"
	gitlabadapter "github.com/jaimegago/joe/internal/adapters/gitlab"
	"github.com/jaimegago/joe/internal/tools/core"
)

// ---- GitHubPRGetTool ----

type fakeGitHubPRGetClient struct {
	fn func(ctx context.Context, sourceID, owner, repo string, number int) (*githubadapter.PRInfo, error)
}

func (f *fakeGitHubPRGetClient) GitHubGetPR(ctx context.Context, sourceID, owner, repo string, number int) (*githubadapter.PRInfo, error) {
	return f.fn(ctx, sourceID, owner, repo, number)
}

func TestGitHubPRGetTool(t *testing.T) {
	fake := &fakeGitHubPRGetClient{
		fn: func(_ context.Context, sourceID, owner, repo string, number int) (*githubadapter.PRInfo, error) {
			if sourceID == "gh-1" && owner == "acme" && repo == "infra" && number == 42 {
				return &githubadapter.PRInfo{Number: 42, Title: "Fix bug", State: "open"}, nil
			}
			return nil, errors.New("not found")
		},
	}
	tool := core.NewGitHubPRGetTool(fake)

	t.Run("name and metadata", func(t *testing.T) {
		if tool.Name() != "github_pr_get" {
			t.Errorf("Name() = %q, want github_pr_get", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("Description() should not be empty")
		}
		params := tool.Parameters()
		if params.Type != "object" {
			t.Errorf("Parameters().Type = %q, want object", params.Type)
		}
		if _, ok := params.Properties["source_id"]; !ok {
			t.Error("Parameters() missing source_id")
		}
	})

	t.Run("missing source_id/owner/repo", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{"pr_number": float64(42)})
		if err == nil {
			t.Error("expected error for missing required fields")
		}
	})

	t.Run("invalid pr_number", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "gh-1",
			"owner":     "acme",
			"repo":      "infra",
		})
		if err == nil {
			t.Error("expected error for missing pr_number")
		}
	})

	t.Run("zero pr_number", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "gh-1",
			"owner":     "acme",
			"repo":      "infra",
			"pr_number": float64(0),
		})
		if err == nil {
			t.Error("expected error for zero pr_number")
		}
	})

	t.Run("success", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "gh-1",
			"owner":     "acme",
			"repo":      "infra",
			"pr_number": float64(42),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		pr := res.(*githubadapter.PRInfo)
		if pr.Number != 42 {
			t.Errorf("Number = %v, want 42", pr.Number)
		}
		if pr.Title != "Fix bug" {
			t.Errorf("Title = %q, want Fix bug", pr.Title)
		}
	})

	t.Run("client error", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "gh-1",
			"owner":     "acme",
			"repo":      "infra",
			"pr_number": float64(99),
		})
		if err == nil {
			t.Error("expected error from client")
		}
	})
}

// ---- GitHubPRDiffTool ----

type fakeGitHubPRDiffClient struct {
	fn func(ctx context.Context, sourceID, owner, repo string, number int) (string, error)
}

func (f *fakeGitHubPRDiffClient) GitHubGetPRDiff(ctx context.Context, sourceID, owner, repo string, number int) (string, error) {
	return f.fn(ctx, sourceID, owner, repo, number)
}

func TestGitHubPRDiffTool(t *testing.T) {
	fake := &fakeGitHubPRDiffClient{
		fn: func(_ context.Context, sourceID, owner, repo string, number int) (string, error) {
			if sourceID == "gh-1" && number == 42 {
				return "diff --git a/main.go b/main.go\n+added line", nil
			}
			return "", errors.New("not found")
		},
	}
	tool := core.NewGitHubPRDiffTool(fake)

	t.Run("name and metadata", func(t *testing.T) {
		if tool.Name() != "github_pr_diff" {
			t.Errorf("Name() = %q, want github_pr_diff", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("Description() should not be empty")
		}
		params := tool.Parameters()
		if params.Type != "object" {
			t.Errorf("Parameters().Type = %q, want object", params.Type)
		}
	})

	t.Run("missing required fields", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{"pr_number": float64(42)})
		if err == nil {
			t.Error("expected error for missing required fields")
		}
	})

	t.Run("missing pr_number", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "gh-1",
			"owner":     "acme",
			"repo":      "infra",
		})
		if err == nil {
			t.Error("expected error for missing pr_number")
		}
	})

	t.Run("success", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "gh-1",
			"owner":     "acme",
			"repo":      "infra",
			"pr_number": float64(42),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		if m["source_id"] != "gh-1" {
			t.Errorf("source_id = %v, want gh-1", m["source_id"])
		}
		diff, _ := m["diff"].(string)
		if diff == "" {
			t.Error("expected non-empty diff")
		}
	})

	t.Run("client error", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "gh-1",
			"owner":     "acme",
			"repo":      "infra",
			"pr_number": float64(99),
		})
		if err == nil {
			t.Error("expected error from client")
		}
	})
}

// ---- GitHubCommentTool ----

type fakeGitHubCommentClient struct {
	fn func(ctx context.Context, sourceID, owner, repo string, number int, body string) error
}

func (f *fakeGitHubCommentClient) GitHubPostComment(ctx context.Context, sourceID, owner, repo string, number int, body string) error {
	return f.fn(ctx, sourceID, owner, repo, number, body)
}

func TestGitHubCommentTool(t *testing.T) {
	fake := &fakeGitHubCommentClient{
		fn: func(_ context.Context, sourceID, owner, repo string, _ int, _ string) error {
			_, _ = owner, repo
			if sourceID == "gh-1" {
				return nil
			}
			return errors.New("unauthorized")
		},
	}
	tool := core.NewGitHubCommentTool(fake)

	t.Run("name and metadata", func(t *testing.T) {
		if tool.Name() != "github_comment" {
			t.Errorf("Name() = %q, want github_comment", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("Description() should not be empty")
		}
		params := tool.Parameters()
		if params.Type != "object" {
			t.Errorf("Parameters().Type = %q, want object", params.Type)
		}
		if _, ok := params.Properties["body"]; !ok {
			t.Error("Parameters() missing body")
		}
	})

	t.Run("missing required fields", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{"pr_number": float64(42)})
		if err == nil {
			t.Error("expected error for missing required fields")
		}
	})

	t.Run("missing pr_number", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "gh-1",
			"owner":     "acme",
			"repo":      "infra",
			"body":      "LGTM",
		})
		if err == nil {
			t.Error("expected error for missing pr_number")
		}
	})

	t.Run("success", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "gh-1",
			"owner":     "acme",
			"repo":      "infra",
			"pr_number": float64(42),
			"body":      "Looks good to me.",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		if m["posted"] != true {
			t.Errorf("posted = %v, want true", m["posted"])
		}
	})

	t.Run("client error", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "bad-source",
			"owner":     "acme",
			"repo":      "infra",
			"pr_number": float64(42),
			"body":      "comment",
		})
		if err == nil {
			t.Error("expected error from client")
		}
	})
}

// ---- GitHubRequestChangesTool ----

type fakeGitHubRequestChangesClient struct {
	fn func(ctx context.Context, sourceID, owner, repo string, number int, body string) error
}

func (f *fakeGitHubRequestChangesClient) GitHubRequestChanges(ctx context.Context, sourceID, owner, repo string, number int, body string) error {
	return f.fn(ctx, sourceID, owner, repo, number, body)
}

func TestGitHubRequestChangesTool(t *testing.T) {
	fake := &fakeGitHubRequestChangesClient{
		fn: func(_ context.Context, sourceID, owner, repo string, _ int, _ string) error {
			_, _ = owner, repo
			if sourceID == "gh-1" {
				return nil
			}
			return errors.New("forbidden")
		},
	}
	tool := core.NewGitHubRequestChangesTool(fake)

	t.Run("name and metadata", func(t *testing.T) {
		if tool.Name() != "github_request_changes" {
			t.Errorf("Name() = %q, want github_request_changes", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("Description() should not be empty")
		}
		params := tool.Parameters()
		if params.Type != "object" {
			t.Errorf("Parameters().Type = %q, want object", params.Type)
		}
	})

	t.Run("missing required fields", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{"pr_number": float64(42)})
		if err == nil {
			t.Error("expected error for missing required fields")
		}
	})

	t.Run("missing pr_number", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "gh-1",
			"owner":     "acme",
			"repo":      "infra",
			"body":      "Please fix security issue",
		})
		if err == nil {
			t.Error("expected error for missing pr_number")
		}
	})

	t.Run("success", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "gh-1",
			"owner":     "acme",
			"repo":      "infra",
			"pr_number": float64(42),
			"body":      "Security vulnerability found in auth handler.",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		if m["changes_requested"] != true {
			t.Errorf("changes_requested = %v, want true", m["changes_requested"])
		}
	})

	t.Run("client error", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "bad-source",
			"owner":     "acme",
			"repo":      "infra",
			"pr_number": float64(42),
			"body":      "issue found",
		})
		if err == nil {
			t.Error("expected error from client")
		}
	})
}

// ---- GitLabMRGetTool ----

type fakeGitLabMRGetClient struct {
	fn func(ctx context.Context, sourceID, projectID string, iid int) (*gitlabadapter.MRInfo, error)
}

func (f *fakeGitLabMRGetClient) GitLabGetMR(ctx context.Context, sourceID, projectID string, iid int) (*gitlabadapter.MRInfo, error) {
	return f.fn(ctx, sourceID, projectID, iid)
}

func TestGitLabMRGetTool(t *testing.T) {
	fake := &fakeGitLabMRGetClient{
		fn: func(_ context.Context, sourceID, projectID string, iid int) (*gitlabadapter.MRInfo, error) {
			if sourceID == "gl-1" && projectID == "42" && iid == 7 {
				return &gitlabadapter.MRInfo{IID: 7, Title: "Add feature", State: "opened"}, nil
			}
			return nil, errors.New("not found")
		},
	}
	tool := core.NewGitLabMRGetTool(fake)

	t.Run("name and metadata", func(t *testing.T) {
		if tool.Name() != "gitlab_mr_get" {
			t.Errorf("Name() = %q, want gitlab_mr_get", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("Description() should not be empty")
		}
		params := tool.Parameters()
		if params.Type != "object" {
			t.Errorf("Parameters().Type = %q, want object", params.Type)
		}
		if _, ok := params.Properties["project_id"]; !ok {
			t.Error("Parameters() missing project_id")
		}
	})

	t.Run("missing source_id/project_id", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{"mr_iid": float64(7)})
		if err == nil {
			t.Error("expected error for missing required fields")
		}
	})

	t.Run("missing mr_iid", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"source_id":  "gl-1",
			"project_id": "42",
		})
		if err == nil {
			t.Error("expected error for missing mr_iid")
		}
	})

	t.Run("zero mr_iid", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"source_id":  "gl-1",
			"project_id": "42",
			"mr_iid":     float64(0),
		})
		if err == nil {
			t.Error("expected error for zero mr_iid")
		}
	})

	t.Run("success", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"source_id":  "gl-1",
			"project_id": "42",
			"mr_iid":     float64(7),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		mr := res.(*gitlabadapter.MRInfo)
		if mr.IID != 7 {
			t.Errorf("IID = %v, want 7", mr.IID)
		}
	})

	t.Run("client error", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"source_id":  "gl-1",
			"project_id": "42",
			"mr_iid":     float64(99),
		})
		if err == nil {
			t.Error("expected error from client")
		}
	})
}

// ---- GitLabMRDiffTool ----

type fakeGitLabMRDiffClient struct {
	fn func(ctx context.Context, sourceID, projectID string, iid int) (string, error)
}

func (f *fakeGitLabMRDiffClient) GitLabGetMRDiff(ctx context.Context, sourceID, projectID string, iid int) (string, error) {
	return f.fn(ctx, sourceID, projectID, iid)
}

func TestGitLabMRDiffTool(t *testing.T) {
	fake := &fakeGitLabMRDiffClient{
		fn: func(_ context.Context, sourceID, projectID string, iid int) (string, error) {
			if sourceID == "gl-1" && iid == 7 {
				return "diff --git a/app.go b/app.go\n+new line", nil
			}
			return "", errors.New("not found")
		},
	}
	tool := core.NewGitLabMRDiffTool(fake)

	t.Run("name and metadata", func(t *testing.T) {
		if tool.Name() != "gitlab_mr_diff" {
			t.Errorf("Name() = %q, want gitlab_mr_diff", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("Description() should not be empty")
		}
		params := tool.Parameters()
		if params.Type != "object" {
			t.Errorf("Parameters().Type = %q, want object", params.Type)
		}
	})

	t.Run("missing required fields", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{"mr_iid": float64(7)})
		if err == nil {
			t.Error("expected error for missing required fields")
		}
	})

	t.Run("missing mr_iid", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"source_id":  "gl-1",
			"project_id": "42",
		})
		if err == nil {
			t.Error("expected error for missing mr_iid")
		}
	})

	t.Run("success", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"source_id":  "gl-1",
			"project_id": "42",
			"mr_iid":     float64(7),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		diff, _ := m["diff"].(string)
		if diff == "" {
			t.Error("expected non-empty diff")
		}
	})

	t.Run("client error", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"source_id":  "gl-1",
			"project_id": "42",
			"mr_iid":     float64(99),
		})
		if err == nil {
			t.Error("expected error from client")
		}
	})
}

// ---- GitLabCommentTool ----

type fakeGitLabCommentClient struct {
	fn func(ctx context.Context, sourceID, projectID string, iid int, body string) error
}

func (f *fakeGitLabCommentClient) GitLabPostNote(ctx context.Context, sourceID, projectID string, iid int, body string) error {
	return f.fn(ctx, sourceID, projectID, iid, body)
}

func TestGitLabCommentTool(t *testing.T) {
	fake := &fakeGitLabCommentClient{
		fn: func(_ context.Context, sourceID, projectID string, _ int, _ string) error {
			_ = projectID
			if sourceID == "gl-1" {
				return nil
			}
			return errors.New("unauthorized")
		},
	}
	tool := core.NewGitLabCommentTool(fake)

	t.Run("name and metadata", func(t *testing.T) {
		if tool.Name() != "gitlab_comment" {
			t.Errorf("Name() = %q, want gitlab_comment", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("Description() should not be empty")
		}
		params := tool.Parameters()
		if params.Type != "object" {
			t.Errorf("Parameters().Type = %q, want object", params.Type)
		}
		if _, ok := params.Properties["body"]; !ok {
			t.Error("Parameters() missing body")
		}
	})

	t.Run("missing required fields", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{"mr_iid": float64(7)})
		if err == nil {
			t.Error("expected error for missing required fields")
		}
	})

	t.Run("missing mr_iid", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"source_id":  "gl-1",
			"project_id": "42",
			"body":       "LGTM",
		})
		if err == nil {
			t.Error("expected error for missing mr_iid")
		}
	})

	t.Run("success", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"source_id":  "gl-1",
			"project_id": "42",
			"mr_iid":     float64(7),
			"body":       "Code looks good.",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		if m["posted"] != true {
			t.Errorf("posted = %v, want true", m["posted"])
		}
	})

	t.Run("client error", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"source_id":  "bad-source",
			"project_id": "42",
			"mr_iid":     float64(7),
			"body":       "comment",
		})
		if err == nil {
			t.Error("expected error from client")
		}
	})
}
