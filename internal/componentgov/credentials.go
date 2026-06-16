// Package componentgov holds the governed-registration rules that make
// component CREATE/DELETE and the register_component LLM tool a single,
// consistent surface (A003 Stream G). Its job is the credential-less-by-
// construction invariant: a registration writes type + name + non-credential
// routing config only, and credentials enter the system later, exclusively at
// promotion (a different stream). This package owns the ONE list of
// credential-bearing config fields so the HTTP create path and the LLM tool
// path cannot drift in what they reject.
package componentgov

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrCredentialField is the sentinel returned when a submitted component
// config carries an authentication-bearing field at registration time. Callers
// surface it as a clear 4xx (HTTP) or tool error (LLM) — never silently strip
// the field, so an operator/LLM that tried to smuggle a credential in at
// registration learns it was refused.
var ErrCredentialField = errors.New("credential-bearing field not permitted at registration")

// credentialBearingFields is the SINGLE authoritative denylist of config keys
// that constitute authentication. A credential-less registration must carry
// NONE of these.
//
// These are duplicated from — and MUST stay in lockstep with — the json tags
// the credential providers parse (internal/credential), which is the seam
// recorded in DECISIONS.md D-0029:
//
//   - "credential_provider": the provider discriminator
//     (internal/credential/provider.go, discriminator.CredentialProvider).
//     Its mere presence selects a non-default provider, so it is itself a
//     credential-routing signal and is rejected.
//   - "value", "env_var": the static provider's inline secret and its
//     environment-variable locator (internal/credential/static.go,
//     staticConfig.Value / staticConfig.EnvVar).
//   - "kubeconfig", "context", "in_cluster": the kubeconfig-exec provider's
//     locators (internal/credential/kubeconfig_exec.go,
//     kubeconfigExecConfig.Kubeconfig / .Context / .InCluster).
//
// The provider structs are unexported, so there is no clean single source to
// import today; this list is the duplication seam. Adding a future credential
// provider field WITHOUT adding it here would silently re-open a credential
// hole in create — the guard test TestCredentialBearingFields_CoverProviders
// (and the DECISIONS.md entry) flag what must change to make this single-
// sourced: export the field set from internal/credential and consume it here.
var credentialBearingFields = []string{
	"credential_provider",
	"value",
	"env_var",
	"kubeconfig",
	"context",
	"in_cluster",
}

// RejectCredentialFields returns a non-nil error wrapping ErrCredentialField if
// the submitted config carries ANY credential-bearing field at its top level —
// the level at which the credential providers actually parse them. A nil/empty
// config, or a config that is not a JSON object (and so cannot carry these
// named fields), is credential-less and accepted.
//
// This is the shared rejection rule for the HTTP create path (Phase 2) and the
// register_component LLM tool (Phase 4); both call it so the rejected set
// cannot diverge.
func RejectCredentialFields(config json.RawMessage) error {
	if len(config) == 0 {
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(config, &fields); err != nil {
		// Not a JSON object (null, array, scalar, or malformed): it cannot
		// carry the named credential fields the providers parse, so there is
		// nothing to reject here. Structural validity of the config is a
		// separate concern owned by the caller.
		return nil
	}
	for _, f := range credentialBearingFields {
		if _, present := fields[f]; present {
			return fmt.Errorf("%w: %q — credentials are supplied only at promotion, not at registration", ErrCredentialField, f)
		}
	}
	return nil
}
