package rbac

import (
	"net/http"
	"strings"
)

// APIKeyProvider maps a Bearer token to a principal name.
// It supports a single API key mapped to a principal, keeping backward
// compatibility with the existing single-key authentication model.
type APIKeyProvider struct {
	apiKey    string
	principal Principal
}

// NewAPIKeyProvider creates an identity provider that maps the given apiKey
// to the given principal. When apiKey is empty, all callers resolve to the
// fallback principal (auth-disabled mode).
func NewAPIKeyProvider(apiKey string, principal Principal) *APIKeyProvider {
	if principal == "" {
		principal = "default-operator"
	}
	return &APIKeyProvider{apiKey: apiKey, principal: principal}
}

// Identity extracts the principal from the Authorization header.
// A matching Bearer token returns the configured principal.
// Any other token (or no token) returns Unknown.
func (p *APIKeyProvider) Identity(r *http.Request) Principal {
	// Auth disabled — all callers are treated as the configured principal.
	if p.apiKey == "" {
		return p.principal
	}

	auth := r.Header.Get("Authorization")
	if !strings.EqualFold(auth, "Bearer "+p.apiKey) {
		return Unknown
	}
	return p.principal
}
