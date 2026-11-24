package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrNoteNotFound is returned when no indexed note matches.
var ErrNoteNotFound = errors.New("note not found")

const noteColumns = `id, path, title, content_hash, size, mtime, word_count, excerpt, frontmatter`

// NoteByID loads one note row. Body is not stored in SQLite; callers read the
// file for content.
func (ix *Index) NoteByID(ctx context.Context, id string) (Note, error) {
	row := ix.db.QueryRowContext(ctx, `SELECT `+noteColumns+` FROM notes WHERE id = ?`, id)
	n, err := scanNote(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Note{}, ErrNoteNotFound
	}
	return n, err
}

// NoteByPath loads one note row by vault-relative path.
func (ix *Index) NoteByPath(ctx context.Context, rel string) (Note, error) {
	row := ix.db.QueryRowContext(ctx, `SELECT `+noteColumns+` FROM notes WHERE path = ?`, rel)
	n, err := scanNote(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Note{}, ErrNoteNotFound
	}
	return n, err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanNote(row rowScanner) (Note, error) {
	var (
		n           Note
		frontmatter string
	)
	if err := row.Scan(&n.ID, &n.Path, &n.Title, &n.ContentHash, &n.Size, &n.Mtime,
		&n.WordCount, &n.Excerpt, &frontmatter); err != nil {
		return Note{}, err
	}
	if frontmatter != "" {
		if err := json.Unmarshal([]byte(frontmatter), &n.Frontmatter); err != nil {
			n.Frontmatter = map[string]any{}
		}
	}
	if n.Frontmatter == nil {
		n.Frontmatter = map[string]any{}
	}
	return n, nil
}

// ListFilter narrows a note list.
type ListFilter struct {
	PathPrefix string
	Tag        string
	Sort       string // updated | created | path | title
	Limit      int
	Offset     int
}

// NoteSummary is one list row.
type NoteSummary struct {
	ID        string   `json:"id"`
	Path      string   `json:"path"`
	Title     string   `json:"title"`
	Excerpt   string   `json:"excerpt"`
	Tags      []string `json:"tags"`
	Size      int64    `json:"size"`
	UpdatedAt int64    `json:"updated_at"`
}

func likePrefix(prefix string) string {
	prefix = strings.ReplaceAll(prefix, `\`, `\\`)
	prefix = strings.ReplaceAll(prefix, `%`, `\%`)
	prefix = strings.ReplaceAll(prefix, `_`, `\_`)
	return prefix + "%"
}

// ListNotes returns a page of note summaries newest-first by default.
func (ix *Index) ListNotes(ctx context.Context, f ListFilter) ([]NoteSummary, int, error) {
	where := []string{"1=1"}
	args := []any{}
	if f.PathPrefix != "" {
		where = append(where, `n.path LIKE ? ESCAPE '\'`)
		args = append(args, likePrefix(f.PathPrefix))
	}
	if f.Tag != "" {
		where = append(where, `EXISTS (SELECT 1 FROM tags t WHERE t.note_id = n.id AND t.tag = ?)`)
		args = append(args, strings.TrimPrefix(f.Tag, "#"))
	}

	order := "n.updated_at DESC, n.path ASC"
	switch f.Sort {
	case "created":
		order = "n.created_at DESC, n.path ASC"
	case "path":
		order = "n.path ASC"
	case "title":
		order = "n.title COLLATE NOCASE ASC, n.path ASC"
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	var total int
	countQuery := `SELECT count(*) FROM notes n WHERE ` + strings.Join(where, " AND ")
	if err := ix.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count notes: %w", err)
	}

	query := `SELECT n.id, n.path, n.title, n.excerpt, n.size, n.updated_at,
		(SELECT group_concat(t.tag, char(31)) FROM tags t WHERE t.note_id = n.id)
		FROM notes n WHERE ` + strings.Join(where, " AND ") + ` ORDER BY ` + order + ` LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := ix.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list notes: %w", err)
	}
	defer rows.Close()

	summaries := []NoteSummary{}
	for rows.Next() {
		var (
			s    NoteSummary
			tags sql.NullString
		)
		if err := rows.Scan(&s.ID, &s.Path, &s.Title, &s.Excerpt, &s.Size, &s.UpdatedAt, &tags); err != nil {
			return nil, 0, fmt.Errorf("scan note summary: %w", err)
		}
		s.Tags = splitTags(tags.String)
		summaries = append(summaries, s)
	}
	return summaries, total, rows.Err()
}

func splitTags(joined string) []string {
	if joined == "" {
		return []string{}
	}
	parts := strings.Split(joined, "\x1f")
	sortStrings(parts)
	return parts
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

// LinkRow is one outgoing link as written to the index.
type LinkRow struct {
	DstID      string
	DstRaw     string
	Alias      string
	Fragment   string
	Kind       string
	Resolution string
	Line       int
	Candidates []string
}

// LinkView is an outgoing link joined with its resolved note.
type LinkView struct {
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

// Backlink is an incoming link joined with its source note.
type Backlink struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	Title    string `json:"title"`
	Alias    string `json:"alias,omitempty"`
	Fragment string `json:"fragment,omitempty"`
	Kind     string `json:"kind"`
	Line     int    `json:"line"`
}

// ReplaceLinks swaps every outgoing link for one note in a transaction.
func (ix *Index) ReplaceLinks(ctx context.Context, srcID string, links []LinkRow) error {
	tx, err := ix.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin link replace: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM links WHERE src_id = ?`, srcID); err != nil {
		return fmt.Errorf("clear links for %s: %w", srcID, err)
	}
	for _, link := range links {
		var dstID any
		if link.DstID != "" {
			dstID = link.DstID
		}
		candidates, err := json.Marshal(link.Candidates)
		if err != nil || link.Candidates == nil {
			candidates = []byte("[]")
		}
		resolution := link.Resolution
		if resolution == "" {
			resolution = "unresolved"
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO links (src_id, dst_id, dst_raw, alias, fragment, kind, resolution, line, candidates)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(src_id, dst_raw, kind, fragment) DO UPDATE SET
				dst_id = excluded.dst_id,
				alias = excluded.alias,
				resolution = excluded.resolution,
				line = excluded.line,
				candidates = excluded.candidates`,
			srcID, dstID, link.DstRaw, link.Alias, link.Fragment, link.Kind, resolution, link.Line, string(candidates)); err != nil {
			return fmt.Errorf("insert link %q: %w", link.DstRaw, err)
		}
	}
	return tx.Commit()
}

// LinksFrom returns a note's outgoing links in document order.
func (ix *Index) LinksFrom(ctx context.Context, srcID string) ([]LinkView, error) {
	rows, err := ix.db.QueryContext(ctx, `
		SELECT COALESCE(l.dst_id, ''), COALESCE(d.path, ''), COALESCE(d.title, ''),
		       l.dst_raw, l.alias, l.fragment, l.kind, l.resolution, l.line, l.candidates
		FROM links l
		LEFT JOIN notes d ON d.id = l.dst_id
		WHERE l.src_id = ?
		ORDER BY l.line, l.dst_raw`, srcID)
	if err != nil {
		return nil, fmt.Errorf("list outgoing links: %w", err)
	}
	defer rows.Close()

	links := []LinkView{}
	for rows.Next() {
		var (
			link       LinkView
			candidates string
		)
		if err := rows.Scan(&link.ID, &link.Path, &link.Title, &link.Target, &link.Alias,
			&link.Fragment, &link.Kind, &link.Resolution, &link.Line, &candidates); err != nil {
			return nil, fmt.Errorf("scan link: %w", err)
		}
		if candidates != "" {
			_ = json.Unmarshal([]byte(candidates), &link.Candidates)
		}
		links = append(links, link)
	}
	return links, rows.Err()
}

// BacklinksTo returns notes linking to dstID in path order.
func (ix *Index) BacklinksTo(ctx context.Context, dstID string) ([]Backlink, error) {
	rows, err := ix.db.QueryContext(ctx, `
		SELECT s.id, s.path, s.title, l.alias, l.fragment, l.kind, l.line
		FROM links l JOIN notes s ON s.id = l.src_id
		WHERE l.dst_id = ?
		ORDER BY s.path, l.line`, dstID)
	if err != nil {
		return nil, fmt.Errorf("list backlinks: %w", err)
	}
	defer rows.Close()

	links := []Backlink{}
	for rows.Next() {
		var b Backlink
		if err := rows.Scan(&b.ID, &b.Path, &b.Title, &b.Alias, &b.Fragment, &b.Kind, &b.Line); err != nil {
			return nil, fmt.Errorf("scan backlink: %w", err)
		}
		links = append(links, b)
	}
	return links, rows.Err()
}

// MoveNotePath updates a note's path after a file rename.
func (ix *Index) MoveNotePath(ctx context.Context, id, newPath string) error {
	if _, err := ix.db.ExecContext(ctx,
		`UPDATE notes SET path = ?, updated_at = unixepoch() WHERE id = ?`, newPath, id); err != nil {
		return fmt.Errorf("move note %s: %w", id, err)
	}
	return nil
}

// NullifyLinksTo marks inbound links unresolved when their target disappears.
func (ix *Index) NullifyLinksTo(ctx context.Context, dstID string) error {
	if _, err := ix.db.ExecContext(ctx, `
		UPDATE links SET dst_id = NULL, resolution = 'unresolved', candidates = '[]'
		WHERE dst_id = ?`, dstID); err != nil {
		return fmt.Errorf("nullify links to %s: %w", dstID, err)
	}
	return nil
}
