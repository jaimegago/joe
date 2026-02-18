//go:build darwin || linux
// +build darwin linux

package paths

import (
	"fmt"
	"os/user"
)

// getSecureHomeDir returns the user's home directory using system APIs that
// cannot be manipulated via environment variables. This prevents HOME env var
// bypass attacks where an attacker could set HOME=/tmp/fake to bypass protection.
// It is a variable to allow overriding in tests.
var getSecureHomeDir = func() (string, error) {
	// Use os/user.Current() which uses getpwuid_r() on Unix
	// This reads from /etc/passwd based on UID, ignoring environment variables
	currentUser, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("cannot get current user: %w", err)
	}

	if currentUser.HomeDir == "" {
		return "", fmt.Errorf("user home directory is empty")
	}

	return currentUser.HomeDir, nil
}
