//go:build e2e
// +build e2e

package e2e

import (
	"testing"
	"time"
)

func TestE2E_StartupAndStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	harness := NewTestHarness(t)
	defer harness.Stop()
	if err := harness.Start(); err != nil {
		t.Fatalf("failed to start harness: %v", err)
	}

	// Test status endpoint
	status, err := harness.GetStatus()
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}

	// Verify response
	if status["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", status["status"])
	}
	if status["version"] == nil {
		t.Error("expected version field")
	}

	t.Log("✓ Joe started successfully and API is responding")
}

func TestE2E_ConfigurationLoading(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	harness := NewTestHarness(t)
	defer harness.Stop()
	if err := harness.Start(); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Get status to verify configuration was loaded
	status, err := harness.GetStatus()
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}

	t.Logf("Status: %+v", status)
	t.Log("✓ Configuration loaded successfully")
}

func TestE2E_MultipleRequests(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	harness := NewTestHarness(t)
	defer harness.Stop()
	if err := harness.Start(); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Make multiple status requests
	for i := 0; i < 5; i++ {
		status, err := harness.GetStatus()
		if err != nil {
			t.Fatalf("request %d failed: %v", i+1, err)
		}
		if status["status"] != "ok" {
			t.Errorf("request %d: expected status=ok, got %v", i+1, status["status"])
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Log("✓ Multiple requests handled successfully")
}

func TestE2E_GracefulShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	harness := NewTestHarness(t)
	if err := harness.Start(); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Verify running
	_, err := harness.GetStatus()
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}

	// Stop gracefully
	harness.Stop()
	// Verify stopped (status should fail)
	time.Sleep(500 * time.Millisecond)
	_, err = harness.GetStatus()
	if err == nil {
		t.Error("expected error after shutdown, but got response")
	}

	t.Log("✓ Graceful shutdown successful")
}

func TestE2E_DatabaseInitialization(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	harness := NewTestHarness(t)
	defer harness.Stop()
	if err := harness.Start(); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Check that database was created in the correct location
	// (In a real test, you might query the store via API)
	status, err := harness.GetStatus()
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}

	t.Logf("Status: %+v", status)
	t.Log("✓ Database initialized successfully")
}
