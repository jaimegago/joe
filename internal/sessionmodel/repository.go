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
	// DeleteSession removes one session by ID. The schema's ON DELETE CASCADE
	// FKs (linked investigations via the self-FK, plus child tables landed in
	// Changes 2/3) carry the §5b-5 expunge downward. This is a single SQL
	// statement (no application-level fan-out).
	DeleteSession(ctx context.Context, id string) error

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

	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		INSERT INTO agent_sessions
			(id, type, incident_state, created_at, last_activity_at, creator_principal, linked_incident_id, retention_class)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`),
		s.ID, string(s.Type), incidentState,
		s.CreatedAt.Format(time.RFC3339), s.LastActivityAt.Format(time.RFC3339),
		s.CreatorPrincipal, linkedID, retentionClass)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return &s, nil
}

func (r *SQLRepository) GetSession(ctx context.Context, id string) (*AgentSession, error) {
	row := r.db.QueryRowContext(ctx, store.Rebind(r.driver, `
		SELECT id, type, incident_state, created_at, last_activity_at,
		       creator_principal, linked_incident_id, retention_class
		FROM agent_sessions WHERE id = ?`), id)
	return scanSession(row.Scan)
}

func (r *SQLRepository) ListSessions(ctx context.Context) ([]AgentSession, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, type, incident_state, created_at, last_activity_at,
		       creator_principal, linked_incident_id, retention_class
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
		       creator_principal, linked_incident_id, retention_class
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

func scanSession(scan func(...any) error) (*AgentSession, error) {
	var (
		s                 AgentSession
		typ               string
		incidentState     sql.NullString
		createdAtStr      string
		lastActivityAtStr string
		linkedIncidentID  sql.NullString
		retentionClass    sql.NullString
	)
	err := scan(&s.ID, &typ, &incidentState, &createdAtStr, &lastActivityAtStr,
		&s.CreatorPrincipal, &linkedIncidentID, &retentionClass)
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
			 transfer_state, incoming_principal, transfer_initiator)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		c.ID, c.SessionID, string(c.CaptainType), c.Principal,
		c.AttachedAt.Format(time.RFC3339), detachedAt,
		transferState, incomingPrincipal, transferInitiator)
	if err != nil {
		return nil, fmt.Errorf("attach captain: %w", err)
	}
	return &c, nil
}

func (r *SQLRepository) GetActiveCaptain(ctx context.Context, sessionID string) (*Captain, error) {
	row := r.db.QueryRowContext(ctx, store.Rebind(r.driver, `
		SELECT id, session_id, captain_type, principal, attached_at, detached_at,
		       transfer_state, incoming_principal, transfer_initiator
		FROM session_captains
		WHERE session_id = ? AND detached_at IS NULL
		ORDER BY attached_at DESC
		LIMIT 1`), sessionID)
	return scanCaptain(row.Scan)
}

func (r *SQLRepository) ListCaptainsForSession(ctx context.Context, sessionID string) ([]Captain, error) {
	rows, err := r.db.QueryContext(ctx, store.Rebind(r.driver, `
		SELECT id, session_id, captain_type, principal, attached_at, detached_at,
		       transfer_state, incoming_principal, transfer_initiator
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
	)
	err := scan(&c.ID, &c.SessionID, &captainType, &c.Principal, &attachedAtStr,
		&detachedAt, &transferState, &incomingPrincipal, &transferInitiator)
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
	return &c, nil
}
