package credential_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	alertmanageradapter "github.com/jaimegago/joe/internal/adapters/alerting/alertmanager"
	grafanaadapter "github.com/jaimegago/joe/internal/adapters/alerting/grafana"
	pagerdutyadapter "github.com/jaimegago/joe/internal/adapters/alerting/pagerduty"
	gitadapter "github.com/jaimegago/joe/internal/adapters/git"
	githubadapter "github.com/jaimegago/joe/internal/adapters/github"
	gitlabadapter "github.com/jaimegago/joe/internal/adapters/gitlab"
	argocdadapter "github.com/jaimegago/joe/internal/adapters/gitops/argocd"
	terraformadapter "github.com/jaimegago/joe/internal/adapters/iac/terraform"
	"github.com/jaimegago/joe/internal/adapters/k8s"
	envoyadapter "github.com/jaimegago/joe/internal/adapters/networking/envoy"
	dynatraceadapter "github.com/jaimegago/joe/internal/adapters/observability/dynatrace"
	jaegeradapter "github.com/jaimegago/joe/internal/adapters/observability/jaeger"
	lokiadapter "github.com/jaimegago/joe/internal/adapters/observability/loki"
	newrelicadapter "github.com/jaimegago/joe/internal/adapters/observability/newrelic"
	prometheusadapter "github.com/jaimegago/joe/internal/adapters/observability/prometheus"
	splunkadapter "github.com/jaimegago/joe/internal/adapters/observability/splunk"
	tempoadapter "github.com/jaimegago/joe/internal/adapters/observability/tempo"
	falcoadapter "github.com/jaimegago/joe/internal/adapters/security/falco"
	"github.com/jaimegago/joe/internal/credential"
	"github.com/jaimegago/joe/internal/store"
)

// adapterConfig is what this test knows about one registrable type's adapter:
// the struct its ParseConfig decodes into, the struct tag that decoding reads
// keys by, and a call into ParseConfig fed a stored JSON config the way the
// adapter's Connect feeds it.
type adapterConfig struct {
	structType reflect.Type
	tag        string
	parse      func(raw []byte) (any, error)
}

// viaMap feeds ParseConfig the map the yaml-round-tripping adapters build from
// the stored JSON at Connect.
func viaMap[T any](parse func(map[string]any) (T, error)) func([]byte) (any, error) {
	return func(raw []byte) (any, error) {
		m := map[string]any{}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &m); err != nil {
				return nil, err
			}
		}
		return parse(m)
	}
}

// viaRaw feeds ParseConfig the stored JSON bytes unchanged.
func viaRaw[T any, B ~[]byte](parse func(B) (T, error)) func([]byte) (any, error) {
	return func(raw []byte) (any, error) { return parse(B(raw)) }
}

