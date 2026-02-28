package notion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// UpdatePage replaces the content of a Notion page with the given plain-text content.
// It clears existing paragraph blocks and appends new ones.
func UpdatePage(ctx context.Context, cfg *Config, pageID, content string) error {
	client := &http.Client{Timeout: 30 * time.Second}

	// 1. Fetch existing block children so we can delete them.
	blockIDs, err := listBlockChildren(ctx, client, cfg.APIToken, pageID)
	if err != nil {
		return fmt.Errorf("list notion blocks: %w", err)
	}

	// 2. Delete existing paragraph-type blocks (best-effort).
	for _, bid := range blockIDs {
		_ = deleteBlock(ctx, client, cfg.APIToken, bid)
	}

	// 3. Append new paragraph blocks for each line.
	if err := appendBlocks(ctx, client, cfg.APIToken, pageID, content); err != nil {
		return fmt.Errorf("append notion blocks: %w", err)
	}
	return nil
}

func listBlockChildren(ctx context.Context, client *http.Client, token, blockID string) ([]string, error) {
	u := fmt.Sprintf("%s/blocks/%s/children?page_size=100", notionBaseURL, blockID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Notion-Version", notionAPIVersion)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("read notion response body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list blocks (status %d)", resp.StatusCode)
	}

	var result struct {
		Results []struct {
			ID string `json:"id"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(result.Results))
	for _, r := range result.Results {
		ids = append(ids, r.ID)
	}
	return ids, nil
}

func deleteBlock(ctx context.Context, client *http.Client, token, blockID string) error {
	u := fmt.Sprintf("%s/blocks/%s", notionBaseURL, blockID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Notion-Version", notionAPIVersion)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func appendBlocks(ctx context.Context, client *http.Client, token, pageID, content string) error {
	// Split content into lines; each non-empty line becomes a paragraph block.
	lines := splitLines(content)
	if len(lines) == 0 {
		lines = []string{content}
	}

	var children []map[string]any
	for _, line := range lines {
		children = append(children, map[string]any{
			"object": "block",
			"type":   "paragraph",
			"paragraph": map[string]any{
				"rich_text": []map[string]any{
					{
						"type": "text",
						"text": map[string]string{"content": line},
					},
				},
			},
		})
	}

	payload := map[string]any{"children": children}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal blocks: %w", err)
	}

	u := fmt.Sprintf("%s/blocks/%s/children", notionBaseURL, pageID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, u, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Notion-Version", notionAPIVersion)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("notion append blocks (status %d): %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			lines = append(lines, line)
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
