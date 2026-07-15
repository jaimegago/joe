package main

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/auth"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/promotereads"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/readposture"
	"github.com/jaimegago/joe/internal/store"
)

// TestRBACEngineSplit_TeamFlatReadAdmit_Graph is the rbac-engine-split regression
// pin. It drives the PRODUCTION assembly path — buildHTTPHandler, the same
// function runServerWithDeps calls — over a temp SQLite database with RBAC
// enabled, the launch-default team_flat read posture, and a single non-admin
// service account holding no grants. It then makes a real HTTP GET /api/v1/graph
// with that account's bearer key.
//
// Admit leg (the bug this pin exists for): before the fix, api.New built its own
// bare policy engine for the guarded accessor — an engine with no read-posture
// resolver — so the team_flat read admit (internal/rbac/policy.go) was
// structurally unreachable on the transport path and this GET returned 403
// no_grant. With the composition-root engine injected into api.New, the accessor
// enforces with the governance-wired engine and the read is admitted 200 with the
// audit reason team_flat_read.
//
// Deny legs (so the fix cannot be satisfied by a permit-all engine): the SAME
// non-admin principal under the zoned posture has no grant on the graph's
// unassigned zone and is denied 403; an unauthenticated request is rejected at
// the edge with 401.
func TestRBACEngineSplit_TeamFlatReadAdmit_Graph(t *testing.T) {
	ts, key, db := buildEngineSplitTestServer(t)

	// Admit leg: team_flat is the launch default, so a fresh install admits every
	// authenticated principal's read regardless of grant.
	if code := getGraph(t, ts, key); code != http.StatusOK {
		t.Fatalf("team_flat GET /api/v1/graph as non-admin svc:reader = %d, want 200 "+
			"(the team_flat read admit must fire on the transport accessor)", code)
	}
	if reason := lastInfraAllowReason(t, db, "graph"); reason != rbac.ReasonTeamFlatRead {
		t.Errorf("audit_log reason for the admitted graph read = %q, want %q",
			reason, rbac.ReasonTeamFlatRead)
	}

	// Deny leg 1: flip the install-wide posture to zoned. The read is now grant
	// based; svc:reader holds no grant on the unassigned zone → 403.
	setPostureZoned(t, db)
	if code := getGraph(t, ts, key); code != http.StatusForbidden {
		t.Errorf("zoned GET /api/v1/graph as ungranted svc:reader = %d, want 403", code)
	}

	// Deny leg 2: an unauthenticated request is rejected at the edge, proving the
	// admit is not a blanket permit-all.
	if code := getGraph(t, ts, ""); code != http.StatusUnauthorized {
		t.Errorf("unauthenticated GET /api/v1/graph = %d, want 401", code)
	}
}

// buildEngineSplitTestServer assembles the HTTP handler through the production
// buildHTTPHandler with a temp SQLite DB, RBAC enabled via one non-admin service
// account, and the default team_flat posture. It returns the test server, the
// bearer key, and the DB (for reading audit_log / flipping the posture).
func buildEngineSplitTestServer(t *testing.T) (*httptest.Server, string, *sql.DB) {
	t.Helper()
	const readerKey = "reader-key"

	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = sqlStore.Close() })

	db := sqlStore.DB()
	driver := sqlStore.Driver()
	auditRepo := audit.NewRepository(db, driver)
	rbacRepo := rbac.NewRepositoryWithAudit(db, driver, auditRepo)
	promoteReadsRepo := promotereads.NewRepository(db, driver)
	readPostureRepo := readposture.NewRepository(db, driver)

	// One non-admin service account → RBACEnabled() true. svc:reader is
	// deliberately never made an admin and holds no grants.
	cfg := &config.Config{
		Server: config.ServerConfig{
			ServiceAccounts: []config.ServiceAccount{{Name: "reader", Key: readerKey}},
		},
	}
	if !cfg.RBACEnabled() {
		t.Fatal("precondition: the pin requires RBAC enabled")
	}

	metrics := observability.NewMetrics()
	services := core.New(cfg, sqlStore, db, driver, adapters.NewRegistry(), metrics)
	services.RBAC = rbacRepo
	services.Audit = auditRepo

	saResolver, err := auth.NewServiceAccountResolver(cfg.Server.ServiceAccounts)
	if err != nil {
		t.Fatalf("service account resolver: %v", err)
	}
	authRepo := auth.NewRepository(db, driver)
	sessionMgr := auth.NewSessionManager(authRepo, cfg.Auth.SessionTTL)

	handler, err := buildHTTPHandler(
		services, cfg, rbacRepo, promoteReadsRepo, readPostureRepo,
		authRepo, sessionMgr, saResolver, false, metrics, api.New,
	)
	if err != nil {
		t.Fatalf("buildHTTPHandler: %v", err)
	}
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts, readerKey, db
}

func getGraph(t *testing.T, ts *httptest.Server, bearer string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/graph", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

// lastInfraAllowReason reads the most recent allow decision reason the guarded
// accessor recorded for the given component (the audit_log 'component_id' column,
// renamed from 'source' by migration 023) — the observable surface the admit
// reason team_flat_read is asserted through.
func lastInfraAllowReason(t *testing.T, db *sql.DB, component string) string {
	t.Helper()
	var reason string
	if err := db.QueryRowContext(context.Background(),
		`SELECT reason FROM audit_log WHERE kind = 'infra_access' AND component_id = ? AND decision = 'allow' ORDER BY id DESC LIMIT 1`,
		component).Scan(&reason); err != nil {
		t.Fatalf("query audit_log for allow reason: %v", err)
	}
	return reason
}

// setPostureZoned flips the install-wide read posture to zoned by writing the
// singleton row directly (no principal / audit dependency), which the policy
// engine picks up live on the next decision.
func setPostureZoned(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	repo := readposture.NewRepository(db, store.DriverSQLite)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := repo.SetPostureTx(ctx, tx, readposture.PostureZoned, time.Now()); err != nil {
		_ = tx.Rollback()
		t.Fatalf("set posture zoned: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit posture: %v", err)
	}
}
