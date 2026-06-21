package adapters

import (
	"strings"
	"testing"
)

func TestRedactURI(t *testing.T) {
	const secret = "sup3rs3cret"

	tests := []struct {
		name    string
		input   string
		want    string
		secrets []string // substrings that must NOT appear in the output
	}{
		{
			name:    "credential-bearing URI strips userinfo",
			input:   "mongodb://admin:" + secret + "@db.internal:27017/prod",
			want:    "mongodb://db.internal:27017/prod",
			secrets: []string{secret, "admin:", "admin"},
		},
		{
			name:  "URI with no userinfo is unchanged",
			input: "mongodb://db.internal:27017/prod",
			want:  "mongodb://db.internal:27017/prod",
		},
		{
			name:    "malformed input never echoes credentials",
			input:   "admin:" + secret + "@:::not a uri\x7f",
			want:    redactedURIPlaceholder,
			secrets: []string{secret, "admin"},
		},
		{
			name:    "credential-bearing string without scheme is not echoed",
			input:   "admin:" + secret + "@db.internal:27017/prod",
			want:    redactedURIPlaceholder,
			secrets: []string{secret},
		},
		{
			name:  "empty input",
			input: "",
			want:  redactedURIPlaceholder,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactURI(tt.input)
			if got != tt.want {
				t.Errorf("RedactURI(%q) = %q, want %q", tt.input, got, tt.want)
			}
			for _, s := range tt.secrets {
				if s != "" && strings.Contains(got, s) {
					t.Errorf("RedactURI(%q) = %q leaked secret substring %q", tt.input, got, s)
				}
			}
		})
	}
}
