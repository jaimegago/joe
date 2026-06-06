// Package sessionmodel owns the durable session, system regime, and captain
// binding records defined in docs/PHASE-0-SESSION-MODEL.md (§5b, §B, §R).
//
// Phase 1 builds the substrate, not the behavior. This package establishes
// shape and persistence; downstream changes layer HTTP, regime declare/
// resolve, captain state machine, and the §C captain-session gate on top.
package sessionmodel

import "time"

// SessionType discriminates session behavior. Only Incident sessions have a
// lifecycle (§5b-1); Investigation and Other are persistent artifacts with no
// terminal state (§5b-2).
type SessionType string

const (
	SessionTypeIncident      SessionType = "incident"
	SessionTypeInvestigation SessionType = "investigation"
	SessionTypeOther         SessionType = "other"
)

// IncidentState is the §5b-1 lifecycle. Non-incident sessions carry no
// incident_state (enforced by CHECK in migration 009).
type IncidentState string

const (
	IncidentStateDeclared          IncidentState = "declared"
	IncidentStateBeingWorked       IncidentState = "being_worked"
	IncidentStateBelievedMitigated IncidentState = "believed_mitigated"
	IncidentStateResolved          IncidentState = "resolved"
	IncidentStateReviewed          IncidentState = "reviewed"
)

// Visibility values for a session (migration 022). v1 is binary; per-principal
// sharing is future (DESIGN-CHAT-SESSIONS.md §10). Validation lives at the app
// layer — the column carries no CHECK so it stays droppable on SQLite.
const (
	VisibilityPrivate = "private"
	VisibilityPublic  = "public"
)

// AgentSession is one row of the agent_sessions table.
type AgentSession struct {
	ID               string
	Type             SessionType
	IncidentState    *IncidentState
	CreatedAt        time.Time
	LastActivityAt   time.Time
	CreatorPrincipal string
	LinkedIncidentID *string
	RetentionClass   *string
	// Title is the human-editable session label (migration 022). nil until a
	// title is set; Phase 2 auto-suggests and lets the owner override it.
	Title *string
	// Visibility is 'private' (default) or 'public' (migration 022). Phase 1
	// only ever writes 'private'; the public read path is Phase 3.
	Visibility string
}

// ChatSessionRow is a session plus its chat message count — the projection the
// owner-scoped Web UI chat list renders (DESIGN-CHAT-SESSIONS.md §6).
type ChatSessionRow struct {
	AgentSession
	MessageCount int
}

// ChatMessage is one row of the interim chat_messages table (migration 022):
// the flat, owner-scoped Web UI chat message store keyed to agent_sessions.
// Seq is the per-session ordering key (1-based, gap-free under single-threaded
// chat). The committed endgame is agent_runs->run_steps; this type is retired
// when chat moves to the run model (DESIGN-CHAT-SESSIONS.md §10).
type ChatMessage struct {
	ID        string
	SessionID string
	Seq       int
	Role      string
	Content   string
	ToolName  *string
	ToolArgs  *string
	CreatedAt time.Time
}

// RegimeMode is the system-wide regime — §R1.
type RegimeMode string

const (
	RegimeModeNormal   RegimeMode = "normal"
	RegimeModeIncident RegimeMode = "incident"
)

// RegimeKind records who declared the current incident regime. The Joe kind
// is a defined-but-inert seam in Phase 1 (R2 / incremental-autonomy pattern).
type RegimeKind string

const (
	RegimeKindHuman RegimeKind = "human"
	RegimeKindJoe   RegimeKind = "joe"
)

// Regime is the single-row system_regime record.
type Regime struct {
	Mode                RegimeMode
	DeclaredAt          *time.Time
	DeclaredByPrincipal *string
	DeclaredKind        *RegimeKind
}

// CaptainType is the R-CAP4 typed role. Human is the v1 default; Joe is an
// inert seam in Phase 1.
type CaptainType string

const (
	CaptainTypeHuman CaptainType = "human"
	CaptainTypeJoe   CaptainType = "joe"
)

// TransferState is the §B state machine for an active captain row.
type TransferState string

const (
	TransferStateActive            TransferState = "active"
	TransferStateTransferRequested TransferState = "transfer_requested"
	TransferStateTransferConfirmed TransferState = "transfer_confirmed"
)

// TransferInitiator records which side opened a transfer, per §B dual
// initiation.
type TransferInitiator string

const (
	TransferInitiatorOutgoing TransferInitiator = "outgoing"
	TransferInitiatorIncoming TransferInitiator = "incoming"
)

// Captain is one row of the session_captains table. The "current captain" of
// a session is the row with DetachedAt == nil. Captain exists only in
// incident regime (§B4).
//
// LastSeenAt is the §6-D NET-NEW reachability signal added in Change 6.
// It is seeded on AttachCaptain (a fresh attach counts as reachable) and
// updated by the heartbeat endpoint. Compared against a threshold by
// IsCaptainReachable to drive the §B3 incoming-initiated-transfer branch.
type Captain struct {
	ID                string
	SessionID         string
	CaptainType       CaptainType
	Principal         string
	AttachedAt        time.Time
	DetachedAt        *time.Time
	TransferState     *TransferState
	IncomingPrincipal *string
	TransferInitiator *TransferInitiator
	LastSeenAt        *time.Time
}
