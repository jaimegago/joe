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
