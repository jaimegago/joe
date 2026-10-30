//go:build e2e
// +build e2e

package e2e

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/runfindings"
)

// The read-only Auto Joe, end to end (joe-pm threads/autojoe-ro-e2e-backfill,
// decision 0018). One joe serves every scenario: a run is aimed by its scope,
// so scenarios that use disjoint scopes share the binary, and the responder
// tells their passes apart by the scope section joe writes into each run's
// system prompt (ChatRequest.IsAutoRunPassOver).
//
// The world a run reads is a git repository on local disk, registered as a
// component, armed with the no-credential kind and granted to svc:agent:core
// by an admin over the API. git_log on it is the tool call that covers it, and
// the Core Agent's refresh of it is what puts the node a finding is about into
// the graph. The other components are registered and never connected: a
// trigger needs a component to exist, not to be reachable.

const (
	// readerKey is the bearer of svc:reader, a grantless non-admin.
	readerKey = "e2e-reader-key-not-a-real-credential"

	compRepo   = "e2e-repo"   // git, granted to svc:agent:core; covered by git_log
	compIdle   = "e2e-idle"   // registered, never connected
	compHold   = "e2e-hold"   // the scope a held run keeps live
	compBudget = "e2e-budget" // the scope a run exhausts its token ceiling on
	compSched  = "e2e-sched"  // the scope a schedule fires on
	compAuthz  = "e2e-authz"  // the scope of the run whose accessor decisions are audited

	// repoNodeID is the anchor node the Core Agent writes for compRepo
	// (coreagent gitAnchorNodeID); a finding is about it.
	repoNodeID = "git/" + compRepo

	// tokenCeiling is the deployment's per-run token ceiling. Every reply the
	// responder gives reports ~15 tokens of usage, except the budget
	// scenario's first, which reports more than the ceiling.
	tokenCeiling = 5000

	// The schedule fires on the scheduler's first 30s tick at or after its
	// one-minute interval, and the refresher's first tick is one minute after
	// boot. Both are joe's cadence, not the test's; this bounds the wait.
	cadenceWait = 150 * time.Second
)

// autoRun is the part of a run record the scenarios assert on, decoded apart
// from internal/autorun so a wire change breaks a test.
type autoRun struct {
	ID                  string   `json:"id"`
	Activator           string   `json:"activator"`
	Principal           string   `json:"principal"`
	ResolvedComponents  []string `json:"resolved_components"`
	TokenCeiling        int      `json:"token_ceiling"`
	WallClockSeconds    int      `json:"wall_clock_seconds"`
	ReportReserveTokens int      `json:"report_reserve_tokens"`
	State               string   `json:"state"`
	StopReason          string   `json:"stop_reason"`
	Covered             []string `json:"covered"`
	NotCovered          []string `json:"not_covered"`
	TokensUsed          int      `json:"tokens_used"`
	ReportTokens        int      `json:"report_tokens"`
	SessionID           string   `json:"session_id"`
	Error               string   `json:"error"`
	StartedAt           string   `json:"started_at"`
	EndedAt             string   `json:"ended_at"`
}

type runFinding struct {
	RunID              string `json:"run_id"`
	Type               string `json:"type"`
	SubjectNodeID      string `json:"subject_node_id"`
	SubjectComponentID string `json:"subject_component_id"`
	Claim              string `json:"claim"`
	Band               string `json:"band"`
	Label              string `json:"label"`
	Derivation         struct {
		EvidenceLine string `json:"evidence_line"`
		BaseBand     string `json:"base_band"`
		Band         string `json:"band"`
	} `json:"derivation"`
}

type runFindings struct {
	Runs []struct {
		RunID        string `json:"run_id"`
		FindingCount int    `json:"finding_count"`
	} `json:"runs"`
	Findings []runFinding `json:"findings"`
	Count    int          `json:"count"`
}

// findingsBlock renders a joe-findings block, the run's output contract
// (runfindings.OutputContract), around a findings array.
func findingsBlock(findings string) string {
	return "```" + runfindings.FindingsFence + "\n{\"findings\": " + findings + "}\n```"
}

// theFinding is the one finding the findings scenario's model reports: a risk
// on the repository's anchor node.
var theFinding = `[{"type": "risk", "subject_node_id": "` + repoNodeID + `", ` +
	`"claim": "e2e-repo has a single branch and no mirror", ` +
	`"consequence": "would_be_unavailable", ` +
	`"condition": "the disk holding the repository is lost", ` +
	`"evidence_line": "git_log: 1 commit on HEAD"}]`

