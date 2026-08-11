package credential

import (
	"reflect"
	"sort"
	"strings"
)

// fields.go exports the authoritative set of component-config JSON keys that
// constitute authentication — the credential_provider discriminator plus every
// wired provider's secret/locator field. This package OWNS the provider config
// structs that parse these tags, so it is the single correct source of the
// credential-bearing-field fact. internal/componentgov consumes
// CredentialBearingFields() to reject credentials at registration; before this
// accessor existed it maintained its own hand-copied literal that mirrored these
// tags with no shared source (the seam D-0029 flagged), so a new provider field
// could silently escape the denylist. Deriving the set from the structs here
// closes that seam: a new authentication field added to a provider config struct
// flows into the set automatically, or must be consciously excluded below.

// nonCredentialConfigFields are json-tagged fields on the provider config structs
// that are NOT authentication-bearing — descriptive/routing metadata an operator
// may legitimately set at registration. "audience" names the token's intended
// audience for display and diagnostics; it is neither a secret nor a secret
// locator, so it is explicitly excluded from the credential-bearing set. Any
// field added to a provider config struct that is NOT listed here is treated as
// credential-bearing by default — the safe direction.
var nonCredentialConfigFields = map[string]struct{}{
	"audience": {},
}

// retiredInlineAuthFields are json keys that are NOT parsed by any live provider
// config struct but ARE authentication material, so they must stay in the
// registration denylist regardless.
//
// They are the git adapter's former inline auth fields. Before D-0150 the git
// component config carried `http_token` (a literal HTTPS token) and
// `ssh_key_path` (a path to a private key) and the adapter consumed them
// directly, outside the credential-provider seam. That seam is now the only path
// — git resolves a static reference or arms explicitly no-credential — so the
// fields are gone from the struct the adapter parses. Deleting them from the
// struct stops the adapter READING them; it does not stop a registration
// SUBMITTING them, and the reflection derivation below can only see fields that
// still exist. Without this declaration
// `{"url":"...","http_token":"ghp_..."}` would be accepted at registration and
// persisted as an inert field holding a live secret — an inline credential
// arriving outside the promotion boundary, which is precisely what
// RejectCredentialFields exists to prevent.
//
// `auth_type` is deliberately NOT listed: it was a discriminator, not credential
// material, and after the struct deletion it is simply an ignored unknown field.
//
// TestCredentialBearingFields_IncludeRetiredInlineAuthFields pins this, so
// removing the declaration fails a test rather than silently reopening the hole.
var retiredInlineAuthFields = []string{
	"http_token",
	"ssh_key_path",
}

// credentialConfigStructs is the set of provider config structs whose json tags
// define Joe's authentication surface: the provider discriminator
// (discriminator, provider.go), the static/env-var provider's secret + locator
// (staticConfig, static.go), the static-bearer provider's locators
// (staticBearerConfig, static_bearer.go — env_var/in_cluster), and the
// entra-exchange provider's locators (entraExchangeConfig, entra_exchange.go —
// tenant_id/client_id/client_secret_env_var/federated_token_file, with audience
// excluded as a descriptor below). CredentialBearingFields derives its answer from
// these by reflection, so the field set cannot drift from the structs that
// actually parse it.
func credentialConfigStructs() []reflect.Type {
	return []reflect.Type{
		reflect.TypeFor[discriminator](),
		reflect.TypeFor[staticConfig](),
		reflect.TypeFor[staticBearerConfig](),
		reflect.TypeFor[entraExchangeConfig](),
		reflect.TypeFor[noneConfig](),
	}
}

// CredentialBearingFields returns the deduplicated, sorted set of top-level
// component-config JSON keys that constitute authentication. It is THE single
// source the registration governance (internal/componentgov) consults to reject
// credentials at registration and the promotion boundary uses to clear a prior
// credential reference on re-arm — single-sourced WITH the providers that parse
// the fields, not duplicated against them (D-0029 seam closure). The order is
// sorted for deterministic comparison in guard tests.
func CredentialBearingFields() []string {
	seen := map[string]struct{}{}
	var out []string
	for _, t := range credentialConfigStructs() {
		for i := 0; i < t.NumField(); i++ {
			name := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]
			if name == "" || name == "-" {
				continue
			}
			if _, skip := nonCredentialConfigFields[name]; skip {
				continue
			}
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			out = append(out, name)
		}
	}
	// Authentication fields no live provider struct parses any more, but which a
	// registration could still submit. See retiredInlineAuthFields.
	for _, name := range retiredInlineAuthFields {
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
