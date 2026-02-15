package safety

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/jaimegago/joe/internal/paths"
)

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

	// Normalize case on case-insensitive filesystems (macOS, Windows)
	// to prevent bypass via /Users/alice/.JOE/ vs /Users/alice/.joe/
	if isCaseInsensitiveFS() {
		cleanPath = strings.ToLower(cleanPath)
		cleanJoeDir = strings.ToLower(cleanJoeDir)
	}

	// Resolve symlinks if the path exists — a symlink to ~/.joe/ must be caught.
	// If the path doesn't exist yet (e.g., write_file creating new file),
	// we check the cleaned path which is still safe since filepath.Clean
	// normalizes away .. segments.
	if resolved, err := filepath.EvalSymlinks(cleanPath); err == nil {
		cleanPath = resolved
		if isCaseInsensitiveFS() {
			cleanPath = strings.ToLower(cleanPath)
		}
	} else if !os.IsNotExist(err) {
		// EvalSymlinks failed for a reason other than "not found" — deny for safety
		return fmt.Errorf("cannot resolve path %s for safety check: %w", absPath, err)
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
	safetyPolicyPath := filepath.Clean(filepath.Join(joeDir, PolicyFileName))
	if isCaseInsensitiveFS() {
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
// - joe, joecored (cannot restart/modify self)
// - kill, pkill, killall (cannot kill self or other processes)
//
// Note: This only blocks the base command. The policy's run_command allowlist
// provides the primary authorization. This function adds an extra layer for
// commands that should NEVER be allowed regardless of policy.
func IsCommandAllowed(command string) error {
	// Extract base command name (remove path)
	baseCmd := filepath.Base(command)

	// Block Joe binaries
	if baseCmd == "joe" || baseCmd == "joecored" {
		return &InvariantViolationError{
			Type:   "command_protection",
			Target: command,
			Reason: "Joe cannot execute joe or joecored commands (self-protection)",
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
func IsInvariantViolation(err error) bool {
	_, ok := err.(*InvariantViolationError)
	return ok
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
	if isCaseInsensitiveFS() {
		cleanPath = strings.ToLower(cleanPath)
	}

	// Resolve symlinks if path exists
	if resolved, err := filepath.EvalSymlinks(cleanPath); err == nil {
		cleanPath = resolved
		if isCaseInsensitiveFS() {
			cleanPath = strings.ToLower(cleanPath)
		}
	}

	for _, dir := range allowedDirs {
		cleanDir := filepath.Clean(dir)
		if isCaseInsensitiveFS() {
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

// isCaseInsensitiveFS returns true if the operating system uses a case-insensitive
// filesystem by default (macOS, Windows). This is used to normalize paths for
// security checks to prevent case-based bypass attacks.
func isCaseInsensitiveFS() bool {
	return runtime.GOOS == "darwin" || runtime.GOOS == "windows"
}
