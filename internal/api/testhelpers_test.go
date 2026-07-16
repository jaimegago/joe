package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
)

// doRequest issues a JSON request against mux and returns the recorder.
// A nil body sends an empty payload.
func doRequest(mux *http.ServeMux, method, path string, body any) *httptest.ResponseRecorder {
	var bodyBytes []byte
	if body != nil {
		bodyBytes, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}
