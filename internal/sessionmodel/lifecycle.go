package sessionmodel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/store"
)

// lifecycle.go implements the §12.5 retention/lifecycle transition store
// (DESIGN-CHAT-SESSIONS.md §12.5, ledger node B007a). Every transition writes
// EXACTLY the migration-025 lifecycle columns the design specifies and preserves
// the invariant "active = all six lifecycle columns null". The manual owner/admin
// transitions reuse the same state writes the B007b sweeper will apply — there
// are no divergent code paths. The *Tx variants run on a caller-supplied
// transaction so the effect and its audit row commit atomically (mutateWithAudit).

// --- Trash (soft-delete) ---

func (r *SQLRepository) TrashSession(ctx context.Context, id, by string, purgeAfter *time.Time) error {
	return r.trashExec(ctx, r.db, id, by, purgeAfter)
}

func (r *SQLRepository) TrashSessionTx(ctx context.Context, tx *sql.Tx, id, by string, purgeAfter *time.Time) error {
	return r.trashExec(ctx, tx, id, by, purgeAfter)
}

// trashExec sets trashed_at=now, trashed_by, purge_after on an ACTIVE session.
// The WHERE clause matches only a row that is not already trashed, so a
// double-trash affects zero rows and returns ErrSessionAlreadyTrashed. It does
// not touch last_activity_at (trashing is not chat activity).
func (r *SQLRepository) trashExec(ctx context.Context, exec sqlExecer, id, by string, purgeAfter *time.Time) error {
	res, err := exec.ExecContext(ctx, store.Rebind(r.driver, `
		UPDATE agent_sessions
		SET trashed_at = ?, trashed_by = ?, purge_after = ?
		WHERE id = ? AND trashed_at IS NULL`),
		time.Now().UTC().Format(time.RFC3339), by, timePtrArg(purgeAfter), id)
	if err != nil {
		return fmt.Errorf("trash session: %w", err)
	}
	return requireOneRow(res, ErrSessionAlreadyTrashed, "trash session")
}

// --- Restore ---

func (r *SQLRepository) RestoreSession(ctx context.Context, id string) error {
	return r.restoreExec(ctx, r.db, id)
}

func (r *SQLRepository) RestoreSessionTx(ctx context.Context, tx *sql.Tx, id string) error {
	return r.restoreExec(ctx, tx, id)
}

// restoreExec clears the trash columns, returning a trashed session to active.
// The WHERE clause matches only a currently-trashed row, so restoring a
// non-trashed session affects zero rows and returns ErrSessionNotTrashed.
func (r *SQLRepository) restoreExec(ctx context.Context, exec sqlExecer, id string) error {
	res, err := exec.ExecContext(ctx, store.Rebind(r.driver, `
		UPDATE agent_sessions
		SET trashed_at = NULL, trashed_by = NULL, purge_after = NULL
		WHERE id = ? AND trashed_at IS NOT NULL`), id)
	if err != nil {
		return fmt.Errorf("restore session: %w", err)
	}
	return requireOneRow(res, ErrSessionNotTrashed, "restore session")
}

// --- Purge (governed expunge) ---

func (r *SQLRepository) PurgeManifest(ctx context.Context, id string) (PurgeManifest, error) {
	var m PurgeManifest
	err := r.db.QueryRowContext(ctx, store.Rebind(r.driver, `
		SELECT
			(SELECT COUNT(*) FROM chat_messages   WHERE session_id = ?),
			(SELECT COUNT(*) FROM agent_sessions  WHERE linked_incident_id = ?)`),
		id, id).Scan(&m.MessageCount, &m.LinkedChildCount)
	if err != nil {
		return PurgeManifest{}, fmt.Errorf("purge manifest: %w", err)
	}
	return m, nil
}

// PurgeSessionTx hard-deletes the session on the caller transaction. The schema
// carries the rest: chat_messages and captain bindings cascade (ON DELETE
// CASCADE); linked children's linked_incident_id is severed (ON DELETE SET NULL)
// — they survive as plain 'default' sessions (§12.4). This is the only
// route-reachable hard delete; the raw DeleteSession is no longer wired to any
// ungoverned route.
func (r *SQLRepository) PurgeSessionTx(ctx context.Context, tx *sql.Tx, id string) error {
	if _, err := tx.ExecContext(ctx, store.Rebind(r.driver, `
		DELETE FROM agent_sessions WHERE id = ?`), id); err != nil {
		return fmt.Errorf("purge session: %w", err)
	}
	return nil
}

// --- Trash listing ---

