// Package api wires HTTP routes for lewnotes-core.
//
// The API is the product: every capability of every client is reachable here.
// Routes are added slice by slice (see .agents/plans/2026-11-24-build-slices.md).
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/caleblewallen/lewnotes/server/internal/auth"
	"github.com/caleblewallen/lewnotes/server/internal/clilogin"
	"github.com/caleblewallen/lewnotes/server/internal/events"
	"github.com/caleblewallen/lewnotes/server/internal/index"
	"github.com/caleblewallen/lewnotes/server/internal/notes"
	"github.com/caleblewallen/lewnotes/server/internal/settings"
	"github.com/caleblewallen/lewnotes/server/internal/webui"
)

// Options configures the root handler.
type Options struct {
	Version string
	Started time.Time
	Logger  *slog.Logger
	Index   *index.Index
	Scanner *index.Scanner

	Auth     *auth.Service
	Notes    *notes.Service
	Events   *events.Hub
	Settings *settings.Store
	CLILogin *clilogin.Service

	TrustProxy bool
	PublicURL  string
	Listen     string

	LoginLimiter    *auth.Limiter
	VerifyLimiter   *auth.Limiter
	GeneralLimiter  *auth.Limiter
	CLILoginLimiter *auth.Limiter
	CLIPollLimiter  *auth.Limiter
}

type healthResponse struct {
	OK            bool    `json:"ok"`
	Version       string  `json:"version"`
	UptimeSeconds float64 `json:"uptime_seconds"`
}

type problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
}

type routeSpec struct {
	Method  string
	Pattern string
	Handler http.HandlerFunc
}

// buildRoutes is the single route table. Tests compare it against the OpenAPI
// document so a route cannot ship undocumented.
func buildRoutes(opts Options) []routeSpec {
	return []routeSpec{
		{"GET", "/api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, healthResponse{
				OK:            true,
				Version:       opts.Version,
				UptimeSeconds: time.Since(opts.Started).Seconds(),
			})
		}},

		{"GET", "/api/v1/meta", handleMeta(opts)},
		{"GET", "/api/v1/index/status", handleIndexStatus(opts)},
		{"POST", "/api/v1/index/rescan", handleIndexRescan(opts)},

		{"GET", "/api/v1/auth/status", handleAuthStatus(opts)},
		{"POST", "/api/v1/auth/setup", handleSetup(opts)},
		{"POST", "/api/v1/auth/login", handleLogin(opts)},
		{"POST", "/api/v1/auth/logout", handleLogout(opts)},
		{"GET", "/api/v1/auth/me", handleMe(opts)},
		{"POST", "/api/v1/auth/password", handlePassword(opts)},

		{"GET", "/api/v1/tokens", handleListTokens(opts)},
		{"POST", "/api/v1/tokens", handleCreateToken(opts)},
		{"DELETE", "/api/v1/tokens/{id}", handleDeleteToken(opts)},

		{"GET", "/api/v1/settings", handleGetSettings(opts)},
		{"PATCH", "/api/v1/settings", handleUpdateSettings(opts)},
		{"POST", "/api/v1/settings/validate", handleValidateServer(opts)},

		{"POST", "/api/v1/cli/login", handleCreateCLILogin(opts)},
		{"GET", "/api/v1/cli/login/{id}", handlePollCLILogin(opts)},
		{"DELETE", "/api/v1/cli/login/{id}", handleCancelCLILogin(opts)},
		{"GET", "/api/v1/cli/login/{id}/preview", handlePreviewCLILogin(opts)},
		{"POST", "/api/v1/cli/login/{id}/approve", handleApproveCLILogin(opts)},
		{"POST", "/api/v1/cli/login/{id}/deny", handleDenyCLILogin(opts)},
		{"POST", "/api/v1/cli/login/resolve", handleResolveCLILogin(opts)},

		{"GET", "/api/v1/notes", handleListNotes(opts)},
		{"POST", "/api/v1/notes", handleCreateNote(opts)},
		{"GET", "/api/v1/notes/by-path", handleNoteByPath(opts)},
		{"GET", "/api/v1/notes/{id}", handleGetNote(opts)},
		{"PUT", "/api/v1/notes/{id}", handleUpdateNote(opts)},
		{"PATCH", "/api/v1/notes/{id}", handlePatchNote(opts)},
		{"DELETE", "/api/v1/notes/{id}", handleDeleteNote(opts)},
		{"POST", "/api/v1/notes/{id}/move", handleMoveNote(opts)},
		{"GET", "/api/v1/notes/{id}/links", handleNoteLinks(opts)},
		{"GET", "/api/v1/notes/{id}/backlinks", handleNoteBacklinks(opts)},

		{"POST", "/api/v1/dirs", handleCreateDir(opts)},
		{"POST", "/api/v1/dirs/move", handleMoveDir(opts)},
		{"GET", "/api/v1/tree", handleTree(opts)},
		{"GET", "/api/v1/files/{path...}", handleFile(opts)},
		{"GET", "/api/v1/search", handleSearch(opts)},
		{"GET", "/api/v1/events", handleEvents(opts)},
		{"GET", "/api/v1/openapi.json", handleOpenAPI()},
	}
}

