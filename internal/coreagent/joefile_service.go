package coreagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jaimegago/joe/internal/adapters/git"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/prompts"
	"github.com/jaimegago/joe/internal/store"
)

// JoeFileService handles .joe/ file discovery, caching, and processing
type JoeFileService struct {
	cache   store.CacheRepository
	llm     llm.LLMAdapter
	logger  *slog.Logger
	metrics *observability.Metrics
}

// NewJoeFileService creates a new .joe/ file service
func NewJoeFileService(cache store.CacheRepository, llmAdapter llm.LLMAdapter, logger *slog.Logger, metrics *observability.Metrics) *JoeFileService {
	return &JoeFileService{
		cache:   cache,
		llm:     llmAdapter,
		logger:  logger.With("component", "joe_file_service"),
		metrics: observability.EnsureMetrics(metrics),
	}
}

// ProcessJoeFiles discovers and processes all .joe/ files from a Git adapter.
// Returns the tool calls that should be executed (cached if available, or interpreted by LLM).
// An empty slice (not nil) indicates .joe/ files exist but produced no tool calls (e.g., LLM returned empty).
// A nil slice indicates no .joe/ files were found.
func (s *JoeFileService) ProcessJoeFiles(ctx context.Context, adapter git.GitAdapter, sourceID string) ([]llm.ToolCall, error) {
	s.logger.Debug("discovering .joe/ files", "source_id", sourceID)

	// Discover .joe/ files (filter for .yaml/.yml)
	fileInfos, err := adapter.ListFiles(ctx, ".joe")
	if err != nil {
		return nil, fmt.Errorf("list .joe files: %w", err)
	}

	joeFiles := make([]string, 0)
	for _, file := range fileInfos {
		if !file.IsDir && (strings.HasSuffix(file.Path, ".yaml") || strings.HasSuffix(file.Path, ".yml")) {
			joeFiles = append(joeFiles, file.Path)
		}
	}

	if len(joeFiles) == 0 {
		s.logger.Debug("no .joe/ files found", "source_id", sourceID)
		return nil, nil // nil indicates no .joe/ files found
	}

	s.logger.Debug("found .joe/ files", "source_id", sourceID, "count", len(joeFiles))

	// Process each .joe/ file
	allToolCalls := make([]llm.ToolCall, 0)
	for _, filePath := range joeFiles {
		s.logger.Debug("processing .joe/ file", "source_id", sourceID, "file", filePath)

		content, err := adapter.ReadFile(ctx, filePath)
		if err != nil {
			s.logger.Warn("failed to read .joe file", "file", filePath, "error", err)
			continue
		}

		// Compute content hash for caching
		hash := computeContentHash(content)

		// Try to get from cache
		cached, err := s.cache.Get(ctx, filePath)
		if err != nil {
			s.logger.Warn("cache lookup error", "file", filePath, "error", err)
		}

		var toolCalls []llm.ToolCall
		if cached != nil && cached.ContentHash == hash && cached.ToolCalls != nil {
			// Cache hit: deserialize and use cached tool calls
			s.logger.Debug("cache hit", "file", filePath, "hash", hash)
			if err := json.Unmarshal(cached.ToolCalls, &toolCalls); err != nil {
				s.logger.Warn("failed to unmarshal cached tool calls", "file", filePath, "error", err)
				// Fall through to LLM interpretation on deserialization error
				toolCalls = nil
			}
		}

		if toolCalls == nil {
			// Cache miss: interpret via LLM
			s.logger.Debug("cache miss, interpreting with LLM", "file", filePath, "hash", hash)
			var err error
			toolCalls, err = s.interpretJoeFile(ctx, filePath, content)
			if err != nil {
				s.logger.Warn("failed to interpret .joe file", "file", filePath, "error", err)
				continue
			}

			// Store in cache
			toolCallsJSON, err := json.Marshal(toolCalls)
			if err != nil {
				s.logger.Warn("failed to marshal tool calls for cache", "file", filePath, "error", err)
				// Continue anyway - we have tool calls even if we can't cache
			} else {
				now := time.Now()
				cacheEntry := &store.JoeFileCache{
					FilePath:    filePath,
					ContentHash: hash,
					ParsedData:  json.RawMessage(content),
					ToolCalls:   toolCallsJSON,
					ProcessedAt: now,
					ParsedAt:    now,
				}
				if err := s.cache.Set(ctx, cacheEntry); err != nil {
					s.logger.Warn("failed to cache tool calls", "file", filePath, "error", err)
					// Continue anyway - cache failure is not fatal
				} else {
					s.logger.Debug("cached tool calls", "file", filePath, "count", len(toolCalls))
				}
			}
		}

		allToolCalls = append(allToolCalls, toolCalls...)
	}

	s.logger.Info("processed .joe/ files", "source_id", sourceID, "files", len(joeFiles), "tool_calls", len(allToolCalls))
	return allToolCalls, nil
}

