package api

import (
	"fmt"
	"net/http"

	"github.com/jaimegago/joe/internal/runfindings"
)

// runFindingsHandler exposes the typed finding records a read-only Auto Joe
// run produces (internal/runfindings, docs/backlog/autojoe-ro.md § Delivery).
// It is the endpoint of its own the findings view reads — a dashboard of data
// backed by real records, in place of the alerts widget D-0038 retired.
//
// Two reads and nothing else: the records are written by a run through the
// repository and reconciled by the next run, never edited through HTTP.
//
//   - GET /run-findings            — the latest run's findings on every scope,
//     by band, with their labels; or one run's with ?run_id=.
//   - GET /run-findings/admissions — the proposed-admissions list: graph
//     updates from the latest runs, addressed to admins and gated to them,
//     because the action they propose (registering a component) is an
//     admission only an admin can perform (D-0029).
//
// Authorization: the first read carries the same floor as the sessions these
// findings are rendered into — team-public (§12.1): any authenticated
// principal who can read the run's session can read its records. Nothing
// here is an input to any authorization decision (D-0167).
type runFindingsHandler struct {
	server *Server
	repo   runfindings.Repository
}

func (s *Server) registerRunFindingsRoutes(mux *http.ServeMux, prefix string) {
	if s.services == nil || s.services.RunFindings == nil {
		return
	}
	h := &runFindingsHandler{server: s, repo: s.services.RunFindings}
	mux.HandleFunc(fmt.Sprintf("GET %s/run-findings", prefix), h.list)
	mux.HandleFunc(fmt.Sprintf("GET %s/run-findings/admissions", prefix), h.admissions)
}

// runFindingsResponse is the list body: the finding sets the findings belong to
// (one per run, so a clean run is visible as a set with no findings) and the
// findings in delivery order.
type runFindingsResponse struct {
	Runs     []runfindings.FindingSet `json:"runs"`
	Findings []runfindings.Finding    `json:"findings"`
	Count    int                      `json:"count"`
}

func (h *runFindingsHandler) list(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if runID := r.URL.Query().Get("run_id"); runID != "" {
		set, err := h.repo.GetSet(ctx, runID)
		if err != nil {
			writeInternalError(w, err, "get run finding set")
			return
		}
		if set == nil {
			writeError(w, http.StatusNotFound, errorCodeNotFound, "run has no finding set")
			return
		}
		findings, err := h.repo.ListByRun(ctx, runID)
		if err != nil {
			writeInternalError(w, err, "list run findings")
			return
		}
		if findings == nil {
			findings = []runfindings.Finding{}
		}
		writeJSON(w, http.StatusOK, runFindingsResponse{Runs: []runfindings.FindingSet{*set}, Findings: findings, Count: len(findings)})
		return
	}
	sets, findings, err := h.repo.ListLatest(ctx)
	if err != nil {
		writeInternalError(w, err, "list latest run findings")
		return
	}
	writeJSON(w, http.StatusOK, runFindingsResponse{Runs: sets, Findings: findings, Count: len(findings)})
}

// admissions returns the graph-update findings of the latest run on every
// scope — the proposed-admissions list — for admins. Resolved and unknown
// rows are excluded: a proposal to register something the run no longer sees
// evidence of is not a proposal.
func (h *runFindingsHandler) admissions(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	sets, findings, err := h.repo.ListLatest(r.Context())
	if err != nil {
		writeInternalError(w, err, "list proposed admissions")
		return
	}
	out := make([]runfindings.Finding, 0, len(findings))
	for _, f := range findings {
		if f.Type != runfindings.TypeGraphUpdate {
			continue
		}
		if f.Label != runfindings.LabelNew && f.Label != runfindings.LabelStanding {
			continue
		}
		out = append(out, f)
	}
	writeJSON(w, http.StatusOK, runFindingsResponse{Runs: sets, Findings: out, Count: len(out)})
}
