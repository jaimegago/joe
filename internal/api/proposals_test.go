package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/knowledge"
	"github.com/jaimegago/joe/internal/knowledge/drafts"
	"github.com/jaimegago/joe/internal/knowledge/proposals"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/test/mocks"
	_ "modernc.org/sqlite"
)

// mockEmbedder wraps MockLLM to satisfy knowledge.Embedder (adds ModelName).
type mockEmbedder struct{ llm *mocks.MockLLM }

func (m *mockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return m.llm.Embed(ctx, text)
}
func (m *mockEmbedder) ModelName() string { return "mock-model" }

func setupProposalsTestServer(t *testing.T) (*http.ServeMux, *proposals.Service, *knowledge.Service) {
	t.Helper()

	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })

	knowledgeSvc := knowledge.NewService(sqlStore.Knowledge, nil)
	proposalRepo := proposals.NewRepository(sqlStore.DB(), sqlStore.Driver())
	proposalSvc := proposals.NewService(proposalRepo)

	services := &core.Services{
		Config:    &config.Config{},
		Store:     sqlStore,
		Adapters:  adapters.NewRegistry(),
		Knowledge: knowledgeSvc,
		Proposals: proposalSvc,
		// DocDrafter intentionally nil; handleCreateProposal returns 503 when nil.
	}

	server := New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	return mux, proposalSvc, knowledgeSvc
}

// setupProposalsWithPublisherTestServer wires a non-nil DocDrafter so that
// publish handlers pass the nil-check and proceed to the dispatch logic.
func setupProposalsWithPublisherTestServer(t *testing.T) (*http.ServeMux, *proposals.Service, *knowledge.Service) {
	t.Helper()

	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })

	mockLLM := mocks.NewMockLLM()
	mockLLM.DefaultResponse = &llm.ChatResponse{
		Content: `{"title":"Test Draft","content":"Generated content."}`,
	}
	knowledgeSvc := knowledge.NewService(sqlStore.Knowledge, &mockEmbedder{llm: mockLLM})
	proposalRepo := proposals.NewRepository(sqlStore.DB(), sqlStore.Driver())
	proposalSvc := proposals.NewService(proposalRepo)
	docDrafter := drafts.New(knowledgeSvc, proposalSvc, mockLLM)

	services := &core.Services{
		Config:     &config.Config{},
		Store:      sqlStore,
		Adapters:   adapters.NewRegistry(),
		Knowledge:  knowledgeSvc,
		Proposals:  proposalSvc,
		DocDrafter: docDrafter,
	}

	server := New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	return mux, proposalSvc, knowledgeSvc
}

func seedProposal(t *testing.T, svc *proposals.Service, p *proposals.Proposal) {
	t.Helper()
	if err := svc.Create(context.Background(), p); err != nil {
		t.Fatalf("seed proposal: %v", err)
	}
}

