package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// testFS mimics a real Vite build output: an index.html shell, a root-level
// static file, and hashed asset chunks under assets/.
func testFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":              {Data: []byte("<!doctype html><title>app shell</title>")},
		"vite.svg":                {Data: []byte("<svg></svg>")},
		"assets/index-abc123.js":  {Data: []byte("console.log('hi')")},
		"assets/index-abc123.css": {Data: []byte("body{margin:0}")},
	}
}

func get(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestRootServesIndex(t *testing.T) {
	h := newStaticHandler(testFS())
	rec := get(t, h, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "app shell") {
		t.Fatalf("body = %q, want index.html shell", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q, want text/html", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("cache-control = %q, want no-cache", cc)
	}
}

func TestRealAssetServesFile(t *testing.T) {
	h := newStaticHandler(testFS())
	rec := get(t, h, "/assets/index-abc123.js")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "console.log") {
		t.Fatalf("body = %q, want JS chunk", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Fatalf("content-type = %q, want text/javascript", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("cache-control = %q, want immutable", cc)
	}
}

func TestRootLevelStaticServesFile(t *testing.T) {
	h := newStaticHandler(testFS())
	rec := get(t, h, "/vite.svg")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "image/svg+xml") {
		t.Fatalf("content-type = %q, want image/svg+xml", ct)
	}
}

func TestUnknownNavigationServesIndex(t *testing.T) {
	h := newStaticHandler(testFS())
	for _, p := range []string{"/graph", "/admin", "/sources/deep/link"} {
		rec := get(t, h, p)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", p, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "app shell") {
			t.Fatalf("%s: body = %q, want index.html shell", p, rec.Body.String())
		}
	}
}

func TestMissingAssetReturns404(t *testing.T) {
	h := newStaticHandler(testFS())
	rec := get(t, h, "/assets/does-not-exist.js")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "app shell") {
		t.Fatalf("missing asset fell through to index.html: %q", rec.Body.String())
	}
}

func TestMountDelegatesAPIPaths(t *testing.T) {
	var apiCalled bool
	apiChain := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		apiCalled = true
		w.WriteHeader(http.StatusTeapot)
	})
	root, err := Mount(apiChain)
	if err != nil {
		t.Fatalf("Mount: %v", err)
	}

	// /api/v1 paths are delegated to the API chain unchanged.
	for _, p := range []string{"/api/v1/me", "/api/v1", "/api/v1/graph/node/x"} {
		apiCalled = false
		rec := get(t, root, p)
		if !apiCalled {
			t.Fatalf("%s: not delegated to API chain", p)
		}
		if rec.Code != http.StatusTeapot {
			t.Fatalf("%s: status = %d, want delegated 418", p, rec.Code)
		}
	}

	// Non-API navigation is served as static, never delegated.
	apiCalled = false
	rec := get(t, root, "/graph")
	if apiCalled {
		t.Fatal("/graph was wrongly delegated to the API chain")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("/graph: status = %d, want 200 (static)", rec.Code)
	}
}

func TestUIBuiltDiscriminator(t *testing.T) {
	// A real Vite build always emits an assets/ directory of hashed chunks.
	if !uiBuilt(testFS()) {
		t.Fatal("uiBuilt(real-build FS) = false, want true")
	}
	// A placeholder-only checkout has just .gitkeep and no assets/.
	placeholder := fstest.MapFS{".gitkeep": {Data: []byte("# placeholder")}}
	if uiBuilt(placeholder) {
		t.Fatal("uiBuilt(placeholder FS) = true, want false")
	}
	// A stray index.html WITHOUT assets/ must not count as a real build — this
	// is exactly the false-positive the assets/ discriminator rules out.
	indexOnly := fstest.MapFS{
		".gitkeep":   {Data: []byte("# placeholder")},
		"index.html": {Data: []byte("<!doctype html><title>not a build</title>")},
	}
	if uiBuilt(indexOnly) {
		t.Fatal("uiBuilt(index-without-assets FS) = true; index.html alone must not register as a build")
	}
}

func TestFallbackServedWhenUINotBuilt(t *testing.T) {
	// Placeholder-only embed: even with a stray index.html present, the handler
	// must serve the fallback page that names `make build` as the fix.
	placeholder := fstest.MapFS{
		".gitkeep":   {Data: []byte("# placeholder")},
		"index.html": {Data: []byte("<!doctype html><title>stray</title>")},
	}
	h := newStaticHandler(placeholder)
	rec := get(t, h, "/graph")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "make build") {
		t.Fatalf("fallback page must name `make build`; got %q", body)
	}
	if !strings.Contains(body, "without its web UI") {
		t.Fatalf("fallback page must state the binary was built without the UI; got %q", body)
	}
	if strings.Contains(body, "stray") {
		t.Fatalf("stray index.html was served instead of the fallback page; got %q", body)
	}
}

// TestMountEndToEndRoutesAPIAndStatic exercises the REAL Mount over the real
// embedded FS: the production isAPIPath routing and the real staticHandler,
// composed exactly as cmd/joe/server.go assembles them. Only the API chain is a
// stand-in — the real one needs a DB, auth, and sources — so this is the
// highest layer that still drives Mount + isAPIPath + staticHandler unmocked.
func TestMountEndToEndRoutesAPIAndStatic(t *testing.T) {
	var apiHits []string
	apiChain := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiHits = append(apiHits, r.URL.Path)
		w.WriteHeader(http.StatusTeapot)
	})
	root, err := Mount(apiChain)
	if err != nil {
		t.Fatalf("Mount: %v", err)
	}

	// An /api/v1 request reaches the API chain via the real isAPIPath, never
	// the static catch-all.
	rec := get(t, root, "/api/v1/graph/node/x")
	if len(apiHits) != 1 || apiHits[0] != "/api/v1/graph/node/x" {
		t.Fatalf("/api/v1 path did not reach API chain; hits = %v", apiHits)
	}
	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want 418 from API chain", rec.Code)
	}

	// A non-API navigation path reaches the real static handler and is served
	// the SPA index shell (identical to the body served at "/"), with no API
	// chain involvement.
	apiHits = nil
	idx := get(t, root, "/")
	nav := get(t, root, "/graph")
	if len(apiHits) != 0 {
		t.Fatalf("/graph wrongly reached the API chain; hits = %v", apiHits)
	}
	if nav.Code != http.StatusOK {
		t.Fatalf("/graph status = %d, want 200 from static handler", nav.Code)
	}
	if ct := nav.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("/graph content-type = %q, want text/html (index shell)", ct)
	}
	if nav.Body.String() != idx.Body.String() || nav.Body.Len() == 0 {
		t.Fatal("/graph body did not match the SPA index shell served at /")
	}
}

func TestHandlerEmbedsPlaceholder(t *testing.T) {
	// Handler() builds over the committed embed tree (placeholder-only in a
	// fresh checkout). It must construct and serve the root without error.
	h, err := Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	rec := get(t, h, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
