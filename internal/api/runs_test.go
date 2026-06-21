package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/runmodel"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
)

// newRunsServer builds an httptest server with the session-model + run-model
// stack wired in. Returns the test server URL, both repositories, and the
// raw store handle (for tests that need to peek at internal state).
func newRunsServer(t *testing.T) (*httptest.Server, sessionmodel.Repository, runmodel.Repository) {
	t.Helper()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	sessRepo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	runRepo := runmodel.NewRepository(s.DB(), store.DriverSQLite)
	svc := &core.Services{
		Store:        s,
		SessionModel: sessRepo,
		RunModel:     runRepo,
	}
	srv := api.New(svc)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	handler := rbac.IdentityMiddleware(testPrincipalProvider{})(mux)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts, sessRepo, runRepo
}

// declareIncident creates a 'default' session owned by principal and PROMOTES
// it in place to the incident master via the atomic regime path (§12.3
// promote-in-place). Declaration no longer mints a fresh row, so the starting
// 'default' session is created first. Returns the (now incident) session id.
// Used as the starting point for run-lifecycle and captain tests.
func declareIncident(t *testing.T, sessRepo sessionmodel.Repository, principal string) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := sessRepo.CreateSession(context.Background(), sessionmodel.AgentSession{
		ID: id, Type: sessionmodel.SessionTypeDefault, CreatorPrincipal: principal,
	}); err != nil {
		t.Fatalf("create default session: %v", err)
	}
	if _, _, err := sessRepo.DeclareIncidentRegime(context.Background(), principal, id, sessionmodel.RegimeKindHuman); err != nil {
		t.Fatalf("declare incident (promote): %v", err)
	}
	return id
}

// startRun posts a new run for the given session and returns the run id.
func startRun(t *testing.T, ts *httptest.Server, sessionID, principal string) string {
	t.Helper()
	resp := doRequest(t, http.MethodPost, ts.URL+"/api/v1/sessions/"+sessionID+"/runs", principal, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("start run status = %d, want 201", resp.StatusCode)
	}
	var run runmodel.Run
	json.NewDecoder(resp.Body).Decode(&run)
	resp.Body.Close()
	if run.ID == "" {
		t.Fatal("start run returned empty id")
	}
	return run.ID
}

// --- §D3 single-threaded: second POST /sessions/{id}/runs is 409 ---

func TestRunsAPI_D3_SingleThreadedRejected(t *testing.T) {
	ts, sessRepo, _ := newRunsServer(t)
	sessionID := declareIncident(t, sessRepo, "alice")

	r1 := doRequest(t, http.MethodPost, ts.URL+"/api/v1/sessions/"+sessionID+"/runs", "alice", nil)
	if r1.StatusCode != http.StatusCreated {
		t.Fatalf("first run status = %d, want 201", r1.StatusCode)
	}
	r1.Body.Close()

	r2 := doRequest(t, http.MethodPost, ts.URL+"/api/v1/sessions/"+sessionID+"/runs", "alice", nil)
	if r2.StatusCode != http.StatusConflict {
		body, _ := readBody(r2)
		t.Fatalf("second run status = %d, want 409 (§D3 single-threaded). body=%s",
			r2.StatusCode, body)
	}
	r2.Body.Close()
}

// --- State-machine matrix at the HTTP layer ---

