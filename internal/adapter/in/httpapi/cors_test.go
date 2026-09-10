package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The browser only sends a method the preflight advertised, so every method the
// router registers must appear in Access-Control-Allow-Methods.
func TestCORSPreflightAllowsRoutedMethods(t *testing.T) {
	handler := CORSMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete, http.MethodOptions} {
		req := httptest.NewRequest(http.MethodOptions, "/api/sessions/abc", nil)
		req.Header.Set("Origin", "http://localhost:8000")
		req.Header.Set("Access-Control-Request-Method", method)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("preflight for %s: got status %d, want %d", method, rec.Code, http.StatusNoContent)
		}
		allowed := rec.Header().Get("Access-Control-Allow-Methods")
		if !strings.Contains(allowed, method) {
			t.Errorf("preflight for %s: Access-Control-Allow-Methods = %q, missing %s", method, allowed, method)
		}
	}
}
