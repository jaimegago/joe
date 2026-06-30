package credential

import (
	"reflect"
	"testing"
)

// TestCredentialBearingFields_ExactSet pins the derived credential-bearing set to
// exactly the discriminator plus the wired providers' secret/locator fields, with
// the non-credential "audience" descriptor excluded. It is the break-test for the
// D-0029 seam closure: adding a new authentication field to a provider config
// struct (staticConfig, kubeconfigExecConfig, staticBearerConfig, entraExchangeConfig,
// discriminator) changes CredentialBearingFields() and fails this test, forcing a
// conscious decision — admit it to the set, or exclude it via
// nonCredentialConfigFields. Either way the rejected-field list cannot silently
// drift from the structs that parse it. The entra-exchange locators
// (client_id/client_secret_env_var/federated_token_file/tenant_id) are admitted:
// they enter only at promotion and are cleared on re-promote.
func TestCredentialBearingFields_ExactSet(t *testing.T) {
	want := []string{
		"client_id",
		"client_secret_env_var",
		"context",
		"credential_provider",
		"env_var",
		"federated_token_file",
		"in_cluster",
		"kubeconfig",
		"tenant_id",
		"value",
	}
	got := CredentialBearingFields()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CredentialBearingFields() = %v, want %v", got, want)
	}
}

// TestCredentialBearingFields_ExcludesAudience guards the one deliberate
// exclusion: "audience" is json-tagged on staticConfig and kubeconfigExecConfig
// but is descriptive, not authentication, so it must NOT appear in the set — a
// component may legitimately carry an audience at registration.
func TestCredentialBearingFields_ExcludesAudience(t *testing.T) {
	for _, f := range CredentialBearingFields() {
		if f == "audience" {
			t.Fatalf("CredentialBearingFields() includes %q; audience is a non-credential descriptor and must be excluded", f)
		}
	}
}
