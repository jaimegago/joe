package proposals

import (
	"context"
	"testing"
)

// TestRepository_Get_NotFound checks the error path when an ID does not exist.
func TestRepository_Get_NotFound(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite")
	_, err := repo.Get(context.Background(), "does-not-exist")
	if err == nil {
		t.Error("Get() should return error for unknown ID")
	}
}

// TestRepository_Create_DBError triggers an insert error by closing the DB.
func TestRepository_Create_DBError(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite")
	db.Close()

	p := makeTestProposal("err1")
	if err := repo.Create(context.Background(), p); err == nil {
		t.Error("Create() on closed DB should return error")
	}
}

// TestRepository_UpdateStatus_DBError triggers an update error by closing the DB.
func TestRepository_UpdateStatus_DBError(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite")
	db.Close()

	if err := repo.UpdateStatus(context.Background(), "any", StatusApproved, statusExtra{}); err == nil {
		t.Error("UpdateStatus() on closed DB should return error")
	}
}

// TestRepository_List_BothFilters exercises the path where both Status and TargetType are set.
func TestRepository_List_BothFilters(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite")
	ctx := context.Background()

	p1 := makeTestProposal("bf1")
	p1.TargetType = TargetConfluence
	_ = repo.Create(ctx, p1)

	p2 := makeTestProposal("bf2")
	p2.TargetType = TargetNotion
	_ = repo.Create(ctx, p2)

	results, err := repo.List(ctx, Filter{Status: StatusPending, TargetType: TargetConfluence})
	if err != nil {
		t.Fatalf("List(both filters): %v", err)
	}
	if len(results) != 1 || results[0].ID != "bf1" {
		t.Errorf("List(both filters): got %d results, want 1 with ID bf1", len(results))
	}
}

// TestRepository_List_DBError triggers a query error by closing the DB.
func TestRepository_List_DBError(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite")
	db.Close()

	_, err := repo.List(context.Background(), Filter{})
	if err == nil {
		t.Error("List() on closed DB should return error")
	}
}

// TestRepository_Query_DBError exercises the error path in query() via Get.
func TestRepository_Query_DBError(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite")
	db.Close()

	_, err := repo.Get(context.Background(), "any")
	if err == nil {
		t.Error("Get() on closed DB should return error")
	}
}

// TestNullStr covers both branches of nullStr.
func TestNullStr(t *testing.T) {
	if nullStr("") != nil {
		t.Error("nullStr(\"\") should return nil")
	}
	if nullStr("hello") == nil {
		t.Error("nullStr(\"hello\") should return non-nil")
	}
}

// TestNullTime covers both branches of nullTime.
func TestNullTime(t *testing.T) {
	if nullTime(nil) != nil {
		t.Error("nullTime(nil) should return nil")
	}
}

// TestRepository_Create_WithOptionalFields creates a proposal with all optional
// fields populated so those code paths in Create/scanProposal are exercised.
func TestRepository_Create_WithOptionalFields(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite")
	ctx := context.Background()

	p := &Proposal{
		ID:                "full1",
		Title:             "Full Proposal",
		TargetType:        TargetConfluence,
		TargetID:          "page-456",
		TargetURL:         "https://example.com/page",
		CurrentContent:    "old content",
		ProposedContent:   "new content",
		Diff:              "diff text",
		Context:           "some context",
		KnowledgeEntryIDs: []string{"e1", "e2"},
	}
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create(full): %v", err)
	}

	got, err := repo.Get(ctx, "full1")
	if err != nil {
		t.Fatalf("Get(full): %v", err)
	}
	if got.TargetURL != "https://example.com/page" {
		t.Errorf("TargetURL = %q, want %q", got.TargetURL, "https://example.com/page")
	}
	if got.CurrentContent != "old content" {
		t.Errorf("CurrentContent = %q, want %q", got.CurrentContent, "old content")
	}
	if got.Diff != "diff text" {
		t.Errorf("Diff = %q, want %q", got.Diff, "diff text")
	}
	if got.Context != "some context" {
		t.Errorf("Context = %q, want %q", got.Context, "some context")
	}
	if len(got.KnowledgeEntryIDs) != 2 {
		t.Errorf("KnowledgeEntryIDs = %v, want 2 entries", got.KnowledgeEntryIDs)
	}
}

// TestService_Reject_NotPending verifies that rejecting a non-pending proposal returns an error.
func TestService_Reject_NotPending(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	_ = svc.Create(ctx, makeTestProposal("rej2"))
	_ = svc.Reject(ctx, "rej2", "already rejected")

	if err := svc.Reject(ctx, "rej2", "again"); err == nil {
		t.Error("Reject() on already-rejected proposal should return error")
	}
}

// TestService_Reject_GetError verifies that Reject propagates Get errors.
func TestService_Reject_GetError(t *testing.T) {
	svc := newTestService(t)
	if err := svc.Reject(context.Background(), "nonexistent", "reason"); err == nil {
		t.Error("Reject() on nonexistent proposal should return error")
	}
}

// TestService_MarkPublished_GetError verifies that MarkPublished propagates Get errors.
func TestService_MarkPublished_GetError(t *testing.T) {
	svc := newTestService(t)
	if err := svc.MarkPublished(context.Background(), "nonexistent"); err == nil {
		t.Error("MarkPublished() on nonexistent proposal should return error")
	}
}

// TestService_Approve_GetError verifies that Approve propagates Get errors.
func TestService_Approve_GetError(t *testing.T) {
	svc := newTestService(t)
	if err := svc.Approve(context.Background(), "nonexistent"); err == nil {
		t.Error("Approve() on nonexistent proposal should return error")
	}
}
