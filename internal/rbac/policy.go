package rbac

import (
	"context"
	"log/slog"
)

// PolicyEngine answers "can this principal perform this action on this component?"
// It is backed by the RBAC repository and uses zone assignments + policy tables.
type PolicyEngine struct {
	repo Repository
}

// NewPolicyEngine creates a new PolicyEngine.
func NewPolicyEngine(repo Repository) *PolicyEngine {
	return &PolicyEngine{repo: repo}
}

// Decision carries the resolved zone and a structured reason alongside the
// allow/deny outcome. It exists so the guarded accessor (Phase F) can write
// a single audit row per decision with the zone and reason it actually
// reached — the OUTCOME is identical to IsAllowed (which is now a thin
// wrapper). Decision is what the accessor consumes; IsAllowed is the
// boolean-only surface other callers keep using.
type Decision struct {
	// Allowed is the same boolean IsAllowed returns.
	Allowed bool
	// Zone is the component's resolved zone — "unassigned" by default, the
	// assignment's zone when set. Never empty for a real decision.
	Zone string
	// Reason is a short machine-readable tag explaining the OUTCOME:
	//   - ReasonPolicyAllow      — at least one principal had a matching grant
	//   - ReasonAdminCapability  — at least one principal holds dynamic admin
	//                              status; the allow bypasses the per-zone
	//                              grant requirement but is still bounded by
	//                              the zone's allowed_actions (Phase H, D-0011)
	//   - ReasonZoneNotFound     — the resolved zone is missing from the table
	//   - ReasonActionNotInZone  — the zone does not allow this action at all
	//   - ReasonNoGrant          — the action is in-zone but no principal holds it
	// The accessor records this in the audit row's reason column, so an
	// admin-allowed action is distinguishable from an ordinary zone-grant
	// allow in the audit trail.
	Reason string
}

// Reason tags surfaced via Decision.Reason and the audit_log.reason column.
// Stable, enumerable, machine-parseable — consistent with the Phase F
// reason-vocabulary convention (D-0009 deviation 3). The admin-capability
// tag is the Phase H addition.
const (
	// ReasonPolicyAllow: at least one principal in the set held an
	// rbac_policies grant for the resolved zone. Ordinary path.
	ReasonPolicyAllow = "policy_allow"

	// ReasonAdminCapability: at least one principal in the set holds
	// dynamic admin status (admin_principals row). Phase H, D-0011. This
	// reason exists explicitly to distinguish admin-basis allows from
	// ordinary zone-grant allows in the audit trail: an operator querying
	// `WHERE reason = 'admin_capability'` sees only decisions admin would
	// not have reached through a per-zone grant.
	ReasonAdminCapability = "admin_capability"

	// ReasonZoneNotFound: the component's resolved zone is missing from
	// security_zones (a schema gap or a stale row).
	ReasonZoneNotFound = "zone_not_found"

	// ReasonActionNotInZone: the zone's allowed_actions list does not
	// include the requested action. Admin does NOT bypass this — see
	// D-0011 for the stricter-interpretation justification.
	ReasonActionNotInZone = "action_not_in_zone"

	// ReasonNoGrant: the action is in-zone but no principal in the set
	// holds the grant and none is admin.
	ReasonNoGrant = "no_grant"
)

// IsAllowed returns true if ANY principal in the set may perform action on
// componentID — the union-of-grants decision (additive / allow-only). A size-1
// set reproduces the previous single-principal decision exactly, which is the
// Phase B regression contract (docs/joe-identity-design.md §2.7). Thin
// wrapper over Decide.
func (e *PolicyEngine) IsAllowed(ctx context.Context, principals PrincipalSet, componentID string, action Action) bool {
	return e.Decide(ctx, principals, componentID, action).Allowed
}

