// Package api wires HTTP routes for lewnotes-core.
//
// The API is the product: every capability of every client is reachable here.
// Routes are added slice by slice (see .agents/plans/2026-11-24-build-slices.md).
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// Options configures the root handler.
type Options struct {
	Version string
	Started time.Time
	Logger  *slog.Logger
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

// NewRouter returns the root HTTP handler.
func NewRouter(opts Options) http.Handler {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Started.IsZero() {
		opts.Started = time.Now()
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, healthResponse{
			OK:            true,
			Version:       opts.Version,
			UptimeSeconds: time.Since(opts.Started).Seconds(),
		})
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, r, http.StatusNotFound, "not_found", "No route matches this path.")
	})

	return recoverPanics(opts.Logger, requestLogger(opts.Logger, securityHeaders(mux)))
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
