package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/jaimegago/joe/internal/access"
	alertmanageradapter "github.com/jaimegago/joe/internal/adapters/alerting/alertmanager"
	grafanaadapter "github.com/jaimegago/joe/internal/adapters/alerting/grafana"
	pagerdutyadapter "github.com/jaimegago/joe/internal/adapters/alerting/pagerduty"
	awsadapter "github.com/jaimegago/joe/internal/adapters/aws"
	elasticsearchadapter "github.com/jaimegago/joe/internal/adapters/datastore/elasticsearch"
	kafkaadapter "github.com/jaimegago/joe/internal/adapters/datastore/kafka"
	mysqladapter "github.com/jaimegago/joe/internal/adapters/datastore/mysql"
	postgresadapter "github.com/jaimegago/joe/internal/adapters/datastore/postgres"
	redisadapter "github.com/jaimegago/joe/internal/adapters/datastore/redis"
	gitadapter "github.com/jaimegago/joe/internal/adapters/git"
	githubadapter "github.com/jaimegago/joe/internal/adapters/github"
	gitlabadapter "github.com/jaimegago/joe/internal/adapters/gitlab"
	argocdadapter "github.com/jaimegago/joe/internal/adapters/gitops/argocd"
	terraformadapter "github.com/jaimegago/joe/internal/adapters/iac/terraform"
	envoyadapter "github.com/jaimegago/joe/internal/adapters/networking/envoy"
	nginxadapter "github.com/jaimegago/joe/internal/adapters/networking/nginx"
	datadogadapter "github.com/jaimegago/joe/internal/adapters/observability/datadog"
	dynatraceadapter "github.com/jaimegago/joe/internal/adapters/observability/dynatrace"
	jaegeradapter "github.com/jaimegago/joe/internal/adapters/observability/jaeger"
	lokiadapter "github.com/jaimegago/joe/internal/adapters/observability/loki"
	newrelicadapter "github.com/jaimegago/joe/internal/adapters/observability/newrelic"
	prometheusadapter "github.com/jaimegago/joe/internal/adapters/observability/prometheus"
	splunkadapter "github.com/jaimegago/joe/internal/adapters/observability/splunk"
	tempoadapter "github.com/jaimegago/joe/internal/adapters/observability/tempo"
	helmadapter "github.com/jaimegago/joe/internal/adapters/packaging/helm"
	artifactoryadapter "github.com/jaimegago/joe/internal/adapters/registry/artifactory"
	ecradapter "github.com/jaimegago/joe/internal/adapters/registry/ecr"
	ociadapter "github.com/jaimegago/joe/internal/adapters/registry/oci"
	falcoadapter "github.com/jaimegago/joe/internal/adapters/security/falco"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
	"time"
)

// inProcessCoreClient is the loop's in-process implementation of
// coretools.CoreToolsClient. Every method reads the caller principal from
// context via rbac.PrincipalFromContext — the SAME principal Phase B/C/D
// established at the edge (auth.EdgeAuth → rbac.WithPrincipal) — and delegates
// to the guarded accessor for adapter/graph access. There is NO loopback HTTP
// hop and NO re-authentication: identity is established once at the edge and
// carried by context, per docs/reference/joe-identity-design.md §1.
//
// For tools that do not touch an adapter or the graph store (list_components),
// the client reaches the in-process service directly. These services are not
// principal-gated today
// (they predate Phase A's accessor) and are NOT what the no-ungoverned-access
// invariant covers — that invariant guards adapters and the graph store only
// (internal/api/access_guard_test.go).
type inProcessCoreClient struct {
	accessor   *access.Accessor
	services   *core.Services
	components store.ComponentRepository
}

// newInProcessCoreClient builds the in-process accessor-backed client used by
// the loop's tool registry. It panics on a nil accessor or services — both are
// composition-time invariants in joe's wiring.
func newInProcessCoreClient(accessor *access.Accessor, services *core.Services) *inProcessCoreClient {
	if accessor == nil {
		panic("api.newInProcessCoreClient: accessor must not be nil")
	}
	if services == nil {
		panic("api.newInProcessCoreClient: services must not be nil")
	}
	c := &inProcessCoreClient{
		accessor: accessor,
		services: services,
	}
	if services.Store != nil {
		c.components = services.Store.Components
	}
	return c
}

