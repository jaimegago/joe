// Package openaicompat implements the llm.LLMAdapter interface against any
// server that speaks the OpenAI Chat Completions wire protocol — OpenAI
// itself, but also vLLM, llama.cpp, Ollama, LocalAI, text-generation-webui,
// and similar projects that expose /v1/chat/completions and /v1/embeddings.
//
// Unlike the native claude and gemini clients (each bound to a vendor SDK),
// this adapter is deliberately a small, dependency-free HTTP client. Two
// properties make hand-rolling the right call for a GENERIC endpoint:
//
//   - It emits max_tokens (not the newer max_completion_tokens). Generic
//     OpenAI-compatible servers overwhelmingly accept max_tokens; OpenAI's own
//     SDK now defaults to max_completion_tokens, which many compatible servers
//     reject. Owning the wire shape lets us pick the field that works broadly.
//   - The API key is OPTIONAL. Keyless local endpoints (llama.cpp, Ollama)
//     need an empty Authorization. The base URL, by contrast, is REQUIRED and
//     supplied via config (ModelConfig.BaseURL).
package openaicompat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jaimegago/joe/internal/env"
	"github.com/jaimegago/joe/internal/llm"
)

// defaultMaxTokens is used when a ChatRequest does not specify a limit.
const defaultMaxTokens = 4096

// Client implements llm.LLMAdapter against an OpenAI-compatible HTTP endpoint.
type Client struct {
	httpClient *http.Client
	baseURL    string // e.g. "http://localhost:11434/v1" — no trailing slash
	apiKey     string // OPTIONAL: empty is valid for keyless local endpoints
	model      string
}

// APIError carries a structured HTTP error from the compatible endpoint. It
// implements the APICode/APIMessage shape that internal/llm's instrumentation
// reads, so status codes surface in metrics exactly like the native clients.
type APIError struct {
	Code    int
	Message string
	Err     error
}

func (e *APIError) Error() string      { return e.Err.Error() }
func (e *APIError) Unwrap() error      { return e.Err }
func (e *APIError) APICode() int       { return e.Code }
func (e *APIError) APIMessage() string { return e.Message }

// NewClient builds a Client for the given model and base URL. The API key is
// read from OPENAI_API_KEY and is OPTIONAL (empty is permitted for keyless
// endpoints). The base URL is REQUIRED — callers (the factory) gate on its
// presence, and NewClient defends the invariant too.
func NewClient(model, baseURL string) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("base_url is required for the openai-compat provider (set it in the model config)")
	}
	return &Client{
		httpClient: &http.Client{Timeout: 120 * time.Second},
		baseURL:    baseURL,
		apiKey:     os.Getenv(env.OpenAIAPIKey), // optional; empty is fine
		model:      model,
	}, nil
}

// --- wire types (OpenAI Chat Completions) ---

type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type wireToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function wireFunction `json:"function"`
}

type wireFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON-encoded string per the OpenAI contract
}

type chatTool struct {
	Type     string           `json:"type"`
	Function chatToolFunction `json:"function"`
}

type chatToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
}

