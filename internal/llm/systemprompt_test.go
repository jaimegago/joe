package llm

import "testing"

// The load-bearing property of segmenting the system prompt is that it changes
// NO bytes reaching a provider. The task handler used to build one string with
// `s += "\n\n" + section`, skipping empty sections; SystemPrompt must render
// exactly that.
func TestStringReproducesTheConcatenationItReplaced(t *testing.T) {
	const (
		static  = "static task system prompt"
		posture = "posture: observation"
		zone    = "zone scope: zone-a"
		skills  = "skills for this query"
	)

	// What the old code produced.
	want := static
	want += "\n\n" + posture
	want += "\n\n" + zone
	want += "\n\n" + skills

	sys := StaticSystem(static)
	sys = sys.Append(posture, false)
	sys = sys.Append(zone, false)
	sys = sys.Append(skills, false)

	if got := sys.String(); got != want {
		t.Fatalf("rendered prompt changed:\ngot  %q\nwant %q", got, want)
	}
}

// An absent section contributed no separator under the old `if section != ""`
// guards, and must not contribute one now.
func TestAppendDropsEmptySectionsAndTheirSeparators(t *testing.T) {
	sys := StaticSystem("head")
	sys = sys.Append("", false) // full mode injects no posture
	sys = sys.Append("tail", false)

	if got, want := sys.String(), "head\n\ntail"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if len(sys) != 2 {
		t.Fatalf("got %d segments, want 2 — an empty section became a segment", len(sys))
	}
}

func TestStaticSystemIsStableAndEmptyTextIsNil(t *testing.T) {
	sys := StaticSystem("constant")
	if len(sys) != 1 || !sys[0].Stable {
		t.Fatalf("StaticSystem did not produce one stable segment: %+v", sys)
	}
	if sys.StablePrefix() != "constant" {
		t.Fatalf("StablePrefix() = %q", sys.StablePrefix())
	}
	if StaticSystem("") != nil {
		t.Fatal("StaticSystem(\"\") should be nil, so an absent prompt sets no System field")
	}
}

func TestEmptySystemPromptRendersEmpty(t *testing.T) {
	var sys SystemPrompt
	if sys.String() != "" || sys.StablePrefix() != "" {
		t.Fatal("a nil SystemPrompt must render empty")
	}
}
