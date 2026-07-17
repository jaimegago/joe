package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/jaimegago/joe/internal/access"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/buildinfo"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/sessionauthz"
)

// Server handles HTTP API requests for joecored
type Server struct {
	services *core.Services
	// accessor is the single guarded seam to infrastructure adapters and
	// the graph store (docs/reference/joe-identity-design.md §2.5, Phase A). HTTP
	// handlers reach adapters/graph ONLY through this; they never resolve
	// services.Adapters or call services.Graph directly.
	accessor *access.Accessor
	// inproc is the in-process accessor-backed client the loop's tool
	// registry uses (Identity Phase E, design §3). It implements every
	// coretools.CoreToolsClient method by reading the caller principal from
	// ctx and dispatching to the accessor (for adapter/graph operations) or
	// directly to in-process services (for list_components, which does not
	// touch an adapter). It replaces the loopback *client.Client; no HTTP
	// self-call remains for in-process tool execution.
	inproc *inProcessCoreClient
	// sessionAuthz is the PER-USER session-authorization seam instance (§12.7
	// seam / §12.8 two-instance defense-in-depth, B003+B005), separate from the
	// component-RBAC accessor above. It is built with an always-false admin
	// checker (newPerUserSessionAuthz) so an admin can never resolve to the admin
	// relationship on a per-user /api/v1/sessions route. Every per-user
	// owner-mutate handler authorizes through (*Server).sessionAccess, which
	// delegates here; the bypass guard pins the seam as the single enforcement
	// point. The real-admin seam instance + the /api/v1/admin/sessions routes are
	// B006's (sessionAuthzAdmin below).
	sessionAuthz *sessionauthz.Seam
	// engine is the RBAC policy engine constructed at the composition root
	// (cmd/joe/server.go) and injected here — the SAME governance-wired engine
	// the guarded accessor above enforces with. It is nil when RBAC is disabled
	// (!cfg.RBACEnabled()), matching the accessor's nil-engine permit-all
	// semantics. Handlers that need componentless zone checks (the regime
	// declare/resolve path) read it instead of constructing their own engine, so
	// there is exactly one engine per request path and the read-posture / promote
	// resolvers wired into it at construction reach every consumer.
	engine *rbac.PolicyEngine
	// sessionAuthzAdmin is the ADMIN session-authorization seam instance (§12.7
	// seam / §12.8 two-instance defense-in-depth, B006). It is built with the REAL
	// D-0011 admin checker (newAdminSessionAuthz / rbacAdminChecker) and is the
	// ONLY instance that can resolve a real admin relationship over a session. The
	// /api/v1/admin/sessions govern handlers authorize through (*Server).
	// sessionAccessAdmin (this instance) AFTER the requireAdmin prefix gate, so
	// cross-tenant governance requires BOTH. No per-user route holds it.
	sessionAuthzAdmin *sessionauthz.Seam
	// modelSwapMu serializes the persist-active-model + live-adapter-Swap pair.
	// TWO handlers perform that pair — POST /models/current (models.go) and the
	// admin POST /admin/llm/model (llmsettings.go) — and the pair is not atomic:
	// two racing switches can interleave persist(A), persist(B), swap(B),
	// swap(A), leaving the live adapter (A) disagreeing with the persisted
	// active model (B) until restart. Both handlers hold this mutex across
	// persist+swap so each pair applies indivisibly and in one order.
	modelSwapMu sync.Mutex
}

// New creates a new API server with access to core services and the RBAC policy
// engine the guarded accessor enforces with. services must not be nil; callers
// that do not have all sub-services wired should pass zero-value sub-service
// fields rather than a nil pointer.
//
// engine is constructed once at the composition root (cmd/joe/server.go, via
// rbac.NewPolicyEngineWithGovernance) and injected here, so the accessor gates
// with the SAME governance-wired engine — carrying the read-posture and
// auto_promote resolvers — that governs the rest of the transport. A nil engine
// means RBAC is disabled and the accessor permits every decision (identical to
// the prior rbac.EnforcementMiddleware(nil) posture); production reaches api.New
// only downstream of cmd/joe's refuse-to-start guard, where the engine is
// non-nil. There is no longer any api.New-internal engine construction — the
// prior newPolicyEngine (which built a bare engine with neither resolver) is
// gone, and with it the drift that left the launch-default team_flat read admit
// structurally unreachable on the transport path.
func New(services *core.Services, engine *rbac.PolicyEngine) *Server {
	if services == nil {
		panic("api.New: services must not be nil")
	}
	services.Metrics = observability.EnsureMetrics(services.Metrics)
	// Phase F: the accessor writes one audit row per decision. The audit
	// repository is wired via core.Services.Audit by cmd/joe/server.go;
	// when nil (auth-disabled local/dev runs without the audit table
	// migration), the accessor skips the audit write and behaves
	// identically to pre-Phase-F.
	var auditRepo audit.Repository
	if services.Audit != nil {
		auditRepo = services.Audit
	}
	accessor := access.New(services.Adapters, services.Graph, engine, auditRepo)
	return &Server{
		services:          services,
		accessor:          accessor,
		engine:            engine,
		inproc:            newInProcessCoreClient(accessor, services),
		sessionAuthz:      newPerUserSessionAuthz(services),
		sessionAuthzAdmin: newAdminSessionAuthz(services),
	}
}

