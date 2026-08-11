package credential

import (
	"reflect"
	"slices"
	"testing"
)

// TestCredentialBearingFields_ExactSet pins the derived credential-bearing set to
// exactly the discriminator plus the wired providers' secret/locator fields, with
// the non-credential "audience" descriptor excluded. It is the break-test for the
// D-0029 seam closure: adding a new authentication field to a provider config
// struct (staticConfig, staticBearerConfig, entraExchangeConfig, discriminator)
// changes CredentialBearingFields() and fails this test, forcing a
// conscious decision — admit it to the set, or exclude it via
// nonCredentialConfigFields. Either way the rejected-field list cannot silently
// drift from the structs that parse it. The entra-exchange locators
// (client_id/client_secret_env_var/federated_token_file/tenant_id) are admitted:
// they enter only at promotion and are cleared on re-promote. The kubeconfig/context
// locators left the set with the kubeconfig-exec provider (agent-identity-doc-04).
func TestCredentialBearingFields_ExactSet(t *testing.T) {
	want := []string{
		"client_id",
		"client_secret_env_var",
		"credential_provider",
		"env_var",
		"federated_token_file",
		// The git adapter's retired inline auth fields. No live provider struct
		// parses them; they stay in the set because a registration can still
		// SUBMIT them. See TestCredentialBearingFields_IncludeRetiredInlineAuthFields.
		"http_token",
		"in_cluster",
		"ssh_key_path",
		"tenant_id",
		"value",
	}
	got := CredentialBearingFields()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CredentialBearingFields() = %v, want %v", got, want)
	}
}

// TestCredentialBearingFields_IncludeRetiredInlineAuthFields is the break-test for
// the retired-inline-fields declaration (D-0150). The git component config used to
// carry an inline http_token and an ssh_key_path that the adapter consumed
// directly, outside the credential-provider seam. Those fields are gone from the
// struct the adapter parses, so the reflection derivation cannot see them — but a
// registration can still submit them, and if the denylist stops naming them a
// literal secret would be accepted at registration and persisted, outside the
// promotion boundary. Deleting retiredInlineAuthFields therefore fails HERE rather
// than silently reopening the hole.
//
// auth_type is deliberately absent: it was a discriminator, not credential
// material, and is now simply an ignored unknown field.
func TestCredentialBearingFields_IncludeRetiredInlineAuthFields(t *testing.T) {
	got := CredentialBearingFields()
	for _, field := range []string{"http_token", "ssh_key_path"} {
		if !slices.Contains(got, field) {
			t.Errorf("CredentialBearingFields() omits retired inline auth field %q; "+
				"a registration could then submit it as an inline secret outside the promotion boundary", field)
		}
	}
	if slices.Contains(got, "auth_type") {
		t.Error("CredentialBearingFields() includes \"auth_type\"; it is a discriminator, not credential material")
	}
}

// TestCredentialBearingFields_ExcludesAudience guards the one deliberate
// exclusion: "audience" is json-tagged on staticConfig and entraExchangeConfig
// but is descriptive, not authentication, so it must NOT appear in the set — a
// component may legitimately carry an audience at registration.
func TestCredentialBearingFields_ExcludesAudience(t *testing.T) {
	for _, f := range CredentialBearingFields() {
		if f == "audience" {
			t.Fatalf("CredentialBearingFields() includes %q; audience is a non-credential descriptor and must be excluded", f)
		}
	}
}
