package coreagent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jaimegago/joe/internal/access"
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
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
)

// Refresher handles background refresh of the graph
type Refresher struct {
	services *core.Services
	llm      llm.LLMAdapter
	logger   *slog.Logger
	metrics  *observability.Metrics
	interval time.Duration
	cancel   context.CancelFunc
	doneCh   chan struct{}
	// accessor is the guarded seam (A001-COREGOV CC-05) through which the
	// refresh resolves each component's adapter under the agent:core principal
	// (carried on the refresh ctx by CC-02) at rbac.ActionRead. It is wired at
	// boot by SetAccessor with the SAME promote-aware policy engine CC-04 armed,
	// so a component whose type has auto_promote_reads ON is admitted and an
	// ungranted/unpromoted component is DENIED before its adapter — and thus its
	// credential — is resolved. When nil the refresh path FAILS CLOSED (CC-08):
	// resolveAdapter denies rather than reading the raw registry, and Start
	// refuses to boot. Production boot in cmd/joe/server.go always wires it.
	accessor *access.Accessor
}

// SetAccessor wires the guarded refresh accessor (A001-COREGOV CC-05). Called
// once at boot, before Start, so every refresh cycle resolves adapters through
// the access guard. A nil accessor is ignored (defensive — never deliberately
// disable the guard at runtime).
func (r *Refresher) SetAccessor(accessor *access.Accessor) {
	if accessor == nil {
		return
	}
	r.accessor = accessor
}

// defaultRefreshInterval is the fallback background-refresh cadence used when
// the operator has not configured refresh.interval_minutes (or set it to a
// non-positive value).
const defaultRefreshInterval = 5 * time.Minute

// NewRefresher creates a new background refresher. The refresh cadence is taken
// from the operator's refresh.interval_minutes config (services.Config) so the
// documented, boot-logged knob actually takes effect; a missing or non-positive
// value falls back to defaultRefreshInterval.
func NewRefresher(services *core.Services, llmAdapter llm.LLMAdapter, logger *slog.Logger, metrics *observability.Metrics) *Refresher {
	interval := defaultRefreshInterval
	if services != nil && services.Config != nil && services.Config.Refresh.Interval > 0 {
		interval = services.Config.Refresh.Interval
	}
	return &Refresher{
		services: services,
		llm:      llmAdapter,
		logger:   logger.With("component", "refresher"),
		metrics:  observability.EnsureMetrics(metrics),
		interval: interval,
		doneCh:   make(chan struct{}),
	}
}

// Start begins the background refresh loop.
//
// REFUSE TO START (A001-COREGOV CC-08, mirroring D-0027 / JOE-IDBOOT): the
// refresh resolves every component's adapter — and thus its credential —
// through the guarded access seam (r.accessor). If that seam was never wired,
// the only fail-closed behavior is to read nothing (resolveAdapter denies), so
// the background refresh would be inert and silently ungoverned. Rather than
// boot in that degraded state, Start returns a FATAL boot error here. This is a
// returned error propagated through the normal boot error path (agent.Start ->
// runServerWithDeps), NOT a panic in the refresh goroutine: cmd/joe/server.go
// turns it into a clean `return 1`, same fail-fast tier as the absent-identity
// refusal. Production boot in cmd/joe/server.go always calls SetRefreshAccessor
// before Start, so this never fires in a correctly-wired binary; it exists to
// make an unwired seam a loud startup failure instead of a quiet one.
func (r *Refresher) Start(ctx context.Context) error {
	if r.accessor == nil {
		return fmt.Errorf("refusing to start background refresh: refresh access seam not wired " +
			"(SetAccessor was not called before Start) — the autonomous refresh would run ungoverned; " +
			"this is a wiring bug in cmd/joe/server.go (A001-COREGOV CC-08)")
	}
	r.logger.Info("starting background refresh", "interval", r.interval)

	loopCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	go r.refreshLoop(loopCtx)
	return nil
}

