package sessionmodel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/store"
)

// Repository is the durable interface for the session model. Phase 1 keeps
// this minimal — just enough for the schema to be exercised and for later
// changes (HTTP API, regime declare/resolve, captain state machine) to
// extend. The §B captain state-machine semantics live in Change 6 on top of
// this primitive; the §R declare/resolve handlers live in Change 5.
type Repository interface {
	// Sessions

	CreateSession(ctx context.Context, s AgentSession) (*AgentSession, error)
	GetSession(ctx context.Context, id string) (*AgentSession, error)
	ListSessions(ctx context.Context) ([]AgentSession, error)
	ListSessionsByType(ctx context.Context, t SessionType) ([]AgentSession, error)
	// ListSessionsByCreator is the caller-scoped chat list — the `mine` filter on
	// the per-user GET /api/v1/sessions surface (§12.8): only the caller's own
	// sessions, newest activity first, with a per-session message count. Distinct
	// from ListRecentSessions, which is the team-wide (unfiltered) list.
	ListSessionsByCreator(ctx context.Context, principal string, limit int) ([]ChatSessionRow, error)
	// ListRecentSessions is the team-wide chat list (DESIGN-CHAT-SESSIONS.md §12.8,
	// team-wide read amendment 2026-06-21): every ACTIVE session (trashed_at and
	// archived_at both null — §12.10 removes trashed sessions from the normal team
	// list), newest activity first, with a per-session message count and no
	// principal predicate. It backs the
	// per-user GET /api/v1/sessions surface (the default, unfiltered view); the
	// caller-scoped "mine" view uses ListSessionsByCreator. Each row carries
	// creator_principal so the handler can stamp read_only per row (a non-owner is
	// read-only on a session it does not own).
	ListRecentSessions(ctx context.Context, limit int) ([]ChatSessionRow, error)
	// ListSessionsByOthers is the "shared with you" list: every session owned by
	// a principal *other* than the caller, newest activity first, with a
	// per-session message count. In the org-wide read model every session is
	// readable by any authenticated user, so this lists all of them (no
	// visibility filter). The owner (creator_principal) is carried on each row so
	// the UI can label it "shared by <owner>".
	ListSessionsByOthers(ctx context.Context, principal string, limit int) ([]ChatSessionRow, error)
	// DeleteSession removes one session by ID. The schema's ON DELETE CASCADE
	// FKs (linked investigations via the self-FK, plus child tables landed in
	// Changes 2/3) carry the §5b-5 expunge downward. This is a single SQL
	// statement (no application-level fan-out).
	DeleteSession(ctx context.Context, id string) error
	// UpdateSessionTitle sets a session's human-editable title (migration 022).
	// This is the MANUAL-RENAME path: the Web UI PATCH handler owner-checks
	// before calling, so this is an unconditional write by ID
	// (DESIGN-CHAT-SESSIONS.md §11 Phase 2). It does not bump last_activity_at —
	// a rename is metadata housekeeping, not chat activity, so it must not
	// reorder the recency-sorted browse list. The auto-title background path must
	// NOT use this method — it must use SetSessionTitleIfUnset (compare-and-set),
	// or it will clobber a concurrent manual rename.
	UpdateSessionTitle(ctx context.Context, id, title string) error
	// SetSessionTitleIfUnset is the AUTO-TITLE path's compare-and-set title
	// write: it sets the title ONLY if the session currently has no title
	// (title IS NULL), returning whether a row was affected (true = this call set
	// the title; false = the session was already titled). This closes the
	// check-then-act race between the auto-title background goroutine and a
	// concurrent manual rename: the auto-title goroutine reads Title==nil and then
	// writes, and a manual UpdateSessionTitle landing in that window would
	// otherwise be clobbered by the auto-title write, violating the documented
	// "user who renamed wins" invariant. Doing the null-check inside the UPDATE's
	// WHERE clause makes the read-and-write atomic at the SQL layer, so the
	// auto-title write is a no-op once any title (manual or prior auto) exists.
	// Like UpdateSessionTitle it does not bump last_activity_at.
	SetSessionTitleIfUnset(ctx context.Context, id, title string) (bool, error)
	// LinkSessionToIncident attaches a plain chat session to the active
	// incident by setting linked_incident_id ONLY (DESIGN-CHAT-SESSIONS.md
	// §12.3): under the two-type model participation is the pointer alone — there
	// is no type flip (the 'investigation' type was removed). The caller
	// owner-checks the session and resolves the active incident first, so this is
	// an unconditional write by ID. Like the title mutator it does not bump
	// last_activity_at (linkage is metadata, not chat activity, so it must not
	// reorder the recency-sorted browse list).
	LinkSessionToIncident(ctx context.Context, sessionID, incidentID string) error
	// ActiveIncidentSession returns the currently-active incident session
	// (type='incident', incident_state NOT IN ('resolved','reviewed')), or nil
	// when none is active. Phase 1 has at most one active incident by
	// construction (declare creates exactly one; resolve clears it), so the
	// most recently created is returned. The Web UI link-incident handler uses
	// it to resolve the link target.
	ActiveIncidentSession(ctx context.Context) (*AgentSession, error)

	// --- Lifecycle transitions (§12.5, B007a) ---
	//
	// Each transition writes EXACTLY the migration-025 lifecycle columns the
	// design specifies and preserves the invariant "active = all six lifecycle
	// columns null". The *Tx variants run the write on a caller-supplied
	// transaction so the effect and its §12.5 audit row commit atomically
	// (mutateWithAudit); the bare variants run standalone on the repo handle (for
	// store-level tests). The manual transitions reuse the SAME state writes the
	// sweeper (B007b) will apply — no divergent code paths.

	// TrashSession soft-deletes a session into trash (§12.5 macOS-trash): it sets
	// trashed_at=now, trashed_by, and purge_after (the trash-grace deadline, nil
	// for no auto-purge). It is the manual entry to the trashed state. Returns
	// ErrSessionAlreadyTrashed if the session is already trashed (the guarded
	// UPDATE matches only an active row). The session is NOT physically removed.
	TrashSession(ctx context.Context, id, by string, purgeAfter *time.Time) error
	// TrashSessionTx is TrashSession on a caller transaction (audited soft-delete).
	TrashSessionTx(ctx context.Context, tx *sql.Tx, id, by string, purgeAfter *time.Time) error
	// RestoreSession clears trashed_at/trashed_by/purge_after, returning a trashed
	// session to the active set (§12.5 restore). Returns ErrSessionNotTrashed if
	// the session is not currently trashed.
	RestoreSession(ctx context.Context, id string) error
	// RestoreSessionTx is RestoreSession on a caller transaction (audited restore).
	RestoreSessionTx(ctx context.Context, tx *sql.Tx, id string) error
	// PurgeManifest returns the §12.5 manifest-with-hard-stop counts (transcript
	// messages destroyed, linked children severed) for a prospective purge.
	PurgeManifest(ctx context.Context, id string) (PurgeManifest, error)
	// PurgeSessionTx is the GOVERNED expunge (§12.5 admin-only purge): a hard
	// DELETE of the session on the caller transaction. The schema carries the
	// effect — chat_messages + captain bindings cascade (ON DELETE CASCADE), and
	// linked children's linked_incident_id is SEVERED (ON DELETE SET NULL), never
	// destroyed (§12.4). This replaces the raw DeleteSession as the only
	// route-reachable hard delete; DeleteSession survives solely as this method's
	// non-transactional twin for store-level cascade tests.
	PurgeSessionTx(ctx context.Context, tx *sql.Tx, id string) error
	// ArchiveSession is the §12.6 archive STATE transition: it stamps
	// archived_at=now / archived_by / archive_ref (the provider-produced locator)
	// AND removes the hot transcript rows (the MOVE to cold storage — the artifact
	// becomes the sole copy). expectedMsgs is the transcript length the caller
	// serialized into the artifact: the transition re-counts chat_messages before
	// deleting and returns ErrArchiveTranscriptChanged (destroying nothing) when a
	// message landed between the artifact read and this write — otherwise that
	// message would be deleted unarchived. Returns ErrSessionAlreadyArchived if
	// already archived. The non-Tx variant is the store-level test twin.
	ArchiveSession(ctx context.Context, id, by, ref string, expectedMsgs int) error
	// ArchiveSessionTx is ArchiveSession on a caller transaction so the column
	// stamp, the transcript-count verification, the transcript move, and the
	// §12.6 audit row commit atomically — a count mismatch rolls the whole
	// transaction back and the caller may re-read and retry.
	ArchiveSessionTx(ctx context.Context, tx *sql.Tx, id, by, ref string, expectedMsgs int) error
	// UnarchiveSession clears the archive columns, returning an archived session
	// to active (§12.6 restore). The caller rebuilds the hot transcript from the
	// artifact. Returns ErrSessionNotArchived if not currently archived.
	UnarchiveSession(ctx context.Context, id string) error
	// UnarchiveSessionTx is UnarchiveSession on a caller transaction so the column
	// clear, the transcript rebuild, and the audit row commit atomically.
	UnarchiveSessionTx(ctx context.Context, tx *sql.Tx, id string) error
	// InsertChatMessageTx rebuilds one transcript row verbatim on a caller
	// transaction — the restore-from-archive write path. It preserves the
	// artifact's seq (exact ordering) and writes ONLY to chat_messages.
	InsertChatMessageTx(ctx context.Context, tx *sql.Tx, m ChatMessage) error
	// ListTrashedSessions lists trashed sessions (trashed_at NOT NULL), newest
	// trashed first, with a per-session message count. principal nil = all trash
	// (admin all-trash, §12.8); non-nil = that creator's own trash (per-user
	// list-own-trash, §12.8). limit <= 0 means no cap.
	ListTrashedSessions(ctx context.Context, principal *string, limit int) ([]ChatSessionRow, error)

	// --- Sweeper scan queries (§12.5, B007b) ---
	//
	// These are READ-ONLY scans the background sweeper uses to find expiration
	// candidates. They target ONLY agent_sessions — never the legacy migration-001
	// `sessions` / `session_messages` tables (§13 hard constraint). They return the
	// candidate rows; the sweeper applies the transition (trash / purge) per row,
	// each in its own effect+audit transaction, so a per-session failure cannot
	// corrupt a cross-session batch.

	// ListSessionsInactiveBefore returns ACTIVE sessions (all six lifecycle
	// columns null) whose last_activity_at is strictly before cutoff — the §12.5
	// inactivity-expiry candidates. The sweeper calls it ONLY when the inactivity
	// window is enabled (it is OFF / nil by default), passing cutoff = now -
	// inactivity_days. Oldest-activity first so the most-stale sessions are
	// processed first.
	ListSessionsInactiveBefore(ctx context.Context, cutoff time.Time) ([]AgentSession, error)
	// ListPurgeableSessions returns TRASHED sessions whose purge_after deadline has
	// passed (trashed_at NOT NULL AND purge_after NOT NULL AND purge_after <= now)
	// — the §12.5 trash-grace purge candidates. It is the second, unconditional
	// pass: it catches both sweeper-trashed sessions (under trash_then_purge) and
	// manually owner-trashed sessions past their grace deadline, regardless of the
	// policy terminal action. A trashed session with no purge_after (no auto-purge
	// deadline) is never returned. Oldest deadline first.
	ListPurgeableSessions(ctx context.Context, now time.Time) ([]AgentSession, error)

	// --- Retention policy (§12.5, migration 026, B007a) ---

	// GetRetentionPolicy returns the single admin retention policy (one row,
	// id=1; seeded by migration 026 with the §12.5 defaults).
	GetRetentionPolicy(ctx context.Context) (*RetentionPolicy, error)
	// SetRetentionPolicyTx writes the retention policy on a caller transaction
	// (audited configure_retention). It stamps updated_at/updated_by.
	SetRetentionPolicyTx(ctx context.Context, tx *sql.Tx, p RetentionPolicy, by string, when time.Time) error
	// ResolveRetention resolves a session against the active policy (§12.4): it
	// returns the per-session class (the policy's terminal action) plus the
	// effective knobs. The sweeper (B007b) reads this to decide a session's fate.
	ResolveRetention(ctx context.Context, sessionID string) (*RetentionResolution, error)
	// StampRetentionClass writes the resolved class onto a session's
	// retention_class column, making that column the live per-session resolution
	// of the policy (§12.4 "no longer inert").
	StampRetentionClass(ctx context.Context, sessionID, class string) error

	// Chat messages (interim flat store, migration 022)

	// AddChatMessage appends a message to a session's flat timeline and bumps
	// the session's last_activity_at. Used by the task handlers'
	// persistTaskMessages.
	AddChatMessage(ctx context.Context, m ChatMessage) (*ChatMessage, error)
	// ListChatMessages returns a session's messages in seq order. Used by the
	// owner-checked Web UI messages endpoint and the streaming task handler's
	// history seeding.
	ListChatMessages(ctx context.Context, sessionID string) ([]ChatMessage, error)

	// Regime

	GetRegime(ctx context.Context) (*Regime, error)
	// SetRegime updates the single-row system_regime. Used by Change 5's
	// declare/resolve handlers. Phase 1 does not enforce the R5
	// declare-may-auto / resolve-may-not asymmetry here — that is the
	// concern of the §R handlers (Changes 5 / 12) plus the AST guard.
	SetRegime(ctx context.Context, r Regime) error

	// Captains

	AttachCaptain(ctx context.Context, c Captain) (*Captain, error)
	GetActiveCaptain(ctx context.Context, sessionID string) (*Captain, error)
	ListCaptainsForSession(ctx context.Context, sessionID string) ([]Captain, error)

	// CurrentCaptainPrincipal returns the active captain's principal for
	// sessionID, plus an ok flag. ok=false means there is no active
	// captain — either the session never had one (incident regime
	// pending_captain state, §B2 null authority) or the captain was
	// detached without a successor.
	//
	// Required by the §B1 principal-threading rule: Change 10's executor
	// wrapper looks up the current captain principal here to substitute
	// it into the RBAC call. Stable getter for downstream consumers.
	CurrentCaptainPrincipal(ctx context.Context, sessionID string) (string, bool, error)

	// Captain reachability (§6-D NET-NEW)

	// RecordCaptainHeartbeat updates the active captain row's
	// last_seen_at if it matches the given principal. Returns
	// ErrCaptainPrincipalMismatch if the active captain's principal is
	// not the one calling — heartbeat is captain-bound, not session-bound.
	RecordCaptainHeartbeat(ctx context.Context, sessionID, principal string, when time.Time) error

	// IsCaptainReachable returns true iff the active captain's
	// last_seen_at is within thresholdSeconds of now. A session with
	// no active captain returns (false, ErrNoActiveCaptain).
	//
	// Used by CaptainService.BeginTransfer (§B3) to decide between the
	// incoming-initiated branches: reachable -> ask outgoing (decision
	// solicitation), unreachable -> direct transfer_confirmed.
	IsCaptainReachable(ctx context.Context, sessionID string, thresholdSeconds int) (bool, error)

	// MarkCaptainDetached sets detached_at on the captain row and
	// clears transfer_state. Used by CaptainService.ConfirmTransfer to
	// step the state machine forward.
	MarkCaptainDetached(ctx context.Context, captainID string, when time.Time) error

	// SwapCaptain atomically detaches the outgoing captain (by id) and
	// attaches `incoming` as the new active captain, both inside a single
	// transaction. Either both writes commit or neither does, so there is
	// never a committed state in which the outgoing captain is detached but
	// no new captain is attached (D-0025). Used by
	// CaptainService.completeTransfer — the transfer-swap path that
	// MarkCaptainDetached + AttachCaptain previously performed as two
	// independent, non-atomic writes.
	SwapCaptain(ctx context.Context, outgoingID string, incoming Captain, when time.Time) error

	// UpdateCaptainTransferState updates the transfer state and
	// incoming-side metadata on the active captain row. Used by
	// CaptainService.BeginTransfer / CancelTransfer.
	UpdateCaptainTransferState(ctx context.Context, captainID string, state *TransferState, incomingPrincipal *string, initiator *TransferInitiator) error

	// UpdateIncidentState transitions an incident session to a new
	// incident_state. Used by tests in Change 5 to advance a session to
	// 'believed_mitigated' so that resolve can be exercised, and by the
	// captain-driven state-machine work in Change 6+. Phase 1 does not
	// enforce the legal-transition matrix here.
	UpdateIncidentState(ctx context.Context, sessionID string, state IncidentState) error

	// Atomic regime transitions (§R / §6-A R7)
	//
	// DeclareIncidentRegime atomically PROMOTES an existing 'default'
	// session IN PLACE into the incident master (§12.3) — it does NOT mint
	// a fresh incident row. In one transaction it flips the designated
	// session's type to 'incident', sets incident_state to 'declared',
	// clears its linked_incident_id, sets system_regime to incident with the
	// given declared_kind, and attaches the declaring principal as captain
	// (R-CAP1). All statements run inside a single DB transaction; if any
	// fails, the entire operation rolls back — proven by the single-tx
	// rollback test in internal/api/regime_test.go (which uses the
	// test-hook variant).
	//
	// The promoted session keeps its original id and its original
	// creator_principal, so the creator (owner) and the captain (declarer)
	// MAY DIFFER (§12.3 consequence, recorded and accepted).
	//
	// Production callers: only the human-declare handler in
	// internal/api/regime.go. Joe-autonomous declare is an inert seam that
	// bypasses this method (the seam returns 403 before any call).
	//
	// Preconditions:
	//   - system_regime.mode must currently be 'normal' (the "no second
	//     concurrent incident" guard). Returns ErrRegimeAlreadyIncident if not.
	//   - sessionID must name an existing session (ErrNotFound otherwise)
	//     that is not already an incident (ErrSessionAlreadyIncident otherwise).
	// Returns the promoted session id (== sessionID) and the new captain id.
	DeclareIncidentRegime(ctx context.Context, principal, sessionID string, declaredKind RegimeKind) (promotedID, captainID string, err error)

	// ResolveIncidentRegime atomically: transitions the active incident
	// session from 'believed_mitigated' to 'resolved', and clears
	// system_regime to 'normal'. Both statements run inside a single DB
	// transaction.
	//
	// Phase 1 callers: only the human-resolve handler in
	// internal/api/regime.go. The AST invariant test in
	// internal/api/regime_invariant_test.go enforces this — it is the
	// named structural guard for §R5 / Invariant 4 (incident-mode exit
	// may not be automated).
	//
	// Preconditions:
	//   - system_regime.mode must currently be 'incident' (else
	//     ErrRegimeNotIncident).
	//   - the active incident session must be in state 'believed_mitigated'
	//     (else ErrIncidentNotMitigated).
	// Returns the resolved session's ID.
	ResolveIncidentRegime(ctx context.Context, principal string) (sessionID string, err error)
}

