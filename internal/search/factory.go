package search

import (
	"fmt"

	"github.com/jaimegago/joe/internal/config"
)

// ProviderSearXNG is the only search provider implemented today: the
// self-hostable, keyless SearXNG backend. Keyed hosted providers (Tavily,
// Brave) are a designed-for additive extension behind this same factory and are
// deferred to the backlog.
const ProviderSearXNG = "searxng"

// NewProvider selects and constructs a search Provider from configuration,
// resolved once at boot. It mirrors llmfactory.NewAdapter in spirit.
//
// Contract:
//   - An empty cfg.Provider returns (nil, nil): web search is unconfigured and
//     therefore inert. There is NO silent default — the web_search tool stays
//     advertised and returns a no-backend-configured tool-error at call time.
//   - A recognized provider with valid config returns a live Provider.
//   - An unknown provider, or a known provider missing required config (e.g.
//     SearXNG without a base_url), returns an error so boot fails fast, exactly
//     as an LLM misconfiguration does.
func NewProvider(cfg config.WebSearchConfig) (Provider, error) {
	if !cfg.Configured() {
		return nil, nil
	}

	switch cfg.Provider {
	case ProviderSearXNG:
		if cfg.BaseURL == "" {
			return nil, fmt.Errorf("web_search: base_url is required for the %s provider", ProviderSearXNG)
		}
		return newSearXNGProvider(cfg.BaseURL, cfg.APIKey), nil
	default:
		return nil, fmt.Errorf("web_search: unsupported provider %q (supported: %s)", cfg.Provider, ProviderSearXNG)
	}
}
