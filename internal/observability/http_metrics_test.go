package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStatusRecorder_WriteHeaderAndWrite(t *testing.T) {
	rr := httptest.NewRecorder()
	r := &statusRecorder{ResponseWriter: rr}

	r.WriteHeader(http.StatusCreated)
	if r.status != http.StatusCreated {
		t.Fatalf("status=%d want %d", r.status, http.StatusCreated)
	}

	n, err := r.Write([]byte("ok"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n != 2 || r.bytes != 2 {
		t.Fatalf("bytes n=%d recorder=%d", n, r.bytes)
	}
}

func TestStatusRecorder_WriteDefaultsStatusOK(t *testing.T) {
	rr := httptest.NewRecorder()
	r := &statusRecorder{ResponseWriter: rr}

	_, err := r.Write([]byte("abc"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if r.status != http.StatusOK {
		t.Fatalf("status=%d want %d", r.status, http.StatusOK)
	}
}

func TestHTTPMetricsMiddleware_NilNext(t *testing.T) {
	h := HTTPMetricsMiddleware(nil, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)

	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("code=%d want %d", rr.Code, http.StatusNotFound)
	}
}

func TestHTTPMetricsMiddleware_SuccessAndError(t *testing.T) {
	t.Run("success route uses url path fallback", func(t *testing.T) {
		metrics := NewMetrics()
		h := HTTPMetricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("ok"))
			if r.Context() == nil {
				t.Fatal("expected context")
			}
		}), metrics)

		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/success", nil)
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("code=%d want %d", rr.Code, http.StatusOK)
		}
	})

	t.Run("error status", func(t *testing.T) {
		metrics := NewMetrics()
		h := HTTPMetricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("boom"))
		}), metrics)

		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/error", nil)
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("code=%d want %d", rr.Code, http.StatusInternalServerError)
		}
	})
}

func TestGetHTTPMetrics_Once(t *testing.T) {
	m := NewMetrics()
	a := m.getHTTPMetrics()
	b := m.getHTTPMetrics()
	if a != b {
		t.Fatal("expected same http metrics pointer from sync.Once")
	}
}
