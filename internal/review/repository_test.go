package review

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	migration, err := os.ReadFile("../../internal/store/migrations/007_review_jobs.up.sql")
	if err != nil {
		// Try relative path from test directory.
		migration, err = os.ReadFile("../store/migrations/007_review_jobs.up.sql")
		if err != nil {
			t.Fatalf("read migration: %v", err)
		}
	}
	if _, err := db.Exec(string(migration)); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
	// Migration 023 (D-0021) renames review_jobs.source_id -> component_id.
	if _, err := db.Exec(`ALTER TABLE review_jobs RENAME COLUMN source_id TO component_id`); err != nil {
		t.Fatalf("apply 023 column rename: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func makeJob(platform Platform, owner, repo string, pr int, sha string) *ReviewJob {
	return &ReviewJob{
		ID:          "job-" + sha,
		EventID:     BuildEventID(platform, owner, repo, pr, sha),
		Platform:    platform,
		ComponentID: "src-1",
		Owner:       owner,
		Repo:        repo,
		PRNumber:    pr,
		HeadSHA:     sha,
		CreatedAt:   time.Now().UTC().Truncate(time.Second),
	}
}

func TestRepository_Enqueue(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite")
	ctx := context.Background()

	job := makeJob(PlatformGitHub, "org", "myrepo", 1, "abc123")
	if err := repo.Enqueue(ctx, job); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Second enqueue with same event_id must return ErrDuplicateEvent.
	err := repo.Enqueue(ctx, &ReviewJob{
		ID:          "job-other",
		EventID:     job.EventID,
		Platform:    PlatformGitHub,
		ComponentID: "src-1",
		Owner:       "org",
		Repo:        "myrepo",
		PRNumber:    1,
		HeadSHA:     "abc123",
		CreatedAt:   time.Now().UTC(),
	})
	if err != ErrDuplicateEvent {
		t.Fatalf("expected ErrDuplicateEvent, got %v", err)
	}
}

func TestRepository_Get(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite")
	ctx := context.Background()

	job := makeJob(PlatformGitHub, "org", "myrepo", 2, "def456")
	if err := repo.Enqueue(ctx, job); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	got, err := repo.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != job.ID {
		t.Errorf("id: got %s, want %s", got.ID, job.ID)
	}
	if got.Status != JobStatusPending {
		t.Errorf("status: got %s, want %s", got.Status, JobStatusPending)
	}
}

func TestRepository_GetByEventID(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite")
	ctx := context.Background()

	job := makeJob(PlatformGitLab, "group", "project", 5, "fff000")
	if err := repo.Enqueue(ctx, job); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	got, err := repo.GetByEventID(ctx, job.EventID)
	if err != nil {
		t.Fatalf("get by event_id: %v", err)
	}
	if got.EventID != job.EventID {
		t.Errorf("event_id: got %s, want %s", got.EventID, job.EventID)
	}
}

func TestRepository_UpdateStatus(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite")
	ctx := context.Background()

	job := makeJob(PlatformGitHub, "org", "svc", 10, "aaa111")
	if err := repo.Enqueue(ctx, job); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	now := time.Now().UTC()
	if err := repo.UpdateStatus(ctx, job.ID, JobStatusRunning, statusExtra{StartedAt: &now}); err != nil {
		t.Fatalf("update running: %v", err)
	}

	got, _ := repo.Get(ctx, job.ID)
	if got.Status != JobStatusRunning {
		t.Errorf("status: got %s, want %s", got.Status, JobStatusRunning)
	}
	if got.StartedAt == nil {
		t.Error("started_at should be set")
	}

	finAt := time.Now().UTC()
	if err := repo.UpdateStatus(ctx, job.ID, JobStatusDone, statusExtra{
		FinishedAt: &finAt,
		ReviewBody: "LGTM",
	}); err != nil {
		t.Fatalf("update done: %v", err)
	}

	got, _ = repo.Get(ctx, job.ID)
	if got.Status != JobStatusDone {
		t.Errorf("status: got %s, want %s", got.Status, JobStatusDone)
	}
	if got.ReviewBody != "LGTM" {
		t.Errorf("review_body: got %s, want LGTM", got.ReviewBody)
	}
}

func TestRepository_ClaimJob(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite")
	ctx := context.Background()

	job := makeJob(PlatformGitHub, "org", "claimrepo", 1, "ccc111")
	if err := repo.Enqueue(ctx, job); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// First claim should succeed.
	claimed, err := repo.ClaimJob(ctx, job.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("ClaimJob: %v", err)
	}
	if !claimed {
		t.Error("expected claimed=true on first attempt")
	}

	// Second claim should return false (already running).
	claimed2, err := repo.ClaimJob(ctx, job.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("ClaimJob (second): %v", err)
	}
	if claimed2 {
		t.Error("expected claimed=false on second attempt")
	}

	// Verify status is running.
	got, _ := repo.Get(ctx, job.ID)
	if got.Status != JobStatusRunning {
		t.Errorf("status after ClaimJob = %s, want running", got.Status)
	}
}

func TestRepository_GetNotFound(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite")
	ctx := context.Background()

	_, err := repo.Get(ctx, "nonexistent")
	if err == nil {
		t.Fatal("Get on nonexistent ID should return error")
	}
}

func TestRepository_GetByEventIDNotFound(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite")
	ctx := context.Background()

	_, err := repo.GetByEventID(ctx, "nonexistent-event")
	if err == nil {
		t.Fatal("GetByEventID on nonexistent event_id should return error")
	}
}

func TestRepository_List(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite")
	ctx := context.Background()

	jobs := []*ReviewJob{
		makeJob(PlatformGitHub, "org", "repo1", 1, "sha1"),
		makeJob(PlatformGitHub, "org", "repo2", 2, "sha2"),
		makeJob(PlatformGitLab, "grp", "proj", 3, "sha3"),
	}
	for _, j := range jobs {
		if err := repo.Enqueue(ctx, j); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}

	all, err := repo.List(ctx, Filter{})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("list all: got %d, want 3", len(all))
	}

	ghOnly, err := repo.List(ctx, Filter{Platform: PlatformGitHub})
	if err != nil {
		t.Fatalf("list github: %v", err)
	}
	if len(ghOnly) != 2 {
		t.Errorf("list github: got %d, want 2", len(ghOnly))
	}
}
