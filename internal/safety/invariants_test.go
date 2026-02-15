package safety

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsPathAllowed(t *testing.T) {
	// Get actual ~/.joe directory for tests
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get home dir: %v", err)
	}
	joeDir := filepath.Join(home, ".joe")

	tests := []struct {
		name    string
		path    string
		wantErr bool
		errType string
	}{
		{
			name:    "allowed path in home",
			path:    filepath.Join(home, "test.txt"),
			wantErr: false,
		},
		{
			name:    "allowed path in tmp",
			path:    "/tmp/test.txt",
			wantErr: false,
		},
		{
			name:    "blocked: ~/.joe directory itself",
			path:    joeDir,
			wantErr: true,
			errType: "path_protection",
		},
		{
			name:    "blocked: config file",
			path:    filepath.Join(joeDir, "config.yaml"),
			wantErr: true,
			errType: "path_protection",
		},
		{
			name:    "blocked: database file",
			path:    filepath.Join(joeDir, "joe.db"),
			wantErr: true,
			errType: "path_protection",
		},
		{
			name:    "blocked: safety policy file",
			path:    filepath.Join(joeDir, PolicyFileName),
			wantErr: true,
			errType: "path_protection",
		},
		{
			name:    "blocked: subdirectory in .joe",
			path:    filepath.Join(joeDir, "subdir", "file.txt"),
			wantErr: true,
			errType: "path_protection",
		},
		{
			name:    "allowed: similar name but different directory",
			path:    filepath.Join(home, ".joe-backup", "test.txt"),
			wantErr: false,
		},
		{
			name:    "blocked: dot-dot traversal into .joe",
			path:    filepath.Join(home, "Documents", "..", ".joe", "config.yaml"),
			wantErr: true,
			errType: "path_protection",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := IsPathAllowed(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("IsPathAllowed(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
				return
			}

			if err != nil && tt.errType != "" {
				invErr, ok := err.(*InvariantViolationError)
				if !ok {
					t.Errorf("IsPathAllowed(%q) returned wrong error type: %T", tt.path, err)
					return
				}
				if invErr.Type != tt.errType {
					t.Errorf("IsPathAllowed(%q) error type = %q, want %q", tt.path, invErr.Type, tt.errType)
				}
			}
		})
	}
}

func TestIsPathAllowed_SymlinkToJoeDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get home dir: %v", err)
	}
	joeDir := filepath.Join(home, ".joe")

	// Only run if ~/.joe exists (it won't in CI)
	if _, err := os.Stat(joeDir); os.IsNotExist(err) {
		t.Skip("~/.joe does not exist, skipping symlink test")
	}

	// Create a temp dir with a symlink pointing to ~/.joe
	tmpDir := t.TempDir()
	symlinkPath := filepath.Join(tmpDir, "sneaky-link")
	if err := os.Symlink(joeDir, symlinkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	// Trying to access a file via the symlink should be blocked
	sneakyPath := filepath.Join(symlinkPath, "config.yaml")
	err = IsPathAllowed(sneakyPath)
	if err == nil {
		t.Error("expected error for symlink to ~/.joe, got nil")
	}

	invErr, ok := err.(*InvariantViolationError)
	if !ok {
		t.Fatalf("expected InvariantViolationError, got %T: %v", err, err)
	}
	if invErr.Type != "path_protection" {
		t.Errorf("error type = %q, want %q", invErr.Type, "path_protection")
	}
}

