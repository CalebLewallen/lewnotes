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

func TestUnknownClientRouteServesSPA(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/no-such-page", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 SPA shell", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content type = %q, want text/html", ct)
	}
	if !strings.Contains(rec.Body.String(), "<!doctype html>") {
		t.Fatal("response did not look like the SPA shell")
	}
}

func TestUnknownAPIRouteRequiresAuth(t *testing.T) {
	env := newTestEnv(t, 5)
	rec := env.anon().do(http.MethodGet, "/api/v1/nope", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 so route probing cannot bypass auth", rec.Code)
	}
}