func TestRunsAPI_StateMachine_LegalTransitions(t *testing.T) {
	ts, sessRepo, _ := newRunsServer(t)
	sessionID := declareIncident(t, sessRepo, "alice")
	runID := startRun(t, ts, sessionID, "alice")

	// running → awaiting_input via solicitation
	r := doRequest(t, http.MethodPost, ts.URL+"/api/v1/runs/"+runID+"/solicitations", "alice",
		map[string]any{
			"kind":    "decision",
			"payload": map[string]any{"options": []string{"a", "b"}},
		})
	if r.StatusCode != http.StatusCreated {
		body, _ := readBody(r)
		t.Fatalf("open solicitation status = %d, want 201; body=%s", r.StatusCode, body)
	}
	var sol runmodel.Solicitation
	json.NewDecoder(r.Body).Decode(&sol)
	r.Body.Close()

	// awaiting_input → running via resolve
	r2 := doRequest(t, http.MethodPost, ts.URL+"/api/v1/solicitations/"+sol.ID+"/resolve", "alice",
		map[string]any{"resolution_payload": map[string]any{"choice": "a"}})
	if r2.StatusCode != http.StatusOK {
		body, _ := readBody(r2)
		t.Fatalf("resolve status = %d, want 200; body=%s", r2.StatusCode, body)
	}
	r2.Body.Close()

	// running → awaiting_world via world handle (idempotency_key required)
	r3 := doRequest(t, http.MethodPost, ts.URL+"/api/v1/runs/"+runID+"/world_handles", "alice",
		map[string]any{
			"idempotency_key": "k1",
			"locator":         "k8s://deploy/x",
			"query_meta":      map[string]any{"namespace": "default"},
		})
	if r3.StatusCode != http.StatusCreated {
		body, _ := readBody(r3)
		t.Fatalf("record handle status = %d, want 201; body=%s", r3.StatusCode, body)
	}
	var h runmodel.WorldHandle
	json.NewDecoder(r3.Body).Decode(&h)
	r3.Body.Close()

	// awaiting_world → running via terminal observe
	r4 := doRequest(t, http.MethodPost,
		ts.URL+"/api/v1/runs/"+runID+"/world_handles/"+h.ID+"/observe", "alice",
		map[string]any{
			"idempotency_key": "k2",
			"observed_state":  "ready",
			"terminal":        true,
		})
	if r4.StatusCode != http.StatusOK {
		body, _ := readBody(r4)
		t.Fatalf("observe terminal status = %d, want 200; body=%s", r4.StatusCode, body)
	}
	var obs map[string]any
	json.NewDecoder(r4.Body).Decode(&obs)
	r4.Body.Close()
	if obs["run_state"] != "running" {
		t.Errorf("run_state after terminal observe = %v, want running", obs["run_state"])
	}

	// running → completed
	r5 := doRequest(t, http.MethodPost, ts.URL+"/api/v1/runs/"+runID+"/complete", "alice", nil)
	if r5.StatusCode != http.StatusOK {
		body, _ := readBody(r5)
		t.Fatalf("complete status = %d, want 200; body=%s", r5.StatusCode, body)
	}
	r5.Body.Close()
}

func TestRunsAPI_StateMachine_IllegalTransitions(t *testing.T) {
	ts, sessRepo, runRepo := newRunsServer(t)
	sessionID := declareIncident(t, sessRepo, "alice")
	runID := startRun(t, ts, sessionID, "alice")

	// Move run to completed.
	r := doRequest(t, http.MethodPost, ts.URL+"/api/v1/runs/"+runID+"/complete", "alice", nil)
	r.Body.Close()

	// Now: any further transition is illegal.
	cases := []struct {
		name string
		path string
		body any
	}{
		{
			"open solicitation on completed",
			"/api/v1/runs/" + runID + "/solicitations",
			map[string]any{"kind": "decision", "payload": map[string]any{"options": []string{"a"}}},
		},
		{
			"record world handle on completed",
			"/api/v1/runs/" + runID + "/world_handles",
			map[string]any{"idempotency_key": "k", "locator": "x"},
		},
		{
			"append step to completed",
			"/api/v1/runs/" + runID + "/steps",
			map[string]any{"step_number": 1, "kind": "reasoning"},
		},
		{
			"second complete on completed",
			"/api/v1/runs/" + runID + "/complete",
			nil,
		},
		{
			"terminate completed",
			"/api/v1/runs/" + runID + "/terminate",
			nil,
		},
		{
			"fail completed",
			"/api/v1/runs/" + runID + "/fail",
			nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := doRequest(t, http.MethodPost, ts.URL+tc.path, "alice", tc.body)
			if resp.StatusCode < 400 || resp.StatusCode >= 500 {
				body, _ := readBody(resp)
				t.Errorf("status = %d, want 4xx; body=%s", resp.StatusCode, body)
			}
			resp.Body.Close()
		})
	}

	// Sanity: the run really is still completed (no half-baked
	// transitions leaked through). Use a fresh ctx since the test
	// server already serves above.
	got, _ := runRepo.GetRun(context.Background(), runID)
	if got.State != runmodel.RunStateCompleted {
		t.Errorf("run state = %q, want completed", got.State)
	}
}

