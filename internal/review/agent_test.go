package review

import (
	"context"
	"fmt"
	"strings"
	"testing"

	githubadapter "github.com/jaimegago/joe/internal/adapters/github"
	gitlabadapter "github.com/jaimegago/joe/internal/adapters/gitlab"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/knowledge"
	"github.com/jaimegago/joe/internal/llm"
)

// --- mock implementations ---

type mockGitHubOps struct {
	pr          *githubadapter.PRInfo
	diff        string
	prErr       error
	diffErr     error
	commentErr  error
	commentBody string
}

func (m *mockGitHubOps) GitHubGetPR(_ context.Context, _, _, _ string, _ int) (*githubadapter.PRInfo, error) {
	return m.pr, m.prErr
}

func (m *mockGitHubOps) GitHubGetPRDiff(_ context.Context, _, _, _ string, _ int) (string, error) {
	return m.diff, m.diffErr
}

func (m *mockGitHubOps) GitHubPostComment(_ context.Context, _, _, _ string, _ int, body string) error {
	m.commentBody = body
	return m.commentErr
}

type mockGitLabOps struct {
	mr       *gitlabadapter.MRInfo
	diff     string
	mrErr    error
	diffErr  error
	noteErr  error
	noteBody string
}

func (m *mockGitLabOps) GitLabGetMR(_ context.Context, _, _ string, _ int) (*gitlabadapter.MRInfo, error) {
	return m.mr, m.mrErr
}

func (m *mockGitLabOps) GitLabGetMRDiff(_ context.Context, _, _ string, _ int) (string, error) {
	return m.diff, m.diffErr
}

func (m *mockGitLabOps) GitLabPostNote(_ context.Context, _, _ string, _ int, body string) error {
	m.noteBody = body
	return m.noteErr
}

type mockKnowledgeSearcher struct {
	results []knowledge.SearchResult
	err     error
}

func (m *mockKnowledgeSearcher) Search(_ context.Context, _ knowledge.SearchRequest) ([]knowledge.SearchResult, error) {
	return m.results, m.err
}

type mockGraphQuerier struct {
	nodes []graph.Node
	err   error
}

func (m *mockGraphQuerier) Query(_ context.Context, _ string) ([]graph.Node, error) {
	return m.nodes, m.err
}

type mockLLMAdapter struct {
	resp *llm.ChatResponse
	err  error
	// lastPrompt captures the last user message for inspection.
	lastPrompt string
}

func (m *mockLLMAdapter) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	if len(req.Messages) > 0 {
		m.lastPrompt = req.Messages[len(req.Messages)-1].Content
	}
	return m.resp, m.err
}

func (m *mockLLMAdapter) ChatStream(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

func (m *mockLLMAdapter) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, nil
}

// newAgentWithMocks builds a ReviewAgent + Service backed by a fresh mockRepo.
func newAgentWithMocks(
	gh GitHubOps,
	gl GitLabOps,
	k KnowledgeSearcher,
	g GraphQuerier,
	l llm.LLMAdapter,
) (*ReviewAgent, *Service) {
	svc := NewService(newMockRepo())
	return NewReviewAgent(gh, gl, k, g, l, svc), svc
}

func okGH() *mockGitHubOps {
	return &mockGitHubOps{
		pr:   &githubadapter.PRInfo{Title: "Add feature", Author: "alice", URL: "https://github.com/org/repo/pull/1"},
		diff: "diff --git a/main.go b/main.go\n+// new line",
	}
}

func okLLM() *mockLLMAdapter {
	return &mockLLMAdapter{resp: &llm.ChatResponse{Content: "LGTM: looks good"}}
}

// --- Tests ---

func TestNewReviewAgent(t *testing.T) {
	svc := NewService(newMockRepo())
	agent := NewReviewAgent(&mockGitHubOps{}, &mockGitLabOps{}, nil, nil, okLLM(), svc)
	if agent == nil {
		t.Fatal("NewReviewAgent returned nil")
	}
	if agent.svc != svc {
		t.Error("svc not wired correctly")
	}
}

func TestReviewAgent_Run_GitHub_Success(t *testing.T) {
	gh := okGH()
	llmAdapter := okLLM()
	agent, svc := newAgentWithMocks(gh, nil, nil, nil, llmAdapter)

	job := makeJob(PlatformGitHub, "org", "repo", 1, "abc1")
	created, _ := svc.Enqueue(context.Background(), job)

	if err := agent.Run(context.Background(), created); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if gh.commentBody == "" {
		t.Error("expected PostComment to be called")
	}
	if !strings.Contains(gh.commentBody, "LGTM") {
		t.Errorf("comment = %q, want 'LGTM'", gh.commentBody)
	}
}

