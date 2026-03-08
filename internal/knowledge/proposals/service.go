package proposals

import (
	"context"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/uid"
)

// Service enforces business rules on proposals.
type Service struct {
	repo Repository
}

// NewService creates a new proposal Service.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// Create persists a new proposal (always starts as pending).
func (s *Service) Create(ctx context.Context, p *Proposal) error {
	if p.ID == "" {
		p.ID = uid.New()
	}
	p.Status = StatusPending
	return s.repo.Create(ctx, p)
}

// Get returns a proposal by ID.
func (s *Service) Get(ctx context.Context, id string) (*Proposal, error) {
	return s.repo.Get(ctx, id)
}

// List returns proposals matching the filter.
func (s *Service) List(ctx context.Context, statusFilter ProposalStatus, targetType TargetType) ([]*Proposal, error) {
	return s.repo.List(ctx, Filter{Status: statusFilter, TargetType: targetType})
}

// Approve transitions a pending proposal to approved.
// Returns an error if the proposal is not in pending state.
func (s *Service) Approve(ctx context.Context, id string) error {
	p, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if p.Status != StatusPending {
		return fmt.Errorf("cannot approve proposal %s: status is %q, must be %q", id, p.Status, StatusPending)
	}
	now := time.Now().UTC()
	return s.repo.UpdateStatus(ctx, id, StatusApproved, statusExtra{ApprovedAt: &now})
}

// Reject transitions a pending proposal to rejected.
// Returns an error if the proposal is not in pending state.
func (s *Service) Reject(ctx context.Context, id, reason string) error {
	p, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if p.Status != StatusPending {
		return fmt.Errorf("cannot reject proposal %s: status is %q, must be %q", id, p.Status, StatusPending)
	}
	return s.repo.UpdateStatus(ctx, id, StatusRejected, statusExtra{RejectedReason: reason})
}

// MarkPublished transitions an approved proposal to published.
// Returns an error if the proposal is not in approved state.
func (s *Service) MarkPublished(ctx context.Context, id string) error {
	p, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if p.Status != StatusApproved {
		return fmt.Errorf("cannot publish proposal %s: status is %q, must be %q", id, p.Status, StatusApproved)
	}
	now := time.Now().UTC()
	return s.repo.UpdateStatus(ctx, id, StatusPublished, statusExtra{PublishedAt: &now})
}