// Decide is the full-fidelity decision call: returns the same outcome as
// IsAllowed and also the resolved zone and a structured reason. The guarded
// accessor calls this so the audit row records the zone and reason actually
// reached (Phase F, docs/joe-identity-design.md §2.6).
//
// Decision path:
//  1. Resolve the component's zone (default: "unassigned" if no assignment) —
//     this is independent of the principal set, so it is computed once.
//  2. If that zone does not allow the action at all, deny outright. Phase H
//     keeps this check ahead of the admin short-circuit: admin bypasses the
//     PER-PRINCIPAL grant requirement, NOT the ZONE'S allowed_actions list
//     (the stricter interpretation, see docs/DECISIONS.md D-0011). A zone
//     classified readonly stays readonly even for an admin; the zone
//     classification is a property of the zone, not of the principal.
//  3. If any principal in the set holds dynamic admin status
//     (admin_principals row), permit with ReasonAdminCapability — no
//     per-zone grant required. Phase H: this closes the
//     zone-created-after-bootstrap gap left by Phase C's snapshot grants.
//     A new zone is covered automatically because the check reads the
//     admin status of the principal, not the historical list of zones the
//     admin once held grants on.
//  4. Otherwise permit if any member of the set holds a policy granting the
//     component's zone; deny if none do.
func (e *PolicyEngine) Decide(ctx context.Context, principals PrincipalSet, componentID string, action Action) Decision {
	// Resolve component zone (independent of the principal set).
	zoneID := "unassigned"
	assignment, err := e.repo.GetAssignment(ctx, componentID)
	if err != nil {
		slog.Warn("rbac: failed to get zone assignment, defaulting to unassigned",
			"component_id", componentID, "error", err)
	} else if assignment != nil {
		zoneID = assignment.ZoneID
	}

	// Load the zone to check its allowed_actions.
	zone, err := e.repo.GetZone(ctx, zoneID)
	if err != nil || zone == nil {
		slog.Warn("rbac: zone not found, denying access", "zone_id", zoneID)
		return Decision{Allowed: false, Zone: zoneID, Reason: ReasonZoneNotFound}
	}

	// Check that this action is even permitted within the zone. Admin does
	// NOT widen this — see D-0011.
	if !zone.Allows(action) {
		return Decision{Allowed: false, Zone: zoneID, Reason: ReasonActionNotInZone}
	}

	// Admin short-circuit (Phase H, D-0011). Evaluated before per-zone
	// grants so the admin reason wins when both bases would allow — the
	// audit trail is then unambiguous about which capability mattered.
	for _, principal := range principals {
		isAdmin, adminErr := e.repo.IsAdmin(ctx, string(principal))
		if adminErr != nil {
			slog.Warn("rbac: failed to check admin status, falling back to grant lookup",
				"principal", principal, "error", adminErr)
			continue
		}
		if isAdmin {
			return Decision{Allowed: true, Zone: zoneID, Reason: ReasonAdminCapability}
		}
	}

	// Permit if ANY principal in the set has a policy granting access to the
	// component's zone (union of grants). A lookup failure for one member denies
	// only that member — the others may still grant — so we continue rather
	// than returning. For a size-1 set this is identical to the old behaviour:
	// the single member's failure yields an overall deny.
	for _, principal := range principals {
		policies, err := e.repo.ListPoliciesForPrincipal(ctx, string(principal))
		if err != nil {
			slog.Warn("rbac: failed to list policies, denying this principal",
				"principal", principal, "error", err)
			continue
		}
		for _, p := range policies {
			if p.ZoneID == zoneID {
				return Decision{Allowed: true, Zone: zoneID, Reason: ReasonPolicyAllow}
			}
		}
	}

	return Decision{Allowed: false, Zone: zoneID, Reason: ReasonNoGrant}
}

// HasZoneAccess answers "does ANY principal in the set hold action on
// zoneID?" — the componentless variant of IsAllowed (additive / allow-only,
// same union-of-grants semantics). Used by componentless capabilities like
// regime declare/resolve where there is no infrastructure component to
// gate on. Does NOT consult component_zone_assignments.
//
// Phase G (D-0010, joe-identity-design.md §2.7 + §2.10): the function
// became set-shaped — mirroring IsAllowed/Decide — so incident
// declare/resolve authorization is on the same multi-principal footing
// as everything else. It was deliberately left single-principal in
// Phase B as out-of-chain (the regime/captain path); the captain-wiring
// phase is where it joins. The behaviour for a size-1 set is identical
// to the previous single-principal call: same allow/deny outcome,
// same logged-failure semantics.
//
// Encoding rationale: see the §6-B finding in
// internal/store/migrations/012_regime_rbac.up.sql. Grafting componentless
// capabilities onto the IsAllowed path would either require sentinel
// rows in `components` (creates incidental coupling) or onto the
// 'unassigned' zone (creates incidental over-privilege across every
// unassigned component). HasZoneAccess reuses the existing zone+policy
// data unchanged and adds no new tables.
func (e *PolicyEngine) HasZoneAccess(ctx context.Context, principals PrincipalSet, zoneID string, action Action) bool {
	zone, err := e.repo.GetZone(ctx, zoneID)
	if err != nil || zone == nil {
		slog.Warn("rbac: zone not found in HasZoneAccess, denying", "zone_id", zoneID, "error", err)
		return false
	}
	if !zone.Allows(action) {
		return false
	}
	// Admin short-circuit (Phase H, D-0011). Same semantics as Decide:
	// dynamic admin capability allows without a per-zone grant, but does
	// NOT bypass the zone's allowed_actions list (the check above already
	// gates that). HasZoneAccess returns only a boolean — it has no Reason
	// field — but the same call shape is preserved so a future audit
	// emitter could observe the admin basis on this path too.
	for _, principal := range principals {
		isAdmin, adminErr := e.repo.IsAdmin(ctx, string(principal))
		if adminErr != nil {
			slog.Warn("rbac: failed to check admin status in HasZoneAccess, falling back to grant lookup",
				"principal", principal, "error", adminErr)
			continue
		}
		if isAdmin {
			return true
		}
	}
	// Union of grants: permit if ANY principal in the set holds a
	// matching policy. A lookup failure for one member denies only that
	// member (continue) — the others may still grant. For a size-1 set
	// this is byte-identical to the prior single-principal behaviour
	// (immediate deny), which is the §2.7 regression contract.
	for _, principal := range principals {
		policies, err := e.repo.ListPoliciesForPrincipal(ctx, string(principal))
		if err != nil {
			slog.Warn("rbac: failed to list policies in HasZoneAccess, denying this principal",
				"principal", principal, "error", err)
			continue
		}
		for _, p := range policies {
			if p.ZoneID == zoneID {
				return true
			}
		}
	}
	return false
}
