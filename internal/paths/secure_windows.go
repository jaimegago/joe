//go:build windows
// +build windows

package paths

import (
	"fmt"
	"os/user"
)

// getSecureHomeDir returns the user's home directory using system APIs that
// cannot be manipulated via environment variables. On Windows, this uses
// the Windows user profile directory.
func getSecureHomeDir() (string, error) {
	// Use os/user.Current() which uses Windows API on Windows
	currentUser, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("cannot get current user: %w", err)
	}
	
	if currentUser.HomeDir == "" {
		return "", fmt.Errorf("user home directory is empty")
	}
	
	return currentUser.HomeDir, nil
}
