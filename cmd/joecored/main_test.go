package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/store"
)

type fakeLLMAdapter struct{}

func (f *fakeLLMAdapter) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{}, nil
}

func (f *fakeLLMAdapter) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeLLMAdapter) Embed(ctx context.Context, text string) ([]float32, error) {
	return nil, errors.New("not implemented")
}

type fakeSourceAdapter struct{}

func (f *fakeSourceAdapter) Connect(_ context.Context, source store.Source) error {
	return nil
}

func (f *fakeSourceAdapter) Disconnect() error {
	return nil
}

func (f *fakeSourceAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true, Message: "fake"}
}

type fakeCoreAgent struct {
	startErr error
	stopErr  error
}

func (f *fakeCoreAgent) Start(ctx context.Context) error {
	return f.startErr
}

func (f *fakeCoreAgent) Stop(ctx context.Context) error {
	return f.stopErr
}

func (f *fakeCoreAgent) ProcessOnboarding(ctx context.Context, input string) error {
	return nil
}

func (f *fakeCoreAgent) TriggerRefresh(ctx context.Context) error {
	return nil
}

func (f *fakeCoreAgent) TriggerRefreshSource(ctx context.Context, sourceID string) error {
	return nil
}

type runCapture struct {
	server         *http.Server
	shutdownCalled bool
}

type fakeSourceRepo struct {
	byType map[string][]*store.Source
	list   []*store.Source
}

func (f *fakeSourceRepo) Create(ctx context.Context, source *store.Source) error {
	return nil
}

func (f *fakeSourceRepo) Get(ctx context.Context, id string) (*store.Source, error) {
	return nil, nil
}

func (f *fakeSourceRepo) List(ctx context.Context) ([]*store.Source, error) {
	return f.list, nil
}

func (f *fakeSourceRepo) ListByType(ctx context.Context, sourceType string) ([]*store.Source, error) {
	return f.byType[sourceType], nil
}

func (f *fakeSourceRepo) Update(ctx context.Context, source *store.Source) error {
	return nil
}

func (f *fakeSourceRepo) UpdateSyncStatus(ctx context.Context, id string, syncedAt time.Time, lastError string) error {
	return nil
}

func (f *fakeSourceRepo) Delete(ctx context.Context, id string) error {
	return nil
}

type fakeGraph struct {
	summary graph.GraphSummary
	err     error
}

func (f *fakeGraph) AddNode(ctx context.Context, node graph.Node) error { return nil }
func (f *fakeGraph) AddEdge(ctx context.Context, edge graph.Edge) error { return nil }
func (f *fakeGraph) GetNode(ctx context.Context, id string) (*graph.Node, error) {
	return nil, nil
}
func (f *fakeGraph) Query(ctx context.Context, query string) ([]graph.Node, error) { return nil, nil }
func (f *fakeGraph) Related(ctx context.Context, nodeID string, depth int) (*graph.Subgraph, error) {
	return &graph.Subgraph{}, nil
}
func (f *fakeGraph) Path(ctx context.Context, from, to string) ([]graph.Edge, error) { return nil, nil }
func (f *fakeGraph) DeleteNode(ctx context.Context, id string) error                 { return nil }
func (f *fakeGraph) DeleteEdge(ctx context.Context, from, to, relation string) error { return nil }
func (f *fakeGraph) Summary(ctx context.Context) (graph.GraphSummary, error) {
	return f.summary, f.err
}
func (f *fakeGraph) ListNodesBySource(ctx context.Context, sourceID string) ([]graph.Node, error) {
	return nil, nil
}
func (f *fakeGraph) ListEdgesForNodes(ctx context.Context, nodeIDs []string) ([]graph.Edge, error) {
	return nil, nil
}

func (f *fakeGraph) ListAll(ctx context.Context) (*graph.Subgraph, error) {
	return &graph.Subgraph{}, nil
}

