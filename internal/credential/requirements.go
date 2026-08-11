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
	// supplied. (static-bearer: "env_var" or "in_cluster"). This mirrors the
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
	KindNone: {
		Kind: KindNone,
		// No fields and no constraints: the discriminator IS the whole reference.
		// An empty declaration is not an oversight — ValidateReference reads it as
		// "nothing is required, and any supplied locator is forbidden", which is
		// exactly the shape of an explicit no-credential arm.
		Fields:      []FieldRequirement{},
		Constraints: []Constraint{},
	},
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
	KindStaticBearer: {
		Kind: KindStaticBearer,
		Fields: []FieldRequirement{
			{Name: "env_var", Required: false},
			{Name: "in_cluster", Required: false},
		},
		Constraints: []Constraint{
			{
				Rule:    ConstraintAtLeastOneOf,
				Fields:  []string{"env_var", "in_cluster"},
				Message: "supply either an env_var name (the variable the bearer token is read from) or in_cluster=true (the pod-mounted service-account token)",
			},
		},
	},
	KindEntraExchange: {
		Kind: KindEntraExchange,
		Fields: []FieldRequirement{
			{Name: "tenant_id", Required: true},
			{Name: "client_id", Required: true},
			// audience is a descriptor (nonCredentialConfigFields) but is REQUIRED
			// for entra-exchange — it is the per-resolution scope. Declared here so
			// the form renders it required and the guard test sees it; the
			// always-permitted special-case in ValidateReference is relaxed for this
			// Kind, and buildArmedConfig is the live authority that enforces it.
			{Name: "audience", Required: true},
			// client_secret_env_var is a DISTINCT field from static-bearer's env_var
			// so it is intentionally exempt from the env-var uniqueness guard (one
			// Azure app registration may front many components). The secret is
			// resolved by reference, never inline.
			{Name: "client_secret_env_var", Required: false},
		},
		Constraints: []Constraint{
			{
				// At-least-one-of the credential SOURCES: the built client-secret
				// reference today, plus the designed-for federated-assertion source
				// (federated_token_file) reserved so it slots in additively without
				// disturbing the client-secret source. Only client_secret_env_var is
				// produced this slice.
				Rule:    ConstraintAtLeastOneOf,
				Fields:  []string{"client_secret_env_var", "federated_token_file"},
				Message: "supply client_secret_env_var (the variable the Entra client secret is read from)",
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
	case KindStaticBearer:
		return reflect.TypeFor[staticBearerConfig](), true
	case KindEntraExchange:
		return reflect.TypeFor[entraExchangeConfig](), true
	case KindNone:
		return reflect.TypeFor[noneConfig](), true
	default:
		return nil, false
	}
}

// KindLocatorFields returns the sorted JSON field names a Kind's provider config
// struct declares that may legitimately appear in a credential reference — every
// json-tagged field EXCEPT the credential_provider discriminator (the routing tag,
// not a reference field). It INCLUDES the descriptor fields (e.g. audience): a
// descriptor legitimately appears in a reference, and for some Kinds it is a
// required input (entra-exchange's audience is the per-resolution scope). Unlike
// CredentialBearingFields it is per-Kind (not deduped across structs); the guard
// test uses it to check the table's named fields and constraint fields against the
// real struct — the table can never name a field the provider struct does not have.
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
	// The credential_provider discriminator is always permitted. audience is a
	// shared descriptor permitted by default, but a Kind that DECLARES audience as
	// a field (entra-exchange, where it is the required per-resolution scope) falls
	// through to normal field validation so its required flag is enforced — the
	// relaxation that keeps this predicate in agreement with buildArmedConfig.
	for name, ok := range present {
		if !ok {
			continue
		}
		if name == "credential_provider" {
			continue
		}
		if name == "audience" && !allowed["audience"] {
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
