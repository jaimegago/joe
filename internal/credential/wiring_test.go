package credential

import (
	"sort"
	"testing"

	"github.com/jaimegago/joe/internal/store"
)

// After A003-W2 the credential-provider seam is wired for exactly this set of
// component types. This test is the structural guard: if an adapter is wired but
// missing from the registry (or present but unwired), this assertion fails.
func TestWiredTypes_ExactSetAfterW2(t *testing.T) {
	got := WiredTypes()
	sort.Strings(got)

	want := []string{
		// A003-W1 spine.
		store.ComponentTypeGitHub,
		store.ComponentTypeGitLab,
		store.ComponentTypeKubernetes,
		// A003-W2 static-token batch.
		store.ComponentTypePrometheus,
		store.ComponentTypeMimir,
		store.ComponentTypeLoki,
		store.ComponentTypeTempo,
		store.ComponentTypeJaeger,
		store.ComponentTypeSplunk,
		store.ComponentTypeDynatrace,
		store.ComponentTypeNewRelic,
		store.ComponentTypeAlertmanager,
		store.ComponentTypePagerDuty,
		store.ComponentTypeGrafana,
		store.ComponentTypeFalco,
		store.ComponentTypeArgoCd,
		store.ComponentTypeArtifactory,
	}
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("WiredTypes() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("WiredTypes() = %v, want %v", got, want)
		}
	}
}

func TestWiredProvider_AnswersForWiredAndUnwiredTypes(t *testing.T) {
	tests := []struct {
		name      string
		component string
		wantKind  Kind
		wantWired bool
	}{
		{"github wired to static", store.ComponentTypeGitHub, KindStatic, true},
		{"gitlab wired to static", store.ComponentTypeGitLab, KindStatic, true},
		{"kubernetes wired to kubeconfig-exec", store.ComponentTypeKubernetes, KindKubeconfigExec, true},
		// A003-W2 static-token backends are now wired to the static provider.
		{"prometheus wired to static", store.ComponentTypePrometheus, KindStatic, true},
		{"mimir wired to static (shares prometheus adapter)", store.ComponentTypeMimir, KindStatic, true},
		{"loki wired to static", store.ComponentTypeLoki, KindStatic, true},
		{"tempo wired to static", store.ComponentTypeTempo, KindStatic, true},
		{"jaeger wired to static", store.ComponentTypeJaeger, KindStatic, true},
		{"splunk wired to static", store.ComponentTypeSplunk, KindStatic, true},
		{"dynatrace wired to static", store.ComponentTypeDynatrace, KindStatic, true},
		{"newrelic wired to static", store.ComponentTypeNewRelic, KindStatic, true},
		{"alertmanager wired to static", store.ComponentTypeAlertmanager, KindStatic, true},
		{"pagerduty wired to static", store.ComponentTypePagerDuty, KindStatic, true},
		{"grafana wired to static", store.ComponentTypeGrafana, KindStatic, true},
		{"falco wired to static", store.ComponentTypeFalco, KindStatic, true},
		{"argocd wired to static", store.ComponentTypeArgoCd, KindStatic, true},
		{"artifactory wired to static", store.ComponentTypeArtifactory, KindStatic, true},
		// Out-of-batch / no-credential types stay UNWIRED: datadog (api_key+app_key
		// pair), git (auth_type-discriminated), oci_registry (basic-auth pair), helm
		// (kubeconfig), terraform (no credential), envoy (no credential).
		{"datadog unwired (credential pair)", store.ComponentTypeDatadog, "", false},
		{"git unwired (discriminated auth)", store.ComponentTypeGit, "", false},
		{"oci_registry unwired (basic-auth pair)", store.ComponentTypeOCIRegistry, "", false},
		{"helm unwired (kubeconfig)", store.ComponentTypeHelm, "", false},
		{"terraform unwired (no credential)", store.ComponentTypeTerraform, "", false},
		{"envoy unwired (no credential)", store.ComponentTypeEnvoy, "", false},
		{"unknown unwired", "does-not-exist", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, wired := WiredProvider(tt.component)
			if wired != tt.wantWired {
				t.Fatalf("WiredProvider(%q) wired = %t, want %t", tt.component, wired, tt.wantWired)
			}
			if kind != tt.wantKind {
				t.Errorf("WiredProvider(%q) kind = %q, want %q", tt.component, kind, tt.wantKind)
			}
			if IsWired(tt.component) != tt.wantWired {
				t.Errorf("IsWired(%q) = %t, want %t", tt.component, IsWired(tt.component), tt.wantWired)
			}
		})
	}
}
