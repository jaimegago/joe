package auth

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/rbac"
)

// GrantedByBootstrapAdminEmail is the granted_by value the bootstrap path
// writes to admin_principals.granted_by when the configured auth.admin_email
// match triggers admin designation. The Phase H CLI (`joe admin grant`) uses
// the symmetric "cli" value so an operator reading admin_principals can
// distinguish bootstrap-designated admins from CLI-promoted ones without
// parsing the reason field.
const GrantedByBootstrapAdminEmail = "bootstrap_admin_email"

// BootstrapAdminReason is the reason value the bootstrap path stores in
// admin_principals.reason. CLI promotions pass through the operator's
// --reason flag instead.
const BootstrapAdminReason = "auth.admin_email match"

// Provisioner manages admin authority. In Phase H (see docs/joe-identity-design.md
// §2.9, docs/DECISIONS.md D-0011) admin authority is a DYNAMIC capability
// stored in admin_principals — a principal-scoped row that the policy engine
// reads at decision time and short-circuits to allow on any zone+action the
// zone itself permits. The previous Phase C definition (a snapshot of
// rbac_policies grants on every zone that existed at bootstrap) is removed:
// it left a day-100 gap where zones created AFTER bootstrap were silently
// uncovered.
//
// The provisioner's only automated caller is the OIDC callback's admin
// bootstrap on every matching admin_email login (idempotent). CLI
// promotions (`joe admin grant`) hit the same admin_principals.AddAdmin
// path through the repository directly.
type Provisioner struct {
	repo rbac.Repository
}

// NewProvisioner builds a Provisioner over the RBAC repository.
func NewProvisioner(repo rbac.Repository) *Provisioner {
	return &Provisioner{repo: repo}
}

// GrantAdmin marks principal as a dynamic admin (Phase H, D-0011).
//
// The policy engine's Decide/HasZoneAccess short-circuit to ReasonAdminCapability
// for an admin principal on any zone+action the zone itself permits — including
// zones created AFTER this call, because the check reads admin status, not a
// historical snapshot. The admin's authority is, precisely, "every zone the
// system knows about, now or later, bounded by each zone's allowed_actions".
//
// This call is idempotent: re-running on every admin login is safe (the row
// is upserted with the same granted_by/reason). It does NOT write
// rbac_policies grants — admin authority has one source of truth, the
// admin_principals table. Any leftover snapshot grants for this principal
// (from a pre-Phase-H deployment, or operator-written zone grants that are
// now redundant) are deleted in the same call to enforce single-source-of-
// truth structurally (asserted by
// internal/auth/provision_test.go::TestPhaseH_NoLeftoverSnapshotGrants).
//
// Bootstrap context: the OIDC callback (internal/auth/handlers.go) calls
// this every time a login's verified email matches auth.admin_email. A
// failure fails the login loudly rather than masquerading as a working
// admin.
//
// The returned wasNew reports whether this call was a real privilege
// escalation (the principal was NOT already an admin) versus a repeat grant
// on an existing admin. The AddAdmin upsert advances granted_at on every
// call and so cannot itself distinguish the two; the discriminator is an
// IsAdmin pre-check read here, BEFORE the upsert, so wasNew reflects the
// state prior to this call. Keeping the read and the upsert inside this one
// method keeps them atomic from the caller's view — callers must not split
// the IsAdmin check and the grant across two calls (a TOCTOU window).
// wasNew lets the OIDC callback audit first-time escalations exactly once
// (internal/auth/handlers.go::recordAdminGrantAudit) without auditing
// per-login repeats.
func (p *Provisioner) GrantAdmin(ctx context.Context, principal rbac.Principal) (wasNew bool, err error) {
	already, err := p.repo.IsAdmin(ctx, string(principal))
	if err != nil {
		return false, fmt.Errorf("auth: check existing admin status: %w", err)
	}
	if err := p.repo.AddAdmin(ctx, rbac.Admin{
		Principal: string(principal),
		GrantedBy: GrantedByBootstrapAdminEmail,
		Reason:    BootstrapAdminReason,
	}); err != nil {
		return false, fmt.Errorf("auth: mark admin: %w", err)
	}
	// Single source of truth: any rbac_policies rows for the admin are
	// redundant (the admin capability covers them on every zone) and could
	// hide as leftover bootstrap-snapshot rows. Delete them so admin
	// authority has exactly one storage site. This is also the migration
	// step from the Phase C snapshot definition.
	if _, err := p.repo.DeletePoliciesForPrincipal(ctx, string(principal)); err != nil {
		return false, fmt.Errorf("auth: clean up redundant policies for admin %q: %w", principal, err)
	}
	return !already, nil
}
