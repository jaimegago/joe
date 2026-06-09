// Package core's CoreToolsClient is the aggregate of every small *Client
// interface the core tools depend on. It is satisfied by the in-process
// accessor-backed client built inside internal/api (the loop's Phase-E path) —
// the sole production implementation. (The two-binary-era HTTP *client.Client
// no longer satisfies it; its managed-system methods were removed along with
// the vestigial direct-HTTP surface.)
//
// Identity Phase E (docs/joe-identity-design.md §3): the loop's tool registry
// is wired to an in-process accessor — there is no loopback HTTP self-call.
// Defining the aggregate here lets NewCoreRegistry take a single client value
// while keeping the per-tool small interfaces intact for unit testing.
package core

// CoreToolsClient is the union of every per-tool *Client interface used by
// registerCoreTools. Any value passed to NewCoreRegistry must satisfy it.
type CoreToolsClient interface {
	ListComponentsClient
	GraphQueryClient
	GraphRelatedClient
	K8sGetClient
	K8sLogsClient
	GitReadClient
	GitLogClient
	GitDiffClient
	AWSEC2Client
	AWSEKSClient
	AWSRDSClient
	AWSVPCClient
	PrometheusClient
	LokiClient
	TempoClient
	JaegerClient
	AlertmanagerClient
	PagerDutyClient
	GrafanaClient
	PostgresClient
	MySQLClient
	RedisClient
	MongoDBClient
	KafkaClient
	ElasticsearchClient
	ArgoCDClient
	FluxK8sClient
	TerraformClient
	HelmClient
	NginxClient
	EnvoyClient
	IstioK8sClient
	CiliumK8sClient
	CertManagerK8sClient
	KEDAK8sClient
	OPAK8sClient
	CrossplaneK8sClient
	FalcoClient
	KnowledgeSearchClient
	OCIRegistryClient
	ArtifactoryClient
	ECRClient
	DocDriftClient
	DocDraftClient
	PublishDocClient
	GitHubPRGetClient
	GitHubPRDiffClient
	GitHubCommentClient
	GitHubRequestChangesClient
	GitLabMRGetClient
	GitLabMRDiffClient
	GitLabCommentClient
}
