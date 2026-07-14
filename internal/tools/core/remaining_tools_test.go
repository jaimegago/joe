package core_test

import (
	"context"
	"errors"
	"testing"
	"time"

	gitadapter "github.com/jaimegago/joe/internal/adapters/git"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/internal/tools/core"
)

// ---- GraphRelatedTool ----

type fakeGraphRelatedClient struct {
	fn func(ctx context.Context, nodeID string, depth int) (*graph.Subgraph, error)
}

func (f *fakeGraphRelatedClient) GraphRelated(ctx context.Context, nodeID string, depth int) (*graph.Subgraph, error) {
	return f.fn(ctx, nodeID, depth)
}

func TestGraphRelatedTool(t *testing.T) {
	fake := &fakeGraphRelatedClient{
		fn: func(_ context.Context, nodeID string, _ int) (*graph.Subgraph, error) {
			if nodeID == "svc-1" {
				return &graph.Subgraph{
					Nodes: []graph.Node{{ID: "svc-1"}, {ID: "db-1"}},
					Edges: []graph.Edge{{From: "svc-1", To: "db-1"}},
				}, nil
			}
			return nil, errors.New("node not found")
		},
	}
	tool := core.NewGraphRelatedTool(fake)

	t.Run("name and metadata", func(t *testing.T) {
		if tool.Name() != "graph_related" {
			t.Errorf("Name() = %q, want graph_related", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("Description() should not be empty")
		}
		params := tool.Parameters()
		if params.Type != "object" {
			t.Errorf("Parameters().Type = %q, want object", params.Type)
		}
		if _, ok := params.Properties["node_id"]; !ok {
			t.Error("Parameters() missing node_id property")
		}
	})

	t.Run("missing node_id", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{})
		if err == nil {
			t.Error("expected error for missing node_id")
		}
	})

	t.Run("empty node_id", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{"node_id": ""})
		if err == nil {
			t.Error("expected error for empty node_id")
		}
	})

	t.Run("success with default depth", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{"node_id": "svc-1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		sg := res.(*graph.Subgraph)
		if len(sg.Nodes) != 2 {
			t.Errorf("expected 2 nodes, got %d", len(sg.Nodes))
		}
	})

	t.Run("success with explicit depth", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{"node_id": "svc-1", "depth": float64(3)})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res == nil {
			t.Error("expected non-nil result")
		}
	})

	t.Run("client error", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{"node_id": "bad"})
		if err == nil {
			t.Error("expected error from client")
		}
	})
}

// ---- GitReadTool ----

type fakeGitReadClient struct {
	readFileFunc  func(ctx context.Context, sourceID, path string) (string, error)
	listFilesFunc func(ctx context.Context, sourceID, dir string) ([]gitadapter.FileInfo, error)
}

func (f *fakeGitReadClient) GitReadFile(ctx context.Context, sourceID, path string) (string, error) {
	return f.readFileFunc(ctx, sourceID, path)
}
func (f *fakeGitReadClient) GitListFiles(ctx context.Context, sourceID, dir string) ([]gitadapter.FileInfo, error) {
	return f.listFilesFunc(ctx, sourceID, dir)
}

func TestGitReadTool(t *testing.T) {
	fake := &fakeGitReadClient{
		readFileFunc: func(_ context.Context, sourceID, path string) (string, error) {
			if sourceID == "src" && path == "README.md" {
				return "# readme", nil
			}
			return "", errors.New("not found")
		},
		listFilesFunc: func(_ context.Context, sourceID, dir string) ([]gitadapter.FileInfo, error) {
			if sourceID == "src" && dir == "/" {
				return []gitadapter.FileInfo{
					{Path: "README.md", Size: 8, IsDir: false},
					{Path: "src", IsDir: true},
				}, nil
			}
			return nil, errors.New("not found")
		},
	}
	tool := core.NewGitReadTool(fake)

	t.Run("name and metadata", func(t *testing.T) {
		if tool.Name() != "git_read" {
			t.Errorf("Name() = %q, want git_read", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("Description() should not be empty")
		}
	})

	t.Run("missing component_id", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{"path": "README.md"})
		if err == nil {
			t.Error("expected error for missing component_id")
		}
	})

	t.Run("missing path", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{"component_id": "src"})
		if err == nil {
			t.Error("expected error for missing path")
		}
	})

	t.Run("read file success", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"component_id": "src",
			"path":         "README.md",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		if m["content"] != "# readme" {
			t.Errorf("content = %v, want '# readme'", m["content"])
		}
		if m["component_id"] != "src" {
			t.Errorf("component_id = %v, want src", m["component_id"])
		}
	})

	t.Run("list files success", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"component_id": "src",
			"path":         "/",
			"list":         true,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		files := m["files"].([]gitadapter.FileInfo)
		if len(files) != 2 {
			t.Errorf("expected 2 files, got %d", len(files))
		}
		if m["count"].(int) != 2 {
			t.Errorf("count = %v, want 2", m["count"])
		}
	})

	t.Run("read file error", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"component_id": "src",
			"path":         "missing.go",
		})
		if err == nil {
			t.Error("expected error from client")
		}
	})

	t.Run("list files error", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"component_id": "src",
			"path":         "/bad",
			"list":         true,
		})
		if err == nil {
			t.Error("expected error from client")
		}
	})
}

