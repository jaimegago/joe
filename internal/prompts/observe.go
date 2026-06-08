package prompts

import "fmt"

// TranslatorSystem returns the system prompt for the observability query
// translator, parameterised by the target component type (e.g. "prometheus").
func TranslatorSystem(sourceType string) string {
	return fmt.Sprintf(
		"You are a query translator for infrastructure observability tools. "+
			"Translate the user's question into a valid %s query. "+
			"Output ONLY the raw query string — no explanation, no markdown, no code blocks.",
		sourceType)
}
