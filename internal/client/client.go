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

// --- Source Management ---

// listSourcesResponse is the JSON shape returned by GET /api/v1/sources.
type listSourcesResponse struct {
	Sources []*store.Source `json:"sources"`
	Count   int             `json:"count"`
}

// ListSources returns all registered infrastructure sources.
func (c *Client) ListSources(ctx context.Context) ([]*store.Source, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+apiSourcesPath, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list sources request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list sources failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result listSourcesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode list sources response: %w", err)
	}

	return result.Sources, nil
}

// CreateSource registers a new infrastructure source.
func (c *Client) CreateSource(ctx context.Context, source *store.Source) (*store.Source, error) {
	payload, err := json.Marshal(source)
	if err != nil {
		return nil, fmt.Errorf("marshal source: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+apiSourcesPath, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("create source request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("create source failed (status %d): %s", resp.StatusCode, string(body))
	}

	var created store.Source
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, fmt.Errorf("decode create source response: %w", err)
	}

	return &created, nil
}

// DeleteSource removes a registered source by ID.
func (c *Client) DeleteSource(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", c.baseURL+apiSourcesPath+"/"+url.PathEscape(id), nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete source request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete source failed (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
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

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("k8s list resources request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("k8s list resources failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result k8sListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode k8s list response: %w", err)
	}

	return result.Resources, nil
}

// K8sGetResource retrieves a single Kubernetes resource.
func (c *Client) K8sGetResource(ctx context.Context, sourceID, resource, namespace, name string) (map[string]any, error) {
	u := fmt.Sprintf("%s%s/%s/resources/%s/%s/%s", c.baseURL, apiK8sBasePath,
		url.PathEscape(sourceID), url.PathEscape(resource),
		url.PathEscape(namespace), url.PathEscape(name))

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("k8s get resource request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("k8s get resource failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Resource map[string]any `json:"resource"`
		SourceID string         `json:"source_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode k8s get response: %w", err)
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

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("k8s get logs request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("k8s get logs failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result k8sLogsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode k8s logs response: %w", err)
	}

	return result.Logs, nil
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

// --- Git Operations ---

// GitReadFile reads a file from a Git repository source.
func (c *Client) GitReadFile(ctx context.Context, sourceID, path string) (string, error) {
	u := fmt.Sprintf("%s%s/%s/file?path=%s", c.baseURL, apiGitBasePath,
		url.PathEscape(sourceID), url.QueryEscape(path))

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("git read file request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("git read file failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode git read file response: %w", err)
	}

	return result.Content, nil
}

// GitListFiles lists files in a directory of a Git repository source.
func (c *Client) GitListFiles(ctx context.Context, sourceID, dir string) ([]gitadapter.FileInfo, error) {
	u := fmt.Sprintf("%s%s/%s/files", c.baseURL, apiGitBasePath, url.PathEscape(sourceID))
	if dir != "" {
		u += "?dir=" + url.QueryEscape(dir)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("git list files request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("git list files failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Files []gitadapter.FileInfo `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode git list files response: %w", err)
	}

	return result.Files, nil
}

// GitLog returns recent commits from a Git repository source.
func (c *Client) GitLog(ctx context.Context, sourceID string, limit int) ([]gitadapter.CommitInfo, error) {
	u := fmt.Sprintf("%s%s/%s/log", c.baseURL, apiGitBasePath, url.PathEscape(sourceID))
	if limit > 0 {
		u += "?limit=" + strconv.Itoa(limit)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("git log request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("git log failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Commits []gitadapter.CommitInfo `json:"commits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode git log response: %w", err)
	}

	return result.Commits, nil
}

// GitDiff returns a diff between two refs in a Git repository source.
func (c *Client) GitDiff(ctx context.Context, sourceID, from, to string) (string, error) {
	u := fmt.Sprintf("%s%s/%s/diff?from=%s&to=%s", c.baseURL, apiGitBasePath,
		url.PathEscape(sourceID), url.QueryEscape(from), url.QueryEscape(to))

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("git diff request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("git diff failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Diff string `json:"diff"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode git diff response: %w", err)
	}

	return result.Diff, nil
}

// --- AWS Resources ---

// AWSEC2ListInstances lists all EC2 instances from an AWS source.
func (c *Client) AWSEC2ListInstances(ctx context.Context, sourceID string) ([]awsadapter.EC2Instance, error) {
	u := fmt.Sprintf("%s%s/%s/ec2/instances", c.baseURL, apiAWSBasePath, url.PathEscape(sourceID))

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("aws ec2 list instances request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("aws ec2 list instances failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Instances []awsadapter.EC2Instance `json:"instances"`
		Count     int                      `json:"count"`
		SourceID  string                   `json:"source_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode aws ec2 list response: %w", err)
	}

	return result.Instances, nil
}

// AWSEC2GetInstance retrieves a single EC2 instance from an AWS source.
func (c *Client) AWSEC2GetInstance(ctx context.Context, sourceID, instanceID string) (*awsadapter.EC2Instance, error) {
	u := fmt.Sprintf("%s%s/%s/ec2/instances/%s", c.baseURL, apiAWSBasePath,
		url.PathEscape(sourceID), url.PathEscape(instanceID))

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("aws ec2 get instance request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("aws ec2 get instance failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Instance *awsadapter.EC2Instance `json:"instance"`
		SourceID string                  `json:"source_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode aws ec2 get response: %w", err)
	}

	return result.Instance, nil
}

