package review

import (
	"context"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/uid"
)

// Service enforces business rules on review jobs.
type Service struct {
	repo Repository
}

// NewService creates a new review Service.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// Enqueue creates a new pending review job.
// Returns ErrDuplicateEvent if the event_id already exists in the queue.
func (s *Service) Enqueue(ctx context.Context, job *ReviewJob) (*ReviewJob, error) {
	if job.ID == "" {
		job.ID = uid.New()
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now().UTC()
	}
	job.Status = JobStatusPending
	if err := s.repo.Enqueue(ctx, job); err != nil {
		return nil, err
	}
	return job, nil
}

// Get returns a review job by ID.
func (s *Service) Get(ctx context.Context, id string) (*ReviewJob, error) {
	return s.repo.Get(ctx, id)
}

// List returns jobs matching the filter.
func (s *Service) List(ctx context.Context, platform Platform, status JobStatus, limit int) ([]*ReviewJob, error) {
	return s.repo.List(ctx, Filter{Platform: platform, Status: status, Limit: limit})
}

// MarkRunning atomically transitions a pending job to running.
// Returns an error (wrapping ErrAlreadyClaimed) when another instance
// already claimed the job — callers should treat this as a signal to skip.
func (s *Service) MarkRunning(ctx context.Context, id string) error {
	now := time.Now().UTC()
	claimed, err := s.repo.ClaimJob(ctx, id, now)
	if err != nil {
		return err
	}
	if !claimed {
		return fmt.Errorf("%w: job %s", ErrAlreadyClaimed, id)
	}
	return nil
}

// MarkDone transitions a running job to done with the review body.
func (s *Service) MarkDone(ctx context.Context, id, reviewBody string) error {
	now := time.Now().UTC()
	return s.repo.UpdateStatus(ctx, id, JobStatusDone, statusExtra{
		FinishedAt: &now,
		ReviewBody: reviewBody,
	})
}

// MarkFailed transitions a job to failed with an error message.
func (s *Service) MarkFailed(ctx context.Context, id, errMsg string) error {
	now := time.Now().UTC()
	return s.repo.UpdateStatus(ctx, id, JobStatusFailed, statusExtra{
		FinishedAt: &now,
		Error:      errMsg,
	})
}
