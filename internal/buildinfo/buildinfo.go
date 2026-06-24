// Package buildinfo is the single source of build truth for the joe binary.
//
// Every consumer that needs to report what build it is — the /api/v1/status
// and /api/v1/version HTTP handlers and the joe_build_info Prometheus gauge —
// reads the assembled value through Get(). Nothing else in the binary should
// declare its own build-identity variables.
//
// # Injected fields
//
// Version, Commit, and BuildTime are package-level string variables (never
// const, so the linker can rewrite them) and are the sole ldflags injection
// targets. A build path sets them by full import path, e.g.:
//
//	go build -ldflags "\
//	  -X github.com/jaimegago/joe/internal/buildinfo.Version=$(git describe --tags --always --dirty) \
//	  -X github.com/jaimegago/joe/internal/buildinfo.Commit=$(git rev-parse --short HEAD) \
//	  -X github.com/jaimegago/joe/internal/buildinfo.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
//	  ./cmd/joe
//
// A plain build with no ldflags still compiles and reports the unset defaults
// ("dev"/"none"/"unknown"), so "dev" only ever appears on a deliberately
// unset build.
//
// # Self-derived freshness digest
//
// UIDigest is NOT injected. It is computed once at boot from the embedded UI
// filesystem by Init, so it is derived from the exact bytes the binary serves:
// it cannot be absent on a real boot and cannot disagree with what is embedded.
// The canonical serialization is documented on Compute precisely enough that an
// external process holding the same dist files can recompute the identical
// digest byte-for-byte, with no shared secret.
package buildinfo

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"sort"
	"sync"
)

// These three variables are the ldflags -X injection targets. They MUST remain
// package-level, string-typed, and non-const so `go build -ldflags "-X ..."`
// can set them by their full import path. The defaults are what an unset build
// reports.
var (
	Version   = "dev"
	Commit    = "none"
	BuildTime = "unknown"
)

// Info is the assembled, read-only build identity returned by Get.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
	UIDigest  string `json:"ui_digest"`
}

var (
	digestOnce sync.Once
	uiDigest   string
)

// Init computes the embedded-UI digest once from the filesystem the binary
// actually serves and caches it for every later Get. It is idempotent: only
// the first call computes; subsequent calls are no-ops and return nil. Call it
// once at boot, before serving requests, passing the served UI fs.FS (the same
// subtree the static handler serves from). On a build with no UI embedded the
// walk still succeeds over whatever files are present, so the digest is always
// well-defined.
func Init(uiFS fs.FS) error {
	var initErr error
	digestOnce.Do(func() {
		uiDigest, initErr = Compute(uiFS)
	})
	return initErr
}

// Get returns the assembled build identity. The injected fields are read from
// the package variables; UIDigest is the value cached by Init (empty string
// until Init runs, which on a real boot is before any request is served).
func Get() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		BuildTime: BuildTime,
		UIDigest:  uiDigest,
	}
}

// Compute returns the canonical sha256 digest of the served UI filesystem as
// lowercase hex.
//
// # Canonical serialization (reproducible byte-for-byte)
//
// An external process holding the same dist files can recompute this exact
// value, with no shared secret, by following this serialization precisely:
//
//  1. Treat fsys as rooted at the served dist directory. Paths are relative to
//     that root and use forward slashes ("index.html", "assets/index-ab12.js");
//     there is no leading "dist/" component.
//  2. Walk the whole tree and collect the relative path of every REGULAR FILE.
//     Directories themselves contribute nothing. Every file is included,
//     dotfiles too (e.g. ".gitkeep") — the embed uses the `all:` prefix, so the
//     served tree contains them and an external harness must include them as
//     well.
//  3. Sort the collected paths lexically, byte-wise ascending (Go's
//     sort.Strings on the UTF-8 path strings).
//  4. Feed a single running sha256 hasher in that path order. For each path,
//     write the path's UTF-8 bytes, immediately followed by that file's raw
//     content bytes. No separators, no length prefixes, no trailing newline.
//  5. The digest is the lowercase-hex encoding of the final 32-byte sum.
func Compute(fsys fs.FS) (string, error) {
	var paths []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		paths = append(paths, p)
		return nil
	})
	if err != nil {
		return "", err
	}

	sort.Strings(paths)

	h := sha256.New()
	for _, p := range paths {
		if _, err := h.Write([]byte(p)); err != nil {
			return "", err
		}
		content, readErr := fs.ReadFile(fsys, p)
		if readErr != nil {
			return "", readErr
		}
		if _, err := h.Write(content); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
