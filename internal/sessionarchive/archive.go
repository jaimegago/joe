// Package sessionarchive implements the §12.6 archive backend behind a provider
// seam (DESIGN-CHAT-SESSIONS.md §12.6, ledger node B007 — the archive half,
// built in the B007c split node). §12 wins over earlier sections of that
// document where they conflict.
//
// WHAT ARCHIVE IS (§12.6). Archiving a session serializes it to a versioned,
// self-contained artifact (session metadata PLUS the full transcript in one
// file), REMOVES the hot transcript rows from chat_messages (the artifact
// becomes the sole copy — a MOVE to cold storage, not a copy), and stamps the
// session's archive_ref with the provider-produced locator. Restore parses the
// artifact back, REBUILDS the hot transcript rows, and clears the archive
// columns. An archived session's transcript is unreadable through the normal
// read path until restored.
//
// THE PROVIDER SEAM. v1 ships ONLY the filesystem provider (FilesystemProvider),
// which writes versioned JSON files under a configured directory. An object-store
// (S3-compatible) provider is a designed-for later addition behind the SAME
// Provider interface — the seam is shaped for it (locator carries a scheme), but
// it is not built in v1.
//
// THE VERSION GATE (§12.6). The artifact carries a schema version. Decode is the
// single refuse-or-migrate gate shared by every provider: it REFUSES an artifact
// whose version it does not recognize rather than mis-parse it. For v1 there is
// exactly one recognized version (CurrentSchemaVersion); an unknown version is
// refused (ErrUnsupportedArtifactVersion), never silently accepted. A future
// format bump adds a migration branch here, not in each provider.
//
// LEGACY TABLES (§13 hard constraint). The transcript read for the artifact and
// the transcript rebuild on restore touch ONLY the live chat_messages table for
// agent_sessions, via the Store seam below. No archive path reads, scans, drops,
// or alters the legacy migration-001 `sessions` / `session_messages` tables.
package sessionarchive

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jaimegago/joe/internal/sessionmodel"
)

// CurrentSchemaVersion is the artifact format version this build WRITES and the
// ONLY version it ACCEPTS on restore. The version exists so a later format change
// can be detected and migrated rather than mis-parsed (§12.6): a future bump
// increments this and adds a migration branch in Decode, while an unrecognized
// version is refused until then.
const CurrentSchemaVersion = 1

// ErrUnsupportedArtifactVersion is returned by Decode (and surfaces from a
// provider's Load) when an artifact's schema_version is not CurrentSchemaVersion.
// It is the §12.6 "refuse rather than mis-parse" outcome — restore must never
// silently accept an unknown version.
var ErrUnsupportedArtifactVersion = errors.New("sessionarchive: unsupported artifact schema version")

// ErrNotArchived is returned by Restore when the session it is handed has no
// archive_ref (it is not archived), so there is nothing to rehydrate.
var ErrNotArchived = errors.New("sessionarchive: session is not archived (no archive_ref)")

// Artifact is the §12.6 versioned, self-contained archive of a session: every
// piece of metadata needed to reconstitute the session row PLUS the full
// transcript, in one file. Restore needs nothing but this artifact.
type Artifact struct {
	// SchemaVersion is stamped by Encode and validated by Decode. It is the
	// refuse-or-migrate discriminator.
	SchemaVersion int `json:"schema_version"`
	// Session is the metadata projection that reconstitutes the agent_sessions
	// row (creator, type, timestamps, incident linkage, retention class, title).
	Session ArchivedSession `json:"session"`
	// Messages is the full transcript in seq order, each carrying its role so
	// restore rebuilds the hot rows exactly (ordering + roles preserved).
	Messages []ArchivedMessage `json:"messages"`
}

// ArchivedSession is the metadata half of the artifact — exactly the fields
// needed to rebuild the agent_sessions row on restore. The lifecycle/archive
// columns themselves are NOT carried: archiving is the event, and restore clears
// them, so persisting them would be redundant and could re-introduce a stale
// archived state on rehydrate.
type ArchivedSession struct {
	ID               string  `json:"id"`
	Type             string  `json:"type"`
	IncidentState    *string `json:"incident_state,omitempty"`
	CreatedAt        string  `json:"created_at"`
	LastActivityAt   string  `json:"last_activity_at"`
	CreatorPrincipal string  `json:"creator_principal"`
	LinkedIncidentID *string `json:"linked_incident_id,omitempty"`
	RetentionClass   *string `json:"retention_class,omitempty"`
	Title            *string `json:"title,omitempty"`
}

