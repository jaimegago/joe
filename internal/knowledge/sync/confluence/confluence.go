// Package confluence provides a knowledge Syncer for Atlassian Confluence.
// It fetches pages from a configured space via the Confluence REST API v2
// and upserts them as Tier 2 knowledge entries.
package confluence

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/jaimegago/joe/internal/knowledge"
)

// Config holds the Confluence connection parameters stored (encrypted) in
// knowledge_sources.config.
type Config struct {
	BaseURL   string `json:"base_url"`   // e.g. "https://mycompany.atlassian.net"
	APIToken  string `json:"api_token"`  // Atlassian API token
	Email     string `json:"email"`      // account email for Basic auth
	SpaceKey  string `json:"space_key"`  // Confluence space key to sync
	PageLimit int    `json:"page_limit"` // max pages per sync (0 = unlimited)
}

// Syncer implements sync.Syncer for Confluence.
type Syncer struct{}

// New creates a Confluence Syncer.
func New() *Syncer { return &Syncer{} }

// Sync fetches all pages in the configured space and upserts them as Tier 2
// knowledge entries. Changed pages (different content hash) are re-embedded.
func (s *Syncer) Sync(ctx context.Context, src *knowledge.KnowledgeSource, svc *knowledge.Service) error {
	cfg, err := parseConfig(src.Config)
	if err != nil {
		return fmt.Errorf("parse confluence config: %w", err)
	}

	pages, err := fetchPages(ctx, cfg)
	if err != nil {
		return fmt.Errorf("fetch confluence pages: %w", err)
	}

	for _, p := range pages {
		entry := &knowledge.Entry{
			Tier:       knowledge.TierSynced,
			Type:       knowledge.EntryTypeDoc,
			Title:      p.Title,
			Content:    p.Body,
			SourceType: knowledge.SourceTypeConfluence,
			SourceID:   p.ID,
			SourceURL:  cfg.BaseURL + "/wiki" + p.Links.WebUI,
			Confidence: 1.0,
		}
		if err := svc.UpsertSynced(ctx, entry); err != nil {
			return fmt.Errorf("upsert confluence page %s: %w", p.ID, err)
		}
	}
	return nil
}

// --- Confluence REST API v2 types ---

type page struct {
	ID    string    `json:"id"`
	Title string    `json:"title"`
	Body  string    // assembled from body.storage.value
	Links pageLinks `json:"_links"`
}

type pageBody struct {
	Storage struct {
		Value string `json:"value"`
	} `json:"storage"`
}

type pageLinks struct {
	WebUI string `json:"webui"`
}

type pagesResponse struct {
	Results []struct {
		ID    string    `json:"id"`
		Title string    `json:"title"`
		Body  pageBody  `json:"body"`
		Links pageLinks `json:"_links"`
	} `json:"results"`
	Links struct {
		Next string `json:"next"`
	} `json:"_links"`
}

// fetchPages retrieves all pages from the Confluence space, handling pagination.
func fetchPages(ctx context.Context, cfg *Config) ([]*page, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	limit := 25
	var allPages []*page
	cursor := ""

	for {
		u := fmt.Sprintf("%s/wiki/api/v2/pages?space-key=%s&body-format=storage&limit=%d",
			cfg.BaseURL, url.QueryEscape(cfg.SpaceKey), limit)
		if cursor != "" {
			u += "&cursor=" + url.QueryEscape(cursor)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.SetBasicAuth(cfg.Email, cfg.APIToken)
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch pages: %w", err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("confluence API error (status %d): %s", resp.StatusCode, string(body))
		}

		var pr pagesResponse
		if err := json.Unmarshal(body, &pr); err != nil {
			return nil, fmt.Errorf("parse response: %w", err)
		}

		for _, r := range pr.Results {
			p := &page{
				ID:    r.ID,
				Title: r.Title,
				Body:  r.Body.Storage.Value,
				Links: r.Links,
			}
			allPages = append(allPages, p)
		}

		if pr.Links.Next == "" {
			break
		}
		// Extract cursor from next link (e.g. "?cursor=abc&limit=25")
		nextURL, err := url.Parse(pr.Links.Next)
		if err != nil {
			break
		}
		cursor = nextURL.Query().Get("cursor")
		if cursor == "" {
			break
		}
		if cfg.PageLimit > 0 && len(allPages) >= cfg.PageLimit {
			break
		}
	}
	return allPages, nil
}

func parseConfig(raw json.RawMessage) (*Config, error) {
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("confluence config missing base_url")
	}
	if cfg.APIToken == "" {
		return nil, fmt.Errorf("confluence config missing api_token")
	}
	if cfg.SpaceKey == "" {
		return nil, fmt.Errorf("confluence config missing space_key")
	}
	return &cfg, nil
}
