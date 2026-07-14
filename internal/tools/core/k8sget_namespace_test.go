package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// notFoundErr builds a real apimachinery not-found and buries it under a
// fmt.Errorf %w layer, mirroring the adapter's own wrap. The tool must still
// recognize it, which is what proves the unwrap works end to end.
func notFoundErr(resource, name string) error {
	gr := schema.GroupResource{Resource: resource}
	return fmt.Errorf("get %s/%s: %w", resource, name, apierrors.NewNotFound(gr, name))
}

// A by-name get with an empty namespace can never succeed against the API, so
// the tool rejects it before spending a round trip.
func TestK8sGetTool_ByName_EmptyNamespace_PreFlightRejects(t *testing.T) {
	client := &mockK8sGetClient{}
	tool := NewK8sGetTool(client)

	_, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "cluster-a",
		"resource":     "pods",
		"name":         "api-7d9f8b6c4d-x2klm",
	})
	if err == nil {
		t.Fatal("expected an error for a by-name get with an empty namespace, got nil")
	}
	if !strings.Contains(err.Error(), "omit name to list") {
		t.Errorf("error must point at the list recovery path, got: %v", err)
	}
	if client.getCalls != 0 || client.listCalls != 0 {
		t.Errorf("client must not be called: getCalls=%d listCalls=%d", client.getCalls, client.listCalls)
	}
}

func TestK8sGetTool_ByName_NotFound_Enriched(t *testing.T) {
	client := &mockK8sGetClient{getErr: notFoundErr("pods", "api-7d9f8b6c4d-x2klm")}
	tool := NewK8sGetTool(client)

	_, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "cluster-a",
		"resource":     "pods",
		"namespace":    "payments",
		"name":         "api-7d9f8b6c4d-x2klm",
	})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	for _, want := range []string{"payments", "omit name to list", "generated"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must contain %q, got: %v", want, err)
		}
	}
	if client.getCalls != 1 {
		t.Errorf("expected exactly one get call, got %d", client.getCalls)
	}
}

// Anything that is not a not-found keeps the plain wrap: the enrichment is
// advice about a wrong name or namespace, and would be misleading otherwise.
func TestK8sGetTool_ByName_NonNotFoundError_NotEnriched(t *testing.T) {
	client := &mockK8sGetClient{getErr: errors.New("connection refused")}
	tool := NewK8sGetTool(client)

	_, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "cluster-a",
		"resource":     "pods",
		"namespace":    "payments",
		"name":         "api-7d9f8b6c4d-x2klm",
	})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("underlying error must survive, got: %v", err)
	}
	if strings.Contains(err.Error(), "omit name to list") {
		t.Errorf("non-not-found error must not carry the not-found recovery advice, got: %v", err)
	}
}

// The list branch is what the empty namespace was always for; it still means
// all namespaces there.
func TestK8sGetTool_List_EmptyNamespace_StillLists(t *testing.T) {
	client := &mockK8sGetClient{
		listResult: []map[string]any{
			{"kind": "Pod", "metadata": map[string]any{"name": "api-1", "namespace": "payments"}},
			{"kind": "Pod", "metadata": map[string]any{"name": "api-2", "namespace": "billing"}},
		},
	}
	tool := NewK8sGetTool(client)

	result, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "cluster-a",
		"resource":     "pods",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.listCalls != 1 || client.getCalls != 0 {
		t.Fatalf("expected the list branch: getCalls=%d listCalls=%d", client.getCalls, client.listCalls)
	}

	res, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type %T", result)
	}
	if res["count"] != 2 {
		t.Errorf("expected count 2, got %v", res["count"])
	}
}