// passScript answers one call of a run's pass: the probe gets plain text, the
// first tools-carrying call gets tool (when there is one), and every other call
// gets final, so a pass is one tool call and one answer.
func passScript(req ChatRequest, tool *ToolCall, final string, usage Usage) Reply {
	switch {
	case req.IsToolIntentProbe():
		return Reply{Content: "No tool call was intended; the answer stands.", Usage: usage}
	case tool != nil && req.HasTools() && !req.HasToolResult():
		return Reply{ToolCalls: []ToolCall{*tool}, Usage: usage}
	default:
		return Reply{Content: final, Usage: usage}
	}
}

// autoRunScripts routes each call to the scenario whose scope it carries.
type autoRunScripts struct {
	mu sync.Mutex
	// findings is the block the findings scenario's next pass answers with.
	findings string

	holdReached chan struct{}
	holdOnce    sync.Once
	release     chan struct{}
	releaseOnce sync.Once
}

func newAutoRunScripts() *autoRunScripts {
	return &autoRunScripts{holdReached: make(chan struct{}), release: make(chan struct{})}
}

func (s *autoRunScripts) setFindings(block string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.findings = block
}

func (s *autoRunScripts) releaseHold() { s.releaseOnce.Do(func() { close(s.release) }) }

func (s *autoRunScripts) script(call int, req ChatRequest) (Reply, error) {
	usage := Usage{PromptTokens: 10, CompletionTokens: 5}
	gitLog := &ToolCall{Name: "git_log", Args: map[string]any{"component_id": compRepo, "limit": 5}}
	empty := "Pass complete.\n\n" + findingsBlock("[]")
	switch {
	case req.IsAutoRunPassOver(compRepo, compIdle), req.IsAutoRunPassOver(compRepo, compAuthz):
		return passScript(req, gitLog, empty, usage), nil
	case req.IsAutoRunPassOver(compRepo):
		s.mu.Lock()
		block := s.findings
		s.mu.Unlock()
		return passScript(req, gitLog, "I read the repository's history.\n\n"+block, usage), nil
	case req.IsAutoRunPassOver(compHold):
		// The held pass stays live until the test releases it: every call
		// answers, after a second, with one more tool call and no usage, so
		// the run neither finishes nor reaches a ceiling, and no HTTP request
		// parks long enough for joe's client timeout to end it (the hold spans
		// a schedule's whole interval in scenario 8). After release the next
		// call gets the final text.
		s.holdOnce.Do(func() { close(s.holdReached) })
		select {
		case <-s.release:
			return passScript(req, nil, empty, usage), nil
		case <-time.After(time.Second):
			if req.IsToolIntentProbe() {
				return Reply{Content: "No tool call was intended; the answer stands."}, nil
			}
			return Reply{ToolCalls: []ToolCall{{Name: DefaultToolName, Args: map[string]any{"hostname": "localhost", "record_type": "A"}}}}, nil
		}
	case req.IsAutoRunPassOver(compBudget):
		// The pass's first reply reports more usage than the run's ceiling, so
		// the loop stops it there; the report call that follows is funded from
		// the reserve and gets plain text, like any tools-absent call.
		if req.HasTools() && !req.HasToolResult() && !req.IsToolIntentProbe() {
			return Reply{
				ToolCalls: []ToolCall{{Name: DefaultToolName, Args: map[string]any{"hostname": "localhost", "record_type": "A"}}},
				Usage:     Usage{PromptTokens: tokenCeiling + 1000, CompletionTokens: 10},
			}, nil
		}
		return passScript(req, nil, "Reporting from the reserve.\n\n"+findingsBlock("[]"), usage), nil
	case req.IsAutoRunPassOver(compSched):
		return passScript(req, nil, empty, usage), nil
	default:
		// Not a run: the /tasks turn the authority scenario posts.
		return DefaultScript(call, req)
	}
}

// newGitRepo creates a repository with one commit in dir and returns its
// file:// URL. The git adapter clones it into ~/.joe/repos (joe resolves its
// home through user.Current, so the test cannot redirect it); the clone is
// removed when the test ends.
func newGitRepo(t *testing.T, dir string) string {
	t.Helper()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("e2e\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("README.md"); err != nil {
		t.Fatal(err)
	}
	sig := &object.Signature{Name: "e2e", Email: "e2e@example.invalid", When: time.Now()}
	if _, err := wt.Commit("e2e: one commit", &gogit.CommitOptions{Author: sig}); err != nil {
		t.Fatal(err)
	}
	url := "file://" + dir
	t.Cleanup(func() {
		u, err := user.Current()
		if err != nil {
			return
		}
		// The adapter's clone path: ~/.joe/repos/<first 16 hex of sha256(url)>.
		os.RemoveAll(filepath.Join(u.HomeDir, ".joe", "repos", fmt.Sprintf("%x", sha256.Sum256([]byte(url)))[:16]))
	})
	return url
}

