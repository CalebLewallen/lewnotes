package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/caleblewallen/lewnotes/server/internal/auth"
	"github.com/caleblewallen/lewnotes/server/internal/clilogin"
)

const (
	handoffHeader    = "X-Lewnotes-Handoff"
	maxJSONBodyBytes = 1 << 20
)

type createLoginRequest struct {
	PublicKey   string `json:"public_key"`
	State       string `json:"state"`
	DeviceName  string `json:"device_name"`
	Client      string `json:"client"`
	RedirectURI string `json:"redirect_uri"`
}

type loginCreatedResponse struct {
	ID          string `json:"id"`
	ApproveURL  string `json:"approve_url"`
	DeviceURL   string `json:"device_url"`
	UserCode    string `json:"user_code"`
	ExpiresAt   string `json:"expires_at"`
	PollAfterMS int    `json:"poll_after_ms"`
}

type loginPreviewResponse struct {
	ID         string `json:"id"`
	DeviceName string `json:"device_name"`
	Client     string `json:"client"`
	CreatedAt  string `json:"created_at"`
	ExpiresAt  string `json:"expires_at"`
	Server     string `json:"server"`
	Status     string `json:"status"`
	ApproveURL string `json:"approve_url,omitempty"`
}

func handleCreateCLILogin(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if opts.CLILogin == nil {
			writeProblem(w, r, http.StatusServiceUnavailable, "login_unavailable", "CLI login is not configured.")
			return
		}
		if allow, retry := opts.CLILoginLimiter.Allow("cli-login:" + clientIP(r, opts.TrustProxy)); !allow {
			writeRateLimited(w, r, retry)
			return
		}

		var req createLoginRequest
		if err := decodeSmallJSON(r, &req); err != nil {
			writeProblem(w, r, http.StatusBadRequest, "invalid_body", "Request body must be JSON.")
			return
		}
		publicKey, err := base64.RawURLEncoding.Strict().DecodeString(strings.TrimSpace(req.PublicKey))
		if err != nil || len(publicKey) != 32 {
			writeProblem(w, r, http.StatusBadRequest, "invalid_public_key", "public_key must be a base64url X25519 public key.")
			return
		}
		state := strings.TrimSpace(req.State)
		if len(state) < 32 || len(state) > 128 {
			writeProblem(w, r, http.StatusBadRequest, "invalid_state", "state must be 32-128 characters.")
			return
		}
		deviceName := strings.TrimSpace(req.DeviceName)
		if deviceName == "" {
			deviceName = "unnamed device"
		}
		if len([]rune(deviceName)) > 100 {
			writeProblem(w, r, http.StatusBadRequest, "invalid_device_name", "device_name must be at most 100 characters.")
			return
		}
		client := strings.TrimSpace(req.Client)
		if client == "" {
			client = "lewnotes/unknown"
		}
		if len(client) > 100 {
			client = client[:100]
		}
		redirectURI := strings.TrimSpace(req.RedirectURI)
		if redirectURI != "" {
			if err := validateLoopbackRedirect(redirectURI); err != nil {
				writeProblem(w, r, http.StatusBadRequest, "invalid_redirect_uri", err.Error())
				return
			}
		}

		origin, _, err := effectivePublicURL(r, opts)
		if err != nil {
			opts.Logger.Error("resolve public URL", "error", err)
			writeProblem(w, r, http.StatusInternalServerError, "public_url_failed", "Could not determine this server's public URL.")
			return
		}

		created, err := opts.CLILogin.Create(r.Context(), clilogin.CreateParams{
			PublicKey:    publicKey,
			State:        state,
			DeviceName:   deviceName,
			Client:       client,
			RedirectURI:  redirectURI,
			ServerOrigin: origin,
		})
		if err != nil {
			opts.Logger.Error("create CLI login", "error", err)
			writeProblem(w, r, http.StatusInternalServerError, "login_failed", "Could not start the login request.")
			return
		}

		writeJSON(w, http.StatusCreated, loginCreatedResponse{
			ID:          created.ID,
			ApproveURL:  origin + "/cli/authorize?id=" + url.QueryEscape(created.ID),
			DeviceURL:   origin + "/cli?code=" + url.QueryEscape(clilogin.DisplayUserCode(created.UserCode)),
			UserCode:    clilogin.DisplayUserCode(created.UserCode),
			ExpiresAt:   created.ExpiresAt.Format(time.RFC3339),
			PollAfterMS: 1000,
		})
	}
}

