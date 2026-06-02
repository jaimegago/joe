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