// --- Solicitation payload validation tests ---

func TestRunsAPI_Solicitation_DecisionRequiresBoundedOptions(t *testing.T) {
	ts, sessRepo, _ := newRunsServer(t)
	sessionID := declareIncident(t, sessRepo, "alice")
	runID := startRun(t, ts, sessionID, "alice")

	cases := []struct {
		name string
		body any
		want int
	}{
		{"missing options", map[string]any{"kind": "decision", "payload": map[string]any{}}, http.StatusBadRequest},
		{"empty options", map[string]any{"kind": "decision", "payload": map[string]any{"options": []string{}}}, http.StatusBadRequest},
		{"non-empty options", map[string]any{"kind": "decision", "payload": map[string]any{"options": []string{"a"}}}, http.StatusCreated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := doRequest(t, http.MethodPost, ts.URL+"/api/v1/runs/"+runID+"/solicitations", "alice", tc.body)
			defer r.Body.Close()
			if r.StatusCode != tc.want {
				body, _ := readBody(r)
				t.Errorf("status = %d, want %d; body=%s", r.StatusCode, tc.want, body)
			}
			// Reset: if the test created a solicitation, resolve it so the
			// next case starts from 'running'.
			if r.StatusCode == http.StatusCreated {
				var sol runmodel.Solicitation
				json.NewDecoder(r.Body).Decode(&sol)
				doRequest(t, http.MethodPost, ts.URL+"/api/v1/solicitations/"+sol.ID+"/resolve", "alice",
					map[string]any{"resolution_payload": map[string]any{"choice": "a"}}).Body.Close()
			}
		})
	}
}

func TestRunsAPI_Solicitation_ProvideDataRequiresLiveness(t *testing.T) {
	ts, sessRepo, _ := newRunsServer(t)
	sessionID := declareIncident(t, sessRepo, "alice")
	runID := startRun(t, ts, sessionID, "alice")

	cases := []struct {
		name string
		body any
		want int
	}{
		{"missing liveness", map[string]any{"kind": "provide_data", "payload": map[string]any{"what": "x"}}, http.StatusBadRequest},
		{"invalid liveness", map[string]any{"kind": "provide_data", "payload": map[string]any{"liveness": "maybe"}}, http.StatusBadRequest},
		{"attached_human_now", map[string]any{"kind": "provide_data", "payload": map[string]any{"liveness": "attached_human_now"}}, http.StatusCreated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := doRequest(t, http.MethodPost, ts.URL+"/api/v1/runs/"+runID+"/solicitations", "alice", tc.body)
			defer r.Body.Close()
			if r.StatusCode != tc.want {
				body, _ := readBody(r)
				t.Errorf("status = %d, want %d; body=%s", r.StatusCode, tc.want, body)
			}
			if r.StatusCode == http.StatusCreated {
				var sol runmodel.Solicitation
				json.NewDecoder(r.Body).Decode(&sol)
				doRequest(t, http.MethodPost, ts.URL+"/api/v1/solicitations/"+sol.ID+"/resolve", "alice",
					map[string]any{"resolution_payload": map[string]any{}}).Body.Close()
			}
		})
	}
}

