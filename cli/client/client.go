// Package client is the typed HTTP client shared by the lewnotes CLI and TUI.
//
// It must never import server internals: the HTTP contract is the boundary.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrUnreachable marks transport failures so callers can exit with code 6.
var ErrUnreachable = errors.New("server unreachable")

// APIError is a problem+json response from the server.
type APIError struct {
	Status int
	Type   string
	Title  string
	Detail string
}

func (e *APIError) Error() string {
	message := e.Detail
	if message == "" {
		message = e.Title
	}
	if message == "" {
		message = http.StatusText(e.Status)
	}
	return fmt.Sprintf("%s (%d)", message, e.Status)
}

// IsAuth reports whether the request was rejected for missing or bad credentials.
func (e *APIError) IsAuth() bool {
	return e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
}

// IsConflict reports precondition and uniqueness failures.
func (e *APIError) IsConflict() bool {
	return e.Status == http.StatusConflict ||
		e.Status == http.StatusPreconditionFailed ||
		e.Status == http.StatusPreconditionRequired
}

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

// AuthStatus mirrors GET /api/v1/auth/status.
type AuthStatus struct {
	SetupRequired bool   `json:"setup_required"`
	InstanceName  string `json:"instance_name,omitempty"`
	Version       string `json:"version,omitempty"`
}

// TokenIdentity describes the device token used for an authenticated call.
type TokenIdentity struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Scope string `json:"scope"`
}

// Me mirrors GET /api/v1/auth/me.
type Me struct {
	Username     string         `json:"username"`
	Auth         string         `json:"auth"`
	CSRFToken    string         `json:"csrf_token,omitempty"`
	InstanceName string         `json:"instance_name,omitempty"`
	Token        *TokenIdentity `json:"token,omitempty"`
}

// Note is a full note as returned by the notes endpoints.
type Note struct {
	ID          string         `json:"id"`
	Path        string         `json:"path"`
	Title       string         `json:"title"`
	ETag        string         `json:"etag"`
	Size        int64          `json:"size"`
	UpdatedAt   string         `json:"updated_at"`
	Tags        []string       `json:"tags"`
	Frontmatter map[string]any `json:"frontmatter"`
	Content     string         `json:"content,omitempty"`
	Body        string         `json:"body"`
	Links       []Link         `json:"links"`
	Backlinks   []Backlink     `json:"backlinks"`
	Warnings    []string       `json:"warnings,omitempty"`
}

// Link is one outgoing wikilink with its resolution state.
type Link struct {
	ID         string   `json:"id,omitempty"`
	Path       string   `json:"path,omitempty"`
	Title      string   `json:"title,omitempty"`
	Target     string   `json:"target"`
	Alias      string   `json:"alias,omitempty"`
	Fragment   string   `json:"fragment,omitempty"`
	Kind       string   `json:"kind"`
	Resolution string   `json:"resolution"`
	Line       int      `json:"line"`
	Candidates []string `json:"candidates,omitempty"`
}

// Backlink is one incoming wikilink.
type Backlink struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	Title    string `json:"title"`
	Alias    string `json:"alias,omitempty"`
	Fragment string `json:"fragment,omitempty"`
	Kind     string `json:"kind"`
	Line     int    `json:"line"`
}

// NoteSummary is one row in a note list.
type NoteSummary struct {
	ID        string   `json:"id"`
	Path      string   `json:"path"`
	Title     string   `json:"title"`
	Excerpt   string   `json:"excerpt"`
	Tags      []string `json:"tags"`
	Size      int64    `json:"size"`
	UpdatedAt int64    `json:"updated_at"`
}

