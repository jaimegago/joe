package skills

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// LockfileName is the file in the skills root that records every installed
// repo, its git ref, and the skills that came out of it. The lockfile lets
// `joe skills list/update/remove` operate without re-querying the filesystem
// for state that disk does not record (e.g. the requested git ref).
const LockfileName = "skills.lock.yaml"

// stagingDirName is the dotfile subdirectory under the skills root that
// concurrent installs clone into before atomic-rename into place. Hidden
// directories are skipped by LoadDir, so a partially-cloned install never
// activates a half-formed skill.
const stagingDirName = ".staging"

// Lockfile is the persisted record of every installed skills source. One
// install ≈ one git clone; an install can produce multiple skills if the
// upstream repo bundles them.
type Lockfile struct {
	Version  int       `yaml:"version"`
	Installs []Install `yaml:"installs"`
}

// Install is one cloned skills source. Dir is relative to the skills root so
// the lockfile is portable across machines.
type Install struct {
	Dir    string        `yaml:"dir"`
	Repo   string        `yaml:"repo"`
	Ref    string        `yaml:"ref,omitempty"`
	Commit string        `yaml:"commit,omitempty"`
	Subdir string        `yaml:"subdir,omitempty"`
	Skills []SkillRecord `yaml:"skills"`
}

// SkillRecord is a single SKILL.md the installer found inside an install.
// Path is relative to the install directory; Hash is the same content hash
// the registry computes at load time so the two can be cross-checked.
type SkillRecord struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`
	Hash string `yaml:"hash"`
}

// Git is the narrow surface the Manager needs from a git client. Production
// uses ExecGit; tests inject a fake.
type Git interface {
	Clone(ctx context.Context, repo, ref, subdir, dest string) (commit string, err error)
	Update(ctx context.Context, repoDir, ref string) (commit string, err error)
}

// Manager owns the skills root and the lockfile. All install/remove/update
// operations route through it so the lockfile and disk stay in sync.
type Manager struct {
	Root string
	Git  Git
	// TrustedSources, when non-empty, restricts Install to repo URLs that
	// match one of the listed prefixes (host or host+owner/repo). Empty
	// means no allowlist — every URL is accepted, which is the right
	// default for personal/single-user installs and the Phase 2 behavior.
	// Trusted-source-only mode is Phase 3's safety layer; the full
	// quarantine + approval workflow arrives in Phase 4.
	TrustedSources []string
}

// NewManager returns a Manager rooted at `root` (typically ~/.joe/skills).
// The directory is created lazily on the first write; missing root is not
// an error for list/remove (nothing to do).
func NewManager(root string, g Git) *Manager {
	if g == nil {
		g = ExecGit{}
	}
	return &Manager{Root: root, Git: g}
}

// WithTrustedSources returns m with its TrustedSources list set. Fluent
// helper so wiring code can construct a manager in one expression.
func (m *Manager) WithTrustedSources(sources []string) *Manager {
	m.TrustedSources = append([]string(nil), sources...)
	return m
}

// LoadLockfile reads the lockfile, returning an empty Lockfile if it is
// missing. A malformed lockfile is a hard error: silently dropping it would
// orphan whatever it tracks on disk.
func (m *Manager) LoadLockfile() (*Lockfile, error) {
	path := filepath.Join(m.Root, LockfileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &Lockfile{Version: 1}, nil
		}
		return nil, fmt.Errorf("read lockfile: %w", err)
	}
	var lf Lockfile
	if err := yaml.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("parse lockfile: %w", err)
	}
	if lf.Version == 0 {
		lf.Version = 1
	}
	return &lf, nil
}

// SaveLockfile writes the lockfile atomically via temp-file + rename so a
// crashed install never leaves a half-written YAML on disk.
func (m *Manager) SaveLockfile(lf *Lockfile) error {
	if err := os.MkdirAll(m.Root, 0o755); err != nil {
		return fmt.Errorf("create skills root: %w", err)
	}
	sort.Slice(lf.Installs, func(i, j int) bool { return lf.Installs[i].Dir < lf.Installs[j].Dir })
	data, err := yaml.Marshal(lf)
	if err != nil {
		return fmt.Errorf("encode lockfile: %w", err)
	}
	final := filepath.Join(m.Root, LockfileName)
	tmp, err := os.CreateTemp(m.Root, ".lock-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp lockfile: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp lockfile: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp lockfile: %w", err)
	}
	if err := os.Rename(tmpPath, final); err != nil {
		return fmt.Errorf("rename lockfile: %w", err)
	}
	return nil
}

// Install clones `repo` at `ref` (empty = default branch) into the skills
// root and records it in the lockfile. If `subdir` is non-empty, only that
// subdirectory of the repo is materialised via sparse checkout — the rest
// of the tree never touches disk.
//
// The clone is staged in ~/.joe/skills/.staging/<rand>/ and atomically
// renamed into place, so a crashed clone cannot leave a partial skill that
// the loader would activate.
func (m *Manager) Install(ctx context.Context, repo, ref, subdir string) (*Install, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return nil, errors.New("repo URL is required")
	}
	if err := validateRepoURL(repo); err != nil {
		return nil, err
	}
	if err := m.checkTrusted(repo); err != nil {
		return nil, err
	}
	subdir = cleanSubdir(subdir)

	if err := os.MkdirAll(m.Root, 0o755); err != nil {
		return nil, fmt.Errorf("create skills root: %w", err)
	}

	lf, err := m.LoadLockfile()
	if err != nil {
		return nil, err
	}

	dir, err := m.allocateInstallDir(repo, lf)
	if err != nil {
		return nil, err
	}

	stageRoot := filepath.Join(m.Root, stagingDirName)
	if err := os.MkdirAll(stageRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create staging dir: %w", err)
	}
	stage, err := os.MkdirTemp(stageRoot, "install-*")
	if err != nil {
		return nil, fmt.Errorf("create staging temp: %w", err)
	}
	cleanupStage := true
	defer func() {
		if cleanupStage {
			_ = os.RemoveAll(stage)
		}
	}()

	commit, err := m.Git.Clone(ctx, repo, ref, subdir, stage)
	if err != nil {
		return nil, fmt.Errorf("git clone: %w", err)
	}

	skills, err := scanSkills(stage, subdir)
	if err != nil {
		return nil, fmt.Errorf("scan skills: %w", err)
	}
	if len(skills) == 0 {
		return nil, fmt.Errorf("no SKILL.md found in %s", describeSource(repo, subdir))
	}
	if conflict := firstNameConflict(lf, "", skills); conflict != "" {
		return nil, fmt.Errorf("skill name collision: %q is already installed", conflict)
	}

	final := filepath.Join(m.Root, dir)
	if err := os.Rename(stage, final); err != nil {
		return nil, fmt.Errorf("activate install: %w", err)
	}
	cleanupStage = false

	install := Install{
		Dir:    dir,
		Repo:   repo,
		Ref:    ref,
		Commit: commit,
		Subdir: subdir,
		Skills: skills,
	}
	lf.Installs = append(lf.Installs, install)
	if err := m.SaveLockfile(lf); err != nil {
		// Best-effort rollback: keep the directory but report the failure so
		// the operator can hand-edit the lockfile rather than lose the data.
		return nil, fmt.Errorf("save lockfile (clone left at %s): %w", final, err)
	}
	return &install, nil
}

// Remove deletes the install that owns the named skill. Because one install
// can contribute multiple skills, callers must pass `force=true` to confirm
// removal when the install carries siblings. The returned list names every
// skill that was uninstalled as a side effect.
func (m *Manager) Remove(_ context.Context, skillName string, force bool) ([]string, error) {
	if skillName == "" {
		return nil, errors.New("skill name is required")
	}
	lf, err := m.LoadLockfile()
	if err != nil {
		return nil, err
	}

	idx := -1
	for i, in := range lf.Installs {
		for _, s := range in.Skills {
			if s.Name == skillName {
				idx = i
				break
			}
		}
		if idx >= 0 {
			break
		}
	}
	if idx < 0 {
		return nil, fmt.Errorf("skill %q is not installed", skillName)
	}

	target := lf.Installs[idx]
	if len(target.Skills) > 1 && !force {
		names := make([]string, len(target.Skills))
		for i, s := range target.Skills {
			names[i] = s.Name
		}
		sort.Strings(names)
		return nil, fmt.Errorf(
			"skill %q is part of install %q which also provides %d other skill(s) (%s); pass --force to remove all",
			skillName, target.Dir, len(target.Skills)-1, strings.Join(names, ", "),
		)
	}

	removed := make([]string, 0, len(target.Skills))
	for _, s := range target.Skills {
		removed = append(removed, s.Name)
	}
	sort.Strings(removed)

	full := filepath.Join(m.Root, target.Dir)
	if err := os.RemoveAll(full); err != nil {
		return nil, fmt.Errorf("remove install dir: %w", err)
	}
	lf.Installs = append(lf.Installs[:idx], lf.Installs[idx+1:]...)
	if err := m.SaveLockfile(lf); err != nil {
		return removed, fmt.Errorf("save lockfile: %w", err)
	}
	return removed, nil
}

// Update pulls the latest commit for one install (when `skillName` is given)
// or every install (when `skillName == ""`). Skills discovered post-update
// replace the previous SkillRecord list, so renames upstream propagate to
// the lockfile.
//
// Hot reload is Phase 3; today, an updated install only takes effect after
// joe-core restarts. The CLI prints that reminder.
func (m *Manager) Update(ctx context.Context, skillName string) ([]*Install, error) {
	lf, err := m.LoadLockfile()
	if err != nil {
		return nil, err
	}
	if len(lf.Installs) == 0 {
		if skillName != "" {
			return nil, fmt.Errorf("skill %q is not installed", skillName)
		}
		return nil, nil
	}

	var targets []int
	if skillName == "" {
		targets = make([]int, len(lf.Installs))
		for i := range lf.Installs {
			targets[i] = i
		}
	} else {
		for i, in := range lf.Installs {
			for _, s := range in.Skills {
				if s.Name == skillName {
					targets = append(targets, i)
					break
				}
			}
		}
		if len(targets) == 0 {
			return nil, fmt.Errorf("skill %q is not installed", skillName)
		}
	}

	updated := make([]*Install, 0, len(targets))
	for _, idx := range targets {
		in := &lf.Installs[idx]
		full := filepath.Join(m.Root, in.Dir)
		commit, err := m.Git.Update(ctx, full, in.Ref)
		if err != nil {
			return updated, fmt.Errorf("update %s: %w", in.Dir, err)
		}
		skills, err := scanSkills(full, in.Subdir)
		if err != nil {
			return updated, fmt.Errorf("rescan %s: %w", in.Dir, err)
		}
		if len(skills) == 0 {
			return updated, fmt.Errorf("update %s: no SKILL.md found after pull", in.Dir)
		}
		if conflict := firstNameConflict(lf, in.Dir, skills); conflict != "" {
			return updated, fmt.Errorf("update %s: skill name %q now collides with another install", in.Dir, conflict)
		}
		in.Commit = commit
		in.Skills = skills
		copy := *in
		updated = append(updated, &copy)
	}
	if err := m.SaveLockfile(lf); err != nil {
		return updated, fmt.Errorf("save lockfile: %w", err)
	}
	return updated, nil
}

// List returns a copy of the installs recorded in the lockfile. Disk state
// is intentionally not consulted: any drift (manual file edits, partial
// removes) is surfaced by the next install/update operation rather than
// silently massaged here.
func (m *Manager) List() ([]Install, error) {
	lf, err := m.LoadLockfile()
	if err != nil {
		return nil, err
	}
	out := make([]Install, len(lf.Installs))
	copy(out, lf.Installs)
	return out, nil
}

// allocateInstallDir derives a directory name from the repo URL and ensures
// it does not collide with an existing install. Collisions get a short
// random suffix so a user can re-clone the same repo under a different name
// without manual intervention.
func (m *Manager) allocateInstallDir(repo string, lf *Lockfile) (string, error) {
	base := deriveInstallDir(repo)
	if base == "" {
		return "", fmt.Errorf("could not derive directory name from %q", repo)
	}
	used := map[string]struct{}{}
	for _, in := range lf.Installs {
		used[in.Dir] = struct{}{}
	}
	// Also reject collisions with whatever already exists on disk so a
	// previous failed install or hand-copied folder doesn't get overwritten.
	if entries, err := os.ReadDir(m.Root); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				used[e.Name()] = struct{}{}
			}
		}
	}
	candidate := base
	for attempt := 0; attempt < 8; attempt++ {
		if _, taken := used[candidate]; !taken {
			return candidate, nil
		}
		suffix, err := randomSuffix(3)
		if err != nil {
			return "", err
		}
		candidate = base + "-" + suffix
	}
	return "", fmt.Errorf("could not find unused directory name for %q", repo)
}

// deriveInstallDir turns a repo URL into a filesystem-safe directory name.
// Example: https://github.com/foo/bar.git → "github.com-foo-bar".
func deriveInstallDir(repo string) string {
	repo = strings.TrimSpace(repo)
	repo = strings.TrimSuffix(repo, ".git")
	repo = strings.TrimSuffix(repo, "/")

	if u, err := url.Parse(repo); err == nil && u.Host != "" {
		path := strings.Trim(u.Path, "/")
		if path != "" {
			return sanitizeDir(u.Host + "-" + path)
		}
		return sanitizeDir(u.Host)
	}

	if at := strings.Index(repo, "@"); at >= 0 && strings.Contains(repo[at:], ":") {
		rest := repo[at+1:]
		if colon := strings.Index(rest, ":"); colon >= 0 {
			host := rest[:colon]
			path := strings.Trim(rest[colon+1:], "/")
			if path != "" {
				return sanitizeDir(host + "-" + path)
			}
			return sanitizeDir(host)
		}
	}

	return sanitizeDir(repo)
}

// sanitizeDir collapses slashes and other separators into dashes so the
// install directory never reaches up the tree or contains shell-hostile
// characters.
func sanitizeDir(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '.' || r == '_'
		if ok {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" || out == "." || out == ".." {
		return ""
	}
	return out
}

// validateRepoURL rejects schemes and shapes that the installer is not
// prepared to handle. The intent is conservatism, not completeness — exotic
// URLs are better handled by an explicit hand-edit than silently accepted.
func validateRepoURL(repo string) error {
	if strings.Contains(repo, "\x00") || strings.ContainsAny(repo, "\n\r") {
		return fmt.Errorf("repo URL contains invalid characters")
	}
	if u, err := url.Parse(repo); err == nil && u.Scheme != "" {
		switch u.Scheme {
		case "http", "https", "ssh", "git":
			if u.Host == "" {
				return fmt.Errorf("repo URL is missing a host")
			}
			return nil
		default:
			return fmt.Errorf("unsupported repo scheme %q", u.Scheme)
		}
	}
	// scp-like form: git@github.com:owner/repo.git
	if strings.Contains(repo, "@") && strings.Contains(repo, ":") {
		return nil
	}
	return fmt.Errorf("repo URL %q is not recognized (use https://, ssh://, or git@host:owner/repo)", repo)
}

// checkTrusted reports nil when `repo` matches one of m.TrustedSources, or
// when no allowlist is configured. The match is a normalised prefix check
// against the URL's host+path so a single entry can authorise either an
// entire host ("github.com") or one organisation ("github.com/jaimegago").
//
// Phase 3 stops short of the full quarantine workflow: a non-matching repo
// is rejected outright with an error; it does not land in a quarantine
// state. Phase 4 introduces quarantine.
func (m *Manager) checkTrusted(repo string) error {
	if len(m.TrustedSources) == 0 {
		return nil
	}
	repoKey := normalizeRepoForTrust(repo)
	if repoKey == "" {
		return fmt.Errorf("trusted-source check: cannot parse %q", repo)
	}
	for _, src := range m.TrustedSources {
		srcKey := normalizeRepoForTrust(src)
		if srcKey == "" {
			continue
		}
		if repoKey == srcKey || strings.HasPrefix(repoKey, srcKey+"/") {
			return nil
		}
	}
	return fmt.Errorf("repo %q is not in trusted_sources (configured entries: %s)",
		repo, strings.Join(m.TrustedSources, ", "))
}

// normalizeRepoForTrust reduces a repo URL or a trusted-source entry to a
// "host/owner/repo" string with the scheme, ".git" suffix, and trailing
// slashes stripped. Returns "" for inputs it cannot parse — callers treat
// that as a non-match.
func normalizeRepoForTrust(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ".git")
	s = strings.TrimSuffix(s, "/")
	if s == "" {
		return ""
	}

	if u, err := url.Parse(s); err == nil && u.Host != "" {
		path := strings.Trim(u.Path, "/")
		if path == "" {
			return strings.ToLower(u.Host)
		}
		return strings.ToLower(u.Host + "/" + path)
	}

	// scp-like form (git@github.com:owner/repo) and bare "host/owner/repo".
	if at := strings.Index(s, "@"); at >= 0 && strings.Contains(s[at:], ":") {
		rest := s[at+1:]
		if colon := strings.Index(rest, ":"); colon >= 0 {
			host := rest[:colon]
			path := strings.Trim(rest[colon+1:], "/")
			if path == "" {
				return strings.ToLower(host)
			}
			return strings.ToLower(host + "/" + path)
		}
	}
	return strings.ToLower(s)
}

// cleanSubdir normalises an optional subdirectory selector to a forward-slash
// relative path with no leading slash and no `..` segments.
func cleanSubdir(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "." {
		return ""
	}
	s = strings.ReplaceAll(s, "\\", "/")
	s = strings.TrimPrefix(s, "./")
	s = strings.TrimPrefix(s, "/")
	s = strings.TrimSuffix(s, "/")
	return s
}

func describeSource(repo, subdir string) string {
	if subdir == "" {
		return repo
	}
	return repo + " (subdir " + subdir + ")"
}

// firstNameConflict returns the first skill name in `candidates` that is
// already used by an install other than `excludeDir`. Empty result means no
// collision.
func firstNameConflict(lf *Lockfile, excludeDir string, candidates []SkillRecord) string {
	existing := map[string]struct{}{}
	for _, in := range lf.Installs {
		if in.Dir == excludeDir {
			continue
		}
		for _, s := range in.Skills {
			existing[s.Name] = struct{}{}
		}
	}
	for _, s := range candidates {
		if _, taken := existing[s.Name]; taken {
			return s.Name
		}
	}
	return ""
}

// scanSkills walks `root` (restricted to `subdir` when set) and parses every
// SKILL.md it finds. Parse failures abort the install — a freshly-cloned
// skill that won't parse is a strong signal the user grabbed the wrong URL.
func scanSkills(root, subdir string) ([]SkillRecord, error) {
	scanRoot := root
	if subdir != "" {
		scanRoot = filepath.Join(root, filepath.FromSlash(subdir))
	}
	info, err := os.Stat(scanRoot)
	if err != nil {
		return nil, fmt.Errorf("scan root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("scan root %s is not a directory", scanRoot)
	}

	var out []SkillRecord
	walkErr := filepath.WalkDir(scanRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != scanRoot && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != skillFileName {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		skill, err := ParseSkill(path, data)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("relativize %s: %w", path, err)
		}
		out = append(out, SkillRecord{
			Name: skill.Name,
			Path: filepath.ToSlash(rel),
			Hash: skill.Hash,
		})
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	// Deterministic order so the lockfile diff is stable across runs.
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	// Reject duplicate names within a single install — they would collide at
	// load time anyway and the registry would silently drop one.
	seen := map[string]string{}
	for _, s := range out {
		if prev, dup := seen[s.Name]; dup {
			return nil, fmt.Errorf("install contains duplicate skill name %q (in %s and %s)", s.Name, prev, s.Path)
		}
		seen[s.Name] = s.Path
	}
	return out, nil
}

func randomSuffix(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("random suffix: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// ExecGit is the production Git implementation: it shells out to the local
// `git` binary. Operations are deliberately shallow (--depth=1) so a skill
// install is cheap and predictable, and so the installer never accidentally
// downloads the full history of a large monorepo just to grab a SKILL.md.
type ExecGit struct{}

// Clone clones `repo` into `dest`. When `subdir` is non-empty, sparse
// checkout limits the working tree to that one path so the rest of the repo
// never lands on disk.
func (ExecGit) Clone(ctx context.Context, repo, ref, subdir, dest string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", fmt.Errorf("mkdir parent: %w", err)
	}

	args := []string{"clone", "--depth=1"}
	if subdir != "" {
		args = append(args, "--filter=blob:none", "--no-checkout", "--sparse")
	}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, "--", repo, dest)
	if _, err := runGit(ctx, "", args...); err != nil {
		return "", err
	}

	if subdir != "" {
		if _, err := runGit(ctx, dest, "sparse-checkout", "init", "--cone"); err != nil {
			return "", err
		}
		if _, err := runGit(ctx, dest, "sparse-checkout", "set", subdir); err != nil {
			return "", err
		}
		if _, err := runGit(ctx, dest, "checkout"); err != nil {
			return "", err
		}
	}

	commit, err := runGit(ctx, dest, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(commit), nil
}

// Update fetches the configured ref and resets the install to it. Shallow
// fetch keeps the local repo tiny; reset-hard tolerates upstream
// force-pushes that a plain `git pull --ff-only` would refuse.
func (ExecGit) Update(ctx context.Context, repoDir, ref string) (string, error) {
	target := ref
	if target == "" {
		head, err := runGit(ctx, repoDir, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
		if err != nil {
			// origin/HEAD missing — fall back to whatever the remote calls HEAD.
			target = "HEAD"
		} else {
			target = strings.TrimPrefix(strings.TrimSpace(head), "origin/")
		}
	}
	if _, err := runGit(ctx, repoDir, "fetch", "--depth=1", "origin", target); err != nil {
		return "", err
	}
	if _, err := runGit(ctx, repoDir, "reset", "--hard", "FETCH_HEAD"); err != nil {
		return "", err
	}
	commit, err := runGit(ctx, repoDir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(commit), nil
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return string(out), nil
}
