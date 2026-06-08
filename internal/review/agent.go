package review

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	githubadapter "github.com/jaimegago/joe/internal/adapters/github"
	gitlabadapter "github.com/jaimegago/joe/internal/adapters/gitlab"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/knowledge"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/prompts"
)

const (
	maxDiffBytes      = 40_000 // truncate very large diffs to keep token counts reasonable
	maxKnowledgeItems = 5
	maxGraphNodes     = 10
	reviewMaxTokens   = 4096
)

// GitHubOps is the set of GitHub operations required by the Review Agent.
type GitHubOps interface {
	GitHubGetPR(ctx context.Context, sourceID, owner, repo string, number int) (*githubadapter.PRInfo, error)
	GitHubGetPRDiff(ctx context.Context, sourceID, owner, repo string, number int) (string, error)
	GitHubPostComment(ctx context.Context, sourceID, owner, repo string, number int, body string) error
}

// GitLabOps is the set of GitLab operations required by the Review Agent.
type GitLabOps interface {
	GitLabGetMR(ctx context.Context, sourceID, projectID string, iid int) (*gitlabadapter.MRInfo, error)
	GitLabGetMRDiff(ctx context.Context, sourceID, projectID string, iid int) (string, error)
	GitLabPostNote(ctx context.Context, sourceID, projectID string, iid int, body string) error
}

// KnowledgeSearcher is a minimal interface for knowledge store search.
type KnowledgeSearcher interface {
	Search(ctx context.Context, req knowledge.SearchRequest) ([]knowledge.SearchResult, error)
}

// GraphQuerier is a minimal interface for graph queries.
type GraphQuerier interface {
	Query(ctx context.Context, q string) ([]graph.Node, error)
}

// ReviewAgent runs a code review on a ReviewJob.
// It fetches the diff, enriches context from the graph and knowledge store,
// calls the LLM, and posts the resulting review comment.
type ReviewAgent struct {
	github    GitHubOps
	gitlab    GitLabOps
	knowledge KnowledgeSearcher
	graph     GraphQuerier
	llm       llm.LLMAdapter
	svc       *Service
	logger    *slog.Logger
}

// NewReviewAgent creates a new ReviewAgent.
// knowledge and graph are optional (may be nil); the agent degrades gracefully.
func NewReviewAgent(
	github GitHubOps,
	gitlab GitLabOps,
	knowledge KnowledgeSearcher,
	graphQ GraphQuerier,
	llmAdapter llm.LLMAdapter,
	svc *Service,
) *ReviewAgent {
	return &ReviewAgent{
		github:    github,
		gitlab:    gitlab,
		knowledge: knowledge,
		graph:     graphQ,
		llm:       llmAdapter,
		svc:       svc,
		logger:    slog.With("component", "review-agent"),
	}
}

// Run processes a single review job synchronously.
// It transitions the job through pending → running → done/failed.
func (a *ReviewAgent) Run(ctx context.Context, job *ReviewJob) error {
	log := a.logger.With("job_id", job.ID, "platform", job.Platform,
		"owner", job.Owner, "repo", job.Repo, "pr", job.PRNumber)

	if err := a.svc.MarkRunning(ctx, job.ID); err != nil {
		if errors.Is(err, ErrAlreadyClaimed) {
			log.Info("review job already claimed by another instance, skipping")
			return nil
		}
		return fmt.Errorf("mark running: %w", err)
	}
	log.Info("review job started")

	review, err := a.runReview(ctx, job)
	if err != nil {
		log.Error("review job failed", "error", err)
		_ = a.svc.MarkFailed(ctx, job.ID, err.Error())
		return err
	}

	if err := a.svc.MarkDone(ctx, job.ID, review); err != nil {
		return fmt.Errorf("mark done: %w", err)
	}
	log.Info("review job done", "review_len", len(review))
	return nil
}

// runReview fetches context, calls the LLM, and posts the review.
func (a *ReviewAgent) runReview(ctx context.Context, job *ReviewJob) (string, error) {
	diff, title, author, url, err := a.fetchPRContext(ctx, job)
	if err != nil {
		return "", fmt.Errorf("fetch PR context: %w", err)
	}

	// Truncate extremely large diffs so we don't blow the LLM context window.
	if len(diff) > maxDiffBytes {
		diff = diff[:maxDiffBytes] + "\n\n[diff truncated]"
	}

	knowledgeCtx := a.fetchKnowledge(ctx, fmt.Sprintf("%s/%s %s", job.Owner, job.Repo, title))
	graphCtx := a.fetchGraph(ctx, fmt.Sprintf("%s/%s", job.Owner, job.Repo))

	prompt := buildReviewPrompt(job, title, author, url, diff, knowledgeCtx, graphCtx)

	a.logger.Debug("calling LLM for review", "job_id", job.ID, "diff_len", len(diff))
	resp, err := a.llm.Chat(ctx, llm.ChatRequest{
		SystemPrompt: prompts.ReviewSystem,
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
		MaxTokens: reviewMaxTokens,
	})
	if err != nil {
		return "", fmt.Errorf("llm chat: %w", err)
	}

	review := resp.Content
	if review == "" {
		return "", fmt.Errorf("LLM returned empty review")
	}

	// Post the review as a comment on the PR/MR.
	if err := a.postReview(ctx, job, review); err != nil {
		return "", fmt.Errorf("post review: %w", err)
	}

	return review, nil
}

