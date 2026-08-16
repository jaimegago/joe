package git

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// Bounds and substrate rules for Search. Every one of these is part of the
// tool's stated contract (see internal/tools/core/reposearch.go), not an
// implementation detail: an undeclared rule makes the searched substrate vary
// invisibly and takes the determinism claim with it (D-0141).
const (
	// searchDefaultMaxMatches is the OUTPUT bound applied when the caller names
	// none. searchMaxMatchesCeiling is the fixed ceiling: a caller may lower the
	// bound, never raise it past this.
	searchDefaultMaxMatches = 200
	searchMaxMatchesCeiling = 1000

	// searchMaxFilesScanned is the WORK bound, and is both the default and the
	// fixed ceiling — a caller may only lower it. It counts files CONSIDERED:
	// every in-scope file the scan reaches, whether it was searched or excluded
	// from the substrate.
	searchMaxFilesScanned = 20000

	// searchBinarySniffBytes is the prefix examined by the binary rule below.
	searchBinarySniffBytes = 8000

	// searchMaxLineBytes caps the line text carried back per match. A longer
	// line is cut here and the match is flagged, so a minified file cannot
	// return a megabyte of text as one "line".
	searchMaxLineBytes = 512

	// searchCancelCheckInterval is how often the scan checks for context
	// cancellation, in files considered.
	searchCancelCheckInterval = 64
)

// SearchOptions is one content search over one component, at one commit.
type SearchOptions struct {
	// Pattern is an RE2 regular expression, or — when Literal is set — a plain
	// substring. An invalid pattern is an error, never zero hits.
	Pattern string
	Literal bool

	// Commit pins the substrate. Empty means "resolve the clone's current head
	// and report it". Non-empty means "answer at exactly this revision or
	// fail"; the resolved commit hash is always reported back.
	Commit string

	// PathPrefix narrows the substrate to one path and everything beneath it.
	// Empty means the whole tree. The applied prefix is reported back.
	PathPrefix string

	// MaxMatches and MaxFilesScanned lower the output and work bounds
	// respectively. Zero or negative means "use the default"; a value above the
	// ceiling is clamped down to it.
	MaxMatches      int
	MaxFilesScanned int
}

// SearchMatch is one matching line. Line is 1-based.
type SearchMatch struct {
	Path          string `json:"path"`
	Line          int    `json:"line"`
	Text          string `json:"text"`
	TextTruncated bool   `json:"text_truncated,omitempty"`
}

// SearchResult carries the matches plus everything the caller needs in order to
// know what was actually searched.
//
// The two exhaustion markers are deliberately separate fields: they answer
// different questions and must never collapse into one signal. Separate is NOT
// exclusive — when the output bound stops the scan with files still unvisited,
// both are true and both are correct. One says why the scan stopped; the other
// says whether the substrate was exhausted.
type SearchResult struct {
	// Commit is the commit the search answered at, always reported, including
	// for an empty result.
	Commit string `json:"commit"`
	// PathPrefix is the scope actually applied, normalized.
	PathPrefix string `json:"path_prefix"`

	Matches []SearchMatch `json:"matches"`

	// FilesInScope is every tracked file at Commit under PathPrefix.
	// FilesConsidered is how many of those the scan reached; FilesSearched is
	// how many of those were actually matched against the pattern, the rest
	// having been excluded from the substrate by the two rules below.
	FilesInScope    int `json:"files_in_scope"`
	FilesConsidered int `json:"files_considered"`
	FilesSearched   int `json:"files_searched"`
	SkippedBinary   int `json:"skipped_binary"`
	SkippedLarge    int `json:"skipped_large"`

	// MatchesTruncated says the OUTPUT bound was reached and the scan stopped
	// there, so the result is a prefix in (path, line) order and MORE MATCHES
	// MAY EXIST. It does not claim a further match was seen: proving none
	// exists means scanning the whole substrate in exactly the case the bound
	// existed to stop early.
	MatchesTruncated bool `json:"matches_truncated"`
	// ScanIncomplete says part of the substrate was never looked at. The answer
	// is unreliable and an ABSENCE OF HITS PROVES NOTHING.
	ScanIncomplete bool `json:"scan_incomplete"`

	// MaxMatches and MaxFilesScanned are the bounds actually in force.
	MaxMatches      int `json:"max_matches"`
	MaxFilesScanned int `json:"max_files_scanned"`
}

