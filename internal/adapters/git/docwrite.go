package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

// DocAuthConfig holds authentication credentials for a Git write operation.
type DocAuthConfig struct {
	AuthType   string // "none", "https", "ssh"
	HTTPToken  string // used when AuthType == "https"
	SSHKeyPath string // used when AuthType == "ssh"
}

// CommitAndPush writes content to filePath in the given repository, commits it,
// and pushes to the remote. It clones the repo to a temporary directory.
func CommitAndPush(ctx context.Context, repoURL, branch, filePath, content, commitMsg string, auth DocAuthConfig) error {
	tmpDir, err := os.MkdirTemp("", "joe-docwrite-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	authMethod, err := buildDocAuth(auth)
	if err != nil {
		return fmt.Errorf("build auth: %w", err)
	}

	cloneOpts := &gogit.CloneOptions{
		URL:      repoURL,
		Auth:     authMethod,
		Depth:    1,
		Progress: nil,
	}
	if branch != "" {
		cloneOpts.ReferenceName = plumbing.NewBranchReferenceName(branch)
		cloneOpts.SingleBranch = true
	}

	repo, err := gogit.PlainCloneContext(ctx, tmpDir, false, cloneOpts)
	if err != nil {
		return fmt.Errorf("clone repo: %w", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("get worktree: %w", err)
	}

	// Write file.
	fullPath := filepath.Join(tmpDir, filePath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return fmt.Errorf("create parent dirs: %w", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	// git add
	if _, err := wt.Add(filePath); err != nil {
		return fmt.Errorf("git add: %w", err)
	}

	// git commit
	if commitMsg == "" {
		commitMsg = fmt.Sprintf("docs: update %s via Joe", filePath)
	}
	if _, err = wt.Commit(commitMsg, &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  "Joe",
			Email: "joe@localhost",
			When:  time.Now(),
		},
	}); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	// git push
	if err := repo.PushContext(ctx, &gogit.PushOptions{Auth: authMethod}); err != nil && err != gogit.NoErrAlreadyUpToDate {
		return fmt.Errorf("git push: %w", err)
	}

	return nil
}

func buildDocAuth(auth DocAuthConfig) (transport.AuthMethod, error) {
	switch auth.AuthType {
	case "ssh":
		if auth.SSHKeyPath == "" {
			return nil, fmt.Errorf("ssh_key_path required for ssh auth")
		}
		keys, err := gitssh.NewPublicKeysFromFile("git", auth.SSHKeyPath, "")
		if err != nil {
			return nil, fmt.Errorf("load ssh key: %w", err)
		}
		return keys, nil
	case "https":
		if auth.HTTPToken == "" {
			return nil, fmt.Errorf("http_token required for https auth")
		}
		return &githttp.BasicAuth{
			Username: "joe",
			Password: auth.HTTPToken,
		}, nil
	default:
		return nil, nil
	}
}
