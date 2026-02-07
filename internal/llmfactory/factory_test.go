package llmfactory

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/config"
)

func TestNewAdapter_UnsupportedProvider(t *testing.T) {
	_, err := NewAdapter(context.Background(), config.ModelConfig{
		Provider: "openai",
		Model:    "gpt-4",
	})
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
	if !strings.Contains(err.Error(), "unsupported LLM provider") {
		t.Errorf("error = %q, want to contain 'unsupported LLM provider'", err.Error())
	}
}

func TestNewAdapter_ClaudeMissingKey(t *testing.T) {
	orig := os.Getenv("ANTHROPIC_API_KEY")
	os.Unsetenv("ANTHROPIC_API_KEY")
	defer func() {
		if orig != "" {
			os.Setenv("ANTHROPIC_API_KEY", orig)
		}
	}()

	_, err := NewAdapter(context.Background(), config.ModelConfig{
		Provider: "claude",
		Model:    "claude-sonnet-4-20250514",
	})
	if err == nil {
		t.Fatal("expected error when ANTHROPIC_API_KEY is not set")
	}
	if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Errorf("error = %q, want to mention ANTHROPIC_API_KEY", err.Error())
	}
}

func TestNewAdapter_GeminiMissingKey(t *testing.T) {
	origGemini := os.Getenv("GEMINI_API_KEY")
	origGoogle := os.Getenv("GOOGLE_API_KEY")
	os.Unsetenv("GEMINI_API_KEY")
	os.Unsetenv("GOOGLE_API_KEY")
	defer func() {
		if origGemini != "" {
			os.Setenv("GEMINI_API_KEY", origGemini)
		}
		if origGoogle != "" {
			os.Setenv("GOOGLE_API_KEY", origGoogle)
		}
	}()

	_, err := NewAdapter(context.Background(), config.ModelConfig{
		Provider: "gemini",
		Model:    "gemini-2.0-flash-lite",
	})
	if err == nil {
		t.Fatal("expected error when GEMINI_API_KEY and GOOGLE_API_KEY are not set")
	}
	if !strings.Contains(err.Error(), "GEMINI_API_KEY") {
		t.Errorf("error = %q, want to mention GEMINI_API_KEY", err.Error())
	}
}
