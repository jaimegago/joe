// Package readposture owns the durable install-wide read posture
// (read-posture-latch) and its single audited write path.
//
// The posture is a global scalar with two values:
//
//   - PostureTeamFlat ("team_flat") — any authenticated principal may read any
//     component, regardless of grant. The launch default.
//   - PostureZoned ("zoned") — the grant-based read decision (the full-mode read
//     path), byte-identical to the pre-posture zone+grant behaviour.
//
// The RBAC policy engine consults it as a DYNAMIC READ ADMIT, resolved LIVE per
// decision (no cache): under team_flat the engine admits any authenticated
// principal for ActionRead on any component the resolved zone already permits to
// be read. The posture is read-only by construction — the engine consults it for
// ActionRead exclusively, so it can never affect a mutate; the write floor and
// write-RBAC govern mutates independently. The engine reads through this
// package's Repository (which satisfies rbac.ReadPostureResolver); it never
// writes.
//
// Default semantics: the read_posture table (migration 028) is SEEDED with one
// singleton row holding 'team_flat'. ReadPosture returns 'team_flat' both when
// it reads that seed AND, defensively, when the row is somehow absent — so a
// fresh install and an install upgraded from a pre-posture build both behave as
// team_flat until an operator flips to zoned.
//
// Two pieces fit together here, mirroring the promotereads (CC-04) and
// llmsettings (Stream G) stacks:
//
//  1. Repository — the live read (ReadPosture) the engine calls per decision and
//     the transactional read/write (ReadPostureTx / SetPostureTx) the mutation
//     service uses inside its transaction.
//
//  2. MutationService — the SOLE write path. SetPosture runs ONE transaction: it
//     reads the prior posture, writes the new value, and writes one
//     KindAdminAccess audit row against the SAME transaction via
//     audit.Repository.InsertTx. Either both rows commit or neither does
//     (fail-closed). This mirrors promotereads.MutationService.SetPromoted and
//     uses the same audit substrate as the rest of the admin surface.
package readposture

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

// Posture values. Kept here as the package's canonical strings; they are the
// SAME literals the rbac engine compares against (rbac.PostureTeamFlat /
// rbac.PostureZoned) and the migration 028 CHECK pins. A guard test asserts the
// two sets stay in sync, the same convention audit/rbac use for their shared
// string vocabulary.
const (
	PostureTeamFlat = "team_flat"
	PostureZoned    = "zoned"
)

// IsValidPosture reports whether s is one of the two recognised posture values.
// The admin setter calls this at the HTTP boundary so an unknown posture is
// rejected (4xx) before any write reaches the table — the same boundary-
// validation convention the read-promotions setter uses for component types.
func IsValidPosture(s string) bool {
	return s == PostureTeamFlat || s == PostureZoned
}

// ErrWriteFailed wraps a lower-level error so callers can identify a read-posture
// write failure without depending on the driver error shape. Tests assert via
// errors.Is.
var ErrWriteFailed = errors.New("readposture: write failed")

// Audit context-key vocabulary. Identical {target, before, after} shape the
// llmsettings mutations, the RBAC admin rows, and the promotereads mutations
// use, so any later admin reader of the audit_log sees one canonical context
// shape across the whole admin surface.
const (
	AuditCtxTarget = "target"
	AuditCtxBefore = "before"
	AuditCtxAfter  = "after"
)

// auditTarget is the canonical target string for a read-posture mutation. There
// is one global posture, so the target is constant — centralised here so the
// format is decided in one place and tests can assert against it.
const auditTarget = "read_posture"

