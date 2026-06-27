package config

import (
	"strings"
	"testing"
)

// TestValidateAPIKeys_OpenAICompat covers the openai-compat validation rules:
// accepted when base_url is set and the key is empty; rejected when base_url
// is missing; and an unknown provider is still rejected by the default case.
func TestValidateAPIKeys_OpenAICompat(t *testing.T) {
	// Key intentionally empty: openai-compat must NOT gate on the API key.
	t.Setenv("OPENAI_API_KEY", "")

	t.Run("accepted with base_url and empty key", func(t *testing.T) {
		mc := ModelConfig{Provider: providerOpenAICompat, Model: "llama3", BaseURL: "http://localhost:11434/v1"}
		if err := ValidateAPIKeys(mc); err != nil {
			t.Fatalf("ValidateAPIKeys = %v, want nil (base_url set, key empty)", err)
		}
		if err := ValidateAPIKeysWithUserMessage(mc); err != nil {
			t.Fatalf("ValidateAPIKeysWithUserMessage = %v, want nil", err)
		}
	})

	t.Run("rejected when base_url missing", func(t *testing.T) {
		mc := ModelConfig{Provider: providerOpenAICompat, Model: "llama3"}
		err := ValidateAPIKeys(mc)
		if err == nil {
			t.Fatal("ValidateAPIKeys = nil, want error when base_url missing")
		}
		if !strings.Contains(err.Error(), "base_url") {
			t.Errorf("error = %q, want it to mention base_url", err.Error())
		}
		if err := ValidateAPIKeysWithUserMessage(mc); err == nil {
			t.Fatal("ValidateAPIKeysWithUserMessage = nil, want error when base_url missing")
		}
	})

	t.Run("unknown provider still rejected", func(t *testing.T) {
		mc := ModelConfig{Provider: "ollama-native", Model: "x"}
		if err := ValidateAPIKeys(mc); err == nil {
			t.Fatal("ValidateAPIKeys = nil, want error for unknown provider")
		}
		if err := ValidateAPIKeysWithUserMessage(mc); err == nil {
			t.Fatal("ValidateAPIKeysWithUserMessage = nil, want error for unknown provider")
		}
	})
}