func baseConfig() *config.Config {
	return &config.Config{
		LLM: config.LLMConfig{
			Current: "test",
			Available: map[string]config.ModelConfig{
				"test": {Provider: "claude", Model: "test"},
			},
		},
		Server: config.ServerConfig{
			Address: "127.0.0.1:0",
		},
		Refresh: config.RefreshConfig{
			IntervalMinutes: 1,
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
	}
}

func newTestDeps(t *testing.T, cfg *config.Config, capture *runCapture) runDeps {
	deps := defaultRunDeps()
	deps.loadConfig = func(path string) (*config.Config, error) {
		return cfg, nil
	}
	deps.defaultOTelConfig = func() observability.Config {
		return observability.Config{
			Enabled:         false,
			TracesEnabled:   false,
			TracesExporter:  "none",
			MetricsEnabled:  false,
			MetricsExporter: "none",
		}
	}
	deps.setupOTel = func(ctx context.Context, cfg observability.Config) (func(context.Context) error, error) {
		return func(context.Context) error { return nil }, nil
	}
	deps.joeDirPath = func() (string, error) {
		return t.TempDir(), nil
	}
	deps.mkdirAll = func(path string, perm os.FileMode) error {
		return nil
	}
	deps.databasePath = func() (string, error) {
		return "test.db", nil
	}
	deps.newStore = func(path string, metrics *observability.Metrics) (*store.Store, error) {
		return &store.Store{}, nil
	}
	deps.migrateStore = func(store *store.Store) error {
		return nil
	}
	deps.closeStore = func(store *store.Store) error {
		return nil
	}
	deps.newAdapterRegistry = adapters.NewRegistry
	deps.connectSources = func(ctx context.Context, store *store.Store, registry *adapters.Registry) {}
	deps.newServices = func(cfg *config.Config, store *store.Store, db *sql.DB, registry *adapters.Registry, metrics *observability.Metrics) *core.Services {
		return &core.Services{
			Config:   cfg,
			Store:    store,
			Adapters: registry,
			Metrics:  metrics,
		}
	}
	deps.registerBusinessMetric = func(services *core.Services) error {
		return nil
	}
	deps.newLLMAdapter = func(ctx context.Context, mc config.ModelConfig) (llm.LLMAdapter, error) {
		return &fakeLLMAdapter{}, nil
	}
	deps.newCoreAgent = func(services *core.Services, llmAdapter llm.LLMAdapter, metrics *observability.Metrics) coreAgentRunner {
		return &fakeCoreAgent{}
	}
	deps.startServer = func(server *http.Server, certFile, keyFile string) <-chan error {
		capture.server = server
		return make(chan error, 1)
	}
	deps.shutdownServer = func(ctx context.Context, server *http.Server) error {
		capture.shutdownCalled = true
		return nil
	}
	deps.startMetricsServer = func(server *http.Server) error {
		return nil
	}
	deps.shutdownMetricsServer = func(ctx context.Context, server *http.Server) error {
		return nil
	}
	deps.waitForShutdown = func(ctx context.Context) <-chan struct{} {
		done := make(chan struct{})
		close(done)
		return done
	}
	return deps
}

func TestRun_SuccessRegistersRoutes(t *testing.T) {
	capture := &runCapture{}
	cfg := baseConfig()
	deps := newTestDeps(t, cfg, capture)

	exitCode := runWithDeps(context.Background(), deps)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if capture.server == nil {
		t.Fatalf("expected server to be created")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	resp := httptest.NewRecorder()
	capture.server.Handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	if !capture.shutdownCalled {
		t.Fatalf("expected shutdown to be called")
	}
}

func TestRun_AuthMiddleware(t *testing.T) {
	capture := &runCapture{}
	cfg := baseConfig()
	cfg.Server.APIKey = "token"
	deps := newTestDeps(t, cfg, capture)

	exitCode := runWithDeps(context.Background(), deps)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	resp := httptest.NewRecorder()
	capture.server.Handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", resp.Code)
	}
}

func TestRun_ConfigLoadError(t *testing.T) {
	deps := defaultRunDeps()
	deps.loadConfig = func(path string) (*config.Config, error) {
		return nil, errors.New("config failed")
	}

	exitCode := runWithDeps(context.Background(), deps)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
}

func TestRun_JoeDirError(t *testing.T) {
	capture := &runCapture{}
	cfg := baseConfig()
	deps := newTestDeps(t, cfg, capture)
	deps.joeDirPath = func() (string, error) {
		return "", errors.New("no home")
	}

	exitCode := runWithDeps(context.Background(), deps)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
}

func TestRun_StoreOpenError(t *testing.T) {
	capture := &runCapture{}
	cfg := baseConfig()
	deps := newTestDeps(t, cfg, capture)
	deps.newStore = func(path string, metrics *observability.Metrics) (*store.Store, error) {
		return nil, errors.New("store failed")
	}

	exitCode := runWithDeps(context.Background(), deps)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
}

func TestRun_MigrateError(t *testing.T) {
	capture := &runCapture{}
	cfg := baseConfig()
	deps := newTestDeps(t, cfg, capture)
	deps.migrateStore = func(store *store.Store) error {
		return errors.New("migrate failed")
	}

	exitCode := runWithDeps(context.Background(), deps)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
}

func TestRun_CurrentModelError(t *testing.T) {
	capture := &runCapture{}
	cfg := baseConfig()
	cfg.LLM.Current = "missing"
	deps := newTestDeps(t, cfg, capture)

	exitCode := runWithDeps(context.Background(), deps)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
}

func TestRun_NewLLMAdapterError(t *testing.T) {
	capture := &runCapture{}
	cfg := baseConfig()
	deps := newTestDeps(t, cfg, capture)
	deps.newLLMAdapter = func(ctx context.Context, mc config.ModelConfig) (llm.LLMAdapter, error) {
		return nil, errors.New("llm failed")
	}

	exitCode := runWithDeps(context.Background(), deps)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
}

func TestRun_CoreAgentStartError(t *testing.T) {
	capture := &runCapture{}
	cfg := baseConfig()
	deps := newTestDeps(t, cfg, capture)
	deps.newCoreAgent = func(services *core.Services, llmAdapter llm.LLMAdapter, metrics *observability.Metrics) coreAgentRunner {
		return &fakeCoreAgent{startErr: errors.New("start failed")}
	}

	exitCode := runWithDeps(context.Background(), deps)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
}

func TestRun_PortBindingError(t *testing.T) {
	capture := &runCapture{}
	cfg := baseConfig()
	deps := newTestDeps(t, cfg, capture)
	deps.startServer = func(server *http.Server, certFile, keyFile string) <-chan error {
		errCh := make(chan error, 1)
		errCh <- errors.New("bind failed")
		return errCh
	}
	deps.waitForShutdown = func(ctx context.Context) <-chan struct{} {
		return make(chan struct{})
	}

	exitCode := runWithDeps(context.Background(), deps)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
}

func TestRun_OTelFailureDoesNotCrash(t *testing.T) {
	capture := &runCapture{}
	cfg := baseConfig()
	deps := newTestDeps(t, cfg, capture)
	deps.setupOTel = func(ctx context.Context, cfg observability.Config) (func(context.Context) error, error) {
		return nil, errors.New("otel down")
	}

	exitCode := runWithDeps(context.Background(), deps)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
}

func TestRun_MetricsServerLifecycle(t *testing.T) {
	observability.ResetMetricsHandler()
	t.Cleanup(observability.ResetMetricsHandler)

	capture := &runCapture{}
	cfg := baseConfig()
	deps := newTestDeps(t, cfg, capture)
	deps.defaultOTelConfig = func() observability.Config {
		return observability.Config{
			Enabled:         true,
			TracesEnabled:   false,
			TracesExporter:  "none",
			MetricsEnabled:  true,
			MetricsExporter: "prometheus",
			MetricsPort:     0,
		}
	}
	deps.setupOTel = observability.Setup

	metricsStarted := false
	metricsStopped := false
	deps.startMetricsServer = func(server *http.Server) error {
		metricsStarted = true
		return nil
	}
	deps.shutdownMetricsServer = func(ctx context.Context, server *http.Server) error {
		metricsStopped = true
		return nil
	}

	exitCode := runWithDeps(context.Background(), deps)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if !metricsStarted {
		t.Fatalf("expected metrics server to start")
	}
	if !metricsStopped {
		t.Fatalf("expected metrics server to stop")
	}
}

func TestRun_ShutdownErrorDoesNotFail(t *testing.T) {
	capture := &runCapture{}
	cfg := baseConfig()
	deps := newTestDeps(t, cfg, capture)
	deps.shutdownServer = func(ctx context.Context, server *http.Server) error {
		capture.shutdownCalled = true
		return errors.New("shutdown failed")
	}

	exitCode := runWithDeps(context.Background(), deps)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if !capture.shutdownCalled {
		t.Fatalf("expected shutdown to be called")
	}
}

func TestRun_CoreAgentStopErrorDoesNotFail(t *testing.T) {
	capture := &runCapture{}
	cfg := baseConfig()
	deps := newTestDeps(t, cfg, capture)
	deps.newCoreAgent = func(services *core.Services, llmAdapter llm.LLMAdapter, metrics *observability.Metrics) coreAgentRunner {
		return &fakeCoreAgent{stopErr: errors.New("stop failed")}
	}

	exitCode := runWithDeps(context.Background(), deps)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
}

func TestRun_RegisterBusinessMetricsErrorDoesNotFail(t *testing.T) {
	capture := &runCapture{}
	cfg := baseConfig()
	deps := newTestDeps(t, cfg, capture)
	deps.registerBusinessMetric = func(services *core.Services) error {
		return errors.New("metrics failed")
	}

	exitCode := runWithDeps(context.Background(), deps)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
}

func TestConnectSourcesDefault_InvalidConfigs(t *testing.T) {
	repo := &fakeSourceRepo{
		byType: map[string][]*store.Source{
			store.SourceTypeKubernetes: {
				{ID: "k8s-1", Type: store.SourceTypeKubernetes, Name: "k8s", Config: []byte("{")},
			},
			store.SourceTypeGit: {
				{ID: "git-1", Type: store.SourceTypeGit, Name: "git", Config: []byte(`{"auth_type":"none"}`)},
			},
			store.SourceTypeAWS: {
				{ID: "aws-1", Type: store.SourceTypeAWS, Name: "aws", Config: []byte(`{"profile":"default"}`)},
			},
			store.SourceTypeAzure: {
				{ID: "az-1", Type: store.SourceTypeAzure, Name: "az", Config: []byte(`{"subscription_id":"sub"}`)},
			},
		},
	}
	storeInst := &store.Store{Sources: repo}
	registry := adapters.NewRegistry()

	connectSourcesDefault(context.Background(), storeInst, registry)

	ids := registry.List()
	if len(ids) != 1 || ids[0] != "az-1" {
		t.Fatalf("expected only azure adapter registered, got %v", ids)
	}
}

func TestRegisterBusinessMetricsDefault(t *testing.T) {
	repo := &fakeSourceRepo{
		list: []*store.Source{
			{ID: "s1", Type: store.SourceTypeGit},
			{ID: "s2", Type: store.SourceTypeGit},
			{ID: "s3", Type: store.SourceTypeKubernetes},
		},
	}
	storeInst := &store.Store{Sources: repo}
	graphStore := &fakeGraph{
		summary: graph.GraphSummary{NodeCount: 2, EdgeCount: 3, NodesByType: map[string]int{"service": 2}},
	}
	registry := adapters.NewRegistry()
	registry.Register("id", &fakeSourceAdapter{})
	services := &core.Services{
		Store:    storeInst,
		Graph:    graphStore,
		Adapters: registry,
		Metrics:  observability.NewMetrics(),
	}

	if err := registerBusinessMetricsDefault(services); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestDefaultWaitForShutdown_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := defaultWaitForShutdown(ctx)
	cancel()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("expected shutdown channel to close")
	}
}

