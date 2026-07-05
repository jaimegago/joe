package search

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSearXNGSearch_MapsResultsAndRespectsCount covers the happy path: the
// provider builds a JSON search request, parses the SearXNG response into
// []Result (title/url/snippet), and caps the number of results at `count`.
func TestSearXNGSearch_MapsResultsAndRespectsCount(t *testing.T) {
	var gotQuery, gotFormat, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		gotFormat = r.URL.Query().Get("format")
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[
			{"title":"A","url":"https://a.example","content":"snippet a"},
			{"title":"B","url":"https://b.example","content":"snippet b"},
			{"title":"C","url":"https://c.example","content":"snippet c"}
		]}`))
	}))
	defer srv.Close()

	p := newSearXNGProvider(srv.URL, "")
	got, err := p.Search(context.Background(), "kafka lag", 2)
	if err != nil {
		t.Fatalf("Search() error = %v, want nil", err)
	}

	if gotQuery != "kafka lag" {
		t.Errorf("q = %q, want %q", gotQuery, "kafka lag")
	}
	if gotFormat != "json" {
		t.Errorf("format = %q, want json", gotFormat)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want application/json", gotAccept)
	}
	if len(got) != 2 {
		t.Fatalf("len(results) = %d, want 2 (count cap)", len(got))
	}
	if got[0].Title != "A" || got[0].URL != "https://a.example" || got[0].Snippet != "snippet a" {
		t.Errorf("results[0] = %+v, want {A, https://a.example, snippet a}", got[0])
	}
}

// TestSearXNGSearch_DefaultCount applies the default cap when count <= 0.
func TestSearXNGSearch_DefaultCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		var b strings.Builder
		b.WriteString(`{"results":[`)
		for i := 0; i < defaultSearXNGResults+5; i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(`{"title":"t","url":"https://e.example","content":"c"}`)
		}
		b.WriteString(`]}`)
		_, _ = w.Write([]byte(b.String()))
	}))
	defer srv.Close()

	p := newSearXNGProvider(srv.URL, "")
	got, err := p.Search(context.Background(), "q", 0)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(got) != defaultSearXNGResults {
		t.Fatalf("len(results) = %d, want defaultSearXNGResults=%d", len(got), defaultSearXNGResults)
	}
}

// TestSearXNGSearch_EmptyQuery rejects a blank query before any request.
func TestSearXNGSearch_EmptyQuery(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer srv.Close()

	p := newSearXNGProvider(srv.URL, "")
	if _, err := p.Search(context.Background(), "   ", 5); err == nil {
		t.Fatal("Search(blank query) = nil error, want error")
	}
	if called {
		t.Error("Search(blank query) issued an HTTP request; want none")
	}
}

// TestSearXNGSearch_Non200 surfaces a non-200 status as an error.
func TestSearXNGSearch_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	p := newSearXNGProvider(srv.URL, "")
	if _, err := p.Search(context.Background(), "q", 5); err == nil {
		t.Fatal("Search() against 502 = nil error, want error")
	}
}

// TestSearXNGSearch_MalformedJSON surfaces an unparseable body as an error.
func TestSearXNGSearch_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>not json</html>"))
	}))
	defer srv.Close()

	p := newSearXNGProvider(srv.URL, "")
	if _, err := p.Search(context.Background(), "q", 5); err == nil {
		t.Fatal("Search() with malformed JSON = nil error, want error")
	}
}

// TestSearXNGSearch_SendsBearerWhenKeyed sends Authorization only when an API
// key is configured (fronting-gateway case).
func TestSearXNGSearch_SendsBearerWhenKeyed(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()

	p := newSearXNGProvider(srv.URL, "secret-token")
	if _, err := p.Search(context.Background(), "q", 5); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if auth != "Bearer secret-token" {
		t.Errorf("Authorization = %q, want %q", auth, "Bearer secret-token")
	}
}
