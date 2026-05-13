package skills

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// DefaultDebounce is the window over which filesystem events are coalesced
// before a reload runs. Git pulls and the installer's atomic-rename produce
// flurries of events; without coalescing the watcher would re-parse the world
// many times per install.
const DefaultDebounce = 750 * time.Millisecond

// ReloadResult captures the before/after state of one reload pass for
// observability and API responses.
type ReloadResult struct {
	// At is the wall-clock time the reload completed.
	At time.Time
	// Trigger names what caused the reload: "fsnotify", "manual", "startup".
	Trigger string
	// Before is the number of skills loaded prior to this reload.
	Before int
	// After is the number of skills loaded after this reload.
	After int
	// Added/Removed list skill name diffs in sorted order. Names unchanged
	// between Before and After are not listed.
	Added   []string
	Removed []string
	// Updated lists skill names whose content hash changed across the reload.
	Updated []string
	// Err is non-nil when the reload failed (validation, walk error, etc.).
	// On failure the previous registry stays active — see Reload.
	Err error
}

// Watcher monitors a skills root directory for filesystem changes and atomically
// republishes the in-memory router whenever the on-disk state shifts.
//
// Validation failures during a reload do NOT activate the new content: the
// last successful router stays in place so a malformed SKILL.md cannot break
// Joe's reasoning loop.
type Watcher struct {
	root     string
	router   *AtomicRouter
	debounce time.Duration

	fs *fsnotify.Watcher

	mu sync.Mutex // serialises reloads so two triggers cannot race

	last atomicResult // last reload outcome, exposed via LastReload()
}

// WatcherOption customises a Watcher at construction time.
type WatcherOption func(*Watcher)

// WithDebounce overrides the event-coalescing window. Useful in tests to
// shrink the window to a few milliseconds.
func WithDebounce(d time.Duration) WatcherOption {
	return func(w *Watcher) {
		if d > 0 {
			w.debounce = d
		}
	}
}

// NewWatcher constructs a Watcher rooted at `root` that publishes reloads
// into `router`. The fsnotify watcher is created here but no goroutine is
// started until Run is called.
//
// The root directory is created if missing — operating on a nonexistent skills
// directory is the expected state on fresh installs and should not block
// startup.
func NewWatcher(root string, router *AtomicRouter, opts ...WatcherOption) (*Watcher, error) {
	if router == nil {
		return nil, errors.New("skills watcher: router is required")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("skills watcher: create root: %w", err)
	}
	w := &Watcher{
		root:     root,
		router:   router,
		debounce: DefaultDebounce,
	}
	for _, opt := range opts {
		opt(w)
	}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("skills watcher: fsnotify init: %w", err)
	}
	w.fs = fsw
	if err := w.addRecursive(); err != nil {
		_ = fsw.Close()
		return nil, fmt.Errorf("skills watcher: register watches: %w", err)
	}
	return w, nil
}

// Run blocks until ctx is canceled, watching for changes under root and
// republishing the router after each debounced batch of events. Returns nil
// on clean shutdown; an error if the underlying fsnotify watcher dies.
func (w *Watcher) Run(ctx context.Context) error {
	defer w.fs.Close()

	var (
		debounceTimer *time.Timer
		debounceC     <-chan time.Time
	)
	armDebounce := func() {
		if debounceTimer == nil {
			debounceTimer = time.NewTimer(w.debounce)
			debounceC = debounceTimer.C
			return
		}
		if !debounceTimer.Stop() {
			// Drain the channel only if it has a pending fire we haven't
			// consumed yet. Reset() docs require this to avoid spurious
			// firings on the next arm.
			select {
			case <-debounceTimer.C:
			default:
			}
		}
		debounceTimer.Reset(w.debounce)
		debounceC = debounceTimer.C
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case err, ok := <-w.fs.Errors:
			if !ok {
				return nil
			}
			slog.Warn("skills watcher: fsnotify error", "error", err)
		case ev, ok := <-w.fs.Events:
			if !ok {
				return nil
			}
			// Ignore events on the lockfile itself: install/remove rewrites it,
			// and we already pick the new skills up via their own events.
			if filepath.Base(ev.Name) == LockfileName {
				continue
			}
			// Ignore events under the staging directory — staged installs are
			// not yet active and we don't want partial-clone churn to trigger
			// reloads.
			rel, _ := filepath.Rel(w.root, ev.Name)
			if rel != "" && strings.HasPrefix(rel, stagingDirName) {
				continue
			}
			armDebounce()
		case <-debounceC:
			debounceC = nil
			debounceTimer = nil
			result := w.Reload(ctx, "fsnotify")
			if result.Err != nil {
				slog.Warn("skills watcher: reload failed; keeping previous registry",
					"error", result.Err,
					"trigger", result.Trigger,
				)
			}
			// New directories may have appeared (e.g. after `joe skills
			// install` rename). Sync watches even if the reload errored so
			// future changes still fire events.
			if err := w.addRecursive(); err != nil {
				slog.Warn("skills watcher: re-add watches failed", "error", err)
			}
		}
	}
}

