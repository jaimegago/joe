package proposals

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE doc_proposals (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		target_type TEXT NOT NULL,
		target_id TEXT NOT NULL,
		target_url TEXT,
		current_content TEXT,
		proposed_content TEXT NOT NULL,
		diff TEXT,
		status TEXT NOT NULL DEFAULT 'pending',
		context TEXT,
		knowledge_entry_ids TEXT,
		rejected_reason TEXT,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		approved_at DATETIME,
		published_at DATETIME
	)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	db := newTestDB(t)
	return NewService(NewRepository(db))
}

func makeTestProposal(id string) *Proposal {
	return &Proposal{
		ID:              id,
		Title:           "Test Doc",
		TargetType:      TargetConfluence,
		TargetID:        "page-123",
		ProposedContent: "New content",
	}
}

func TestService_Create(t *testing.T) {
	svc := newTestService(t)
	p := makeTestProposal("p1")

	if err := svc.Create(context.Background(), p); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if p.Status != StatusPending {
		t.Errorf("Status = %q, want %q", p.Status, StatusPending)
	}
}

func TestService_Create_AutoID(t *testing.T) {
	svc := newTestService(t)
	p := &Proposal{
		Title:           "Auto ID Test",
		TargetType:      TargetNotion,
		TargetID:        "db-1",
		ProposedContent: "content",
	}
	if err := svc.Create(context.Background(), p); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if p.ID == "" {
		t.Error("ID should be auto-generated when empty")
	}
}

func TestService_Get(t *testing.T) {
	svc := newTestService(t)
	_ = svc.Create(context.Background(), makeTestProposal("get1"))

	got, err := svc.Get(context.Background(), "get1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.ID != "get1" {
		t.Errorf("ID = %q, want %q", got.ID, "get1")
	}
}

func TestService_Approve(t *testing.T) {
	svc := newTestService(t)
	_ = svc.Create(context.Background(), makeTestProposal("ap1"))

	if err := svc.Approve(context.Background(), "ap1"); err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	got, _ := svc.Get(context.Background(), "ap1")
	if got.Status != StatusApproved {
		t.Errorf("Status = %q, want %q", got.Status, StatusApproved)
	}
	if got.ApprovedAt == nil {
		t.Error("ApprovedAt should be set after approval")
	}
}

func TestService_Approve_NotPending(t *testing.T) {
	svc := newTestService(t)
	_ = svc.Create(context.Background(), makeTestProposal("ap2"))
	_ = svc.Approve(context.Background(), "ap2")

	if err := svc.Approve(context.Background(), "ap2"); err == nil {
		t.Error("Approve() on already-approved proposal should return error")
	}
}

func TestService_Reject(t *testing.T) {
	svc := newTestService(t)
	_ = svc.Create(context.Background(), makeTestProposal("rej1"))

	if err := svc.Reject(context.Background(), "rej1", "out of scope"); err != nil {
		t.Fatalf("Reject() error = %v", err)
	}
	got, _ := svc.Get(context.Background(), "rej1")
	if got.Status != StatusRejected {
		t.Errorf("Status = %q, want %q", got.Status, StatusRejected)
	}
	if got.RejectedReason != "out of scope" {
		t.Errorf("RejectedReason = %q, want %q", got.RejectedReason, "out of scope")
	}
}

func TestService_MarkPublished(t *testing.T) {
	svc := newTestService(t)
	_ = svc.Create(context.Background(), makeTestProposal("pub1"))
	_ = svc.Approve(context.Background(), "pub1")

	if err := svc.MarkPublished(context.Background(), "pub1"); err != nil {
		t.Fatalf("MarkPublished() error = %v", err)
	}
	got, _ := svc.Get(context.Background(), "pub1")
	if got.Status != StatusPublished {
		t.Errorf("Status = %q, want %q", got.Status, StatusPublished)
	}
	if got.PublishedAt == nil {
		t.Error("PublishedAt should be set after publishing")
	}
}

func TestService_MarkPublished_NotApproved(t *testing.T) {
	svc := newTestService(t)
	_ = svc.Create(context.Background(), makeTestProposal("pub2"))

	if err := svc.MarkPublished(context.Background(), "pub2"); err == nil {
		t.Error("MarkPublished() on pending proposal should return error")
	}
}

func TestService_List(t *testing.T) {
	svc := newTestService(t)
	p1 := makeTestProposal("l1")
	p2 := makeTestProposal("l2")
	p2.TargetType = TargetNotion
	_ = svc.Create(context.Background(), p1)
	_ = svc.Create(context.Background(), p2)

	all, err := svc.List(context.Background(), "", "")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(all) != 2 {
		t.Errorf("List() returned %d proposals, want 2", len(all))
	}

	notion, err := svc.List(context.Background(), "", TargetNotion)
	if err != nil {
		t.Fatalf("List(notion) error = %v", err)
	}
	if len(notion) != 1 {
		t.Errorf("List(notion) returned %d, want 1", len(notion))
	}
}
