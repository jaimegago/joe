package componentgov

import (
	"encoding/json"
	"fmt"

	"github.com/jaimegago/joe/internal/store"
)

// componentIDReferenceFields declares, per component type, the registration-config
// keys whose VALUE is another component's id. A key listed here is shape-validated
// at registration against the same format rule the id itself must satisfy.
//
// Shape only, deliberately. A reference is NOT checked for existence: the named
// component may legitimately be registered later, or be deleted afterwards, and
// making registration order-dependent would be a worse defect than a reference
// that resolves to nothing. What consumes the reference decides what a dangling
// value means — for git's provider_component_id the graph refresher logs and
// emits no edge (D-0150).
var componentIDReferenceFields = map[string][]string{
	store.ComponentTypeGit: {"provider_component_id"},
}

// ValidateRegistrationConfig checks the non-credential shape rules a registration
// config must satisfy for its component type. It is the third shared registration
// seam beside RejectCredentialFields and NormalizeRegistrationConfig, called by
// both registration surfaces — the governed HTTP create path and the
// register_component agent tool — so the two cannot drift in what they accept.
//
// Today it validates declared component-id references. A config that is absent,
// empty, or not a JSON object carries no such reference and is accepted; a
// present-but-empty reference is treated as absent (the field is optional and an
// empty selection is legal).
func ValidateRegistrationConfig(componentType string, config json.RawMessage) error {
	fields, ok := componentIDReferenceFields[componentType]
	if !ok || len(config) == 0 {
		return nil
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(config, &decoded); err != nil {
		// Not a JSON object: it cannot carry the named fields. Structural validity
		// of the config is the adapter's concern, not this seam's.
		return nil
	}
	for _, field := range fields {
		raw, present := decoded[field]
		if !present {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return fmt.Errorf("%q must be a component ID string", field)
		}
		if value == "" {
			continue
		}
		if err := ValidateComponentID(value); err != nil {
			return fmt.Errorf("%q: %w", field, err)
		}
	}
	return nil
}
