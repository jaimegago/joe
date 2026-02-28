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

// IsAllowed returns true if principal may perform action on sourceID.
//
// Decision path:
//  1. Look up the zone for sourceID (default: "unassigned" if no assignment).
//  2. Look up all zones the principal has access to via rbac_policies.
//  3. If principal has access to the source's zone and that zone allows action → permit.
//  4. Otherwise → deny.
func (e *PolicyEngine) IsAllowed(ctx context.Context, principal Principal, sourceID string, action Action) bool {
	// Resolve source zone.
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
		return false
	}

	// Check that this action is even permitted within the zone.
	if !zone.Allows(action) {
		return false
	}

	// Check that this principal has a policy granting access to this zone.
	policies, err := e.repo.ListPoliciesForPrincipal(ctx, string(principal))
	if err != nil {
		slog.Warn("rbac: failed to list policies, denying access",
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