func TestDefaultStartServer(t *testing.T) {
	server := &http.Server{Addr: "127.0.0.1:0", Handler: http.NewServeMux()}
	errCh := defaultStartServer(server, "", "")
	_ = server.Shutdown(context.Background())

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("unexpected server error: %v", err)
		}
	default:
	}
}

func TestDefaultStartMetricsServer(t *testing.T) {
	server := &http.Server{Addr: "127.0.0.1:0", Handler: http.NewServeMux()}
	if err := defaultStartMetricsServer(server); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	_ = server.Shutdown(context.Background())
}

func TestRun_DebugModeLogging(t *testing.T) {
	capture := &runCapture{}
	cfg := baseConfig()
	cfg.Logging.Level = "debug"
	deps := newTestDeps(t, cfg, capture)

	if ret := runWithDeps(context.Background(), deps); ret != 0 {
		t.Errorf("expected return 0, got %d", ret)
	}
}

func TestRun_APIKeyConfigured(t *testing.T) {
	capture := &runCapture{}
	cfg := baseConfig()
	cfg.Server.APIKey = "test-key"
	deps := newTestDeps(t, cfg, capture)

	if ret := runWithDeps(context.Background(), deps); ret != 0 {
		t.Errorf("expected return 0, got %d", ret)
	}
}