// autoRunE2E is the state the scenarios share.
type autoRunE2E struct {
	t       *testing.T
	h       *JoeTestHarness
	scripts *autoRunScripts
}

// call sends one request as key and decodes a 2xx body into out (when out is
// non-nil), failing the test unless the status is want.
func (e *autoRunE2E) call(t *testing.T, method, path, key string, body any, want int, out any) {
	t.Helper()
	status, raw, err := e.h.Do(method, path, key, body)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	if status != want {
		t.Fatalf("%s %s: status %d, want %d: %s", method, path, status, want, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s %s: decode %s: %v", method, path, raw, err)
		}
	}
}

func (e *autoRunE2E) admin(t *testing.T, method, path string, body any, want int, out any) {
	t.Helper()
	e.call(t, method, path, e2eServiceAccountKey, body, want, out)
}

func scope(components ...string) map[string]any {
	return map[string]any{"scope": map[string]any{"components": components}}
}

// trigger starts a run as the admin and returns its record as accepted.
func (e *autoRunE2E) trigger(t *testing.T, components ...string) autoRun {
	t.Helper()
	var run autoRun
	e.admin(t, http.MethodPost, "/api/v1/auto/runs", scope(components...), http.StatusAccepted, &run)
	if run.ID == "" || run.State != "running" {
		t.Fatalf("trigger returned %+v, want a running record with an id", run)
	}
	return run
}

