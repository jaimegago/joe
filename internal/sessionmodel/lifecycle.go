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

// --- Archive (move to cold storage) ---

func (r *SQLRepository) ArchiveSession(ctx context.Context, id, by, ref string, expectedMsgs int) error {
	return r.archiveExec(ctx, r.db, id, by, ref, expectedMsgs)
}

func (r *SQLRepository) ArchiveSessionTx(ctx context.Context, tx *sql.Tx, id, by, ref string, expectedMsgs int) error {
	return r.archiveExec(ctx, tx, id, by, ref, expectedMsgs)
}

// archiveExec performs the §12.6 archive STATE transition: it stamps
// archived_at=now / archived_by / archive_ref (the provider-produced locator) on
// a not-yet-archived session, THEN removes the hot transcript rows — the MOVE to
// cold storage, after which the artifact is the sole copy and the normal read
// path (ListChatMessages) returns nothing for this session until restore. The
// guarded UPDATE matches only an un-archived row, so re-archiving affects zero
// rows and returns ErrSessionAlreadyArchived BEFORE any transcript is removed.
// It touches only agent_sessions and chat_messages — never the legacy tables.
//
// MID-WINDOW message-loss guard (§12.6). The transcript is read
// (Archiver.Archive → ListChatMessages) and serialized into the artifact BEFORE
// this DELETE runs, and the two are not in one transaction (the file write cannot
// be). A chat_messages row inserted for this session AFTER the artifact read but
// BEFORE this DELETE commits would be captured by neither the artifact nor the
// surviving hot rows — it would be silently destroyed. expectedMsgs is the
// transcript length the caller serialized into the artifact; this transition
// RE-COUNTS chat_messages inside the same transaction and, on a mismatch, writes
// nothing and returns ErrArchiveTranscriptChanged so the caller's transaction
// rolls back (destroying nothing) and it may re-read the transcript and retry. The
// count read + column stamp + transcript delete all run on the same executor, so
// the guard is atomic with the move.
func (r *SQLRepository) archiveExec(ctx context.Context, exec sqlExecer, id, by, ref string, expectedMsgs int) error {
	// Re-count the live transcript in this transaction. If it no longer matches
	// what the caller serialized, a message landed (or vanished) since the
	// artifact was built — refuse and destroy nothing.
	var actual int
	if err := exec.QueryRowContext(ctx, store.Rebind(r.driver, `
		SELECT COUNT(*) FROM chat_messages WHERE session_id = ?`), id).Scan(&actual); err != nil {
		return fmt.Errorf("archive session: count transcript: %w", err)
	}
	if actual != expectedMsgs {
		return fmt.Errorf("archive session: %w (artifact has %d messages, live transcript has %d)",
			ErrArchiveTranscriptChanged, expectedMsgs, actual)
	}

	res, err := exec.ExecContext(ctx, store.Rebind(r.driver, `
		UPDATE agent_sessions
		SET archived_at = ?, archived_by = ?, archive_ref = ?
		WHERE id = ? AND archived_at IS NULL`),
		time.Now().UTC().Format(time.RFC3339), by, ref, id)
	if err != nil {
		return fmt.Errorf("archive session: %w", err)
	}
	if err := requireOneRow(res, ErrSessionAlreadyArchived, "archive session"); err != nil {
		return err
	}
	// Move: the artifact now holds the transcript, so the hot rows leave
	// chat_messages. This runs in the same transaction as the column stamp (and
	// the caller's audit row), so a rollback restores both the columns and the
	// rows together.
	if _, err := exec.ExecContext(ctx, store.Rebind(r.driver, `
		DELETE FROM chat_messages WHERE session_id = ?`), id); err != nil {
		return fmt.Errorf("archive session: move transcript: %w", err)
	}
	return nil
}

// --- Unarchive (restore from cold storage) ---

func (r *SQLRepository) UnarchiveSession(ctx context.Context, id string) error {
	return r.unarchiveExec(ctx, r.db, id)
}

func (r *SQLRepository) UnarchiveSessionTx(ctx context.Context, tx *sql.Tx, id string) error {
	return r.unarchiveExec(ctx, tx, id)
}

