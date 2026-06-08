package git

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/paths"
	"github.com/jaimegago/joe/internal/store"
)

// FileInfo represents a file entry in the repo.
type FileInfo struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	IsDir bool   `json:"is_dir"`
}

// CommitInfo represents a commit entry.
type CommitInfo struct {
	Hash    string    `json:"hash"`
	Author  string    `json:"author"`
	Date    time.Time `json:"date"`
	Message string    `json:"message"`
}

// GitAdapter extends the base Adapter with git-specific operations.
type GitAdapter interface {
	adapters.Adapter
	ReadFile(ctx context.Context, path string) (string, error)
	ListFiles(ctx context.Context, dir string) ([]FileInfo, error)
	Log(ctx context.Context, limit int) ([]CommitInfo, error)
	Diff(ctx context.Context, fromRef, toRef string) (string, error)
}

// Adapter is the concrete git adapter using go-git.
type Adapter struct {
	mu        sync.RWMutex
	config    Config
	repo      *gogit.Repository
	repoPath  string
	connected bool
}

// New creates a new unconnected git adapter.
func New() *Adapter {
	return &Adapter{}
}

// NewWithRepo creates an adapter with a pre-opened repo (for testing).
func NewWithRepo(repo *gogit.Repository, repoPath string) *Adapter {
	return &Adapter{
		repo:      repo,
		repoPath:  repoPath,
		connected: true,
	}
}

func (a *Adapter) Connect(_ context.Context, source store.Component) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	cfg, err := ParseConfig(source.Config)
	if err != nil {
		return fmt.Errorf("parse source config: %w", err)
	}
	a.config = cfg

	auth, err := buildAuth(cfg)
	if err != nil {
		return fmt.Errorf("build auth: %w", err)
	}

	repoPath, err := repoDir(cfg.URL)
	if err != nil {
		return fmt.Errorf("determine repo dir: %w", err)
	}
	a.repoPath = repoPath

	repo, err := gogit.PlainOpen(repoPath)
	if err != nil {
		cloneOpts := &gogit.CloneOptions{
			URL:  cfg.URL,
			Auth: auth,
		}
		if cfg.Branch != "" && cfg.Branch != "HEAD" {
			cloneOpts.ReferenceName = plumbing.NewBranchReferenceName(cfg.Branch)
			cloneOpts.SingleBranch = true
		}
		repo, err = gogit.PlainClone(repoPath, false, cloneOpts)
		if err != nil {
			return fmt.Errorf("clone repo: %w", err)
		}
	} else {
		wt, err := repo.Worktree()
		if err != nil {
			return fmt.Errorf("get worktree: %w", err)
		}
		pullOpts := &gogit.PullOptions{Auth: auth}
		if pullErr := wt.Pull(pullOpts); pullErr != nil && pullErr != gogit.NoErrAlreadyUpToDate {
			return fmt.Errorf("pull repo: %w", pullErr)
		}
	}

	if _, err := repo.Head(); err != nil {
		return fmt.Errorf("verify repo HEAD: %w", err)
	}

	a.repo = repo
	a.connected = true
	return nil
}

func (a *Adapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.connected = false
	a.repo = nil
	return nil
}

func (a *Adapter) Status() adapters.Status {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.connected {
		return adapters.Status{Connected: true, Message: "connected"}
	}
	return adapters.Status{Connected: false, Message: "disconnected"}
}

// repoDir returns a deterministic local path for cloning.
func repoDir(repoURL string) (string, error) {
	joeDir, err := paths.JoeDirPath()
	if err != nil {
		return "", fmt.Errorf("get joe dir: %w", err)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(repoURL)))
	return filepath.Join(joeDir, "repos", hash[:16]), nil
}

// buildAuth creates a transport.AuthMethod from config.
func buildAuth(cfg Config) (transport.AuthMethod, error) {
	switch cfg.AuthType {
	case "ssh":
		if cfg.SSHKeyPath == "" {
			return nil, fmt.Errorf("ssh_key_path required for ssh auth")
		}
		keys, err := gitssh.NewPublicKeysFromFile("git", cfg.SSHKeyPath, "")
		if err != nil {
			return nil, fmt.Errorf("load ssh key: %w", err)
		}
		return keys, nil
	case "https":
		if cfg.HTTPToken == "" {
			return nil, fmt.Errorf("http_token required for https auth")
		}
		return &githttp.BasicAuth{
			Username: "joe",
			Password: cfg.HTTPToken,
		}, nil
	case "none", "":
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown auth_type: %q", cfg.AuthType)
	}
}
