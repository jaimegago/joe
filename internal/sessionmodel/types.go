// Package sessionmodel owns the durable session, system regime, and captain
// binding records defined in docs/PHASE-0-SESSION-MODEL.md (§5b, §B, §R).
//
// Phase 1 builds the substrate, not the behavior. This package establishes
// shape and persistence; downstream changes layer HTTP, regime declare/
// resolve, captain state machine, and the §C captain-session gate on top.
package sessionmodel

import "time"

// SessionType discriminates session behavior. The domain is exactly two values
// (DESIGN-CHAT-SESSIONS.md §12.3): Default is an ordinary chat/work session;
// Incident is the single master session of an active incident regime. The
// as-built third type ('investigation') is removed — participation in an
// incident is expressed by the linked_incident_id pointer (a fact), not a type.
// Only Incident sessions carry incident_state; Default sessions never do.
type SessionType string

const (
	SessionTypeDefault  SessionType = "default"
	SessionTypeIncident SessionType = "incident"
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

// AgentSession is one row of the agent_sessions table (clean schema, migration
// 025 / DESIGN-CHAT-SESSIONS.md §12.4).
type AgentSession struct {
	ID             string
	Type           SessionType
	IncidentState  *IncidentState
	CreatedAt      time.Time
	LastActivityAt time.Time
	// CreatorPrincipal is the human owner. It is ALWAYS the context-resolved
	// authenticated principal at write time and is NEVER accepted from a request
	// body (§12.1) — callers set it from rbac.PrincipalFromContext, not from
	// client input, which makes the spoofable-creator defect impossible by
	// construction.
	CreatorPrincipal string
	// LinkedIncidentID is the participation pointer to the active incident
	// (§12.3). Its FK is ON DELETE SET NULL (§12.4): purging an incident severs
	// the link and never destroys this independent session.
	LinkedIncidentID *string
	// RetentionClass is the per-session resolution of the active admin retention
	// policy (§12.4) — no longer inert.
	RetentionClass *string
	// Title is the human-editable session label. nil until a title is set.
	Title *string
	// Lifecycle is timestamp-driven, not a state enum (§12.4): an ACTIVE session
	// has all six of these fields nil. Soft-delete sets TrashedAt/TrashedBy (and
	// PurgeAfter under the trash-then-purge policy); archive sets
	// ArchivedAt/ArchivedBy/ArchiveRef. The sweeper and admin transitions
	// (later nodes) drive these; this node only persists the columns.
	TrashedAt  *time.Time
	TrashedBy  *string
	PurgeAfter *time.Time
	ArchivedAt *time.Time
	ArchivedBy *string
	ArchiveRef *string
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
