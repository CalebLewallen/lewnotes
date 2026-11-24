package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/caleblewallen/lewnotes/server/internal/auth"
)

type tokenView struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	Scope      string  `json:"scope"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
	UserAgent  string  `json:"user_agent,omitempty"`
}

type issuedTokenResponse struct {
	Token string `json:"token"`
	tokenView
}

func handleListTokens(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := auth.FromContext(r.Context())
		if !ok {
			writeProblem(w, r, http.StatusUnauthorized, "unauthenticated", "Sign in or provide a valid device token.")
			return
		}

		tokens, err := opts.Auth.ListTokens(r.Context(), principal.UserID)
		if err != nil {
			opts.Logger.Error("list tokens failed", "error", err)
			writeProblem(w, r, http.StatusInternalServerError, "tokens_failed", "Could not list device tokens.")
			return
		}

		views := make([]tokenView, 0, len(tokens))
		for _, token := range tokens {
			views = append(views, viewOfToken(token))
		}

		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, map[string]any{"tokens": views})
	}
}

func handleCreateToken(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := auth.FromContext(r.Context())
		if !ok {
			writeProblem(w, r, http.StatusUnauthorized, "unauthenticated", "Sign in or provide a valid device token.")
			return
		}

		var req struct {
			Name string `json:"name"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			writeProblem(w, r, http.StatusBadRequest, "invalid_body", `Request body must be JSON like {"name":"caleb-laptop"}.`)
			return
		}

		origin := opts.PublicURL
		if origin == "" {
			origin = originFromRequest(r, opts.TrustProxy)
		}

		issued, err := opts.Auth.CreateToken(r.Context(), principal.UserID, req.Name, origin, r.UserAgent())
		switch {
		case errors.Is(err, auth.ErrInvalidTokenName):
			writeProblem(w, r, http.StatusBadRequest, "invalid_token_name", "Token name must be 1-100 characters.")
			return
		case errors.Is(err, auth.ErrInvalidServerURL):
			opts.Logger.Error("create token with invalid server URL", "error", err)
			writeProblem(w, r, http.StatusInternalServerError, "token_failed", "Could not determine this server's public URL.")
			return
		case err != nil:
			opts.Logger.Error("create token failed", "error", err)
			writeProblem(w, r, http.StatusInternalServerError, "token_failed", "Could not create the device token.")
			return
		}

		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusCreated, issuedTokenResponse{
			Token:     issued.Raw,
			tokenView: viewOfToken(issued.Record),
		})
	}
}

func handleDeleteToken(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := auth.FromContext(r.Context())
		if !ok {
			writeProblem(w, r, http.StatusUnauthorized, "unauthenticated", "Sign in or provide a valid device token.")
			return
		}

		err := opts.Auth.RevokeToken(r.Context(), principal.UserID, r.PathValue("id"))
		switch {
		case errors.Is(err, auth.ErrTokenNotFound):
			writeProblem(w, r, http.StatusNotFound, "token_not_found", "No such device token.")
			return
		case err != nil:
			opts.Logger.Error("revoke token failed", "error", err)
			writeProblem(w, r, http.StatusInternalServerError, "token_failed", "Could not revoke the device token.")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func viewOfToken(token auth.Token) tokenView {
	view := tokenView{
		ID:        token.ID,
		Name:      token.Name,
		Prefix:    token.Prefix,
		Scope:     token.Scope,
		CreatedAt: token.CreatedAt.UTC().Format(time.RFC3339),
		UserAgent: token.UserAgent,
	}
	if token.LastUsedAt != nil {
		lastUsed := token.LastUsedAt.UTC().Format(time.RFC3339)
		view.LastUsedAt = &lastUsed
	}
	return view
}