// Each method below reads rbac.PrincipalFromContext(ctx) at the call site (NOT
// via a helper) so the static guard in access_phaseb_test.go can see the
// context derivation in each function body. The edge middleware (auth.EdgeAuth)
// always sets a principal for a non-public path — see
// docs/reference/joe-identity-design.md §1.

// --- list_components (uses services.Store.Components; not an adapter) ---

func (c *inProcessCoreClient) ListComponents(ctx context.Context) ([]*store.Component, error) {
	if c.components == nil {
		return nil, fmt.Errorf("component repository not configured")
	}
	return c.components.List(ctx)
}

// --- graph (gated by accessor under reserved GraphComponentID) ---

func (c *inProcessCoreClient) GraphQuery(ctx context.Context, query string) ([]graph.Node, error) {
	return c.accessor.GraphQuery(ctx, rbac.PrincipalFromContext(ctx), query)
}

func (c *inProcessCoreClient) GraphRelated(ctx context.Context, nodeID string, depth int) (*graph.Subgraph, error) {
	return c.accessor.GraphRelated(ctx, rbac.PrincipalFromContext(ctx), nodeID, depth)
}

// --- k8s (gated by accessor; convert unstructured → map[string]any to keep
//     the tool-facing shape identical to what *client.Client returned over HTTP) ---

func (c *inProcessCoreClient) K8sListResources(ctx context.Context, sourceID, resource, namespace string) ([]map[string]any, error) {
	items, err := c.accessor.K8sListResources(ctx, rbac.PrincipalFromContext(ctx), sourceID, resource, namespace)
	if err != nil {
		return nil, mapAccessError(err, sourceID)
	}
	out := make([]map[string]any, len(items))
	for i, item := range items {
		out[i] = item.Object
	}
	return out, nil
}

func (c *inProcessCoreClient) K8sGetResource(ctx context.Context, sourceID, resource, namespace, name string) (map[string]any, error) {
	obj, err := c.accessor.K8sGetResource(ctx, rbac.PrincipalFromContext(ctx), sourceID, resource, namespace, name)
	if err != nil {
		return nil, mapAccessError(err, sourceID)
	}
	return obj.Object, nil
}

func (c *inProcessCoreClient) K8sGetLogs(ctx context.Context, sourceID, namespace, pod, container string, tailLines int) (string, error) {
	logs, err := c.accessor.K8sGetPodLogs(ctx, rbac.PrincipalFromContext(ctx), sourceID, namespace, pod, container, tailLines)
	if err != nil {
		return "", mapAccessError(err, sourceID)
	}
	return logs, nil
}

// --- git (gated by accessor) ---

func (c *inProcessCoreClient) GitReadFile(ctx context.Context, sourceID, path string) (string, error) {
	out, err := c.accessor.GitReadFile(ctx, rbac.PrincipalFromContext(ctx), sourceID, path)
	return out, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) GitListFiles(ctx context.Context, sourceID, dir string) ([]gitadapter.FileInfo, error) {
	out, err := c.accessor.GitListFiles(ctx, rbac.PrincipalFromContext(ctx), sourceID, dir)
	return out, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) GitLog(ctx context.Context, sourceID string, limit int) ([]gitadapter.CommitInfo, error) {
	out, err := c.accessor.GitLog(ctx, rbac.PrincipalFromContext(ctx), sourceID, limit)
	return out, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) GitDiff(ctx context.Context, sourceID, from, to string) (string, error) {
	out, err := c.accessor.GitDiff(ctx, rbac.PrincipalFromContext(ctx), sourceID, from, to)
	return out, mapAccessError(err, sourceID)
}

// --- AWS ---

func (c *inProcessCoreClient) AWSEC2ListInstances(ctx context.Context, sourceID string) ([]*awsadapter.EC2Instance, error) {
	items, err := c.accessor.AWSListEC2Instances(ctx, rbac.PrincipalFromContext(ctx), sourceID)
	if err != nil {
		return nil, mapAccessError(err, sourceID)
	}
	out := make([]*awsadapter.EC2Instance, len(items))
	for i := range items {
		v := items[i]
		out[i] = &v
	}
	return out, nil
}

