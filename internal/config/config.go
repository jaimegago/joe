package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/jaimegago/joe/internal/paths"
	"gopkg.in/yaml.v3"
)

// Config represents the Joe configuration
type Config struct {
	LLM           LLMConfig          `yaml:"llm"`
	Server        ServerConfig       `yaml:"server"`
	Refresh       RefreshConfig      `yaml:"refresh"`
	Notifications NotificationConfig `yaml:"notifications"`
	Logging       LoggingConfig      `yaml:"logging"`
	Knowledge     KnowledgeConfig    `yaml:"knowledge"`
	Database      DatabaseConfig     `yaml:"database"`
	Skills        SkillsConfig       `yaml:"skills"`
	Auth          AuthConfig         `yaml:"auth"`

	// explicitProvider records whether the user expressed an explicit LLM
	// provider preference during Load — either JOE_LLM_PROVIDER or an
	// llm.current set in the config file. AutoSelectProvider leaves the
	// configuration untouched when this is true. Unexported so it is never
	// serialized by Save.
	explicitProvider bool
}

// SkillsConfig governs the Agent Skills consumer (~/.joe/skills/). Phase 3
// adds hot reload and a trusted-source allowlist; the full policy file with
// quarantine + signing arrives in Phase 4.
type SkillsConfig struct {
	// TrustedSources, when non-empty, restricts `joe skills install` to git
	// URLs whose host (or host + owner prefix) matches one of the listed
	// entries. Empty disables the allowlist entirely.
	TrustedSources []string `yaml:"trusted_sources"`
	// HotReloadDisabled turns off the filesystem watcher. Default (zero
	// value) keeps hot reload on, since eliminating restart friction is the
	// whole point of Phase 3. Set to true to require explicit reloads via
	// `joe skills reload` / POST /api/v1/skills/reload.
	HotReloadDisabled bool `yaml:"hot_reload_disabled"`
}

// defaultAuthSessionTTL bounds a login session's lifetime when the operator
// sets no auth.session_ttl. Twelve hours covers a working day without forcing
// a mid-day re-login, while still expiring well within a day (design §2.3).
const defaultAuthSessionTTL = 12 * time.Hour

// AuthConfig configures human authentication (Identity Phase C, design §2.1–§2.9).
// Humans log in through a single configurable OIDC issuer; their verified email
// becomes a user:<email> principal carried by a server-side session cookie.
type AuthConfig struct {
	// OIDC holds the single configurable OpenID Connect issuer. When Issuer is
	// empty, OIDC login is disabled and the auth endpoints report 503; the only
	// remaining authentication mechanism is the server API key (service-account
	// keys arrive in Phase D).
	OIDC OIDCConfig `yaml:"oidc"`

	// AdminEmail is the bootstrap admin. The first principal whose verified
	// email matches this value is granted admin authority on first login
	// (design §2.9). This is the only bootstrap path. Empty disables the
	// bootstrap; no login ever auto-grants authority.
	AdminEmail string `yaml:"admin_email"`

	// SessionTTL bounds how long a minted session is accepted before re-login
	// is required (design §2.3 — sessions must not outlive the identity
	// assertion indefinitely). Defaults to defaultAuthSessionTTL when zero.
	SessionTTL time.Duration `yaml:"session_ttl"`

	// PostLoginRedirect is where the callback sends the browser after a
	// successful login. Defaults to "/" (the Web UI root) when empty.
	PostLoginRedirect string `yaml:"post_login_redirect"`
}

