package sessionmodel_test

import (
	"context"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
)

// mkSession creates a minimal active default session owned by creator.
func mkSession(t *testing.T, repo sessionmodel.Repository, id, creator string) {
	t.Helper()
	if _, err := repo.CreateSession(context.Background(), sessionmodel.AgentSession{
		ID: id, Type: sessionmodel.SessionTypeDefault, CreatorPrincipal: creator,
	}); err != nil {
		t.Fatalf("CreateSession %s: %v", id, err)
	}
}

// TestLifecycle_TrashRestore proves the §12.5 macOS-trash transitions at the
// store level: soft-delete sets trashed_at/trashed_by (+ purge_after) and moves
// the session OUT of the active set while leaving it physically present; restore
// clears all three and returns it to active. "Active = all lifecycle columns
// null" is preserved at every step.
func TestLifecycle_TrashRestore(t *testing.T) {
	s := newTestStore(t)
	repo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	mkSession(t, repo, "s1", "user:alice")
	deadline := time.Now().UTC().Add(30 * 24 * time.Hour)

	if err := repo.TrashSession(ctx, "s1", "user:alice", &deadline); err != nil {
		t.Fatalf("TrashSession: %v", err)
	}
	sess, _ := repo.GetSession(ctx, "s1")
	if sess == nil {
		t.Fatal("session physically gone after soft-delete — must be trashed, not purged")
	}
	if sess.TrashedAt == nil || sess.TrashedBy == nil || *sess.TrashedBy != "user:alice" {
		t.Errorf("trashed_at/by = %v/%v, want set with by=user:alice", sess.TrashedAt, sess.TrashedBy)
	}
	if sess.PurgeAfter == nil {
		t.Error("purge_after not set on soft-delete")
	}
	// Out of the active team list.
	active, _ := repo.ListRecentSessions(ctx, 0)
	if containsID(active, "s1") {
		t.Error("trashed session still in the active list")
	}
	// In the owner's trash list.
	trash, _ := repo.ListTrashedSessions(ctx, strPtr("user:alice"), 0)
	if !containsID(trash, "s1") {
		t.Error("trashed session missing from its owner's trash list")
	}

	// Double-trash is rejected.
	if err := repo.TrashSession(ctx, "s1", "user:alice", &deadline); err == nil {
		t.Error("double soft-delete must error (already trashed)")
	}

	// Restore clears every lifecycle column → active again.
	if err := repo.RestoreSession(ctx, "s1"); err != nil {
		t.Fatalf("RestoreSession: %v", err)
	}
	sess, _ = repo.GetSession(ctx, "s1")
	if sess.TrashedAt != nil || sess.TrashedBy != nil || sess.PurgeAfter != nil {
		t.Errorf("restore left lifecycle columns set: %+v", sess)
	}
	active, _ = repo.ListRecentSessions(ctx, 0)
	if !containsID(active, "s1") {
		t.Error("restored session missing from the active list")
	}
	// Restoring a non-trashed session is rejected.
	if err := repo.RestoreSession(ctx, "s1"); err == nil {
		t.Error("restore of a non-trashed session must error")
	}
}