// TestHandleCreateProposal_NoDocDrafter verifies 503 when DocDrafter is not wired.
func TestHandleCreateProposal_NoDocDrafter(t *testing.T) {
	mux, _, _ := setupProposalsTestServer(t)

	w := doRequest(mux, http.MethodPost, apiPrefix+"/knowledge/proposals", map[string]any{
		"topic":       "deployment runbook",
		"target_type": "confluence",
		"target_id":   "page-1",
	})
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

// TestHandleListProposals_Empty verifies empty list returns OK with empty array.
func TestHandleListProposals_Empty(t *testing.T) {
	mux, _, _ := setupProposalsTestServer(t)

	w := doRequest(mux, http.MethodGet, apiPrefix+"/knowledge/proposals", nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	count := resp["count"].(float64)
	if count != 0 {
		t.Errorf("count = %.0f, want 0", count)
	}
}

// TestHandleListProposals_WithFilter verifies status and target_type filters.
func TestHandleListProposals_WithFilter(t *testing.T) {
	mux, proposalSvc, _ := setupProposalsTestServer(t)

	seedProposal(t, proposalSvc, &proposals.Proposal{
		ID: "p1", Title: "Doc A", TargetType: proposals.TargetConfluence, TargetID: "page-1",
		ProposedContent: "content",
	})
	seedProposal(t, proposalSvc, &proposals.Proposal{
		ID: "p2", Title: "Doc B", TargetType: proposals.TargetNotion, TargetID: "db-1",
		ProposedContent: "content",
	})

	// Filter by target_type=notion
	w := doRequest(mux, http.MethodGet, apiPrefix+"/knowledge/proposals?target_type=notion", nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	count := resp["count"].(float64)
	if count != 1 {
		t.Errorf("count = %.0f, want 1 (only notion proposals)", count)
	}
}

// TestHandleGetProposal verifies retrieving an existing proposal.
func TestHandleGetProposal(t *testing.T) {
	mux, proposalSvc, _ := setupProposalsTestServer(t)

	seedProposal(t, proposalSvc, &proposals.Proposal{
		ID: "get-1", Title: "Get Me", TargetType: proposals.TargetGit, TargetID: "docs/file.md",
		ProposedContent: "content",
	})

	w := doRequest(mux, http.MethodGet, apiPrefix+"/knowledge/proposals/get-1", nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp proposals.Proposal
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.ID != "get-1" {
		t.Errorf("ID = %q, want %q", resp.ID, "get-1")
	}
}

// TestHandleGetProposal_NotFound verifies 404 for nonexistent proposal.
func TestHandleGetProposal_NotFound(t *testing.T) {
	mux, _, _ := setupProposalsTestServer(t)

	w := doRequest(mux, http.MethodGet, apiPrefix+"/knowledge/proposals/does-not-exist", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestHandleApproveProposal verifies approving a pending proposal.
func TestHandleApproveProposal(t *testing.T) {
	mux, proposalSvc, _ := setupProposalsTestServer(t)

	seedProposal(t, proposalSvc, &proposals.Proposal{
		ID: "approve-1", Title: "Approve Me", TargetType: proposals.TargetConfluence, TargetID: "page-1",
		ProposedContent: "content",
	})

	w := doRequest(mux, http.MethodPost, apiPrefix+"/knowledge/proposals/approve-1/approve", nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// Verify it's now approved.
	p, _ := proposalSvc.Get(context.Background(), "approve-1")
	if p.Status != proposals.StatusApproved {
		t.Errorf("Status = %q, want %q", p.Status, proposals.StatusApproved)
	}
}

// TestHandleApproveProposal_AlreadyApproved verifies 422 when approving an already-approved proposal.
func TestHandleApproveProposal_AlreadyApproved(t *testing.T) {
	mux, proposalSvc, _ := setupProposalsTestServer(t)

	seedProposal(t, proposalSvc, &proposals.Proposal{
		ID: "approve-2", Title: "Already Approved", TargetType: proposals.TargetConfluence, TargetID: "page-2",
		ProposedContent: "content",
	})
	if err := proposalSvc.Approve(context.Background(), "approve-2"); err != nil {
		t.Fatalf("initial approve: %v", err)
	}

	// Second approve should fail.
	w := doRequest(mux, http.MethodPost, apiPrefix+"/knowledge/proposals/approve-2/approve", nil)
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

// TestHandleRejectProposal verifies rejecting a pending proposal with a reason.
func TestHandleRejectProposal(t *testing.T) {
	mux, proposalSvc, _ := setupProposalsTestServer(t)

	seedProposal(t, proposalSvc, &proposals.Proposal{
		ID: "reject-1", Title: "Reject Me", TargetType: proposals.TargetNotion, TargetID: "db-1",
		ProposedContent: "content",
	})

	w := doRequest(mux, http.MethodPost, apiPrefix+"/knowledge/proposals/reject-1/reject", map[string]any{
		"reason": "out of scope",
	})
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	p, _ := proposalSvc.Get(context.Background(), "reject-1")
	if p.Status != proposals.StatusRejected {
		t.Errorf("Status = %q, want %q", p.Status, proposals.StatusRejected)
	}
	if p.RejectedReason != "out of scope" {
		t.Errorf("RejectedReason = %q, want %q", p.RejectedReason, "out of scope")
	}
}

// TestHandleCreateProposal_InvalidJSON verifies 400 for malformed JSON body.
func TestHandleCreateProposal_InvalidJSON(t *testing.T) {
	mux, _, _ := setupProposalsWithPublisherTestServer(t)

	req := httptest.NewRequest(http.MethodPost, apiPrefix+"/knowledge/proposals", strings.NewReader("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestHandleCreateProposal_MissingFields verifies 400 when required fields are absent.
func TestHandleCreateProposal_MissingFields(t *testing.T) {
	mux, _, _ := setupProposalsWithPublisherTestServer(t)

	w := doRequest(mux, http.MethodPost, apiPrefix+"/knowledge/proposals", map[string]any{
		"topic": "only topic, no target",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestHandleCreateProposal_Success verifies 201 when DocDrafter generates a draft.
func TestHandleCreateProposal_Success(t *testing.T) {
	mux, _, _ := setupProposalsWithPublisherTestServer(t)

	w := doRequest(mux, http.MethodPost, apiPrefix+"/knowledge/proposals", map[string]any{
		"topic":       "deployment runbook",
		"target_type": "confluence",
		"target_id":   "page-123",
	})
	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusCreated, w.Body.String())
	}
}

// TestProposalHandlers_ServiceUnavailable covers the nil-service guard in all proposal endpoints.
func TestProposalHandlers_ServiceUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	New(&core.Services{
		Config:    &config.Config{},
		Adapters:  adapters.NewRegistry(),
		Proposals: nil,
	}).RegisterRoutes(mux)

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"list proposals", http.MethodGet, apiPrefix + "/knowledge/proposals"},
		{"get proposal", http.MethodGet, apiPrefix + "/knowledge/proposals/x"},
		{"approve proposal", http.MethodPost, apiPrefix + "/knowledge/proposals/x/approve"},
		{"reject proposal", http.MethodPost, apiPrefix + "/knowledge/proposals/x/reject"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := doRequest(mux, tc.method, tc.path, nil)
			if w.Code != http.StatusServiceUnavailable {
				t.Errorf("%s: status = %d, want 503", tc.name, w.Code)
			}
		})
	}
}

// TestHandleRejectProposal_NotFound covers the Reject-error path.
func TestHandleRejectProposal_NotFound(t *testing.T) {
	mux, _, _ := setupProposalsTestServer(t)
	w := doRequest(mux, http.MethodPost, apiPrefix+"/knowledge/proposals/does-not-exist/reject",
		map[string]string{"reason": "bad proposal"})
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422 (not found)", w.Code)
	}
}

// --- publishProposalToTarget dispatch coverage ---
//
// The HTTP publish entry point (POST .../proposals/{id}/publish) was removed
// with the vestigial direct-HTTP managed-system surface. The live publish path
// is inProcessCoreClient.PublishProposal → publishProposalToTarget, so these
// tests drive publishProposalToTarget directly (the shared dispatch helper that
// the in-process tool path uses).

// setupPublishDispatch builds the core services needed to exercise the
// publishProposalToTarget dispatch helper and its publishTo{Confluence,Notion,
// Git} branches without an HTTP server.
func setupPublishDispatch(t *testing.T) (*core.Services, *knowledge.Service) {
	t.Helper()

	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })

	knowledgeSvc := knowledge.NewService(sqlStore.Knowledge, nil)
	services := &core.Services{
		Config:    &config.Config{},
		Store:     sqlStore,
		Adapters:  adapters.NewRegistry(),
		Knowledge: knowledgeSvc,
	}
	return services, knowledgeSvc
}

// TestPublishToConfluence_WrongSourceType seeds a non-confluence source so the
// loop runs but skips it, covering the "type != confluence → continue" path.
func TestPublishToConfluence_WrongSourceType(t *testing.T) {
	services, knowledgeSvc := setupPublishDispatch(t)

	if err := knowledgeSvc.CreateSource(context.Background(), &knowledge.KnowledgeSource{
		ID:     "git-src-wrong-type",
		Type:   "git",
		Name:   "My Git Repo",
		Config: json.RawMessage(`{"url":"https://github.com/example/repo"}`),
	}); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}

	err := publishProposalToTarget(context.Background(), services, &proposals.Proposal{
		ID: "conf-wrong-type", Title: "Confluence Doc", TargetType: proposals.TargetConfluence,
		TargetID: "page-1", ProposedContent: "content",
	})
	if err == nil {
		t.Error("expected error (no confluence source configured), got nil")
	}
}

// TestPublishToConfluence_InvalidJSONConfig seeds a confluence source with
// malformed JSON config, covering the "unmarshal error → continue" path.
func TestPublishToConfluence_InvalidJSONConfig(t *testing.T) {
	services, knowledgeSvc := setupPublishDispatch(t)

	if err := knowledgeSvc.CreateSource(context.Background(), &knowledge.KnowledgeSource{
		ID:     "conf-bad-cfg",
		Type:   "confluence",
		Name:   "Bad Config Confluence",
		Config: json.RawMessage(`{not valid json}`),
	}); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}

	// Unmarshal fails → continue → "no confluence source configured".
	err := publishProposalToTarget(context.Background(), services, &proposals.Proposal{
		ID: "conf-bad-json", Title: "Confluence Doc", TargetType: proposals.TargetConfluence,
		TargetID: "page-1", ProposedContent: "content",
	})
	if err == nil {
		t.Error("expected error (unmarshal fail), got nil")
	}
}

// TestPublishToConfluence_GetPageVersionError seeds a valid confluence source
// pointing at a test server that returns 500, covering the GetPageVersion error path.
func TestPublishToConfluence_GetPageVersionError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	services, knowledgeSvc := setupPublishDispatch(t)

	cfgJSON, _ := json.Marshal(map[string]any{
		"base_url":  ts.URL,
		"api_token": "tok",
		"email":     "test@example.com",
		"space_key": "DOC",
	})
	if err := knowledgeSvc.CreateSource(context.Background(), &knowledge.KnowledgeSource{
		ID:     "conf-gpv-src",
		Type:   "confluence",
		Name:   "Confluence Source",
		Config: cfgJSON,
	}); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}

	// GetPageVersion returns 500 → error → publishToConfluence returns error.
	err := publishProposalToTarget(context.Background(), services, &proposals.Proposal{
		ID: "conf-gpv-err", Title: "Confluence Doc", TargetType: proposals.TargetConfluence,
		TargetID: "page-1", ProposedContent: "content",
	})
	if err == nil {
		t.Error("expected error (GetPageVersion error), got nil")
	}
}

// TestPublishToNotion_WrongSourceType covers the "type != notion → continue" path.
func TestPublishToNotion_WrongSourceType(t *testing.T) {
	services, knowledgeSvc := setupPublishDispatch(t)

	if err := knowledgeSvc.CreateSource(context.Background(), &knowledge.KnowledgeSource{
		ID:     "git-src-notion-test",
		Type:   "git",
		Name:   "My Git Repo",
		Config: json.RawMessage(`{"url":"https://github.com/example/repo"}`),
	}); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}

	err := publishProposalToTarget(context.Background(), services, &proposals.Proposal{
		ID: "notion-wrong-type", Title: "Notion Doc", TargetType: proposals.TargetNotion,
		TargetID: "db-1", ProposedContent: "content",
	})
	if err == nil {
		t.Error("expected error (no notion source configured), got nil")
	}
}

