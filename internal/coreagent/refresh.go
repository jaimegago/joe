package coreagent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jaimegago/joe/internal/adapters"
	alertmanageradapter "github.com/jaimegago/joe/internal/adapters/alerting/alertmanager"
	grafanaadapter "github.com/jaimegago/joe/internal/adapters/alerting/grafana"
	pagerdutyadapter "github.com/jaimegago/joe/internal/adapters/alerting/pagerduty"
	awsadapter "github.com/jaimegago/joe/internal/adapters/aws"
	azureadapter "github.com/jaimegago/joe/internal/adapters/azure"
	elasticsearchadapter "github.com/jaimegago/joe/internal/adapters/datastore/elasticsearch"
	kafkaadapter "github.com/jaimegago/joe/internal/adapters/datastore/kafka"
	mongodbadapter "github.com/jaimegago/joe/internal/adapters/datastore/mongodb"
	mysqladapter "github.com/jaimegago/joe/internal/adapters/datastore/mysql"
	postgresadapter "github.com/jaimegago/joe/internal/adapters/datastore/postgres"
	redisadapter "github.com/jaimegago/joe/internal/adapters/datastore/redis"
	gitadapter "github.com/jaimegago/joe/internal/adapters/git"
	argocdadapter "github.com/jaimegago/joe/internal/adapters/gitops/argocd"
	terraformadapter "github.com/jaimegago/joe/internal/adapters/iac/terraform"
	"github.com/jaimegago/joe/internal/adapters/k8s"
	envoypadapter "github.com/jaimegago/joe/internal/adapters/networking/envoy"
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
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/store"
)

// Refresher handles background refresh of the graph
type Refresher struct {
	services       *core.Services
	llm            llm.LLMAdapter
	joeFileService *JoeFileService
	logger         *slog.Logger
	metrics        *observability.Metrics
	interval       time.Duration
	stopCh         chan struct{}
	doneCh         chan struct{}
}

// NewRefresher creates a new background refresher
func NewRefresher(services *core.Services, llmAdapter llm.LLMAdapter, logger *slog.Logger, metrics *observability.Metrics) *Refresher {
	joeFileService := NewJoeFileService(services.Store.Cache, llmAdapter, logger, metrics)
	return &Refresher{
		services:       services,
		llm:            llmAdapter,
		joeFileService: joeFileService,
		logger:         logger.With("component", "refresher"),
		metrics:        observability.EnsureMetrics(metrics),
		interval:       5 * time.Minute,
		stopCh:         make(chan struct{}),
		doneCh:         make(chan struct{}),
	}
}

// Start begins the background refresh loop
func (r *Refresher) Start(ctx context.Context) error {
	r.logger.Info("starting background refresh", "interval", r.interval)

	go r.refreshLoop(ctx)
	return nil
}

// Stop gracefully stops the background refresh
func (r *Refresher) Stop(ctx context.Context) error {
	r.logger.Info("stopping background refresh")

	close(r.stopCh)

	// Wait for refresh loop to finish, with timeout
	select {
	case <-r.doneCh:
		r.logger.Info("background refresh stopped")
		return nil
	case <-time.After(10 * time.Second):
		r.logger.Warn("background refresh stop timeout")
		return fmt.Errorf("timeout waiting for refresh to stop")
	}
}

// refreshLoop is the main background refresh loop
func (r *Refresher) refreshLoop(ctx context.Context) {
	defer close(r.doneCh)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := r.refresh(ctx); err != nil {
				r.logger.Error("refresh cycle failed", "error", err)
			}
		case <-r.stopCh:
			r.logger.Info("refresh loop stopping")
			return
		case <-ctx.Done():
			r.logger.Info("refresh loop stopping due to context cancellation")
			return
		}
	}
}