// ArchivedMessage is one transcript row in the artifact. Seq preserves the exact
// per-session ordering; Role/Content/ToolName/ToolArgs/CreatedAt are carried
// verbatim so the rebuilt hot row equals the original.
type ArchivedMessage struct {
	ID        string  `json:"id"`
	Seq       int     `json:"seq"`
	Role      string  `json:"role"`
	Content   string  `json:"content"`
	ToolName  *string `json:"tool_name,omitempty"`
	ToolArgs  *string `json:"tool_args,omitempty"`
	CreatedAt string  `json:"created_at"`
}

// Provider is the §12.6 archive-backend seam. v1 ships only FilesystemProvider;
// an object-store provider is a later addition behind this SAME interface. The
// locator returned by Store is what is persisted in agent_sessions.archive_ref
// and handed back to Load/Remove. Implementations MUST route the artifact through
// Encode/Decode so the shared version gate applies uniformly.
type Provider interface {
	// Store serializes the artifact to cold storage and returns its locator
	// (archive_ref). The locator carries the provider scheme so a later
	// multi-provider seam can dispatch Load/Remove by scheme.
	Store(ctx context.Context, a *Artifact) (ref string, err error)
	// Load reads and decodes the artifact at ref. It refuses an unrecognized
	// schema version (ErrUnsupportedArtifactVersion) — never silently accepts.
	Load(ctx context.Context, ref string) (*Artifact, error)
	// Remove deletes the artifact at ref. Used to clean up an orphaned artifact
	// when the archive state transaction rolls back.
	Remove(ctx context.Context, ref string) error
	// Scheme is the locator scheme this provider produces/accepts (e.g. "fs").
	Scheme() string
}

// Store is the narrow slice of sessionmodel.Repository the archiver needs. It is
// satisfied by *sessionmodel.SQLRepository. Taking an interface keeps the
// archiver unit-testable and makes the legacy-table constraint structural: the
// only transcript methods reachable here read/write the LIVE chat_messages table.
type Store interface {
	// ListChatMessages reads the LIVE transcript (chat_messages) in seq order —
	// the artifact's source. Never the legacy session_messages table.
	ListChatMessages(ctx context.Context, sessionID string) ([]sessionmodel.ChatMessage, error)
	// ArchiveSessionTx sets archived_at/archived_by/archive_ref AND removes the
	// hot transcript rows (the move), on a caller transaction.
	ArchiveSessionTx(ctx context.Context, tx *sql.Tx, id, by, ref string) error
	// UnarchiveSessionTx clears the archive columns on a caller transaction.
	UnarchiveSessionTx(ctx context.Context, tx *sql.Tx, id string) error
	// InsertChatMessageTx rebuilds one hot transcript row verbatim (preserving
	// seq/role) on a caller transaction. Writes ONLY to chat_messages.
	InsertChatMessageTx(ctx context.Context, tx *sql.Tx, m sessionmodel.ChatMessage) error
}

// CommitFn runs the effect+audit transaction — the caller's mutateWithAudit with
// its audit event already bound. The archiver hands it the archive (or restore)
// state mutation; the caller couples that mutation to its audit row in ONE
// transaction. Binding the event in the caller is deliberate: the admin route
// writes a KindAdminAccess row, the sweeper a KindSessionLifecycle row, and the
// archiver stays agnostic to which.
type CommitFn func(mutate func(*sql.Tx) error) error

// Archiver couples a provider with the session store so the admin route and the
// sweeper share ONE archive/restore implementation — no divergent code paths
// (§12.5). Construct with New.
type Archiver struct {
	provider Provider
	store    Store
}

// New builds an Archiver over the provider and store.
func New(provider Provider, store Store) *Archiver {
	return &Archiver{provider: provider, store: store}
}

// Archive performs the §12.6 archive of sess under principal `by`:
//  1. read the LIVE transcript (chat_messages only);
//  2. build the versioned self-contained artifact;
//  3. write it via the provider, producing the archive_ref locator;
//  4. run commit — the caller's same-tx effect+audit — to stamp the archive
//     columns AND remove the hot transcript rows in ONE transaction.
//
// The file write (step 3) cannot be transactional with SQLite, so it happens
// before the commit; if commit fails (e.g. a forced audit failure rolls the
// transaction back) the just-written artifact is REMOVED so no orphan file
// outlives a rolled-back state. Returns the archive_ref on success.
func (a *Archiver) Archive(ctx context.Context, sess sessionmodel.AgentSession, by string, commit CommitFn) (string, error) {
	msgs, err := a.store.ListChatMessages(ctx, sess.ID)
	if err != nil {
		return "", fmt.Errorf("archive: read transcript: %w", err)
	}
	artifact := BuildArtifact(sess, msgs)
	ref, err := a.provider.Store(ctx, artifact)
	if err != nil {
		return "", fmt.Errorf("archive: store artifact: %w", err)
	}
	if err := commit(func(tx *sql.Tx) error {
		return a.store.ArchiveSessionTx(ctx, tx, sess.ID, by, ref)
	}); err != nil {
		// The state transaction rolled back: the hot rows survive and the columns
		// are unset, so the artifact would be an orphan. Remove it (best-effort) so
		// there is never an artifact without a committed archive state.
		_ = a.provider.Remove(ctx, ref)
		return "", err
	}
	return ref, nil
}

