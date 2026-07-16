package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/agentloop"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/auth"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/env"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/llmsettings"
	"github.com/jaimegago/joe/internal/llmusage"
	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/promotereads"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/readposture"
	"github.com/jaimegago/joe/internal/store"
)

// Stream G phase G5 — admin-gated HTTP API tests. The cases below
// together cover: the admin gate (admin OK, non-admin forbidden,
// auth-disabled permits); the current-user endpoint (admin /
// non-admin / auth-disabled); settings GET labelling (unset
// backstop, configured positive, explicit-disable negative); each
// settings write admin-gated AND atomic with an audit row for an
// admin AND no row written for a non-admin; usage endpoints
// (aggregate/per-model auth-only, per-principal admin-gated);
// providers booleans-only and no key leakage.
//
// Tests live in package `api` (not `api_test`) so they can stub the
// `newModelAdapter` factory seam — the same trick models_test.go uses
// — without spinning up real provider clients.

// --- Common test fixture builders ---

type llmadminFixture struct {
	t         *testing.T
	store     *store.Store
	rbac      rbac.Repository
	audit     audit.Repository
	usageRepo llmusage.Repository
	settings  *llmsettings.MutationService
	services  *core.Services
	server    *Server
	mux       *http.ServeMux
	// sessions is the auth session store the principal-disable path purges
	// (Identity Stage 3). Tests seed sessions through it and assert they are
	// gone after a disable.
	sessions auth.Repository
}

func newLLMAdminStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func newLLMAdminFixture(t *testing.T, rbacEnabled bool) *llmadminFixture {
	return newLLMAdminFixtureCfg(t, rbacEnabled, "")
}

// newLLMAdminFixtureCfg is newLLMAdminFixture with a configurable bootstrap
// admin email (auth.admin_email) so the Stage 3 admin-remove bootstrap-guard
// test can exercise the 409. adminEmail must be set BEFORE RegisterRoutes — the
// admin handler captures it at registration time.
func newLLMAdminFixtureCfg(t *testing.T, rbacEnabled bool, adminEmail string) *llmadminFixture {
	t.Helper()
	s := newLLMAdminStore(t)
	// The audit sink is wrapped in a swappable indirection shared by BOTH the
	// RBAC repository (which now writes admin-mutation rows in-transaction) and
	// services.Audit (the handler path for reads and gate denials). A test can
	// break the underlying sink via breakAudit() to exercise the fail-closed /
	// fail-open paths uniformly across both layers.
	auditRepo := &swappableAudit{inner: audit.NewRepository(s.DB(), s.Driver())}
	rbacRepo := rbac.NewRepositoryWithAudit(s.DB(), s.Driver(), auditRepo)
	usageRepo := llmusage.NewRepository(s.DB(), s.Driver())
	settingsRepo := llmsettings.NewRepository(s.DB(), s.Driver())
	settingsSvc := llmsettings.NewMutationService(settingsRepo, auditRepo)
	promoteReadsSvc := promotereads.NewMutationService(promotereads.NewRepository(s.DB(), s.Driver()), auditRepo)
	readPostureSvc := readposture.NewMutationService(readposture.NewRepository(s.DB(), s.Driver()), auditRepo)
	sessionLimitsProvider := llmsettings.NewSessionLimitsProvider(settingsRepo, agentloop.NewStaticSessionLimits(), nil)
	costLimitsProvider := llmsettings.NewCostLimitsProvider(settingsRepo, llmusage.NewStaticCostLimits(), nil)
	contextBudgetProvider := llmsettings.NewContextBudgetProvider(settingsRepo, agentloop.NewStaticContextBudget(), nil)

	sessionsRepo := auth.NewRepository(s.DB(), s.Driver())
	cfg := &config.Config{
		LLM: config.LLMConfig{
			Current: "default",
			Available: map[string]config.ModelConfig{
				"default": {Provider: "claude", Model: "claude-sonnet-4-20250514"},
				"alt":     {Provider: "gemini", Model: "gemini-2.5-flash"},
			},
			Currency:            config.CurrencyUSD,
			USDToConfiguredRate: 1.0,
		},
	}
	cfg.Auth.AdminEmail = adminEmail
	metrics := observability.NewMetrics()
	services := core.New(cfg, s, s.DB(), s.Driver(), nil, metrics)
	services.RBAC = rbacRepo
	services.Audit = auditRepo
	services.LLMUsage = usageRepo
	services.LLMSettings = settingsSvc
	services.PromoteReads = promoteReadsSvc
	services.ReadPosture = readPostureSvc
	services.SessionLimitsProvider = sessionLimitsProvider
	services.CostLimitsProvider = costLimitsProvider
	services.ContextBudgetProvider = contextBudgetProvider
	services.RBACEnabled = rbacEnabled
	services.LLM = llm.NewSwappableAdapter(&silentLLMAdapter{}, "default")
	// Identity Stage 3 wiring: the admin REST surface manages the identity
	// registry and admin roster. rbacRepo satisfies PrincipalRepository too.
	services.Principals = rbacRepo
	services.Provisioner = auth.NewProvisioner(rbacRepo)
	services.PrincipalAdmin = auth.NewPrincipalAdmin(rbacRepo, sessionsRepo)

	srv := New(services, TestingPolicyEngine(services))
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	return &llmadminFixture{
		t:         t,
		store:     s,
		rbac:      rbacRepo,
		audit:     auditRepo,
		usageRepo: usageRepo,
		settings:  settingsSvc,
		services:  services,
		server:    srv,
		mux:       mux,
		sessions:  sessionsRepo,
	}
}