func TestRun_MetricsDisabledPath(t *testing.T) {
	capture := &runCapture{}
	cfg := baseConfig()
	deps := newTestDeps(t, cfg, capture)
	deps.defaultOTelConfig = func() observability.Config {
		return observability.Config{
			MetricsEnabled:  false,
			MetricsExporter: "prometheus",
		}
	}

	if ret := runWithDeps(context.Background(), deps); ret != 0 {
		t.Errorf("expected return 0, got %d", ret)
	}
}

func TestRun_MetricsServerStartupError(t *testing.T) {
	capture := &runCapture{}
	cfg := baseConfig()
	deps := newTestDeps(t, cfg, capture)
	deps.defaultOTelConfig = func() observability.Config {
		return observability.Config{
			MetricsEnabled:  true,
			MetricsExporter: "prometheus",
			MetricsPort:     8888,
		}
	}
	deps.startMetricsServer = func(server *http.Server) error {
		return errors.New("metrics startup failed")
	}

	if ret := runWithDeps(context.Background(), deps); ret != 0 {
		t.Errorf("expected return 0, got %d", ret)
	}
}

func TestRun_DatabasePathError(t *testing.T) {
	capture := &runCapture{}
	cfg := baseConfig()
	deps := newTestDeps(t, cfg, capture)
	deps.databasePath = func() (string, error) {
		return "", errors.New("database path failed")
	}

	if ret := runWithDeps(context.Background(), deps); ret != 1 {
		t.Errorf("expected return 1, got %d", ret)
	}
}