func TestReviewAgent_Run_GitLab_Success(t *testing.T) {
	gl := &mockGitLabOps{
		mr:   &gitlabadapter.MRInfo{Title: "Fix bug", Author: "bob", WebURL: "https://gitlab.com/-/5"},
		diff: "diff --git a/fix.go b/fix.go",
	}
	llmAdapter := &mockLLMAdapter{resp: &llm.ChatResponse{Content: "Changes requested"}}
	agent, svc := newAgentWithMocks(nil, gl, nil, nil, llmAdapter)

	job := makeJob(PlatformGitLab, "123", "proj", 5, "dead1")
	created, _ := svc.Enqueue(context.Background(), job)

	if err := agent.Run(context.Background(), created); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if gl.noteBody == "" {
		t.Error("expected PostNote to be called")
	}
}

func TestReviewAgent_Run_AlreadyClaimed(t *testing.T) {
	repo := newMockRepo()
	svc := NewService(repo)
	agent := NewReviewAgent(okGH(), nil, nil, nil, okLLM(), svc)

	job := makeJob(PlatformGitHub, "org", "repo", 2, "abc2")
	created, _ := svc.Enqueue(context.Background(), job)

	// Pre-claim so MarkRunning returns ErrAlreadyClaimed.
	_, _ = repo.ClaimJob(context.Background(), created.ID, created.CreatedAt)

	if err := agent.Run(context.Background(), created); err != nil {
		t.Fatalf("Run() on already-claimed job should return nil, got %v", err)
	}
}

func TestReviewAgent_Run_FetchPRError(t *testing.T) {
	gh := &mockGitHubOps{prErr: fmt.Errorf("github: not found")}
	agent, svc := newAgentWithMocks(gh, nil, nil, nil, okLLM())

	job := makeJob(PlatformGitHub, "org", "repo", 3, "abc3")
	created, _ := svc.Enqueue(context.Background(), job)

	if err := agent.Run(context.Background(), created); err == nil {
		t.Fatal("expected error when PR fetch fails")
	}
}

func TestReviewAgent_Run_FetchDiffError(t *testing.T) {
	gh := &mockGitHubOps{
		pr:      &githubadapter.PRInfo{Title: "T", Author: "a", URL: "u"},
		diffErr: fmt.Errorf("diff unavailable"),
	}
	agent, svc := newAgentWithMocks(gh, nil, nil, nil, okLLM())
	job := makeJob(PlatformGitHub, "org", "repo", 4, "abc4")
	created, _ := svc.Enqueue(context.Background(), job)
	if err := agent.Run(context.Background(), created); err == nil {
		t.Fatal("expected error when diff fetch fails")
	}
}

func TestReviewAgent_Run_LLMError(t *testing.T) {
	l := &mockLLMAdapter{err: fmt.Errorf("llm: unavailable")}
	agent, svc := newAgentWithMocks(okGH(), nil, nil, nil, l)
	job := makeJob(PlatformGitHub, "org", "repo", 5, "abc5")
	created, _ := svc.Enqueue(context.Background(), job)
	if err := agent.Run(context.Background(), created); err == nil {
		t.Fatal("expected error when LLM fails")
	}
}

func TestReviewAgent_Run_EmptyLLMResponse(t *testing.T) {
	l := &mockLLMAdapter{resp: &llm.ChatResponse{Content: ""}}
	agent, svc := newAgentWithMocks(okGH(), nil, nil, nil, l)
	job := makeJob(PlatformGitHub, "org", "repo", 6, "abc6")
	created, _ := svc.Enqueue(context.Background(), job)
	if err := agent.Run(context.Background(), created); err == nil {
		t.Fatal("expected error for empty LLM response")
	}
}

func TestReviewAgent_Run_PostCommentError(t *testing.T) {
	gh := &mockGitHubOps{
		pr:         &githubadapter.PRInfo{Title: "T", Author: "a", URL: "u"},
		diff:       "diff",
		commentErr: fmt.Errorf("rate limited"),
	}
	agent, svc := newAgentWithMocks(gh, nil, nil, nil, okLLM())
	job := makeJob(PlatformGitHub, "org", "repo", 7, "abc7")
	created, _ := svc.Enqueue(context.Background(), job)
	if err := agent.Run(context.Background(), created); err == nil {
		t.Fatal("expected error when PostComment fails")
	}
}

func TestReviewAgent_Run_GitLabDiffError(t *testing.T) {
	gl := &mockGitLabOps{
		mr:      &gitlabadapter.MRInfo{Title: "T", Author: "a", WebURL: "u"},
		diffErr: fmt.Errorf("diff unavailable"),
	}
	agent, svc := newAgentWithMocks(nil, gl, nil, nil, okLLM())
	job := makeJob(PlatformGitLab, "123", "proj", 8, "abc8")
	created, _ := svc.Enqueue(context.Background(), job)
	if err := agent.Run(context.Background(), created); err == nil {
		t.Fatal("expected error when MR diff fetch fails")
	}
}

