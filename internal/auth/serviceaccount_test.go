package auth

import (
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/rbac"
)

// TestServiceAccountResolver_ResolvesToSvcPrincipal proves the core mapping: a
// configured key resolves to its svc:<name> principal, and the principal
// carries the reserved svc: prefix (the Phase D static invariant).
func TestServiceAccountResolver_ResolvesToSvcPrincipal(t *testing.T) {
	r := mustResolver(t,
		config.ServiceAccount{Name: "mcp", Key: "key-mcp"},
		config.ServiceAccount{Name: "ci", Key: "key-ci"},
	)

	cases := []struct {
		key  string
		want rbac.Principal
	}{
		{"key-mcp", "svc:mcp"},
		{"key-ci", "svc:ci"},
	}
	for _, tc := range cases {
		got, ok := r.Resolve(tc.key)
		if !ok {
			t.Errorf("Resolve(%q) not found", tc.key)
			continue
		}
		if got != tc.want {
			t.Errorf("Resolve(%q) = %q, want %q", tc.key, got, tc.want)
		}
		if !strings.HasPrefix(string(got), rbac.PrefixSvc) {
			t.Errorf("resolved principal %q does not carry the svc: prefix", got)
		}
	}
}

// TestServiceAccountResolver_UnknownKey proves an unmatched key is reported as
// not-found — EdgeAuth treats that exactly as an unauthenticated request.
func TestServiceAccountResolver_UnknownKey(t *testing.T) {
	r := mustResolver(t, config.ServiceAccount{Name: "ci", Key: "known"})
	if p, ok := r.Resolve("unknown"); ok {
		t.Errorf("Resolve(unknown) = (%q, true), want (\"\", false)", p)
	}
	if p, ok := r.Resolve(""); ok {
		t.Errorf("Resolve(empty) = (%q, true), want (\"\", false)", p)
	}
}

// TestServiceAccountResolver_Configured covers the gate used to decide whether
// the bearer mechanism (and, with OIDC absent, enforcement) is active.
func TestServiceAccountResolver_Configured(t *testing.T) {
	if mustResolver(t).Configured() {
		t.Error("empty resolver reports Configured()=true")
	}
	if !mustResolver(t, config.ServiceAccount{Name: "ci", Key: "k"}).Configured() {
		t.Error("non-empty resolver reports Configured()=false")
	}
	var nilResolver *ServiceAccountResolver
	if nilResolver.Configured() {
		t.Error("nil resolver reports Configured()=true")
	}
	if _, ok := nilResolver.Resolve("anything"); ok {
		t.Error("nil resolver resolved a key")
	}
}

// TestServiceAccountResolver_RejectsInvalidConfig proves a malformed
// configuration fails loudly (a fatal startup error) rather than silently
// dropping an identity: empty/duplicate keys, duplicate names, empty names, and
// names that collide with a reserved prefix are all rejected.
func TestServiceAccountResolver_RejectsInvalidConfig(t *testing.T) {
	cases := []struct {
		name     string
		accounts []config.ServiceAccount
	}{
		{"empty key", []config.ServiceAccount{{Name: "ci", Key: ""}}},
		{"empty name", []config.ServiceAccount{{Name: "", Key: "k"}}},
		{"duplicate name", []config.ServiceAccount{{Name: "ci", Key: "k1"}, {Name: "ci", Key: "k2"}}},
		{"duplicate key", []config.ServiceAccount{{Name: "ci", Key: "shared"}, {Name: "mcp", Key: "shared"}}},
		{"name carries svc: prefix", []config.ServiceAccount{{Name: "svc:ci", Key: "k"}}},
		{"name carries user: prefix", []config.ServiceAccount{{Name: "user:ci", Key: "k"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewServiceAccountResolver(tc.accounts); err == nil {
				t.Errorf("NewServiceAccountResolver(%v) = nil error, want rejection", tc.accounts)
			}
		})
	}
}
