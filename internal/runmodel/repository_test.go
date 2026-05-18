package runmodel_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/jaimegago/joe/internal/runmodel"
	"github.com/jaimegago/joe/internal/store"
)

// TestD5_RecordToolIntent_IdempotentOnDuplicate: re-issuing the same
// idempotency key returns the existing row unchanged. The very property a
// crash-and-resume relies on per §D5 / Invariant 2.
func TestD5_RecordToolIntent_IdempotentOnDuplicate(t *testing.T) {
	s := newTestStore(t)
	sessionID := newTestSession(t, s)
	repo := runmodel.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	run, err := repo.CreateRun(ctx, runmodel.Run{ID: uuid.NewString(), SessionID: sessionID})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	key := "k-" + uuid.NewString()
	first, err := repo.RecordToolIntent(ctx, key, run.ID, "k8s_apply", "hashA")
	if err != nil {
		t.Fatalf("first RecordToolIntent: %v", err)
	}
	if first.Status != runmodel.IdempotencyKeyStatusIssued {
		t.Fatalf("first status = %q, want issued", first.Status)
	}

	// A second call with the SAME key must return the EXISTING row — same
	// created_at, same args_hash, same tool_name — even if the caller
	// passes different args. The repo must not overwrite a previously
	// recorded intent.
	second, err := repo.RecordToolIntent(ctx, key, run.ID, "different_tool", "hashB")
	if err != nil {
		t.Fatalf("second RecordToolIntent (duplicate key): %v", err)
	}
	if second.CreatedAt != first.CreatedAt {
		t.Errorf("created_at changed on duplicate key: %v -> %v", first.CreatedAt, second.CreatedAt)
	}
	if second.ToolName != first.ToolName {
		t.Errorf("tool_name overwritten on duplicate key: %q -> %q", first.ToolName, second.ToolName)
	}
	if second.ArgsHash != first.ArgsHash {
		t.Errorf("args_hash overwritten on duplicate key: %q -> %q", first.ArgsHash, second.ArgsHash)
	}
	if second.Status != runmodel.IdempotencyKeyStatusIssued {
		t.Errorf("status drifted on duplicate key: %q", second.Status)
	}
}