func handlePollCLILogin(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if opts.CLILogin == nil {
			writeProblem(w, r, http.StatusServiceUnavailable, "login_unavailable", "CLI login is not configured.")
			return
		}
		id := r.PathValue("id")
		if allow, retry := opts.CLIPollLimiter.Allow("cli-poll:" + id); !allow {
			writeRateLimited(w, r, retry)
			return
		}

		req, err := opts.CLILogin.Get(r.Context(), id)
		if err != nil {
			// Unknown ids and wrong states are indistinguishable.
			writeProblem(w, r, http.StatusNotFound, "login_not_found", "No such login request.")
			return
		}
		if !opts.CLILogin.VerifyState(req, r.Header.Get(handoffHeader)) {
			writeProblem(w, r, http.StatusNotFound, "login_not_found", "No such login request.")
			return
		}

		switch req.Status {
		case clilogin.StatusPending:
			writeJSON(w, http.StatusOK, map[string]string{
				"status":     clilogin.StatusPending,
				"expires_at": req.ExpiresAt.Format(time.RFC3339),
			})
		case clilogin.StatusApproved:
			blob, err := opts.CLILogin.TakeHandoff(r.Context(), id, r.Header.Get(handoffHeader))
			if errors.Is(err, clilogin.ErrConsumed) {
				writeProblem(w, r, http.StatusGone, "login_consumed", "This login request was already used.")
				return
			}
			if err != nil {
				opts.Logger.Error("take login handoff", "error", err)
				writeProblem(w, r, http.StatusInternalServerError, "login_failed", "Could not complete the login.")
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": clilogin.StatusApproved, "handoff": blob})
		case clilogin.StatusConsumed:
			writeProblem(w, r, http.StatusGone, "login_consumed", "This login request was already used.")
		case clilogin.StatusExpired:
			writeJSON(w, http.StatusOK, map[string]string{"status": clilogin.StatusExpired})
		default:
			writeJSON(w, http.StatusOK, map[string]string{"status": req.Status})
		}
	}
}

func handlePreviewCLILogin(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if opts.CLILogin == nil {
			writeProblem(w, r, http.StatusServiceUnavailable, "login_unavailable", "CLI login is not configured.")
			return
		}
		req, err := opts.CLILogin.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			writeProblem(w, r, http.StatusNotFound, "login_not_found", "No such login request.")
			return
		}
		writeJSON(w, http.StatusOK, loginPreviewResponse{
			ID:         req.ID,
			DeviceName: req.DeviceName,
			Client:     req.Client,
			CreatedAt:  req.CreatedAt.Format(time.RFC3339),
			ExpiresAt:  req.ExpiresAt.Format(time.RFC3339),
			Server:     req.ServerOrigin,
			Status:     req.Status,
		})
	}
}

func handleApproveCLILogin(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := auth.FromContext(r.Context())
		if !ok {
			writeProblem(w, r, http.StatusUnauthorized, "unauthenticated", "Sign in to approve this device.")
			return
		}
		if opts.CLILogin == nil {
			writeProblem(w, r, http.StatusServiceUnavailable, "login_unavailable", "CLI login is not configured.")
			return
		}
		var body struct {
			DeviceName string `json:"device_name"`
		}
		if r.ContentLength > 0 {
			if err := decodeSmallJSON(r, &body); err != nil {
				writeProblem(w, r, http.StatusBadRequest, "invalid_body", "Request body must be JSON.")
				return
			}
		}
		deviceName := strings.TrimSpace(body.DeviceName)
		if len([]rune(deviceName)) > 100 {
			writeProblem(w, r, http.StatusBadRequest, "invalid_device_name", "device_name must be at most 100 characters.")
			return
		}

		req, err := opts.CLILogin.Approve(r.Context(), r.PathValue("id"), principal.UserID, principal.Username, deviceName)
		switch {
		case errors.Is(err, clilogin.ErrNotFound):
			writeProblem(w, r, http.StatusNotFound, "login_not_found", "No such login request.")
			return
		case errors.Is(err, clilogin.ErrExpired):
			writeProblem(w, r, http.StatusGone, "login_expired", "This login request expired.")
			return
		case errors.Is(err, clilogin.ErrNotPending):
			writeProblem(w, r, http.StatusConflict, "login_not_pending", "This login request is no longer pending.")
			return
		case err != nil:
			opts.Logger.Error("approve CLI login", "error", err)
			writeProblem(w, r, http.StatusInternalServerError, "login_failed", "Could not approve the login.")
			return
		}

		redirectURL := ""
		if req.RedirectURI != "" {
			redirectURL = req.RedirectURI + "?id=" + url.QueryEscape(req.ID)
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"status":       clilogin.StatusApproved,
			"device_name":  req.DeviceName,
			"redirect_url": redirectURL,
		})
	}
}

