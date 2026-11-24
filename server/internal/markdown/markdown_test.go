package markdown

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	nextID = "01ARZ3NDEKTSV4RRFFQ69G5FAW"
)

func TestGoldenPairs(t *testing.T) {
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".golden.md") {
			continue
		}
		base := strings.TrimSuffix(name, ".md")

		in, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join("testdata", base+".golden.md"))
		if err != nil {
			t.Fatal(err)
		}

		var got []byte
		var changed bool
		if strings.HasPrefix(base, "setid-") {
			got, changed, err = SetID(in, nextID)
		} else {
			got, changed, err = EnsureID(in, testID)
		}
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !changed {
			t.Errorf("%s: changed = false, want true", name)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s:\n got %q\nwant %q", name, got, want)
		}
	}
}

func TestEnsureIDIsIdempotent(t *testing.T) {
	in := []byte("---\nid: " + testID + "\ntitle: A\n---\nBody\n")

	out, changed, err := EnsureID(in, nextID)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("changed = true, want false for an existing valid id")
	}
	if !bytes.Equal(out, in) {
		t.Errorf("output changed: %q", out)
	}
}

func TestEnsureIDPreservesCRLF(t *testing.T) {
	in := []byte("---\r\ntitle: Win\r\n---\r\nBody\r\n")

	out, changed, err := EnsureID(in, testID)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}
	want := "---\r\ntitle: Win\r\nid: " + testID + "\r\n---\r\nBody\r\n"
	if string(out) != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

func TestUnterminatedFrontmatterIsBody(t *testing.T) {
	in := []byte("---\ntitle: oops\n\nBody\n")

	out, _, err := EnsureID(in, testID)
	if err != nil {
		t.Fatal(err)
	}
	want := "---\nid: " + testID + "\n---\n" + string(in)
	if string(out) != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

func TestParseFrontmatterAndTitle(t *testing.T) {
	data := []byte("---\ntitle: Kitchen\ntags:\n  - home\n  - diy\n---\n\n# Heading\n")

	doc, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if !doc.HasFrontmatter {
		t.Error("HasFrontmatter = false, want true")
	}
	if doc.Title() != "Kitchen" {
		t.Errorf("Title() = %q, want Kitchen", doc.Title())
	}
	if !bytes.HasPrefix(doc.Body, []byte("\n# Heading")) {
		t.Errorf("Body = %q, want it to start after the closing delimiter", doc.Body)
	}
	if doc.ID() != "" {
		t.Errorf("ID() = %q, want empty", doc.ID())
	}
}

func TestTitleFromHeadingSkipsCodeFence(t *testing.T) {
	doc, err := Parse([]byte("```\n# not this\n```\n\n# Real Title\n"))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title() != "Real Title" {
		t.Errorf("Title() = %q, want Real Title", doc.Title())
	}
}

func TestValidID(t *testing.T) {
	if !ValidID(testID) {
		t.Errorf("ValidID(%q) = false, want true", testID)
	}
	for _, bad := range []string{"", "not-a-ulid", "01ARZ3NDEKTSV4RRFFQ69G5FA", "01ARZ3NDEKTSV4RRFFQ69G5FAVV"} {
		if ValidID(bad) {
			t.Errorf("ValidID(%q) = true, want false", bad)
		}
	}
}

func TestExcerptCollapsesWhitespace(t *testing.T) {
	got := Excerpt([]byte("# Title\n\nHello   world,\nthis  is a note."), 20)
	if got != "# Title Hello world," {
		t.Errorf("Excerpt() = %q", got)
	}
}
