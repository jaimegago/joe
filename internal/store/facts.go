package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
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
	db *sql.DB
}

func (r *sqlFactRepository) Create(ctx context.Context, fact *OnboardingFact) error {
	query := `
		INSERT INTO onboarding_facts (fact_type, subject, content, source, source_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`
	fact.CreatedAt = time.Now()
	result, err := r.db.ExecContext(ctx, query,
		fact.FactType, fact.Subject, fact.Content, fact.Source, fact.SourceID, fact.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert fact: %w", err)
	}
	fact.ID, _ = result.LastInsertId()
	return nil
}

func (r *sqlFactRepository) GetBySubject(ctx context.Context, subject string) ([]*OnboardingFact, error) {
	query := `
		SELECT id, fact_type, subject, content, source, source_id, created_at
		FROM onboarding_facts WHERE subject = ? ORDER BY created_at
	`
	return r.queryFacts(ctx, query, subject)
}

func (r *sqlFactRepository) GetByType(ctx context.Context, factType string) ([]*OnboardingFact, error) {
	query := `
		SELECT id, fact_type, subject, content, source, source_id, created_at
		FROM onboarding_facts WHERE fact_type = ? ORDER BY created_at
	`
	return r.queryFacts(ctx, query, factType)
}

func (r *sqlFactRepository) Search(ctx context.Context, searchQuery string) ([]*OnboardingFact, error) {
	query := `
		SELECT id, fact_type, subject, content, source, source_id, created_at
		FROM onboarding_facts
		WHERE subject LIKE ? OR content LIKE ?
		ORDER BY created_at
	`
	pattern := "%" + searchQuery + "%"
	return r.queryFacts(ctx, query, pattern, pattern)
}

func (r *sqlFactRepository) queryFacts(ctx context.Context, query string, args ...any) ([]*OnboardingFact, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query facts: %w", err)
	}
	defer rows.Close()

	var facts []*OnboardingFact
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

func (r *sqlFactRepository) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM onboarding_facts WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete fact: %w", err)
	}
	return nil
}
