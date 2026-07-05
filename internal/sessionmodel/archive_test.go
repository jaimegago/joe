package sessionmodel_test

import (
	"context"
	"errors"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
)

// addMsg appends a message through the live AddChatMessage path (seq assigned).
func addMsg(t *testing.T, repo sessionmodel.Repository, id, sessionID, role, content string) {
	t.Helper()
	if _, err := repo.AddChatMessage(context.Background(), sessionmodel.ChatMessage{
		ID: id, SessionID: sessionID, Role: role, Content: content,
	}); err != nil {
		t.Fatalf("AddChatMessage %s: %v", id, err)
	}
}

// TestArchive_MovesTranscriptAndStampsColumns proves the §12.6 MOVE semantics at
// the store level: ArchiveSession stamps archived_at/archived_by/archive_ref AND
// removes the hot transcript rows, so the normal read path (ListChatMessages)
// returns nothing for the session afterward. Re-archiving is rejected without
// touching the transcript.
func TestArchive_MovesTranscriptAndStampsColumns(t *testing.T) {
	s := newTestStore(t)
	repo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	mkSession(t, repo, "s1", "user:alice")
	addMsg(t, repo, "m1", "s1", "user", "hello")
	addMsg(t, repo, "m2", "s1", "assistant", "hi there")

	if err := repo.ArchiveSession(ctx, "s1", "user:admin", "fs:s1.json", 2); err != nil {
		t.Fatalf("ArchiveSession: %v", err)
	}

	sess, _ := repo.GetSession(ctx, "s1")
	if sess == nil || sess.ArchivedAt == nil || sess.ArchivedBy == nil || sess.ArchiveRef == nil {
		t.Fatalf("archive columns not stamped: %+v", sess)
	}
	if *sess.ArchivedBy != "user:admin" || *sess.ArchiveRef != "fs:s1.json" {
		t.Errorf("archived_by/ref = %q/%q, want user:admin/fs:s1.json", *sess.ArchivedBy, *sess.ArchiveRef)
	}
	// The MOVE: hot rows are gone; the normal read path returns nothing.
	if msgs, _ := repo.ListChatMessages(ctx, "s1"); len(msgs) != 0 {
		t.Errorf("transcript still in hot storage after archive: %d rows", len(msgs))
	}

	// Re-archive is rejected (guard matched no un-archived row). The transcript is
	// already moved (0 live rows), so the count guard passes and the
	// already-archived guard fires.
	if err := repo.ArchiveSession(ctx, "s1", "user:admin", "fs:dup.json", 0); !errors.Is(err, sessionmodel.ErrSessionAlreadyArchived) {
		t.Errorf("double archive err = %v, want ErrSessionAlreadyArchived", err)
	}
}

// TestArchive_RefusesTranscriptChanged proves the §12.6 mid-window guard: if the
// live transcript no longer matches the count the caller serialized into the
// artifact (a message landed between the artifact read and the archive commit),
// ArchiveSession refuses with ErrArchiveTranscriptChanged and destroys NOTHING —
// the columns stay unset and every hot row survives, so the caller may re-read and
// retry. This is the structural close of the "message written mid-archive is
// silently deleted" data-loss window.
func TestArchive_RefusesTranscriptChanged(t *testing.T) {
	s := newTestStore(t)
	repo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	mkSession(t, repo, "s1", "user:alice")
	addMsg(t, repo, "m1", "s1", "user", "hello")
	addMsg(t, repo, "m2", "s1", "assistant", "hi there")
	// The caller built its artifact from 2 messages; then a 3rd landed before the
	// archive commit. Passing the stale expectedMsgs=2 must be refused.
	addMsg(t, repo, "m3", "s1", "user", "wait, one more")

	err := repo.ArchiveSession(ctx, "s1", "user:admin", "fs:s1.json", 2)
	if !errors.Is(err, sessionmodel.ErrArchiveTranscriptChanged) {
		t.Fatalf("ArchiveSession err = %v, want ErrArchiveTranscriptChanged", err)
	}

	// Nothing was destroyed or stamped.
	sess, _ := repo.GetSession(ctx, "s1")
	if sess.ArchivedAt != nil || sess.ArchiveRef != nil {
		t.Errorf("archive columns stamped despite a refused transcript-changed archive: %+v", sess)
	}
	if msgs, _ := repo.ListChatMessages(ctx, "s1"); len(msgs) != 3 {
		t.Errorf("transcript rows = %d, want 3 (nothing deleted on a refused archive)", len(msgs))
	}
}