// ---- GitLogTool ----

type fakeGitLogClient struct {
	fn func(ctx context.Context, sourceID string, limit int) ([]gitadapter.CommitInfo, error)
}

func (f *fakeGitLogClient) GitLog(ctx context.Context, sourceID string, limit int) ([]gitadapter.CommitInfo, error) {
	return f.fn(ctx, sourceID, limit)
}

func TestGitLogTool(t *testing.T) {
	fake := &fakeGitLogClient{
		fn: func(_ context.Context, sourceID string, limit int) ([]gitadapter.CommitInfo, error) {
			if sourceID == "src" {
				commits := make([]gitadapter.CommitInfo, limit)
				for i := range commits {
					commits[i] = gitadapter.CommitInfo{Hash: "abc", Author: "dev", Date: time.Now(), Message: "fix"}
				}
				return commits, nil
			}
			return nil, errors.New("source not found")
		},
	}
	tool := core.NewGitLogTool(fake)

	t.Run("name and metadata", func(t *testing.T) {
		if tool.Name() != "git_log" {
			t.Errorf("Name() = %q, want git_log", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("Description() should not be empty")
		}
	})

	t.Run("missing component_id", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{})
		if err == nil {
			t.Error("expected error for missing component_id")
		}
	})

	t.Run("success default limit", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{"component_id": "src"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		commits := m["commits"].([]gitadapter.CommitInfo)
		if len(commits) != 20 {
			t.Errorf("expected 20 commits (default), got %d", len(commits))
		}
		if m["component_id"] != "src" {
			t.Errorf("component_id = %v, want src", m["component_id"])
		}
	})

	t.Run("success custom limit", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"component_id": "src",
			"limit":        float64(5),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		commits := m["commits"].([]gitadapter.CommitInfo)
		if len(commits) != 5 {
			t.Errorf("expected 5 commits, got %d", len(commits))
		}
	})

	t.Run("client error", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{"component_id": "bad"})
		if err == nil {
			t.Error("expected error from client")
		}
	})
}

// ---- ListComponentsTool ----

type fakeListComponentsClient struct {
	fn func(ctx context.Context) ([]*store.Component, error)
}

func (f *fakeListComponentsClient) ListComponents(ctx context.Context) ([]*store.Component, error) {
	return f.fn(ctx)
}

func TestListComponentsTool(t *testing.T) {
	fake := &fakeListComponentsClient{
		fn: func(_ context.Context) ([]*store.Component, error) {
			return []*store.Component{
				{ID: "k8s-1", Type: "kubernetes", Name: "prod"},
				{ID: "git-1", Type: "git", Name: "repo"},
				{ID: "k8s-2", Type: "kubernetes", Name: "staging"},
			}, nil
		},
	}
	tool := core.NewListComponentsTool(fake)

	t.Run("name and metadata", func(t *testing.T) {
		if tool.Name() != "list_components" {
			t.Errorf("Name() = %q, want list_components", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("Description() should not be empty")
		}
	})

	t.Run("list all components", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		if m["count"].(int) != 3 {
			t.Errorf("count = %v, want 3", m["count"])
		}
	})

	t.Run("filter by type", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{"type": "kubernetes"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		if m["count"].(int) != 2 {
			t.Errorf("count = %v, want 2 kubernetes components", m["count"])
		}
		if m["type"] != "kubernetes" {
			t.Errorf("type = %v, want kubernetes", m["type"])
		}
	})

	t.Run("filter by type no match", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{"type": "aws"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		if m["count"].(int) != 0 {
			t.Errorf("count = %v, want 0", m["count"])
		}
	})

	t.Run("client error", func(t *testing.T) {
		errTool := core.NewListComponentsTool(&fakeListComponentsClient{
			fn: func(_ context.Context) ([]*store.Component, error) {
				return nil, errors.New("connection refused")
			},
		})
		_, err := errTool.Execute(context.Background(), map[string]any{})
		if err == nil {
			t.Error("expected error from client")
		}
	})
}

// ---- K8sGetTool ----

type fakeK8sGetClient struct {
	listFn func(ctx context.Context, sourceID, resource, namespace string) ([]map[string]any, error)
	getFn  func(ctx context.Context, sourceID, resource, namespace, name string) (map[string]any, error)
}

func (f *fakeK8sGetClient) K8sListResources(ctx context.Context, sourceID, resource, namespace string) ([]map[string]any, error) {
	return f.listFn(ctx, sourceID, resource, namespace)
}
func (f *fakeK8sGetClient) K8sGetResource(ctx context.Context, sourceID, resource, namespace, name string) (map[string]any, error) {
	return f.getFn(ctx, sourceID, resource, namespace, name)
}

