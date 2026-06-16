// Package componentgov holds the governed-registration rules that make
// component CREATE/DELETE and the register_component LLM tool a single,
// consistent surface (A003 Stream G). Its job is the credential-less-by-
// construction invariant: a registration writes type + name + non-credential
// routing config only, and credentials enter the system later, exclusively at
// promotion (a different stream). This package owns the ONE list of
// credential-bearing config fields so the HTTP create path and the LLM tool
// path cannot drift in what they reject. That rejected set is itself
// single-sourced from internal/credential (the package that owns the provider
// config structs), so it cannot drift from the providers either (D-0029 seam
// closure).
package componentgov

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jaimegago/joe/internal/credential"
)

// ErrCredentialField is the sentinel returned when a submitted component
// config carries an authentication-bearing field at registration time. Callers
// surface it as a clear 4xx (HTTP) or tool error (LLM) — never silently strip
// the field, so an operator/LLM that tried to smuggle a credential in at
// registration learns it was refused.
var ErrCredentialField = errors.New("credential-bearing field not permitted at registration")

// credentialBearingFields is the authoritative set of config keys that
// constitute authentication. A credential-less registration must carry NONE of
// these.
//
// It is single-sourced FROM the credential package
// (credential.CredentialBearingFields), which owns the provider config structs
// and derives the set from the json tags they actually parse:
//
//   - "credential_provider": the provider discriminator
//     (internal/credential/provider.go, discriminator.CredentialProvider).
//   - "value", "env_var": the static provider's inline secret and its
//     environment-variable locator (internal/credential/static.go).
//   - "kubeconfig", "context", "in_cluster": the kubeconfig-exec provider's
//     locators (internal/credential/kubeconfig_exec.go).
//
// Before D-0029's seam closure this was a hand-copied literal mirroring those
// tags with no shared source: a new provider field could silently re-open a
// credential hole in create. Consuming the exported accessor means a new
// authentication field flows into the denylist automatically (or must be
// consciously excluded in the credential package). The divergence guard is
// TestCredentialBearingFields_MatchCredentialPackage.
var credentialBearingFields = credential.CredentialBearingFields()

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
