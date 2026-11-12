package credential

import (
	"slices"
	"sort"

	"github.com/jaimegago/joe/internal/store"
)

// registration.go is the SINGLE declared source of truth for which
// non-credential routing fields a registration may carry, per component type
// (D-0168). It sits beside wiredTypes because the two answer the halves of one
// question — what may enter a component's config, and when. wiredTypes says
// which credential provider a type is armed with at promotion; this table says
// which routing fields its adapter reads, what JSON type each must have, and the
// phase at which each becomes required.
//
// Every key an adapter's ParseConfig struct reads is classified exactly once:
//
//   - declared here — a non-credential routing field, accepted at registration;
//   - in adapterSecretFields — a secret the adapter still reads inline, never a
//     registration field;
//   - credential-bearing — in the reflection-derived CredentialBearingFields
//     denylist, entering only at promotion.
//
// TestRegistrationContract_TwoWayAgainstAdapters (in this package's external
// test) holds the classification to the adapters in both directions: a field an
// adapter reads that nobody classified fails the build, and so does a declared
// field no adapter reads.
//
// Registration enforces this table for SHAPE only
// (componentgov.ValidateRegistrationConfig): an undeclared field, a value of the
// wrong JSON type, an unparseable URL, or an absent registration-required field is
// refused. Nothing here resolves, dials, or probes anything — D-0029's
// probe-free create path is unchanged.

// FieldType is the JSON type a registration field's value must have on the wire.
// The declared type governs every caller with no coercion: "123" is not an
// integer and "true" is not a boolean.
type FieldType string

const (
	// FieldString is a JSON string.
	FieldString FieldType = "string"
	// FieldURL is a JSON string that must parse as an absolute URL of the
	// scheme://… form.
	FieldURL FieldType = "url"
	// FieldInteger is a JSON number with an integral literal.
	FieldInteger FieldType = "integer"
	// FieldBoolean is a JSON true or false.
	FieldBoolean FieldType = "boolean"
)

// Phase is the step of a component's lifecycle at which a declared field
// becomes required.
type Phase string

const (
	// PhaseRegistration fields must be present for a registration to succeed. A
	// component missing one could never be given it later — there is no route
	// that edits a component's config — so it is refused at entry (D-0168).
	PhaseRegistration Phase = "registration"
	// PhasePromotion fields enter at promotion, where the promote handler
	// requires them. Registration accepts them but does not require them.
	PhasePromotion Phase = "promotion"
	// PhaseOptional fields are never required; an absent one takes the adapter's
	// default, which the declaration names where one exists.
	PhaseOptional Phase = "optional"
)

// RegistrationField declares one non-credential routing field of a component
// type. The JSON tags are the shape GET /api/v1/component-types serves to the
// Register dialog as the type's descriptor.
type RegistrationField struct {
	// Name is the config key, exactly as the adapter reads it.
	Name string `json:"name"`
	// Type is the JSON type the value must have.
	Type FieldType `json:"type"`
	// Phase is when the field becomes required.
	Phase Phase `json:"phase"`
	// Default is the value the adapter uses when the field is absent, for an
	// optional field that has one; nil when it has none.
	Default any `json:"default,omitempty"`
	// Label is the short operator-facing name the Register dialog shows.
	Label string `json:"label"`
	// Help is an optional one-sentence note the dialog shows under the field.
	Help string `json:"help,omitempty"`
	// RefersTo, when set, marks a string field whose value is another
	// component's id and names the component types it may refer to. Registration
	// checks the value against the component-id format only — never that the
	// component exists or what its type is, because a dangling reference is legal
	// (D-0150). The list is what the dialog offers to pick from.
	RefersTo []string `json:"refers_to,omitempty"`
	// Multiline marks a string the dialog collects in a multi-line input: a
	// single-line input strips the line breaks of a PEM bundle.
	Multiline bool `json:"multiline,omitempty"`
}

// urlField is a registration-required URL locator, the most common shape.
func urlField(label string) RegistrationField {
	return RegistrationField{Name: "url", Type: FieldURL, Phase: PhaseRegistration, Label: label}
}

// orgIDField is the optional multi-tenancy header the Prometheus-family
// backends send as X-Scope-OrgID.
var orgIDField = RegistrationField{
	Name: "org_id", Type: FieldString, Phase: PhaseOptional, Label: "Tenant ID (X-Scope-OrgID)",
	Help: "Only for a multi-tenant backend.",
}

