package sessionarchive_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/sessionarchive"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
)

type fixture struct {
	t        *testing.T
	db       *sql.DB
	repo     *sessionmodel.SQLRepository
	archiver *sessionarchive.Archiver
	dir      string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	repo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	dir := t.TempDir()
	return &fixture{
		t:        t,
		db:       s.DB(),
		repo:     repo,
		archiver: sessionarchive.New(sessionarchive.NewFilesystemProvider(dir), repo),
		dir:      dir,
	}
}

// commit is a real same-tx effect+audit wrapper over the fixture DB, the test
// analogue of (*api.Server).mutateWithAudit / the sweeper's mutateWithAudit.
func (f *fixture) commit(ev audit.Event, failAudit bool) sessionarchive.CommitFn {
	auditRepo := audit.NewRepository(f.db, store.DriverSQLite)
	return func(mutate func(*sql.Tx) error) (err error) {
		tx, err := f.db.BeginTx(context.Background(), nil)
		if err != nil {
			return err
		}
		defer func() {
			if err != nil {
				_ = tx.Rollback()
			}
		}()
		if err = mutate(tx); err != nil {
			return err
		}
		if failAudit {
			return errors.New("forced audit failure")
		}
		if err = auditRepo.InsertTx(context.Background(), tx, ev); err != nil {
			return err
		}
		return tx.Commit()
	}
}

func (f *fixture) mkSession(id, creator string) {
	f.t.Helper()
	if _, err := f.repo.CreateSession(context.Background(), sessionmodel.AgentSession{
		ID: id, Type: sessionmodel.SessionTypeDefault, CreatorPrincipal: creator,
		Title: strptr("My session"),
	}); err != nil {
		f.t.Fatalf("CreateSession: %v", err)
	}
}

func (f *fixture) addMsg(id, sessionID, role, content string) {
	f.t.Helper()
	if _, err := f.repo.AddChatMessage(context.Background(), sessionmodel.ChatMessage{
		ID: id, SessionID: sessionID, Role: role, Content: content,
	}); err != nil {
		f.t.Fatalf("AddChatMessage: %v", err)
	}
}

func strptr(s string) *string { return &s }

// TestRoundTrip proves the §12.6 archive→restore round-trip: archiving produces
// an artifact and sets the archive columns; the hot transcript is gone; restoring
// from the artifact reconstitutes the session to active with its transcript
// rebuilt in correct order with correct roles — the restored transcript equals
// the original.
func TestRoundTrip(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mkSession("s1", "user:alice")
	f.addMsg("m1", "s1", "user", "first")
	f.addMsg("m2", "s1", "assistant", "second")
	f.addMsg("m3", "s1", "user", "third")
	original, _ := f.repo.ListChatMessages(ctx, "s1")

	// Archive.
	sess, _ := f.repo.GetSession(ctx, "s1")
	ref, err := f.archiver.Archive(ctx, *sess, "user:admin",
		f.commit(audit.Event{Action: audit.ActionSessionArchive, Principal: "user:admin", Kind: audit.KindAdminAccess, Decision: audit.DecisionAllow}, false))
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if !strings.HasPrefix(ref, "fs:") {
		t.Errorf("archive_ref %q lacks the fs: scheme", ref)
	}
	// Artifact exists on disk.
	if _, err := os.Stat(filepath.Join(f.dir, "s1.json")); err != nil {
		t.Errorf("artifact file missing: %v", err)
	}
	// Columns set; hot transcript gone.
	sess, _ = f.repo.GetSession(ctx, "s1")
	if sess.ArchivedAt == nil || sess.ArchiveRef == nil || *sess.ArchiveRef != ref {
		t.Fatalf("archive columns wrong: %+v", sess)
	}
	if msgs, _ := f.repo.ListChatMessages(ctx, "s1"); len(msgs) != 0 {
		t.Errorf("hot transcript survived archive: %d rows", len(msgs))
	}

	// Restore.
	if err := f.archiver.Restore(ctx, *sess,
		f.commit(audit.Event{Action: audit.ActionSessionUnarchive, Principal: "user:admin", Kind: audit.KindAdminAccess, Decision: audit.DecisionAllow}, false)); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	sess, _ = f.repo.GetSession(ctx, "s1")
	if sess.ArchivedAt != nil || sess.ArchiveRef != nil {
		t.Errorf("archive columns not cleared on restore: %+v", sess)
	}
	restored, _ := f.repo.ListChatMessages(ctx, "s1")
	if len(restored) != len(original) {
		t.Fatalf("restored %d messages, want %d", len(restored), len(original))
	}
	for i := range original {
		if restored[i].ID != original[i].ID || restored[i].Seq != original[i].Seq ||
			restored[i].Role != original[i].Role || restored[i].Content != original[i].Content {
			t.Errorf("restored[%d] = %+v, want %+v", i, restored[i], original[i])
		}
	}
}

