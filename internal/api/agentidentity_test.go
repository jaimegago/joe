package api_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/prompts"
	"github.com/jaimegago/joe/internal/safety"
)

type agentIdentityBody struct {
	ConfigHash       string `json:"config_hash"`
	SystemPromptHash string `json:"system_prompt_hash"`
	Model            string `json:"model"`
	Provider         string `json:"provider"`
}

var sha256Form = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// agentIdentityOf serves GET /agent-identity from a server built on services
// and decodes the answer.
func agentIdentityOf(t *testing.T, services *core.Services) agentIdentityBody {
	t.Helper()
	if services.Adapters == nil {
		services.Adapters = adapters.NewRegistry()
	}
	server := api.New(services, api.TestingPolicyEngine(services))
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/agent-identity", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var body agentIdentityBody
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

func agentIdentityConfig(model string) *config.Config {
	return &config.Config{LLM: config.LLMConfig{
		Current: "main",
		Available: map[string]config.ModelConfig{
			"main": {Provider: "gemini", Model: model},
		},
	}}
}

// The read answers with both hashes in one form, and with the model and the
// provider a task response would attest (D-0153): the provider's identifier,
// not the catalogue key.
func TestAgentIdentity_ReportsHashesAndTheActiveModel(t *testing.T) {
	got := agentIdentityOf(t, &core.Services{Config: agentIdentityConfig("gemini-2.5-pro")})

	if !sha256Form.MatchString(got.ConfigHash) {
		t.Errorf("config_hash = %q, want sha256:<64 hex>", got.ConfigHash)
	}
	if !sha256Form.MatchString(got.SystemPromptHash) {
		t.Errorf("system_prompt_hash = %q, want sha256:<64 hex>", got.SystemPromptHash)
	}
	if got.Model != "gemini-2.5-pro" || got.Provider != "gemini" {
		t.Errorf("model, provider = %q, %q; want gemini-2.5-pro, gemini", got.Model, got.Provider)
	}
}

// In full mode no posture section is appended, so the system prompt every
// task starts from is the task system prompt and the hash is its digest: an
// outside party holding the prompt reproduces the value.
func TestAgentIdentity_SystemPromptHashIsTheDigestOfTheTaskSystemPrompt(t *testing.T) {
	got := agentIdentityOf(t, &core.Services{Config: agentIdentityConfig("m")})

	sum := sha256.Sum256([]byte(prompts.TaskSystem))
	if want := "sha256:" + hex.EncodeToString(sum[:]); got.SystemPromptHash != want {
		t.Errorf("system_prompt_hash = %s, want the digest of prompts.TaskSystem %s", got.SystemPromptHash, want)
	}
}

// Two reads of one configuration agree, and a change to what the hash covers
// moves it: the model, and the posture.
func TestAgentIdentity_ConfigHashFollowsTheConfiguration(t *testing.T) {
	base := agentIdentityOf(t, &core.Services{Config: agentIdentityConfig("model-a")})
	again := agentIdentityOf(t, &core.Services{Config: agentIdentityConfig("model-a")})
	if base.ConfigHash != again.ConfigHash {
		t.Errorf("one configuration read twice gave %s and %s", base.ConfigHash, again.ConfigHash)
	}

	otherModel := agentIdentityOf(t, &core.Services{Config: agentIdentityConfig("model-b")})
	if otherModel.ConfigHash == base.ConfigHash {
		t.Error("config_hash did not move with the active model")
	}

	observation := agentIdentityOf(t, &core.Services{
		Config:     agentIdentityConfig("model-a"),
		WriteFloor: safety.ResolveWriteFloor(false, true),
	})
	if observation.ConfigHash == base.ConfigHash {
		t.Error("config_hash did not move with the posture")
	}
	if observation.SystemPromptHash == base.SystemPromptHash {
		t.Error("system_prompt_hash did not move with the posture section")
	}
}

// What the hash leaves out does not move it: where the process listens, and
// its credentials. Two deployments that differ only there are one agent
// configuration.
func TestAgentIdentity_ConfigHashIgnoresPlacementAndCredentials(t *testing.T) {
	// Both carry a service account, so access control is on in both: whether
	// one is configured is covered by the hash, and which key it holds is not.
	original := agentIdentityConfig("model-a")
	original.Server.ServiceAccounts = []config.ServiceAccount{{Name: "sa", Key: "one-key"}}
	base := agentIdentityOf(t, &core.Services{Config: original})

	moved := agentIdentityConfig("model-a")
	moved.Server.Address = "localhost:9999"
	moved.Server.ServiceAccounts = []config.ServiceAccount{{Name: "sa", Key: "another-key"}}
	moved.Database.DSN = "/somewhere/else/joe.db"
	moved.Auth.OIDC.ClientSecret = "another-secret"
	if got := agentIdentityOf(t, &core.Services{Config: moved}); got.ConfigHash != base.ConfigHash {
		t.Errorf("config_hash moved with placement or credentials: %s, was %s", got.ConfigHash, base.ConfigHash)
	}
}

// A server with no model configured reports neither a model nor a provider:
// the keys are absent, never empty strings standing in for a value.
func TestAgentIdentity_NoActiveModelOmitsBoth(t *testing.T) {
	services := &core.Services{Config: &config.Config{}, Adapters: adapters.NewRegistry()}
	server := api.New(services, api.TestingPolicyEngine(services))
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/agent-identity", nil))
	var raw map[string]any
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{"model", "provider"} {
		if _, ok := raw[key]; ok {
			t.Errorf("%q is present with no active model: %v", key, raw[key])
		}
	}
}
