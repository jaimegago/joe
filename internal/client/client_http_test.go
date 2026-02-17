package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/store"
)

func TestAPIError_Error(t *testing.T) {
	t.Run("with code", func(t *testing.T) {
		err := (&APIError{Status: 404, Code: "not_found", Message: "missing"}).Error()
		if err != "api error (404 not_found): missing" {
			t.Fatalf("unexpected message: %q", err)
		}
	})

	t.Run("without code", func(t *testing.T) {
		err := (&APIError{Status: 500, RawBody: "oops"}).Error()
		if err != "api error (500): oops" {
			t.Fatalf("unexpected message: %q", err)
		}
	})
}

func TestParseAPIError(t *testing.T) {
	tests := []struct {
		name string
		body string
		ok   bool
	}{
		{name: "empty", body: "", ok: false},
		{name: "invalid json", body: "{", ok: false},
		{name: "missing fields", body: `{"foo":"bar"}`, ok: false},
		{name: "valid", body: `{"error":"bad_request","message":"invalid"}`, ok: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err, ok := parseAPIError([]byte(tt.body), 400)
			if ok != tt.ok {
				t.Fatalf("ok=%v want %v err=%v", ok, tt.ok, err)
			}
		})
	}
}

func TestGetStatus_DecodeError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-json"))
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.GetStatus(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decode status response") {
		t.Fatalf("expected decode status error, got %v", err)
	}
}

func TestGraphQuery_Non200StructuredError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_query", "message": "bad q"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.GraphQuery(context.Background(), "bad")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T (%v)", err, err)
	}
	if apiErr.Code != "invalid_query" || apiErr.Status != http.StatusBadRequest {
		t.Fatalf("unexpected APIError: %+v", apiErr)
	}
}

func TestGraphQuery_Non200RawBodyError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("plain error"))
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.GraphQuery(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "graph query failed (status 500)") {
		t.Fatalf("expected wrapped non-200 error, got %v", err)
	}
}

func TestGraphRelated_NotFoundFallbackAndDecode(t *testing.T) {
	t.Run("404 fallback when response is non-json", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("missing"))
		}))
		defer ts.Close()

		c := New(ts.URL)
		_, err := c.GraphRelated(context.Background(), "node-x", 1)
		if err == nil || !strings.Contains(err.Error(), `node "node-x" not found`) {
			t.Fatalf("expected node not found fallback, got %v", err)
		}
	})

	t.Run("decode error on success status", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{"))
		}))
		defer ts.Close()

		c := New(ts.URL)
		_, err := c.GraphRelated(context.Background(), "n", 1)
		if err == nil || !strings.Contains(err.Error(), "decode graph related response") {
			t.Fatalf("expected decode error, got %v", err)
		}
	})
}

func TestCreateDeleteSource_RequestShapeAndHeaders(t *testing.T) {
	var methods []string
	var contentTypes []string
	var authHeaders []string
	var capturedRequestURI string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		contentTypes = append(contentTypes, r.Header.Get("Content-Type"))
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		capturedRequestURI = r.RequestURI

		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(store.Source{ID: "s1", Name: "src"})
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer ts.Close()

	c := New(ts.URL, WithAPIKey("token-1"))
	_, err := c.CreateSource(context.Background(), &store.Source{ID: "s1", Name: "src"})
	if err != nil {
		t.Fatalf("CreateSource() error: %v", err)
	}

	err = c.DeleteSource(context.Background(), "source with spaces")
	if err != nil {
		t.Fatalf("DeleteSource() error: %v", err)
	}

	if len(methods) != 2 || methods[0] != http.MethodPost || methods[1] != http.MethodDelete {
		t.Fatalf("unexpected methods: %v", methods)
	}
	if contentTypes[0] != "application/json" {
		t.Fatalf("expected JSON content type on POST, got %q", contentTypes[0])
	}
	if contentTypes[1] != "" {
		t.Fatalf("expected empty content type on DELETE, got %q", contentTypes[1])
	}
	if authHeaders[0] != "Bearer token-1" || authHeaders[1] != "Bearer token-1" {
		t.Fatalf("unexpected auth headers: %v", authHeaders)
	}
	if capturedRequestURI != "/api/v1/sources/source%20with%20spaces" {
		t.Fatalf("unexpected escaped delete request URI: %q", capturedRequestURI)
	}
}

