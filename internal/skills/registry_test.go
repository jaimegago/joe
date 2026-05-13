package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSkill(t *testing.T, dir, name, desc, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + desc + "\n---\n\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDir_MissingRoot(t *testing.T) {
	reg, err := LoadDir(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("LoadDir on missing root should not error, got %v", err)
	}
	if reg.Len() != 0 {
		t.Errorf("expected empty registry, got %d skills", reg.Len())
	}
}

func TestLoadDir_LoadsMultipleSkills(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "alpha"), "alpha", "first skill", "alpha body")
	writeSkill(t, filepath.Join(root, "beta"), "beta", "second skill", "beta body")

	reg, err := LoadDir(root)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if reg.Len() != 2 {
		t.Fatalf("Len = %d, want 2", reg.Len())
	}
	// Should be sorted by name.
	all := reg.All()
	if all[0].Name != "alpha" || all[1].Name != "beta" {
		t.Errorf("expected alpha,beta order; got %s,%s", all[0].Name, all[1].Name)
	}
	if reg.Get("alpha") == nil || reg.Get("beta") == nil {
		t.Error("Get failed for known skill names")
	}
	if reg.Get("missing") != nil {
		t.Error("Get for unknown name should return nil")
	}
}

func TestLoadDir_SkipsMalformedAndDuplicates(t *testing.T) {
	root := t.TempDir()

	// Valid skill.
	writeSkill(t, filepath.Join(root, "good"), "good", "valid skill", "body")

	// Malformed skill — missing frontmatter.
	bad := filepath.Join(root, "bad")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "SKILL.md"), []byte("no frontmatter here"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Duplicate name — should be skipped, not fatal.
	writeSkill(t, filepath.Join(root, "good-dup"), "good", "dup skill", "body")

	reg, err := LoadDir(root)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if reg.Len() != 1 {
		t.Fatalf("expected 1 skill (good), got %d", reg.Len())
	}
	if reg.Get("good") == nil {
		t.Error("expected 'good' skill to load")
	}
}

func TestLoadDir_NotADirectory(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDir(f); err == nil {
		t.Fatal("expected error when root is a file, got nil")
	}
}
