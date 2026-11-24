package vault

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func newVault(t *testing.T) *Vault {
	t.Helper()
	v, err := Open(t.TempDir(), []string{"node_modules/**"})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return v
}

func TestResolveRejectsUnsafePaths(t *testing.T) {
	v := newVault(t)

	for _, rel := range []string{
		"",
		".",
		"..",
		"../secret.md",
		"a/../../secret.md",
		"/etc/passwd",
		".lewnotes/index.db",
	} {
		if _, err := v.Resolve(rel); err == nil {
			t.Errorf("Resolve(%q) error = nil, want rejection", rel)
		}
	}
}

func TestResolveAllowsNestedNewPath(t *testing.T) {
	v := newVault(t)

	full, err := v.Resolve("notes/2026/plan.md")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !strings.HasPrefix(full, v.Root()) {
		t.Errorf("resolved %q outside root %q", full, v.Root())
	}
}

func TestResolveRejectsSymlinkEscape(t *testing.T) {
	v := newVault(t)

	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(v.Root(), "escape")); err != nil {
		t.Fatal(err)
	}

	_, err := v.Resolve("escape/secret.md")
	if err == nil {
		t.Fatal("Resolve() error = nil, want symlink escape rejection")
	}
	if !errors.Is(err, ErrEscapesVault) {
		t.Errorf("error = %v, want ErrEscapesVault", err)
	}
}

func TestWriteAtomicRoundTrip(t *testing.T) {
	v := newVault(t)

	if err := v.WriteAtomic("notes/a.md", []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteAtomic() error = %v", err)
	}
	data, info, err := v.Read("notes/a.md")
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("content = %q, want hello", data)
	}
	if info.Size() != 5 {
		t.Errorf("size = %d, want 5", info.Size())
	}

	entries, err := os.ReadDir(filepath.Join(v.Root(), "notes"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("directory has %d entries, want only a.md (no temp files)", len(entries))
	}
}

func TestWalkSkipsIgnored(t *testing.T) {
	v := newVault(t)

	files := map[string]string{
		"notes/a.md":            "a",
		"todos/t.md":            "t",
		"README.txt":            "readme",
		".obsidian/app.json":    "{}",
		"node_modules/pkg/x.md": "x",
	}
	for rel, body := range files {
		full := filepath.Join(v.Root(), filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	var got []string
	if err := v.Walk(func(rel string, _ os.FileInfo) error {
		got = append(got, rel)
		return nil
	}); err != nil {
		t.Fatalf("Walk() error = %v", err)
	}
	sort.Strings(got)

	want := []string{"README.txt", "notes/a.md", "todos/t.md"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Walk() = %v, want %v", got, want)
	}
}