// TestRestoreRefusesUnknownVersion proves §12.6: restore REFUSES an artifact whose
// schema version it does not recognize rather than silently accepting it. The
// stored artifact's version is tampered to a future value; the restore must fail
// with ErrUnsupportedArtifactVersion and leave the session archived (unchanged).
func TestRestoreRefusesUnknownVersion(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mkSession("s1", "user:alice")
	f.addMsg("m1", "s1", "user", "hi")

	sess, _ := f.repo.GetSession(ctx, "s1")
	if _, err := f.archiver.Archive(ctx, *sess, "user:admin",
		f.commit(audit.Event{Action: audit.ActionSessionArchive, Principal: "user:admin", Kind: audit.KindAdminAccess, Decision: audit.DecisionAllow}, false)); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	// Tamper the on-disk artifact: bump schema_version to an unrecognized value.
	path := filepath.Join(f.dir, "s1.json")
	raw, _ := os.ReadFile(path)
	var blob map[string]any
	if err := json.Unmarshal(raw, &blob); err != nil {
		t.Fatalf("unmarshal artifact: %v", err)
	}
	blob["schema_version"] = sessionarchive.CurrentSchemaVersion + 99
	bumped, _ := json.Marshal(blob)
	if err := os.WriteFile(path, bumped, 0o600); err != nil {
		t.Fatalf("rewrite artifact: %v", err)
	}

	sess, _ = f.repo.GetSession(ctx, "s1")
	err := f.archiver.Restore(ctx, *sess,
		f.commit(audit.Event{Action: audit.ActionSessionUnarchive, Principal: "user:admin", Kind: audit.KindAdminAccess, Decision: audit.DecisionAllow}, false))
	if !errors.Is(err, sessionarchive.ErrUnsupportedArtifactVersion) {
		t.Fatalf("Restore err = %v, want ErrUnsupportedArtifactVersion (must refuse, never silently accept)", err)
	}
	// The refusal left the session archived and its hot transcript empty.
	sess, _ = f.repo.GetSession(ctx, "s1")
	if sess.ArchivedAt == nil {
		t.Error("session un-archived despite a refused restore")
	}
}

// TestArchiveAuditFailureRollsBack proves the same-tx effect↔audit coupling on
// archive: a forced audit failure rolls the archive state transition back (the
// session stays active, the hot transcript survives) AND the orphaned artifact is
// removed — no archive_ref/state without the other.
func TestArchiveAuditFailureRollsBack(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mkSession("s1", "user:alice")
	f.addMsg("m1", "s1", "user", "hi")

	sess, _ := f.repo.GetSession(ctx, "s1")
	_, err := f.archiver.Archive(ctx, *sess, "user:admin",
		f.commit(audit.Event{Action: audit.ActionSessionArchive, Principal: "user:admin", Kind: audit.KindAdminAccess, Decision: audit.DecisionAllow}, true /* failAudit */))
	if err == nil {
		t.Fatal("Archive succeeded despite forced audit failure")
	}
	// State rolled back: still active, transcript intact.
	sess, _ = f.repo.GetSession(ctx, "s1")
	if sess.ArchivedAt != nil {
		t.Error("archive state persisted despite audit failure — not same-tx")
	}
	if msgs, _ := f.repo.ListChatMessages(ctx, "s1"); len(msgs) != 1 {
		t.Errorf("transcript lost despite rollback: %d rows, want 1", len(msgs))
	}
	// Orphan cleanup: the artifact file was removed on the failed commit.
	if _, statErr := os.Stat(filepath.Join(f.dir, "s1.json")); !os.IsNotExist(statErr) {
		t.Errorf("orphaned artifact left behind after rollback: %v", statErr)
	}
}

