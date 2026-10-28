package runfindings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/store"
)

// Repository is the durable interface for run findings. It is write-once per
// run: Put persists a run's whole set in one transaction, and nothing updates
// or deletes a row — the next run's set is the reconciliation (D-0163), and
// session expunge cascades the rows away (migration 034).
//
// No authorization or enforcement path reads this interface (D-0167). Its
// readers are the run's own labelling of the next run, the findings endpoint,
// and the renderer.
type Repository interface {
	// Put persists a run's finding set and its findings atomically. Every
	// finding must carry set.RunID. A set with no findings is still written,
	// so a clean run is recorded as clean.
	Put(ctx context.Context, set FindingSet, findings []Finding) error
	// ListByRun returns a run's findings in delivery order (Sort).
	ListByRun(ctx context.Context, runID string) ([]Finding, error)
	// GetSet returns a run's finding set, or nil when the run persisted none.
	GetSet(ctx context.Context, runID string) (*FindingSet, error)
	// LatestSetForScope returns the most recent finding set on a scope, or nil
	// when no run has reported on it. The run that reads this is finding "the
	// previous completed run on the same scope" for labelling. before, when
	// non-zero, excludes sets created at or after it, so a run looks behind
	// itself.
	LatestSetForScope(ctx context.Context, scopeKey string, before time.Time) (*FindingSet, error)
	// ListLatest returns, for every scope that has one, the most recent finding
	// set and its findings. This is what the findings view reads.
	ListLatest(ctx context.Context) ([]FindingSet, []Finding, error)
}

// SQLRepository implements Repository on top of *sql.DB.
type SQLRepository struct {
	db     *sql.DB
	driver string
}

// NewRepository constructs a SQLRepository.
func NewRepository(db *sql.DB, driver string) *SQLRepository {
	return &SQLRepository{db: db, driver: driver}
}

const findingColumns = `id, finding_key, run_id, scope_key, session_id, type, reference,
	subject_node_id, subject_component_id, claim, condition, rationale, consequence,
	band, reach_count, weight, derivation, proposal, label,
	evidence_session_id, evidence_message_id, established_at, created_at`

