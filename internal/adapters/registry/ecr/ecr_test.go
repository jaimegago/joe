package ecr_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	awsecr "github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"

	"github.com/jaimegago/joe/internal/adapters/registry/ecr"
)

// mockECRClient implements the ecrClient interface for testing.
type mockECRClient struct {
	describeRegistryFn   func(ctx context.Context, params *awsecr.DescribeRegistryInput, optFns ...func(*awsecr.Options)) (*awsecr.DescribeRegistryOutput, error)
	describeRepositoryFn func(ctx context.Context, params *awsecr.DescribeRepositoriesInput, optFns ...func(*awsecr.Options)) (*awsecr.DescribeRepositoriesOutput, error)
	describeImagesFn     func(ctx context.Context, params *awsecr.DescribeImagesInput, optFns ...func(*awsecr.Options)) (*awsecr.DescribeImagesOutput, error)
}

func (m *mockECRClient) DescribeRegistry(ctx context.Context, params *awsecr.DescribeRegistryInput, optFns ...func(*awsecr.Options)) (*awsecr.DescribeRegistryOutput, error) {
	if m.describeRegistryFn != nil {
		return m.describeRegistryFn(ctx, params, optFns...)
	}
	return &awsecr.DescribeRegistryOutput{}, nil
}

func (m *mockECRClient) DescribeRepositories(ctx context.Context, params *awsecr.DescribeRepositoriesInput, optFns ...func(*awsecr.Options)) (*awsecr.DescribeRepositoriesOutput, error) {
	if m.describeRepositoryFn != nil {
		return m.describeRepositoryFn(ctx, params, optFns...)
	}
	return &awsecr.DescribeRepositoriesOutput{}, nil
}

func (m *mockECRClient) DescribeImages(ctx context.Context, params *awsecr.DescribeImagesInput, optFns ...func(*awsecr.Options)) (*awsecr.DescribeImagesOutput, error) {
	if m.describeImagesFn != nil {
		return m.describeImagesFn(ctx, params, optFns...)
	}
	return &awsecr.DescribeImagesOutput{}, nil
}

func TestListRepositories_Success(t *testing.T) {
	now := time.Now()
	mock := &mockECRClient{
		describeRepositoryFn: func(_ context.Context, _ *awsecr.DescribeRepositoriesInput, _ ...func(*awsecr.Options)) (*awsecr.DescribeRepositoriesOutput, error) {
			return &awsecr.DescribeRepositoriesOutput{
				Repositories: []ecrtypes.Repository{
					{
						RepositoryName: awssdk.String("my-app"),
						RepositoryUri:  awssdk.String("123456789.dkr.ecr.us-east-1.amazonaws.com/my-app"),
						CreatedAt:      &now,
					},
					{
						RepositoryName: awssdk.String("my-worker"),
						RepositoryUri:  awssdk.String("123456789.dkr.ecr.us-east-1.amazonaws.com/my-worker"),
						CreatedAt:      &now,
					},
				},
			}, nil
		},
	}

	a := ecr.NewWithClient(mock, ecr.Config{Region: "us-east-1"})
	repos, err := a.ListRepositories(context.Background())
	if err != nil {
		t.Fatalf("ListRepositories() error = %v", err)
	}
	if len(repos) != 2 {
		t.Errorf("got %d repos, want 2", len(repos))
	}
	if repos[0].Name != "my-app" {
		t.Errorf("repos[0].Name = %q, want my-app", repos[0].Name)
	}
}

func TestListRepositories_Pagination(t *testing.T) {
	calls := 0
	mock := &mockECRClient{
		describeRepositoryFn: func(_ context.Context, params *awsecr.DescribeRepositoriesInput, _ ...func(*awsecr.Options)) (*awsecr.DescribeRepositoriesOutput, error) {
			calls++
			if calls == 1 {
				return &awsecr.DescribeRepositoriesOutput{
					Repositories: []ecrtypes.Repository{{RepositoryName: awssdk.String("repo-a")}},
					NextToken:    awssdk.String("token-1"),
				}, nil
			}
			return &awsecr.DescribeRepositoriesOutput{
				Repositories: []ecrtypes.Repository{{RepositoryName: awssdk.String("repo-b")}},
			}, nil
		},
	}

	a := ecr.NewWithClient(mock, ecr.Config{Region: "us-east-1"})
	repos, err := a.ListRepositories(context.Background())
	if err != nil {
		t.Fatalf("ListRepositories() error = %v", err)
	}
	if len(repos) != 2 {
		t.Errorf("got %d repos, want 2 (pagination)", len(repos))
	}
	if calls != 2 {
		t.Errorf("expected 2 API calls for pagination, got %d", calls)
	}
}