// searchCandidate is one tracked file in scope, before any substrate rule runs.
type searchCandidate struct {
	path string
	hash plumbing.Hash
}

// Search returns matching lines from the tracked files of one commit.
//
// The substrate is the TREE at the pinned commit, never the state on disk:
// searching the worktree would make the reported commit a fiction and would let
// a hit fail to reproduce under a git_read at the same commit.
//
// Traversal order is part of the contract — full path in byte order, then line
// number ascending — because a bound and determinism compose only if the bound
// takes a prefix of a defined total order and truncation always cuts its tail.
func (a *Adapter) Search(ctx context.Context, opts SearchOptions) (*SearchResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if !a.connected {
		return nil, fmt.Errorf("adapter not connected")
	}

	re, err := compileSearchPattern(opts)
	if err != nil {
		return nil, err
	}

	maxMatches := clampBound(opts.MaxMatches, searchDefaultMaxMatches, searchMaxMatchesCeiling)
	maxFiles := clampBound(opts.MaxFilesScanned, searchMaxFilesScanned, searchMaxFilesScanned)

	commit, err := a.resolvePinnedCommit(opts.Commit)
	if err != nil {
		return nil, err
	}

	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("get tree: %w", err)
	}

	prefix := normalizePathPrefix(opts.PathPrefix)
	candidates, err := collectSearchCandidates(tree, prefix)
	if err != nil {
		return nil, err
	}

	res := &SearchResult{
		Commit:          commit.Hash.String(),
		PathPrefix:      prefix,
		Matches:         []SearchMatch{},
		FilesInScope:    len(candidates),
		MaxMatches:      maxMatches,
		MaxFilesScanned: maxFiles,
	}

	for i, c := range candidates {
		if i%searchCancelCheckInterval == 0 {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
		}
		if res.FilesConsidered >= maxFiles {
			break
		}
		res.FilesConsidered++

		blob, err := object.GetBlob(a.repo.Storer, c.hash)
		if err != nil {
			return nil, fmt.Errorf("read blob for %q: %w", c.path, err)
		}
		if blob.Size > maxFileSize {
			// Excluded, and counted. A hit here could never become a citation:
			// git_read refuses the same files at the same threshold
			// (read.go's maxFileSize), so a lead in one is unfollowable.
			res.SkippedLarge++
			continue
		}

		content, err := readBlob(blob)
		if err != nil {
			return nil, fmt.Errorf("read file %q: %w", c.path, err)
		}
		if isBinary(content) {
			res.SkippedBinary++
			continue
		}
		res.FilesSearched++

		if scanFile(re, c.path, content, res, maxMatches) {
			break
		}
	}

	res.deriveExhaustionMarkers()
	return res, nil
}

// deriveExhaustionMarkers computes both markers from counters the result
// already carries, rather than setting each at the site that stopped the scan.
//
// That shape is the fix for a real defect, not a refactor. Two flags set in two
// branches diverged from what the counters said: whichever bound bit set its
// own flag and left the other at its zero value, so a scan stopped by the
// OUTPUT bound reported scan_incomplete false with files unvisited — advertised
// as "part of the repository was never looked at", and false exactly when the
// caller most needs it. Derived here, both are total functions of the result
// and no branch can forget one.
func (r *SearchResult) deriveExhaustionMarkers() {
	r.ScanIncomplete = r.FilesConsidered < r.FilesInScope
	r.MatchesTruncated = len(r.Matches) >= r.MaxMatches
}

