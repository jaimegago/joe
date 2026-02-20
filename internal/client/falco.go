package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	falcoadapter "github.com/jaimegago/joe/internal/adapters/security/falco"
)

// FalcoEvents returns recent Falco runtime security events.
// priority, source, and rule are optional filters. limit 0 uses the server default.
func (c *Client) FalcoEvents(ctx context.Context, sourceID, priority, source, rule string, limit int) ([]falcoadapter.Event, error) {
	u := fmt.Sprintf("%s%s/%s/events?limit=%s",
		c.baseURL, apiFalcoBasePath, url.PathEscape(sourceID),
		strconv.Itoa(limit))
	if priority != "" {
		u += "&priority=" + url.QueryEscape(priority)
	}
	if source != "" {
		u += "&source=" + url.QueryEscape(source)
	}
	if rule != "" {
		u += "&rule=" + url.QueryEscape(rule)
	}

	var result struct {
		Events   []falcoadapter.Event `json:"events"`
		Count    int                  `json:"count"`
		SourceID string               `json:"source_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "falco events"); err != nil {
		return nil, err
	}

	return result.Events, nil
}

// FalcoRules returns the Falco rules derived from recent events.
func (c *Client) FalcoRules(ctx context.Context, sourceID string) ([]falcoadapter.Rule, error) {
	u := fmt.Sprintf("%s%s/%s/rules",
		c.baseURL, apiFalcoBasePath, url.PathEscape(sourceID))

	var result struct {
		Rules    []falcoadapter.Rule `json:"rules"`
		Count    int                 `json:"count"`
		SourceID string              `json:"source_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "falco rules"); err != nil {
		return nil, err
	}

	return result.Rules, nil
}