// fetchPRContext retrieves the diff and metadata for a GitHub PR or GitLab MR.
func (a *ReviewAgent) fetchPRContext(ctx context.Context, job *ReviewJob) (diff, title, author, url string, err error) {
	switch job.Platform {
	case PlatformGitHub:
		if a.github == nil {
			return "", "", "", "", fmt.Errorf("GitHub ops not configured")
		}
		pr, prErr := a.github.GitHubGetPR(ctx, job.ComponentID, job.Owner, job.Repo, job.PRNumber)
		if prErr != nil {
			return "", "", "", "", prErr
		}
		d, dErr := a.github.GitHubGetPRDiff(ctx, job.ComponentID, job.Owner, job.Repo, job.PRNumber)
		if dErr != nil {
			return "", "", "", "", dErr
		}
		return d, pr.Title, pr.Author, pr.URL, nil

	case PlatformGitLab:
		if a.gitlab == nil {
			return "", "", "", "", fmt.Errorf("GitLab ops not configured")
		}
		mr, mrErr := a.gitlab.GitLabGetMR(ctx, job.ComponentID, job.Owner, job.PRNumber)
		if mrErr != nil {
			return "", "", "", "", mrErr
		}
		d, dErr := a.gitlab.GitLabGetMRDiff(ctx, job.ComponentID, job.Owner, job.PRNumber)
		if dErr != nil {
			return "", "", "", "", dErr
		}
		return d, mr.Title, mr.Author, mr.WebURL, nil

	default:
		return "", "", "", "", fmt.Errorf("unsupported platform: %s", job.Platform)
	}
}

// postReview posts the review as a comment on the PR/MR.
func (a *ReviewAgent) postReview(ctx context.Context, job *ReviewJob, review string) error {
	switch job.Platform {
	case PlatformGitHub:
		return a.github.GitHubPostComment(ctx, job.ComponentID, job.Owner, job.Repo, job.PRNumber, review)
	case PlatformGitLab:
		return a.gitlab.GitLabPostNote(ctx, job.ComponentID, job.Owner, job.PRNumber, review)
	default:
		return fmt.Errorf("unsupported platform: %s", job.Platform)
	}
}

// fetchKnowledge queries the knowledge store for entries relevant to the repo/PR.
func (a *ReviewAgent) fetchKnowledge(ctx context.Context, query string) string {
	if a.knowledge == nil {
		return ""
	}
	results, err := a.knowledge.Search(ctx, knowledge.SearchRequest{
		Query: query,
		TopK:  maxKnowledgeItems,
	})
	if err != nil {
		a.logger.Warn("knowledge search failed", "error", err)
		return ""
	}
	if len(results) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, r := range results {
		fmt.Fprintf(&sb, "### %s\n%s\n\n", r.Entry.Title, r.Entry.Content)
	}
	return sb.String()
}

// fetchGraph queries the graph for nodes related to the repository.
func (a *ReviewAgent) fetchGraph(ctx context.Context, repoRef string) string {
	if a.graph == nil {
		return ""
	}
	nodes, err := a.graph.Query(ctx, repoRef)
	if err != nil {
		a.logger.Warn("graph query failed", "error", err)
		return ""
	}
	if len(nodes) == 0 {
		return ""
	}
	if len(nodes) > maxGraphNodes {
		nodes = nodes[:maxGraphNodes]
	}
	var sb strings.Builder
	for _, n := range nodes {
		fmt.Fprintf(&sb, "- %s (%s)\n", n.ID, n.Type)
	}
	return sb.String()
}

// buildReviewPrompt constructs the user message for the LLM review call.
func buildReviewPrompt(job *ReviewJob, title, author, url, diff, knowledgeCtx, graphCtx string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Please review this %s:\n\n", prMRLabel(job.Platform))
	fmt.Fprintf(&sb, "**Title:** %s\n", title)
	fmt.Fprintf(&sb, "**Author:** %s\n", author)
	fmt.Fprintf(&sb, "**URL:** %s\n", url)
	fmt.Fprintf(&sb, "**Repository:** %s/%s\n\n", job.Owner, job.Repo)

	if knowledgeCtx != "" {
		sb.WriteString("## Relevant Knowledge\n\n")
		sb.WriteString(knowledgeCtx)
	}

	if graphCtx != "" {
		sb.WriteString("## Related Infrastructure Nodes\n\n")
		sb.WriteString(graphCtx)
		sb.WriteString("\n")
	}

	sb.WriteString("## Diff\n\n```diff\n")
	sb.WriteString(diff)
	sb.WriteString("\n```\n")

	return sb.String()
}

func prMRLabel(p Platform) string {
	if p == PlatformGitLab {
		return "merge request"
	}
	return "pull request"
}
