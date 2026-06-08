package core

import (
	"context"
	"testing"
)

// mockK8sGetClient implements K8sGetClient for testing.
type mockK8sGetClient struct {
	getResult  map[string]any
	listResult []map[string]any
	getErr     error
	listErr    error
}

func (m *mockK8sGetClient) K8sGetResource(_ context.Context, _, _, _, _ string) (map[string]any, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.getResult, nil
}

func (m *mockK8sGetClient) K8sListResources(_ context.Context, _, _, _ string) ([]map[string]any, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.listResult, nil
}

func TestK8sGetTool_SecretRedaction_SingleGet(t *testing.T) {
	client := &mockK8sGetClient{
		getResult: map[string]any{
			"kind": "Secret",
			"metadata": map[string]any{
				"name":      "db-credentials",
				"namespace": "payments",
			},
			"type": "Opaque",
			"data": map[string]any{
				"DB_PASSWORD": "c3VwZXJzZWNyZXQ=",
				"DB_USER":     "YWRtaW4=",
			},
		},
	}

	tool := NewK8sGetTool(client)
	result, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "cluster-a",
		"resource":     "secrets",
		"namespace":    "payments",
		"name":         "db-credentials",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	res := result.(map[string]any)
	resource := res["resource"].(map[string]any)
	data := resource["data"].(map[string]any)

	for key, val := range data {
		if val != "[REDACTED]" {
			t.Errorf("secret data key %q should be redacted, got %q", key, val)
		}
	}

	// Metadata should be preserved
	metadata := resource["metadata"].(map[string]any)
	if metadata["name"] != "db-credentials" {
		t.Errorf("metadata.name should be preserved, got %v", metadata["name"])
	}
}

func TestK8sGetTool_SecretRedaction_StringData(t *testing.T) {
	client := &mockK8sGetClient{
		getResult: map[string]any{
			"kind": "Secret",
			"metadata": map[string]any{
				"name":      "api-key",
				"namespace": "default",
			},
			"stringData": map[string]any{
				"API_KEY": "sk-live-abc123",
			},
		},
	}

	tool := NewK8sGetTool(client)
	result, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "cluster-a",
		"resource":     "secrets",
		"namespace":    "default",
		"name":         "api-key",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	res := result.(map[string]any)
	resource := res["resource"].(map[string]any)
	stringData := resource["stringData"].(map[string]any)

	if stringData["API_KEY"] != "[REDACTED]" {
		t.Errorf("stringData.API_KEY should be redacted, got %q", stringData["API_KEY"])
	}
}

func TestK8sGetTool_NonSecret_NotRedacted(t *testing.T) {
	client := &mockK8sGetClient{
		getResult: map[string]any{
			"kind": "ConfigMap",
			"metadata": map[string]any{
				"name":      "app-config",
				"namespace": "default",
			},
			"data": map[string]any{
				"LOG_LEVEL": "debug",
				"PORT":      "8080",
			},
		},
	}

	tool := NewK8sGetTool(client)
	result, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "cluster-a",
		"resource":     "configmaps",
		"namespace":    "default",
		"name":         "app-config",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	res := result.(map[string]any)
	resource := res["resource"].(map[string]any)
	data := resource["data"].(map[string]any)

	if data["LOG_LEVEL"] != "debug" {
		t.Errorf("ConfigMap data should NOT be redacted, got %q", data["LOG_LEVEL"])
	}
	if data["PORT"] != "8080" {
		t.Errorf("ConfigMap data should NOT be redacted, got %q", data["PORT"])
	}
}

func TestK8sGetTool_SecretRedaction_List(t *testing.T) {
	client := &mockK8sGetClient{
		listResult: []map[string]any{
			{
				"kind": "Secret",
				"metadata": map[string]any{
					"name": "secret-1",
				},
				"data": map[string]any{
					"password": "aHVudGVyMg==",
				},
			},
			{
				"kind": "ConfigMap",
				"metadata": map[string]any{
					"name": "config-1",
				},
				"data": map[string]any{
					"key": "value",
				},
			},
		},
	}

	tool := NewK8sGetTool(client)
	result, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "cluster-a",
		"resource":     "secrets",
		"namespace":    "default",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	res := result.(map[string]any)
	items := res["resources"].([]map[string]any)

	// Secret should be redacted
	secretData := items[0]["data"].(map[string]any)
	if secretData["password"] != "[REDACTED]" {
		t.Errorf("secret in list should be redacted, got %q", secretData["password"])
	}

	// ConfigMap should NOT be redacted
	configData := items[1]["data"].(map[string]any)
	if configData["key"] != "value" {
		t.Errorf("configmap in list should NOT be redacted, got %q", configData["key"])
	}
}

func TestRedactSecretData_NoKind(t *testing.T) {
	// Object without "kind" field should not be modified
	obj := map[string]any{
		"data": map[string]any{
			"key": "value",
		},
	}
	redactSecretData(obj)

	data := obj["data"].(map[string]any)
	if data["key"] != "value" {
		t.Errorf("object without kind should not be redacted, got %q", data["key"])
	}
}
