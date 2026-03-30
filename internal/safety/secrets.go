package safety

import (
	"encoding/base64"
	"log/slog"
	"strings"
)

const secretRedactionMarker = "[Secret value redacted from response]"

// RedactSecretsFromResponse scans text for any of the provided secret values
// and replaces them with a redaction marker. This is a defense-in-depth
// measure: the primary protection is tool-level redaction in k8s_get, but
// this catches secret values that may have entered the LLM context through
// other paths (env vars, logs, configmap references).
//
// secretValues should contain the raw (decoded) secret values to search for.
// Both the raw value and its base64-encoded form are checked.
func RedactSecretsFromResponse(text string, secretValues []string) (string, bool) {
	if len(secretValues) == 0 || text == "" {
		return text, false
	}

	redacted := false
	for _, val := range secretValues {
		if val == "" {
			continue
		}

		// Check for the raw value
		if strings.Contains(text, val) {
			text = strings.ReplaceAll(text, val, secretRedactionMarker)
			redacted = true
		}

		// Check for the base64-encoded form
		encoded := base64.StdEncoding.EncodeToString([]byte(val))
		if strings.Contains(text, encoded) {
			text = strings.ReplaceAll(text, encoded, secretRedactionMarker)
			redacted = true
		}
	}

	if redacted {
		slog.Info("response secret redaction: one or more secret values were redacted from the response")
	}
	return text, redacted
}
