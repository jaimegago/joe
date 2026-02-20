package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	jaegeradapter "github.com/jaimegago/joe/internal/adapters/observability/jaeger"
	lokiadapter "github.com/jaimegago/joe/internal/adapters/observability/loki"
	prometheusadapter "github.com/jaimegago/joe/internal/adapters/observability/prometheus"
	tempoadapter "github.com/jaimegago/joe/internal/adapters/observability/tempo"
)

// --- Prometheus ---

// PrometheusQuery executes an instant PromQL query.
func (c *Client) PrometheusQuery(ctx context.Context, sourceID, query string, queryTime time.Time) (*prometheusadapter.QueryResult, error) {
	u := fmt.Sprintf("%s%s/%s/query?query=%s",
		c.baseURL, apiPrometheusBasePath,
		url.PathEscape(sourceID), url.QueryEscape(query))
	if !queryTime.IsZero() {
		u += "&time=" + strconv.FormatInt(queryTime.Unix(), 10)
	}

	var result struct {
		Result   *prometheusadapter.QueryResult `json:"result"`
		SourceID string                         `json:"source_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "prometheus query"); err != nil {
		return nil, err
	}

	return result.Result, nil
}

// PrometheusQueryRange executes a range PromQL query.
func (c *Client) PrometheusQueryRange(ctx context.Context, sourceID, query string, start, end time.Time, step time.Duration) (*prometheusadapter.QueryResult, error) {
	stepSec := int64(step.Seconds())
	if stepSec < 1 {
		stepSec = 15
	}

	u := fmt.Sprintf("%s%s/%s/query_range?query=%s&start=%d&end=%d&step=%d",
		c.baseURL, apiPrometheusBasePath,
		url.PathEscape(sourceID),
		url.QueryEscape(query),
		start.Unix(), end.Unix(), stepSec)

	var result struct {
		Result   *prometheusadapter.QueryResult `json:"result"`
		SourceID string                         `json:"source_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "prometheus query_range"); err != nil {
		return nil, err
	}

	return result.Result, nil
}

// PrometheusTargets returns Prometheus scrape targets.
func (c *Client) PrometheusTargets(ctx context.Context, sourceID string) ([]prometheusadapter.Target, error) {
	u := fmt.Sprintf("%s%s/%s/targets",
		c.baseURL, apiPrometheusBasePath, url.PathEscape(sourceID))

	var result struct {
		Targets  []prometheusadapter.Target `json:"targets"`
		Count    int                        `json:"count"`
		SourceID string                     `json:"source_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "prometheus targets"); err != nil {
		return nil, err
	}

	return result.Targets, nil
}

// --- Loki ---

// LokiQuery executes an instant LogQL query.
func (c *Client) LokiQuery(ctx context.Context, sourceID, query string, limit int, since time.Duration) (*lokiadapter.QueryResult, error) {
	sinceSec := int64(since.Seconds())
	u := fmt.Sprintf("%s%s/%s/query?query=%s&limit=%d&since=%d",
		c.baseURL, apiLokiBasePath,
		url.PathEscape(sourceID),
		url.QueryEscape(query),
		limit, sinceSec)

	var result struct {
		Result   *lokiadapter.QueryResult `json:"result"`
		SourceID string                   `json:"source_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "loki query"); err != nil {
		return nil, err
	}

	return result.Result, nil
}

// LokiQueryRange executes a range LogQL query.
func (c *Client) LokiQueryRange(ctx context.Context, sourceID, query string, start, end time.Time, limit int) (*lokiadapter.QueryResult, error) {
	u := fmt.Sprintf("%s%s/%s/query_range?query=%s&start=%d&end=%d&limit=%d",
		c.baseURL, apiLokiBasePath,
		url.PathEscape(sourceID),
		url.QueryEscape(query),
		start.Unix(), end.Unix(), limit)

	var result struct {
		Result   *lokiadapter.QueryResult `json:"result"`
		SourceID string                   `json:"source_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "loki query_range"); err != nil {
		return nil, err
	}

	return result.Result, nil
}

// --- Tempo ---

// TempoSearch searches for traces in Tempo.
func (c *Client) TempoSearch(ctx context.Context, sourceID, service, tags string, minDurationMs, maxDurationMs, limit int) ([]tempoadapter.TraceSearchResult, error) {
	u := fmt.Sprintf("%s%s/%s/search?limit=%d",
		c.baseURL, apiTempoBasePath, url.PathEscape(sourceID), limit)
	if service != "" {
		u += "&service=" + url.QueryEscape(service)
	}
	if tags != "" {
		u += "&tags=" + url.QueryEscape(tags)
	}
	if minDurationMs > 0 {
		u += "&min_duration=" + strconv.Itoa(minDurationMs)
	}
	if maxDurationMs > 0 {
		u += "&max_duration=" + strconv.Itoa(maxDurationMs)
	}

	var result struct {
		Traces   []tempoadapter.TraceSearchResult `json:"traces"`
		Count    int                              `json:"count"`
		SourceID string                           `json:"source_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "tempo search"); err != nil {
		return nil, err
	}

	return result.Traces, nil
}

// TempoGetTrace retrieves a full trace by ID from Tempo.
func (c *Client) TempoGetTrace(ctx context.Context, sourceID, traceID string) (*tempoadapter.Trace, error) {
	u := fmt.Sprintf("%s%s/%s/traces/%s",
		c.baseURL, apiTempoBasePath,
		url.PathEscape(sourceID), url.PathEscape(traceID))

	var result struct {
		Trace    *tempoadapter.Trace `json:"trace"`
		SourceID string              `json:"source_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "tempo get trace"); err != nil {
		return nil, err
	}

	return result.Trace, nil
}

// --- Jaeger ---

// JaegerServices returns all service names from Jaeger.
func (c *Client) JaegerServices(ctx context.Context, sourceID string) ([]string, error) {
	u := fmt.Sprintf("%s%s/%s/services",
		c.baseURL, apiJaegerBasePath, url.PathEscape(sourceID))

	var result struct {
		Services []string `json:"services"`
		Count    int      `json:"count"`
		SourceID string   `json:"source_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "jaeger services"); err != nil {
		return nil, err
	}

	return result.Services, nil
}

// JaegerTraces searches for traces in Jaeger by service.
func (c *Client) JaegerTraces(ctx context.Context, sourceID, service, operation string, limit int) ([]jaegeradapter.TraceSearchResult, error) {
	u := fmt.Sprintf("%s%s/%s/traces?service=%s&limit=%d",
		c.baseURL, apiJaegerBasePath,
		url.PathEscape(sourceID),
		url.QueryEscape(service), limit)
	if operation != "" {
		u += "&operation=" + url.QueryEscape(operation)
	}

	var result struct {
		Traces   []jaegeradapter.TraceSearchResult `json:"traces"`
		Count    int                               `json:"count"`
		SourceID string                            `json:"source_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "jaeger traces"); err != nil {
		return nil, err
	}

	return result.Traces, nil
}
