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
	// ListSessionsByCreator is the owner-scoped Web UI chat list (§11 Phase 1
	// isolation fix): only the caller's own sessions, newest activity first,
	// with a per-session message count. Distinct from ListSessions, which is
	// team-global and backs the /agent-sessions route.
	ListSessionsByCreator(ctx context.Context, principal string, limit int) ([]ChatSessionRow, error)
	// DeleteSession removes one session by ID. The schema's ON DELETE CASCADE
	// FKs (linked investigations via the self-FK, plus child tables landed in
	// Changes 2/3) carry the §5b-5 expunge downward. This is a single SQL
	// statement (no application-level fan-out).
	DeleteSession(ctx context.Context, id string) error

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
	// DeclareIncidentRegime atomically: creates an incident-type session
	// in state 'declared', sets system_regime to incident with the given
	// declared_kind, and attaches the declaring principal as captain
	// (R-CAP1). All three statements run inside a single DB transaction;
	// if any fails, the entire operation rolls back — proven by the
	// single-tx rollback test in internal/api/regime_test.go (which uses
	// the unexported test-hook variant).
	//
	// Phase 1 callers: only the human-declare handler in
	// internal/api/regime.go. Joe-autonomous declare is a Change 12 inert
	// seam that bypasses this method (the seam returns 403 before any
	// call). The AST invariant test asserts the production-code call count.
	//
	// Preconditions:
	//   - system_regime.mode must currently be 'normal'.
	// Returns ErrRegimeAlreadyIncident if not.
	DeclareIncidentRegime(ctx context.Context, principal string, declaredKind RegimeKind) (sessionID, captainID string, err error)

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
	ErrRegimeAlreadyIncident = errors.New("sessionmodel: regime is already incident")
	ErrRegimeNotIncident     = errors.New("sessionmodel: regime is not incident")
	ErrIncidentNotMitigated  = errors.New("sessionmodel: active incident is not in 'believed_mitigated'")
	ErrNoActiveIncident      = errors.New("sessionmodel: no active incident session found")
)

// Errors returned by captain operations.
var (
	ErrNoActiveCaptain          = errors.New("sessionmodel: no active captain for session")
	ErrCaptainPrincipalMismatch = errors.New("sessionmodel: principal does not match active captain")
)

// ErrNotFound is returned when a lookup finds no matching row.
var ErrNotFound = errors.New("sessionmodel: not found")

// SQLRepository implements Repository on top of *sql.DB. It uses raw
// *sql.DB rather than the store.Store wrapper, parallel to the established
// secondary-repo pattern (RBAC, graph, knowledge, review, proposals,
// panic-state). The shared persistence interface seam is preserved by
// using ? placeholders + store.Rebind for cross-driver portability — see
// PHASE-0-SESSION-MODEL.md §5b-6 and Invariant 6.
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

	if s.Visibility == "" {
		s.Visibility = VisibilityPrivate
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

	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		INSERT INTO agent_sessions
			(id, type, incident_state, created_at, last_activity_at, creator_principal, linked_incident_id, retention_class, title, visibility)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		s.ID, string(s.Type), incidentState,
		s.CreatedAt.Format(time.RFC3339), s.LastActivityAt.Format(time.RFC3339),
		s.CreatorPrincipal, linkedID, retentionClass, title, s.Visibility)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return &s, nil
}

func (r *SQLRepository) GetSession(ctx context.Context, id string) (*AgentSession, error) {
	row := r.db.QueryRowContext(ctx, store.Rebind(r.driver, `
		SELECT id, type, incident_state, created_at, last_activity_at,
		       creator_principal, linked_incident_id, retention_class, title, visibility
		FROM agent_sessions WHERE id = ?`), id)
	return scanSession(row.Scan)
}

func (r *SQLRepository) ListSessions(ctx context.Context) ([]AgentSession, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, type, incident_state, created_at, last_activity_at,
		       creator_principal, linked_incident_id, retention_class, title, visibility
		FROM agent_sessions ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	return scanSessionRows(rows)
}

func (r *SQLRepository) ListSessionsByType(ctx context.Context, t SessionType) ([]AgentSession, error) {
	rows, err := r.db.QueryContext(ctx, store.Rebind(r.driver, `
		SELECT id, type, incident_state, created_at, last_activity_at,
		       creator_principal, linked_incident_id, retention_class, title, visibility
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
// chat list — the §11 Phase 1 isolation fix: unlike ListSessions (team-global,
// used by /agent-sessions), this filters by creator_principal so one user never
// sees another's chat history. The LEFT JOIN counts messages in one query (no
// N+1) so the list can render a message count per session.
func (r *SQLRepository) ListSessionsByCreator(ctx context.Context, principal string, limit int) ([]ChatSessionRow, error) {
	query := `
		SELECT s.id, s.type, s.incident_state, s.created_at, s.last_activity_at,
		       s.creator_principal, s.linked_incident_id, s.retention_class, s.title, s.visibility,
		       COUNT(m.id) AS message_count
		FROM agent_sessions s
		LEFT JOIN chat_messages m ON m.session_id = s.id
		WHERE s.creator_principal = ?
		GROUP BY s.id
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

	var out []ChatSessionRow
	for rows.Next() {
		var count int
		// scanSession reads the 10 session columns; the join's trailing
		// message_count is captured by passing &count as the final scan dest.
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

	if _, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		INSERT INTO chat_messages
			(id, session_id, seq, role, content, tool_name, tool_args, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`),
		m.ID, m.SessionID, m.Seq, m.Role, m.Content, toolName, toolArgs,
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
		SELECT id, session_id, seq, role, content, tool_name, tool_args, created_at
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
			createdAtStr string
		)
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Seq, &m.Role, &m.Content,
			&toolName, &toolArgs, &createdAtStr); err != nil {
			return nil, fmt.Errorf("scan chat message: %w", err)
		}
		if toolName.Valid {
			m.ToolName = &toolName.String
		}
		if toolArgs.Valid {
			m.ToolArgs = &toolArgs.String
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
		out = append(out, m)
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
		visibility        string
	)
	err := scan(&s.ID, &typ, &incidentState, &createdAtStr, &lastActivityAtStr,
		&s.CreatorPrincipal, &linkedIncidentID, &retentionClass, &title, &visibility)
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
	s.Visibility = visibility
	return &s, nil
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

	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
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
