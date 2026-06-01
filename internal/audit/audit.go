// Package audit is the append-only audit trail.
//
// Identity & Authentication design (docs/joe-identity-design.md §2.6),
// Phase F: every authorization decision the accessor makes — and every
// regime/captain transition — is recorded as one row in the audit_log
// table. The repository exposes ONLY an Insert path; there is no Update or
// Delete method (code-level append-only). Migration 015 adds SQLite
// triggers that ABORT any UPDATE or DELETE against the same table
// (database-level append-only). The two enforcements are deliberately
// redundant.
//
// Failure posture (docs/joe-identity-design.md §4):
//
//   - Mutating actions (mutate, delete, and every transition row) FAIL
//     CLOSED — if the audit row cannot be written, the action does not
//     proceed. The accessor returns the audit-write error to its caller;
//     a transition site likewise refuses to perform the mutation.
//
//   - Reads (read, query) FAIL OPEN — the action proceeds even when the
//     audit row cannot be written. The failure is logged at WARN with all
//     fields needed to reconstruct the missing row from the operational
//     log; availability of read endpoints is preferred over a hard halt
//     when the audit store is unhealthy. This is a deliberate tradeoff,
//     stated explicitly in §4.
//
// The split is enforced by FailurePosture (below) so every audit caller
// makes the same decision the same way.
package audit

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/jaimegago/joe/internal/rbac"
)

// Decision is the recorded outcome of an authorization decision (and of a
// transition event — transitions are always recorded as "allow" because
// the row records that the transition happened, not that it was permitted
// by a separate gate).
type Decision string

const (
	DecisionAllow Decision = "allow"
	DecisionDeny  Decision = "deny"
)

// Kind discriminates the three sources of audit rows. The kind is the
// audit_log.kind column value (see migration 015).
type Kind string

const (
	// KindInfraAccess is every authorization decision the guarded
	// accessor makes against an infrastructure adapter or the graph
	// store. One row per call site.
	KindInfraAccess Kind = "infra_access"

	// KindRegimeTransition is incident declare / resolve — the events
	// whose history previously lived in system_regime and got nulled
	// on resolve (bug #3).
	KindRegimeTransition Kind = "regime_transition"

	// KindCaptainTransition is captain attach / transfer begin /
	// transfer confirm / transfer cancel — the events whose history
	// previously cascade-deleted with the session row. Phase G extends
	// this kind to also cover §C captain-gate refusals (action
	// captain_gate_refused) emitted by internal/captaingate: a gate
	// refusal is a captain-mechanism event (which session may mutate
	// during incident regime), so the existing kind is its natural
	// home and no migration is needed.
	KindCaptainTransition Kind = "captain_transition"

	// KindLLMSettingsMutation is an operator change to an LLM control:
	// the active model, a cost-window threshold, or the session token
	// ceiling. Written by the (later) settings service so every
	// configuration change has a forensic trail. The audit_log.kind
	// CHECK admits this value as of migration 017.
	KindLLMSettingsMutation Kind = "llm_settings_mutation"

	// KindLLMLimitTriggered is an enforcement event: the cost-window
	// gate refused an LLM call, or the runaway gate terminated a
	// session for exceeding the token ceiling. Written by the (later)
	// gates so refusals/terminations are forensically observable from
	// the same table as authorization decisions. The audit_log.kind
	// CHECK admits this value as of migration 017.
	KindLLMLimitTriggered Kind = "llm_limit_triggered"
)

// Action-verb constants for transition rows. Accessor rows use the
// rbac.Action string directly (read, query, mutate, delete).
const (
	ActionDeclareIncident        = "declare_incident"
	ActionResolveIncident        = "resolve_incident"
	ActionCaptainAttach          = "captain_attach"
	ActionCaptainTransferBegin   = "captain_transfer_begin"
	ActionCaptainTransferConfirm = "captain_transfer_confirm"
	ActionCaptainTransferCancel  = "captain_transfer_cancel"
	// ActionCaptainGateRefused is the Phase G refusal event written by
	// captaingate.Wrapper when the §C gate refuses a non-captain
	// mutation in incident regime. The row's kind is
	// KindCaptainTransition (the gate is part of the captain mechanism,
	// not a separate kind) and decision is "deny". A read-class action
	// is never emitted here because the gate lets T1 reads through
	// without an audit row.
	ActionCaptainGateRefused = "captain_gate_refused"

	// ReasonCaptainGateRefused is the structured reason tag captaingate
	// records for refusals; consistent with Phase F's enumerable-tag
	// vocabulary (see D-0009 deviation 3).
	ReasonCaptainGateRefused = "captain_gate_refused"

	// Stream G action verbs. The settings-mutation verbs are written
	// with kind KindLLMSettingsMutation; the limit-triggered verbs are
	// written with kind KindLLMLimitTriggered. These constants are
	// declared in G1 so all later phases (settings service, cost-window
	// gate, runaway gate) reference the same string values — no
	// scattered string literals.

	// ActionLLMSetActiveModel records an operator change to the live
	// active-model setting (table llm_settings).
	ActionLLMSetActiveModel = "llm_set_active_model"

	// ActionLLMSetCostLimit records an operator change to a per-window
	// cost threshold (table llm_cost_limits). The audit row's context
	// carries the window name ('hourly' | 'daily' | 'monthly') and the
	// new threshold value.
	ActionLLMSetCostLimit = "llm_set_cost_limit"

	// ActionLLMSetRunawayCeiling records an operator change to the
	// session token ceiling (table llm_runaway_limits).
	ActionLLMSetRunawayCeiling = "llm_set_runaway_ceiling"

	// ActionLLMRunawayTerminated records a session termination by the
	// runaway gate when a session exceeded the configured token
	// ceiling. The decision on this row is "deny" (the action the gate
	// refused to allow to continue).
	ActionLLMRunawayTerminated = "llm_runaway_terminated"

	// ActionLLMCostLimitRefused records the cost-window gate refusing
	// an LLM call because a configured threshold would be exceeded.
	// The decision on this row is "deny".
	ActionLLMCostLimitRefused = "llm_cost_limit_refused"
)