// registrationContract maps every registrable component type to the routing
// fields its adapter reads, in the order the Register dialog shows them.
//
// The phases follow the adapters: a field the adapter's ParseConfig refuses to do
// without is required at registration; kubernetes's cluster coordinates enter at
// promotion, where the promote handler requires api_server and auth_method; every
// other field is optional, carrying the adapter's default where it has one.
var registrationContract = map[string][]RegistrationField{
	store.ComponentTypeKubernetes: {
		{Name: "api_server", Type: FieldURL, Phase: PhasePromotion, Label: "API server URL"},
		{Name: "auth_method", Type: FieldString, Phase: PhasePromotion, Label: "Auth method"},
		{Name: "ca_data", Type: FieldString, Phase: PhaseOptional, Label: "CA bundle (PEM)",
			Help: "The cluster's CA certificate, inline. Can also be supplied at promotion.", Multiline: true},
		{Name: "namespace", Type: FieldString, Phase: PhaseOptional, Label: "Default namespace",
			Help: "Can also be supplied at promotion."},
	},
	store.ComponentTypeGit: {
		{Name: "url", Type: FieldURL, Phase: PhaseRegistration, Label: "Repository URL",
			Help: "Joe clones this repository under its own data directory once the component is armed. A private repository is armed with a credential reference at promotion."},
		{Name: "branch", Type: FieldString, Phase: PhaseOptional, Default: "HEAD", Label: "Branch"},
		{Name: "provider_component_id", Type: FieldString, Phase: PhaseOptional, Label: "Hosting provider",
			Help:     "The registered GitHub or GitLab component that hosts this repository. Discovery only: it grants no access and changes no permissions.",
			RefersTo: []string{store.ComponentTypeGitHub, store.ComponentTypeGitLab}},
	},
	store.ComponentTypePrometheus: {urlField("Prometheus URL"), orgIDField},
	store.ComponentTypeMimir:      {urlField("Mimir URL"), orgIDField},
	store.ComponentTypeLoki:       {urlField("Loki URL"), orgIDField},
	store.ComponentTypeTempo:      {urlField("Tempo URL"), orgIDField},
	store.ComponentTypeJaeger:     {urlField("Jaeger URL")},
	store.ComponentTypeSplunk: {
		urlField("Splunk URL"),
		{Name: "index", Type: FieldString, Phase: PhaseOptional, Label: "Default index"},
	},
	store.ComponentTypeDynatrace: {urlField("Environment URL")},
	store.ComponentTypeNewRelic: {
		{Name: "account_id", Type: FieldInteger, Phase: PhaseRegistration, Label: "Account ID"},
		{Name: "region", Type: FieldString, Phase: PhaseOptional, Default: "US", Label: "Region",
			Help: "US or EU."},
	},
	store.ComponentTypeAlertmanager: {urlField("Alertmanager URL")},
	store.ComponentTypePagerDuty: {
		{Name: "api_url", Type: FieldURL, Phase: PhaseOptional, Default: "https://api.pagerduty.com", Label: "API URL",
			Help: "Override for the EU service region."},
	},
	store.ComponentTypeGrafana: {urlField("Grafana URL")},
	store.ComponentTypeArgoCd: {
		urlField("Argo CD URL"),
		{Name: "insecure_tls", Type: FieldBoolean, Phase: PhaseOptional, Default: false, Label: "Skip TLS verification"},
	},
	store.ComponentTypeTerraform: {
		{Name: "state_path", Type: FieldString, Phase: PhaseRegistration, Label: "State file path",
			Help: "A .tfstate file, or a directory holding terraform.tfstate, on the host Joe runs on."},
	},
	store.ComponentTypeEnvoy: {
		urlField("Admin API URL"),
		{Name: "timeout_ms", Type: FieldInteger, Phase: PhaseOptional, Default: 10000, Label: "Request timeout (ms)"},
	},
	store.ComponentTypeFalco: {urlField("Falcosidekick UI URL")},
	store.ComponentTypeGitHub: {
		{Name: "base_url", Type: FieldURL, Phase: PhaseOptional, Default: "https://api.github.com", Label: "API base URL",
			Help: "Override for GitHub Enterprise Server."},
	},
	store.ComponentTypeGitLab: {
		{Name: "base_url", Type: FieldURL, Phase: PhaseOptional, Default: "https://gitlab.com", Label: "Instance URL",
			Help: "Override for a self-managed GitLab."},
	},
}

// adapterSecretFields is the pinned set of config keys that adapters still READ
// as inline secrets, outside the credential-provider seam: the inline `api_key`
// and `token` fallbacks adapters honour at Connect, and the `webhook_secret` the
// github and gitlab adapters expose. They reached adapter config structs
// before D-0029 drew the line between routing config and credentials.
//
// They are NOT registration fields — registration refuses them as undeclared —
// and they are deliberately NOT in CredentialBearingFields: that denylist is
// derived by reflection from the credential-provider structs and stays so. This
// set is its hand-kept complement for the keys the adapters read. Retiring the
// inline fallback, and giving webhook_secret a path in through promotion, is
// joe backlog item adapter-inline-secrets.
//
// TestAdapterSecretFields_ExactSet pins it, so a new inline secret in an adapter
// fails the build until someone classifies it.
var adapterSecretFields = []string{
	"api_key",
	"token",
	"webhook_secret",
}

// RegistrationFields returns the routing fields declared for a component type,
// in display order, and whether the type has a declaration at all. The slice is
// a copy; callers may not change the contract through it.
func RegistrationFields(componentType string) ([]RegistrationField, bool) {
	fields, ok := registrationContract[componentType]
	if !ok {
		return nil, false
	}
	out := make([]RegistrationField, len(fields))
	for i, f := range fields {
		f.RefersTo = slices.Clone(f.RefersTo)
		out[i] = f
	}
	return out, true
}

// RegistrationContractTypes returns the component types that carry a
// declaration, sorted. It exists so tests can hold the declared set to the
// registrable set without reaching into the map.
func RegistrationContractTypes() []string {
	out := make([]string, 0, len(registrationContract))
	for t := range registrationContract {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// AdapterSecretFields returns the pinned set of adapter-read inline secrets,
// sorted.
func AdapterSecretFields() []string {
	out := slices.Clone(adapterSecretFields)
	sort.Strings(out)
	return out
}

// IsAdapterSecretField reports whether a config key is one of the adapter-read
// inline secrets.
func IsAdapterSecretField(name string) bool {
	return slices.Contains(adapterSecretFields, name)
}
