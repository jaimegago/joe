package api_test

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/auth"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
)

// --- Behavioural acceptance ---

// TestPhaseE_LoopEnforcesAgainstRealCallerPrincipal is THE Phase E behavioural
// acceptance: a loop run initiated by a principal WITH a grant on the target
// zone succeeds at the tool call; the SAME run initiated by a principal
// WITHOUT that grant is denied at the tool call. This proves the loop now
// enforces against the real caller principal, not a server identity
// re-authenticated via a loopback HTTP self-call.
//
// On pre-Phase-E code this assertion is impossible to satisfy: the loop's
// tools reached infra by building a loopback *client.Client that
// re-authenticated as svc:server, discarding whatever principal the request
// context carried. So either every caller's run would succeed (if svc:server
// was granted) or every run would fail (if it was not) — the caller
// principal could never make a difference. After Phase E the loop's tool
// registry is wired to an in-process accessor-backed client; the principal in
// ctx is read at each tool call and passed to the accessor with no fallback.
//
// The driver below seeds a deterministic mock LLM that emits one k8s_get tool
// call for "s-prod" then a final answer. The principal is injected into the
// request context via rbac.WithPrincipal, mirroring how auth.EdgeAuth seeds
// it on the real request path. EnforcementMiddleware is intentionally omitted
// (after Phase E it is a pass-through anyway) so the accessor is the sole
// RBAC gate — proving the loop's enforcement runs in-process via the accessor.
func TestPhaseE_LoopEnforcesAgainstRealCallerPrincipal(t *testing.T) {
	sqlStore := mustRegStore(t)
	ctx := context.Background()

	mustCreateComponent(t, sqlStore, "s-prod")
	rbacRepo := rbac.NewRepository(sqlStore.DB(), sqlStore.Driver())
	if err := rbacRepo.UpsertAssignment(ctx, rbac.ComponentZoneAssignment{
		ComponentID: "s-prod", ZoneID: "prod-readonly", AssignedBy: "test",
	}, "test"); err != nil {
		t.Fatalf("assign s-prod: %v", err)
	}
	// alice has the grant; mallory does NOT. NOTE: svc:server is NOT granted —
	// proving that the loop no longer needs the server account to reach infra
	// (it reaches it as the caller).
	if _, err := rbacRepo.CreatePolicy(ctx, rbac.Policy{Principal: "user:alice", ZoneID: "prod-readonly"}, "test"); err != nil {
		t.Fatalf("grant user:alice: %v", err)
	}

	registry := adapters.NewRegistry()
	registry.Register("s-prod", apiFakeK8s{})

	// Service-account configured ⇒ accessor's policy engine is non-nil and
	// enforces — mirrors cmd/joe/server.go's newPolicyEngine condition.
	services := buildPhaseEServices(t, sqlStore, rbacRepo, registry,
		[]config.ServiceAccount{{Name: "any", Key: "any"}})

	run := func(principal rbac.Principal) (allowed bool, raw any) {
		services.LLM = mockToolThenFinalLLM()
		srv := api.New(services)
		mux := http.NewServeMux()
		srv.RegisterRoutes(mux)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(`{"message":"poke"}`))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(rbac.WithPrincipal(req.Context(), principal))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("task handler returned %d, want 200 (errors should surface in the response body): body=%s", w.Code, w.Body.String())
		}
		var resp struct {
			Steps []struct {
				ToolResults []struct {
					Name   string `json:"name"`
					Result any    `json:"result"`
					Error  string `json:"error"`
				} `json:"tool_results"`
			} `json:"steps"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v: body=%s", err, w.Body.String())
		}
		for _, s := range resp.Steps {
			for _, tr := range s.ToolResults {
				if tr.Name != "k8s_get" {
					continue
				}
				if tr.Error != "" {
					return false, tr.Error
				}
				return true, tr.Result
			}
		}
		t.Fatalf("no k8s_get tool result observed: body=%s", w.Body.String())
		return false, nil
	}

	ok, result := run("user:alice")
	if !ok {
		t.Errorf("alice (granted) tool call should succeed; got error: %v", result)
	}
	ok, result = run("user:mallory")
	if ok {
		t.Errorf("mallory (ungranted) tool call should be denied; got success: %v", result)
	}
	if msg, _ := result.(string); !strings.Contains(strings.ToLower(msg), "access denied") {
		t.Errorf("mallory denial should surface an access-denied error to the loop; got: %v", result)
	}
}

// TestPhaseE_LoopGraphAccessIsInProcess proves the graph tool reaches
// services.Graph in-process via the accessor — no HTTP hop to joe's own
// /api/v1/graph endpoint. We seed the graph store directly, run the loop with
// NO HTTP server started, and assert the seeded node appears in the loop's
// output. A loopback HTTP client would fail with a connection error; the
// in-process accessor reaches the graph store directly.
func TestPhaseE_LoopGraphAccessIsInProcess(t *testing.T) {
	sqlStore := mustRegStore(t)
	ctx := context.Background()

	services := buildPhaseEServices(t, sqlStore, nil, adapters.NewRegistry(), nil)
	if err := services.Graph.AddNode(ctx, graph.Node{ID: "svc-x", Type: "service"}); err != nil { //nolint:staticcheck // test setup is not a principal-gated path
		t.Fatalf("seed graph: %v", err)
	}

	services.LLM = mockGraphQueryThenFinalLLM()
	srv := api.New(services)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(`{"message":"poke"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("graph loop run returned %d: body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "svc-x") {
		t.Errorf("expected in-process graph access to surface seeded node 'svc-x'; got body=%s", body)
	}
}

// --- Equivalence test (gates the EnforcementMiddleware demotion) ---

// TestPhaseE_AccessorAloneMatchesPriorOutcomes is the EQUIVALENCE test that
// gated the EnforcementMiddleware demotion (Phase E req 6). It asserts that
// the accessor ALONE on the HTTP path produces the SAME allow/deny/unauth
// (200/403/401) outcomes the prior middleware-plus-accessor chain produced.
//
// Two chains are constructed over the same routes + RBAC state:
//
//   - "demoted-middleware-plus-accessor": EdgeAuth + the demoted
//     EnforcementMiddleware (a pass-through in Phase E) + accessor. This is
//     the production chain as it stands AFTER the demotion.
//   - "accessor-alone": EdgeAuth + accessor (no middleware) — the strict
//     interpretation of "accessor is the sole authoritative gate".
//
// Both chains must return identical status codes across the allow/deny/unauth
// matrix. If a future change reintroduces middleware enforcement (or breaks
// the accessor's path), this divergence is what fails the test. The legacy
// pre-Phase-E "middleware + accessor with IsAllowed" arrangement no longer
// exists in the codebase, so the test compares "demoted middleware +
// accessor" against "accessor alone" — both should agree and both should
// match the Phase A regression outcomes (`TestPhaseA_HTTPRBACOutcomesPreserved`).
func TestPhaseE_AccessorAloneMatchesPriorOutcomes(t *testing.T) {
	sqlStore := mustRegStore(t)
	ctx := context.Background()

	mustCreateComponent(t, sqlStore, "s-allow")
	mustCreateComponent(t, sqlStore, "s-deny")
	repo := rbac.NewRepository(sqlStore.DB(), sqlStore.Driver())
	if err := repo.UpsertAssignment(ctx, rbac.ComponentZoneAssignment{ComponentID: "s-allow", ZoneID: "prod-readonly", AssignedBy: "test"}, "test"); err != nil {
		t.Fatalf("assign s-allow: %v", err)
	}
	if err := repo.UpsertAssignment(ctx, rbac.ComponentZoneAssignment{ComponentID: "s-deny", ZoneID: "prod-write", AssignedBy: "test"}, "test"); err != nil {
		t.Fatalf("assign s-deny: %v", err)
	}
	if _, err := repo.CreatePolicy(ctx, rbac.Policy{Principal: "svc:operator", ZoneID: "prod-readonly"}, "test"); err != nil {
		t.Fatalf("grant svc:operator: %v", err)
	}

	registry := adapters.NewRegistry()
	registry.Register("s-allow", apiFakeK8s{})
	registry.Register("s-deny", apiFakeK8s{})

	const apiKey = "secret"
	services := &core.Services{
		Config:   &config.Config{Server: config.ServerConfig{ServiceAccounts: []config.ServiceAccount{{Name: "operator", Key: apiKey}}}},
		Store:    sqlStore,
		RBAC:     repo,
		Adapters: registry,
	}
	srv := api.New(services)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	// Test-only protected route reaching the guarded accessor (never in the
	// production route table); the auth/RBAC chain is what's under test here,
	// not any product route.
	srv.RegisterProbeRouteForTest(mux)

	engine := rbac.NewPolicyEngine(repo)
	resolver := mustResolver(t, config.ServiceAccount{Name: "operator", Key: apiKey})

	chains := map[string]http.Handler{
		"demoted-middleware-plus-accessor": api.Chain(mux,
			auth.EdgeAuth(auth.EdgeConfig{ServiceAccounts: resolver}),
			rbac.EnforcementMiddleware(engine),
		),
		"accessor-alone": api.Chain(mux,
			auth.EdgeAuth(auth.EdgeConfig{ServiceAccounts: resolver}),
		),
	}

	cases := []struct {
		name  string
		path  string
		token string
		want  int
	}{
		{"granted read", "/api/v1/probe/s-allow/read", "Bearer " + apiKey, http.StatusOK},
		{"ungranted zone", "/api/v1/probe/s-deny/read", "Bearer " + apiKey, http.StatusForbidden},
		{"missing token", "/api/v1/probe/s-allow/read", "", http.StatusUnauthorized},
		{"invalid token", "/api/v1/probe/s-allow/read", "Bearer not-a-real-key", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		for chainName, handler := range chains {
			t.Run(tc.name+"/"+chainName, func(t *testing.T) {
				r := httptest.NewRequest("GET", tc.path, nil)
				if tc.token != "" {
					r.Header.Set("Authorization", tc.token)
				}
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				if w.Code != tc.want {
					t.Errorf("path %s on chain %s: got %d, want %d", tc.path, chainName, w.Code, tc.want)
				}
			})
		}
	}
}

// --- Static guards (Phase E proper) ---

// TestPhaseE_NoLoopbackClientForInProcessToolExecution asserts that no
// loopback *client.Client is constructed for the in-process loop's tool
// registry. tasks.go (and tasks_stream.go) used to build a
// `client.New(loopbackURL, ...)` per task; after Phase E they wire the
// in-process accessor-backed client (s.inproc). This static guard prevents a
// regression that silently reintroduces the loopback hop.
func TestPhaseE_NoLoopbackClientForInProcessToolExecution(t *testing.T) {
	repoRoot := findRepoRoot(t)
	apiDir := filepath.Join(repoRoot, "internal", "api")

	suspectFiles := []string{"tasks.go", "tasks_stream.go", "inproc_client.go"}

	for _, name := range suspectFiles {
		path := filepath.Join(apiDir, name)
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			if pkg.Name == "client" && sel.Sel.Name == "New" {
				line := fset.Position(call.Pos()).Line
				t.Errorf("%s:%d constructs client.New(...) — Phase E removed the in-process loopback; "+
					"the loop's tool registry must use the in-process accessor-backed client", name, line)
			}
			return true
		})
	}
}

