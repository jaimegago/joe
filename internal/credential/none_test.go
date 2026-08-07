package credential

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jaimegago/joe/internal/store"
)

// The explicit no-credential provider (D-0150). What matters about it is what it
// does NOT yield: an adapter must be unable to mistake a no-credential arm for a
// credential, so every typed accessor reports false.

func TestNoneProvider_ResolvesWithoutCredential(t *testing.T) {
	res, err := NewNoneProvider().Resolve(context.Background(), "repo-pub", json.RawMessage(`{"credential_provider":"none"}`))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !res.Diagnostic.OK || res.Diagnostic.Stage != StageMintSucceeded {
		t.Errorf("diagnostic = %+v; want a successful mint-succeeded", res.Diagnostic)
	}
	if res.Diagnostic.Provider != KindNone {
		t.Errorf("provider = %q, want %q", res.Diagnostic.Provider, KindNone)
	}
	if v, ok := res.StaticValue(); ok {
		t.Errorf("StaticValue() returned (%q, true); a no-credential resolution must yield nothing", v)
	}
	if v, ok := res.BearerToken(); ok {
		t.Errorf("BearerToken() returned (%q, true); a no-credential resolution must yield nothing", v)
	}
}

func TestNoneProvider_ProbeIsNoOpSuccess(t *testing.T) {
	p := NewNoneProvider()
	res, err := p.Resolve(context.Background(), "repo-pub", nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	probed, err := p.Probe(context.Background(), res)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if probed.Diagnostic.Stage != StageConnectivityProbed || !probed.Diagnostic.OK {
		t.Errorf("probed diagnostic = %+v; want connectivity-probed and OK", probed.Diagnostic)
	}
}

// A no-credential reference has nothing to enumerate, so the provider says so
// rather than forcing env-var semantics onto itself.
func TestNoneProvider_ReferencesNotApplicable(t *testing.T) {
	refs, err := NewNoneProvider().AvailableReferences(store.ComponentTypeGit)
	if err != nil {
		t.Fatalf("AvailableReferences: %v", err)
	}
	if refs.Applicable {
		t.Error("a no-credential reference is not an enumerable set")
	}
	if refs.Candidates == nil {
		t.Error("Candidates must be non-nil so it serializes as [] not null")
	}
}

func TestProviderForKind_None(t *testing.T) {
	p, err := ProviderForKind(KindNone)
	if err != nil {
		t.Fatalf("ProviderForKind(none): %v", err)
	}
	if _, ok := p.(*NoneProvider); !ok {
		t.Errorf("ProviderForKind(none) = %T, want *NoneProvider", p)
	}
}

// Select routes on the config discriminator, so an armed no-credential component
// reaches the none provider through the same seam every other adapter uses.
func TestSelect_RoutesNoneDiscriminator(t *testing.T) {
	p, err := Select(json.RawMessage(`{"credential_provider":"none"}`))
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if _, ok := p.(*NoneProvider); !ok {
		t.Errorf("Select(none) = %T, want *NoneProvider", p)
	}
}

// --- selectable kinds ---

// TestSelectableKinds pins which types may be armed with more than one Kind. git
// is the multi-Kind type: a static HTTPS-token reference, or the explicit
// no-credential arm. Every other wired type reports its single wired Kind, so the
// promotion boundary's existing exact-match behaviour is unchanged for them.
func TestSelectableKinds(t *testing.T) {
	tests := []struct {
		componentType string
		want          []Kind
		wired         bool
	}{
		{store.ComponentTypeGit, []Kind{KindStatic, KindNone}, true},
		{store.ComponentTypeGitHub, []Kind{KindStatic}, true},
		{store.ComponentTypeKubernetes, []Kind{KindStaticBearer}, true},
		{store.ComponentTypeTerraform, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.componentType, func(t *testing.T) {
			got, wired := SelectableKinds(tt.componentType)
			if wired != tt.wired {
				t.Fatalf("SelectableKinds(%q) wired = %t, want %t", tt.componentType, wired, tt.wired)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("SelectableKinds(%q) = %v, want %v", tt.componentType, got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("SelectableKinds(%q) = %v, want %v", tt.componentType, got, tt.want)
				}
			}
		})
	}

	// The type-level default reported by WiredProvider must be the FIRST
	// selectable Kind, since the promotion boundary falls back to it when the
	// request supplies no discriminator.
	for _, ct := range WiredTypes() {
		def, _ := WiredProvider(ct)
		kinds, _ := SelectableKinds(ct)
		if len(kinds) == 0 || kinds[0] != def {
			t.Errorf("%q: first selectable kind %v is not the wired default %q", ct, kinds, def)
		}
	}
}

func TestIsSelectableKind(t *testing.T) {
	if !IsSelectableKind(store.ComponentTypeGit, KindNone) {
		t.Error("git must accept the no-credential kind")
	}
	if !IsSelectableKind(store.ComponentTypeGit, KindStatic) {
		t.Error("git must accept a static reference")
	}
	if IsSelectableKind(store.ComponentTypeGit, KindStaticBearer) {
		t.Error("git must not accept a kind belonging to another type")
	}
	if IsSelectableKind(store.ComponentTypeGitHub, KindNone) {
		t.Error("the no-credential kind is git's, not every static type's")
	}
	if IsSelectableKind("not-a-type", KindStatic) {
		t.Error("an unwired type has no selectable kinds")
	}
}
