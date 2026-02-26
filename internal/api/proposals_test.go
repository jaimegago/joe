package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/knowledge"
	"github.com/jaimegago/joe/internal/knowledge/drafts"
	"github.com/jaimegago/joe/internal/knowledge/proposals"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/test/mocks"
	_ "github.com/mattn/go-sqlite3"
)

func setupProposalsTestServer(t *testing.T) (*http.ServeMux, *proposals.Service, *knowledge.Service) {
	t.Helper()

	sqlStore, err := store.New(":memory:?_foreign_keys=on", nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })

	knowledgeSvc := knowledge.NewService(sqlStore.Knowledge, nil)
	proposalRepo := proposals.NewRepository(sqlStore.DB())
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

	sqlStore, err := store.New(":memory:?_foreign_keys=on", nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })

	knowledgeSvc := knowledge.NewService(sqlStore.Knowledge, nil)
	proposalRepo := proposals.NewRepository(sqlStore.DB())
	proposalSvc := proposals.NewService(proposalRepo)
	docDrafter := drafts.New(knowledgeSvc, proposalSvc, mocks.NewMockLLM())

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

// TestHandlePublishProposal_NotApproved verifies 422 when publishing a pending proposal.
func TestHandlePublishProposal_NotApproved(t *testing.T) {
	// Use publisher-enabled server so DocDrafter nil-check passes.
	mux, proposalSvc, _ := setupProposalsWithPublisherTestServer(t)

	seedProposal(t, proposalSvc, &proposals.Proposal{
		ID: "pub-pending", Title: "Pending", TargetType: proposals.TargetConfluence, TargetID: "page-1",
		ProposedContent: "content",
	})

	w := doRequest(mux, http.MethodPost, apiPrefix+"/knowledge/proposals/pub-pending/publish", nil)
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d (pending proposal)", w.Code, http.StatusUnprocessableEntity)
	}
}

// TestHandlePublishProposal_ApprovedNoSource verifies publish dispatch returns 500
// when no matching source is configured for the target type.
func TestHandlePublishProposal_ApprovedNoSource(t *testing.T) {
	// Use publisher-enabled server so DocDrafter nil-check passes.
	mux, proposalSvc, _ := setupProposalsWithPublisherTestServer(t)

	seedProposal(t, proposalSvc, &proposals.Proposal{
		ID: "pub-approved", Title: "Approved", TargetType: proposals.TargetConfluence, TargetID: "page-1",
		ProposedContent: "content",
	})
	if err := proposalSvc.Approve(context.Background(), "pub-approved"); err != nil {
		t.Fatalf("approve: %v", err)
	}

	// No confluence source configured → publishToConfluence returns error → 500
	w := doRequest(mux, http.MethodPost, apiPrefix+"/knowledge/proposals/pub-approved/publish", nil)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d (no source configured)", w.Code, http.StatusInternalServerError)
	}
}
