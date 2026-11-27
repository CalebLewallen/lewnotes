package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/caleblewallen/lewnotes/server/internal/auth"
	"github.com/caleblewallen/lewnotes/server/internal/settings"
)

type instanceSettingsResponse struct {
	PublicURL     string `json:"public_url"`
	Source        string `json:"source"`
	DerivedOrigin string `json:"derived_origin"`
	TrustProxy    bool   `json:"trust_proxy"`
	Listen        string `json:"listen"`
	InstanceName  string `json:"instance_name,omitempty"`
}

type probeResponse struct {
	Valid         bool    `json:"valid"`
	URL           string  `json:"url"`
	Version       string  `json:"version,omitempty"`
	InstanceName  string  `json:"instance_name,omitempty"`
	SetupRequired bool    `json:"setup_required"`
	LatencyMS     float64 `json:"latency_ms"`
}

func handleGetSettings(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, buildSettingsResponse(r, opts))
	}
}

func handleUpdateSettings(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if opts.Settings == nil {
			writeProblem(w, r, http.StatusServiceUnavailable, "settings_unavailable", "Settings are not configured.")
			return
		}
		var body struct {
			PublicURL string `json:"public_url"`
		}
		if err := decodeSmallJSON(r, &body); err != nil {
			writeProblem(w, r, http.StatusBadRequest, "invalid_body", `Request body must be JSON like {"public_url":"https://notes.example.com"}.`)
			return
		}
		origin, err := auth.ValidateServerURL(body.PublicURL)
		if err != nil {
			writeProblem(w, r, http.StatusBadRequest, "invalid_public_url", "Use an absolute http(s) origin such as https://notes.example.com.")
			return
		}
		if opts.PublicURL != "" {
			writeProblem(w, r, http.StatusConflict, "settings_from_config", "public_url is set by server.public_url in config.toml; change it there instead.")
			return
		}
		if err := opts.Settings.Set(r.Context(), settings.PublicURLKey, origin, time.Now()); err != nil {
			opts.Logger.Error("save public URL", "error", err)
			writeProblem(w, r, http.StatusInternalServerError, "settings_failed", "Could not save the setting.")
			return
		}
		writeJSON(w, http.StatusOK, buildSettingsResponse(r, opts))
	}
}

// handleValidateServer pings a candidate origin's health endpoint so the owner
// gets an immediate answer in the settings page instead of discovering a typo
// when the first CLI login fails.
func handleValidateServer(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := auth.FromContext(r.Context())
		if !ok {
			writeProblem(w, r, http.StatusUnauthorized, "unauthenticated", "Sign in first.")
			return
		}
		if allow, retry := opts.CLILoginLimiter.Allow("settings-validate:" + principal.UserID); !allow {
			writeRateLimited(w, r, retry)
			return
		}
		var body struct {
			URL string `json:"url"`
		}
		if err := decodeSmallJSON(r, &body); err != nil {
			writeProblem(w, r, http.StatusBadRequest, "invalid_body", `Request body must be JSON like {"url":"https://notes.example.com"}.`)
			return
		}
		origin, err := auth.ValidateServerURL(body.URL)
		if err != nil {
			writeProblem(w, r, http.StatusBadRequest, "invalid_public_url", "Use an absolute http(s) origin such as https://notes.example.com.")
			return
		}

		start := time.Now()
		probe, err := probeInstance(r.Context(), origin)
		if err != nil {
			writeProblem(w, r, http.StatusBadGateway, "unreachable", fmt.Sprintf("%s did not answer like a lewnotes server: %v", origin, err))
			return
		}
		probe.LatencyMS = float64(time.Since(start).Microseconds()) / 1000
		writeJSON(w, http.StatusOK, probe)
	}
}

func buildSettingsResponse(r *http.Request, opts Options) instanceSettingsResponse {
	origin, source, err := effectivePublicURL(r, opts)
	if err != nil {
		opts.Logger.Error("resolve public URL", "error", err)
		origin, source = originFromRequest(r, opts.TrustProxy), "derived"
	}
	instanceName, _ := opts.Auth.InstanceName(r.Context())
	return instanceSettingsResponse{
		PublicURL:     origin,
		Source:        source,
		DerivedOrigin: originFromRequest(r, opts.TrustProxy),
		TrustProxy:    opts.TrustProxy,
		Listen:        opts.Listen,
		InstanceName:  instanceName,
	}
}

// probeInstance asks an origin for its health document, then its auth status.
// Redirects are not followed: the address the owner typed must answer itself.
func probeInstance(ctx context.Context, origin string) (probeResponse, error) {
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	health, err := getJSON(ctx, client, origin+"/api/v1/health")
	if err != nil {
		return probeResponse{}, err
	}
	var healthBody struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(health, &healthBody); err != nil || !healthBody.OK {
		return probeResponse{}, errors.New("health response was not a lewnotes health document")
	}

	probe := probeResponse{Valid: true, URL: origin, Version: healthBody.Version}
	// Auth status is not required for validity, but it tells the owner whether
	// the instance has been set up and what it calls itself.
	if authStatus, err := getJSON(ctx, client, origin+"/api/v1/auth/status"); err == nil {
		var status struct {
			SetupRequired bool   `json:"setup_required"`
			InstanceName  string `json:"instance_name"`
		}
		if json.Unmarshal(authStatus, &status) == nil {
			probe.SetupRequired = status.SetupRequired
			probe.InstanceName = status.InstanceName
		}
	}
	return probe, nil
}

func getJSON(ctx context.Context, client *http.Client, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Lewnotes-Client", "web")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("returned %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<10))
}