func (f *llmadminFixture) markAdmin(principal string) {
	f.t.Helper()
	if err := f.rbac.AddAdmin(context.Background(), rbac.Admin{
		Principal: principal,
		GrantedBy: "test", Reason: "test fixture",
	}, "test"); err != nil {
		f.t.Fatalf("AddAdmin %q: %v", principal, err)
	}
}

func (f *llmadminFixture) do(method, path string, body string, principal rbac.Principal) *httptest.ResponseRecorder {
	f.t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	if principal != "" {
		req = req.WithContext(rbac.WithPrincipal(req.Context(), principal))
	}
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, req)
	return w
}

func (f *llmadminFixture) countAudit(action string) int {
	f.t.Helper()
	var n int
	err := f.store.DB().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM audit_log WHERE action = ?`, action).Scan(&n)
	if err != nil {
		f.t.Fatalf("count audit rows %q: %v", action, err)
	}
	return n
}

// silentLLMAdapter is the bare-minimum LLMAdapter the SwappableAdapter
// wraps in these tests. None of the G5 admin endpoints invoke Chat —
// they only read .Current() — so the methods can return errors freely.
type silentLLMAdapter struct{}

func (silentLLMAdapter) Chat(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// stubAdapterFactory replaces newModelAdapter with one that always
// returns a silent adapter so the SetActiveModel admin write does not
// require real provider credentials. Same seam models_test.go uses.
func stubAdapterFactory(t *testing.T) {
	t.Helper()
	orig := newModelAdapter
	newModelAdapter = func(ctx context.Context, mc config.ModelConfig) (llm.LLMAdapter, error) {
		return &silentLLMAdapter{}, nil
	}
	t.Cleanup(func() { newModelAdapter = orig })
}

// --- Admin gate ---

func TestRequireAdmin_AdminAllowed(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	w := f.do(http.MethodPost, "/api/v1/llm/settings/runaway-ceiling",
		`{"value": 0}`, "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("admin call: status=%d body=%s; want 200", w.Code, w.Body.String())
	}
}

func TestRequireAdmin_NonAdminForbidden(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	w := f.do(http.MethodPost, "/api/v1/llm/settings/runaway-ceiling",
		`{"value": 9999}`, "user:bob")
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin call: status=%d body=%s; want 403", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "forbidden" {
		t.Errorf("error code = %v; want \"forbidden\"", body["error"])
	}
	if n := f.countAudit(audit.ActionLLMSetRunawayCeiling); n != 0 {
		t.Errorf("audit rows for runaway-ceiling mutation = %d; want 0 — non-admin write must not audit", n)
	}
}

func TestRequireAdmin_AuthDisabledPermits(t *testing.T) {
	f := newLLMAdminFixture(t, false)
	w := f.do(http.MethodPost, "/api/v1/llm/settings/runaway-ceiling",
		`{"value": 0}`, "user:nobody")
	if w.Code != http.StatusOK {
		t.Fatalf("auth-disabled call: status=%d body=%s; want 200", w.Code, w.Body.String())
	}
}

// --- Current-user endpoint ---

func TestCurrentUser_Admin(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	w := f.do(http.MethodGet, "/api/v1/me", "", "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Principal   string `json:"principal"`
		IsAdmin     bool   `json:"is_admin"`
		RBACEnabled bool   `json:"rbac_enabled"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Principal != "user:alice" || !body.IsAdmin || !body.RBACEnabled {
		t.Errorf("got %+v; want {Principal:user:alice IsAdmin:true RBACEnabled:true}", body)
	}
}

