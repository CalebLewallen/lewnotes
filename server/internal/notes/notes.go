// Package notes implements the note domain: files are the source of truth and
// every mutation goes through here so the index and events stay consistent.
package notes

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"mime"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"gopkg.in/yaml.v3"

	"github.com/caleblewallen/lewnotes/server/internal/events"
	"github.com/caleblewallen/lewnotes/server/internal/index"
	"github.com/caleblewallen/lewnotes/server/internal/links"
	"github.com/caleblewallen/lewnotes/server/internal/markdown"
	"github.com/caleblewallen/lewnotes/server/internal/vault"
)

var (
	ErrNotFound             = errors.New("note not found")
	ErrExists               = errors.New("note already exists")
	ErrInvalidPath          = errors.New("invalid note path")
	ErrInvalidNote          = errors.New("invalid note")
	ErrNoteTooLarge         = errors.New("note is too large")
	ErrPreconditionRequired = errors.New("if-match is required")
)

// DefaultMaxBytes caps a single note's content.
const DefaultMaxBytes = 5 << 20

// ConflictError reports a stale If-Match with the current ETag.
type ConflictError struct {
	ETag string
}

func (e *ConflictError) Error() string {
	return "note changed since the provided if-match"
}

// Options wires the note service.
type Options struct {
	Vault    *vault.Vault
	Index    *index.Index
	Events   *events.Hub
	Logger   *slog.Logger
	MaxBytes int64
}

// Service is the single mutation path for notes.
type Service struct {
	vault    *vault.Vault
	index    *index.Index
	events   *events.Hub
	logger   *slog.Logger
	maxBytes int64
}

// New builds a note service.
func New(opts Options) *Service {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = DefaultMaxBytes
	}
	return &Service{
		vault:    opts.Vault,
		index:    opts.Index,
		events:   opts.Events,
		logger:   opts.Logger,
		maxBytes: opts.MaxBytes,
	}
}

// Note is the API representation of one note.
type Note struct {
	ID          string           `json:"id"`
	Path        string           `json:"path"`
	Title       string           `json:"title"`
	ETag        string           `json:"etag"`
	Size        int64            `json:"size"`
	UpdatedAt   string           `json:"updated_at"`
	Tags        []string         `json:"tags"`
	Frontmatter map[string]any   `json:"frontmatter"`
	Content     string           `json:"content,omitempty"`
	Body        string           `json:"body"`
	Links       []index.LinkView `json:"links"`
	Backlinks   []index.Backlink `json:"backlinks"`
	Warnings    []string         `json:"warnings,omitempty"`
}

// CreateInput describes a new note.
type CreateInput struct {
	Path        string         `json:"path"`
	Content     string         `json:"content"`
	Frontmatter map[string]any `json:"frontmatter"`
}

// ListOptions narrows a list request.
type ListOptions struct {
	PathPrefix string
	Tag        string
	Sort       string
	Limit      int
	Offset     int
}

// ListResult is one page of summaries.
type ListResult struct {
	Notes      []index.NoteSummary `json:"notes"`
	Total      int                 `json:"total"`
	NextCursor string              `json:"next_cursor,omitempty"`
}

// Create writes a new note file, injects an id, and indexes it.
func (s *Service) Create(ctx context.Context, in CreateInput) (Note, error) {
	rel, err := CleanPath(in.Path)
	if err != nil {
		return Note{}, err
	}
	if _, err := s.vault.Stat(rel); err == nil {
		return Note{}, ErrExists
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Note{}, err
	}
	if int64(len(in.Content)) > s.maxBytes {
		return Note{}, ErrNoteTooLarge
	}

	content := []byte(in.Content)
	if len(in.Frontmatter) > 0 {
		doc, err := markdown.Parse(content)
		if err != nil {
			return Note{}, fmt.Errorf("%w: %v", ErrInvalidNote, err)
		}
		if !doc.HasFrontmatter {
			encoded, err := yaml.Marshal(in.Frontmatter)
			if err != nil {
				return Note{}, fmt.Errorf("%w: %v", ErrInvalidNote, err)
			}
			block := "---\n" + string(encoded) + "---\n"
			content = append([]byte(block), content...)
		}
	}

	content, _, err = markdown.EnsureID(content, ulid.Make().String())
	if err != nil {
		return Note{}, fmt.Errorf("%w: %v", ErrInvalidNote, err)
	}
	if err := s.vault.WriteAtomic(rel, content, 0o644); err != nil {
		return Note{}, err
	}
	info, err := s.vault.Stat(rel)
	if err != nil {
		return Note{}, err
	}
	row, err := s.reindex(ctx, rel, content, info)
	if err != nil {
		return Note{}, err
	}
	// A new note can turn other notes' ghost links into resolved links.
	s.reResolveLinks(ctx)
	note, err := s.view(ctx, row, content)
	if err != nil {
		return Note{}, err
	}
	s.publish(ctx, "note.created", note)
	return note, nil
}

// Read returns one note by id.
func (s *Service) Read(ctx context.Context, id string) (Note, error) {
	row, err := s.noteRow(ctx, id)
	if err != nil {
		return Note{}, err
	}
	data, _, err := s.readFile(row.Path)
	if err != nil {
		return Note{}, err
	}
	return s.view(ctx, row, data)
}

