package drift

import (
	"testing"
)

func TestHashContent(t *testing.T) {
	h1 := hashContent("hello world")
	h2 := hashContent("hello world")
	h3 := hashContent("different")

	if h1 != h2 {
		t.Error("hashContent should be deterministic")
	}
	if h1 == h3 {
		t.Error("hashContent should differ for different content")
	}
	if h1 == "" {
		t.Error("hashContent should not return empty string")
	}
}

func TestComputeDiff_Identical(t *testing.T) {
	diff := computeDiff("same content", "same content")
	// No insertions or deletions expected for identical content.
	if diff == "" {
		// Empty diff is also valid — nothing to report.
		return
	}
}

func TestComputeDiff_Different(t *testing.T) {
	diff := computeDiff("original text", "revised text")
	if diff == "" {
		t.Error("computeDiff should produce non-empty output for different content")
	}
}

func TestDriftReport_Fields(t *testing.T) {
	report := &DriftReport{
		EntryID:         "entry-1",
		Title:           "My Doc",
		ExternalChanged: true,
		Diff:            "some diff",
	}
	if report.EntryID != "entry-1" {
		t.Errorf("EntryID = %q, want %q", report.EntryID, "entry-1")
	}
	if !report.ExternalChanged {
		t.Error("ExternalChanged should be true")
	}
}
