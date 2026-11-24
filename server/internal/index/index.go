// Package index owns the SQLite index: notes, tags, links, FTS, and scan state.
//
// The database is disposable. Every row here can be rebuilt by scanning the
// vault, except server-only state (added in later slices).
package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

// Meta keys describing the most recent scan.
const (
	metaScanState    = "scan.state"
	metaScanMode     = "scan.mode"
	metaScanTotal    = "scan.files_total"
	metaScanDone     = "scan.files_done"
	metaScanStarted  = "scan.started_at"
	metaScanFinished = "scan.finished_at"
	metaScanWarnings = "scan.warnings"
	metaScanError    = "scan.error"
)

// Index is the SQLite-backed content index.
type Index struct {
	db   *sql.DB
	path string
}

// Note is one indexed markdown file.
type Note struct {
	ID          string
	Path        string
	Title       string
	ContentHash string
	Size        int64
	Mtime       int64
	WordCount   int
	Excerpt     string
	Frontmatter map[string]any
	Body        string
}

// NoteRef is the minimal row needed by the scanner to diff the vault.
type NoteRef struct {
	ID          string
	Path        string
	ContentHash string
	Size        int64
	Mtime       int64
}

// ScanStatus is the observable state of indexing.
type ScanStatus struct {
	State      string   `json:"state"`
	Mode       string   `json:"mode,omitempty"`
	FilesTotal int      `json:"files_total"`
	FilesDone  int      `json:"files_done"`
	StartedAt  string   `json:"started_at,omitempty"`
	FinishedAt string   `json:"finished_at,omitempty"`
	Warnings   []string `json:"warnings"`
	Notes      int      `json:"notes"`
}

// Open opens (creating if needed) the database and applies migrations.
func Open(path string) (*Index, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}

	dsn := "file:" + filepath.ToSlash(path) +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=synchronous(NORMAL)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// SQLite allows one writer; serializing every connection keeps handlers
	// from ever observing SQLITE_BUSY.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect database: %w", err)
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Index{db: db, path: path}, nil
}

// Close flushes and closes the database.
func (ix *Index) Close() error { return ix.db.Close() }

// Path is the database file location.
func (ix *Index) Path() string { return ix.path }

// DB exposes the shared single-connection pool for server-only state such as
// auth. Handlers must not open their own connections.
func (ix *Index) DB() *sql.DB { return ix.db }

// UpsertNote writes one note and refreshes its tags and FTS row.
func (ix *Index) UpsertNote(ctx context.Context, n Note) error {
	frontmatter, err := json.Marshal(n.Frontmatter)
	if err != nil {
		frontmatter = []byte("{}")
	}

	tx, err := ix.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin note upsert: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO notes (id, path, title, content_hash, size, mtime, word_count, excerpt, frontmatter, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, unixepoch())
		ON CONFLICT(path) DO UPDATE SET
			id = excluded.id,
			title = excluded.title,
			content_hash = excluded.content_hash,
			size = excluded.size,
			mtime = excluded.mtime,
			word_count = excluded.word_count,
			excerpt = excluded.excerpt,
			frontmatter = excluded.frontmatter,
			updated_at = unixepoch()`,
		n.ID, n.Path, n.Title, n.ContentHash, n.Size, n.Mtime, n.WordCount, n.Excerpt, string(frontmatter))
	if err != nil {
		return fmt.Errorf("upsert note %s: %w", n.Path, err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM tags WHERE note_id = ?`, n.ID); err != nil {
		return fmt.Errorf("clear tags for %s: %w", n.Path, err)
	}
	for _, tag := range tagsFrom(n.Frontmatter) {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO tags (note_id, tag) VALUES (?, ?)`, n.ID, tag); err != nil {
			return fmt.Errorf("insert tag %q: %w", tag, err)
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM notes_fts WHERE note_id = ?`, n.ID); err != nil {
		return fmt.Errorf("clear fts for %s: %w", n.Path, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO notes_fts (note_id, title, body) VALUES (?, ?, ?)`, n.ID, n.Title, n.Body); err != nil {
		return fmt.Errorf("index fts for %s: %w", n.Path, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit note %s: %w", n.Path, err)
	}
	return nil
}

// DeleteNote removes a note and its derived rows.
func (ix *Index) DeleteNote(ctx context.Context, id string) error {
	tx, err := ix.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin note delete: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM notes_fts WHERE note_id = ?`, id); err != nil {
		return fmt.Errorf("delete fts row: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM notes WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete note: %w", err)
	}
	return tx.Commit()
}

// AllNotes returns every indexed note reference.
func (ix *Index) AllNotes(ctx context.Context) ([]NoteRef, error) {
	rows, err := ix.db.QueryContext(ctx, `SELECT id, path, content_hash, size, mtime FROM notes`)
	if err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}
	defer rows.Close()

	var notes []NoteRef
	for rows.Next() {
		var n NoteRef
		if err := rows.Scan(&n.ID, &n.Path, &n.ContentHash, &n.Size, &n.Mtime); err != nil {
			return nil, fmt.Errorf("scan note row: %w", err)
		}
		notes = append(notes, n)
	}
	return notes, rows.Err()
}

// CountNotes returns the number of indexed notes.
func (ix *Index) CountNotes(ctx context.Context) (int, error) {
	var count int
	if err := ix.db.QueryRowContext(ctx, `SELECT count(*) FROM notes`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count notes: %w", err)
	}
	return count, nil
}

// Meta reads a meta value, returning "" when the key is absent.
func (ix *Index) Meta(ctx context.Context, key string) (string, error) {
	var value string
	err := ix.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read meta %s: %w", key, err)
	}
	return value, nil
}

// SetMeta writes meta values in one transaction.
func (ix *Index) SetMeta(ctx context.Context, values map[string]string) error {
	tx, err := ix.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin meta write: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for key, value := range values {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO meta (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value); err != nil {
			return fmt.Errorf("write meta %s: %w", key, err)
		}
	}
	return tx.Commit()
}

// ScanStatus reads the current scan state and note count.
func (ix *Index) ScanStatus(ctx context.Context) (ScanStatus, error) {
	status := ScanStatus{State: "idle", Warnings: []string{}}

	rows, err := ix.db.QueryContext(ctx, `SELECT key, value FROM meta WHERE key LIKE 'scan.%'`)
	if err != nil {
		return status, fmt.Errorf("read scan meta: %w", err)
	}
	defer rows.Close()

	values := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return status, fmt.Errorf("scan scan meta: %w", err)
		}
		values[key] = value
	}
	if err := rows.Err(); err != nil {
		return status, err
	}

	if v := values[metaScanState]; v != "" {
		status.State = v
	}
	status.Mode = values[metaScanMode]
	status.StartedAt = values[metaScanStarted]
	status.FinishedAt = values[metaScanFinished]
	status.FilesTotal = atoi(values[metaScanTotal])
	status.FilesDone = atoi(values[metaScanDone])
	if raw := values[metaScanWarnings]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &status.Warnings); err != nil {
			status.Warnings = []string{raw}
		}
	}

	count, err := ix.CountNotes(ctx)
	if err != nil {
		return status, err
	}
	status.Notes = count
	return status, nil
}

func atoi(s string) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return v
}

func tagsFrom(frontmatter map[string]any) []string {
	raw, ok := frontmatter["tags"]
	if !ok {
		return nil
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