// RoutePatterns lists "METHOD pattern" for every registered API route.
func RoutePatterns() []string {
	specs := buildRoutes(Options{})
	patterns := make([]string, 0, len(specs))
	for _, spec := range specs {
		patterns = append(patterns, spec.Method+" "+spec.Pattern)
	}
	return patterns
}

// NewRouter returns the root HTTP handler.
func NewRouter(opts Options) http.Handler {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Started.IsZero() {
		opts.Started = time.Now()
	}
	if opts.LoginLimiter == nil {
		opts.LoginLimiter = auth.NewLimiter(auth.LimiterOptions{Limit: 5, Window: time.Minute})
	}
	if opts.VerifyLimiter == nil {
		opts.VerifyLimiter = auth.NewLimiter(auth.LimiterOptions{Limit: 30, Window: time.Minute})
	}
	if opts.GeneralLimiter == nil {
		opts.GeneralLimiter = auth.NewLimiter(auth.LimiterOptions{Limit: 600, Window: time.Minute})
	}
	if opts.CLILoginLimiter == nil {
		opts.CLILoginLimiter = auth.NewLimiter(auth.LimiterOptions{Limit: 10, Window: time.Minute})
	}
	if opts.CLIPollLimiter == nil {
		opts.CLIPollLimiter = auth.NewLimiter(auth.LimiterOptions{Limit: 60, Window: time.Minute})
	}

	mux := http.NewServeMux()
	for _, spec := range buildRoutes(opts) {
		mux.HandleFunc(spec.Method+" "+spec.Pattern, spec.Handler)
	}
	spa := webui.Handler()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Unknown API paths stay machine-readable; every other unknown path
		// belongs to a client-side SvelteKit route.
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeProblem(w, r, http.StatusNotFound, "not_found", "No route matches this path.")
			return
		}
		spa.ServeHTTP(w, r)
	})

	return recoverPanics(opts.Logger, requestLogger(opts.Logger, securityHeaders(authMiddleware(opts, mux))))
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Default().Error("encode response", "error", err)
	}
}

func writeProblem(w http.ResponseWriter, r *http.Request, status int, kind, detail string) {
	body := problem{
		Type:   "https://lewnotes.dev/errors/" + kind,
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
	}
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Default().Error("encode problem", "error", err, "path", r.URL.Path)
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(status int) {
	s.status = status
	s.ResponseWriter.WriteHeader(status)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusRecorder) Flush() {
	if flusher, ok := s.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", float64(time.Since(start).Microseconds())/1000,
		)
	})
}

func recoverPanics(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				logger.Error("panic", "value", v, "path", r.URL.Path)
				writeProblem(w, r, http.StatusInternalServerError, "internal", "Unexpected server error.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