// ReadByPath returns one note by vault-relative path.
func (s *Service) ReadByPath(ctx context.Context, rel string) (Note, error) {
	clean, err := CleanPath(rel)
	if err != nil {
		return Note{}, err
	}
	row, err := s.index.NoteByPath(ctx, clean)
	if errors.Is(err, index.ErrNoteNotFound) {
		return Note{}, ErrNotFound
	}
	if err != nil {
		return Note{}, err
	}
	data, _, err := s.readFile(row.Path)
	if err != nil {
		return Note{}, err
	}
	return s.view(ctx, row, data)
}

// List returns a page of note summaries.
func (s *Service) List(ctx context.Context, opts ListOptions) (ListResult, error) {
	offset := opts.Offset
	if opts.Limit < 0 {
		opts.Limit = 0
	}
	rows, total, err := s.index.ListNotes(ctx, index.ListFilter{
		PathPrefix: opts.PathPrefix,
		Tag:        opts.Tag,
		Sort:       opts.Sort,
		Limit:      opts.Limit,
		Offset:     offset,
	})
	if err != nil {
		return ListResult{}, err
	}
	result := ListResult{Notes: rows, Total: total}
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if next := offset + len(rows); next < total {
		result.NextCursor = EncodeCursor(next)
	}
	return result, nil
}

