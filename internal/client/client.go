package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
)

// APIError represents a structured error returned by the joecored API.
type APIError struct {
	Status  int
	Code    string
	Message string
	Details map[string]any
	RawBody string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("api error (%d %s): %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("api error (%d): %s", e.Status, e.RawBody)
}

type apiErrorResponse struct {
	Error   string         `json:"error"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func parseAPIError(body []byte, status int) (*APIError, bool) {
	if len(body) == 0 {
		return nil, false
	}
	var resp apiErrorResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, false
	}
	if resp.Error == "" && resp.Message == "" {
		return nil, false
	}
	return &APIError{
		Status:  status,
		Code:    resp.Error,
		Message: resp.Message,
		Details: resp.Details,
		RawBody: string(body),
	}, true
}

// Client connects to joecored HTTP API
type Client struct {
	baseURL    string
	httpClient *http.Client
	apiKey     string // Bearer token for API authentication (optional)
}

// New creates a new joecored client
func New(baseURL string, opts ...ClientOption) *Client {
	c := &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ClientOption configures optional Client settings.
type ClientOption func(*Client)

// WithAPIKey sets the Bearer token for API authentication.
func WithAPIKey(key string) ClientOption {
	return func(c *Client) {
		c.apiKey = key
	}
}

// WithTLS configures the client to use HTTPS with the system's default TLS
// trust store. Use this when joecored is started with a CA-signed certificate.
// For self-signed certificates use WithTLSConfig instead.
func WithTLS() ClientOption {
	return func(c *Client) {
		c.httpClient = &http.Client{
			Timeout:   DefaultTimeout,
			Transport: &http.Transport{},
		}
	}
}

func (c *Client) doJSON(ctx context.Context, method, url string, body io.Reader, expectedStatus int, out any, errPrefix string) error {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s request failed: %w", errPrefix, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != expectedStatus {
		bodyBytes, _ := io.ReadAll(resp.Body)
		if apiErr, ok := parseAPIError(bodyBytes, resp.StatusCode); ok {
			return apiErr
		}
		return fmt.Errorf("%s failed (status %d): %s", errPrefix, resp.StatusCode, string(bodyBytes))
	}

	if out == nil {
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s response: %w", errPrefix, err)
	}

	return nil
}

// setAuth adds the Authorization header if an API key is configured.
func (c *Client) setAuth(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}

// Status represents joecored status response
type Status struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Time    string `json:"time"`
}

// GetStatus checks if joecored is running
func (c *Client) GetStatus(ctx context.Context) (*Status, error) {
	var status Status
	if err := c.doJSON(ctx, "GET", c.baseURL+apiStatusPath, nil, http.StatusOK, &status, "status"); err != nil {
		return nil, err
	}

	return &status, nil
}

// Ping checks connectivity to joecored
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.GetStatus(ctx)
	return err
}

// graphQueryResponse is the JSON shape returned by /api/v1/graph/query.
type graphQueryResponse struct {
	Nodes []graph.Node `json:"nodes"`
	Count int          `json:"count"`
}

// GraphQuery searches the graph for nodes matching the query.
func (c *Client) GraphQuery(ctx context.Context, query string) ([]graph.Node, error) {
	u := c.baseURL + apiGraphQueryPath + "?q=" + url.QueryEscape(query)
	var result graphQueryResponse
	if err := c.doJSON(ctx, "GET", u, nil, http.StatusOK, &result, "graph query"); err != nil {
		return nil, err
	}

	return result.Nodes, nil
}

// GraphRelated finds nodes related to the given node within the specified depth.
func (c *Client) GraphRelated(ctx context.Context, nodeID string, depth int) (*graph.Subgraph, error) {
	u := c.baseURL + apiGraphRelatedPath + "?nodeID=" + url.QueryEscape(nodeID) + "&depth=" + strconv.Itoa(depth)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("graph related request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		if apiErr, ok := parseAPIError(body, resp.StatusCode); ok {
			return nil, apiErr
		}
		return nil, fmt.Errorf("node %q not found", nodeID)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if apiErr, ok := parseAPIError(body, resp.StatusCode); ok {
			return nil, apiErr
		}
		return nil, fmt.Errorf("graph related failed (status %d): %s", resp.StatusCode, string(body))
	}

	var subgraph graph.Subgraph
	if err := json.NewDecoder(resp.Body).Decode(&subgraph); err != nil {
		return nil, fmt.Errorf("decode graph related response: %w", err)
	}

	return &subgraph, nil
}

// --- Source Management ---

// listComponentsResponse is the JSON shape returned by GET /api/v1/components.
type listComponentsResponse struct {
	Components []*store.Component `json:"components"`
	Count      int                `json:"count"`
}

// ListComponents returns all registered infrastructure components.
func (c *Client) ListComponents(ctx context.Context) ([]*store.Component, error) {
	var result listComponentsResponse
	if err := c.doJSON(ctx, "GET", c.baseURL+apiComponentsPath, nil, http.StatusOK, &result, "list components"); err != nil {
		return nil, err
	}

	return result.Components, nil
}

// CreateComponent registers a new infrastructure source.
func (c *Client) CreateComponent(ctx context.Context, source *store.Component) (*store.Component, error) {
	payload, err := json.Marshal(source)
	if err != nil {
		return nil, fmt.Errorf("marshal source: %w", err)
	}
	var created store.Component
	if err := c.doJSON(ctx, "POST", c.baseURL+apiComponentsPath, bytes.NewReader(payload), http.StatusCreated, &created, "create source"); err != nil {
		return nil, err
	}

	return &created, nil
}

// DeleteComponent removes a registered source by ID.
func (c *Client) DeleteComponent(ctx context.Context, id string) error {
	return c.doJSON(ctx, "DELETE", c.baseURL+apiComponentsPath+"/"+url.PathEscape(id), nil, http.StatusNoContent, nil, "delete source")
}

// GraphSummary returns a high-level summary of the graph.
func (c *Client) GraphSummary(ctx context.Context) (*graph.GraphSummary, error) {
	var summary graph.GraphSummary
	if err := c.doJSON(ctx, "GET", c.baseURL+apiGraphSummaryPath, nil, http.StatusOK, &summary, "graph summary"); err != nil {
		return nil, err
	}

	return &summary, nil
}
