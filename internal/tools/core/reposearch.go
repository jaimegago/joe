package core

import (
	"context"
	"fmt"

	gitadapter "github.com/jaimegago/joe/internal/adapters/git"
	"github.com/jaimegago/joe/internal/llm"
)

// RepoSearchClient defines the subset of client.Client needed for
// RepoSearchTool.
type RepoSearchClient interface {
	GitSearch(ctx context.Context, sourceID string, opts gitadapter.SearchOptions) (*gitadapter.SearchResult, error)
}

// RepoSearchTool searches file contents in one Git repository component at a
// pinned commit. It is dumb in the D-0141 sense: the output is a deterministic
// function of the arguments plus the substrate, with no model and no ranking
// inside the tool boundary.
//
// One component per call, by design. Fleet-wide search is loop iteration over
// components, never one global query — not because a multi-component call could
// not resolve entitlement per component (it could), but because with one
// component per call an entitlement failure IS the call's answer instead of a
// per-item footnote, the bounds allocate per component instead of one noisy repo
// starving the rest, and the pinned commit is inherently per component since
// each clone has its own head.
type RepoSearchTool struct {
	client RepoSearchClient
}

// NewRepoSearchTool creates a new repo_search tool.
func NewRepoSearchTool(c RepoSearchClient) *RepoSearchTool {
	return &RepoSearchTool{client: c}
}

func (t *RepoSearchTool) Name() string { return "repo_search" }

// Description carries the tool's caveats because the loop knows only what the
// tool advertises: a contract the loop cannot see does not constrain it. The two
// exhaustion markers and the leads-are-not-citations rule are therefore stated
// here, not only in the code.
func (t *RepoSearchTool) Description() string {
	return "Search file contents in one connected Git repository component, at a pinned commit. " +
		"Substring or RE2 regular-expression matching over the tracked files of that commit; " +
		"no ranking. Results are ordered by path, then line number. " +
		"Search ONE component per call — to cover a fleet, call once per component. " +
		"Every result reports the commit searched and the path scope applied, including an empty result. " +
		"Two independent exhaustion markers come back with every result and answer DIFFERENT questions: " +
		"`matches_truncated` answers WHY THE SCAN STOPPED — the output bound was reached and you were given a prefix, so more matches may exist; " +
		"`scan_incomplete` answers WHETHER THE REPOSITORY WAS EXHAUSTED — part of it was never looked at, so the answer is unreliable and " +
		"AN ABSENCE OF HITS PROVES NOTHING, and you must never conclude that something does not appear in the repository from an incomplete scan. " +
		"THE TWO ARE INDEPENDENT, NOT EXCLUSIVE: when the output bound stops the scan with files still unvisited, BOTH are true and both are correct — " +
		"do not read one being true as evidence about the other. " +
		"`files_in_scope` versus `files_considered` is the same fact as a count. " +
		"`skipped_binary` and `skipped_large` report files excluded from the substrate. " +
		"An invalid pattern is an explicit error, never zero hits. " +
		"HITS ARE LEADS, NEVER CITABLE: a search result is input to triage. Cite a claim only after re-reading the file with git_read at the reported commit, " +
		"which takes that commit as an argument and reports back the commit it answered at."
}

func (t *RepoSearchTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {Type: "string", Description: "ID of the Git component to search."},
			"pattern":      {Type: "string", Description: "RE2 regular expression to match against each line, or a plain substring when literal is true. Backreferences and lookaround are not supported."},
			"literal":      {Type: "boolean", Description: "If true, treat pattern as a literal substring instead of a regular expression."},
			"commit":       {Type: "string", Description: "Commit to search at. Omit to search the clone's current head, which is reported back. If given, the search answers at exactly that commit or fails."},
			"path_prefix":  {Type: "string", Description: "Restrict the search to this path and everything beneath it. Omit to search the whole tree."},
			"max_matches":  {Type: "integer", Description: "Lower the output bound on matches returned. Cannot be raised past the fixed ceiling."},
			"max_files_scanned": {
				Type:        "integer",
				Description: "Lower the work bound on files considered. Cannot be raised past the fixed ceiling. Hitting it sets scan_incomplete.",
			},
		},
		Required: []string{"component_id", "pattern"},
	}
}

func (t *RepoSearchTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}

	pattern, ok := args["pattern"].(string)
	if !ok || pattern == "" {
		return nil, fmt.Errorf("missing required parameter: pattern")
	}

	literal, _ := args["literal"].(bool)
	commit, _ := args["commit"].(string)
	pathPrefix, _ := args["path_prefix"].(string)

	opts := gitadapter.SearchOptions{
		Pattern:         pattern,
		Literal:         literal,
		Commit:          commit,
		PathPrefix:      pathPrefix,
		MaxMatches:      optionalIntArg(args, "max_matches"),
		MaxFilesScanned: optionalIntArg(args, "max_files_scanned"),
	}

	res, err := t.client.GitSearch(ctx, sourceID, opts)
	if err != nil {
		return nil, fmt.Errorf("repo search failed: %w", err)
	}

	return map[string]any{
		"component_id":      sourceID,
		"commit":            res.Commit,
		"path_prefix":       res.PathPrefix,
		"matches":           res.Matches,
		"match_count":       len(res.Matches),
		"files_in_scope":    res.FilesInScope,
		"files_considered":  res.FilesConsidered,
		"files_searched":    res.FilesSearched,
		"skipped_binary":    res.SkippedBinary,
		"skipped_large":     res.SkippedLarge,
		"matches_truncated": res.MatchesTruncated,
		"scan_incomplete":   res.ScanIncomplete,
		"max_matches":       res.MaxMatches,
		"max_files_scanned": res.MaxFilesScanned,
	}, nil
}

// optionalIntArg reads an optional integer parameter. JSON numbers arrive as
// float64 through the tool-call boundary; an absent or non-numeric value yields
// 0, which the adapter reads as "use the default bound".
func optionalIntArg(args map[string]any, name string) int {
	switch v := args[name].(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}
