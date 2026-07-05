package sessionarchive

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// fsScheme is the locator scheme the filesystem provider produces and accepts.
// A locator looks like "fs:<filename>"; the scheme prefix shapes the seam for a
// later multi-provider dispatch (e.g. "s3:<key>") even though only this provider
// ships in v1.
const fsScheme = "fs"

// FilesystemProvider is the §12.6 v1 archive backend: it serializes artifacts to
// versioned JSON files under a configured directory. The locator it returns is
// "fs:<filename>"; the filename is derived from the session id (one artifact per
// session), so a re-archive after a restore overwrites the stale file rather than
// accumulating. The base directory is provider-private, so moving it does not
// invalidate stored locators (they carry only the filename).
type FilesystemProvider struct {
	dir string
}

// NewFilesystemProvider builds a filesystem archive provider rooted at dir. The
// caller is responsible for creating the directory (cmd/joe/server.go does this
// at boot); the provider does not mkdir on every Store.
func NewFilesystemProvider(dir string) *FilesystemProvider {
	return &FilesystemProvider{dir: dir}
}

// Scheme returns the locator scheme ("fs").
func (p *FilesystemProvider) Scheme() string { return fsScheme }

// Store encodes the artifact (stamping the schema version) and writes it
// atomically so a crash mid-write never leaves a truncated artifact that Decode
// would later mis-read. The session id keys the filename, so one session has at
// most one artifact.
//
// It REFUSES to overwrite an existing final artifact: if a file already exists at
// the target, Store returns ErrArtifactExists rather than clobbering it. Because
// the artifact is keyed only by session id, a byte-identical locator is produced
// by any concurrent/prior archival of the same session; the guarded archive-state
// UPDATE (archiveExec, WHERE archived_at IS NULL) means at most one archival can
// legitimately win, so a pre-existing artifact is owned by that winner and must
// not be clobbered or later removed by a loser. This closes the data-loss window
// where a rolled-back loser's Remove(ref) would delete the winner's committed
// artifact (the ref is session-keyed, hence identical).
//
// The claim is atomic against a racing creator: the artifact is written to a
// per-call unique temp file (so two concurrent Stores never share a temp), then
// os.Link claims the final name — Link fails with os.ErrExist if the target
// already exists, so exactly one racer wins the final name. The temp is always
// cleaned up.
func (p *FilesystemProvider) Store(_ context.Context, a *Artifact) (string, error) {
	name, err := artifactFilename(a.Session.ID)
	if err != nil {
		return "", err
	}
	data, err := Encode(a)
	if err != nil {
		return "", err
	}
	final := filepath.Join(p.dir, name)

	// Per-call unique temp so concurrent Stores of the same session do not
	// overwrite each other's in-progress bytes before the atomic claim below.
	tmp, err := os.CreateTemp(p.dir, name+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("archive: create temp artifact: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("archive: write artifact: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("archive: write artifact: %w", err)
	}

	// Atomically claim the final name. os.Link fails with os.ErrExist if a final
	// artifact already exists, so a concurrent/prior archival's committed file is
	// never clobbered — the caller sees ErrArtifactExists and removes nothing.
	if err := os.Link(tmpName, final); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", ErrArtifactExists
		}
		return "", fmt.Errorf("archive: commit artifact: %w", err)
	}
	return fsScheme + ":" + name, nil
}

// Load reads and decodes the artifact at ref. The shared Decode applies the
// version gate, so an unrecognized version is refused here too.
func (p *FilesystemProvider) Load(_ context.Context, ref string) (*Artifact, error) {
	path, err := p.resolve(ref)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("archive: read artifact: %w", err)
	}
	return Decode(data)
}

// Remove deletes the artifact at ref. A missing file is not an error — the caller
// uses Remove to clean up a possibly-already-rolled-back write.
func (p *FilesystemProvider) Remove(_ context.Context, ref string) error {
	path, err := p.resolve(ref)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("archive: remove artifact: %w", err)
	}
	return nil
}

// resolve validates the locator's scheme and maps it to an absolute path inside
// the provider's directory. It rejects a locator whose filename contains a path
// separator (defense against a crafted archive_ref escaping the archive dir).
func (p *FilesystemProvider) resolve(ref string) (string, error) {
	prefix := fsScheme + ":"
	if !strings.HasPrefix(ref, prefix) {
		return "", fmt.Errorf("archive: locator %q is not a %s locator", ref, fsScheme)
	}
	name := strings.TrimPrefix(ref, prefix)
	if name == "" || name != filepath.Base(name) || strings.ContainsRune(name, os.PathSeparator) {
		return "", fmt.Errorf("archive: locator %q has an invalid filename", ref)
	}
	return filepath.Join(p.dir, name), nil
}

// artifactFilename derives the per-session artifact filename, rejecting a session
// id that is not a safe single path element (the id keys the on-disk file).
func artifactFilename(sessionID string) (string, error) {
	if sessionID == "" || sessionID != filepath.Base(sessionID) || strings.ContainsRune(sessionID, os.PathSeparator) {
		return "", fmt.Errorf("archive: session id %q is not a safe artifact filename", sessionID)
	}
	return sessionID + ".json", nil
}

// formatArtifactTime renders a timestamp as the artifact's RFC3339 string, the
// same wire format the session/transcript columns use.
func formatArtifactTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// parseArtifactTime parses an artifact RFC3339 timestamp, degrading a malformed
// value to the zero time (the chat-message writer defaults a zero CreatedAt to
// now, matching the live AddChatMessage path).
func parseArtifactTime(value string) time.Time {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return t
}
