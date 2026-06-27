package rbac

import (
	"context"
	"log/slog"
)

// PolicyEngine answers "can this principal perform this action on this component?"
// It is backed by the RBAC repository and uses zone assignments + policy tables.
type PolicyEngine struct {
	repo Repository
	// promote is the OPTIONAL auto_promote_reads resolver (A001-COREGOV CC-04).
	// When non-nil it powers the dynamic admit predicate for the agent:core
	// principal on ActionRead: a component whose type has the flag ON is
	// admitted with no materialized grant, resolved live per decision. When nil
	// (every caller that uses NewPolicyEngine — all existing call sites and all
	// test fakes), the predicate is inert and Decide behaves exactly as before;
	// this is the behaviour-neutral default that keeps the predicate out of the
	// main decision path until it is deliberately wired at the engine-build site
	// (cmd/joe/server.go). Kept as a SEPARATE narrow interface rather than two
	// methods on the broad Repository so the many Repository fakes need no
	// change — the lowest-coupling of the two seams CC-04 offered.
	promote PromoteReadsResolver
	// posture is the OPTIONAL install-wide read-posture resolver
	// (read-posture-latch). When non-nil it powers the team_flat read admit:
	// for ActionRead, when the live posture is team_flat, ANY authenticated
	// principal is admitted with no materialized grant, resolved live per
	// decision. When nil (every caller using NewPolicyEngine /
	// NewPolicyEngineWithPromote — all existing call sites and test fakes) the
	// admit is inert and Decide behaves exactly as the grant-based (zoned)
	// decision did before; this is the behaviour-neutral default that keeps the
	// posture admit out of the decision path until it is deliberately wired at
	// the engine-build site (cmd/joe/server.go) via NewPolicyEngineWithGovernance.
	// A SEPARATE narrow interface (not a method on the broad Repository) so the
	// many Repository fakes need no change, exactly as PromoteReadsResolver is.
	posture ReadPostureResolver
}

// PromoteReadsResolver is the minimal live-read seam the auto_promote_reads
// dynamic admit predicate needs (A001-COREGOV CC-04): resolve a componentID to
// its type, and report whether a type has the flag ON. Satisfied by
// internal/promotereads.Repository and injected only at the engine-build site.
// Both reads are live per decision (no cache), so flipping a type ON makes its
// components readable on the NEXT decision with no restart.
type PromoteReadsResolver interface {
	// ComponentType resolves componentID to its component type. Returns
	// ("", nil) for a missing/unknown id so the predicate can fail closed.
	ComponentType(ctx context.Context, componentID string) (string, error)
	// IsPromoted reports whether the component type has auto_promote_reads ON
	// (absent flag => false, the OFF default).
	IsPromoted(ctx context.Context, componentType string) (bool, error)
}

// Read-posture values (read-posture-latch). These are the SAME literals
// internal/readposture defines (PostureTeamFlat / PostureZoned) and the
// migration-028 CHECK pins; the engine compares the resolver's returned string
// against them. Kept as plain literals here (not imported from readposture) so
// internal/rbac does not import internal/readposture — readposture imports rbac
// for these constants and PrincipalFromContext, and the reverse import would
// close a cycle. readposture's guard asserts the two sets stay in sync.
const (
	// PostureTeamFlat: any authenticated principal is admitted for a read
	// action on any component the resolved zone permits to be read, regardless
	// of grant. The launch default.
	PostureTeamFlat = "team_flat"
	// PostureZoned: the grant-based read decision (the full-mode read path) —
	// byte-identical to the pre-posture zone+grant behaviour.
	PostureZoned = "zoned"
)

// ReadPostureResolver is the minimal live-read seam the team_flat read admit
// needs (read-posture-latch): resolve the install-wide read posture per
// decision. Satisfied by internal/readposture.Repository and injected only at
// the engine-build site. The read is live per decision (no cache), so flipping
// the posture takes effect on the NEXT decision with no restart.
type ReadPostureResolver interface {
	// ReadPosture returns the current install-wide read posture
	// (PostureTeamFlat or PostureZoned). An absent/unset value resolves to
	// PostureTeamFlat, the launch default.
	ReadPosture(ctx context.Context) (string, error)
}