func (c *inProcessCoreClient) AWSEC2GetInstance(ctx context.Context, sourceID, instanceID string) (*awsadapter.EC2Instance, error) {
	v, err := c.accessor.AWSGetEC2Instance(ctx, rbac.PrincipalFromContext(ctx), sourceID, instanceID)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) AWSEKSListClusters(ctx context.Context, sourceID string) ([]*awsadapter.EKSCluster, error) {
	items, err := c.accessor.AWSListEKSClusters(ctx, rbac.PrincipalFromContext(ctx), sourceID)
	if err != nil {
		return nil, mapAccessError(err, sourceID)
	}
	out := make([]*awsadapter.EKSCluster, len(items))
	for i := range items {
		v := items[i]
		out[i] = &v
	}
	return out, nil
}

func (c *inProcessCoreClient) AWSEKSGetCluster(ctx context.Context, sourceID, clusterName string) (*awsadapter.EKSCluster, error) {
	v, err := c.accessor.AWSGetEKSCluster(ctx, rbac.PrincipalFromContext(ctx), sourceID, clusterName)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) AWSRDSListInstances(ctx context.Context, sourceID string) ([]*awsadapter.RDSInstance, error) {
	items, err := c.accessor.AWSListRDSInstances(ctx, rbac.PrincipalFromContext(ctx), sourceID)
	if err != nil {
		return nil, mapAccessError(err, sourceID)
	}
	out := make([]*awsadapter.RDSInstance, len(items))
	for i := range items {
		v := items[i]
		out[i] = &v
	}
	return out, nil
}

func (c *inProcessCoreClient) AWSRDSGetInstance(ctx context.Context, sourceID, instanceID string) (*awsadapter.RDSInstance, error) {
	v, err := c.accessor.AWSGetRDSInstance(ctx, rbac.PrincipalFromContext(ctx), sourceID, instanceID)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) AWSVPCList(ctx context.Context, sourceID string) ([]*awsadapter.VPC, error) {
	items, err := c.accessor.AWSListVPCs(ctx, rbac.PrincipalFromContext(ctx), sourceID)
	if err != nil {
		return nil, mapAccessError(err, sourceID)
	}
	out := make([]*awsadapter.VPC, len(items))
	for i := range items {
		v := items[i]
		out[i] = &v
	}
	return out, nil
}

func (c *inProcessCoreClient) AWSVPCGet(ctx context.Context, sourceID, vpcID string) (*awsadapter.VPC, error) {
	v, err := c.accessor.AWSGetVPC(ctx, rbac.PrincipalFromContext(ctx), sourceID, vpcID)
	return v, mapAccessError(err, sourceID)
}

// --- Prometheus / Loki / Tempo / Jaeger ---

func (c *inProcessCoreClient) PrometheusQuery(ctx context.Context, sourceID, query string, queryTime time.Time) (*prometheusadapter.QueryResult, error) {
	v, err := c.accessor.PrometheusQuery(ctx, rbac.PrincipalFromContext(ctx), sourceID, query, queryTime)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) PrometheusQueryRange(ctx context.Context, sourceID, query string, start, end time.Time, step time.Duration) (*prometheusadapter.QueryResult, error) {
	v, err := c.accessor.PrometheusQueryRange(ctx, rbac.PrincipalFromContext(ctx), sourceID, query, start, end, step)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) PrometheusTargets(ctx context.Context, sourceID string) ([]prometheusadapter.Target, error) {
	v, err := c.accessor.PrometheusTargets(ctx, rbac.PrincipalFromContext(ctx), sourceID)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) LokiQuery(ctx context.Context, sourceID, query string, limit int, since time.Duration) (*lokiadapter.QueryResult, error) {
	v, err := c.accessor.LokiQuery(ctx, rbac.PrincipalFromContext(ctx), sourceID, query, limit, since)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) LokiQueryRange(ctx context.Context, sourceID, query string, start, end time.Time, limit int) (*lokiadapter.QueryResult, error) {
	v, err := c.accessor.LokiQueryRange(ctx, rbac.PrincipalFromContext(ctx), sourceID, query, start, end, limit)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) TempoSearch(ctx context.Context, sourceID, service, tags string, minDurationMs, maxDurationMs, limit int) ([]tempoadapter.TraceSearchResult, error) {
	v, err := c.accessor.TempoSearch(ctx, rbac.PrincipalFromContext(ctx), sourceID, service, tags, minDurationMs, maxDurationMs, limit)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) TempoGetTrace(ctx context.Context, sourceID, traceID string) (*tempoadapter.Trace, error) {
	v, err := c.accessor.TempoGetTrace(ctx, rbac.PrincipalFromContext(ctx), sourceID, traceID)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) JaegerServices(ctx context.Context, sourceID string) ([]string, error) {
	v, err := c.accessor.JaegerServices(ctx, rbac.PrincipalFromContext(ctx), sourceID)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) JaegerTraces(ctx context.Context, sourceID, service, operation string, limit int) ([]jaegeradapter.TraceSearchResult, error) {
	v, err := c.accessor.JaegerSearchTraces(ctx, rbac.PrincipalFromContext(ctx), sourceID, service, operation, limit)
	return v, mapAccessError(err, sourceID)
}