// AWSEKSListClusters lists all EKS clusters from an AWS source.
func (c *Client) AWSEKSListClusters(ctx context.Context, sourceID string) ([]awsadapter.EKSCluster, error) {
	u := fmt.Sprintf("%s%s/%s/eks/clusters", c.baseURL, apiAWSBasePath, url.PathEscape(sourceID))

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("aws eks list clusters request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("aws eks list clusters failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Clusters []awsadapter.EKSCluster `json:"clusters"`
		Count    int                     `json:"count"`
		SourceID string                  `json:"source_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode aws eks list response: %w", err)
	}

	return result.Clusters, nil
}

// AWSEKSGetCluster retrieves a single EKS cluster from an AWS source.
func (c *Client) AWSEKSGetCluster(ctx context.Context, sourceID, clusterName string) (*awsadapter.EKSCluster, error) {
	u := fmt.Sprintf("%s%s/%s/eks/clusters/%s", c.baseURL, apiAWSBasePath,
		url.PathEscape(sourceID), url.PathEscape(clusterName))

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("aws eks get cluster request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("aws eks get cluster failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Cluster  *awsadapter.EKSCluster `json:"cluster"`
		SourceID string                 `json:"source_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode aws eks get response: %w", err)
	}

	return result.Cluster, nil
}

// AWSRDSListInstances lists all RDS instances from an AWS source.
func (c *Client) AWSRDSListInstances(ctx context.Context, sourceID string) ([]awsadapter.RDSInstance, error) {
	u := fmt.Sprintf("%s%s/%s/rds/instances", c.baseURL, apiAWSBasePath, url.PathEscape(sourceID))

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("aws rds list instances request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("aws rds list instances failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Instances []awsadapter.RDSInstance `json:"instances"`
		Count     int                      `json:"count"`
		SourceID  string                   `json:"source_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode aws rds list response: %w", err)
	}

	return result.Instances, nil
}

// AWSRDSGetInstance retrieves a single RDS instance from an AWS source.
func (c *Client) AWSRDSGetInstance(ctx context.Context, sourceID, dbInstanceID string) (*awsadapter.RDSInstance, error) {
	u := fmt.Sprintf("%s%s/%s/rds/instances/%s", c.baseURL, apiAWSBasePath,
		url.PathEscape(sourceID), url.PathEscape(dbInstanceID))

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("aws rds get instance request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("aws rds get instance failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Instance *awsadapter.RDSInstance `json:"instance"`
		SourceID string                  `json:"source_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode aws rds get response: %w", err)
	}

	return result.Instance, nil
}

// AWSVPCListVPCs lists all VPCs from an AWS source.
func (c *Client) AWSVPCListVPCs(ctx context.Context, sourceID string) ([]awsadapter.VPC, error) {
	u := fmt.Sprintf("%s%s/%s/vpc/vpcs", c.baseURL, apiAWSBasePath, url.PathEscape(sourceID))

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("aws vpc list vpcs request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("aws vpc list vpcs failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		VPCs     []awsadapter.VPC `json:"vpcs"`
		Count    int              `json:"count"`
		SourceID string           `json:"source_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode aws vpc list response: %w", err)
	}

	return result.VPCs, nil
}

// AWSVPCGetVPC retrieves a single VPC from an AWS source.
func (c *Client) AWSVPCGetVPC(ctx context.Context, sourceID, vpcID string) (*awsadapter.VPC, error) {
	u := fmt.Sprintf("%s%s/%s/vpc/vpcs/%s", c.baseURL, apiAWSBasePath,
		url.PathEscape(sourceID), url.PathEscape(vpcID))

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("aws vpc get vpc request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("aws vpc get vpc failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		VPC      *awsadapter.VPC `json:"vpc"`
		SourceID string          `json:"source_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode aws vpc get response: %w", err)
	}

	return result.VPC, nil
}