// adapterConfigs maps every registrable type to its adapter's config. A
// registrable type missing here fails TestRegistrationContract_TwoWayAgainstAdapters,
// so a new type cannot ship without its fields being classified.
var adapterConfigs = map[string]adapterConfig{
	store.ComponentTypeKubernetes:   {reflect.TypeFor[k8s.Config](), "json", viaRaw(k8s.ParseConfig)},
	store.ComponentTypeGit:          {reflect.TypeFor[gitadapter.Config](), "json", viaRaw(gitadapter.ParseConfig)},
	store.ComponentTypePrometheus:   {reflect.TypeFor[prometheusadapter.Config](), "yaml", viaMap(prometheusadapter.ParseConfig)},
	store.ComponentTypeMimir:        {reflect.TypeFor[prometheusadapter.Config](), "yaml", viaMap(prometheusadapter.ParseConfig)},
	store.ComponentTypeLoki:         {reflect.TypeFor[lokiadapter.Config](), "yaml", viaMap(lokiadapter.ParseConfig)},
	store.ComponentTypeTempo:        {reflect.TypeFor[tempoadapter.Config](), "yaml", viaMap(tempoadapter.ParseConfig)},
	store.ComponentTypeJaeger:       {reflect.TypeFor[jaegeradapter.Config](), "yaml", viaMap(jaegeradapter.ParseConfig)},
	store.ComponentTypeSplunk:       {reflect.TypeFor[splunkadapter.Config](), "yaml", viaMap(splunkadapter.ParseConfig)},
	store.ComponentTypeDynatrace:    {reflect.TypeFor[dynatraceadapter.Config](), "yaml", viaMap(dynatraceadapter.ParseConfig)},
	store.ComponentTypeNewRelic:     {reflect.TypeFor[newrelicadapter.Config](), "yaml", viaMap(newrelicadapter.ParseConfig)},
	store.ComponentTypeAlertmanager: {reflect.TypeFor[alertmanageradapter.Config](), "yaml", viaMap(alertmanageradapter.ParseConfig)},
	store.ComponentTypePagerDuty:    {reflect.TypeFor[pagerdutyadapter.Config](), "yaml", viaMap(pagerdutyadapter.ParseConfig)},
	store.ComponentTypeGrafana:      {reflect.TypeFor[grafanaadapter.Config](), "yaml", viaMap(grafanaadapter.ParseConfig)},
	store.ComponentTypeFalco:        {reflect.TypeFor[falcoadapter.Config](), "yaml", viaMap(falcoadapter.ParseConfig)},
	store.ComponentTypeArgoCd:       {reflect.TypeFor[argocdadapter.Config](), "json", viaRaw(argocdadapter.ParseConfig)},
	store.ComponentTypeTerraform:    {reflect.TypeFor[terraformadapter.Config](), "json", viaRaw(terraformadapter.ParseConfig)},
	store.ComponentTypeEnvoy:        {reflect.TypeFor[envoyadapter.Config](), "json", viaRaw(envoyadapter.ParseConfig)},
	store.ComponentTypeGitHub:       {reflect.TypeFor[githubadapter.Config](), "json", viaRaw(githubadapter.ParseConfig)},
	store.ComponentTypeGitLab:       {reflect.TypeFor[gitlabadapter.Config](), "json", viaRaw(gitlabadapter.ParseConfig)},
}