func (r *SQLRepository) Put(ctx context.Context, set FindingSet, findings []Finding) error {
	if set.RunID == "" || set.ScopeKey == "" || set.SessionID == "" {
		return errors.New("put run findings: run_id, scope_key and session_id are required")
	}
	if set.CreatedAt.IsZero() {
		set.CreatedAt = time.Now().UTC()
	}
	set.FindingCount = len(findings)
	for _, f := range findings {
		if f.RunID != set.RunID {
			return fmt.Errorf("put run findings: finding %s carries run %q, set is run %q", f.Key, f.RunID, set.RunID)
		}
		if f.ID == "" || f.Key == "" || f.Label == "" {
			return fmt.Errorf("put run findings: finding %q needs id, key and label", f.Key)
		}
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("put run findings: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, store.Rebind(r.driver, `
		INSERT INTO run_finding_sets (run_id, scope_key, session_id, created_at, finding_count)
		VALUES (?, ?, ?, ?, ?)`),
		set.RunID, set.ScopeKey, set.SessionID, set.CreatedAt.UTC().Format(time.RFC3339Nano), set.FindingCount,
	); err != nil {
		return fmt.Errorf("put run findings: insert set: %w", err)
	}
	for _, f := range findings {
		derivation, err := json.Marshal(f.Derivation)
		if err != nil {
			return fmt.Errorf("put run findings: encode derivation: %w", err)
		}
		var proposal any
		if f.Proposal != nil {
			pb, err := json.Marshal(f.Proposal)
			if err != nil {
				return fmt.Errorf("put run findings: encode proposal: %w", err)
			}
			proposal = string(pb)
		}
		var evidenceMessage any
		if f.EvidenceMessageID != "" {
			evidenceMessage = f.EvidenceMessageID
		}
		if _, err := tx.ExecContext(ctx, store.Rebind(r.driver, `
			INSERT INTO run_findings (`+findingColumns+`)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
			f.ID, f.Key, f.RunID, f.ScopeKey, f.SessionID, string(f.Type), string(f.Reference),
			f.SubjectNodeID, f.SubjectComponentID, f.Claim, f.Condition, f.Rationale, string(f.Consequence),
			string(f.Band), f.ReachCount, f.Weight, string(derivation), proposal, string(f.Label),
			f.EvidenceSessionID, evidenceMessage,
			f.EstablishedAt.UTC().Format(time.RFC3339Nano), f.CreatedAt.UTC().Format(time.RFC3339Nano),
		); err != nil {
			return fmt.Errorf("put run findings: insert finding %s: %w", f.Key, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("put run findings: commit: %w", err)
	}
	return nil
}

func (r *SQLRepository) ListByRun(ctx context.Context, runID string) ([]Finding, error) {
	rows, err := r.db.QueryContext(ctx, store.Rebind(r.driver,
		`SELECT `+findingColumns+` FROM run_findings WHERE run_id = ?`), runID)
	if err != nil {
		return nil, fmt.Errorf("list run findings: %w", err)
	}
	defer rows.Close()
	out, err := scanFindings(rows)
	if err != nil {
		return nil, err
	}
	Sort(out)
	return out, nil
}

func (r *SQLRepository) GetSet(ctx context.Context, runID string) (*FindingSet, error) {
	row := r.db.QueryRowContext(ctx, store.Rebind(r.driver, `
		SELECT run_id, scope_key, session_id, created_at, finding_count
		FROM run_finding_sets WHERE run_id = ?`), runID)
	return scanSet(row.Scan)
}

func (r *SQLRepository) LatestSetForScope(ctx context.Context, scopeKey string, before time.Time) (*FindingSet, error) {
	q := `SELECT run_id, scope_key, session_id, created_at, finding_count
		FROM run_finding_sets WHERE scope_key = ?`
	args := []any{scopeKey}
	if !before.IsZero() {
		q += ` AND created_at < ?`
		args = append(args, before.UTC().Format(time.RFC3339Nano))
	}
	q += ` ORDER BY created_at DESC, run_id DESC LIMIT 1`
	row := r.db.QueryRowContext(ctx, store.Rebind(r.driver, q), args...)
	return scanSet(row.Scan)
}

func (r *SQLRepository) ListLatest(ctx context.Context) ([]FindingSet, []Finding, error) {
	// The latest set per scope: the max created_at per scope_key, joined back.
	// Two sets on one scope with the same created_at (nanosecond ties) both
	// qualify; the UI groups by run so neither is lost.
	setRows, err := r.db.QueryContext(ctx, `
		SELECT s.run_id, s.scope_key, s.session_id, s.created_at, s.finding_count
		FROM run_finding_sets s
		JOIN (
			SELECT scope_key, MAX(created_at) AS latest
			FROM run_finding_sets GROUP BY scope_key
		) m ON m.scope_key = s.scope_key AND s.created_at = m.latest
		ORDER BY s.created_at DESC, s.run_id`)
	if err != nil {
		return nil, nil, fmt.Errorf("list latest run finding sets: %w", err)
	}
	defer setRows.Close()
	var sets []FindingSet
	for setRows.Next() {
		s, err := scanSet(setRows.Scan)
		if err != nil {
			return nil, nil, err
		}
		if s != nil {
			sets = append(sets, *s)
		}
	}
	if err := setRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("list latest run finding sets: %w", err)
	}

	var findings []Finding
	for _, s := range sets {
		fs, err := r.ListByRun(ctx, s.RunID)
		if err != nil {
			return nil, nil, err
		}
		findings = append(findings, fs...)
	}
	if sets == nil {
		sets = []FindingSet{}
	}
	if findings == nil {
		findings = []Finding{}
	}
	return sets, findings, nil
}

func scanSet(scan func(...any) error) (*FindingSet, error) {
	var (
		s         FindingSet
		createdAt string
	)
	err := scan(&s.RunID, &s.ScopeKey, &s.SessionID, &createdAt, &s.FindingCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan run finding set: %w", err)
	}
	s.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	return &s, nil
}

func scanFindings(rows *sql.Rows) ([]Finding, error) {
	var out []Finding
	for rows.Next() {
		var (
			f               Finding
			typ, ref, cons  string
			band, label     string
			derivation      string
			proposal        sql.NullString
			evidenceMessage sql.NullString
			establishedAt   string
			createdAt       string
		)
		if err := rows.Scan(
			&f.ID, &f.Key, &f.RunID, &f.ScopeKey, &f.SessionID, &typ, &ref,
			&f.SubjectNodeID, &f.SubjectComponentID, &f.Claim, &f.Condition, &f.Rationale, &cons,
			&band, &f.ReachCount, &f.Weight, &derivation, &proposal, &label,
			&f.EvidenceSessionID, &evidenceMessage, &establishedAt, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan run finding: %w", err)
		}
		f.Type, f.Reference, f.Consequence = Type(typ), Reference(ref), Consequence(cons)
		f.Band, f.Label = Band(band), Label(label)
		if err := json.Unmarshal([]byte(derivation), &f.Derivation); err != nil {
			return nil, fmt.Errorf("decode derivation for %s: %w", f.Key, err)
		}
		if proposal.Valid && proposal.String != "" {
			var p Proposal
			if err := json.Unmarshal([]byte(proposal.String), &p); err != nil {
				return nil, fmt.Errorf("decode proposal for %s: %w", f.Key, err)
			}
			f.Proposal = &p
		}
		if evidenceMessage.Valid {
			f.EvidenceMessageID = evidenceMessage.String
		}
		f.EstablishedAt, _ = time.Parse(time.RFC3339Nano, establishedAt)
		f.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan run findings: %w", err)
	}
	return out, nil
}