func TestListRepositories_Error(t *testing.T) {
	mock := &mockECRClient{
		describeRepositoryFn: func(_ context.Context, _ *awsecr.DescribeRepositoriesInput, _ ...func(*awsecr.Options)) (*awsecr.DescribeRepositoriesOutput, error) {
			return nil, fmt.Errorf("access denied")
		},
	}

	a := ecr.NewWithClient(mock, ecr.Config{Region: "us-east-1"})
	_, err := a.ListRepositories(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestListImages_Success(t *testing.T) {
	now := time.Now()
	mock := &mockECRClient{
		describeImagesFn: func(_ context.Context, _ *awsecr.DescribeImagesInput, _ ...func(*awsecr.Options)) (*awsecr.DescribeImagesOutput, error) {
			return &awsecr.DescribeImagesOutput{
				ImageDetails: []ecrtypes.ImageDetail{
					{
						ImageDigest:      awssdk.String("sha256:abc123"),
						ImageTags:        []string{"latest", "v1.0"},
						ImagePushedAt:    &now,
						ImageSizeInBytes: awssdk.Int64(50_000_000),
					},
				},
			}, nil
		},
	}

	a := ecr.NewWithClient(mock, ecr.Config{Region: "us-east-1"})
	images, err := a.ListImages(context.Background(), "my-app")
	if err != nil {
		t.Fatalf("ListImages() error = %v", err)
	}
	if len(images) != 1 {
		t.Errorf("got %d images, want 1", len(images))
	}
	if images[0].Digest != "sha256:abc123" {
		t.Errorf("Digest = %q, want sha256:abc123", images[0].Digest)
	}
}

func TestListImages_WithScanFindings(t *testing.T) {
	now := time.Now()
	mock := &mockECRClient{
		describeImagesFn: func(_ context.Context, _ *awsecr.DescribeImagesInput, _ ...func(*awsecr.Options)) (*awsecr.DescribeImagesOutput, error) {
			return &awsecr.DescribeImagesOutput{
				ImageDetails: []ecrtypes.ImageDetail{
					{
						ImageDigest:   awssdk.String("sha256:def456"),
						ImagePushedAt: &now,
						ImageScanFindingsSummary: &ecrtypes.ImageScanFindingsSummary{
							FindingSeverityCounts: map[string]int32{
								"CRITICAL": 2,
								"HIGH":     5,
							},
						},
					},
				},
			}, nil
		},
	}

	a := ecr.NewWithClient(mock, ecr.Config{Region: "us-east-1"})
	images, err := a.ListImages(context.Background(), "my-app")
	if err != nil {
		t.Fatalf("ListImages() error = %v", err)
	}
	if images[0].ScanFindings != "CRITICAL:2,HIGH:5" {
		t.Errorf("ScanFindings = %q, want CRITICAL:2,HIGH:5", images[0].ScanFindings)
	}
}

func TestGetImageDetails_Success(t *testing.T) {
	now := time.Now()
	mock := &mockECRClient{
		describeImagesFn: func(_ context.Context, params *awsecr.DescribeImagesInput, _ ...func(*awsecr.Options)) (*awsecr.DescribeImagesOutput, error) {
			if len(params.ImageIds) == 0 || awssdk.ToString(params.ImageIds[0].ImageTag) != "v1.0" {
				return &awsecr.DescribeImagesOutput{}, nil
			}
			return &awsecr.DescribeImagesOutput{
				ImageDetails: []ecrtypes.ImageDetail{
					{
						ImageDigest:   awssdk.String("sha256:abc123"),
						ImageTags:     []string{"v1.0"},
						ImagePushedAt: &now,
					},
				},
			}, nil
		},
	}

	a := ecr.NewWithClient(mock, ecr.Config{Region: "us-east-1"})
	img, err := a.GetImageDetails(context.Background(), "my-app", "v1.0")
	if err != nil {
		t.Fatalf("GetImageDetails() error = %v", err)
	}
	if img.Digest != "sha256:abc123" {
		t.Errorf("Digest = %q, want sha256:abc123", img.Digest)
	}
}

func TestGetImageDetails_NotFound(t *testing.T) {
	mock := &mockECRClient{
		describeImagesFn: func(_ context.Context, _ *awsecr.DescribeImagesInput, _ ...func(*awsecr.Options)) (*awsecr.DescribeImagesOutput, error) {
			return &awsecr.DescribeImagesOutput{ImageDetails: nil}, nil
		},
	}

	a := ecr.NewWithClient(mock, ecr.Config{Region: "us-east-1"})
	_, err := a.GetImageDetails(context.Background(), "my-app", "missing")
	if err == nil {
		t.Fatal("expected error for missing image, got nil")
	}
}

func TestDisconnect(t *testing.T) {
	a := ecr.NewWithClient(&mockECRClient{}, ecr.Config{Region: "us-east-1"})
	if err := a.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if a.Status().Connected {
		t.Error("expected disconnected after Disconnect()")
	}
}

func TestStatus_Connected(t *testing.T) {
	a := ecr.NewWithClient(&mockECRClient{}, ecr.Config{Region: "eu-west-1"})
	s := a.Status()
	if !s.Connected {
		t.Error("expected connected")
	}
	if s.Message == "" {
		t.Error("expected non-empty status message")
	}
}