// RegisterRoutes registers all API routes on the given mux
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	s.registerStatusRoutes(mux, apiPrefix)
	s.registerGraphRoutes(mux, apiPrefix)
	s.registerComponentRoutes(mux, apiPrefix)
	s.registerAlertingRoutes(mux, apiPrefix)
	s.registerDatastoreRoutes(mux, apiPrefix)
	s.registerNetworkingRoutes(mux, apiPrefix)
	s.registerSecurityRoutes(mux, apiPrefix)
	s.registerRegistryRoutes(mux, apiPrefix)
	s.registerPanicRoutes(mux, apiPrefix)
	// Read-only mutate-status endpoint: reports the boot-resolved write floor
	// (D-0018/D-0019) as {can_mutate, reason} where reason is one of
	// full/observation/safe_mode.
	s.registerMutateStatusRoutes(mux, apiPrefix)
	s.registerAdminRoutes(mux, apiPrefix)
	// B006: the admin session-governance namespace /api/v1/admin/sessions —
	// cross-tenant list/get/get-messages plus the admin-gated, audited govern
	// actions (purge/archive/restore-archive/retention) whose store effects are
	// deferred to B007.
	s.registerAdminSessionRoutes(mux, apiPrefix)
	s.registerObserveCategoryRoutes(mux, apiPrefix)
	s.registerTaskRoutes(mux, apiPrefix)
	// Phase 2: model control plane — list/swap the single LLM contact point.
	s.registerModelRoutes(mux, apiPrefix)
	s.registerSkillsRoutes(mux, apiPrefix)
	s.registerWebUIRoutes(mux, apiPrefix)
	// The per-user session CRUD lives in the webUI routes above under
	// /api/v1/sessions. The legacy team-global /api/v1/agent-sessions namespace
	// was removed in B005 (§12.8); its captain/findings/runs sub-resources are
	// re-homed under /api/v1/sessions below.
	s.registerFindingsRoutes(mux, apiPrefix)
	s.registerWarningsRoutes(mux, apiPrefix)
	s.registerRegimeRoutes(mux, apiPrefix)
	// Phase 1 Change 6: captain attach + transfer state machine.
	s.registerCaptainRoutes(mux, apiPrefix)
	// Phase 1 Change 7: run lifecycle HTTP API.
	s.registerRunRoutes(mux, apiPrefix)
	// Stream G phase G5: LLM-instrumentation HTTP API — current-user,
	// settings reads + admin-gated writes, usage views, providers.
	s.registerCurrentUserRoutes(mux, apiPrefix)
	// Stream H2 follow-up: public auth-config endpoint under /api/v1/auth/,
	// so the cold logged-out shell can read oidc_enabled before /me (which
	// 401s pre-auth) is reachable.
	s.registerAuthConfigRoutes(mux, apiPrefix)
	s.registerLLMSettingsRoutes(mux, apiPrefix)
	s.registerLLMUsageRoutes(mux, apiPrefix)
	s.registerLLMProvidersRoutes(mux, apiPrefix)
}

// registerStatusRoutes registers status and health check routes
func (s *Server) registerStatusRoutes(mux *http.ServeMux, prefix string) {
	mux.HandleFunc(fmt.Sprintf("GET %s/status", prefix), s.handleStatus)
	// The authoritative build-identity surface. Namespaced under /api/v1 for
	// consistency with /status; serializes the full buildinfo.Info including the
	// boot-computed ui_digest.
	mux.HandleFunc(fmt.Sprintf("GET %s/version", prefix), s.handleVersion)
}

