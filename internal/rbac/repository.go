package rbac

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/store"
)

// ErrZoneInUse is returned by DeleteZone when the zone still has at least one
// component assigned to it (component_zone_assignments.zone_id is ON DELETE
// RESTRICT). It is a distinguishable sentinel so a caller — the HTTP handler
// in a later stage — can map it to a 409 Conflict rather than a 500. The
// referencing rbac_policies rows, by contrast, are ON DELETE CASCADE and are
// removed silently with the zone; only the RESTRICT case blocks the delete.
var ErrZoneInUse = errors.New("rbac: zone has component assignments and cannot be deleted")

// Repository provides read/write access to RBAC data.
//
// Every mutating method takes an `actor` (the acting principal) as its final
// argument and writes one append-only audit row in the SAME database
// transaction as the mutation itself: the mutation and its audit row commit or
// roll back together. This moves the audit write down from the HTTP handler
// (where any non-handler caller produced no row) into the repository, so the
// audit guarantee holds for every caller. Authorization/gating is NOT in the
// repository — it stays at the HTTP boundary. When the repository is
// constructed without an audit sink (NewRepository, a test-only constructor)
// the mutation runs directly with no audit row, the same nil-audit carve-out
// the handler's recordAdminAudit and the captaingate wrapper use. No production
// wiring uses the audit-less constructor — the server wires
// NewRepositoryWithAudit (cmd/joe/server.go), the sole writer to RBAC state.
type Repository interface {
	// Zones
	ListZones(ctx context.Context) ([]Zone, error)
	GetZone(ctx context.Context, id string) (*Zone, error)
	CreateZone(ctx context.Context, z Zone, actor string) (*Zone, error)
	// UpdateZone edits a zone's name, description, and allowed_actions for the
	// given zone id. Returns the updated zone, or (nil, nil) if no zone with
	// that id exists.
	UpdateZone(ctx context.Context, z Zone, actor string) (*Zone, error)
	// DeleteZone deletes the zone with the given id. Referencing rbac_policies
	// rows cascade away; if any component is still assigned to the zone the delete
	// is refused with ErrZoneInUse (the RESTRICT foreign key).
	DeleteZone(ctx context.Context, id string, actor string) error

	// Component → Zone assignments
	ListAssignments(ctx context.Context) ([]ComponentZoneAssignment, error)
	GetAssignment(ctx context.Context, componentID string) (*ComponentZoneAssignment, error)
	UpsertAssignment(ctx context.Context, a ComponentZoneAssignment, actor string) error
	// DeleteAssignment removes a component→zone assignment by component id. Returns
	// the number of rows removed (0 if the component had no assignment).
	DeleteAssignment(ctx context.Context, componentID string, actor string) (int64, error)

	// Policies
	ListPolicies(ctx context.Context) ([]Policy, error)
	ListPoliciesForPrincipal(ctx context.Context, principal string) ([]Policy, error)
	CreatePolicy(ctx context.Context, p Policy, actor string) (*Policy, error)
	DeletePolicy(ctx context.Context, id int64, actor string) error
	// DeletePolicyForPrincipalZone revokes a single principal→zone grant by its
	// natural key. Returns the number of policy rows removed (0 if the grant
	// did not exist). Used by the admin REST revoke handler
	// (POST /api/v1/admin/policies/revoke), which keys on (principal, zone)
	// rather than the synthetic policy id.
	DeletePolicyForPrincipalZone(ctx context.Context, principal, zoneID string, actor string) (int64, error)
	// DeletePoliciesForPrincipal removes ALL rbac_policies rows for the given
	// principal in one statement. Returns the number of rows removed. Phase H
	// uses this to enforce "admin authority has exactly one source of truth"
	// (the admin_principals table): on the bootstrap path the configured
	// admin's leftover snapshot grants are cleaned up so the dynamic admin
	// capability is the sole basis for the principal's authority.
	DeletePoliciesForPrincipal(ctx context.Context, principal string) (int64, error)

	// Unassigned components (no zone assignment yet)
	ListUnassignedComponentIDs(ctx context.Context) ([]string, error)

	// Admin status (Phase H, see docs/project/DECISIONS.md D-0011). Admin is a
	// principal-scoped capability, not a (principal, zone) grant. The
	// policy engine reads IsAdmin during Decide/HasZoneAccess and
	// short-circuits to allow if the principal holds the row, subject to
	// the zone's own allowed_actions still being meaningful (see D-0011).
	IsAdmin(ctx context.Context, principal string) (bool, error)
	ListAdmins(ctx context.Context) ([]Admin, error)
	AddAdmin(ctx context.Context, a Admin, actor string) error
	// AddFirstAdmin writes a as admin ONLY IF the roster is empty, reporting
	// whether it did. It is the one-shot seam the offline bootstrap CLI
	// (`joe admin bootstrap`) needs and exists SEPARATELY from AddAdmin
	// because the emptiness test and the write must be one atomic act: a
	// check-then-AddAdmin pair reads outside the write's transaction, and two
	// concurrent invocations could each observe an empty roster and both
	// write. See the implementation for where the atomicity actually lives.
	AddFirstAdmin(ctx context.Context, a Admin, actor string) (bool, error)
	RemoveAdmin(ctx context.Context, principal string, actor string) (int64, error)
}

