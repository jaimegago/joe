package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/observability"
)

// FactRepository defines operations on onboarding facts.
type FactRepository interface {
	Create(ctx context.Context, fact *OnboardingFact) error
	GetBySubject(ctx context.Context, subject string) ([]*OnboardingFact, error)
	GetByType(ctx context.Context, factType string) ([]*OnboardingFact, error)
	Search(ctx context.Context, query string) ([]*OnboardingFact, error)
	Delete(ctx context.Context, id int64) error
}

type sqlFactRepository struct {
	db      *sql.DB
	driver  string
	metrics *observability.Metrics
}

func (r *sqlFactRepository) Create(ctx context.Context, fact *OnboardingFact) (err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "facts.create", time.Since(start), err) }()

	query := Rebind(r.driver, `
		INSERT INTO onboarding_facts (fact_type, subject, content, source, source_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		RETURNING id
	`)
	fact.CreatedAt = time.Now()
	err = r.db.QueryRowContext(ctx, query,
		fact.FactType, fact.Subject, fact.Content, fact.Source, fact.SourceID, fact.CreatedAt,
	).Scan(&fact.ID)
	if err != nil {
		return fmt.Errorf("insert fact: %w", err)
	}
	return nil
}

func (r *sqlFactRepository) GetBySubject(ctx context.Context, subject string) (facts []*OnboardingFact, err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "facts.get_by_subject", time.Since(start), err) }()

	query := Rebind(r.driver, `
		SELECT id, fact_type, subject, content, source, source_id, created_at
		FROM onboarding_facts WHERE subject = ? ORDER BY created_at
	`)
	return r.queryFacts(ctx, query, subject)
}

func (r *sqlFactRepository) GetByType(ctx context.Context, factType string) (facts []*OnboardingFact, err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "facts.get_by_type", time.Since(start), err) }()

	query := Rebind(r.driver, `
		SELECT id, fact_type, subject, content, source, source_id, created_at
		FROM onboarding_facts WHERE fact_type = ? ORDER BY created_at
	`)
	return r.queryFacts(ctx, query, factType)
}

func (r *sqlFactRepository) Search(ctx context.Context, searchQuery string) (facts []*OnboardingFact, err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "facts.search", time.Since(start), err) }()

	query := Rebind(r.driver, `
		SELECT id, fact_type, subject, content, source, source_id, created_at
		FROM onboarding_facts
		WHERE subject LIKE ? OR content LIKE ?
		ORDER BY created_at
	`)
	pattern := "%" + searchQuery + "%"
	return r.queryFacts(ctx, query, pattern, pattern)
}

func (r *sqlFactRepository) queryFacts(ctx context.Context, query string, args ...any) (facts []*OnboardingFact, err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "facts.query", time.Since(start), err) }()

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query facts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var f OnboardingFact
		var sourceID sql.NullString
		if err := rows.Scan(&f.ID, &f.FactType, &f.Subject, &f.Content, &f.Source, &sourceID, &f.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan fact: %w", err)
		}
		if sourceID.Valid {
			f.SourceID = sourceID.String
		}
		facts = append(facts, &f)
	}

	return facts, rows.Err()
}

func (r *sqlFactRepository) Delete(ctx context.Context, id int64) (err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "facts.delete", time.Since(start), err) }()

	_, err = r.db.ExecContext(ctx, Rebind(r.driver, "DELETE FROM onboarding_facts WHERE id = ?"), id)
	if err != nil {
		return fmt.Errorf("delete fact: %w", err)
	}
	return nil
}
