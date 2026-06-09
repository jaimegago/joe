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

// Kind discriminates the three components of audit rows. The kind is the
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

	// KindAuthLogin records credential use at the edge: an OIDC human
	// login (one row per login episode, written from the auth callback
	// after the session cookie is set) and break-glass service-account
	// bearer use (one row per episode, windowed-deduplicated in the edge
	// middleware so it is not written on every request). Stream H3; the
	// audit_log.kind CHECK admits this value as of migration 018. Both
	// paths are fail-open-but-loud — an audit-write failure logs loudly
	// via FailurePosture but never blocks the login or the request.
	KindAuthLogin Kind = "auth_login"
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

	// ActionLLMSetContextBudget records an operator change to the
	// context-budget fraction (table llm_context_budget). The audit row's
	// context carries the prior and new fraction. Kind is
	// KindLLMSettingsMutation, like the other settings mutations; the
	// audit_log.kind CHECK already admits that kind (migration 017), so no
	// schema change is needed for this action.
	ActionLLMSetContextBudget = "llm_set_context_budget"

	// ActionLLMRunawayTerminated records a session termination by the
	// runaway gate when a session exceeded the configured token
	// ceiling. The decision on this row is "deny" (the action the gate
	// refused to allow to continue).
	ActionLLMRunawayTerminated = "llm_runaway_terminated"

	// ActionLLMContextOverflow records a turn refused because its assembled
	// prompt exceeded the model's context window (the provider rejected it and
	// an adapter classified the rejection into llm.ErrContextOverflow). The
	// decision on this row is "deny" — the turn did not proceed. Kind is
	// KindLLMLimitTriggered, the SAME kind the runaway-ceiling termination uses
	// (ActionLLMRunawayTerminated): both are enforcement events on the LLM
	// path, distinguished by action. The audit_log.kind CHECK already admits
	// KindLLMLimitTriggered (migration 017), so no schema change is needed —
	// the CHECK enumerates kinds, not actions. This closes the parity gap
	// flagged in DECISIONS.md D-0015(f): overflow's
	// sibling failure already audited, overflow did not.
	ActionLLMContextOverflow = "llm_context_overflow"

	// ActionLLMCostLimitRefused records the cost-window gate refusing
	// an LLM call because a configured threshold would be exceeded.
	// The decision on this row is "deny".
	ActionLLMCostLimitRefused = "llm_cost_limit_refused"

	// Stream H3 auth-login action verbs. Both rows carry kind
	// KindAuthLogin and decision "allow" (the row records that the
	// credential was used and accepted, not a separate gate outcome).

	// ActionOIDCLogin records a successful OIDC human login. Written once
	// per login episode from the auth callback, after the session cookie
	// is set and before the redirect. The principal is user:<email>.
	ActionOIDCLogin = "oidc_login"

	// ActionBreakGlassUse records break-glass service-account bearer
	// credential use. Written from the edge middleware, windowed-
	// deduplicated so it fires once per episode (per principal+remote
	// within the session-TTL window) rather than on every request. The
	// principal is svc:<name>.
	ActionBreakGlassUse = "break_glass_use"

	// ActionAdminGranted records a privilege escalation: a logging-in user
	// bootstrapped to admin for the FIRST time via the auth.admin_email
	// match in the OIDC callback. Carries kind KindAuthLogin and decision
	// "allow", like the other auth-login verbs. Written exactly once — on
	// the login that first grants admin authority — never on subsequent
	// repeat admin logins, so it captures escalation without per-login
	// noise. The principal is user:<email>.
	ActionAdminGranted = "admin_granted"

	// --- D-0013 admin-RBAC-surface action verbs ---
	//
	// These verbs cover every event on the RBAC admin HTTP surface
	// (internal/api/admin.go): the eight handlers under /api/v1/admin/
	// plus the gate denial. All carry kind KindAdminAccess. D-0012
	// admin-gated that surface but it wrote ZERO audit rows; D-0013 wires
	// these verbs in so a zone mint, a policy grant/revoke, a source-zone
	// assignment, and a denied escalation attempt all leave a durable
	// trail. The dotted naming (zone.create, policy.grant, ...) is the
	// vocabulary specified by D-0013; it reads as resource.verb and groups
	// cleanly under the admin_access kind. The mutating verbs fail CLOSED
	// (no audit row ⇒ no mutation); the .read verbs are read-class and
	// fail OPEN (see isFailOpen below).

	// ActionAdminZoneCreate records an admin minting a new security zone
	// (POST /api/v1/admin/zones). Decision "allow"; mutating (fail-closed).
	ActionAdminZoneCreate = "zone.create"
	// ActionAdminZoneRead records an admin listing/reading zones
	// (GET /api/v1/admin/zones). The read leaks the authz topology
	// (D-0012), so it is audited; read-class, fail-open.
	ActionAdminZoneRead = "zone.read"
	// ActionAdminPolicyGrant records an admin granting a principal access
	// to a zone (POST /api/v1/admin/policies). Decision "allow"; mutating.
	ActionAdminPolicyGrant = "policy.grant"
	// ActionAdminPolicyRevoke records an admin revoking such a grant
	// (DELETE /api/v1/admin/policies/{id}). Decision "allow"; mutating.
	ActionAdminPolicyRevoke = "policy.revoke"
	// ActionAdminPolicyRead records an admin listing/reading policies
	// (GET /api/v1/admin/policies) — leaks who holds which zone. Read-class.
	ActionAdminPolicyRead = "policy.read"
	// ActionAdminComponentZoneAssign records an admin assigning a source to a
	// zone (POST /api/v1/admin/component-zones). Decision "allow"; mutating.
	ActionAdminComponentZoneAssign = "component_zone.assign"
	// ActionAdminComponentZoneRead records an admin listing source-zone
	// assignments OR the unassigned-source roster (GET
	// /api/v1/admin/component-zones, GET /api/v1/admin/unassigned). Read-class.
	ActionAdminComponentZoneRead = "component_zone.read"
	// ActionAdminGrant records an admin promoting another principal to
	// admin via the admin REST surface (POST /api/v1/admin/admins →
	// Provisioner.GrantAdmin → internal/rbac AddAdmin), the single audited
	// writer now that the operator CLI is gone (identity Stage 4).
	// Decision "allow"; mutating (fail-closed). NOTE: distinct from
	// ActionAdminGranted ("admin_granted"), which is the OIDC bootstrap
	// self-escalation under KindAuthLogin.
	ActionAdminGrant = "admin.grant"
	// ActionAdminRevoke records an admin demoting another principal.
	// CLI-only today; same forward-compat rationale as ActionAdminGrant.
	ActionAdminRevoke = "admin.revoke"
	// ActionAdminAccessDenied records a requireAdmin gate denial: a
	// non-admin (or a principal whose admin status could not be read)
	// attempted an /api/v1/admin/ endpoint and got 403. Decision "deny".
	// This is the "non-admin tried to escalate" trail that completes the
	// D-0012 privilege-escalation story. The attempted endpoint
	// (method + path) rides in the Context blob's "target" field.
	ActionAdminAccessDenied = "admin.access_denied"
	// ActionAdminAdminRead records an admin listing the admin roster
	// (GET /api/v1/admin/admins) — leaks who holds admin authority.
	// Read-class (fail-open), so it is in isFailOpen below.
	ActionAdminAdminRead = "admin.read"
	// ActionAdminPrincipalRead records an admin listing the identity
	// registry (GET /api/v1/admin/principals) — the Users page query.
	// Read-class (fail-open), so it is in isFailOpen below.
	ActionAdminPrincipalRead = "principal.read"
	// ActionAdminCredentialStatusRead records an admin reading the
	// per-component credential authz/connectivity status surface (D-0026
	// unit 3): the passive Describe listing (GET
	// /api/v1/admin/credential-status), the explicit connectivity Probe
	// (POST .../{componentID}/probe), and the deliberate captured-stderr
	// fetch (POST .../{componentID}/probe/stderr). All three are diagnostic
	// reads of credential health — they never mutate Joe's authz config and
	// the credential half never serializes — so all are read-class
	// (fail-open) and share this one verb. It is in isFailOpen below.
	ActionAdminCredentialStatusRead = "credential_status.read"

	// --- Identity Stage 1 admin-mutation action verbs ---
	//
	// These complete the admin-surface vocabulary so every RBAC/identity
	// mutation the repository now performs (and audits in the same
	// transaction) has a verb. All carry kind KindAdminAccess and decision
	// "allow"; all are mutating (fail-closed) — none is added to isFailOpen.

	// ActionAdminZoneUpdate records an admin editing a zone's name,
	// description, or allowed_actions (PATCH /api/v1/admin/zones/{id}).
	ActionAdminZoneUpdate = "zone.update"
	// ActionAdminZoneDelete records an admin deleting a zone
	// (DELETE /api/v1/admin/zones/{id}). rbac_policies for the zone cascade;
	// a zone still referenced by a source assignment is refused (RESTRICT).
	ActionAdminZoneDelete = "zone.delete"
	// ActionAdminComponentZoneUnassign records an admin removing a source→zone
	// assignment (DELETE /api/v1/admin/component-zones/{componentID}). The source
	// then falls back to the default unassigned zone.
	ActionAdminComponentZoneUnassign = "component_zone.unassign"
	// ActionAdminPrincipalDisable records an admin disabling a principal in
	// the identity registry (status active→disabled).
	ActionAdminPrincipalDisable = "principal.disable"
	// ActionAdminPrincipalEnable records an admin re-enabling a principal
	// (status disabled→active).
	ActionAdminPrincipalEnable = "principal.enable"
)

