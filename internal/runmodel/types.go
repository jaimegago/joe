// Package runmodel owns the durable run substrate defined in
// docs/PHASE-0-SESSION-MODEL.md §D: runs, steps, solicitations, world
// handles, idempotency keys, and the action ledger.
//
// Phase 1 builds the substrate, not the agentic loop. Downstream changes
// layer the HTTP control plane (Change 7), the §C captain-session gate
// (Changes 8/10), and the §D5 idempotency-key persist-before-issue protocol
// in joe's executor wrapper (Change 9) on top.
package runmodel

import "time"

// RunState is the §D1 state machine. running / awaiting_input /
// awaiting_world are live; completed / failed / cancelled are terminal.
type RunState string

const (
	RunStateRunning       RunState = "running"
	RunStateAwaitingInput RunState = "awaiting_input"
	RunStateAwaitingWorld RunState = "awaiting_world"
	RunStateCompleted     RunState = "completed"
	RunStateFailed        RunState = "failed"
	RunStateCancelled     RunState = "cancelled"
)

// Run is one row of agent_runs.
type Run struct {
	ID         string
	SessionID  string
	State      RunState
	StartedAt  time.Time
	EndedAt    *time.Time
	LastStepID *string
}

// StepKind is the §D4 durable-unit discriminator.
type StepKind string

const (
	StepKindReasoning            StepKind = "reasoning"
	StepKindToolCallIntent       StepKind = "tool_call_intent"
	StepKindToolCallResult       StepKind = "tool_call_result"
	StepKindSolicitationOpen     StepKind = "solicitation_open"
	StepKindSolicitationResolved StepKind = "solicitation_resolved"
	StepKindWorldHandleRecorded  StepKind = "world_handle_recorded"
	StepKindWorldHandleObserved  StepKind = "world_handle_observed"
)

// Step is one row of run_steps. Payload is opaque JSON-encoded text;
// schemas for each kind live in the executor / HTTP layer.
type Step struct {
	ID          string
	RunID       string
	StepNumber  int64
	Kind        StepKind
	Payload     string
	PersistedAt time.Time
}

// SolicitationKind is the §D awaiting_input taxonomy.
type SolicitationKind string

const (
	SolicitationKindDecision     SolicitationKind = "decision"
	SolicitationKindProvideData  SolicitationKind = "provide_data"
	SolicitationKindConfirmClose SolicitationKind = "confirm_close"
)

// LivenessFlag is meaningful only for provide_data solicitations per §D
// taxonomy. attached_human_now vs out_of_band_human_work distinguishes
// whether the human is currently attached or whether attachment may lapse.
type LivenessFlag string

const (
	LivenessFlagAttachedHumanNow   LivenessFlag = "attached_human_now"
	LivenessFlagOutOfBandHumanWork LivenessFlag = "out_of_band_human_work"
)

// Solicitation is one row of run_solicitations.
type Solicitation struct {
	ID                string
	RunID             string
	Kind              SolicitationKind
	Payload           string
	CreatedAt         time.Time
	ResolvedAt        *time.Time
	ResolutionPayload *string
	LivenessFlag      *LivenessFlag
}

// WorldHandle is one row of run_world_handles — the §D6 reattachable handle.
type WorldHandle struct {
	ID                string
	RunID             string
	Locator           string
	QueryMeta         string
	RecordedAt        time.Time
	LastPollAt        *time.Time
	LastObservedState *string
}

// IdempotencyKeyStatus is the §D5 lifecycle of a tool-key row.
// 'issued' is the only state where the actual tool call may happen;
// 'completed' and 'failed' are terminal.
type IdempotencyKeyStatus string

const (
	IdempotencyKeyStatusIssued    IdempotencyKeyStatus = "issued"
	IdempotencyKeyStatusCompleted IdempotencyKeyStatus = "completed"
	IdempotencyKeyStatusFailed    IdempotencyKeyStatus = "failed"
)

// IdempotencyKey is one row of tool_idempotency_keys.
type IdempotencyKey struct {
	Key         string
	RunID       string
	StepID      *string
	ToolName    string
	ArgsHash    string
	CreatedAt   time.Time
	CompletedAt *time.Time
	Result      *string
	Status      IdempotencyKeyStatus
}

// Tier is the T1/T2/T3 Safety tier captured in the action ledger.
type Tier int

const (
	TierT1 Tier = 1
	TierT2 Tier = 2
	TierT3 Tier = 3
)

// LedgerEntry is one row of action_ledger — the §D8 attaching-SRE view.
type LedgerEntry struct {
	ID             string
	RunID          string
	IdempotencyKey string
	ToolName       string
	Tier           Tier
	Principal      string
	SourceID       *string
	Summary        string
	RecordedAt     time.Time
	CompletedAt    *time.Time
	Status         string
}
