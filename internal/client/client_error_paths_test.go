// Package client error-path tests.
// Each function at 80% has exactly one uncovered branch: the error return
// from doJSON when the server responds with a non-2xx status. This file
// covers those branches across all source files to push overall coverage
// above 90%.
package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// errorServer returns a helper that responds with 500 + structured JSON error.
func errorServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "internal", "message": "test error"})
	}))
}

// --- client.go error paths ---

func TestGraphQuery_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.GraphQuery(context.Background(), "q")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGraphRelated_Non200StructuredError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "bad_request", "message": "invalid node"})
	}))
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.GraphRelated(context.Background(), "node-x", 1)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestListSources_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.ListComponents(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGraphSummary_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.GraphSummary(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestK8sListResources_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.K8sListResources(context.Background(), "src", "pods", "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestK8sGetResource_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.K8sGetResource(context.Background(), "src", "pods", "default", "mypod")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestK8sGetLogs_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.K8sGetLogs(context.Background(), "src", "default", "pod", "", 0)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGitReadFile_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.GitReadFile(context.Background(), "git-1", "README.md")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGitDiff_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.GitDiff(context.Background(), "git-1", "main", "feature")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAWSEC2ListInstances_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.AWSEC2ListInstances(context.Background(), "aws-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAWSEC2GetInstance_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.AWSEC2GetInstance(context.Background(), "aws-1", "i-123")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAWSEKSListClusters_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.AWSEKSListClusters(context.Background(), "aws-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAWSRDSListInstances_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.AWSRDSListInstances(context.Background(), "aws-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAWSRDSGetInstance_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.AWSRDSGetInstance(context.Background(), "aws-1", "db-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAWSVPCListVPCs_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.AWSVPCListVPCs(context.Background(), "aws-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAWSVPCGetVPC_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.AWSVPCGetVPC(context.Background(), "aws-1", "vpc-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- datastore.go error paths ---

func TestPostgresStat_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.PostgresStat(context.Background(), "pg-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPostgresQuery_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.PostgresQuery(context.Background(), "pg-1", "SELECT 1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMySQLStat_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.MySQLStat(context.Background(), "mysql-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMySQLQuery_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.MySQLQuery(context.Background(), "mysql-1", "SELECT 1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRedisSlowLog_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.RedisSlowLog(context.Background(), "redis-1", 10)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRedisDBSize_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.RedisDBSize(context.Background(), "redis-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMongoDBServerStatus_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.MongoDBServerStatus(context.Background(), "mongo-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMongoDBReplicaStatus_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.MongoDBReplicaStatus(context.Background(), "mongo-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMongoDBCurrentOp_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.MongoDBCurrentOp(context.Background(), "mongo-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestKafkaTopics_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.KafkaTopics(context.Background(), "kafka-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestKafkaBrokers_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.KafkaBrokers(context.Background(), "kafka-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestKafkaConsumerGroups_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.KafkaConsumerGroups(context.Background(), "kafka-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestElasticsearchHealth_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.ElasticsearchHealth(context.Background(), "es-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- gitops.go error paths ---

func TestArgoCDGetApp_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.ArgoCDGetApp(context.Background(), "argocd-1", "my-app")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestArgoCDGetDiff_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.ArgoCDGetDiff(context.Background(), "argocd-1", "my-app")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestArgoCDGetHistory_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.ArgoCDGetHistory(context.Background(), "argocd-1", "my-app", 10)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestTerraformGetResource_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.TerraformGetResource(context.Background(), "tf-1", "aws_instance.web")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestTerraformOutputs_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.TerraformOutputs(context.Background(), "tf-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHelmGetRelease_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.HelmGetRelease(context.Background(), "helm-1", "my-release", "default")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHelmHistory_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.HelmHistory(context.Background(), "helm-1", "default", "my-release", 10)
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- networking.go error paths ---

func TestNginxStatus_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.NginxStatus(context.Background(), "nginx-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEnvoyClusters_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.EnvoyClusters(context.Background(), "envoy-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- drift.go error paths ---

func TestDetectDriftByEntry_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.DetectDriftByEntry(context.Background(), "entry-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- knowledge.go error paths ---

func TestGetKnowledgeEntry_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.GetKnowledgeEntry(context.Background(), "entry-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- proposals.go error paths ---

func TestGetProposal_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.GetProposal(context.Background(), "prop-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- registry.go error paths ---

func TestOCIListRepos_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.OCIListRepos(context.Background(), "oci-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestOCIListTags_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.OCIListTags(context.Background(), "oci-1", "myrepo")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestOCIGetManifest_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.OCIGetManifest(context.Background(), "oci-1", "myrepo", "latest")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestArtifactoryListRepos_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.ArtifactoryListRepos(context.Background(), "art-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestArtifactoryListDockerTags_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.ArtifactoryListDockerTags(context.Background(), "art-1", "docker-local", "myimage")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestArtifactoryGetArtifactInfo_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.ArtifactoryGetArtifactInfo(context.Background(), "art-1", "libs-release", "com/example/artifact.jar")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestECRListRepos_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.ECRListRepos(context.Background(), "ecr-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestECRListImages_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.ECRListImages(context.Background(), "ecr-1", "myrepo")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestECRGetImage_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.ECRGetImage(context.Background(), "ecr-1", "myrepo", "latest")
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestAWSEKSGetCluster_Error exercises the error return from doJSON
// (already partially covered by existing decode-error test, but adds HTTP error)
func TestAWSEKSGetCluster_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.AWSEKSGetCluster(context.Background(), "aws-1", "my-cluster")
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestGraphRelated_404StructuredError exercises the 404 structured-error path
func TestGraphRelated_404StructuredError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "not_found", "message": "node missing"})
	}))
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.GraphRelated(context.Background(), "ghost-node", 1)
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestGraphRelated_Non200RawError exercises the non-200 raw error path
func TestGraphRelated_Non200RawError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("service unavailable"))
	}))
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.GraphRelated(context.Background(), "node-y", 2)
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestGraphRelated_Non200StructuredError exercises the 500 structured error path
func TestGraphRelated_Non200StructuredAPIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "internal", "message": "store error"})
	}))
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.GraphRelated(context.Background(), "node-z", 1)
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestPrometheusQuery_Error exercises error path for PrometheusQuery
func TestPrometheusQuery_Error(t *testing.T) {
	ts := errorServer(t)
	defer ts.Close()
	c := New(ts.URL)
	_, err := c.PrometheusQuery(context.Background(), "prom-1", "up", time.Time{})
	if err == nil {
		t.Fatal("expected error")
	}
}
