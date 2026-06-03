package webui

import (
	"bytes"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

// assetPrefix is the URL prefix under which Vite emits hashed, immutable build
// chunks. A miss here must 404 — never fall through to index.html, or a typo'd
// chunk name would silently return HTML and break the app.
const assetPrefix = "/assets/"

// fallbackIndex is served for navigation paths when this binary was built
// without its web UI (placeholder-only embed — see uiBuilt). It states plainly
// that the UI is missing and names the supported fix (`make build`), since this
// is what a person trying joe from a bare `go build` would otherwise hit with
// no explanation.
const fallbackIndex = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>joe — web UI not built</title></head>
<body>
<h1>joe was built without its web UI</h1>
<p>This binary was compiled without the embedded web UI. That happens when joe
is built with a bare <code>go build ./cmd/joe</code>, which embeds only a
placeholder instead of the real UI assets.</p>
<p>To get a UI-complete binary, build with <code>make build</code>. That target
builds the web UI first and embeds it into the binary; a plain
<code>go build</code> does not.</p>
<p>The joe API is still fully available under <code>/api/v1</code>.</p>
</body>
</html>`

// staticHandler serves the embedded SPA from a filesystem rooted at the dist
// directory (so index.html and assets/ are reachable at the top level).
type staticHandler struct {
	fsys  fs.FS
	index []byte
}

// uiBuilt reports whether fsys holds a real Vite build rather than only the
// committed placeholder. It tests for an assets/ directory: a production Vite
// build always emits assets/ with hashed JS/CSS chunks, and the only way that
// directory reaches the embed tree is `make build` copying ui/dist into it.
// The placeholder checkout contains just .gitkeep and no assets/. This is the
// authoritative discriminator — a stray index.html is not, since a placeholder
// could carry one, but no placeholder can manufacture an assets/ dir of hashed
// build chunks, so this signal cannot false-positive on a real build.
func uiBuilt(fsys fs.FS) bool {
	info, err := fs.Stat(fsys, "assets")
	return err == nil && info.IsDir()
}

// newStaticHandler builds a handler over fsys. The SPA shell is read once at
// construction: a real build's index.html when the UI is embedded, otherwise
// the built-in fallback page (which explains how to produce a UI-complete
// binary) so the handler is always serviceable.
func newStaticHandler(fsys fs.FS) *staticHandler {
	index, err := fs.ReadFile(fsys, "index.html")
	if err != nil || !uiBuilt(fsys) {
		index = []byte(fallbackIndex)
	}
	return &staticHandler{fsys: fsys, index: index}
}

func (h *staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	upath := r.URL.Path
	if upath == "" || upath == "/" {
		h.serveIndex(w, r)
		return
	}

	name := strings.TrimPrefix(path.Clean(upath), "/")

	// Asset requests resolve to a real file or 404 — never the SPA fallback.
	if strings.HasPrefix(upath, assetPrefix) {
		if h.fileExists(name) {
			h.serveFile(w, r, name)
		} else {
			http.NotFound(w, r)
		}
		return
	}

	// Root-level static files (favicon, vite.svg, ...) are served directly.
	if h.fileExists(name) {
		h.serveFile(w, r, name)
		return
	}

	// Any other path is a client-side React Router route (/graph, /admin, ...).
	// Serve the SPA shell so the browser-side router can take over.
	h.serveIndex(w, r)
}

// fileExists reports whether name resolves to a regular file in the embedded
// FS. Invalid paths (including traversal attempts) fail fs.Open and return
// false.
func (h *staticHandler) fileExists(name string) bool {
	if name == "" || name == "." {
		return false
	}
	f, err := h.fsys.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		return false
	}
	return true
}

// serveIndex writes the SPA shell. index.html must not be cached so deploys of
// a new build (with new hashed asset names) are picked up immediately.
func (h *staticHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(h.index))
}

// serveFile writes a real embedded file. Hashed assets are immutable and so are
// marked cacheable forever; other static files inherit the default (no explicit
// cache header).
func (h *staticHandler) serveFile(w http.ResponseWriter, r *http.Request, name string) {
	data, err := fs.ReadFile(h.fsys, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType(name))
	if strings.HasPrefix(r.URL.Path, assetPrefix) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

// contentType maps a filename to a content type. Common web extensions are
// handled explicitly so the result does not depend on the host's mime database
// (notably .js, which some systems leave unset or map to text/plain).
func contentType(name string) string {
	switch path.Ext(name) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".json", ".map":
		return "application/json"
	case ".png":
		return "image/png"
	case ".ico":
		return "image/x-icon"
	case ".woff2":
		return "font/woff2"
	case ".woff":
		return "font/woff"
	default:
		if t := mime.TypeByExtension(path.Ext(name)); t != "" {
			return t
		}
		return "application/octet-stream"
	}
}
