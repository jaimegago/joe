package coreagent

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/adapters/k8s"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
	_ "github.com/mattn/go-sqlite3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type fakeK8sAdapter struct {
	items map[string][]unstructured.Unstructured
}

func (f *fakeK8sAdapter) Connect(_ store.Source) error { return nil }

func (f *fakeK8sAdapter) Disconnect() error { return nil }

func (f *fakeK8sAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true, Message: "connected"}
}

func (f *fakeK8sAdapter) ListResources(_ context.Context, resource, _ string) ([]unstructured.Unstructured, error) {
	items, ok := f.items[resource]
	if !ok {
		return nil, nil
	}
	return items, nil
}

func (f *fakeK8sAdapter) GetResource(_ context.Context, resource, _, name string) (*unstructured.Unstructured, error) {
	items, ok := f.items[resource]
	if !ok {
		return nil, errors.New("not found")
	}
	for i := range items {
		if items[i].GetName() == name {
			return &items[i], nil
		}
	}
	return nil, errors.New("not found")
}

func (f *fakeK8sAdapter) GetPodLogs(_ context.Context, _, _, _ string, _ int) (string, error) {
	return "", nil
}

func setupK8sGraphStore(t *testing.T) graph.GraphStore {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=on")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE graph_nodes (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			source_id TEXT,
			metadata TEXT DEFAULT '{}',
			first_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			last_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX idx_graph_nodes_type ON graph_nodes(type);
		CREATE INDEX idx_graph_nodes_source ON graph_nodes(source_id);

		CREATE TABLE graph_edges (
			from_node TEXT NOT NULL REFERENCES graph_nodes(id) ON DELETE CASCADE,
			to_node TEXT NOT NULL REFERENCES graph_nodes(id) ON DELETE CASCADE,
			relation TEXT NOT NULL,
			confidence INTEGER DEFAULT 3,
			source TEXT,
			context TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (from_node, to_node, relation)
		);
		CREATE INDEX idx_graph_edges_from ON graph_edges(from_node);
		CREATE INDEX idx_graph_edges_to ON graph_edges(to_node);
		CREATE INDEX idx_graph_edges_relation ON graph_edges(relation);
	`)
	if err != nil {
		t.Fatalf("create tables: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })
	return graph.NewSQLiteStore(db, nil)
}

func TestRefreshK8sSourceMapping(t *testing.T) {
	graphStore := setupK8sGraphStore(t)
	refresher := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}

	source := &store.Source{ID: "src-1", Type: store.SourceTypeKubernetes, Name: "test"}
	adapter := &fakeK8sAdapter{items: map[string][]unstructured.Unstructured{
		"namespaces":  {makeNamespace("apps")},
		"deployments": {makeDeployment("apps", "api")},
		"services":    {makeService("apps", "api")},
		"configmaps":  {makeConfigMap("apps", "app-config"), makeConfigMap("apps", "cm-vol")},
		"secrets":     {makeSecret("apps", "app-secret")},
		"nodes":       {makeNode("node-1")},
	}}

	if err := refresher.refreshK8sSource(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshK8sSource error: %v", err)
	}

	nodes, edges, err := LoadGraphStateForSource(context.Background(), graphStore, source.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForSource error: %v", err)
	}

	if len(nodes) != 7 {
		t.Fatalf("nodes count = %d, want 7", len(nodes))
	}

	nodeIDs := map[string]struct{}{}
	for _, node := range nodes {
		nodeIDs[node.ID] = struct{}{}
	}

	expectedNodes := []string{
		"k8s/src-1/namespace/apps",
		"k8s/src-1/deployment/apps/api",
		"k8s/src-1/service/apps/api",
		"k8s/src-1/configmap/apps/app-config",
		"k8s/src-1/configmap/apps/cm-vol",
		"k8s/src-1/secret/apps/app-secret",
		"k8s/src-1/node/node-1",
	}
	for _, id := range expectedNodes {
		if _, ok := nodeIDs[id]; !ok {
			t.Fatalf("missing node %s", id)
		}
	}

	requireEdge(t, edges, "k8s/src-1/namespace/apps", "k8s/src-1/deployment/apps/api", "contains")
	requireEdge(t, edges, "k8s/src-1/namespace/apps", "k8s/src-1/service/apps/api", "contains")
	requireEdge(t, edges, "k8s/src-1/namespace/apps", "k8s/src-1/configmap/apps/app-config", "contains")
	requireEdge(t, edges, "k8s/src-1/namespace/apps", "k8s/src-1/secret/apps/app-secret", "contains")
	requireEdge(t, edges, "k8s/src-1/service/apps/api", "k8s/src-1/deployment/apps/api", "routes_to")
	requireEdge(t, edges, "k8s/src-1/deployment/apps/api", "k8s/src-1/configmap/apps/app-config", "references")
	requireEdge(t, edges, "k8s/src-1/deployment/apps/api", "k8s/src-1/configmap/apps/cm-vol", "references")
	requireEdge(t, edges, "k8s/src-1/deployment/apps/api", "k8s/src-1/secret/apps/app-secret", "references")
}

func TestRefreshK8sSourceMapping_NoSelectorMatch(t *testing.T) {
	graphStore := setupK8sGraphStore(t)
	refresher := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}

	source := &store.Source{ID: "src-2", Type: store.SourceTypeKubernetes, Name: "test"}
	adapter := &fakeK8sAdapter{items: map[string][]unstructured.Unstructured{
		"namespaces":  {makeNamespace("apps")},
		"deployments": {makeDeployment("apps", "api")},
		"services":    {makeServiceWithSelector("apps", "api", map[string]any{"app": "other"})},
		"nodes":       {makeNode("node-1")},
	}}

	if err := refresher.refreshK8sSource(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshK8sSource error: %v", err)
	}

	_, edges, err := LoadGraphStateForSource(context.Background(), graphStore, source.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForSource error: %v", err)
	}

	requireNoEdge(t, edges, "k8s/src-2/service/apps/api", "k8s/src-2/deployment/apps/api", "routes_to")
	requireNoEdge(t, edges, "k8s/src-2/namespace/apps", "k8s/src-2/node/node-1", "contains")
}

func requireEdge(t *testing.T, edges []graph.Edge, from, to, relation string) {
	t.Helper()
	for _, edge := range edges {
		if edge.From == from && edge.To == to && edge.Relation == relation {
			return
		}
	}
	t.Fatalf("missing edge %s --%s--> %s", from, relation, to)
}

func requireNoEdge(t *testing.T, edges []graph.Edge, from, to, relation string) {
	t.Helper()
	for _, edge := range edges {
		if edge.From == from && edge.To == to && edge.Relation == relation {
			t.Fatalf("unexpected edge %s --%s--> %s", from, relation, to)
		}
	}
}

func makeNamespace(name string) unstructured.Unstructured {
	obj := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]any{
			"name": name,
		},
	}}
	return obj
}

func makeDeployment(namespace, name string) unstructured.Unstructured {
	obj := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
			"labels": map[string]any{
				"app": name,
			},
		},
		"spec": map[string]any{
			"replicas": int64(2),
			"selector": map[string]any{
				"matchLabels": map[string]any{
					"app": name,
				},
			},
			"template": map[string]any{
				"metadata": map[string]any{
					"labels": map[string]any{
						"app": name,
					},
				},
				"spec": map[string]any{
					"containers": []any{
						map[string]any{
							"name": name,
							"envFrom": []any{
								map[string]any{
									"configMapRef": map[string]any{"name": "app-config"},
								},
							},
							"env": []any{
								map[string]any{
									"name": "SECRET",
									"valueFrom": map[string]any{
										"secretKeyRef": map[string]any{"name": "app-secret"},
									},
								},
							},
						},
					},
					"volumes": []any{
						map[string]any{"configMap": map[string]any{"name": "cm-vol"}},
					},
				},
			},
		},
	}}
	return obj
}

func makeService(namespace, name string) unstructured.Unstructured {
	return makeServiceWithSelector(namespace, name, map[string]any{"app": name})
}

func makeServiceWithSelector(namespace, name string, selector map[string]any) unstructured.Unstructured {
	obj := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
		"spec": map[string]any{
			"type":     "ClusterIP",
			"selector": selector,
			"ports": []any{
				map[string]any{"port": int64(80), "targetPort": int64(8080)},
			},
		},
	}}
	return obj
}

func makeConfigMap(namespace, name string) unstructured.Unstructured {
	obj := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
		"data": map[string]any{
			"FOO": "bar",
		},
	}}
	return obj
}

func makeSecret(namespace, name string) unstructured.Unstructured {
	obj := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
		"data": map[string]any{
			"BAR": "YmF6",
		},
	}}
	return obj
}

func makeNode(name string) unstructured.Unstructured {
	obj := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Node",
		"metadata": map[string]any{
			"name": name,
			"labels": map[string]any{
				"node-role.kubernetes.io/worker": "",
			},
		},
		"spec": map[string]any{
			"taints": []any{map[string]any{"key": "dedicated"}},
		},
		"status": map[string]any{
			"capacity": map[string]any{
				"cpu":    "4",
				"memory": "16Gi",
				"pods":   "110",
			},
		},
	}}
	return obj
}

var _ k8s.KubernetesAdapter = (*fakeK8sAdapter)(nil)