func handleDenyCLILogin(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := auth.FromContext(r.Context())
		if !ok {
			writeProblem(w, r, http.StatusUnauthorized, "unauthenticated", "Sign in to deny this device.")
			return
		}
		if opts.CLILogin == nil {
			writeProblem(w, r, http.StatusServiceUnavailable, "login_unavailable", "CLI login is not configured.")
			return
		}
		err := opts.CLILogin.Deny(r.Context(), r.PathValue("id"), principal.UserID)
		switch {
		case errors.Is(err, clilogin.ErrNotPending):
			writeProblem(w, r, http.StatusConflict, "login_not_pending", "This login request is no longer pending.")
			return
		case err != nil:
			opts.Logger.Error("deny CLI login", "error", err)
			writeProblem(w, r, http.StatusInternalServerError, "login_failed", "Could not deny the login.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": clilogin.StatusDenied})
	}
}

func handleResolveCLILogin(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := auth.FromContext(r.Context())
		if !ok {
			writeProblem(w, r, http.StatusUnauthorized, "unauthenticated", "Sign in to approve this device.")
			return
		}
		if opts.CLILogin == nil {
			writeProblem(w, r, http.StatusServiceUnavailable, "login_unavailable", "CLI login is not configured.")
			return
		}
		if allow, retry := opts.CLILoginLimiter.Allow("cli-resolve:" + principal.UserID); !allow {
			writeRateLimited(w, r, retry)
			return
		}
		var body struct {
			UserCode string `json:"user_code"`
		}
		if err := decodeSmallJSON(r, &body); err != nil {
			writeProblem(w, r, http.StatusBadRequest, "invalid_body", `Request body must be JSON like {"user_code":"7K4M-9QRT"}.`)
			return
		}
		req, err := opts.CLILogin.FindByUserCode(r.Context(), body.UserCode)
		if errors.Is(err, clilogin.ErrInvalidCode) {
			writeProblem(w, r, http.StatusNotFound, "invalid_login_code", "That code is not valid or has expired.")
			return
		}
		if err != nil {
			opts.Logger.Error("resolve CLI login", "error", err)
			writeProblem(w, r, http.StatusInternalServerError, "login_failed", "Could not look up the code.")
			return
		}
		origin, _, _ := effectivePublicURL(r, opts)
		writeJSON(w, http.StatusOK, loginPreviewResponse{
			ID:         req.ID,
			DeviceName: req.DeviceName,
			Client:     req.Client,
			CreatedAt:  req.CreatedAt.Format(time.RFC3339),
			ExpiresAt:  req.ExpiresAt.Format(time.RFC3339),
			Server:     req.ServerOrigin,
			Status:     req.Status,
			ApproveURL: origin + "/cli/authorize?id=" + url.QueryEscape(req.ID),
		})
	}
}

func handleCancelCLILogin(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if opts.CLILogin == nil {
			writeProblem(w, r, http.StatusServiceUnavailable, "login_unavailable", "CLI login is not configured.")
			return
		}
		req, err := opts.CLILogin.Get(r.Context(), r.PathValue("id"))
		if err != nil || !opts.CLILogin.VerifyState(req, r.Header.Get(handoffHeader)) {
			writeProblem(w, r, http.StatusNotFound, "login_not_found", "No such login request.")
			return
		}
		if err := opts.CLILogin.Cancel(r.Context(), req.ID); err != nil {
			opts.Logger.Error("cancel CLI login", "error", err)
			writeProblem(w, r, http.StatusInternalServerError, "login_failed", "Could not cancel the login.")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// effectivePublicURL resolves the origin baked into tokens and login links:
// config wins, then the runtime setting, then the request's own origin.
func effectivePublicURL(r *http.Request, opts Options) (string, string, error) {
	if opts.PublicURL != "" {
		return strings.TrimRight(opts.PublicURL, "/"), "config", nil
	}
	if opts.Settings != nil {
		value, ok, err := opts.Settings.Get(r.Context(), "public_url")
		if err != nil {
			return "", "", err
		}
		if ok && value != "" {
			return strings.TrimRight(value, "/"), "settings", nil
		}
	}
	return originFromRequest(r, opts.TrustProxy), "derived", nil
}

func validateLoopbackRedirect(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("redirect_uri must be a valid URL")
	}
	if u.Scheme != "http" {
		return errors.New("redirect_uri must use http")
	}
	host := u.Hostname()
	if host != "127.0.0.1" && host != "::1" {
		return errors.New("redirect_uri must point at a loopback address")
	}
	if u.Port() == "" || u.Path != "/cb" {
		return errors.New("redirect_uri must include an explicit port and the /cb path")
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return errors.New("redirect_uri must not contain a query, fragment, or credentials")
	}
	if _, _, err := net.SplitHostPort(u.Host); err != nil {
		return errors.New("redirect_uri must include an explicit port")
	}
	return nil
}

func decodeSmallJSON(r *http.Request, out any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxJSONBodyBytes))
	return decoder.Decode(out)
}
