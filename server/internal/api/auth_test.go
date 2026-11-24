package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caleblewallen/lewnotes/server/internal/auth"
	"github.com/caleblewallen/lewnotes/server/internal/events"
	"github.com/caleblewallen/lewnotes/server/internal/index"
	"github.com/caleblewallen/lewnotes/server/internal/notes"
	"github.com/caleblewallen/lewnotes/server/internal/vault"
)

const testPassword = "correct horse battery staple"

var testArgon2 = auth.Argon2Params{Memory: 1024, Time: 1, Threads: 1, SaltLength: 16, KeyLength: 32}

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type testEnv struct {
	handler http.Handler
	ix      *index.Index
	svc     *auth.Service
	notes   *notes.Service
	hub     *events.Hub
	logs    *bytes.Buffer
	clock   *testClock
}

func newTestEnv(t *testing.T, loginLimit int) *testEnv {
	t.Helper()

	ix, err := index.Open(filepath.Join(t.TempDir(), "lewnotes.db"))
	if err != nil {
		t.Fatalf("index.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = ix.Close() })

	clock := &testClock{now: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	svc, err := auth.NewService(auth.ServiceOptions{
		Store:      auth.NewStore(ix.DB()),
		SessionTTL: 30 * 24 * time.Hour,
		Argon2:     testArgon2,
		Now:        clock.Now,
	})
	if err != nil {
		t.Fatalf("auth.NewService() error = %v", err)
	}

	v, err := vault.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("vault.Open() error = %v", err)
	}
	hub := events.New(events.Options{Buffer: 100})
	noteSvc := notes.New(notes.Options{Vault: v, Index: ix, Events: hub, Logger: logger})

	handler := NewRouter(Options{
		Version:      "test",
		Started:      clock.Now(),
		Logger:       logger,
		Index:        ix,
		Auth:         svc,
		Notes:        noteSvc,
		Events:       hub,
		LoginLimiter: auth.NewLimiter(auth.LimiterOptions{Limit: loginLimit, Window: time.Minute, Now: clock.Now}),
	})
	return &testEnv{handler: handler, ix: ix, svc: svc, notes: noteSvc, hub: hub, logs: logs, clock: clock}
}

type testClient struct {
	env     *testEnv
	session *http.Cookie
	csrf    string
}

func (e *testEnv) anon() *testClient { return &testClient{env: e} }

func (c *testClient) do(method, path, body string) *httptest.ResponseRecorder {
	return c.doRaw(method, path, body, nil)
}

func (c *testClient) doRaw(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	if c.session != nil {
		req.AddCookie(c.session)
	}
	if c.csrf != "" && method != http.MethodGet && method != http.MethodHead {
		req.Header.Set(auth.CSRFHeader, c.csrf)
	}

	rec := httptest.NewRecorder()
	c.env.handler.ServeHTTP(rec, req)
	return rec
}

func (c *testClient) capture(rec *httptest.ResponseRecorder) {
	for _, cookie := range rec.Result().Cookies() {
		switch cookie.Name {
		case auth.SessionCookieName:
			if cookie.Value == "" {
				c.session = nil
			} else {
				c.session = &http.Cookie{Name: cookie.Name, Value: cookie.Value}
			}
		case auth.CSRFCookieName:
			c.csrf = cookie.Value
		}
	}
}

func (c *testClient) setup(t *testing.T) {
	t.Helper()
	rec := c.do(http.MethodPost, "/api/v1/auth/setup",
		`{"password":"`+testPassword+`","instance_name":"home"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup status = %d, body = %s", rec.Code, rec.Body.String())
	}
	c.capture(rec)
}

func (c *testClient) login(t *testing.T, password string) *httptest.ResponseRecorder {
	rec := c.do(http.MethodPost, "/api/v1/auth/login", `{"password":"`+password+`"}`)
	c.capture(rec)
	return rec
}

func TestAuthStatusBeforeSetup(t *testing.T) {
	env := newTestEnv(t, 5)
	rec := env.anon().do(http.MethodGet, "/api/v1/auth/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d", rec.Code)
	}

	var body authStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.SetupRequired {
		t.Error("setup_required = false, want true")
	}
}

func TestSetupSignsInAndProtectsRoutes(t *testing.T) {
	env := newTestEnv(t, 5)
	client := env.anon()
	client.setup(t)

	me := client.do(http.MethodGet, "/api/v1/auth/me", "")
	if me.Code != http.StatusOK {
		t.Fatalf("me status = %d, body = %s", me.Code, me.Body.String())
	}
	var meBody authResponse
	if err := json.Unmarshal(me.Body.Bytes(), &meBody); err != nil {
		t.Fatal(err)
	}
	if meBody.Auth != "session" || meBody.Username != "owner" || meBody.InstanceName != "home" {
		t.Errorf("me = %+v", meBody)
	}
	if meBody.CSRFToken == "" {
		t.Error("me did not return a csrf token")
	}

	if rec := client.do(http.MethodGet, "/api/v1/index/status", ""); rec.Code != http.StatusOK {
		t.Errorf("cookie-authed index status = %d", rec.Code)
	}

	client.csrf = ""
	if rec := client.do(http.MethodPost, "/api/v1/index/rescan", `{"full":false}`); rec.Code != http.StatusForbidden {
		t.Errorf("rescan without CSRF = %d, want 403", rec.Code)
	}
	client.csrf = "wrong"
	if rec := client.do(http.MethodPost, "/api/v1/index/rescan", `{"full":false}`); rec.Code != http.StatusForbidden {
		t.Errorf("rescan with wrong CSRF = %d, want 403", rec.Code)
	}
}

func TestLoginLogoutAndUniformErrors(t *testing.T) {
	env := newTestEnv(t, 5)
	setup := env.anon()
	setup.setup(t)

	if rec := env.anon().login(t, "wrong password"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("login(wrong) = %d, want 401", rec.Code)
	} else if !strings.Contains(rec.Body.String(), "Incorrect password.") {
		t.Errorf("login(wrong) body = %s, want uniform message", rec.Body.String())
	}

	client := env.anon()
	if rec := client.login(t, testPassword); rec.Code != http.StatusOK {
		t.Fatalf("login = %d, body = %s", rec.Code, rec.Body.String())
	}
	if client.csrf == "" {
		t.Fatal("login did not set a csrf cookie")
	}

	if rec := client.do(http.MethodPost, "/api/v1/auth/logout", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("logout = %d", rec.Code)
	}
	if rec := client.do(http.MethodGet, "/api/v1/auth/me", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("me after logout = %d, want 401", rec.Code)
	}
}

func TestProtectedRoutesRequireAuth(t *testing.T) {
	env := newTestEnv(t, 5)
	client := env.anon()

	protected := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/index/status"},
		{http.MethodPost, "/api/v1/index/rescan"},
		{http.MethodGet, "/api/v1/auth/me"},
		{http.MethodPost, "/api/v1/auth/logout"},
		{http.MethodPost, "/api/v1/auth/password"},
		{http.MethodGet, "/api/v1/tokens"},
		{http.MethodPost, "/api/v1/tokens"},
		{http.MethodDelete, "/api/v1/tokens/01ARZ3NDEKTSV4RRFFQ69G5FAV"},
		{http.MethodGet, "/api/v1/meta"},
		{http.MethodGet, "/api/v1/notes"},
		{http.MethodPost, "/api/v1/notes"},
		{http.MethodGet, "/api/v1/notes/01ARZ3NDEKTSV4RRFFQ69G5FAV"},
		{http.MethodGet, "/api/v1/notes/by-path?path=a.md"},
		{http.MethodGet, "/api/v1/tree"},
		{http.MethodGet, "/api/v1/files/a.png"},
		{http.MethodGet, "/api/v1/search?q=hello"},
		{http.MethodGet, "/api/v1/events"},
		{http.MethodGet, "/api/v1/openapi.json"},
		{http.MethodGet, "/api/v1/does-not-exist"},
	}
	for _, route := range protected {
		rec := client.do(route.method, route.path, "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401", route.method, route.path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
			t.Errorf("%s %s content type = %q", route.method, route.path, ct)
		}
	}

	if rec := client.do(http.MethodGet, "/api/v1/health", ""); rec.Code != http.StatusOK {
		t.Errorf("health = %d, want public", rec.Code)
	}
	if rec := client.do(http.MethodGet, "/api/v1/auth/status", ""); rec.Code != http.StatusOK {
		t.Errorf("auth status = %d, want public", rec.Code)
	}
}

func TestDeviceTokenFlow(t *testing.T) {
	env := newTestEnv(t, 5)
	client := env.anon()
	client.setup(t)

	rec := client.do(http.MethodPost, "/api/v1/tokens", `{"name":"cli"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create token = %d, body = %s", rec.Code, rec.Body.String())
	}
	var issued struct {
		Token  string `json:"token"`
		ID     string `json:"id"`
		Prefix string `json:"prefix"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(issued.Token, "lnt.") || !strings.Contains(issued.Prefix, "…") {
		t.Errorf("issued = %+v", issued)
	}

	bearer := env.anon()
	req := bearer.doRaw(http.MethodGet, "/api/v1/index/status", "", map[string]string{
		"Authorization": "Bearer " + issued.Token,
	})
	if req.Code != http.StatusOK {
		t.Fatalf("bearer index status = %d, body = %s", req.Code, req.Body.String())
	}

	// Bearer requests skip CSRF.
	if rec := bearer.doRaw(http.MethodPost, "/api/v1/tokens", `{"name":"ci"}`, map[string]string{
		"Authorization": "Bearer " + issued.Token,
	}); rec.Code != http.StatusCreated {
		t.Errorf("create token via bearer = %d, want 201", rec.Code)
	}

	if rec := client.do(http.MethodDelete, "/api/v1/tokens/"+issued.ID, ""); rec.Code != http.StatusNoContent {
		t.Errorf("revoke = %d, want 204", rec.Code)
	}
	if rec := bearer.doRaw(http.MethodGet, "/api/v1/index/status", "", map[string]string{
		"Authorization": "Bearer " + issued.Token,
	}); rec.Code != http.StatusUnauthorized {
		t.Errorf("revoked bearer = %d, want 401", rec.Code)
	}
}

func TestLoginRateLimitTrips(t *testing.T) {
	env := newTestEnv(t, 5)
	env.anon().setup(t)
	env.clock.Advance(time.Minute + time.Second)

	for i := 0; i < 5; i++ {
		if rec := env.anon().login(t, "wrong password"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d, want 401", i+1, rec.Code)
		}
	}

	rec := env.anon().login(t, "wrong password")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("6th attempt = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("429 response has no Retry-After header")
	}

	env.clock.Advance(time.Minute + time.Second)
	if rec := env.anon().login(t, "wrong password"); rec.Code != http.StatusUnauthorized {
		t.Errorf("attempt after backoff = %d, want 401", rec.Code)
	}
}

func TestPasswordChangeRevokesOtherSessions(t *testing.T) {
	env := newTestEnv(t, 10)
	first := env.anon()
	first.setup(t)

	second := env.anon()
	if rec := second.login(t, testPassword); rec.Code != http.StatusOK {
		t.Fatalf("second login = %d", rec.Code)
	}

	rec := second.do(http.MethodPost, "/api/v1/auth/password",
		`{"current_password":"`+testPassword+`","new_password":"another good password","revoke_tokens":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("password change = %d, body = %s", rec.Code, rec.Body.String())
	}
	second.capture(rec)

	if rec := first.do(http.MethodGet, "/api/v1/auth/me", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("old session = %d, want 401", rec.Code)
	}
	if rec := env.anon().login(t, testPassword); rec.Code != http.StatusUnauthorized {
		t.Errorf("login with old password = %d, want 401", rec.Code)
	}
	if rec := env.anon().login(t, "another good password"); rec.Code != http.StatusOK {
		t.Errorf("login with new password = %d, want 200", rec.Code)
	}
}

func TestSecretsNeverReachTheLog(t *testing.T) {
	env := newTestEnv(t, 5)
	client := env.anon()
	client.setup(t)

	rec := client.do(http.MethodPost, "/api/v1/tokens", `{"name":"log-check"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create token = %d", rec.Code)
	}
	var issued struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	secret := issued.Token[strings.LastIndex(issued.Token, ".")+1:]
	if secret == "" {
		t.Fatal("could not isolate the secret")
	}

	client.do(http.MethodGet, "/api/v1/auth/me", "")
	client.login(t, "wrong password")

	logs := env.logs.String()
	for name, secretValue := range map[string]string{
		"password": testPassword,
		"token":    issued.Token,
		"secret":   secret,
	} {
		if strings.Contains(logs, secretValue) {
			t.Errorf("log output contains the %s", name)
		}
	}
}
