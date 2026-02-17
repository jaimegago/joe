package aws

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
)

func TestConvertEC2Instance_Full(t *testing.T) {
	launchTime := time.Date(2026, 2, 17, 10, 30, 0, 0, time.UTC)
	instance := ec2types.Instance{
		InstanceId:       strPtr("i-123"),
		InstanceType:     ec2types.InstanceTypeT3Micro,
		State:            &ec2types.InstanceState{Name: ec2types.InstanceStateNameRunning},
		PublicIpAddress:  strPtr("1.2.3.4"),
		PrivateIpAddress: strPtr("10.0.0.10"),
		VpcId:            strPtr("vpc-1"),
		SubnetId:         strPtr("subnet-1"),
		LaunchTime:       &launchTime,
		Placement:        &ec2types.Placement{AvailabilityZone: strPtr("eu-west-1a")},
		SecurityGroups: []ec2types.GroupIdentifier{{
			GroupId:   strPtr("sg-1"),
			GroupName: strPtr("web"),
		}},
		Tags: []ec2types.Tag{{Key: strPtr("Name"), Value: strPtr("app")}},
	}

	got := convertEC2Instance(instance)

	if got.InstanceID != "i-123" || got.InstanceType != string(ec2types.InstanceTypeT3Micro) || got.State != string(ec2types.InstanceStateNameRunning) {
		t.Fatalf("unexpected required fields: %+v", got)
	}
	if got.PublicIP != "1.2.3.4" || got.PrivateIP != "10.0.0.10" || got.VpcID != "vpc-1" || got.SubnetID != "subnet-1" {
		t.Fatalf("unexpected optional fields: %+v", got)
	}
	if got.AvailabilityZone != "eu-west-1a" || got.LaunchTime != launchTime.Format(timeFormatRFC3339) {
		t.Fatalf("unexpected placement/launch fields: %+v", got)
	}
	if len(got.SecurityGroups) != 1 || got.SecurityGroups[0].GroupID != "sg-1" || got.SecurityGroups[0].GroupName != "web" {
		t.Fatalf("unexpected security groups: %+v", got.SecurityGroups)
	}
	if got.Tags["Name"] != "app" {
		t.Fatalf("unexpected tags: %+v", got.Tags)
	}
}

func TestConvertEC2Instance_Partial(t *testing.T) {
	got := convertEC2Instance(ec2types.Instance{})

	if got.InstanceID != "" || got.InstanceType != "" || got.State != "" {
		t.Fatalf("expected zero values for partial instance, got %+v", got)
	}
	if got.Tags == nil {
		t.Fatal("expected tags map to be initialized")
	}
}

func TestConvertEKSCluster_Full(t *testing.T) {
	created := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	cluster := ekstypes.Cluster{
		Name:            strPtr("cluster-a"),
		Arn:             strPtr("arn:aws:eks:region:acct:cluster/cluster-a"),
		Version:         strPtr("1.30"),
		Status:          ekstypes.ClusterStatusActive,
		Endpoint:        strPtr("https://eks.local"),
		RoleArn:         strPtr("arn:aws:iam::acct:role/eks"),
		CreatedAt:       &created,
		PlatformVersion: strPtr("eks.5"),
		ResourcesVpcConfig: &ekstypes.VpcConfigResponse{
			VpcId:            strPtr("vpc-1"),
			SubnetIds:        []string{"subnet-1", "subnet-2"},
			SecurityGroupIds: []string{"sg-1"},
		},
		Tags: map[string]string{"env": "prod"},
	}

	got := convertEKSCluster(cluster)

	if got.Name != "cluster-a" || got.ARN == "" || got.Status != string(ekstypes.ClusterStatusActive) {
		t.Fatalf("unexpected cluster mapping: %+v", got)
	}
	if got.VpcID != "vpc-1" || len(got.VpcConfig.SubnetIDs) != 2 || got.VpcConfig.EndpointConfig == "" {
		t.Fatalf("unexpected vpc config mapping: %+v", got.VpcConfig)
	}
	if got.Tags["env"] != "prod" || got.CreatedAt != created.Format(timeFormatRFC3339) {
		t.Fatalf("unexpected tag/time mapping: %+v", got)
	}
}

