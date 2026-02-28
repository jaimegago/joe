// Package rbac implements role-based access control for joecored.
// It provides security zones, a policy engine, and HTTP middleware.
package rbac

import "time"

// Action represents an operation that can be performed on an infrastructure source.
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
)

// Zone represents a security zone that groups sources by risk level and
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

// SourceZoneAssignment records which zone a source belongs to.
type SourceZoneAssignment struct {
	SourceID   string    `json:"source_id"   db:"source_id"`
	ZoneID     string    `json:"zone_id"     db:"zone_id"`
	AssignedBy string    `json:"assigned_by" db:"assigned_by"`
	Reason     string    `json:"reason"      db:"reason"`
	AssignedAt time.Time `json:"assigned_at" db:"assigned_at"`
}

// Policy records that a principal has access to a security zone.
type Policy struct {
	ID        int64     `json:"id"          db:"id"`
	Principal string    `json:"principal"   db:"principal"`
	ZoneID    string    `json:"zone_id"     db:"zone_id"`
	CreatedAt time.Time `json:"created_at"  db:"created_at"`
}
