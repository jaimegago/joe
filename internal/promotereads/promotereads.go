// Package promotereads owns the durable per-component-type auto_promote_reads
// flag (A001-COREGOV CC-04) and its single audited write path.
//
// The flag is a boolean keyed by component-type string. The RBAC policy engine
// consults it as a DYNAMIC ADMIT PREDICATE for the agent:core principal on
// ActionRead ONLY: a component whose type has the flag ON is readable by
// agent:core with no materialized grant row, resolved live at decision time
// (see internal/rbac/policy.go). The engine reads through this package's
// Repository; it never writes.
//
// Default semantics: ABSENT row == OFF. The agent_read_promotions table
// (migration 024) is NOT pre-seeded — promote == upsert a row with enabled=1;
// OFF == an absent row OR a row with enabled=0. A freshly migrated system
// therefore has every type OFF with zero rows, the conservative deny default.
//
// Three pieces fit together here, mirroring the llmsettings (Stream G) stack:
//
//  1. Repository — direct reads (IsPromoted, List, ComponentType) and the
//     transactional upsert (UpsertTx) used inside the mutation service's
//     transaction. ComponentType resolves a componentID -> type via a single
//     PK query against the components table; it is the engine's component-type
//     resolution seam.
//
//  2. MutationService — the SOLE write path. SetPromoted runs ONE transaction:
//     it reads the prior enabled bit, upserts the new value, and writes one
//     KindAdminAccess audit row against the SAME transaction via
//     audit.Repository.InsertTx. Either both rows commit, or neither does
//     (fail-closed). This mirrors llmsettings.MutationService.runMutation and
//     uses the same audit substrate as the RBAC admin grants.
package promotereads

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
)

// ErrWriteFailed wraps a lower-level error so callers can identify a
// promote-reads write failure without depending on the driver error shape.
// Tests assert via errors.Is.
var ErrWriteFailed = errors.New("promotereads: write failed")

// Audit context-key vocabulary. Identical {target, before, after} shape the
// llmsettings mutations and the RBAC admin rows use, so any later admin reader
// of the audit_log sees one canonical context shape across the admin surface.
const (
	AuditCtxTarget = "target"
	AuditCtxBefore = "before"
	AuditCtxAfter  = "after"
)

// auditTarget returns the canonical target string for a promote-reads mutation
// on the given component type. Centralised so the format is decided in one
// place and tests can assert against it.
func auditTarget(componentType string) string {
	return "read_promotion:" + componentType
}

// Repository is the read+upsert surface on agent_read_promotions.
//
// Reads are direct (the policy engine calls IsPromoted/ComponentType live per
// decision). The upsert is transactional — every write lives inside the
// mutation service's transaction so the value row and its audit row commit
// together. A nil tx on UpsertTx / ReadEnabledTx is a programming error.
type Repository interface {
	// IsPromoted reports whether the given component type has
	// auto_promote_reads ON. An absent row reports false (the OFF default).
	IsPromoted(ctx context.Context, componentType string) (bool, error)
	// List returns the enabled bit for every component type that has a row.
	// Types with no row are absent from the map and are OFF by default; the
	// HTTP layer composes the full per-type view over the authoritative enum.
	List(ctx context.Context) (map[string]bool, error)
	// ComponentType resolves a componentID to its component type via a single
	// PK query against the components table. Returns ("", nil) when no such
	// component exists, so the caller (the policy engine) can fail closed on a
	// missing/unknown id without a sentinel error. This is the engine's
	// component-type resolution seam.
	ComponentType(ctx context.Context, componentID string) (string, error)

	// ReadEnabledTx returns the stored enabled bit for one type inside the
	// caller's transaction (absent row => false), used by the mutation service
	// to capture the audit "before" value.
	ReadEnabledTx(ctx context.Context, tx *sql.Tx, componentType string) (bool, error)
	// UpsertTx writes the enabled bit for one type inside the caller's
	// transaction (insert-or-update on the component_type primary key).
	UpsertTx(ctx context.Context, tx *sql.Tx, componentType string, enabled bool, now time.Time) error

	// DB exposes the underlying handle so the mutation service can BeginTx
	// against it. Same narrow seam llmsettings.Repository.DB uses.
	DB() *sql.DB
}

// NewRepository builds the SQL-backed Repository.
func NewRepository(db *sql.DB, driver string) Repository {
	return &sqlRepository{db: db, driver: driver}
}

type sqlRepository struct {
	db     *sql.DB
	driver string
}

func (r *sqlRepository) DB() *sql.DB { return r.db }

func (r *sqlRepository) IsPromoted(ctx context.Context, componentType string) (bool, error) {
	var enabled int
	err := r.db.QueryRowContext(ctx, store.Rebind(r.driver,
		`SELECT enabled FROM agent_read_promotions WHERE component_type = ?`),
		componentType,
	).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("promotereads: read enabled %q: %w", componentType, err)
	}
	return enabled != 0, nil
}

