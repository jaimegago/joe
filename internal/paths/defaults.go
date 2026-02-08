package paths

import (
	"os"
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
// Returns ~/.joe/config.yaml
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join("~", JoeDir, ConfigFile)
	}
	return filepath.Join(home, JoeDir, ConfigFile)
}

// JoeDirPath returns the .joe directory path in the user's home.
func JoeDirPath() (string, error) {
	home := os.Getenv("HOME")
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(home, JoeDir), nil
}

// DatabasePath returns the full path to the Joe database file.
func DatabasePath() (string, error) {
	dir, err := JoeDirPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, DatabaseFile), nil
}
