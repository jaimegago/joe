// Package rbac implements role-based access control for joecored.
// It provides security zones, a policy engine, and HTTP middleware.
package rbac

import "time"

// Action represents an operation that can be performed on an infrastructure component.
type Action string

const (
	// ActionRead covers read-only observations (T1 tier tools).
	ActionRead Action = "read"

	// ActionQuery covers graph, search, and aggregation queries (T1 reads).
	ActionQuery Action = "query"

	// ActionMutate covers writes and mutations (T2/T3 tier tools).
	ActionMutate Action = "mutate"

	// ActionDelete covers destructive operations (T3 tier tools).
	ActionDelete Action = "delete"

	// ActionDeclareIncident is the componentless capability to declare an
	// incident regime (§R2/§R6). Phase 1 Change 5. Granted via a policy
	// entry binding a principal to the seeded 'regime-control' zone;
	// evaluated by PolicyEngine.HasZoneAccess, NOT IsAllowed. See §6-B
	// finding in migrations/012_regime_rbac.up.sql.
	ActionDeclareIncident Action = "declare_incident"

	// ActionResolveIncident is the componentless capability to resolve the
	// incident regime (§R4/§R6). Same encoding as ActionDeclareIncident.
	ActionResolveIncident Action = "resolve_incident"
)

// Zone represents a security zone that groups components by risk level and
// controls which actions are permitted within that zone.
type Zone struct {
	ID             string    `json:"id"              db:"id"`
	Name           string    `json:"name"            db:"name"`
	Description    string    `json:"description"     db:"description"`
	AllowedActions []Action  `json:"allowed_actions" db:"-"`
	CreatedAt      time.Time `json:"created_at"      db:"created_at"`
}

// Allows reports whether the given action is permitted in this zone.
func (z Zone) Allows(a Action) bool {
	for _, allowed := range z.AllowedActions {
		if allowed == a {
			return true
		}
	}
	return false
}

// ComponentZoneAssignment records which zone a component belongs to.
type ComponentZoneAssignment struct {
	ComponentID string    `json:"component_id"   db:"component_id"`
	ZoneID      string    `json:"zone_id"     db:"zone_id"`
	AssignedBy  string    `json:"assigned_by" db:"assigned_by"`
	Reason      string    `json:"reason"      db:"reason"`
	AssignedAt  time.Time `json:"assigned_at" db:"assigned_at"`
}

// Policy records that a principal has access to a security zone.
type Policy struct {
	ID        int64     `json:"id"          db:"id"`
	Principal string    `json:"principal"   db:"principal"`
	ZoneID    string    `json:"zone_id"     db:"zone_id"`
	CreatedAt time.Time `json:"created_at"  db:"created_at"`
}

// Admin records that a principal holds dynamic admin status — the
// authorization-decision capability introduced by Phase H (see
// docs/reference/joe-identity-design.md §2.9, docs/project/DECISIONS.md D-0011).
//
// Admin is a property of the principal, not of a (principal, zone) pair:
// holding the row means the policy engine allows the principal on any zone
// for any action the zone itself permits, with no per-zone rbac_policies
// grant required. The single source of truth is the admin_principals
// table; rbac_policies rows for an admin are redundant (the policy engine
// short-circuits to allow before consulting them) and the bootstrap path
// removes any leftover ones so the source-of-truth property is structural.
//
// Admin does NOT bypass the zone's allowed_actions list. A zone classified
// readonly stays readonly even for an admin; "I have admin authority" is
// not the same as "I can change what kind of operation a zone is for".
// The interpretation choice and its justification are in D-0011.
type Admin struct {
	Principal string    `json:"principal"  db:"principal"`
	GrantedAt time.Time `json:"granted_at" db:"granted_at"`
	GrantedBy string    `json:"granted_by" db:"granted_by"`
	Reason    string    `json:"reason"     db:"reason"`
}