// Reload performs a synchronous rescan of the skills root and republishes
// the router. Returns the reload outcome. On validation failure the previous
// router stays active and the returned ReloadResult.Err is non-nil.
//
// Reload is safe to call concurrently with the watcher's Run loop and with
// itself; calls are serialised internally.
func (w *Watcher) Reload(_ context.Context, trigger string) ReloadResult {
	w.mu.Lock()
	defer w.mu.Unlock()

	result := ReloadResult{At: time.Now().UTC(), Trigger: trigger}

	prev := w.router.Snapshot()
	prevNames, prevHashes := registrySummary(prev)
	result.Before = len(prevNames)

	reg, err := LoadDir(w.root)
	if err != nil {
		result.Err = err
		w.last.store(result)
		return result
	}
	router := NewRouter(reg)
	newNames, newHashes := registrySummary(router)
	result.After = len(newNames)
	result.Added, result.Removed, result.Updated = diffSkills(prevNames, prevHashes, newNames, newHashes)

	w.router.Set(router)

	if result.Before != result.After || len(result.Added)+len(result.Removed)+len(result.Updated) > 0 {
		slog.Info("skill_audit",
			"event", "reload",
			"trigger", trigger,
			"before", result.Before,
			"after", result.After,
			"added", result.Added,
			"removed", result.Removed,
			"updated", result.Updated,
		)
	} else {
		slog.Debug("skills reload (no changes)", "trigger", trigger, "count", result.After)
	}
	w.last.store(result)
	return result
}

// LastReload returns metadata about the most recent reload pass. Useful for
// status endpoints. The zero value is returned before the first reload.
func (w *Watcher) LastReload() ReloadResult {
	if w == nil {
		return ReloadResult{}
	}
	return w.last.load()
}

// Close stops the watcher and releases its fsnotify resources. Safe to call
// after Run has returned; the underlying watcher is already closed in that
// case and the second Close is a no-op.
func (w *Watcher) Close() error {
	if w == nil || w.fs == nil {
		return nil
	}
	return w.fs.Close()
}

// addRecursive walks the skills root and ensures every directory is being
// watched. fsnotify.Add is idempotent: re-adding an already-watched directory
// returns nil, so we can call this on every reload without bookkeeping.
//
// Hidden directories (notably `.staging` and `.git`) are skipped so partial
// clones and git internals never trigger spurious reloads.
func (w *Watcher) addRecursive() error {
	return filepath.WalkDir(w.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Race: file removed between walk start and the visit. Not fatal.
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if path != w.root && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if err := w.fs.Add(path); err != nil {
			slog.Warn("skills watcher: add dir failed", "path", path, "error", err)
		}
		return nil
	})
}

// registrySummary extracts the sorted skill names and their content hashes
// from a router's registry. Both maps are keyed by skill name. Used by
// Reload() to compute diffs.
func registrySummary(r *Router) ([]string, map[string]string) {
	if r == nil || r.Registry() == nil {
		return nil, map[string]string{}
	}
	skills := r.Registry().All()
	names := make([]string, len(skills))
	hashes := make(map[string]string, len(skills))
	for i, s := range skills {
		names[i] = s.Name
		hashes[s.Name] = s.Hash
	}
	sort.Strings(names)
	return names, hashes
}

// diffSkills returns (added, removed, updated) skill name slices, all sorted.
// "updated" means a skill with the same name has a different content hash.
func diffSkills(beforeNames []string, beforeHashes map[string]string, afterNames []string, afterHashes map[string]string) (added, removed, updated []string) {
	beforeSet := make(map[string]struct{}, len(beforeNames))
	for _, n := range beforeNames {
		beforeSet[n] = struct{}{}
	}
	afterSet := make(map[string]struct{}, len(afterNames))
	for _, n := range afterNames {
		afterSet[n] = struct{}{}
	}
	for _, n := range afterNames {
		if _, had := beforeSet[n]; !had {
			added = append(added, n)
			continue
		}
		if beforeHashes[n] != afterHashes[n] {
			updated = append(updated, n)
		}
	}
	for _, n := range beforeNames {
		if _, has := afterSet[n]; !has {
			removed = append(removed, n)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(updated)
	return added, removed, updated
}

// atomicResult is a tiny mutex-protected holder for the last ReloadResult.
// We avoid sync/atomic.Pointer here because ReloadResult contains slices,
// which cannot be stored in an atomic.Value safely across types.
type atomicResult struct {
	mu     sync.RWMutex
	result ReloadResult
}

func (a *atomicResult) store(r ReloadResult) {
	a.mu.Lock()
	a.result = r
	a.mu.Unlock()
}

func (a *atomicResult) load() ReloadResult {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.result
}
