package credential

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// requirements.go is the DESCRIBE-ONLY companion to the wiring registry
// (wiring.go): for each provider Kind it declares what a credential REFERENCE
// must contain — which locator fields an operator supplies, which are required,
// and the Kind-level cross-field rules. The promotion-requirements read endpoint
// (GET /api/v1/components/{id}/promotion-requirements) composes its response from
// this so the operator UI can render the correct provider-conditional form, and a
// guard test (internal/api) pins it to buildArmedConfig's live inline enforcement
// (internal/api/components.go).
//
// This table drives NO enforcement in this node: buildArmedConfig remains the
// sole authority that validates a reference and arms a component. Collapsing the
// two declarations into one (validate-FROM-the-table) is the deferred refactor in
// docs/backlog/promotion-requirements-single-source.md; until then the guard test
// is what makes leaving buildArmedConfig untouched safe.

// Constraint rule identifiers — stable, machine-readable names the form switches
// on to render the right cross-field affordance and message.
const (
	// ConstraintForbidInlineValue marks fields that are inline secrets and must
	// NOT be supplied; the reference is an indirection only. (static: "value")
	ConstraintForbidInlineValue = "forbid-inline-value"
	// ConstraintAtLeastOneOf requires that at least one of the named fields be
	// supplied. (kubeconfig-exec: "in_cluster" or "kubeconfig"). This mirrors the
	// LIVE buildArmedConfig rule, which accepts BOTH being set — it is
	// at-least-one, NOT exactly-one.
	ConstraintAtLeastOneOf = "at-least-one-of"
)

// FieldRequirement describes one locator field an operator supplies as part of a
// credential reference for a Kind: the JSON key name and whether it is required
// on its own. A field that is optional individually but participates in a
// cross-field rule carries Required=false; the rule lives in
// Requirements.Constraints.
type FieldRequirement struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
}

// Constraint is a Kind-level cross-field rule no single per-field flag can
// express. Fields are the JSON keys the rule ranges over; Message is the
// human-facing description the form renders.
type Constraint struct {
	Rule    string   `json:"rule"`
	Fields  []string `json:"fields"`
	Message string   `json:"message"`
}

// Requirements is the describe-only declaration of a Kind's credential-reference
// shape: the per-field required flags plus the Kind-level cross-field
// constraints. It mirrors buildArmedConfig's inline enforcement (guard-tested)
// but enforces nothing itself.
type Requirements struct {
	Kind        Kind               `json:"kind"`
	Fields      []FieldRequirement `json:"fields"`
	Constraints []Constraint       `json:"constraints"`
}

// promotionRequirements is the per-Kind requirements table. Its accept/reject
// semantics are pinned to buildArmedConfig by a guard test in internal/api — do
// not change one without the other.
var promotionRequirements = map[Kind]Requirements{
	KindStatic: {
		Kind: KindStatic,
		Fields: []FieldRequirement{
			{Name: "env_var", Required: true},
		},
		Constraints: []Constraint{
			{
				Rule:    ConstraintForbidInlineValue,
				Fields:  []string{"value"},
				Message: "an inline credential value is not accepted; supply an env_var indirection (the armed record carries a reference, not a secret)",
			},
		},
	},
	KindKubeconfigExec: {
		Kind: KindKubeconfigExec,
		Fields: []FieldRequirement{
			{Name: "in_cluster", Required: false},
			{Name: "kubeconfig", Required: false},
			{Name: "context", Required: false},
		},
		Constraints: []Constraint{
			{
				Rule:    ConstraintAtLeastOneOf,
				Fields:  []string{"in_cluster", "kubeconfig"},
				Message: "supply either in_cluster=true or a kubeconfig path",
			},
		},
	},
}

// PromotionRequirements returns the describe-only requirements for a provider
// Kind and whether the Kind has an entry. The promotion-requirements endpoint
// composes its response from this.
func PromotionRequirements(kind Kind) (Requirements, bool) {
	r, ok := promotionRequirements[kind]
	return r, ok
}

// kindConfigStruct maps a Kind to the provider config struct whose json tags
// define that Kind's locator surface.
func kindConfigStruct(kind Kind) (reflect.Type, bool) {
	switch kind {
	case KindStatic:
		return reflect.TypeFor[staticConfig](), true
	case KindKubeconfigExec:
		return reflect.TypeFor[kubeconfigExecConfig](), true
	default:
		return nil, false
	}
}

// KindLocatorFields returns the sorted locator JSON field names declared by a
// Kind's provider config struct — every json-tagged field EXCEPT the
// credential_provider discriminator and the non-credential descriptors
// (nonCredentialConfigFields, e.g. audience). These are the field names that may
// legitimately appear in a credential reference for that Kind. Unlike
// CredentialBearingFields it is per-Kind (not deduped across structs) and drops
// the discriminator, so the requirements endpoint and guard test can check the
// table's named fields against the real struct — the table can never name a field
// the provider struct does not have.
func KindLocatorFields(kind Kind) ([]string, bool) {
	t, ok := kindConfigStruct(kind)
	if !ok {
		return nil, false
	}
	var out []string
	for i := 0; i < t.NumField(); i++ {
		name := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		if name == "credential_provider" {
			continue
		}
		if _, skip := nonCredentialConfigFields[name]; skip {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, true
}

// ValidateReference evaluates a set of supplied locator fields against this
// Kind's describe-only requirements and returns nil iff the reference would be
// accepted. present maps each supplied locator field name to true (a field is
// "present" when the operator supplied a non-empty/true value for it). This is a
// GENERIC interpretation of the declaration — not a copy of buildArmedConfig's
// branching — so the guard test that runs both and asserts they agree fails the
// instant the table diverges from the handler. The credential_provider
// discriminator and the audience descriptor are always permitted.
func (r Requirements) ValidateReference(present map[string]bool) error {
	allowed := make(map[string]bool, len(r.Fields))
	for _, f := range r.Fields {
		allowed[f.Name] = true
	}
	// Any supplied field that is neither a declared locator for this Kind nor a
	// shared descriptor is forbidden. This covers static's inline value and
	// cross-Kind contamination (e.g. a kubeconfig field on a static reference).
	for name, ok := range present {
		if !ok {
			continue
		}
		if name == "credential_provider" || name == "audience" {
			continue
		}
		if !allowed[name] {
			return fmt.Errorf("field %q is not a valid locator for kind %q", name, r.Kind)
		}
	}
	// Every individually-required field must be supplied.
	for _, f := range r.Fields {
		if f.Required && !present[f.Name] {
			return fmt.Errorf("field %q is required for kind %q", f.Name, r.Kind)
		}
	}
	// Kind-level cross-field rules.
	for _, c := range r.Constraints {
		switch c.Rule {
		case ConstraintForbidInlineValue:
			for _, fld := range c.Fields {
				if present[fld] {
					return fmt.Errorf("inline %q is not accepted for kind %q", fld, r.Kind)
				}
			}
		case ConstraintAtLeastOneOf:
			satisfied := false
			for _, fld := range c.Fields {
				if present[fld] {
					satisfied = true
					break
				}
			}
			if !satisfied {
				return fmt.Errorf("kind %q requires at least one of %v", r.Kind, c.Fields)
			}
		}
	}
	return nil
}
