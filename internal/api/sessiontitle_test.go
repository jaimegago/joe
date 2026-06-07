package api

import (
	"strings"
	"testing"
)

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

// TestUntitledSentinelDetected pins the guard that stops the title prompt's
// "New chat" sentinel (returned for a meaningless opening message) from being
// persisted as a real title — which would freeze the session at "New chat".
// generateTitleAsync sanitizes the model reply then compares it case-insensitively
// to untitledSentinel; this exercises that same path on the forms a model emits.
func TestUntitledSentinelDetected(t *testing.T) {
	sentinelReplies := []string{
		"New chat",
		"new chat",
		"\"New chat\"",
		"New chat.",
		"NEW CHAT",
	}
	for _, raw := range sentinelReplies {
		if got := sanitizeLLMTitle(raw); !strings.EqualFold(got, untitledSentinel) {
			t.Errorf("sanitizeLLMTitle(%q) = %q; want it to match the untitled sentinel %q so it is not persisted", raw, got, untitledSentinel)
		}
	}

	// A real title must NOT be mistaken for the sentinel.
	if strings.EqualFold(sanitizeLLMTitle("New Chat Service Outage"), untitledSentinel) {
		t.Error("a genuine title containing the word 'chat' was wrongly treated as the untitled sentinel")
	}
}
