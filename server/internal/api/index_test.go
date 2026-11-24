package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caleblewallen/lewnotes/server/internal/auth"
	"github.com/caleblewallen/lewnotes/server/internal/index"
	"github.com/caleblewallen/lewnotes/server/internal/vault"
)

// authedHandler injects a valid session (and CSRF header on mutations) so the
// existing index tests keep testing the index rather than auth.
type authedHandler struct {
	next   http.Handler
	cookie *http.Cookie
	csrf   string
}

func (a *authedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.AddCookie(a.cookie)
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		r.Header.Set(auth.CSRFHeader, a.csrf)
	}
	a.next.ServeHTTP(w, r)
}

func testRouter(t *testing.T, withIndex, withScanner bool) (http.Handler, *index.Index) {
	t.Helper()

	vaultDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(vaultDir, "note.md"), []byte("# Note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(vaultDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	ix, err := index.Open(filepath.Join(t.TempDir(), "lewnotes.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ix.Close() })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc, err := auth.NewService(auth.ServiceOptions{
		Store:  auth.NewStore(ix.DB()),
		Argon2: testArgon2,
	})
	if err != nil {
		t.Fatal(err)
	}
	session, _, err := svc.Setup(context.Background(), testPassword, "test", auth.RequestMeta{})
	if err != nil {
		t.Fatal(err)
	}

	opts := Options{Version: "test", Started: time.Now(), Logger: logger, Auth: svc}
	if withIndex {
		opts.Index = ix
	}
	if withScanner {
		opts.Scanner = index.NewScanner(v, ix, logger)
	}

	router := NewRouter(opts)
	return &authedHandler{
		next:   router,
		cookie: &http.Cookie{Name: auth.SessionCookieName, Value: session.Token},
		csrf:   session.CSRF,
	}, ix
}

func testIndexAPI(t *testing.T) (http.Handler, *index.Index) {
	t.Helper()
	return testRouter(t, true, true)
}

func getStatus(t *testing.T, router http.Handler) index.ScanStatus {
	t.Helper()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/index/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", rec.Code, rec.Body.String())
	}
	var status index.ScanStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	return status
}

func TestIndexStatusBeforeScan(t *testing.T) {
	router, _ := testIndexAPI(t)

	status := getStatus(t, router)
	if status.State != "idle" || status.Notes != 0 {
		t.Errorf("status = %+v, want idle with 0 notes", status)
	}
}

func TestRescanRunsInBackground(t *testing.T) {
	router, _ := testIndexAPI(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/index/rescan", strings.NewReader(`{"full":true}`))
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status code = %d, body = %s", rec.Code, rec.Body.String())
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status := getStatus(t, router)
		if status.State == "idle" && status.Notes == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("scan did not finish: %+v", getStatus(t, router))
}

func TestRescanRejectsInvalidBody(t *testing.T) {
	router, _ := testIndexAPI(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/index/rescan", strings.NewReader(`{"full":`))
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want 400", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Errorf("content type = %q, want problem+json", ct)
	}
}

func TestIndexRoutesUnavailableWithoutIndex(t *testing.T) {
	router, _ := testRouter(t, false, false)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/index/status", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want 503 when the index is not wired", rec.Code)
	}
}