// Errors returned by atomic regime transitions.
var (
	ErrRegimeAlreadyIncident  = errors.New("sessionmodel: regime is already incident")
	ErrRegimeNotIncident      = errors.New("sessionmodel: regime is not incident")
	ErrIncidentNotMitigated   = errors.New("sessionmodel: active incident is not in 'believed_mitigated'")
	ErrNoActiveIncident       = errors.New("sessionmodel: no active incident session found")
	ErrSessionAlreadyIncident = errors.New("sessionmodel: session is already an incident — cannot promote twice")
)

// Errors returned by captain operations.
var (
	ErrNoActiveCaptain          = errors.New("sessionmodel: no active captain for session")
	ErrCaptainPrincipalMismatch = errors.New("sessionmodel: principal does not match active captain")
	// ErrCaptainAlreadyDetached — the SwapCaptain detach matched no active row
	// (the outgoing captain was already detached, e.g. by a concurrent
	// confirm-transfer). The whole swap rolls back so the loser cannot create a
	// second active captain (single-active-captain invariant, D-0025).
	ErrCaptainAlreadyDetached = errors.New("sessionmodel: outgoing captain is already detached")
)

// ErrNotFound is returned when a lookup finds no matching row.
var ErrNotFound = errors.New("sessionmodel: not found")

