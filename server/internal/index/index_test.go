package index

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

const testNoteID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

func testIndex(t *testing.T) *Index {
	t.Helper()
	ix, err := Open(filepath.Join(t.TempDir(), "lewnotes.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = ix.Close() })
	return ix
}

func TestOpenAppliesMigrations(t *testing.T) {
	ix := testIndex(t)

	var version int
	if err := ix.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 3 {
		t.Errorf("user_version = %d, want 3", version)
	}

	if _, err := os.Stat(ix.Path()); err != nil {
		t.Errorf("database file missing: %v", err)
	}
}

func TestUpsertNoteIndexesTagsAndBody(t *testing.T) {
	ctx := context.Background()
	ix := testIndex(t)

	note := Note{
		ID:          testNoteID,
		Path:        "notes/kitchen.md",
		Title:       "Kitchen",
		ContentHash: "hash-1",
		Size:        10,
		Mtime:       100,
		WordCount:   3,
		Excerpt:     "Hello body",
		Body:        "Hello body words",
		Frontmatter: map[string]any{"tags": []any{"home", "#diy"}},
	}
	if err := ix.UpsertNote(ctx, note); err != nil {
		t.Fatalf("UpsertNote() error = %v", err)
	}

	count, err := ix.CountNotes(ctx)
	if err != nil || count != 1 {
		t.Fatalf("CountNotes() = %d, %v; want 1, nil", count, err)
	}

	var tagCount int
	if err := ix.db.QueryRow(`SELECT count(*) FROM tags WHERE note_id = ?`, testNoteID).Scan(&tagCount); err != nil {
		t.Fatal(err)
	}
	if tagCount != 2 {
		t.Errorf("tag count = %d, want 2 (leading # stripped)", tagCount)
	}

	var hits int
	if err := ix.db.QueryRow(`SELECT count(*) FROM notes_fts WHERE notes_fts MATCH 'words'`).Scan(&hits); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Errorf("fts hits = %d, want 1", hits)
	}

	note.ContentHash = "hash-2"
	if err := ix.UpsertNote(ctx, note); err != nil {
		t.Fatalf("second UpsertNote() error = %v", err)
	}
	if count, _ := ix.CountNotes(ctx); count != 1 {
		t.Errorf("count after re-upsert = %d, want 1", count)
	}

	if err := ix.DeleteNote(ctx, testNoteID); err != nil {
		t.Fatalf("DeleteNote() error = %v", err)
	}
	if count, _ := ix.CountNotes(ctx); count != 0 {
		t.Errorf("count after delete = %d, want 0", count)
	}
	if err := ix.db.QueryRow(`SELECT count(*) FROM notes_fts`).Scan(&hits); err != nil {
		t.Fatal(err)
	}
	if hits != 0 {
		t.Errorf("fts rows after delete = %d, want 0", hits)
	}
}

func TestScanStatusDefaults(t *testing.T) {
	ctx := context.Background()
	ix := testIndex(t)

	status, err := ix.ScanStatus(ctx)
	if err != nil {
		t.Fatalf("ScanStatus() error = %v", err)
	}
	if status.State != "idle" {
		t.Errorf("state = %q, want idle", status.State)
	}
	if status.Warnings == nil {
		t.Error("warnings = nil, want empty slice")
	}
}

func TestMetaRoundTrip(t *testing.T) {
	ctx := context.Background()
	ix := testIndex(t)

	if err := ix.SetMeta(ctx, map[string]string{"scan.state": "scanning"}); err != nil {
		t.Fatal(err)
	}
	got, err := ix.Meta(ctx, "scan.state")
	if err != nil || got != "scanning" {
		t.Fatalf("Meta() = %q, %v; want scanning", got, err)
	}
	missing, err := ix.Meta(ctx, "nope")
	if err != nil || missing != "" {
		t.Fatalf("Meta(missing) = %q, %v; want empty", missing, err)
	}
}
