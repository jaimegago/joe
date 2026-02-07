package local

import (
	"os"
	"path/filepath"
	"strings"
)

// ExpandPath expands ~ to home directory and makes path absolute
func ExpandPath(path string) (string, error) {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if len(path) == 1 {
			path = home
		} else {
			path = filepath.Join(home, path[1:])
		}
	}
	return filepath.Abs(path)
}
