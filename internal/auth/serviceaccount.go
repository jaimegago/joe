package auth

import (
	"fmt"

	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/rbac"
)

// ServiceAccountResolver maps a presented plaintext bearer key to the
// svc:<name> principal it authenticates as (Identity Phase D, design §2.4).
//
// This type is the ISOLATED resolution seam called out in the Phase D seam note
// (DECISIONS D-0007). Everything that makes service-account keys "plaintext in
// config, looked up by exact match" lives here and nowhere else. A future
// upgrade to DB-minted, hashed, runtime-revocable keys replaces ONLY this
// type's storage (the map) and its lookup (Resolve) — the downstream
// principal-in-context flow (EdgeAuth → rbac.WithPrincipal →
// rbac.PrincipalFromContext → accessor/EnforcementMiddleware) is untouched,
// because it depends only on Resolve returning an rbac.Principal.
type ServiceAccountResolver struct {
	byKey map[string]rbac.Principal
}

// NewServiceAccountResolver builds a resolver from the configured service
// accounts. It mints each account's svc:<name> principal through
// rbac.ServicePrincipal (the single point that enforces the svc: prefix and
// rejects names colliding with a reserved prefix), and rejects a configuration
// that would make resolution ambiguous or insecure:
//   - an empty key (a keyless account would authenticate every keyless request),
//   - a duplicate key (one key resolving to two principals is ambiguous), and
//   - a duplicate name (two accounts minting the same principal).
//
// An invalid configuration is a fatal startup error, not a silently-dropped
// entry, so a typo never quietly removes an identity's authority.
func NewServiceAccountResolver(accounts []config.ServiceAccount) (*ServiceAccountResolver, error) {
	byKey := make(map[string]rbac.Principal, len(accounts))
	seenNames := make(map[string]bool, len(accounts))
	for _, sa := range accounts {
		principal, err := rbac.ServicePrincipal(sa.Name)
		if err != nil {
			return nil, fmt.Errorf("service account %q: %w", sa.Name, err)
		}
		if seenNames[sa.Name] {
			return nil, fmt.Errorf("service account %q: duplicate name", sa.Name)
		}
		if sa.Key == "" {
			return nil, fmt.Errorf("service account %q: empty key", sa.Name)
		}
		if existing, dup := byKey[sa.Key]; dup {
			return nil, fmt.Errorf("service account %q: key already used by %q", sa.Name, existing)
		}
		seenNames[sa.Name] = true
		byKey[sa.Key] = principal
	}
	return &ServiceAccountResolver{byKey: byKey}, nil
}

// Resolve returns the principal a presented key authenticates as, and whether
// the key matched a configured account. An unmatched key returns ("", false) —
// the caller treats that exactly as an unauthenticated request, the same as an
// invalid bearer token before Phase D. Nil-safe so callers need not branch.
func (r *ServiceAccountResolver) Resolve(key string) (rbac.Principal, bool) {
	if r == nil || key == "" {
		return "", false
	}
	p, ok := r.byKey[key]
	return p, ok
}

// Configured reports whether any service account is present. It gates whether
// the bearer mechanism is active and (with OIDC) whether authentication is
// enforced at the edge. Nil-safe.
func (r *ServiceAccountResolver) Configured() bool {
	return r != nil && len(r.byKey) > 0
}
