package rbac

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/store"
)

// Principal lifecycle states. The principals table CHECK constraint admits
// exactly these (migration 021); a future suspended/pending state would widen
// both the CHECK and this set.
const (
	// PrincipalStatusActive is the default state of a provisioned principal.
	PrincipalStatusActive = "active"
	// PrincipalStatusDisabled marks a principal an admin has turned off. The
	// per-request gate (Stage 2) consults this; Stage 1 only records it.
	PrincipalStatusDisabled = "disabled"
)

// PrincipalRecord is one row of the authoritative identity registry
// (principals table, migration 021). It is the MUTABLE per-user record the
// append-only audit event stream cannot provide: a lifecycle status, the
// disable provenance, and mutable display metadata.
//
// Nullable columns map to pointer / empty-string fields: a nil DisabledAt /
// LastSeenAt or an empty DisabledBy / DisplayName is SQL NULL.
type PrincipalRecord struct {
	Principal   string     `json:"principal"`
	CreatedAt   time.Time  `json:"created_at"`
	Status      string     `json:"status"`
	DisabledAt  *time.Time `json:"disabled_at,omitempty"`
	DisabledBy  string     `json:"disabled_by,omitempty"`
	DisplayName string     `json:"display_name,omitempty"`
	LastSeenAt  *time.Time `json:"last_seen_at,omitempty"`
}

// PrincipalRepository is the read/write surface on the identity registry. It is
// a separate interface from Repository (rather than folded into it) so the many
// existing rbac.Repository implementers do not have to grow identity methods;
// *SQLRepository satisfies both. SetStatus writes its audit row in the same
// transaction as the status change (see Repository's contract); the other
// methods are reads or provisioning upserts and write no audit row.
type PrincipalRepository interface {
	// UpsertPrincipal inserts a principal or, if it already exists, refreshes
	// only its mutable metadata (display_name, last_seen_at). It never changes
	// status, created_at, or the disable provenance — those are owned by
	// SetPrincipalStatus. Provisioning hooks (Stage 2) call this on login.
	UpsertPrincipal(ctx context.Context, p PrincipalRecord) error
	// GetPrincipal returns the registry row for the id, or (nil, nil) if the
	// principal has never been provisioned.
	GetPrincipal(ctx context.Context, principal string) (*PrincipalRecord, error)
	// ListPrincipals returns every registry row ordered by principal — the
	// Users-page query.
	ListPrincipals(ctx context.Context) ([]PrincipalRecord, error)
	// SetPrincipalStatus moves a principal between 'active' and 'disabled',
	// recording disabled_at/disabled_by on disable and clearing them on enable,
	// and writes the matching audit row in the same transaction. Returns the
	// number of rows changed (0 if the principal does not exist).
	SetPrincipalStatus(ctx context.Context, principal, status, actor string) (int64, error)
}