// KindAdminAccess is every event on the RBAC admin HTTP surface
// (internal/api/admin.go): zone/policy/source-zone reads, creates,
// grants, revokes, and assignments, plus requireAdmin gate denials. It is
// the admin-surface parallel of KindInfraAccess (which is every decision
// the guarded accessor makes): one kind for the whole surface,
// discriminated by action + decision. Added by D-0013; the audit_log.kind
// CHECK admits this value as of migration 020. Phase F covered the
// accessor's decision point but not mutations of the authorization
// CONFIGURATION the accessor reads — this kind is that missing surface.
const KindAdminAccess Kind = "admin_access"

// Details is the typed JSON shape for the audit_log.context column on
// configuration-mutation rows. It is the shape Stream G locked for LLM
// settings mutations — the llm_settings_mutation rows carry the identical
// {target, before, after} keys (see internal/llmsettings.AuditCtxTarget /
// AuditCtxBefore / AuditCtxAfter, written by MutationService.runMutation).
// D-0013 reuses that shape for admin-RBAC rows per the locked decision that
// "settings-mutations and admin-mutations share the audit table with a
// typed details column."
//
// Field semantics:
//
//   - Target: WHAT was acted on — the target resource identifier. For a
//     zone: "zone:<id>"; a policy grant: "policy:<principal>@<zone>"; a
//     policy revoke: "policy:<id>"; a source-zone assignment:
//     "component_zone:<sourceID>"; a read: the resource collection name
//     ("zones", "policies", "component_zones", "unassigned"); a denial: the
//     attempted endpoint "<METHOD> <path>". Always set.
//
//   - Before: the target's state BEFORE the mutation, for revokes and
//     edits. Omitted (omitempty) for creates/grants (no prior state) and
//     reads.
//
//   - After: the target's state AFTER the mutation, for creates/grants and
//     edits. Omitted for revokes (no remaining state) and reads.
//
// The any-typed Before/After hold whatever the resource marshals to (a
// Zone, a Policy, a SourceZoneAssignment), matching the settings service's
// any-typed before/after. omitempty keeps read and create/revoke rows from
// carrying meaningless null keys.
type Details struct {
	Target string `json:"target"`
	Before any    `json:"before,omitempty"`
	After  any    `json:"after,omitempty"`
}

