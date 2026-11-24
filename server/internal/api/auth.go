package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/caleblewallen/lewnotes/server/internal/auth"
)

type authStatusResponse struct {
	SetupRequired bool   `json:"setup_required"`
	InstanceName  string `json:"instance_name,omitempty"`
	Version       string `json:"version,omitempty"`
}

type tokenIdentity struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Scope string `json:"scope"`
}

type authResponse struct {
	Username     string         `json:"username"`
	Auth         string         `json:"auth"`
	CSRFToken    string         `json:"csrf_token,omitempty"`
	InstanceName string         `json:"instance_name,omitempty"`
	Token        *tokenIdentity `json:"token,omitempty"`
}

func handleAuthStatus(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if opts.Auth == nil {
			writeProblem(w, r, http.StatusServiceUnavailable, "auth_unavailable", "Authentication is not configured.")
			return
		}

		setupRequired, err := opts.Auth.SetupRequired(r.Context())
		if err != nil {
			opts.Logger.Error("read auth status", "error", err)
			writeProblem(w, r, http.StatusInternalServerError, "auth_status_failed", "Could not read authentication status.")
			return
		}

		response := authStatusResponse{SetupRequired: setupRequired, Version: opts.Version}
		if !setupRequired {
			name, err := opts.Auth.InstanceName(r.Context())
			if err != nil {
				opts.Logger.Error("read instance name", "error", err)
				writeProblem(w, r, http.StatusInternalServerError, "auth_status_failed", "Could not read authentication status.")
				return
			}
			response.InstanceName = name
		}

		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, response)
	}
}

func handleSetup(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if opts.Auth == nil {
			writeProblem(w, r, http.StatusServiceUnavailable, "auth_unavailable", "Authentication is not configured.")
			return
		}
		if allow, retry := opts.LoginLimiter.Allow("ip:" + clientIP(r, opts.TrustProxy)); !allow {
			writeRateLimited(w, r, retry)
			return
		}

		var req struct {
			Password     string `json:"password"`
			InstanceName string `json:"instance_name"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			writeProblem(w, r, http.StatusBadRequest, "invalid_body", `Request body must be JSON like {"password":"…","instance_name":"home"}.`)
			return
		}

		session, user, err := opts.Auth.Setup(r.Context(), req.Password, req.InstanceName, requestMeta(r, opts))
		switch {
		case errors.Is(err, auth.ErrAlreadySetup):
			writeProblem(w, r, http.StatusConflict, "setup_complete", "This instance is already set up. Sign in instead.")
			return
		case isPasswordPolicyError(err):
			writeProblem(w, r, http.StatusBadRequest, "weak_password", err.Error())
			return
		case err != nil:
			opts.Logger.Error("setup failed", "error", err)
			writeProblem(w, r, http.StatusInternalServerError, "setup_failed", "Could not create the owner account.")
			return
		}

		opts.Auth.SetSessionCookies(w, session)
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusCreated, authResponse{
			Username:     user.Username,
			Auth:         "session",
			CSRFToken:    session.CSRF,
			InstanceName: user.InstanceName,
		})
	}
}

func handleLogin(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if opts.Auth == nil {
			writeProblem(w, r, http.StatusServiceUnavailable, "auth_unavailable", "Authentication is not configured.")
			return
		}

		ip := clientIP(r, opts.TrustProxy)
		if allow, retry := opts.LoginLimiter.Allow("ip:" + ip); !allow {
			writeRateLimited(w, r, retry)
			return
		}

		var req struct {
			Password string `json:"password"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			writeProblem(w, r, http.StatusBadRequest, "invalid_body", `Request body must be JSON like {"password":"…"}.`)
			return
		}

		session, user, err := opts.Auth.Login(r.Context(), req.Password, requestMeta(r, opts))
		switch {
		case errors.Is(err, auth.ErrNotSetup):
			writeProblem(w, r, http.StatusConflict, "setup_required", "This instance has no owner yet. Complete setup first.")
			return
		case errors.Is(err, auth.ErrInvalidCredentials):
			// Deliberately uniform: never reveal whether the user exists.
			writeProblem(w, r, http.StatusUnauthorized, "invalid_credentials", "Incorrect password.")
			return
		case err != nil:
			opts.Logger.Error("login failed", "error", err)
			writeProblem(w, r, http.StatusInternalServerError, "login_failed", "Could not sign in.")
			return
		}

		opts.Auth.SetSessionCookies(w, session)
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, authResponse{
			Username:     user.Username,
			Auth:         "session",
			CSRFToken:    session.CSRF,
			InstanceName: user.InstanceName,
		})
	}
}

func handleLogout(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if opts.Auth == nil {
			writeProblem(w, r, http.StatusServiceUnavailable, "auth_unavailable", "Authentication is not configured.")
			return
		}
		if err := opts.Auth.Logout(r.Context(), opts.Auth.SessionFromRequest(r)); err != nil {
			opts.Logger.Error("logout failed", "error", err)
			writeProblem(w, r, http.StatusInternalServerError, "logout_failed", "Could not sign out.")
			return
		}
		opts.Auth.ClearCookies(w)
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleMe(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := auth.FromContext(r.Context())
		if !ok {
			writeProblem(w, r, http.StatusUnauthorized, "unauthenticated", "Sign in or provide a valid device token.")
			return
		}

		response := authResponse{Username: principal.Username, Auth: principal.Kind}
		if principal.Kind == "session" {
			response.CSRFToken = principal.CSRF
		} else {
			response.Token = &tokenIdentity{ID: principal.TokenID, Name: principal.TokenName, Scope: principal.Scope}
		}
		if name, err := opts.Auth.InstanceName(r.Context()); err == nil {
			response.InstanceName = name
		} else {
			opts.Logger.Warn("read instance name for me", "error", err)
		}

		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, response)
	}
}

func handlePassword(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := auth.FromContext(r.Context())
		if !ok {
			writeProblem(w, r, http.StatusUnauthorized, "unauthenticated", "Sign in or provide a valid device token.")
			return
		}
		if opts.Auth == nil {
			writeProblem(w, r, http.StatusServiceUnavailable, "auth_unavailable", "Authentication is not configured.")
			return
		}

		var req struct {
			CurrentPassword string `json:"current_password"`
			NewPassword     string `json:"new_password"`
			RevokeTokens    bool   `json:"revoke_tokens"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			writeProblem(w, r, http.StatusBadRequest, "invalid_body",
				`Request body must be JSON like {"current_password":"…","new_password":"…","revoke_tokens":false}.`)
			return
		}

		session, err := opts.Auth.ChangePassword(r.Context(), principal.UserID,
			req.CurrentPassword, req.NewPassword, req.RevokeTokens, requestMeta(r, opts))
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			writeProblem(w, r, http.StatusBadRequest, "invalid_password", "Current password is incorrect.")
			return
		case isPasswordPolicyError(err):
			writeProblem(w, r, http.StatusBadRequest, "weak_password", err.Error())
			return
		case err != nil:
			opts.Logger.Error("change password failed", "error", err)
			writeProblem(w, r, http.StatusInternalServerError, "password_failed", "Could not change the password.")
			return
		}

		opts.Auth.SetSessionCookies(w, session)
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, map[string]string{"csrf_token": session.CSRF})
	}
}

func isPasswordPolicyError(err error) bool {
	var policy *auth.PasswordPolicyError
	return errors.As(err, &policy)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("unexpected trailing data")
	}
	return nil
}
