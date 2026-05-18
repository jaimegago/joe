-- Phase 1 Change 5: regime declare/resolve RBAC.
-- See docs/PHASE-1-DECOMPOSITION.md (Change 5, §6-B).
--
-- §6-B FINDING: IsAllowed unmatched-sourceID behavior verified at
-- internal/rbac/policy.go:28-35 — unmatched sourceID defaults to zone
-- 'unassigned' (seeded with allowed_actions = ["read"]). Grafting the
-- regime capabilities onto 'unassigned' would incidentally widen source
-- authority. Encoding chosen: a dedicated sourceless zone
-- 'regime-control', evaluated via the new PolicyEngine.HasZoneAccess
-- helper that does NOT consult source_zone_assignments. Reuses the
-- existing security_zones / rbac_policies shape unchanged. No new
-- tables, no policy_kind column — RBAC is not redesigned.

INSERT INTO security_zones (id, name, description, allowed_actions, created_at) VALUES (
    'regime-control',
    'Regime Control',
    'Sourceless capabilities for declaring and resolving incident regime (§R2 / §R4).',
    '["declare_incident","resolve_incident"]',
    CURRENT_TIMESTAMP
);
