package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
)

// auditTrailFixture is a server over a migrated store with the audit log's
// write and read surfaces wired the way cmd/joe wires them.
type auditTrailFixture struct {
	mux      *http.ServeMux
	store    *store.Store
	sessions sessionmodel.Repository
	audit    audit.Repository
}

func newAuditTrailFixture(t *testing.T, withReader bool) *auditTrailFixture {
	t.Helper()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	f := &auditTrailFixture{
		store:    s,
		sessions: sessionmodel.NewRepository(s.DB(), store.DriverSQLite),
		audit:    audit.NewRepository(s.DB(), store.DriverSQLite),
	}
	svc := &core.Services{Store: s, SessionModel: f.sessions, Audit: f.audit}
	if withReader {
		svc.AuditTrail = audit.NewTrailReader(s.DB(), store.DriverSQLite)
	}
	srv := api.New(svc, rbac.NewPolicyEngine(rbac.NewRepository(s.DB(), store.DriverSQLite)))
	f.mux = http.NewServeMux()
	srv.RegisterRoutes(f.mux)
	return f
}

func (f *auditTrailFixture) session(t *testing.T, id string) {
	t.Helper()
	if _, err := f.sessions.CreateSession(context.Background(), sessionmodel.AgentSession{
		ID: id, Type: sessionmodel.SessionTypeDefault, CreatorPrincipal: "svc:e2e",
	}); err != nil {
		t.Fatalf("create session %s: %v", id, err)
	}
}

// act records one act for sessionID the way the accessor does.
func (f *auditTrailFixture) act(t *testing.T, sessionID string, op audit.Operation, call audit.Call) {
	t.Helper()
	ctx := audit.WithScope(context.Background(), audit.Scope{
		SessionID: sessionID,
		TaskID:    "task-1",
		Trigger:   audit.Trigger{Type: audit.TriggerOperatorRequest, Ref: "task/task-1"},
	})
	a, sid := audit.ActFor(audit.WithCall(ctx, call), op)
	if err := f.audit.Insert(context.Background(), audit.Event{
		Principal: "svc:e2e", Action: "read", Zone: "unassigned", ComponentID: "k8s-prod",
		Decision: audit.DecisionAllow, Reason: "team_flat_read", Kind: audit.KindInfraAccess,
		SessionID: sid, Context: a.Encode(),
	}); err != nil {
		t.Fatalf("insert act: %v", err)
	}
}

