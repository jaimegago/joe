package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/jaimegago/joe/internal/graph"
)

// Client connects to joecored HTTP API
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// New creates a new joecored client
func New(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
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
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+apiStatusPath, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var status Status
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
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
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("graph query request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("graph query failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result graphQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode graph query response: %w", err)
	}

	return result.Nodes, nil
}

// GraphRelated finds nodes related to the given node within the specified depth.
func (c *Client) GraphRelated(ctx context.Context, nodeID string, depth int) (*graph.Subgraph, error) {
	u := c.baseURL + apiGraphRelatedPath + url.PathEscape(nodeID) + "?depth=" + strconv.Itoa(depth)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("graph related request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("node %q not found", nodeID)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("graph related failed (status %d): %s", resp.StatusCode, string(body))
	}

	var subgraph graph.Subgraph
	if err := json.NewDecoder(resp.Body).Decode(&subgraph); err != nil {
		return nil, fmt.Errorf("decode graph related response: %w", err)
	}

	return &subgraph, nil
}

// GraphSummary returns a high-level summary of the graph.
func (c *Client) GraphSummary(ctx context.Context) (*graph.GraphSummary, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+apiGraphSummaryPath, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("graph summary request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("graph summary failed (status %d): %s", resp.StatusCode, string(body))
	}

	var summary graph.GraphSummary
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		return nil, fmt.Errorf("decode graph summary response: %w", err)
	}

	return &summary, nil
}