// TestPublishToNotion_InvalidJSONConfig covers the "notion unmarshal error → continue" path.
func TestPublishToNotion_InvalidJSONConfig(t *testing.T) {
	services, knowledgeSvc := setupPublishDispatch(t)

	if err := knowledgeSvc.CreateSource(context.Background(), &knowledge.KnowledgeSource{
		ID:     "notion-bad-cfg",
		Type:   "notion",
		Name:   "Bad Notion Config",
		Config: json.RawMessage(`{not valid json}`),
	}); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}

	err := publishProposalToTarget(context.Background(), services, &proposals.Proposal{
		ID: "notion-bad-json", Title: "Notion Doc", TargetType: proposals.TargetNotion,
		TargetID: "db-1", ProposedContent: "content",
	})
	if err == nil {
		t.Error("expected error (notion unmarshal fail), got nil")
	}
}

// TestPublishToGit_WrongSourceType covers the "type != git → continue" path.
func TestPublishToGit_WrongSourceType(t *testing.T) {
	services, knowledgeSvc := setupPublishDispatch(t)

	if err := knowledgeSvc.CreateSource(context.Background(), &knowledge.KnowledgeSource{
		ID:     "conf-src-git-test",
		Type:   "confluence",
		Name:   "Confluence Source",
		Config: json.RawMessage(`{"base_url":"https://example.atlassian.net"}`),
	}); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}

	err := publishProposalToTarget(context.Background(), services, &proposals.Proposal{
		ID: "git-wrong-type", Title: "Git Doc", TargetType: proposals.TargetGit,
		TargetID: "docs/README.md", ProposedContent: "content",
	})
	if err == nil {
		t.Error("expected error (no git source configured), got nil")
	}
}