// --- Datadog / Splunk / Dynatrace / New Relic ---

func (c *inProcessCoreClient) DatadogMetricsQuery(ctx context.Context, sourceID, query string, from, to int64) (*datadogadapter.MetricsResult, error) {
	v, err := c.accessor.DatadogMetricsQuery(ctx, rbac.PrincipalFromContext(ctx), sourceID, query, from, to)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) DatadogLogsSearch(ctx context.Context, sourceID, query string, from, to int64, limit int) (*datadogadapter.LogsResult, error) {
	v, err := c.accessor.DatadogLogsSearch(ctx, rbac.PrincipalFromContext(ctx), sourceID, query, from, to, limit)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) SplunkSearch(ctx context.Context, sourceID, query, earliest, latest string, limit int) (*splunkadapter.SearchResult, error) {
	v, err := c.accessor.SplunkSearch(ctx, rbac.PrincipalFromContext(ctx), sourceID, query, earliest, latest, limit)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) DynatraceMetricsQuery(ctx context.Context, sourceID, query string, from, to int64) (*dynatraceadapter.MetricsResult, error) {
	v, err := c.accessor.DynatraceMetricsQuery(ctx, rbac.PrincipalFromContext(ctx), sourceID, query, from, to)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) DynatraceEvents(ctx context.Context, sourceID string, from, to int64, limit int) (*dynatraceadapter.EventsResult, error) {
	v, err := c.accessor.DynatraceEvents(ctx, rbac.PrincipalFromContext(ctx), sourceID, from, to, limit)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) NewRelicNRQLQuery(ctx context.Context, sourceID string, accountID int, query string) (*newrelicadapter.NRQLResult, error) {
	v, err := c.accessor.NewRelicNRQL(ctx, rbac.PrincipalFromContext(ctx), sourceID, accountID, query)
	return v, mapAccessError(err, sourceID)
}

// --- Alerting (Alertmanager / PagerDuty / Grafana) ---

func (c *inProcessCoreClient) AlertmanagerAlerts(ctx context.Context, sourceID, filter string) ([]alertmanageradapter.Alert, error) {
	v, err := c.accessor.AlertmanagerListAlerts(ctx, rbac.PrincipalFromContext(ctx), sourceID, filter)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) PagerDutyIncidents(ctx context.Context, sourceID, serviceID, status string, limit int) ([]pagerdutyadapter.Incident, error) {
	v, err := c.accessor.PagerDutyListIncidents(ctx, rbac.PrincipalFromContext(ctx), sourceID, serviceID, status, limit)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) PagerDutyServices(ctx context.Context, sourceID string) ([]pagerdutyadapter.Service, error) {
	v, err := c.accessor.PagerDutyListServices(ctx, rbac.PrincipalFromContext(ctx), sourceID)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) GrafanaDashboards(ctx context.Context, sourceID, query string, limit int) ([]grafanaadapter.Dashboard, error) {
	v, err := c.accessor.GrafanaListDashboards(ctx, rbac.PrincipalFromContext(ctx), sourceID, query, limit)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) GrafanaDashboard(ctx context.Context, sourceID, uid string) (*grafanaadapter.DashboardDetail, error) {
	v, err := c.accessor.GrafanaGetDashboard(ctx, rbac.PrincipalFromContext(ctx), sourceID, uid)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) GrafanaAlerts(ctx context.Context, sourceID string) ([]grafanaadapter.GrafanaAlert, error) {
	v, err := c.accessor.GrafanaListAlerts(ctx, rbac.PrincipalFromContext(ctx), sourceID)
	return v, mapAccessError(err, sourceID)
}

// --- Datastores (Postgres / MySQL / Redis / MongoDB / Kafka / Elasticsearch) ---

