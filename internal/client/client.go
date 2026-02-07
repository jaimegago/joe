package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
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
			Timeout: 30 * time.Second,
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
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/v1/status", nil)
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