// TestPublishToGit_InvalidJSONConfig covers the "git unmarshal error → continue" path.
func TestPublishToGit_InvalidJSONConfig(t *testing.T) {
	services, knowledgeSvc := setupPublishDispatch(t)

	if err := knowledgeSvc.CreateSource(context.Background(), &knowledge.KnowledgeSource{
		ID:     "git-bad-cfg",
		Type:   "git",
		Name:   "Bad Git Config",
		Config: json.RawMessage(`{not valid json}`),
	}); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}

	err := publishProposalToTarget(context.Background(), services, &proposals.Proposal{
		ID: "git-bad-json", Title: "Git Doc", TargetType: proposals.TargetGit,
		TargetID: "docs/README.md", ProposedContent: "content",
	})
	if err == nil {
		t.Error("expected error (git unmarshal fail), got nil")
	}
}

// TestPublishToGit_CommitAndPushError seeds a valid git source with an unreachable
// URL, covering the CommitAndPush error return path.
func TestPublishToGit_CommitAndPushError(t *testing.T) {
	services, knowledgeSvc := setupPublishDispatch(t)

	if err := knowledgeSvc.CreateSource(context.Background(), &knowledge.KnowledgeSource{
		ID:   "git-src-unreachable",
		Type: "git",
		Name: "Unreachable Git",
		// Port 12345 is not listening; clone will fail immediately with connection refused.
		Config: json.RawMessage(`{"url":"http://localhost:12345/nonexistent.git","branch":"main"}`),
	}); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}

	// CommitAndPush fails with connection refused → publishToGit returns error.
	err := publishProposalToTarget(context.Background(), services, &proposals.Proposal{
		ID: "git-cap-err", Title: "Git Doc", TargetType: proposals.TargetGit,
		TargetID: "docs/README.md", ProposedContent: "content",
	})
	if err == nil {
		t.Error("expected error (CommitAndPush connection refused), got nil")
	}
}

// TestPublishProposalToTarget_UnsupportedTarget covers the default branch of
// publishProposalToTarget (unknown target type).
func TestPublishProposalToTarget_UnsupportedTarget(t *testing.T) {
	services, _ := setupPublishDispatch(t)

	err := publishProposalToTarget(context.Background(), services, &proposals.Proposal{
		ID: "unknown-pub", Title: "Unknown", TargetType: "unknown-backend", TargetID: "some-id",
		ProposedContent: "content",
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported target type") {
		t.Errorf("expected unsupported target type error, got %v", err)
	}
}