func (f *auditTrailFixture) get(t *testing.T, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

func (f *auditTrailFixture) rows(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.store.DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM audit_log`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// trailBody is the wire shape of GET /sessions/{id}/audit-trail, decoded
// field by field so the test pins the names a consumer reads.
type trailBody struct {
	SessionID string `json:"session_id"`
	Count     int    `json:"count"`
	Entries   []struct {
		ID          int64  `json:"id"`
		RecordedAt  string `json:"recorded_at"`
		Principal   string `json:"principal"`
		ComponentID string `json:"component_id"`
		Decision    string `json:"decision"`
		SessionID   string `json:"session_id"`
		TaskID      string `json:"task_id"`
		Tool        string `json:"tool"`
		ToolCallID  string `json:"tool_call_id"`
		Operation   struct {
			Verb        string `json:"verb"`
			APIGroup    string `json:"api_group"`
			Resource    string `json:"resource"`
			Subresource string `json:"subresource"`
			Name        string `json:"name"`
			Namespace   string `json:"namespace"`
		} `json:"operation"`
		Reasoning *string `json:"reasoning"`
		Trigger   *struct {
			Type   string `json:"type"`
			Ref    string `json:"ref"`
			Source string `json:"source"`
		} `json:"trigger"`
	} `json:"entries"`
}

// TestSessionAuditTrail_ReturnsTheSessionsActsInWriteOrder drives the read
// route: a session's entries come back in the order they were written, each
// with its operation and target, the model's reason and the trigger, and the
// read leaves the audit log exactly as it found it.
func TestSessionAuditTrail_ReturnsTheSessionsActsInWriteOrder(t *testing.T) {
	f := newAuditTrailFixture(t, true)
	f.session(t, "sess-a")
	f.session(t, "sess-b")

	f.act(t, "sess-a", audit.Operation{Verb: "list", Resource: "pods", Namespace: "shop"},
		audit.Call{Tool: "k8s_get", CallID: "c1", Reason: "See what is running in shop."})
	f.act(t, "sess-b", audit.Operation{Verb: "get", Resource: "nodes", Name: "n1"},
		audit.Call{Tool: "k8s_get", CallID: "x1", Reason: "another session"})
	f.act(t, "sess-a", audit.Operation{Verb: "get", Resource: "pods", Subresource: "log", Name: "checkout-0", Namespace: "shop"},
		audit.Call{Tool: "k8s_logs", CallID: "c2", TriggerType: "alert", TriggerRef: "alert/high-latency"})

	before := f.rows(t)
	w := f.get(t, http.MethodGet, "/api/v1/sessions/sess-a/audit-trail")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body)
	}
	if after := f.rows(t); after != before {
		t.Errorf("the read wrote to audit_log: %d rows before, %d after", before, after)
	}

	var body trailBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", w.Body, err)
	}
	if body.SessionID != "sess-a" || body.Count != 2 || len(body.Entries) != 2 {
		t.Fatalf("session %q, count %d, %d entries; want sess-a, 2, 2: %s", body.SessionID, body.Count, len(body.Entries), w.Body)
	}

	first, second := body.Entries[0], body.Entries[1]
	if first.ID >= second.ID {
		t.Errorf("entries out of write order: ids %d then %d", first.ID, second.ID)
	}
	if first.Operation.Verb != "list" || first.Operation.Resource != "pods" || first.Operation.Namespace != "shop" {
		t.Errorf("first operation = %+v", first.Operation)
	}
	if first.Reasoning == nil || *first.Reasoning != "See what is running in shop." {
		t.Errorf("first reasoning = %v", first.Reasoning)
	}
	if first.Trigger == nil || first.Trigger.Type != "operator_request" || first.Trigger.Ref != "task/task-1" || first.Trigger.Source != "session" {
		t.Errorf("first trigger = %+v", first.Trigger)
	}
	if first.Tool != "k8s_get" || first.ToolCallID != "c1" || first.SessionID != "sess-a" || first.TaskID != "task-1" {
		t.Errorf("first entry = %+v", first)
	}
	if first.Decision != "allow" || first.ComponentID != "k8s-prod" || first.Principal != "svc:e2e" || first.RecordedAt == "" {
		t.Errorf("first entry row columns = %+v", first)
	}

	if second.Operation.Verb != "get" || second.Operation.Subresource != "log" || second.Operation.Name != "checkout-0" {
		t.Errorf("second operation = %+v", second.Operation)
	}
	// The model stated no reason: the field is present and empty, not absent.
	if second.Reasoning == nil || *second.Reasoning != "" {
		t.Errorf("second reasoning = %v; want a present, empty string", second.Reasoning)
	}
	if second.Trigger == nil || second.Trigger.Type != "alert" || second.Trigger.Ref != "alert/high-latency" || second.Trigger.Source != "model" {
		t.Errorf("second trigger = %+v", second.Trigger)
	}
}

// TestSessionAuditTrail_ThreeAnswersStayApart: an existing session with no
// acts is an empty trail; a session Joe does not hold is not found; a server
// with no reader wired is unavailable. None of the three reads as another.
func TestSessionAuditTrail_ThreeAnswersStayApart(t *testing.T) {
	f := newAuditTrailFixture(t, true)
	f.session(t, "quiet")

	w := f.get(t, http.MethodGet, "/api/v1/sessions/quiet/audit-trail")
	if w.Code != http.StatusOK {
		t.Fatalf("existing session with no acts: status = %d, body %s", w.Code, w.Body)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(raw["entries"]) != "[]" {
		t.Errorf("entries = %s; an empty trail serialises as [] and never as null", raw["entries"])
	}

	if w := f.get(t, http.MethodGet, "/api/v1/sessions/no-such-session/audit-trail"); w.Code != http.StatusNotFound {
		t.Errorf("unknown session: status = %d, want 404 (not an empty trail)", w.Code)
	}

	bare := newAuditTrailFixture(t, false)
	bare.session(t, "quiet")
	if w := bare.get(t, http.MethodGet, "/api/v1/sessions/quiet/audit-trail"); w.Code != http.StatusServiceUnavailable {
		t.Errorf("no reader wired: status = %d, want 503", w.Code)
	}
}

// TestSessionAuditTrail_IsReadOnly: the route answers GET and nothing else.
// There is no verb on it — or anywhere on the API — that alters or removes an
// entry, and after every refused attempt the trail reads as it did.
func TestSessionAuditTrail_IsReadOnly(t *testing.T) {
	f := newAuditTrailFixture(t, true)
	f.session(t, "sess-a")
	f.act(t, "sess-a", audit.Operation{Verb: "list", Resource: "pods"}, audit.Call{Tool: "k8s_get", Reason: "look"})

	want := f.get(t, http.MethodGet, "/api/v1/sessions/sess-a/audit-trail").Body.String()
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		w := f.get(t, method, "/api/v1/sessions/sess-a/audit-trail")
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s on the audit trail: status = %d, want 405", method, w.Code)
		}
	}
	if got := f.get(t, http.MethodGet, "/api/v1/sessions/sess-a/audit-trail").Body.String(); got != want {
		t.Errorf("the trail changed across the refused requests:\nbefore %s\n after %s", want, got)
	}
}
