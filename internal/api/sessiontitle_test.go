package api

import "testing"

func TestHeuristicTitle(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"whitespace only", "   \n\t ", ""},
		{"short", "why is the pod crashing", "why is the pod crashing"},
		{"caps to six words", "please tell me why the payment service keeps restarting today",
			"please tell me why the payment"},
		{"collapses whitespace", "  why   is\tthe   pod  down ", "why is the pod down"},
		// Two words whose join exceeds the 60-char cap: the cap trims back to the
		// word boundary so the title never ends mid-word.
		{"length cap trims to word boundary",
			"supercalifragilisticexpialidocious antidisestablishmentarianism",
			"supercalifragilisticexpialidocious"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := heuristicTitle(c.in); got != c.want {
				t.Errorf("heuristicTitle(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestSanitizeLLMTitle(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain", "Payment Service Restart Loop", "Payment Service Restart Loop"},
		{"strips wrapping quotes", `"Pod CrashLoopBackOff Investigation"`, "Pod CrashLoopBackOff Investigation"},
		{"strips trailing punctuation", "Database Connection Pool Exhaustion.", "Database Connection Pool Exhaustion"},
		{"keeps first non-empty line", "\nHere is a title\nignored second line", "Here is a title"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sanitizeLLMTitle(c.in); got != c.want {
				t.Errorf("sanitizeLLMTitle(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
