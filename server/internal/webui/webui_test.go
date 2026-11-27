package webui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbeddedIndexExists(t *testing.T) {
	data, err := fs.ReadFile(embedded, "dist/index.html")
	if err != nil {
		t.Fatalf("embedded index: %v", err)
	}
	if !strings.Contains(string(data), "<!doctype html>") {
		t.Fatalf("embedded index does not look like HTML")
	}
}

func TestSPAIndexForClientRoutes(t *testing.T) {
	handler := Handler()
	for _, target := range []string{"/", "/notes", "/login", "/setup", "/n/inbox/Welcome.md", "/index.html"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s = %d, want 200", target, rec.Code)
			continue
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("%s content type = %q, want text/html", target, ct)
		}
		if !strings.Contains(rec.Body.String(), "<!doctype html>") {
			t.Errorf("%s did not serve the SPA shell", target)
		}
	}
}

func TestStaticAssetIsServedAndCached(t *testing.T) {
	asset := firstImmutableAsset(t)
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, asset, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s = %d, want 200", asset, rec.Code)
	}
	if rec.Body.Len() == 0 {
		t.Fatalf("%s served an empty body", asset)
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Errorf("Cache-Control = %q, want immutable", got)
	}
}

func TestMissingAppAssetIs404(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/_app/immutable/chunks/does-not-exist.js", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing asset = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "<!doctype html>") {
		t.Fatal("missing asset incorrectly served the SPA shell")
	}
}

func TestSPAMethodAndHead(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/notes", nil))
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("POST = %d Allow=%q", rec.Code, rec.Header().Get("Allow"))
	}

	rec = httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/notes", nil))
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("HEAD = %d body=%d", rec.Code, rec.Body.Len())
	}
}

func TestTraversalCannotReadOutsideDist(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/../../go.mod", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("cleaned traversal = %d, want SPA 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "module github.com") {
		t.Fatal("traversal returned a source file")
	}
}

func firstImmutableAsset(t *testing.T) string {
	t.Helper()
	found := ""
	err := fs.WalkDir(embedded, "dist/_app/immutable", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(name, ".js") {
			found = "/" + strings.TrimPrefix(name, "dist/")
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded assets: %v", err)
	}
	if found == "" {
		t.Fatal("no embedded JS asset found")
	}
	return found
}
