// Package webui serves the embedded single-page web UI for joe.
//
// The production React build (ui/dist) is copied into this package's in-tree
// dist directory by `make build-ui` and embedded at compile time. go:embed
// cannot reach ui/dist directly (it lives outside this package's subtree), so
// the build stages the real output here first.
//
// A committed placeholder (dist/.gitkeep) keeps `go build` and `go vet` green
// in a fresh checkout where the UI has not been built; the real build output is
// gitignored. When no embedded index.html is present (placeholder only, e.g.
// `go run ./cmd/joe` without a UI build) the handler serves a minimal
// built-in page instead of failing.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// apiPathPrefix is the absolute prefix for every joe API route. Requests
// under it are delegated to the API middleware chain unchanged; every other
// request is served from the embedded UI. Kept in sync with
// internal/api.apiPrefix ("/api/v1").
const apiPathPrefix = "/api/v1"

// distFS holds the embedded UI build output. The all: prefix includes the
// committed .gitkeep placeholder so the directive matches a file in a
// fresh checkout where the real UI has not been staged.
//
//go:embed all:dist
var distFS embed.FS

// Handler returns an http.Handler that serves the embedded SPA build: real
// files for hashed assets and root-level statics, index.html (with SPA
// deep-link fallback) for navigation paths, and 404 for missing assets.
func Handler() (http.Handler, error) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, err
	}
	return newStaticHandler(sub), nil
}

// Embedded reports whether this binary was built with a real web UI embedded
// (via `make build`) rather than only the committed placeholder. It reuses the
// uiBuilt discriminator (presence of an assets/ directory of hashed Vite
// chunks), so it cannot false-positive on a real build. The server calls this
// once at startup to decide whether to warn about a UI-less binary; it is cheap
// and never runs per request.
func Embedded() bool {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return false
	}
	return uiBuilt(sub)
}

// Mount wraps the API middleware chain so requests under /api/v1 are delegated
// to apiChain unchanged, and all other requests are served from the embedded
// UI entirely outside the chain (no edge auth, rate limit, metrics, or body
// cap). This makes the logged-out login UI reachable with no credential while
// the API surface keeps its exact behavior, including JSON 404s for unknown
// /api/v1 paths.
func Mount(apiChain http.Handler) (http.Handler, error) {
	static, err := Handler()
	if err != nil {
		return nil, err
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path) {
			apiChain.ServeHTTP(w, r)
			return
		}
		static.ServeHTTP(w, r)
	}), nil
}

// isAPIPath reports whether p is the API prefix itself or a path beneath it.
// It deliberately does not match siblings like "/api/v1xyz" so only genuine
// API routes bypass the static handler.
func isAPIPath(p string) bool {
	return p == apiPathPrefix || strings.HasPrefix(p, apiPathPrefix+"/")
}