// finished waits for a run to leave running AND for its one assistant turn to
// be in its session. The service writes the record before the turn, so a
// record reading completed is not yet a delivered run; waiting on the record
// alone is the race the unit test's flake was (see the thread's report).
func (e *autoRunE2E) finished(t *testing.T, id string, within time.Duration) (autoRun, string) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		var run autoRun
		e.admin(t, http.MethodGet, "/api/v1/auto/runs/"+id, nil, http.StatusOK, &run)
		if run.State != "running" && run.SessionID != "" {
			var msgs struct {
				Count    int `json:"count"`
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			e.admin(t, http.MethodGet, "/api/v1/sessions/"+run.SessionID+"/messages", nil, http.StatusOK, &msgs)
			if msgs.Count > 0 {
				if msgs.Count != 1 || msgs.Messages[0].Role != "assistant" {
					t.Fatalf("run %s session carries %d messages (%+v), want one assistant turn", id, msgs.Count, msgs.Messages)
				}
				return run, msgs.Messages[0].Content
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("run %s did not finish and deliver its turn within %s", id, within)
	return autoRun{}, ""
}

func (e *autoRunE2E) findingsOf(t *testing.T, runID string) runFindings {
	t.Helper()
	var out runFindings
	e.admin(t, http.MethodGet, "/api/v1/run-findings?run_id="+runID, nil, http.StatusOK, &out)
	return out
}

// setUp registers the components and grants svc:agent:core the read of the
// repository, all over the admin API.
func (e *autoRunE2E) setUp(t *testing.T, repoURL string) {
	t.Helper()
	e.admin(t, http.MethodPost, "/api/v1/components",
		map[string]any{"id": compRepo, "type": "git", "name": compRepo, "config": map[string]any{"url": repoURL}},
		http.StatusCreated, nil)
	for _, id := range []string{compIdle, compHold, compBudget, compSched, compAuthz} {
		e.admin(t, http.MethodPost, "/api/v1/components",
			map[string]any{"id": id, "type": "kubernetes", "name": id}, http.StatusCreated, nil)
	}
	// Armed with the explicit no-credential kind: the clone is anonymous.
	// Promotion brings the adapter up, so git_log can read it at once.
	e.admin(t, http.MethodPost, "/api/v1/components/"+compRepo+"/promote",
		map[string]any{"credential_provider": "none"}, http.StatusOK, nil)
	// git is excluded from auto_promote_reads, so svc:agent:core reads it on a
	// grant: a read zone holding the repository, and a policy on the zone.
	var zone struct {
		ID string `json:"id"`
	}
	e.admin(t, http.MethodPost, "/api/v1/admin/zones",
		map[string]any{"id": "e2e-read", "name": "e2e-read", "allowed_actions": []string{"read"}}, http.StatusCreated, &zone)
	e.admin(t, http.MethodPost, "/api/v1/admin/component-zones",
		map[string]any{"component_id": compRepo, "zone_id": zone.ID, "assigned_by": "svc:e2e", "reason": "e2e: the repository the runs read"}, http.StatusOK, nil)
	// The findings pipeline reads a finding's subject node through the run's
	// accessor, which authorizes every graph read against the reserved
	// component id "graph" (access.GraphComponentID). That id is no component
	// row, so it cannot be assigned to a zone (the assignment's foreign key
	// refuses it) and always resolves to "unassigned"; a grant on the
	// repository's zone does not reach it. The one grant that does is a policy
	// on "unassigned", which also admits every unassigned component — broader
	// than a run needs, and nothing in joe makes it by default. Without it
	// every pipeline call under enforced RBAC fails no_grant and no finding is
	// persisted (see the thread's report).
	e.admin(t, http.MethodPost, "/api/v1/admin/policies",
		map[string]any{"principal": "svc:agent:core", "zone_id": "unassigned"}, http.StatusCreated, nil)
	e.admin(t, http.MethodPost, "/api/v1/admin/policies",
		map[string]any{"principal": "svc:agent:core", "zone_id": zone.ID}, http.StatusCreated, nil)
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// TestE2E_AutoRun drives the read-only Auto Joe over its public API: a run end
// to end, findings persisted and labelled across three runs, one live run per
// scope, the budget seam, a schedule, and the run's authority, with RBAC
// enforced throughout (configuring service accounts enforces it).
func TestE2E_AutoRun(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	h := NewTestHarness(t,
		WithServiceAccount("reader", readerKey),
		// The refresher never ticks at boot, so its interval is how long the
		// graph takes to hold the repository's node.
		WithRefreshIntervalMinutes(1),
		WithConfigYAML(fmt.Sprintf("auto_run:\n  token_ceiling: %d\n", tokenCeiling)),
	)
	defer h.Stop()
	if err := h.BootstrapAdmin("svc:e2e"); err != nil {
		t.Fatal(err)
	}
	repoURL := newGitRepo(t, filepath.Join(h.TmpDir(), "repo"))
	scripts := newAutoRunScripts()
	defer scripts.releaseHold()
	h.Responder.SetScript(scripts.script)
	if err := h.Start(); err != nil {
		t.Fatalf("failed to start harness: %v", err)
	}
	booted := time.Now()

	e := &autoRunE2E{t: t, h: h, scripts: scripts}
	e.setUp(t, repoURL)

	// 5 and 8, first halves. Both schedules are installed now so their
	// intervals run while the other scenarios do: one on a scope nothing else
	// touches, which fires; one on the held scope, which comes due while the
	// held run is live and is refused.
	type schedule struct {
		ID          string `json:"id"`
		InstalledBy string `json:"installed_by"`
		NextRunAt   string `json:"next_run_at"`
		LastRunAt   string `json:"last_run_at"`
		LastOutcome string `json:"last_outcome"`
	}
	var sched, holdSched schedule
	e.admin(t, http.MethodPost, "/api/v1/auto/schedules",
		map[string]any{"scope": map[string]any{"components": []string{compSched}}, "interval_minutes": 1},
		http.StatusCreated, &sched)
	if sched.ID == "" || sched.InstalledBy != "svc:e2e" || sched.NextRunAt == "" {
		t.Fatalf("installed schedule = %+v, want an id, installed_by svc:e2e and a due time", sched)
	}
	e.admin(t, http.MethodPost, "/api/v1/auto/schedules",
		map[string]any{"scope": map[string]any{"components": []string{compHold}}, "interval_minutes": 1},
		http.StatusCreated, &holdSched)
	holdInstalled := time.Now()

	// Run ids the later scenarios read back: scenario 1's completed run and
	// scenario 4's budget-stopped run (read by 7), and scenario 3's held run
	// (released by 8).
	var completedRun, budgetRun, heldRun string

	// The scenarios run in this order, not by number: those that need nothing
	// of joe's cadence first (1, 3, 4, 6, then 7, which reads 1's and 4's
	// records back), then 2, which waits for the refresher's first tick, then
	// 8 and 5, which wait for the scheduler's — so the suite pays each wait
	// once, while the others run.
	t.Run("1_run_end_to_end", func(t *testing.T) {
		started := e.trigger(t, compRepo, compIdle)
		if started.Activator != "svc:e2e" || started.Principal != "svc:agent:core" {
			t.Errorf("activator %q principal %q, want svc:e2e and svc:agent:core", started.Activator, started.Principal)
		}
		run, turn := e.finished(t, started.ID, 30*time.Second)
		if run.State != "completed" || run.Error != "" {
			t.Fatalf("run = %+v, want completed without error", run)
		}
		completedRun = run.ID
		if strings.Join(run.ResolvedComponents, ",") != compIdle+","+compRepo {
			t.Errorf("resolved_components = %v, want [%s %s]", run.ResolvedComponents, compIdle, compRepo)
		}
		if run.TokenCeiling != tokenCeiling || run.WallClockSeconds <= 0 || run.ReportReserveTokens <= 0 {
			t.Errorf("budget on the record: ceiling %d wall clock %ds reserve %d", run.TokenCeiling, run.WallClockSeconds, run.ReportReserveTokens)
		}
		// The pass answered on its own: the loop's natural stop, no ceiling.
		if run.StopReason == "token_ceiling" || run.StopReason == "wall_clock" || run.StopReason == "error" {
			t.Errorf("stop_reason = %q, want the loop's natural stop", run.StopReason)
		}
		// Coverage is derived from the calls: git_log read the repository, and
		// nothing read the idle component.
		if strings.Join(run.Covered, ",") != compRepo || strings.Join(run.NotCovered, ",") != compIdle {
			t.Errorf("covered %v not_covered %v, want [%s] and [%s]", run.Covered, run.NotCovered, compRepo, compIdle)
		}
		if run.TokensUsed <= 0 {
			t.Errorf("tokens_used = %d, want the responder's usage counted", run.TokensUsed)
		}

		// The session it wrote: created by svc:agent:core, readable by a
		// grantless non-admin as any team-public session is, one assistant turn
		// carrying the limits section.
		var sess struct {
			ID               string `json:"id"`
			CreatorPrincipal string `json:"creator_principal"`
		}
		e.admin(t, http.MethodGet, "/api/v1/admin/sessions/"+run.SessionID, nil, http.StatusOK, &sess)
		if sess.CreatorPrincipal != "svc:agent:core" {
			t.Errorf("session creator = %q, want svc:agent:core", sess.CreatorPrincipal)
		}
		var asReader struct {
			ID       string `json:"id"`
			ReadOnly bool   `json:"read_only"`
		}
		e.call(t, http.MethodGet, "/api/v1/sessions/"+run.SessionID, readerKey, nil, http.StatusOK, &asReader)
		if asReader.ID != run.SessionID || !asReader.ReadOnly {
			t.Errorf("as svc:reader: %+v, want the session, read-only", asReader)
		}
		for _, want := range []string{"## Limits", "Covered (1): " + compRepo, "Not looked at (1): " + compIdle, "svc:agent:core"} {
			if !strings.Contains(turn, want) {
				t.Errorf("the assistant turn lacks %q:\n%s", want, turn)
			}
		}
	})

	t.Run("3_one_live_run_per_scope", func(t *testing.T) {
		first := e.trigger(t, compHold)
		select {
		case <-scripts.holdReached:
		case <-time.After(20 * time.Second):
			t.Fatal("the held run never reached the responder")
		}
		status, raw, err := h.Do(http.MethodPost, "/api/v1/auto/runs", e2eServiceAccountKey, scope(compHold))
		if err != nil {
			t.Fatal(err)
		}
		if status != http.StatusConflict {
			t.Fatalf("second trigger on a live scope: status %d, want 409: %s", status, raw)
		}
		var live autoRun
		e.admin(t, http.MethodGet, "/api/v1/auto/runs/"+first.ID, nil, http.StatusOK, &live)
		if live.State != "running" {
			t.Fatalf("the first run is %q while held, want running", live.State)
		}
		heldRun = first.ID
		// The hold is kept: scenario 8 needs the scope live when the schedule
		// on it comes due, and releases it.
	})

	t.Run("4_budget_seam", func(t *testing.T) {
		// Driven by the token ceiling: the responder's usage numbers exceed it.
		started := e.trigger(t, compBudget)
		run, turn := e.finished(t, started.ID, 30*time.Second)
		if run.State != "completed" || run.StopReason != "token_ceiling" {
			t.Fatalf("run = %+v, want completed with stop_reason token_ceiling", run)
		}
		budgetRun = run.ID
		if run.TokensUsed < tokenCeiling {
			t.Errorf("tokens_used = %d, want at least the ceiling %d", run.TokensUsed, tokenCeiling)
		}
		if run.ReportTokens <= 0 {
			t.Errorf("report_tokens = %d, want the report's spend recorded apart", run.ReportTokens)
		}
		if len(run.Covered) != 0 || !contains(run.NotCovered, compBudget) {
			t.Errorf("covered %v not_covered %v, want nothing covered", run.Covered, run.NotCovered)
		}
		for _, want := range []string{"## Limits", "token_ceiling", "Not looked at (1): " + compBudget, "not a clean bill", "Reporting from the reserve."} {
			if !strings.Contains(turn, want) {
				t.Errorf("the assistant turn lacks %q:\n%s", want, turn)
			}
		}
	})

	t.Run("6_authority", func(t *testing.T) {
		// A non-admin cannot trigger, install a schedule, or read admissions.
		for _, c := range []struct {
			method, path string
			body         any
		}{
			{http.MethodPost, "/api/v1/auto/runs", scope(compAuthz)},
			{http.MethodPost, "/api/v1/auto/schedules", map[string]any{"scope": map[string]any{"components": []string{compAuthz}}, "interval_minutes": 1}},
			{http.MethodGet, "/api/v1/run-findings/admissions", nil},
		} {
			e.call(t, c.method, c.path, readerKey, c.body, http.StatusForbidden, nil)
		}

		var before, after struct {
			Count int `json:"count"`
		}
		e.admin(t, http.MethodGet, "/api/v1/auto/runs", nil, http.StatusOK, &before)
		if _, err := h.RunTask("Resolve localhost."); err != nil {
			t.Fatalf("POST /api/v1/tasks: %v", err)
		}
		e.admin(t, http.MethodGet, "/api/v1/auto/runs", nil, http.StatusOK, &after)
		if after.Count != before.Count {
			t.Errorf("POST /api/v1/tasks changed the run count %d → %d; a task is not a run", before.Count, after.Count)
		}

		// A run triggered by svc:e2e reads as svc:agent:core: every accessor
		// decision recorded from its trigger to its delivered turn is under
		// that principal. audit_log has no HTTP read surface, so it is read
		// from joe's database file, read-only. Nothing else in this window
		// reads a component as a person: the window is the run's, the
		// refresher's and the schedule's, all svc:agent:core.
		db, err := sql.Open("sqlite", "file:"+h.DBPath()+"?mode=ro")
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		var mark int64
		if err := db.QueryRow(`SELECT COALESCE(MAX(id), 0) FROM audit_log`).Scan(&mark); err != nil {
			t.Fatal(err)
		}
		started := e.trigger(t, compRepo, compAuthz)
		run, _ := e.finished(t, started.ID, 30*time.Second)
		if run.State != "completed" || !contains(run.Covered, compRepo) {
			t.Fatalf("run = %+v, want completed with %s covered", run, compRepo)
		}
		rows, err := db.Query(`SELECT COALESCE(principal, ''), decision, COALESCE(component_id, '') FROM audit_log
			WHERE kind = 'infra_access' AND id > ?`, mark)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		n := 0
		var readRepo bool
		for rows.Next() {
			var principal, decision, component string
			if err := rows.Scan(&principal, &decision, &component); err != nil {
				t.Fatal(err)
			}
			n++
			if principal != "svc:agent:core" {
				t.Errorf("accessor decision %s on %q recorded under %q, want svc:agent:core", decision, component, principal)
			}
			if component == compRepo && decision == "allow" {
				readRepo = true
			}
		}
		if n == 0 || !readRepo {
			t.Errorf("%d accessor decisions since the trigger, readRepo=%v; want the run's read of %s recorded", n, readRepo, compRepo)
		}
	})

	t.Run("7_what_the_page_reads_is_served", func(t *testing.T) {
		// The Auto Joe page renders the record and derives nothing
		// (joe-pm threads/autojoe-ro-run-visibility): every field a run
		// reading or a schedule reading shows is a field these reads serve,
		// by name, next-due included. Checked on the raw JSON, on a completed
		// run (scenario 1's) and on the installed schedules.
		var runs struct {
			Runs []map[string]any `json:"runs"`
		}
		e.admin(t, http.MethodGet, "/api/v1/auto/runs", nil, http.StatusOK, &runs)
		var listed map[string]any
		for _, r := range runs.Runs {
			if r["id"] == completedRun {
				listed = r
			}
		}
		if listed == nil {
			t.Fatalf("the list does not carry run %s", completedRun)
		}
		var single map[string]any
		e.admin(t, http.MethodGet, "/api/v1/auto/runs/"+completedRun, nil, http.StatusOK, &single)
		runFields := []string{
			"id", "activator", "principal", "resolved_components", "state",
			"covered", "not_covered", "token_ceiling", "wall_clock_seconds",
			"report_reserve_tokens", "tokens_used", "report_tokens",
			"started_at", "ended_at", "session_id",
		}
		for name, read := range map[string]map[string]any{"list": listed, "single": single} {
			for _, f := range runFields {
				if _, ok := read[f]; !ok {
					t.Errorf("the %s read of a completed run lacks %q: %v", name, f, read)
				}
			}
			// finding_count is served where a set exists; scenario 1's run
			// reported an empty block, so its set exists with 0 findings.
			if n, ok := read["finding_count"]; !ok || n != float64(0) {
				t.Errorf("the %s read of a run with a finding set carries finding_count %v (present %v), want 0", name, n, ok)
			}
			if read["principal"] != "svc:agent:core" || read["state"] != "completed" {
				t.Errorf("the %s read: principal %v state %v", name, read["principal"], read["state"])
			}
		}
		// stop_reason and error are omitted when empty and present
		// otherwise: the budget run's record carries its ceiling as the
		// stop reason.
		var budgetRead map[string]any
		e.admin(t, http.MethodGet, "/api/v1/auto/runs/"+budgetRun, nil, http.StatusOK, &budgetRead)
		if budgetRead["stop_reason"] != "token_ceiling" {
			t.Errorf("the budget run's read carries stop_reason %v, want token_ceiling", budgetRead["stop_reason"])
		}

		var schedules struct {
			Schedules []map[string]any `json:"schedules"`
		}
		e.admin(t, http.MethodGet, "/api/v1/auto/schedules", nil, http.StatusOK, &schedules)
		if len(schedules.Schedules) != 2 {
			t.Fatalf("%d schedules listed, want the two installed", len(schedules.Schedules))
		}
		for _, sch := range schedules.Schedules {
			for _, f := range []string{
				"id", "selection", "interval_seconds", "token_ceiling", "wall_clock_seconds",
				"report_reserve_tokens", "installed_by", "installed_at", "next_run_at",
			} {
				if _, ok := sch[f]; !ok {
					t.Errorf("the schedule list read lacks %q: %v", f, sch)
				}
			}
			if sch["installed_by"] != "svc:e2e" {
				t.Errorf("schedule installed_by = %v, want svc:e2e", sch["installed_by"])
			}
		}
	})

	t.Run("2_findings_persisted_and_labelled", func(t *testing.T) {
		// The finding is about the repository's node, which exists once the
		// Core Agent has refreshed the repository.
		deadline := booted.Add(cadenceWait)
		for {
			var g struct {
				Nodes []struct {
					ID string `json:"id"`
				} `json:"nodes"`
			}
			e.admin(t, http.MethodGet, "/api/v1/graph", nil, http.StatusOK, &g)
			found := false
			for _, n := range g.Nodes {
				found = found || n.ID == repoNodeID
			}
			if found {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("node %s not in the graph %s after boot", repoNodeID, cadenceWait)
			}
			time.Sleep(time.Second)
		}

		passes := []struct {
			block string
			label string
		}{
			{findingsBlock(theFinding), "new"},
			{findingsBlock(theFinding), "standing"},
			// The block is present and the finding is not in it: a missing
			// block is an error the pipeline persists nothing for, so "absent"
			// is the finding's absence from an empty set.
			{findingsBlock("[]"), "resolved"},
		}
		for i, p := range passes {
			scripts.setFindings(p.block)
			started := e.trigger(t, compRepo)
			run, turn := e.finished(t, started.ID, 30*time.Second)
			if run.State != "completed" || !contains(run.Covered, compRepo) {
				t.Fatalf("pass %d: run = %+v, want completed with %s covered", i+1, run, compRepo)
			}
			got := e.findingsOf(t, run.ID)
			if got.Count != 1 || len(got.Findings) != 1 {
				t.Fatalf("pass %d: %d findings %+v, want one", i+1, got.Count, got.Findings)
			}
			f := got.Findings[0]
			if f.Label != p.label || f.SubjectNodeID != repoNodeID || f.SubjectComponentID != compRepo || f.RunID != run.ID {
				t.Errorf("pass %d: finding %+v, want label %s on %s", i+1, f, p.label, repoNodeID)
			}
			// A risk that would make its subject unavailable, with no
			// dependents and the default weight, stays at its cell's base band.
			if f.Band != "P2" || f.Derivation.BaseBand != "P2" || f.Derivation.Band != "P2" ||
				f.Derivation.EvidenceLine != "git_log: 1 commit on HEAD" {
				t.Errorf("pass %d: band %q derivation %+v, want P2 from the evidence line", i+1, f.Band, f.Derivation)
			}
			if !strings.Contains(turn, "## Findings") || !strings.Contains(turn, "["+p.label+"]") {
				t.Errorf("pass %d: the assistant turn lacks the findings section labelled %s:\n%s", i+1, p.label, turn)
			}
		}
	})

	t.Run("8_refused_fire_is_visible_on_the_schedule", func(t *testing.T) {
		// With scenario 3's run held live on the scope, the schedule on the
		// same scope comes due and is refused: the refusal is written on the
		// schedule as its last outcome, no run row is written for it, and the
		// held run completes once released (joe-pm
		// threads/autojoe-ro-run-visibility § Scenarios owed, 2).
		if heldRun == "" {
			t.Fatal("scenario 3 left no held run")
		}
		var list struct {
			Schedules []schedule `json:"schedules"`
		}
		deadline := holdInstalled.Add(cadenceWait)
		var refused *schedule
		for refused == nil {
			e.admin(t, http.MethodGet, "/api/v1/auto/schedules", nil, http.StatusOK, &list)
			for i := range list.Schedules {
				if list.Schedules[i].ID == holdSched.ID && list.Schedules[i].LastOutcome != "" {
					refused = &list.Schedules[i]
				}
			}
			if refused == nil {
				if time.Now().After(deadline) {
					t.Fatalf("schedule %s on the held scope has no outcome within %s of install", holdSched.ID, cadenceWait)
				}
				time.Sleep(time.Second)
			}
		}
		if !strings.HasPrefix(refused.LastOutcome, "refused:") || refused.LastRunAt == "" {
			t.Errorf("schedule on the held scope = %+v, want last_outcome refused: ... and a last-fired time", refused)
		}
		if refused.NextRunAt <= holdSched.NextRunAt {
			t.Errorf("next_run_at %s did not advance past %s after the refused fire", refused.NextRunAt, holdSched.NextRunAt)
		}
		// Removed before release, so it fires no run on the scope after the
		// held one completes.
		e.admin(t, http.MethodDelete, "/api/v1/auto/schedules/"+holdSched.ID, nil, http.StatusNoContent, nil)

		var live autoRun
		e.admin(t, http.MethodGet, "/api/v1/auto/runs/"+heldRun, nil, http.StatusOK, &live)
		if live.State != "running" {
			t.Fatalf("the held run is %q at the refusal, want running", live.State)
		}
		scripts.releaseHold()
		run, _ := e.finished(t, heldRun, 30*time.Second)
		if run.State != "completed" {
			t.Errorf("the held run, released, = %+v, want completed", run)
		}
		// Neither the refused fire nor scenario 3's refused trigger queued
		// anything: one run on the scope, and none activated by the schedule.
		var runs struct {
			Runs []autoRun `json:"runs"`
		}
		e.admin(t, http.MethodGet, "/api/v1/auto/runs", nil, http.StatusOK, &runs)
		n := 0
		for _, r := range runs.Runs {
			if contains(r.ResolvedComponents, compHold) {
				n++
			}
			if r.Activator == "schedule:"+holdSched.ID {
				t.Errorf("a run %s was written for the refused fire", r.ID)
			}
		}
		if n != 1 {
			t.Errorf("%d runs on the held scope, want 1", n)
		}
	})

	t.Run("5_schedule_fires_and_is_removed", func(t *testing.T) {
		var list struct {
			Schedules []schedule `json:"schedules"`
		}
		e.admin(t, http.MethodGet, "/api/v1/auto/schedules", nil, http.StatusOK, &list)
		if len(list.Schedules) != 1 || list.Schedules[0].ID != sched.ID {
			t.Fatalf("schedules = %+v, want the one installed on %s", list.Schedules, compSched)
		}

		activator := "schedule:" + sched.ID
		deadline := booted.Add(cadenceWait)
		var fired *autoRun
		for fired == nil {
			var runs struct {
				Runs []autoRun `json:"runs"`
			}
			e.admin(t, http.MethodGet, "/api/v1/auto/runs", nil, http.StatusOK, &runs)
			for i := range runs.Runs {
				if runs.Runs[i].Activator == activator {
					fired = &runs.Runs[i]
				}
			}
			if fired == nil {
				if time.Now().After(deadline) {
					t.Fatalf("no run with activator %s within %s of boot", activator, cadenceWait)
				}
				time.Sleep(time.Second)
			}
		}
		run, _ := e.finished(t, fired.ID, 30*time.Second)
		if run.State != "completed" || run.Principal != "svc:agent:core" || strings.Join(run.ResolvedComponents, ",") != compSched {
			t.Errorf("scheduled run = %+v, want completed as svc:agent:core over %s", run, compSched)
		}
		e.admin(t, http.MethodGet, "/api/v1/auto/schedules", nil, http.StatusOK, &list)
		if len(list.Schedules) != 1 || list.Schedules[0].LastOutcome != "started "+run.ID {
			t.Errorf("schedule after firing = %+v, want last_outcome %q", list.Schedules, "started "+run.ID)
		}

		e.admin(t, http.MethodDelete, "/api/v1/auto/schedules/"+sched.ID, nil, http.StatusNoContent, nil)
		e.admin(t, http.MethodGet, "/api/v1/auto/schedules", nil, http.StatusOK, &list)
		if len(list.Schedules) != 0 {
			t.Errorf("schedules after removal = %+v, want none", list.Schedules)
		}
	})
}
