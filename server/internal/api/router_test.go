package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestRouter() http.Handler {
	return NewRouter(Options{Version: "test-1", Started: time.Now().Add(-time.Second)})
}

func TestHealth(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content type = %q, want application/json", ct)
	}

	var body healthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !body.OK || body.Version != "test-1" {
		t.Fatalf("body = %+v, want ok=true version=test-1", body)
	}
	if body.UptimeSeconds < 0 {
		t.Fatalf("uptime = %v, want >= 0", body.UptimeSeconds)
	}
}

func TestUnknownRouteIsProblemJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("content type = %q, want application/problem+json", ct)
	}

	var body problem
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != http.StatusNotFound || body.Detail == "" {
		t.Fatalf("body = %+v, want status=404 with detail", body)
	}
}
