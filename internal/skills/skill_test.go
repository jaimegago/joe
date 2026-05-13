package skills

import (
	"strings"
	"testing"
)

func TestParseSkill_ValidFrontmatter(t *testing.T) {
	data := []byte(`---
name: restart-loop-diagnosis
description: Diagnose pods stuck in CrashLoopBackOff before recommending another restart.
---

# How to think about restart loops

Restart-restart-restart without root cause is a smell. Identify the failure mode (OOM vs crash vs liveness probe) first.
`)
	got, err := ParseSkill("test.md", data)
	if err != nil {
		t.Fatalf("ParseSkill: %v", err)
	}
	if got.Name != "restart-loop-diagnosis" {
		t.Errorf("Name = %q, want restart-loop-diagnosis", got.Name)
	}
	if !strings.HasPrefix(got.Description, "Diagnose pods") {
		t.Errorf("Description not parsed correctly: %q", got.Description)
	}
	if !strings.Contains(got.Body, "Restart-restart-restart") {
		t.Errorf("Body missing expected content: %q", got.Body)
	}
	if got.Hash == "" {
		t.Error("Hash should be set")
	}
	if got.Source != "test.md" {
		t.Errorf("Source = %q, want test.md", got.Source)
	}
}

func TestParseSkill_LeadingBlankLinesAndBOM(t *testing.T) {
	// UTF-8 BOM, then a blank line, then frontmatter.
	data := []byte("\xEF\xBB\xBF\n---\nname: x\ndescription: y\n---\n\nbody\n")
	got, err := ParseSkill("x.md", data)
	if err != nil {
		t.Fatalf("ParseSkill: %v", err)
	}
	if got.Name != "x" || got.Description != "y" || got.Body != "body" {
		t.Errorf("unexpected parse: %+v", got)
	}
}

func TestParseSkill_MissingFrontmatter(t *testing.T) {
	data := []byte("# Just a markdown file with no frontmatter\n")
	if _, err := ParseSkill("x.md", data); err == nil {
		t.Fatal("expected error for missing frontmatter, got nil")
	}
}

func TestParseSkill_MissingClosingDelimiter(t *testing.T) {
	data := []byte("---\nname: x\ndescription: y\nbody without closing\n")
	if _, err := ParseSkill("x.md", data); err == nil {
		t.Fatal("expected error for missing closing delimiter, got nil")
	}
}

func TestParseSkill_MissingRequiredFields(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{"no name", "---\ndescription: only desc\n---\nbody\n"},
		{"no description", "---\nname: only-name\n---\nbody\n"},
		{"empty name", "---\nname: \"\"\ndescription: x\n---\nbody\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseSkill("x.md", []byte(tc.data)); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestParseSkill_SizeLimit(t *testing.T) {
	big := make([]byte, MaxSkillSize+1)
	for i := range big {
		big[i] = 'a'
	}
	if _, err := ParseSkill("x.md", big); err == nil {
		t.Fatal("expected size limit error, got nil")
	}
}

func TestParseSkill_HashStable(t *testing.T) {
	data := []byte("---\nname: x\ndescription: y\n---\nbody\n")
	a, _ := ParseSkill("a.md", data)
	b, _ := ParseSkill("b.md", data)
	if a.Hash != b.Hash {
		t.Errorf("hash should depend only on content, got %s vs %s", a.Hash, b.Hash)
	}
}