// NewPolicyEngine creates a new PolicyEngine. The auto_promote_reads predicate
// is OFF (resolver nil): this constructor's engines decide exactly as they did
// before CC-04. Use NewPolicyEngineWithPromote to enable the predicate.
func NewPolicyEngine(repo Repository) *PolicyEngine {
	return &PolicyEngine{repo: repo}
}

// NewPolicyEngineWithPromote creates a PolicyEngine with the auto_promote_reads
// dynamic admit predicate enabled via the supplied resolver (A001-COREGOV
// CC-04). A nil resolver is equivalent to NewPolicyEngine (predicate inert).
// Only the engine-build site in cmd/joe/server.go uses this; every other call
// site keeps NewPolicyEngine and is unaffected.
func NewPolicyEngineWithPromote(repo Repository, promote PromoteReadsResolver) *PolicyEngine {
	return &PolicyEngine{repo: repo, promote: promote}
}

// NewPolicyEngineWithGovernance creates a PolicyEngine with BOTH live governance
// seams wired (read-posture-latch): the auto_promote_reads predicate (promote)
// and the install-wide read posture (posture). It is the governance-complete
// constructor the engine-build site in cmd/joe/server.go uses so agent:core
// auto-promotion AND the team_flat read admit are both live. A nil resolver for
// either seam is equivalent to leaving that seam inert; passing both nil is
// equivalent to NewPolicyEngine. Every other call site keeps NewPolicyEngine /
// NewPolicyEngineWithPromote and is unaffected.
func NewPolicyEngineWithGovernance(repo Repository, promote PromoteReadsResolver, posture ReadPostureResolver) *PolicyEngine {
	return &PolicyEngine{repo: repo, promote: promote, posture: posture}
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

	// ReasonAutoPromoteRead: the deciding principal is the canonical
	// agent:core service account, the action is ActionRead, and the target
	// component's type has auto_promote_reads ON (A001-COREGOV CC-04). This
	// admit needs no materialized grant — the grant-less pattern the admin
	// short-circuit established — and is read-only by construction: the
	// predicate fires for ActionRead exclusively, so it can never grant a
	// mutate. The distinct reason keeps an auto-promoted read distinguishable
	// from an ordinary grant or admin allow in the audit trail.
	ReasonAutoPromoteRead = "auto_promote_read"

	// ReasonTeamFlatRead: the install-wide read posture is team_flat, the
	// action is ActionRead, and the principal set is non-empty (an
	// authenticated caller). The read is admitted with no materialized grant —
	// the grant-less pattern the admin short-circuit and auto_promote read
	// admit established — and is read-only by construction: the admit fires for
	// ActionRead exclusively, so it can NEVER admit a mutate. The distinct
	// reason keeps a team_flat-admitted read distinguishable from an ordinary
	// grant, an admin allow, or an auto_promote read in the audit trail
	// (read-posture-latch).
	ReasonTeamFlatRead = "team_flat_read"
)

// IsAllowed returns true if ANY principal in the set may perform action on
// componentID — the union-of-grants decision (additive / allow-only). A size-1
// set reproduces the previous single-principal decision exactly, which is the
// Phase B regression contract (docs/reference/joe-identity-design.md §2.7). Thin
// wrapper over Decide.
func (e *PolicyEngine) IsAllowed(ctx context.Context, principals PrincipalSet, componentID string, action Action) bool {
	return e.Decide(ctx, principals, componentID, action).Allowed
}

