package rbac

import (
	"fmt"
	"net/http"
	"strings"
)

// Principal is the identity of the caller making a request.
type Principal string

// Unknown is the principal used when identity cannot be determined.
const Unknown Principal = "unknown"

// Reserved principal-kind prefixes (design §2.2). Encoding the kind as a
// reserved prefix is the whole "typing" mechanism — it stops a human named
// alice colliding with a group or service account named alice, at zero schema
// cost. Kubernetes uses the same trick with its reserved system: prefix.
const (
	// PrefixUser tags a human principal keyed on a verified OIDC email:
	// user:<verified-email>. Minted by the Phase C OIDC login flow.
	PrefixUser = "user:"
	// PrefixGroup tags an IdP group. Reserved only — group: members are a v2
	// seam (design §2.7/§6); nothing mints them yet.
	PrefixGroup = "group:"
	// PrefixSvc tags a service account / machine identity (named API keys,
	// Phase D). Reserved now so the CLI can provision svc: grants ahead of D.
	PrefixSvc = "svc:"
)

// reservedPrefixes is the set of kind prefixes an IdP-supplied email must not
// already carry, so that user:<email> cannot be spoofed into another kind.
var reservedPrefixes = []string{PrefixUser, PrefixGroup, PrefixSvc}

// UserPrincipal builds the user:<email> principal for a verified human email
// (design §2.2). It rejects an email that already begins with a reserved kind
// prefix — an impersonation guard that does not trigger in practice, since
// real email local-parts do not start with "user:"/"group:"/"svc:". The caller
// is responsible for having already enforced email_verified == true; this
// function only encodes the principal, it does not vet the assertion.
func UserPrincipal(email string) (Principal, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return "", fmt.Errorf("rbac: cannot mint user principal from empty email")
	}
	lower := strings.ToLower(email)
	for _, p := range reservedPrefixes {
		if strings.HasPrefix(lower, p) {
			return "", fmt.Errorf("rbac: email %q collides with reserved principal prefix %q", email, p)
		}
	}
	return Principal(PrefixUser + email), nil
}

// ServicePrincipal builds the svc:<name> principal for a named service account
// (design §2.4, Phase D). It mirrors UserPrincipal: it rejects an empty name
// and a name that already carries a reserved kind prefix, so that a config
// typo like name "svc:ci" cannot double-encode into "svc:svc:ci" or let a
// service account masquerade as another kind. The svc: prefix is always
// applied here — it is the single point where a service-account name becomes a
// principal, so the prefix invariant holds by construction.
func ServicePrincipal(name string) (Principal, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("rbac: cannot mint service principal from empty name")
	}
	lower := strings.ToLower(name)
	for _, p := range reservedPrefixes {
		if strings.HasPrefix(lower, p) {
			return "", fmt.Errorf("rbac: service-account name %q collides with reserved principal prefix %q", name, p)
		}
	}
	return Principal(PrefixSvc + name), nil
}

// HasReservedPrefix reports whether s begins with one of the reserved kind
// prefixes. Used by CLI provisioning to reject grants to malformed principals.
func HasReservedPrefix(s string) bool {
	for _, p := range reservedPrefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// IdentityProvider extracts a Principal from an HTTP request.
type IdentityProvider interface {
	Identity(r *http.Request) Principal
}