// ReadFile returns raw bytes plus MIME type and an ETag.
func (s *Service) ReadFile(ctx context.Context, rel string) (data []byte, contentType, etag string, err error) {
	clean := path.Clean(strings.TrimPrefix(strings.ReplaceAll(rel, `\`, "/"), "/"))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return nil, "", "", ErrInvalidPath
	}
	if strings.HasPrefix(clean, ".lewnotes/") || clean == ".lewnotes" {
		return nil, "", "", ErrInvalidPath
	}
	data, info, err := s.vault.Read(clean)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, "", "", ErrNotFound
		}
		return nil, "", "", err
	}
	if info.IsDir() {
		return nil, "", "", ErrNotFound
	}
	contentType = mime.TypeByExtension(path.Ext(clean))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return data, contentType, ETag(data), nil
}

// ETag returns the strong ETag for content.
func ETag(data []byte) string {
	sum := sha256.Sum256(data)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

func contentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (s *Service) noteRow(ctx context.Context, id string) (index.Note, error) {
	row, err := s.index.NoteByID(ctx, id)
	if errors.Is(err, index.ErrNoteNotFound) {
		return index.Note{}, ErrNotFound
	}
	return row, err
}

func (s *Service) readFile(rel string) ([]byte, fs.FileInfo, error) {
	data, info, err := s.vault.Read(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, ErrNotFound
	}
	return data, info, err
}

func (s *Service) view(ctx context.Context, row index.Note, data []byte) (Note, error) {
	doc, err := markdown.Parse(data)
	if err != nil {
		return Note{}, fmt.Errorf("%w: %v", ErrInvalidNote, err)
	}
	title := doc.Title()
	if title == "" {
		title = strings.TrimSuffix(path.Base(row.Path), path.Ext(row.Path))
	}
	outLinks, err := s.index.LinksFrom(ctx, row.ID)
	if err != nil {
		return Note{}, err
	}
	backlinks, err := s.index.BacklinksTo(ctx, row.ID)
	if err != nil {
		return Note{}, err
	}
	return Note{
		ID:          row.ID,
		Path:        row.Path,
		Title:       title,
		ETag:        ETag(data),
		Size:        int64(len(data)),
		UpdatedAt:   time.Unix(row.Mtime, 0).UTC().Format(time.RFC3339),
		Tags:        tagsFrom(doc.Frontmatter),
		Frontmatter: doc.Frontmatter,
		Content:     string(data),
		Body:        string(doc.Body),
		Links:       outLinks,
		Backlinks:   backlinks,
	}, nil
}

func (s *Service) reindex(ctx context.Context, rel string, data []byte, info fs.FileInfo) (index.Note, error) {
	doc, err := markdown.Parse(data)
	if err != nil {
		return index.Note{}, fmt.Errorf("%w: %v", ErrInvalidNote, err)
	}
	title := doc.Title()
	if title == "" {
		title = strings.TrimSuffix(path.Base(rel), path.Ext(rel))
	}
	row := index.Note{
		ID:          doc.ID(),
		Path:        rel,
		Title:       title,
		ContentHash: contentHash(data),
		Size:        int64(len(data)),
		Mtime:       info.ModTime().Unix(),
		WordCount:   len(strings.Fields(string(doc.Body))),
		Excerpt:     markdown.Excerpt(doc.Body, 240),
		Frontmatter: doc.Frontmatter,
		Body:        string(doc.Body),
	}
	if !markdown.ValidID(row.ID) {
		return index.Note{}, fmt.Errorf("%w: note has no valid id", ErrInvalidNote)
	}
	if err := s.index.UpsertNote(ctx, row); err != nil {
		return index.Note{}, err
	}
	if err := s.refreshLinks(ctx, row.ID, doc.Body); err != nil {
		return index.Note{}, err
	}
	return row, nil
}

// reResolveLinks re-resolves every note's links after the set of paths
// changes. Bodies are read from disk because SQLite does not store them.
func (s *Service) reResolveLinks(ctx context.Context) {
	all, err := s.index.AllNotes(ctx)
	if err != nil {
		s.logger.Warn("re-resolve links: list notes", "error", err)
		return
	}
	candidates := make([]links.Candidate, 0, len(all))
	for _, note := range all {
		candidates = append(candidates, links.Candidate{ID: note.ID, Path: note.Path})
	}
	for _, note := range all {
		data, _, err := s.vault.Read(note.Path)
		if err != nil {
			s.logger.Warn("re-resolve links: read note", "path", note.Path, "error", err)
			continue
		}
		doc, err := markdown.Parse(data)
		if err != nil {
			s.logger.Warn("re-resolve links: parse note", "path", note.Path, "error", err)
			continue
		}
		s.replaceLinks(ctx, note.ID, doc.Body, candidates)
	}
}

func (s *Service) refreshLinks(ctx context.Context, srcID string, body []byte) error {
	all, err := s.index.AllNotes(ctx)
	if err != nil {
		return err
	}
	candidates := make([]links.Candidate, 0, len(all))
	for _, note := range all {
		candidates = append(candidates, links.Candidate{ID: note.ID, Path: note.Path})
	}
	return s.replaceLinks(ctx, srcID, body, candidates)
}

func (s *Service) replaceLinks(ctx context.Context, srcID string, body []byte, candidates []links.Candidate) error {
	refs := links.Parse(body)
	if len(refs) == 0 {
		return s.index.ReplaceLinks(ctx, srcID, nil)
	}

	rows := make([]index.LinkRow, 0, len(refs))
	for _, ref := range refs {
		resolution := links.Resolve(ref.Target, candidates)
		rows = append(rows, index.LinkRow{
			DstID:      resolution.ID,
			DstRaw:     ref.Target,
			Alias:      ref.Alias,
			Fragment:   ref.Fragment,
			Kind:       string(ref.Kind),
			Resolution: resolution.Status,
			Line:       ref.Line,
			Candidates: resolution.Candidates,
		})
	}
	return s.index.ReplaceLinks(ctx, srcID, rows)
}

func (s *Service) publish(ctx context.Context, kind string, note Note) {
	if s.events == nil {
		return
	}
	s.events.Publish(kind, map[string]any{
		"id":     note.ID,
		"path":   note.Path,
		"title":  note.Title,
		"etag":   note.ETag,
		"origin": OriginFromContext(ctx),
	})
}

// CleanPath validates and normalizes a vault-relative note path, adding the
// .md extension when it is missing.
func CleanPath(rel string) (string, error) {
	clean := strings.TrimSpace(strings.ReplaceAll(rel, `\`, "/"))
	clean = strings.TrimPrefix(clean, "/")
	clean = path.Clean(clean)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return "", ErrInvalidPath
	}
	if len(clean) > 512 || strings.ContainsRune(clean, 0) {
		return "", ErrInvalidPath
	}
	for _, segment := range strings.Split(clean, "/") {
		if segment == "" || strings.HasPrefix(segment, ".") {
			return "", ErrInvalidPath
		}
	}
	switch ext := path.Ext(clean); {
	case ext == "":
		clean += ".md"
	case strings.EqualFold(ext, ".md"):
	default:
		return "", fmt.Errorf("%w: notes must be .md", ErrInvalidPath)
	}
	return clean, nil
}

// EncodeCursor turns a numeric offset into an opaque list cursor.
func EncodeCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("off:" + strconv.Itoa(offset)))
}

// DecodeCursor parses an opaque cursor from a list request.
func DecodeCursor(cursor string) (int, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || !strings.HasPrefix(string(raw), "off:") {
		return 0, ErrInvalidPath
	}
	offset, err := strconv.Atoi(strings.TrimPrefix(string(raw), "off:"))
	if err != nil || offset < 0 {
		return 0, ErrInvalidPath
	}
	return offset, nil
}

func tagsFrom(frontmatter map[string]any) []string {
	raw, ok := frontmatter["tags"]
	if !ok {
		return []string{}
	}
	var values []string
	switch v := raw.(type) {
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				values = append(values, s)
			}
		}
	case []string:
		values = append(values, v...)
	case string:
		values = strings.Fields(v)
	}
	tags := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		tag := strings.TrimPrefix(strings.TrimSpace(value), "#")
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		tags = append(tags, tag)
	}
	return tags
}

type originKey struct{}

// WithOrigin records which client made a change for SSE echo suppression.
func WithOrigin(ctx context.Context, origin string) context.Context {
	if origin == "" {
		return ctx
	}
	return context.WithValue(ctx, originKey{}, origin)
}

// OriginFromContext returns the client label, defaulting to "core".
func OriginFromContext(ctx context.Context) string {
	if origin, ok := ctx.Value(originKey{}).(string); ok && origin != "" {
		return origin
	}
	return "core"
}
