package index

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/caleblewallen/lewnotes/server/internal/markdown"
	"github.com/caleblewallen/lewnotes/server/internal/vault"
)

func newScanner(t *testing.T, root string) (*Scanner, *Index) {
	t.Helper()
	v, err := vault.Open(root, nil)
	if err != nil {
		t.Fatalf("vault.Open() error = %v", err)
	}
	ix, err := Open(filepath.Join(t.TempDir(), "lewnotes.db"))
	if err != nil {
		t.Fatalf("index.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = ix.Close() })
	return NewScanner(v, ix, slog.New(slog.NewTextHandler(io.Discard, nil))), ix
}

func writeNote(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func noteID(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := markdown.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return doc.ID()
}

func TestScanAssignsIDsAndSkipsUnchanged(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeNote(t, root, "alpha.md", "# Alpha\n\nBody\n")
	writeNote(t, root, "sub/beta.md", "---\ntitle: Beta\ntags: [work]\n---\nBeta body\n")
	writeNote(t, root, ".obsidian/hidden.md", "# Hidden\n")

	scanner, ix := newScanner(t, root)

	stats, err := scanner.Run(ctx, true)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if stats.Total != 2 || stats.Indexed != 2 {
		t.Errorf("stats = %+v, want total 2 indexed 2", stats)
	}
	if id := noteID(t, root, "alpha.md"); !markdown.ValidID(id) {
		t.Errorf("alpha id = %q, want a ULID", id)
	}
	if id := noteID(t, root, "sub/beta.md"); !markdown.ValidID(id) {
		t.Errorf("beta id = %q, want a ULID", id)
	}

	status, err := ix.ScanStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != "idle" || status.Notes != 2 || status.FilesTotal != 2 {
		t.Errorf("status = %+v, want idle with 2 notes", status)
	}

	firstAlpha := noteID(t, root, "alpha.md")
	stats, err = scanner.Run(ctx, false)
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if stats.Skipped != 2 || stats.Indexed != 0 {
		t.Errorf("second stats = %+v, want 2 skipped", stats)
	}
	if got := noteID(t, root, "alpha.md"); got != firstAlpha {
		t.Errorf("id changed on rescan: %q -> %q", firstAlpha, got)
	}
}

func TestRescanAfterDatabaseLoss(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeNote(t, root, "a.md", "# A\n")
	writeNote(t, root, "nested/b.md", "---\ntitle: B\n---\nBody\n")

	scanner, _ := newScanner(t, root)
	if _, err := scanner.Run(ctx, true); err != nil {
		t.Fatal(err)
	}
	before := map[string]string{
		"a.md":        noteID(t, root, "a.md"),
		"nested/b.md": noteID(t, root, "nested/b.md"),
	}

	// A brand-new index (different temp dir) simulates deleting the database.
	v, err := vault.Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := Open(filepath.Join(t.TempDir(), "lewnotes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fresh.Close() }()
	freshScanner := NewScanner(v, fresh, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := freshScanner.Run(ctx, true); err != nil {
		t.Fatal(err)
	}

	for rel, want := range before {
		if got := noteID(t, root, rel); got != want {
			t.Errorf("%s id after rebuild = %q, want %q", rel, got, want)
		}
	}
	if count, err := fresh.CountNotes(ctx); err != nil || count != 2 {
		t.Errorf("rebuilt count = %d, %v; want 2", count, err)
	}
}

func TestDuplicateIDsAreReassigned(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	body := "---\nid: " + testNoteID + "\ntitle: Same\n---\nBody\n"
	writeNote(t, root, "one.md", body)
	writeNote(t, root, "two.md", body)

	scanner, _ := newScanner(t, root)
	stats, err := scanner.Run(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.Warnings) == 0 {
		t.Error("warnings = none, want a duplicate-id warning")
	}

	one := noteID(t, root, "one.md")
	two := noteID(t, root, "two.md")
	if !markdown.ValidID(one) || !markdown.ValidID(two) {
		t.Fatalf("ids = %q, %q; want ULIDs", one, two)
	}
	if one == two {
		t.Errorf("ids are still identical (%q)", one)
	}
}

func TestStaleNotesAreRemoved(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeNote(t, root, "gone.md", "# Gone\n")

	scanner, ix := newScanner(t, root)
	if _, err := scanner.Run(ctx, true); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(filepath.Join(root, "gone.md")); err != nil {
		t.Fatal(err)
	}
	stats, err := scanner.Run(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Removed != 1 {
		t.Errorf("removed = %d, want 1", stats.Removed)
	}
	if count, _ := ix.CountNotes(ctx); count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}
