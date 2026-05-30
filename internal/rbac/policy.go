package rbac

import (
	"context"
	"log/slog"
)

// PolicyEngine answers "can this principal perform this action on this source?"
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
	// Zone is the source's resolved zone — "unassigned" by default, the
	// assignment's zone when set. Never empty for a real decision.
	Zone string
	// Reason is a short machine-readable tag explaining the OUTCOME:
	//   - "policy_allow"        — at least one principal had a matching grant
	//   - "zone_not_found"      — the resolved zone is missing from the table
	//   - "action_not_in_zone"  — the zone does not allow this action at all
	//   - "no_grant"             — the action is in-zone but no principal holds it
	// The accessor records this in the audit row's reason column.
	Reason string
}

// IsAllowed returns true if ANY principal in the set may perform action on
// sourceID — the union-of-grants decision (additive / allow-only). A size-1
// set reproduces the previous single-principal decision exactly, which is the
// Phase B regression contract (docs/joe-identity-design.md §2.7). Thin
// wrapper over Decide.
func (e *PolicyEngine) IsAllowed(ctx context.Context, principals PrincipalSet, sourceID string, action Action) bool {
	return e.Decide(ctx, principals, sourceID, action).Allowed
}

// Decide is the full-fidelity decision call: returns the same outcome as
// IsAllowed and also the resolved zone and a structured reason. The guarded
// accessor calls this so the audit row records the zone and reason actually
// reached (Phase F, docs/joe-identity-design.md §2.6).
//
// Decision path:
//  1. Resolve the source's zone (default: "unassigned" if no assignment) —
//     this is independent of the principal set, so it is computed once.
//  2. If that zone does not allow the action at all, deny outright.
//  3. Otherwise permit if any member of the set holds a policy granting the
//     source's zone; deny if none do.
func (e *PolicyEngine) Decide(ctx context.Context, principals PrincipalSet, sourceID string, action Action) Decision {
	// Resolve source zone (independent of the principal set).
	zoneID := "unassigned"
	assignment, err := e.repo.GetAssignment(ctx, sourceID)
	if err != nil {
		slog.Warn("rbac: failed to get zone assignment, defaulting to unassigned",
			"source_id", sourceID, "error", err)
	} else if assignment != nil {
		zoneID = assignment.ZoneID
	}

	// Load the zone to check its allowed_actions.
	zone, err := e.repo.GetZone(ctx, zoneID)
	if err != nil || zone == nil {
		slog.Warn("rbac: zone not found, denying access", "zone_id", zoneID)
		return Decision{Allowed: false, Zone: zoneID, Reason: "zone_not_found"}
	}

	// Check that this action is even permitted within the zone.
	if !zone.Allows(action) {
		return Decision{Allowed: false, Zone: zoneID, Reason: "action_not_in_zone"}
	}

	// Permit if ANY principal in the set has a policy granting access to the
	// source's zone (union of grants). A lookup failure for one member denies
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
				return Decision{Allowed: true, Zone: zoneID, Reason: "policy_allow"}
			}
		}
	}

	return Decision{Allowed: false, Zone: zoneID, Reason: "no_grant"}
}

// HasZoneAccess answers "does principal hold action on zoneID?" — the
// sourceless variant of IsAllowed. Used by sourceless capabilities like
// regime declare/resolve where there is no infrastructure source to
// gate on. Does NOT consult source_zone_assignments.
//
// Encoding rationale: see the §6-B finding in
// internal/store/migrations/012_regime_rbac.up.sql. Grafting sourceless
// capabilities onto the IsAllowed path would either require sentinel
// rows in `sources` (creates incidental coupling) or onto the
// 'unassigned' zone (creates incidental over-privilege across every
// unassigned source). HasZoneAccess reuses the existing zone+policy
// data unchanged and adds no new tables.
func (e *PolicyEngine) HasZoneAccess(ctx context.Context, principal Principal, zoneID string, action Action) bool {
	zone, err := e.repo.GetZone(ctx, zoneID)
	if err != nil || zone == nil {
		slog.Warn("rbac: zone not found in HasZoneAccess, denying", "zone_id", zoneID, "error", err)
		return false
	}
	if !zone.Allows(action) {
		return false
	}
	policies, err := e.repo.ListPoliciesForPrincipal(ctx, string(principal))
	if err != nil {
		slog.Warn("rbac: failed to list policies in HasZoneAccess, denying",
			"principal", principal, "error", err)
		return false
	}
	for _, p := range policies {
		if p.ZoneID == zoneID {
			return true
		}
	}
	return false
}
