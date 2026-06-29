package credential

import "github.com/jaimegago/joe/internal/store"

// references.go declares the env-var NAMING convention for static credential
// references and the provider-described "available references" capability types.
//
// THE CONVENTION (one place, here): a static credential reference is an
// environment variable named
//
//	JOE_<SEGMENT>_<LABEL>
//
// where <SEGMENT> is a per-component-type, env-safe prefix segment declared in
// envPrefixSegments below and <LABEL> is the operator's free choice of which
// reference within that type (e.g. PROD, STAGING). Underscores ONLY — POSIX env
// var names cannot contain hyphens, so the irregular type literals (nginx-ingress,
// oci_registry) cannot be used as segments mechanically; each wired static type
// declares its segment explicitly. EnvPrefix("github") -> "JOE_GITHUB_";
// ComposeEnvVarName("github", "PROD") -> "JOE_GITHUB_PROD".
//
// This is the artifact the promotion-candidates endpoint and reference picker
// consume: it lets a future DB/external-manager provider answer the SAME
// "available references" question against its own backing store without the
// endpoint or form learning any env-var specifics — those stay behind the
// provider seam (the static provider enumerates os.Environ in static.go; the
// endpoint never does).

// envVarPrefix is the fixed namespace every Joe static credential env var lives
// under, so enumeration is scoped to JOE_<SEGMENT>_ and never the environment at
// large.
const envVarPrefix = "JOE_"

// envPrefixSegments maps each KindStatic-wired component type to its env-safe
// prefix SEGMENT. Because the type literals are irregular (nginx-ingress,
// oci_registry, azuremonitor) the segment cannot be derived mechanically from the
// type string — it is an explicit per-type declaration. Defined for exactly the
// KindStatic wired types (kubernetes is wired but static-bearer, whose env_var
// source is a free-form operator-chosen name, so it has no JOE_<SEGMENT>_ env
// segment). A guard test (references_test.go) asserts coverage against the
// KindStatic subset of wiredTypes and that every segment is a valid POSIX env var
// name fragment, so a new static wired type cannot be added without a segment.
var envPrefixSegments = map[string]string{
	store.ComponentTypeGitHub:       "GITHUB",
	store.ComponentTypeGitLab:       "GITLAB",
	store.ComponentTypePrometheus:   "PROMETHEUS",
	store.ComponentTypeMimir:        "MIMIR",
	store.ComponentTypeLoki:         "LOKI",
	store.ComponentTypeTempo:        "TEMPO",
	store.ComponentTypeJaeger:       "JAEGER",
	store.ComponentTypeSplunk:       "SPLUNK",
	store.ComponentTypeDynatrace:    "DYNATRACE",
	store.ComponentTypeNewRelic:     "NEWRELIC",
	store.ComponentTypeAlertmanager: "ALERTMANAGER",
	store.ComponentTypePagerDuty:    "PAGERDUTY",
	store.ComponentTypeGrafana:      "GRAFANA",
	store.ComponentTypeFalco:        "FALCO",
	store.ComponentTypeArgoCd:       "ARGOCD",
}

// EnvPrefixSegment returns the declared env-safe prefix segment for a component
// type and whether one exists. Only the KindStatic wired types have a segment.
func EnvPrefixSegment(componentType string) (string, bool) {
	seg, ok := envPrefixSegments[componentType]
	return seg, ok
}

// EnvPrefix returns the full env var name prefix a component type's static
// references share — "JOE_<SEGMENT>_" — and whether the type has a declared
// segment. This is the scope the static provider enumerates within: promoting a
// github component can only ever surface JOE_GITHUB_* names, never another type's
// prefix and never an unprefixed variable.
func EnvPrefix(componentType string) (string, bool) {
	seg, ok := envPrefixSegments[componentType]
	if !ok {
		return "", false
	}
	return envVarPrefix + seg + "_", true
}

// ComposeEnvVarName composes the full env var name for a (componentType, label)
// pair following the JOE_<SEGMENT>_<LABEL> convention, and reports whether the
// type has a declared segment. The label is used verbatim (callers supply an
// env-safe label).
func ComposeEnvVarName(componentType, label string) (string, bool) {
	prefix, ok := EnvPrefix(componentType)
	if !ok {
		return "", false
	}
	return prefix + label, true
}

// Candidate is one provider-described credential reference an admin may choose
// for a component at promotion: a human LABEL and the full composed reference
// NAME. For the static provider the name is the env var name (JOE_<SEGMENT>_<LABEL>);
// a future DB/manager provider fills these with its own backing identifiers. It
// NEVER carries a credential value — names and labels only.
type Candidate struct {
	Label      string `json:"label"`
	EnvVarName string `json:"env_var_name"`
}

// References is the normalized answer to "which references can the admin choose
// for this component right now?" — the provider-described capability result.
// Applicable is false for providers whose reference is not an enumerable set (the
// kubeconfig-exec provider's reference is a file path, not a candidate list);
// such a provider returns Applicable=false with no candidates rather than forcing
// env semantics onto itself. Prefix is the env-name scope enumerated (static
// only; empty when not applicable). Candidates is always non-nil so it serializes
// as [] not null.
type References struct {
	Applicable bool        `json:"applicable"`
	Prefix     string      `json:"prefix,omitempty"`
	Candidates []Candidate `json:"candidates"`
}