// --- Test helpers ---

// buildPhaseEServices constructs a *core.Services backed by a real graph store
// (via core.New so services.Graph is populated) but wired to the supplied
// adapter registry and RBAC repo. The caller provides the ServiceAccounts that
// gate the policy engine's enabled-state in api.New.
func buildPhaseEServices(t *testing.T, sqlStore *store.Store, rbacRepo rbac.Repository, registry *adapters.Registry, accounts []config.ServiceAccount) *core.Services {
	t.Helper()
	cfg := &config.Config{Server: config.ServerConfig{ServiceAccounts: accounts}}
	metrics := observability.NewMetrics()
	services := core.New(cfg, sqlStore, sqlStore.DB(), sqlStore.Driver(), registry, metrics)
	services.RBAC = rbacRepo
	return services
}

// --- Deterministic LLM mocks for the loop ---

func mockToolThenFinalLLM() llm.LLMAdapter {
	return &seqLLM{turns: []*llm.ChatResponse{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "k8s_get", Args: map[string]any{
			"component_id": "s-prod", "resource": "pods",
		}}}},
		{Content: "done"},
	}}
}

func mockGraphQueryThenFinalLLM() llm.LLMAdapter {
	return &seqLLM{turns: []*llm.ChatResponse{
		{ToolCalls: []llm.ToolCall{{ID: "g1", Name: "graph_query", Args: map[string]any{
			"query": "type:service",
		}}}},
		{Content: "done"},
	}}
}

type seqLLM struct {
	turns []*llm.ChatResponse
	n     int
}

func (s *seqLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	if s.n < len(s.turns) {
		r := s.turns[s.n]
		s.n++
		return r, nil
	}
	return &llm.ChatResponse{Content: "done"}, nil
}

func (s *seqLLM) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{0}, nil
}