// unarchiveExec clears the archive columns, returning an archived session to
// active. The caller (the archive provider's Restore) rebuilds the hot transcript
// rows from the artifact in the SAME transaction via InsertChatMessageTx. The
// guarded UPDATE matches only a currently-archived row, so unarchiving a
// non-archived session affects zero rows and returns ErrSessionNotArchived.
func (r *SQLRepository) unarchiveExec(ctx context.Context, exec sqlExecer, id string) error {
	res, err := exec.ExecContext(ctx, store.Rebind(r.driver, `
		UPDATE agent_sessions
		SET archived_at = NULL, archived_by = NULL, archive_ref = NULL
		WHERE id = ? AND archived_at IS NOT NULL`), id)
	if err != nil {
		return fmt.Errorf("unarchive session: %w", err)
	}
	return requireOneRow(res, ErrSessionNotArchived, "unarchive session")
}

// InsertChatMessageTx rebuilds ONE transcript row verbatim on a caller
// transaction — the restore-from-archive write path. Unlike AddChatMessage it
// does NOT compute the next seq or bump last_activity_at: it preserves the
// artifact's recorded seq (so ordering is exact) and leaves recency alone (a
// restore is not new chat activity). It writes ONLY to chat_messages (the live
// transcript table) — never the legacy session_messages table.
func (r *SQLRepository) InsertChatMessageTx(ctx context.Context, tx *sql.Tx, m ChatMessage) error {
	if m.ID == "" || m.SessionID == "" {
		return fmt.Errorf("insert chat message: id and session_id required")
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	if _, err := tx.ExecContext(ctx, store.Rebind(r.driver, `
		INSERT INTO chat_messages
			(id, session_id, seq, role, content, tool_name, tool_args, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`),
		m.ID, m.SessionID, m.Seq, m.Role, m.Content,
		strPtrArg(m.ToolName), strPtrArg(m.ToolArgs),
		m.CreatedAt.Format(time.RFC3339)); err != nil {
		return fmt.Errorf("insert chat message: %w", err)
	}
	return nil
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

// --- Sweeper scan queries (§12.5, B007b) ---

// ListSessionsInactiveBefore returns active sessions older than cutoff (§12.5
// inactivity expiry). "Active" is the canonical all-six-lifecycle-columns-null
// predicate; the query also excludes incident-typed sessions defensively — an
// active incident master is governed by the regime lifecycle, not auto-expired
// by inactivity. It reads ONLY agent_sessions (legacy tables untouched, §13).
func (r *SQLRepository) ListSessionsInactiveBefore(ctx context.Context, cutoff time.Time) ([]AgentSession, error) {
	rows, err := r.db.QueryContext(ctx, store.Rebind(r.driver, `
		SELECT `+sessionColumns+`
		FROM agent_sessions
		WHERE last_activity_at < ?
		  AND type <> 'incident'
		  AND trashed_at IS NULL AND archived_at IS NULL
		  AND trashed_by IS NULL AND purge_after IS NULL
		  AND archived_by IS NULL AND archive_ref IS NULL
		ORDER BY last_activity_at ASC`),
		cutoff.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("list inactive sessions: %w", err)
	}
	defer rows.Close()
	return scanSessionRows(rows)
}

// ListPurgeableSessions returns trashed sessions whose purge_after deadline has
// passed (§12.5 trash-grace purge). It reads ONLY agent_sessions (legacy tables
// untouched, §13).
func (r *SQLRepository) ListPurgeableSessions(ctx context.Context, now time.Time) ([]AgentSession, error) {
	rows, err := r.db.QueryContext(ctx, store.Rebind(r.driver, `
		SELECT `+sessionColumns+`
		FROM agent_sessions
		WHERE trashed_at IS NOT NULL
		  AND purge_after IS NOT NULL
		  AND purge_after <= ?
		ORDER BY purge_after ASC`),
		now.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("list purgeable sessions: %w", err)
	}
	defer rows.Close()
	return scanSessionRows(rows)
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