func (r *SQLRepository) UpsertPrincipal(ctx context.Context, p PrincipalRecord) error {
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
	}
	if p.Status == "" {
		p.Status = PrincipalStatusActive
	}
	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		INSERT INTO principals (principal, created_at, status, display_name, last_seen_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(principal) DO UPDATE SET
			display_name = excluded.display_name,
			last_seen_at = excluded.last_seen_at`),
		p.Principal, p.CreatedAt.UTC().Format(time.RFC3339), p.Status,
		nullStr(p.DisplayName), nullTime(p.LastSeenAt))
	if err != nil {
		return fmt.Errorf("upsert principal: %w", err)
	}
	return nil
}

func (r *SQLRepository) GetPrincipal(ctx context.Context, principal string) (*PrincipalRecord, error) {
	p, err := scanPrincipal(r.db.QueryRowContext(ctx, store.Rebind(r.driver, `
		SELECT principal, created_at, status, disabled_at, disabled_by, display_name, last_seen_at
		FROM principals WHERE principal = ?`), principal))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get principal: %w", err)
	}
	return p, nil
}

func (r *SQLRepository) ListPrincipals(ctx context.Context) ([]PrincipalRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT principal, created_at, status, disabled_at, disabled_by, display_name, last_seen_at
		FROM principals ORDER BY principal`)
	if err != nil {
		return nil, fmt.Errorf("list principals: %w", err)
	}
	defer rows.Close()

	var out []PrincipalRecord
	for rows.Next() {
		p, serr := scanPrincipal(rows)
		if serr != nil {
			return nil, fmt.Errorf("scan principal: %w", serr)
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (r *SQLRepository) SetPrincipalStatus(ctx context.Context, principal, status, actor string) (int64, error) {
	var action string
	switch status {
	case PrincipalStatusDisabled:
		action = audit.ActionAdminPrincipalDisable
	case PrincipalStatusActive:
		action = audit.ActionAdminPrincipalEnable
	default:
		return 0, fmt.Errorf("set principal status: invalid status %q", status)
	}

	var changed int64
	err := r.mutate(ctx, func(exec execQuerier) (audit.Event, error) {
		before, gerr := getPrincipalOn(ctx, exec, r.driver, principal)
		if gerr != nil {
			return audit.Event{}, gerr
		}

		var (
			disabledAt sql.NullString
			disabledBy sql.NullString
		)
		if status == PrincipalStatusDisabled {
			disabledAt = sql.NullString{String: time.Now().UTC().Format(time.RFC3339), Valid: true}
			disabledBy = sql.NullString{String: actor, Valid: actor != ""}
		}
		res, uerr := exec.ExecContext(ctx, store.Rebind(r.driver, `
			UPDATE principals SET status = ?, disabled_at = ?, disabled_by = ?
			WHERE principal = ?`),
			status, disabledAt, disabledBy, principal)
		if uerr != nil {
			return audit.Event{}, fmt.Errorf("set principal status: %w", uerr)
		}
		n, aerr := res.RowsAffected()
		if aerr != nil {
			return audit.Event{}, fmt.Errorf("rows affected: %w", aerr)
		}
		changed = n
		if n == 0 {
			// Principal not in the registry — nothing changed, so do not audit.
			return audit.Event{}, nil
		}
		after, gerr := getPrincipalOn(ctx, exec, r.driver, principal)
		if gerr != nil {
			return audit.Event{}, gerr
		}
		d := audit.Details{Target: "principal:" + principal, After: after}
		if before != nil {
			d.Before = *before
		}
		return adminEvent(actor, action, d)
	})
	if err != nil {
		return 0, err
	}
	return changed, nil
}

// getPrincipalOn reads a principal against the supplied execQuerier so the
// before/after reads for an audited status change run inside the same
// transaction as the update. Returns (nil, nil) when absent.
func getPrincipalOn(ctx context.Context, exec execQuerier, driver, principal string) (*PrincipalRecord, error) {
	p, err := scanPrincipal(exec.QueryRowContext(ctx, store.Rebind(driver, `
		SELECT principal, created_at, status, disabled_at, disabled_by, display_name, last_seen_at
		FROM principals WHERE principal = ?`), principal))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get principal: %w", err)
	}
	return p, nil
}

// rowScanner is satisfied by *sql.Row and *sql.Rows so scanPrincipal serves
// both the point-read and the list paths.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanPrincipal(s rowScanner) (*PrincipalRecord, error) {
	var (
		p           PrincipalRecord
		createdAt   string
		disabledAt  sql.NullString
		disabledBy  sql.NullString
		displayName sql.NullString
		lastSeenAt  sql.NullString
	)
	if err := s.Scan(&p.Principal, &createdAt, &p.Status, &disabledAt, &disabledBy, &displayName, &lastSeenAt); err != nil {
		return nil, err
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	if disabledAt.Valid {
		t, _ := time.Parse(time.RFC3339, disabledAt.String)
		p.DisabledAt = &t
	}
	p.DisabledBy = disabledBy.String
	p.DisplayName = displayName.String
	if lastSeenAt.Valid {
		t, _ := time.Parse(time.RFC3339, lastSeenAt.String)
		p.LastSeenAt = &t
	}
	return &p, nil
}

// nullStr maps "" → SQL NULL so an unset display_name / disabled_by stays NULL.
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullTime maps a nil *time.Time → SQL NULL, else an RFC3339 UTC string.
func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}