func TestConvertRDSInstance_Full(t *testing.T) {
	created := time.Date(2026, 1, 15, 14, 0, 0, 0, time.UTC)
	rdsInstance := rdstypes.DBInstance{
		DBInstanceIdentifier: strPtr("db-1"),
		DBName:               strPtr("appdb"),
		Engine:               strPtr("postgres"),
		EngineVersion:        strPtr("16"),
		DBInstanceClass:      strPtr("db.t4g.medium"),
		DBInstanceStatus:     strPtr("available"),
		Endpoint:             &rdstypes.Endpoint{Address: strPtr("db.local"), Port: int32Ptr(5432)},
		DbInstancePort:       int32Ptr(6432),
		DBSubnetGroup: &rdstypes.DBSubnetGroup{
			VpcId:             strPtr("vpc-1"),
			DBSubnetGroupName: strPtr("subnet-group-1"),
		},
		InstanceCreateTime: &created,
		VpcSecurityGroups: []rdstypes.VpcSecurityGroupMembership{{
			VpcSecurityGroupId: strPtr("sg-1"),
			Status:             strPtr("active"),
		}},
	}

	got := convertRDSInstance(rdsInstance)

	if got.DBInstanceID != "db-1" || got.Engine != "postgres" || got.Status != "available" {
		t.Fatalf("unexpected required mapping: %+v", got)
	}
	if got.Port != 6432 {
		t.Fatalf("expected DbInstancePort to override endpoint port, got %d", got.Port)
	}
	if got.VpcID != "vpc-1" || got.SubnetGroup != "subnet-group-1" || got.CreatedTime != created.Format(timeFormatRFC3339) {
		t.Fatalf("unexpected networking/time mapping: %+v", got)
	}
	if len(got.SecurityGroups) != 1 || got.SecurityGroups[0].GroupName != "active" {
		t.Fatalf("unexpected security group mapping: %+v", got.SecurityGroups)
	}
}

func TestConvertVPCAndSubnet_Full(t *testing.T) {
	vpc := convertVPC(ec2types.Vpc{
		VpcId:     strPtr("vpc-1"),
		CidrBlock: strPtr("10.0.0.0/16"),
		State:     ec2types.VpcStateAvailable,
		IsDefault: boolPtr(true),
		Tags:      []ec2types.Tag{{Key: strPtr("Name"), Value: strPtr("main")}},
	})

	subnet := convertSubnet(ec2types.Subnet{
		SubnetId:         strPtr("subnet-1"),
		VpcId:            strPtr("vpc-1"),
		CidrBlock:        strPtr("10.0.1.0/24"),
		AvailabilityZone: strPtr("eu-west-1a"),
		State:            ec2types.SubnetStateAvailable,
		Tags:             []ec2types.Tag{{Key: strPtr("tier"), Value: strPtr("private")}},
	})

	if vpc.VpcID != "vpc-1" || vpc.Tags["Name"] != "main" || !vpc.IsDefault {
		t.Fatalf("unexpected vpc mapping: %+v", vpc)
	}
	if subnet.SubnetID != "subnet-1" || subnet.Tags["tier"] != "private" || subnet.State != string(ec2types.SubnetStateAvailable) {
		t.Fatalf("unexpected subnet mapping: %+v", subnet)
	}
}

func TestAdapter_ConnectedButMissingClients(t *testing.T) {
	adapter := &Adapter{connected: true}
	ctx := context.Background()

	_, err := adapter.ListEC2Instances(ctx)
	if err == nil || !strings.Contains(err.Error(), "EC2 client not initialized") {
		t.Fatalf("expected EC2 client initialization error, got %v", err)
	}

	_, err = adapter.ListEKSClusters(ctx)
	if err == nil || !strings.Contains(err.Error(), "EKS client not initialized") {
		t.Fatalf("expected EKS client initialization error, got %v", err)
	}

	_, err = adapter.ListRDSInstances(ctx)
	if err == nil || !strings.Contains(err.Error(), "RDS client not initialized") {
		t.Fatalf("expected RDS client initialization error, got %v", err)
	}

	_, err = adapter.ListVPCs(ctx)
	if err == nil || !strings.Contains(err.Error(), "EC2 client not initialized") {
		t.Fatalf("expected EC2 client initialization error for VPC, got %v", err)
	}
}

func TestAdapter_DisconnectedReturnsErrNotConnected(t *testing.T) {
	adapter := New()
	ctx := context.Background()

	_, err := adapter.ListEC2Instances(ctx)
	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("expected ErrNotConnected, got %v", err)
	}
}

func strPtr(v string) *string { return &v }
func int32Ptr(v int32) *int32 { return &v }
func boolPtr(v bool) *bool    { return &v }