// scanFile appends every matching line of one file, in ascending line order.
// It reports true when the output bound bit, which stops the scan. It sets no
// marker: the markers are derived from the counters once, after the loop.
func scanFile(re *regexp.Regexp, path string, content []byte, res *SearchResult, maxMatches int) bool {
	lineNo := 0
	for start := 0; start < len(content); {
		var seg []byte
		if nl := bytes.IndexByte(content[start:], '\n'); nl < 0 {
			seg = content[start:]
			start = len(content)
		} else {
			seg = content[start : start+nl]
			start += nl + 1
		}
		lineNo++

		if !re.Match(seg) {
			continue
		}
		truncated := false
		if len(seg) > searchMaxLineBytes {
			seg = seg[:searchMaxLineBytes]
			truncated = true
		}
		res.Matches = append(res.Matches, SearchMatch{
			Path:          path,
			Line:          lineNo,
			Text:          string(seg),
			TextTruncated: truncated,
		})
		if len(res.Matches) >= maxMatches {
			return true
		}
	}
	return false
}

// compileSearchPattern builds the matcher. The engine is RE2 (Go's regexp),
// whose linear-time guarantee is the point: it makes the work bound a function
// of substrate size rather than of pattern pathology, so a caller-supplied
// pattern cannot exhaust the bound on a small repo. Backreferences and
// lookaround are unavailable and are not needed.
func compileSearchPattern(opts SearchOptions) (*regexp.Regexp, error) {
	if opts.Pattern == "" {
		return nil, fmt.Errorf("empty search pattern")
	}
	expr := opts.Pattern
	if opts.Literal {
		expr = regexp.QuoteMeta(expr)
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		// Explicit failure, never zero hits: the same trap as an incomplete
		// scan, one layer up.
		return nil, fmt.Errorf("invalid search pattern %q: %w", opts.Pattern, err)
	}
	return re, nil
}

// collectSearchCandidates enumerates every tracked file at the commit under the
// prefix, sorted by full path in byte order. Enumeration is deliberately NOT
// bounded: the total order has to be known before a bound can take a prefix of
// it, and FilesInScope is what makes an incomplete scan measurable.
func collectSearchCandidates(tree *object.Tree, prefix string) ([]searchCandidate, error) {
	walker := object.NewTreeWalker(tree, true, nil)
	defer walker.Close()

	var candidates []searchCandidate
	for {
		name, entry, err := walker.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("walk tree: %w", err)
		}
		if !entry.Mode.IsFile() || !inPathScope(name, prefix) {
			continue
		}
		candidates = append(candidates, searchCandidate{path: name, hash: entry.Hash})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].path < candidates[j].path
	})
	return candidates, nil
}

// inPathScope reports whether a path is the scope or sits beneath it. The
// boundary is a path segment, so a prefix of "cmd" does not pull in "cmdline".
func inPathScope(path, prefix string) bool {
	if prefix == "" {
		return true
	}
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

func normalizePathPrefix(prefix string) string {
	prefix = strings.Trim(prefix, "/")
	if prefix == "." {
		return ""
	}
	return prefix
}

// isBinary is the substrate's stated exclusion rule: a NUL byte within the
// first searchBinarySniffBytes. It is written out here rather than delegated to
// a library's judgement so the substrate cannot vary invisibly.
func isBinary(content []byte) bool {
	head := content
	if len(head) > searchBinarySniffBytes {
		head = head[:searchBinarySniffBytes]
	}
	return bytes.IndexByte(head, 0) >= 0
}

func readBlob(blob *object.Blob) ([]byte, error) {
	reader, err := blob.Reader()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

// clampBound applies "the caller may lower, never raise past the ceiling".
func clampBound(requested, def, ceiling int) int {
	if requested <= 0 {
		requested = def
	}
	if requested > ceiling {
		return ceiling
	}
	return requested
}