func (r *SQLRepository) ListTrashedSessions(ctx context.Context, principal *string, limit int) ([]ChatSessionRow, error) {
	query := `
		SELECT ` + sessionColumnsPrefixed + `,
		       COUNT(m.id) AS message_count
		FROM agent_sessions s
		LEFT JOIN chat_messages m ON m.session_id = s.id
		WHERE s.trashed_at IS NOT NULL`
	args := []any{}
	if principal != nil {
		query += "\n\t\tAND s.creator_principal = ?"
		args = append(args, *principal)
	}
	query += "\n\t\tGROUP BY s.id\n\t\tORDER BY s.trashed_at DESC"
	if limit > 0 {
		query += "\n\t\tLIMIT ?"
		args = append(args, limit)
	}

	rows, err := r.db.QueryContext(ctx, store.Rebind(r.driver, query), args...)
	if err != nil {
		return nil, fmt.Errorf("list trashed sessions: %w", err)
	}
	defer rows.Close()

	var out []ChatSessionRow
	for rows.Next() {
		var count int
		s, err := scanSession(func(dest ...any) error {
			return rows.Scan(append(dest, &count)...)
		})
		if err != nil {
			return nil, err
		}
		if s != nil {
			out = append(out, ChatSessionRow{AgentSession: *s, MessageCount: count})
		}
	}
	return out, rows.Err()
}

// --- Retention policy (§12.5, migration 026) ---

func (r *SQLRepository) GetRetentionPolicy(ctx context.Context) (*RetentionPolicy, error) {
	var (
		inactivityDays sql.NullInt64
		trashGraceDays int
		terminalAction string
		updatedAt      sql.NullString
		updatedBy      sql.NullString
	)
	err := r.db.QueryRowContext(ctx, store.Rebind(r.driver, `
		SELECT inactivity_days, trash_grace_days, terminal_action, updated_at, updated_by
		FROM session_retention_policy WHERE id = 1`)).
		Scan(&inactivityDays, &trashGraceDays, &terminalAction, &updatedAt, &updatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get retention policy: %w", err)
	}
	p := &RetentionPolicy{
		TrashGraceDays: trashGraceDays,
		TerminalAction: TerminalAction(terminalAction),
	}
	if inactivityDays.Valid {
		d := int(inactivityDays.Int64)
		p.InactivityDays = &d
	}
	if updatedAt.Valid {
		p.UpdatedAt = parseTimeOrNil(updatedAt.String)
	}
	if updatedBy.Valid {
		p.UpdatedBy = &updatedBy.String
	}
	return p, nil
}

func (r *SQLRepository) SetRetentionPolicyTx(ctx context.Context, tx *sql.Tx, p RetentionPolicy, by string, when time.Time) error {
	if when.IsZero() {
		when = time.Now().UTC()
	}
	var inactivity any
	if p.InactivityDays != nil {
		inactivity = *p.InactivityDays
	}
	if _, err := tx.ExecContext(ctx, store.Rebind(r.driver, `
		UPDATE session_retention_policy
		SET inactivity_days = ?, trash_grace_days = ?, terminal_action = ?,
		    updated_at = ?, updated_by = ?
		WHERE id = 1`),
		inactivity, p.TrashGraceDays, string(p.TerminalAction),
		when.Format(time.RFC3339), by); err != nil {
		return fmt.Errorf("set retention policy: %w", err)
	}
	return nil
}

// ResolveRetention resolves a session against the active policy (§12.4). With one
// global policy in v1, a session resolves to that policy's terminal action as its
// class. A missing session resolves to ErrNotFound so callers can distinguish it
// from a clean resolution.
func (r *SQLRepository) ResolveRetention(ctx context.Context, sessionID string) (*RetentionResolution, error) {
	sess, err := r.GetSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("resolve retention: %w", err)
	}
	if sess == nil {
		return nil, ErrNotFound
	}
	p, err := r.GetRetentionPolicy(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve retention: %w", err)
	}
	return &RetentionResolution{
		Class:          string(p.TerminalAction),
		InactivityDays: p.InactivityDays,
		TrashGraceDays: p.TrashGraceDays,
		TerminalAction: p.TerminalAction,
	}, nil
}

func (r *SQLRepository) StampRetentionClass(ctx context.Context, sessionID, class string) error {
	res, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		UPDATE agent_sessions SET retention_class = ? WHERE id = ?`), class, sessionID)
	if err != nil {
		return fmt.Errorf("stamp retention class: %w", err)
	}
	return requireOneRow(res, ErrNotFound, "stamp retention class")
}

// requireOneRow turns a guarded UPDATE that matched no row into notFoundErr, so a
// precondition miss (already-trashed, not-trashed, missing) surfaces as a typed
// error rather than a silent no-op. A driver that does not report RowsAffected
// (returns an error) is treated as success to avoid false negatives.
func requireOneRow(res sql.Result, notFoundErr error, op string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return nil
	}
	if n == 0 {
		return fmt.Errorf("%s: %w", op, notFoundErr)
	}
	return nil
}