func TestRunsAPI_Solicitation_ConfirmCloseRequiresLedger(t *testing.T) {
	ts, sessRepo, _ := newRunsServer(t)
	sessionID := declareIncident(t, sessRepo, "alice")
	runID := startRun(t, ts, sessionID, "alice")

	r1 := doRequest(t, http.MethodPost, ts.URL+"/api/v1/runs/"+runID+"/solicitations", "alice",
		map[string]any{"kind": "confirm_close", "payload": map[string]any{"conclusion": "done"}})
	if r1.StatusCode != http.StatusBadRequest {
		body, _ := readBody(r1)
		t.Errorf("confirm_close without action_ledger status = %d, want 400; body=%s", r1.StatusCode, body)
	}
	r1.Body.Close()

	r2 := doRequest(t, http.MethodPost, ts.URL+"/api/v1/runs/"+runID+"/solicitations", "alice",
		map[string]any{"kind": "confirm_close", "payload": map[string]any{
			"conclusion":    "done",
			"action_ledger": []any{},
		}})
	if r2.StatusCode != http.StatusCreated {
		body, _ := readBody(r2)
		t.Errorf("confirm_close with action_ledger status = %d, want 201; body=%s", r2.StatusCode, body)
	}
	r2.Body.Close()
}

// --- §D5 idempotency-key required on world-handle endpoints ---

func TestRunsAPI_D5_WorldHandleRequiresIdempotencyKey(t *testing.T) {
	ts, sessRepo, _ := newRunsServer(t)
	sessionID := declareIncident(t, sessRepo, "alice")
	runID := startRun(t, ts, sessionID, "alice")

	// Record without key → 400.
	r := doRequest(t, http.MethodPost, ts.URL+"/api/v1/runs/"+runID+"/world_handles", "alice",
		map[string]any{"locator": "k8s://x"})
	if r.StatusCode != http.StatusBadRequest {
		body, _ := readBody(r)
		t.Errorf("missing idempotency_key status = %d, want 400; body=%s", r.StatusCode, body)
	}
	r.Body.Close()

	// With key → 201.
	r2 := doRequest(t, http.MethodPost, ts.URL+"/api/v1/runs/"+runID+"/world_handles", "alice",
		map[string]any{"idempotency_key": "k", "locator": "k8s://x"})
	if r2.StatusCode != http.StatusCreated {
		body, _ := readBody(r2)
		t.Fatalf("with key status = %d, want 201; body=%s", r2.StatusCode, body)
	}
	var h runmodel.WorldHandle
	json.NewDecoder(r2.Body).Decode(&h)
	r2.Body.Close()

	// Observe without key → 400.
	r3 := doRequest(t, http.MethodPost,
		ts.URL+"/api/v1/runs/"+runID+"/world_handles/"+h.ID+"/observe", "alice",
		map[string]any{"observed_state": "ready", "terminal": false})
	if r3.StatusCode != http.StatusBadRequest {
		body, _ := readBody(r3)
		t.Errorf("observe missing key status = %d, want 400; body=%s", r3.StatusCode, body)
	}
	r3.Body.Close()
}

// --- §D6 never-re-issue: response schema on world-handle observe has
//    NO field named retry / reissue / re_issue / replay / re_fire. ---

