package llmusage

import "testing"

// TestLookupCapabilities_KnownModelsReturnSeededValues asserts each shipped
// model resolves to its seeded window + output values from the built-in
// table (behaviour, not compilation).
func TestLookupCapabilities_KnownModelsReturnSeededValues(t *testing.T) {
	cases := []struct {
		provider, model string
		wantWindow      int
		wantOutput      int
	}{
		{"claude", "claude-sonnet-4-20250514", 200000, 4096},
		{"gemini", "gemini-2.5-flash", 1048576, 4096},
		// Provider is normalised (lowercased) on lookup, mirroring the cost
		// table; an upper-cased provider must still resolve.
		{"Claude", "claude-sonnet-4-20250514", 200000, 4096},
	}
	for _, tc := range cases {
		got := LookupCapabilities(tc.provider, tc.model)
		if got.ContextWindowTokens != tc.wantWindow {
			t.Errorf("LookupCapabilities(%q,%q).ContextWindowTokens = %d, want %d",
				tc.provider, tc.model, got.ContextWindowTokens, tc.wantWindow)
		}
		if got.MaxOutputTokens != tc.wantOutput {
			t.Errorf("LookupCapabilities(%q,%q).MaxOutputTokens = %d, want %d",
				tc.provider, tc.model, got.MaxOutputTokens, tc.wantOutput)
		}
	}
}

// TestLookupCapabilities_UnknownReturnsConservativeDefault asserts an
// unrecognised model falls back to the conservative default (small window,
// 4096 output) rather than an optimistic guess.
func TestLookupCapabilities_UnknownReturnsConservativeDefault(t *testing.T) {
	got := LookupCapabilities("acme", "some-future-model")
	if got.ContextWindowTokens != defaultContextWindowTokens {
		t.Errorf("unknown model window = %d, want conservative default %d", got.ContextWindowTokens, defaultContextWindowTokens)
	}
	if got.MaxOutputTokens != defaultMaxOutputTokens {
		t.Errorf("unknown model output = %d, want conservative default %d", got.MaxOutputTokens, defaultMaxOutputTokens)
	}
}

// TestCapabilitiesTable_OverrideThenBuiltin asserts the table's override
// layer takes precedence over the built-in entry, and that Lookup reports
// found/not-found like the cost table does.
func TestCapabilitiesTable_OverrideThenBuiltin(t *testing.T) {
	tbl := NewCapabilitiesTable()
	if _, ok := tbl.Lookup("acme", "x"); ok {
		t.Fatal("Lookup of unknown pair reported found")
	}
	got, ok := tbl.Lookup("claude", "claude-sonnet-4-20250514")
	if !ok || got.ContextWindowTokens != 200000 {
		t.Fatalf("builtin lookup = %+v ok=%v, want window 200000", got, ok)
	}
	tbl.WithOverride("claude", "claude-sonnet-4-20250514", ModelCapabilities{ContextWindowTokens: 999, MaxOutputTokens: 7})
	got, ok = tbl.Lookup("claude", "claude-sonnet-4-20250514")
	if !ok || got.ContextWindowTokens != 999 || got.MaxOutputTokens != 7 {
		t.Errorf("override lookup = %+v ok=%v, want window 999 output 7", got, ok)
	}
}