// NoteList mirrors GET /api/v1/notes.
type NoteList struct {
	Notes      []NoteSummary `json:"notes"`
	Total      int           `json:"total"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

// CreateNoteInput is the body of POST /api/v1/notes.
type CreateNoteInput struct {
	Path    string `json:"path"`
	Content string `json:"content,omitempty"`
}

// PatchOp is one cheap edit accepted by PATCH /api/v1/notes/{id}.
type PatchOp struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// MoveResult mirrors POST /api/v1/notes/{id}/move.
type MoveResult struct {
	Moved          bool     `json:"moved"`
	Note           Note     `json:"note"`
	LinksRewritten int      `json:"links_rewritten"`
	Warnings       []string `json:"warnings,omitempty"`
}

// DeleteResult mirrors DELETE /api/v1/notes/{id}.
type DeleteResult struct {
	Deleted   bool   `json:"deleted"`
	TrashPath string `json:"trash_path"`
}

// SearchHit is one ranked search result.
type SearchHit struct {
	ID      string   `json:"id"`
	Path    string   `json:"path"`
	Title   string   `json:"title"`
	Snippet string   `json:"snippet"`
	Tags    []string `json:"tags"`
	Type    string   `json:"type"`
}

// SearchResponse mirrors GET /api/v1/search.
type SearchResponse struct {
	Hits       []SearchHit `json:"hits"`
	Total      int         `json:"total"`
	NextCursor string      `json:"next_cursor,omitempty"`
}

// IndexStatus mirrors GET /api/v1/index/status.
type IndexStatus struct {
	State      string   `json:"state"`
	Mode       string   `json:"mode,omitempty"`
	FilesTotal int      `json:"files_total"`
	FilesDone  int      `json:"files_done"`
	StartedAt  string   `json:"started_at,omitempty"`
	FinishedAt string   `json:"finished_at,omitempty"`
	Warnings   []string `json:"warnings"`
	Notes      int      `json:"notes"`
}

// TokenView is one device token in the list.
type TokenView struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	Scope      string  `json:"scope"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
	UserAgent  string  `json:"user_agent,omitempty"`
}

// TokenList mirrors GET /api/v1/tokens.
type TokenList struct {
	Tokens []TokenView `json:"tokens"`
}

// ListOptions narrows a note list request.
type ListOptions struct {
	Limit      int
	Cursor     string
	PathPrefix string
	Tag        string
	Sort       string
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
	err := c.do(ctx, http.MethodGet, "/api/v1/health", nil, &out, nil)
	return out, err
}

// AuthStatus reports whether first-run setup is still required.
func (c *Client) AuthStatus(ctx context.Context) (AuthStatus, error) {
	var out AuthStatus
	err := c.do(ctx, http.MethodGet, "/api/v1/auth/status", nil, &out, nil)
	return out, err
}

// Me returns the principal behind the configured credential.
func (c *Client) Me(ctx context.Context) (Me, error) {
	var out Me
	err := c.do(ctx, http.MethodGet, "/api/v1/auth/me", nil, &out, nil)
	return out, err
}

// ListNotes returns a page of note summaries.
func (c *Client) ListNotes(ctx context.Context, opts ListOptions) (NoteList, error) {
	query := url.Values{}
	if opts.Limit > 0 {
		query.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		query.Set("cursor", opts.Cursor)
	}
	if opts.PathPrefix != "" {
		query.Set("path_prefix", opts.PathPrefix)
	}
	if opts.Tag != "" {
		query.Set("tag", opts.Tag)
	}
	if opts.Sort != "" {
		query.Set("sort", opts.Sort)
	}
	var out NoteList
	err := c.do(ctx, http.MethodGet, withQuery("/api/v1/notes", query), nil, &out, nil)
	return out, err
}

// GetNote fetches a note by ULID.
func (c *Client) GetNote(ctx context.Context, id string) (Note, error) {
	var out Note
	err := c.do(ctx, http.MethodGet, "/api/v1/notes/"+url.PathEscape(id), nil, &out, nil)
	return out, err
}

// GetNoteByPath fetches a note by vault-relative path.
func (c *Client) GetNoteByPath(ctx context.Context, path string) (Note, error) {
	query := url.Values{"path": {path}}
	var out Note
	err := c.do(ctx, http.MethodGet, withQuery("/api/v1/notes/by-path", query), nil, &out, nil)
	return out, err
}

// CreateNote writes a new note and returns it with its injected id and ETag.
func (c *Client) CreateNote(ctx context.Context, in CreateNoteInput) (Note, error) {
	var out Note
	err := c.do(ctx, http.MethodPost, "/api/v1/notes", in, &out, nil)
	return out, err
}

// UpdateNote replaces note content, requiring the current ETag.
func (c *Client) UpdateNote(ctx context.Context, id, content, etag string) (Note, error) {
	headers := http.Header{"If-Match": {etag}}
	var out Note
	err := c.do(ctx, http.MethodPut, "/api/v1/notes/"+url.PathEscape(id),
		map[string]string{"content": content}, &out, headers)
	return out, err
}

