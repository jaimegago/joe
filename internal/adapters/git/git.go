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

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/credential"
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
	Search(ctx context.Context, opts SearchOptions) (*SearchResult, error)
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

func (a *Adapter) Connect(ctx context.Context, source store.Component) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// go-git's network operations honour a context deadline but impose no default
	// timeout of their own; a clone/pull against an unreachable remote would
	// otherwise block forever while a.mu is held. Ensure a bound exists.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}

	cfg, err := ParseConfig(source.Config)
	if err != nil {
		return fmt.Errorf("parse component config: %w", err)
	}
	a.config = cfg

	// Resolve the credential at USE TIME through the provider seam, the same way
	// every other credential-wired adapter does (D-0026 / D-0150). The armed
	// config's credential_provider discriminator selects the provider: `static`
	// yields an HTTPS token resolved from the referenced environment variable,
	// `none` yields no credential at all and the clone proceeds anonymously. An
	// unpromoted component has no discriminator, so Select defaults to the static
	// provider, which resolves an empty value — anonymous, and the clone fails at
	// the remote if the repository is in fact private.
	auth, err := resolveAuth(ctx, source)
	if err != nil {
		return fmt.Errorf("resolve credential: %w", err)
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
		repo, err = gogit.PlainCloneContext(ctx, repoPath, false, cloneOpts)
		if err != nil {
			return fmt.Errorf("clone repo: %w", err)
		}
	} else {
		wt, err := repo.Worktree()
		if err != nil {
			return fmt.Errorf("get worktree: %w", err)
		}
		pullOpts := &gogit.PullOptions{Auth: auth}
		if pullErr := wt.PullContext(ctx, pullOpts); pullErr != nil && pullErr != gogit.NoErrAlreadyUpToDate {
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

// resolveAuth resolves the component's credential through the provider seam and
// converts it into a go-git transport.AuthMethod.
//
// A nil AuthMethod means an ANONYMOUS clone, which is the correct and intended
// outcome for a component armed with the no-credential kind. A resolved static
// value becomes HTTPS basic auth carrying the token as the password — the
// convention every major forge accepts for a personal access token.
//
// The credential value never leaves this function: it flows from the typed
// accessor straight into the auth method, and a resolution failure surfaces only
// the provider's non-sensitive diagnostic reason.
func resolveAuth(ctx context.Context, source store.Component) (transport.AuthMethod, error) {
	provider, err := credential.Select(source.Config)
	if err != nil {
		return nil, fmt.Errorf("select credential provider: %w", err)
	}
	res, err := provider.Resolve(ctx, source.ID, source.Config)
	if err != nil {
		return nil, err
	}
	if !res.Diagnostic.OK {
		// Non-sensitive reason only; the credential value never enters this error.
		return nil, fmt.Errorf("%s", res.Diagnostic.Reason)
	}
	token, ok := res.StaticValue()
	if !ok || token == "" {
		// No credential resolved: the no-credential arm, or an unpromoted
		// component. Clone anonymously.
		return nil, nil
	}
	return &githttp.BasicAuth{Username: "joe", Password: token}, nil
}