func TestIsCommandAllowed(t *testing.T) {
	tests := []struct {
		name    string
		command string
		wantErr bool
		errType string
	}{
		{
			name:    "allowed: ls",
			command: "ls",
			wantErr: false,
		},
		{
			name:    "allowed: kubectl with path",
			command: "/usr/local/bin/kubectl",
			wantErr: false,
		},
		{
			name:    "blocked: joe binary",
			command: "joe",
			wantErr: true,
			errType: "command_protection",
		},
		{
			name:    "blocked: joecored binary",
			command: "joecored",
			wantErr: true,
			errType: "command_protection",
		},
		{
			name:    "blocked: joe with path",
			command: "/usr/local/bin/joe",
			wantErr: true,
			errType: "command_protection",
		},
		{
			name:    "blocked: kill",
			command: "kill",
			wantErr: true,
			errType: "command_protection",
		},
		{
			name:    "blocked: pkill",
			command: "pkill",
			wantErr: true,
			errType: "command_protection",
		},
		{
			name:    "blocked: killall",
			command: "killall",
			wantErr: true,
			errType: "command_protection",
		},
		{
			name:    "blocked: kill with path",
			command: "/bin/kill",
			wantErr: true,
			errType: "command_protection",
		},
		{
			name:    "allowed: skilled (not blocklisted, just similar name)",
			command: "skilled",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := IsCommandAllowed(tt.command)
			if (err != nil) != tt.wantErr {
				t.Errorf("IsCommandAllowed(%q) error = %v, wantErr %v", tt.command, err, tt.wantErr)
				return
			}

			if err != nil && tt.errType != "" {
				invErr, ok := err.(*InvariantViolationError)
				if !ok {
					t.Errorf("IsCommandAllowed(%q) returned wrong error type: %T", tt.command, err)
					return
				}
				if invErr.Type != tt.errType {
					t.Errorf("IsCommandAllowed(%q) error type = %q, want %q", tt.command, invErr.Type, tt.errType)
				}
			}
		})
	}
}

func TestInvariantViolationError(t *testing.T) {
	err := &InvariantViolationError{
		Type:   "path_protection",
		Target: "/home/user/.joe/config.yaml",
		Reason: "Joe cannot access its config file",
	}

	want := "safety invariant violation (path_protection): /home/user/.joe/config.yaml - Joe cannot access its config file"
	got := err.Error()

	if got != want {
		t.Errorf("InvariantViolationError.Error() = %q, want %q", got, want)
	}

	if !IsInvariantViolation(err) {
		t.Error("IsInvariantViolation() should return true for InvariantViolationError")
	}
}

func TestIsInvariantViolation_NotInvariantError(t *testing.T) {
	err := &AccessDeniedError{
		ToolName: "test",
		Tier:     TierAct,
		Reason:   "disabled",
	}

	if IsInvariantViolation(err) {
		t.Error("IsInvariantViolation() should return false for non-invariant errors")
	}
}

// TestIsPathAllowed_CaseSensitivity verifies case-insensitive filesystem bypass protection.
// On macOS and Windows, /Users/alice/.JOE/ is the same as /Users/alice/.joe/
func TestIsPathAllowed_CaseSensitivity(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get home dir: %v", err)
	}
	joeDir := filepath.Join(home, ".joe")

	// Test various case permutations
	testPaths := []string{
		filepath.Join(home, ".JOE", "config.yaml"),         // Uppercase
		filepath.Join(home, ".Joe", "config.yaml"),         // Mixed case
		filepath.Join(home, ".joE", "config.yaml"),         // Mixed case
		strings.ToUpper(joeDir) + "/config.yaml",           // Full uppercase path
		filepath.Join(strings.ToUpper(home), ".joe", "db"), // Uppercase home
	}

	for _, path := range testPaths {
		t.Run(path, func(t *testing.T) {
			err := IsPathAllowed(path)

			// On case-insensitive systems (macOS, Windows), these should be blocked
			if isCaseInsensitiveFS() {
				if err == nil {
					t.Errorf("SECURITY: IsPathAllowed(%q) allowed on case-insensitive FS, should block", path)
				}
			}
			// On case-sensitive systems (Linux), these are different paths and may be allowed
			// (we don't test the block because the uppercase path may not exist)
		})
	}
}

// TestIsPathAllowed_HomeEnvBypass verifies that HOME env manipulation doesn't bypass protection.
func TestIsPathAllowed_HomeEnvBypass(t *testing.T) {
	// Get real home before manipulation using the same secure method paths package uses
	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("Cannot get home directory: %v", err)
	}
	realJoeDir := filepath.Join(realHome, ".joe")

	// Set fake HOME (JoeDirPath uses secure method, should ignore this)
	t.Setenv("HOME", "/tmp/fake-home")

	// Try to access real .joe directory (should still be blocked because
	// paths.JoeDirPath() uses getSecureHomeDir which ignores HOME env)
	configPath := filepath.Join(realJoeDir, "config.yaml")
	err = IsPathAllowed(configPath)

	// The protection should still work because paths.JoeDirPath()
	// determines the protected directory using getSecureHomeDir(), not HOME
	if err == nil {
		t.Errorf("SECURITY: IsPathAllowed(%q) succeeded despite HOME manipulation", configPath)
	}
	if !strings.Contains(err.Error(), "self-protection") {
		t.Errorf("Expected self-protection error, got: %v", err)
	}
}
