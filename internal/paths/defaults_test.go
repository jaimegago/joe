package paths

import (
	"path/filepath"
	"testing"
)

func TestDefaultConfigPath_UsesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got := DefaultConfigPath()
	want := filepath.Join(home, JoeDir, ConfigFile)
	if got != want {
		t.Fatalf("DefaultConfigPath() = %q, want %q", got, want)
	}
}

func TestJoeDirPath_UsesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := JoeDirPath()
	if err != nil {
		t.Fatalf("JoeDirPath() error: %v", err)
	}
	want := filepath.Join(home, JoeDir)
	if got != want {
		t.Fatalf("JoeDirPath() = %q, want %q", got, want)
	}
}

func TestDatabasePath_UsesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := DatabasePath()
	if err != nil {
		t.Fatalf("DatabasePath() error: %v", err)
	}
	want := filepath.Join(home, JoeDir, DatabaseFile)
	if got != want {
		t.Fatalf("DatabasePath() = %q, want %q", got, want)
	}
}
