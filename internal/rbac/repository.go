package rbac

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/store"
)

// Repository provides read/write access to RBAC data.
type Repository interface {
	// Zones
	ListZones(ctx context.Context) ([]Zone, error)
	GetZone(ctx context.Context, id string) (*Zone, error)
	CreateZone(ctx context.Context, z Zone) (*Zone, error)

	// Source → Zone assignments
	ListAssignments(ctx context.Context) ([]SourceZoneAssignment, error)
	GetAssignment(ctx context.Context, sourceID string) (*SourceZoneAssignment, error)
	UpsertAssignment(ctx context.Context, a SourceZoneAssignment) error

	// Policies
	ListPolicies(ctx context.Context) ([]Policy, error)
	ListPoliciesForPrincipal(ctx context.Context, principal string) ([]Policy, error)
	CreatePolicy(ctx context.Context, p Policy) (*Policy, error)
	DeletePolicy(ctx context.Context, id int64) error
	// DeletePolicyForPrincipalZone revokes a single principal→zone grant by its
	// natural key. Returns the number of policy rows removed (0 if the grant
	// did not exist). Used by CLI zone revocation, which keys on
	// (principal, zone) rather than the synthetic policy id.
	DeletePolicyForPrincipalZone(ctx context.Context, principal, zoneID string) (int64, error)

	// Unassigned sources (no zone assignment yet)
	ListUnassignedSourceIDs(ctx context.Context) ([]string, error)
}

// SQLRepository implements Repository on top of a *sql.DB.
type SQLRepository struct {
	db     *sql.DB
	driver string
}

// NewRepository creates a new SQL-backed RBAC repository.
func NewRepository(db *sql.DB, driver string) *SQLRepository {
	return &SQLRepository{db: db, driver: driver}
}

// --- Zones ---

func (r *SQLRepository) ListZones(ctx context.Context) ([]Zone, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, COALESCE(description,''), allowed_actions, created_at
		FROM security_zones ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list zones: %w", err)
	}
	defer rows.Close()

	var zones []Zone
	for rows.Next() {
		var z Zone
		var actionsJSON, createdAtStr string
		if err := rows.Scan(&z.ID, &z.Name, &z.Description, &actionsJSON, &createdAtStr); err != nil {
			return nil, fmt.Errorf("scan zone: %w", err)
		}
		if err := json.Unmarshal([]byte(actionsJSON), &z.AllowedActions); err != nil {
			return nil, fmt.Errorf("parse allowed_actions for zone %s: %w", z.ID, err)
		}
		z.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
		zones = append(zones, z)
	}
	return zones, rows.Err()
}

func (r *SQLRepository) GetZone(ctx context.Context, id string) (*Zone, error) {
	var z Zone
	var actionsJSON, createdAtStr string
	err := r.db.QueryRowContext(ctx, store.Rebind(r.driver, `
		SELECT id, name, COALESCE(description,''), allowed_actions, created_at
		FROM security_zones WHERE id = ?`), id).
		Scan(&z.ID, &z.Name, &z.Description, &actionsJSON, &createdAtStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get zone: %w", err)
	}
	if err := json.Unmarshal([]byte(actionsJSON), &z.AllowedActions); err != nil {
		return nil, fmt.Errorf("parse allowed_actions: %w", err)
	}
	z.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
	return &z, nil
}

func (r *SQLRepository) CreateZone(ctx context.Context, z Zone) (*Zone, error) {
	if z.CreatedAt.IsZero() {
		z.CreatedAt = time.Now().UTC()
	}
	actionsJSON, err := json.Marshal(z.AllowedActions)
	if err != nil {
		return nil, fmt.Errorf("marshal allowed_actions: %w", err)
	}
	_, err = r.db.ExecContext(ctx, store.Rebind(r.driver, `
		INSERT INTO security_zones (id, name, description, allowed_actions, created_at)
		VALUES (?, ?, ?, ?, ?)`),
		z.ID, z.Name, z.Description, string(actionsJSON), z.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("create zone: %w", err)
	}
	return &z, nil
}

// --- Source zone assignments ---