type chatRequest struct {
	Model     string        `json:"model"`
	Messages  []chatMessage `json:"messages"`
	Tools     []chatTool    `json:"tools,omitempty"`
	MaxTokens int           `json:"max_tokens,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content   string         `json:"content"`
			ToolCalls []wireToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// Chat sends a non-streaming chat completion request and maps the response
// back into Joe's neutral ChatResponse shape.
func (c *Client) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	messages := make([]chatMessage, 0, len(req.Messages)+1)

	// SystemPrompt becomes a leading system-role message.
	if req.SystemPrompt != "" {
		messages = append(messages, chatMessage{Role: "system", Content: req.SystemPrompt})
	}

	for _, msg := range req.Messages {
		switch {
		case msg.ToolResultID != "":
			// Tool-result inbound message → tool-role message.
			messages = append(messages, chatMessage{
				Role:       "tool",
				ToolCallID: msg.ToolResultID,
				Content:    msg.Content,
			})
		case msg.Role == "assistant":
			out := chatMessage{Role: "assistant", Content: msg.Content}
			// Replay the assistant's prior tool calls so the server sees its
			// own history (required when a tool result follows).
			for _, tc := range msg.ToolCalls {
				argsJSON, err := json.Marshal(tc.Args)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal tool call args for %q: %w", tc.Name, err)
				}
				out.ToolCalls = append(out.ToolCalls, wireToolCall{
					ID:       tc.ID,
					Type:     "function",
					Function: wireFunction{Name: tc.Name, Arguments: string(argsJSON)},
				})
			}
			messages = append(messages, out)
		default:
			// "user" and any other role map to a user-role message.
			messages = append(messages, chatMessage{Role: "user", Content: msg.Content})
		}
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = defaultMaxTokens
	}

	body := chatRequest{
		Model:     c.model,
		Messages:  messages,
		MaxTokens: maxTokens,
	}

	for _, tool := range req.Tools {
		body.Tools = append(body.Tools, chatTool{
			Type: "function",
			Function: chatToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  convertParameters(tool.Parameters),
			},
		})
	}

	var parsed chatResponse
	if err := c.post(ctx, "/chat/completions", body, &parsed); err != nil {
		return nil, err
	}

	result := &llm.ChatResponse{
		Usage: llm.TokenUsage{
			InputTokens:  parsed.Usage.PromptTokens,
			OutputTokens: parsed.Usage.CompletionTokens,
			TotalTokens:  parsed.Usage.TotalTokens,
		},
	}
	if result.Usage.TotalTokens == 0 {
		result.Usage.TotalTokens = parsed.Usage.PromptTokens + parsed.Usage.CompletionTokens
	}

	if len(parsed.Choices) > 0 {
		choice := parsed.Choices[0].Message
		result.Content = choice.Content
		for _, tc := range choice.ToolCalls {
			args := make(map[string]any)
			if tc.Function.Arguments != "" {
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
					// Preserve the tool call even if args fail to parse, so the
					// loop knows the tool was called (mirrors the claude client).
					args = map[string]any{"_parse_error": err.Error()}
				}
			}
			result.ToolCalls = append(result.ToolCalls, llm.ToolCall{
				ID:   tc.ID,
				Name: tc.Function.Name,
				Args: args,
			})
		}
	}

	return result, nil
}

// --- embeddings ---

type embeddingRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed generates an embedding via /v1/embeddings. If the endpoint does not
// support embeddings (404 / not found), it returns a clear, actionable error
// rather than a generic failure.
func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	body := embeddingRequest{Model: c.model, Input: text}

	var parsed embeddingResponse
	if err := c.post(ctx, "/embeddings", body, &parsed); err != nil {
		var apiErr *APIError
		if ok := asAPIError(err, &apiErr); ok && (apiErr.Code == http.StatusNotFound || apiErr.Code == http.StatusNotImplemented) {
			return nil, fmt.Errorf("the configured openai-compat endpoint at %s does not support embeddings (/v1/embeddings returned %d). Point embedding_model at an endpoint that implements /v1/embeddings, or disable embedding-backed features: %w", c.baseURL, apiErr.Code, err)
		}
		return nil, err
	}

	if len(parsed.Data) == 0 || len(parsed.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("openai-compat endpoint at %s returned no embedding data", c.baseURL)
	}
	return parsed.Data[0].Embedding, nil
}

// --- shared HTTP plumbing ---

// post marshals body, POSTs it to baseURL+path, and decodes a successful
// response into out. Non-2xx responses become *APIError carrying the status
// code and the raw response body.
func (c *Client) post(ctx context.Context, path string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// API key is optional: only send Authorization when one is configured.
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("openai-compat request to %s failed: %w", c.baseURL+path, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(respBody))
		return &APIError{
			Code:    resp.StatusCode,
			Message: msg,
			Err:     fmt.Errorf("openai-compat endpoint returned %d: %s", resp.StatusCode, msg),
		}
	}

	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("failed to decode openai-compat response: %w", err)
	}
	return nil
}

// convertParameters turns Joe's neutral ParameterSchema into a JSON-Schema
// object map, as the OpenAI tools[].function.parameters field expects.
func convertParameters(p llm.ParameterSchema) map[string]any {
	properties := make(map[string]any, len(p.Properties))
	for name, prop := range p.Properties {
		properties[name] = convertProperty(prop)
	}

	schemaType := p.Type
	if schemaType == "" {
		schemaType = "object"
	}
	schema := map[string]any{
		"type":       schemaType,
		"properties": properties,
	}
	if len(p.Required) > 0 {
		schema["required"] = p.Required
	}
	return schema
}

func convertProperty(prop llm.Property) map[string]any {
	out := map[string]any{"type": prop.Type}
	if prop.Description != "" {
		out["description"] = prop.Description
	}
	if prop.Type == "array" && prop.Items != nil {
		out["items"] = convertProperty(*prop.Items)
	}
	return out
}

// asAPIError is a tiny errors.As helper kept local so the embeddings path can
// inspect the status code without importing errors at every call site.
func asAPIError(err error, target **APIError) bool {
	for err != nil {
		if ae, ok := err.(*APIError); ok {
			*target = ae
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
