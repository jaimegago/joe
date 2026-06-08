package aws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/store"
)

const (
	// Time formats
	timeFormatRFC3339 = time.RFC3339

	// Status messages
	statusNotConnected = "Not connected to AWS"
	statusConnectedFmt = "Connected to AWS region %s"
)

var (
	// ErrNotConnected indicates the adapter has not established AWS connectivity.
	ErrNotConnected = errors.New("adapter not connected to AWS")
	// ErrInstanceNotFound indicates an EC2 instance lookup failed to find a match.
	ErrInstanceNotFound = errors.New("instance not found")
	// ErrClusterNotFound indicates an EKS cluster lookup failed to find a match.
	ErrClusterNotFound = errors.New("cluster not found")
	// ErrDBInstanceNotFound indicates an RDS instance lookup failed to find a match.
	ErrDBInstanceNotFound = errors.New("database instance not found")
	// ErrVPCNotFound indicates a VPC lookup failed to find a match.
	ErrVPCNotFound = errors.New("VPC not found")
)

// EC2Instance represents an EC2 instance
type EC2Instance struct {
	InstanceID       string            `json:"instance_id"`
	InstanceType     string            `json:"instance_type"`
	State            string            `json:"state"`
	PublicIP         string            `json:"public_ip,omitempty"`
	PrivateIP        string            `json:"private_ip,omitempty"`
	VpcID            string            `json:"vpc_id,omitempty"`
	SubnetID         string            `json:"subnet_id,omitempty"`
	SecurityGroups   []SecurityGroup   `json:"security_groups,omitempty"`
	Tags             map[string]string `json:"tags,omitempty"`
	LaunchTime       string            `json:"launch_time,omitempty"`
	AvailabilityZone string            `json:"availability_zone,omitempty"`
}

// EKSCluster represents an EKS cluster
type EKSCluster struct {
	Name            string            `json:"name"`
	ARN             string            `json:"arn"`
	Version         string            `json:"version"`
	Status          string            `json:"status"`
	Endpoint        string            `json:"endpoint,omitempty"`
	RoleARN         string            `json:"role_arn"`
	VpcID           string            `json:"vpc_id,omitempty"`
	VpcConfig       VPCConfig         `json:"vpc_config,omitempty"`
	Tags            map[string]string `json:"tags,omitempty"`
	CreatedAt       string            `json:"created_at,omitempty"`
	PlatformVersion string            `json:"platform_version,omitempty"`
}

// RDSInstance represents an RDS instance
type RDSInstance struct {
	DBInstanceID   string            `json:"db_instance_id"`
	DBName         string            `json:"db_name,omitempty"`
	Engine         string            `json:"engine"`
	EngineVersion  string            `json:"engine_version"`
	InstanceClass  string            `json:"instance_class"`
	Status         string            `json:"status"`
	Endpoint       string            `json:"endpoint,omitempty"`
	Port           int32             `json:"port,omitempty"`
	VpcID          string            `json:"vpc_id,omitempty"`
	SubnetGroup    string            `json:"subnet_group,omitempty"`
	SecurityGroups []SecurityGroup   `json:"security_groups,omitempty"`
	Tags           map[string]string `json:"tags,omitempty"`
	CreatedTime    string            `json:"created_time,omitempty"`
}

// VPC represents a Virtual Private Cloud
type VPC struct {
	VpcID     string            `json:"vpc_id"`
	CidrBlock string            `json:"cidr_block"`
	State     string            `json:"state"`
	IsDefault bool              `json:"is_default"`
	Tags      map[string]string `json:"tags,omitempty"`
	Subnets   []Subnet          `json:"subnets,omitempty"`
}

// Subnet represents a VPC subnet
type Subnet struct {
	SubnetID         string            `json:"subnet_id"`
	VpcID            string            `json:"vpc_id"`
	CidrBlock        string            `json:"cidr_block"`
	AvailabilityZone string            `json:"availability_zone"`
	State            string            `json:"state"`
	Tags             map[string]string `json:"tags,omitempty"`
}

// SecurityGroup represents a security group
type SecurityGroup struct {
	GroupID   string `json:"group_id"`
	GroupName string `json:"group_name"`
}

// VPCConfig represents EKS VPC configuration
type VPCConfig struct {
	SubnetIDs        []string `json:"subnet_ids,omitempty"`
	SecurityGroupIDs []string `json:"security_group_ids,omitempty"`
	EndpointConfig   string   `json:"endpoint_config,omitempty"`
}

// AWSAdapter extends the base Adapter with AWS-specific operations
type AWSAdapter interface {
	adapters.Adapter
	// EC2 operations
	ListEC2Instances(ctx context.Context) ([]EC2Instance, error)
	GetEC2Instance(ctx context.Context, instanceID string) (*EC2Instance, error)
	// EKS operations
	ListEKSClusters(ctx context.Context) ([]EKSCluster, error)
	GetEKSCluster(ctx context.Context, clusterName string) (*EKSCluster, error)
	// RDS operations
	ListRDSInstances(ctx context.Context) ([]RDSInstance, error)
	GetRDSInstance(ctx context.Context, dbInstanceID string) (*RDSInstance, error)
	// VPC operations
	ListVPCs(ctx context.Context) ([]VPC, error)
	GetVPC(ctx context.Context, vpcID string) (*VPC, error)
}

