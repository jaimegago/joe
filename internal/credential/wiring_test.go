package credential

import (
	"sort"
	"testing"

	"github.com/jaimegago/joe/internal/store"
)

// After A003-W1 the credential-provider seam is wired for exactly these three
// component types. This test is the structural guard: if an adapter is wired but
// missing from the registry (or present but unwired), this assertion fails.
func TestWiredTypes_ExactSetAfterW1(t *testing.T) {
	got := WiredTypes()
	sort.Strings(got)

	want := []string{
		store.ComponentTypeGitHub,
		store.ComponentTypeGitLab,
		store.ComponentTypeKubernetes,
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
		// prometheus is a valid component type but is NOT wired to the seam.
		{"prometheus unwired", store.ComponentTypePrometheus, "", false},
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
