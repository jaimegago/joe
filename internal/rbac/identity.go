package rbac

import "net/http"

// Principal is the identity of the caller making a request.
type Principal string

// Unknown is the principal used when identity cannot be determined.
const Unknown Principal = "unknown"

// IdentityProvider extracts a Principal from an HTTP request.
type IdentityProvider interface {
	Identity(r *http.Request) Principal
}