// registerGraphRoutes registers graph query routes.
// Graph access is gated by the guarded accessor (s.accessor), not by a
// direct graph.GraphStore handle.
func (s *Server) registerGraphRoutes(mux *http.ServeMux, prefix string) {
	handler := &graphHandler{server: s}
	mux.HandleFunc(fmt.Sprintf("GET %s/graph/query", prefix), handler.handleQuery)
	mux.HandleFunc(fmt.Sprintf("GET %s/graph/related", prefix), handler.handleRelated)
	mux.HandleFunc(fmt.Sprintf("GET %s/graph/summary", prefix), handler.handleSummary)
}

// registerComponentRoutes registers source management routes
func (s *Server) registerComponentRoutes(mux *http.ServeMux, prefix string) {
	handler := &sourceHandler{server: s}
	// Authenticated (not admin-gated) read of the component-type enum, for the
	// registration UI's type selector. A distinct literal path from
	// /components/{id}, so no route collision.
	mux.HandleFunc(fmt.Sprintf("GET %s/component-types", prefix), handler.handleListTypes)
	mux.HandleFunc(fmt.Sprintf("GET %s/components", prefix), handler.handleList)
	mux.HandleFunc(fmt.Sprintf("POST %s/components", prefix), handler.handleCreate)
	mux.HandleFunc(fmt.Sprintf("GET %s/components/{id}", prefix), handler.handleGet)
	mux.HandleFunc(fmt.Sprintf("DELETE %s/components/{id}", prefix), handler.handleDelete)
	// Describe-only read of the promotion input contract for a component (A002):
	// a GET sub-resource of the component, sibling of the POST .../promote write,
	// that the promotion UI renders its provider-conditional form from. Admin-gated
	// like promote — it describes a privileged capability. A distinct, longer path
	// than GET /components/{id}, so no route collision.
	mux.HandleFunc(fmt.Sprintf("GET %s/components/{id}/promotion-requirements", prefix), handler.handlePromotionRequirements)
	// Live promotion-candidate read (A002): the SIBLING of promotion-requirements.
	// Where promotion-requirements describes the cacheable SHAPE of a reference,
	// this returns the LIVE candidate SET the reference picker offers — for a
	// static-wired component, the env var names under the type's prefix (names
	// only, no values), delegated to the provider seam. Admin-gated; a distinct,
	// longer path than GET /components/{id}, so no route collision.
	mux.HandleFunc(fmt.Sprintf("GET %s/components/{id}/promotion-candidates", prefix), handler.handlePromotionCandidates)
	// Promotion boundary (A003): the single governed read-only-to-armed
	// transition. A POST to a /promote sub-path of the component resource keyed on
	// {id} — the arming verb on the existing resource, distinct from create/get/
	// delete and from any (non-existent) full-resource PATCH/PUT.
	mux.HandleFunc(fmt.Sprintf("POST %s/components/{id}/promote", prefix), handler.handlePromote)
}

// registerAlertingRoutes registers alerting query routes (Alertmanager, PagerDuty, Grafana).
func (s *Server) registerAlertingRoutes(mux *http.ServeMux, prefix string) {
	h := &alertingHandler{server: s}
	// Alertmanager
	mux.HandleFunc(fmt.Sprintf("GET %s/alertmanager/{componentID}/alerts", prefix), h.handleAlertmanagerAlerts)
	// PagerDuty
	mux.HandleFunc(fmt.Sprintf("GET %s/pagerduty/{componentID}/incidents", prefix), h.handlePagerDutyIncidents)
	mux.HandleFunc(fmt.Sprintf("GET %s/pagerduty/{componentID}/services", prefix), h.handlePagerDutyServices)
	// Grafana
	mux.HandleFunc(fmt.Sprintf("GET %s/grafana/{componentID}/dashboards", prefix), h.handleGrafanaDashboards)
	mux.HandleFunc(fmt.Sprintf("GET %s/grafana/{componentID}/dashboards/{uid}", prefix), h.handleGrafanaGetDashboard)
	mux.HandleFunc(fmt.Sprintf("GET %s/grafana/{componentID}/alerts", prefix), h.handleGrafanaAlerts)
}

// alertingHandler delegates to Server alerting methods.
type alertingHandler struct{ server *Server }