func TestRun_MkdirError(t *testing.T) {
	capture := &runCapture{}
	cfg := baseConfig()
	deps := newTestDeps(t, cfg, capture)
	deps.mkdirAll = func(path string, perm os.FileMode) error {
		return errors.New("mkdir failed")
	}

	if ret := runWithDeps(context.Background(), deps); ret != 1 {
		t.Errorf("expected return 1, got %d", ret)
	}
}

func TestRun_WithRateLimiting(t *testing.T) {
	capture := &runCapture{}
	cfg := baseConfig()
	cfg.Server.RateLimitRPS = 10.0
	cfg.Server.RateLimitBurst = 20
	deps := newTestDeps(t, cfg, capture)

	if ret := runWithDeps(context.Background(), deps); ret != 0 {
		t.Errorf("expected return 0, got %d", ret)
	}
}

func TestRun_WithTLSConfigured(t *testing.T) {
	capture := &runCapture{}
	cfg := baseConfig()
	cfg.Server.TLSCertFile = "/tmp/cert.pem"
	cfg.Server.TLSKeyFile = "/tmp/key.pem"
	deps := newTestDeps(t, cfg, capture)

	if ret := runWithDeps(context.Background(), deps); ret != 0 {
		t.Errorf("expected return 0, got %d", ret)
	}
}

