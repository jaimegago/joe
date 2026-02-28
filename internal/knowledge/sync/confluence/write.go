package confluence

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// UpdatePage updates a Confluence page via the REST API v2.
// version must be the current version number + 1 (Confluence requires it for optimistic locking).
func UpdatePage(ctx context.Context, cfg *Config, pageID, title, content string, version int) error {
	payload := map[string]any{
		"id":     pageID,
		"status": "current",
		"title":  title,
		"body": map[string]any{
			"representation": "storage",
			"value":          content,
		},
		"version": map[string]any{
			"number": version,
		},
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal update payload: %w", err)
	}

	u := fmt.Sprintf("%s/wiki/api/v2/pages/%s", cfg.BaseURL, pageID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.SetBasicAuth(cfg.Email, cfg.APIToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("update confluence page: %w", err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("confluence update error (status %d): %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// GetPageVersion fetches the current version number of a Confluence page.
// Required before calling UpdatePage to supply the correct next version.
func GetPageVersion(ctx context.Context, cfg *Config, pageID string) (int, error) {
	u := fmt.Sprintf("%s/wiki/api/v2/pages/%s", cfg.BaseURL, pageID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}
	req.SetBasicAuth(cfg.Email, cfg.APIToken)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("get confluence page: %w", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return 0, fmt.Errorf("read confluence response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("confluence get error (status %d)", resp.StatusCode)
	}

	var pr struct {
		Version struct {
			Number int `json:"number"`
		} `json:"version"`
	}
	if err := json.Unmarshal(body, &pr); err != nil {
		return 0, fmt.Errorf("parse page version: %w", err)
	}
	return pr.Version.Number, nil
}
