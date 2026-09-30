package coreagent

import (
	"context"
	"log/slog"
	"slices"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/store"
)

// anchorExemptTypes names every registered component type that writes no anchor
// node, and why. It is the only way a registered type may go without one: a
// type that is neither here nor in anchorFixtureAdapters fails
// TestEveryRegisteredComponentTypeWritesOneAnchor. Each entry is a type with no
// case in refreshComponent's switch — nothing refreshes it, so nothing can
// write its anchor.
var anchorExemptTypes = map[string]string{
	store.ComponentTypeFalco:  "no refresher — docs/backlog/falco-refresher.md",
	store.ComponentTypeGitHub: "no refresher — a code-review provider a git component points at",
	store.ComponentTypeGitLab: "no refresher — a code-review provider a git component points at",
}

// anchorFixtureAdapters supplies a fake adapter for every registered component
// type that has a refresher.
var anchorFixtureAdapters = map[string]func() adapters.Adapter{
	store.ComponentTypeKubernetes: func() adapters.Adapter {
		return &fakeK8sAdapter{items: map[string][]unstructured.Unstructured{}}
	},
	store.ComponentTypeGit:          func() adapters.Adapter { return &fakeGitAdapter{} },
	store.ComponentTypePrometheus:   func() adapters.Adapter { return &fakePrometheusAdapter{} },
	store.ComponentTypeMimir:        func() adapters.Adapter { return &fakePrometheusAdapter{} },
	store.ComponentTypeLoki:         func() adapters.Adapter { return &fakeLokiAdapter{} },
	store.ComponentTypeTempo:        func() adapters.Adapter { return &fakeTempoAdapter{} },
	store.ComponentTypeJaeger:       func() adapters.Adapter { return &fakeJaegerAdapter{} },
	store.ComponentTypeSplunk:       func() adapters.Adapter { return &fakeSplunkAdapter{} },
	store.ComponentTypeDynatrace:    func() adapters.Adapter { return &fakeDynatraceAdapter{} },
	store.ComponentTypeNewRelic:     func() adapters.Adapter { return &fakeNewRelicAdapter{} },
	store.ComponentTypeAlertmanager: func() adapters.Adapter { return &fakeAlertmanagerAdapter{} },
	store.ComponentTypePagerDuty:    func() adapters.Adapter { return &fakePagerDutyAdapter{} },
	store.ComponentTypeGrafana:      func() adapters.Adapter { return &fakeGrafanaAdapter{} },
	store.ComponentTypeArgoCd:       func() adapters.Adapter { return &fakeArgoCDAdapter{} },
	store.ComponentTypeTerraform:    func() adapters.Adapter { return &fakeTerraformAdapter{} },
	store.ComponentTypeEnvoy:        func() adapters.Adapter { return &fakeEnvoyAdapter{} },
}

// assertOneAnchor refreshes a fixture component of componentType through
// refreshComponent and asserts that exactly one node typed
// <componentType>_component carries its component ID.
func assertOneAnchor(t *testing.T, componentType string, adapter adapters.Adapter) {
	t.Helper()
	svc, reg := setupAlertingTestServices(t)
	source := &store.Component{ID: "anchor-" + componentType, Type: componentType, Name: componentType}
	reg.Register(source.ID, adapter)

	r := withPermitAllAccessor(&Refresher{services: svc, logger: slog.Default()})
	if err := r.refreshComponent(context.Background(), source); err != nil {
		t.Fatalf("refreshComponent(%s) error: %v", componentType, err)
	}

	nodes, err := svc.Graph.ListNodesByComponent(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("ListNodesByComponent(%s) error: %v", source.ID, err)
	}
	want := componentType + "_component"
	anchors := 0
	for _, node := range nodes {
		if node.Type == want {
			anchors++
		}
	}
	if anchors != 1 {
		t.Errorf("component type %q wrote %d nodes typed %q, want exactly 1", componentType, anchors, want)
	}
}

// TestEveryRegisteredComponentTypeWritesOneAnchor pins graph-contract promise 2:
// every registered component type is represented in the graph by exactly one
// anchor node typed <type>_component. It walks store.AllowedComponentTypes — the
// registry, not the refreshers — so a type added without a refresher, or with a
// refresher that writes no anchor, fails here rather than passing unseen. The
// only way out is a named entry in anchorExemptTypes.
func TestEveryRegisteredComponentTypeWritesOneAnchor(t *testing.T) {
	registered := store.AllowedComponentTypes()

	for _, componentType := range registered {
		t.Run(componentType, func(t *testing.T) {
			newAdapter, hasFixture := anchorFixtureAdapters[componentType]
			_, exempt := anchorExemptTypes[componentType]
			switch {
			case exempt && hasFixture:
				t.Fatalf("component type %q is both exempt and has an anchor fixture; remove one", componentType)
			case exempt:
				return
			case !hasFixture:
				t.Fatalf("registered component type %q has no anchor fixture and no exemption: "+
					"give its refresher an anchor node and add it to anchorFixtureAdapters, "+
					"or name it in anchorExemptTypes with the reason", componentType)
			}
			assertOneAnchor(t, componentType, newAdapter())
		})
	}

	for componentType := range anchorExemptTypes {
		if !slices.Contains(registered, componentType) {
			t.Errorf("anchorExemptTypes names %q, which is not a registered component type", componentType)
		}
	}
}

// TestUnregistrableCloudTypesWriteOneAnchor pins the same invariant for aws and
// azure. Both are UNREGISTRABLE (internal/store/constants.go), so the walk over
// the registry above never reaches them, yet their refresh cases still act on
// rows stored before the trim.
func TestUnregistrableCloudTypesWriteOneAnchor(t *testing.T) {
	t.Run(store.ComponentTypeAWS, func(t *testing.T) {
		assertOneAnchor(t, store.ComponentTypeAWS, &fakeAWSAdapter{})
	})
	t.Run(store.ComponentTypeAzure, func(t *testing.T) {
		assertOneAnchor(t, store.ComponentTypeAzure, &fakeAzureAdapter{})
	})
}
