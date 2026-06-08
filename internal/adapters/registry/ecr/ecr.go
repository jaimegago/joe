// Package ecr provides an adapter for AWS Elastic Container Registry (ECR).
// It uses the AWS SDK v2 and follows the same credential resolution pattern
// as the AWS adapter in internal/adapters/aws/.
package ecr

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	awsecr "github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/store"
)

const (
	statusNotConnected = "not connected to ECR"
	statusConnectedFmt = "connected to ECR region %s"
)

// ecrClient is the interface satisfied by *awsecr.Client, used for test injection.
type ecrClient interface {
	DescribeRegistry(ctx context.Context, params *awsecr.DescribeRegistryInput, optFns ...func(*awsecr.Options)) (*awsecr.DescribeRegistryOutput, error)
	DescribeRepositories(ctx context.Context, params *awsecr.DescribeRepositoriesInput, optFns ...func(*awsecr.Options)) (*awsecr.DescribeRepositoriesOutput, error)
	DescribeImages(ctx context.Context, params *awsecr.DescribeImagesInput, optFns ...func(*awsecr.Options)) (*awsecr.DescribeImagesOutput, error)
}

// ECRAdapter extends the base Adapter with ECR-specific operations.
type ECRAdapter interface {
	adapters.Adapter
	ListRepositories(ctx context.Context) ([]Repository, error)
	ListImages(ctx context.Context, repo string) ([]ImageDetail, error)
	GetImageDetails(ctx context.Context, repo, tag string) (*ImageDetail, error)
}

// Repository describes an ECR repository.
type Repository struct {
	Name      string `json:"name"`
	URI       string `json:"uri"`        // Full ECR URI: <account>.dkr.ecr.<region>.amazonaws.com/<name>
	CreatedAt string `json:"created_at"` // RFC3339
}

// ImageDetail holds metadata for an ECR image.
type ImageDetail struct {
	Digest       string   `json:"digest"`
	Tags         []string `json:"tags,omitempty"`
	PushedAt     string   `json:"pushed_at"` // RFC3339
	SizeBytes    int64    `json:"size_bytes"`
	ScanFindings string   `json:"scan_findings,omitempty"` // e.g. "CRITICAL:1,HIGH:3"
}

// Adapter implements ECRAdapter.
type Adapter struct {
	mu        sync.RWMutex
	config    Config
	ecr       ecrClient
	connected bool
}

// New creates a new ECR adapter (not yet connected).
func New() *Adapter {
	return &Adapter{}
}

// NewWithClient creates an adapter with an injected ECR client (for testing).
func NewWithClient(client ecrClient, cfg Config) *Adapter {
	return &Adapter{
		config:    cfg,
		ecr:       client,
		connected: true,
	}
}

// Connect establishes AWS credentials and validates ECR access.
func (a *Adapter) Connect(ctx context.Context, source store.Component) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// source.Config is json.RawMessage; unmarshal to map then parse, mirroring
	// the pattern used by the AWS adapter.
	var configMap map[string]any
	if len(source.Config) > 0 {
		if err := json.Unmarshal(source.Config, &configMap); err != nil {
			return fmt.Errorf("parse ecr source config JSON: %w", err)
		}
	} else {
		configMap = make(map[string]any)
	}

	cfg, err := ParseConfig(configMap)
	if err != nil {
		return fmt.Errorf("parse ecr source config: %w", err)
	}
	a.config = cfg

	awsCfg, err := buildAWSConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("build AWS config for ECR: %w", err)
	}

	a.ecr = awsecr.NewFromConfig(awsCfg)

	// Validate credentials by describing the registry.
	if _, err := a.ecr.DescribeRegistry(ctx, &awsecr.DescribeRegistryInput{}); err != nil {
		return fmt.Errorf("validate ECR access: %w", err)
	}

	a.connected = true
	return nil
}

// Disconnect clears the adapter state.
func (a *Adapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.connected = false
	a.ecr = nil
	return nil
}

// Status returns the current connection status.
func (a *Adapter) Status() adapters.Status {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.connected {
		return adapters.Status{Connected: true, Message: fmt.Sprintf(statusConnectedFmt, a.config.Region)}
	}
	return adapters.Status{Connected: false, Message: statusNotConnected}
}