func (c *inProcessCoreClient) PostgresStat(ctx context.Context, sourceID string) (*postgresadapter.Stat, error) {
	v, err := c.accessor.PostgresStat(ctx, rbac.PrincipalFromContext(ctx), sourceID)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) PostgresQuery(ctx context.Context, sourceID, query string) ([]map[string]any, error) {
	v, err := c.accessor.PostgresQuery(ctx, rbac.PrincipalFromContext(ctx), sourceID, query)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) MySQLStat(ctx context.Context, sourceID string) (*mysqladapter.Stat, error) {
	v, err := c.accessor.MySQLStat(ctx, rbac.PrincipalFromContext(ctx), sourceID)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) MySQLQuery(ctx context.Context, sourceID, query string) ([]map[string]any, error) {
	v, err := c.accessor.MySQLQuery(ctx, rbac.PrincipalFromContext(ctx), sourceID, query)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) RedisInfo(ctx context.Context, sourceID, section string) (map[string]string, error) {
	v, err := c.accessor.RedisInfo(ctx, rbac.PrincipalFromContext(ctx), sourceID, section)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) RedisSlowLog(ctx context.Context, sourceID string, count int64) ([]redisadapter.SlowLogEntry, error) {
	v, err := c.accessor.RedisSlowLog(ctx, rbac.PrincipalFromContext(ctx), sourceID, count)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) RedisDBSize(ctx context.Context, sourceID string) (int64, error) {
	v, err := c.accessor.RedisDBSize(ctx, rbac.PrincipalFromContext(ctx), sourceID)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) MongoDBServerStatus(ctx context.Context, sourceID string) (map[string]any, error) {
	v, err := c.accessor.MongoDBServerStatus(ctx, rbac.PrincipalFromContext(ctx), sourceID)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) MongoDBReplicaStatus(ctx context.Context, sourceID string) (map[string]any, error) {
	v, err := c.accessor.MongoDBReplicaStatus(ctx, rbac.PrincipalFromContext(ctx), sourceID)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) MongoDBCurrentOp(ctx context.Context, sourceID string) (map[string]any, error) {
	v, err := c.accessor.MongoDBCurrentOp(ctx, rbac.PrincipalFromContext(ctx), sourceID)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) KafkaTopics(ctx context.Context, sourceID string) ([]kafkaadapter.TopicInfo, error) {
	v, err := c.accessor.KafkaTopics(ctx, rbac.PrincipalFromContext(ctx), sourceID)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) KafkaBrokers(ctx context.Context, sourceID string) ([]kafkaadapter.BrokerInfo, error) {
	v, err := c.accessor.KafkaBrokers(ctx, rbac.PrincipalFromContext(ctx), sourceID)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) KafkaConsumerGroups(ctx context.Context, sourceID string) ([]kafkaadapter.ConsumerGroupInfo, error) {
	v, err := c.accessor.KafkaConsumerGroups(ctx, rbac.PrincipalFromContext(ctx), sourceID)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) ElasticsearchHealth(ctx context.Context, sourceID string) (*elasticsearchadapter.ClusterHealth, error) {
	v, err := c.accessor.ElasticsearchClusterHealth(ctx, rbac.PrincipalFromContext(ctx), sourceID)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) ElasticsearchIndices(ctx context.Context, sourceID, pattern string) ([]elasticsearchadapter.IndexInfo, error) {
	v, err := c.accessor.ElasticsearchListIndices(ctx, rbac.PrincipalFromContext(ctx), sourceID, pattern)
	return v, mapAccessError(err, sourceID)
}

// --- GitOps / Terraform / Helm ---

