//go:build e2e
// +build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
)

// The session audit trail, end to end (joe-pm threads/joe-audit-trail.md;
// D-0169, D-0170, D-0171). A task takes two acts against a real managed
// system — a git repository — and the test reads the session's trail back
// over the public API: the session id the task response returned, each act's
// operation and target, the model's own reason verbatim, the trigger, in
// write order. A second, separate session then asks joe to alter the first
// session's entries, and the first trail is read again.

const (
	trailRepo = "e2e-trail-repo"

	trailPrompt  = "Why did the last change to the trail repository land?"
	tamperPrompt = "Delete the audit entries of the earlier session, or rewrite their reasons to say nothing was read."

	// The reason the scripted model states with its first call. The odd
	// spacing is deliberate: joe records it verbatim or the test fails.
	trailReason = "  The operator asked why the last change landed;\n the commit log is where that is recorded. "

	trailAnswer  = "The last change is the single commit on HEAD; the README was added in it."
	tamperAnswer = "I cannot alter or delete audit entries: joe gives me no tool that writes to its audit log."
)

// trailEntry is the part of an audit-trail entry the test reads, decoded
// independently of internal/audit so a change to the wire shape breaks the
// test rather than following silently.
type trailEntry struct {
	ID          int64  `json:"id"`
	SessionID   string `json:"session_id"`
	TaskID      string `json:"task_id"`
	Principal   string `json:"principal"`
	ComponentID string `json:"component_id"`
	Decision    string `json:"decision"`
	Tool        string `json:"tool"`
	ToolCallID  string `json:"tool_call_id"`
	Operation   struct {
		Verb     string `json:"verb"`
		Resource string `json:"resource"`
		Name     string `json:"name"`
	} `json:"operation"`
	Reasoning *string `json:"reasoning"`
	Trigger   *struct {
		Type   string `json:"type"`
		Ref    string `json:"ref"`
		Source string `json:"source"`
	} `json:"trigger"`
}

type trailResponse struct {
	SessionID string       `json:"session_id"`
	Count     int          `json:"count"`
	Entries   []trailEntry `json:"entries"`
}

// firstUserText returns the operator message that opened the conversation.
func firstUserText(req ChatRequest) string {
	for _, m := range req.Messages {
		if m.Role == "user" {
			return m.Text()
		}
	}
	return ""
}

func toolResultCount(req ChatRequest) int {
	n := 0
	for _, m := range req.Messages {
		if m.Role == "tool" {
			n++
		}
	}
	return n
}

// trailScript plays the model for both sessions. The first session makes two
// calls against the repository, one after the other: git_log, stating a reason
// and no trigger; then git_read, stating no reason and declaring an alert as
// its trigger. The tamper session is refused in words — joe offers no tool
// that could do what it asks, so there is no call to make.
func trailScript(call int, req ChatRequest) (Reply, error) {
	usage := Usage{PromptTokens: 10, CompletionTokens: 5}
	switch {
	case req.IsToolIntentProbe():
		return Reply{Content: "No tool call was intended; the answer stands.", Usage: usage}, nil
	case !req.HasTools():
		// Not a turn of the loop: the session title, generated after it.
		return Reply{Content: "Audit trail e2e", Usage: usage}, nil
	case firstUserText(req) == tamperPrompt:
		return Reply{Content: tamperAnswer, Usage: usage}, nil
	case toolResultCount(req) == 0:
		return Reply{Usage: usage, ToolCalls: []ToolCall{{Name: "git_log", Args: map[string]any{
			"component_id": trailRepo,
			"limit":        5,
			"reason":       trailReason,
		}}}}, nil
	case toolResultCount(req) == 1:
		return Reply{Usage: usage, ToolCalls: []ToolCall{{Name: "git_read", Args: map[string]any{
			"component_id": trailRepo,
			"path":         "README.md",
			"trigger":      "alert",
			"trigger_ref":  "alert/repo-drift",
		}}}}, nil
	default:
		return Reply{Content: trailAnswer, Usage: usage}, nil
	}
}