func TestReviewAgent_Run_GitLabPostNoteError(t *testing.T) {
	gl := &mockGitLabOps{
		mr:      &gitlabadapter.MRInfo{Title: "T", Author: "a", WebURL: "u"},
		diff:    "diff",
		noteErr: fmt.Errorf("gitlab API error"),
	}
	agent, svc := newAgentWithMocks(nil, gl, nil, nil, okLLM())
	job := makeJob(PlatformGitLab, "123", "proj", 9, "abc9")
	created, _ := svc.Enqueue(context.Background(), job)
	if err := agent.Run(context.Background(), created); err == nil {
		t.Fatal("expected error when PostNote fails")
	}
}

func TestReviewAgent_FetchPRContext_GitHubNilOps(t *testing.T) {
	agent, svc := newAgentWithMocks(nil, nil, nil, nil, okLLM())
	job := makeJob(PlatformGitHub, "org", "repo", 10, "abc10")
	created, _ := svc.Enqueue(context.Background(), job)
	if err := agent.Run(context.Background(), created); err == nil {
		t.Fatal("expected error when GitHub ops is nil")
	}
}

func TestReviewAgent_FetchPRContext_GitLabNilOps(t *testing.T) {
	agent, svc := newAgentWithMocks(nil, nil, nil, nil, okLLM())
	job := makeJob(PlatformGitLab, "123", "proj", 11, "abc11")
	created, _ := svc.Enqueue(context.Background(), job)
	if err := agent.Run(context.Background(), created); err == nil {
		t.Fatal("expected error when GitLab ops is nil")
	}
}

func TestReviewAgent_UnsupportedPlatform(t *testing.T) {
	agent, svc := newAgentWithMocks(nil, nil, nil, nil, okLLM())
	job := &ReviewJob{
		Platform: Platform("unknown"),
		Owner:    "org",
		Repo:     "repo",
		PRNumber: 1,
		HeadSHA:  "sha",
	}
	created, _ := svc.Enqueue(context.Background(), job)
	if err := agent.Run(context.Background(), created); err == nil {
		t.Fatal("expected error for unsupported platform")
	}
}

func TestReviewAgent_Run_KnowledgeContext(t *testing.T) {
	gh := okGH()
	k := &mockKnowledgeSearcher{
		results: []knowledge.SearchResult{
			{Entry: knowledge.Entry{Title: "Deploy Runbook", Content: "How to deploy"}},
		},
	}
	l := okLLM()
	agent, svc := newAgentWithMocks(gh, nil, k, nil, l)
	job := makeJob(PlatformGitHub, "org", "repo", 12, "abc12")
	created, _ := svc.Enqueue(context.Background(), job)
	_ = agent.Run(context.Background(), created)

	if !strings.Contains(l.lastPrompt, "Deploy Runbook") {
		t.Errorf("prompt = %q, want to contain knowledge entry title", l.lastPrompt)
	}
}

func TestReviewAgent_Run_GraphContext(t *testing.T) {
	gh := okGH()
	g := &mockGraphQuerier{
		nodes: []graph.Node{{ID: "payment-svc", Type: "deployment"}},
	}
	l := okLLM()
	agent, svc := newAgentWithMocks(gh, nil, nil, g, l)
	job := makeJob(PlatformGitHub, "org", "repo", 13, "abc13")
	created, _ := svc.Enqueue(context.Background(), job)
	_ = agent.Run(context.Background(), created)

	if !strings.Contains(l.lastPrompt, "payment-svc") {
		t.Errorf("prompt = %q, want to contain graph node ID", l.lastPrompt)
	}
}

func TestReviewAgent_FetchKnowledge_ErrorDegradesGracefully(t *testing.T) {
	k := &mockKnowledgeSearcher{err: fmt.Errorf("knowledge store down")}
	agent, svc := newAgentWithMocks(okGH(), nil, k, nil, okLLM())
	job := makeJob(PlatformGitHub, "org", "repo", 14, "abc14")
	created, _ := svc.Enqueue(context.Background(), job)
	if err := agent.Run(context.Background(), created); err != nil {
		t.Fatalf("Run() should degrade gracefully on knowledge error, got %v", err)
	}
}

