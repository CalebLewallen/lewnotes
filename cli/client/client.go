// Package client is the typed HTTP client shared by the lewnotes CLI and TUI.
//
// It must never import server internals: the HTTP contract is the boundary.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to one lewnotes server.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// Health mirrors GET /api/v1/health.
type Health struct {
	OK            bool    `json:"ok"`
	Version       string  `json:"version"`
	UptimeSeconds float64 `json:"uptime_seconds"`
}

// New returns a client for baseURL with a conservative timeout.
func New(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    &http.Client{Timeout: 15 * time.Second},
	}
}

// Health checks that the server is reachable.
func (c *Client) Health(ctx context.Context) (Health, error) {
	var out Health
	if err := c.do(ctx, http.MethodGet, "/api/v1/health", &out); err != nil {
		return Health{}, err
	}
	return out, nil
}

func (c *Client) do(ctx context.Context, method, path string, out any) error {
	if c.BaseURL == "" {
		return fmt.Errorf("server URL is not configured")
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("reach %s: %w", c.BaseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, strings.TrimSpace(string(snippet)))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s %s: %w", method, path, err)
	}
	return nil
}
