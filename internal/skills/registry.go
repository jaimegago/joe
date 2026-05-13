package skills

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// skillFileName is the canonical file every skill folder must contain.
const skillFileName = "SKILL.md"

// Registry is an in-memory collection of parsed skills, keyed by name. The
// zero value is safe to use and contains no skills, which is the correct
// behavior when ~/.joe/skills/ does not exist or is empty.
type Registry struct {
	skills []*Skill
	byName map[string]*Skill
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byName: map[string]*Skill{}}
}

// All returns a stable, name-sorted snapshot of every loaded skill. The slice
// is safe for the caller to retain; the underlying Skill pointers are shared.
func (r *Registry) All() []*Skill {
	if r == nil {
		return nil
	}
	out := make([]*Skill, len(r.skills))
	copy(out, r.skills)
	return out
}

// Len returns the number of loaded skills.
func (r *Registry) Len() int {
	if r == nil {
		return 0
	}
	return len(r.skills)
}

// Get returns the skill with the given name, or nil if not found.
func (r *Registry) Get(name string) *Skill {
	if r == nil {
		return nil
	}
	return r.byName[name]
}

// LoadDir walks `root` (typically ~/.joe/skills/) and loads every SKILL.md
// it finds. Each immediate or nested subdirectory that contains a SKILL.md is
// treated as one skill. Malformed skills and name collisions are logged and
// skipped — they do not abort the whole load.
//
// If `root` does not exist, LoadDir returns an empty registry without error.
// This is the expected state on a fresh install.
func LoadDir(root string) (*Registry, error) {
	reg := NewRegistry()

	info, err := os.Stat(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return reg, nil
		}
		return nil, fmt.Errorf("stat skills dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("skills path %q is not a directory", root)
	}

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			slog.Warn("skills: walk error", "path", path, "error", err)
			return nil
		}
		// Skip dotfile directories so the installer's `.staging/` (and any
		// other tooling-owned hidden dirs) cannot activate a half-cloned
		// skill mid-install.
		if d.IsDir() {
			if path != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != skillFileName {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("skills: read failed", "path", path, "error", err)
			return nil
		}

		skill, err := ParseSkill(path, data)
		if err != nil {
			slog.Warn("skills: parse failed", "path", path, "error", err)
			return nil
		}

		if existing, ok := reg.byName[skill.Name]; ok {
			slog.Warn("skills: duplicate name — skipping",
				"name", skill.Name,
				"existing", existing.Source,
				"duplicate", skill.Source,
			)
			return nil
		}

		reg.byName[skill.Name] = skill
		reg.skills = append(reg.skills, skill)
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk skills dir: %w", walkErr)
	}

	sort.Slice(reg.skills, func(i, j int) bool {
		return reg.skills[i].Name < reg.skills[j].Name
	})
	return reg, nil
}
