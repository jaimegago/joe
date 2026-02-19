package local

import "github.com/jaimegago/joe/internal/paths"

// ExpandPath expands ~ to home directory and makes path absolute.
// Delegates to paths.ExpandPath which uses secure home directory resolution.
func ExpandPath(path string) (string, error) {
	return paths.ExpandPath(path)
}
