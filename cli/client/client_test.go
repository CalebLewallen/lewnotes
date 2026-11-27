package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/health" {
			t.Errorf("path = %q, want /api/v1/health", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"version":"test-1","uptime_seconds":1.5}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL + "/").Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if !got.OK || got.Version != "test-1" {
		t.Fatalf("Health() = %+v, want ok=true version=test-1", got)
	}
}

func TestHealthServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	if _, err := New(srv.URL).Health(context.Background()); err == nil {
		t.Fatal("Health() error = nil, want error")
	}
}