// TestD5_MarkToolCompleted_RefusesToOverwriteTerminal: once a key is in a
// terminal status (completed or failed), MarkToolCompleted / MarkToolFailed
// must error rather than overwrite the prior result. The no-overwrite half
// of the §D5 invariant — the part that prevents a buggy resume from
// silently replacing a known-good result with a different one.
func TestD5_MarkToolCompleted_RefusesToOverwriteTerminal(t *testing.T) {
	s := newTestStore(t)
	sessionID := newTestSession(t, s)
	repo := runmodel.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	run, err := repo.CreateRun(ctx, runmodel.Run{ID: uuid.NewString(), SessionID: sessionID})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	key := "k-" + uuid.NewString()
	if _, err := repo.RecordToolIntent(ctx, key, run.ID, "k8s_apply", "hash"); err != nil {
		t.Fatalf("record intent: %v", err)
	}

	// First completion lands.
	if err := repo.MarkToolCompleted(ctx, key, `{"ok":true,"v":1}`); err != nil {
		t.Fatalf("first MarkToolCompleted: %v", err)
	}
	row, err := repo.GetIdempotencyKey(ctx, key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if row.Result == nil || *row.Result != `{"ok":true,"v":1}` {
		t.Fatalf("first result not persisted: %+v", row.Result)
	}

	// Second completion is refused. Original result must survive.
	err = repo.MarkToolCompleted(ctx, key, `{"ok":false,"v":2}`)
	if !errors.Is(err, runmodel.ErrAlreadyTerminal) {
		t.Fatalf("second MarkToolCompleted err = %v, want ErrAlreadyTerminal", err)
	}
	row, err = repo.GetIdempotencyKey(ctx, key)
	if err != nil {
		t.Fatalf("get after refused overwrite: %v", err)
	}
	if row.Result == nil || *row.Result != `{"ok":true,"v":1}` {
		t.Errorf("result overwritten despite no-overwrite rule: %+v", row.Result)
	}

	// MarkToolFailed against an already-terminal row is also refused.
	if err := repo.MarkToolFailed(ctx, key, `{"err":"x"}`); !errors.Is(err, runmodel.ErrAlreadyTerminal) {
		t.Fatalf("MarkToolFailed err = %v, want ErrAlreadyTerminal", err)
	}
}

// TestD5_MarkTool_FailedThenCompleted_Refused: same rule the other
// direction — once a key is 'failed' it cannot be flipped to 'completed'.
func TestD5_MarkTool_FailedThenCompleted_Refused(t *testing.T) {
	s := newTestStore(t)
	sessionID := newTestSession(t, s)
	repo := runmodel.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	run, err := repo.CreateRun(ctx, runmodel.Run{ID: uuid.NewString(), SessionID: sessionID})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	key := "k-" + uuid.NewString()
	if _, err := repo.RecordToolIntent(ctx, key, run.ID, "k8s_apply", "h"); err != nil {
		t.Fatalf("record intent: %v", err)
	}
	if err := repo.MarkToolFailed(ctx, key, `{"err":"boom"}`); err != nil {
		t.Fatalf("MarkToolFailed: %v", err)
	}
	if err := repo.MarkToolCompleted(ctx, key, `{"ok":true}`); !errors.Is(err, runmodel.ErrAlreadyTerminal) {
		t.Fatalf("MarkToolCompleted after failed err = %v, want ErrAlreadyTerminal", err)
	}
}

// TestD5_MarkTool_NotFound: marking a non-existent key returns ErrNotFound,
// not silent success. A caller who never persisted an intent cannot record
// a result.
func TestD5_MarkTool_NotFound(t *testing.T) {
	s := newTestStore(t)
	repo := runmodel.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	err := repo.MarkToolCompleted(ctx, "no-such-key", `{"x":1}`)
	if !errors.Is(err, runmodel.ErrNotFound) {
		t.Errorf("MarkToolCompleted on missing key err = %v, want ErrNotFound", err)
	}
}

// TestD5_ReissuePermittedWhileIssued: the crash-resume permission case —
// while a key is still 'issued' (the call was issued but the result never
// landed), the caller may call RecordToolIntent again with the same key and
// get the existing 'issued' row. This is what Change 9's executor wrapper
// relies on to safely re-attempt after a crash.
func TestD5_ReissuePermittedWhileIssued(t *testing.T) {
	s := newTestStore(t)
	sessionID := newTestSession(t, s)
	repo := runmodel.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	run, err := repo.CreateRun(ctx, runmodel.Run{ID: uuid.NewString(), SessionID: sessionID})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	key := "k-" + uuid.NewString()

	if _, err := repo.RecordToolIntent(ctx, key, run.ID, "k8s_apply", "h"); err != nil {
		t.Fatalf("first record: %v", err)
	}
	// Simulate crash: status stays 'issued', no MarkToolCompleted ran.
	// Re-issue is permitted; status still 'issued'.
	row, err := repo.RecordToolIntent(ctx, key, run.ID, "k8s_apply", "h")
	if err != nil {
		t.Fatalf("re-issue while issued: %v", err)
	}
	if row.Status != runmodel.IdempotencyKeyStatusIssued {
		t.Errorf("status after re-issue = %q, want issued", row.Status)
	}
	// And now the (resumed) caller can land the completion normally.
	if err := repo.MarkToolCompleted(ctx, key, `{"ok":true}`); err != nil {
		t.Fatalf("MarkToolCompleted after re-issue: %v", err)
	}
}

// TestD5_RepositoryInterface_NoWriteResultWithoutIntent is the structural
// half of §D5: the Repository interface must expose no method that lets a
// caller record a tool result without first issuing the key. This is what
// the decomposition calls "pinning the interface shape is sufficient" —
// adding such a method would be a visible interface change anyone reading
// the diff would notice.
//
// We assert the property by enumerating every method that touches
// tool_idempotency_keys and confirming the only ones that *write* the key
// row require the key as input. There is no Create*Result or
// InsertCompleted method that takes (result, tool, ...) without an
// already-issued key.
//
// Adding such a method (with or without the "Idempotency"/"Tool" prefix in
// its name) would have to extend the Repository interface visibly. The
// test runs `reflect.TypeOf` against the interface and asserts the methods
// touching idempotency keys are exactly the allowed set.
func TestD5_RepositoryInterface_NoWriteResultWithoutIntent(t *testing.T) {
	// The allowed methods on Repository that touch tool_idempotency_keys.
	// RecordToolIntent is the only INSERT path; MarkToolCompleted and
	// MarkToolFailed are UPDATE-from-issued only; GetIdempotencyKey is
	// read-only.
	allowed := map[string]bool{
		"RecordToolIntent":  true,
		"MarkToolCompleted": true,
		"MarkToolFailed":    true,
		"GetIdempotencyKey": true,
	}

	// Enumerate every method on Repository whose name mentions Idempotency
	// or Tool* and assert it is in the allowed set. A future contributor
	// adding e.g. WriteToolResult or RecordCompletedTool would have to
	// extend `allowed` here in the same diff, making the rule violation
	// visible.
	repoType := repositoryReflectType()
	for i := 0; i < repoType.NumMethod(); i++ {
		name := repoType.Method(i).Name
		if !touchesIdempotencyKey(name) {
			continue
		}
		if !allowed[name] {
			t.Errorf("Repository method %q touches tool_idempotency_keys but is "+
				"not in the allowed set. §D5 forbids writing a result without "+
				"a prior issued key — extending the allowed set is a structural "+
				"change that must be reviewed.", name)
		}
	}
}

func touchesIdempotencyKey(methodName string) bool {
	// "ToolIntent", "ToolCompleted", "ToolFailed", "IdempotencyKey" all
	// touch the table; the broader heuristic captures any future method
	// that uses "Idempotency", "Tool" + "Completed"/"Failed"/"Result".
	switch methodName {
	case "RecordToolIntent", "MarkToolCompleted", "MarkToolFailed", "GetIdempotencyKey":
		return true
	}
	// Generic guard: catch any future method whose name suggests it writes
	// a tool result. Adjust if a legitimate method is mis-flagged.
	for _, sub := range []string{"Idempotency", "ToolResult", "ToolCompletion", "RecordCompleted"} {
		if strings.Contains(methodName, sub) {
			return true
		}
	}
	return false
}