func TestK8sGetTool(t *testing.T) {
	fake := &fakeK8sGetClient{
		listFn: func(_ context.Context, sourceID, resource, _ string) ([]map[string]any, error) {
			if sourceID == "k8s-1" && resource == "pods" {
				return []map[string]any{
					{"name": "pod-1", "namespace": "default"},
					{"name": "pod-2", "namespace": "default"},
				}, nil
			}
			return nil, errors.New("bad source")
		},
		getFn: func(_ context.Context, sourceID, resource, _, name string) (map[string]any, error) {
			if sourceID == "k8s-1" && resource == "pods" && name == "pod-1" {
				return map[string]any{"name": "pod-1", "status": "Running"}, nil
			}
			return nil, errors.New("not found")
		},
	}
	tool := core.NewK8sGetTool(fake)

	t.Run("name and metadata", func(t *testing.T) {
		if tool.Name() != "k8s_get" {
			t.Errorf("Name() = %q, want k8s_get", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("Description() should not be empty")
		}
		params := tool.Parameters()
		if _, ok := params.Properties["component_id"]; !ok {
			t.Error("Parameters() missing component_id")
		}
		if _, ok := params.Properties["resource"]; !ok {
			t.Error("Parameters() missing resource")
		}
	})

	t.Run("missing component_id", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{"resource": "pods"})
		if err == nil {
			t.Error("expected error for missing component_id")
		}
	})

	t.Run("missing resource", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{"component_id": "k8s-1"})
		if err == nil {
			t.Error("expected error for missing resource")
		}
	})

	t.Run("list resources success", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"component_id": "k8s-1",
			"resource":     "pods",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		if m["count"].(int) != 2 {
			t.Errorf("count = %v, want 2", m["count"])
		}
	})

	t.Run("get single resource success", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"component_id": "k8s-1",
			"resource":     "pods",
			"namespace":    "default",
			"name":         "pod-1",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		obj := m["resource"].(map[string]any)
		if obj["status"] != "Running" {
			t.Errorf("status = %v, want Running", obj["status"])
		}
	})

	t.Run("list resources error", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"component_id": "bad",
			"resource":     "pods",
		})
		if err == nil {
			t.Error("expected error from client")
		}
	})

	t.Run("get resource error", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"component_id": "k8s-1",
			"resource":     "pods",
			"name":         "missing",
		})
		if err == nil {
			t.Error("expected error from client")
		}
	})
}

// ---- K8sLogsTool ----

type fakeK8sLogsClient struct {
	fn func(ctx context.Context, sourceID, namespace, pod, container string, tailLines int) (string, error)
}

func (f *fakeK8sLogsClient) K8sGetLogs(ctx context.Context, sourceID, namespace, pod, container string, tailLines int) (string, error) {
	return f.fn(ctx, sourceID, namespace, pod, container, tailLines)
}

func TestK8sLogsTool(t *testing.T) {
	fake := &fakeK8sLogsClient{
		fn: func(_ context.Context, sourceID, namespace, pod, container string, tailLines int) (string, error) {
			_, _, _ = namespace, container, tailLines
			if sourceID == "k8s-1" && pod == "pod-1" {
				return "2024-01-01 INFO started\n2024-01-01 INFO ready", nil
			}
			return "", errors.New("pod not found")
		},
	}
	tool := core.NewK8sLogsTool(fake)

	t.Run("name and metadata", func(t *testing.T) {
		if tool.Name() != "k8s_logs" {
			t.Errorf("Name() = %q, want k8s_logs", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("Description() should not be empty")
		}
		params := tool.Parameters()
		if _, ok := params.Properties["component_id"]; !ok {
			t.Error("Parameters() missing component_id")
		}
	})

	t.Run("missing component_id", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"namespace": "default",
			"pod":       "pod-1",
		})
		if err == nil {
			t.Error("expected error for missing component_id")
		}
	})

	t.Run("missing namespace", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"component_id": "k8s-1",
			"pod":          "pod-1",
		})
		if err == nil {
			t.Error("expected error for missing namespace")
		}
	})

	t.Run("missing pod", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"component_id": "k8s-1",
			"namespace":    "default",
		})
		if err == nil {
			t.Error("expected error for missing pod")
		}
	})

	t.Run("success with defaults", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"component_id": "k8s-1",
			"namespace":    "default",
			"pod":          "pod-1",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		if m["pod"] != "pod-1" {
			t.Errorf("pod = %v, want pod-1", m["pod"])
		}
		if m["namespace"] != "default" {
			t.Errorf("namespace = %v, want default", m["namespace"])
		}
	})

	t.Run("success with custom tail", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"component_id": "k8s-1",
			"namespace":    "default",
			"pod":          "pod-1",
			"tail":         float64(50),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("success with container", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"component_id": "k8s-1",
			"namespace":    "default",
			"pod":          "pod-1",
			"container":    "sidecar",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("client error", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"component_id": "k8s-1",
			"namespace":    "default",
			"pod":          "missing",
		})
		if err == nil {
			t.Error("expected error from client")
		}
	})
}