// OIDCConfig configures the single OpenID Connect issuer used for human login
// (design §2.1). Discovery, JWKS, and ID-token verification are handled by a
// maintained library (github.com/coreos/go-oidc/v3); the authorization-code
// flow with PKCE is handled by golang.org/x/oauth2.
type OIDCConfig struct {
	// Issuer is the OIDC issuer URL (e.g. https://accounts.google.com). Used
	// for OIDC discovery (.well-known/openid-configuration) and JWKS.
	Issuer string `yaml:"issuer"`
	// ClientID is the OAuth2 client identifier registered with the issuer.
	ClientID string `yaml:"client_id"`
	// ClientSecret is the OAuth2 client secret registered with the issuer.
	ClientSecret string `yaml:"client_secret"`
	// RedirectURL is the absolute callback URL registered with the issuer; it
	// must point at this server's /api/v1/auth/callback endpoint.
	RedirectURL string `yaml:"redirect_url"`
}

// Configured reports whether an OIDC issuer has been set.
func (o OIDCConfig) Configured() bool {
	return o.Issuer != "" && o.ClientID != "" && o.RedirectURL != ""
}

// DatabaseConfig selects the backing database driver and connection string.
// When Driver is empty, joecored defaults to SQLite at the standard path.
type DatabaseConfig struct {
	// Driver is "sqlite" (default) or "pgx" (PostgreSQL via pgx/v5 stdlib).
	Driver string `yaml:"driver"`
	// DSN is the data source name.
	// For SQLite: an absolute file path (e.g. ~/.joe/joe.db).
	// For pgx:    a libpq-style connection string (e.g. postgres://user:pass@host:5432/joe).
	// When empty with Driver "sqlite", joecored uses the default database path.
	DSN string `yaml:"dsn"`
}

// KnowledgeConfig configures the Phase 7 Knowledge Store.
type KnowledgeConfig struct {
	// EmbeddingModel is the model key (from LLM.Available) used for embeddings.
	// Defaults to the LLM.Current model when empty.
	EmbeddingModel string `yaml:"embedding_model"`
	// SemanticTopK is the default number of results returned by semantic search.
	SemanticTopK int `yaml:"semantic_top_k"`
	// DerivedMinConfidence is the minimum confidence for Tier 3 (derived) entries
	// to appear in semantic search results. 0 means include all.
	DerivedMinConfidence float32 `yaml:"derived_min_confidence"`
	// SyncEnabled controls whether background sync of external knowledge components runs.
	SyncEnabled bool `yaml:"sync_enabled"`
}

// serverServiceAccountName is the reserved name of the service account that
// represents joe itself (principal svc:server). The joe CLI and REPL —
// separate external processes that share this config — present this account's
// key when authenticating against joe's HTTP API. It is the direct
// descendant of the old single server.api_key, folded into the service-account
// map (D-0007). Identity Phase E (D-0008) removed the in-process loopback,
// so the in-process agent-loop no longer uses this key; the surviving
// consumers are the external co-located CLI processes only.
const serverServiceAccountName = "server"

// ServiceAccount is one named machine identity (Identity Phase D, design §2.4).
// A request bearing Key authenticates as principal svc:<Name>. Keys are
// plaintext-at-rest in config — the same posture as the single key they
// replace; a future upgrade to DB-minted, hashed, runtime-revocable keys
// replaces only the storage and the auth.ServiceAccountResolver lookup, not the
// principal-in-context flow downstream (D-0007 seam note).
type ServiceAccount struct {
	// Name is the service-account name; the minted principal is svc:<Name>.
	// Must be non-empty and must not itself carry a reserved principal prefix.
	Name string `yaml:"name"`
	// Key is the plaintext bearer token the account presents.
	Key string `yaml:"key"`
}

