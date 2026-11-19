package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/jaimegago/joe/internal/agentloop"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/prompts"
)

// The agent identity read (D-0172): the part of what an external evaluator
// must declare about the agent it measured that the build identity does not
// carry. GET /version says which binary this is; GET /agent-identity says how that
// binary is configured to behave — a hash of the configuration that shapes
// what the agent does, a hash of the system prompt every task starts from,
// and the model and provider a task would run on if it were prepared now.
//
// It is a read in the strict sense. It takes no input, changes nothing, and
// writes no audit row: it reaches no managed system, so there is no act to
// record. It sits behind the same edge authentication as GET /version.
//
// Nothing here is a secret and nothing here is derived from one. The
// configuration hash is taken over an explicit list of fields, below, none of
// which is a credential; a hash over the whole configuration would have put
// service-account keys and the OIDC client secret under a digest, and a
// digest of a low-entropy secret is a thing to guess against.

// agentIdentityResponse is the body of GET /agent-identity.
type agentIdentityResponse struct {
	// ConfigHash is "sha256:" and the lowercase-hex digest of the canonical
	// JSON of agentIdentityConfiguration. Two processes report the same value
	// exactly when every field of that struct is equal.
	ConfigHash string `json:"config_hash"`
	// SystemPromptHash is "sha256:" and the lowercase-hex digest of the
	// system prompt every task of this process starts from: the task system
	// prompt and, where the write floor is up, the posture section — the
	// bytes buildTaskRun assembles before anything that varies per request.
	// The zone scope, the graph summary and the query-selected skills are
	// appended after it per request and are not in it.
	SystemPromptHash string `json:"system_prompt_hash"`
	// Model and Provider are the provider model identifier and the adapter
	// family a task would attest if it were prepared now (D-0153): the same
	// resolution, so this value and a task response's `model` are comparable.
	// Each is omitted when nothing resolves, never invented.
	Model    string `json:"model,omitempty"`
	Provider string `json:"provider,omitempty"`
}

// agentIdentityConfiguration is exactly what ConfigHash covers: the configuration
// that shapes what the agent does with a task.
//
// It is an allow-list on purpose. What is left out is where the process
// listens and stores (server address, TLS, rate limits, database, logging,
// archive directory), every credential, the cost and runaway limits held in
// the database, and the registered components — two deployments that differ
// only in those are the same agent configuration by this hash, and a reader
// who needs one of them pinned must pin it separately. Adding a field here
// changes the hash of every deployment, which is the point of adding it.
//
// The JSON encoding is the canonical form: fields in declaration order, no
// omitted keys, slices in configured order. An external process holding the
// same values reproduces the digest byte for byte.
type agentIdentityConfiguration struct {
	LLM                   agentIdentityLLM       `json:"llm"`
	Posture               string                 `json:"posture"`
	RBACEnabled           bool                   `json:"rbac_enabled"`
	Skills                agentIdentitySkills    `json:"skills"`
	WebSearch             agentIdentityWebSearch `json:"web_search"`
	AutoRun               agentIdentityAutoRun   `json:"auto_run"`
	ContextBudgetFraction float64                `json:"context_budget_fraction"`
}

// agentIdentityLLM is the active model: the catalogue key and the entry it names.
type agentIdentityLLM struct {
	Current  string `json:"current"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	BaseURL  string `json:"base_url"`
}

type agentIdentitySkills struct {
	TrustedSources    []string `json:"trusted_sources"`
	HotReloadDisabled bool     `json:"hot_reload_disabled"`
}

type agentIdentityWebSearch struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"base_url"`
}

type agentIdentityAutoRun struct {
	TokenCeiling         int `json:"token_ceiling"`
	WallClockMinutes     int `json:"wall_clock_minutes"`
	ReportReserveTokens  int `json:"report_reserve_tokens"`
	ReportReserveSeconds int `json:"report_reserve_seconds"`
}

// agentIdentityConfiguration assembles what the configuration hash covers from the
// same values the task path reads.
func (s *Server) agentIdentityConfiguration() agentIdentityConfiguration {
	out := agentIdentityConfiguration{
		// "" is full mode; "observation" and "safe_mode" are the two reasons
		// the write floor is up.
		Posture:               string(s.services.WriteFloor.Reason()),
		Skills:                agentIdentitySkills{TrustedSources: []string{}},
		ContextBudgetFraction: agentloop.DefaultContextBudgetFraction,
	}
	if s.services.ContextBudgetProvider != nil {
		out.ContextBudgetFraction = s.services.ContextBudgetProvider.BudgetFraction()
	}

	cfg := s.services.Config
	if cfg == nil {
		return out
	}
	_, activeKey, _, _ := s.resolveActiveModel()
	out.LLM.Current = activeKey
	if mc, ok := cfg.LLM.Available[activeKey]; ok {
		out.LLM.Provider, out.LLM.Model, out.LLM.BaseURL = mc.Provider, mc.Model, mc.BaseURL
	}
	out.RBACEnabled = cfg.RBACEnabled()
	if len(cfg.Skills.TrustedSources) > 0 {
		out.Skills.TrustedSources = append(out.Skills.TrustedSources, cfg.Skills.TrustedSources...)
	}
	out.Skills.HotReloadDisabled = cfg.Skills.HotReloadDisabled
	out.WebSearch = agentIdentityWebSearch{Provider: cfg.WebSearch.Provider, BaseURL: cfg.WebSearch.BaseURL}
	out.AutoRun = agentIdentityAutoRun{
		TokenCeiling:         cfg.AutoRun.TokenCeiling,
		WallClockMinutes:     cfg.AutoRun.WallClockMinutes,
		ReportReserveTokens:  cfg.AutoRun.ReportReserveTokens,
		ReportReserveSeconds: cfg.AutoRun.ReportReserveSeconds,
	}
	return out
}

// taskSystemPrefix is the system prompt every task of this process starts
// from, assembled the way buildTaskRun assembles it: the task system prompt,
// then the posture section. Both are constant for the life of the process.
func (s *Server) taskSystemPrefix() string {
	return llm.StaticSystem(prompts.TaskSystem).
		Append(prompts.PostureSection(s.services.WriteFloor.Reason()), false).
		String()
}

// sha256Hex renders a digest as "sha256:<lowercase hex>".
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (s *Server) handleAgentIdentity(w http.ResponseWriter, r *http.Request) {
	canonical, err := json.Marshal(s.agentIdentityConfiguration())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "identity_unavailable", "could not encode the configuration the identity hash covers")
		return
	}
	_, _, model, provider := s.resolveActiveModel()
	writeJSON(w, http.StatusOK, agentIdentityResponse{
		ConfigHash:       sha256Hex(canonical),
		SystemPromptHash: sha256Hex([]byte(s.taskSystemPrefix())),
		Model:            model,
		Provider:         provider,
	})
}
