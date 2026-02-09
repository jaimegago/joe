package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/adapters"
	gitadapter "github.com/jaimegago/joe/internal/adapters/git"
	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/test/mocks"
)

func setupGitTestServer(t *testing.T, mock *mocks.MockGitAdapter) (*api.Server, *http.ServeMux) {
	t.Helper()

	registry := adapters.NewRegistry()
	registry.Register("test-repo", mock)

	services := &core.Services{
		Config:   &config.Config{},
		Adapters: registry,
	}

	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	return server, mux
}

func TestHandleGitReadFile(t *testing.T) {
	tests := []struct {
		name       string
		sourceID   string
		query      string
		mock       *mocks.MockGitAdapter
		wantStatus int
		wantBody   string
	}{
		{
			name:     "read file success",
			sourceID: "test-repo",
			query:    "path=README.md",
			mock: &mocks.MockGitAdapter{
				ReadFileResult: "# Hello\n",
			},
			wantStatus: http.StatusOK,
			wantBody:   "# Hello\n",
		},
		{
			name:       "missing path param",
			sourceID:   "test-repo",
			query:      "",
			mock:       mocks.NewMockGitAdapter(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "source not found",
			sourceID:   "nope",
			query:      "path=README.md",
			mock:       mocks.NewMockGitAdapter(),
			wantStatus: http.StatusNotFound,
		},
		{
			name:     "adapter error",
			sourceID: "test-repo",
			query:    "path=nope.txt",
			mock: &mocks.MockGitAdapter{
				ReadFileErr: fmt.Errorf("file not found"),
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, mux := setupGitTestServer(t, tt.mock)

			path := "/api/v1/git/" + tt.sourceID + "/file"
			if tt.query != "" {
				path += "?" + tt.query
			}

			req := httptest.NewRequest("GET", path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantBody != "" && tt.wantStatus == http.StatusOK {
				var body struct {
					Content string `json:"content"`
				}
				if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if body.Content != tt.wantBody {
					t.Errorf("content = %q, want %q", body.Content, tt.wantBody)
				}
			}
		})
	}
}

func TestHandleGitListFiles(t *testing.T) {
	tests := []struct {
		name       string
		sourceID   string
		query      string
		mock       *mocks.MockGitAdapter
		wantStatus int
		wantCount  int
	}{
		{
			name:     "list root",
			sourceID: "test-repo",
			query:    "",
			mock: &mocks.MockGitAdapter{
				ListFilesResult: []gitadapter.FileInfo{
					{Path: "README.md", Size: 10},
					{Path: "main.go", Size: 50},
					{Path: "cmd", IsDir: true},
				},
			},
			wantStatus: http.StatusOK,
			wantCount:  3,
		},
		{
			name:     "list subdirectory",
			sourceID: "test-repo",
			query:    "dir=cmd",
			mock: &mocks.MockGitAdapter{
				ListFilesResult: []gitadapter.FileInfo{
					{Path: "app.go", Size: 20},
				},
			},
			wantStatus: http.StatusOK,
			wantCount:  1,
		},
		{
			name:       "source not found",
			sourceID:   "nope",
			mock:       mocks.NewMockGitAdapter(),
			wantStatus: http.StatusNotFound,
		},
		{
			name:     "adapter error",
			sourceID: "test-repo",
			query:    "dir=nope",
			mock: &mocks.MockGitAdapter{
				ListFilesErr: fmt.Errorf("directory not found"),
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, mux := setupGitTestServer(t, tt.mock)

			path := "/api/v1/git/" + tt.sourceID + "/files"
			if tt.query != "" {
				path += "?" + tt.query
			}

			req := httptest.NewRequest("GET", path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantStatus == http.StatusOK {
				var body struct {
					Count int `json:"count"`
				}
				if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if body.Count != tt.wantCount {
					t.Errorf("count = %d, want %d", body.Count, tt.wantCount)
				}
			}
		})
	}
}

func TestHandleGitLog(t *testing.T) {
	tests := []struct {
		name       string
		sourceID   string
		query      string
		mock       *mocks.MockGitAdapter
		wantStatus int
		wantCount  int
	}{
		{
			name:     "get log",
			sourceID: "test-repo",
			query:    "limit=2",
			mock: &mocks.MockGitAdapter{
				LogResult: []gitadapter.CommitInfo{
					{Hash: "abc123", Author: "dev", Date: time.Now(), Message: "second"},
					{Hash: "def456", Author: "dev", Date: time.Now(), Message: "first"},
				},
			},
			wantStatus: http.StatusOK,
			wantCount:  2,
		},
		{
			name:     "default limit",
			sourceID: "test-repo",
			mock: &mocks.MockGitAdapter{
				LogResult: []gitadapter.CommitInfo{
					{Hash: "abc123", Author: "dev", Message: "commit"},
				},
			},
			wantStatus: http.StatusOK,
			wantCount:  1,
		},
		{
			name:       "invalid limit",
			sourceID:   "test-repo",
			query:      "limit=abc",
			mock:       mocks.NewMockGitAdapter(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "source not found",
			sourceID:   "nope",
			mock:       mocks.NewMockGitAdapter(),
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, mux := setupGitTestServer(t, tt.mock)

			path := "/api/v1/git/" + tt.sourceID + "/log"
			if tt.query != "" {
				path += "?" + tt.query
			}

			req := httptest.NewRequest("GET", path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantStatus == http.StatusOK {
				var body struct {
					Count int `json:"count"`
				}
				if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if body.Count != tt.wantCount {
					t.Errorf("count = %d, want %d", body.Count, tt.wantCount)
				}
			}
		})
	}
}

func TestHandleGitDiff(t *testing.T) {
	tests := []struct {
		name       string
		sourceID   string
		query      string
		mock       *mocks.MockGitAdapter
		wantStatus int
	}{
		{
			name:     "diff success",
			sourceID: "test-repo",
			query:    "from=abc123&to=def456",
			mock: &mocks.MockGitAdapter{
				DiffResult: "diff --git a/main.go b/main.go\n",
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing from",
			sourceID:   "test-repo",
			query:      "to=def456",
			mock:       mocks.NewMockGitAdapter(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing to",
			sourceID:   "test-repo",
			query:      "from=abc123",
			mock:       mocks.NewMockGitAdapter(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing both",
			sourceID:   "test-repo",
			mock:       mocks.NewMockGitAdapter(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "source not found",
			sourceID:   "nope",
			query:      "from=abc&to=def",
			mock:       mocks.NewMockGitAdapter(),
			wantStatus: http.StatusNotFound,
		},
		{
			name:     "adapter error",
			sourceID: "test-repo",
			query:    "from=bad&to=ref",
			mock: &mocks.MockGitAdapter{
				DiffErr: fmt.Errorf("resolve failed"),
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, mux := setupGitTestServer(t, tt.mock)

			path := "/api/v1/git/" + tt.sourceID + "/diff"
			if tt.query != "" {
				path += "?" + tt.query
			}

			req := httptest.NewRequest("GET", path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantStatus == http.StatusOK {
				var body struct {
					Diff string `json:"diff"`
				}
				if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if body.Diff == "" {
					t.Error("diff should not be empty")
				}
			}
		})
	}
}
