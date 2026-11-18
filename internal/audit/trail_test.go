package audit_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/store"
)

// insertAct writes one act row for sessionID the way the accessor does.
func insertAct(t *testing.T, repo audit.Repository, sessionID, component string, decision audit.Decision, op audit.Operation, reason string) {
	t.Helper()
	ctx := audit.WithScope(context.Background(), audit.Scope{
		SessionID: sessionID,
		TaskID:    "task-" + sessionID,
		Trigger:   audit.Trigger{Type: audit.TriggerOperatorRequest, Ref: "task/task-" + sessionID},
	})
	ctx = audit.WithCall(ctx, audit.Call{Tool: "k8s_get", CallID: "call-" + op.Name, Reason: reason})
	act, sid := audit.ActFor(ctx, op)
	if err := repo.Insert(context.Background(), audit.Event{
		Principal:   "svc:e2e",
		Action:      "read",
		Zone:        "unassigned",
		ComponentID: component,
		Decision:    decision,
		Reason:      "policy_allow",
		Kind:        audit.KindInfraAccess,
		SessionID:   sid,
		Context:     act.Encode(),
	}); err != nil {
		t.Fatalf("Insert: %v", err)
	}
}

func countRows(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM audit_log`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// TestSessionTrail_ReturnsOneSessionsActsInWriteOrder: the trail of a session
// is its act rows and only its act rows, in the order they were written, each
// carrying what was recorded.
func TestSessionTrail_ReturnsOneSessionsActsInWriteOrder(t *testing.T) {
	s := freshStore(t)
	repo := audit.NewRepository(s.DB(), store.DriverSQLite)
	reader := audit.NewTrailReader(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	list := audit.Operation{Verb: "list", Resource: "pods", Namespace: "default"}
	get := audit.Operation{Verb: "get", APIGroup: "apps", Resource: "deployments", Name: "api", Namespace: "default"}
	logs := audit.Operation{Verb: "get", Resource: "pods", Subresource: "log", Name: "api-0", Namespace: "default"}

	insertAct(t, repo, "s-a", "k8s-prod", audit.DecisionAllow, list, "see what is running")
	insertAct(t, repo, "s-b", "k8s-prod", audit.DecisionAllow, get, "another session's act")
	insertAct(t, repo, "s-a", "k8s-prod", audit.DecisionAllow, get, "")
	// A row with no session — a direct read, a transition — is in no trail.
	if err := repo.Insert(ctx, audit.Event{
		Principal: "svc:e2e", Action: "read", ComponentID: "k8s-prod",
		Decision: audit.DecisionAllow, Reason: "policy_allow", Kind: audit.KindInfraAccess,
	}); err != nil {
		t.Fatalf("Insert sessionless: %v", err)
	}
	insertAct(t, repo, "s-a", "k8s-prod", audit.DecisionDeny, logs, "read the crash output")

	before := countRows(t, s.DB())
	got, err := reader.SessionTrail(ctx, "s-a")
	if err != nil {
		t.Fatalf("SessionTrail: %v", err)
	}
	if after := countRows(t, s.DB()); after != before {
		t.Errorf("reading the trail changed audit_log: %d rows before, %d after", before, after)
	}

	wantOps := []audit.Operation{list, get, logs}
	if len(got) != len(wantOps) {
		t.Fatalf("got %d entries, want %d: %+v", len(got), len(wantOps), got)
	}
	for i, e := range got {
		if e.Operation != wantOps[i] {
			t.Errorf("entry %d operation = %+v, want %+v (entries must be in write order)", i, e.Operation, wantOps[i])
		}
		if e.SessionID != "s-a" {
			t.Errorf("entry %d session_id = %q, want s-a", i, e.SessionID)
		}
		if i > 0 && got[i-1].ID >= e.ID {
			t.Errorf("entry ids not ascending: %d then %d", got[i-1].ID, e.ID)
		}
		if e.ComponentID != "k8s-prod" || e.Principal != "svc:e2e" || e.RecordedAt == "" {
			t.Errorf("entry %d row columns not carried: %+v", i, e)
		}
		if e.Trigger == nil || e.Trigger.Type != audit.TriggerOperatorRequest || e.Trigger.Ref != "task/task-s-a" {
			t.Errorf("entry %d trigger = %+v", i, e.Trigger)
		}
	}
	if got[0].Reasoning != "see what is running" || got[1].Reasoning != "" || got[2].Reasoning != "read the crash output" {
		t.Errorf("reasoning not carried per entry: %q, %q, %q", got[0].Reasoning, got[1].Reasoning, got[2].Reasoning)
	}
	if got[0].Decision != audit.DecisionAllow || got[2].Decision != audit.DecisionDeny {
		t.Errorf("decisions = %q, %q; want allow, deny", got[0].Decision, got[2].Decision)
	}

	// Reading twice returns the same trail: the read left nothing behind.
	again, err := reader.SessionTrail(ctx, "s-a")
	if err != nil {
		t.Fatalf("second SessionTrail: %v", err)
	}
	if !reflect.DeepEqual(got, again) {
		t.Errorf("two reads of one trail differ:\n first %+v\nsecond %+v", got, again)
	}
}

// TestSessionTrail_NoActsIsAnEmptyTrail: a session that recorded nothing, and
// the empty session id, both read as an empty non-nil trail, so the answer
// serialises as `[]` and never as null.
func TestSessionTrail_NoActsIsAnEmptyTrail(t *testing.T) {
	s := freshStore(t)
	reader := audit.NewTrailReader(s.DB(), store.DriverSQLite)
	for _, id := range []string{"never-acted", ""} {
		got, err := reader.SessionTrail(context.Background(), id)
		if err != nil {
			t.Fatalf("SessionTrail(%q): %v", id, err)
		}
		if got == nil || len(got) != 0 {
			t.Errorf("SessionTrail(%q) = %#v; want an empty non-nil slice", id, got)
		}
	}
}

// TestSessionTrail_UndecodableEntryFailsTheRead: a trail is never returned
// short. A row that cannot be read as an act fails the read instead of being
// skipped.
func TestSessionTrail_UndecodableEntryFailsTheRead(t *testing.T) {
	s := freshStore(t)
	repo := audit.NewRepository(s.DB(), store.DriverSQLite)
	reader := audit.NewTrailReader(s.DB(), store.DriverSQLite)

	insertAct(t, repo, "s-a", "k8s-prod", audit.DecisionAllow, audit.Operation{Verb: "list", Resource: "pods"}, "")
	if err := repo.Insert(context.Background(), audit.Event{
		Principal: "svc:e2e", Action: "read", ComponentID: "k8s-prod", Decision: audit.DecisionAllow,
		Kind: audit.KindInfraAccess, SessionID: "s-a", Context: `not json`,
	}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	_, err := reader.SessionTrail(context.Background(), "s-a")
	if !errors.Is(err, audit.ErrTrailReadFailed) {
		t.Fatalf("SessionTrail = %v; want ErrTrailReadFailed for a trail holding an undecodable entry", err)
	}
}

// TestTrailReader_DeclaresOneReadAndNoWrite keeps the read surface a read
// surface: TrailReader has exactly the one method, so a handler holding it has
// no path to a write. The write surface's own guard
// (TestRepositoryAPISurface_AppendOnly) keeps Repository free of reads.
func TestTrailReader_DeclaresOneReadAndNoWrite(t *testing.T) {
	typ := reflect.TypeOf((*audit.TrailReader)(nil)).Elem()
	if typ.NumMethod() != 1 || typ.Method(0).Name != "SessionTrail" {
		names := make([]string, 0, typ.NumMethod())
		for i := 0; i < typ.NumMethod(); i++ {
			names = append(names, typ.Method(i).Name)
		}
		t.Fatalf("audit.TrailReader methods = %v; want exactly [SessionTrail]", names)
	}
}

// TestMigration036_SessionColumn_PreservesAppendOnly: adding the session
// column left the append-only enforcement where it was. After the full chain,
// an act row with a session is refused both UPDATE and DELETE, the new index
// exists beside the old ones, and the row is still there.
func TestMigration036_SessionColumn_PreservesAppendOnly(t *testing.T) {
	s := freshStore(t)
	repo := audit.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	insertAct(t, repo, "s-a", "k8s-prod", audit.DecisionAllow, audit.Operation{Verb: "list", Resource: "pods"}, "why")

	for name, stmt := range map[string]string{
		"UPDATE of the session":   `UPDATE audit_log SET session_id = 'other'`,
		"UPDATE of the reasoning": `UPDATE audit_log SET context = '{}'`,
		"DELETE":                  `DELETE FROM audit_log WHERE session_id = 's-a'`,
	} {
		if _, err := s.DB().ExecContext(ctx, stmt); err == nil {
			t.Errorf("%s was accepted; audit_log must refuse it", name)
		} else if !strings.Contains(strings.ToLower(err.Error()), "append-only") {
			t.Errorf("%s error = %v; expected the append-only abort", name, err)
		}
	}

	wantIndexes := []string{
		"idx_audit_log_created_at",
		"idx_audit_log_kind",
		"idx_audit_log_principal",
		"idx_audit_log_session",
	}
	if got := indexesOnTable(t, s.DB(), "audit_log"); !sliceContainsAll(got, wantIndexes) {
		t.Errorf("indexes on audit_log = %v, want all of %v", got, wantIndexes)
	}

	got, err := audit.NewTrailReader(s.DB(), store.DriverSQLite).SessionTrail(ctx, "s-a")
	if err != nil || len(got) != 1 || got[0].Reasoning != "why" {
		t.Fatalf("after the refused statements the trail = %+v, err %v; want the one entry intact", got, err)
	}
}