func (c *inProcessCoreClient) ArgoCDApps(ctx context.Context, sourceID, project string) ([]argocdadapter.App, error) {
	v, err := c.accessor.ArgoCDApps(ctx, rbac.PrincipalFromContext(ctx), sourceID, project)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) ArgoCDGetApp(ctx context.Context, sourceID, name string) (*argocdadapter.AppDetail, error) {
	v, err := c.accessor.ArgoCDGetApp(ctx, rbac.PrincipalFromContext(ctx), sourceID, name)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) ArgoCDGetDiff(ctx context.Context, sourceID, name string) (*argocdadapter.Diff, error) {
	v, err := c.accessor.ArgoCDGetDiff(ctx, rbac.PrincipalFromContext(ctx), sourceID, name)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) ArgoCDGetHistory(ctx context.Context, sourceID, name string, limit int) ([]argocdadapter.SyncOperation, error) {
	v, err := c.accessor.ArgoCDGetHistory(ctx, rbac.PrincipalFromContext(ctx), sourceID, name, limit)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) TerraformResources(ctx context.Context, sourceID, resourceType string) ([]terraformadapter.Resource, error) {
	v, err := c.accessor.TerraformResources(ctx, rbac.PrincipalFromContext(ctx), sourceID, resourceType)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) TerraformGetResource(ctx context.Context, sourceID, address string) (*terraformadapter.Resource, error) {
	v, err := c.accessor.TerraformGetResource(ctx, rbac.PrincipalFromContext(ctx), sourceID, address)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) TerraformOutputs(ctx context.Context, sourceID string) (map[string]terraformadapter.Output, error) {
	v, err := c.accessor.TerraformOutputs(ctx, rbac.PrincipalFromContext(ctx), sourceID)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) HelmReleases(ctx context.Context, sourceID, namespace string) ([]helmadapter.Release, error) {
	v, err := c.accessor.HelmReleases(ctx, rbac.PrincipalFromContext(ctx), sourceID, namespace)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) HelmGetRelease(ctx context.Context, sourceID, namespace, name string) (*helmadapter.ReleaseDetail, error) {
	v, err := c.accessor.HelmGetRelease(ctx, rbac.PrincipalFromContext(ctx), sourceID, namespace, name)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) HelmHistory(ctx context.Context, sourceID, namespace, name string, limit int) ([]helmadapter.RevisionEntry, error) {
	v, err := c.accessor.HelmHistory(ctx, rbac.PrincipalFromContext(ctx), sourceID, namespace, name, limit)
	return v, mapAccessError(err, sourceID)
}

// --- Networking (Nginx / Envoy) ---

func (c *inProcessCoreClient) NginxIngresses(ctx context.Context, sourceID, namespace string) ([]nginxadapter.Ingress, error) {
	v, err := c.accessor.NginxListIngresses(ctx, rbac.PrincipalFromContext(ctx), sourceID, namespace)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) NginxStatus(ctx context.Context, sourceID string) (*nginxadapter.NginxStatus, error) {
	v, err := c.accessor.NginxGetStatus(ctx, rbac.PrincipalFromContext(ctx), sourceID)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) NginxConfigMaps(ctx context.Context, sourceID, namespace string) ([]nginxadapter.ConfigMapSummary, error) {
	v, err := c.accessor.NginxListConfigMaps(ctx, rbac.PrincipalFromContext(ctx), sourceID, namespace)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) EnvoyClusters(ctx context.Context, sourceID string) ([]envoyadapter.ClusterStatus, error) {
	v, err := c.accessor.EnvoyClusters(ctx, rbac.PrincipalFromContext(ctx), sourceID)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) EnvoyConfigDump(ctx context.Context, sourceID, section string) (map[string]any, error) {
	v, err := c.accessor.EnvoyConfigDump(ctx, rbac.PrincipalFromContext(ctx), sourceID, section)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) EnvoyStats(ctx context.Context, sourceID, filter string) ([]envoyadapter.Stat, error) {
	v, err := c.accessor.EnvoyStats(ctx, rbac.PrincipalFromContext(ctx), sourceID, filter)
	return v, mapAccessError(err, sourceID)
}

// --- Falco (security) ---

func (c *inProcessCoreClient) FalcoEvents(ctx context.Context, sourceID, priority, source, rule string, limit int) ([]falcoadapter.Event, error) {
	v, err := c.accessor.FalcoListEvents(ctx, rbac.PrincipalFromContext(ctx), sourceID, priority, source, rule, limit)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) FalcoRules(ctx context.Context, sourceID string) ([]falcoadapter.Rule, error) {
	v, err := c.accessor.FalcoListRules(ctx, rbac.PrincipalFromContext(ctx), sourceID)
	return v, mapAccessError(err, sourceID)
}

// --- Registries (OCI / Artifactory / ECR) ---