// Restore performs the §12.6 unarchive of sess:
//  1. parse the artifact at sess.ArchiveRef (Load refuses an unknown version);
//  2. run commit — the caller's same-tx effect+audit — to clear the archive
//     columns AND rebuild the hot transcript rows (in seq order, roles preserved)
//     in ONE transaction.
//
// It refuses a session with no archive_ref (ErrNotArchived) and surfaces
// ErrUnsupportedArtifactVersion unchanged so the route can report a refused
// (never silently accepted) unknown version.
func (a *Archiver) Restore(ctx context.Context, sess sessionmodel.AgentSession, commit CommitFn) error {
	if sess.ArchiveRef == nil {
		return ErrNotArchived
	}
	artifact, err := a.provider.Load(ctx, *sess.ArchiveRef)
	if err != nil {
		return err // ErrUnsupportedArtifactVersion bubbles up unchanged
	}
	return commit(func(tx *sql.Tx) error {
		if err := a.store.UnarchiveSessionTx(ctx, tx, sess.ID); err != nil {
			return err
		}
		for _, m := range artifact.Messages {
			if err := a.store.InsertChatMessageTx(ctx, tx, sessionmodel.ChatMessage{
				ID:        m.ID,
				SessionID: sess.ID,
				Seq:       m.Seq,
				Role:      m.Role,
				Content:   m.Content,
				ToolName:  m.ToolName,
				ToolArgs:  m.ToolArgs,
				CreatedAt: parseArtifactTime(m.CreatedAt),
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

// BuildArtifact assembles the versioned artifact from a session row and its live
// transcript. It stamps no version itself — Encode does, so the on-disk bytes are
// the single source of the version stamp.
func BuildArtifact(s sessionmodel.AgentSession, msgs []sessionmodel.ChatMessage) *Artifact {
	as := ArchivedSession{
		ID:               s.ID,
		Type:             string(s.Type),
		CreatedAt:        formatArtifactTime(s.CreatedAt),
		LastActivityAt:   formatArtifactTime(s.LastActivityAt),
		CreatorPrincipal: s.CreatorPrincipal,
		LinkedIncidentID: s.LinkedIncidentID,
		RetentionClass:   s.RetentionClass,
		Title:            s.Title,
	}
	if s.IncidentState != nil {
		v := string(*s.IncidentState)
		as.IncidentState = &v
	}
	out := &Artifact{Session: as, Messages: make([]ArchivedMessage, 0, len(msgs))}
	for _, m := range msgs {
		out.Messages = append(out.Messages, ArchivedMessage{
			ID:        m.ID,
			Seq:       m.Seq,
			Role:      m.Role,
			Content:   m.Content,
			ToolName:  m.ToolName,
			ToolArgs:  m.ToolArgs,
			CreatedAt: formatArtifactTime(m.CreatedAt),
		})
	}
	return out
}

// Encode marshals the artifact for cold storage, stamping CurrentSchemaVersion so
// the on-disk bytes always carry this build's version. Every provider routes its
// Store through Encode.
func Encode(a *Artifact) ([]byte, error) {
	a.SchemaVersion = CurrentSchemaVersion
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode artifact: %w", err)
	}
	return data, nil
}

// Decode is the single §12.6 refuse-or-migrate gate, shared by every provider's
// Load. It unmarshals the envelope and REFUSES an artifact whose schema_version
// is not recognized (ErrUnsupportedArtifactVersion) rather than mis-parse it. For
// v1 the only recognized version is CurrentSchemaVersion; a future format bump
// adds a migration branch here.
func Decode(data []byte) (*Artifact, error) {
	var a Artifact
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("decode artifact: %w", err)
	}
	if a.SchemaVersion != CurrentSchemaVersion {
		return nil, fmt.Errorf("%w: got %d, this build understands %d",
			ErrUnsupportedArtifactVersion, a.SchemaVersion, CurrentSchemaVersion)
	}
	return &a, nil
}