func (r *SQLRepository) ListAssignments(ctx context.Context) ([]SourceZoneAssignment, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT source_id, zone_id, assigned_by, COALESCE(reason,''), assigned_at
		FROM source_zone_assignments ORDER BY source_id`)
	if err != nil {
		return nil, fmt.Errorf("list assignments: %w", err)
	}
	defer rows.Close()

	var out []SourceZoneAssignment
	for rows.Next() {
		var a SourceZoneAssignment
		var assignedAtStr string
		if err := rows.Scan(&a.SourceID, &a.ZoneID, &a.AssignedBy, &a.Reason, &assignedAtStr); err != nil {
			return nil, fmt.Errorf("scan assignment: %w", err)
		}
		a.AssignedAt, _ = time.Parse(time.RFC3339, assignedAtStr)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *SQLRepository) GetAssignment(ctx context.Context, sourceID string) (*SourceZoneAssignment, error) {
	var a SourceZoneAssignment
	var assignedAtStr string
	err := r.db.QueryRowContext(ctx, store.Rebind(r.driver, `
		SELECT source_id, zone_id, assigned_by, COALESCE(reason,''), assigned_at
		FROM source_zone_assignments WHERE source_id = ?`), sourceID).
		Scan(&a.SourceID, &a.ZoneID, &a.AssignedBy, &a.Reason, &assignedAtStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get assignment: %w", err)
	}
	a.AssignedAt, _ = time.Parse(time.RFC3339, assignedAtStr)
	return &a, nil
}

func (r *SQLRepository) UpsertAssignment(ctx context.Context, a SourceZoneAssignment) error {
	if a.AssignedAt.IsZero() {
		a.AssignedAt = time.Now().UTC()
	}
	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		INSERT INTO source_zone_assignments (source_id, zone_id, assigned_by, reason, assigned_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(source_id) DO UPDATE SET
			zone_id     = excluded.zone_id,
			assigned_by = excluded.assigned_by,
			reason      = excluded.reason,
			assigned_at = excluded.assigned_at`),
		a.SourceID, a.ZoneID, a.AssignedBy, a.Reason, a.AssignedAt.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("upsert assignment: %w", err)
	}
	return nil
}

// --- Policies ---

func (r *SQLRepository) ListPolicies(ctx context.Context) ([]Policy, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, principal, zone_id, created_at
		FROM rbac_policies ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list policies: %w", err)
	}
	defer rows.Close()

	var out []Policy
	for rows.Next() {
		var p Policy
		var createdAtStr string
		if err := rows.Scan(&p.ID, &p.Principal, &p.ZoneID, &createdAtStr); err != nil {
			return nil, fmt.Errorf("scan policy: %w", err)
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *SQLRepository) ListPoliciesForPrincipal(ctx context.Context, principal string) ([]Policy, error) {
	rows, err := r.db.QueryContext(ctx, store.Rebind(r.driver, `
		SELECT id, principal, zone_id, created_at
		FROM rbac_policies WHERE principal = ? ORDER BY id`), principal)
	if err != nil {
		return nil, fmt.Errorf("list policies for principal: %w", err)
	}
	defer rows.Close()

	var out []Policy
	for rows.Next() {
		var p Policy
		var createdAtStr string
		if err := rows.Scan(&p.ID, &p.Principal, &p.ZoneID, &createdAtStr); err != nil {
			return nil, fmt.Errorf("scan policy: %w", err)
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *SQLRepository) CreatePolicy(ctx context.Context, p Policy) (*Policy, error) {
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
	}
	err := r.db.QueryRowContext(ctx, store.Rebind(r.driver, `
		INSERT INTO rbac_policies (principal, zone_id, created_at)
		VALUES (?, ?, ?)
		RETURNING id`),
		p.Principal, p.ZoneID, p.CreatedAt.Format(time.RFC3339)).Scan(&p.ID)
	if err != nil {
		return nil, fmt.Errorf("create policy: %w", err)
	}
	return &p, nil
}

func (r *SQLRepository) DeletePolicy(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `DELETE FROM rbac_policies WHERE id = ?`), id)
	if err != nil {
		return fmt.Errorf("delete policy: %w", err)
	}
	return nil
}

func (r *SQLRepository) DeletePolicyForPrincipalZone(ctx context.Context, principal, zoneID string) (int64, error) {
	res, err := r.db.ExecContext(ctx, store.Rebind(r.driver,
		`DELETE FROM rbac_policies WHERE principal = ? AND zone_id = ?`), principal, zoneID)
	if err != nil {
		return 0, fmt.Errorf("delete policy for principal/zone: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return n, nil
}

func (r *SQLRepository) ListUnassignedSourceIDs(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id FROM sources
		WHERE id NOT IN (SELECT source_id FROM source_zone_assignments)
		ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list unassigned sources: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan source id: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