// Event is one audit row. The fields map 1:1 to audit_log columns
// (migration 015). Timestamp is filled by the repository if zero; callers
// are not required to stamp.
type Event struct {
	Timestamp time.Time
	Principal string
	Action    string
	Zone      string
	Source    string
	Decision  Decision
	Reason    string
	Kind      Kind
	// Context is a JSON blob carrying kind-specific specifics
	// (declared_kind, session_id, captain_id, transfer initiator, etc.).
	// "" is stored as the default "{}" by the repository.
	Context string
}

// Repository is the insert-only audit-log surface. There is NO Update or
// Delete method by design — the AST guard
// (TestRepositoryAPISurface_AppendOnly in audit_test.go) asserts the
// interface exposes EXACTLY the two insert-shaped methods below and
// nothing else. Production code receives this interface (never the
// concrete SQL repository) so no caller has a path that could mutate or
// remove a row.
//
// Two inserts, one audit row. The non-transactional Insert is the path
// the accessor and the regime/captain transitions use today — each
// audit row is its own atomic event. InsertTx is the path the (later)
// settings service uses to make a settings change AND its audit row a
// single durable event: the caller opens a transaction, writes the
// settings row, then asks the audit repository to write its row against
// the same transaction. Both rows commit together or roll back together.
// The two methods share one private SQL body in the concrete repository
// (sql.go) so the row shape cannot diverge.
type Repository interface {
	// Insert writes one audit row on its own connection. If e.Timestamp
	// is the zero value it is stamped with time.Now().UTC(). On error the
	// row was NOT written; callers MUST honour the fail-open /
	// fail-closed split for the kind/action being audited (see
	// FailurePosture).
	Insert(ctx context.Context, e Event) error
	// InsertTx writes one audit row against the caller-supplied
	// transaction. The row commits or rolls back atomically with the
	// caller's transaction; the audit repository never calls Commit or
	// Rollback. A nil tx is a programming error and returns
	// ErrAuditWriteFailed rather than silently falling back to the
	// database handle. Defaulting, empty-to-null mapping, and required
	// fields are identical to Insert — the two paths share one private
	// SQL body in the concrete repository.
	InsertTx(ctx context.Context, tx *sql.Tx, e Event) error
}

// FailurePosture decides what to do when an audit Insert fails. It
// implements the §4 split: mutating actions fail CLOSED, reads fail OPEN.
// One helper, called from every audit caller, so the split cannot drift.
//
// Returns nil iff the action should proceed (audit succeeded, or the
// action is a read and the failure is fail-open). Returns the original
// audit error iff the action should be aborted (fail-closed).
//
// The action argument is the rbac.Action constant or one of the transition
// action verbs above. Transition action verbs are treated as mutating —
// declaring / resolving an incident, attaching / transferring captaincy
// are state changes; if the audit row cannot be written, the durable trail
// the design demands does not exist, so the mutation must not proceed.
//
// `where` is a short label used in the warn log (e.g. "accessor:k8s_read"
// or "regime:declare"); it stays out of the audit row.
func FailurePosture(ctx context.Context, action string, auditErr error, where string) error {
	if auditErr == nil {
		return nil
	}
	if isFailOpen(action) {
		// Reads and queries proceed despite a missing audit row. The
		// failure is logged loudly per §4 ("a read proceeds even if its
		// audit row cannot be written, with a loud operational alert").
		slog.Warn("AUDIT WRITE FAILED — read proceeded without audit row (fail-open per §4); investigate audit store",
			"where", where,
			"action", action,
			"error", auditErr,
		)
		return nil
	}
	// Mutating actions and every transition row fail CLOSED. The original
	// audit error is returned to the caller, which surfaces it as an
	// internal-error / fails the mutation.
	slog.Error("AUDIT WRITE FAILED — mutating action ABORTED (fail-closed per §4)",
		"where", where,
		"action", action,
		"error", auditErr,
	)
	return auditErr
}

// isFailOpen reports whether the given action verb is read-class for the
// purposes of the §4 failure split. Read-class: rbac.ActionRead,
// rbac.ActionQuery. Mutate-class: everything else, including all
// transition verbs.
func isFailOpen(action string) bool {
	switch action {
	case string(rbac.ActionRead), string(rbac.ActionQuery):
		return true
	default:
		return false
	}
}

// ErrAuditWriteFailed wraps a lower-level error so callers can identify an
// audit-write failure without depending on the underlying driver error
// shape. The repository wraps its error in this; consumers test via
// errors.Is.
var ErrAuditWriteFailed = errors.New("audit: write failed")
