package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/knowledge/drafts"
	"github.com/jaimegago/joe/internal/knowledge/proposals"
)

func TestCreateProposal(t *testing.T) {
	want := proposals.Proposal{ID: "prop-1", Title: "New Doc", Status: proposals.StatusPending}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Method = %q, want POST", r.Method)
		}
		if r.URL.Path != apiKnowledgeProposalsPath {
			t.Errorf("Path = %q, want %q", r.URL.Path, apiKnowledgeProposalsPath)
		}
		var req drafts.GenerateRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Topic != "deployment runbook" {
			t.Errorf("Topic = %q, want %q", req.Topic, "deployment runbook")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(want)
	}))
	defer ts.Close()

	c := New(ts.URL)
	got, err := c.CreateProposal(context.Background(), drafts.GenerateRequest{Topic: "deployment runbook"})
	if err != nil {
		t.Fatalf("CreateProposal() error = %v", err)
	}
	if got.ID != want.ID {
		t.Errorf("ID = %q, want %q", got.ID, want.ID)
	}
	if got.Status != want.Status {
		t.Errorf("Status = %q, want %q", got.Status, want.Status)
	}
}

func TestGetProposal(t *testing.T) {
	want := proposals.Proposal{ID: "prop-get", Status: proposals.StatusPending}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Method = %q, want GET", r.Method)
		}
		if r.URL.Path != apiKnowledgeProposalsPath+"/prop-get" {
			t.Errorf("Path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want)
	}))
	defer ts.Close()

	c := New(ts.URL)
	got, err := c.GetProposal(context.Background(), "prop-get")
	if err != nil {
		t.Fatalf("GetProposal() error = %v", err)
	}
	if got.ID != want.ID {
		t.Errorf("ID = %q, want %q", got.ID, want.ID)
	}
}

func TestListProposals_NoFilter(t *testing.T) {
	var capturedQuery string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		result := struct {
			Proposals []*proposals.Proposal `json:"proposals"`
			Count     int                   `json:"count"`
		}{
			Proposals: []*proposals.Proposal{{ID: "p1"}, {ID: "p2"}},
			Count:     2,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}))
	defer ts.Close()

	c := New(ts.URL)
	ps, err := c.ListProposals(context.Background(), "", "")
	if err != nil {
		t.Fatalf("ListProposals() error = %v", err)
	}
	if len(ps) != 2 {
		t.Errorf("len(proposals) = %d, want 2", len(ps))
	}
	// No filters → no query string appended.
	if capturedQuery != "" {
		t.Errorf("query = %q, want empty (no filters)", capturedQuery)
	}
}

func TestListProposals_WithStatusFilter(t *testing.T) {
	var capturedStatus string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedStatus = r.URL.Query().Get("status")
		result := struct {
			Proposals []*proposals.Proposal `json:"proposals"`
			Count     int                   `json:"count"`
		}{}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.ListProposals(context.Background(), proposals.StatusApproved, "")
	if err != nil {
		t.Fatalf("ListProposals() error = %v", err)
	}
	if capturedStatus != "approved" {
		t.Errorf("status param = %q, want %q", capturedStatus, "approved")
	}
}

func TestListProposals_WithTargetTypeFilter(t *testing.T) {
	var capturedTargetType string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedTargetType = r.URL.Query().Get("target_type")
		result := struct {
			Proposals []*proposals.Proposal `json:"proposals"`
			Count     int                   `json:"count"`
		}{}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.ListProposals(context.Background(), "", proposals.TargetConfluence)
	if err != nil {
		t.Fatalf("ListProposals() error = %v", err)
	}
	if capturedTargetType != "confluence" {
		t.Errorf("target_type param = %q, want %q", capturedTargetType, "confluence")
	}
}

func TestApproveProposal(t *testing.T) {
	var capturedMethod, capturedPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(proposals.Proposal{ID: "prop-appr", Status: proposals.StatusApproved})
	}))
	defer ts.Close()

	c := New(ts.URL)
	err := c.ApproveProposal(context.Background(), "prop-appr")
	if err != nil {
		t.Fatalf("ApproveProposal() error = %v", err)
	}
	if capturedMethod != http.MethodPost {
		t.Errorf("Method = %q, want POST", capturedMethod)
	}
	wantPath := apiKnowledgeProposalsPath + "/prop-appr/approve"
	if capturedPath != wantPath {
		t.Errorf("Path = %q, want %q", capturedPath, wantPath)
	}
}

func TestPublishProposal(t *testing.T) {
	var capturedPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(proposals.Proposal{ID: "prop-pub", Status: proposals.StatusPublished})
	}))
	defer ts.Close()

	c := New(ts.URL)
	err := c.PublishProposal(context.Background(), "prop-pub")
	if err != nil {
		t.Fatalf("PublishProposal() error = %v", err)
	}
	wantPath := apiKnowledgeProposalsPath + "/prop-pub/publish"
	if capturedPath != wantPath {
		t.Errorf("Path = %q, want %q", capturedPath, wantPath)
	}
}

func TestRejectProposal(t *testing.T) {
	var capturedPath string
	var capturedBody map[string]string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(proposals.Proposal{ID: "prop-rej", Status: proposals.StatusRejected})
	}))
	defer ts.Close()

	c := New(ts.URL)
	err := c.RejectProposal(context.Background(), "prop-rej", "out of scope")
	if err != nil {
		t.Fatalf("RejectProposal() error = %v", err)
	}
	wantPath := apiKnowledgeProposalsPath + "/prop-rej/reject"
	if capturedPath != wantPath {
		t.Errorf("Path = %q, want %q", capturedPath, wantPath)
	}
	if capturedBody["reason"] != "out of scope" {
		t.Errorf("reason = %q, want %q", capturedBody["reason"], "out of scope")
	}
}
