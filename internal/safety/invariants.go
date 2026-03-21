package safety

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/jaimegago/joe/internal/paths"
)

// caseInsensitiveFS is true on operating systems with case-insensitive filesystems (macOS, Windows).
var caseInsensitiveFS = runtime.GOOS == "darwin" || runtime.GOOS == "windows"

// Self-protection invariants are hardcoded constraints that protect Joe from
// modifying its own configuration, killing itself, or bypassing safety checks.
// These cannot be overridden by policy files or LLM reasoning.

// IsPathAllowed checks if a file path is allowed for read/write operations.
// Returns an error if the path violates self-protection invariants.
//
// Blocked paths:
// - ~/.joe/ directory (config, database, safety policy)
// - Safety policy file specifically
//
// This function expects an absolute path. Callers should resolve relative paths
// and expand ~ before calling this function.
func IsPathAllowed(absPath string) error {
	// Get ~/.joe directory
	joeDir, err := paths.JoeDirPath()
	if err != nil {
		// If we can't determine Joe's config dir, deny access for safety
		return fmt.Errorf("cannot verify path safety: %w", err)
	}

	// Clean paths for comparison (handles .. traversal)
	cleanPath := filepath.Clean(absPath)
	cleanJoeDir := filepath.Clean(joeDir)

	// Resolve symlinks for both the input path and the Joe directory.
	// This prevents escape via symlinked parent dirs (e.g., /tmp/link-to-joe/file)
	// and ensures cleanJoeDir is the real path even if ~/.joe is itself a symlink.
	cleanPath = resolvePathSymlinks(cleanPath)
	cleanJoeDir = resolvePathSymlinks(cleanJoeDir)

	// Normalize case on case-insensitive filesystems (macOS, Windows)
	// to prevent bypass via /Users/alice/.JOE/ vs /Users/alice/.joe/
	if caseInsensitiveFS {
		cleanPath = strings.ToLower(cleanPath)
		cleanJoeDir = strings.ToLower(cleanJoeDir)
	}

	// Block any path within ~/.joe/
	if strings.HasPrefix(cleanPath, cleanJoeDir+string(filepath.Separator)) || cleanPath == cleanJoeDir {
		return &InvariantViolationError{
			Type:   "path_protection",
			Target: absPath,
			Reason: "Joe cannot read or write files within ~/.joe/ (self-protection)",
		}
	}

	// Extra check for safety policy file specifically (defense in depth)
	safetyPolicyPath := resolvePathSymlinks(filepath.Clean(filepath.Join(joeDir, PolicyFileName)))
	if caseInsensitiveFS {
		safetyPolicyPath = strings.ToLower(safetyPolicyPath)
	}
	if cleanPath == safetyPolicyPath {
		return &InvariantViolationError{
			Type:   "path_protection",
			Target: absPath,
			Reason: "Joe cannot access its safety policy file (self-protection)",
		}
	}

	return nil
}

// IsCommandAllowed checks if a command is allowed to run.
// Returns an error if the command violates self-protection invariants.
//
// Blocked commands (hardcoded):
// - joe, joe-core (cannot restart/modify self)
// - kill, pkill, killall (cannot kill self or other processes)
//
// Note: This only blocks the base command. The policy's run_command allowlist
// provides the primary authorization. This function adds an extra layer for
// commands that should NEVER be allowed regardless of policy.
func IsCommandAllowed(command string) error {
	// Extract base command name (remove path)
	baseCmd := filepath.Base(command)

	// Block Joe binaries
	if baseCmd == "joe" || baseCmd == "joe-core" {
		return &InvariantViolationError{
			Type:   "command_protection",
			Target: command,
			Reason: "Joe cannot execute joe or joe-core commands (self-protection)",
		}
	}

	// Block process killing commands
	blockedKillCommands := map[string]bool{
		"kill":    true,
		"pkill":   true,
		"killall": true,
	}

	if blockedKillCommands[baseCmd] {
		return &InvariantViolationError{
			Type:   "command_protection",
			Target: command,
			Reason: "Joe cannot execute process termination commands (self-protection)",
		}
	}

	return nil
}

// InvariantViolationError is returned when an operation violates self-protection
// invariants. These errors cannot be bypassed by policy configuration.
type InvariantViolationError struct {
	Type   string // "path_protection" or "command_protection"
	Target string // The path or command that was blocked
	Reason string // Human-readable explanation
}

func (e *InvariantViolationError) Error() string {
	return fmt.Sprintf("safety invariant violation (%s): %s - %s", e.Type, e.Target, e.Reason)
}

// IsInvariantViolation checks if an error is an InvariantViolationError.
// Uses errors.As to correctly unwrap wrapped errors.
func IsInvariantViolation(err error) bool {
	var e *InvariantViolationError
	return errors.As(err, &e)
}

// IsWritePathInAllowedDir checks if absPath falls under one of the allowed
// directories. If allowedDirs is empty, all paths are permitted (no sandboxing).
// This is called AFTER IsPathAllowed (self-protection), providing an additional
// layer that restricts where write_file can operate.
func IsWritePathInAllowedDir(absPath string, allowedDirs []string) error {
	if len(allowedDirs) == 0 {
		return nil // no sandbox configured — allow all
	}

	cleanPath := filepath.Clean(absPath)

	// Resolve symlinks — including parent directories for paths that don't exist
	// yet. This prevents escape via symlinked parent dirs.
	cleanPath = resolvePathSymlinks(cleanPath)
	if caseInsensitiveFS {
		cleanPath = strings.ToLower(cleanPath)
	}

	for _, dir := range allowedDirs {
		cleanDir := resolvePathSymlinks(filepath.Clean(dir))
		if caseInsensitiveFS {
			cleanDir = strings.ToLower(cleanDir)
		}
		if strings.HasPrefix(cleanPath, cleanDir+string(filepath.Separator)) || cleanPath == cleanDir {
			return nil
		}
	}

	return &InvariantViolationError{
		Type:   "path_sandbox",
		Target: absPath,
		Reason: fmt.Sprintf("path is outside allowed directories: %v", allowedDirs),
	}
}

// resolvePathSymlinks resolves symlinks in a path, handling the case where the
// file doesn't exist yet. When the full path doesn't exist, it walks up to the
// nearest existing ancestor, resolves symlinks there, and appends the remaining
// components. This prevents escape via symlinked parent directories.
//
// Example: /tmp/symlink-to-etc/newfile.txt
//   - /tmp/symlink-to-etc/newfile.txt doesn't exist → EvalSymlinks fails
//   - /tmp/symlink-to-etc exists and is a symlink to /etc → resolves to /etc
//   - Result: /etc/newfile.txt (correctly reveals the real destination)
func resolvePathSymlinks(cleanPath string) string {
	// Try the full path first (fast path for existing files)
	if resolved, err := filepath.EvalSymlinks(cleanPath); err == nil {
		return resolved
	}

	// Walk up to find the nearest existing ancestor
	remaining := ""
	current := cleanPath
	for {
		parent := filepath.Dir(current)
		if parent == current {
			// Reached root without finding existing path
			break
		}
		remaining = filepath.Join(filepath.Base(current), remaining)
		current = parent

		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			return filepath.Join(resolved, remaining)
		}
	}

	// Nothing resolvable — return the cleaned path as-is
	return cleanPath
}
