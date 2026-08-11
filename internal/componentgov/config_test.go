package componentgov

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/store"
)

// ValidateRegistrationConfig is the third shared registration seam (D-0150). What
// it must get right is the split between SHAPE and EXISTENCE: a component-id
// reference is format-checked, and a reference naming no existing component is
// accepted, because making registration order-dependent would be a worse defect
// than a reference that resolves to nothing.
func TestValidateRegistrationConfig(t *testing.T) {
	tests := []struct {
		name          string
		componentType string
		config        string
		wantErr       bool
	}{
		{"git with a well-formed reference", store.ComponentTypeGit,
			`{"url":"https://example.com/r.git","provider_component_id":"gh-main"}`, false},
		{"git with a dangling but well-formed reference is legal", store.ComponentTypeGit,
			`{"url":"https://example.com/r.git","provider_component_id":"gh-never-registered"}`, false},
		{"git with no reference", store.ComponentTypeGit, `{"url":"https://example.com/r.git"}`, false},
		{"git with an empty reference is an empty selection", store.ComponentTypeGit,
			`{"url":"https://example.com/r.git","provider_component_id":""}`, false},
		{"git with a malformed reference", store.ComponentTypeGit,
			`{"provider_component_id":"Not A Valid ID"}`, true},
		{"git with an uppercase reference", store.ComponentTypeGit,
			`{"provider_component_id":"GH-Main"}`, true},
		{"git with a non-string reference", store.ComponentTypeGit,
			`{"provider_component_id":42}`, true},
		{"a type with no declared reference fields", store.ComponentTypeKubernetes,
			`{"provider_component_id":"Not A Valid ID"}`, false},
		{"empty config", store.ComponentTypeGit, ``, false},
		{"non-object config", store.ComponentTypeGit, `"scalar"`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRegistrationConfig(tt.componentType, json.RawMessage(tt.config))
			if tt.wantErr && err == nil {
				t.Fatalf("ValidateRegistrationConfig accepted %s", tt.config)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateRegistrationConfig rejected %s: %v", tt.config, err)
			}
			if tt.wantErr && !strings.Contains(err.Error(), "provider_component_id") {
				t.Errorf("rejection does not name the offending field: %v", err)
			}
		})
	}
}
