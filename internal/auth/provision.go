package auth

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/rbac"
)

// Provisioner writes RBAC grants. In Phase C its only automated caller is the
// admin bootstrap on first login (design §2.9); the CLI zone-grant command
// writes the same rbac_policies rows through the same repository.
type Provisioner struct {
	repo rbac.Repository
}

// NewProvisioner builds a Provisioner over the RBAC repository.
func NewProvisioner(repo rbac.Repository) *Provisioner {
	return &Provisioner{repo: repo}
}

// GrantAdmin grants principal "admin authority". In Phase C, admin authority is
// defined concretely in terms of existing primitives (see DECISIONS.md D-0006):
// an rbac_policies grant on EVERY security zone currently defined. Because RBAC
// is zone-scoped and additive/allow-only, holding every zone means the
// principal is permitted every action the zones allow on every source assigned
// to any of them — read/query/mutate/delete across prod-readonly, prod-write,
// dev-full and unassigned, plus the sourceless declare/resolve-incident
// capabilities of regime-control.
//
// It is idempotent: zones already granted to the principal are skipped, so the
// bootstrap may safely re-run on every admin login. It does NOT grant any zone
// created after bootstrap — admin is, precisely, "all zones present at the time
// the grant ran" (re-running on the next login picks up newer zones).
func (p *Provisioner) GrantAdmin(ctx context.Context, principal rbac.Principal) error {
	zones, err := p.repo.ListZones(ctx)
	if err != nil {
		return fmt.Errorf("auth: list zones for admin bootstrap: %w", err)
	}
	existing, err := p.repo.ListPoliciesForPrincipal(ctx, string(principal))
	if err != nil {
		return fmt.Errorf("auth: list existing policies for admin bootstrap: %w", err)
	}
	have := make(map[string]bool, len(existing))
	for _, pol := range existing {
		have[pol.ZoneID] = true
	}
	for _, z := range zones {
		if have[z.ID] {
			continue
		}
		if _, err := p.repo.CreatePolicy(ctx, rbac.Policy{Principal: string(principal), ZoneID: z.ID}); err != nil {
			return fmt.Errorf("auth: grant zone %q to admin %q: %w", z.ID, principal, err)
		}
	}
	return nil
}