// interpretJoeFile sends a .joe/ file to the LLM for interpretation and returns tool calls
func (s *JoeFileService) interpretJoeFile(ctx context.Context, filePath string, content string) ([]llm.ToolCall, error) {
	s.logger.Debug("interpreting .joe file with LLM", "file", filePath)

	systemPrompt := prompts.JoeFileInterpretation

	userMessage := fmt.Sprintf("File: %s\n\nContent:\n%s", filePath, content)

	req := llm.ChatRequest{
		SystemPrompt: systemPrompt,
		Messages: []llm.Message{
			{Role: "user", Content: userMessage},
		},
		Tools:     s.buildToolDefinitions(),
		MaxTokens: 2048,
	}

	resp, err := s.llm.Chat(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("LLM chat: %w", err)
	}

	if len(resp.ToolCalls) == 0 {
		s.logger.Debug("LLM returned no tool calls", "file", filePath, "response", resp.Content)
		return nil, nil
	}

	s.logger.Debug("LLM interpreted .joe file", "file", filePath, "tool_calls", len(resp.ToolCalls))
	return resp.ToolCalls, nil
}

// buildToolDefinitions returns the tool definitions available for .joe/ interpretation
func (s *JoeFileService) buildToolDefinitions() []llm.ToolDefinition {
	return []llm.ToolDefinition{
		{
			Name:        "graph_add_node",
			Description: "Add a new infrastructure node to the knowledge graph",
			Parameters: llm.ParameterSchema{
				Type: "object",
				Properties: map[string]llm.Property{
					"node_id":   {Type: "string", Description: "Unique identifier for the node (e.g., service/payment, database/users-db)"},
					"node_type": {Type: "string", Description: "Type of infrastructure (service, postgresql, redis, kafka_topic, etc.)"},
					"metadata":  {Type: "object", Description: "Key-value pairs of node metadata (team, language, purpose, etc.)"},
				},
				Required: []string{"node_id", "node_type"},
			},
		},
		{
			Name:        "graph_add_edge",
			Description: "Create a relationship between two infrastructure nodes",
			Parameters: llm.ParameterSchema{
				Type: "object",
				Properties: map[string]llm.Property{
					"from":     {Type: "string", Description: "Source node ID"},
					"to":       {Type: "string", Description: "Target node ID"},
					"relation": {Type: "string", Description: "Type of relationship (calls, uses, produces, consumes, defines, etc.)"},
					"metadata": {Type: "object", Description: "Optional metadata about the relationship"},
				},
				Required: []string{"from", "to", "relation"},
			},
		},
		{
			Name:        "save_onboarding_fact",
			Description: "Store a fact that doesn't fit as a graph node (ownership, purpose, etc.)",
			Parameters: llm.ParameterSchema{
				Type: "object",
				Properties: map[string]llm.Property{
					"fact_type": {Type: "string", Description: "Type of fact (ownership, purpose, config_location, etc.)"},
					"subject":   {Type: "string", Description: "What this fact is about (service name, component name, etc.)"},
					"content":   {Type: "string", Description: "The fact content"},
				},
				Required: []string{"fact_type", "subject", "content"},
			},
		},
	}
}

// computeContentHash computes SHA256 hash of content for cache keying
func computeContentHash(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}
