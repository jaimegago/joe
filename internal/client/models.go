package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// ModelsList is the response from GET /api/v1/models.
type ModelsList struct {
	Available []string `json:"available"`
	Current   string   `json:"current"`
}

// ListModels returns the model keys joe has configured and the active one.
func (c *Client) ListModels(ctx context.Context) (*ModelsList, error) {
	var out ModelsList
	if err := c.doJSON(ctx, "GET", c.baseURL+apiModelsPath, nil, http.StatusOK, &out, "list models"); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetModelResult is the response from POST /api/v1/models/current.
type SetModelResult struct {
	Current  string `json:"current"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// SetModel hot-swaps joe's active model to the given configured key.
func (c *Client) SetModel(ctx context.Context, name string) (*SetModelResult, error) {
	payload, err := json.Marshal(setModelBody{Name: name})
	if err != nil {
		return nil, fmt.Errorf("marshal set model: %w", err)
	}
	var out SetModelResult
	if err := c.doJSON(ctx, "POST", c.baseURL+apiModelsCurrentPath, bytes.NewReader(payload), http.StatusOK, &out, "set model"); err != nil {
		return nil, err
	}
	return &out, nil
}

type setModelBody struct {
	Name string `json:"name"`
}
