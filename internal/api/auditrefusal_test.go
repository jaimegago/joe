package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/access"
	"github.com/jaimegago/joe/internal/audit"
)

// TestWriteAccessError_AuditRefusalIs503: an act the accessor refused because
// its audit entry could not be written (D-0169) surfaces to an HTTP caller as
// 503 — nothing was read, and the request can be retried once the audit store
// is healthy — and never as a 403, which would say the caller lacked access.
func TestWriteAccessError_AuditRefusalIs503(t *testing.T) {
	// The shape permit returns: its own message wrapping the repository's
	// ErrAuditWriteFailed.
	err := fmt.Errorf("audit entry could not be written, so the act was not taken: %w",
		fmt.Errorf("%w: disk full", audit.ErrAuditWriteFailed))

	w := httptest.NewRecorder()
	if !writeAccessError(w, err) {
		t.Fatal("writeAccessError did not handle an audit-write refusal")
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
	var body apiError
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error != errorCodeServiceUnavailable {
		t.Errorf("error code = %q, want %q", body.Error, errorCodeServiceUnavailable)
	}

	// Control: a permission denial is still a 403, so the 503 above is the
	// audit branch and not a catch-all.
	w = httptest.NewRecorder()
	if !writeAccessError(w, &access.PermissionDeniedError{Component: "c", Action: "read", Reason: "no_grant"}) || w.Code != http.StatusForbidden {
		t.Errorf("permission denial: status = %d, want 403", w.Code)
	}
	// And an unrelated error is left to the caller.
	if writeAccessError(httptest.NewRecorder(), errors.New("adapter exploded")) {
		t.Error("writeAccessError claimed an unrelated error")
	}
}

// TestWithTaskAuditScope_NamesTheSessionAndTheRequest: the scope a task turn
// runs under carries the session id and task id the response returns, and an
// operator-request trigger referenced by that task id (D-0171).
func TestWithTaskAuditScope_NamesTheSessionAndTheRequest(t *testing.T) {
	scope, ok := audit.ScopeFrom(withTaskAuditScope(context.Background(), "sess-1", "task-1"))
	if !ok {
		t.Fatal("no audit scope stamped")
	}
	want := audit.Scope{
		SessionID: "sess-1",
		TaskID:    "task-1",
		Trigger:   audit.Trigger{Type: audit.TriggerOperatorRequest, Ref: "task/task-1"},
	}
	if scope != want {
		t.Errorf("scope = %+v, want %+v", scope, want)
	}
}
