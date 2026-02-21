// Package drift detects when documentation in external systems has diverged
// from the content stored in Joe's knowledge store (Tier 2 synced entries).
package drift

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/sergi/go-diff/diffmatchpatch"

	"github.com/jaimegago/joe/internal/knowledge"
)

// DriftReport describes drift detected for a single knowledge entry.
type DriftReport struct {
	EntryID         string               `json:"entry_id"`
	Title           string               `json:"title"`
	SourceType      knowledge.SourceType `json:"source_type"`
	SourceURL       string               `json:"source_url"`
	ExternalChanged bool                 `json:"external_changed"`
	LocalChanged    bool                 `json:"local_changed"`
	Diff            string               `json:"diff,omitempty"`
	DetectedAt      time.Time            `json:"detected_at"`
}

// Detector checks for drift between the knowledge store and external sources.
type Detector struct {
	svc        *knowledge.Service
	httpClient *http.Client
	logger     *slog.Logger
}

// New creates a new drift Detector.
func New(svc *knowledge.Service) *Detector {
	return &Detector{
		svc:        svc,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		logger:     slog.Default(),
	}
}

// Detect checks drift for a single knowledge entry by ID.
func (d *Detector) Detect(ctx context.Context, entryID string) (*DriftReport, error) {
	entry, err := d.svc.Get(ctx, entryID)
	if err != nil {
		return nil, fmt.Errorf("get entry: %w", err)
	}
	if entry.Tier != knowledge.TierSynced {
		return nil, fmt.Errorf("drift detection only applies to Tier 2 (synced) entries")
	}

	externalContent, err := d.fetchExternal(ctx, entry)
	if err != nil {
		return nil, fmt.Errorf("fetch external content: %w", err)
	}

	externalHash := hashContent(externalContent)
	report := &DriftReport{
		EntryID:    entry.ID,
		Title:      entry.Title,
		SourceType: entry.SourceType,
		SourceURL:  entry.SourceURL,
		DetectedAt: time.Now().UTC(),
	}

	if externalHash != entry.ContentHash {
		report.ExternalChanged = true
		report.Diff = computeDiff(entry.Content, externalContent)
	}

	return report, nil
}

// DetectAll checks drift for all Tier 2 entries, optionally filtered by source type.
// Only entries with drift are returned.
func (d *Detector) DetectAll(ctx context.Context, sourceType knowledge.SourceType) ([]*DriftReport, error) {
	entries, err := d.svc.List(ctx, knowledge.EntryFilter{
		Tier:       knowledge.TierSynced,
		SourceType: sourceType,
	})
	if err != nil {
		return nil, fmt.Errorf("list synced entries: %w", err)
	}

	var reports []*DriftReport
	for _, e := range entries {
		report, err := d.Detect(ctx, e.ID)
		if err != nil {
			d.logger.Warn("drift detection failed for entry",
				"entry_id", e.ID, "title", e.Title, "error", err)
			continue
		}
		if report.ExternalChanged || report.LocalChanged {
			reports = append(reports, report)
		}
	}
	return reports, nil
}

// --- external content fetching ---

// confluenceSourceConfig holds the minimal config fields needed.
type confluenceSourceConfig struct {
	BaseURL  string `json:"base_url"`
	APIToken string `json:"api_token"`
	Email    string `json:"email"`
}

// notionSourceConfig holds the minimal config fields needed.
type notionSourceConfig struct {
	APIToken string `json:"api_token"`
}

func (d *Detector) fetchExternal(ctx context.Context, entry *knowledge.Entry) (string, error) {
	sources, err := d.svc.ListSources(ctx)
	if err != nil {
		return "", fmt.Errorf("list sources: %w", err)
	}

	switch entry.SourceType {
	case knowledge.SourceTypeConfluence:
		for _, src := range sources {
			if src.Type != "confluence" {
				continue
			}
			var cfg confluenceSourceConfig
			if err := json.Unmarshal(src.Config, &cfg); err != nil {
				continue
			}
			return d.fetchConfluencePage(ctx, cfg, entry.SourceID)
		}
		return "", fmt.Errorf("no confluence source configured")

	case knowledge.SourceTypeNotion:
		for _, src := range sources {
			if src.Type != "notion" {
				continue
			}
			var cfg notionSourceConfig
			if err := json.Unmarshal(src.Config, &cfg); err != nil {
				continue
			}
			return d.fetchNotionPage(ctx, cfg.APIToken, entry.SourceID)
		}
		return "", fmt.Errorf("no notion source configured")

	default:
		return "", fmt.Errorf("drift detection not supported for source type %q", entry.SourceType)
	}
}

func (d *Detector) fetchConfluencePage(ctx context.Context, cfg confluenceSourceConfig, pageID string) (string, error) {
	u := fmt.Sprintf("%s/wiki/api/v2/pages/%s?body-format=storage", cfg.BaseURL, pageID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.SetBasicAuth(cfg.Email, cfg.APIToken)
	req.Header.Set("Accept", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch confluence page: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("confluence API error (status %d)", resp.StatusCode)
	}

	var pr struct {
		Body struct {
			Storage struct {
				Value string `json:"value"`
			} `json:"storage"`
		} `json:"body"`
	}
	if err := json.Unmarshal(body, &pr); err != nil {
		return "", fmt.Errorf("parse confluence page: %w", err)
	}
	return pr.Body.Storage.Value, nil
}

func (d *Detector) fetchNotionPage(ctx context.Context, token, pageID string) (string, error) {
	u := fmt.Sprintf("https://api.notion.com/v1/blocks/%s/children?page_size=100", pageID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Notion-Version", "2022-06-28")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch notion page: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("notion API error (status %d)", resp.StatusCode)
	}

	type richText struct {
		PlainText string `json:"plain_text"`
	}
	var result struct {
		Results []map[string]json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse notion blocks: %w", err)
	}

	var sb strings.Builder
	for _, block := range result.Results {
		for _, blockType := range []string{"paragraph", "heading_1", "heading_2", "heading_3",
			"bulleted_list_item", "numbered_list_item", "quote", "callout"} {
			raw, ok := block[blockType]
			if !ok {
				continue
			}
			var bt struct {
				RichText []richText `json:"rich_text"`
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

// --- helpers ---

func hashContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", sum)
}

func computeDiff(original, revised string) string {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(original, revised, false)
	return dmp.DiffPrettyText(diffs)
}