// ListRepositories returns all ECR repositories for the configured account.
func (a *Adapter) ListRepositories(ctx context.Context) ([]Repository, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	var repos []Repository
	var nextToken *string

	for {
		input := &awsecr.DescribeRepositoriesInput{}
		if a.config.RegistryID != "" {
			input.RegistryId = awssdk.String(a.config.RegistryID)
		}
		if nextToken != nil {
			input.NextToken = nextToken
		}

		out, err := a.ecr.DescribeRepositories(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("describe ECR repositories: %w", err)
		}

		for _, r := range out.Repositories {
			repo := Repository{
				Name: awssdk.ToString(r.RepositoryName),
				URI:  awssdk.ToString(r.RepositoryUri),
			}
			if r.CreatedAt != nil {
				repo.CreatedAt = r.CreatedAt.UTC().Format(time.RFC3339)
			}
			repos = append(repos, repo)
		}

		if out.NextToken == nil {
			break
		}
		nextToken = out.NextToken
	}

	return repos, nil
}

// ListImages returns all images in the given repository.
func (a *Adapter) ListImages(ctx context.Context, repo string) ([]ImageDetail, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	input := &awsecr.DescribeImagesInput{
		RepositoryName: awssdk.String(repo),
	}
	if a.config.RegistryID != "" {
		input.RegistryId = awssdk.String(a.config.RegistryID)
	}

	out, err := a.ecr.DescribeImages(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("describe ECR images in %s: %w", repo, err)
	}

	return convertImageDetails(out.ImageDetails), nil
}

// GetImageDetails returns details for a specific tagged image.
func (a *Adapter) GetImageDetails(ctx context.Context, repo, tag string) (*ImageDetail, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	input := &awsecr.DescribeImagesInput{
		RepositoryName: awssdk.String(repo),
		ImageIds: []ecrtypes.ImageIdentifier{
			{ImageTag: awssdk.String(tag)},
		},
	}
	if a.config.RegistryID != "" {
		input.RegistryId = awssdk.String(a.config.RegistryID)
	}

	out, err := a.ecr.DescribeImages(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("describe ECR image %s:%s: %w", repo, tag, err)
	}
	if len(out.ImageDetails) == 0 {
		return nil, fmt.Errorf("image %s:%s not found", repo, tag)
	}

	details := convertImageDetails(out.ImageDetails)
	return &details[0], nil
}

// checkConnected returns an error if the adapter is not connected.
func (a *Adapter) checkConnected() error {
	if !a.connected {
		return fmt.Errorf("ecr adapter not connected")
	}
	return nil
}

// buildAWSConfig constructs an aws.Config from the ECR adapter config.
// Mirrors the implementation in internal/adapters/aws/aws.go.
func buildAWSConfig(ctx context.Context, cfg Config) (awssdk.Config, error) {
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cfg.Region))
	if err != nil {
		return awssdk.Config{}, fmt.Errorf("load default AWS config: %w", err)
	}

	if cfg.Profile != "" {
		awsCfg, err = config.LoadDefaultConfig(ctx,
			config.WithRegion(cfg.Region),
			config.WithSharedConfigProfile(cfg.Profile),
		)
		if err != nil {
			return awssdk.Config{}, fmt.Errorf("load AWS config with profile %s: %w", cfg.Profile, err)
		}
	}

	if cfg.AccessKey != "" && cfg.SecretKey != "" {
		awsCfg.Credentials = credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")
	}

	if cfg.RoleARN != "" {
		stsClient := sts.NewFromConfig(awsCfg)
		awsCfg.Credentials = stscreds.NewAssumeRoleProvider(stsClient, cfg.RoleARN)
	}

	return awsCfg, nil
}

// convertImageDetails maps ECR SDK ImageDetail slices to the local type.
func convertImageDetails(details []ecrtypes.ImageDetail) []ImageDetail {
	result := make([]ImageDetail, 0, len(details))
	for _, d := range details {
		img := ImageDetail{
			Digest:    awssdk.ToString(d.ImageDigest),
			Tags:      d.ImageTags,
			SizeBytes: awssdk.ToInt64(d.ImageSizeInBytes),
		}
		if d.ImagePushedAt != nil {
			img.PushedAt = d.ImagePushedAt.UTC().Format(time.RFC3339)
		}
		if d.ImageScanFindingsSummary != nil {
			img.ScanFindings = formatScanFindings(d.ImageScanFindingsSummary.FindingSeverityCounts)
		}
		result = append(result, img)
	}
	return result
}

// formatScanFindings converts a severity count map to a compact string, e.g. "CRITICAL:1,HIGH:3".
func formatScanFindings(counts map[string]int32) string {
	if len(counts) == 0 {
		return ""
	}
	order := []string{"CRITICAL", "HIGH", "MEDIUM", "LOW", "INFORMATIONAL", "UNDEFINED"}
	var parts []string
	for _, sev := range order {
		if n, ok := counts[sev]; ok && n > 0 {
			parts = append(parts, fmt.Sprintf("%s:%d", sev, n))
		}
	}
	return strings.Join(parts, ",")
}
