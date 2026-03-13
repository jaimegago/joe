// Package drafts generates documentation update proposals by combining
// knowledge store entries with an LLM to produce proposed content.
package drafts

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jaimegago/joe/internal/uid"
	"github.com/sergi/go-diff/diffmatchpatch"

	"github.com/jaimegago/joe/internal/knowledge"
	"github.com/jaimegago/joe/internal/knowledge/proposals"
	"github.com/jaimegago/joe/internal/llm"
)

// GenerateRequest specifies what to document.
type GenerateRequest struct {
	Topic      string               `json:"topic"`
	TargetType proposals.TargetType `json:"target_type"`
	TargetID   string               `json:"target_id"`
	Context    string               `json:"context,omitempty"`
}

// notionAPIBase is a var so tests can override it with an httptest.Server URL.
var notionAPIBase = "https://api.notion.com/v1"

// Generator creates documentation proposals using knowledge store + LLM.
type Generator struct {
	svc         *knowledge.Service
	proposalSvc *proposals.Service
	llm         llm.LLMAdapter
	httpClient  *http.Client
	logger      *slog.Logger
}

// New creates a new draft Generator.
func New(svc *knowledge.Service, proposalSvc *proposals.Service, llmAdapter llm.LLMAdapter) *Generator {
	return &Generator{
		svc:         svc,
		proposalSvc: proposalSvc,
		llm:         llmAdapter,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		logger:      slog.Default(),
	}
}

// Generate creates a documentation proposal for the given request.
func (g *Generator) Generate(ctx context.Context, req GenerateRequest) (*proposals.Proposal, error) {
	// 1. Search knowledge store for relevant entries.
	results, err := g.svc.Search(ctx, knowledge.SearchRequest{
		Query: req.Topic,
		TopK:  10,
	})
	if err != nil {
		return nil, fmt.Errorf("search knowledge store: %w", err)
	}

	var entryIDs []string
	var knowledgeCtx strings.Builder
	for _, r := range results {
		entryIDs = append(entryIDs, r.Entry.ID)
		knowledgeCtx.WriteString("### ")
		knowledgeCtx.WriteString(r.Entry.Title)
		knowledgeCtx.WriteString("\n")
		knowledgeCtx.WriteString(r.Entry.Content)
		knowledgeCtx.WriteString("\n\n")
	}

	// 2. Fetch current content from the target system.
	currentContent, targetURL, fetchErr := g.fetchCurrentContent(ctx, req)
	if fetchErr != nil {
		g.logger.Warn("could not fetch current content",
			"target_type", req.TargetType, "target_id", req.TargetID, "error", fetchErr)
	}

	// 3. Call LLM to generate proposed content.
	proposedContent, title, err := g.callLLM(ctx, req, currentContent, knowledgeCtx.String())
	if err != nil {
		return nil, fmt.Errorf("llm draft generation: %w", err)
	}

	// 4. Compute diff.
	diff := computeDiff(currentContent, proposedContent)

	// 5. Persist proposal.
	p := &proposals.Proposal{
		ID:                uid.New(),
		Title:             title,
		TargetType:        req.TargetType,
		TargetID:          req.TargetID,
		TargetURL:         targetURL,
		CurrentContent:    currentContent,
		ProposedContent:   proposedContent,
		Diff:              diff,
		Status:            proposals.StatusPending,
		Context:           req.Context,
		KnowledgeEntryIDs: entryIDs,
	}
	if err := g.proposalSvc.Create(ctx, p); err != nil {
		return nil, fmt.Errorf("persist proposal: %w", err)
	}

	g.logger.Info("generated doc proposal",
		"proposal_id", p.ID,
		"target_type", req.TargetType,
		"target_id", req.TargetID,
		"knowledge_entries", len(entryIDs),
	)
	return p, nil
}

// --- LLM generation ---

type draftResponse struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

const draftSystemPrompt = `You are a technical documentation assistant for Joe, an infrastructure copilot.
You are given:
1. A topic to document
2. Relevant knowledge entries from Joe's knowledge store
3. The current content of the document (may be empty if new)
4. Optional extra context from the user

Generate an updated documentation page that:
- Incorporates the relevant knowledge accurately
- Preserves any correct existing content
- Is written in clear, concise technical markdown
- Stays focused on the topic

Output ONLY a JSON object with fields:
  title (string, the document title, ≤120 chars)
  content (string, the full proposed documentation in markdown)

Do not include any other text or explanation.`