func (h *alertingHandler) handleAlertmanagerAlerts(w http.ResponseWriter, r *http.Request) {
	h.server.handleAlertmanagerAlerts(w, r)
}
func (h *alertingHandler) handlePagerDutyIncidents(w http.ResponseWriter, r *http.Request) {
	h.server.handlePagerDutyIncidents(w, r)
}
func (h *alertingHandler) handlePagerDutyServices(w http.ResponseWriter, r *http.Request) {
	h.server.handlePagerDutyServices(w, r)
}
func (h *alertingHandler) handleGrafanaDashboards(w http.ResponseWriter, r *http.Request) {
	h.server.handleGrafanaDashboards(w, r)
}
func (h *alertingHandler) handleGrafanaGetDashboard(w http.ResponseWriter, r *http.Request) {
	h.server.handleGrafanaGetDashboard(w, r)
}
func (h *alertingHandler) handleGrafanaAlerts(w http.ResponseWriter, r *http.Request) {
	h.server.handleGrafanaAlerts(w, r)
}

// graphHandler handles graph-related requests
type graphHandler struct {
	server *Server
}

func (h *graphHandler) handleQuery(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required query parameter 'q'", map[string]any{
			"param": "q",
		})
		return
	}

	principal := rbac.PrincipalFromContext(r.Context())
	nodes, err := h.server.accessor.GraphQuery(r.Context(), principal, q)
	if err != nil {
		if writeAccessError(w, err) {
			return
		}
		writeInternalError(w, err, "graph query")
		return
	}

	if nodes == nil {
		nodes = []graph.Node{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"nodes": nodes,
		"count": len(nodes),
	})
}

func (h *graphHandler) handleRelated(w http.ResponseWriter, r *http.Request) {
	nodeID := r.URL.Query().Get("nodeID")
	if nodeID == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required query parameter 'nodeID'", map[string]any{
			"param": "nodeID",
		})
		return
	}

	depth := 1
	if d := r.URL.Query().Get("depth"); d != "" {
		parsed, err := strconv.Atoi(d)
		if err != nil || parsed < 0 {
			writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "depth must be a non-negative integer", map[string]any{
				"param": "depth",
				"value": d,
			})
			return
		}
		depth = parsed
	}

	principal := rbac.PrincipalFromContext(r.Context())
	subgraph, err := h.server.accessor.GraphRelated(r.Context(), principal, nodeID, depth)
	if err != nil {
		if errors.Is(err, graph.ErrNodeNotFound) {
			writeError(w, http.StatusNotFound, errorCodeNotFound, "node not found", map[string]any{
				"node_id": nodeID,
			})
			return
		}
		if writeAccessError(w, err) {
			return
		}
		writeInternalError(w, err, "graph related")
		return
	}

	writeJSON(w, http.StatusOK, subgraph)
}

func (h *graphHandler) handleSummary(w http.ResponseWriter, r *http.Request) {
	principal := rbac.PrincipalFromContext(r.Context())
	summary, err := h.server.accessor.GraphSummary(r.Context(), principal)
	if err != nil {
		if writeAccessError(w, err) {
			return
		}
		writeInternalError(w, err, "graph summary")
		return
	}

	writeJSON(w, http.StatusOK, summary)
}

// sourceHandler handles source management requests
type sourceHandler struct {
	server *Server
}

func (h *sourceHandler) handleList(w http.ResponseWriter, r *http.Request) {
	h.server.handleListComponents(w, r)
}

func (h *sourceHandler) handleListTypes(w http.ResponseWriter, r *http.Request) {
	h.server.handleListComponentTypes(w, r)
}

func (h *sourceHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	h.server.handleCreateComponent(w, r)
}

func (h *sourceHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	h.server.handleGetComponent(w, r)
}

func (h *sourceHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	h.server.handleDeleteComponent(w, r)
}

func (h *sourceHandler) handlePromote(w http.ResponseWriter, r *http.Request) {
	h.server.handlePromoteComponent(w, r)
}

func (h *sourceHandler) handlePromotionRequirements(w http.ResponseWriter, r *http.Request) {
	h.server.handleComponentPromotionRequirements(w, r)
}

func (h *sourceHandler) handlePromotionCandidates(w http.ResponseWriter, r *http.Request) {
	h.server.handleComponentPromotionCandidates(w, r)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  statusOK,
		"version": buildinfo.Get().Version,
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}

// handleVersion serves the authoritative build-identity surface: the full
// buildinfo.Info (version, commit, build_time, ui_digest). This is the single
// place an external caller reads ui_digest; /status keeps its slim shape and
// does not carry the digest.
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, buildinfo.Get())
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to encode JSON response", "error", err)
	}
}