// TestLifecycle_PurgeSeversLinkedChildren proves the §12.5 purge: a hard expunge
// that destroys the session + transcript (cascade) and SEVERS linked children's
// linked_incident_id (ON DELETE SET NULL) rather than destroying them. The
// manifest counts what will be destroyed/severed.
func TestLifecycle_PurgeSeversLinkedChildren(t *testing.T) {
	s := newTestStore(t)
	repo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	// Incident with a transcript and two linked children.
	if _, err := repo.CreateSession(ctx, sessionmodel.AgentSession{
		ID: "inc", Type: sessionmodel.SessionTypeIncident,
		IncidentState: incidentStatePtr(sessionmodel.IncidentStateDeclared), CreatorPrincipal: "user:alice",
	}); err != nil {
		t.Fatalf("create incident: %v", err)
	}
	for _, id := range []string{"c1", "c2"} {
		link := "inc"
		if _, err := repo.CreateSession(ctx, sessionmodel.AgentSession{
			ID: id, Type: sessionmodel.SessionTypeDefault, CreatorPrincipal: "user:bob",
			LinkedIncidentID: &link,
		}); err != nil {
			t.Fatalf("create child %s: %v", id, err)
		}
	}
	for i, body := range []string{"a", "b", "c"} {
		if _, err := repo.AddChatMessage(ctx, sessionmodel.ChatMessage{
			ID: "m" + body, SessionID: "inc", Role: "user", Content: body,
			CreatedAt: time.Now().UTC().Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("add message: %v", err)
		}
	}

	manifest, err := repo.PurgeManifest(ctx, "inc")
	if err != nil {
		t.Fatalf("PurgeManifest: %v", err)
	}
	if manifest.MessageCount != 3 {
		t.Errorf("manifest messages = %d, want 3", manifest.MessageCount)
	}
	if manifest.LinkedChildCount != 2 {
		t.Errorf("manifest linked children = %d, want 2", manifest.LinkedChildCount)
	}

	// Purge in a transaction (the route-backing path).
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := repo.PurgeSessionTx(ctx, tx, "inc"); err != nil {
		t.Fatalf("PurgeSessionTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Incident + its transcript are gone.
	if sess, _ := repo.GetSession(ctx, "inc"); sess != nil {
		t.Error("incident survived purge")
	}
	if msgs, _ := repo.ListChatMessages(ctx, "inc"); len(msgs) != 0 {
		t.Errorf("transcript not cascade-deleted: %d remain", len(msgs))
	}
	// Linked children SURVIVE with the link severed.
	for _, id := range []string{"c1", "c2"} {
		child, _ := repo.GetSession(ctx, id)
		if child == nil {
			t.Fatalf("linked child %s destroyed by purge (must be severed, not cascaded)", id)
		}
		if child.LinkedIncidentID != nil {
			t.Errorf("child %s link not severed: %v", id, *child.LinkedIncidentID)
		}
	}
}

// TestLifecycle_RetentionPolicyAndResolution proves the §12.5 retention-policy
// store and the §12.4 retention_class resolution: the policy round-trips through
// Set/Get, and a session resolves to the policy's terminal action, which stamps
// its retention_class column (no longer inert).
func TestLifecycle_RetentionPolicyAndResolution(t *testing.T) {
	s := newTestStore(t)
	repo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	// Seeded defaults (§12.5).
	p, err := repo.GetRetentionPolicy(ctx)
	if err != nil {
		t.Fatalf("GetRetentionPolicy: %v", err)
	}
	if p.InactivityDays != nil || p.TrashGraceDays != 30 || p.TerminalAction != sessionmodel.TerminalActionTrashThenPurge {
		t.Errorf("seeded policy = %+v, want OFF / 30 / trash_then_purge", p)
	}

	// Configure: inactivity 90d, grace 7d, terminal action archive.
	inactivity := 90
	next := sessionmodel.RetentionPolicy{
		InactivityDays: &inactivity, TrashGraceDays: 7,
		TerminalAction: sessionmodel.TerminalActionArchive,
	}
	tx, _ := s.DB().BeginTx(ctx, nil)
	if err := repo.SetRetentionPolicyTx(ctx, tx, next, "user:admin", time.Now().UTC()); err != nil {
		t.Fatalf("SetRetentionPolicyTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	got, _ := repo.GetRetentionPolicy(ctx)
	if got.InactivityDays == nil || *got.InactivityDays != 90 || got.TrashGraceDays != 7 ||
		got.TerminalAction != sessionmodel.TerminalActionArchive || got.UpdatedBy == nil {
		t.Errorf("stored policy = %+v, want 90 / 7 / archive / updated_by set", got)
	}

	// Resolution: a session resolves to the policy's terminal action, and stamping
	// it makes retention_class non-inert.
	mkSession(t, repo, "s1", "user:alice")
	res, err := repo.ResolveRetention(ctx, "s1")
	if err != nil {
		t.Fatalf("ResolveRetention: %v", err)
	}
	if res.Class != "archive" {
		t.Errorf("resolved class = %q, want archive (policy terminal action)", res.Class)
	}
	if err := repo.StampRetentionClass(ctx, "s1", res.Class); err != nil {
		t.Fatalf("StampRetentionClass: %v", err)
	}
	sess, _ := repo.GetSession(ctx, "s1")
	if sess.RetentionClass == nil || *sess.RetentionClass != "archive" {
		t.Errorf("retention_class column = %v, want archive (resolution is no longer inert)", sess.RetentionClass)
	}
}

// TestSweeperScans_InactiveAndPurgeable proves the B007b scan queries select the
// right candidates: ListSessionsInactiveBefore returns only ACTIVE sessions older
// than the cutoff (excluding fresh, trashed, and incident sessions), and
// ListPurgeableSessions returns only trashed sessions whose purge_after has
// passed (excluding trashed-with-future-deadline and trashed-with-no-deadline).
func TestSweeperScans_InactiveAndPurgeable(t *testing.T) {
	s := newTestStore(t)
	repo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()
	now := time.Now().UTC()

	// Active + stale → an inactivity candidate.
	if _, err := repo.CreateSession(ctx, sessionmodel.AgentSession{
		ID: "stale", Type: sessionmodel.SessionTypeDefault, CreatorPrincipal: "user:a",
		CreatedAt: now.Add(-40 * 24 * time.Hour), LastActivityAt: now.Add(-40 * 24 * time.Hour),
	}); err != nil {
		t.Fatalf("create stale: %v", err)
	}
	// Active + fresh → not a candidate.
	if _, err := repo.CreateSession(ctx, sessionmodel.AgentSession{
		ID: "fresh", Type: sessionmodel.SessionTypeDefault, CreatorPrincipal: "user:a",
		CreatedAt: now.Add(-1 * time.Hour), LastActivityAt: now.Add(-1 * time.Hour),
	}); err != nil {
		t.Fatalf("create fresh: %v", err)
	}
	// Trashed-past-deadline → a purge candidate, NOT an inactivity candidate.
	trashedAt := now.Add(-60 * 24 * time.Hour)
	pastDeadline := now.Add(-1 * time.Hour)
	if _, err := repo.CreateSession(ctx, sessionmodel.AgentSession{
		ID: "due", Type: sessionmodel.SessionTypeDefault, CreatorPrincipal: "user:a",
		CreatedAt: trashedAt, LastActivityAt: trashedAt,
		TrashedAt: &trashedAt, TrashedBy: strPtr("user:a"), PurgeAfter: &pastDeadline,
	}); err != nil {
		t.Fatalf("create due: %v", err)
	}
	// Trashed-future-deadline → neither candidate.
	futureDeadline := now.Add(24 * time.Hour)
	if _, err := repo.CreateSession(ctx, sessionmodel.AgentSession{
		ID: "waiting", Type: sessionmodel.SessionTypeDefault, CreatorPrincipal: "user:a",
		CreatedAt: trashedAt, LastActivityAt: trashedAt,
		TrashedAt: &trashedAt, TrashedBy: strPtr("user:a"), PurgeAfter: &futureDeadline,
	}); err != nil {
		t.Fatalf("create waiting: %v", err)
	}

	cutoff := now.Add(-7 * 24 * time.Hour)
	inactive, err := repo.ListSessionsInactiveBefore(ctx, cutoff)
	if err != nil {
		t.Fatalf("ListSessionsInactiveBefore: %v", err)
	}
	if !onlyIDs(inactive, "stale") {
		t.Errorf("inactive candidates = %v, want exactly [stale]", ids(inactive))
	}

	purgeable, err := repo.ListPurgeableSessions(ctx, now)
	if err != nil {
		t.Fatalf("ListPurgeableSessions: %v", err)
	}
	if !onlyIDs(purgeable, "due") {
		t.Errorf("purgeable candidates = %v, want exactly [due]", ids(purgeable))
	}
}

func ids(rows []sessionmodel.AgentSession) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

func onlyIDs(rows []sessionmodel.AgentSession, want ...string) bool {
	if len(rows) != len(want) {
		return false
	}
	got := map[string]bool{}
	for _, r := range rows {
		got[r.ID] = true
	}
	for _, w := range want {
		if !got[w] {
			return false
		}
	}
	return true
}

func containsID(rows []sessionmodel.ChatSessionRow, id string) bool {
	for _, r := range rows {
		if r.ID == id {
			return true
		}
	}
	return false
}

func strPtr(s string) *string { return &s }

func incidentStatePtr(s sessionmodel.IncidentState) *sessionmodel.IncidentState { return &s }
