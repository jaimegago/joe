package credential

import (
	"reflect"
	"slices"
	"sort"
	"testing"

	"github.com/jaimegago/joe/internal/store"
)

// TestRegistrationContract_CoversEveryRegistrableType holds the declared set to
// the registrable set: every type a registration can name has a contract, and
// nothing unregistrable carries one.
func TestRegistrationContract_CoversEveryRegistrableType(t *testing.T) {
	want := store.AllowedComponentTypes()
	sort.Strings(want)
	if got := RegistrationContractTypes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("RegistrationContractTypes() = %v; want the registrable set %v", got, want)
	}
}

// TestAdapterSecretFields_ExactSet pins the adapter-read inline secrets exactly,
// the way TestCredentialBearingFields_ExactSet pins the derived denylist. A new
// inline secret in an adapter fails the two-way test until it is classified;
// adding it here is that classification, made on purpose.
func TestAdapterSecretFields_ExactSet(t *testing.T) {
	want := []string{"api_key", "token", "webhook_secret"}
	if got := AdapterSecretFields(); !reflect.DeepEqual(got, want) {
		t.Fatalf("AdapterSecretFields() = %v; want %v", got, want)
	}
}

// TestAdapterSecretFields_AreNotCredentialBearing keeps the secret set and the
// denylist apart: the denylist stays derived by reflection from the
// credential-provider structs, and the secret set is its hand-kept complement,
// never a second copy of it.
func TestAdapterSecretFields_AreNotCredentialBearing(t *testing.T) {
	denylist := CredentialBearingFields()
	for _, s := range AdapterSecretFields() {
		if slices.Contains(denylist, s) {
			t.Errorf("adapter secret %q is also in the derived credential denylist", s)
		}
	}
}

// TestRegistrationContract_DeclaresNoSecret: a declared field is a routing
// field, so it can be neither credential-bearing nor an adapter secret. This is
// also why the descriptor served to the UI can never carry either.
func TestRegistrationContract_DeclaresNoSecret(t *testing.T) {
	denylist := CredentialBearingFields()
	for componentType, fields := range registrationContract {
		for _, f := range fields {
			if slices.Contains(denylist, f.Name) {
				t.Errorf("%s declares credential-bearing field %q", componentType, f.Name)
			}
			if IsAdapterSecretField(f.Name) {
				t.Errorf("%s declares adapter secret %q", componentType, f.Name)
			}
		}
	}
}

// TestRegistrationContract_WellFormed checks each declaration is internally
// consistent: a known type and phase, a label, no duplicate names, a default
// only on an optional field and of the declared type, a component reference
// only on a string naming registrable types, and multi-line only on a string.
func TestRegistrationContract_WellFormed(t *testing.T) {
	for componentType, fields := range registrationContract {
		seen := map[string]bool{}
		for _, f := range fields {
			where := componentType + "." + f.Name
			if f.Name == "" || f.Label == "" {
				t.Errorf("%s: name and label are both required", where)
			}
			if seen[f.Name] {
				t.Errorf("%s: declared twice", where)
			}
			seen[f.Name] = true
			switch f.Type {
			case FieldString, FieldURL, FieldInteger, FieldBoolean:
			default:
				t.Errorf("%s: unknown type %q", where, f.Type)
			}
			switch f.Phase {
			case PhaseRegistration, PhasePromotion, PhaseOptional:
			default:
				t.Errorf("%s: unknown phase %q", where, f.Phase)
			}
			if f.Default != nil {
				if f.Phase != PhaseOptional {
					t.Errorf("%s: a default is declared on a %s-phase field; only an optional field has one", where, f.Phase)
				}
				ok := false
				switch f.Type {
				case FieldString, FieldURL:
					_, ok = f.Default.(string)
				case FieldInteger:
					_, ok = f.Default.(int)
				case FieldBoolean:
					_, ok = f.Default.(bool)
				}
				if !ok {
					t.Errorf("%s: default %v (%T) is not a %s", where, f.Default, f.Default, f.Type)
				}
			}
			if len(f.RefersTo) > 0 {
				if f.Type != FieldString {
					t.Errorf("%s: a component reference must be a string field", where)
				}
				for _, ref := range f.RefersTo {
					if !store.IsValidComponentType(ref) {
						t.Errorf("%s: refers to %q, which is not registrable", where, ref)
					}
				}
			}
			if f.Multiline && f.Type != FieldString {
				t.Errorf("%s: only a string field can be multi-line", where)
			}
		}
	}
}

// TestRegistrationContract_Phases pins which fields are required at which phase,
// exactly as the registration-routing-locator order settled them (D-0168). Moving
// a field between phases changes what an operator must supply, so it is a
// deliberate edit here and not a side effect.
func TestRegistrationContract_Phases(t *testing.T) {
	got := map[Phase][]string{}
	for componentType, fields := range registrationContract {
		for _, f := range fields {
			if f.Phase != PhaseOptional {
				got[f.Phase] = append(got[f.Phase], componentType+"."+f.Name)
			}
		}
	}
	for _, v := range got {
		sort.Strings(v)
	}
	want := map[Phase][]string{
		PhaseRegistration: {
			"alertmanager.url", "argocd.url", "dynatrace.url", "envoy.url", "falco.url",
			"git.url", "grafana.url", "jaeger.url", "loki.url", "mimir.url",
			"newrelic.account_id", "prometheus.url", "splunk.url", "tempo.url",
			"terraform.state_path",
		},
		PhasePromotion: {"kubernetes.api_server", "kubernetes.auth_method"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("required fields by phase = %v; want %v", got, want)
	}
}

// TestRegistrationFields_ReturnsACopy: a caller cannot change the contract
// through the slice it was handed.
func TestRegistrationFields_ReturnsACopy(t *testing.T) {
	fields, ok := RegistrationFields(store.ComponentTypeGit)
	if !ok {
		t.Fatal("git has no registration contract")
	}
	for i := range fields {
		fields[i].Name = "mutated"
		if len(fields[i].RefersTo) > 0 {
			fields[i].RefersTo[0] = "mutated"
		}
	}
	again, _ := RegistrationFields(store.ComponentTypeGit)
	for _, f := range again {
		if f.Name == "mutated" || slices.Contains(f.RefersTo, "mutated") {
			t.Fatalf("mutating the returned slice changed the contract: %+v", again)
		}
	}
}