func TestRunsAPI_D6_ObserveResponseHasNoReissueHints(t *testing.T) {
	ts, sessRepo, _ := newRunsServer(t)
	sessionID := declareIncident(t, sessRepo, "alice")
	runID := startRun(t, ts, sessionID, "alice")

	r := doRequest(t, http.MethodPost, ts.URL+"/api/v1/runs/"+runID+"/world_handles", "alice",
		map[string]any{"idempotency_key": "k", "locator": "k8s://x"})
	var h runmodel.WorldHandle
	json.NewDecoder(r.Body).Decode(&h)
	r.Body.Close()

	r2 := doRequest(t, http.MethodPost,
		ts.URL+"/api/v1/runs/"+runID+"/world_handles/"+h.ID+"/observe", "alice",
		map[string]any{"idempotency_key": "k2", "observed_state": "pending", "terminal": false})
	defer r2.Body.Close()

	if r2.StatusCode != http.StatusOK {
		t.Fatalf("observe status = %d, want 200", r2.StatusCode)
	}

	// Parse to map and assert the field set.
	body, _ := readBody(r2)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("parse observe response: %v", err)
	}
	forbidden := []string{"retry", "reissue", "re_issue", "replay", "re_fire", "rewind", "pre_effect", "pre-effect"}
	for _, k := range parsed {
		_ = k
	}
	for name := range parsed {
		lower := strings.ToLower(name)
		for _, bad := range forbidden {
			if strings.Contains(lower, bad) {
				t.Errorf("§D6 violation: observe response contains forbidden field %q (matches %q)", name, bad)
			}
		}
	}
}

// --- §D7 override-forms: exactly three terminal endpoints; no rewind. ---

func TestRunsAPI_D7_NoRewindOrPreEffectEndpoints(t *testing.T) {
	ts, sessRepo, _ := newRunsServer(t)
	sessionID := declareIncident(t, sessRepo, "alice")
	runID := startRun(t, ts, sessionID, "alice")

	// Forbidden paths must 404 / 405 (route not registered).
	forbidden := []string{
		"/api/v1/runs/" + runID + "/rewind",
		"/api/v1/runs/" + runID + "/pre_effect",
		"/api/v1/runs/" + runID + "/pre-effect",
		"/api/v1/runs/" + runID + "/treat_as_never_happened",
		"/api/v1/runs/" + runID + "/replay",
		"/api/v1/runs/" + runID + "/reissue",
	}
	for _, p := range forbidden {
		t.Run(p, func(t *testing.T) {
			r := doRequest(t, http.MethodPost, ts.URL+p, "alice", nil)
			defer r.Body.Close()
			if r.StatusCode != http.StatusNotFound && r.StatusCode != http.StatusMethodNotAllowed {
				t.Errorf("status = %d, want 404 or 405 (§D7 forbids non-canonical override paths)", r.StatusCode)
			}
		})
	}

	// Confirm: each of the three canonical endpoints is reachable.
	// Complete the existing run first so subsequent runs on the same
	// session don't hit the §D3 single-running guard. After each
	// terminal transition the run is no longer 'running', so a fresh
	// run can start on the same session.
	doRequest(t, http.MethodPost, ts.URL+"/api/v1/runs/"+runID+"/complete", "alice", nil).Body.Close()
	canonical := []string{"complete", "fail", "terminate"}
	for _, c := range canonical {
		t.Run("canonical "+c, func(t *testing.T) {
			rid := startRun(t, ts, sessionID, "alice")
			r := doRequest(t, http.MethodPost, ts.URL+"/api/v1/runs/"+rid+"/"+c, "alice", nil)
			defer r.Body.Close()
			if r.StatusCode != http.StatusOK {
				body, _ := readBody(r)
				t.Errorf("canonical %s status = %d, want 200; body=%s", c, r.StatusCode, body)
			}
		})
	}
}

// --- §D8 SITREP shape: response includes only the allowlisted fields,
//    NEVER a 'steps' field (that would be the reasoning trace). ---

