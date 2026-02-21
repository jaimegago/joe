// Package notion provides a knowledge Syncer for Notion databases.
// It fetches pages from a configured database via the Notion REST API
// and upserts them as Tier 2 knowledge entries.
package notion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jaimegago/joe/internal/knowledge"
)

// Config holds the Notion connection parameters stored (encrypted) in
// knowledge_sources.config.
type Config struct {
	APIToken   string `json:"api_token"`   // Notion integration token
	DatabaseID string `json:"database_id"` // Notion database to sync
	PageLimit  int    `json:"page_limit"`  // max pages per sync (0 = unlimited)
}

const notionAPIVersion = "2022-06-28"
const notionBaseURL = "https://api.notion.com/v1"

// Syncer implements sync.Syncer for Notion.
type Syncer struct{}

// New creates a Notion Syncer.
func New() *Syncer { return &Syncer{} }

// Sync fetches all pages in the configured Notion database and upserts them
// as Tier 2 knowledge entries.
func (s *Syncer) Sync(ctx context.Context, src *knowledge.KnowledgeSource, svc *knowledge.Service) error {
	cfg, err := parseConfig(src.Config)
	if err != nil {
		return fmt.Errorf("parse notion config: %w", err)
	}

	pages, err := fetchPages(ctx, cfg)
	if err != nil {
		return fmt.Errorf("fetch notion pages: %w", err)
	}

	for _, p := range pages {
		entry := &knowledge.Entry{
			Tier:       knowledge.TierSynced,
			Type:       knowledge.EntryTypeDoc,
			Title:      p.title,
			Content:    p.content,
			SourceType: knowledge.SourceTypeNotion,
			SourceID:   p.id,
			SourceURL:  "https://www.notion.so/" + strings.ReplaceAll(p.id, "-", ""),
			Confidence: 1.0,
		}
		if err := svc.UpsertSynced(ctx, entry); err != nil {
			return fmt.Errorf("upsert notion page %s: %w", p.id, err)
		}
	}
	return nil
}

// --- internal types ---

type notionPage struct {
	id      string
	title   string
	content string
}

type queryDBResponse struct {
	Results    []notionPageResult `json:"results"`
	HasMore    bool               `json:"has_more"`
	NextCursor string             `json:"next_cursor"`
}

type notionPageResult struct {
	ID         string                    `json:"id"`
	Properties map[string]notionProperty `json:"properties"`
}

type notionProperty struct {
	Type  string           `json:"type"`
	Title []notionRichText `json:"title,omitempty"`
}

type notionRichText struct {
	PlainText string `json:"plain_text"`
}

// fetchPages retrieves all pages from a Notion database, handling pagination,
// and fetches each page's block content to build a plain-text representation.
func fetchPages(ctx context.Context, cfg *Config) ([]*notionPage, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	var allPages []*notionPage
	cursor := ""

	for {
		reqBody := map[string]any{"page_size": 100}
		if cursor != "" {
			reqBody["start_cursor"] = cursor
		}
		bodyBytes, _ := json.Marshal(reqBody)

		u := fmt.Sprintf("%s/databases/%s/query", notionBaseURL, cfg.DatabaseID)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
		req.Header.Set("Notion-Version", notionAPIVersion)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("query database: %w", err)
		}
		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("notion API error (status %d): %s", resp.StatusCode, string(respBody))
		}

		var qr queryDBResponse
		if err := json.Unmarshal(respBody, &qr); err != nil {
			return nil, fmt.Errorf("parse query response: %w", err)
		}

		for _, r := range qr.Results {
			title := extractTitle(r.Properties)
			content, _ := fetchPageContent(ctx, client, cfg.APIToken, r.ID)
			allPages = append(allPages, &notionPage{
				id:      r.ID,
				title:   title,
				content: content,
			})
		}

		if !qr.HasMore || qr.NextCursor == "" {
			break
		}
		cursor = qr.NextCursor
		if cfg.PageLimit > 0 && len(allPages) >= cfg.PageLimit {
			break
		}
	}
	return allPages, nil
}

// fetchPageContent retrieves the plain-text content of a Notion page by
// concatenating its block texts.
func fetchPageContent(ctx context.Context, client *http.Client, token, pageID string) (string, error) {
	u := fmt.Sprintf("%s/blocks/%s/children?page_size=100", notionBaseURL, pageID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Notion-Version", notionAPIVersion)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch blocks (status %d)", resp.StatusCode)
	}

	var result struct {
		Results []map[string]json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	var sb strings.Builder
	for _, block := range result.Results {
		// Extract text from known block types (paragraph, heading_1/2/3, bulleted_list_item, etc.)
		for _, blockType := range []string{"paragraph", "heading_1", "heading_2", "heading_3",
			"bulleted_list_item", "numbered_list_item", "to_do", "quote", "callout"} {
			raw, ok := block[blockType]
			if !ok {
				continue
			}
			var bt struct {
				RichText []notionRichText `json:"rich_text"`
			}
			if err := json.Unmarshal(raw, &bt); err == nil {
				for _, rt := range bt.RichText {
					sb.WriteString(rt.PlainText)
				}
				sb.WriteString("\n")
			}
			break
		}
	}
	return sb.String(), nil
}

func extractTitle(props map[string]notionProperty) string {
	for _, p := range props {
		if p.Type == "title" && len(p.Title) > 0 {
			return p.Title[0].PlainText
		}
	}
	return "Untitled"
}

func parseConfig(raw json.RawMessage) (*Config, error) {
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	if cfg.APIToken == "" {
		return nil, fmt.Errorf("notion config missing api_token")
	}
	if cfg.DatabaseID == "" {
		return nil, fmt.Errorf("notion config missing database_id")
	}
	return &cfg, nil
}