// Repository is the read+write surface on the read_posture singleton row.
//
// ReadPosture is the live read the policy engine calls per decision; it
// satisfies rbac.ReadPostureResolver directly (same name, same signature). The
// write is transactional — it lives inside the mutation service's transaction so
// the value row and its audit row commit together. A nil tx on the Tx methods is
// a programming error.
type Repository interface {
	// ReadPosture returns the current install-wide read posture, resolved live
	// (no cache). An absent row reports PostureTeamFlat (the launch default),
	// matching the migration-028 seed so a never-flipped install reads team_flat.
	ReadPosture(ctx context.Context) (string, error)

	// ReadPostureTx returns the stored posture inside the caller's transaction
	// (absent row => PostureTeamFlat), used by the mutation service to capture
	// the audit "before" value.
	ReadPostureTx(ctx context.Context, tx *sql.Tx) (string, error)
	// SetPostureTx writes the posture for the singleton row inside the caller's
	// transaction (upsert on the fixed id = 1 primary key).
	SetPostureTx(ctx context.Context, tx *sql.Tx, posture string, now time.Time) error

	// DB exposes the underlying handle so the mutation service can BeginTx
	// against it. Same narrow seam llmsettings.Repository.DB / promotereads use.
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

func (r *sqlRepository) ReadPosture(ctx context.Context) (string, error) {
	var posture string
	err := r.db.QueryRowContext(ctx,
		`SELECT posture FROM read_posture WHERE id = 1`).Scan(&posture)
	if errors.Is(err, sql.ErrNoRows) {
		return PostureTeamFlat, nil
	}
	if err != nil {
		return "", fmt.Errorf("readposture: read posture: %w", err)
	}
	return posture, nil
}

func (r *sqlRepository) ReadPostureTx(ctx context.Context, tx *sql.Tx) (string, error) {
	if tx == nil {
		return "", fmt.Errorf("%w: nil transaction", ErrWriteFailed)
	}
	var posture string
	err := tx.QueryRowContext(ctx,
		`SELECT posture FROM read_posture WHERE id = 1`).Scan(&posture)
	if errors.Is(err, sql.ErrNoRows) {
		return PostureTeamFlat, nil
	}
	if err != nil {
		return "", fmt.Errorf("readposture: read posture (tx): %w", err)
	}
	return posture, nil
}

func (r *sqlRepository) SetPostureTx(ctx context.Context, tx *sql.Tx, posture string, now time.Time) error {
	if tx == nil {
		return fmt.Errorf("%w: nil transaction", ErrWriteFailed)
	}
	_, err := tx.ExecContext(ctx, store.Rebind(r.driver, `
		INSERT INTO read_posture (id, posture, last_modified)
		VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			posture       = excluded.posture,
			last_modified = excluded.last_modified`),
		posture, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("%w: upsert posture %q: %v", ErrWriteFailed, posture, err)
	}
	return nil
}

// MutationService is the SOLE write path for the read_posture row. SetPosture
// opens a transaction, reads the prior posture inside it, writes the new value,
// and writes one admin-access audit row through audit.Repository.InsertTx
// against the SAME transaction. On any error the transaction rolls back: NEITHER
// the value row NOR the audit row persists.
//
// Mirrors promotereads.MutationService.SetPromoted; the audit row carries the
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
// handler). Writes MUST go through SetPosture, which commits the value AND the
// audit row in one transaction.
func (s *MutationService) Repo() Repository { return s.repo }

// SetPosture persists the new install-wide read posture and writes the audit row
// atomically against the same transaction. The caller is responsible for having
// validated posture against IsValidPosture at the HTTP boundary; the service
// does not re-validate here, matching the read-promotions / llmsettings setters'
// boundary-validation convention.
func (s *MutationService) SetPosture(ctx context.Context, posture string) (err error) {
	tx, err := s.repo.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: begin tx: %v", ErrWriteFailed, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	before, rerr := s.repo.ReadPostureTx(ctx, tx)
	if rerr != nil {
		err = rerr
		return err
	}
	if werr := s.repo.SetPostureTx(ctx, tx, posture, s.now()); werr != nil {
		err = werr
		return err
	}

	blob, marshalErr := json.Marshal(map[string]any{
		AuditCtxTarget: auditTarget,
		AuditCtxBefore: before,
		AuditCtxAfter:  posture,
	})
	if marshalErr != nil {
		err = fmt.Errorf("%w: marshal audit context: %v", ErrWriteFailed, marshalErr)
		return err
	}

	auditErr := s.audit.InsertTx(ctx, tx, audit.Event{
		Principal: string(rbac.PrincipalFromContext(ctx)),
		Action:    audit.ActionAdminReadPostureSet,
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