func TestDefaultRunDeps_MigrateCloseClosures(t *testing.T) {
	deps := defaultRunDeps()

	sqlStore, err := store.New(":memory:", nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer sqlStore.Close()

	if err := deps.migrateStore(sqlStore); err != nil {
		t.Errorf("migrateStore error: %v", err)
	}
	if err := deps.closeStore(sqlStore); err != nil {
		t.Errorf("closeStore error: %v", err)
	}
}

func TestDefaultRunDeps_ShutdownClosures(t *testing.T) {
	deps := defaultRunDeps()

	srv := &http.Server{Addr: "127.0.0.1:0", Handler: http.NewServeMux()}
	if err := deps.shutdownServer(context.Background(), srv); err != nil {
		t.Errorf("shutdownServer error: %v", err)
	}

	srv2 := &http.Server{Addr: "127.0.0.1:0", Handler: http.NewServeMux()}
	if err := deps.shutdownMetricsServer(context.Background(), srv2); err != nil {
		t.Errorf("shutdownMetricsServer error: %v", err)
	}
}

func TestDefaultRunDeps_NewCoreAgent(t *testing.T) {
	deps := defaultRunDeps()

	sqlStore, err := store.New(":memory:", nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer sqlStore.Close()
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	reg := adapters.NewRegistry()
	cfg := &config.Config{}
	services := core.New(cfg, sqlStore, sqlStore.DB(), reg, nil)

	agent := deps.newCoreAgent(services, &fakeLLMAdapter{}, observability.NewMetrics())
	if agent == nil {
		t.Error("newCoreAgent returned nil")
	}
}

func TestDefaultRunDeps_RegisterBusinessMetric(t *testing.T) {
	deps := defaultRunDeps()

	sqlStore, err := store.New(":memory:", nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer sqlStore.Close()
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	reg := adapters.NewRegistry()
	cfg := &config.Config{}
	services := core.New(cfg, sqlStore, sqlStore.DB(), reg, observability.NewMetrics())

	if err := deps.registerBusinessMetric(services); err != nil {
		t.Errorf("registerBusinessMetric error: %v", err)
	}
}

func TestDefaultWaitForShutdown_Signal(t *testing.T) {
	ctx := context.Background()
	done := defaultWaitForShutdown(ctx)

	// Send SIGTERM to ourselves; signal.Notify intercepts it before the default handler.
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	select {
	case <-done:
		// signal was received and done was closed
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected done channel to close after SIGTERM")
	}
}

// errSourceRepo is a fakeSourceRepo variant that returns errors from ListByType.
type errSourceRepo struct{}

func (e *errSourceRepo) Create(_ context.Context, _ *store.Source) error { return nil }
func (e *errSourceRepo) Get(_ context.Context, _ string) (*store.Source, error) {
	return nil, nil
}
func (e *errSourceRepo) List(_ context.Context) ([]*store.Source, error) { return nil, nil }
func (e *errSourceRepo) ListByType(_ context.Context, _ string) ([]*store.Source, error) {
	return nil, errors.New("db error")
}
func (e *errSourceRepo) Update(_ context.Context, _ *store.Source) error { return nil }
func (e *errSourceRepo) UpdateSyncStatus(_ context.Context, _ string, _ time.Time, _ string) error {
	return nil
}
func (e *errSourceRepo) Delete(_ context.Context, _ string) error { return nil }

func TestConnectSourcesDefault_ListErrors(t *testing.T) {
	storeInst := &store.Store{Sources: &errSourceRepo{}}
	registry := adapters.NewRegistry()

	// Should not panic; list errors are logged and skipped.
	connectSourcesDefault(context.Background(), storeInst, registry)

	if len(registry.List()) != 0 {
		t.Errorf("expected no adapters registered on list error, got %d", len(registry.List()))
	}
}
