package ecr_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	awsecr "github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"

	"github.com/jaimegago/joe/internal/adapters/registry/ecr"
	"github.com/jaimegago/joe/internal/store"
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

func TestStatus_NotConnected(t *testing.T) {
	a := ecr.New()
	s := a.Status()
	if s.Connected {
		t.Error("expected not connected for New() adapter")
	}
}

func TestListRepositories_Empty(t *testing.T) {
	mock := &mockECRClient{
		describeRepositoryFn: func(_ context.Context, _ *awsecr.DescribeRepositoriesInput, _ ...func(*awsecr.Options)) (*awsecr.DescribeRepositoriesOutput, error) {
			return &awsecr.DescribeRepositoriesOutput{}, nil
		},
	}
	a := ecr.NewWithClient(mock, ecr.Config{Region: "us-east-1"})
	repos, err := a.ListRepositories(context.Background())
	if err != nil {
		t.Fatalf("ListRepositories() error = %v", err)
	}
	if len(repos) != 0 {
		t.Errorf("expected 0 repos, got %d", len(repos))
	}
}

func TestListImages_Error(t *testing.T) {
	mock := &mockECRClient{
		describeImagesFn: func(_ context.Context, _ *awsecr.DescribeImagesInput, _ ...func(*awsecr.Options)) (*awsecr.DescribeImagesOutput, error) {
			return nil, fmt.Errorf("access denied")
		},
	}
	a := ecr.NewWithClient(mock, ecr.Config{Region: "us-east-1"})
	_, err := a.ListImages(context.Background(), "my-app")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetImageDetails_Error(t *testing.T) {
	mock := &mockECRClient{
		describeImagesFn: func(_ context.Context, _ *awsecr.DescribeImagesInput, _ ...func(*awsecr.Options)) (*awsecr.DescribeImagesOutput, error) {
			return nil, fmt.Errorf("throttled")
		},
	}
	a := ecr.NewWithClient(mock, ecr.Config{Region: "us-east-1"})
	_, err := a.GetImageDetails(context.Background(), "my-app", "v1.0")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name    string
		input   map[string]any
		wantErr bool
	}{
		{
			name:  "valid with region",
			input: map[string]any{"region": "us-east-1"},
		},
		{
			name:  "valid with all fields",
			input: map[string]any{"region": "eu-west-1", "profile": "prod", "registry_id": "123456789"},
		},
		{
			name:    "missing region",
			input:   map[string]any{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ecr.ParseConfig(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && cfg.Region == "" {
				t.Error("Region should not be empty on valid config")
			}
		})
	}
}

func TestListRepositories_NotConnected(t *testing.T) {
	a := ecr.New()
	_, err := a.ListRepositories(context.Background())
	if err == nil {
		t.Error("expected error for not connected adapter")
	}
}

func TestListImages_NotConnected(t *testing.T) {
	a := ecr.New()
	_, err := a.ListImages(context.Background(), "my-app")
	if err == nil {
		t.Error("expected error for not connected adapter")
	}
}

func TestGetImageDetails_NotConnected(t *testing.T) {
	a := ecr.New()
	_, err := a.GetImageDetails(context.Background(), "my-app", "v1.0")
	if err == nil {
		t.Error("expected error for not connected adapter")
	}
}

func TestListImages_WithRegistryID(t *testing.T) {
	mock := &mockECRClient{
		describeImagesFn: func(_ context.Context, params *awsecr.DescribeImagesInput, _ ...func(*awsecr.Options)) (*awsecr.DescribeImagesOutput, error) {
			return &awsecr.DescribeImagesOutput{
				ImageDetails: []ecrtypes.ImageDetail{
					{
						ImageDigest: awssdk.String("sha256:xyz"),
						// No scan findings - tests zero-count path.
						ImageScanFindingsSummary: &ecrtypes.ImageScanFindingsSummary{
							FindingSeverityCounts: map[string]int32{"CRITICAL": 0},
						},
					},
				},
			}, nil
		},
	}
	a := ecr.NewWithClient(mock, ecr.Config{Region: "us-east-1", RegistryID: "123456"})
	images, err := a.ListImages(context.Background(), "my-app")
	if err != nil {
		t.Fatalf("ListImages() error = %v", err)
	}
	if len(images) != 1 {
		t.Errorf("expected 1 image, got %d", len(images))
	}
}

func TestListRepositories_WithRegistryID(t *testing.T) {
	// Exercises the RegistryID branch in ListRepositories.
	mock := &mockECRClient{
		describeRepositoryFn: func(_ context.Context, params *awsecr.DescribeRepositoriesInput, _ ...func(*awsecr.Options)) (*awsecr.DescribeRepositoriesOutput, error) {
			if params.RegistryId == nil || *params.RegistryId != "111122223333" {
				return nil, fmt.Errorf("expected registry id 111122223333")
			}
			return &awsecr.DescribeRepositoriesOutput{
				Repositories: []ecrtypes.Repository{
					{RepositoryName: awssdk.String("svc")},
				},
			}, nil
		},
	}
	a := ecr.NewWithClient(mock, ecr.Config{Region: "us-east-1", RegistryID: "111122223333"})
	repos, err := a.ListRepositories(context.Background())
	if err != nil {
		t.Fatalf("ListRepositories() error = %v", err)
	}
	if len(repos) != 1 {
		t.Errorf("expected 1 repo, got %d", len(repos))
	}
}

func TestGetImageDetails_WithRegistryID(t *testing.T) {
	now := time.Now()
	mock := &mockECRClient{
		describeImagesFn: func(_ context.Context, params *awsecr.DescribeImagesInput, _ ...func(*awsecr.Options)) (*awsecr.DescribeImagesOutput, error) {
			return &awsecr.DescribeImagesOutput{
				ImageDetails: []ecrtypes.ImageDetail{
					{
						ImageDigest:   awssdk.String("sha256:regid"),
						ImagePushedAt: &now,
					},
				},
			}, nil
		},
	}
	a := ecr.NewWithClient(mock, ecr.Config{Region: "us-east-1", RegistryID: "123"})
	img, err := a.GetImageDetails(context.Background(), "svc", "latest")
	if err != nil {
		t.Fatalf("GetImageDetails() error = %v", err)
	}
	if img.Digest != "sha256:regid" {
		t.Errorf("Digest = %q, want sha256:regid", img.Digest)
	}
}

func TestListImages_ImageWithoutPushedAt(t *testing.T) {
	// ImagePushedAt nil — PushedAt should be empty string.
	mock := &mockECRClient{
		describeImagesFn: func(_ context.Context, _ *awsecr.DescribeImagesInput, _ ...func(*awsecr.Options)) (*awsecr.DescribeImagesOutput, error) {
			return &awsecr.DescribeImagesOutput{
				ImageDetails: []ecrtypes.ImageDetail{
					{
						ImageDigest: awssdk.String("sha256:nopushedat"),
						// ImagePushedAt intentionally nil
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
		t.Fatalf("expected 1 image")
	}
	if images[0].PushedAt != "" {
		t.Errorf("PushedAt = %q, want empty", images[0].PushedAt)
	}
}

func TestFormatScanFindings_AllSeverities(t *testing.T) {
	// Exercise formatScanFindings with all severity levels.
	mock := &mockECRClient{
		describeImagesFn: func(_ context.Context, _ *awsecr.DescribeImagesInput, _ ...func(*awsecr.Options)) (*awsecr.DescribeImagesOutput, error) {
			return &awsecr.DescribeImagesOutput{
				ImageDetails: []ecrtypes.ImageDetail{
					{
						ImageDigest: awssdk.String("sha256:allsev"),
						ImageScanFindingsSummary: &ecrtypes.ImageScanFindingsSummary{
							FindingSeverityCounts: map[string]int32{
								"CRITICAL":      1,
								"HIGH":          2,
								"MEDIUM":        3,
								"LOW":           4,
								"INFORMATIONAL": 5,
								"UNDEFINED":     6,
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
	want := "CRITICAL:1,HIGH:2,MEDIUM:3,LOW:4,INFORMATIONAL:5,UNDEFINED:6"
	if images[0].ScanFindings != want {
		t.Errorf("ScanFindings = %q, want %q", images[0].ScanFindings, want)
	}
}

func TestParseConfig_WithAccessKeyAndRoleARN(t *testing.T) {
	cfg, err := ecr.ParseConfig(map[string]any{
		"region":     "us-west-2",
		"access_key": "AKIAEXAMPLE",
		"secret_key": "secretexample",
		"role_arn":   "arn:aws:iam::123456789012:role/MyRole",
	})
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	if cfg.AccessKey != "AKIAEXAMPLE" {
		t.Errorf("AccessKey = %q, want AKIAEXAMPLE", cfg.AccessKey)
	}
	if cfg.RoleARN == "" {
		t.Error("RoleARN should not be empty")
	}
}

func TestConnect_BadJSON(t *testing.T) {
	a := ecr.New()
	src := store.Component{Config: []byte(`{bad json`)}
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("Connect() expected error for bad JSON")
	}
}

func TestConnect_MissingRegion(t *testing.T) {
	a := ecr.New()
	data, _ := json.Marshal(map[string]any{}) // no region
	src := store.Component{Config: data}
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("Connect() expected error for missing region")
	}
}

func TestConnect_DescribeRegistryError(t *testing.T) {
	// buildAWSConfig succeeds (static credentials), but DescribeRegistry fails.
	// We can't easily inject a fake ECR into Connect's real path, so we test
	// that Connect with static creds against a non-existent endpoint errors.
	a := ecr.New()
	data, _ := json.Marshal(map[string]any{
		"region":     "us-east-1",
		"access_key": "AKIAFAKE",
		"secret_key": "fakesecret",
	})
	src := store.Component{Config: data}
	// This will fail because there's no real AWS endpoint — that's the point.
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("Connect() expected error when DescribeRegistry fails")
	}
}

func TestConnect_WithRoleARN(t *testing.T) {
	// buildAWSConfig with RoleARN — STS assume-role provider is wired.
	// The actual DescribeRegistry call will fail (no real AWS), which is fine.
	a := ecr.New()
	data, _ := json.Marshal(map[string]any{
		"region":     "us-east-1",
		"access_key": "AKIAFAKE",
		"secret_key": "fakesecret",
		"role_arn":   "arn:aws:iam::123456789012:role/FakeRole",
	})
	src := store.Component{Config: data}
	// Expected to fail at DescribeRegistry; we just need buildAWSConfig to run.
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("Connect() expected error for fake AWS creds")
	}
}

func TestConnect_WithProfile(t *testing.T) {
	// Profile branch in buildAWSConfig: uses a named profile that doesn't exist.
	// LoadDefaultConfig with a non-existent profile may succeed (just no creds),
	// but DescribeRegistry will fail.
	a := ecr.New()
	data, _ := json.Marshal(map[string]any{
		"region":  "us-east-1",
		"profile": "nonexistent-profile-for-testing",
	})
	src := store.Component{Config: data}
	// Expected to fail — either at profile load or DescribeRegistry.
	err := a.Connect(context.Background(), src)
	// We just verify it doesn't panic; failure is expected.
	_ = err
}

func TestFormatScanFindings_EmptyMap(t *testing.T) {
	// formatScanFindings with an empty severity counts map returns "".
	mock := &mockECRClient{
		describeImagesFn: func(_ context.Context, _ *awsecr.DescribeImagesInput, _ ...func(*awsecr.Options)) (*awsecr.DescribeImagesOutput, error) {
			return &awsecr.DescribeImagesOutput{
				ImageDetails: []ecrtypes.ImageDetail{
					{
						ImageDigest: awssdk.String("sha256:empty"),
						ImageScanFindingsSummary: &ecrtypes.ImageScanFindingsSummary{
							FindingSeverityCounts: map[string]int32{},
						},
					},
				},
			}, nil
		},
	}
	a := ecr.NewWithClient(mock, ecr.Config{Region: "us-east-1"})
	images, err := a.ListImages(context.Background(), "repo")
	if err != nil {
		t.Fatalf("ListImages() error = %v", err)
	}
	if images[0].ScanFindings != "" {
		t.Errorf("ScanFindings = %q, want empty", images[0].ScanFindings)
	}
}