// Errors returned by lifecycle transitions (§12.5, B007a).
var (
	// ErrSessionAlreadyTrashed — soft-delete on a session that is already in
	// trash (the guarded UPDATE matched no active row).
	ErrSessionAlreadyTrashed = errors.New("sessionmodel: session is already trashed")
	// ErrSessionNotTrashed — restore on a session that is not currently trashed.
	ErrSessionNotTrashed = errors.New("sessionmodel: session is not trashed")
	// ErrSessionAlreadyArchived — archive on a session that is already archived
	// (the guarded UPDATE matched no un-archived row).
	ErrSessionAlreadyArchived = errors.New("sessionmodel: session is already archived")
	// ErrArchiveTranscriptChanged — the in-transaction chat_messages count no
	// longer matches the transcript the caller serialized into the artifact: a
	// message landed (or vanished) between the artifact read and the archive
	// commit. The transition writes nothing (the caller's transaction rolls
	// back) so the new message is never deleted unarchived; the caller may
	// re-read the transcript and retry.
	ErrArchiveTranscriptChanged = errors.New("sessionmodel: transcript changed since the archive artifact was built")
	// ErrSessionNotArchived — restore-from-archive on a session that is not
	// currently archived.
	ErrSessionNotArchived = errors.New("sessionmodel: session is not archived")
)

