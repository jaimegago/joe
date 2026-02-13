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

	awsadapter "github.com/jaimegago/joe/internal/adapters/aws"
	gitadapter "github.com/jaimegago/joe/internal/adapters/git"
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

func (c *Client) doJSON(ctx context.Context, method, url string, body io.Reader, expectedStatus int, out any, errPrefix string) error {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

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

// listSourcesResponse is the JSON shape returned by GET /api/v1/sources.
type listSourcesResponse struct {
	Sources []*store.Source `json:"sources"`
	Count   int             `json:"count"`
}

// ListSources returns all registered infrastructure sources.
func (c *Client) ListSources(ctx context.Context) ([]*store.Source, error) {
	var result listSourcesResponse
	if err := c.doJSON(ctx, "GET", c.baseURL+apiSourcesPath, nil, http.StatusOK, &result, "list sources"); err != nil {
		return nil, err
	}

	return result.Sources, nil
}

// CreateSource registers a new infrastructure source.
func (c *Client) CreateSource(ctx context.Context, source *store.Source) (*store.Source, error) {
	payload, err := json.Marshal(source)
	if err != nil {
		return nil, fmt.Errorf("marshal source: %w", err)
	}
	var created store.Source
	if err := c.doJSON(ctx, "POST", c.baseURL+apiSourcesPath, bytes.NewReader(payload), http.StatusCreated, &created, "create source"); err != nil {
		return nil, err
	}

	return &created, nil
}

// DeleteSource removes a registered source by ID.
func (c *Client) DeleteSource(ctx context.Context, id string) error {
	return c.doJSON(ctx, "DELETE", c.baseURL+apiSourcesPath+"/"+url.PathEscape(id), nil, http.StatusNoContent, nil, "delete source")
}

// --- K8s Resources ---

// k8sListResponse is the JSON shape returned by GET /api/v1/k8s/{sourceID}/resources.
type k8sListResponse struct {
	Resources []map[string]any `json:"resources"`
	Count     int              `json:"count"`
	SourceID  string           `json:"source_id"`
}

// K8sListResources lists Kubernetes resources of a given type.
func (c *Client) K8sListResources(ctx context.Context, sourceID, resource, namespace string) ([]map[string]any, error) {
	u := fmt.Sprintf("%s%s/%s/resources?resource=%s", c.baseURL, apiK8sBasePath,
		url.PathEscape(sourceID), url.QueryEscape(resource))
	if namespace != "" {
		u += "&namespace=" + url.QueryEscape(namespace)
	}

	var result k8sListResponse
	if err := c.doJSON(ctx, "GET", u, nil, http.StatusOK, &result, "k8s list resources"); err != nil {
		return nil, err
	}

	return result.Resources, nil
}

// K8sGetResource retrieves a single Kubernetes resource.
func (c *Client) K8sGetResource(ctx context.Context, sourceID, resource, namespace, name string) (map[string]any, error) {
	u := fmt.Sprintf("%s%s/%s/resources/%s/%s/%s", c.baseURL, apiK8sBasePath,
		url.PathEscape(sourceID), url.PathEscape(resource),
		url.PathEscape(namespace), url.PathEscape(name))

	var result struct {
		Resource map[string]any `json:"resource"`
		SourceID string         `json:"source_id"`
	}
	if err := c.doJSON(ctx, "GET", u, nil, http.StatusOK, &result, "k8s get resource"); err != nil {
		return nil, err
	}

	return result.Resource, nil
}

// k8sLogsResponse is the JSON shape returned by GET /api/v1/k8s/{sourceID}/logs/{namespace}/{pod}.
type k8sLogsResponse struct {
	Logs      string `json:"logs"`
	Pod       string `json:"pod"`
	Namespace string `json:"namespace"`
	SourceID  string `json:"source_id"`
}

// K8sGetLogs retrieves logs from a Kubernetes pod.
func (c *Client) K8sGetLogs(ctx context.Context, sourceID, namespace, pod, container string, tailLines int) (string, error) {
	u := fmt.Sprintf("%s%s/%s/logs/%s/%s", c.baseURL, apiK8sBasePath,
		url.PathEscape(sourceID), url.PathEscape(namespace), url.PathEscape(pod))

	params := url.Values{}
	if container != "" {
		params.Set("container", container)
	}
	if tailLines > 0 {
		params.Set("tail", strconv.Itoa(tailLines))
	}
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	var result k8sLogsResponse
	if err := c.doJSON(ctx, "GET", u, nil, http.StatusOK, &result, "k8s get logs"); err != nil {
		return "", err
	}

	return result.Logs, nil
}

// GraphSummary returns a high-level summary of the graph.
func (c *Client) GraphSummary(ctx context.Context) (*graph.GraphSummary, error) {
	var summary graph.GraphSummary
	if err := c.doJSON(ctx, "GET", c.baseURL+apiGraphSummaryPath, nil, http.StatusOK, &summary, "graph summary"); err != nil {
		return nil, err
	}

	return &summary, nil
}

// --- Git Operations ---

// GitReadFile reads a file from a Git repository source.
func (c *Client) GitReadFile(ctx context.Context, sourceID, path string) (string, error) {
	u := fmt.Sprintf("%s%s/%s/file?path=%s", c.baseURL, apiGitBasePath,
		url.PathEscape(sourceID), url.QueryEscape(path))

	var result struct {
		Content string `json:"content"`
	}
	if err := c.doJSON(ctx, "GET", u, nil, http.StatusOK, &result, "git read file"); err != nil {
		return "", err
	}

	return result.Content, nil
}

// GitListFiles lists files in a directory of a Git repository source.
func (c *Client) GitListFiles(ctx context.Context, sourceID, dir string) ([]gitadapter.FileInfo, error) {
	u := fmt.Sprintf("%s%s/%s/files", c.baseURL, apiGitBasePath, url.PathEscape(sourceID))
	if dir != "" {
		u += "?dir=" + url.QueryEscape(dir)
	}

	var result struct {
		Files []gitadapter.FileInfo `json:"files"`
	}
	if err := c.doJSON(ctx, "GET", u, nil, http.StatusOK, &result, "git list files"); err != nil {
		return nil, err
	}

	return result.Files, nil
}

// GitLog returns recent commits from a Git repository source.
func (c *Client) GitLog(ctx context.Context, sourceID string, limit int) ([]gitadapter.CommitInfo, error) {
	u := fmt.Sprintf("%s%s/%s/log", c.baseURL, apiGitBasePath, url.PathEscape(sourceID))
	if limit > 0 {
		u += "?limit=" + strconv.Itoa(limit)
	}

	var result struct {
		Commits []gitadapter.CommitInfo `json:"commits"`
	}
	if err := c.doJSON(ctx, "GET", u, nil, http.StatusOK, &result, "git log"); err != nil {
		return nil, err
	}

	return result.Commits, nil
}

// GitDiff returns a diff between two refs in a Git repository source.
func (c *Client) GitDiff(ctx context.Context, sourceID, from, to string) (string, error) {
	u := fmt.Sprintf("%s%s/%s/diff?from=%s&to=%s", c.baseURL, apiGitBasePath,
		url.PathEscape(sourceID), url.QueryEscape(from), url.QueryEscape(to))

	var result struct {
		Diff string `json:"diff"`
	}
	if err := c.doJSON(ctx, "GET", u, nil, http.StatusOK, &result, "git diff"); err != nil {
		return "", err
	}

	return result.Diff, nil
}

// --- AWS Resources ---

// AWSEC2ListInstances lists all EC2 instances from an AWS source.
func (c *Client) AWSEC2ListInstances(ctx context.Context, sourceID string) ([]awsadapter.EC2Instance, error) {
	u := fmt.Sprintf("%s%s/%s/ec2/instances", c.baseURL, apiAWSBasePath, url.PathEscape(sourceID))

	var result struct {
		Instances []awsadapter.EC2Instance `json:"instances"`
		Count     int                      `json:"count"`
		SourceID  string                   `json:"source_id"`
	}
	if err := c.doJSON(ctx, "GET", u, nil, http.StatusOK, &result, "aws ec2 list instances"); err != nil {
		return nil, err
	}

	return result.Instances, nil
}

// AWSEC2GetInstance retrieves a single EC2 instance from an AWS source.
func (c *Client) AWSEC2GetInstance(ctx context.Context, sourceID, instanceID string) (*awsadapter.EC2Instance, error) {
	u := fmt.Sprintf("%s%s/%s/ec2/instances/%s", c.baseURL, apiAWSBasePath,
		url.PathEscape(sourceID), url.PathEscape(instanceID))

	var result struct {
		Instance *awsadapter.EC2Instance `json:"instance"`
		SourceID string                  `json:"source_id"`
	}
	if err := c.doJSON(ctx, "GET", u, nil, http.StatusOK, &result, "aws ec2 get instance"); err != nil {
		return nil, err
	}

	return result.Instance, nil
}

// AWSEKSListClusters lists all EKS clusters from an AWS source.
func (c *Client) AWSEKSListClusters(ctx context.Context, sourceID string) ([]awsadapter.EKSCluster, error) {
	u := fmt.Sprintf("%s%s/%s/eks/clusters", c.baseURL, apiAWSBasePath, url.PathEscape(sourceID))

	var result struct {
		Clusters []awsadapter.EKSCluster `json:"clusters"`
		Count    int                     `json:"count"`
		SourceID string                  `json:"source_id"`
	}
	if err := c.doJSON(ctx, "GET", u, nil, http.StatusOK, &result, "aws eks list clusters"); err != nil {
		return nil, err
	}

	return result.Clusters, nil
}

// AWSEKSGetCluster retrieves a single EKS cluster from an AWS source.
func (c *Client) AWSEKSGetCluster(ctx context.Context, sourceID, clusterName string) (*awsadapter.EKSCluster, error) {
	u := fmt.Sprintf("%s%s/%s/eks/clusters/%s", c.baseURL, apiAWSBasePath,
		url.PathEscape(sourceID), url.PathEscape(clusterName))

	var result struct {
		Cluster  *awsadapter.EKSCluster `json:"cluster"`
		SourceID string                 `json:"source_id"`
	}
	if err := c.doJSON(ctx, "GET", u, nil, http.StatusOK, &result, "aws eks get cluster"); err != nil {
		return nil, err
	}

	return result.Cluster, nil
}

// AWSRDSListInstances lists all RDS instances from an AWS source.
func (c *Client) AWSRDSListInstances(ctx context.Context, sourceID string) ([]awsadapter.RDSInstance, error) {
	u := fmt.Sprintf("%s%s/%s/rds/instances", c.baseURL, apiAWSBasePath, url.PathEscape(sourceID))

	var result struct {
		Instances []awsadapter.RDSInstance `json:"instances"`
		Count     int                      `json:"count"`
		SourceID  string                   `json:"source_id"`
	}
	if err := c.doJSON(ctx, "GET", u, nil, http.StatusOK, &result, "aws rds list instances"); err != nil {
		return nil, err
	}

	return result.Instances, nil
}

// AWSRDSGetInstance retrieves a single RDS instance from an AWS source.
func (c *Client) AWSRDSGetInstance(ctx context.Context, sourceID, dbInstanceID string) (*awsadapter.RDSInstance, error) {
	u := fmt.Sprintf("%s%s/%s/rds/instances/%s", c.baseURL, apiAWSBasePath,
		url.PathEscape(sourceID), url.PathEscape(dbInstanceID))

	var result struct {
		Instance *awsadapter.RDSInstance `json:"instance"`
		SourceID string                  `json:"source_id"`
	}
	if err := c.doJSON(ctx, "GET", u, nil, http.StatusOK, &result, "aws rds get instance"); err != nil {
		return nil, err
	}

	return result.Instance, nil
}

// AWSVPCListVPCs lists all VPCs from an AWS source.
func (c *Client) AWSVPCListVPCs(ctx context.Context, sourceID string) ([]awsadapter.VPC, error) {
	u := fmt.Sprintf("%s%s/%s/vpc/vpcs", c.baseURL, apiAWSBasePath, url.PathEscape(sourceID))

	var result struct {
		VPCs     []awsadapter.VPC `json:"vpcs"`
		Count    int              `json:"count"`
		SourceID string           `json:"source_id"`
	}
	if err := c.doJSON(ctx, "GET", u, nil, http.StatusOK, &result, "aws vpc list vpcs"); err != nil {
		return nil, err
	}

	return result.VPCs, nil
}

// AWSVPCGetVPC retrieves a single VPC from an AWS source.
func (c *Client) AWSVPCGetVPC(ctx context.Context, sourceID, vpcID string) (*awsadapter.VPC, error) {
	u := fmt.Sprintf("%s%s/%s/vpc/vpcs/%s", c.baseURL, apiAWSBasePath,
		url.PathEscape(sourceID), url.PathEscape(vpcID))

	var result struct {
		VPC      *awsadapter.VPC `json:"vpc"`
		SourceID string          `json:"source_id"`
	}
	if err := c.doJSON(ctx, "GET", u, nil, http.StatusOK, &result, "aws vpc get vpc"); err != nil {
		return nil, err
	}

	return result.VPC, nil
}
