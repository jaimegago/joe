package review

import (
	"context"
	"errors"
	"testing"
	"time"
)

// mockRepo is a minimal in-memory Repository for service tests.
type mockRepo struct {
	jobs map[string]*ReviewJob
}

func newMockRepo() *mockRepo {
	return &mockRepo{jobs: make(map[string]*ReviewJob)}
}

func (m *mockRepo) Enqueue(_ context.Context, job *ReviewJob) error {
	for _, j := range m.jobs {
		if j.EventID == job.EventID {
			return ErrDuplicateEvent
		}
	}
	cp := *job
	m.jobs[job.ID] = &cp
	return nil
}

func (m *mockRepo) Get(_ context.Context, id string) (*ReviewJob, error) {
	j, ok := m.jobs[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *j
	return &cp, nil
}

func (m *mockRepo) GetByEventID(_ context.Context, eventID string) (*ReviewJob, error) {
	for _, j := range m.jobs {
		if j.EventID == eventID {
			cp := *j
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

func (m *mockRepo) List(_ context.Context, f Filter) ([]*ReviewJob, error) {
	var result []*ReviewJob
	for _, j := range m.jobs {
		if f.Platform != "" && j.Platform != f.Platform {
			continue
		}
		if f.Status != "" && j.Status != f.Status {
			continue
		}
		cp := *j
		result = append(result, &cp)
	}
	return result, nil
}

func (m *mockRepo) UpdateStatus(_ context.Context, id string, status JobStatus, extra statusExtra) error {
	j, ok := m.jobs[id]
	if !ok {
		return ErrNotFound
	}
	j.Status = status
	j.StartedAt = extra.StartedAt
	j.FinishedAt = extra.FinishedAt
	j.ReviewBody = extra.ReviewBody
	j.Error = extra.Error
	return nil
}

func TestService_Enqueue(t *testing.T) {
	svc := NewService(newMockRepo())
	ctx := context.Background()

	job, err := svc.Enqueue(ctx, &ReviewJob{
		EventID:  "github:org/repo#1:abc",
		Platform: PlatformGitHub,
		SourceID: "src-1",
		Owner:    "org",
		Repo:     "repo",
		PRNumber: 1,
		HeadSHA:  "abc",
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if job.ID == "" {
		t.Error("expected ID to be set")
	}
	if job.Status != JobStatusPending {
		t.Errorf("status: got %s, want %s", job.Status, JobStatusPending)
	}

	// Duplicate should return ErrDuplicateEvent.
	_, err = svc.Enqueue(ctx, &ReviewJob{
		EventID:  "github:org/repo#1:abc",
		Platform: PlatformGitHub,
		SourceID: "src-1",
		Owner:    "org",
		Repo:     "repo",
		PRNumber: 1,
		HeadSHA:  "abc",
	})
	if !errors.Is(err, ErrDuplicateEvent) {
		t.Errorf("expected ErrDuplicateEvent, got %v", err)
	}
}

func TestService_MarkRunning(t *testing.T) {
	repo := newMockRepo()
	svc := NewService(repo)
	ctx := context.Background()

	job, _ := svc.Enqueue(ctx, &ReviewJob{
		EventID:  "github:org/repo#2:def",
		Platform: PlatformGitHub,
		SourceID: "src-1",
		Owner:    "org",
		Repo:     "repo",
		PRNumber: 2,
		HeadSHA:  "def",
	})

	if err := svc.MarkRunning(ctx, job.ID); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	got, _ := svc.Get(ctx, job.ID)
	if got.Status != JobStatusRunning {
		t.Errorf("status: got %s, want %s", got.Status, JobStatusRunning)
	}
	if got.StartedAt == nil {
		t.Error("started_at should be set")
	}

	// Cannot mark running a second time (already running).
	if err := svc.MarkRunning(ctx, job.ID); err == nil {
		t.Error("expected error when marking already-running job as running")
	}
}

func TestService_MarkDone(t *testing.T) {
	repo := newMockRepo()
	svc := NewService(repo)
	ctx := context.Background()

	job, _ := svc.Enqueue(ctx, &ReviewJob{
		EventID:  "github:org/repo#3:ghi",
		Platform: PlatformGitHub,
		SourceID: "src-1",
		Owner:    "org",
		Repo:     "repo",
		PRNumber: 3,
		HeadSHA:  "ghi",
	})
	_ = svc.MarkRunning(ctx, job.ID)

	if err := svc.MarkDone(ctx, job.ID, "Looks good!"); err != nil {
		t.Fatalf("mark done: %v", err)
	}
	got, _ := svc.Get(ctx, job.ID)
	if got.Status != JobStatusDone {
		t.Errorf("status: got %s, want %s", got.Status, JobStatusDone)
	}
	if got.ReviewBody != "Looks good!" {
		t.Errorf("review_body: got %q, want %q", got.ReviewBody, "Looks good!")
	}
	if got.FinishedAt == nil {
		t.Error("finished_at should be set")
	}
}

func TestService_MarkFailed(t *testing.T) {
	repo := newMockRepo()
	svc := NewService(repo)
	ctx := context.Background()

	job, _ := svc.Enqueue(ctx, &ReviewJob{
		EventID:  "github:org/repo#4:jkl",
		Platform: PlatformGitHub,
		SourceID: "src-1",
		Owner:    "org",
		Repo:     "repo",
		PRNumber: 4,
		HeadSHA:  "jkl",
	})

	if err := svc.MarkFailed(ctx, job.ID, "LLM timeout"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	got, _ := svc.Get(ctx, job.ID)
	if got.Status != JobStatusFailed {
		t.Errorf("status: got %s, want %s", got.Status, JobStatusFailed)
	}
	if got.Error != "LLM timeout" {
		t.Errorf("error: got %q, want %q", got.Error, "LLM timeout")
	}
}

func TestService_List(t *testing.T) {
	repo := newMockRepo()
	svc := NewService(repo)
	ctx := context.Background()

	for i, sha := range []string{"a1", "a2", "b1"} {
		platform := PlatformGitHub
		if i == 2 {
			platform = PlatformGitLab
		}
		_, _ = svc.Enqueue(ctx, &ReviewJob{
			EventID:  BuildEventID(platform, "org", "r", i+1, sha),
			Platform: platform,
			SourceID: "src-1",
			Owner:    "org",
			Repo:     "r",
			PRNumber: i + 1,
			HeadSHA:  sha,
		})
	}

	all, err := svc.List(ctx, "", "", 0)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("list all: got %d, want 3", len(all))
	}

	gh, err := svc.List(ctx, PlatformGitHub, "", 0)
	if err != nil {
		t.Fatalf("list github: %v", err)
	}
	if len(gh) != 2 {
		t.Errorf("list github: got %d, want 2", len(gh))
	}
	_ = time.Now() // keep time import
}