func TestK8sAndGitURLConstruction(t *testing.T) {
	var got []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.RequestURI)
		w.WriteHeader(http.StatusOK)
		switch {
		case strings.Contains(r.URL.Path, "/k8s/") && strings.Contains(r.URL.Path, "/resources") && !strings.Contains(r.URL.Path, "/resources/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"resources": []map[string]any{}, "count": 0, "source_id": "k1"})
		case strings.Contains(r.URL.Path, "/k8s/") && strings.Contains(r.URL.Path, "/resources/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"resource": map[string]any{}, "source_id": "k1"})
		case strings.Contains(r.URL.Path, "/k8s/") && strings.Contains(r.URL.Path, "/logs/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"logs": "ok", "pod": "p", "namespace": "ns", "source_id": "k1"})
		case strings.Contains(r.URL.Path, "/git/") && strings.HasSuffix(r.URL.Path, "/file"):
			_ = json.NewEncoder(w).Encode(map[string]any{"content": "abc"})
		case strings.Contains(r.URL.Path, "/git/") && strings.HasSuffix(r.URL.Path, "/files"):
			_ = json.NewEncoder(w).Encode(map[string]any{"files": []map[string]any{}})
		case strings.Contains(r.URL.Path, "/git/") && strings.HasSuffix(r.URL.Path, "/log"):
			_ = json.NewEncoder(w).Encode(map[string]any{"commits": []map[string]any{}})
		case strings.Contains(r.URL.Path, "/git/") && strings.HasSuffix(r.URL.Path, "/diff"):
			_ = json.NewEncoder(w).Encode(map[string]any{"diff": ""})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, _ = c.K8sListResources(context.Background(), "src 1", "deployments", "prod/ns")
	_, _ = c.K8sGetResource(context.Background(), "src 1", "deployments", "prod/ns", "name/with/slash")
	_, _ = c.K8sGetLogs(context.Background(), "src 1", "prod/ns", "pod 1", "ctr", 50)
	_, _ = c.GitReadFile(context.Background(), "git 1", "dir/file.go")
	_, _ = c.GitListFiles(context.Background(), "git 1", "infra")
	_, _ = c.GitLog(context.Background(), "git 1", 10)
	_, _ = c.GitDiff(context.Background(), "git 1", "main", "feature/x")

	joined := strings.Join(got, "\n")
	assertContains(t, joined, "/api/v1/k8s/src%201/resources?resource=deployments&namespace=prod%2Fns")
	assertContains(t, joined, "/api/v1/k8s/src%201/resources/deployments/prod%2Fns/name%2Fwith%2Fslash")
	assertContains(t, joined, "/api/v1/k8s/src%201/logs/prod%2Fns/pod%201?container=ctr&tail=50")
	assertContains(t, joined, "/api/v1/git/git%201/file?path=dir%2Ffile.go")
	assertContains(t, joined, "/api/v1/git/git%201/files?dir=infra")
	assertContains(t, joined, "/api/v1/git/git%201/log?limit=10")
	assertContains(t, joined, "/api/v1/git/git%201/diff?from=main&to=feature%2Fx")
}

func TestAWSEndpointsAndDecodeError(t *testing.T) {
	var paths []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.WriteHeader(http.StatusOK)
		switch {
		case strings.HasSuffix(r.URL.Path, "/ec2/instances"):
			_ = json.NewEncoder(w).Encode(map[string]any{"instances": []map[string]any{}, "count": 0, "source_id": "a1"})
		case strings.Contains(r.URL.Path, "/ec2/instances/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"instance": map[string]any{"instance_id": "i-1"}, "source_id": "a1"})
		case strings.HasSuffix(r.URL.Path, "/eks/clusters"):
			_ = json.NewEncoder(w).Encode(map[string]any{"clusters": []map[string]any{}, "count": 0, "source_id": "a1"})
		case strings.Contains(r.URL.Path, "/eks/clusters/"):
			_, _ = w.Write([]byte("{")) // trigger decode error
		case strings.HasSuffix(r.URL.Path, "/rds/instances"):
			_ = json.NewEncoder(w).Encode(map[string]any{"instances": []map[string]any{}, "count": 0, "source_id": "a1"})
		case strings.Contains(r.URL.Path, "/rds/instances/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"instance": map[string]any{"db_instance_id": "db-1"}, "source_id": "a1"})
		case strings.HasSuffix(r.URL.Path, "/vpc/vpcs"):
			_ = json.NewEncoder(w).Encode(map[string]any{"vpcs": []map[string]any{}, "count": 0, "source_id": "a1"})
		case strings.Contains(r.URL.Path, "/vpc/vpcs/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"vpc": map[string]any{"vpc_id": "vpc-1"}, "source_id": "a1"})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, _ = c.AWSEC2ListInstances(context.Background(), "src")
	_, _ = c.AWSEC2GetInstance(context.Background(), "src", "i-1")
	_, _ = c.AWSEKSListClusters(context.Background(), "src")
	_, err := c.AWSEKSGetCluster(context.Background(), "src", "c1")
	if err == nil || !strings.Contains(err.Error(), "decode aws eks get cluster response") {
		t.Fatalf("expected decode error, got %v", err)
	}
	_, _ = c.AWSRDSListInstances(context.Background(), "src")
	_, _ = c.AWSRDSGetInstance(context.Background(), "src", "db-1")
	_, _ = c.AWSVPCListVPCs(context.Background(), "src")
	_, _ = c.AWSVPCGetVPC(context.Background(), "src", "vpc-1")

	joined := strings.Join(paths, "\n")
	assertContains(t, joined, "/api/v1/aws/src/ec2/instances")
	assertContains(t, joined, "/api/v1/aws/src/ec2/instances/i-1")
	assertContains(t, joined, "/api/v1/aws/src/eks/clusters")
	assertContains(t, joined, "/api/v1/aws/src/eks/clusters/c1")
	assertContains(t, joined, "/api/v1/aws/src/rds/instances")
	assertContains(t, joined, "/api/v1/aws/src/rds/instances/db-1")
	assertContains(t, joined, "/api/v1/aws/src/vpc/vpcs")
	assertContains(t, joined, "/api/v1/aws/src/vpc/vpcs/vpc-1")
}

func TestContextCancellation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","version":"v","time":"t"}`))
	}))
	defer ts.Close()

	c := New(ts.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := c.GetStatus(ctx)
	if err == nil || !strings.Contains(err.Error(), "status request failed") {
		t.Fatalf("expected request failed due to context cancellation, got %v", err)
	}
}

func TestPingListSourcesGraphSummary(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		switch r.URL.Path {
		case apiStatusPath:
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "version": "v", "time": "t"})
		case apiSourcesPath:
			_ = json.NewEncoder(w).Encode(map[string]any{"sources": []map[string]any{}, "count": 0})
		case apiGraphSummaryPath:
			_ = json.NewEncoder(w).Encode(map[string]any{"NodeCount": 1, "EdgeCount": 2, "NodesByType": map[string]int{"svc": 1}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	c := New(ts.URL)
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error: %v", err)
	}

	sources, err := c.ListSources(context.Background())
	if err != nil {
		t.Fatalf("ListSources() error: %v", err)
	}
	if len(sources) != 0 {
		t.Fatalf("expected no sources, got %d", len(sources))
	}

	summary, err := c.GraphSummary(context.Background())
	if err != nil {
		t.Fatalf("GraphSummary() error: %v", err)
	}
	if summary.NodeCount != 1 || summary.EdgeCount != 2 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("expected %q to contain %q", got, want)
	}
}