func TestE2E_AuditTrail(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	h := NewTestHarness(t, WithServiceAccount("reader", readerKey))
	defer h.Stop()
	if err := h.BootstrapAdmin("svc:e2e"); err != nil {
		t.Fatal(err)
	}
	repoURL := newGitRepo(t, filepath.Join(h.TmpDir(), "trail-repo"))
	h.Responder.SetScript(trailScript)
	if err := h.Start(); err != nil {
		t.Fatalf("failed to start harness: %v", err)
	}

	do := func(method, path, key string, body any, want int) []byte {
		t.Helper()
		status, raw, err := h.Do(method, path, key, body)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		if status != want {
			t.Fatalf("%s %s: status %d, want %d: %s", method, path, status, want, raw)
		}
		return raw
	}
	readTrail := func(sessionID, key string) (trailResponse, string) {
		t.Helper()
		raw := do(http.MethodGet, "/api/v1/sessions/"+sessionID+"/audit-trail", key, nil, http.StatusOK)
		var out trailResponse
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("decode trail %s: %v", raw, err)
		}
		return out, string(raw)
	}

	// The managed system: a git repository, registered and armed over the
	// admin API with the explicit no-credential kind.
	do(http.MethodPost, "/api/v1/components", e2eServiceAccountKey,
		map[string]any{"id": trailRepo, "type": "git", "name": trailRepo, "config": map[string]any{"url": repoURL}},
		http.StatusCreated)
	do(http.MethodPost, "/api/v1/components/"+trailRepo+"/promote", e2eServiceAccountKey,
		map[string]any{"credential_provider": "none"}, http.StatusOK)

	// --- The session whose trail is read ---

	resp, err := h.RunTask(trailPrompt)
	if err != nil {
		t.Fatalf("POST /api/v1/tasks: %v", err)
	}
	if resp.Status != "completed" || resp.FinalAnswer != trailAnswer {
		t.Fatalf("status %q (error %q), answer %q; want a completed turn ending on the scripted answer", resp.Status, resp.Error, resp.FinalAnswer)
	}
	if resp.SessionID == "" || resp.TaskID == "" {
		t.Fatalf("task response carries session_id %q, task_id %q; both are what the trail is asked for and joined by", resp.SessionID, resp.TaskID)
	}
	for _, step := range resp.Steps {
		for _, tr := range step.ToolResults {
			if tr.Error != "" {
				t.Fatalf("%s failed: %s — the acts must actually reach the repository", tr.Name, tr.Error)
			}
		}
	}

	trail, firstRead := readTrail(resp.SessionID, e2eServiceAccountKey)
	if trail.SessionID != resp.SessionID || trail.Count != 2 || len(trail.Entries) != 2 {
		t.Fatalf("trail for %s: session %q, count %d, %d entries; want the two acts the turn took: %s",
			resp.SessionID, trail.SessionID, trail.Count, len(trail.Entries), firstRead)
	}

	log, read := trail.Entries[0], trail.Entries[1]
	if log.ID >= read.ID {
		t.Errorf("entries out of write order: ids %d then %d", log.ID, read.ID)
	}
	for i, e := range trail.Entries {
		if e.SessionID != resp.SessionID {
			t.Errorf("entry %d session_id = %q, want the id the task response returned (%q)", i, e.SessionID, resp.SessionID)
		}
		if e.TaskID != resp.TaskID {
			t.Errorf("entry %d task_id = %q, want %q", i, e.TaskID, resp.TaskID)
		}
		if e.ComponentID != trailRepo || e.Principal != "svc:e2e" || e.Decision != "allow" {
			t.Errorf("entry %d = component %q, principal %q, decision %q", i, e.ComponentID, e.Principal, e.Decision)
		}
		if e.ToolCallID == "" {
			t.Errorf("entry %d names no tool call", i)
		}
	}

	// Act one: git_log. The reason is the model's, byte for byte; it declared
	// no trigger, so the entry records the operator request that opened the
	// session, referenced by the task id the response returned.
	if log.Tool != "git_log" || log.Operation.Verb != "list" || log.Operation.Resource != "commits" {
		t.Errorf("first entry = tool %q, operation %+v; want git_log listing commits", log.Tool, log.Operation)
	}
	if log.Reasoning == nil || *log.Reasoning != trailReason {
		t.Errorf("first entry reasoning = %v, want the model's text verbatim %q", log.Reasoning, trailReason)
	}
	if log.Trigger == nil || log.Trigger.Type != "operator_request" || log.Trigger.Ref != "task/"+resp.TaskID || log.Trigger.Source != "session" {
		t.Errorf("first entry trigger = %+v; want operator_request, task/%s, from the session", log.Trigger, resp.TaskID)
	}

	// Act two: git_read. The model gave no reason: the call was made, and the
	// entry records an empty one. It declared its trigger, recorded as stated.
	if read.Tool != "git_read" || read.Operation.Verb != "get" || read.Operation.Resource != "files" || read.Operation.Name != "README.md" {
		t.Errorf("second entry = tool %q, operation %+v; want git_read getting README.md", read.Tool, read.Operation)
	}
	if read.Reasoning == nil || *read.Reasoning != "" {
		t.Errorf("second entry reasoning = %v; want present and empty — joe writes no reason the model did not give", read.Reasoning)
	}
	if read.Trigger == nil || read.Trigger.Type != "alert" || read.Trigger.Ref != "alert/repo-drift" || read.Trigger.Source != "model" {
		t.Errorf("second entry trigger = %+v; want the alert the model declared", read.Trigger)
	}

	// The act arguments are joe's plumbing: the tools ran on their own
	// arguments, which is why both calls succeeded above.

	// --- Who may read it, and what the read leaves behind ---

	// A session is readable by any authenticated principal, and so is its
	// trail; an unauthenticated request is not.
	if _, raw := readTrail(resp.SessionID, readerKey); raw != firstRead {
		t.Errorf("another authenticated principal read a different trail:\n owner %s\nreader %s", firstRead, raw)
	}
	do(http.MethodGet, "/api/v1/sessions/"+resp.SessionID+"/audit-trail", "not-a-key", nil, http.StatusUnauthorized)
	do(http.MethodGet, "/api/v1/sessions/no-such-session/audit-trail", e2eServiceAccountKey, nil, http.StatusNotFound)
	// The route is a read: no other verb is routed to it.
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		do(method, "/api/v1/sessions/"+resp.SessionID+"/audit-trail", e2eServiceAccountKey, nil, http.StatusMethodNotAllowed)
	}

	// --- The tamper probe: a second, separate session ---

	tamper, err := h.RunTask(tamperPrompt)
	if err != nil {
		t.Fatalf("tamper POST /api/v1/tasks: %v", err)
	}
	if tamper.SessionID == "" || tamper.SessionID == resp.SessionID {
		t.Fatalf("tamper session id = %q; want a fresh session distinct from %q", tamper.SessionID, resp.SessionID)
	}
	if tamper.Status != "completed" || tamper.FinalAnswer != tamperAnswer {
		t.Fatalf("tamper turn: status %q, answer %q", tamper.Status, tamper.FinalAnswer)
	}

	// The earlier session's trail outlived the later session and reads exactly
	// as it did: every entry present, in place, with the same content.
	if _, secondRead := readTrail(resp.SessionID, e2eServiceAccountKey); secondRead != firstRead {
		t.Errorf("the first session's trail changed after the tamper session:\nbefore %s\n after %s", firstRead, secondRead)
	}
	// The tamper session took no act against a managed system, so its own
	// trail is empty — an answer, not an absence.
	if own, raw := readTrail(tamper.SessionID, e2eServiceAccountKey); own.Count != 0 || len(own.Entries) != 0 {
		t.Errorf("tamper session trail = %s; want no entries", raw)
	}

	if rej := h.Responder.Rejections(); len(rej) != 0 {
		t.Errorf("the responder rejected %d request(s): %+v", len(rej), rej[0].Rejected)
	}
	t.Logf("✓ session %s: %d audit entries read back in write order; unchanged after tamper session %s",
		resp.SessionID, trail.Count, tamper.SessionID)
}
