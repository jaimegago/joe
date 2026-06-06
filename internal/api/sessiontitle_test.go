package api

import "testing"

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
