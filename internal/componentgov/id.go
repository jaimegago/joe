package componentgov

import (
	"fmt"
	"regexp"
)

// ComponentIDMaxLength is the maximum length of a component ID. Component IDs
// are load-bearing identifiers — URL path segments on every component
// endpoint, RBAC zone-assignment keys, zone-scope enforcement keys in the tool
// executor, graph node stamps, audit targets, and log fields — so the format
// is deliberately narrow.
const ComponentIDMaxLength = 63

// componentIDPattern admits lowercase letters, digits, and hyphens, starting
// and ending with a letter or digit. The empty string and over-length IDs are
// rejected by ValidateComponentID before this pattern is consulted.
var componentIDPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// ComponentIDRule states the format rule in operator-facing terms. It is
// embedded in every rejection so the error names the rule rather than just
// the failure.
const ComponentIDRule = "1-63 characters of lowercase letters (a-z), digits (0-9), and hyphens, starting and ending with a letter or digit"

// ValidateComponentID enforces the component ID format at registration. Both
// registration surfaces pass through it: the governed HTTP create path
// validates the operator-supplied ID, and the register_component agent tool
// asserts its generated ID post-generation so a future type name that breaks
// the format fails loudly instead of silently minting an invalid ID.
// Validation applies at registration only — pre-existing rows are
// grandfathered and never re-checked.
func ValidateComponentID(id string) error {
	if id == "" {
		return fmt.Errorf("component ID is empty: must be %s", ComponentIDRule)
	}
	if len(id) > ComponentIDMaxLength {
		return fmt.Errorf("component ID %q is %d characters long: must be %s", id, len(id), ComponentIDRule)
	}
	if !componentIDPattern.MatchString(id) {
		return fmt.Errorf("component ID %q contains disallowed characters: must be %s", id, ComponentIDRule)
	}
	return nil
}
