package aws

import (
	"context"
	"strings"
	"testing"
)

func TestBuildAWSConfig_StaticCredentials(t *testing.T) {
	cfg, err := buildAWSConfig(context.Background(), Config{
		Region:    "us-west-2",
		AccessKey: "AKIA_TEST",
		SecretKey: "SECRET_TEST",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	creds, err := cfg.Credentials.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("retrieve creds: %v", err)
	}

	if creds.AccessKeyID != "AKIA_TEST" {
		t.Fatalf("unexpected access key: %q", creds.AccessKeyID)
	}
}

func TestBuildAWSConfig_InvalidProfile(t *testing.T) {
	_, err := buildAWSConfig(context.Background(), Config{
		Region:  "us-west-2",
		Profile: "profile-does-not-exist-joe-tests",
	})
	if err == nil {
		t.Fatal("expected error for invalid profile")
	}
	if !strings.Contains(err.Error(), "load AWS config with profile") {
		t.Fatalf("expected wrapped profile error, got %v", err)
	}
}
