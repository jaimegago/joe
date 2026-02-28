package rbac_test

import (
	"net/http"
	"testing"

	"github.com/jaimegago/joe/internal/rbac"
)

func TestAPIKeyProvider_MatchingKey(t *testing.T) {
	p := rbac.NewAPIKeyProvider("secret-token", "ops-team")

	r, _ := http.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer secret-token")

	if got := p.Identity(r); got != "ops-team" {
		t.Errorf("expected principal ops-team, got %q", got)
	}
}

func TestAPIKeyProvider_WrongKey(t *testing.T) {
	p := rbac.NewAPIKeyProvider("secret-token", "ops-team")

	r, _ := http.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer wrong-token")

	if got := p.Identity(r); got != rbac.Unknown {
		t.Errorf("expected Unknown for wrong key, got %q", got)
	}
}

func TestAPIKeyProvider_MissingHeader(t *testing.T) {
	p := rbac.NewAPIKeyProvider("secret-token", "ops-team")

	r, _ := http.NewRequest("GET", "/", nil)

	if got := p.Identity(r); got != rbac.Unknown {
		t.Errorf("expected Unknown for missing header, got %q", got)
	}
}

func TestAPIKeyProvider_AuthDisabled(t *testing.T) {
	// Empty apiKey = auth disabled, all callers resolve to default principal.
	p := rbac.NewAPIKeyProvider("", "default-operator")

	r, _ := http.NewRequest("GET", "/", nil)

	if got := p.Identity(r); got != "default-operator" {
		t.Errorf("expected default-operator, got %q", got)
	}
}

func TestAPIKeyProvider_DefaultPrincipal(t *testing.T) {
	// Empty principal should default to "default-operator".
	p := rbac.NewAPIKeyProvider("key", "")

	r, _ := http.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer key")

	if got := p.Identity(r); got != "default-operator" {
		t.Errorf("expected default-operator, got %q", got)
	}
}
