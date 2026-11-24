package index

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// SearchFilter is a parsed search query ready for SQL.
type SearchFilter struct {
	Match      string // FTS5 MATCH expression; empty means filter-only
	Tags       []string
	PathPrefix string
	Resolution string // exact | suffix | unresolved | ambiguous
	Kind       string // link | embed
	Limit      int
	Offset     int
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

// Search runs an FTS5 query or a filter-only list. Title matches rank higher.
func (ix *Index) Search(ctx context.Context, f SearchFilter) ([]SearchHit, int, error) {
	where := []string{"1=1"}
	args := []any{}

	join := ""
	selectSnippet := "n.excerpt"
	order := "n.updated_at DESC, n.path ASC"
	if f.Match != "" {
		join = " JOIN notes_fts f ON f.note_id = n.id "
		where = append(where, "notes_fts MATCH ?")
		args = append(args, f.Match)
		selectSnippet = "snippet(notes_fts, 2, '<mark>', '</mark>', '…', 18)"
		order = "bm25(notes_fts, 0.0, 8.0, 1.0) ASC, n.path ASC"
	}
	for _, tag := range f.Tags {
		where = append(where, `EXISTS (SELECT 1 FROM tags t WHERE t.note_id = n.id AND t.tag = ?)`)
		args = append(args, strings.TrimPrefix(tag, "#"))
	}
	if f.PathPrefix != "" {
		where = append(where, `n.path LIKE ? ESCAPE '\'`)
		args = append(args, likePrefix(f.PathPrefix))
	}
	if f.Resolution != "" {
		where = append(where, `EXISTS (SELECT 1 FROM links l WHERE l.src_id = n.id AND l.resolution = ?)`)
		args = append(args, f.Resolution)
	}
	if f.Kind != "" {
		where = append(where, `EXISTS (SELECT 1 FROM links l WHERE l.src_id = n.id AND l.kind = ?)`)
		args = append(args, f.Kind)
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

	clause := strings.Join(where, " AND ")
	countQuery := `SELECT count(*) FROM notes n` + join + ` WHERE ` + clause
	var total int
	if err := ix.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count search hits: %w", err)
	}

	query := `SELECT n.id, n.path, n.title, ` + selectSnippet + `,
		(SELECT group_concat(t.tag, char(31)) FROM tags t WHERE t.note_id = n.id)
		FROM notes n` + join + ` WHERE ` + clause + ` ORDER BY ` + order + ` LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := ix.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("search notes: %w", err)
	}
	defer rows.Close()

	hits := []SearchHit{}
	for rows.Next() {
		var (
			hit  SearchHit
			tags sql.NullString
		)
		if err := rows.Scan(&hit.ID, &hit.Path, &hit.Title, &hit.Snippet, &tags); err != nil {
			return nil, 0, fmt.Errorf("scan search hit: %w", err)
		}
		hit.Tags = splitTags(tags.String)
		hit.Type = "note"
		hits = append(hits, hit)
	}
	return hits, total, rows.Err()
}