// refresh performs a single refresh cycle
func (r *Refresher) refresh(ctx context.Context) (err error) {
	start := time.Now()
	defer func() { r.metrics.RecordRefreshCycle(ctx, time.Since(start), err) }()
	r.logger.Debug("starting refresh cycle")

	// Phase 5 MVP: Basic refresh structure
	// Future implementation will:
	// 1. Load connected sources from store
	// 2. For each source, query current state
	// 3. Diff against existing graph
	// 4. Apply deterministic changes
	// 5. Queue ambiguous findings for clarification

	// Load sources
	sources, err := r.services.Store.Sources.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to load sources: %w", err)
	}

	r.logger.Debug("loaded sources for refresh", "count", len(sources))

	for _, source := range sources {
		if err := r.refreshSource(ctx, source); err != nil {
			r.logger.Error("failed to refresh source", "source_id", source.ID, "error", err)
			// Continue with other sources even if one fails
		}
	}

	r.logger.Debug("completed refresh cycle")
	return nil
}

// refreshSource refreshes a single infrastructure source
func (r *Refresher) refreshSource(ctx context.Context, source *store.Source) (err error) {
	start := time.Now()
	defer func() {
		lastError := ""
		if err != nil {
			lastError = err.Error()
		}
		if updateErr := r.services.Store.Sources.UpdateSyncStatus(ctx, source.ID, time.Now(), lastError); updateErr != nil {
			r.logger.Warn("failed to update source sync status", "source_id", source.ID, "error", updateErr)
		}
		r.logger.Info("source refresh finished", "source_id", source.ID, "duration_ms", time.Since(start).Milliseconds(), "error", lastError)
	}()

	r.logger.Debug("refreshing source", "source_id", source.ID, "type", source.Type)

	adapter, err := r.services.Adapters.Get(source.ID)
	if err != nil {
		if errors.Is(err, adapters.ErrAdapterNotFound) {
			return fmt.Errorf("adapter not found for source %s", source.ID)
		}
		return fmt.Errorf("get adapter for source %s: %w", source.ID, err)
	}

	switch source.Type {
	case store.SourceTypeKubernetes:
		k8sAdapter, ok := adapter.(k8s.KubernetesAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not kubernetes", source.ID)
		}
		return r.refreshK8sSource(ctx, source, k8sAdapter)
	case store.SourceTypeGit:
		gitAdapter, ok := adapter.(gitadapter.GitAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not git", source.ID)
		}
		return r.refreshGitSource(ctx, source, gitAdapter)
	case store.SourceTypeAWS:
		awsAdapter, ok := adapter.(awsadapter.AWSAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not aws", source.ID)
		}
		return r.refreshAWSSource(ctx, source, awsAdapter)
	case store.SourceTypeAzure:
		azureAdapter, ok := adapter.(azureadapter.AzureAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not azure", source.ID)
		}
		return r.refreshAzureSource(ctx, source, azureAdapter)
	case store.SourceTypePrometheus, store.SourceTypeMimir:
		pa, ok := adapter.(prometheusadapter.PrometheusAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not prometheus", source.ID)
		}
		return r.refreshPrometheusSource(ctx, source, pa)
	case store.SourceTypeLoki:
		la, ok := adapter.(lokiadapter.LokiAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not loki", source.ID)
		}
		return r.refreshLokiSource(ctx, source, la)
	case store.SourceTypeTempo:
		ta, ok := adapter.(tempoadapter.TempoAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not tempo", source.ID)
		}
		return r.refreshTempoSource(ctx, source, ta)
	case store.SourceTypeJaeger:
		ja, ok := adapter.(jaegeradapter.JaegerAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not jaeger", source.ID)
		}
		return r.refreshJaegerSource(ctx, source, ja)
	case store.SourceTypeAlertmanager:
		aa, ok := adapter.(alertmanageradapter.AlertmanagerAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not alertmanager", source.ID)
		}
		return r.refreshAlertmanagerSource(ctx, source, aa)
	case store.SourceTypePagerDuty:
		pa, ok := adapter.(pagerdutyadapter.PagerDutyAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not pagerduty", source.ID)
		}
		return r.refreshPagerDutySource(ctx, source, pa)
	case store.SourceTypeGrafana:
		ga, ok := adapter.(grafanaadapter.GrafanaAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not grafana", source.ID)
		}
		return r.refreshGrafanaSource(ctx, source, ga)

	// Phase 6.7 — data store sources.
	case store.SourceTypePostgreSQL:
		pa, ok := adapter.(postgresadapter.PostgreSQLAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not postgresql", source.ID)
		}
		return r.refreshPostgreSQLSource(ctx, source, pa)
	case store.SourceTypeMySQL:
		ma, ok := adapter.(mysqladapter.MySQLAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not mysql", source.ID)
		}
		return r.refreshMySQLSource(ctx, source, ma)
	case store.SourceTypeRedis:
		ra, ok := adapter.(redisadapter.RedisAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not redis", source.ID)
		}
		return r.refreshRedisSource(ctx, source, ra)
	case store.SourceTypeMongoDB:
		ma, ok := adapter.(mongodbadapter.MongoDBAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not mongodb", source.ID)
		}
		return r.refreshMongoDBSource(ctx, source, ma)
	case store.SourceTypeKafka:
		ka, ok := adapter.(kafkaadapter.KafkaAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not kafka", source.ID)
		}
		return r.refreshKafkaSource(ctx, source, ka)
	case store.SourceTypeElasticsearch:
		ea, ok := adapter.(elasticsearchadapter.ElasticsearchAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not elasticsearch", source.ID)
		}
		return r.refreshElasticsearchSource(ctx, source, ea)

	// Phase 6.8 — GitOps / CD / IaC sources.
	case store.SourceTypeArgoCd:
		aa, ok := adapter.(argocdadapter.ArgoCDAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not argocd", source.ID)
		}
		return r.refreshArgoCDSource(ctx, source, aa)
	case store.SourceTypeHelm:
		ha, ok := adapter.(helmadapter.HelmAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not helm", source.ID)
		}
		return r.refreshHelmSource(ctx, source, ha)
	case store.SourceTypeTerraform:
		ta, ok := adapter.(terraformadapter.TerraformAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not terraform", source.ID)
		}
		return r.refreshTerraformSource(ctx, source, ta)

	// Phase 6.9 — Networking & ingress sources.
	case store.SourceTypeNginx:
		na, ok := adapter.(nginxadapter.NginxAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not nginx-ingress", source.ID)
		}
		return r.refreshNginxSource(ctx, source, na)
	case store.SourceTypeEnvoy:
		ea, ok := adapter.(envoypadapter.EnvoyAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not envoy", source.ID)
		}
		return r.refreshEnvoySource(ctx, source, ea)

	// Phase 6.12 — Proprietary observability vendors.
	case store.SourceTypeDatadog:
		da, ok := adapter.(datadogadapter.DatadogAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not datadog", source.ID)
		}
		return r.refreshDatadogSource(ctx, source, da)
	case store.SourceTypeSplunk:
		sa, ok := adapter.(splunkadapter.SplunkAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not splunk", source.ID)
		}
		return r.refreshSplunkSource(ctx, source, sa)
	case store.SourceTypeDynatrace:
		da, ok := adapter.(dynatraceadapter.DynatraceAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not dynatrace", source.ID)
		}
		return r.refreshDynatraceSource(ctx, source, da)
	case store.SourceTypeNewRelic:
		na, ok := adapter.(newrelicadapter.NewRelicAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not newrelic", source.ID)
		}
		return r.refreshNewRelicSource(ctx, source, na)

	// Phase 6.13 — Artifact registry sources.
	case store.SourceTypeOCIRegistry, store.SourceTypeDockerHub:
		oa, ok := adapter.(ociadapter.OCIAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not oci_registry", source.ID)
		}
		if source.Type == store.SourceTypeDockerHub {
			return r.refreshDockerHubSource(ctx, source, oa)
		}
		return r.refreshOCISource(ctx, source, oa)
	case store.SourceTypeArtifactory:
		aa, ok := adapter.(artifactoryadapter.ArtifactoryAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not artifactory", source.ID)
		}
		return r.refreshArtifactorySource(ctx, source, aa)
	case store.SourceTypeECR:
		ea, ok := adapter.(ecradapter.ECRAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not ecr", source.ID)
		}
		return r.refreshECRSource(ctx, source, ea)

	default:
		r.logger.Debug("skipping unsupported source type", "source_id", source.ID, "type", source.Type)
		return nil
	}
}
