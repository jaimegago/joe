//go:build e2e
// +build e2e

package e2e

import (
	"testing"
)

// e2eConfiguredModel is the model id the harness config tells joe to ask the
// responder for (llm.available.e2e-responder.model in harness.go).
const e2eConfiguredModel = "e2e-responder"

// TestE2E_TaskReportsProviderStatedModel drives one turn over the public API
// and reads back, on the task response, the model the scripted responder named
// on its own completions — beside the id joe is configured to ask for, and not
// in its place (joe D-0173).
//
// The two values differ by construction (ResponderStatedModel), so the
// assertions cannot pass on the configured id repeated back: what is under
// test is that the provider's statement travels from the response through the
// adapter and the loop to the wire.
func TestE2E_TaskReportsProviderStatedModel(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	harness := NewTestHarness(t)
	defer harness.Stop()
	if err := harness.Start(); err != nil {
		t.Fatalf("failed to start harness: %v", err)
	}

	resp, err := harness.RunTask("Resolve localhost and tell me what you found.")
	if err != nil {
		t.Fatalf("POST /api/v1/tasks: %v", err)
	}
	if resp.Status != "completed" {
		t.Fatalf("status = %q (error %q), want completed", resp.Status, resp.Error)
	}

	// The configured id keeps its meaning and its place.
	if resp.Model != e2eConfiguredModel {
		t.Errorf("model = %q, want the configured id %q", resp.Model, e2eConfiguredModel)
	}

	// The responder's own statement is reported beside it: one distinct value,
	// accounting for every call of the turn, including the no-tool-call probe
	// that surfaces as no step.
	if len(resp.StatedModels) != 1 || resp.StatedModels[0] != ResponderStatedModel {
		t.Errorf("stated_models = %v, want [%s]", resp.StatedModels, ResponderStatedModel)
	}
	if !resp.StatedModelsComplete {
		t.Error("stated_models_complete = false, want true: the responder names a model on every completion")
	}

	// And per step: each step's response carries the statement it was made with.
	if len(resp.Steps) == 0 {
		t.Fatal("the turn reported no steps")
	}
	for i, step := range resp.Steps {
		if step.LLMResponse.StatedModel != ResponderStatedModel {
			t.Errorf("steps[%d].llm_response.stated_model = %q, want %q", i, step.LLMResponse.StatedModel, ResponderStatedModel)
		}
	}

	t.Logf("✓ task %s: model %q, stated_models %v over %d steps", resp.TaskID, resp.Model, resp.StatedModels, len(resp.Steps))
}
