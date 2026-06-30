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

// credentialConfigStructs is the set of provider config structs whose json tags
// define Joe's authentication surface: the provider discriminator
// (discriminator, provider.go), the static/env-var provider's secret + locator
// (staticConfig, static.go), the kubeconfig-exec provider's locators
// (kubeconfigExecConfig, kubeconfig_exec.go), the static-bearer provider's
// locators (staticBearerConfig, static_bearer.go — env_var/in_cluster), and the
// entra-exchange provider's locators (entraExchangeConfig, entra_exchange.go —
// tenant_id/client_id/client_secret_env_var/federated_token_file, with audience
// excluded as a descriptor below). CredentialBearingFields derives its answer from
// these by reflection, so the field set cannot drift from the structs that
// actually parse it.
func credentialConfigStructs() []reflect.Type {
	return []reflect.Type{
		reflect.TypeFor[discriminator](),
		reflect.TypeFor[staticConfig](),
		reflect.TypeFor[kubeconfigExecConfig](),
		reflect.TypeFor[staticBearerConfig](),
		reflect.TypeFor[entraExchangeConfig](),
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
	sort.Strings(out)
	return out
}
