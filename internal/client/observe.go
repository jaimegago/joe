package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jaimegago/joe/internal/observe"
)

type observeRequest struct {
	Service  string `json:"service"`
	Question string `json:"question"`
}

// QueryMetrics asks joe-core to resolve the metrics source for the service, translate
// the question, execute it, and return a normalized ObservabilityResult.
func (c *Client) QueryMetrics(ctx context.Context, service, question string) (*observe.ObservabilityResult, error) {
	payload, err := json.Marshal(observeRequest{Service: service, Question: question})
	if err != nil {
		return nil, fmt.Errorf("marshal observe metrics request: %w", err)
	}
	var result observe.ObservabilityResult
	if err := c.doJSON(ctx, http.MethodPost, c.baseURL+apiObserveBasePath+"/metrics",
		bytes.NewReader(payload), http.StatusOK, &result, "observe metrics"); err != nil {
		return nil, err
	}
	return &result, nil
}

// QueryLogs asks joe-core to resolve the logs source for the service, translate
// the question, execute it, and return a normalized ObservabilityResult.
func (c *Client) QueryLogs(ctx context.Context, service, question string) (*observe.ObservabilityResult, error) {
	payload, err := json.Marshal(observeRequest{Service: service, Question: question})
	if err != nil {
		return nil, fmt.Errorf("marshal observe logs request: %w", err)
	}
	var result observe.ObservabilityResult
	if err := c.doJSON(ctx, http.MethodPost, c.baseURL+apiObserveBasePath+"/logs",
		bytes.NewReader(payload), http.StatusOK, &result, "observe logs"); err != nil {
		return nil, err
	}
	return &result, nil
}

// QueryTraces asks joe-core to resolve the traces source for the service and return
// recent trace results as a normalized ObservabilityResult.
func (c *Client) QueryTraces(ctx context.Context, service, question string) (*observe.ObservabilityResult, error) {
	payload, err := json.Marshal(observeRequest{Service: service, Question: question})
	if err != nil {
		return nil, fmt.Errorf("marshal observe traces request: %w", err)
	}
	var result observe.ObservabilityResult
	if err := c.doJSON(ctx, http.MethodPost, c.baseURL+apiObserveBasePath+"/traces",
		bytes.NewReader(payload), http.StatusOK, &result, "observe traces"); err != nil {
		return nil, err
	}
	return &result, nil
}

// QueryAlerts asks joe-core to resolve the alerts source for the service and return
// active alerts as a normalized AlertsResult.
func (c *Client) QueryAlerts(ctx context.Context, service, question string) (*observe.AlertsResult, error) {
	payload, err := json.Marshal(observeRequest{Service: service, Question: question})
	if err != nil {
		return nil, fmt.Errorf("marshal observe alerts request: %w", err)
	}
	var result observe.AlertsResult
	if err := c.doJSON(ctx, http.MethodPost, c.baseURL+apiObserveBasePath+"/alerts",
		bytes.NewReader(payload), http.StatusOK, &result, "observe alerts"); err != nil {
		return nil, err
	}
	return &result, nil
}

// QueryK8s asks joe-core to resolve the Kubernetes source for the service and answer
// the question (pod list or logs) as a normalized K8sResult.
func (c *Client) QueryK8s(ctx context.Context, service, question string) (*observe.K8sResult, error) {
	payload, err := json.Marshal(observeRequest{Service: service, Question: question})
	if err != nil {
		return nil, fmt.Errorf("marshal observe k8s request: %w", err)
	}
	var result observe.K8sResult
	if err := c.doJSON(ctx, http.MethodPost, c.baseURL+apiObserveBasePath+"/k8s",
		bytes.NewReader(payload), http.StatusOK, &result, "observe k8s"); err != nil {
		return nil, err
	}
	return &result, nil
}