func TestRunsAPI_D8_SITREPShape(t *testing.T) {
	ts, sessRepo, runRepo := newRunsServer(t)
	sessionID := declareIncident(t, sessRepo, "alice")
	runID := startRun(t, ts, sessionID, "alice")

	// Append a reasoning step so the synthesized_understanding field
	// gets populated from the latest reasoning payload.
	doRequest(t, http.MethodPost, ts.URL+"/api/v1/runs/"+runID+"/steps", "alice",
		map[string]any{"step_number": 1, "kind": "reasoning", "payload": map[string]any{"summary": "step 1"}}).Body.Close()
	doRequest(t, http.MethodPost, ts.URL+"/api/v1/runs/"+runID+"/steps", "alice",
		map[string]any{"step_number": 2, "kind": "reasoning", "payload": map[string]any{"summary": "step 2"}}).Body.Close()

	// Open a solicitation so the SITREP includes an open_solicitation.
	sol := doRequest(t, http.MethodPost, ts.URL+"/api/v1/runs/"+runID+"/solicitations", "alice",
		map[string]any{"kind": "decision", "payload": map[string]any{"options": []string{"a"}}})
	var solRow runmodel.Solicitation
	json.NewDecoder(sol.Body).Decode(&solRow)
	sol.Body.Close()
	// Manually link the open solicitation to its step (the SITREP
	// lookup walks steps backward to find the solicitation_open kind).
	doRequest(t, http.MethodPost, ts.URL+"/api/v1/runs/"+runID+"/steps", "alice",
		map[string]any{
			"step_number": 3,
			"kind":        "solicitation_open",
			"payload":     map[string]any{"solicitation_id": solRow.ID},
		}).Body.Close()

	// Resolve to test other states too; but first inspect the SITREP
	// while awaiting_input so OpenSolicitation is set.
	r := doRequest(t, http.MethodGet, ts.URL+"/api/v1/runs/"+runID, "alice", nil)
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("sitrep status = %d, want 200", r.StatusCode)
	}
	body, _ := readBody(r)

	// Parse to a generic map and assert the EXACT key set on the
	// top level. The §D8 allowlist is:
	//   run, synthesized_understanding, open_solicitation, world_handles, action_ledger.
	// "steps" must NOT appear — that would be the reasoning trace.
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("parse sitrep: %v", err)
	}
	expected := []string{"run", "synthesized_understanding", "open_solicitation", "world_handles", "action_ledger"}
	got := make([]string, 0, len(parsed))
	for k := range parsed {
		got = append(got, k)
	}
	sortStrings(got)
	sortStrings(expected)
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("§D8 SITREP shape drifted.\n  got:      %v\n  expected: %v\n\n"+
			"The SITREP is the §D8 attaching-SRE rehydration payload. "+
			"It must include only: run state, latest synthesized understanding, "+
			"open solicitation, world handles, action ledger. The full step trail "+
			"(the reasoning trace) is excluded by design. Adding a field requires "+
			"updating this allowlist with explicit justification against §D8.",
			got, expected)
	}

	// And specifically: no 'steps' field.
	for k := range parsed {
		lower := strings.ToLower(k)
		if lower == "steps" || strings.Contains(lower, "reasoning_trace") {
			t.Errorf("§D8 violation: SITREP contains forbidden field %q (reasoning trace excluded)", k)
		}
	}

	// synthesized_understanding picks the LATEST reasoning step's payload.
	var synth *string
	json.Unmarshal(parsed["synthesized_understanding"], &synth)
	if synth == nil || !strings.Contains(*synth, "step 2") {
		t.Errorf("synthesized_understanding = %v, want latest reasoning step (step 2)", synth)
	}

	// Direct repo lookup confirms the run is genuinely awaiting_input.
	got2, _ := runRepo.GetRun(context.Background(), runID)
	if got2.State != runmodel.RunStateAwaitingInput {
		t.Errorf("run state = %q, want awaiting_input", got2.State)
	}
}

// readBody reads and returns the response body without closing it
// (caller manages defer). Returns "" on error.
func readBody(r *http.Response) (string, error) {
	if r.Body == nil {
		return "", nil
	}
	var buf [4096]byte
	n, _ := r.Body.Read(buf[:])
	return string(buf[:n]), nil
}

// sortStrings is a tiny inline sort to avoid pulling in sort just for
// the §D8 deep-equal. The slices are tiny.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
