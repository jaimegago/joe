package credential

import "testing"

// TestPromotionRequirements_EveryWiredKindHasEntry is the coverage guard: every
// type in the wiring registry resolves to a Kind that has a requirements entry.
// It fails if a future wired Kind is added with no entry — so the describe
// endpoint can never face a wired component it cannot describe.
func TestPromotionRequirements_EveryWiredKindHasEntry(t *testing.T) {
	for _, typ := range WiredTypes() {
		kind, ok := WiredProvider(typ)
		if !ok {
			t.Fatalf("WiredTypes() returned %q but WiredProvider reports it unwired", typ)
		}
		if _, ok := PromotionRequirements(kind); !ok {
			t.Errorf("wired type %q (kind %q) has no promotion requirements entry", typ, kind)
		}
	}
}

// TestPromotionRequirements_FieldsSubsetOfReflected pins every name the table
// uses — both the per-field locator names and the cross-field constraint field
// names — to the provider struct's reflected json fields. The table cannot name a
// field the provider config struct does not actually have.
func TestPromotionRequirements_FieldsSubsetOfReflected(t *testing.T) {
	for kind, reqs := range promotionRequirements {
		reflected, ok := KindLocatorFields(kind)
		if !ok {
			t.Fatalf("kind %q has no reflected config struct", kind)
		}
		set := make(map[string]bool, len(reflected))
		for _, f := range reflected {
			set[f] = true
		}
		for _, f := range reqs.Fields {
			if !set[f.Name] {
				t.Errorf("kind %q requirements name locator field %q not in reflected struct fields %v",
					kind, f.Name, reflected)
			}
		}
		for _, c := range reqs.Constraints {
			for _, cf := range c.Fields {
				if !set[cf] {
					t.Errorf("kind %q constraint %q references field %q not in reflected struct fields %v",
						kind, c.Rule, cf, reflected)
				}
			}
		}
	}
}