func (r *sqlRepository) List(ctx context.Context) (map[string]bool, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT component_type, enabled FROM agent_read_promotions`)
	if err != nil {
		return nil, fmt.Errorf("promotereads: list: %w", err)
	}
	defer rows.Close()
	out := make(map[string]bool)
	for rows.Next() {
		var ct string
		var enabled int
		if err := rows.Scan(&ct, &enabled); err != nil {
			return nil, fmt.Errorf("promotereads: scan: %w", err)
		}
		out[ct] = enabled != 0
	}
	return out, rows.Err()
}

func (r *sqlRepository) ComponentType(ctx context.Context, componentID string) (string, error) {
	var t string
	err := r.db.QueryRowContext(ctx, store.Rebind(r.driver,
		`SELECT type FROM components WHERE id = ?`),
		componentID,
	).Scan(&t)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("promotereads: resolve component type %q: %w", componentID, err)
	}
	return t, nil
}

func (r *sqlRepository) ReadEnabledTx(ctx context.Context, tx *sql.Tx, componentType string) (bool, error) {
	if tx == nil {
		return false, fmt.Errorf("%w: nil transaction", ErrWriteFailed)
	}
	var enabled int
	err := tx.QueryRowContext(ctx, store.Rebind(r.driver,
		`SELECT enabled FROM agent_read_promotions WHERE component_type = ?`),
		componentType,
	).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("promotereads: read enabled (tx) %q: %w", componentType, err)
	}
	return enabled != 0, nil
}

func (r *sqlRepository) UpsertTx(ctx context.Context, tx *sql.Tx, componentType string, enabled bool, now time.Time) error {
	if tx == nil {
		return fmt.Errorf("%w: nil transaction", ErrWriteFailed)
	}
	bit := 0
	if enabled {
		bit = 1
	}
	_, err := tx.ExecContext(ctx, store.Rebind(r.driver, `
		INSERT INTO agent_read_promotions (component_type, enabled, last_modified)
		VALUES (?, ?, ?)
		ON CONFLICT(component_type) DO UPDATE SET
			enabled       = excluded.enabled,
			last_modified = excluded.last_modified`),
		componentType, bit, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("%w: upsert %q: %v", ErrWriteFailed, componentType, err)
	}
	return nil
}

// MutationService is the SOLE write path for the agent_read_promotions table.
// SetPromoted opens a transaction, reads the prior enabled bit inside it,
// upserts the new value, and writes one admin-access audit row through
// audit.Repository.InsertTx against the SAME transaction. On any error the
// transaction rolls back: NEITHER the value row NOR the audit row persists.
//
// Mirrors llmsettings.MutationService.runMutation; the audit row carries the
// same {target, before, after} context shape as the rest of the admin surface.
type MutationService struct {
	repo  Repository
	audit audit.Repository
	now   func() time.Time
}

// NewMutationService builds the mutation service. auditRepo is required — every
// mutation writes an audit row, and the service refuses to construct without a
// sink (a nil sink would silently drop the forensic trail and defeat the
// atomicity contract).
func NewMutationService(repo Repository, auditRepo audit.Repository) *MutationService {
	return &MutationService{repo: repo, audit: auditRepo, now: time.Now}
}

// WithClock overrides the time source. Used by tests for a deterministic
// last_modified stamp.
func (s *MutationService) WithClock(now func() time.Time) *MutationService {
	s.now = now
	return s
}

// Repo exposes the underlying repository for READ-ONLY callers (the GET
// handler). Writes MUST go through SetPromoted, which commits the value AND the
// audit row in one transaction.
func (s *MutationService) Repo() Repository { return s.repo }

// SetPromoted persists the new enabled bit for one component type and writes
// the audit row atomically against the same transaction. The caller is
// responsible for having validated componentType against the authoritative
// component-type enum (internal/store.IsValidComponentType) at the HTTP
// boundary; the service does not re-validate the key here, matching the
// llmsettings setters' boundary-validation convention.
func (s *MutationService) SetPromoted(ctx context.Context, componentType string, enabled bool) (err error) {
	tx, err := s.repo.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: begin tx: %v", ErrWriteFailed, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	before, rerr := s.repo.ReadEnabledTx(ctx, tx, componentType)
	if rerr != nil {
		err = rerr
		return err
	}
	if werr := s.repo.UpsertTx(ctx, tx, componentType, enabled, s.now()); werr != nil {
		err = werr
		return err
	}

	blob, marshalErr := json.Marshal(map[string]any{
		AuditCtxTarget: auditTarget(componentType),
		AuditCtxBefore: before,
		AuditCtxAfter:  enabled,
	})
	if marshalErr != nil {
		err = fmt.Errorf("%w: marshal audit context: %v", ErrWriteFailed, marshalErr)
		return err
	}

	auditErr := s.audit.InsertTx(ctx, tx, audit.Event{
		Principal: string(rbac.PrincipalFromContext(ctx)),
		Action:    audit.ActionAdminReadPromoteSet,
		Decision:  audit.DecisionAllow,
		Reason:    "admin_mutation",
		Kind:      audit.KindAdminAccess,
		Context:   string(blob),
	})
	if auditErr != nil {
		err = fmt.Errorf("%w: audit insert: %v", ErrWriteFailed, auditErr)
		return err
	}

	if cErr := tx.Commit(); cErr != nil {
		err = fmt.Errorf("%w: commit: %v", ErrWriteFailed, cErr)
		return err
	}
	return nil
}