// ServerConfig holds joecored server settings
type ServerConfig struct {
	Address string `yaml:"address"` // e.g., ":7777" or "localhost:7777"
	// ServiceAccounts is the set of named machine identities joe accepts
	// (Identity Phase D). It is the ONLY machine-authentication input; there is
	// no separate single api_key. Each entry maps its Key to principal
	// svc:<Name>. Empty means no machine authentication is configured.
	ServiceAccounts []ServiceAccount `yaml:"service_accounts"`
	TLSCertFile     string           `yaml:"tls_cert_file"`    // Path to TLS certificate (enables HTTPS on server)
	TLSKeyFile      string           `yaml:"tls_key_file"`     // Path to TLS private key (enables HTTPS on server)
	TLSEnabled      bool             `yaml:"tls_enabled"`      // joe client: connect over HTTPS (must match server TLS setting)
	RateLimitRPS    float64          `yaml:"rate_limit_rps"`   // Requests per second per IP (0 = disabled)
	RateLimitBurst  int              `yaml:"rate_limit_burst"` // Burst size for rate limiter (default 10)
	// InsecureCookies drops the Secure attribute from the auth cookies (session
	// + OIDC state). DEV-ONLY: it lets browsers that refuse Secure cookies over
	// plain HTTP (Safari, Firefox) complete the OIDC login against an http://
	// origin, where Chrome's localhost special-case otherwise hides the problem.
	// Default false (Secure). NEVER enable in production — a non-Secure session
	// cookie can leak over plaintext. It is deliberately an explicit opt-in
	// rather than auto-derived from TLSConfigured(), because a TLS-terminating
	// reverse proxy serves joe over HTTP while the browser is on HTTPS; there the
	// cookies must stay Secure.
	InsecureCookies bool `yaml:"insecure_cookies"`
}

// TLSConfigured reports whether TLS has been configured for the server side.
func (s *ServerConfig) TLSConfigured() bool {
	return s.TLSCertFile != "" && s.TLSKeyFile != ""
}

// ServiceAccountsConfigured reports whether any machine identity is configured.
// It gates the RBAC policy engine exactly as the old non-empty api_key did: the
// engine is built when a real caller principal can be established (this OR a
// configured OIDC issuer).
func (s *ServerConfig) ServiceAccountsConfigured() bool {
	return len(s.ServiceAccounts) > 0
}

// LoopbackKey returns the bearer key a co-located external client process
// presents to joe: the joe CLI and the REPL. It is the key of the
// service account that represents the server itself — the one named "server"
// (svc:server), the fold of the old single server.api_key. When no "server"
// account exists it falls back to the first configured account (deterministic,
// config order) so the CLI keeps working with whatever machine identity is
// configured. Empty when no service accounts are configured (auth-disabled
// mode), in which case clients present no bearer and the nil policy engine
// permits all — the pre-Phase-D posture.
//
// Note: the historical name "LoopbackKey" refers to the now-removed in-process
// loopback (Identity Phase E, D-0008). The loopback itself is gone — the
// agent-loop reaches infra directly through the in-process accessor with the
// real caller principal — but the joe CLI and REPL are still external HTTP
// clients to joe and still present this key.
func (s *ServerConfig) LoopbackKey() string {
	for _, sa := range s.ServiceAccounts {
		if sa.Name == serverServiceAccountName {
			return sa.Key
		}
	}
	if len(s.ServiceAccounts) > 0 {
		return s.ServiceAccounts[0].Key
	}
	return ""
}

// LLMConfig configures LLM providers with support for multiple models
type LLMConfig struct {
	Current   string                 `yaml:"current"`   // Key into Available for the active model
	Available map[string]ModelConfig `yaml:"available"` // All configured models

	// Currency is the operator-configured currency that LLM cost rows
	// (llm_usage.currency) are stamped with at record time and that cost
	// caps (llm_cost_limits) are denominated in. Allowed values:
	// CurrencyUSD ("USD") or CurrencyEUR ("EUR"). Default CurrencyUSD.
	// Stream G phase G1: definition + validation only — no reader,
	// recorder, or cost-gate wired this phase.
	Currency string `yaml:"currency"`

	// USDToConfiguredRate is the FX rate from USD to Currency, used by
	// the (later) recorder to convert USD-quoted provider prices into
	// the configured currency before storing. Required and must be
	// positive when Currency is not CurrencyUSD; ignored (implicitly
	// 1.0) when Currency is CurrencyUSD. Stream G phase G1: definition +
	// validation only — no caller multiplies by this value yet.
	USDToConfiguredRate float64 `yaml:"usd_to_configured_rate"`
}

