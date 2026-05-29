package rbac_test

import (
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/rbac"
)

// TestServicePrincipal_MintsSvcPrefix proves the single point that turns a
// service-account name into a principal always applies the reserved svc:
// prefix (Identity Phase D, design §2.4).
func TestServicePrincipal_MintsSvcPrefix(t *testing.T) {
	p, err := rbac.ServicePrincipal("ci")
	if err != nil {
		t.Fatalf("ServicePrincipal(ci): %v", err)
	}
	if p != "svc:ci" {
		t.Errorf("ServicePrincipal(ci) = %q, want svc:ci", p)
	}
	if !strings.HasPrefix(string(p), rbac.PrefixSvc) {
		t.Errorf("minted principal %q lacks the svc: prefix", p)
	}
}

// TestServicePrincipal_TrimsAndRejects covers whitespace trimming and the
// rejections that prevent a config typo from minting a malformed or
// kind-spoofing principal.
func TestServicePrincipal_TrimsAndRejects(t *testing.T) {
	if p, err := rbac.ServicePrincipal("  ci  "); err != nil || p != "svc:ci" {
		t.Errorf("ServicePrincipal with surrounding space = (%q, %v), want (svc:ci, nil)", p, err)
	}

	for _, bad := range []string{"", "   ", "svc:ci", "user:ci", "group:ci"} {
		if _, err := rbac.ServicePrincipal(bad); err == nil {
			t.Errorf("ServicePrincipal(%q) = nil error, want rejection", bad)
		}
	}
}