// Stop gracefully stops the background refresh
func (r *Refresher) Stop(ctx context.Context) error {
	r.logger.Info("stopping background refresh")

	// Signal the loop to stop via context cancellation. A nil cancel means
	// Start was never called; the doneCh wait below then hits its timeout.
	if r.cancel != nil {
		r.cancel()
	}

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
		case <-ctx.Done():
			r.logger.Info("refresh loop stopping")
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
	// 1. Load connected components from store
	// 2. For each source, query current state
	// 3. Diff against existing graph
	// 4. Apply deterministic changes
	// 5. Queue ambiguous findings for clarification

	// Load components
	components, err := r.services.Store.Components.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to load components: %w", err)
	}

	r.logger.Debug("loaded components for refresh", "count", len(components))

	for _, source := range components {
		if err := r.refreshComponent(ctx, source); err != nil {
			r.logger.Error("failed to refresh component", "component_id", source.ID, "error", err)
			// Continue with other components even if one fails
		}
	}

	r.logger.Debug("completed refresh cycle")
	return nil
}

// resolveAdapter resolves a component's adapter through the guarded access seam
// (A001-COREGOV CC-05). It floors the refresh read: the agent:core principal
// carried on ctx (CC-02) is evaluated against the promote-aware policy engine
// (CC-04) at rbac.ActionRead BEFORE the adapter — and thus its credential — is
// resolved, so an ungranted/unpromoted component is denied with
// access.ErrPermissionDenied and no infrastructure handle is produced.
//
// A nil accessor FAILS CLOSED (A001-COREGOV CC-08): it returns
// access.ErrPermissionDenied without resolving any adapter, so absent the
// guarded seam the refresh reads nothing. This is the belt to the
// suspenders of the boot-time refuse-to-start check in agent.Start: production
// boot in cmd/joe/server.go always wires the accessor before Start, and if it
// somehow did not, Start refuses to come up — but should any direct-call path
// reach here without an accessor, it still denies rather than reopening the raw,
// ungoverned registry read this design closed. The denial flows into
// refreshComponent's skip-quietly path (errors.Is(err, ErrPermissionDenied)).
func (r *Refresher) resolveAdapter(ctx context.Context, source *store.Component) (adapters.Adapter, error) {
	if r.accessor == nil {
		return nil, fmt.Errorf("%w: refresh accessor not wired (fail-closed; CC-08)", access.ErrPermissionDenied)
	}
	principal := rbac.PrincipalFromContext(ctx)
	return r.accessor.ResolveAdapter(ctx, principal, source.ID, rbac.ActionRead)
}

