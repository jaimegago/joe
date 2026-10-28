package autorun

import (
	"context"
	"fmt"
	"sort"

	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
)

// ZoneAssignments is the one read a scope resolution needs from RBAC: which
// component sits in which zone. rbac.Repository satisfies it; nil means RBAC
// is not configured and a selection naming zones cannot be resolved.
type ZoneAssignments interface {
	ListAssignments(ctx context.Context) ([]rbac.ComponentZoneAssignment, error)
}

// ResolveScope turns a selection into the fixed component set a run carries:
// the components it names, plus every component assigned to a zone it names,
// de-duplicated and sorted. It reads the component registry (the same
// ungoverned registry read list_components makes — it names what exists, it
// does not read through to anything) and the zone assignments, and nothing
// else. Whether the run may READ a resolved component is not decided here: it
// is decided per call by the accessor under svc:agent:core, so a scope can
// name a component the run is then denied only in the sense that the refresher
// would be denied it too.
//
// A selection naming a component the registry does not hold is refused
// (ErrUnknownComponent): a scope is a selection of what exists, and a run
// recorded over a phantom would report it as not covered forever.
func ResolveScope(ctx context.Context, components store.ComponentRepository, zones ZoneAssignments, sel Selection) ([]string, error) {
	sel = sel.Normalize()
	if sel.Empty() {
		return nil, ErrEmptyScope
	}
	if components == nil {
		return nil, fmt.Errorf("autorun: component repository not configured")
	}
	all, err := components.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("autorun: list components: %w", err)
	}
	known := make(map[string]struct{}, len(all))
	for _, c := range all {
		known[c.ID] = struct{}{}
	}

	set := make(map[string]struct{})
	for _, id := range sel.Components {
		if _, ok := known[id]; !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnknownComponent, id)
		}
		set[id] = struct{}{}
	}
	if len(sel.Zones) > 0 {
		if zones == nil {
			return nil, ErrZonesUnavailable
		}
		assignments, err := zones.ListAssignments(ctx)
		if err != nil {
			return nil, fmt.Errorf("autorun: list zone assignments: %w", err)
		}
		wanted := make(map[string]struct{}, len(sel.Zones))
		for _, z := range sel.Zones {
			wanted[z] = struct{}{}
		}
		for _, a := range assignments {
			if _, ok := wanted[a.ZoneID]; !ok {
				continue
			}
			if _, ok := known[a.ComponentID]; !ok {
				continue
			}
			set[a.ComponentID] = struct{}{}
		}
	}
	if len(set) == 0 {
		return nil, ErrEmptyScope
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}
