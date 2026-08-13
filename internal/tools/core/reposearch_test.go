package core_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	gitadapter "github.com/jaimegago/joe/internal/adapters/git"
	"github.com/jaimegago/joe/internal/tools/core"
)

type fakeRepoSearchClient struct {
	gotSourceID string
	gotOpts     gitadapter.SearchOptions
	result      *gitadapter.SearchResult
	err         error
}

func (f *fakeRepoSearchClient) GitSearch(_ context.Context, sourceID string, opts gitadapter.SearchOptions) (*gitadapter.SearchResult, error) {
	f.gotSourceID = sourceID
	f.gotOpts = opts
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func TestRepoSearchTool_Execute_PassesEveryArgumentThrough(t *testing.T) {
	c := &fakeRepoSearchClient{result: &gitadapter.SearchResult{
		Commit:     "deadbeef",
		PathPrefix: "internal",
		Matches:    []gitadapter.SearchMatch{{Path: "internal/a.go", Line: 7, Text: "hit"}},
	}}
	tool := core.NewRepoSearchTool(c)

	res, err := tool.Execute(context.Background(), map[string]any{
		"component_id":      "src-1",
		"pattern":           "hit",
		"literal":           true,
		"commit":            "deadbeef",
		"path_prefix":       "internal",
		"max_matches":       float64(5),
		"max_files_scanned": float64(50),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if c.gotSourceID != "src-1" {
		t.Errorf("sourceID = %q, want src-1", c.gotSourceID)
	}
	want := gitadapter.SearchOptions{
		Pattern: "hit", Literal: true, Commit: "deadbeef", PathPrefix: "internal",
		MaxMatches: 5, MaxFilesScanned: 50,
	}
	if c.gotOpts != want {
		t.Errorf("opts = %+v, want %+v", c.gotOpts, want)
	}

	m := res.(map[string]any)
	if m["commit"] != "deadbeef" || m["path_prefix"] != "internal" || m["match_count"] != 1 {
		t.Errorf("result = %v, want the commit, scope and count reported back", m)
	}
}

// TestRepoSearchTool_Execute_ReportsBothMarkers pins that the two exhaustion
// markers reach the loop as separate fields. Collapsed into one signal, an
// incomplete scan would be readable as a clean empty result.
func TestRepoSearchTool_Execute_ReportsBothMarkers(t *testing.T) {
	c := &fakeRepoSearchClient{result: &gitadapter.SearchResult{
		Commit: "c0ffee", ScanIncomplete: true, MatchesTruncated: false,
		FilesInScope: 900, FilesConsidered: 100, SkippedBinary: 3, SkippedLarge: 2,
	}}
	tool := core.NewRepoSearchTool(c)

	res, err := tool.Execute(context.Background(), map[string]any{"component_id": "src-1", "pattern": "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := res.(map[string]any)

	for key, want := range map[string]any{
		"scan_incomplete":   true,
		"matches_truncated": false,
		"skipped_binary":    3,
		"skipped_large":     2,
		"files_in_scope":    900,
		"files_considered":  100,
	} {
		if m[key] != want {
			t.Errorf("result[%q] = %v, want %v", key, m[key], want)
		}
	}
}

func TestRepoSearchTool_Execute_MissingParams(t *testing.T) {
	tool := core.NewRepoSearchTool(&fakeRepoSearchClient{})
	cases := []map[string]any{
		{},
		{"pattern": "x"},
		{"component_id": "src-1"},
		{"component_id": "src-1", "pattern": ""},
	}
	for _, args := range cases {
		if _, err := tool.Execute(context.Background(), args); err == nil {
			t.Errorf("expected error for args: %v", args)
		}
	}
}

// TestRepoSearchTool_Execute_DenialSurfacesAsAnError pins that an entitlement
// failure is the call's answer, not an empty result set. This is the property
// one-component-per-call exists to preserve.
func TestRepoSearchTool_Execute_DenialSurfacesAsAnError(t *testing.T) {
	tool := core.NewRepoSearchTool(&fakeRepoSearchClient{
		err: errors.New("access denied for component \"src-1\": permission denied"),
	})

	res, err := tool.Execute(context.Background(), map[string]any{"component_id": "src-1", "pattern": "x"})
	if err == nil {
		t.Fatalf("Execute returned (%v, nil) on a denial; a denial must not be indistinguishable from finding nothing", res)
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("error = %q, want the accessor's own denial carried through unchanged", err)
	}
}

// TestRepoSearchTool_DescriptionCarriesTheCaveats pins the contract the loop can
// actually see. The loop knows only what the tool advertises, so a caveat that
// lives only in code does not constrain it.
func TestRepoSearchTool_DescriptionCarriesTheCaveats(t *testing.T) {
	desc := core.NewRepoSearchTool(&fakeRepoSearchClient{}).Description()
	for _, phrase := range []string{
		"matches_truncated",
		"scan_incomplete",
		"ABSENCE OF HITS PROVES NOTHING",
		"NEVER CITABLE",
		"git_read",
		"ONE component per call",
	} {
		if !strings.Contains(desc, phrase) {
			t.Errorf("Description() is missing %q", phrase)
		}
	}
}
