package componentgov

import (
	"reflect"
	"sort"
	"testing"

	"github.com/jaimegago/joe/internal/credential"
)

// TestCredentialBearingFields_MatchCredentialPackage is the divergence guard for
// the D-0029 seam closure: the set componentgov enforces at registration MUST be
// exactly the set the credential package derives from the provider config structs.
// Before the closure these were two hand-maintained literals that could drift; now
// componentgov consumes credential.CredentialBearingFields() directly, so this test
// fails the moment that wiring is replaced by a local literal again — the field
// list can no longer silently fall out of lockstep with the providers that parse it.
func TestCredentialBearingFields_MatchCredentialPackage(t *testing.T) {
	got := append([]string(nil), credentialBearingFields...)
	want := credential.CredentialBearingFields()
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("componentgov credentialBearingFields = %v, want credential.CredentialBearingFields() = %v; "+
			"the registration denylist must stay single-sourced from the credential package", got, want)
	}
}

// TestRejectCredentialFields_RejectsEveryCredentialField proves each
// credential-bearing field the credential package declares is actually rejected
// by RejectCredentialFields — the set is not merely equal, it is enforced.
func TestRejectCredentialFields_RejectsEveryCredentialField(t *testing.T) {
	for _, f := range credential.CredentialBearingFields() {
		t.Run(f, func(t *testing.T) {
			cfg := []byte(`{"` + f + `":"x"}`)
			if err := RejectCredentialFields(cfg); err == nil {
				t.Fatalf("RejectCredentialFields accepted config carrying credential field %q; want rejection", f)
			}
		})
	}
}

// TestRejectCredentialFields_AcceptsCredentialLessRouting proves a config of only
// non-credential routing fields (endpoint, audience) is accepted.
func TestRejectCredentialFields_AcceptsCredentialLessRouting(t *testing.T) {
	cfg := []byte(`{"endpoint":"https://prom.internal","audience":"prom"}`)
	if err := RejectCredentialFields(cfg); err != nil {
		t.Fatalf("RejectCredentialFields rejected a credential-less routing config: %v", err)
	}
}
