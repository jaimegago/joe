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
}