// SQLRepository implements Repository (and PrincipalRepository) on top of a
// *sql.DB. When audit is non-nil every mutating method writes its audit row in
// the same transaction as the mutation via audit.Repository.InsertTx.
type SQLRepository struct {
	db     *sql.DB
	driver string
	// audit is the append-only audit sink. When nil, mutations run directly
	// on the db handle and write no audit row (the test-only path). When set
	// (production wiring), every mutation and its audit row commit or roll
	// back as one transaction.
	audit audit.Repository
}

// NewRepository creates a new SQL-backed RBAC repository WITHOUT an audit sink.
// Mutations run directly and write no audit row. This is a TEST-ONLY
// constructor: it has no production caller (the operator CLI that once used it
// was removed in Identity Stage 4). All production wiring uses
// NewRepositoryWithAudit so every admin mutation is recorded in the same
// transaction.
func NewRepository(db *sql.DB, driver string) *SQLRepository {
	return &SQLRepository{db: db, driver: driver}
}

// NewRepositoryWithAudit creates a SQL-backed RBAC repository that writes one
// append-only audit row in the same transaction as every mutation. The server
// uses this so the audit guarantee holds for every mutation regardless of which
// caller (HTTP handler today, others later) invoked it.
func NewRepositoryWithAudit(db *sql.DB, driver string, auditRepo audit.Repository) *SQLRepository {
	return &SQLRepository{db: db, driver: driver, audit: auditRepo}
}

