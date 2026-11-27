package notes_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caleblewallen/lewnotes/server/internal/events"
	"github.com/caleblewallen/lewnotes/server/internal/index"
	"github.com/caleblewallen/lewnotes/server/internal/markdown"
	"github.com/caleblewallen/lewnotes/server/internal/notes"
	"github.com/caleblewallen/lewnotes/server/internal/vault"
)

type fixture struct {
	svc   *notes.Service
	vault *vault.Vault
	index *index.Index
	dir   string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	v, err := vault.Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	ix, err := index.Open(filepath.Join(t.TempDir(), "lewnotes.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ix.Close() })
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	svc := notes.New(notes.Options{
		Vault:       v,
		Index:       ix,
		Events:      events.New(events.Options{Buffer: 10}),
		Logger:      logger,
		Attachments: "attachments",
	})
	return &fixture{svc: svc, vault: v, index: ix, dir: dir}
}

func mustCreate(t *testing.T, svc *notes.Service, path, content string) notes.Note {
	t.Helper()
	note, err := svc.Create(context.Background(), notes.CreateInput{Path: path, Content: content})
	if err != nil {
		t.Fatalf("Create(%s) error = %v", path, err)
	}
	return note
}

func TestCreateReadUpdateWithETag(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	note := mustCreate(t, f.svc, "inbox/Kitchen", "# Kitchen\n\nHello")
	if !markdown.ValidID(note.ID) {
		t.Fatalf("id = %q, want a ULID", note.ID)
	}
	if note.Path != "inbox/Kitchen.md" {
		t.Errorf("path = %q", note.Path)
	}
	if !strings.Contains(note.Content, "id: "+note.ID) {
		t.Errorf("content has no injected id:\n%s", note.Content)
	}
	if note.ETag == "" || !strings.HasPrefix(note.ETag, `"`) {
		t.Errorf("etag = %q", note.ETag)
	}

	read, err := f.svc.Read(ctx, note.ID)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if read.ETag != note.ETag || read.Title != "Kitchen" {
		t.Errorf("read = %+v", read)
	}

	byPath, err := f.svc.ReadByPath(ctx, "inbox/Kitchen.md")
	if err != nil || byPath.ID != note.ID {
		t.Fatalf("ReadByPath() = %+v, %v", byPath, err)
	}

	replacement := "---\nid: " + note.ID + "\n---\n\n# Renamed\n\nNew body\n"
	if _, err := f.svc.Update(ctx, note.ID, []byte(replacement), ""); !errors.Is(err, notes.ErrPreconditionRequired) {
		t.Fatalf("Update(no if-match) error = %v, want ErrPreconditionRequired", err)
	}
	if _, err := f.svc.Update(ctx, note.ID, []byte(replacement), `"deadbeef"`); err == nil {
		t.Fatal("Update(stale) error = nil")
	} else {
		var conflict *notes.ConflictError
		if !errors.As(err, &conflict) || conflict.ETag != note.ETag {
			t.Fatalf("Update(stale) error = %#v, want ConflictError with current etag", err)
		}
	}
	onDisk, _, err := f.vault.Read(note.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(onDisk), "New body") {
		t.Fatal("stale update modified the file")
	}

	updated, err := f.svc.Update(ctx, note.ID, []byte(replacement), note.ETag)
	if err != nil {
		t.Fatalf("Update(if-match) error = %v", err)
	}
	if updated.ETag == note.ETag {
		t.Error("etag did not change after update")
	}
	if updated.Title != "Renamed" {
		t.Errorf("title = %q", updated.Title)
	}

	appended, err := f.svc.Patch(ctx, note.ID, updated.ETag, []notes.PatchOp{{Type: "append", Text: "more"}})
	if err != nil {
		t.Fatalf("Patch(append) error = %v", err)
	}
	if !strings.HasSuffix(appended.Body, "more") {
		t.Errorf("body after append = %q", appended.Body)
	}

	// A note id cannot be changed through a write.
	foreign := strings.Replace(replacement, note.ID, "01ARZ3NDEKTSV4RRFFQ69G5FAV", 1)
	if _, err := f.svc.Update(ctx, note.ID, []byte(foreign), appended.ETag); !errors.Is(err, notes.ErrInvalidNote) {
		t.Errorf("Update(foreign id) error = %v, want ErrInvalidNote", err)
	}
}

func TestWritesRejectOversizedNotes(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	svc := notes.New(notes.Options{Vault: f.vault, Index: f.index, MaxBytes: 32})
	note, err := svc.Create(ctx, notes.CreateInput{Path: "big.md", Content: "# Big"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, notes.CreateInput{Path: "too-big.md", Content: strings.Repeat("x", 33)}); !errors.Is(err, notes.ErrNoteTooLarge) {
		t.Errorf("Create(oversized) error = %v, want ErrNoteTooLarge", err)
	}
	if _, err := svc.Update(ctx, note.ID, []byte(strings.Repeat("x", 33)), note.ETag); !errors.Is(err, notes.ErrNoteTooLarge) {
		t.Errorf("Update(oversized) error = %v, want ErrNoteTooLarge", err)
	}
}

func TestLinksBacklinksAndMoveRewrite(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	a := mustCreate(t, f.svc, "A.md", "# A\n")
	b := mustCreate(t, f.svc, "B.md", "# B\n\nSee [[A]], [[A|alias]], [[A#Section]], and ![[A]].\n")

	linkList, err := f.svc.Read(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The index key is (source, target, kind, fragment), so two plain links
	// with different aliases are one row.
	if len(linkList.Links) != 3 {
		t.Fatalf("links = %+v, want 3", linkList.Links)
	}
	for _, link := range linkList.Links {
		if link.Resolution != "exact" || link.ID != a.ID {
			t.Errorf("link = %+v, want exact to A", link)
		}
	}
	aLinks, err := f.svc.Read(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	backlinks := aLinks.Backlinks
	if len(backlinks) != 3 {
		// Backlinks are one row per link, all sourced from B.
		t.Fatalf("backlinks = %+v", backlinks)
	}
	for _, backlink := range backlinks {
		if backlink.ID != b.ID {
			t.Errorf("backlink source = %+v, want B", backlink)
		}
	}

	result, err := f.svc.Move(ctx, a.ID, "archive/Renamed.md", true)
	if err != nil {
		t.Fatalf("Move() error = %v", err)
	}
	if !result.Moved || result.LinksRewritten != 1 {
		t.Fatalf("move result = %+v", result)
	}
	if result.Note.Path != "archive/Renamed.md" {
		t.Errorf("moved path = %q", result.Note.Path)
	}
	rewritten, err := f.svc.Read(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[[Renamed]]", "[[Renamed|alias]]", "[[Renamed#Section]]", "![[Renamed]]"} {
		if !strings.Contains(rewritten.Body, want) {
			t.Errorf("body missing %q:\n%s", want, rewritten.Body)
		}
	}
	if strings.Contains(rewritten.Body, "[[A") {
		t.Errorf("body still has old targets:\n%s", rewritten.Body)
	}
	for _, link := range rewritten.Links {
		if link.Resolution != "suffix" && link.Resolution != "exact" {
			t.Errorf("link after move = %+v", link)
		}
	}

	if _, err := f.svc.ReadByPath(ctx, "A.md"); !errors.Is(err, notes.ErrNotFound) {
		t.Errorf("ReadByPath(old) error = %v, want ErrNotFound", err)
	}
}

func TestDirectoriesCreateAndMove(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	created, err := f.svc.CreateDir(ctx, "projects")
	if err != nil || created != "projects" {
		t.Fatalf("CreateDir() = %q, %v", created, err)
	}
	if _, err := f.svc.CreateDir(ctx, "projects"); !errors.Is(err, notes.ErrExists) {
		t.Errorf("CreateDir(existing) error = %v, want ErrExists", err)
	}

	a := mustCreate(t, f.svc, "projects/A.md", "# A\n")
	b := mustCreate(t, f.svc, "projects/B.md", "# B\n\nSee [[projects/A]] and [[A]].\n")
	outside := mustCreate(t, f.svc, "outside.md", "# Outside\n\nSee [[projects/A]] and [[A]].\n")

	result, err := f.svc.MoveDir(ctx, "projects", "archive/projects", true)
	if err != nil {
		t.Fatalf("MoveDir() error = %v", err)
	}
	if !result.Moved || result.NotesMoved != 2 || result.Path != "archive/projects" {
		t.Errorf("MoveDir() = %+v", result)
	}
	if result.LinksRewritten != 2 {
		t.Errorf("LinksRewritten = %d, want 2", result.LinksRewritten)
	}

	for _, note := range []notes.Note{a, b} {
		read, err := f.svc.Read(ctx, note.ID)
		if err != nil {
			t.Fatalf("Read(%s) error = %v", note.ID, err)
		}
		if !strings.HasPrefix(read.Path, "archive/projects/") {
			t.Errorf("moved path = %q", read.Path)
		}
	}
	if _, err := f.svc.ReadByPath(ctx, "projects/A.md"); !errors.Is(err, notes.ErrNotFound) {
		t.Errorf("ReadByPath(old dir) error = %v, want ErrNotFound", err)
	}

	rewrittenB, err := f.svc.Read(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rewrittenB.Body, "[[archive/projects/A]]") || !strings.Contains(rewrittenB.Body, "[[A]]") {
		t.Errorf("moved note links = %q", rewrittenB.Body)
	}
	rewrittenOutside, err := f.svc.Read(ctx, outside.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rewrittenOutside.Body, "[[archive/projects/A]]") || !strings.Contains(rewrittenOutside.Body, "[[A]]") {
		t.Errorf("outside links = %q", rewrittenOutside.Body)
	}
	for _, link := range rewrittenOutside.Links {
		if link.Target == "archive/projects/A" && link.Resolution != "exact" {
			t.Errorf("rewritten link = %+v", link)
		}
	}

	tree, err := f.svc.Tree(ctx, "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !treeContains(tree, "archive/projects") || treeContains(tree, "projects") {
		t.Errorf("tree after move = %+v", tree)
	}

	if _, err := f.svc.MoveDir(ctx, "archive/projects", "archive/projects/inside", true); !errors.Is(err, notes.ErrInvalidPath) {
		t.Errorf("MoveDir(into itself) error = %v, want ErrInvalidPath", err)
	}
	if _, err := f.svc.CreateDir(ctx, "archive/taken"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.MoveDir(ctx, "archive/projects", "archive/taken", true); !errors.Is(err, notes.ErrExists) {
		t.Errorf("MoveDir(existing) error = %v, want ErrExists", err)
	}
}

func treeContains(nodes []notes.TreeNode, path string) bool {
	for _, node := range nodes {
		if node.Path == path {
			return true
		}
		if treeContains(node.Children, path) {
			return true
		}
	}
	return false
}

func TestAttachmentsRejectNotes(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	if _, err := f.svc.Create(ctx, notes.CreateInput{Path: "attachments/sketch.md"}); !errors.Is(err, notes.ErrInvalidPath) {
		t.Fatalf("Create(attachments) error = %v, want ErrInvalidPath", err)
	}
	if _, err := f.svc.Create(ctx, notes.CreateInput{Path: "attachments/2026/sketch.md"}); !errors.Is(err, notes.ErrInvalidPath) {
		t.Fatalf("Create(attachments subfolder) error = %v, want ErrInvalidPath", err)
	}

	note := mustCreate(t, f.svc, "Kitchen", "# Kitchen\n")
	if _, err := f.svc.Move(ctx, note.ID, "attachments/Kitchen.md", true); !errors.Is(err, notes.ErrInvalidPath) {
		t.Fatalf("Move(into attachments) error = %v, want ErrInvalidPath", err)
	}
	if _, err := f.svc.CreateDir(ctx, "projects"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.MoveDir(ctx, "projects", "attachments/projects", true); !errors.Is(err, notes.ErrInvalidPath) {
		t.Fatalf("MoveDir(into attachments) error = %v, want ErrInvalidPath", err)
	}
	if _, err := f.svc.ReadByPath(ctx, note.Path); err != nil {
		t.Fatalf("rejected move changed the note: %v", err)
	}
}

func TestDeleteMovesToTrash(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	note := mustCreate(t, f.svc, "trash-me.md", "# Trash me\n")

	if _, err := f.svc.Delete(ctx, note.ID, `"nope"`); err == nil {
		t.Fatal("Delete(stale) error = nil")
	}
	result, err := f.svc.Delete(ctx, note.ID, note.ETag)
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if !result.Deleted || !strings.HasPrefix(result.TrashPath, ".lewnotes/trash/") {
		t.Fatalf("delete result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(f.dir, filepath.FromSlash(result.TrashPath))); err != nil {
		t.Errorf("trash file missing: %v", err)
	}
	if _, err := f.svc.Read(ctx, note.ID); !errors.Is(err, notes.ErrNotFound) {
		t.Errorf("Read(deleted) error = %v, want ErrNotFound", err)
	}
}

func TestTreeIncludesFiles(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	mustCreate(t, f.svc, "notes/Kitchen.md", "# Kitchen\n")
	if err := f.vault.WriteAtomic("attachments/sketch.png", []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := f.vault.WriteAtomic("notes/board.excalidraw", []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	tree, err := f.svc.Tree(ctx, "", 5)
	if err != nil {
		t.Fatalf("Tree() error = %v", err)
	}
	var byPath = map[string]notes.TreeNode{}
	var walk func([]notes.TreeNode)
	walk = func(nodes []notes.TreeNode) {
		for _, node := range nodes {
			byPath[node.Path] = node
			walk(node.Children)
		}
	}
	walk(tree)

	if byPath["notes"].Type != "dir" || byPath["attachments/sketch.png"].Type != "image" ||
		byPath["notes/board.excalidraw"].Type != "board" || byPath["notes/Kitchen.md"].Type != "note" {
		t.Errorf("tree types = %+v", byPath)
	}

	data, contentType, etag, err := f.svc.ReadFile(ctx, "attachments/sketch.png")
	if err != nil || string(data) != "png" || contentType != "image/png" || etag == "" {
		t.Errorf("ReadFile() = %q, %q, %q, %v", data, contentType, etag, err)
	}
	if _, _, _, err := f.svc.ReadFile(ctx, ".lewnotes/trash/x.md"); !errors.Is(err, notes.ErrInvalidPath) {
		t.Errorf("ReadFile(reserved) error = %v", err)
	}
}

func TestTreeSortsFilesBeforeDirectories(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	mustCreate(t, f.svc, "zebra.md", "# Zebra\n")
	mustCreate(t, f.svc, "Beta.md", "# Beta\n")
	mustCreate(t, f.svc, "alpha/nested.md", "# Nested\n")

	tree, err := f.svc.Tree(ctx, "", 2)
	if err != nil {
		t.Fatalf("Tree() error = %v", err)
	}
	var got []string
	for _, node := range tree {
		switch node.Path {
		case "Beta.md", "zebra.md", "alpha":
			got = append(got, node.Path)
		}
	}
	if want := []string{"Beta.md", "zebra.md", "alpha"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("tree order = %v, want %v", got, want)
	}
}

func TestRebuildReproducesIDsAndLinks(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	a := mustCreate(t, f.svc, "A.md", "# A\n")
	b := mustCreate(t, f.svc, "sub/B.md", "# B\n\nTo [[A]] and [[Missing]].\n")

	rebuilt, err := index.Open(filepath.Join(t.TempDir(), "rebuilt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer rebuilt.Close()
	scanner := index.NewScanner(f.vault, rebuilt, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	stats, err := scanner.Run(ctx, true)
	if err != nil {
		t.Fatalf("scan error = %v", err)
	}
	if stats.Total != 2 || stats.Indexed != 2 {
		t.Errorf("stats = %+v", stats)
	}

	for _, want := range []index.NoteRef{{ID: a.ID, Path: a.Path}, {ID: b.ID, Path: b.Path}} {
		row, err := rebuilt.NoteByID(ctx, want.ID)
		if err != nil {
			t.Fatalf("rebuilt NoteByID(%s) error = %v", want.ID, err)
		}
		if row.Path != want.Path {
			t.Errorf("rebuilt path = %q, want %q", row.Path, want.Path)
		}
	}

	links, err := rebuilt.LinksFrom(ctx, b.ID)
	if err != nil || len(links) != 2 {
		t.Fatalf("rebuilt links = %+v, %v", links, err)
	}
	if links[0].Resolution != "exact" || links[0].ID != a.ID {
		t.Errorf("resolved link = %+v", links[0])
	}
	if links[1].Resolution != "unresolved" {
		t.Errorf("ghost link = %+v", links[1])
	}

	// A full rescan over unchanged files keeps the same IDs and rows.
	if _, err := scanner.Run(ctx, true); err != nil {
		t.Fatalf("second scan error = %v", err)
	}
	if count, _ := rebuilt.CountNotes(ctx); count != 2 {
		t.Errorf("count after rebuild = %d", count)
	}
}