func (g *Generator) callLLM(ctx context.Context, req GenerateRequest, current, knowledgeCtx string) (content, title string, err error) {
	userMsg := fmt.Sprintf("Topic: %s\n\nCurrent content:\n%s\n\nRelevant knowledge:\n%s\n\nAdditional context: %s",
		req.Topic,
		orFallback(current, "(no existing content)"),
		orFallback(knowledgeCtx, "(no relevant entries found)"),
		orFallback(req.Context, "(none)"),
	)

	resp, err := g.llm.Chat(ctx, llm.ChatRequest{
		SystemPrompt: draftSystemPrompt,
		Messages:     []llm.Message{{Role: "user", Content: userMsg}},
		MaxTokens:    4096,
	})
	if err != nil {
		return "", "", fmt.Errorf("llm chat: %w", err)
	}

	raw := strings.TrimSpace(resp.Content)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var dr draftResponse
	if err := json.Unmarshal([]byte(raw), &dr); err != nil {
		return "", "", fmt.Errorf("parse draft JSON: %w (raw: %.200s)", err, raw)
	}
	if dr.Title == "" {
		dr.Title = req.Topic
	}
	return dr.Content, dr.Title, nil
}

// --- content fetching ---

// confluenceSourceConfig holds the fields we need from a confluence source config.
type confluenceSourceConfig struct {
	BaseURL  string `json:"base_url"`
	APIToken string `json:"api_token"`
	Email    string `json:"email"`
}

// notionSourceConfig holds the fields we need from a notion source config.
type notionSourceConfig struct {
	APIToken string `json:"api_token"`
}

func (g *Generator) fetchCurrentContent(ctx context.Context, req GenerateRequest) (content, targetURL string, err error) {
	sources, err := g.svc.ListSources(ctx)
	if err != nil {
		return "", "", fmt.Errorf("list sources: %w", err)
	}

	switch req.TargetType {
	case proposals.TargetConfluence:
		for _, src := range sources {
			if src.Type != "confluence" {
				continue
			}
			var cfg confluenceSourceConfig
			if err := json.Unmarshal(src.Config, &cfg); err != nil {
				continue
			}
			content, targetURL, err = g.fetchConfluencePage(ctx, cfg, req.TargetID)
			return content, targetURL, err
		}
		return "", "", nil // no confluence source, proceed without current content

	case proposals.TargetNotion:
		for _, src := range sources {
			if src.Type != "notion" {
				continue
			}
			var cfg notionSourceConfig
			if err := json.Unmarshal(src.Config, &cfg); err != nil {
				continue
			}
			content, err = g.fetchNotionPage(ctx, cfg.APIToken, req.TargetID)
			targetURL = "https://www.notion.so/" + strings.ReplaceAll(req.TargetID, "-", "")
			return content, targetURL, err
		}
		return "", "", nil

	default:
		return "", "", nil
	}
}

func (g *Generator) fetchConfluencePage(ctx context.Context, cfg confluenceSourceConfig, pageID string) (content, webURL string, err error) {
	u := fmt.Sprintf("%s/wiki/api/v2/pages/%s?body-format=storage", cfg.BaseURL, pageID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", "", fmt.Errorf("create request: %w", err)
	}
	req.SetBasicAuth(cfg.Email, cfg.APIToken)
	req.Header.Set("Accept", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("fetch confluence page: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("confluence API error (status %d)", resp.StatusCode)
	}

	var pr struct {
		Body struct {
			Storage struct {
				Value string `json:"value"`
			} `json:"storage"`
		} `json:"body"`
		Links struct {
			WebUI string `json:"webui"`
		} `json:"_links"`
	}
	if err := json.Unmarshal(body, &pr); err != nil {
		return "", "", fmt.Errorf("parse confluence page: %w", err)
	}
	return pr.Body.Storage.Value, cfg.BaseURL + "/wiki" + pr.Links.WebUI, nil
}

func (g *Generator) fetchNotionPage(ctx context.Context, token, pageID string) (string, error) {
	u := fmt.Sprintf("%s/blocks/%s/children?page_size=100", notionAPIBase, pageID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Notion-Version", "2022-06-28")

	resp, err := g.httpClient.Do(req)
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

// --- diff ---

func computeDiff(original, revised string) string {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(original, revised, false)
	return dmp.DiffPrettyText(diffs)
}

func orFallback(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
