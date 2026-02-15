package paths

import (
	"fmt"
	"path/filepath"
)

const (
	// Joe directory and files
	JoeDir        = ".joe"
	ConfigFile    = "config.yaml"
	DatabaseFile  = "joe.db"
	DatabaseFlags = "?_foreign_keys=on"
)

// DefaultConfigPath returns the default configuration file path.
// Returns ~/.joe/config.yaml using secure home directory resolution.
func DefaultConfigPath() string {
	home, err := getSecureHomeDir()
	if err != nil {
		// Fallback to tilde expansion if secure method fails
		return filepath.Join("~", JoeDir, ConfigFile)
	}
	return filepath.Join(home, JoeDir, ConfigFile)
}

// JoeDirPath returns the .joe directory path in the user's home.
// Uses getSecureHomeDir() which bypasses HOME environment variable to prevent
// security bypass attacks where an attacker sets HOME=/tmp/fake.
func JoeDirPath() (string, error) {
	home, err := getSecureHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, JoeDir), nil
}

// SecureHomeDir returns the user's home directory using system APIs that
// bypass the HOME environment variable. This is the exported version of
// getSecureHomeDir for use by other packages that need secure home resolution.
func SecureHomeDir() (string, error) {
	return getSecureHomeDir()
}

// DatabasePath returns the full path to the Joe database file.
func DatabasePath() (string, error) {
	dir, err := JoeDirPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, DatabaseFile), nil
}
