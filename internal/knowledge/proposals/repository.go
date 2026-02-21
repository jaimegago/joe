package proposals

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Repository defines CRUD operations for proposals.
type Repository interface {
	Create(ctx context.Context, p *Proposal) error
	Get(ctx context.Context, id string) (*Proposal, error)
	List(ctx context.Context, f Filter) ([]*Proposal, error)
	UpdateStatus(ctx context.Context, id string, status ProposalStatus, extra statusExtra) error
}

// Filter scopes a List call.
type Filter struct {
	Status     ProposalStatus
	TargetType TargetType
}

// statusExtra carries optional fields set alongside a status transition.
type statusExtra struct {
	ApprovedAt     *time.Time
	PublishedAt    *time.Time
	RejectedReason string
}

// sqlRepository is the SQLite-backed Repository implementation.
type sqlRepository struct {
	db *sql.DB
}

// NewRepository creates a new SQL-backed proposal Repository.
func NewRepository(db *sql.DB) Repository {
	return &sqlRepository{db: db}
}

func (r *sqlRepository) Create(ctx context.Context, p *Proposal) error {
	entryIDs, err := json.Marshal(p.KnowledgeEntryIDs)
	if err != nil {
		return fmt.Errorf("marshal knowledge_entry_ids: %w", err)
	}
	now := time.Now().UTC()
	p.CreatedAt = now
	p.UpdatedAt = now
	if p.Status == "" {
		p.Status = StatusPending
	}

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO doc_proposals
		  (id, title, target_type, target_id, target_url,
		   current_content, proposed_content, diff,
		   status, context, knowledge_entry_ids,
		   created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.Title, string(p.TargetType), p.TargetID, nullStr(p.TargetURL),
		nullStr(p.CurrentContent), p.ProposedContent, nullStr(p.Diff),
		string(p.Status), nullStr(p.Context), string(entryIDs),
		p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert doc_proposal: %w", err)
	}
	return nil
}

func (r *sqlRepository) Get(ctx context.Context, id string) (*Proposal, error) {
	rows, err := r.query(ctx, "WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("proposal not found: %s", id)
	}
	return rows[0], nil
}

func (r *sqlRepository) List(ctx context.Context, f Filter) ([]*Proposal, error) {
	var conds []string
	var args []any
	if f.Status != "" {
		conds = append(conds, "status = ?")
		args = append(args, string(f.Status))
	}
	if f.TargetType != "" {
		conds = append(conds, "target_type = ?")
		args = append(args, string(f.TargetType))
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	return r.query(ctx, where, args...)
}

func (r *sqlRepository) UpdateStatus(ctx context.Context, id string, status ProposalStatus, extra statusExtra) error {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx, `
		UPDATE doc_proposals SET
		  status=?, updated_at=?,
		  approved_at=?, published_at=?, rejected_reason=?
		WHERE id=?`,
		string(status), now,
		nullTime(extra.ApprovedAt), nullTime(extra.PublishedAt), nullStr(extra.RejectedReason),
		id,
	)
	if err != nil {
		return fmt.Errorf("update proposal status: %w", err)
	}
	return nil
}

func (r *sqlRepository) query(ctx context.Context, where string, args ...any) ([]*Proposal, error) {
	q := `SELECT id, title, target_type, target_id, target_url,
		current_content, proposed_content, diff,
		status, context, knowledge_entry_ids, rejected_reason,
		created_at, updated_at, approved_at, published_at
		FROM doc_proposals ` + where + ` ORDER BY created_at DESC`

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query doc_proposals: %w", err)
	}
	defer rows.Close()

	var proposals []*Proposal
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		proposals = append(proposals, p)
	}
	return proposals, rows.Err()
}

func scanProposal(row *sql.Rows) (*Proposal, error) {
	var p Proposal
	var (
		targetURL, currentContent, diff, context sql.NullString
		rejectedReason, entryIDsJSON             sql.NullString
		approvedAt, publishedAt                  sql.NullString
		targetType, status                       string
	)
	if err := row.Scan(
		&p.ID, &p.Title, &targetType, &p.TargetID, &targetURL,
		&currentContent, &p.ProposedContent, &diff,
		&status, &context, &entryIDsJSON, &rejectedReason,
		&p.CreatedAt, &p.UpdatedAt, &approvedAt, &publishedAt,
	); err != nil {
		return nil, fmt.Errorf("scan doc_proposal: %w", err)
	}
	p.TargetType = TargetType(targetType)
	p.Status = ProposalStatus(status)
	if targetURL.Valid {
		p.TargetURL = targetURL.String
	}
	if currentContent.Valid {
		p.CurrentContent = currentContent.String
	}
	if diff.Valid {
		p.Diff = diff.String
	}
	if context.Valid {
		p.Context = context.String
	}
	if rejectedReason.Valid {
		p.RejectedReason = rejectedReason.String
	}
	if entryIDsJSON.Valid && entryIDsJSON.String != "" {
		_ = json.Unmarshal([]byte(entryIDsJSON.String), &p.KnowledgeEntryIDs)
	}
	if approvedAt.Valid {
		if t, err := time.Parse(time.RFC3339, approvedAt.String); err == nil {
			p.ApprovedAt = &t
		}
	}
	if publishedAt.Valid {
		if t, err := time.Parse(time.RFC3339, publishedAt.String); err == nil {
			p.PublishedAt = &t
		}
	}
	return &p, nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}
