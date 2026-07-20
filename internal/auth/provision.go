package auth

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/rbac"
)

// GrantedByBootstrapAdminEmail is the granted_by value GrantAdmin writes to
// admin_principals.granted_by. Both of its callers route through GrantAdmin —
// the OIDC admin_email bootstrap and the admin REST surface
// (POST /api/v1/admin/admins) — so an operator reading admin_principals sees a
// single, uniform provenance value for every admin granted on those two paths.
const GrantedByBootstrapAdminEmail = "bootstrap_admin_email"

// BootstrapAdminReason is the reason value GrantAdmin stores in
// admin_principals.reason for every grant it performs.
const BootstrapAdminReason = "auth.admin_email match"

// GrantedByCLI is the granted_by value GrantFirstAdmin writes. It is the
// provenance value migration 016 reserved for the offline CLI path, kept
// distinct from GrantedByBootstrapAdminEmail for the reason that migration
// gives: an operator reading admin_principals can tell the two writers apart
// without parsing the reason field.
const GrantedByCLI = "cli"

// CLIBootstrapReason is the reason value GrantFirstAdmin stores. It is fixed
// rather than operator-supplied: the command has exactly one circumstance in
// which it can run (an empty admin roster), so there is nothing an operator
// could say about a particular invocation that the row does not already imply.
const CLIBootstrapReason = "joe admin bootstrap: first admin on an empty roster"

// ActorCLIBootstrap is the acting principal GrantFirstAdmin records on the
// admin.grant audit row.
//
// There is no authenticated caller on this path, so the actor has to be
// decided rather than defaulted. It is deliberately NOT the granted principal:
// GrantAdmin passes the granted principal as actor because the OIDC bootstrap
// genuinely IS self-escalation by the logging-in user, whereas the service
// account named on the command line performed no action at all — recording it
// would assert a self-escalation that did not happen and would be
// indistinguishable, later, from one that did.
//
// The value names the mechanism instead: an operator with filesystem access to
// the database, acting through the offline CLI. Its "cli:" prefix is not one of
// rbac's reserved principal prefixes (user:, group:, svc:) and nothing in the
// tree mints a principal carrying it, so the value can never collide with — or
// be granted anything as — a real identity.
const ActorCLIBootstrap = "cli:admin-bootstrap"

// adminGrantProvenance carries the three values that distinguish one grant path
// from another: the two admin_principals provenance columns and the acting
// principal on the audit row. GrantAdmin and GrantFirstAdmin differ in these
// and in nothing else about how the row is formed, so they are parameters of
// the shared grant body rather than two copies of it.
type adminGrantProvenance struct {
	grantedBy string
	reason    string
	actor     string
}

// Provisioner manages admin authority. In Phase H (see docs/reference/joe-identity-design.md
// §2.9, docs/project/DECISIONS.md D-0011) admin authority is a DYNAMIC capability
// stored in admin_principals — a principal-scoped row that the policy engine
// reads at decision time and short-circuits to allow on any zone+action the
// zone itself permits. The previous Phase C definition (a snapshot of
// rbac_policies grants on every zone that existed at bootstrap) is removed:
// it left a day-100 gap where zones created AFTER bootstrap were silently
// uncovered.
//
// The provisioner is the single seam every admin grant routes through, so the
// grant-plus-redundant-policy-cleanup invariant is never re-implemented by a
// caller. GrantAdmin serves the OIDC callback's admin bootstrap on every
// matching admin_email login (idempotent) and the admin REST handler
// (POST /api/v1/admin/admins); GrantFirstAdmin serves the offline bootstrap CLI
// (`joe admin bootstrap`), which differs only in its provenance and in being
// refused once any admin exists.
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
	// The acting principal for the bootstrap grant is the logging-in user
	// itself (admin_email self-escalation).
	prov := adminGrantProvenance{
		grantedBy: GrantedByBootstrapAdminEmail,
		reason:    BootstrapAdminReason,
		actor:     string(principal),
	}
	already, err := p.repo.IsAdmin(ctx, string(principal))
	if err != nil {
		return false, fmt.Errorf("auth: check existing admin status: %w", err)
	}
	// The repository records the admin.grant audit row in the same transaction
	// as the AddAdmin upsert.
	if err := p.repo.AddAdmin(ctx, adminRow(principal, prov), prov.actor); err != nil {
		return false, fmt.Errorf("auth: mark admin: %w", err)
	}
	if err := p.dropRedundantPolicies(ctx, principal); err != nil {
		return false, err
	}
	return !already, nil
}

// GrantFirstAdmin marks principal as a dynamic admin ONLY IF no admin exists
// yet, reporting whether it did. A false return with a nil error is the
// refusal — an admin already exists — not a failure.
//
// It is the writer behind the offline bootstrap CLI (`joe admin bootstrap`,
// cmd/joe/admin.go), which exists because a deployment configured with service
// accounts and no identity provider registers no OIDC callback, so the
// admin_email bootstrap writer is structurally absent; requireAdmin genuinely
// enforces; and the last-admin guard prevents falling to zero from one. Zero
// admins is therefore an absorbing state that nothing else in the binary can
// leave. This method opens it exactly once per database.
//
// The one-shot containment is enforced in the repository, not here: the
// emptiness test rides inside the INSERT's own NOT EXISTS predicate
// (rbac.SQLRepository.AddFirstAdmin), so it cannot be separated from the write
// by a concurrent second invocation. Doing the check here — an IsAdmin read
// followed by a write — would reintroduce exactly the window the separate
// repository method exists to close.
//
// Everything else matches GrantAdmin: the same admin_principals row shape, the
// same in-transaction admin.grant audit row, and the same redundant-policy
// cleanup, which is why this routes through the Provisioner rather than
// reaching for AddFirstAdmin directly. Only the provenance differs
// (GrantedByCLI / CLIBootstrapReason / ActorCLIBootstrap). A refused call
// performs no cleanup — nothing was granted, so there is nothing redundant.
func (p *Provisioner) GrantFirstAdmin(ctx context.Context, principal rbac.Principal) (granted bool, err error) {
	prov := adminGrantProvenance{
		grantedBy: GrantedByCLI,
		reason:    CLIBootstrapReason,
		actor:     ActorCLIBootstrap,
	}
	granted, err = p.repo.AddFirstAdmin(ctx, adminRow(principal, prov), prov.actor)
	if err != nil {
		return false, fmt.Errorf("auth: mark first admin: %w", err)
	}
	if !granted {
		return false, nil
	}
	if err := p.dropRedundantPolicies(ctx, principal); err != nil {
		return false, err
	}
	return true, nil
}

// adminRow builds the admin_principals row for a grant of principal under prov.
// One construction site, so the two grant paths cannot drift in row shape.
func adminRow(principal rbac.Principal, prov adminGrantProvenance) rbac.Admin {
	return rbac.Admin{
		Principal: string(principal),
		GrantedBy: prov.grantedBy,
		Reason:    prov.reason,
	}
}

// dropRedundantPolicies enforces the single-source-of-truth invariant after a
// grant: any rbac_policies rows for the admin are redundant (the admin
// capability covers them on every zone) and could hide as leftover
// bootstrap-snapshot rows, so admin authority has exactly one storage site.
// This is also the migration step from the Phase C snapshot definition.
func (p *Provisioner) dropRedundantPolicies(ctx context.Context, principal rbac.Principal) error {
	if _, err := p.repo.DeletePoliciesForPrincipal(ctx, string(principal)); err != nil {
		return fmt.Errorf("auth: clean up redundant policies for admin %q: %w", principal, err)
	}
	return nil
}
