package factory

import (
	"errors"
	"testing"

	"github.com/jaimegago/joe/internal/store"
)

// TestNew_CoversEveryRegistrableComponentType is THE coverage guard: every type
// an operator can register must construct. A registrable type the constructor
// cannot build is a component that registers, promotes, and then has no live
// adapter in any context — which is the defect this package was created to end.
//
// It reads store.AllowedComponentTypes rather than a copied list on purpose. A
// new registrable type added without a factory case fails here, at the seam,
// instead of surfacing as a nil adapter at 3am.
func TestNew_CoversEveryRegistrableComponentType(t *testing.T) {
	types := store.AllowedComponentTypes()
	if len(types) == 0 {
		t.Fatal("store.AllowedComponentTypes returned nothing — this guard would pass vacuously")
	}
	for _, componentType := range types {
		adapter, err := New(componentType)
		if err != nil {
			t.Errorf("New(%q) = error %v; every registrable component type must construct", componentType, err)
			continue
		}
		if adapter == nil {
			t.Errorf("New(%q) returned a nil adapter with no error", componentType)
		}
	}
}

// TestNew_BuildsTypesEachFormerPathMissed is the break-test the consolidation
// order names: a valid type present in the const list but previously missing
// from ONE of the two construction paths now constructs.
//
// The membership is spelled out rather than derived, because the regression this
// pins is exactly a divergence between two hand-maintained sets — deriving it
// from either former set would reintroduce the thing being guarded against. The
// two groups are the two shapes of dead window the backlog item measured:
//
//   - formerlyBootOnly — in the boot pass, absent from the runtime map, so a
//     promoted component of this type got a silent nil no-op and had no live
//     adapter until the next restart.
//   - formerlyRuntimeOnly — in the runtime map, absent from the boot pass, so a
//     component of this type lost its adapter on every restart and nothing
//     reconstructed it short of a manual Test Connection.
func TestNew_BuildsTypesEachFormerPathMissed(t *testing.T) {
	formerlyBootOnly := []string{
		store.ComponentTypeSplunk,
		store.ComponentTypeDynatrace,
		store.ComponentTypeNewRelic,
		store.ComponentTypeGitHub,
		store.ComponentTypeGitLab,
	}
	formerlyRuntimeOnly := []string{
		store.ComponentTypePrometheus,
		store.ComponentTypeMimir,
		store.ComponentTypeLoki,
		store.ComponentTypeTempo,
		store.ComponentTypeJaeger,
		store.ComponentTypeAlertmanager,
		store.ComponentTypePagerDuty,
		store.ComponentTypeGrafana,
		store.ComponentTypeArgoCd,
		store.ComponentTypeTerraform,
		store.ComponentTypeEnvoy,
	}

	for name, group := range map[string][]string{
		"formerly boot-only":    formerlyBootOnly,
		"formerly runtime-only": formerlyRuntimeOnly,
	} {
		for _, componentType := range group {
			adapter, err := New(componentType)
			if err != nil {
				t.Errorf("New(%q) [%s] = error %v; the union must cover it", componentType, name, err)
				continue
			}
			if adapter == nil {
				t.Errorf("New(%q) [%s] returned a nil adapter with no error", componentType, name)
			}
		}
	}
}

// TestNew_ArtifactRegistryTypesStayExcluded pins the one deliberate hole in the
// union. These four have never had a construction path — that absence is WHY
// D-0058 made them unregistrable — and wiring them needs a credential path
// first, deferred to docs/backlog/trim-deadonarrival-component-types.md.
//
// The guard runs in the direction that matters: it fails if one is quietly
// wired, which would mean the deferred work landed without the item being closed
// and without the type being made registrable, leaving a constructible type no
// operator can create.
func TestNew_ArtifactRegistryTypesStayExcluded(t *testing.T) {
	for _, componentType := range []string{
		store.ComponentTypeOCIRegistry,
		store.ComponentTypeDockerHub,
		store.ComponentTypeArtifactory,
		store.ComponentTypeECR,
	} {
		adapter, err := New(componentType)
		if !errors.Is(err, ErrNoAdapter) {
			t.Errorf("New(%q) = err %v; want ErrNoAdapter until the deferred wiring lands", componentType, err)
		}
		if adapter != nil {
			t.Errorf("New(%q) returned a non-nil adapter alongside its error", componentType)
		}
	}
}

// TestNew_UnknownTypeIsAnErrorNotANilAdapter pins the shape of the failure. The
// predecessor returned a bare nil, which every caller then had to decide how to
// read — and both read it as "nothing to do here", which is how a coverage gap
// became a silent no-op at promotion and a false success at Test Connection.
func TestNew_UnknownTypeIsAnErrorNotANilAdapter(t *testing.T) {
	adapter, err := New("not-a-component-type")
	if !errors.Is(err, ErrNoAdapter) {
		t.Errorf("New(unknown) = err %v; want ErrNoAdapter", err)
	}
	if adapter != nil {
		t.Errorf("New(unknown) = adapter %T; want nil", adapter)
	}
}