// SQLRepository implements Repository on top of *sql.DB. It uses raw
// *sql.DB rather than the store.Store wrapper, parallel to the established
// secondary-repo pattern (RBAC, graph, knowledge, review, proposals,
// panic-state). The shared persistence interface seam is preserved by
// using ? placeholders + store.Rebind for cross-driver portability — see
// the session-model design (Phase 0) §5b-6 and Invariant 6.
type SQLRepository struct {
	db     *sql.DB
	driver string
}

// NewRepository constructs a SQLRepository against an existing *sql.DB.
func NewRepository(db *sql.DB, driver string) *SQLRepository {
	return &SQLRepository{db: db, driver: driver}
}

// --- Sessions ---

func (r *SQLRepository) CreateSession(ctx context.Context, s AgentSession) (*AgentSession, error) {
	if s.ID == "" {
		return nil, fmt.Errorf("create session: id required")
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now().UTC()
	}
	if s.LastActivityAt.IsZero() {
		s.LastActivityAt = s.CreatedAt
	}

	var incidentState any
	if s.IncidentState != nil {
		incidentState = string(*s.IncidentState)
	}
	var linkedID any
	if s.LinkedIncidentID != nil {
		linkedID = *s.LinkedIncidentID
	}
	var retentionClass any
	if s.RetentionClass != nil {
		retentionClass = *s.RetentionClass
	}
	var title any
	if s.Title != nil {
		title = *s.Title
	}

	// Lifecycle columns (§12.4): an active session writes all-null. A caller may
	// pre-set them (e.g. seeding a trashed/archived row in a test or migration),
	// so they are threaded through rather than hard-coded to NULL.
	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		INSERT INTO agent_sessions
			(id, type, incident_state, created_at, last_activity_at, creator_principal,
			 linked_incident_id, retention_class, title,
			 trashed_at, trashed_by, purge_after, archived_at, archived_by, archive_ref)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		s.ID, string(s.Type), incidentState,
		s.CreatedAt.Format(time.RFC3339), s.LastActivityAt.Format(time.RFC3339),
		s.CreatorPrincipal, linkedID, retentionClass, title,
		timePtrArg(s.TrashedAt), strPtrArg(s.TrashedBy), timePtrArg(s.PurgeAfter),
		timePtrArg(s.ArchivedAt), strPtrArg(s.ArchivedBy), strPtrArg(s.ArchiveRef))
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return &s, nil
}

// timePtrArg renders a *time.Time as an RFC3339 string arg, or nil for a NULL.
func timePtrArg(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format(time.RFC3339)
}

// strPtrArg renders a *string as a SQL arg, or nil for a NULL.
func strPtrArg(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func (r *SQLRepository) GetSession(ctx context.Context, id string) (*AgentSession, error) {
	row := r.db.QueryRowContext(ctx, store.Rebind(r.driver, `
		SELECT `+sessionColumns+`
		FROM agent_sessions WHERE id = ?`), id)
	return scanSession(row.Scan)
}

func (r *SQLRepository) ListSessions(ctx context.Context) ([]AgentSession, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+sessionColumns+`
		FROM agent_sessions ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	return scanSessionRows(rows)
}

func (r *SQLRepository) ListSessionsByType(ctx context.Context, t SessionType) ([]AgentSession, error) {
	rows, err := r.db.QueryContext(ctx, store.Rebind(r.driver, `
		SELECT `+sessionColumns+`
		FROM agent_sessions WHERE type = ? ORDER BY created_at`), string(t))
	if err != nil {
		return nil, fmt.Errorf("list sessions by type: %w", err)
	}
	defer rows.Close()
	return scanSessionRows(rows)
}

func (r *SQLRepository) DeleteSession(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		DELETE FROM agent_sessions WHERE id = ?`), id)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// UpdateSessionTitle sets the title column for one session. last_activity_at is
// deliberately left untouched so a rename does not jump the session to the top
// of the recency-ordered browse list.
func (r *SQLRepository) UpdateSessionTitle(ctx context.Context, id, title string) error {
	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		UPDATE agent_sessions SET title = ? WHERE id = ?`), title, id)
	if err != nil {
		return fmt.Errorf("update session title: %w", err)
	}
	return nil
}

// SetSessionTitleIfUnset sets the title column ONLY when it is currently NULL,
// returning whether the write landed. The `title IS NULL` predicate makes the
// auto-title path a compare-and-set: a concurrent manual UpdateSessionTitle (an
// unconditional write) that lands first fills the column, and this call then
// matches zero rows and returns (false, nil) rather than clobbering the manual
// value. last_activity_at is deliberately left untouched (parallel to
// UpdateSessionTitle) so an auto-title does not reorder the recency browse list.
//
// This is the ONLY title-write the auto-title background goroutine must use;
// UpdateSessionTitle stays reserved for the owner-checked manual-rename path.
func (r *SQLRepository) SetSessionTitleIfUnset(ctx context.Context, id, title string) (bool, error) {
	res, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		UPDATE agent_sessions SET title = ? WHERE id = ? AND title IS NULL`), title, id)
	if err != nil {
		return false, fmt.Errorf("set session title if unset: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		// A driver that does not report RowsAffected cannot confirm the CAS
		// outcome; report no-write so the caller does not assume it won the race.
		return false, nil
	}
	return n > 0, nil
}

// LinkSessionToIncident sets linked_incident_id ONLY — no type flip. Under the
// two-type model (§12.3) participation in an incident is expressed solely by the
// linked_incident_id pointer; the removed 'investigation' type no longer exists,
// so a linked session stays a plain 'default' conversation. last_activity_at is
// deliberately left untouched (parallel to UpdateSessionTitle) so a link does
// not reorder the recency-sorted browse list. The CHECK permits linked_incident_id
// on any non-incident type.
func (r *SQLRepository) LinkSessionToIncident(ctx context.Context, sessionID, incidentID string) error {
	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		UPDATE agent_sessions SET linked_incident_id = ? WHERE id = ?`),
		incidentID, sessionID)
	if err != nil {
		return fmt.Errorf("link session to incident: %w", err)
	}
	return nil
}

// ActiveIncidentSession returns the active incident session, or nil if none.
// "Active" excludes resolved/reviewed (the §C/sessiongate definition). The
// query mirrors ResolveIncidentRegime's active-incident lookup.
func (r *SQLRepository) ActiveIncidentSession(ctx context.Context) (*AgentSession, error) {
	row := r.db.QueryRowContext(ctx, store.Rebind(r.driver, `
		SELECT `+sessionColumns+`
		FROM agent_sessions
		WHERE type = 'incident' AND incident_state NOT IN ('resolved', 'reviewed')
		ORDER BY created_at DESC
		LIMIT 1`))
	return scanSession(row.Scan)
}

func (r *SQLRepository) UpdateIncidentState(ctx context.Context, sessionID string, state IncidentState) error {
	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		UPDATE agent_sessions
		SET incident_state = ?, last_activity_at = ?
		WHERE id = ? AND type = 'incident'`),
		string(state), time.Now().UTC().Format(time.RFC3339), sessionID)
	if err != nil {
		return fmt.Errorf("update incident state: %w", err)
	}
	return nil
}

