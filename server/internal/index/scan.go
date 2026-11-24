package index

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/caleblewallen/lewnotes/server/internal/markdown"
	"github.com/caleblewallen/lewnotes/server/internal/vault"
)

// ErrScanRunning is returned when another scan holds the scanner.
var ErrScanRunning = errors.New("a scan is already running")

// Stats summarizes one scan.
type Stats struct {
	Total    int
	Indexed  int
	Updated  int
	Skipped  int
	Removed  int
	Warnings []string
}

// Scanner keeps the SQLite index in step with the vault.
type Scanner struct {
	vault   *vault.Vault
	index   *Index
	logger  *slog.Logger
	running atomic.Bool
}

// NewScanner wires a vault to an index.
func NewScanner(v *vault.Vault, ix *Index, logger *slog.Logger) *Scanner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Scanner{vault: v, index: ix, logger: logger}
}

// Running reports whether a scan is in progress.
func (s *Scanner) Running() bool { return s.running.Load() }

// Run performs one full or incremental scan. It refuses to run concurrently.
func (s *Scanner) Run(ctx context.Context, full bool) (Stats, error) {
	if !s.running.CompareAndSwap(false, true) {
		return Stats{}, ErrScanRunning
	}
	defer s.running.Store(false)

	stats := Stats{}
	mode := "incremental"
	if full {
		mode = "full"
	}
	if err := s.index.SetMeta(ctx, map[string]string{
		metaScanState:   "scanning",
		metaScanMode:    mode,
		metaScanStarted: time.Now().UTC().Format(time.RFC3339),
		metaScanDone:    "0",
		metaScanError:   "",
	}); err != nil {
		return stats, err
	}

	err := s.scan(ctx, full, &stats)

	finished := time.Now().UTC().Format(time.RFC3339)
	if err != nil {
		// Keep the failure visible even if the request context was canceled.
		writeCtx := context.WithoutCancel(ctx)
		_ = s.index.SetMeta(writeCtx, map[string]string{
			metaScanState:    "error",
			metaScanError:    err.Error(),
			metaScanFinished: finished,
		})
		return stats, err
	}

	warnings := []byte("[]")
	if len(stats.Warnings) > 0 {
		if encoded, marshalErr := json.Marshal(stats.Warnings); marshalErr == nil {
			warnings = encoded
		}
	}
	if err := s.index.SetMeta(ctx, map[string]string{
		metaScanState:    "idle",
		metaScanFinished: finished,
		metaScanWarnings: string(warnings),
		metaScanTotal:    strconv.Itoa(stats.Total),
		metaScanDone:     strconv.Itoa(stats.Total),
	}); err != nil {
		return stats, err
	}

	s.logger.Info("index scan finished",
		"mode", mode,
		"files", stats.Total,
		"indexed", stats.Indexed,
		"updated", stats.Updated,
		"skipped", stats.Skipped,
		"removed", stats.Removed,
		"warnings", len(stats.Warnings),
	)
	return stats, nil
}

type fileEntry struct {
	rel   string
	size  int64
	mtime int64
}

func (s *Scanner) scan(ctx context.Context, full bool, stats *Stats) error {
	files := make([]fileEntry, 0, 128)
	if err := s.vault.Walk(func(rel string, info fs.FileInfo) error {
		if !strings.EqualFold(path.Ext(rel), ".md") {
			return nil
		}
		files = append(files, fileEntry{rel: rel, size: info.Size(), mtime: info.ModTime().Unix()})
		return nil
	}); err != nil {
		return fmt.Errorf("walk vault: %w", err)
	}
	stats.Total = len(files)
	if err := s.index.SetMeta(ctx, map[string]string{metaScanTotal: strconv.Itoa(len(files))}); err != nil {
		return err
	}

	existing, err := s.index.AllNotes(ctx)
	if err != nil {
		return err
	}
	byPath := make(map[string]NoteRef, len(existing))
	owners := make(map[string]string, len(existing)) // note id -> path
	for _, note := range existing {
		byPath[note.Path] = note
		owners[note.ID] = note.Path
	}

	seen := make(map[string]bool, len(files))
	for i, file := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		seen[file.rel] = true

		if prev, ok := byPath[file.rel]; ok && !full && prev.Size == file.size && prev.Mtime == file.mtime {
			stats.Skipped++
			continue
		}

		data, info, err := s.vault.Read(file.rel)
		if err != nil {
			s.warn(stats, "read %s: %v", file.rel, err)
			continue
		}
		doc, err := markdown.Parse(data)
		if err != nil {
			s.warn(stats, "parse %s: %v", file.rel, err)
			continue
		}

		id := doc.ID()
		duplicate := false
		if owner, taken := owners[id]; taken && owner != file.rel && markdown.ValidID(id) {
			duplicate = true
		}

		if !markdown.ValidID(id) || duplicate {
			newID := ulid.Make().String()
			var rewritten []byte
			var changed bool
			if duplicate {
				rewritten, changed, err = markdown.SetID(data, newID)
			} else {
				rewritten, changed, err = markdown.EnsureID(data, newID)
			}
			if err != nil {
				s.warn(stats, "assign id to %s: %v", file.rel, err)
				continue
			}
			if changed {
				if err := s.vault.WriteAtomic(file.rel, rewritten, 0o644); err != nil {
					s.warn(stats, "write id to %s: %v", file.rel, err)
					continue
				}
				data = rewritten
				// Writing the id changed size and mtime; record the post-write
				// metadata so the next incremental scan can skip this file.
				fresh, statErr := s.vault.Stat(file.rel)
				if statErr != nil {
					s.warn(stats, "stat %s after id write: %v", file.rel, statErr)
					continue
				}
				info = fresh
			}
			doc, err = markdown.Parse(data)
			if err != nil {
				s.warn(stats, "reparse %s: %v", file.rel, err)
				continue
			}
			id = doc.ID()
			if duplicate {
				s.warn(stats, "duplicate id in %s; assigned %s", file.rel, id)
			}
		}

		if !markdown.ValidID(id) {
			s.warn(stats, "could not assign an id to %s", file.rel)
			continue
		}
		owners[id] = file.rel

		title := doc.Title()
		if title == "" {
			title = strings.TrimSuffix(path.Base(file.rel), path.Ext(file.rel))
		}

		note := Note{
			ID:          id,
			Path:        file.rel,
			Title:       title,
			ContentHash: hashBytes(data),
			Size:        info.Size(),
			Mtime:       info.ModTime().Unix(),
			WordCount:   len(strings.Fields(string(doc.Body))),
			Excerpt:     markdown.Excerpt(doc.Body, 240),
			Frontmatter: doc.Frontmatter,
			Body:        string(doc.Body),
		}
		if err := s.index.UpsertNote(ctx, note); err != nil {
			return err
		}

		if _, ok := byPath[file.rel]; ok {
			stats.Updated++
		} else {
			stats.Indexed++
		}

		if (i+1)%25 == 0 {
			_ = s.index.SetMeta(ctx, map[string]string{metaScanDone: strconv.Itoa(i + 1)})
		}
	}

	for rel, ref := range byPath {
		if seen[rel] {
			continue
		}
		if err := s.index.DeleteNote(ctx, ref.ID); err != nil {
			s.warn(stats, "remove stale note %s: %v", rel, err)
			continue
		}
		s.logger.Info("removed note missing from vault", "path", rel)
		stats.Removed++
	}

	return nil
}

func (s *Scanner) warn(stats *Stats, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	stats.Warnings = append(stats.Warnings, msg)
	s.logger.Warn("index warning", "detail", msg)
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