// Decide is the full-fidelity decision call: returns the same outcome as
// IsAllowed and also the resolved zone and a structured reason. The guarded
// accessor calls this so the audit row records the zone and reason actually
// reached (Phase F, docs/reference/joe-identity-design.md §2.6).
//
// Decision path:
//  1. Resolve the component's zone (default: "unassigned" if no assignment) —
//     this is independent of the principal set, so it is computed once.
//  2. If that zone does not allow the action at all, deny outright. Phase H
//     keeps this check ahead of the admin short-circuit: admin bypasses the
//     PER-PRINCIPAL grant requirement, NOT the ZONE'S allowed_actions list
//     (the stricter interpretation, see docs/project/DECISIONS.md D-0011). A zone
//     classified readonly stays readonly even for an admin; the zone
//     classification is a property of the zone, not of the principal.
//     2a. read-posture-latch: for ActionRead by an authenticated caller, if the
//     live install-wide read posture is team_flat, admit with
//     ReasonTeamFlatRead — no grant required. Bound by the step-2 zone gate
//     (the posture widens WHO may read, never WHICH actions a zone allows) and
//     scoped to ActionRead (never a mutate). Skipped under the zoned posture
//     (or a nil resolver), where the decision below is byte-identical to the
//     pre-posture behaviour.
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

	// team_flat read admit (read-posture-latch). Consulted LIVE per decision
	// from durable storage and bound by the SAME zone.Allows gate above (a zone
	// that forbids read still forbids it — the posture widens WHO may read a
	// permitted action, not WHICH actions a zone permits, exactly as admin and
	// auto_promote do not bypass allowed_actions). It admits ONLY when:
	//   - a posture resolver is wired (nil in every non-build-site engine —
	//     inert, so those engines keep the grant-based zoned decision), AND
	//   - the action is ActionRead — so the admit can NEVER widen a mutate for
	//     any posture; the read/mutate split is independent and unconditional,
	//     and the write floor + write-RBAC govern mutates regardless of posture,
	//     AND
	//   - the principal set is non-empty (an authenticated caller —
	//     unauthenticated callers never reach the engine; they are rejected at
	//     the edge), AND
	//   - the live posture is team_flat.
	// Under team_flat this admit is the dominant reason a read is allowed, so it
	// sits ahead of the auto_promote and admin/grant logic below — all of which
	// are moot when every authenticated principal may already read. A
	// posture-resolve error does NOT admit: it falls through to the grant-based
	// logic (the zoned path), the safe (narrower) direction. When the posture is
	// zoned, this block is skipped and the decision below is byte-identical to
	// the pre-posture behaviour.
	if e.posture != nil && action == ActionRead && len(principals) > 0 {
		posture, perr := e.posture.ReadPosture(ctx)
		if perr != nil {
			slog.Warn("rbac: read-posture resolve failed, falling back to grant-based (zoned) logic",
				"component_id", componentID, "error", perr)
		} else if posture == PostureTeamFlat {
			return Decision{Allowed: true, Zone: zoneID, Reason: ReasonTeamFlatRead}
		}
	}

	// auto_promote_reads dynamic admit predicate (A001-COREGOV CC-04).
	// Evaluated alongside the grant-less admin short-circuit and bound by the
	// SAME zone.Allows gate above (a zone that forbids read still forbids it —
	// the promote flag widens WHO may read a permitted action, not WHICH
	// actions a zone permits, exactly as admin does not bypass allowed_actions
	// per D-0011). It fires ONLY when:
	//   - a resolver is wired (nil in every non-build-site engine — inert), AND
	//   - the action is ActionRead — so the predicate can NEVER grant a mutate
	//     for any type; the read/mutate floor is independent and unconditional,
	//     AND
	//   - the canonical agent:core principal is in the set, AND
	//   - the target component's type has auto_promote_reads ON, resolved live.
	// Fails CLOSED: a missing/unknown componentID (empty resolved type) or any
	// lookup error does NOT admit — it falls through to the normal admin/grant
	// logic, which denies absent any other basis. For any non-agent:core
	// principal the predicate never fires, so their decisions are unchanged.
	if e.promote != nil && action == ActionRead {
		if corePrincipal, perr := AgentCorePrincipal(); perr == nil {
			for _, principal := range principals {
				if principal != corePrincipal {
					continue
				}
				componentType, terr := e.promote.ComponentType(ctx, componentID)
				if terr != nil {
					slog.Warn("rbac: auto_promote_reads component-type resolve failed, not admitting",
						"component_id", componentID, "error", terr)
					break
				}
				if componentType == "" {
					// Unknown/missing component — fail closed.
					break
				}
				promoted, derr := e.promote.IsPromoted(ctx, componentType)
				if derr != nil {
					slog.Warn("rbac: auto_promote_reads flag lookup failed, not admitting",
						"component_type", componentType, "error", derr)
					break
				}
				if promoted {
					return Decision{Allowed: true, Zone: zoneID, Reason: ReasonAutoPromoteRead}
				}
				// Flag OFF for this type — fall through to normal logic.
				break
			}
		}
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