// ListSessionsByCreator returns the caller's own sessions, newest activity
// first, capped at limit (<=0 means no cap). This is the owner-scoped Web UI
// chat list — the §11 Phase 1 isolation fix: unlike ListRecentSessions
// (team-wide), this filters by creator_principal so one user never
// sees another's chat history. The LEFT JOIN counts messages in one query (no
// N+1) so the list can render a message count per session.
func (r *SQLRepository) ListSessionsByCreator(ctx context.Context, principal string, limit int) ([]ChatSessionRow, error) {
	query := `
		SELECT ` + sessionColumnsPrefixed + `,
		       COUNT(m.id) AS message_count,
		       p.title AS master_title
		FROM agent_sessions s
		LEFT JOIN chat_messages m ON m.session_id = s.id
		LEFT JOIN agent_sessions p ON p.id = s.linked_incident_id
		WHERE s.creator_principal = ?
		  AND s.trashed_at IS NULL AND s.archived_at IS NULL
		GROUP BY s.id, p.title
		ORDER BY s.last_activity_at DESC`
	if limit > 0 {
		query += "\n\t\tLIMIT ?"
	}

	var (
		rows *sql.Rows
		err  error
	)
	if limit > 0 {
		rows, err = r.db.QueryContext(ctx, store.Rebind(r.driver, query), principal, limit)
	} else {
		rows, err = r.db.QueryContext(ctx, store.Rebind(r.driver, query), principal)
	}
	if err != nil {
		return nil, fmt.Errorf("list sessions by creator: %w", err)
	}
	defer rows.Close()

	return scanChatSessionRows(rows)
}

// ListRecentSessions returns EVERY session, newest activity first, capped at
// limit (<=0 means no cap). This is the team-wide chat list (§12.8, team-wide
// read): unlike ListSessionsByCreator it applies no principal predicate, so any
// authenticated caller sees every team session. It mirrors the owner-scoped
// list's single-query LEFT JOIN message count (no N+1) and recency order; the
// handler stamps read_only per row from each row's creator_principal.
func (r *SQLRepository) ListRecentSessions(ctx context.Context, limit int) ([]ChatSessionRow, error) {
	query := `
		SELECT ` + sessionColumnsPrefixed + `,
		       COUNT(m.id) AS message_count,
		       p.title AS master_title
		FROM agent_sessions s
		LEFT JOIN chat_messages m ON m.session_id = s.id
		LEFT JOIN agent_sessions p ON p.id = s.linked_incident_id
		WHERE s.trashed_at IS NULL AND s.archived_at IS NULL
		GROUP BY s.id, p.title
		ORDER BY s.last_activity_at DESC`
	if limit > 0 {
		query += "\n\t\tLIMIT ?"
	}

	var (
		rows *sql.Rows
		err  error
	)
	if limit > 0 {
		rows, err = r.db.QueryContext(ctx, store.Rebind(r.driver, query), limit)
	} else {
		rows, err = r.db.QueryContext(ctx, store.Rebind(r.driver, query))
	}
	if err != nil {
		return nil, fmt.Errorf("list recent sessions: %w", err)
	}
	defer rows.Close()

	return scanChatSessionRows(rows)
}

// ListSessionsByOthers returns every session owned by a principal other than
// the caller, newest activity first, capped at limit (<=0 means no cap). This
// backs the "shared with you" browse list: in the org-wide read model every
// session is readable by any authenticated user, so all of them are listed (no
// visibility filter). It mirrors ListSessionsByCreator (same LEFT JOIN message
// count, same recency order) but inverts the principal predicate. The caller's
// own sessions are excluded so they are not duplicated across the two lists.
// creator_principal is carried on each row so the UI can show "shared by <owner>".
func (r *SQLRepository) ListSessionsByOthers(ctx context.Context, principal string, limit int) ([]ChatSessionRow, error) {
	query := `
		SELECT ` + sessionColumnsPrefixed + `,
		       COUNT(m.id) AS message_count,
		       p.title AS master_title
		FROM agent_sessions s
		LEFT JOIN chat_messages m ON m.session_id = s.id
		LEFT JOIN agent_sessions p ON p.id = s.linked_incident_id
		WHERE s.creator_principal != ?
		  AND s.trashed_at IS NULL AND s.archived_at IS NULL
		GROUP BY s.id, p.title
		ORDER BY s.last_activity_at DESC`
	if limit > 0 {
		query += "\n\t\tLIMIT ?"
	}

	var (
		rows *sql.Rows
		err  error
	)
	if limit > 0 {
		rows, err = r.db.QueryContext(ctx, store.Rebind(r.driver, query), principal, limit)
	} else {
		rows, err = r.db.QueryContext(ctx, store.Rebind(r.driver, query), principal)
	}
	if err != nil {
		return nil, fmt.Errorf("list sessions by others: %w", err)
	}
	defer rows.Close()

	return scanChatSessionRows(rows)
}

// --- Chat messages (interim flat store, migration 022) ---

