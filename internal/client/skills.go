package client

import (
	"context"
	"net/http"
)

const apiSkillsReloadPath = "/api/v1/skills/reload"

// SkillsReloadResult mirrors the JSON shape returned by POST /skills/reload.
// The before/after counts plus the diff slices give CLI and CI/CD callers a
// concise summary of what changed in the registry without a follow-up list
// call.
type SkillsReloadResult struct {
	Status  string   `json:"status"`
	Trigger string   `json:"trigger"`
	Before  int      `json:"before"`
	After   int      `json:"after"`
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
	Updated []string `json:"updated,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// ReloadSkills triggers a synchronous rescan of joe-core's ~/.joe/skills/
// directory and atomic registry swap. Returns the reload summary.
func (c *Client) ReloadSkills(ctx context.Context) (*SkillsReloadResult, error) {
	var result SkillsReloadResult
	if err := c.doJSON(ctx, "POST", c.baseURL+apiSkillsReloadPath, nil, http.StatusOK, &result, "skills reload"); err != nil {
		return nil, err
	}
	return &result, nil
}