// Adapter is the concrete AWS adapter using AWS SDK v2
type Adapter struct {
	mu        sync.RWMutex
	config    Config
	awsConfig aws.Config
	ec2Client ec2Client
	eksClient eksClient
	rdsClient rdsClient
	connected bool
}

type ec2Client interface {
	DescribeInstances(ctx context.Context, params *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	DescribeVpcs(ctx context.Context, params *ec2.DescribeVpcsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error)
	DescribeSubnets(ctx context.Context, params *ec2.DescribeSubnetsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error)
}

type eksClient interface {
	ListClusters(ctx context.Context, params *eks.ListClustersInput, optFns ...func(*eks.Options)) (*eks.ListClustersOutput, error)
	DescribeCluster(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error)
}

type rdsClient interface {
	DescribeDBInstances(ctx context.Context, params *rds.DescribeDBInstancesInput, optFns ...func(*rds.Options)) (*rds.DescribeDBInstancesOutput, error)
}

// New creates a new AWS adapter (not yet connected)
func New() *Adapter {
	return &Adapter{}
}

// NewWithClients creates an adapter with pre-built clients (for testing)
func NewWithClients(ec2Client *ec2.Client, eksClient *eks.Client, rdsClient *rds.Client) *Adapter {
	return &Adapter{
		ec2Client: ec2Client,
		eksClient: eksClient,
		rdsClient: rdsClient,
		connected: true,
	}
}

// Connect establishes a connection to AWS
func (a *Adapter) Connect(ctx context.Context, source store.Component) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Parse raw JSON config to map
	var configMap map[string]any
	if len(source.Config) > 0 {
		err := json.Unmarshal(source.Config, &configMap)
		if err != nil {
			return fmt.Errorf("parse source config JSON: %w", err)
		}
	} else {
		configMap = make(map[string]any)
	}

	cfg, err := ParseConfig(configMap)
	if err != nil {
		return fmt.Errorf("parse source config: %w", err)
	}
	a.config = cfg

	awsConfig, err := buildAWSConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("build AWS config: %w", err)
	}
	a.awsConfig = awsConfig

	// Create service clients
	a.ec2Client = ec2.NewFromConfig(awsConfig)
	a.eksClient = eks.NewFromConfig(awsConfig)
	a.rdsClient = rds.NewFromConfig(awsConfig)

	// Verify connectivity by calling STS GetCallerIdentity
	stsClient := sts.NewFromConfig(awsConfig)
	_, err = stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return fmt.Errorf("verify AWS connectivity: %w", err)
	}

	a.connected = true
	return nil
}

// Disconnect closes the AWS connection
func (a *Adapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.connected = false
	a.ec2Client = nil
	a.eksClient = nil
	a.rdsClient = nil

	return nil
}

// Status returns the current connection status
func (a *Adapter) Status() adapters.Status {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.connected {
		return adapters.Status{
			Connected: true,
			Message:   fmt.Sprintf(statusConnectedFmt, a.config.Region),
		}
	}

	return adapters.Status{
		Connected: false,
		Message:   statusNotConnected,
	}
}

// buildAWSConfig creates AWS SDK config from adapter config
func buildAWSConfig(ctx context.Context, cfg Config) (aws.Config, error) {
	// Load default config
	awsConfig, err := config.LoadDefaultConfig(ctx, config.WithRegion(cfg.Region))
	if err != nil {
		return aws.Config{}, fmt.Errorf("load default AWS config: %w", err)
	}

	// Override with profile if specified
	if cfg.Profile != "" {
		awsConfig, err = config.LoadDefaultConfig(ctx,
			config.WithRegion(cfg.Region),
			config.WithSharedConfigProfile(cfg.Profile),
		)
		if err != nil {
			return aws.Config{}, fmt.Errorf("load AWS config with profile %s: %w", cfg.Profile, err)
		}
	}

	// Override with static credentials if provided (not recommended for production)
	if cfg.AccessKey != "" && cfg.SecretKey != "" {
		awsConfig.Credentials = credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")
	}

	// Assume role if specified
	if cfg.RoleARN != "" {
		stsClient := sts.NewFromConfig(awsConfig)
		awsConfig.Credentials = stscreds.NewAssumeRoleProvider(stsClient, cfg.RoleARN)
	}

	return awsConfig, nil
}

// checkConnected validates that the adapter is connected and returns an error if not
func (a *Adapter) checkConnected() error {
	if !a.connected {
		return ErrNotConnected
	}
	return nil
}