// AddChatMessage appends a message to a session and bumps the session's
// last_activity_at so the owner-scoped list orders by recency. Seq is assigned
// as MAX(seq)+1 for the session; chat is single-threaded per session (the run
// model permits at most one running run per session), so the read-then-insert
// is safe, and a concurrent collision is caught by the UNIQUE(session_id, seq)
// constraint rather than producing a duplicate.
func (r *SQLRepository) AddChatMessage(ctx context.Context, m ChatMessage) (*ChatMessage, error) {
	if m.ID == "" {
		return nil, fmt.Errorf("add chat message: id required")
	}
	if m.SessionID == "" {
		return nil, fmt.Errorf("add chat message: session_id required")
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}

	var maxSeq sql.NullInt64
	if err := r.db.QueryRowContext(ctx, store.Rebind(r.driver,
		`SELECT MAX(seq) FROM chat_messages WHERE session_id = ?`), m.SessionID).Scan(&maxSeq); err != nil {
		return nil, fmt.Errorf("add chat message: next seq: %w", err)
	}
	m.Seq = int(maxSeq.Int64) + 1

	var toolName any
	if m.ToolName != nil {
		toolName = *m.ToolName
	}
	var toolArgs any
	if m.ToolArgs != nil {
		toolArgs = *m.ToolArgs
	}

	// stop_reason (migration 030): stored NULL when empty so the column reads as
	// "no special stop reason" for the common case and for every pre-migration row.
	var stopReason any
	if m.StopReason != "" {
		stopReason = m.StopReason
	}

	if _, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		INSERT INTO chat_messages
			(id, session_id, seq, role, content, tool_name, tool_args, stop_reason, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		m.ID, m.SessionID, m.Seq, m.Role, m.Content, toolName, toolArgs, stopReason,
		m.CreatedAt.Format(time.RFC3339)); err != nil {
		return nil, fmt.Errorf("add chat message: %w", err)
	}

	// Best-effort recency bump; a failure here must not lose the message.
	if _, err := r.db.ExecContext(ctx, store.Rebind(r.driver,
		`UPDATE agent_sessions SET last_activity_at = ? WHERE id = ?`),
		m.CreatedAt.Format(time.RFC3339), m.SessionID); err != nil {
		return nil, fmt.Errorf("add chat message: bump activity: %w", err)
	}
	return &m, nil
}

// ListChatMessages returns a session's messages in seq order (oldest first).
func (r *SQLRepository) ListChatMessages(ctx context.Context, sessionID string) ([]ChatMessage, error) {
	rows, err := r.db.QueryContext(ctx, store.Rebind(r.driver, `
		SELECT id, session_id, seq, role, content, tool_name, tool_args, stop_reason, created_at
		FROM chat_messages WHERE session_id = ? ORDER BY seq`), sessionID)
	if err != nil {
		return nil, fmt.Errorf("list chat messages: %w", err)
	}
	defer rows.Close()

	var out []ChatMessage
	for rows.Next() {
		var (
			m            ChatMessage
			toolName     sql.NullString
			toolArgs     sql.NullString
			stopReason   sql.NullString
			createdAtStr string
		)
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Seq, &m.Role, &m.Content,
			&toolName, &toolArgs, &stopReason, &createdAtStr); err != nil {
			return nil, fmt.Errorf("scan chat message: %w", err)
		}
		if toolName.Valid {
			m.ToolName = &toolName.String
		}
		if toolArgs.Valid {
			m.ToolArgs = &toolArgs.String
		}
		if stopReason.Valid {
			m.StopReason = stopReason.String
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
		out = append(out, m)
	}
	return out, rows.Err()
}

// sessionColumns is the canonical agent_sessions projection (migration 025).
// Every SELECT that feeds scanSession must list exactly these columns in this
// order. A LEFT JOIN message count is appended by the chat-list queries.
const sessionColumns = `id, type, incident_state, created_at, last_activity_at,
	       creator_principal, linked_incident_id, retention_class, title,
	       trashed_at, trashed_by, purge_after, archived_at, archived_by, archive_ref`

// sessionColumnsPrefixed is sessionColumns aliased to the agent_sessions table
// as "s" — used by the chat-list queries that LEFT JOIN chat_messages and would
// otherwise have an ambiguous "id".
const sessionColumnsPrefixed = `s.id, s.type, s.incident_state, s.created_at, s.last_activity_at,
	       s.creator_principal, s.linked_incident_id, s.retention_class, s.title,
	       s.trashed_at, s.trashed_by, s.purge_after, s.archived_at, s.archived_by, s.archive_ref`

// scanChatSessionRows drains the chat-list query result into ChatSessionRow
// values. Each SELECT it consumes lists the canonical session columns
// (sessionColumnsPrefixed), then the LEFT JOIN message count, then the linked
// master's title (the p.title self-join) — in that order. The two trailing
// columns are captured by appending their scan dests after the session columns.
//
// Those queries GROUP BY (s.id, p.title), not s.id alone: under Postgres a
// joined column is not functionally dependent on the grouped table's PK, so
// p.title must be in the GROUP BY. The pair groups identically to s.id because
// p is joined 1:1 by its own PK (p.id = s.linked_incident_id), so the message
// COUNT is unaffected.
func scanChatSessionRows(rows *sql.Rows) ([]ChatSessionRow, error) {
	var out []ChatSessionRow
	for rows.Next() {
		var (
			count       int
			masterTitle sql.NullString
		)
		s, err := scanSession(func(dest ...any) error {
			return rows.Scan(append(dest, &count, &masterTitle)...)
		})
		if err != nil {
			return nil, err
		}
		if s != nil {
			row := ChatSessionRow{AgentSession: *s, MessageCount: count}
			if masterTitle.Valid {
				row.LinkedIncidentTitle = masterTitle.String
			}
			out = append(out, row)
		}
	}
	return out, rows.Err()
}

func scanSession(scan func(...any) error) (*AgentSession, error) {
	var (
		s                 AgentSession
		typ               string
		incidentState     sql.NullString
		createdAtStr      string
		lastActivityAtStr string
		linkedIncidentID  sql.NullString
		retentionClass    sql.NullString
		title             sql.NullString
		trashedAt         sql.NullString
		trashedBy         sql.NullString
		purgeAfter        sql.NullString
		archivedAt        sql.NullString
		archivedBy        sql.NullString
		archiveRef        sql.NullString
	)
	err := scan(&s.ID, &typ, &incidentState, &createdAtStr, &lastActivityAtStr,
		&s.CreatorPrincipal, &linkedIncidentID, &retentionClass, &title,
		&trashedAt, &trashedBy, &purgeAfter, &archivedAt, &archivedBy, &archiveRef)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan session: %w", err)
	}
	s.Type = SessionType(typ)
	if incidentState.Valid {
		is := IncidentState(incidentState.String)
		s.IncidentState = &is
	}
	s.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
	s.LastActivityAt, _ = time.Parse(time.RFC3339, lastActivityAtStr)
	if linkedIncidentID.Valid {
		s.LinkedIncidentID = &linkedIncidentID.String
	}
	if retentionClass.Valid {
		s.RetentionClass = &retentionClass.String
	}
	if title.Valid {
		s.Title = &title.String
	}
	if trashedAt.Valid {
		s.TrashedAt = parseTimeOrNil(trashedAt.String)
	}
	if trashedBy.Valid {
		s.TrashedBy = &trashedBy.String
	}
	if purgeAfter.Valid {
		s.PurgeAfter = parseTimeOrNil(purgeAfter.String)
	}
	if archivedAt.Valid {
		s.ArchivedAt = parseTimeOrNil(archivedAt.String)
	}
	if archivedBy.Valid {
		s.ArchivedBy = &archivedBy.String
	}
	if archiveRef.Valid {
		s.ArchiveRef = &archiveRef.String
	}
	return &s, nil
}

// parseTimeOrNil parses an RFC3339 timestamp, returning nil on failure so a
// malformed stored value degrades to "unset" rather than a zero time.
func parseTimeOrNil(value string) *time.Time {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil
	}
	return &t
}