// PatchNote applies cheap edits, requiring the current ETag.
func (c *Client) PatchNote(ctx context.Context, id string, ops []PatchOp, etag string) (Note, error) {
	headers := http.Header{"If-Match": {etag}}
	var out Note
	err := c.do(ctx, http.MethodPatch, "/api/v1/notes/"+url.PathEscape(id),
		map[string]any{"ops": ops}, &out, headers)
	return out, err
}

// DeleteNote soft-deletes a note, requiring the current ETag.
func (c *Client) DeleteNote(ctx context.Context, id, etag string) (DeleteResult, error) {
	headers := http.Header{"If-Match": {etag}}
	var out DeleteResult
	err := c.do(ctx, http.MethodDelete, "/api/v1/notes/"+url.PathEscape(id), nil, &out, headers)
	return out, err
}

// MoveNote moves a note and optionally rewrites inbound links.
func (c *Client) MoveNote(ctx context.Context, id, newPath string, rewrite bool) (MoveResult, error) {
	var out MoveResult
	err := c.do(ctx, http.MethodPost, "/api/v1/notes/"+url.PathEscape(id)+"/move",
		map[string]any{"path": newPath, "rewrite_links": rewrite}, &out, nil)
	return out, err
}

// Links returns a note's outgoing links.
func (c *Client) Links(ctx context.Context, id string) ([]Link, error) {
	var out struct {
		Links []Link `json:"links"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/notes/"+url.PathEscape(id)+"/links", nil, &out, nil)
	return out.Links, err
}

// Backlinks returns a note's incoming links.
func (c *Client) Backlinks(ctx context.Context, id string) ([]Backlink, error) {
	var out struct {
		Backlinks []Backlink `json:"backlinks"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/notes/"+url.PathEscape(id)+"/backlinks", nil, &out, nil)
	return out.Backlinks, err
}

// Search runs the shared query grammar.
func (c *Client) Search(ctx context.Context, query string, limit int, cursor string) (SearchResponse, error) {
	values := url.Values{"q": {query}}
	if limit > 0 {
		values.Set("limit", strconv.Itoa(limit))
	}
	if cursor != "" {
		values.Set("cursor", cursor)
	}
	var out SearchResponse
	err := c.do(ctx, http.MethodGet, withQuery("/api/v1/search", values), nil, &out, nil)
	return out, err
}

// IndexStatus returns scan state and counts.
func (c *Client) IndexStatus(ctx context.Context) (IndexStatus, error) {
	var out IndexStatus
	err := c.do(ctx, http.MethodGet, "/api/v1/index/status", nil, &out, nil)
	return out, err
}

// Rescan starts a background index scan.
func (c *Client) Rescan(ctx context.Context, full bool) (map[string]any, error) {
	var out map[string]any
	err := c.do(ctx, http.MethodPost, "/api/v1/index/rescan", map[string]bool{"full": full}, &out, nil)
	return out, err
}

// ListTokens returns active device tokens.
func (c *Client) ListTokens(ctx context.Context) (TokenList, error) {
	var out TokenList
	err := c.do(ctx, http.MethodGet, "/api/v1/tokens", nil, &out, nil)
	return out, err
}

// RevokeToken deletes a device token.
func (c *Client) RevokeToken(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/tokens/"+url.PathEscape(id), nil, nil, nil)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any, headers http.Header) error {
	if c.BaseURL == "" {
		return fmt.Errorf("server URL is not configured")
	}

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request body: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Lewnotes-Client", "cli")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return decodeProblem(resp)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s %s: %w", method, path, err)
	}
	return nil
}

func decodeProblem(resp *http.Response) error {
	var problem struct {
		Type   string `json:"type"`
		Title  string `json:"title"`
		Status int    `json:"status"`
		Detail string `json:"detail"`
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err := json.Unmarshal(data, &problem); err != nil || problem.Status == 0 {
		detail := strings.TrimSpace(string(data))
		if len(detail) > 300 {
			detail = detail[:300]
		}
		return &APIError{Status: resp.StatusCode, Title: http.StatusText(resp.StatusCode), Detail: detail}
	}
	return &APIError{Status: problem.Status, Type: problem.Type, Title: problem.Title, Detail: problem.Detail}
}

func withQuery(path string, values url.Values) string {
	if len(values) == 0 {
		return path
	}
	return path + "?" + values.Encode()
}