// ModelConfig describes a single LLM model
type ModelConfig struct {
	Provider string `yaml:"provider"` // "claude", "gemini"
	Model    string `yaml:"model"`    // e.g. "claude-sonnet-4-20250514"
}

// CurrentModel returns the ModelConfig for the currently selected model
func (c *LLMConfig) CurrentModel() (ModelConfig, error) {
	mc, ok := c.Available[c.Current]
	if !ok {
		return ModelConfig{}, fmt.Errorf("current model %q not found in available models", c.Current)
	}
	return mc, nil
}

// ModelNames returns the sorted list of available model keys
func (c *LLMConfig) ModelNames() []string {
	names := make([]string, 0, len(c.Available))
	for k := range c.Available {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// RefreshConfig configures background refresh
type RefreshConfig struct {
	IntervalMinutes int           `yaml:"interval_minutes"`
	Interval        time.Duration `yaml:"-"` // Computed from IntervalMinutes
	LLMBudget       LLMBudget     `yaml:"llm_budget"`
}

// LLMBudget limits LLM usage during background refresh
type LLMBudget struct {
	MaxCallsPerHour int           `yaml:"max_calls_per_hour"`
	BatchThreshold  int           `yaml:"batch_threshold"`
	BatchTimeoutSec int           `yaml:"batch_timeout_sec"`
	BatchTimeout    time.Duration `yaml:"-"` // Computed from BatchTimeoutSec
}

// NotificationConfig configures notifications
type NotificationConfig struct {
	Desktop    ChannelConfig    `yaml:"desktop"`
	Slack      ChannelConfig    `yaml:"slack"`
	QuietHours QuietHoursConfig `yaml:"quiet_hours"`
}

// ChannelConfig configures a notification channel
type ChannelConfig struct {
	Enabled           bool   `yaml:"enabled"`
	PriorityThreshold string `yaml:"priority_threshold"` // "low", "medium", "high", "urgent"
}

// QuietHoursConfig configures quiet hours
type QuietHoursConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Start    string `yaml:"start"`
	End      string `yaml:"end"`
	Timezone string `yaml:"timezone"`
}

// LoggingConfig configures logging
type LoggingConfig struct {
	Level string `yaml:"level"` // "debug", "info", "warn", "error"
	File  string `yaml:"file"`
}

// Load loads configuration from the specified file path
// Falls back to defaults if file doesn't exist
// Environment variables override config file values
func Load(configPath string) (*Config, error) {
	// Start with defaults
	cfg := defaultConfig()
	slog.Debug("config: initialized with defaults")

	// Expand home directory if path starts with ~
	if len(configPath) > 0 && configPath[0] == '~' {
		expanded, err := paths.ExpandPath(configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to expand config path: %w", err)
		}
		configPath = expanded
	}

	// Track config source
	configSource := "defaults"

	// Track whether the user expressed an explicit provider preference.
	explicit := false

	// Try to load from file
	if configPath != "" {
		currentSet, err := loadFromFile(cfg, configPath)
		if err != nil {
			// If file doesn't exist, that's okay - use defaults
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("failed to load config from %s: %w", configPath, err)
			}
			slog.Info("no config file detected, using defaults", "path", configPath)
			slog.Debug("config: file not found, using defaults", "path", configPath)
		} else {
			explicit = currentSet
			configSource = configPath
			slog.Info("loaded config from file", "path", configPath)
			slog.Debug("config: loaded from file", "path", configPath)
		}
	} else {
		slog.Info("no config path specified, using defaults")
		slog.Debug("config: no path specified, using defaults")
	}

	// Apply environment variable overrides
	envOverrides := applyEnvOverrides(cfg)
	if len(envOverrides) > 0 {
		slog.Debug("config: applied environment overrides", "vars", envOverrides)
	}

	// An explicit JOE_LLM_PROVIDER is a provider preference too. Both this and
	// an llm.current in the config file disable auto-selection.
	for _, name := range envOverrides {
		if name == "JOE_LLM_PROVIDER" {
			explicit = true
		}
	}
	cfg.explicitProvider = explicit

	// Compute derived fields
	cfg.Refresh.Interval = time.Duration(cfg.Refresh.IntervalMinutes) * time.Minute
	cfg.Refresh.LLMBudget.BatchTimeout = time.Duration(cfg.Refresh.LLMBudget.BatchTimeoutSec) * time.Second

	// Stream G phase G2 boundary check. The cost-currency invariant
	// (non-USD currency requires a positive USD-to-configured FX rate)
	// is a property of the configuration itself, so it is enforced
	// ONCE here at the load boundary — not re-checked by each
	// downstream consumer (the recorder, the cost gate, the future
	// settings service). Running it after env-var overrides means
	// JOE_LLM_PROVIDER and other env-driven mutations can't slip a
	// stale value past the validator. Returning the error from Load
	// surfaces it through runWithDeps' existing
	// "failed to load config" path so a misconfigured Joe FAILS
	// TO START rather than booting silently mispriced.
	if err := ValidateCostCurrency(cfg.LLM); err != nil {
		return nil, fmt.Errorf("invalid LLM cost-currency configuration: %w", err)
	}

	// Log final configuration
	slog.Debug("config: loaded",
		"source", configSource,
		"server.address", cfg.Server.Address,
		"llm.current", cfg.LLM.Current,
		"llm.available_models", len(cfg.LLM.Available),
		"refresh.interval_minutes", cfg.Refresh.IntervalMinutes,
		"logging.level", cfg.Logging.Level,
	)

	return cfg, nil
}

