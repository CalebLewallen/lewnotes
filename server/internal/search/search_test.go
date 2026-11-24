package search

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseFiltersAndText(t *testing.T) {
	q, err := Parse(`kitchen "deep sink" tag:home -tag:draft path:notes/ -path:archive is:unresolved has:embed -old`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !reflect.DeepEqual(q.Text, []string{"kitchen", `"deep sink"`}) {
		t.Errorf("Text = %#v", q.Text)
	}
	if !reflect.DeepEqual(q.NegText, []string{"old"}) {
		t.Errorf("NegText = %#v", q.NegText)
	}
	if !reflect.DeepEqual(q.Tags, []string{"home"}) || !reflect.DeepEqual(q.NegTags, []string{"draft"}) {
		t.Errorf("tags = %#v / %#v", q.Tags, q.NegTags)
	}
	if !reflect.DeepEqual(q.Paths, []string{"notes/"}) || !reflect.DeepEqual(q.NegPaths, []string{"archive"}) {
		t.Errorf("paths = %#v / %#v", q.Paths, q.NegPaths)
	}
	if !reflect.DeepEqual(q.Is, []string{"unresolved"}) || !reflect.DeepEqual(q.Has, []string{"embed"}) {
		t.Errorf("is/has = %#v / %#v", q.Is, q.Has)
	}

	match := q.FTSMatch()
	if !strings.Contains(match, `"kitchen"*`) || !strings.Contains(match, `"deep sink"`) ||
		!strings.Contains(match, `NOT "old"*`) {
		t.Errorf("FTSMatch() = %q", match)
	}
}

func TestParseErrors(t *testing.T) {
	for _, input := range []string{
		"tag:",
		"is:resolved",
		"has:attachment",
		"todo:open",
		"project:home",
		"unknown:value",
		"-lonely",
	} {
		if _, err := Parse(input); err == nil {
			t.Errorf("Parse(%q) error = nil, want error", input)
		}
	}
}

func TestFTSQuoting(t *testing.T) {
	q, err := Parse(`a"b`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := q.FTSMatch(), `"a""b"`; got != want {
		t.Errorf("FTSMatch() = %q, want %q", got, want)
	}

	// A hyphenated word tokenizes into several words; prefixing the phrase is
	// invalid FTS5 syntax.
	q, err = Parse("smoke-123")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := q.FTSMatch(), `"smoke-123"`; got != want {
		t.Errorf("FTSMatch(hyphen) = %q, want %q", got, want)
	}
}
