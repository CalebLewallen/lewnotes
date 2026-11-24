package api

import (
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/caleblewallen/lewnotes/server/internal/auth"
)

// authMiddleware is default-deny for /api/v1/*: only the documented public
// routes skip authentication. Unknown API routes are rejected before routing,
// so a new route cannot accidentally ship unauthenticated.
func authMiddleware(opts Options, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/") || isPublicRoute(r) {
			next.ServeHTTP(w, r)
			return
		}
		if opts.Auth == nil {
			writeProblem(w, r, http.StatusServiceUnavailable, "auth_unavailable", "Authentication is not configured.")
			return
		}

		ip := clientIP(r, opts.TrustProxy)
		bearer := bearerToken(r)
		if bearer != "" {
			if allow, retry := opts.VerifyLimiter.Allow("ip:" + ip); !allow {
				writeRateLimited(w, r, retry)
				return
			}
		}

		principal, err := opts.Auth.Authenticate(r.Context(), opts.Auth.SessionFromRequest(r), bearer)
		if err != nil {
			if errors.Is(err, auth.ErrUnauthenticated) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="lewnotes"`)
				writeProblem(w, r, http.StatusUnauthorized, "unauthenticated", "Sign in or provide a valid device token.")
				return
			}
			opts.Logger.Error("authenticate request", "error", err, "path", r.URL.Path)
			writeProblem(w, r, http.StatusInternalServerError, "auth_failed", "Could not verify credentials.")
			return
		}

		if allow, retry := opts.GeneralLimiter.Allow("principal:" + principal.UserID); !allow {
			writeRateLimited(w, r, retry)
			return
		}

		if principal.Kind == "session" && isMutating(r.Method) &&
			!auth.VerifyCSRF(principal.CSRF, r.Header.Get(auth.CSRFHeader)) {
			writeProblem(w, r, http.StatusForbidden, "csrf_failed", "Missing or invalid CSRF token.")
			return
		}

		next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), principal)))
	})
}

func isPublicRoute(r *http.Request) bool {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/health":
		return true
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/auth/status":
		return true
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/setup":
		return true
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/login":
		return true
	}
	return false
}

func isMutating(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if header == "" {
		return ""
	}
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			first, _, _ := strings.Cut(forwarded, ",")
			if ip := strings.TrimSpace(first); ip != "" {
				return ip
			}
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// originFromRequest reconstructs the origin a browser used.
func originFromRequest(r *http.Request, trustProxy bool) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if trustProxy {
		if proto := r.Header.Get("X-Forwarded-Proto"); proto == "https" || proto == "http" {
			scheme = proto
		}
	}
	host := r.Host
	if trustProxy {
		if forwardedHost := r.Header.Get("X-Forwarded-Host"); forwardedHost != "" {
			host = forwardedHost
		}
	}
	return scheme + "://" + host
}

func writeRateLimited(w http.ResponseWriter, r *http.Request, retry time.Duration) {
	seconds := int(retry.Round(time.Second) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeProblem(w, r, http.StatusTooManyRequests, "rate_limited", "Too many attempts. Try again later.")
}

func requestMeta(r *http.Request, opts Options) auth.RequestMeta {
	return auth.RequestMeta{UserAgent: r.UserAgent(), IP: clientIP(r, opts.TrustProxy)}
}