// adapterKeys returns the config keys an adapter's struct reads, each with the
// struct field that receives it.
func adapterKeys(ac adapterConfig) map[string]reflect.StructField {
	keys := map[string]reflect.StructField{}
	for i := 0; i < ac.structType.NumField(); i++ {
		f := ac.structType.Field(i)
		name := strings.Split(f.Tag.Get(ac.tag), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		keys[name] = f
	}
	return keys
}

// TestRegistrationContract_TwoWayAgainstAdapters is the guard decision 1 of the
// registration-routing-locator design asked for, holding the declaration to the
// adapters in both directions (D-0168):
//
//   - every key an adapter's ParseConfig struct reads is declared, in the pinned
//     adapter-secret set, or credential-bearing under the derived denylist — so a
//     new adapter field nobody classified fails the build;
//   - every declared field is read by its adapter — so the contract cannot accept
//     a locator the adapter ignores;
//   - every declared JSON type matches the Go kind the adapter decodes into.
func TestRegistrationContract_TwoWayAgainstAdapters(t *testing.T) {
	denylist := credential.CredentialBearingFields()
	for _, componentType := range store.AllowedComponentTypes() {
		t.Run(componentType, func(t *testing.T) {
			ac, ok := adapterConfigs[componentType]
			if !ok {
				t.Fatalf("registrable type %q has no adapter config mapped in this test; map it so its fields are classified", componentType)
			}
			fields, ok := credential.RegistrationFields(componentType)
			if !ok {
				t.Fatalf("registrable type %q has no registration contract", componentType)
			}
			declared := map[string]credential.RegistrationField{}
			for _, f := range fields {
				declared[f.Name] = f
			}
			keys := adapterKeys(ac)

			for key := range keys {
				_, isDeclared := declared[key]
				if !isDeclared && !credential.IsAdapterSecretField(key) && !slices.Contains(denylist, key) {
					t.Errorf("the %s adapter reads %q, which is neither declared, an adapter secret, nor credential-bearing — classify it", componentType, key)
				}
			}
			for name, f := range declared {
				sf, read := keys[name]
				if !read {
					t.Errorf("%q is declared for %s but its adapter never reads it", name, componentType)
					continue
				}
				if want := kindFor(f.Type); !slices.Contains(want, sf.Type.Kind()) {
					t.Errorf("%s.%s is declared %s but the adapter decodes it into a %s", componentType, name, f.Type, sf.Type.Kind())
				}
			}
		})
	}
	for componentType := range adapterConfigs {
		if !store.IsValidComponentType(componentType) {
			t.Errorf("adapterConfigs maps %q, which is not registrable", componentType)
		}
	}
}

func kindFor(t credential.FieldType) []reflect.Kind {
	switch t {
	case credential.FieldString, credential.FieldURL:
		return []reflect.Kind{reflect.String}
	case credential.FieldInteger:
		return []reflect.Kind{reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64}
	case credential.FieldBoolean:
		return []reflect.Kind{reflect.Bool}
	}
	return nil
}

// TestRegistrationContract_DefaultsAreTheAdapters feeds each adapter's
// ParseConfig a config carrying exactly the type's registration-required fields
// and checks two things: the adapter accepts it — so what registration requires
// is enough for the adapter to parse — and every other declared field comes back
// holding the default the declaration names, or its zero value where the
// declaration names none. A declared default that is not the adapter's fails
// here.
func TestRegistrationContract_DefaultsAreTheAdapters(t *testing.T) {
	for _, componentType := range store.AllowedComponentTypes() {
		t.Run(componentType, func(t *testing.T) {
			ac, ok := adapterConfigs[componentType]
			if !ok {
				t.Fatalf("registrable type %q has no adapter config mapped", componentType)
			}
			fields, _ := credential.RegistrationFields(componentType)
			minimal := map[string]any{}
			for _, f := range fields {
				if f.Phase == credential.PhaseRegistration {
					minimal[f.Name] = sample(f.Type)
				}
			}
			raw, _ := json.Marshal(minimal)
			parsed, err := ac.parse(raw)
			if err != nil {
				t.Fatalf("the %s adapter refused the minimal registration config %s: %v", componentType, raw, err)
			}
			v := reflect.ValueOf(parsed)
			keys := adapterKeys(ac)
			for _, f := range fields {
				if f.Phase == credential.PhaseRegistration {
					continue
				}
				got := v.FieldByIndex(keys[f.Name].Index)
				if f.Default == nil {
					if !got.IsZero() {
						t.Errorf("%s.%s declares no default, but the adapter defaults it to %v", componentType, f.Name, got.Interface())
					}
					continue
				}
				if fmt.Sprint(got.Interface()) != fmt.Sprint(f.Default) {
					t.Errorf("%s.%s declares default %v, but the adapter defaults it to %v", componentType, f.Name, f.Default, got.Interface())
				}
			}
		})
	}
}

func sample(t credential.FieldType) any {
	switch t {
	case credential.FieldURL:
		return "https://backend.example.com"
	case credential.FieldInteger:
		return 1
	case credential.FieldBoolean:
		return true
	default:
		return "sample"
	}
}

// TestAdapterSecretFields_AreReadByTheAdapters holds the pinned secret set to
// the adapters: each member is read by at least one registrable adapter, under
// exactly that key. A secret the adapters stopped reading is dead weight in the
// set and fails here, so the set can only shrink deliberately.
func TestAdapterSecretFields_AreReadByTheAdapters(t *testing.T) {
	readBy := map[string][]string{}
	for componentType, ac := range adapterConfigs {
		for key := range adapterKeys(ac) {
			readBy[key] = append(readBy[key], componentType)
		}
	}
	for _, secret := range credential.AdapterSecretFields() {
		if len(readBy[secret]) == 0 {
			t.Errorf("adapter secret %q is read by no registrable adapter", secret)
			continue
		}
		sort.Strings(readBy[secret])
		t.Logf("%s is read by: %s", secret, strings.Join(readBy[secret], ", "))
	}
}