func TestCurrentUser_NonAdmin(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	w := f.do(http.MethodGet, "/api/v1/me", "", "user:bob")
	var body struct {
		Principal   string `json:"principal"`
		IsAdmin     bool   `json:"is_admin"`
		RBACEnabled bool   `json:"rbac_enabled"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Principal != "user:bob" || body.IsAdmin || !body.RBACEnabled {
		t.Errorf("got %+v; want {Principal:user:bob IsAdmin:false RBACEnabled:true}", body)
	}
}

func TestCurrentUser_AuthDisabled(t *testing.T) {
	f := newLLMAdminFixture(t, false)
	w := f.do(http.MethodGet, "/api/v1/me", "", "user:anyone")
	var body struct {
		Principal   string `json:"principal"`
		IsAdmin     bool   `json:"is_admin"`
		RBACEnabled bool   `json:"rbac_enabled"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.IsAdmin || body.RBACEnabled {
		t.Errorf("got %+v; want is_admin=true rbac_enabled=false (auth-disabled permits)", body)
	}
}

// --- Current-user zone assignments (Item 5 / OPERATOR_SURFACE_VERIFICATION
// item 11): /me carries the caller's reachable zones so the UI can detect the
// zero-zone dead-end. The fixture store runs migration 006, which seeds the
// four default zones (prod-readonly, prod-write, dev-full, unassigned).

type meZonesBody struct {
	Zones []struct {
		ID             string   `json:"id"`
		AllowedActions []string `json:"allowed_actions"`
	} `json:"zones"`
}

func (f *llmadminFixture) meZones(t *testing.T, principal rbac.Principal) meZonesBody {
	t.Helper()
	w := f.do(http.MethodGet, "/api/v1/me", "", principal)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body meZonesBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

// TestCurrentUser_Zones_AdminSeesAll: an admin reaches every seeded zone, each
// carrying its allowed actions.
func TestCurrentUser_Zones_AdminSeesAll(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")

	all, err := f.rbac.ListZones(context.Background())
	if err != nil {
		t.Fatalf("ListZones: %v", err)
	}
	body := f.meZones(t, "user:alice")
	if len(body.Zones) != len(all) {
		t.Fatalf("admin zones = %d; want all %d seeded zones", len(body.Zones), len(all))
	}
	// allowed_actions must be populated for at least one zone (not just ids).
	var sawActions bool
	for _, z := range body.Zones {
		if len(z.AllowedActions) > 0 {
			sawActions = true
		}
	}
	if !sawActions {
		t.Errorf("no zone reported allowed_actions; got %+v", body.Zones)
	}
}

// TestCurrentUser_Zones_NonAdminSeesOnlyGranted: a non-admin reaches exactly
// the zones their rbac_policies grants cover, not the full set.
func TestCurrentUser_Zones_NonAdminSeesOnlyGranted(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	if _, err := f.rbac.CreatePolicy(context.Background(), rbac.Policy{
		Principal: "user:bob", ZoneID: "prod-readonly",
	}, "test"); err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}

	body := f.meZones(t, "user:bob")
	if len(body.Zones) != 1 || body.Zones[0].ID != "prod-readonly" {
		t.Fatalf("non-admin zones = %+v; want exactly [prod-readonly]", body.Zones)
	}
}

// TestCurrentUser_Zones_ZeroZoneIsEmptyArray: a non-admin with no grants gets
// a non-nil empty array — the exact signal the UI keys its access-pending
// empty state on (it must serialize as [], never null).
func TestCurrentUser_Zones_ZeroZoneIsEmptyArray(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	body := f.meZones(t, "user:nobody")
	if len(body.Zones) != 0 {
		t.Fatalf("zero-zone user zones = %+v; want empty", body.Zones)
	}
	// Assert the wire form is [] not null.
	w := f.do(http.MethodGet, "/api/v1/me", "", "user:nobody")
	if !strings.Contains(w.Body.String(), `"zones":[]`) {
		t.Errorf("expected `\"zones\":[]` in body, got %s", w.Body.String())
	}
}

// Stream H2 — /me reports oidc_enabled sourced from services.OIDCEnabled
// (cfg.Auth.OIDC.Configured() at the build site). Present and correct
// whether OIDC is configured or not.
func TestCurrentUser_OIDCEnabled(t *testing.T) {
	for _, tc := range []struct {
		name        string
		oidcEnabled bool
	}{
		{"oidc configured", true},
		{"oidc not configured", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newLLMAdminFixture(t, true)
			f.services.OIDCEnabled = tc.oidcEnabled
			w := f.do(http.MethodGet, "/api/v1/me", "", "user:alice")
			if w.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			var body struct {
				OIDCEnabled bool `json:"oidc_enabled"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.OIDCEnabled != tc.oidcEnabled {
				t.Errorf("oidc_enabled = %v; want %v", body.OIDCEnabled, tc.oidcEnabled)
			}
		})
	}
}

// --- Settings GET: backstop-fallback vs configured (including negative) ---

func TestSettingsGet_BackstopAndConfiguredLabels(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	ctx := context.Background()

	w := f.do(http.MethodGet, "/api/v1/llm/settings", "", "user:bob")
	if w.Code != http.StatusOK {
		t.Fatalf("get settings: status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		ActiveModel string `json:"active_model"`
		CostLimits  []struct {
			Window    string `json:"window"`
			StoredRaw int64  `json:"stored_raw"`
			State     string `json:"state"`
			Effective int64  `json:"effective"`
		} `json:"cost_limits"`
		RunawayCeiling struct {
			StoredRaw int    `json:"stored_raw"`
			State     string `json:"state"`
			Effective int    `json:"effective"`
		} `json:"runaway_ceiling"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	stateByWindow := map[string]string{}
	rawByWindow := map[string]int64{}
	effByWindow := map[string]int64{}
	for _, l := range resp.CostLimits {
		stateByWindow[l.Window] = l.State
		rawByWindow[l.Window] = l.StoredRaw
		effByWindow[l.Window] = l.Effective
	}
	// Unset (stored zero) window: backstop-fallback state, and the
	// effective value is the hardcoded backstop the gate substitutes —
	// NOT zero — sourced through services.CostLimitsProvider.
	if stateByWindow["hourly"] != LimitStateBackstop {
		t.Errorf("hourly state=%q raw=%d; want backstop label for unset zero", stateByWindow["hourly"], rawByWindow["hourly"])
	}
	if effByWindow["hourly"] != llmusage.DefaultHourlyCostLimitNano {
		t.Errorf("hourly effective=%d; want backstop %d for unset zero", effByWindow["hourly"], llmusage.DefaultHourlyCostLimitNano)
	}
	if resp.RunawayCeiling.State != LimitStateBackstop || resp.RunawayCeiling.StoredRaw != 0 {
		t.Errorf("runaway: state=%q raw=%d; want backstop+0", resp.RunawayCeiling.State, resp.RunawayCeiling.StoredRaw)
	}
	if resp.RunawayCeiling.Effective != agentloop.DefaultSessionTokenCeiling {
		t.Errorf("runaway effective=%d; want backstop ceiling %d for unset zero", resp.RunawayCeiling.Effective, agentloop.DefaultSessionTokenCeiling)
	}

	if err := f.settings.SetCostLimit(ctx, "hourly", 42_000); err != nil {
		t.Fatalf("set hourly: %v", err)
	}
	if err := f.settings.SetCostLimit(ctx, "monthly", -1); err != nil {
		t.Fatalf("set monthly negative: %v", err)
	}

	w2 := f.do(http.MethodGet, "/api/v1/llm/settings", "", "user:bob")
	if w2.Code != http.StatusOK {
		t.Fatalf("get settings #2: status=%d body=%s", w2.Code, w2.Body.String())
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode #2: %v", err)
	}
	stateByWindow = map[string]string{}
	rawByWindow = map[string]int64{}
	effByWindow = map[string]int64{}
	for _, l := range resp.CostLimits {
		stateByWindow[l.Window] = l.State
		rawByWindow[l.Window] = l.StoredRaw
		effByWindow[l.Window] = l.Effective
	}
	// Configured positive: effective equals the stored value.
	if stateByWindow["hourly"] != LimitStateConfigured || rawByWindow["hourly"] != 42_000 {
		t.Errorf("hourly after set: state=%q raw=%d; want configured+42000", stateByWindow["hourly"], rawByWindow["hourly"])
	}
	if effByWindow["hourly"] != 42_000 {
		t.Errorf("hourly effective=%d after set; want stored value 42000", effByWindow["hourly"])
	}
	// Negative explicit-disable: state stays configured, but the gate
	// enforces nothing on a non-positive limit, so the effective value is
	// 0 ("no limit in force") — not the raw negative the UI would misread.
	if stateByWindow["monthly"] != LimitStateConfigured || rawByWindow["monthly"] != -1 {
		t.Errorf("monthly after explicit-disable: state=%q raw=%d; want configured+(-1) — negative is operator-explicit, not backstop",
			stateByWindow["monthly"], rawByWindow["monthly"])
	}
	if effByWindow["monthly"] != 0 {
		t.Errorf("monthly effective=%d after explicit-disable; want 0 — gate enforces nothing on a non-positive limit", effByWindow["monthly"])
	}
	// Untouched window still backstop-fallback with its backstop effective.
	if stateByWindow["daily"] != LimitStateBackstop || rawByWindow["daily"] != 0 {
		t.Errorf("daily untouched: state=%q raw=%d; want backstop+0", stateByWindow["daily"], rawByWindow["daily"])
	}
	if effByWindow["daily"] != llmusage.DefaultDailyCostLimitNano {
		t.Errorf("daily effective=%d; want backstop %d for unset zero", effByWindow["daily"], llmusage.DefaultDailyCostLimitNano)
	}
}

// --- Settings writes: admin-gated AND atomic-with-audit ---

func TestSetActiveModel_AdminMutatesAndAudits(t *testing.T) {
	stubAdapterFactory(t)
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")

	before := f.countAudit(audit.ActionLLMSetActiveModel)
	w := f.do(http.MethodPost, "/api/v1/llm/settings/active-model",
		`{"name":"alt"}`, "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("admin set-active-model: status=%d body=%s", w.Code, w.Body.String())
	}
	got, err := f.services.LLMSettings.Repo().ReadActiveModel(context.Background())
	if err != nil {
		t.Fatalf("read active model: %v", err)
	}
	if got != "alt" {
		t.Errorf("active_model = %q; want \"alt\"", got)
	}
	if n := f.countAudit(audit.ActionLLMSetActiveModel); n != before+1 {
		t.Errorf("audit rows for set-active-model = %d; want %d", n, before+1)
	}
	if sw := f.services.LLM.(*llm.SwappableAdapter); sw.Current() != "alt" {
		t.Errorf("SwappableAdapter.Current = %q; want \"alt\" — live adapter must swap on success", sw.Current())
	}
}

func TestSetActiveModel_NonAdminForbiddenNoMutationNoAudit(t *testing.T) {
	stubAdapterFactory(t)
	f := newLLMAdminFixture(t, true)

	before := f.countAudit(audit.ActionLLMSetActiveModel)
	w := f.do(http.MethodPost, "/api/v1/llm/settings/active-model",
		`{"name":"alt"}`, "user:bob")
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin set-active-model: status=%d body=%s; want 403", w.Code, w.Body.String())
	}
	got, err := f.services.LLMSettings.Repo().ReadActiveModel(context.Background())
	if err != nil {
		t.Fatalf("read active model: %v", err)
	}
	if got == "alt" {
		t.Errorf("active_model = %q; want unchanged — non-admin must not mutate", got)
	}
	if n := f.countAudit(audit.ActionLLMSetActiveModel); n != before {
		t.Errorf("audit rows = %d; want %d — non-admin must not audit", n, before)
	}
	if sw := f.services.LLM.(*llm.SwappableAdapter); sw.Current() == "alt" {
		t.Errorf("live adapter swapped on a forbidden request")
	}
}

func TestSetCostLimit_AdminMutatesAndAudits(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")

	before := f.countAudit(audit.ActionLLMSetCostLimit)
	w := f.do(http.MethodPost, "/api/v1/llm/settings/cost-limit",
		`{"window":"hourly","value":1234}`, "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("admin set-cost-limit: status=%d body=%s", w.Code, w.Body.String())
	}
	if n := f.countAudit(audit.ActionLLMSetCostLimit); n != before+1 {
		t.Errorf("audit rows = %d; want %d", n, before+1)
	}
}

func TestSetCostLimit_NonAdminForbiddenNoMutationNoAudit(t *testing.T) {
	f := newLLMAdminFixture(t, true)

	before := f.countAudit(audit.ActionLLMSetCostLimit)
	w := f.do(http.MethodPost, "/api/v1/llm/settings/cost-limit",
		`{"window":"hourly","value":9999}`, "user:bob")
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin set-cost-limit: status=%d body=%s; want 403", w.Code, w.Body.String())
	}
	limits, err := f.services.LLMSettings.Repo().ReadCostLimits(context.Background())
	if err != nil {
		t.Fatalf("read cost limits: %v", err)
	}
	if limits.HourlyNano == 9999 {
		t.Errorf("hourly mutated by non-admin")
	}
	if n := f.countAudit(audit.ActionLLMSetCostLimit); n != before {
		t.Errorf("audit rows = %d; want %d — non-admin must not audit", n, before)
	}
}

func TestSetRunawayCeiling_AdminMutatesAndAudits(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")

	before := f.countAudit(audit.ActionLLMSetRunawayCeiling)
	w := f.do(http.MethodPost, "/api/v1/llm/settings/runaway-ceiling",
		`{"value":5555}`, "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("admin set-runaway: status=%d body=%s", w.Code, w.Body.String())
	}
	if n := f.countAudit(audit.ActionLLMSetRunawayCeiling); n != before+1 {
		t.Errorf("audit rows = %d; want %d", n, before+1)
	}
}

// --- Usage endpoints ---

func TestUsageAggregate_ReturnsRollupsForAuthenticatedCaller(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := f.usageRepo.Insert(ctx, llmusage.Row{
		Timestamp:         now,
		Principal:         "user:alice",
		Model:             "claude-sonnet-4-20250514",
		InputTokens:       100,
		OutputTokens:      50,
		EstimatedCostNano: 1_500,
		Currency:          "USD",
		SessionID:         "sess-1",
	}); err != nil {
		t.Fatalf("insert usage: %v", err)
	}

	w := f.do(http.MethodGet, "/api/v1/llm/usage/aggregate", "", "user:bob")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Today []struct {
			Calls             int64  `json:"calls"`
			EstimatedCostNano int64  `json:"estimated_cost_nano"`
			Currency          string `json:"currency"`
		} `json:"today"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Today) != 1 || body.Today[0].Calls != 1 || body.Today[0].EstimatedCostNano != 1_500 {
		t.Errorf("today rollup = %+v; want one USD row with 1 call / 1500 cost", body.Today)
	}
	if body.Today[0].Currency != "USD" {
		t.Errorf("currency missing from aggregate row: %+v", body.Today[0])
	}
}

func TestUsageSession_ReturnsRows(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	ctx := context.Background()
	if err := f.usageRepo.Insert(ctx, llmusage.Row{
		Model:             "claude-sonnet-4-20250514",
		EstimatedCostNano: 7,
		Currency:          "USD",
		SessionID:         "sess-X",
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	w := f.do(http.MethodGet, "/api/v1/llm/usage/sessions/sess-X", "", "user:bob")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "\"calls\":1") {
		t.Errorf("session response missing calls=1: %s", w.Body.String())
	}
}

func TestUsagePerModel_AvailableToAuthenticated(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	ctx := context.Background()
	if err := f.usageRepo.Insert(ctx, llmusage.Row{
		Model:    "claude-sonnet-4-20250514",
		Currency: "USD",
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	w := f.do(http.MethodGet, "/api/v1/llm/usage/per-model?window=day", "", "user:bob")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "claude-sonnet-4-20250514") {
		t.Errorf("per-model response missing model name: %s", w.Body.String())
	}
}

func TestUsagePerPrincipal_AdminVsNonAdmin(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	ctx := context.Background()
	if err := f.usageRepo.Insert(ctx, llmusage.Row{
		Model: "claude-sonnet-4-20250514", Currency: "USD", Principal: "user:carol",
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	wDeny := f.do(http.MethodGet, "/api/v1/llm/usage/per-principal?window=day", "", "user:bob")
	if wDeny.Code != http.StatusForbidden {
		t.Fatalf("non-admin per-principal: status=%d body=%s; want 403", wDeny.Code, wDeny.Body.String())
	}

	wOK := f.do(http.MethodGet, "/api/v1/llm/usage/per-principal?window=day", "", "user:alice")
	if wOK.Code != http.StatusOK {
		t.Fatalf("admin per-principal: status=%d body=%s; want 200", wOK.Code, wOK.Body.String())
	}
	if !strings.Contains(wOK.Body.String(), "user:carol") {
		t.Errorf("admin response missing per-principal row: %s", wOK.Body.String())
	}
}

// --- Providers endpoint ---

func TestProviders_BooleansOnlyAndNoKeyLeak(t *testing.T) {
	const sentinelKey = "sk-leaktest-abcdefghijklmnopqrstuvwxyz-do-not-leak"
	t.Setenv(env.AnthropicAPIKey, sentinelKey)
	t.Setenv(env.GeminiAPIKey, "")
	_ = os.Unsetenv(env.GoogleAPIKey)
	t.Setenv(env.GoogleAPIKey, "")

	f := newLLMAdminFixture(t, true)

	w := f.do(http.MethodGet, "/api/v1/llm/providers", "", "user:bob")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	raw := w.Body.String()
	if strings.Contains(raw, sentinelKey) {
		t.Errorf("response leaked sentinel key:\n%s", raw)
	}
	if strings.Contains(raw, "sk-leaktest") {
		t.Errorf("response leaked sentinel key prefix:\n%s", raw)
	}
	if strings.Contains(raw, fmt.Sprintf("%d", len(sentinelKey))) {
		// Looser guard: forbid the key-length number appearing in the
		// response at all. The legitimate response fields are tokens,
		// costs, and booleans; coincidence is unlikely here.
		if strings.Contains(raw, fmt.Sprintf("\"length\":%d", len(sentinelKey))) {
			t.Errorf("response surfaced sentinel key length:\n%s", raw)
		}
	}

	var body struct {
		Providers []struct {
			Name       string `json:"name"`
			Provider   string `json:"provider"`
			Model      string `json:"model"`
			Configured bool   `json:"configured"`
			KeyPresent bool   `json:"key_present"`
		} `json:"providers"`
		Current string `json:"current"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Providers) == 0 {
		t.Fatalf("no providers returned; want at least the configured models")
	}
	sawClaudeWithKey := false
	sawGeminiNoKey := false
	for _, p := range body.Providers {
		if !p.Configured {
			t.Errorf("provider %+v reports configured=false; the entry came from config", p)
		}
		if p.Provider == "claude" && p.KeyPresent {
			sawClaudeWithKey = true
		}
		if p.Provider == "gemini" && !p.KeyPresent {
			sawGeminiNoKey = true
		}
	}
	if !sawClaudeWithKey {
		t.Errorf("expected at least one claude entry with key_present=true (we set ANTHROPIC_API_KEY)")
	}
	if !sawGeminiNoKey {
		t.Errorf("expected at least one gemini entry with key_present=false (we cleared the gemini keys)")
	}
	if body.Current == "" {
		t.Errorf("response missing current selection")
	}
}