func (c *inProcessCoreClient) OCIListRepos(ctx context.Context, sourceID string) ([]string, error) {
	v, err := c.accessor.OCIListRepositories(ctx, rbac.PrincipalFromContext(ctx), sourceID)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) OCIListTags(ctx context.Context, sourceID, repo string) ([]string, error) {
	v, err := c.accessor.OCIListTags(ctx, rbac.PrincipalFromContext(ctx), sourceID, repo)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) OCIGetManifest(ctx context.Context, sourceID, repo, reference string) (*ociadapter.Manifest, error) {
	v, err := c.accessor.OCIGetManifest(ctx, rbac.PrincipalFromContext(ctx), sourceID, repo, reference)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) ArtifactoryListRepos(ctx context.Context, sourceID string) ([]artifactoryadapter.Repository, error) {
	v, err := c.accessor.ArtifactoryListRepositories(ctx, rbac.PrincipalFromContext(ctx), sourceID)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) ArtifactoryListDockerTags(ctx context.Context, sourceID, repo, image string) ([]string, error) {
	v, err := c.accessor.ArtifactoryListDockerTags(ctx, rbac.PrincipalFromContext(ctx), sourceID, repo, image)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) ArtifactoryGetArtifactInfo(ctx context.Context, sourceID, repo, path string) (*artifactoryadapter.ArtifactInfo, error) {
	v, err := c.accessor.ArtifactoryGetArtifactInfo(ctx, rbac.PrincipalFromContext(ctx), sourceID, repo, path)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) ECRListRepos(ctx context.Context, sourceID string) ([]ecradapter.Repository, error) {
	v, err := c.accessor.ECRListRepositories(ctx, rbac.PrincipalFromContext(ctx), sourceID)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) ECRListImages(ctx context.Context, sourceID, repo string) ([]ecradapter.ImageDetail, error) {
	v, err := c.accessor.ECRListImages(ctx, rbac.PrincipalFromContext(ctx), sourceID, repo)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) ECRGetImage(ctx context.Context, sourceID, repo, tag string) (*ecradapter.ImageDetail, error) {
	v, err := c.accessor.ECRGetImageDetails(ctx, rbac.PrincipalFromContext(ctx), sourceID, repo, tag)
	return v, mapAccessError(err, sourceID)
}

// --- VCS review (GitHub / GitLab) ---

func (c *inProcessCoreClient) GitHubGetPR(ctx context.Context, sourceID, owner, repo string, number int) (*githubadapter.PRInfo, error) {
	v, err := c.accessor.GitHubGetPR(ctx, rbac.PrincipalFromContext(ctx), sourceID, owner, repo, number)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) GitHubGetPRDiff(ctx context.Context, sourceID, owner, repo string, number int) (string, error) {
	v, err := c.accessor.GitHubGetPRDiff(ctx, rbac.PrincipalFromContext(ctx), sourceID, owner, repo, number)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) GitHubPostComment(ctx context.Context, sourceID, owner, repo string, number int, body string) error {
	err := c.accessor.GitHubPostComment(ctx, rbac.PrincipalFromContext(ctx), sourceID, owner, repo, number, body)
	return mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) GitHubRequestChanges(ctx context.Context, sourceID, owner, repo string, number int, body string) error {
	err := c.accessor.GitHubRequestChanges(ctx, rbac.PrincipalFromContext(ctx), sourceID, owner, repo, number, body)
	return mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) GitLabGetMR(ctx context.Context, sourceID, projectID string, iid int) (*gitlabadapter.MRInfo, error) {
	v, err := c.accessor.GitLabGetMR(ctx, rbac.PrincipalFromContext(ctx), sourceID, projectID, iid)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) GitLabGetMRDiff(ctx context.Context, sourceID, projectID string, iid int) (string, error) {
	v, err := c.accessor.GitLabGetMRDiff(ctx, rbac.PrincipalFromContext(ctx), sourceID, projectID, iid)
	return v, mapAccessError(err, sourceID)
}

func (c *inProcessCoreClient) GitLabPostNote(ctx context.Context, sourceID, projectID string, iid int, body string) error {
	err := c.accessor.GitLabPostNote(ctx, rbac.PrincipalFromContext(ctx), sourceID, projectID, iid, body)
	return mapAccessError(err, sourceID)
}

// mapAccessError normalises an accessor error to one a tool's caller can act on.
// nil passes through unchanged. A typed permission denial wraps to a more
// LLM-actionable string identifying the refused source (per design §4: "the
// loop surfaces the specific refused action and zone rather than silently
// stalling"). All other errors are returned as-is.
func mapAccessError(err error, sourceID string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, access.ErrPermissionDenied) {
		return fmt.Errorf("access denied for component %q: %w", sourceID, err)
	}
	return err
}