// refreshComponent refreshes a single infrastructure component
func (r *Refresher) refreshComponent(ctx context.Context, source *store.Component) (err error) {
	start := time.Now()
	defer func() {
		lastError := ""
		if err != nil {
			lastError = err.Error()
		}
		if updateErr := r.services.Store.Components.UpdateSyncStatus(ctx, source.ID, time.Now(), lastError); updateErr != nil {
			r.logger.Warn("failed to update component sync status", "component_id", source.ID, "error", updateErr)
		}
		r.logger.Info("component refresh finished", "component_id", source.ID, "duration_ms", time.Since(start).Milliseconds(), "error", lastError)
	}()

	r.logger.Debug("refreshing component", "component_id", source.ID, "type", source.Type)

	adapter, err := r.resolveAdapter(ctx, source)
	if err != nil {
		// A permit DENIAL is the expected steady state for any component whose
		// type is not granted to agent:core and not promoted via
		// auto_promote_reads — every such component is denied on every tick.
		// Skip it quietly (debug) and let the caller proceed to the next
		// component; do NOT surface it as an error (which the loop logs loudly
		// and which would also stamp a misleading sync error on the component).
		if errors.Is(err, access.ErrPermissionDenied) {
			r.logger.Debug("refresh access denied for component, skipping",
				"component_id", source.ID, "type", source.Type)
			return nil
		}
		if errors.Is(err, adapters.ErrAdapterNotFound) || errors.Is(err, store.ErrComponentNotFound) {
			return fmt.Errorf("adapter not found for component %s", source.ID)
		}
		return fmt.Errorf("get adapter for component %s: %w", source.ID, err)
	}

	switch source.Type {
	case store.ComponentTypeKubernetes:
		k8sAdapter, ok := adapter.(k8s.KubernetesAdapter)
		if !ok {
			return fmt.Errorf("adapter for component %s is not kubernetes", source.ID)
		}
		return r.refreshK8sComponent(ctx, source, k8sAdapter)
	case store.ComponentTypeGit:
		gitAdapter, ok := adapter.(gitadapter.GitAdapter)
		if !ok {
			return fmt.Errorf("adapter for component %s is not git", source.ID)
		}
		return r.refreshGitComponent(ctx, source, gitAdapter)
	case store.ComponentTypeAWS:
		awsAdapter, ok := adapter.(awsadapter.AWSAdapter)
		if !ok {
			return fmt.Errorf("adapter for component %s is not aws", source.ID)
		}
		return r.refreshAWSComponent(ctx, source, awsAdapter)
	case store.ComponentTypeAzure:
		azureAdapter, ok := adapter.(azureadapter.AzureAdapter)
		if !ok {
			return fmt.Errorf("adapter for component %s is not azure", source.ID)
		}
		return r.refreshAzureComponent(ctx, source, azureAdapter)
	case store.ComponentTypePrometheus, store.ComponentTypeMimir:
		pa, ok := adapter.(prometheusadapter.PrometheusAdapter)
		if !ok {
			return fmt.Errorf("adapter for component %s is not prometheus", source.ID)
		}
		return r.refreshPrometheusComponent(ctx, source, pa)
	case store.ComponentTypeLoki:
		la, ok := adapter.(lokiadapter.LokiAdapter)
		if !ok {
			return fmt.Errorf("adapter for component %s is not loki", source.ID)
		}
		return r.refreshLokiComponent(ctx, source, la)
	case store.ComponentTypeTempo:
		ta, ok := adapter.(tempoadapter.TempoAdapter)
		if !ok {
			return fmt.Errorf("adapter for component %s is not tempo", source.ID)
		}
		return r.refreshTempoComponent(ctx, source, ta)
	case store.ComponentTypeJaeger:
		ja, ok := adapter.(jaegeradapter.JaegerAdapter)
		if !ok {
			return fmt.Errorf("adapter for component %s is not jaeger", source.ID)
		}
		return r.refreshJaegerComponent(ctx, source, ja)
	case store.ComponentTypeAlertmanager:
		aa, ok := adapter.(alertmanageradapter.AlertmanagerAdapter)
		if !ok {
			return fmt.Errorf("adapter for component %s is not alertmanager", source.ID)
		}
		return r.refreshAlertmanagerComponent(ctx, source, aa)
	case store.ComponentTypePagerDuty:
		pa, ok := adapter.(pagerdutyadapter.PagerDutyAdapter)
		if !ok {
			return fmt.Errorf("adapter for component %s is not pagerduty", source.ID)
		}
		return r.refreshPagerDutyComponent(ctx, source, pa)
	case store.ComponentTypeGrafana:
		ga, ok := adapter.(grafanaadapter.GrafanaAdapter)
		if !ok {
			return fmt.Errorf("adapter for component %s is not grafana", source.ID)
		}
		return r.refreshGrafanaComponent(ctx, source, ga)

	// Phase 6.7 — data store components.
	case store.ComponentTypePostgreSQL:
		pa, ok := adapter.(postgresadapter.PostgreSQLAdapter)
		if !ok {
			return fmt.Errorf("adapter for component %s is not postgresql", source.ID)
		}
		return r.refreshPostgreSQLComponent(ctx, source, pa)
	case store.ComponentTypeMySQL:
		ma, ok := adapter.(mysqladapter.MySQLAdapter)
		if !ok {
			return fmt.Errorf("adapter for component %s is not mysql", source.ID)
		}
		return r.refreshMySQLComponent(ctx, source, ma)
	case store.ComponentTypeRedis:
		ra, ok := adapter.(redisadapter.RedisAdapter)
		if !ok {
			return fmt.Errorf("adapter for component %s is not redis", source.ID)
		}
		return r.refreshRedisComponent(ctx, source, ra)
	case store.ComponentTypeMongoDB:
		ma, ok := adapter.(mongodbadapter.MongoDBAdapter)
		if !ok {
			return fmt.Errorf("adapter for component %s is not mongodb", source.ID)
		}
		return r.refreshMongoDBComponent(ctx, source, ma)
	case store.ComponentTypeKafka:
		ka, ok := adapter.(kafkaadapter.KafkaAdapter)
		if !ok {
			return fmt.Errorf("adapter for component %s is not kafka", source.ID)
		}
		return r.refreshKafkaComponent(ctx, source, ka)
	case store.ComponentTypeElasticsearch:
		ea, ok := adapter.(elasticsearchadapter.ElasticsearchAdapter)
		if !ok {
			return fmt.Errorf("adapter for component %s is not elasticsearch", source.ID)
		}
		return r.refreshElasticsearchComponent(ctx, source, ea)

	// Phase 6.8 — GitOps / CD / IaC components.
	case store.ComponentTypeArgoCd:
		aa, ok := adapter.(argocdadapter.ArgoCDAdapter)
		if !ok {
			return fmt.Errorf("adapter for component %s is not argocd", source.ID)
		}
		return r.refreshArgoCDComponent(ctx, source, aa)
	case store.ComponentTypeHelm:
		ha, ok := adapter.(helmadapter.HelmAdapter)
		if !ok {
			return fmt.Errorf("adapter for component %s is not helm", source.ID)
		}
		return r.refreshHelmComponent(ctx, source, ha)
	case store.ComponentTypeTerraform:
		ta, ok := adapter.(terraformadapter.TerraformAdapter)
		if !ok {
			return fmt.Errorf("adapter for component %s is not terraform", source.ID)
		}
		return r.refreshTerraformComponent(ctx, source, ta)

	// Phase 6.9 — Networking & ingress components.
	case store.ComponentTypeNginx:
		na, ok := adapter.(nginxadapter.NginxAdapter)
		if !ok {
			return fmt.Errorf("adapter for component %s is not nginx-ingress", source.ID)
		}
		return r.refreshNginxComponent(ctx, source, na)
	case store.ComponentTypeEnvoy:
		ea, ok := adapter.(envoypadapter.EnvoyAdapter)
		if !ok {
			return fmt.Errorf("adapter for component %s is not envoy", source.ID)
		}
		return r.refreshEnvoyComponent(ctx, source, ea)

	// Phase 6.12 — Proprietary observability vendors.
	case store.ComponentTypeDatadog:
		da, ok := adapter.(datadogadapter.DatadogAdapter)
		if !ok {
			return fmt.Errorf("adapter for component %s is not datadog", source.ID)
		}
		return r.refreshDatadogComponent(ctx, source, da)
	case store.ComponentTypeSplunk:
		sa, ok := adapter.(splunkadapter.SplunkAdapter)
		if !ok {
			return fmt.Errorf("adapter for component %s is not splunk", source.ID)
		}
		return r.refreshSplunkComponent(ctx, source, sa)
	case store.ComponentTypeDynatrace:
		da, ok := adapter.(dynatraceadapter.DynatraceAdapter)
		if !ok {
			return fmt.Errorf("adapter for component %s is not dynatrace", source.ID)
		}
		return r.refreshDynatraceComponent(ctx, source, da)
	case store.ComponentTypeNewRelic:
		na, ok := adapter.(newrelicadapter.NewRelicAdapter)
		if !ok {
			return fmt.Errorf("adapter for component %s is not newrelic", source.ID)
		}
		return r.refreshNewRelicComponent(ctx, source, na)

	// Phase 6.13 — Artifact registry components.
	case store.ComponentTypeOCIRegistry, store.ComponentTypeDockerHub:
		oa, ok := adapter.(ociadapter.OCIAdapter)
		if !ok {
			return fmt.Errorf("adapter for component %s is not oci_registry", source.ID)
		}
		if source.Type == store.ComponentTypeDockerHub {
			return r.refreshDockerHubComponent(ctx, source, oa)
		}
		return r.refreshOCIComponent(ctx, source, oa)
	case store.ComponentTypeArtifactory:
		aa, ok := adapter.(artifactoryadapter.ArtifactoryAdapter)
		if !ok {
			return fmt.Errorf("adapter for component %s is not artifactory", source.ID)
		}
		return r.refreshArtifactoryComponent(ctx, source, aa)
	case store.ComponentTypeECR:
		ea, ok := adapter.(ecradapter.ECRAdapter)
		if !ok {
			return fmt.Errorf("adapter for component %s is not ecr", source.ID)
		}
		return r.refreshECRComponent(ctx, source, ea)

	default:
		r.logger.Debug("skipping unsupported component type", "component_id", source.ID, "type", source.Type)
		return nil
	}
}