// TestRestoreAuditFailureRollsBack proves the same-tx coupling on restore: a
// forced audit failure rolls back BOTH the column clear and the transcript
// rebuild, so the session stays archived and no half-rebuilt transcript leaks.
func TestRestoreAuditFailureRollsBack(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mkSession("s1", "user:alice")
	f.addMsg("m1", "s1", "user", "hi")

	sess, _ := f.repo.GetSession(ctx, "s1")
	if _, err := f.archiver.Archive(ctx, *sess, "user:admin",
		f.commit(audit.Event{Action: audit.ActionSessionArchive, Principal: "user:admin", Kind: audit.KindAdminAccess, Decision: audit.DecisionAllow}, false)); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	sess, _ = f.repo.GetSession(ctx, "s1")
	err := f.archiver.Restore(ctx, *sess,
		f.commit(audit.Event{Action: audit.ActionSessionUnarchive, Principal: "user:admin", Kind: audit.KindAdminAccess, Decision: audit.DecisionAllow}, true /* failAudit */))
	if err == nil {
		t.Fatal("Restore succeeded despite forced audit failure")
	}
	sess, _ = f.repo.GetSession(ctx, "s1")
	if sess.ArchivedAt == nil {
		t.Error("session un-archived despite audit failure — not same-tx")
	}
	if msgs, _ := f.repo.ListChatMessages(ctx, "s1"); len(msgs) != 0 {
		t.Errorf("transcript rebuilt despite rollback: %d rows, want 0", len(msgs))
	}
}

// TestLegacyTablesUntouched proves the §13 hard constraint by effect: a full
// archive→restore round-trip never reads, writes, or alters the legacy
// migration-001 sessions / session_messages tables. Rows seeded there survive
// untouched, and the round-trip demonstrably acts on the live tables.
func TestLegacyTablesUntouched(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := f.db.Exec(`INSERT INTO sessions (id, summary) VALUES ('legacy-1', 'legacy row')`); err != nil {
		t.Fatalf("seed legacy sessions: %v", err)
	}
	if _, err := f.db.Exec(
		`INSERT INTO session_messages (session_id, role, content, created_at) VALUES ('legacy-1','user','hi',?)`, now); err != nil {
		t.Fatalf("seed legacy session_messages: %v", err)
	}

	f.mkSession("s1", "user:alice")
	f.addMsg("m1", "s1", "user", "live")
	sess, _ := f.repo.GetSession(ctx, "s1")
	if _, err := f.archiver.Archive(ctx, *sess, "user:admin",
		f.commit(audit.Event{Action: audit.ActionSessionArchive, Principal: "user:admin", Kind: audit.KindAdminAccess, Decision: audit.DecisionAllow}, false)); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	sess, _ = f.repo.GetSession(ctx, "s1")
	if err := f.archiver.Restore(ctx, *sess,
		f.commit(audit.Event{Action: audit.ActionSessionUnarchive, Principal: "user:admin", Kind: audit.KindAdminAccess, Decision: audit.DecisionAllow}, false)); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	var legacySessions, legacyMessages int
	_ = f.db.QueryRow(`SELECT count(*) FROM sessions`).Scan(&legacySessions)
	_ = f.db.QueryRow(`SELECT count(*) FROM session_messages`).Scan(&legacyMessages)
	if legacySessions != 1 || legacyMessages != 1 {
		t.Errorf("legacy rows changed by archive round-trip: sessions=%d messages=%d, want 1/1", legacySessions, legacyMessages)
	}
	// Sanity: the round-trip really acted on the live transcript.
	if msgs, _ := f.repo.ListChatMessages(ctx, "s1"); len(msgs) != 1 {
		t.Errorf("live transcript not rebuilt: %d rows, want 1", len(msgs))
	}
}

// TestEncodeDecodeVersionGate is the unit-level proof that Decode is the shared
// refuse-or-migrate gate: a current-version artifact round-trips, an unknown
// version is refused.
func TestEncodeDecodeVersionGate(t *testing.T) {
	a := &sessionarchive.Artifact{
		Session:  sessionarchive.ArchivedSession{ID: "s1", Type: "default", CreatorPrincipal: "user:alice"},
		Messages: []sessionarchive.ArchivedMessage{{ID: "m1", Seq: 1, Role: "user", Content: "hi"}},
	}
	data, err := sessionarchive.Encode(a)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := sessionarchive.Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.SchemaVersion != sessionarchive.CurrentSchemaVersion || got.Session.ID != "s1" || len(got.Messages) != 1 {
		t.Errorf("decoded artifact mismatch: %+v", got)
	}

	// Unknown version → refused.
	var blob map[string]any
	_ = json.Unmarshal(data, &blob)
	blob["schema_version"] = sessionarchive.CurrentSchemaVersion + 1
	bumped, _ := json.Marshal(blob)
	if _, err := sessionarchive.Decode(bumped); !errors.Is(err, sessionarchive.ErrUnsupportedArtifactVersion) {
		t.Errorf("Decode(unknown version) = %v, want ErrUnsupportedArtifactVersion", err)
	}
}