// defaultConfig returns a config with sensible defaults
func defaultConfig() *Config {
	return &Config{
		LLM: LLMConfig{
			Current: defaultLLMCurrent,
			Available: map[string]ModelConfig{
				defaultLLMCurrent: {Provider: providerClaude, Model: defaultLLMModel},
			},
			Currency: defaultCurrency,
		},
		Server: ServerConfig{
			Address: defaultServerAddress,
		},
		Refresh: RefreshConfig{
			IntervalMinutes: defaultRefreshIntervalMinutes,
			LLMBudget: LLMBudget{
				MaxCallsPerHour: defaultMaxCallsPerHour,
				BatchThreshold:  defaultBatchThreshold,
				BatchTimeoutSec: defaultBatchTimeoutSec,
			},
		},
		Notifications: NotificationConfig{
			Desktop: ChannelConfig{
				Enabled:           false,
				PriorityThreshold: defaultDesktopThreshold,
			},
			Slack: ChannelConfig{
				Enabled:           false,
				PriorityThreshold: defaultSlackThreshold,
			},
			QuietHours: QuietHoursConfig{
				Enabled:  false,
				Start:    defaultQuietStart,
				End:      defaultQuietEnd,
				Timezone: defaultQuietTimezone,
			},
		},
		Logging: LoggingConfig{
			Level: "info",
			File:  "",
		},
		Knowledge: KnowledgeConfig{
			SemanticTopK:         defaultKnowledgeSemanticTopK,
			DerivedMinConfidence: defaultKnowledgeMinConfidence,
			SyncEnabled:          false,
		},
		Auth: AuthConfig{
			SessionTTL:        defaultAuthSessionTTL,
			PostLoginRedirect: "/",
		},
	}
}

