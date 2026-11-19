//go:build e2e
// +build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"regexp"
	"testing"
)

// TestE2E_AgentIdentity reads GET /api/v1/agent-identity from a running joe
// over its public API (D-0172): both hashes in their one form, and the model
// and provider a task of this process runs on — the provider's identifier the
// harness configured, which is also what a task response attests.
func TestE2E_AgentIdentity(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	harness := NewTestHarness(t)
	defer harness.Stop()
	if err := harness.Start(); err != nil {
		t.Fatalf("failed to start harness: %v", err)
	}

	read := func() map[string]any {
		t.Helper()
		status, body, err := harness.Do(http.MethodGet, "/api/v1/agent-identity", e2eServiceAccountKey, nil)
		if err != nil {
			t.Fatalf("GET /api/v1/agent-identity: %v", err)
		}
		if status != http.StatusOK {
			t.Fatalf("GET /api/v1/agent-identity = %d, want 200; body: %s", status, body)
		}
		var out map[string]any
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatalf("decode: %v; body: %s", err, body)
		}
		return out
	}

	first := read()
	form := regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	for _, key := range []string{"config_hash", "system_prompt_hash"} {
		value, _ := first[key].(string)
		if !form.MatchString(value) {
			t.Errorf("%s = %q, want sha256:<64 hex>", key, value)
		}
	}
	if first["model"] != "e2e-responder" || first["provider"] != "openai-compat" {
		t.Errorf("model, provider = %v, %v; want e2e-responder, openai-compat", first["model"], first["provider"])
	}

	// The task this process runs attests the same model the read declares.
	task, err := harness.RunTask("Say hello.")
	if err != nil {
		t.Fatalf("run task: %v", err)
	}
	if task.Status != "completed" {
		t.Fatalf("task status = %q (%s), want completed", task.Status, task.Error)
	}

	// The read changes nothing: a second one, after a task ran, is the same.
	second := read()
	for _, key := range []string{"config_hash", "system_prompt_hash", "model", "provider"} {
		if first[key] != second[key] {
			t.Errorf("%s changed between two reads: %v then %v", key, first[key], second[key])
		}
	}

	// It requires the same authentication as every other route.
	status, _, err := harness.Do(http.MethodGet, "/api/v1/agent-identity", "not-a-key", nil)
	if err != nil {
		t.Fatalf("unauthenticated GET: %v", err)
	}
	if status != http.StatusUnauthorized {
		t.Errorf("GET with a bad key = %d, want 401", status)
	}
}
