package paths

import (
	"path/filepath"
	"testing"
)

func TestDefaultConfigPath_UsesHome(t *testing.T) {
	// Get secure home directory
	home, err := getSecureHomeDir()
	if err != nil {
		t.Skipf("Cannot get home directory: %v", err)
	}

	got := DefaultConfigPath()
	want := filepath.Join(home, JoeDir, ConfigFile)
	if got != want {
		t.Fatalf("DefaultConfigPath() = %q, want %q", got, want)
	}
}

func TestJoeDirPath_UsesHome(t *testing.T) {
	// Get secure home directory
	home, err := getSecureHomeDir()
	if err != nil {
		t.Skipf("Cannot get home directory: %v", err)
	}

	got, err := JoeDirPath()
	if err != nil {
		t.Fatalf("JoeDirPath() error: %v", err)
	}
	want := filepath.Join(home, JoeDir)
	if got != want {
		t.Fatalf("JoeDirPath() = %q, want %q", got, want)
	}
}

// TestJoeDirPath_IgnoresHOMEEnv verifies that HOME environment variable
// is ignored to prevent security bypass attacks.
func TestJoeDirPath_IgnoresHOMEEnv(t *testing.T) {
	// Get actual home directory before setting fake HOME
	// Use getSecureHomeDir which should ignore HOME env var
	realHome, err := getSecureHomeDir()
	if err != nil {
		t.Skipf("Cannot get home directory: %v", err)
	}

	// Set HOME to attacker-controlled path
	t.Setenv("HOME", "/tmp/fake-home")

	// JoeDirPath should still return real home, not /tmp/fake-home
	got, err := JoeDirPath()
	if err != nil {
		t.Fatalf("JoeDirPath() error: %v", err)
	}

	want := filepath.Join(realHome, JoeDir)
	if got != want {
		t.Errorf("JoeDirPath() = %q, want %q (should ignore HOME env var)", got, want)
	}

	// Verify it did NOT use the fake HOME
	fakeJoeDir := filepath.Join("/tmp/fake-home", JoeDir)
	if got == fakeJoeDir {
		t.Errorf("SECURITY: JoeDirPath() used fake HOME from env var: %q", got)
	}
}

func TestDatabasePath_UsesHome(t *testing.T) {
	// Get secure home directory
	home, err := getSecureHomeDir()
	if err != nil {
		t.Skipf("Cannot get home directory: %v", err)
	}

	got, err := DatabasePath()
	if err != nil {
		t.Fatalf("DatabasePath() error: %v", err)
	}
	want := filepath.Join(home, JoeDir, DatabaseFile)
	if got != want {
		t.Fatalf("DatabasePath() = %q, want %q", got, want)
	}
}