func TestReviewAgent_FetchGraph_ErrorDegradesGracefully(t *testing.T) {
	g := &mockGraphQuerier{err: fmt.Errorf("graph store down")}
	agent, svc := newAgentWithMocks(okGH(), nil, nil, g, okLLM())
	job := makeJob(PlatformGitHub, "org", "repo", 15, "abc15")
	created, _ := svc.Enqueue(context.Background(), job)
	if err := agent.Run(context.Background(), created); err != nil {
		t.Fatalf("Run() should degrade gracefully on graph error, got %v", err)
	}
}

func TestReviewAgent_FetchKnowledge_NilService(t *testing.T) {
	agent, _ := newAgentWithMocks(nil, nil, nil, nil, okLLM())
	result := agent.fetchKnowledge(context.Background(), "query")
	if result != "" {
		t.Errorf("fetchKnowledge with nil service should return empty string, got %q", result)
	}
}

func TestReviewAgent_FetchGraph_NilQuerier(t *testing.T) {
	agent, _ := newAgentWithMocks(nil, nil, nil, nil, okLLM())
	result := agent.fetchGraph(context.Background(), "query")
	if result != "" {
		t.Errorf("fetchGraph with nil querier should return empty string, got %q", result)
	}
}

func TestReviewAgent_FetchGraph_ManyNodes(t *testing.T) {
	nodes := make([]graph.Node, maxGraphNodes+3)
	for i := range nodes {
		nodes[i] = graph.Node{ID: fmt.Sprintf("svc-%d", i), Type: "service"}
	}
	g := &mockGraphQuerier{nodes: nodes}
	agent, _ := newAgentWithMocks(nil, nil, nil, g, okLLM())
	result := agent.fetchGraph(context.Background(), "query")
	count := strings.Count(result, "\n")
	if count > maxGraphNodes {
		t.Errorf("fetchGraph returned %d lines, want at most %d", count, maxGraphNodes)
	}
}

func TestReviewAgent_DiffTruncation(t *testing.T) {
	gh := &mockGitHubOps{
		pr:   &githubadapter.PRInfo{Title: "T", Author: "a", URL: "u"},
		diff: strings.Repeat("a", maxDiffBytes+100),
	}
	l := okLLM()
	agent, svc := newAgentWithMocks(gh, nil, nil, nil, l)
	job := makeJob(PlatformGitHub, "org", "repo", 16, "abc16")
	created, _ := svc.Enqueue(context.Background(), job)
	_ = agent.Run(context.Background(), created)
	if !strings.Contains(l.lastPrompt, "[diff truncated]") {
		t.Error("expected '[diff truncated]' marker in prompt")
	}
}

func TestBuildReviewPrompt_GitHub(t *testing.T) {
	job := &ReviewJob{Platform: PlatformGitHub, Owner: "org", Repo: "repo", PRNumber: 1}
	prompt := buildReviewPrompt(job, "My PR", "alice", "https://example.com", "some diff", "", "")
	if !strings.Contains(prompt, "pull request") {
		t.Error("expected 'pull request' in prompt for GitHub")
	}
	if !strings.Contains(prompt, "My PR") {
		t.Error("expected PR title in prompt")
	}
	if !strings.Contains(prompt, "alice") {
		t.Error("expected author in prompt")
	}
}

func TestBuildReviewPrompt_GitLab(t *testing.T) {
	job := &ReviewJob{Platform: PlatformGitLab, Owner: "grp", Repo: "proj", PRNumber: 5}
	prompt := buildReviewPrompt(job, "My MR", "bob", "https://gitlab.com/mr/5", "diff", "", "")
	if !strings.Contains(prompt, "merge request") {
		t.Error("expected 'merge request' in prompt for GitLab")
	}
}

func TestBuildReviewPrompt_WithKnowledgeAndGraph(t *testing.T) {
	job := &ReviewJob{Platform: PlatformGitHub, Owner: "org", Repo: "repo", PRNumber: 1}
	prompt := buildReviewPrompt(job, "T", "a", "u", "diff",
		"### Runbook\nDeploy steps\n\n",
		"- payment-svc (deployment)\n",
	)
	if !strings.Contains(prompt, "Relevant Knowledge") {
		t.Error("expected knowledge section in prompt")
	}
	if !strings.Contains(prompt, "Related Infrastructure Nodes") {
		t.Error("expected graph section in prompt")
	}
}

func TestPRMRLabel(t *testing.T) {
	if prMRLabel(PlatformGitHub) != "pull request" {
		t.Errorf("prMRLabel(GitHub) = %q, want 'pull request'", prMRLabel(PlatformGitHub))
	}
	if prMRLabel(PlatformGitLab) != "merge request" {
		t.Errorf("prMRLabel(GitLab) = %q, want 'merge request'", prMRLabel(PlatformGitLab))
	}
	other := prMRLabel(Platform("other"))
	if other != "pull request" {
		t.Errorf("prMRLabel(other) = %q, want 'pull request' (default)", other)
	}
}