// Event is one audit row. The fields map 1:1 to audit_log columns
// (migration 015). Timestamp is filled by the repository if zero; callers
// are not required to stamp.
type Event struct {
	Timestamp   time.Time
	Principal   string
	Action      string
	Zone        string
	ComponentID string
	Decision    Decision
	Reason      string
	Kind        Kind
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

// Posture is what the CALLER will do with FailurePosture's return value. It
// selects the WORDING of the loud failure log so the log names the outcome
// that actually happens at the call site. It does NOT decide the §4 split
// (the return value and log level) — that stays derived from the action
// inside FailurePosture, so the split cannot drift.
//
// The posture is necessary because a mutating-class action can be handled
// two ways: a site that propagates the return ABORTS, while a site that
// discards it (the fail-open-but-loud auth / observability writes) PROCEEDS.
// isFailOpen(action) alone cannot tell those apart, so the caller must say.
type Posture int

const (
	// FailClosed: the caller propagates the returned error and ABORTS the
	// action. The log reads "... ABORTED (fail-closed ...)".
	FailClosed Posture = iota
	// FailOpen: the caller discards the return and PROCEEDS regardless. The
	// log still fires loudly, but reads "... PROCEEDED ..." — never claiming
	// an abort that did not happen. Used by the read-class accessor path and
	// by the fail-open-but-loud auth / observability sites.
	FailOpen
)

// PostureForAction returns the §4 default posture for an action: read-class
// actions are FailOpen (the caller proceeds), everything else FailClosed
// (the caller aborts). Call sites that HONOR FailurePosture's return — i.e.
// propagate it and act on it — pass this so the logged wording matches the
// action's split. Sites that always discard the return pass FailOpen
// directly. Keeping the read/mutate classification here means it lives in
// one place and cannot drift.
func PostureForAction(action string) Posture {
	if isFailOpen(action) {
		return FailOpen
	}
	return FailClosed
}

// FailurePosture decides what to do when an audit Insert fails. It
// implements the §4 split: mutating actions fail CLOSED, reads fail OPEN.
// One helper, called from every audit caller, so the split cannot drift.
//
// Returns nil iff the action should proceed (audit succeeded, or the
// action is a read and the failure is fail-open). Returns the original
// audit error iff the action should be aborted (fail-closed). The return
// value and the log LEVEL are derived from the action — NOT from posture —
// so the §4 split cannot be overridden by a caller.
//
// The action argument is the rbac.Action constant or one of the transition
// action verbs above. Transition action verbs are treated as mutating —
// declaring / resolving an incident, attaching / transferring captaincy
// are state changes; if the audit row cannot be written, the durable trail
// the design demands does not exist, so the mutation must not proceed.
//
// posture tells the helper what the caller will actually do with the
// return, so the loud log names the real outcome: a fail-open caller that
// discards the return PROCEEDS even on a mutating action, and the log must
// say PROCEEDED, not ABORTED. Propagating callers pass
// PostureForAction(action); discarding callers pass FailOpen.
//
// `where` is a short label used in the warn log (e.g. "accessor:k8s_read"
// or "regime:declare"); it stays out of the audit row.
func FailurePosture(ctx context.Context, action string, auditErr error, where string, posture Posture) error {
	if auditErr == nil {
		return nil
	}
	// Wording follows the caller's posture (what actually happens); level
	// and return follow the §4 split (derived from the action, so it cannot
	// drift). A fail-open caller logs loudly but says PROCEEDED; a
	// fail-closed caller says ABORTED.
	msg := "AUDIT WRITE FAILED — mutating action ABORTED (fail-closed per §4)"
	if posture == FailOpen {
		msg = "AUDIT WRITE FAILED — action PROCEEDED without audit row (fail-open-but-loud per §4); investigate audit store"
	}
	if isFailOpen(action) {
		// Reads and queries proceed despite a missing audit row. The
		// failure is logged loudly per §4 ("a read proceeds even if its
		// audit row cannot be written, with a loud operational alert").
		slog.Warn(msg,
			"where", where,
			"action", action,
			"error", auditErr,
		)
		return nil
	}
	// Mutating actions and every transition row fail CLOSED at the return:
	// the original audit error is returned to the caller, which either
	// surfaces it (fail-closed, aborting the mutation) or discards it
	// (fail-open-but-loud, proceeding). The posture above already named the
	// real outcome in the log.
	slog.Error(msg,
		"where", where,
		"action", action,
		"error", auditErr,
	)
	return auditErr
}

// isFailOpen reports whether the given action verb is read-class for the
// purposes of the §4 failure split. Read-class: the infra read verbs "read"
// and "query" (the values of rbac.ActionRead / rbac.ActionQuery), and the
// D-0013 admin-surface read verbs (zone.read, policy.read, component_zone.read,
// admin.read, principal.read) and the D-0026 credential-status read verb
// (credential_status.read).
// Mutate-class: everything else, including all transition verbs, the admin
// mutations (zone.create, zone.update, zone.delete, policy.grant,
// policy.revoke, component_zone.assign, component_zone.unassign, admin.grant,
// admin.revoke, principal.disable, principal.enable), and the admin
// gate-denial verb (admin.access_denied is a deny event, not a read — its
// absence must be loud, matching captaingate's fail-closed refusal posture).
//
// The two infra verbs are compared as string literals rather than via
// rbac.ActionRead / rbac.ActionQuery so this package does NOT import
// internal/rbac: internal/rbac now writes its admin-mutation audit rows in the
// same transaction as the mutation (via InsertTx), so rbac imports audit, and
// an audit→rbac import would close a cycle. The literals are the canonical
// values of those constants and are asserted to stay in sync by audit_test.go.
func isFailOpen(action string) bool {
	switch action {
	case "read", "query",
		ActionAdminZoneRead, ActionAdminPolicyRead, ActionAdminComponentZoneRead,
		ActionAdminAdminRead, ActionAdminPrincipalRead, ActionAdminCredentialStatusRead:
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