// TestUnarchive_ClearsColumns proves UnarchiveSession clears the archive columns,
// returning the session to active. The transcript rebuild is the caller's job
// (InsertChatMessageTx), exercised in the archive provider round-trip test.
func TestUnarchive_ClearsColumns(t *testing.T) {
	s := newTestStore(t)
	repo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	mkSession(t, repo, "s1", "user:alice")
	if err := repo.ArchiveSession(ctx, "s1", "user:admin", "fs:s1.json", 0); err != nil {
		t.Fatalf("ArchiveSession: %v", err)
	}
	if err := repo.UnarchiveSession(ctx, "s1"); err != nil {
		t.Fatalf("UnarchiveSession: %v", err)
	}
	sess, _ := repo.GetSession(ctx, "s1")
	if sess.ArchivedAt != nil || sess.ArchivedBy != nil || sess.ArchiveRef != nil {
		t.Errorf("archive columns not cleared: %+v", sess)
	}
	// Unarchiving a non-archived session is rejected.
	if err := repo.UnarchiveSession(ctx, "s1"); !errors.Is(err, sessionmodel.ErrSessionNotArchived) {
		t.Errorf("unarchive non-archived err = %v, want ErrSessionNotArchived", err)
	}
}

// TestInsertChatMessageTx_PreservesSeqAndRoles proves the restore write path
// rebuilds rows verbatim — preserving the artifact's seq (exact ordering) and
// roles — without recomputing seq or bumping last_activity_at.
func TestInsertChatMessageTx_PreservesSeqAndRoles(t *testing.T) {
	s := newTestStore(t)
	repo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	mkSession(t, repo, "s1", "user:alice")
	before, _ := repo.GetSession(ctx, "s1")

	rows := []sessionmodel.ChatMessage{
		{ID: "m1", SessionID: "s1", Seq: 1, Role: "user", Content: "a", CreatedAt: time.Now().UTC()},
		{ID: "m2", SessionID: "s1", Seq: 2, Role: "assistant", Content: "b", CreatedAt: time.Now().UTC()},
		{ID: "m3", SessionID: "s1", Seq: 3, Role: "user", Content: "c", CreatedAt: time.Now().UTC()},
	}
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	for _, m := range rows {
		if err := repo.InsertChatMessageTx(ctx, tx, m); err != nil {
			t.Fatalf("InsertChatMessageTx %s: %v", m.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	got, _ := repo.ListChatMessages(ctx, "s1")
	if len(got) != 3 {
		t.Fatalf("rebuilt rows = %d, want 3", len(got))
	}
	for i, m := range got {
		if m.Seq != rows[i].Seq || m.Role != rows[i].Role || m.Content != rows[i].Content {
			t.Errorf("row %d = seq %d/%s/%s, want %d/%s/%s", i,
				m.Seq, m.Role, m.Content, rows[i].Seq, rows[i].Role, rows[i].Content)
		}
	}
	// last_activity_at is untouched by the rebuild (a restore is not chat activity).
	after, _ := repo.GetSession(ctx, "s1")
	if !after.LastActivityAt.Equal(before.LastActivityAt) {
		t.Errorf("last_activity_at moved on rebuild: %v -> %v", before.LastActivityAt, after.LastActivityAt)
	}
}

// TestArchive_SameTxRollback proves the archive column stamp and the transcript
// move share ONE transaction with whatever else the caller runs: a forced failure
// after ArchiveSessionTx rolls BOTH back — the columns stay unset and the hot
// transcript rows survive.
func TestArchive_SameTxRollback(t *testing.T) {
	s := newTestStore(t)
	repo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	mkSession(t, repo, "s1", "user:alice")
	addMsg(t, repo, "m1", "s1", "user", "hello")

	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := repo.ArchiveSessionTx(ctx, tx, "s1", "user:admin", "fs:s1.json", 1); err != nil {
		t.Fatalf("ArchiveSessionTx: %v", err)
	}
	// Simulate a later same-tx failure (e.g. the audit insert) by rolling back.
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	sess, _ := repo.GetSession(ctx, "s1")
	if sess.ArchivedAt != nil {
		t.Error("archive columns persisted despite rollback — not same-tx")
	}
	if msgs, _ := repo.ListChatMessages(ctx, "s1"); len(msgs) != 1 {
		t.Errorf("transcript lost despite rollback: %d rows, want 1", len(msgs))
	}
}