// loadFromFile loads config from a YAML file. currentSet reports whether the
// file explicitly set llm.current, which signals an explicit provider
// preference that auto-selection must not override.
func loadFromFile(cfg *Config, path string) (currentSet bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return false, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Detect whether the file explicitly selected a current model. A pointer
	// field distinguishes "absent" from "present but empty".
	var probe struct {
		LLM struct {
			Current *string `yaml:"current"`
		} `yaml:"llm"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return false, fmt.Errorf("failed to parse config file: %w", err)
	}

	return probe.LLM.Current != nil, nil
}

// applyEnvOverrides applies environment variable overrides
// Supported environment variables:
//   - JOE_LLM_PROVIDER: override LLM provider
//   - JOE_LLM_MODEL: override LLM model
//   - JOE_LOG_LEVEL: override logging level (debug, info, warn, error)
//   - JOE_SERVER_ADDRESS: override server address
//   - JOE_DATABASE_DSN: override database path/DSN
//
// Returns a slice of environment variable names that were applied.
func applyEnvOverrides(cfg *Config) []string {
	var overrides []string

	// LLM overrides
	provider := os.Getenv("JOE_LLM_PROVIDER")
	model := os.Getenv("JOE_LLM_MODEL")

	if provider != "" || model != "" {
		// Get the current model entry (or create one if missing)
		current := cfg.LLM.Current
		mc, ok := cfg.LLM.Available[current]
		if !ok {
			mc = ModelConfig{}
		}

		if provider != "" {
			mc.Provider = provider
			overrides = append(overrides, "JOE_LLM_PROVIDER")
		}
		if model != "" {
			mc.Model = model
			overrides = append(overrides, "JOE_LLM_MODEL")
		}

		if cfg.LLM.Available == nil {
			cfg.LLM.Available = make(map[string]ModelConfig)
		}
		cfg.LLM.Available[current] = mc
	}

	// Logging level override
	if logLevel := os.Getenv("JOE_LOG_LEVEL"); logLevel != "" {
		cfg.Logging.Level = logLevel
		overrides = append(overrides, "JOE_LOG_LEVEL")
	}

	// Server address override
	if serverAddr := os.Getenv("JOE_SERVER_ADDRESS"); serverAddr != "" {
		cfg.Server.Address = serverAddr
		overrides = append(overrides, "JOE_SERVER_ADDRESS")
	}

	// API key override. Phase D folds the old single key into the
	// service-account map: JOE_API_KEY sets the key of the reserved "server"
	// service account (principal svc:server), creating it if absent. This is
	// the env equivalent of the old single server.api_key and is what
	// co-located client processes present via ServerConfig.LoopbackKey.
	if apiKey := os.Getenv("JOE_API_KEY"); apiKey != "" {
		setServerServiceAccountKey(&cfg.Server, apiKey)
		overrides = append(overrides, "JOE_API_KEY")
	}

	// Database DSN override
	if dsn := os.Getenv("JOE_DATABASE_DSN"); dsn != "" {
		cfg.Database.DSN = dsn
		overrides = append(overrides, "JOE_DATABASE_DSN")
	}

	return overrides
}

// setServerServiceAccountKey sets the key of the reserved "server" service
// account, replacing it in place if it already exists or appending it
// otherwise. Used by the JOE_API_KEY env override to fold the old single key
// into the service-account map.
func setServerServiceAccountKey(s *ServerConfig, key string) {
	for i := range s.ServiceAccounts {
		if s.ServiceAccounts[i].Name == serverServiceAccountName {
			s.ServiceAccounts[i].Key = key
			return
		}
	}
	s.ServiceAccounts = append(s.ServiceAccounts, ServiceAccount{Name: serverServiceAccountName, Key: key})
}

// Save saves the config to a YAML file
func Save(cfg *Config, path string) error {
	// Expand home directory if path starts with ~
	if len(path) > 0 && path[0] == '~' {
		expanded, err := paths.ExpandPath(path)
		if err != nil {
			return fmt.Errorf("failed to expand config path: %w", err)
		}
		path = expanded
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Marshal to YAML with indentation
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to file
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
