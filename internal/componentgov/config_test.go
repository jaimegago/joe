package componentgov

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/credential"
	"github.com/jaimegago/joe/internal/store"
)

// ValidateRegistrationConfig is the third shared registration seam, enforcing
// each type's declared registration contract for shape only (D-0168). Two
// splits matter most. SHAPE against EXISTENCE: a component-id reference is
// format-checked, and a reference naming no existing component is accepted
// (D-0150). And DECLARED against EVERYTHING ELSE: a field the type does not
// declare is refused, including the secrets adapters still read inline.
func TestValidateRegistrationConfig(t *testing.T) {
	const gitURL = `"url":"https://example.com/r.git"`
	tests := []struct {
		name          string
		componentType string
		config        string
		// wantErr is a substring the rejection must contain; "" means accepted.
		wantErr string
	}{
		// git's component-id reference (D-0150), kept inside the same table.
		{"git with a well-formed reference", store.ComponentTypeGit,
			`{` + gitURL + `,"provider_component_id":"gh-main"}`, ""},
		{"git with a dangling but well-formed reference is legal", store.ComponentTypeGit,
			`{` + gitURL + `,"provider_component_id":"gh-never-registered"}`, ""},
		{"git with no reference", store.ComponentTypeGit, `{` + gitURL + `}`, ""},
		{"git with an empty reference is an empty selection", store.ComponentTypeGit,
			`{` + gitURL + `,"provider_component_id":""}`, ""},
		{"git with a malformed reference", store.ComponentTypeGit,
			`{` + gitURL + `,"provider_component_id":"Not A Valid ID"}`, "provider_component_id"},
		{"git with an uppercase reference", store.ComponentTypeGit,
			`{` + gitURL + `,"provider_component_id":"GH-Main"}`, "provider_component_id"},
		{"git with a non-string reference", store.ComponentTypeGit,
			`{` + gitURL + `,"provider_component_id":42}`, "provider_component_id"},
		{"git with a local file URL", store.ComponentTypeGit, `{"url":"file:///srv/repos/platform"}`, ""},
		{"git without its url", store.ComponentTypeGit, `{"branch":"main"}`, `"url" is required`},
		{"git with no config at all", store.ComponentTypeGit, ``, `"url" is required`},

		// Required at registration.
		{"prometheus with a url", store.ComponentTypePrometheus, `{"url":"http://prometheus:9090"}`, ""},
		{"prometheus with a url and a tenant", store.ComponentTypePrometheus,
			`{"url":"http://mimir:8080","org_id":"team-a"}`, ""},
		{"prometheus without a url", store.ComponentTypePrometheus, `{}`, `"url" is required`},
		{"prometheus with an absent config", store.ComponentTypePrometheus, ``, `"url" is required`},
		{"prometheus with an empty url is a missing url", store.ComponentTypePrometheus, `{"url":""}`, `"url" is required`},
		{"a url missing its scheme", store.ComponentTypePrometheus, `{"url":"prometheus:9090"}`, "absolute URL"},
		{"a bare host is not a url", store.ComponentTypePrometheus, `{"url":"prometheus"}`, "absolute URL"},
		{"a url that does not parse", store.ComponentTypePrometheus, `{"url":"http://[::1"}`, "absolute URL"},
		{"a url of the wrong JSON type", store.ComponentTypePrometheus, `{"url":9090}`, "must be a JSON string"},
		{"a null url", store.ComponentTypePrometheus, `{"url":null}`, "must be a JSON string"},
		{"terraform with a state path", store.ComponentTypeTerraform, `{"state_path":"/srv/tf/prod"}`, ""},
		{"terraform without a state path", store.ComponentTypeTerraform, `{}`, `"state_path" is required`},

		// The JSON type governs the wire, with no coercion.
		{"newrelic with an integer account", store.ComponentTypeNewRelic, `{"account_id":1234567,"region":"EU"}`, ""},
		{"newrelic with a string account", store.ComponentTypeNewRelic, `{"account_id":"1234567"}`, "must be a JSON integer"},
		{"newrelic with a fractional account", store.ComponentTypeNewRelic, `{"account_id":12.5}`, "must be a JSON integer"},
		{"newrelic with account 0 is a missing account", store.ComponentTypeNewRelic, `{"account_id":0}`, `"account_id" is required`},
		{"argocd with a boolean", store.ComponentTypeArgoCd, `{"url":"https://argocd","insecure_tls":true}`, ""},
		{"argocd with a string boolean", store.ComponentTypeArgoCd, `{"url":"https://argocd","insecure_tls":"true"}`, "must be a JSON boolean"},
		{"envoy with an integer timeout", store.ComponentTypeEnvoy, `{"url":"http://envoy:9901","timeout_ms":5000}`, ""},
		{"envoy with a string timeout", store.ComponentTypeEnvoy, `{"url":"http://envoy:9901","timeout_ms":"5000"}`, "must be a JSON integer"},

		// Undeclared fields, secrets among them.
		{"an undeclared field", store.ComponentTypeGitHub, `{"endpoint":"https://github.example.com"}`, `"endpoint" is not a registration field`},
		{"an inline api_key", store.ComponentTypePrometheus, `{"url":"http://prometheus:9090","api_key":"k"}`, `"api_key" is a secret`},
		{"an inline token", store.ComponentTypeArgoCd, `{"url":"https://argocd","token":"t"}`, `"token" is a secret`},
		{"a webhook secret", store.ComponentTypeGitHub, `{"webhook_secret":"s"}`, `"webhook_secret" is a secret`},
		{"a field another type declares", store.ComponentTypeKubernetes,
			`{"provider_component_id":"gh-main"}`, `"provider_component_id" is not a registration field`},

		// Types with no registration-required field register with nothing.
		{"kubernetes with an absent config", store.ComponentTypeKubernetes, ``, ""},
		{"kubernetes with a null config", store.ComponentTypeKubernetes, `null`, ""},
		{"kubernetes with an empty object", store.ComponentTypeKubernetes, `{}`, ""},
		{"kubernetes with its promotion-phase coordinate early", store.ComponentTypeKubernetes,
			`{"api_server":"https://10.0.0.1:6443"}`, ""},
		{"kubernetes with a malformed api_server", store.ComponentTypeKubernetes, `{"api_server":"10.0.0.1"}`, "absolute URL"},
		{"github with an Enterprise Server base url", store.ComponentTypeGitHub,
			`{"base_url":"https://ghe.example.com/api/v3"}`, ""},
		{"pagerduty with nothing", store.ComponentTypePagerDuty, `{}`, ""},

		// A config must be an object.
		{"a scalar config", store.ComponentTypeKubernetes, `"scalar"`, "must be a JSON object"},
		{"an array config", store.ComponentTypeKubernetes, `[]`, "must be a JSON object"},

		// A type with no declaration is refused, never accepted unchecked.
		{"an undeclared type", "not-a-type", `{}`, "no registration contract"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRegistrationConfig(tt.componentType, json.RawMessage(tt.config))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateRegistrationConfig(%s, %s) rejected: %v", tt.componentType, tt.config, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateRegistrationConfig(%s, %s) accepted; want a rejection containing %q",
					tt.componentType, tt.config, tt.wantErr)
			}
			if !errors.Is(err, ErrInvalidRegistrationConfig) {
				t.Errorf("rejection does not wrap ErrInvalidRegistrationConfig: %v", err)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("rejection = %q; want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestValidateRegistrationConfig_RequiredFieldsAreSufficientAndNecessary walks
// every registrable type's declaration: a config carrying exactly its
// registration-required fields is accepted, and dropping any one of them is
// refused. A type that declares none accepts an absent config (D-0057 as
// narrowed by D-0168).
func TestValidateRegistrationConfig_RequiredFieldsAreSufficientAndNecessary(t *testing.T) {
	for _, componentType := range store.AllowedComponentTypes() {
		t.Run(componentType, func(t *testing.T) {
			fields, ok := credential.RegistrationFields(componentType)
			if !ok {
				t.Fatalf("no registration contract for registrable type %q", componentType)
			}
			minimal := map[string]any{}
			for _, f := range fields {
				if f.Phase == credential.PhaseRegistration {
					minimal[f.Name] = sampleValue(t, f)
				}
			}
			raw, _ := json.Marshal(minimal)
			if err := ValidateRegistrationConfig(componentType, raw); err != nil {
				t.Fatalf("minimal config %s rejected: %v", raw, err)
			}
			if len(minimal) == 0 {
				if err := ValidateRegistrationConfig(componentType, nil); err != nil {
					t.Fatalf("type with no registration-required field rejected an absent config: %v", err)
				}
			}
			for name := range minimal {
				without := map[string]any{}
				for k, v := range minimal {
					if k != name {
						without[k] = v
					}
				}
				raw, _ := json.Marshal(without)
				if err := ValidateRegistrationConfig(componentType, raw); err == nil {
					t.Errorf("config %s without required %q was accepted", raw, name)
				}
			}
		})
	}
}

// sampleValue is a well-formed, non-zero value of a declared field's type.
func sampleValue(t *testing.T, f credential.RegistrationField) any {
	t.Helper()
	switch f.Type {
	case credential.FieldURL:
		return "https://backend.example.com"
	case credential.FieldString:
		return "sample"
	case credential.FieldInteger:
		return 1
	case credential.FieldBoolean:
		return true
	}
	t.Fatalf("field %q has an unknown type %q", f.Name, f.Type)
	return nil
}

// TestNormalizeRegistrationConfig pins what a config-less registration
// persists: an absent config and a JSON null both become the empty object
// (D-0057); anything else passes through untouched.
func TestNormalizeRegistrationConfig(t *testing.T) {
	for in, want := range map[string]string{
		``:                     `{}`,
		`null`:                 `{}`,
		` null `:               `{}`,
		`{}`:                   `{}`,
		`{"url":"http://x:1"}`: `{"url":"http://x:1"}`,
	} {
		if got := string(NormalizeRegistrationConfig(json.RawMessage(in))); got != want {
			t.Errorf("NormalizeRegistrationConfig(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestIsAbsoluteURL(t *testing.T) {
	for s, want := range map[string]bool{
		"https://prometheus:9090":        true,
		"http://10.0.0.1:9090/prefix":    true,
		"file:///srv/repos/platform":     true,
		"https://ghe.example.com/api/v3": true,
		"prometheus:9090":                false,
		"localhost:9090":                 false,
		"prometheus":                     false,
		"//prometheus:9090":              false,
		"http://":                        false,
		" https://prometheus:9090":       false,
		"http://[::1":                    false,
		"mailto:ops@example.com":         false,
		"":                               false,
	} {
		if got := IsAbsoluteURL(s); got != want {
			t.Errorf("IsAbsoluteURL(%q) = %v; want %v", s, got, want)
		}
	}
}
