package componentgov

import (
	"strings"
	"testing"
)

func TestValidateComponentID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{"valid simple", "prod-cluster", false},
		{"valid single char", "a", false},
		{"valid single digit", "7", false},
		{"valid digits and hyphens", "team-42-loki", false},
		{"valid agent-generated shape", "kubernetes-a1b2c3d4e5f60718", false},
		{"valid 63 chars", strings.Repeat("a", 63), false},
		{"uppercase", "Prod-Cluster", true},
		{"leading hyphen", "-prod", true},
		{"trailing hyphen", "prod-", true},
		{"slash", "prod/cluster", true},
		{"space", "prod cluster", true},
		{"percent", "prod%20cluster", true},
		{"underscore", "prod_cluster", true},
		{"empty", "", true},
		{"64 chars", strings.Repeat("a", 64), true},
		{"only hyphen", "-", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateComponentID(tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateComponentID(%q) error = %v, wantErr %v", tt.id, err, tt.wantErr)
			}
			// Every rejection names the rule so the operator learns what is
			// allowed, not just that the ID was refused.
			if err != nil && !strings.Contains(err.Error(), ComponentIDRule) {
				t.Errorf("ValidateComponentID(%q) error %q does not state the rule", tt.id, err)
			}
		})
	}
}
