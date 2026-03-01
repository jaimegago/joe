package review

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
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
		job.ID = uuid.New().String()
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

// MarkRunning transitions a pending job to running.
func (s *Service) MarkRunning(ctx context.Context, id string) error {
	job, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if job.Status != JobStatusPending {
		return fmt.Errorf("cannot mark job %s running: status is %q, must be %q", id, job.Status, JobStatusPending)
	}
	now := time.Now().UTC()
	return s.repo.UpdateStatus(ctx, id, JobStatusRunning, statusExtra{StartedAt: &now})
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