func scanSessionRows(rows *sql.Rows) ([]AgentSession, error) {
	var out []AgentSession
	for rows.Next() {
		s, err := scanSession(rows.Scan)
		if err != nil {
			return nil, err
		}
		if s != nil {
			out = append(out, *s)
		}
	}
	return out, rows.Err()
}

// --- Regime ---

func (r *SQLRepository) GetRegime(ctx context.Context) (*Regime, error) {
	var (
		mode                string
		declaredAt          sql.NullString
		declaredByPrincipal sql.NullString
		declaredKind        sql.NullString
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT mode, declared_at, declared_by_principal, declared_kind
		FROM system_regime WHERE id = 1`).
		Scan(&mode, &declaredAt, &declaredByPrincipal, &declaredKind)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get regime: %w", err)
	}
	out := &Regime{Mode: RegimeMode(mode)}
	if declaredAt.Valid {
		t, _ := time.Parse(time.RFC3339, declaredAt.String)
		out.DeclaredAt = &t
	}
	if declaredByPrincipal.Valid {
		out.DeclaredByPrincipal = &declaredByPrincipal.String
	}
	if declaredKind.Valid {
		k := RegimeKind(declaredKind.String)
		out.DeclaredKind = &k
	}
	return out, nil
}

func (r *SQLRepository) SetRegime(ctx context.Context, reg Regime) error {
	var declaredAt any
	if reg.DeclaredAt != nil {
		declaredAt = reg.DeclaredAt.Format(time.RFC3339)
	}
	var declaredByPrincipal any
	if reg.DeclaredByPrincipal != nil {
		declaredByPrincipal = *reg.DeclaredByPrincipal
	}
	var declaredKind any
	if reg.DeclaredKind != nil {
		declaredKind = string(*reg.DeclaredKind)
	}

	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		UPDATE system_regime
		SET mode = ?, declared_at = ?, declared_by_principal = ?, declared_kind = ?
		WHERE id = 1`),
		string(reg.Mode), declaredAt, declaredByPrincipal, declaredKind)
	if err != nil {
		return fmt.Errorf("set regime: %w", err)
	}
	return nil
}

// --- Captains ---

func (r *SQLRepository) AttachCaptain(ctx context.Context, c Captain) (*Captain, error) {
	return r.attachCaptainExec(ctx, r.db, c)
}

// sqlExecer is the subset of *sql.DB / *sql.Tx that the captain writes and the
// lifecycle transitions use, so a shared core can run either standalone (on
// r.db) or inside a transaction (on a *sql.Tx). It lets AttachCaptain and the
// SwapCaptain transaction share one INSERT implementation instead of forking it,
// and lets archiveExec re-count chat_messages in the same executor as its writes
// (QueryRowContext) so the count-guard commits or rolls back atomically with the
// transcript move. Both *sql.DB and *sql.Tx satisfy the full set.
type sqlExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// attachCaptainExec is the shared attach core: it validates required fields,
// applies the §6-D last_seen_at seeding + attached_at default, and runs the
// session_captains INSERT against the given executor. AttachCaptain calls it
// on r.db; SwapCaptain calls it on a *sql.Tx so the insert joins the swap
// transaction. The defaults and INSERT are defined once here so the two
// callers cannot drift.
func (r *SQLRepository) attachCaptainExec(ctx context.Context, exec sqlExecer, c Captain) (*Captain, error) {
	if c.ID == "" {
		return nil, fmt.Errorf("attach captain: id required")
	}
	if c.SessionID == "" {
		return nil, fmt.Errorf("attach captain: session_id required")
	}
	if c.AttachedAt.IsZero() {
		c.AttachedAt = time.Now().UTC()
	}
	// §6-D: seed last_seen_at on attach so a fresh attach counts as
	// reachable. If the caller already provided one (e.g. tests
	// constructing a stale captain) we honor it.
	if c.LastSeenAt == nil {
		t := c.AttachedAt
		c.LastSeenAt = &t
	}

	var detachedAt any
	if c.DetachedAt != nil {
		detachedAt = c.DetachedAt.Format(time.RFC3339)
	}
	var transferState any
	if c.TransferState != nil {
		transferState = string(*c.TransferState)
	}
	var incomingPrincipal any
	if c.IncomingPrincipal != nil {
		incomingPrincipal = *c.IncomingPrincipal
	}
	var transferInitiator any
	if c.TransferInitiator != nil {
		transferInitiator = string(*c.TransferInitiator)
	}

	_, err := exec.ExecContext(ctx, store.Rebind(r.driver, `
		INSERT INTO session_captains
			(id, session_id, captain_type, principal, attached_at, detached_at,
			 transfer_state, incoming_principal, transfer_initiator, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		c.ID, c.SessionID, string(c.CaptainType), c.Principal,
		c.AttachedAt.Format(time.RFC3339), detachedAt,
		transferState, incomingPrincipal, transferInitiator,
		c.LastSeenAt.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("attach captain: %w", err)
	}
	return &c, nil
}

func (r *SQLRepository) GetActiveCaptain(ctx context.Context, sessionID string) (*Captain, error) {
	row := r.db.QueryRowContext(ctx, store.Rebind(r.driver, `
		SELECT id, session_id, captain_type, principal, attached_at, detached_at,
		       transfer_state, incoming_principal, transfer_initiator, last_seen_at
		FROM session_captains
		WHERE session_id = ? AND detached_at IS NULL
		ORDER BY attached_at DESC
		LIMIT 1`), sessionID)
	return scanCaptain(row.Scan)
}

func (r *SQLRepository) ListCaptainsForSession(ctx context.Context, sessionID string) ([]Captain, error) {
	rows, err := r.db.QueryContext(ctx, store.Rebind(r.driver, `
		SELECT id, session_id, captain_type, principal, attached_at, detached_at,
		       transfer_state, incoming_principal, transfer_initiator, last_seen_at
		FROM session_captains WHERE session_id = ? ORDER BY attached_at`), sessionID)
	if err != nil {
		return nil, fmt.Errorf("list captains: %w", err)
	}
	defer rows.Close()

	var out []Captain
	for rows.Next() {
		c, err := scanCaptain(rows.Scan)
		if err != nil {
			return nil, err
		}
		if c != nil {
			out = append(out, *c)
		}
	}
	return out, rows.Err()
}

func scanCaptain(scan func(...any) error) (*Captain, error) {
	var (
		c                 Captain
		captainType       string
		attachedAtStr     string
		detachedAt        sql.NullString
		transferState     sql.NullString
		incomingPrincipal sql.NullString
		transferInitiator sql.NullString
		lastSeenAt        sql.NullString
	)
	err := scan(&c.ID, &c.SessionID, &captainType, &c.Principal, &attachedAtStr,
		&detachedAt, &transferState, &incomingPrincipal, &transferInitiator, &lastSeenAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan captain: %w", err)
	}
	c.CaptainType = CaptainType(captainType)
	c.AttachedAt, _ = time.Parse(time.RFC3339, attachedAtStr)
	if detachedAt.Valid {
		t, _ := time.Parse(time.RFC3339, detachedAt.String)
		c.DetachedAt = &t
	}
	if transferState.Valid {
		ts := TransferState(transferState.String)
		c.TransferState = &ts
	}
	if incomingPrincipal.Valid {
		c.IncomingPrincipal = &incomingPrincipal.String
	}
	if transferInitiator.Valid {
		ti := TransferInitiator(transferInitiator.String)
		c.TransferInitiator = &ti
	}
	if lastSeenAt.Valid {
		t, _ := time.Parse(time.RFC3339, lastSeenAt.String)
		c.LastSeenAt = &t
	}
	return &c, nil
}

// --- §B1 captain principal getter + §6-D reachability ---

func (r *SQLRepository) CurrentCaptainPrincipal(ctx context.Context, sessionID string) (string, bool, error) {
	cap, err := r.GetActiveCaptain(ctx, sessionID)
	if err != nil {
		return "", false, err
	}
	if cap == nil {
		return "", false, nil
	}
	return cap.Principal, true, nil
}

func (r *SQLRepository) RecordCaptainHeartbeat(ctx context.Context, sessionID, principal string, when time.Time) error {
	if when.IsZero() {
		when = time.Now().UTC()
	}
	cap, err := r.GetActiveCaptain(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("record captain heartbeat: %w", err)
	}
	if cap == nil {
		return ErrNoActiveCaptain
	}
	if cap.Principal != principal {
		return ErrCaptainPrincipalMismatch
	}
	_, err = r.db.ExecContext(ctx, store.Rebind(r.driver, `
		UPDATE session_captains SET last_seen_at = ? WHERE id = ?`),
		when.Format(time.RFC3339), cap.ID)
	if err != nil {
		return fmt.Errorf("record captain heartbeat: %w", err)
	}
	return nil
}

func (r *SQLRepository) IsCaptainReachable(ctx context.Context, sessionID string, thresholdSeconds int) (bool, error) {
	cap, err := r.GetActiveCaptain(ctx, sessionID)
	if err != nil {
		return false, fmt.Errorf("is captain reachable: %w", err)
	}
	if cap == nil {
		return false, ErrNoActiveCaptain
	}
	// A captain row with no last_seen_at (defensive: AttachCaptain seeds
	// it, but legacy rows or external inserts may not) is treated as
	// unreachable — fail closed.
	if cap.LastSeenAt == nil {
		return false, nil
	}
	age := time.Since(*cap.LastSeenAt)
	return age <= time.Duration(thresholdSeconds)*time.Second, nil
}

func (r *SQLRepository) MarkCaptainDetached(ctx context.Context, captainID string, when time.Time) error {
	if when.IsZero() {
		when = time.Now().UTC()
	}
	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		UPDATE session_captains
		SET detached_at = ?, transfer_state = NULL,
		    incoming_principal = NULL, transfer_initiator = NULL
		WHERE id = ?`),
		when.Format(time.RFC3339), captainID)
	if err != nil {
		return fmt.Errorf("mark captain detached: %w", err)
	}
	return nil
}