// execQuerier is the subset of *sql.DB / *sql.Tx the repository mutations use.
// A mutation runs against this so the SAME handle (the db, or the open
// transaction) performs the before-state read, the write, and — for the
// transactional path — the audit insert, all atomically.
type execQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// mutate runs an audited mutation. The fn performs the before-read and the
// write against the supplied execQuerier and returns the audit Event to record.
//
//   - With no audit sink: fn runs directly on the db handle and the returned
//     Event is discarded (no row) — the nil-audit carve-out.
//   - With an audit sink: a transaction is opened, fn runs against it, the
//     Event is written via InsertTx on the SAME transaction, and both commit
//     together. Any error rolls the whole thing back, so there is no code path
//     where the mutation commits without its audit row.
//
// fn may return a nil-Action Event together with a nil error to signal "no
// mutation happened, do not audit" (e.g. an early return before any write); in
// that case the transaction commits with no audit row.
func (r *SQLRepository) mutate(ctx context.Context, fn func(exec execQuerier) (audit.Event, error)) error {
	if r.audit == nil {
		_, err := fn(r.db)
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	ev, err := fn(tx)
	if err != nil {
		return err
	}
	if ev.Action != "" {
		if err := r.audit.InsertTx(ctx, tx, ev); err != nil {
			return fmt.Errorf("audit insert: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	committed = true
	return nil
}

// auditCtx marshals a Details into the JSON string the audit_log.context column
// carries. A marshal failure aborts the mutation (the row could not be formed),
// matching the handler's prior fail-closed posture for admin mutations.
func auditCtx(d audit.Details) (string, error) {
	blob, err := json.Marshal(d)
	if err != nil {
		return "", fmt.Errorf("marshal audit details: %w", err)
	}
	return string(blob), nil
}

// adminEvent builds a KindAdminAccess "allow" audit Event for a mutating action
// with the given acting principal and details.
func adminEvent(actor, action string, d audit.Details) (audit.Event, error) {
	blob, err := auditCtx(d)
	if err != nil {
		return audit.Event{}, err
	}
	return audit.Event{
		Principal: actor,
		Action:    action,
		Decision:  audit.DecisionAllow,
		Reason:    "admin_mutation",
		Kind:      audit.KindAdminAccess,
		Context:   blob,
	}, nil
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

func (r *SQLRepository) CreateZone(ctx context.Context, z Zone, actor string) (*Zone, error) {
	if z.CreatedAt.IsZero() {
		z.CreatedAt = time.Now().UTC()
	}
	actionsJSON, err := json.Marshal(z.AllowedActions)
	if err != nil {
		return nil, fmt.Errorf("marshal allowed_actions: %w", err)
	}
	err = r.mutate(ctx, func(exec execQuerier) (audit.Event, error) {
		if _, err := exec.ExecContext(ctx, store.Rebind(r.driver, `
			INSERT INTO security_zones (id, name, description, allowed_actions, created_at)
			VALUES (?, ?, ?, ?, ?)`),
			z.ID, z.Name, z.Description, string(actionsJSON), z.CreatedAt.Format(time.RFC3339)); err != nil {
			return audit.Event{}, fmt.Errorf("create zone: %w", err)
		}
		return adminEvent(actor, audit.ActionAdminZoneCreate,
			audit.Details{Target: "zone:" + z.ID, After: z})
	})
	if err != nil {
		return nil, err
	}
	return &z, nil
}

// UpdateZone edits a zone's name, description, and allowed_actions. The prior
// zone state is captured inside the same transaction for the audit Before, and
// the update + audit row commit together. If no zone with the id exists the
// method is a no-op and returns (nil, nil).
func (r *SQLRepository) UpdateZone(ctx context.Context, z Zone, actor string) (*Zone, error) {
	actionsJSON, err := json.Marshal(z.AllowedActions)
	if err != nil {
		return nil, fmt.Errorf("marshal allowed_actions: %w", err)
	}
	var updated *Zone
	err = r.mutate(ctx, func(exec execQuerier) (audit.Event, error) {
		before, gerr := getZoneOn(ctx, exec, r.driver, z.ID)
		if gerr != nil {
			return audit.Event{}, gerr
		}
		if before == nil {
			// Nothing to update; do not write an audit row for a no-op.
			return audit.Event{}, nil
		}
		res, uerr := exec.ExecContext(ctx, store.Rebind(r.driver, `
			UPDATE security_zones SET name = ?, description = ?, allowed_actions = ?
			WHERE id = ?`),
			z.Name, z.Description, string(actionsJSON), z.ID)
		if uerr != nil {
			return audit.Event{}, fmt.Errorf("update zone: %w", uerr)
		}
		if _, aerr := res.RowsAffected(); aerr != nil {
			return audit.Event{}, fmt.Errorf("rows affected: %w", aerr)
		}
		// created_at is immutable; carry it onto the returned/after state.
		z.CreatedAt = before.CreatedAt
		updated = &z
		return adminEvent(actor, audit.ActionAdminZoneUpdate,
			audit.Details{Target: "zone:" + z.ID, Before: *before, After: z})
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// DeleteZone deletes the zone with the given id. If any component is still
// assigned to the zone (component_zone_assignments.zone_id ON DELETE RESTRICT) the
// delete is refused with ErrZoneInUse rather than relying on the driver's
// foreign-key error text. Referencing rbac_policies rows are ON DELETE CASCADE
// and are removed with the zone.
func (r *SQLRepository) DeleteZone(ctx context.Context, id string, actor string) error {
	return r.mutate(ctx, func(exec execQuerier) (audit.Event, error) {
		var assigned int
		if err := exec.QueryRowContext(ctx, store.Rebind(r.driver,
			`SELECT COUNT(*) FROM component_zone_assignments WHERE zone_id = ?`), id).Scan(&assigned); err != nil {
			return audit.Event{}, fmt.Errorf("count zone assignments: %w", err)
		}
		if assigned > 0 {
			return audit.Event{}, ErrZoneInUse
		}
		before, gerr := getZoneOn(ctx, exec, r.driver, id)
		if gerr != nil {
			return audit.Event{}, gerr
		}
		if _, err := exec.ExecContext(ctx, store.Rebind(r.driver,
			`DELETE FROM security_zones WHERE id = ?`), id); err != nil {
			return audit.Event{}, fmt.Errorf("delete zone: %w", err)
		}
		d := audit.Details{Target: "zone:" + id}
		if before != nil {
			d.Before = *before
		}
		return adminEvent(actor, audit.ActionAdminZoneDelete, d)
	})
}

// getZoneOn reads a single zone against the supplied execQuerier (db or tx) so
// the before-state read for an audited zone mutation runs inside the same
// transaction as the mutation. Returns (nil, nil) when the zone is absent.
func getZoneOn(ctx context.Context, exec execQuerier, driver, id string) (*Zone, error) {
	var z Zone
	var actionsJSON, createdAtStr string
	err := exec.QueryRowContext(ctx, store.Rebind(driver, `
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

// --- Component zone assignments ---

func (r *SQLRepository) ListAssignments(ctx context.Context) ([]ComponentZoneAssignment, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT component_id, zone_id, assigned_by, COALESCE(reason,''), assigned_at
		FROM component_zone_assignments ORDER BY component_id`)
	if err != nil {
		return nil, fmt.Errorf("list assignments: %w", err)
	}
	defer rows.Close()

	var out []ComponentZoneAssignment
	for rows.Next() {
		var a ComponentZoneAssignment
		var assignedAtStr string
		if err := rows.Scan(&a.ComponentID, &a.ZoneID, &a.AssignedBy, &a.Reason, &assignedAtStr); err != nil {
			return nil, fmt.Errorf("scan assignment: %w", err)
		}
		a.AssignedAt, _ = time.Parse(time.RFC3339, assignedAtStr)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *SQLRepository) GetAssignment(ctx context.Context, componentID string) (*ComponentZoneAssignment, error) {
	var a ComponentZoneAssignment
	var assignedAtStr string
	err := r.db.QueryRowContext(ctx, store.Rebind(r.driver, `
		SELECT component_id, zone_id, assigned_by, COALESCE(reason,''), assigned_at
		FROM component_zone_assignments WHERE component_id = ?`), componentID).
		Scan(&a.ComponentID, &a.ZoneID, &a.AssignedBy, &a.Reason, &assignedAtStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get assignment: %w", err)
	}
	a.AssignedAt, _ = time.Parse(time.RFC3339, assignedAtStr)
	return &a, nil
}

func (r *SQLRepository) UpsertAssignment(ctx context.Context, a ComponentZoneAssignment, actor string) error {
	if a.AssignedAt.IsZero() {
		a.AssignedAt = time.Now().UTC()
	}
	return r.mutate(ctx, func(exec execQuerier) (audit.Event, error) {
		// Capture prior assignment (an upsert may overwrite one) inside the
		// transaction so the audit Before reflects true prior state.
		before, gerr := getAssignmentOn(ctx, exec, r.driver, a.ComponentID)
		if gerr != nil {
			return audit.Event{}, gerr
		}
		if _, err := exec.ExecContext(ctx, store.Rebind(r.driver, `
			INSERT INTO component_zone_assignments (component_id, zone_id, assigned_by, reason, assigned_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(component_id) DO UPDATE SET
				zone_id     = excluded.zone_id,
				assigned_by = excluded.assigned_by,
				reason      = excluded.reason,
				assigned_at = excluded.assigned_at`),
			a.ComponentID, a.ZoneID, a.AssignedBy, a.Reason, a.AssignedAt.Format(time.RFC3339)); err != nil {
			return audit.Event{}, fmt.Errorf("upsert assignment: %w", err)
		}
		d := audit.Details{Target: "component_zone:" + a.ComponentID, After: a}
		if before != nil {
			d.Before = *before
		}
		return adminEvent(actor, audit.ActionAdminComponentZoneAssign, d)
	})
}

// DeleteAssignment removes a component→zone assignment by component id. Returns the
// number of rows removed (0 if the component had no assignment). The prior
// assignment is captured in-transaction for the audit Before.
func (r *SQLRepository) DeleteAssignment(ctx context.Context, componentID string, actor string) (int64, error) {
	var removed int64
	err := r.mutate(ctx, func(exec execQuerier) (audit.Event, error) {
		before, gerr := getAssignmentOn(ctx, exec, r.driver, componentID)
		if gerr != nil {
			return audit.Event{}, gerr
		}
		res, derr := exec.ExecContext(ctx, store.Rebind(r.driver,
			`DELETE FROM component_zone_assignments WHERE component_id = ?`), componentID)
		if derr != nil {
			return audit.Event{}, fmt.Errorf("delete assignment: %w", derr)
		}
		n, aerr := res.RowsAffected()
		if aerr != nil {
			return audit.Event{}, fmt.Errorf("rows affected: %w", aerr)
		}
		removed = n
		d := audit.Details{Target: "component_zone:" + componentID}
		if before != nil {
			d.Before = *before
		}
		return adminEvent(actor, audit.ActionAdminComponentZoneUnassign, d)
	})
	if err != nil {
		return 0, err
	}
	return removed, nil
}

// getAssignmentOn reads a single component-zone assignment against the supplied
// execQuerier so the before-state read runs inside the mutation's transaction.
// Returns (nil, nil) when no assignment exists for the component.
func getAssignmentOn(ctx context.Context, exec execQuerier, driver, componentID string) (*ComponentZoneAssignment, error) {
	var a ComponentZoneAssignment
	var assignedAtStr string
	err := exec.QueryRowContext(ctx, store.Rebind(driver, `
		SELECT component_id, zone_id, assigned_by, COALESCE(reason,''), assigned_at
		FROM component_zone_assignments WHERE component_id = ?`), componentID).
		Scan(&a.ComponentID, &a.ZoneID, &a.AssignedBy, &a.Reason, &assignedAtStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get assignment: %w", err)
	}
	a.AssignedAt, _ = time.Parse(time.RFC3339, assignedAtStr)
	return &a, nil
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

func (r *SQLRepository) CreatePolicy(ctx context.Context, p Policy, actor string) (*Policy, error) {
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
	}
	err := r.mutate(ctx, func(exec execQuerier) (audit.Event, error) {
		if err := exec.QueryRowContext(ctx, store.Rebind(r.driver, `
			INSERT INTO rbac_policies (principal, zone_id, created_at)
			VALUES (?, ?, ?)
			RETURNING id`),
			p.Principal, p.ZoneID, p.CreatedAt.Format(time.RFC3339)).Scan(&p.ID); err != nil {
			return audit.Event{}, fmt.Errorf("create policy: %w", err)
		}
		return adminEvent(actor, audit.ActionAdminPolicyGrant,
			audit.Details{Target: fmt.Sprintf("policy:%s@%s", p.Principal, p.ZoneID), After: p})
	})
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *SQLRepository) DeletePolicy(ctx context.Context, id int64, actor string) error {
	return r.mutate(ctx, func(exec execQuerier) (audit.Event, error) {
		// Capture the grant being revoked (in-transaction) so the audit Before
		// records what was removed.
		d := audit.Details{Target: fmt.Sprintf("policy:%d", id)}
		var p Policy
		var createdAtStr string
		switch err := exec.QueryRowContext(ctx, store.Rebind(r.driver,
			`SELECT id, principal, zone_id, created_at FROM rbac_policies WHERE id = ?`), id).
			Scan(&p.ID, &p.Principal, &p.ZoneID, &createdAtStr); err {
		case nil:
			p.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
			d.Before = p
		case sql.ErrNoRows:
			// No such grant — the delete below is a no-op; Before stays unset.
		default:
			return audit.Event{}, fmt.Errorf("read policy: %w", err)
		}
		if _, err := exec.ExecContext(ctx, store.Rebind(r.driver,
			`DELETE FROM rbac_policies WHERE id = ?`), id); err != nil {
			return audit.Event{}, fmt.Errorf("delete policy: %w", err)
		}
		return adminEvent(actor, audit.ActionAdminPolicyRevoke, d)
	})
}

func (r *SQLRepository) DeletePolicyForPrincipalZone(ctx context.Context, principal, zoneID string, actor string) (int64, error) {
	var removed int64
	err := r.mutate(ctx, func(exec execQuerier) (audit.Event, error) {
		res, err := exec.ExecContext(ctx, store.Rebind(r.driver,
			`DELETE FROM rbac_policies WHERE principal = ? AND zone_id = ?`), principal, zoneID)
		if err != nil {
			return audit.Event{}, fmt.Errorf("delete policy for principal/zone: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return audit.Event{}, fmt.Errorf("rows affected: %w", err)
		}
		removed = n
		return adminEvent(actor, audit.ActionAdminPolicyRevoke,
			audit.Details{Target: fmt.Sprintf("policy:%s@%s", principal, zoneID)})
	})
	if err != nil {
		return 0, err
	}
	return removed, nil
}

func (r *SQLRepository) DeletePoliciesForPrincipal(ctx context.Context, principal string) (int64, error) {
	res, err := r.db.ExecContext(ctx, store.Rebind(r.driver,
		`DELETE FROM rbac_policies WHERE principal = ?`), principal)
	if err != nil {
		return 0, fmt.Errorf("delete policies for principal: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return n, nil
}

// --- Admin status (Phase H) ---

func (r *SQLRepository) IsAdmin(ctx context.Context, principal string) (bool, error) {
	var one int
	err := r.db.QueryRowContext(ctx, store.Rebind(r.driver,
		`SELECT 1 FROM admin_principals WHERE principal = ?`), principal).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("is admin: %w", err)
	}
	return true, nil
}

func (r *SQLRepository) ListAdmins(ctx context.Context) ([]Admin, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT principal, granted_at, granted_by, reason
		FROM admin_principals ORDER BY principal`)
	if err != nil {
		return nil, fmt.Errorf("list admins: %w", err)
	}
	defer rows.Close()

	var out []Admin
	for rows.Next() {
		var a Admin
		var grantedAtStr string
		if err := rows.Scan(&a.Principal, &grantedAtStr, &a.GrantedBy, &a.Reason); err != nil {
			return nil, fmt.Errorf("scan admin: %w", err)
		}
		a.GrantedAt, _ = time.Parse(time.RFC3339, grantedAtStr)
		out = append(out, a)
	}
	return out, rows.Err()
}

// AddAdmin marks principal as admin. Idempotent: a row that already exists
// is updated in place (granted_at advanced, granted_by/reason replaced).
// Phase H bootstrap relies on the idempotency to safely re-run on every
// matching admin_email login. The choice of UPSERT (vs INSERT-OR-IGNORE)
// is so an operator re-issuing the admin REST grant
// (POST /api/v1/admin/admins) can update the rationale without first revoking.
func (r *SQLRepository) AddAdmin(ctx context.Context, a Admin, actor string) error {
	if a.Principal == "" {
		return fmt.Errorf("add admin: principal is required")
	}
	if a.GrantedAt.IsZero() {
		a.GrantedAt = time.Now().UTC()
	}
	return r.mutate(ctx, func(exec execQuerier) (audit.Event, error) {
		if _, err := exec.ExecContext(ctx, store.Rebind(r.driver, `
			INSERT INTO admin_principals (principal, granted_at, granted_by, reason)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(principal) DO UPDATE SET
				granted_at = excluded.granted_at,
				granted_by = excluded.granted_by,
				reason     = excluded.reason`),
			a.Principal, a.GrantedAt.Format(time.RFC3339), a.GrantedBy, a.Reason); err != nil {
			return audit.Event{}, fmt.Errorf("add admin: %w", err)
		}
		return adminEvent(actor, audit.ActionAdminGrant,
			audit.Details{Target: "admin:" + a.Principal, After: a})
	})
}

// AddFirstAdmin inserts a as admin ONLY IF admin_principals is empty, and
// reports whether the row was written. A false return with a nil error is the
// refusal — the roster was already non-empty — not a failure.
//
// This is the containment clause behind `joe admin bootstrap`: the offline CLI
// can open the zero-admin absorbing state exactly once per database, and every
// later grant is forced through the governed admin REST surface. It is a
// separate method rather than a caller-side IsAdmin check in front of AddAdmin
// because that shape reads outside the write's transaction — two transactions
// with a window between them — which is benign for GrantAdmin's wasNew
// discriminator but genuinely racy for a containment guard.
//
// The atomicity lives in the STATEMENT, not in the transaction's isolation
// level: the NOT EXISTS predicate is evaluated as part of the INSERT, under the
// write lock that statement takes, so the emptiness test and the write cannot
// be separated by another writer. mutate opens a deferred transaction, but this
// method never reads before it writes, so there is no read-then-upgrade window
// for a second writer to slip through either. A concurrent second invocation
// either blocks on the write lock and then observes the row (0 rows affected,
// refused) or is serialized behind the first commit with the same outcome.
//
// A refusal writes NO audit row: nothing changed, so there is nothing to
// record, and mutate skips the insert for a zero Event. The grant path writes
// its admin.grant row inside the same transaction as the INSERT, exactly as
// AddAdmin does.
func (r *SQLRepository) AddFirstAdmin(ctx context.Context, a Admin, actor string) (bool, error) {
	if a.Principal == "" {
		return false, fmt.Errorf("add first admin: principal is required")
	}
	if a.GrantedAt.IsZero() {
		a.GrantedAt = time.Now().UTC()
	}
	var granted bool
	err := r.mutate(ctx, func(exec execQuerier) (audit.Event, error) {
		res, err := exec.ExecContext(ctx, store.Rebind(r.driver, `
			INSERT INTO admin_principals (principal, granted_at, granted_by, reason)
			SELECT ?, ?, ?, ?
			WHERE NOT EXISTS (SELECT 1 FROM admin_principals)`),
			a.Principal, a.GrantedAt.Format(time.RFC3339), a.GrantedBy, a.Reason)
		if err != nil {
			return audit.Event{}, fmt.Errorf("add first admin: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return audit.Event{}, fmt.Errorf("rows affected: %w", err)
		}
		if n == 0 {
			// Refused: the roster was not empty. Zero Event, no audit row.
			return audit.Event{}, nil
		}
		granted = true
		return adminEvent(actor, audit.ActionAdminGrant,
			audit.Details{Target: "admin:" + a.Principal, After: a})
	})
	if err != nil {
		return false, err
	}
	return granted, nil
}

func (r *SQLRepository) RemoveAdmin(ctx context.Context, principal string, actor string) (int64, error) {
	var removed int64
	err := r.mutate(ctx, func(exec execQuerier) (audit.Event, error) {
		res, err := exec.ExecContext(ctx, store.Rebind(r.driver,
			`DELETE FROM admin_principals WHERE principal = ?`), principal)
		if err != nil {
			return audit.Event{}, fmt.Errorf("remove admin: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return audit.Event{}, fmt.Errorf("rows affected: %w", err)
		}
		removed = n
		return adminEvent(actor, audit.ActionAdminRevoke,
			audit.Details{Target: "admin:" + principal})
	})
	if err != nil {
		return 0, err
	}
	return removed, nil
}

func (r *SQLRepository) ListUnassignedComponentIDs(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id FROM components
		WHERE id NOT IN (SELECT component_id FROM component_zone_assignments)
		ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list unassigned components: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan component id: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