// SwapCaptain detaches the outgoing captain and attaches the incoming
// captain inside a single transaction. See the interface doc for the
// invariant. Delegates to swapCaptainWithHook with no fault hook.
func (r *SQLRepository) SwapCaptain(ctx context.Context, outgoingID string, incoming Captain, when time.Time) error {
	return r.swapCaptainWithHook(ctx, outgoingID, incoming, when, nil)
}

// SwapCaptainWithHook is the test-seam variant of SwapCaptain. hookAfterDetach,
// when non-nil, runs inside the transaction after the detach UPDATE and before
// the attach INSERT; returning an error from it forces the swap to fail mid-way
// and roll back. This mirrors ResolveIncidentRegimeWithHook (D-0024) and exists
// so the co-located captain test can prove the detach+attach are atomic — that a
// failure after the detach leaves the outgoing captain still active rather than
// stranding the incident captain-less. Production code calls SwapCaptain (nil
// hook).
func (r *SQLRepository) SwapCaptainWithHook(ctx context.Context, outgoingID string, incoming Captain, when time.Time, hookAfterDetach func(*sql.Tx) error) error {
	return r.swapCaptainWithHook(ctx, outgoingID, incoming, when, hookAfterDetach)
}

func (r *SQLRepository) swapCaptainWithHook(ctx context.Context, outgoingID string, incoming Captain, when time.Time, hookAfterDetach func(*sql.Tx) error) (err error) {
	if when.IsZero() {
		when = time.Now().UTC()
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("swap captain: begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// 1. Detach the outgoing captain. Same SET clause as the D-0024 resolve
	// detach (detached_at set; transfer columns cleared), keyed by id — but
	// guarded on detached_at IS NULL so only a still-active captain is detached.
	// Two racing confirm-transfers each read the same active captain and both
	// call SwapCaptain; without this guard both detaches "succeed" and both
	// attaches insert, leaving two rows with detached_at IS NULL (two active
	// captains, violating the single-active-captain invariant, D-0025). The guard
	// makes the detach a compare-and-set: the loser matches zero rows and gets
	// ErrCaptainAlreadyDetached, so its whole swap transaction rolls back (via the
	// deferred Rollback) instead of attaching a second captain.
	var res sql.Result
	if res, err = tx.ExecContext(ctx, store.Rebind(r.driver, `
		UPDATE session_captains
		SET detached_at = ?, transfer_state = NULL,
		    incoming_principal = NULL, transfer_initiator = NULL
		WHERE id = ? AND detached_at IS NULL`),
		when.Format(time.RFC3339), outgoingID); err != nil {
		return fmt.Errorf("swap captain: detach outgoing: %w", err)
	}
	if err = requireOneRow(res, ErrCaptainAlreadyDetached, "swap captain: detach outgoing"); err != nil {
		return err
	}

	if hookAfterDetach != nil {
		if hookErr := hookAfterDetach(tx); hookErr != nil {
			err = hookErr
			return err
		}
	}

	// 2. Attach the incoming captain via the shared attach core, on the same
	// tx so the INSERT commits or rolls back together with the detach.
	if _, err = r.attachCaptainExec(ctx, tx, incoming); err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("swap captain: commit: %w", err)
	}
	return nil
}

func (r *SQLRepository) UpdateCaptainTransferState(ctx context.Context, captainID string, state *TransferState, incomingPrincipal *string, initiator *TransferInitiator) error {
	var stateVal any
	if state != nil {
		stateVal = string(*state)
	}
	var incomingVal any
	if incomingPrincipal != nil {
		incomingVal = *incomingPrincipal
	}
	var initiatorVal any
	if initiator != nil {
		initiatorVal = string(*initiator)
	}
	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		UPDATE session_captains
		SET transfer_state = ?, incoming_principal = ?, transfer_initiator = ?
		WHERE id = ?`),
		stateVal, incomingVal, initiatorVal, captainID)
	if err != nil {
		return fmt.Errorf("update captain transfer state: %w", err)
	}
	return nil
}
